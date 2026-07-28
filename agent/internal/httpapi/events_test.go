package httpapi

import (
	"bufio"
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
	server := httptest.NewServer(New(provider, "test"))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

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

	if contentType := response.Header.Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	reader := bufio.NewReader(response.Body)
	ready := readSSEFrame(t, reader)
	change := readSSEFrame(t, reader)
	if !strings.Contains(ready, "event: ready\ndata: {}") ||
		!strings.Contains(change, "event: change") ||
		!strings.Contains(change, `"sequence":1`) {
		t.Fatalf("unexpected event stream: ready=%q change=%q", ready, change)
	}
}

func readSSEFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		if line == "\n" {
			return strings.TrimSuffix(frame.String(), "\n")
		}
		frame.WriteString(line)
	}
}

func TestEventsRejectsProviderWithoutChangeSource(t *testing.T) {
	recorder := performRequest(New(&fakeProvider{}, "test"), http.MethodGet, "/v1/events", nil)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
