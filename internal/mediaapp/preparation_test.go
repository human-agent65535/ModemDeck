package mediaapp

import (
	"context"
	"errors"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type preparationStore struct {
	mu    sync.Mutex
	call  store.Call
	reads chan struct{}
}

func (s *preparationStore) CallByID(context.Context, string) (store.Call, error) {
	s.mu.Lock()
	call := s.call
	s.mu.Unlock()
	select {
	case s.reads <- struct{}{}:
	default:
	}
	return call, nil
}

type preparationCore struct {
	fakeCore
	bound chan callmedia.ActiveCall
}

func (c *preparationCore) BindPreparedSocket(_ context.Context, _ string, call callmedia.ActiveCall, _ *callmedia.Session) error {
	c.bound <- call
	return nil
}

type preparationRefresher struct{ calls atomic.Int32 }

func (r *preparationRefresher) Refresh(context.Context) (communication.Status, error) {
	r.calls.Add(1)
	return communication.Status{}, nil
}
func TestPreparedHardwareWaitsForAuthoritativeAvailabilityWithoutPolling(t *testing.T) {
	calls := &preparationStore{call: store.Call{ID: "call", Direction: "outgoing", Phase: "ringing-out"}, reads: make(chan struct{}, 2)}
	core := &preparationCore{bound: make(chan callmedia.ActiveCall, 1)}
	refresher := &preparationRefresher{}
	service, err := New(refresher, calls, core)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.BindPreparedSocket(ctx, "prepare", "call", &callmedia.Session{}) }()
	select {
	case <-calls.reads:
	case <-ctx.Done():
		t.Fatal("binding never read authoritative call")
	}
	select {
	case <-core.bound:
		t.Fatal("ringing transport readiness was mistaken for PCM")
	default:
	}
	calls.mu.Lock()
	calls.call.MediaAvailable = true
	updated := calls.call
	calls.mu.Unlock()
	if err := service.ReconcileAuthoritativeCalls(ctx, []store.Call{updated}); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-core.bound:
		if call.ID != "call" {
			t.Fatal("wrong endpoint identity")
		}
	case <-ctx.Done():
		t.Fatal("lifecycle availability did not unblock binding")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if refresher.calls.Load() != 0 {
		t.Fatal("binding polled hardware refresh")
	}
}
func TestPreparedHardwareWaitEndsWithTerminalLifecycle(t *testing.T) {
	calls := &preparationStore{call: store.Call{ID: "call", Direction: "outgoing", Phase: "dialing"}, reads: make(chan struct{}, 2)}
	core := &preparationCore{bound: make(chan callmedia.ActiveCall, 1)}
	service, _ := New(&preparationRefresher{}, calls, core)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.BindPreparedSocket(ctx, "prepare", "call", &callmedia.Session{}) }()
	select {
	case <-calls.reads:
	case <-ctx.Done():
		t.Fatal("binding did not wait")
	}
	calls.mu.Lock()
	calls.call.Phase = "ended"
	calls.mu.Unlock()
	service.ReconcileAuthoritativeCalls(ctx, nil)
	select {
	case err := <-result:
		if !errors.Is(err, ErrNotActive) {
			t.Fatal("terminal call was still bindable", err)
		}
	case <-ctx.Done():
		t.Fatal("terminal lifecycle did not unblock binding")
	}
	select {
	case <-core.bound:
		t.Fatal("terminal call acquired PCM")
	default:
	}
}

func TestOutgoingEarlyPCMReconnectUsesOrdinarySocketCore(t *testing.T) {
	for _, phase := range []string{"dialing", "connecting", "ringing-out"} {
		t.Run(phase, func(t *testing.T) {
			core := &fakeCore{}
			service, err := New(fakeRefresher{}, fakeCallStore{call: store.Call{ID: "call", Direction: "outgoing", Phase: phase, MediaAvailable: true}}, core)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.OpenSocket(context.Background(), "call", "same-owner", nil); err != nil {
				t.Fatal("outgoing PCM waited for active", err)
			}
			if core.exchanges != 1 || core.call.ID != "call" {
				t.Fatal("early reconnect used another media path")
			}
		})
	}
}
