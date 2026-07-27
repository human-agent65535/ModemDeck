package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	defaultDiagnosticLogLimit = 500
	maxDiagnosticLogLimit     = diagnostics.DefaultLogCapacity
	maxDiagnosticSearchLength = 200
	maxDiagnosticFilterLength = 64
)

type diagnosticAvailability struct {
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type diagnosticAgent struct {
	Connected      bool                     `json:"connected"`
	Provider       string                   `json:"provider"`
	AgentVersion   string                   `json:"agent_version"`
	RuntimeVersion string                   `json:"runtime_version"`
	BootEpoch      string                   `json:"boot_epoch"`
	Revision       string                   `json:"revision"`
	ObservedAt     string                   `json:"observed_at"`
	LastError      string                   `json:"last_error,omitempty"`
	Capabilities   agentclient.Capabilities `json:"capabilities"`
}

type diagnosticCall struct {
	ID              string `json:"id"`
	LineID          string `json:"line_id"`
	EndpointLineID  string `json:"endpoint_line_id,omitempty"`
	Direction       string `json:"direction"`
	Phase           string `json:"phase"`
	Bearer          string `json:"bearer"`
	MediaAvailable  bool   `json:"media_available"`
	AudioEncoding   string `json:"audio_encoding,omitempty"`
	AudioResolution string `json:"audio_resolution,omitempty"`
	AudioRate       uint32 `json:"audio_rate,omitempty"`
}

type diagnosticsResponse struct {
	Status      string                 `json:"status"`
	ObservedAt  string                 `json:"observed_at"`
	Database    diagnosticAvailability `json:"database"`
	HostAgent   diagnosticAgent        `json:"host_agent"`
	Calls       diagnosticAvailability `json:"call_runtime"`
	Lines       []store.LineSummary    `json:"lines"`
	ActiveCalls []diagnosticCall       `json:"active_calls"`
}

type diagnosticLogResponse struct {
	Entries   []diagnostics.LogEntry `json:"entries"`
	OldestID  uint64                 `json:"oldest_id"`
	NewestID  uint64                 `json:"newest_id"`
	Truncated bool                   `json:"truncated"`
}

type diagnosticLogFilter struct {
	after     uint64
	limit     int
	level     string
	component string
	search    string
}

func (api *API) diagnostics(response http.ResponseWriter, request *http.Request) {
	result := diagnosticsResponse{
		Status:      "ok",
		ObservedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Database:    diagnosticAvailability{Available: true},
		Calls:       diagnosticAvailability{Available: true},
		Lines:       []store.LineSummary{},
		ActiveCalls: []diagnosticCall{},
	}
	if err := api.repository.Ping(request.Context()); err != nil {
		result.Status = "unavailable"
		result.Database = diagnosticAvailability{Error: err.Error()}
	}
	if api.communications == nil {
		result.HostAgent.LastError = "communication service is not configured"
		result.Calls = diagnosticAvailability{Error: "communication service is not configured"}
		if result.Status == "ok" {
			result.Status = "degraded"
		}
		writeJSON(response, http.StatusOK, result)
		return
	}

	status, statusErr := api.communications.Status(request.Context())
	result.HostAgent = diagnosticAgentFromStatus(status, statusErr)
	result.Lines = status.Lines
	if statusErr != nil || !status.Connected {
		if result.Status == "ok" {
			result.Status = "degraded"
		}
	}
	calls, callsErr := api.communications.ActiveCalls(request.Context())
	if callsErr != nil {
		result.Calls = diagnosticAvailability{Error: callsErr.Error()}
		if result.Status == "ok" {
			result.Status = "degraded"
		}
	} else {
		for _, call := range calls {
			result.ActiveCalls = append(result.ActiveCalls, diagnosticCall{
				ID:              call.ID,
				LineID:          call.LineID,
				EndpointLineID:  call.EndpointLineID,
				Direction:       call.Direction,
				Phase:           call.Phase,
				Bearer:          call.Bearer,
				MediaAvailable:  call.MediaAvailable,
				AudioEncoding:   call.AudioEncoding,
				AudioResolution: call.AudioResolution,
				AudioRate:       call.AudioRate,
			})
		}
	}
	writeJSON(response, http.StatusOK, result)
}

func diagnosticAgentFromStatus(status communication.Status, statusErr error) diagnosticAgent {
	result := diagnosticAgent{
		Connected:      status.Connected,
		Provider:       status.ProviderName,
		AgentVersion:   status.AgentVersion,
		RuntimeVersion: status.RuntimeVersion,
		BootEpoch:      status.BootEpoch,
		Revision:       status.Revision,
		LastError:      status.LastError,
		Capabilities:   status.Capabilities,
	}
	if !status.ObservedAt.IsZero() {
		result.ObservedAt = status.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	if statusErr != nil {
		result.LastError = statusErr.Error()
	}
	return result
}

func (api *API) diagnosticLogHistory(response http.ResponseWriter, request *http.Request) {
	filter, ok := parseDiagnosticLogFilter(response, request)
	if !ok {
		return
	}
	if api.diagnosticLogs == nil {
		writeError(response, http.StatusServiceUnavailable, "logs_unavailable", "Runtime logs are unavailable", "")
		return
	}
	writeJSON(response, http.StatusOK, filterDiagnosticWindow(api.diagnosticLogs.Snapshot(filter.after), filter))
}

func (api *API) diagnosticLogStream(response http.ResponseWriter, request *http.Request) {
	filter, ok := parseDiagnosticLogFilter(response, request)
	if !ok {
		return
	}
	if api.diagnosticLogs == nil {
		writeError(response, http.StatusServiceUnavailable, "logs_unavailable", "Runtime logs are unavailable", "")
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "Streaming is unavailable", "")
		return
	}
	window, updates, cancel := api.diagnosticLogs.Subscribe(filter.after)
	defer cancel()

	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(response)
	_ = controller.SetWriteDeadline(time.Time{})
	if _, err := fmt.Fprint(response, "retry: 2000\n\n"); err != nil {
		return
	}
	if window.Truncated {
		if !writeDiagnosticSSE(response, flusher, "reset", window.OldestID, map[string]uint64{
			"oldest_id": window.OldestID,
			"newest_id": window.NewestID,
		}) {
			return
		}
	}
	for _, entry := range window.Entries {
		if matchesDiagnosticLog(entry, filter) &&
			!writeDiagnosticSSE(response, flusher, "log", entry.ID, entry) {
			return
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case entry, open := <-updates:
			if !open {
				return
			}
			if matchesDiagnosticLog(entry, filter) &&
				!writeDiagnosticSSE(response, flusher, "log", entry.ID, entry) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(response, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (api *API) downloadDiagnosticLogs(response http.ResponseWriter, request *http.Request) {
	filter, ok := parseDiagnosticLogFilter(response, request)
	if !ok {
		return
	}
	if api.diagnosticLogs == nil {
		writeError(response, http.StatusServiceUnavailable, "logs_unavailable", "Runtime logs are unavailable", "")
		return
	}
	filter.limit = maxDiagnosticLogLimit
	window := filterDiagnosticWindow(api.diagnosticLogs.Snapshot(filter.after), filter)
	response.Header().Set("Content-Type", "application/x-ndjson")
	response.Header().Set(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=\"modemdeck-logs-%s.ndjson\"", time.Now().UTC().Format("20060102-150405")),
	)
	response.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(response)
	for _, entry := range window.Entries {
		if err := encoder.Encode(entry); err != nil {
			return
		}
	}
}

func parseDiagnosticLogFilter(response http.ResponseWriter, request *http.Request) (diagnosticLogFilter, bool) {
	filter := diagnosticLogFilter{
		limit:     defaultDiagnosticLogLimit,
		level:     strings.ToLower(strings.TrimSpace(request.URL.Query().Get("level"))),
		component: strings.ToLower(strings.TrimSpace(request.URL.Query().Get("component"))),
		search:    strings.ToLower(strings.TrimSpace(request.URL.Query().Get("search"))),
	}
	if value := strings.TrimSpace(request.URL.Query().Get("after")); value != "" {
		after, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_argument", "after must be an unsigned integer", "after")
			return diagnosticLogFilter{}, false
		}
		filter.after = after
	} else if value := strings.TrimSpace(request.Header.Get("Last-Event-ID")); value != "" {
		after, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_argument", "Last-Event-ID must be an unsigned integer", "Last-Event-ID")
			return diagnosticLogFilter{}, false
		}
		filter.after = after
	}
	if value := strings.TrimSpace(request.URL.Query().Get("limit")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maxDiagnosticLogLimit {
			writeError(response, http.StatusBadRequest, "invalid_argument", "limit must be between 1 and 2000", "limit")
			return diagnosticLogFilter{}, false
		}
		filter.limit = limit
	}
	switch filter.level {
	case "", "debug", "info", "warn", "error":
	default:
		writeError(response, http.StatusBadRequest, "invalid_argument", "level must be debug, info, warn, or error", "level")
		return diagnosticLogFilter{}, false
	}
	if len(filter.component) > maxDiagnosticFilterLength {
		writeError(response, http.StatusBadRequest, "invalid_argument", "component is too long", "component")
		return diagnosticLogFilter{}, false
	}
	if len(filter.search) > maxDiagnosticSearchLength {
		writeError(response, http.StatusBadRequest, "invalid_argument", "search is too long", "search")
		return diagnosticLogFilter{}, false
	}
	return filter, true
}

func filterDiagnosticWindow(window diagnostics.LogWindow, filter diagnosticLogFilter) diagnosticLogResponse {
	entries := make([]diagnostics.LogEntry, 0, len(window.Entries))
	for _, entry := range window.Entries {
		if matchesDiagnosticLog(entry, filter) {
			entries = append(entries, entry)
		}
	}
	truncated := window.Truncated
	if len(entries) > filter.limit {
		entries = entries[len(entries)-filter.limit:]
		truncated = true
	}
	return diagnosticLogResponse{
		Entries:   entries,
		OldestID:  window.OldestID,
		NewestID:  window.NewestID,
		Truncated: truncated,
	}
}

func matchesDiagnosticLog(entry diagnostics.LogEntry, filter diagnosticLogFilter) bool {
	if filter.level != "" && !strings.EqualFold(entry.Level, filter.level) {
		return false
	}
	if filter.component != "" && !strings.EqualFold(entry.Component, filter.component) {
		return false
	}
	if filter.search == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Message), filter.search) ||
		strings.Contains(strings.ToLower(entry.Component), filter.search) ||
		strings.Contains(strings.ToLower(entry.Caller), filter.search) {
		return true
	}
	for key, value := range entry.Fields {
		if strings.Contains(strings.ToLower(key), filter.search) ||
			strings.Contains(strings.ToLower(fmt.Sprint(value)), filter.search) {
			return true
		}
	}
	return false
}

func writeDiagnosticSSE(
	response http.ResponseWriter,
	flusher http.Flusher,
	event string,
	id uint64,
	value any,
) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(response, "id: %d\nevent: %s\ndata: %s\n\n", id, event, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
