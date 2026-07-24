package runtimeevents

import (
	"reflect"
	"testing"
	"time"
)

func TestBufferPublishesIdempotentlyAndReplaysAfterID(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	observedAt := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	first, created := buffer.Publish(Event{
		EventKey:   "lines:1",
		Resources:  []Resource{ResourceLines, ResourceLines, "unsupported"},
		ObservedAt: observedAt,
	})
	if !created || first.ID != 1 ||
		!reflect.DeepEqual(first.Resources, []Resource{ResourceLines}) ||
		!first.ObservedAt.Equal(observedAt.UTC()) {
		t.Fatalf("first publish = %+v, created = %v", first, created)
	}
	replayed, created := buffer.Publish(Event{
		EventKey:  "lines:1",
		Resources: []Resource{ResourceNetwork},
	})
	if created || !reflect.DeepEqual(replayed, first) {
		t.Fatalf("duplicate publish = %+v, created = %v; want %+v, false", replayed, created, first)
	}
	second, created := buffer.Publish(Event{
		EventKey:  "network:1",
		Resources: []Resource{ResourceNetwork, ResourceCalls},
	})
	if !created || second.ID != 2 || second.ObservedAt.IsZero() {
		t.Fatalf("second publish = %+v, created = %v", second, created)
	}

	window, updates, cancel := buffer.Subscribe(first.ID)
	defer cancel()
	if window.Reset || len(window.Events) != 1 || !reflect.DeepEqual(window.Events[0], second) {
		t.Fatalf("window = %+v, want only second event", window)
	}
	third, _ := buffer.Publish(Event{Resources: []Resource{ResourceCalls}})
	if update := <-updates; !reflect.DeepEqual(update, third) {
		t.Fatalf("live update = %+v, want %+v", update, third)
	}
}

func TestBufferRejectsEventsWithoutSupportedResources(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	event, created := buffer.Publish(Event{Resources: []Resource{"unsupported"}})
	if created || !reflect.DeepEqual(event, Event{}) {
		t.Fatalf("publish = %+v, created = %v; want zero, false", event, created)
	}
	window, _, cancel := buffer.SubscribeCurrent()
	cancel()
	if window.NewestID != 0 || len(window.Events) != 0 {
		t.Fatalf("window = %+v; want empty buffer", window)
	}
}

func TestBufferCurrentSubscriptionStartsAtNewestEvent(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	buffer.Publish(Event{Resources: []Resource{ResourceLines}})
	second, _ := buffer.Publish(Event{Resources: []Resource{ResourceNetwork}})

	window, updates, cancel := buffer.SubscribeCurrent()
	defer cancel()
	if window.Reset || window.NewestID != second.ID || len(window.Events) != 0 {
		t.Fatalf("current window = %+v, want newest ID without replay", window)
	}

	third, _ := buffer.Publish(Event{Resources: []Resource{ResourceCalls}})
	if update := <-updates; !reflect.DeepEqual(update, third) {
		t.Fatalf("live update = %+v, want %+v", update, third)
	}
}

func TestBufferResetsExpiredAndFutureCursors(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(2)
	buffer.Publish(Event{Resources: []Resource{ResourceLines}})
	buffer.Publish(Event{Resources: []Resource{ResourceNetwork}})
	buffer.Publish(Event{Resources: []Resource{ResourceCalls}})
	buffer.Publish(Event{Resources: []Resource{ResourceLines, ResourceNetwork}})

	expired, _, cancelExpired := buffer.Subscribe(1)
	cancelExpired()
	if !expired.Reset || expired.OldestID != 3 || expired.NewestID != 4 ||
		len(expired.Events) != 0 {
		t.Fatalf("expired window = %+v", expired)
	}

	future, _, cancelFuture := buffer.Subscribe(99)
	cancelFuture()
	if !future.Reset || len(future.Events) != 0 {
		t.Fatalf("future window = %+v", future)
	}

	empty := NewBuffer(2)
	restarted, _, cancelRestarted := empty.Subscribe(7)
	cancelRestarted()
	if !restarted.Reset || restarted.OldestID != 0 || restarted.NewestID != 0 {
		t.Fatalf("empty restarted window = %+v", restarted)
	}
}
