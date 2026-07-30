package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEventCursorPrefersNativeReconnectHeader(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/messages/events?after=5",
		nil,
	)
	request.Header.Set("Last-Event-ID", "7")

	after, replay, ok := eventCursor(response, request)

	if !ok || !replay || after != 7 {
		t.Fatalf("cursor = %d, replay = %t, ok = %t", after, replay, ok)
	}
}
