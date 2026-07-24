package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func eventCursor(
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

func writeSSE(
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
