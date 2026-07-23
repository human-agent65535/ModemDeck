package calllifecycle

import (
	"context"
	"errors"
	"sync"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type Reconciler interface {
	ReconcileAuthoritativeCalls(context.Context, []store.Call) error
}

// Coordinator publishes one authoritative call snapshot to its reconcilers in
// order. Media must be registered before recording so endpoint authority is
// established before a recording can subscribe.
type Coordinator struct {
	mu          sync.Mutex
	reconcilers []Reconciler
}

func New(reconcilers ...Reconciler) (*Coordinator, error) {
	if len(reconcilers) == 0 {
		return nil, errors.New("call lifecycle coordinator requires reconcilers")
	}
	owned := make([]Reconciler, len(reconcilers))
	for index, reconciler := range reconcilers {
		if reconciler == nil {
			return nil, errors.New("call lifecycle coordinator received a nil reconciler")
		}
		owned[index] = reconciler
	}
	return &Coordinator{reconcilers: owned}, nil
}

func (c *Coordinator) ReconcileAuthoritativeCalls(
	ctx context.Context,
	calls []store.Call,
) error {
	if c == nil {
		return errors.New("call lifecycle coordinator is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var result error
	for _, reconciler := range c.reconcilers {
		snapshot := append([]store.Call(nil), calls...)
		if err := reconciler.ReconcileAuthoritativeCalls(ctx, snapshot); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
