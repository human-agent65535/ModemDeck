package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

const (
	updateEventPollInterval      = 750 * time.Millisecond
	updateEventHeartbeatInterval = 5 * time.Second
	updateEventStatusTimeout     = 3 * time.Second
)

func (api *API) updateEventStream(response http.ResponseWriter, request *http.Request) {
	if api.updateManager == nil {
		writeError(response, http.StatusServiceUnavailable, "updater_unavailable", "Automatic updates are unavailable for this deployment", "")
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "Streaming is unavailable", "")
		return
	}
	_, _, authorized := api.authorizeEventStream(response, request, true)
	if !authorized {
		return
	}
	release, ok := api.acquireEventStream(response, request)
	if !ok {
		return
	}
	defer release()

	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(response)
	_ = controller.SetWriteDeadline(time.Time{})
	if _, err := fmt.Fprint(response, "retry: 1000\n\n"); err != nil {
		return
	}
	flusher.Flush()
	lastWrite := time.Now()

	var previous updatecheck.Operation
	hasPrevious := false
	writeCurrent := func() (bool, bool) {
		ctx, cancel := context.WithTimeout(request.Context(), updateEventStatusTimeout)
		defer cancel()
		operation, err := api.updateManager.Status(ctx)
		if err != nil {
			return true, false
		}
		if hasPrevious && reflect.DeepEqual(previous, operation) {
			return true, false
		}
		if !writeSSE(response, flusher, "operation", operation) {
			return false, false
		}
		previous = operation
		hasPrevious = true
		return true, true
	}
	if ok, wrote := writeCurrent(); !ok {
		return
	} else if wrote {
		lastWrite = time.Now()
	}

	status := time.NewTicker(updateEventPollInterval)
	defer status.Stop()
	heartbeat := time.NewTicker(updateEventHeartbeatInterval)
	defer heartbeat.Stop()
	authentication := time.NewTicker(api.streamAuthInterval)
	defer authentication.Stop()

	for {
		select {
		case <-request.Context().Done():
			return
		case <-status.C:
			ok, wrote := writeCurrent()
			if !ok {
				return
			}
			if wrote {
				lastWrite = time.Now()
			}
		case observedAt := <-heartbeat.C:
			if time.Since(lastWrite) < updateEventHeartbeatInterval {
				continue
			}
			if !writeEventHeartbeat(response, flusher, observedAt) {
				return
			}
			lastWrite = time.Now()
		case <-authentication.C:
			if _, _, err := api.currentStreamAccess(request, true); err != nil {
				return
			}
		}
	}
}
