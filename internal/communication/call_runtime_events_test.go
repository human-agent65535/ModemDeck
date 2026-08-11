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
	if err := service.SetCallEventPublisher(events); err != nil {
		t.Fatalf("SetCallEventPublisher() error = %v", err)
	}
	updates, cancel := events.Subscribe()
	defer cancel()

	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	event := <-updates
	if event.Kind != callevents.KindIncoming ||
		event.CallID != "call-incoming-1" ||
		event.LineID != "line-1" ||
		event.RemoteNumber != "+818012345678" ||
		event.DisplayName != "Example Contact" ||
		!event.ObservedAt.Equal(now) {
		t.Fatalf("published event = %+v", event)
	}
}

func TestRefreshPublishesCommittedTerminalCall(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 3, 0, 0, 0, time.UTC)
	activeAt := now.Add(-time.Minute).Format(time.RFC3339Nano)
	repository := &fakeRepository{snapshotResult: store.HardwareSnapshotResult{
		TerminalCalls: []store.Call{{
			ID:           "call-terminal-1",
			LineID:       "line-1",
			RemoteNumber: "+818012345678",
			ContactName:  "Example Contact",
			Direction:    "incoming",
			Phase:        "ended",
			Revision:     12,
			ActiveAt:     &activeAt,
			EndReason:    "terminated",
		}},
	}}
	events := callevents.NewBuffer(8)
	service, err := New(connectedAgent(now), repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := service.SetCallEventPublisher(events); err != nil {
		t.Fatalf("SetCallEventPublisher() error = %v", err)
	}
	updates, cancel := events.Subscribe()
	defer cancel()

	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	event := <-updates
	if event.Kind != callevents.KindTerminal ||
		event.CallID != "call-terminal-1" ||
		event.LineID != "line-1" ||
		event.Revision != 12 ||
		event.Phase != "ended" ||
		event.EndReason != "terminated" ||
		!event.WasAnswered ||
		!event.ObservedAt.Equal(now) {
		t.Fatalf("published event = %+v", event)
	}
}
