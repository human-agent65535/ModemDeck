package calllifecycle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type orderedReconciler struct {
	name  string
	order *[]string
	err   error
	seen  *[]string
}

func (r orderedReconciler) ReconcileAuthoritativeCalls(
	_ context.Context,
	calls []store.Call,
) error {
	*r.order = append(*r.order, r.name)
	if len(calls) > 0 {
		if r.seen != nil {
			*r.seen = append(*r.seen, calls[0].ID)
		}
		calls[0].ID = "mutated"
	}
	return r.err
}

func TestCoordinatorOwnsOneSnapshotPerReconciler(t *testing.T) {
	t.Parallel()
	var order, seen []string
	coordinator, err := New(
		orderedReconciler{name: "media", order: &order},
		orderedReconciler{name: "recording", order: &order, seen: &seen},
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := []store.Call{{ID: "call-1"}}
	if err := coordinator.ReconcileAuthoritativeCalls(context.Background(), calls); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"call-1"}) {
		t.Fatalf("second reconciler snapshot = %v", seen)
	}
}

func TestCoordinatorPublishesInOrderAndContinuesCleanupOnFailure(t *testing.T) {
	t.Parallel()
	var order []string
	expected := errors.New("media failed")
	coordinator, err := New(
		orderedReconciler{name: "media", order: &order, err: expected},
		orderedReconciler{name: "recording", order: &order},
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := []store.Call{{ID: "call-1"}}
	if err := coordinator.ReconcileAuthoritativeCalls(context.Background(), calls); !errors.Is(err, expected) {
		t.Fatalf("ReconcileAuthoritativeCalls() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"media", "recording"}) {
		t.Fatalf("reconcile order = %v", order)
	}
	if calls[0].ID != "call-1" {
		t.Fatalf("caller snapshot was mutated: %+v", calls)
	}
}

func TestCoordinatorRejectsMissingReconcilers(t *testing.T) {
	t.Parallel()
	if _, err := New(); err == nil {
		t.Fatal("New() accepted no reconcilers")
	}
	if _, err := New(nil); err == nil {
		t.Fatal("New() accepted a nil reconciler")
	}
}
