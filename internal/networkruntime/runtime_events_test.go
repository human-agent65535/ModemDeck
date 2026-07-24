package networkruntime

import (
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

func TestNetworkSnapshotsPublishBoundedRuntimeEvents(t *testing.T) {
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
	if len(window.Events) != 1 {
		t.Fatalf("duplicate snapshot events = %+v, want one", window.Events)
	}
	assertNetworkRuntimeResource(t, window.Events[0])

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

func assertNetworkRuntimeResource(t *testing.T, event runtimeevents.Event) {
	t.Helper()
	if len(event.Resources) != 1 || event.Resources[0] != runtimeevents.ResourceNetwork {
		t.Fatalf("runtime event resources = %v", event.Resources)
	}
}
