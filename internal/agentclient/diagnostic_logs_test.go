package agentclient

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchDiagnosticLogsReplaysResetAndEntries(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/diagnostics/logs/stream" || request.URL.Query().Get("after") != "7" {
			http.NotFound(response, request)
			return
		}
		flusher := response.(http.Flusher)
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "event: reset\ndata: {\"oldest_id\":4,\"newest_id\":8}\n\n")
		_, _ = fmt.Fprint(response, "event: log\ndata: {\"id\":8,\"timestamp\":\"2026-08-01T12:00:00Z\",\"level\":\"info\",\"source\":\"hardware-agent\",\"component\":\"modemmanager\",\"message\":\"line state changed\",\"fields\":{\"line_id\":\"line-1\"}}\n\n")
		flusher.Flush()
		<-request.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	var opened atomic.Bool
	var reset atomic.Bool
	err := client.WatchDiagnosticLogs(ctx, 7, DiagnosticLogHandlers{
		OnOpen: func() { opened.Store(true) },
		OnReset: func(oldestID, newestID uint64) {
			reset.Store(oldestID == 4 && newestID == 8)
		},
		OnEntry: func(entry DiagnosticLogEntry) {
			if entry.ID != 8 || entry.Source != "hardware-agent" || entry.Fields["line_id"] != "line-1" {
				t.Errorf("entry = %+v", entry)
			}
			cancel()
		},
	})
	if err != nil {
		t.Fatalf("WatchDiagnosticLogs() error = %v", err)
	}
	if !opened.Load() || !reset.Load() {
		t.Fatalf("opened = %v, reset = %v", opened.Load(), reset.Load())
	}
}

func TestWatchDiagnosticLogsFailsWhenStreamStalls(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		response.(http.Flusher).Flush()
		<-request.Context().Done()
	}))
	client.diagnosticLogIdleLimit = 25 * time.Millisecond
	if err := client.WatchDiagnosticLogs(context.Background(), 0, DiagnosticLogHandlers{
		OnEntry: func(DiagnosticLogEntry) {},
	}); err == nil {
		t.Fatal("WatchDiagnosticLogs() accepted a stalled stream")
	}
}
