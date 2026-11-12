package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const diagnosticLogHeartbeatInterval = 15 * time.Second

func (h *handler) diagnosticLogStream(w http.ResponseWriter, r *http.Request) {
	if h.diagnosticLogs == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"stream_diagnostic_logs",
			"",
			"hardware diagnostic logs are unavailable",
		)
		return
	}
	after, ok := h.diagnosticLogCursor(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeAPIError(
			w,
			http.StatusInternalServerError,
			domain.ErrorInternal,
			"stream_diagnostic_logs",
			"",
			"streaming response is unavailable",
		)
		return
	}
	window, updates, cancel := h.diagnosticLogs.Subscribe(after)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	if !startSSE(w) {
		return
	}
	if window.Truncated && !writeSSE(w, flusher, "reset", map[string]uint64{
		"oldest_id": window.OldestID,
		"newest_id": window.NewestID,
	}) {
		return
	}
	for _, entry := range window.Entries {
		if !writeSSE(w, flusher, "log", entry) {
			return
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(diagnosticLogHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case entry, open := <-updates:
			if !open || !writeSSE(w, flusher, "log", entry) {
				return
			}
		case <-heartbeat.C:
			if !writeSSEKeepalive(w, flusher) {
				return
			}
		}
	}
}

func (h *handler) diagnosticLogCursor(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("after"))
	if value == "" {
		value = strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	}
	if value == "" {
		return 0, true
	}
	after, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		h.writeAPIError(
			w,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"stream_diagnostic_logs",
			"",
			"after must be an unsigned integer",
		)
		return 0, false
	}
	return after, true
}
