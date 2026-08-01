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

func writeSSE(
	response http.ResponseWriter,
	flusher http.Flusher,
	event string,
	value any,
) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, data); err != nil {
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
