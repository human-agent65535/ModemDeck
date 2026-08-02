package agentclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type DiagnosticLogEntry struct {
	ID        uint64         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Source    string         `json:"source"`
	Component string         `json:"component"`
	Caller    string         `json:"caller,omitempty"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type DiagnosticLogHandlers struct {
	OnOpen  func()
	OnReset func(oldestID, newestID uint64)
	OnEntry func(DiagnosticLogEntry)
}

func (client *Client) WatchDiagnosticLogs(
	ctx context.Context,
	after uint64,
	handlers DiagnosticLogHandlers,
) error {
	if client == nil || client.httpClient == nil {
		return errors.New("host agent client is not initialized")
	}
	if handlers.OnEntry == nil {
		return ErrInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	path := "/v1/diagnostics/logs/stream"
	if after > 0 {
		path += "?" + url.Values{"after": {strconv.FormatUint(after, 10)}}.Encode()
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://modemdeck-agent"+path,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create host agent diagnostic log request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set(controlLeaseHeader, client.controllerID)
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("request host agent diagnostic logs: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
		if readErr != nil {
			return fmt.Errorf("read host agent diagnostic log error: %w", readErr)
		}
		if len(responseBody) > maxResponseBodyBytes {
			return fmt.Errorf("%w: host agent response is too large", ErrProtocol)
		}
		return decodeOperationError(response.StatusCode, responseBody)
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return fmt.Errorf("%w: host agent diagnostic log stream has content type %q", ErrProtocol, contentType)
	}
	if handlers.OnOpen != nil {
		handlers.OnOpen()
	}

	type scanResult struct {
		line string
		err  error
		done bool
	}
	scanContext, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	results := make(chan scanResult)
	scannerDone := make(chan struct{})
	go func() {
		defer close(scannerDone)
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 256<<10)
		for scanner.Scan() {
			select {
			case results <- scanResult{line: scanner.Text()}:
			case <-scanContext.Done():
				return
			}
		}
		result := scanResult{err: scanner.Err(), done: true}
		select {
		case results <- result:
		case <-scanContext.Done():
		}
	}()

	idleLimit := client.diagnosticLogIdleLimit
	if idleLimit <= 0 {
		idleLimit = defaultDiagnosticLogIdleLimit
	}
	idle := time.NewTimer(idleLimit)
	defer idle.Stop()
	eventName := ""
	dataLines := []string{}
	for {
		select {
		case <-ctx.Done():
			cancelScan()
			_ = response.Body.Close()
			<-scannerDone
			return nil
		case <-idle.C:
			cancelScan()
			_ = response.Body.Close()
			<-scannerDone
			return fmt.Errorf("read host agent diagnostic logs: no activity for %s", idleLimit)
		case result := <-results:
			if result.done {
				if ctx.Err() != nil {
					return nil
				}
				if result.err != nil {
					return fmt.Errorf("read host agent diagnostic logs: %w", result.err)
				}
				return io.ErrUnexpectedEOF
			}
			resetTimer(idle, idleLimit)
			line := strings.TrimSuffix(result.line, "\r")
			if line == "" {
				if err := dispatchDiagnosticLogEvent(eventName, strings.Join(dataLines, "\n"), handlers); err != nil {
					return err
				}
				eventName = ""
				dataLines = dataLines[:0]
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if value, found := strings.CutPrefix(line, "event:"); found {
				eventName = strings.TrimSpace(value)
				continue
			}
			if value, found := strings.CutPrefix(line, "data:"); found {
				dataLines = append(dataLines, strings.TrimSpace(value))
			}
		}
	}
}

func dispatchDiagnosticLogEvent(
	eventName string,
	data string,
	handlers DiagnosticLogHandlers,
) error {
	if eventName == "" || data == "" {
		return nil
	}
	switch eventName {
	case "reset":
		var reset struct {
			OldestID uint64 `json:"oldest_id"`
			NewestID uint64 `json:"newest_id"`
		}
		if err := json.Unmarshal([]byte(data), &reset); err != nil {
			return fmt.Errorf("%w: decode host agent diagnostic log reset: %v", ErrProtocol, err)
		}
		if handlers.OnReset != nil {
			handlers.OnReset(reset.OldestID, reset.NewestID)
		}
	case "log":
		var entry DiagnosticLogEntry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			return fmt.Errorf("%w: decode host agent diagnostic log entry: %v", ErrProtocol, err)
		}
		if entry.ID == 0 || entry.Timestamp.IsZero() || strings.TrimSpace(entry.Component) == "" ||
			strings.TrimSpace(entry.Message) == "" || !diagnosticLogLevel(entry.Level) {
			return fmt.Errorf("%w: invalid host agent diagnostic log entry", ErrProtocol)
		}
		handlers.OnEntry(entry)
	}
	return nil
}

func diagnosticLogLevel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
