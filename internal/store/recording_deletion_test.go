package store

import (
	"context"
	"testing"
	"time"
)

func TestDeleteRecordingSegmentClearsFavoriteOnlyAfterFinalSegment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newHardwareTestStore(t)
	const callID = "call-delete-recording-favorite"
	applyRecordingTestCall(t, repository, callID, "", "incoming")

	for index, segmentID := range []string{
		"segment-delete-favorite-1",
		"segment-delete-favorite-2",
	} {
		if _, err := repository.CreateRecordingSegment(ctx, RecordingSegment{
			ID:           segmentID,
			CallID:       callID,
			RelativePath: callID + "/" + segmentID + ".ogg",
		}); err != nil {
			t.Fatalf("create segment %d: %v", index+1, err)
		}
		if err := repository.CompleteRecordingSegment(
			ctx,
			callID,
			segmentID,
			time.Date(2026, time.July, 29, 14, index, 0, 0, time.UTC),
			1000,
			8,
		); err != nil {
			t.Fatalf("complete segment %d: %v", index+1, err)
		}
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET is_favorite = 1
		 WHERE call_id = ?`,
		callID,
	); err != nil {
		t.Fatalf("favorite recording aggregate: %v", err)
	}

	if err := repository.DeleteRecordingSegment(
		ctx,
		callID,
		"segment-delete-favorite-1",
	); err != nil {
		t.Fatalf("delete first segment: %v", err)
	}
	if favorite := recordingFavoriteValue(t, repository, callID); favorite != 1 {
		t.Fatalf("favorite after first deletion = %d, want 1", favorite)
	}

	if err := repository.DeleteRecordingSegment(
		ctx,
		callID,
		"segment-delete-favorite-2",
	); err != nil {
		t.Fatalf("delete final segment: %v", err)
	}
	if favorite := recordingFavoriteValue(t, repository, callID); favorite != 0 {
		t.Fatalf("favorite after final deletion = %d, want 0", favorite)
	}
	entries, err := repository.RecordingEntries(
		ctx,
		RecordingQuery{Limit: MaxQueryLimit},
	)
	if err != nil {
		t.Fatalf("list recording entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("recording aggregate still exists: %+v", entries)
	}
	if _, err := repository.CallByID(ctx, callID); err != nil {
		t.Fatalf("call was deleted with its recording segments: %v", err)
	}
}

func recordingFavoriteValue(
	t *testing.T,
	repository *Store,
	callID string,
) int {
	t.Helper()
	var favorite int
	if err := repository.database.QueryRowContext(
		context.Background(),
		`SELECT is_favorite
		 FROM modemdeck_call_recording_state
		 WHERE call_id = ?`,
		callID,
	).Scan(&favorite); err != nil {
		t.Fatalf("read recording favorite state: %v", err)
	}
	return favorite
}
