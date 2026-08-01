package messageevents

import (
	"testing"
	"time"
)

func TestBufferPublishesOnlyToCurrentSubscribers(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	observedAt := time.Date(2026, time.July, 24, 16, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	buffer.Publish(IncomingSMS{
		MessageID:  "1",
		ObservedAt: observedAt,
	})

	updates, cancel := buffer.Subscribe()
	defer cancel()
	select {
	case event := <-updates:
		t.Fatalf("subscription replayed event %+v", event)
	default:
	}

	buffer.Publish(IncomingSMS{MessageID: "2", ObservedAt: observedAt})
	update := <-updates
	if update.MessageID != "2" || !update.ObservedAt.Equal(observedAt.UTC()) {
		t.Fatalf("live update = %+v", update)
	}
}

func TestBufferDisconnectsSlowSubscriber(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(1)
	updates, cancel := buffer.Subscribe()
	defer cancel()
	buffer.Publish(IncomingSMS{MessageID: "1"})
	buffer.Publish(IncomingSMS{MessageID: "2"})

	first, open := <-updates
	if !open || first.MessageID != "1" {
		t.Fatalf("buffered event = %+v, open = %t", first, open)
	}
	if _, open := <-updates; open {
		t.Fatal("slow subscriber remained connected")
	}
}
