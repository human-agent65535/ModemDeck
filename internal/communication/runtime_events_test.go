package communication

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

func TestRefreshPublishesRuntimeEventsOnlyForNewSnapshotRevision(t *testing.T) {
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
	if len(window.Events) != 1 {
		t.Fatalf("unchanged revision events = %+v, want one", window.Events)
	}
	assertCommunicationRuntimeResources(t, window.Events[0])

	agent.snapshot.Revision = "snapshot-4"
	agent.snapshot.ObservedAt = observedAt.Add(3 * time.Second)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("changed Refresh() error = %v", err)
	}
	select {
	case event := <-updates:
		assertCommunicationRuntimeResources(t, event)
	case <-time.After(time.Second):
		t.Fatal("runtime event was not published for changed revision")
	}
}

func assertCommunicationRuntimeResources(t *testing.T, event runtimeevents.Event) {
	t.Helper()
	if len(event.Resources) != 2 ||
		event.Resources[0] != runtimeevents.ResourceLines ||
		event.Resources[1] != runtimeevents.ResourceCalls {
		t.Fatalf("runtime event resources = %v", event.Resources)
	}
}
