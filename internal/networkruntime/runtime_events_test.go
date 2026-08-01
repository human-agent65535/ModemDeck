package networkruntime

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

func TestNetworkSnapshotsPublishLatestStateSignals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 2, 14, 0, 0, 0, time.UTC)
	hub := runtimeevents.NewHub()
	service := &Service{
		now:           func() time.Time { return now },
		runtimeEvents: hub,
		state: Status{
			Lines:   []agentclient.NetworkLine{},
			Proxies: []agentclient.NetworkProxy{},
		},
	}
	_, updates, cancel := hub.Subscribe()
	defer cancel()
	snapshot := agentclient.NetworkSnapshot{
		BootEpoch:  "boot-network-events",
		ObservedAt: now,
		Lines:      []agentclient.NetworkLine{},
		Proxies:    []agentclient.NetworkProxy{},
	}
	service.setSnapshot(snapshot)
	service.setSnapshot(snapshot)
	coalesced := nextNetworkSignal(t, updates)
	if coalesced.Revision != 2 || coalesced.DataRevision != 0 {
		t.Fatalf("coalesced signal = %+v", coalesced)
	}

	snapshot.ObservedAt = now.Add(30 * time.Second)
	service.setSnapshot(snapshot)
	latest := nextNetworkSignal(t, updates)
	if latest.Revision != 3 || !latest.ObservedAt.Equal(snapshot.ObservedAt) {
		t.Fatalf("latest signal = %+v", latest)
	}

	service.setUnavailable("host agent unavailable")
	service.setUnavailable("host agent unavailable")
	unavailable := nextNetworkSignal(t, updates)
	if unavailable.Revision != 4 || unavailable.DataRevision != 0 {
		t.Fatalf("unavailable signal = %+v", unavailable)
	}
	select {
	case signal := <-updates:
		t.Fatalf("unchanged unavailable state published another signal: %+v", signal)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPeriodicReconciliationAppliesSelectionAfterLateLineDiscovery(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	service.interval = 5 * time.Millisecond

	policy, err := repository.EnsureNetworkSelectionPolicy(
		context.Background(),
		"line-1",
	)
	if err != nil {
		t.Fatalf("EnsureNetworkSelectionPolicy() error = %v", err)
	}
	if _, err := repository.UpdateNetworkSelectionPolicy(
		context.Background(),
		"line-1",
		"manual",
		"44010",
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateNetworkSelectionPolicy() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		agent.mu.Lock()
		fullSnapshotCalls := agent.fullSnapshotCalls
		selectionCalls := len(agent.selectionCalls)
		agent.mu.Unlock()
		status, err := service.Status(context.Background())
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if fullSnapshotCalls >= 1 && selectionCalls == 0 &&
			!status.ApplyPending && status.ApplyStatus == ApplyStatusApplied {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"startup did not settle before line discovery: snapshots=%d selections=%d status=%+v",
				fullSnapshotCalls,
				selectionCalls,
				status,
			)
		}
		time.Sleep(time.Millisecond)
	}

	agent.mu.Lock()
	agent.fullSnapshot = agentclient.Snapshot{Lines: []agentclient.Line{{
		ID:                   "line-1",
		SIMPresent:           true,
		SavedPolicySupported: true,
	}}}
	agent.mu.Unlock()

	deadline = time.Now().Add(time.Second)
	for {
		applied, err := repository.NetworkSelectionPolicy(
			context.Background(),
			"line-1",
		)
		if err != nil {
			t.Fatalf("NetworkSelectionPolicy() error = %v", err)
		}
		if applied.AppliedRevision == applied.Revision &&
			applied.AppliedBootEpoch == "boot-1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic reconciliation did not apply configured policy: %+v", applied)
		}
		time.Sleep(time.Millisecond)
	}
}

func nextNetworkSignal(t *testing.T, updates <-chan runtimeevents.Signal) runtimeevents.Signal {
	t.Helper()
	select {
	case signal := <-updates:
		return signal
	case <-time.After(time.Second):
		t.Fatal("network state signal was not published")
		return runtimeevents.Signal{}
	}
}
