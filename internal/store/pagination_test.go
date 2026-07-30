package store

import (
	"context"
	"testing"
)

func TestCommunicationQueriesContinueAfterStableCursor(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(ctx, `
		INSERT INTO contacts (id, display_name) VALUES
			('contact-a', 'Alice'),
			('contact-b', 'Bob'),
			('contact-c', 'Bob');
		INSERT INTO sms (
			line_id, iccid, peer, content, type, timestamp, created_at
		) VALUES
			('line-main', '', '+810000000001', 'one', 1, '2026-07-30 01:00:00', '2026-07-30 01:00:00'),
			('line-main', '', '+810000000001', 'two', 1, '2026-07-30 02:00:00', '2026-07-30 02:00:00'),
			('line-main', '', '+810000000001', 'three', 1, '2026-07-30 02:00:00', '2026-07-30 02:00:00');
		DELETE FROM sms_contacts
			WHERE line_id = 'line-main' AND peer = '+810000000001';
		INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp,
			last_content, last_type
		) VALUES
			('line-a', '', '', '+810000000011', 11, '2026-07-30 04:00:00', 'first', 1),
			('line-a', '', '', '+810000000012', 12, '2026-07-30 03:00:00', 'second', 1),
			('line-b', '', '', '+810000000013', 13, '2026-07-30 03:00:00', 'third', 1);
		INSERT INTO call_history (
			id, line_id, direction, remote_number, phase, created_at, ended_at
		) VALUES
			('call-c', 'line-main', 'incoming', '+810000000021', 'ended', '2026-07-30 05:00:00', '2026-07-30 05:00:01'),
			('call-b', 'line-main', 'incoming', '+810000000022', 'ended', '2026-07-30 04:00:00', '2026-07-30 04:00:01'),
			('call-a', 'line-main', 'incoming', '+810000000023', 'ended', '2026-07-30 04:00:00', '2026-07-30 04:00:01');
		INSERT INTO modemdeck_call_recordings (
			id, call_id, segment_index, status, started_at, relative_path,
			created_at, updated_at
		) VALUES
			('recording-c', 'call-c', 1, 'ready', '2026-07-30 05:00:00', 'c.ogg', '2026-07-30 05:00:00', '2026-07-30 05:00:00'),
			('recording-b', 'call-b', 1, 'ready', '2026-07-30 04:00:00', 'b.ogg', '2026-07-30 04:00:00', '2026-07-30 04:00:00'),
			('recording-a', 'call-a', 1, 'ready', '2026-07-30 04:00:00', 'a.ogg', '2026-07-30 04:00:00', '2026-07-30 04:00:00');
	`); err != nil {
		t.Fatal(err)
	}

	contacts, err := repository.Contacts(ctx, ContactQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertContactIDs(t, contacts, []string{"contact-a", "contact-b"})
	contacts, err = repository.Contacts(ctx, ContactQuery{
		Limit: 2,
		After: &ContactCursor{
			DisplayName: contacts[1].DisplayName,
			ID:          contacts[1].ID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContactIDs(t, contacts, []string{"contact-c"})

	messages, err := repository.Messages(ctx, MessageQuery{
		LineID: "line-main",
		Peer:   "+810000000001",
		Limit:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMessageIDs(t, messages, []int64{3, 2})
	messages, err = repository.Messages(ctx, MessageQuery{
		LineID: "line-main",
		Peer:   "+810000000001",
		Limit:  2,
		After: &MessageCursor{
			Timestamp: messages[1].SortTimestamp,
			ID:        messages[1].ID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMessageIDs(t, messages, []int64{1})

	threads, err := repository.MessageThreads(ctx, ThreadQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertThreadKeys(t, threads, []string{
		"line-a|+810000000011",
		"line-b|+810000000013",
	})
	threads, err = repository.MessageThreads(ctx, ThreadQuery{
		Limit: 2,
		After: &ThreadCursor{
			LastTimestamp: threads[1].SortTimestamp,
			LastMessageID: threads[1].LastMessageID,
			LineID:        threads[1].LineID,
			Peer:          threads[1].Peer,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertThreadKeys(t, threads, []string{"line-a|+810000000012"})

	calls, err := repository.Calls(ctx, CallQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertCallIDs(t, calls, []string{"call-c", "call-b"})
	calls, err = repository.Calls(ctx, CallQuery{
		Limit: 2,
		After: &CallCursor{EndedAt: calls[1].SortEndedAt, ID: calls[1].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCallIDs(t, calls, []string{"call-a"})

	recordings, err := repository.RecordingEntries(ctx, RecordingQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertRecordingIDs(t, recordings, []string{"recording-c", "recording-b"})
	recordings, err = repository.RecordingEntries(ctx, RecordingQuery{
		Limit: 2,
		After: &RecordingCursor{
			RecordedAt:   recordings[1].Segment.SortRecordedAt,
			CallID:       recordings[1].Segment.CallID,
			SegmentIndex: recordings[1].Segment.SegmentIndex,
			ID:           recordings[1].Segment.ID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRecordingIDs(t, recordings, []string{"recording-a"})
}

func assertContactIDs(t *testing.T, contacts []Contact, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(contacts))
	for _, contact := range contacts {
		actual = append(actual, contact.ID)
	}
	assertStringIDs(t, actual, expected)
}

func assertThreadKeys(t *testing.T, threads []MessageThread, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(threads))
	for _, thread := range threads {
		actual = append(actual, thread.Key)
	}
	assertStringIDs(t, actual, expected)
}

func assertCallIDs(t *testing.T, calls []Call, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(calls))
	for _, call := range calls {
		actual = append(actual, call.ID)
	}
	assertStringIDs(t, actual, expected)
}

func assertRecordingIDs(
	t *testing.T,
	recordings []RecordingEntry,
	expected []string,
) {
	t.Helper()
	actual := make([]string, 0, len(recordings))
	for _, recording := range recordings {
		actual = append(actual, recording.Segment.ID)
	}
	assertStringIDs(t, actual, expected)
}

func assertStringIDs(t *testing.T, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("IDs = %v, want %v", actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("IDs = %v, want %v", actual, expected)
		}
	}
}
