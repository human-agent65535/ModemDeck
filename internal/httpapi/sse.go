package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type eventHeartbeat struct {
	At time.Time `json:"at"`
}

// This file is the one place the API writes server-sent event frames.

// sseReconnectDelay is how long a browser waits before reconnecting a stream.
const sseReconnectDelay = 2 * time.Second

// startSSE opens a stream by telling the client its reconnect delay.
func startSSE(response http.ResponseWriter) bool {
	_, err := fmt.Fprintf(response, "retry: %d\n\n", sseReconnectDelay.Milliseconds())
	return err == nil
}

// writeSSEKeepalive writes a comment frame that keeps an idle stream open.
func writeSSEKeepalive(response http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := fmt.Fprint(response, ": keepalive\n\n"); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeSSE(
	response http.ResponseWriter,
	flusher http.Flusher,
	event string,
	value any,
) bool {
	return writeSSEFrame(response, flusher, "", event, value)
}

// writeSSEWithID writes an event the client can resume from with
// Last-Event-ID.
func writeSSEWithID(
	response http.ResponseWriter,
	flusher http.Flusher,
	event string,
	id uint64,
	value any,
) bool {
	return writeSSEFrame(response, flusher, fmt.Sprintf("id: %d\n", id), event, value)
}

func writeSSEFrame(
	response http.ResponseWriter,
	flusher http.Flusher,
	idLine string,
	event string,
	value any,
) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(response, "%sevent: %s\ndata: %s\n\n", idLine, event, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeEventHeartbeat(
	response http.ResponseWriter,
	flusher http.Flusher,
	observedAt time.Time,
) bool {
	return writeSSE(response, flusher, "heartbeat", eventHeartbeat{
		At: observedAt.UTC(),
	})
}
