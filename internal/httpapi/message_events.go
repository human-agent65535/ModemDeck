package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

func (api *API) messageEventStream(response http.ResponseWriter, request *http.Request) {
	if api.messageEvents == nil {
		writeError(response, http.StatusServiceUnavailable, "message_events_unavailable", "Message events are unavailable", "")
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
	if window.Reset && !writeSSE(response, flusher, "reset", 0, map[string]uint64{
		"oldest_id": window.OldestID,
		"newest_id": window.NewestID,
	}) {
		return
	}
	for _, event := range window.Events {
		allowed, err := api.currentStreamCanAccessLine(request, event.LineID)
		if err != nil {
			return
		}
		if !allowed {
			continue
		}
		if !writeSSE(response, flusher, "sms", event.ID, incomingMessageEvent(event)) {
			return
		}
	}
	if !writeSSE(response, flusher, "ready", window.NewestID, map[string]uint64{
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
			if !writeSSE(response, flusher, "sms", event.ID, incomingMessageEvent(event)) {
				return
			}
		case <-heartbeat.C:
			if _, _, err := api.currentStreamPrincipal(request); err != nil {
				return
			}
			if _, err := fmt.Fprint(response, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (api *API) currentStreamCanAccessLine(
	request *http.Request,
	lineID string,
) (bool, error) {
	principal, scoped, err := api.currentStreamPrincipal(request)
	if err != nil {
		return false, err
	}
	return !scoped || principal.CanAccessLine(strings.TrimSpace(lineID)), nil
}

func (api *API) currentStreamPrincipal(
	request *http.Request,
) (auth.Principal, bool, error) {
	if api.authenticator == nil {
		principal, exists := auth.PrincipalFromContext(request.Context())
		return principal, exists, nil
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return auth.Principal{}, false, err
	}
	authentication, err := api.authenticator.Authenticate(
		request.Context(),
		auth.SessionToken(cookie.Value),
	)
	if err != nil {
		return auth.Principal{}, false, err
	}
	principal, exists := authentication.Principal()
	if !exists {
		if _, scoped := auth.PrincipalFromContext(request.Context()); scoped {
			return auth.Principal{}, false, errors.New("current session has no user principal")
		}
	}
	return principal, exists, nil
}

func incomingMessageEvent(event messageevents.IncomingSMS) incomingMessageEventResponse {
	return incomingMessageEventResponse{
		ID:        event.ID,
		EventKey:  event.EventKey,
		MessageID: event.MessageID,
		ThreadKey: event.ThreadKey,
		LineID:    event.LineID,
		Peer:      event.Peer,
		Content:   event.Content,
		Timestamp: event.Timestamp,
	}
}
