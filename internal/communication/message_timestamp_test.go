package communication

import (
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestProjectMessageKeepsModemTimestampWithHourOnlyOffset(t *testing.T) {
	t.Parallel()
	observedAt := time.Date(2026, 7, 24, 7, 0, 0, 0, time.UTC)
	projected := projectMessage(
		agentclient.Message{
			ID:        "message-1",
			LineID:    "line-1",
			Number:    "+818012345678",
			Text:      "hello",
			Direction: "incoming",
			State:     "received",
			StateCode: 3,
			Timestamp: "2026-07-24T14:30:17+08",
		},
		store.LineSummary{ID: "line-1", ICCID: "iccid-1", IMSI: "imsi-1"},
		"",
		"hello",
		observedAt,
		1,
	)
	want := time.Date(2026, 7, 24, 6, 30, 17, 0, time.UTC)
	if !projected.Timestamp.Equal(want) {
		t.Fatalf("projected timestamp = %s, want %s", projected.Timestamp, want)
	}
}

func TestProjectSnapshotWaitsForIncomingMessageCompletion(t *testing.T) {
	t.Parallel()
	observedAt := time.Date(2026, 7, 24, 7, 0, 0, 0, time.UTC)
	snapshot := agentclient.Snapshot{
		Revision:   "revision-1",
		ObservedAt: observedAt,
		Lines: []agentclient.Line{{
			ID:            "line-1",
			SIMIdentifier: "iccid-1",
			IMSI:          "imsi-1",
		}},
		Messages: []agentclient.Message{{
			ID:        "message-1",
			LineID:    "line-1",
			Number:    "+818012345678",
			Text:      "partial",
			Direction: "incoming",
			State:     "receiving",
			StateCode: 2,
			Timestamp: "2026-07-24T14:30:17+08",
		}},
	}
	projected, _ := projectSnapshot(snapshot, "boot-1")
	if len(projected.Messages) != 0 {
		t.Fatalf("receiving message was persisted: %+v", projected.Messages)
	}

	snapshot.Messages[0].State = "received"
	snapshot.Messages[0].StateCode = 3
	projected, _ = projectSnapshot(snapshot, "boot-1")
	if len(projected.Messages) != 1 {
		t.Fatalf("completed message was not persisted: %+v", projected.Messages)
	}
}
