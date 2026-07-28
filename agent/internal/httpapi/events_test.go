package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeChangeProvider struct {
	*fakeProvider
	events     chan domain.ChangeEvent
	subscribed chan struct{}
}

func (p *fakeChangeProvider) SubscribeChanges(context.Context) (<-chan domain.ChangeEvent, error) {
	select {
	case <-p.subscribed:
	default:
		close(p.subscribed)
	}
	return p.events, nil
}

func TestEventsStreamsReadyAndProviderChanges(t *testing.T) {
	provider := &fakeChangeProvider{
		fakeProvider: &fakeProvider{},
		events:       make(chan domain.ChangeEvent, 1),
		subscribed:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		New(provider, "test").ServeHTTP(recorder, request)
	}()

	select {
	case <-provider.subscribed:
	case <-time.After(time.Second):
		t.Fatal("event handler did not subscribe")
	}
	provider.events <- domain.ChangeEvent{
		Sequence:   1,
		Source:     "org.freedesktop.DBus.Properties.PropertiesChanged",
		ObservedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(recorder.Body.String(), "event: change") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event handler did not stop after cancellation")
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: ready\ndata: {}") ||
		!strings.Contains(body, "event: change") ||
		!strings.Contains(body, `"sequence":1`) {
		t.Fatalf("unexpected event stream: %q", body)
	}
}

func TestEventsRejectsProviderWithoutChangeSource(t *testing.T) {
	recorder := performRequest(New(&fakeProvider{}, "test"), http.MethodGet, "/v1/events", nil)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
