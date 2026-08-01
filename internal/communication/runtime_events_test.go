package communication

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestRefreshPublishesLatestStateOnlyWhenProjectionChanges(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.August, 2, 14, 0, 0, 0, time.UTC)
	agent := connectedAgent(observedAt)
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	hub := runtimeevents.NewHub()
	if err := service.SetRuntimeEventPublisher(hub); err != nil {
		t.Fatalf("SetRuntimeEventPublisher() error = %v", err)
	}
	_, updates, cancel := hub.Subscribe()
	defer cancel()

	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	initial := nextRuntimeSignal(t, updates)
	if initial.Revision != 1 || initial.DataRevision != 0 {
		t.Fatalf("initial signal = %+v", initial)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("unchanged Refresh() error = %v", err)
	}
	assertNoRuntimeSignal(t, updates)

	agent.snapshot.Revision = "snapshot-4"
	agent.snapshot.ObservedAt = observedAt.Add(3 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("revision-only Refresh() error = %v", err)
	}
	assertNoRuntimeSignal(t, updates)

	agent.snapshot.Lines[0].State = "registered"
	agent.snapshot.Revision = "snapshot-5"
	agent.snapshot.ObservedAt = observedAt.Add(6 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("line Refresh() error = %v", err)
	}
	lineSignal := nextRuntimeSignal(t, updates)
	if lineSignal.Revision != 2 || lineSignal.DataRevision != 0 {
		t.Fatalf("line signal = %+v; durable revision must stay unchanged", lineSignal)
	}

	agent.snapshot.Calls = []agentclient.Call{{
		ID:        "call-1",
		LineID:    "line-1",
		Number:    "+819012345678",
		Direction: "outgoing",
		State:     "ringing-out",
	}}
	agent.snapshot.Revision = "snapshot-6"
	agent.snapshot.ObservedAt = observedAt.Add(9 * time.Second)
	repository.activeCalls = []store.Call{{
		ID:           "call-1",
		LineID:       "line-1",
		Direction:    "outgoing",
		RemoteNumber: "+819012345678",
		Phase:        "ringing",
	}}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("call Refresh() error = %v", err)
	}
	callSignal := nextRuntimeSignal(t, updates)
	if callSignal.Revision != 3 || callSignal.DataRevision != 0 {
		t.Fatalf("call signal = %+v; starting a call is live state", callSignal)
	}
}

func TestRefreshPublishesPersistedActiveCallChanges(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.August, 2, 14, 30, 0, 0, time.UTC)
	agent := connectedAgent(observedAt)
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	hub := runtimeevents.NewHub()
	if err := service.SetRuntimeEventPublisher(hub); err != nil {
		t.Fatalf("SetRuntimeEventPublisher() error = %v", err)
	}
	_, updates, cancel := hub.Subscribe()
	defer cancel()
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh() error = %v", err)
	}
	initial := nextRuntimeSignal(t, updates)

	repository.activeCalls = []store.Call{{
		ID:             "call-command-created",
		LineID:         "line-1",
		Direction:      "outgoing",
		RemoteNumber:   "+819012345678",
		Phase:          "active",
		MediaAvailable: true,
	}}
	agent.snapshot.ObservedAt = observedAt.Add(time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("active call Refresh() error = %v", err)
	}
	active := nextRuntimeSignal(t, updates)
	if active.DataRevision != initial.DataRevision {
		t.Fatalf("active signal = %+v, initial = %+v", active, initial)
	}

	repository.activeCalls = nil
	agent.snapshot.ObservedAt = observedAt.Add(2 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("ended call Refresh() error = %v", err)
	}
	ended := nextRuntimeSignal(t, updates)
	if ended.DataRevision != active.DataRevision+1 {
		t.Fatalf("ended signal = %+v, active = %+v", ended, active)
	}
}

func TestRefreshPublishesCurrentStateAcrossProviderFailure(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.August, 2, 15, 0, 0, 0, time.UTC)
	agent := connectedAgent(observedAt)
	service, err := New(agent, &fakeRepository{}, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	hub := runtimeevents.NewHub()
	if err := service.SetRuntimeEventPublisher(hub); err != nil {
		t.Fatalf("SetRuntimeEventPublisher() error = %v", err)
	}
	_, updates, cancel := hub.Subscribe()
	defer cancel()
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh() error = %v", err)
	}
	initial := nextRuntimeSignal(t, updates)

	agent.health.Provider.BootEpoch = "boot-2"
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("new-epoch Refresh() error = %v", err)
	}
	epoch := nextRuntimeSignal(t, updates)
	if epoch.Revision != initial.Revision+1 || epoch.DataRevision != initial.DataRevision {
		t.Fatalf("epoch signal = %+v, initial = %+v", epoch, initial)
	}

	agent.healthError = errors.New("agent unavailable")
	if _, err := service.Refresh(context.Background()); err == nil {
		t.Fatal("failed Refresh() error = nil")
	}
	failure := nextRuntimeSignal(t, updates)
	if failure.DataRevision != epoch.DataRevision {
		t.Fatalf("failure signal = %+v; failure is live state, not durable data", failure)
	}

	agent.healthError = nil
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("recovery Refresh() error = %v", err)
	}
	recovery := nextRuntimeSignal(t, updates)
	if recovery.Revision != failure.Revision+1 ||
		recovery.DataRevision != failure.DataRevision {
		t.Fatalf("recovery signal = %+v, failure = %+v", recovery, failure)
	}
}

func nextRuntimeSignal(t *testing.T, updates <-chan runtimeevents.Signal) runtimeevents.Signal {
	t.Helper()
	select {
	case signal := <-updates:
		return signal
	case <-time.After(time.Second):
		t.Fatal("runtime signal was not published")
		return runtimeevents.Signal{}
	}
}

func assertNoRuntimeSignal(t *testing.T, updates <-chan runtimeevents.Signal) {
	t.Helper()
	select {
	case signal := <-updates:
		t.Fatalf("unexpected runtime signal: %+v", signal)
	case <-time.After(20 * time.Millisecond):
	}
}
