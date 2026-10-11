package calllease

import (
	"context"
	"errors"
	"github.com/human-agent65535/modemdeck/internal/store"
	"testing"
	"time"
)

func TestPreparationLeaseSnapshotsRenewConsumeAndRevoke(t *testing.T) {
	now := time.Now()
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{HolderID: "credential", SubjectID: "user"}
	if _, err := manager.PrepareOutgoingFor(context.Background(), "prep", "line", owner); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.RequireOutgoing("prep", "credential"); err != nil {
		t.Fatal("snapshot destroyed preparation", err)
	}
	if err := manager.RequireOutgoing("prep", "other"); !errors.Is(err, ErrNotOwner) {
		t.Fatal("other credential authorized", err)
	}
	now = now.Add(10 * time.Second)
	expiry, err := manager.RenewOutgoing("prep", "credential", 30*time.Second)
	if err != nil || !expiry.Equal(now.Add(30*time.Second)) {
		t.Fatal(expiry, err)
	}
	if err := manager.BeginOutgoing("prep", "credential"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginOutgoing("prep", "credential"); err == nil {
		t.Fatal("double commit accepted")
	}
	calls.calls["actual"] = store.Call{ID: "actual", LineID: "line", Phase: "ringing-out", Direction: "outgoing"}
	if _, err := manager.ActivateOutgoing(context.Background(), "prep", "actual", "credential"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Require(context.Background(), "actual", "credential"); err != nil {
		t.Fatal(err)
	}
	revoked, err := manager.RevokeHolder("credential")
	if err != nil || len(revoked) != 1 || revoked[0] != "actual" {
		t.Fatal(revoked, err)
	}
	if err := manager.RequireOutgoing("prep", "credential"); err == nil {
		t.Fatal("revoked reservation usable")
	}
}
func TestPreparationExpiresAndNeverMatchesUnrelatedOutgoingCall(t *testing.T) {
	now := time.Now()
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, _ := newInitializedManager(calls, &fakeController{}, Options{Now: func() time.Time { return now }})
	_, err := manager.PrepareOutgoingFor(context.Background(), "prep", "line", Owner{HolderID: "one", SubjectID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.PrepareOutgoingFor(context.Background(), "other", "line", Owner{HolderID: "two", SubjectID: "user"}); !errors.Is(err, ErrCallOwned) {
		t.Fatal("line not exclusive", err)
	}
	projected, err := manager.ProjectActive([]store.Call{{ID: "foreign", LineID: "line", Direction: "outgoing", Phase: "dialing"}}, "one")
	if err != nil || projected.Calls[0].ControlState != ControlOccupied {
		t.Fatal("preparation claimed unrelated call", projected, err)
	}
	now = now.Add(16 * time.Second)
	if err := manager.BeginOutgoing("prep", "one"); err == nil {
		t.Fatal("expired preparation dispatched")
	}
	manager.expiredCalls()
	if err := manager.RequireOutgoing("prep", "one"); !errors.Is(err, ErrReservationNotFound) {
		t.Fatal("expired preparation retained", err)
	}
}
func TestCancelledDispatchRetainsOwnershipAcrossEmptySnapshot(t *testing.T) {
	now := time.Now()
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, _ := newInitializedManager(calls, &fakeController{}, Options{Now: func() time.Time { return now }})
	_, err := manager.PrepareOutgoingFor(context.Background(), "prep", "line", Owner{HolderID: "one", SubjectID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginOutgoing("prep", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CancelOutgoing("prep", "one"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if manager.findRequestLocked("prep") == nil {
		t.Fatal("empty snapshot discarded cancelled in-flight ownership")
	}
	late := store.Call{ID: "late", LineID: "line", Direction: "outgoing", Phase: "ringing-out"}
	calls.calls[late.ID] = late
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), []store.Call{late}); err != nil {
		t.Fatal(err)
	}
	expired := manager.expiredCalls()
	if len(expired) != 1 || expired[0].callID != "late" {
		t.Fatal("late cancelled dial got a new grace window", expired)
	}
}
