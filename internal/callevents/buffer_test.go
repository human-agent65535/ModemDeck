package callevents

import (
	"testing"
	"time"
)

func TestBufferPublishesCallEventsInOrder(t *testing.T) {
	t.Parallel()
	buffer := NewBuffer(4)
	updates, cancel := buffer.Subscribe()
	defer cancel()

	observedAt := time.Date(2026, time.August, 9, 1, 2, 3, 0, time.FixedZone("test", 9*60*60))
	buffer.Publish(Event{Kind: KindIncoming, CallID: "call-1", ObservedAt: observedAt})
	buffer.Publish(Event{Kind: KindTerminal, CallID: "call-1", ObservedAt: observedAt.Add(time.Second)})

	incoming := <-updates
	terminal := <-updates
	if incoming.Kind != KindIncoming || incoming.CallID != "call-1" ||
		!incoming.ObservedAt.Equal(observedAt.UTC()) || incoming.ObservedAt.Location() != time.UTC {
		t.Fatalf("incoming event = %+v", incoming)
	}
	if terminal.Kind != KindTerminal || terminal.CallID != "call-1" ||
		!terminal.ObservedAt.Equal(observedAt.Add(time.Second).UTC()) ||
		terminal.ObservedAt.Location() != time.UTC {
		t.Fatalf("terminal event = %+v", terminal)
	}
}

func TestBufferDisconnectsSlowSubscriber(t *testing.T) {
	t.Parallel()
	buffer := NewBuffer(1)
	updates, cancel := buffer.Subscribe()
	defer cancel()

	buffer.Publish(Event{Kind: KindIncoming, CallID: "call-1"})
	buffer.Publish(Event{Kind: KindTerminal, CallID: "call-1"})
	if _, open := <-updates; !open {
		t.Fatal("buffered event was discarded")
	}
	if _, open := <-updates; open {
		t.Fatal("slow subscriber remained connected")
	}
}
