package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecordingSettingsAndIncomingCallDefault(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	settings, err := repository.RecordingSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DefaultEnabled || settings.Revision != 1 {
		t.Fatalf("initial settings = %+v", settings)
	}
	settings, err = repository.UpdateRecordingSettings(ctx, true, settings.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.DefaultEnabled || settings.Revision != 2 {
		t.Fatalf("updated settings = %+v", settings)
	}
	if _, err := repository.UpdateRecordingSettings(ctx, false, 1); !errors.Is(
		err,
		ErrRecordingRevisionConflict,
	) {
		t.Fatalf("stale update error = %v, want revision conflict", err)
	}

	applyRecordingTestCall(t, repository, "call-incoming-default", "", "incoming")
	state, err := repository.CallRecordingState(ctx, "call-incoming-default")
	if err != nil {
		t.Fatal(err)
	}
	if state.Preference != RecordingPreferenceDefault ||
		!state.Enabled ||
		state.Generation != 1 ||
		state.Status != RecordingStatePending {
		t.Fatalf("incoming recording state = %+v", state)
	}
}

func TestOutgoingRecordingOverrideIsBoundToRequestID(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	settings, err := repository.RecordingSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateRecordingSettings(ctx, true, settings.Revision); err != nil {
		t.Fatal(err)
	}
	if err := repository.PrepareCallRecordingRequest(ctx, "request-recording-off", false); err != nil {
		t.Fatal(err)
	}
	if err := repository.PrepareCallRecordingRequest(ctx, "request-recording-off", false); err != nil {
		t.Fatalf("idempotent prepare error = %v", err)
	}
	if err := repository.PrepareCallRecordingRequest(
		ctx,
		"request-recording-off",
		true,
	); !errors.Is(err, ErrRecordingRequestConflict) {
		t.Fatalf("conflicting prepare error = %v", err)
	}

	applyRecordingTestCall(
		t,
		repository,
		"call-outgoing-override",
		"request-recording-off",
		"outgoing",
	)
	state, err := repository.CallRecordingState(ctx, "call-outgoing-override")
	if err != nil {
		t.Fatal(err)
	}
	if state.Preference != RecordingPreferenceOverride ||
		state.Enabled ||
		state.Generation != 0 ||
		state.Status != RecordingStateOff {
		t.Fatalf("outgoing recording state = %+v", state)
	}
	if got := recordingRequestCount(t, repository, "request-recording-off"); got != 0 {
		t.Fatalf("consumed request rows = %d, want 0", got)
	}
	state, err = repository.SetCallRecordingEnabled(ctx, "call-outgoing-override", true)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled {
		t.Fatalf("toggled recording state = %+v", state)
	}
	if err := repository.PrepareCallRecordingRequest(
		ctx,
		"request-recording-off",
		false,
	); err != nil {
		t.Fatalf("bound idempotent prepare error = %v", err)
	}
	if err := repository.PrepareCallRecordingRequest(
		ctx,
		"request-recording-off",
		true,
	); !errors.Is(err, ErrRecordingRequestConflict) {
		t.Fatalf("bound conflicting prepare error = %v", err)
	}
}

func TestExpiredRecordingRequestCleanupIsBoundedAndSkipsBoundCalls(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	for _, requestID := range []string{"request-old-a", "request-old-b", "request-old-c"} {
		if err := repository.PrepareCallRecordingRequest(ctx, requestID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.PrepareCallRecordingRequest(ctx, "request-recent", false); err != nil {
		t.Fatal(err)
	}
	if err := repository.PrepareCallRecordingRequest(ctx, "request-bound", false); err != nil {
		t.Fatal(err)
	}
	applyRecordingTestCall(
		t,
		repository,
		"call-bound-request",
		"request-bound",
		"outgoing",
	)
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_call_recording_requests (request_id, enabled, created_at)
		 VALUES ('request-bound', 0, '2000-01-01 00:00:00')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_requests
		 SET created_at = '2000-01-01 00:00:00'
		 WHERE request_id LIKE 'request-old-%'`,
	); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	deleted, err := repository.DeleteExpiredCallRecordingRequests(ctx, cutoff, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("first cleanup deleted = %d, want 2", deleted)
	}
	deleted, err = repository.DeleteExpiredCallRecordingRequests(ctx, cutoff, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("second cleanup deleted = %d, want 1", deleted)
	}
	if got := recordingRequestCount(t, repository, "request-bound"); got != 1 {
		t.Fatalf("bound request rows = %d, want 1", got)
	}
	if got := recordingRequestCount(t, repository, "request-recent"); got != 1 {
		t.Fatalf("recent request rows = %d, want 1", got)
	}
	if _, err := repository.DeleteExpiredCallRecordingRequests(
		ctx,
		time.Time{},
		1,
	); !errors.Is(err, ErrRecordingValidation) {
		t.Fatalf("zero cutoff error = %v, want validation", err)
	}
	if _, err := repository.DeleteExpiredCallRecordingRequests(
		ctx,
		cutoff,
		maxRecordingCleanupBatch+1,
	); !errors.Is(err, ErrRecordingValidation) {
		t.Fatalf("oversized batch error = %v, want validation", err)
	}
}

func TestExpiredRecordingRequestCleanupUsesBoundedIndexes(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	rows, err := repository.database.QueryContext(
		context.Background(),
		`EXPLAIN QUERY PLAN
		 SELECT request.request_id
		 FROM modemdeck_call_recording_requests request
		 WHERE request.created_at < ?
		 AND NOT EXISTS (
			SELECT 1 FROM call_history call
			WHERE call.request_id = request.request_id
			AND call.request_id <> ''
		 )
		 ORDER BY request.created_at ASC, request.request_id ASC
		 LIMIT ?`,
		time.Now().UTC().Format(sqliteTimestampLayout),
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var requestAgeIndex, boundCallIndex bool
	for rows.Next() {
		var (
			id      int
			parent  int
			notUsed int
			detail  string
		)
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		requestAgeIndex = requestAgeIndex ||
			strings.Contains(detail, "idx_modemdeck_call_recording_requests_created")
		boundCallIndex = boundCallIndex ||
			strings.Contains(detail, "ux_call_history_request_id")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !requestAgeIndex || !boundCallIndex {
		t.Fatalf(
			"cleanup indexes: request age=%t, bound call=%t",
			requestAgeIndex,
			boundCallIndex,
		)
	}
}

func TestExpiredRecordingRequestCleanupHonorsSameDayCutoff(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	for requestID, createdAt := range map[string]string{
		"request-expired": "2026-07-22 14:59:59",
		"request-fresh":   "2026-07-22 15:00:01",
	} {
		if _, err := repository.database.ExecContext(
			ctx,
			`INSERT INTO modemdeck_call_recording_requests (request_id, enabled, created_at)
			 VALUES (?, 0, ?)`,
			requestID,
			createdAt,
		); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := time.Date(2026, time.July, 22, 15, 0, 0, 0, time.UTC)
	deleted, err := repository.DeleteExpiredCallRecordingRequests(ctx, cutoff, 10)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("same-day cleanup deleted = %d, want 1", deleted)
	}
	if got := recordingRequestCount(t, repository, "request-expired"); got != 0 {
		t.Fatalf("expired request rows = %d, want 0", got)
	}
	if got := recordingRequestCount(t, repository, "request-fresh"); got != 1 {
		t.Fatalf("fresh request rows = %d, want 1", got)
	}
}

func TestRecordingToggleSegmentsAndCrashCleanup(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	applyRecordingTestCall(t, repository, "call-segments", "", "incoming")

	state, err := repository.SetCallRecordingEnabled(ctx, "call-segments", true)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.Generation != 1 || state.Status != RecordingStatePending {
		t.Fatalf("enabled state = %+v", state)
	}
	first, err := repository.CreateRecordingSegment(ctx, RecordingSegment{
		ID:           "segment-one",
		CallID:       "call-segments",
		RelativePath: "call-segments/segment-one.ogg",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	if err := repository.MarkRecordingSegmentStarted(
		ctx,
		first.CallID,
		first.ID,
		now,
	); err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteRecordingSegment(
		ctx,
		first.CallID,
		first.ID,
		now.Add(time.Second),
		1000,
		2048,
	); err != nil {
		t.Fatal(err)
	}
	state, err = repository.CallRecordingState(ctx, "call-segments")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != RecordingStateReady || state.ActiveSegmentID != "" || !state.Enabled {
		t.Fatalf("ready state = %+v", state)
	}

	state, err = repository.SetCallRecordingEnabled(ctx, "call-segments", false)
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled || state.Generation != 2 || state.Status != RecordingStateOff {
		t.Fatalf("disabled state = %+v", state)
	}
	state, err = repository.SetCallRecordingEnabled(ctx, "call-segments", true)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.Generation != 3 || state.Status != RecordingStatePending {
		t.Fatalf("re-enabled state = %+v", state)
	}
	second, err := repository.CreateRecordingSegment(ctx, RecordingSegment{
		ID:           "segment-two",
		CallID:       "call-segments",
		RelativePath: "call-segments/segment-two.ogg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRecordingSegmentStarted(
		ctx,
		second.CallID,
		second.ID,
		now.Add(2*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if err := repository.FailInterruptedRecordingSegments(ctx); err != nil {
		t.Fatal(err)
	}
	segments, err := repository.RecordingSegments(ctx, "call-segments")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 ||
		segments[0].SegmentIndex != 1 ||
		segments[0].Status != RecordingSegmentReady ||
		segments[1].SegmentIndex != 2 ||
		segments[1].Status != RecordingSegmentFailed ||
		segments[1].FailureCode != "interrupted" {
		t.Fatalf("segments = %+v", segments)
	}
	state, err = repository.CallRecordingState(ctx, "call-segments")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != RecordingStateFailed ||
		state.ActiveSegmentID != "" ||
		state.LastErrorCode != "interrupted" {
		t.Fatalf("interrupted state = %+v", state)
	}
}

func TestRecordingPathValidationRejectsTraversal(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	applyRecordingTestCall(t, repository, "call-path", "", "incoming")
	_, err := repository.CreateRecordingSegment(context.Background(), RecordingSegment{
		ID:           "segment-path",
		CallID:       "call-path",
		RelativePath: "../segment-path.ogg",
	})
	if !errors.Is(err, ErrRecordingValidation) {
		t.Fatalf("CreateRecordingSegment() error = %v, want validation", err)
	}
}

func TestInterruptedRecoveryPreservesActivePendingRecordingWithoutSegment(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	applyRecordingTestCall(t, repository, "call-pending-media", "", "incoming")
	if _, err := repository.EnsureCallRecordingState(ctx, "call-pending-media"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SetCallRecordingEnabled(ctx, "call-pending-media", true); err != nil {
		t.Fatal(err)
	}
	if err := repository.FailInterruptedRecordingSegments(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := repository.CallRecordingState(ctx, "call-pending-media")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != RecordingStatePending || state.LastErrorCode != "" {
		t.Fatalf("active pending state = %+v", state)
	}

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-recording",
		Revision:   "snapshot-pending-media-ended",
		ObservedAt: time.Date(2026, time.July, 23, 14, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.FailInterruptedRecordingSegments(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = repository.CallRecordingState(ctx, "call-pending-media")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != RecordingStateFailed ||
		state.LastErrorCode != "call_ended_before_recording" {
		t.Fatalf("terminal pending state = %+v", state)
	}
}

func applyRecordingTestCall(
	t *testing.T,
	repository *Store,
	callID, requestID, direction string,
) {
	t.Helper()
	observed := time.Date(2026, time.July, 23, 14, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "line-recording",
		Model:               "Fixture modem",
		EquipmentIdentifier: "990000000000099",
		State:               "connected",
		ICCID:               "8901000000000000099",
		IMSI:                "440500000000099",
	}
	err := repository.ApplyHardwareSnapshot(context.Background(), HardwareSnapshot{
		BootEpoch:  "boot-recording",
		Revision:   "snapshot-" + callID,
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls: []HardwareCall{{
			AppID:           callID,
			RequestID:       requestID,
			LineID:          line.ID,
			EndpointCallID:  "endpoint-" + callID,
			Number:          "+818000000099",
			Direction:       direction,
			Phase:           "active",
			AudioPort:       "hw:9,0",
			AudioEncoding:   "pcm",
			AudioResolution: "s16le",
			AudioRate:       8000,
			MediaAvailable:  true,
			Revision:        1,
			ObservedAt:      observed,
		}},
	})
	if err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
}

func recordingRequestCount(t *testing.T, repository *Store, requestID string) int {
	t.Helper()
	var count int
	if err := repository.database.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM modemdeck_call_recording_requests WHERE request_id = ?`,
		requestID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
