package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
)

const agentDiagnosticLogReconnectDelay = 2 * time.Second

func mirrorAgentDiagnosticLogs(
	ctx context.Context,
	client *agentclient.Client,
	sink *diagnostics.LogBuffer,
	logger *slog.Logger,
) {
	if client == nil || sink == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	var cursor uint64
	connectedOnce := false
	disconnected := false
	for {
		err := client.WatchDiagnosticLogs(ctx, cursor, agentclient.DiagnosticLogHandlers{
			OnReset: func(oldestID, _ uint64) {
				if oldestID > 0 {
					cursor = oldestID - 1
				} else {
					cursor = 0
				}
			},
			OnEntry: func(entry agentclient.DiagnosticLogEntry) {
				if !connectedOnce {
					logger.Info("hardware diagnostic log stream connected")
					connectedOnce = true
				} else if disconnected {
					logger.Info("hardware diagnostic log stream recovered")
				}
				disconnected = false
				cursor = entry.ID
				source := strings.TrimSpace(entry.Source)
				if source == "" {
					source = "hardware-agent"
				}
				sink.AppendExternal(diagnostics.LogEntry{
					Timestamp: entry.Timestamp,
					Level:     entry.Level,
					Source:    source,
					Component: entry.Component,
					Caller:    entry.Caller,
					Message:   entry.Message,
					Fields:    entry.Fields,
				})
			},
		})
		if ctx.Err() != nil {
			return
		}
		if agentDiagnosticLogsUnsupported(err) {
			logger.Info("hardware diagnostic log stream is unavailable on this agent version")
			return
		}
		if err != nil && !disconnected {
			logger.Warn("hardware diagnostic log stream interrupted", "error", err)
			disconnected = true
		}
		timer := time.NewTimer(agentDiagnosticLogReconnectDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func agentDiagnosticLogsUnsupported(err error) bool {
	var operationError *agentclient.OperationError
	if !errors.As(err, &operationError) {
		return false
	}
	return operationError.Status == http.StatusNotFound ||
		strings.EqualFold(operationError.Code, "not_supported")
}
