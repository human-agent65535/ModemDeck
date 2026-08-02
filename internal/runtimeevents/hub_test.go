package runtimeevents

import (
	"testing"
	"time"
)

func TestHubPublishesCurrentWatermarks(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	initial, updates, cancel := hub.Subscribe()
	defer cancel()
	if initial.Epoch == "" || initial.Revision != 0 || initial.DataRevision != 0 {
		t.Fatalf("initial signal = %+v", initial)
	}

	observedAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	first := hub.Publish(Change{
		ObservedAt: observedAt,
		Sections:   SectionNetwork,
	})
	if first.Epoch != initial.Epoch || first.Revision != 1 ||
		first.DataRevision != 0 || first.Sections != SectionNetwork ||
		!first.ObservedAt.Equal(observedAt) {
		t.Fatalf("first signal = %+v", first)
	}
	if update := <-updates; update != first {
		t.Fatalf("update = %+v, want %+v", update, first)
	}

	second := hub.Publish(Change{Durable: true})
	if second.Revision != 2 || second.DataRevision != 1 || second.ObservedAt.IsZero() {
		t.Fatalf("second signal = %+v", second)
	}
	current, _, cancelCurrent := hub.Subscribe()
	cancelCurrent()
	if current != second {
		t.Fatalf("current = %+v, want %+v", current, second)
	}
}

func TestHubCoalescesSlowSubscribersToNewestSignal(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	_, updates, cancel := hub.Subscribe()
	defer cancel()
	hub.Publish(Change{Sections: SectionCommunication})
	hub.Publish(Change{Durable: true})
	newest := hub.Publish(Change{Sections: SectionCalls})

	select {
	case update := <-updates:
		if update.Revision != newest.Revision ||
			update.DataRevision != newest.DataRevision ||
			update.Sections != SectionCommunication|SectionCalls {
			t.Fatalf("coalesced update = %+v, newest = %+v", update, newest)
		}
	case <-time.After(time.Second):
		t.Fatal("coalesced update was not delivered")
	}
	select {
	case update := <-updates:
		t.Fatalf("unexpected intermediate update: %+v", update)
	default:
	}
}

func TestHubIgnoresChangesWithoutStateOrDurableData(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	initial, updates, cancel := hub.Subscribe()
	defer cancel()
	current := hub.Publish(Change{ObservedAt: time.Now()})
	if current != initial {
		t.Fatalf("no-op signal = %+v, want %+v", current, initial)
	}
	select {
	case update := <-updates:
		t.Fatalf("no-op change published %+v", update)
	default:
	}
}
