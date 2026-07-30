package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

const runtimeHeartbeatInterval = 5 * time.Second

func (api *API) runtimeEventStream(response http.ResponseWriter, request *http.Request) {
	if api.runtimeEvents == nil {
		writeError(response, http.StatusServiceUnavailable, "runtime_events_unavailable", "Runtime events are unavailable", "")
		return
	}
	after, replay, ok := eventCursor(response, request)
	if !ok {
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "Streaming is unavailable", "")
		return
	}

	var window runtimeevents.Window
	var updates <-chan runtimeevents.Event
	var cancel func()
	if replay {
		window, updates, cancel = api.runtimeEvents.Subscribe(after)
	} else {
		window, updates, cancel = api.runtimeEvents.SubscribeCurrent()
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
	if window.Reset && !writeSSE(response, flusher, "reset", 0, map[string]uint64{
		"oldest_id": window.OldestID,
		"newest_id": window.NewestID,
	}) {
		return
	}
	for _, event := range window.Events {
		if !writeSSE(response, flusher, "runtime", event.ID, event) {
			return
		}
	}
	if !writeSSE(response, flusher, "ready", window.NewestID, map[string]uint64{
		"newest_id": window.NewestID,
	}) {
		return
	}

	heartbeat := time.NewTicker(runtimeHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-updates:
			if !open || !writeSSE(response, flusher, "runtime", event.ID, event) {
				return
			}
		case observedAt := <-heartbeat.C:
			if !writeEventHeartbeat(response, flusher, observedAt) {
				return
			}
		}
	}
}

func (api *API) publishRuntimeResources(resources ...runtimeevents.Resource) {
	publisher, ok := api.runtimeEvents.(runtimeevents.Publisher)
	if !ok {
		return
	}
	publisher.Publish(runtimeevents.Event{Resources: resources})
}
