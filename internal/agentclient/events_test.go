package agentclient

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchChangesNotifiesForReadyAndChangeEvents(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/events" {
			http.NotFound(response, request)
			return
		}
		flusher, ok := response.(http.Flusher)
		if !ok {
			t.Fatal("test response does not support flushing")
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "event: ready\ndata: {}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(response, ": keep-alive\n\nevent: change\ndata: {}\n\n")
		flusher.Flush()
		<-request.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var notifications atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- client.WatchChanges(ctx, func() {
			if notifications.Add(1) == 2 {
				cancel()
			}
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WatchChanges() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WatchChanges() did not stop")
	}
	if got := notifications.Load(); got != 2 {
		t.Fatalf("notifications = %d, want 2", got)
	}
}

func TestWatchChangesRejectsNonEventStream(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{}`))
	}))
	err := client.WatchChanges(context.Background(), func() {})
	if err == nil {
		t.Fatal("WatchChanges() accepted a non-event response")
	}
}

func TestWatchChangesFailsWhenAgentStreamStalls(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		flusher, ok := response.(http.Flusher)
		if !ok {
			t.Fatal("test response does not support flushing")
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "event: ready\ndata: {}\n\n")
		flusher.Flush()
		<-request.Context().Done()
	}))
	client.eventIdleLimit = 25 * time.Millisecond

	started := make(chan struct{})
	err := client.WatchChanges(context.Background(), func() {
		select {
		case <-started:
		default:
			close(started)
		}
	})
	if err == nil {
		t.Fatal("WatchChanges() accepted a stalled event stream")
	}
	select {
	case <-started:
	default:
		t.Fatal("ready event was not observed before the stream stalled")
	}
}
