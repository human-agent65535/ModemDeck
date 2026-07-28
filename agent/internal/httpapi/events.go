package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const eventHeartbeatInterval = 2 * time.Second

func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	if h.changes == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"subscribe_changes",
			"",
			"provider does not expose change events",
		)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeAPIError(
			w,
			http.StatusInternalServerError,
			domain.ErrorInternal,
			"subscribe_changes",
			"",
			"streaming response is unavailable",
		)
		return
	}
	events, err := h.changes.SubscribeChanges(r.Context())
	if err != nil {
		h.writeError(w, err, "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(eventHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			payload, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: change\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
