package recording

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestDeleteAllRecordingSegmentsRemovesAggregateButKeepsCall(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	const callID = "call-delete-all-segments"
	applyServiceTestCall(t, fixture.repository, callID, "", "incoming", false)

	paths := []string{
		createReadyRecordingFile(t, fixture, callID, "segment-delete-all-1"),
		createReadyRecordingFile(t, fixture, callID, "segment-delete-all-2"),
	}

	if err := fixture.service.DeleteRecording(
		context.Background(),
		callID,
		"segment-delete-all-1",
	); err != nil {
		t.Fatalf("delete first segment: %v", err)
	}
	receiveRecordingChange(t, fixture.changes)
	recordings, err := fixture.service.CallRecordings(context.Background(), callID)
	if err != nil {
		t.Fatalf("list recordings after first deletion: %v", err)
	}
	if len(recordings.Segments) != 1 {
		t.Fatalf("segments after first deletion = %+v, want one segment", recordings.Segments)
	}

	if err := fixture.service.DeleteRecording(
		context.Background(),
		callID,
		"segment-delete-all-2",
	); err != nil {
		t.Fatalf("delete final segment: %v", err)
	}
	receiveRecordingChange(t, fixture.changes)

	for _, path := range paths {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recording file %q still exists: %v", path, err)
		}
	}
	recordings, err = fixture.service.CallRecordings(context.Background(), callID)
	if err != nil {
		t.Fatalf("list recordings after final deletion: %v", err)
	}
	if len(recordings.Segments) != 0 {
		t.Fatalf("segments after final deletion = %+v, want none", recordings.Segments)
	}
	entries, err := fixture.repository.RecordingEntries(
		context.Background(),
		store.RecordingQuery{Limit: store.MaxQueryLimit},
	)
	if err != nil {
		t.Fatalf("list recording catalog after final deletion: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("recording aggregate still exists: %+v", entries)
	}
	if _, err := fixture.repository.CallByID(context.Background(), callID); err != nil {
		t.Fatalf("call was deleted with its recording segments: %v", err)
	}
}
