package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

const messageHeartbeatInterval = 15 * time.Second

func (api *API) messageEventStream(response http.ResponseWriter, request *http.Request) {
	if api.messageEvents == nil {
		writeError(response, http.StatusServiceUnavailable, "message_events_unavailable", "Message events are unavailable", "")
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "Streaming is unavailable", "")
		return
	}
	if !api.authorizeEventStream(response, request, false) {
		return
	}
	release, ok := api.acquireEventStream(response, request)
	if !ok {
		return
	}
	defer release()

	updates, cancel := api.messageEvents.Subscribe()
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
	flusher.Flush()

	heartbeat := time.NewTicker(messageHeartbeatInterval)
	defer heartbeat.Stop()
	authentication := time.NewTicker(api.streamAuthInterval)
	defer authentication.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-authentication.C:
			if _, _, err := api.currentStreamAccess(request, false); err != nil {
				return
			}
		case event, open := <-updates:
			if !open {
				return
			}
			allowed, err := api.currentStreamCanAccessLine(request, event.LineID)
			if err != nil {
				return
			}
			if !allowed {
				continue
			}
			if !writeSSE(response, flusher, "sms", incomingMessageEvent(event)) {
				return
			}
		case observedAt := <-heartbeat.C:
			if _, _, err := api.currentStreamPrincipal(request); err != nil {
				return
			}
			if !writeEventHeartbeat(response, flusher, observedAt) {
				return
			}
		}
	}
}

func incomingMessageEvent(event messageevents.IncomingSMS) incomingMessageEventResponse {
	return incomingMessageEventResponse{
		MessageID:  event.MessageID,
		ThreadKey:  event.ThreadKey,
		LineID:     event.LineID,
		Peer:       event.Peer,
		Content:    event.Content,
		Timestamp:  event.Timestamp,
		ObservedAt: event.ObservedAt,
	}
}
