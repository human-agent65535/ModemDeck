package communication

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

func TestRefreshPublishesRuntimeEventsOnlyForChangedResources(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC)
	agent := connectedAgent(observedAt)
	service, err := New(agent, &fakeRepository{}, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	events := runtimeevents.NewBuffer(8)
	if err := service.SetRuntimeEventPublisher(events); err != nil {
		t.Fatalf("SetRuntimeEventPublisher() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("unchanged Refresh() error = %v", err)
	}

	window, updates, cancel := events.Subscribe(0)
	defer cancel()
	if len(window.Events) != 2 {
		t.Fatalf("initial runtime events = %+v, want lines and calls", window.Events)
	}
	assertRuntimeResource(t, window.Events[0], runtimeevents.ResourceLines)
	assertRuntimeResource(t, window.Events[1], runtimeevents.ResourceCalls)

	agent.snapshot.Revision = "snapshot-4"
	agent.snapshot.ObservedAt = observedAt.Add(3 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("revision-only Refresh() error = %v", err)
	}
	select {
	case event := <-updates:
		t.Fatalf("revision-only refresh published event %+v", event)
	default:
	}

	agent.snapshot.Lines[0].State = "registered"
	agent.snapshot.Revision = "snapshot-5"
	agent.snapshot.ObservedAt = observedAt.Add(6 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("line Refresh() error = %v", err)
	}
	select {
	case event := <-updates:
		assertRuntimeResource(t, event, runtimeevents.ResourceLines)
	case <-time.After(time.Second):
		t.Fatal("runtime event was not published for changed line state")
	}

	agent.snapshot.Lines[0].State = ""
	agent.snapshot.Revision = "snapshot-6"
	agent.snapshot.ObservedAt = observedAt.Add(9 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("reverted line Refresh() error = %v", err)
	}
	select {
	case event := <-updates:
		assertRuntimeResource(t, event, runtimeevents.ResourceLines)
	case <-time.After(time.Second):
		t.Fatal("runtime event was not published when line state returned to an earlier value")
	}

	agent.snapshot.Calls = []agentclient.Call{{
		ID:        "call-1",
		LineID:    "line-1",
		Number:    "+819012345678",
		Direction: "outgoing",
		State:     "ringing-out",
	}}
	agent.snapshot.Revision = "snapshot-7"
	agent.snapshot.ObservedAt = observedAt.Add(12 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("call Refresh() error = %v", err)
	}
	select {
	case event := <-updates:
		assertRuntimeResource(t, event, runtimeevents.ResourceCalls)
	case <-time.After(time.Second):
		t.Fatal("runtime event was not published for changed call state")
	}
}

func assertRuntimeResource(
	t *testing.T,
	event runtimeevents.Event,
	resource runtimeevents.Resource,
) {
	t.Helper()
	if len(event.Resources) != 1 || event.Resources[0] != resource {
		t.Fatalf("runtime event resources = %v, want [%s]", event.Resources, resource)
	}
}
