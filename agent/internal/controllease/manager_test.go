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
	snapshotErrs []error
	snapshots    int
	hangupErr    error
	hangupErrors map[string]error
	hangupCalls  []string
	lastRequest  domain.CallCommandRequest
	hangupSignal chan struct{}
}

func (stub *controllerStub) Snapshot(context.Context) (domain.Snapshot, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.snapshots++
	if len(stub.snapshotErrs) > 0 {
		err := stub.snapshotErrs[0]
		stub.snapshotErrs = stub.snapshotErrs[1:]
		if err != nil {
			return domain.Snapshot{}, err
		}
	}
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
	stub.hangupCalls = append(stub.hangupCalls, request.CallID)
	stub.lastRequest = request
	if err := stub.hangupErrors[request.CallID]; err != nil {
		return domain.CommandReceipt{}, err
	}
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
	release, err := manager.Protect("app-a")
	if err != nil {
		t.Fatalf("Require(current) error = %v", err)
	}
	release()
	if _, err := manager.Protect("app-b"); err == nil {
		t.Fatal("Require(other) succeeded")
	}
	if _, err := manager.Renew("app-b"); err == nil {
		t.Fatal("Renew(other) replaced a live lease")
	}
	now = now.Add(time.Second)
	if _, err := manager.Protect("app-a"); err == nil {
		t.Fatal("Require(expired) succeeded")
	}
}

func TestManagerExpirationWaitsForProtectedCommand(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	controller := &controllerStub{calls: []domain.Call{{
		ID:        "call-created-by-command",
		LineID:    "line-1",
		StateCode: 4,
	}}}
	manager, err := New(controller, Options{
		Duration: time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew("app-a"); err != nil {
		t.Fatal(err)
	}
	releaseCommand, err := manager.Protect("app-a")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Second)
	type expirationResult struct {
		expired bool
		err     error
	}
	attempted := make(chan struct{})
	result := make(chan expirationResult, 1)
	go func() {
		close(attempted)
		expired, err := manager.expire(context.Background())
		result <- expirationResult{expired: expired, err: err}
	}()
	<-attempted
	select {
	case got := <-result:
		t.Fatalf("expiration crossed a protected command: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	controller.mu.Lock()
	if controller.hangupCount != 0 {
		t.Fatalf("hangupCount while command is protected = %d", controller.hangupCount)
	}
	controller.mu.Unlock()

	releaseCommand()
	select {
	case got := <-result:
		if !got.expired || got.err != nil {
			t.Fatalf("expiration result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expiration did not run after the protected command finished")
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 1 {
		t.Fatalf("hangupCount = %d, want 1", controller.hangupCount)
	}
}

func TestManagerExpiredLeaseMustCleanBeforeAnotherControllerCanRenew(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	controller := &controllerStub{calls: []domain.Call{{
		ID:        "call-created-by-old-controller",
		LineID:    "line-1",
		StateCode: 4,
	}}}
	manager, err := New(controller, Options{
		Duration: time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew("app-a"); err != nil {
		t.Fatal(err)
	}
	releaseCommand, err := manager.Protect("app-a")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)

	if _, err := manager.Renew("app-b"); err == nil {
		t.Fatal("new controller renewed before expired call cleanup")
	}

	type expirationResult struct {
		expired bool
		err     error
	}
	result := make(chan expirationResult, 1)
	go func() {
		expired, err := manager.expire(context.Background())
		result <- expirationResult{expired: expired, err: err}
	}()
	select {
	case got := <-result:
		t.Fatalf("expiration crossed a protected command: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	releaseCommand()
	select {
	case got := <-result:
		if got.err != nil || !got.expired {
			t.Fatalf("expiration result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expiration did not finish after the protected command")
	}
	status, err := manager.Renew("app-b")
	if err != nil || status.ControllerID != "app-b" {
		t.Fatalf("new controller after cleanup = %+v, %v", status, err)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.hangupCount != 1 {
		t.Fatalf("hangupCount = %d, want 1", controller.hangupCount)
	}
}

func TestManagerRetriesExpiredCleanupAfterSnapshotFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	controller := &controllerStub{
		calls: []domain.Call{{
			ID:        "call-1",
			LineID:    "line-1",
			StateCode: 4,
		}},
		snapshotErrs: []error{errors.New("temporary snapshot failure"), nil},
	}
	manager, err := New(controller, Options{
		Duration: time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Renew("app-a"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if expired, err := manager.expire(context.Background()); !expired || err == nil {
		t.Fatalf("first expiration = %t, %v; want cleanup failure", expired, err)
	}
	if _, err := manager.Protect("app-a"); err == nil {
		t.Fatal("expired controller remained usable after failed cleanup")
	}
	if expired, err := manager.expire(context.Background()); !expired || err != nil {
		t.Fatalf("cleanup retry = %t, %v; want success", expired, err)
	}
	if status, err := manager.Renew("app-b"); err != nil || status.ControllerID != "app-b" {
		t.Fatalf("new controller after cleanup = %+v, %v", status, err)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.snapshots != 2 || controller.hangupCount != 1 {
		t.Fatalf(
			"snapshots = %d, hangups = %d; want 2, 1",
			controller.snapshots,
			controller.hangupCount,
		)
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

func TestManagerContinuesSameLineCleanupAfterStaleCallIsAlreadyGone(t *testing.T) {
	t.Parallel()
	controller := &controllerStub{
		calls: []domain.Call{
			{ID: "call-stale", LineID: "line-1", StateCode: 4},
			{ID: "call-live", LineID: "line-1", StateCode: 3},
		},
		hangupErrors: map[string]error{
			"call-stale": domain.NotFound("hangup_call", "active call was not found"),
		},
	}
	manager, err := New(controller, Options{CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if len(controller.hangupCalls) != 2 ||
		controller.hangupCalls[0] != "call-stale" ||
		controller.hangupCalls[1] != "call-live" {
		t.Fatalf("hangup calls = %v, want stale then live", controller.hangupCalls)
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
