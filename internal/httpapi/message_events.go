package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

func (api *API) messageEventStream(response http.ResponseWriter, request *http.Request) {
	if api.messageEvents == nil {
		writeError(response, http.StatusServiceUnavailable, "message_events_unavailable", "Message events are unavailable", "")
		return
	}
	after, replay, ok := messageEventCursor(response, request)
	if !ok {
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "Streaming is unavailable", "")
		return
	}

	var window messageevents.Window
	var updates <-chan messageevents.IncomingSMS
	var cancel func()
	if replay {
		window, updates, cancel = api.messageEvents.Subscribe(after)
	} else {
		window, updates, cancel = api.messageEvents.SubscribeCurrent()
	}
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
	if window.Reset && !writeMessageSSE(response, flusher, "reset", 0, map[string]uint64{
		"oldest_id": window.OldestID,
		"newest_id": window.NewestID,
	}) {
		return
	}
	for _, event := range window.Events {
		if !writeMessageSSE(response, flusher, "sms", event.ID, event) {
			return
		}
	}
	if !writeMessageSSE(response, flusher, "ready", window.NewestID, map[string]uint64{
		"newest_id": window.NewestID,
	}) {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-updates:
			if !open || !writeMessageSSE(response, flusher, "sms", event.ID, event) {
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

func messageEventCursor(
	response http.ResponseWriter,
	request *http.Request,
) (after uint64, replay bool, ok bool) {
	value := strings.TrimSpace(request.URL.Query().Get("after"))
	field := "after"
	if value == "" {
		value = strings.TrimSpace(request.Header.Get("Last-Event-ID"))
		field = "Last-Event-ID"
	}
	if value == "" {
		return 0, false, true
	}
	after, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_argument", field+" must be an unsigned integer", field)
		return 0, false, false
	}
	return after, true, true
}

func writeMessageSSE(
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
	if id > 0 {
		if _, err := fmt.Fprintf(response, "id: %d\n", id); err != nil {
			return false
		}
	} else if event == "reset" {
		if _, err := fmt.Fprint(response, "id: 0\n"); err != nil {
			return false
		}
	}
	if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
