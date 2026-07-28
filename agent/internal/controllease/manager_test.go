package controllease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type controllerStub struct {
	mu           sync.Mutex
	calls        []domain.Call
	hangupCount  int
	snapshotErr  error
	hangupErr    error
	lastRequest  domain.CallCommandRequest
	hangupSignal chan struct{}
}

func (stub *controllerStub) Snapshot(context.Context) (domain.Snapshot, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.snapshotErr != nil {
		return domain.Snapshot{}, stub.snapshotErr
	}
	return domain.Snapshot{Calls: append([]domain.Call(nil), stub.calls...)}, nil
}

func (stub *controllerStub) HangupCall(
	_ context.Context,
	request domain.CallCommandRequest,
) (domain.CommandReceipt, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.hangupCount++
	stub.lastRequest = request
	if stub.hangupErr != nil {
		return domain.CommandReceipt{}, stub.hangupErr
	}
	for index := range stub.calls {
		if stub.calls[index].ID == request.CallID {
			stub.calls[index].StateCode = 7
		}
	}
	if stub.hangupSignal != nil {
		select {
		case stub.hangupSignal <- struct{}{}:
		default:
		}
	}
	return domain.CommandReceipt{
		RequestID:  request.RequestID,
		ResourceID: request.CallID,
	}, nil
}

func TestManagerRequiresCurrentController(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	manager, err := New(&controllerStub{}, Options{
		Duration: time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Renew("app-a")
	if err != nil {
		t.Fatal(err)
	}
	if status.ControllerID != "app-a" || !status.ExpiresAt.Equal(now.Add(time.Second)) {
		t.Fatalf("Renew() = %+v", status)
	}
	if err := manager.Require("app-a"); err != nil {
		t.Fatalf("Require(current) error = %v", err)
	}
	if err := manager.Require("app-b"); err == nil {
		t.Fatal("Require(other) succeeded")
	}
	if _, err := manager.Renew("app-b"); err == nil {
		t.Fatal("Renew(other) replaced a live lease")
	}
	now = now.Add(time.Second)
	if err := manager.Require("app-a"); err == nil {
		t.Fatal("Require(expired) succeeded")
	}
}

func TestManagerExpirationHangsUpCall(t *testing.T) {
	t.Parallel()
	controller := &controllerStub{
		calls: []domain.Call{
			{
				ID:        "call-1",
				LineID:    "line-1",
				StateCode: 4,
			},
			{
				ID:        "call-2",
				LineID:    "line-1",
				StateCode: 3,
			},
		},
		hangupSignal: make(chan struct{}, 2),
	}
	manager, err := New(controller, Options{
		Duration:       30 * time.Millisecond,
		CheckInterval:  5 * time.Millisecond,
		CommandTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew("app-a"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.Run(ctx)

	for range 2 {
		select {
		case <-controller.hangupSignal:
		case <-time.After(time.Second):
			t.Fatal("expired lease did not hang up every observed call")
		}
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 2 ||
		controller.lastRequest.CallID != "call-2" ||
		controller.lastRequest.RequestID == "" {
		t.Fatalf(
			"hangupCount=%d request=%+v",
			controller.hangupCount,
			controller.lastRequest,
		)
	}
}

func TestManagerReleaseAndRecoveryHangUpCalls(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		run  func(*Manager) error
	}{
		{
			name: "release",
			run: func(manager *Manager) error {
				if _, err := manager.Renew("app-a"); err != nil {
					return err
				}
				return manager.Release(context.Background(), "app-a")
			},
		},
		{
			name: "startup recovery",
			run: func(manager *Manager) error {
				return manager.Recover(context.Background())
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := &controllerStub{calls: []domain.Call{{
				ID:        "call-1",
				LineID:    "line-1",
				StateCode: 4,
			}}}
			manager, err := New(controller, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.run(manager); err != nil {
				t.Fatal(err)
			}
			controller.mu.Lock()
			defer controller.mu.Unlock()
			if controller.hangupCount != 1 {
				t.Fatalf("hangupCount = %d", controller.hangupCount)
			}
		})
	}
}

func TestManagerReportsCleanupFailureOnce(t *testing.T) {
	t.Parallel()
	controller := &controllerStub{
		calls: []domain.Call{{
			ID:        "call-1",
			LineID:    "line-1",
			StateCode: 4,
		}},
		hangupErr: errors.New("hangup failed"),
	}
	manager, err := New(controller, Options{
		CommandTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = manager.Recover(context.Background())
	if err == nil {
		t.Fatal("Recover() succeeded despite hangup failure")
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 1 {
		t.Fatalf("hangupCount = %d, want 1", controller.hangupCount)
	}
}

func TestManagerDoesNotRepeatFailedTerminationOnSameLine(t *testing.T) {
	t.Parallel()

	controller := &controllerStub{
		calls: []domain.Call{
			{ID: "call-1", LineID: "line-1", StateCode: 4},
			{ID: "call-2", LineID: "line-1", StateCode: 3},
		},
		hangupErr: errors.New("hangup and reset failed"),
	}
	manager, err := New(controller, Options{CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(context.Background()); err == nil {
		t.Fatal("Recover() succeeded despite termination failure")
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 1 {
		t.Fatalf("hangupCount = %d, want 1", controller.hangupCount)
	}
}

func TestManagerDoesNotRepeatReleaseCleanupWithoutOwner(t *testing.T) {
	t.Parallel()

	controller := &controllerStub{
		calls: []domain.Call{{
			ID:        "call-1",
			LineID:    "line-1",
			StateCode: 4,
		}},
		hangupErr: errors.New("hangup and reset failed"),
	}
	manager, err := New(controller, Options{CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew("app-a"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Release(context.Background(), "app-a"); err == nil {
		t.Fatal("first Release() succeeded despite termination failure")
	}
	if err := manager.Release(context.Background(), "app-a"); err != nil {
		t.Fatalf("duplicate Release() error = %v", err)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 1 {
		t.Fatalf("hangupCount = %d, want 1", controller.hangupCount)
	}
}

func TestManagerRecordsForcedTerminationAndAllowsRecovery(t *testing.T) {
	t.Parallel()
	controller := &controllerStub{
		calls: []domain.Call{
			{
				ID:        "call-1",
				LineID:    "line-1",
				StateCode: 4,
			},
			{
				ID:        "stale-call-on-reset-line",
				LineID:    "line-1",
				StateCode: 3,
			},
		},
		hangupErr: errors.Join(
			errors.New("hangup failed"),
			domain.ErrForcedCallTermination,
		),
	}
	var reported []error
	manager, err := New(controller, Options{
		CommandTimeout: time.Second,
		Report: func(err error) {
			reported = append(reported, err)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error after forced termination = %v", err)
	}
	if len(reported) != 1 ||
		!errors.Is(reported[0], domain.ErrForcedCallTermination) {
		t.Fatalf("reported incidents = %#v", reported)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 1 {
		t.Fatalf("hangupCount = %d, want 1", controller.hangupCount)
	}
}
