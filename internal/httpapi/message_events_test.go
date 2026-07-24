package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

func TestMessageEventStreamReplaysLastEventID(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:1",
		MessageID: "1",
		ThreadKey: "iccid|+818000000001",
	})
	events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:2",
		MessageID: "2",
		ThreadKey: "iccid|+818000000002",
	})
	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events", nil)
	request.Header.Set("Last-Event-ID", "1")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: sms") ||
		!strings.Contains(body, `"message_id":"2"`) ||
		strings.Contains(body, `"message_id":"1"`) ||
		!strings.Contains(body, "event: ready") {
		t.Fatalf("stream = %q", body)
	}
}

func TestMessageEventStreamResetsCursorFromPreviousProcess(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	events.Publish(messageevents.IncomingSMS{EventKey: "sms:1", MessageID: "1"})
	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events", nil)
	request.Header.Set("Last-Event-ID", "99")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "id: 0\nevent: reset") ||
		!strings.Contains(body, `"message_id":"1"`) {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestMessageEventStreamRejectsInvalidCursor(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         messageevents.NewBuffer(8),
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/messages/events?after=nope", nil),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}
