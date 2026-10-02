package calltest

import (
	"context"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllifecycle"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/store"
)

// session adapts one in-memory call to the production media runtime. No modem,
// database, recording consumer, or second implementation of leases is involved.
type session struct {
	mu        sync.Mutex
	controlMu sync.Mutex
	status    Status
	owner     string
	ctx       context.Context
	cancel    context.CancelFunc
	runtime   *mediaapp.Runtime
	lifecycle *calllifecycle.Coordinator
	audio     *audioEndpoint
}

func (e *session) snapshot() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *session) CallByID(_ context.Context, id string) (store.Call, error) {
	status := e.snapshot()
	if id != status.ID {
		return store.Call{}, store.ErrCallNotFound
	}
	phase := status.Phase
	if e.ctx.Err() != nil || !time.Now().Before(status.ExpiresAt) {
		phase = "ended"
	}
	return store.Call{ID: id, LineID: id, Direction: "incoming", Phase: phase,
		MediaAvailable: phase == "active"}, nil
}

func (e *session) ActiveCalls(ctx context.Context) ([]store.Call, error) {
	call, err := e.CallByID(ctx, e.snapshot().ID)
	if err != nil {
		return nil, err
	}
	if call.Phase != "active" && call.Phase != "ringing" {
		return nil, nil
	}
	return []store.Call{call}, nil
}

func (e *session) Refresh(ctx context.Context) (communication.Status, error) {
	e.controlMu.Lock()
	defer e.controlMu.Unlock()
	return communication.Status{}, e.reconcile(ctx)
}

func (e *session) reconcile(ctx context.Context) error {
	calls, err := e.ActiveCalls(ctx)
	if err != nil {
		return err
	}
	return e.lifecycle.ReconcileAuthoritativeCalls(ctx, calls)
}

func (e *session) finish(failure string) {
	e.controlMu.Lock()
	defer e.controlMu.Unlock()
	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	e.status.Phase, e.status.TestPhase = "ended", "completed"
	if failure != "" {
		e.status.Phase, e.status.FailureCode = "failed", failure
	}
	e.cancel()
	e.mu.Unlock()
	// The same authoritative removal closes media and releases ownership.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = e.reconcile(ctx)
}

func (e *session) EndCall(_ context.Context, id string) error {
	if e.snapshot().ID != id {
		return store.ErrCallNotFound
	}
	e.finish("owner_expired")
	return nil
}
