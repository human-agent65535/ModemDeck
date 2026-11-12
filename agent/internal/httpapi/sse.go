package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// This file is the one place the Agent writes server-sent event frames.

// startSSE tells the client its reconnect delay. It does not flush, so a
// caller can send the retry line together with its first events.
func startSSE(w http.ResponseWriter) bool {
	_, err := fmt.Fprint(w, "retry: 2000\n\n")
	return err == nil
}

// writeSSEKeepalive writes a comment frame that keeps an idle stream open.
func writeSSEKeepalive(w http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// writeSSE writes one event and flushes it.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
