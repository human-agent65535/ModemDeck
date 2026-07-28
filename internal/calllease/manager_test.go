package calllease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/communication"
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
	actions chan communication.CallActionInput
	err     error
}

func (controller *fakeController) CallAction(
	_ context.Context,
	input communication.CallActionInput,
) (store.Call, error) {
	controller.actions <- input
	if controller.err != nil {
		return store.Call{}, controller.err
	}
	return store.Call{ID: input.CallID, Phase: "ending"}, nil
}

func TestLeaseExpiryEndsCall(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		actions: make(chan communication.CallActionInput, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration:      30 * time.Millisecond,
		CheckInterval: 5 * time.Millisecond,
		HangupTimeout: time.Second,
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
	case action := <-controller.actions:
		if action.CallID != "call-1" ||
			action.Action != "hangup" ||
			action.RequestID == "" {
			t.Fatalf("call action = %+v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("expired browser call lease did not end the call")
	}
}

func TestReconciledCallGetsUnclaimedGracePeriod(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "ringing"},
	}}
	controller := &fakeController{
		actions: make(chan communication.CallActionInput, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration:      30 * time.Millisecond,
		CheckInterval: 5 * time.Millisecond,
		HangupTimeout: time.Second,
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
	case <-controller.actions:
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
		actions: make(chan communication.CallActionInput, 1),
	}
	manager, err := New(calls, controller, Options{
		Duration:      80 * time.Millisecond,
		CheckInterval: 5 * time.Millisecond,
		HangupTimeout: time.Second,
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
	case action := <-controller.actions:
		t.Fatalf("call ended before renewed lease expired: %+v", action)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-controller.actions:
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
				&fakeController{actions: make(chan communication.CallActionInput, 1)},
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
		&fakeController{actions: make(chan communication.CallActionInput, 1)},
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

func TestFailedHangupRetriesAfterShortDelay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		actions: make(chan communication.CallActionInput, 2),
		err:     errors.New("temporary control failure"),
	}
	manager, err := New(calls, controller, Options{
		Duration:   10 * time.Second,
		RetryDelay: time.Second,
		Now:        func() time.Time { return now },
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
	firstAction := <-controller.actions

	now = now.Add(500 * time.Millisecond)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("call retried before retry delay = %+v", expired)
	}
	controller.err = nil
	now = now.Add(500 * time.Millisecond)
	second := manager.expiredCalls()
	if len(second) != 1 {
		t.Fatalf("second expiration = %+v", second)
	}
	manager.endExpiredCall(context.Background(), second[0].callID, second[0].attempt)
	secondAction := <-controller.actions
	if firstAction.RequestID == secondAction.RequestID {
		t.Fatalf("retry reused request id %q", firstAction.RequestID)
	}
}

func TestSuccessfulHangupRepeatsUntilAuthoritativeCallDisappears(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "active"},
	}}
	controller := &fakeController{
		actions: make(chan communication.CallActionInput, 2),
	}
	manager, err := New(calls, controller, Options{
		Duration:   10 * time.Second,
		RetryDelay: time.Second,
		Now:        func() time.Time { return now },
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
	<-controller.actions

	now = now.Add(time.Second)
	second := manager.expiredCalls()
	if len(second) != 1 {
		t.Fatalf("active call was not checked again after hangup = %+v", second)
	}
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	manager.endExpiredCall(context.Background(), second[0].callID, second[0].attempt)
	select {
	case action := <-controller.actions:
		t.Fatalf("terminal authoritative snapshot triggered stale action = %+v", action)
	default:
	}
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("terminal authoritative snapshot retained lease = %+v", expired)
	}
}
