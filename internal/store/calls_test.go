package store

import (
	"context"
	"testing"
)

func TestMissedCallReadStatePersistsAndExcludesLiveCalls(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO call_history (
			id, line_id, direction, remote_number, phase,
			created_at, ended_at, end_reason
		 ) VALUES
			(
				'call-missed', 'line-main', 'incoming', '+818011111111', 'ended',
				'2026-07-28 05:00:00', '2026-07-28 05:00:10', 'not_present_in_snapshot'
			),
			(
				'call-ringing', 'line-main', 'incoming', '+818022222222', 'ringing',
				'2026-07-28 05:01:00', NULL, ''
			),
			(
				'call-rejected', 'line-main', 'incoming', '+818033333333', 'ended',
				'2026-07-28 05:02:00', '2026-07-28 05:02:02', 'rejected'
			)`,
	); err != nil {
		t.Fatalf("insert calls: %v", err)
	}

	calls, err := repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() before read error = %v", err)
	}
	byID := make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if !byID["call-missed"].Missed || byID["call-missed"].Read {
		t.Fatalf("unread missed call = %+v", byID["call-missed"])
	}
	if byID["call-ringing"].Missed {
		t.Fatalf("ringing call was classified as missed: %+v", byID["call-ringing"])
	}
	if byID["call-rejected"].Missed {
		t.Fatalf("rejected call was classified as missed: %+v", byID["call-rejected"])
	}

	if err := repository.MarkMissedCallsRead(ctx); err != nil {
		t.Fatalf("MarkMissedCallsRead() error = %v", err)
	}
	calls, err = repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() after read error = %v", err)
	}
	byID = make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if !byID["call-missed"].Read {
		t.Fatalf("missed call read state was not persisted: %+v", byID["call-missed"])
	}
	if byID["call-ringing"].Read || byID["call-rejected"].Read {
		t.Fatalf(
			"non-missed calls were marked read: ringing=%+v rejected=%+v",
			byID["call-ringing"],
			byID["call-rejected"],
		)
	}

	missed, err := repository.Calls(ctx, CallQuery{Kind: CallKindMissed})
	if err != nil {
		t.Fatalf("Calls(missed) error = %v", err)
	}
	if len(missed) != 1 || missed[0].ID != "call-missed" || !missed[0].Read {
		t.Fatalf("missed calls = %+v, want one persisted read call", missed)
	}
}

func TestMarkMissedCallsReadByIDsOnlyMarksRequestedMissedCalls(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO call_history (
			id, line_id, direction, remote_number, phase,
			created_at, ended_at, end_reason
		 ) VALUES
			(
				'call-requested', 'line-main', 'incoming', '+818011111111', 'ended',
				'2026-07-28 06:00:00', '2026-07-28 06:00:10', 'not_present_in_snapshot'
			),
			(
				'call-not-requested', 'line-other', 'incoming', '+818022222222', 'ended',
				'2026-07-28 06:01:00', '2026-07-28 06:01:10', 'not_present_in_snapshot'
			),
			(
				'call-outgoing', 'line-main', 'outgoing', '+818033333333', 'ended',
				'2026-07-28 06:02:00', '2026-07-28 06:02:10', 'completed'
			)`,
	); err != nil {
		t.Fatalf("insert calls: %v", err)
	}

	if err := repository.MarkMissedCallsReadByIDs(ctx, []string{
		"",
		"call-requested",
		"call-requested",
		"call-outgoing",
	}); err != nil {
		t.Fatalf("MarkMissedCallsReadByIDs() error = %v", err)
	}

	calls, err := repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() error = %v", err)
	}
	byID := make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if !byID["call-requested"].Read {
		t.Fatalf("requested missed call remains unread: %+v", byID["call-requested"])
	}
	if byID["call-not-requested"].Read {
		t.Fatalf("unrequested missed call was marked read: %+v", byID["call-not-requested"])
	}
	if byID["call-outgoing"].Read {
		t.Fatalf("requested outgoing call was marked read: %+v", byID["call-outgoing"])
	}
}
