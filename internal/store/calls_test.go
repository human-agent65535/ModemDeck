package store

import (
	"context"
	"errors"
	"testing"
)

func TestCallFavoriteStatePersistsAndBatchFailureIsAtomic(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO call_history (
			id, line_id, direction, remote_number, phase, created_at, ended_at
		 ) VALUES
			(
				'call-favorite-a', 'line-main', 'incoming', '+818011111111',
				'ended', '2026-07-29 05:00:00', '2026-07-29 05:00:10'
			),
			(
				'call-favorite-b', 'line-main', 'outgoing', '+818022222222',
				'ended', '2026-07-29 05:01:00', '2026-07-29 05:01:10'
			)`,
	); err != nil {
		t.Fatalf("insert calls: %v", err)
	}

	if err := repository.SetCallFavoritesByIDs(
		ctx,
		[]string{" call-favorite-a ", "call-favorite-a", "call-favorite-b"},
		true,
	); err != nil {
		t.Fatalf("SetCallFavoritesByIDs() error = %v", err)
	}
	calls, err := repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() error = %v", err)
	}
	byID := make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if !byID["call-favorite-a"].Favorite || !byID["call-favorite-b"].Favorite {
		t.Fatalf("favorite calls = %+v", byID)
	}

	err = repository.SetCallFavoritesByIDs(
		ctx,
		[]string{"call-favorite-a", "call-does-not-exist"},
		false,
	)
	if !errors.Is(err, ErrCallNotFound) {
		t.Fatalf("failed batch error = %v, want ErrCallNotFound", err)
	}
	calls, err = repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() after failed batch error = %v", err)
	}
	byID = make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if !byID["call-favorite-a"].Favorite || !byID["call-favorite-b"].Favorite {
		t.Fatalf("failed batch partially changed favorites: %+v", byID)
	}
}

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

	if err := repository.MarkMissedCallsUnreadByIDs(ctx, []string{
		"call-requested",
		"call-outgoing",
	}); err != nil {
		t.Fatalf("MarkMissedCallsUnreadByIDs() error = %v", err)
	}
	calls, err = repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() after unread error = %v", err)
	}
	byID = make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if byID["call-requested"].Read {
		t.Fatalf("requested missed call remains read: %+v", byID["call-requested"])
	}
	if byID["call-outgoing"].Read {
		t.Fatalf("requested outgoing call was changed: %+v", byID["call-outgoing"])
	}
}

func TestCallsAssociateContactsOnlyByUnambiguousCanonicalNumber(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO contacts (id, display_name) VALUES
			('contact-jp', 'Japan'),
			('contact-cn', 'China'),
			('contact-shared-a', 'Shared A'),
			('contact-shared-b', 'Shared B');
		 INSERT INTO contact_phones (
			id, contact_id, original_number, canonical_e164, region, is_primary
		 ) VALUES
			('phone-jp', 'contact-jp', '09012345678', '+819011111111', 'JP', 1),
			('phone-cn', 'contact-cn', '09012345678', '+8613800138000', 'CN', 1),
			('phone-shared-a', 'contact-shared-a', '08011112222', '+819022222222', 'JP', 1),
			('phone-shared-b', 'contact-shared-b', '08033334444', '+819022222222', 'JP', 1);
		 INSERT INTO call_history (
			id, direction, remote_number, phase, created_at, ended_at
		 ) VALUES
			(
				'call-raw-local', 'incoming', '09012345678', 'ended',
				'2026-07-29 01:00:00', '2026-07-29 01:00:01'
			),
			(
				'call-unique-canonical', 'incoming', '+819011111111', 'ended',
				'2026-07-29 01:01:00', '2026-07-29 01:01:01'
			),
			(
				'call-ambiguous-canonical', 'incoming', '+819022222222', 'ended',
				'2026-07-29 01:02:00', '2026-07-29 01:02:01'
			);`,
	); err != nil {
		t.Fatalf("seed contact identity calls: %v", err)
	}

	calls, err := repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() error = %v", err)
	}
	byID := make(map[string]Call, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	if byID["call-raw-local"].ContactID != "" ||
		byID["call-raw-local"].ContactName != "" {
		t.Fatalf("raw local call matched a contact: %+v", byID["call-raw-local"])
	}
	if byID["call-unique-canonical"].ContactID != "contact-jp" ||
		byID["call-unique-canonical"].ContactName != "Japan" {
		t.Fatalf(
			"unique canonical call = %+v, want contact-jp",
			byID["call-unique-canonical"],
		)
	}
	if byID["call-ambiguous-canonical"].ContactID != "" ||
		byID["call-ambiguous-canonical"].ContactName != "" {
		t.Fatalf(
			"ambiguous canonical call matched a contact: %+v",
			byID["call-ambiguous-canonical"],
		)
	}
}
