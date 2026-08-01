package networkruntime

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

func TestNetworkSnapshotsPublishRuntimeInvalidations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC)
	events := runtimeevents.NewBuffer(8)
	service := &Service{
		now:           func() time.Time { return now },
		runtimeEvents: events,
		state: Status{
			Lines:   []agentclient.NetworkLine{},
			Proxies: []agentclient.NetworkProxy{},
		},
	}
	snapshot := agentclient.NetworkSnapshot{
		BootEpoch:  "boot-network-events",
		ObservedAt: now,
		Lines:      []agentclient.NetworkLine{},
		Proxies:    []agentclient.NetworkProxy{},
	}
	service.setSnapshot(snapshot)
	service.setSnapshot(snapshot)

	window, updates, cancel := events.Subscribe(0)
	defer cancel()
	if len(window.Events) != 2 {
		t.Fatalf("snapshot events = %+v, want one invalidation per observation", window.Events)
	}
	for _, event := range window.Events {
		assertNetworkRuntimeResource(t, event)
	}

	snapshot.ObservedAt = now.Add(30 * time.Second)
	service.setSnapshot(snapshot)
	select {
	case event := <-updates:
		assertNetworkRuntimeResource(t, event)
	case <-time.After(time.Second):
		t.Fatal("runtime event was not published for a new network sample")
	}

	service.setUnavailable("host agent unavailable")
	service.setUnavailable("host agent unavailable")
	select {
	case event := <-updates:
		assertNetworkRuntimeResource(t, event)
	case <-time.After(time.Second):
		t.Fatal("runtime event was not published for availability transition")
	}
	select {
	case event := <-updates:
		t.Fatalf("unchanged unavailable state published another event: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestLineEventReplaysConfiguredNetworkSelectionAfterLateDiscovery(t *testing.T) {
	t.Parallel()

	events := runtimeevents.NewBuffer(8)
	service, repository, agent := newNetworkTestService(t, nil)
	service.interval = time.Hour
	service.runtimeEventSource = events

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
		if fullSnapshotCalls == 1 && selectionCalls == 0 &&
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
	events.Publish(runtimeevents.Event{
		Resources: []runtimeevents.Resource{runtimeevents.ResourceLines},
	})

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
			t.Fatalf("line event did not replay configured policy: %+v", applied)
		}
		time.Sleep(time.Millisecond)
	}
}

func assertNetworkRuntimeResource(t *testing.T, event runtimeevents.Event) {
	t.Helper()
	if len(event.Resources) != 1 || event.Resources[0] != runtimeevents.ResourceNetwork {
		t.Fatalf("runtime event resources = %v", event.Resources)
	}
}
