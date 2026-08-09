package callevents

import (
	"testing"
	"time"
)

func TestBufferPublishesIncomingCalls(t *testing.T) {
	t.Parallel()
	buffer := NewBuffer(4)
	updates, cancel := buffer.Subscribe()
	defer cancel()

	observedAt := time.Date(2026, time.August, 9, 1, 2, 3, 0, time.FixedZone("test", 9*60*60))
	buffer.Publish(IncomingCall{CallID: "call-1", ObservedAt: observedAt})

	event := <-updates
	if event.CallID != "call-1" || !event.ObservedAt.Equal(observedAt.UTC()) || event.ObservedAt.Location() != time.UTC {
		t.Fatalf("event = %+v", event)
	}
}

func TestBufferDisconnectsSlowSubscriber(t *testing.T) {
	t.Parallel()
	buffer := NewBuffer(1)
	updates, cancel := buffer.Subscribe()
	defer cancel()

	buffer.Publish(IncomingCall{CallID: "call-1"})
	buffer.Publish(IncomingCall{CallID: "call-2"})
	if _, open := <-updates; !open {
		t.Fatal("buffered event was discarded")
	}
	if _, open := <-updates; open {
		t.Fatal("slow subscriber remained connected")
	}
}
