package communication

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callevents"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestRefreshPublishesCommittedIncomingCall(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 3, 0, 0, 0, time.UTC)
	repository := &fakeRepository{snapshotResult: store.HardwareSnapshotResult{
		CreatedIncomingCalls: []store.Call{{
			ID:           "call-incoming-1",
			LineID:       "line-1",
			RemoteNumber: "+818012345678",
			ContactName:  "Example Contact",
			Direction:    "incoming",
			Phase:        "ringing",
		}},
	}}
	events := callevents.NewBuffer(8)
	service, err := New(connectedAgent(now), repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := service.SetIncomingCallPublisher(events); err != nil {
		t.Fatalf("SetIncomingCallPublisher() error = %v", err)
	}
	updates, cancel := events.Subscribe()
	defer cancel()

	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	event := <-updates
	if event.CallID != "call-incoming-1" ||
		event.LineID != "line-1" ||
		event.RemoteNumber != "+818012345678" ||
		event.DisplayName != "Example Contact" ||
		!event.ObservedAt.Equal(now) {
		t.Fatalf("published event = %+v", event)
	}
}
