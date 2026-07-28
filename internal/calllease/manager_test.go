package calllease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeCalls struct {
	mu    sync.Mutex
	calls map[string]store.Call
}

func (calls *fakeCalls) CallByID(
	_ context.Context,
	callID string,
) (store.Call, error) {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	call, found := calls.calls[callID]
	if !found {
		return store.Call{}, store.ErrCallNotFound
	}
	return call, nil
}

type fakeController struct {
	releases   chan struct{}
	releaseErr error
}

func (controller *fakeController) ReleaseCallControl(context.Context) error {
	if controller.releases != nil {
		controller.releases <- struct{}{}
	}
	return controller.releaseErr
}

func TestLeaseExpiryEndsCall(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		releases: make(chan struct{}, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration:       30 * time.Millisecond,
		CheckInterval:  5 * time.Millisecond,
		ReleaseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.Run(ctx)
	select {
	case <-controller.releases:
	case <-time.After(time.Second):
		t.Fatal("expired browser call lease did not release call control")
	}
}

func TestReconciledCallGetsUnclaimedGracePeriod(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "ringing"},
	}}
	controller := &fakeController{
		releases: make(chan struct{}, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration:       30 * time.Millisecond,
		CheckInterval:  5 * time.Millisecond,
		ReleaseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		[]store.Call{{ID: "call-1", Phase: "ringing"}},
	); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.Run(ctx)
	select {
	case <-controller.releases:
	case <-time.After(time.Second):
		t.Fatal("unclaimed active call did not expire")
	}
}

func TestRenewalExtendsCallLease(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		releases: make(chan struct{}, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration:       80 * time.Millisecond,
		CheckInterval:  5 * time.Millisecond,
		ReleaseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	if _, err := manager.Renew(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-controller.releases:
		t.Fatal("call control was released before the renewed lease expired")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-controller.releases:
	case <-time.After(time.Second):
		t.Fatal("renewed lease never expired")
	}
}

func TestTerminalCallCannotRenew(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"unknown", "ending", "ended", "failed"} {
		phase := phase
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			calls := &fakeCalls{calls: map[string]store.Call{
				"call-1": {ID: "call-1", Phase: phase},
			}}
			manager, err := New(
				calls,
				&fakeController{releases: make(chan struct{}, 1)},
				Options{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Renew(
				context.Background(),
				"call-1",
				"browser-1",
			); err != ErrCallNotActive {
				t.Fatalf("Renew() error = %v, want ErrCallNotActive", err)
			}
		})
	}
}

func TestAnyLiveBrowserHolderKeepsCallAlive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	manager, err := New(
		calls,
		&fakeController{releases: make(chan struct{}, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	if _, err := manager.Renew(context.Background(), "call-1", "browser-2"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("expired calls with a live browser holder = %+v", expired)
	}
	now = now.Add(5 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 1 ||
		expired[0].callID != "call-1" {
		t.Fatalf("expired calls after every holder elapsed = %+v", expired)
	}
}

func TestFailedReleaseIsNotRetriedWithoutNewAuthoritativeState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		releases:   make(chan struct{}, 1),
		releaseErr: errors.New("temporary control failure"),
	}
	reports := make(chan error, 1)
	manager, err := New(calls, controller, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
		Report:   func(err error) { reports <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	first := manager.expiredCalls()
	if len(first) != 1 {
		t.Fatalf("first expiration = %+v", first)
	}
	manager.endExpiredCall(context.Background(), first[0].callID, first[0].attempt)
	<-controller.releases
	<-reports

	now = now.Add(time.Hour)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("failed release was retried without a new call state = %+v", expired)
	}
}

func TestSuccessfulReleaseWaitsForAuthoritativeCallEnd(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		releases: make(chan struct{}, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	first := manager.expiredCalls()
	if len(first) != 1 {
		t.Fatalf("first expiration = %+v", first)
	}
	manager.endExpiredCall(context.Background(), first[0].callID, first[0].attempt)
	<-controller.releases

	now = now.Add(time.Second)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("accepted release was repeated = %+v", expired)
	}
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("terminal authoritative snapshot retained lease = %+v", expired)
	}
}
