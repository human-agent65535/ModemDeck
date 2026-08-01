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

type claimOnlyCalls struct {
	call store.Call
}

func (calls *claimOnlyCalls) CallByID(
	_ context.Context,
	callID string,
) (store.Call, error) {
	if calls.call.ID != callID {
		return store.Call{}, store.ErrCallNotFound
	}
	return calls.call, nil
}

func (*claimOnlyCalls) ActiveCalls(context.Context) ([]store.Call, error) {
	return nil, nil
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

func (calls *fakeCalls) ActiveCalls(
	_ context.Context,
) ([]store.Call, error) {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	result := make([]store.Call, 0, len(calls.calls))
	for _, call := range calls.calls {
		if trackedPhase(call.Phase) {
			result = append(result, call)
		}
	}
	return result, nil
}

type fakeController struct {
	ended    chan string
	endError error
}

// Most manager tests model calls observed after the application's initial
// complete snapshot. Tests for restart recovery use New directly so they can
// exercise the fail-closed startup baseline.
func newInitializedManager(
	calls CallStore,
	controller CallController,
	options Options,
) (*Manager, error) {
	manager, err := New(calls, controller, options)
	if err == nil {
		manager.initialized = true
	}
	return manager, err
}

func TestOutgoingReservationOwnsOneLinePerBrowser(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	manager, err := newInitializedManager(
		&fakeCalls{calls: map[string]store.Call{}},
		&fakeController{},
		Options{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}

	reservation, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.LineID != "line-1" ||
		!reservation.Created ||
		reservation.ControlState != ControlOwned ||
		!reservation.CreatedAt.Equal(now) {
		t.Fatalf("reservation = %+v", reservation)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-2",
		"line-1",
		"browser-2",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("same-line reservation error = %v, want ErrCallOwned", err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-3",
		"line-2",
		"browser-1",
	); !errors.Is(err, ErrHolderBusy) {
		t.Fatalf("same-holder reservation error = %v, want ErrHolderBusy", err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-4",
		"line-2",
		"browser-2",
	); err != nil {
		t.Fatalf("different-line reservation error = %v", err)
	}

	visible, err := manager.OutgoingReservations("browser-2")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]ControlState{}
	for _, item := range visible {
		states[item.LineID] = item.ControlState
	}
	if states["line-1"] != ControlOccupied || states["line-2"] != ControlOwned {
		t.Fatalf("reservation states = %+v", states)
	}
}

func TestOutgoingReservationActivatesAsCallLease(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	); err != nil {
		t.Fatal(err)
	}
	calls.mu.Lock()
	calls.calls["call-1"] = store.Call{
		ID:     "call-1",
		LineID: "line-1",
		Phase:  "dialing",
	}
	calls.mu.Unlock()

	status, err := manager.ActivateOutgoing(
		context.Background(),
		"request-1",
		"call-1",
		"browser-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if status.CallID != "call-1" || status.HolderID != "browser-1" {
		t.Fatalf("lease status = %+v", status)
	}
	if reservations, err := manager.OutgoingReservations("browser-1"); err != nil ||
		len(reservations) != 0 {
		t.Fatalf("reservations = %+v, error = %v", reservations, err)
	}
	state, err := manager.ControlState(context.Background(), "call-1", "browser-1")
	if err != nil || state != ControlOwned {
		t.Fatalf("control state = %q, error = %v", state, err)
	}
}

func TestOutgoingReservationReplaysWithoutCreatingOrReleasing(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created {
		t.Fatalf("first reservation = %+v, want created", first)
	}
	replayed, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created {
		t.Fatalf("replayed reservation = %+v, want existing", replayed)
	}
	calls.mu.Lock()
	calls.calls["call-1"] = store.Call{
		ID:     "call-1",
		LineID: "line-1",
		Phase:  "dialing",
	}
	calls.mu.Unlock()
	if _, err := manager.ActivateOutgoing(
		context.Background(),
		"request-1",
		"call-1",
		"browser-1",
	); err != nil {
		t.Fatal(err)
	}
	replayed, err = manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created {
		t.Fatalf("activated replay = %+v, want existing", replayed)
	}
	released, err := manager.ReleaseOutgoing("request-1", "browser-1")
	if err != nil || released {
		t.Fatalf("activated release = %t, error = %v", released, err)
	}
	if _, err := manager.ActivateOutgoing(
		context.Background(),
		"request-1",
		"call-1",
		"browser-1",
	); err != nil {
		t.Fatalf("replayed activation error = %v", err)
	}
}

func TestOutgoingReservationRejectsAnActiveLine(t *testing.T) {
	t.Parallel()
	manager, err := newInitializedManager(
		&fakeCalls{calls: map[string]store.Call{
			"call-1": {
				ID:     "call-1",
				LineID: "line-1",
				Phase:  "active",
			},
		}},
		&fakeController{},
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("reservation error = %v, want ErrCallOwned", err)
	}
}

func TestOutgoingReservationCanBeReleased(t *testing.T) {
	t.Parallel()
	manager, err := newInitializedManager(
		&fakeCalls{calls: map[string]store.Call{}},
		&fakeController{},
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	); err != nil {
		t.Fatal(err)
	}
	released, err := manager.ReleaseOutgoing("request-1", "browser-1")
	if err != nil || !released {
		t.Fatalf("release = %t, error = %v", released, err)
	}
	released, err = manager.ReleaseOutgoing("request-1", "browser-1")
	if err != nil || released {
		t.Fatalf("second release = %t, error = %v", released, err)
	}
}

func TestIndeterminateOutgoingBindsOnNextCompleteSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 13, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"session-1",
	); err != nil {
		t.Fatal(err)
	}
	if err := manager.AwaitOutgoingResolution("request-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	call := store.Call{
		ID:        "call-1",
		LineID:    "line-1",
		Direction: "outgoing",
		Phase:     "dialing",
	}
	calls.calls[call.ID] = call
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		[]store.Call{call},
	); err != nil {
		t.Fatal(err)
	}
	state, err := manager.ControlState(context.Background(), call.ID, "session-1")
	if err != nil || state != ControlOwned {
		t.Fatalf("resolved state = %q, %v; want owned", state, err)
	}
	if len(manager.records) != 1 || manager.records[0].callID != call.ID {
		t.Fatalf("resolved records = %+v", manager.records)
	}
}

func TestIndeterminateOutgoingClearsAfterCompleteAbsentSnapshot(t *testing.T) {
	t.Parallel()
	manager, err := newInitializedManager(
		&fakeCalls{calls: map[string]store.Call{}},
		&fakeController{},
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"session-1",
	); err != nil {
		t.Fatal(err)
	}
	if err := manager.AwaitOutgoingResolution("request-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(manager.records) != 0 {
		t.Fatalf("absent snapshot retained records: %+v", manager.records)
	}
}

func TestPendingOutgoingReservationBlocksClaim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		reservationLine   string
		reservationHolder string
		callLine          string
		claimHolder       string
		want              error
	}{
		{
			name:              "same line",
			reservationLine:   "line-1",
			reservationHolder: "browser-1",
			callLine:          "line-1",
			claimHolder:       "browser-2",
			want:              ErrCallOwned,
		},
		{
			name:              "same holder",
			reservationLine:   "line-1",
			reservationHolder: "browser-1",
			callLine:          "line-2",
			claimHolder:       "browser-1",
			want:              ErrHolderBusy,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := &fakeCalls{calls: map[string]store.Call{}}
			manager, err := newInitializedManager(calls, &fakeController{}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.ReserveOutgoing(
				context.Background(),
				"request-1",
				test.reservationLine,
				test.reservationHolder,
			); err != nil {
				t.Fatal(err)
			}
			calls.mu.Lock()
			calls.calls["call-1"] = store.Call{
				ID:        "call-1",
				LineID:    test.callLine,
				Direction: "incoming",
				Phase:     "ringing",
			}
			calls.mu.Unlock()

			if _, err := manager.Claim(
				context.Background(),
				"call-1",
				test.claimHolder,
			); !errors.Is(err, test.want) {
				t.Fatalf("Claim() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestBoundOutgoingRecordBlocksClaim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		call        store.Call
		claimHolder string
		want        error
	}{
		{
			name: "same line",
			call: store.Call{
				ID:        "call-2",
				LineID:    "line-1",
				Direction: "incoming",
				Phase:     "ringing",
			},
			claimHolder: "browser-2",
			want:        ErrCallOwned,
		},
		{
			name: "same holder",
			call: store.Call{
				ID:        "call-2",
				LineID:    "line-2",
				Direction: "incoming",
				Phase:     "ringing",
			},
			claimHolder: "browser-1",
			want:        ErrHolderBusy,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := &fakeCalls{calls: map[string]store.Call{}}
			manager, err := newInitializedManager(calls, &fakeController{}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.ReserveOutgoing(
				context.Background(),
				"request-1",
				"line-1",
				"browser-1",
			); err != nil {
				t.Fatal(err)
			}
			outgoing := store.Call{
				ID:        "call-1",
				RequestID: "request-1",
				LineID:    "line-1",
				Phase:     "dialing",
			}
			calls.mu.Lock()
			calls.calls[outgoing.ID] = outgoing
			calls.calls[test.call.ID] = test.call
			calls.mu.Unlock()
			if _, err := manager.ActivateOutgoing(
				context.Background(),
				"request-1",
				outgoing.ID,
				"browser-1",
			); err != nil {
				t.Fatal(err)
			}

			if _, err := manager.Claim(
				context.Background(),
				test.call.ID,
				test.claimHolder,
			); !errors.Is(err, test.want) {
				t.Fatalf("Claim() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReserveAndClaimSerializeLineAndHolderOwnership(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		reservationLine   string
		reservationHolder string
		callLine          string
		claimHolder       string
		wantConflict      error
	}{
		{
			name:              "line",
			reservationLine:   "line-1",
			reservationHolder: "browser-1",
			callLine:          "line-1",
			claimHolder:       "browser-2",
			wantConflict:      ErrCallOwned,
		},
		{
			name:              "holder",
			reservationLine:   "line-1",
			reservationHolder: "browser-1",
			callLine:          "line-2",
			claimHolder:       "browser-1",
			wantConflict:      ErrHolderBusy,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := &claimOnlyCalls{call: store.Call{
				ID:        "call-1",
				LineID:    test.callLine,
				Direction: "incoming",
				Phase:     "ringing",
			}}
			manager, err := newInitializedManager(calls, &fakeController{}, Options{})
			if err != nil {
				t.Fatal(err)
			}

			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := manager.ReserveOutgoing(
					context.Background(),
					"request-1",
					test.reservationLine,
					test.reservationHolder,
				)
				results <- err
			}()
			go func() {
				<-start
				_, err := manager.Claim(
					context.Background(),
					"call-1",
					test.claimHolder,
				)
				results <- err
			}()
			close(start)

			successes := 0
			conflicts := 0
			for range 2 {
				switch err := <-results; {
				case err == nil:
					successes++
				case errors.Is(err, test.wantConflict):
					conflicts++
				default:
					t.Fatalf("ownership operation error = %v", err)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf(
					"operations = %d success, %d conflict",
					successes,
					conflicts,
				)
			}
		})
	}
}

func TestActiveProjectionReplacesMatchingPendingReservation(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-2",
		"line-2",
		"browser-2",
	); err != nil {
		t.Fatal(err)
	}
	active := []store.Call{{
		ID:        "call-1",
		RequestID: "request-1",
		LineID:    "line-1",
		Phase:     "dialing",
	}}

	owner, err := manager.ProjectActive(active, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(owner.Calls) != 1 ||
		owner.Calls[0].Call.ID != "call-1" ||
		owner.Calls[0].ControlState != ControlOwned {
		t.Fatalf("owner projection calls = %+v", owner.Calls)
	}
	if len(owner.Reservations) != 1 ||
		owner.Reservations[0].ID != "request-2" ||
		owner.Reservations[0].ControlState != ControlOccupied {
		t.Fatalf("owner projection reservations = %+v", owner.Reservations)
	}

	other, err := manager.ProjectActive(active, "browser-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Calls) != 1 ||
		other.Calls[0].ControlState != ControlOccupied {
		t.Fatalf("other projection calls = %+v", other.Calls)
	}
	if len(other.Reservations) != 1 ||
		other.Reservations[0].ID != "request-2" ||
		other.Reservations[0].ControlState != ControlOwned {
		t.Fatalf("other projection reservations = %+v", other.Reservations)
	}
}

func TestActiveProjectionRequiresRequestAndLineMatch(t *testing.T) {
	t.Parallel()
	manager, err := newInitializedManager(
		&fakeCalls{calls: map[string]store.Call{}},
		&fakeController{},
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoing(
		context.Background(),
		"request-1",
		"line-1",
		"browser-1",
	); err != nil {
		t.Fatal(err)
	}
	projection, err := manager.ProjectActive([]store.Call{{
		ID:        "call-1",
		RequestID: "request-1",
		LineID:    "line-2",
		Phase:     "dialing",
	}}, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Calls) != 1 ||
		projection.Calls[0].ControlState != ControlOccupied {
		t.Fatalf("calls = %+v", projection.Calls)
	}
	if len(projection.Reservations) != 1 ||
		projection.Reservations[0].ID != "request-1" ||
		projection.Reservations[0].ControlState != ControlOwned {
		t.Fatalf("reservations = %+v", projection.Reservations)
	}
}

func TestActiveProjectionIsStableAcrossActivationRace(t *testing.T) {
	t.Parallel()
	for range 32 {
		calls := &fakeCalls{calls: map[string]store.Call{}}
		manager, err := newInitializedManager(calls, &fakeController{}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.ReserveOutgoing(
			context.Background(),
			"request-1",
			"line-1",
			"browser-1",
		); err != nil {
			t.Fatal(err)
		}
		call := store.Call{
			ID:        "call-1",
			RequestID: "request-1",
			LineID:    "line-1",
			Phase:     "dialing",
		}
		calls.mu.Lock()
		calls.calls[call.ID] = call
		calls.mu.Unlock()

		start := make(chan struct{})
		activation := make(chan error, 1)
		projection := make(chan ActiveProjection, 1)
		projectionError := make(chan error, 1)
		go func() {
			<-start
			_, err := manager.ActivateOutgoing(
				context.Background(),
				"request-1",
				"call-1",
				"browser-1",
			)
			activation <- err
		}()
		go func() {
			<-start
			result, err := manager.ProjectActive(
				[]store.Call{call},
				"browser-1",
			)
			projection <- result
			projectionError <- err
		}()
		close(start)

		if err := <-activation; err != nil {
			t.Fatal(err)
		}
		result := <-projection
		if err := <-projectionError; err != nil {
			t.Fatal(err)
		}
		if len(result.Calls) != 1 ||
			result.Calls[0].ControlState != ControlOwned ||
			len(result.Reservations) != 0 {
			t.Fatalf("projection = %+v", result)
		}
	}
}

func (controller *fakeController) EndCall(_ context.Context, callID string) error {
	if controller.ended != nil {
		controller.ended <- callID
	}
	return controller.endError
}

func TestLeaseExpiryEndsCall(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Direction: "incoming", Phase: "ringing"},
	}}
	controller := &fakeController{
		ended: make(chan string, 1),
	}
	manager, err := newInitializedManager(calls, controller, Options{
		Duration:       30 * time.Millisecond,
		CheckInterval:  5 * time.Millisecond,
		ReleaseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.Run(ctx)
	select {
	case callID := <-controller.ended:
		if callID != "call-1" {
			t.Fatalf("ended call = %q, want call-1", callID)
		}
	case <-time.After(time.Second):
		t.Fatal("expired browser call lease did not end its call")
	}
}

func TestExplicitCredentialAndSubjectRevocationOverrideMediaLiveness(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-web": {
			ID:        "call-web",
			LineID:    "line-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
		"call-mobile": {
			ID:        "call-mobile",
			LineID:    "line-2",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ClaimFor(context.Background(), "call-web", Owner{
		HolderID:  "session-web",
		SubjectID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ClaimFor(context.Background(), "call-mobile", Owner{
		HolderID:  "session-mobile",
		SubjectID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	manager.MediaConnected("call-web")
	manager.MediaConnected("call-mobile")

	revoked, err := manager.RevokeHolder("session-web")
	if err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || revoked[0] != "call-web" {
		t.Fatalf("holder revocation calls = %v, want [call-web]", revoked)
	}
	manager.MediaConnected("call-web")
	if err := manager.Require(
		context.Background(),
		"call-web",
		"session-web",
	); !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("revoked holder Require() error = %v, want ErrCallNotActive", err)
	}
	if again, err := manager.RevokeHolder("session-web"); err != nil || len(again) != 0 {
		t.Fatalf("repeated holder revocation = %v, %v; want no calls", again, err)
	}
	if err := manager.Require(
		context.Background(),
		"call-mobile",
		"session-mobile",
	); err != nil {
		t.Fatalf("other credential before subject revocation = %v", err)
	}

	revoked, err = manager.RevokeSubject("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || revoked[0] != "call-mobile" {
		t.Fatalf("subject revocation calls = %v, want [call-mobile]", revoked)
	}
	if err := manager.Require(
		context.Background(),
		"call-mobile",
		"session-mobile",
	); !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("revoked subject Require() error = %v, want ErrCallNotActive", err)
	}
}

func TestRevokedPendingDialBindsOnceAndEnds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{}}
	controller := &fakeController{ended: make(chan string, 1)}
	manager, err := newInitializedManager(calls, controller, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveOutgoingFor(
		context.Background(),
		"request-1",
		"line-1",
		Owner{HolderID: "session-web", SubjectID: "user-1"},
	); err != nil {
		t.Fatal(err)
	}
	if revoked, err := manager.RevokeHolder("session-web"); err != nil || len(revoked) != 0 {
		t.Fatalf("pending revocation = %v, %v; want no bound calls", revoked, err)
	}
	now = now.Add(5 * time.Second)
	if revoked, err := manager.RevokeHolder("session-web"); err != nil || len(revoked) != 0 {
		t.Fatalf("repeated pending revocation = %v, %v; want no bound calls", revoked, err)
	}
	call := store.Call{
		ID:        "call-1",
		RequestID: "request-1",
		LineID:    "line-1",
		Direction: "outgoing",
		Phase:     "dialing",
	}
	calls.mu.Lock()
	calls.calls[call.ID] = call
	calls.mu.Unlock()
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		[]store.Call{call},
	); err != nil {
		t.Fatal(err)
	}
	select {
	case callID := <-controller.ended:
		t.Fatalf("revoked pending dial ended before its timeout: %q", callID)
	default:
	}
	now = now.Add(5 * time.Second)
	expired := manager.expiredCalls()
	if len(expired) != 1 || expired[0].callID != call.ID {
		t.Fatalf("expired calls = %+v, want %q", expired, call.ID)
	}
	manager.endExpiredCall(context.Background(), expired[0].callID, expired[0].record)
	select {
	case callID := <-controller.ended:
		if callID != call.ID {
			t.Fatalf("ended call = %q, want %q", callID, call.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked pending dial was not ended after its timeout")
	}
	if again := manager.expiredCalls(); len(again) != 0 {
		t.Fatalf("revoked pending dial expired twice: %+v", again)
	}
}

func TestLeaseExpiryEndsOnlyItsCall(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", LineID: "line-1", Direction: "incoming", Phase: "ringing"},
		"call-2": {ID: "call-2", LineID: "line-2", Direction: "incoming", Phase: "ringing"},
	}}
	controller := &fakeController{ended: make(chan string, 2)}
	manager, err := newInitializedManager(calls, controller, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-2", "browser-2"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Second)
	if _, err := manager.Renew(context.Background(), "call-2", "browser-2"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)

	expired := manager.expiredCalls()
	if len(expired) != 1 || expired[0].callID != "call-1" {
		t.Fatalf("expired calls = %+v, want call-1 only", expired)
	}
	manager.endExpiredCall(context.Background(), expired[0].callID, expired[0].record)
	if callID := <-controller.ended; callID != "call-1" {
		t.Fatalf("ended call = %q, want call-1", callID)
	}
	select {
	case callID := <-controller.ended:
		t.Fatalf("unexpected second call termination: %s", callID)
	default:
	}
	state, err := manager.ControlState(context.Background(), "call-2", "browser-2")
	if err != nil || state != ControlOwned {
		t.Fatalf("second call state = %q, %v; want owned", state, err)
	}
}

func TestReconciledCallGetsUnclaimedGracePeriod(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Phase: "ringing"},
	}}
	controller := &fakeController{
		ended: make(chan string, 1),
	}
	manager, err := newInitializedManager(calls, controller, Options{
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
	case callID := <-controller.ended:
		if callID != "call-1" {
			t.Fatalf("ended call = %q, want call-1", callID)
		}
	case <-time.After(time.Second):
		t.Fatal("unclaimed active call did not expire")
	}
}

func TestUnclaimedOngoingCallKeepsItsLineClosed(t *testing.T) {
	t.Parallel()

	ongoing := store.Call{
		ID:        "call-active",
		LineID:    "line-1",
		Direction: "incoming",
		Phase:     "active",
	}
	waiting := store.Call{
		ID:        "call-waiting",
		LineID:    "line-1",
		Direction: "incoming",
		Phase:     "ringing",
	}
	calls := &fakeCalls{calls: map[string]store.Call{
		ongoing.ID: ongoing,
		waiting.ID: waiting,
	}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		[]store.Call{ongoing, waiting},
	); err != nil {
		t.Fatal(err)
	}

	projection, err := manager.ProjectActive(
		[]store.Call{ongoing, waiting},
		"session-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, projected := range projection.Calls {
		if projected.ControlState != ControlOccupied {
			t.Fatalf("call %s state = %q, want occupied", projected.Call.ID, projected.ControlState)
		}
	}
	if _, err := manager.Claim(
		context.Background(),
		waiting.ID,
		"session-1",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("waiting call claim = %v, want ErrCallOwned", err)
	}
}

func TestRenewalExtendsCallLease(t *testing.T) {
	t.Parallel()
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Direction: "incoming", Phase: "ringing"},
	}}
	controller := &fakeController{
		ended: make(chan string, 1),
	}
	manager, err := newInitializedManager(calls, controller, Options{
		Duration:       80 * time.Millisecond,
		CheckInterval:  5 * time.Millisecond,
		ReleaseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
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
	case <-controller.ended:
		t.Fatal("call ended before the renewed lease expired")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case callID := <-controller.ended:
		if callID != "call-1" {
			t.Fatalf("ended call = %q, want call-1", callID)
		}
	case <-time.After(time.Second):
		t.Fatal("renewed lease never expired")
	}
}

func TestConnectedMediaProtectsOwnershipUntilTransportEnds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {
			ID:        "call-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"session-1",
	); err != nil {
		t.Fatal(err)
	}
	calls.mu.Lock()
	call := calls.calls["call-1"]
	call.Phase = "active"
	calls.calls["call-1"] = call
	calls.mu.Unlock()

	manager.MediaConnected("call-1")
	now = now.Add(time.Hour)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("connected media ownership expired = %+v", expired)
	}

	manager.MediaDisconnected("call-1")
	now = now.Add(9 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("media recovery grace expired early = %+v", expired)
	}
	now = now.Add(2 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 1 ||
		expired[0].callID != "call-1" {
		t.Fatalf("expired calls = %+v, want call-1", expired)
	}
}

func TestExpiredDeadlineNeverTransfersOwnership(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {
			ID:        "call-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(calls, &fakeController{}, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"session-1",
	); err != nil {
		t.Fatal(err)
	}

	now = now.Add(11 * time.Second)
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"session-2",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("claim after deadline = %v, want ErrCallOwned", err)
	}
	if _, err := manager.Renew(
		context.Background(),
		"call-1",
		"session-1",
	); err != nil {
		t.Fatalf("same session could not recover before cleanup began: %v", err)
	}

	now = now.Add(11 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 1 {
		t.Fatalf("expiration = %+v, want one call", expired)
	}
	if _, err := manager.Renew(
		context.Background(),
		"call-1",
		"session-1",
	); !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("renew after ending began = %v, want ErrCallNotActive", err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"session-2",
	); !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("claim after ending began = %v, want ErrCallNotActive", err)
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
			manager, err := newInitializedManager(
				calls,
				&fakeController{ended: make(chan string, 1)},
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

func TestFirstBrowserClaimWins(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Direction: "incoming", Phase: "ringing"},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"browser-2",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("second Claim() error = %v, want ErrCallOwned", err)
	}
	if _, err := manager.Renew(
		context.Background(),
		"call-1",
		"browser-2",
	); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-owner Renew() error = %v, want ErrNotOwner", err)
	}
	now = now.Add(11 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 1 ||
		expired[0].callID != "call-1" {
		t.Fatalf("expired owned call = %+v", expired)
	}
}

func TestBrowserCannotClaimTwoCalls(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", LineID: "line-1", Direction: "incoming", Phase: "ringing"},
		"call-2": {
			ID:        "call-2",
			LineID:    "line-2",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"browser-1",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-2",
		"browser-1",
	); !errors.Is(err, ErrHolderBusy) {
		t.Fatalf("second call Claim() error = %v, want ErrHolderBusy", err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-2",
		"browser-2",
	); err != nil {
		t.Fatalf("other browser could not claim second call: %v", err)
	}
}

func TestConcurrentBrowserClaimsHaveOneWinner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {
			ID:        "call-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, holderID := range []string{"browser-1", "browser-2"} {
		holderID := holderID
		go func() {
			<-start
			_, err := manager.Claim(context.Background(), "call-1", holderID)
			results <- err
		}()
	}
	close(start)

	successes := 0
	conflicts := 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrCallOwned):
			conflicts++
		default:
			t.Fatalf("Claim() error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claims = %d success, %d conflict", successes, conflicts)
	}
}

func TestControlStateIsRelativeToBrowser(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {
			ID:        "call-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	state, err := manager.ControlState(
		context.Background(),
		"call-1",
		"browser-1",
	)
	if err != nil || state != ControlAvailable {
		t.Fatalf("unclaimed state = %q, %v", state, err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	state, err = manager.ControlState(
		context.Background(),
		"call-1",
		"browser-1",
	)
	if err != nil || state != ControlOwned {
		t.Fatalf("owner state = %q, %v", state, err)
	}
	state, err = manager.ControlState(
		context.Background(),
		"call-1",
		"browser-2",
	)
	if err != nil || state != ControlOccupied {
		t.Fatalf("other browser state = %q, %v", state, err)
	}
}

func TestFailedIncomingAnswerDoesNotTransferClaim(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {
			ID:        "call-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(
		context.Background(),
		"call-1",
		"browser-2",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("second browser claim error = %v, want ErrCallOwned", err)
	}
}

func TestUnansweredIncomingCallDoesNotExpire(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {
			ID:        "call-1",
			Direction: "incoming",
			Phase:     "ringing",
		},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		[]store.Call{calls.calls["call-1"]},
	); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("unanswered incoming call expired = %+v", expired)
	}
}

func TestCallCannotBeClaimedBeforeInitialAuthoritativeSnapshot(t *testing.T) {
	t.Parallel()
	call := store.Call{
		ID:        "call-1",
		LineID:    "line-1",
		Direction: "incoming",
		Phase:     "ringing",
	}
	calls := &fakeCalls{calls: map[string]store.Call{call.ID: call}}
	manager, err := New(calls, &fakeController{}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	state, err := manager.ControlState(
		context.Background(),
		call.ID,
		"session-1",
	)
	if err != nil || state != ControlOccupied {
		t.Fatalf("pre-snapshot state = %q, %v; want occupied", state, err)
	}
	if _, err := manager.Claim(
		context.Background(),
		call.ID,
		"session-1",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("pre-snapshot claim = %v, want ErrCallOwned", err)
	}
	if len(manager.records) != 0 {
		t.Fatalf("pre-snapshot claim created records: %+v", manager.records)
	}
}

func TestStartupCallIsOccupiedAndEndsInsteadOfTransferring(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	call := store.Call{
		ID:        "call-1",
		LineID:    "line-1",
		Direction: "incoming",
		Phase:     "ringing",
	}
	calls := &fakeCalls{calls: map[string]store.Call{call.ID: call}}
	manager, err := New(calls, &fakeController{}, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileAuthoritativeCalls(
		context.Background(),
		[]store.Call{call},
	); err != nil {
		t.Fatal(err)
	}
	state, err := manager.ControlState(
		context.Background(),
		call.ID,
		"session-1",
	)
	if err != nil || state != ControlOccupied {
		t.Fatalf("startup call state = %q, %v; want occupied", state, err)
	}
	if _, err := manager.Claim(
		context.Background(),
		call.ID,
		"session-1",
	); !errors.Is(err, ErrCallOwned) {
		t.Fatalf("startup call claim = %v, want ErrCallOwned", err)
	}
	now = now.Add(11 * time.Second)
	if expired := manager.expiredCalls(); len(expired) != 1 ||
		expired[0].callID != call.ID {
		t.Fatalf("startup expiration = %+v", expired)
	}
}

func TestStaleSnapshotDoesNotDropNewBrowserClaim(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Direction: "incoming", Phase: "ringing"},
	}}
	manager, err := newInitializedManager(
		calls,
		&fakeController{ended: make(chan string, 1)},
		Options{
			Duration: 10 * time.Second,
			Now:      func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}

	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	state, err := manager.ControlState(
		context.Background(),
		"call-1",
		"browser-1",
	)
	if err != nil || state != ControlOwned {
		t.Fatalf("state after stale snapshot = %q, %v", state, err)
	}
}

func TestFailedReleaseIsNotRetriedWithoutNewAuthoritativeState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	calls := &fakeCalls{calls: map[string]store.Call{
		"call-1": {ID: "call-1", Direction: "incoming", Phase: "ringing"},
	}}
	controller := &fakeController{
		ended:    make(chan string, 1),
		endError: errors.New("temporary control failure"),
	}
	reports := make(chan error, 1)
	manager, err := newInitializedManager(calls, controller, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
		Report:   func(err error) { reports <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	first := manager.expiredCalls()
	if len(first) != 1 {
		t.Fatalf("first expiration = %+v", first)
	}
	manager.endExpiredCall(context.Background(), first[0].callID, first[0].record)
	if callID := <-controller.ended; callID != "call-1" {
		t.Fatalf("ended call = %q, want call-1", callID)
	}
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
		"call-1": {ID: "call-1", Direction: "incoming", Phase: "ringing"},
	}}
	controller := &fakeController{
		ended: make(chan string, 1),
	}
	manager, err := newInitializedManager(calls, controller, Options{
		Duration: 10 * time.Second,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(context.Background(), "call-1", "browser-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	first := manager.expiredCalls()
	if len(first) != 1 {
		t.Fatalf("first expiration = %+v", first)
	}
	manager.endExpiredCall(context.Background(), first[0].callID, first[0].record)
	if callID := <-controller.ended; callID != "call-1" {
		t.Fatalf("ended call = %q, want call-1", callID)
	}

	now = now.Add(time.Second)
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("accepted release was repeated = %+v", expired)
	}
	calls.mu.Lock()
	delete(calls.calls, "call-1")
	calls.mu.Unlock()
	if err := manager.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if expired := manager.expiredCalls(); len(expired) != 0 {
		t.Fatalf("terminal authoritative snapshot retained lease = %+v", expired)
	}
}
