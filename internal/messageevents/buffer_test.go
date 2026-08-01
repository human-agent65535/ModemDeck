package messageevents

import (
	"testing"
	"time"
)

func TestBufferPublishesIdempotentlyAndReplaysAfterID(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	observedAt := time.Date(2026, time.July, 24, 16, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	first, created := buffer.Publish(IncomingSMS{
		EventKey:   "sms:1",
		MessageID:  "1",
		ObservedAt: observedAt,
	})
	if !created || first.ID != 1 || !first.ObservedAt.Equal(observedAt.UTC()) {
		t.Fatalf("first publish = %+v, created = %v", first, created)
	}
	replayed, created := buffer.Publish(IncomingSMS{EventKey: "sms:1", MessageID: "different"})
	if created || replayed != first {
		t.Fatalf("duplicate publish = %+v, created = %v; want %+v, false", replayed, created, first)
	}
	second, created := buffer.Publish(IncomingSMS{EventKey: "sms:2", MessageID: "2"})
	if !created || second.ID != 2 {
		t.Fatalf("second publish = %+v, created = %v", second, created)
	}

	window, updates, cancel := buffer.Subscribe(first.ID)
	defer cancel()
	if window.Reset || len(window.Events) != 1 || window.Events[0] != second {
		t.Fatalf("window = %+v, want only second event", window)
	}
	third, _ := buffer.Publish(IncomingSMS{EventKey: "sms:3", MessageID: "3"})
	if update := <-updates; update != third {
		t.Fatalf("live update = %+v, want %+v", update, third)
	}
}

func TestBufferCurrentSubscriptionStartsAtNewestEvent(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	buffer.Publish(IncomingSMS{EventKey: "sms:1", MessageID: "1"})
	second, _ := buffer.Publish(IncomingSMS{EventKey: "sms:2", MessageID: "2"})

	window, updates, cancel := buffer.SubscribeCurrent()
	defer cancel()
	if window.Reset || window.NewestID != second.ID || len(window.Events) != 0 {
		t.Fatalf("current window = %+v, want newest ID without replay", window)
	}

	third, _ := buffer.Publish(IncomingSMS{EventKey: "sms:3", MessageID: "3"})
	if update := <-updates; update != third {
		t.Fatalf("live update = %+v, want %+v", update, third)
	}
}

func TestBufferResetsExpiredAndFutureCursors(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(2)
	buffer.Publish(IncomingSMS{EventKey: "sms:1"})
	buffer.Publish(IncomingSMS{EventKey: "sms:2"})
	buffer.Publish(IncomingSMS{EventKey: "sms:3"})
	buffer.Publish(IncomingSMS{EventKey: "sms:4"})

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
