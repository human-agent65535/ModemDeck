package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
)

func TestMessagesChronologicalReturnsLatestWindowOldestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := platformdb.Open(ctx, platformdb.Config{
		TargetPath: filepath.Join(t.TempDir(), "modemdeck.db"),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for index, timestamp := range []string{
		"2026-07-25T01:00:00Z",
		"2026-07-25T02:00:00Z",
		"2026-07-25T03:00:00Z",
		"2026-07-25T04:00:00Z",
	} {
		_, err := database.ExecContext(
			ctx,
			`INSERT INTO sms (
				line_id, iccid, peer, content, type, timestamp, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"line_test",
			"test-iccid",
			"+818012345678",
			"message",
			(index%2)+1,
			timestamp,
			timestamp,
		)
		if err != nil {
			t.Fatalf("insert message %d: %v", index+1, err)
		}
	}

	recent, err := repository.Messages(ctx, MessageQuery{
		LineID: "line_test",
		Peer:   "+818012345678",
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("query recent messages: %v", err)
	}
	assertMessageIDs(t, recent, []int64{4, 3, 2})

	chronological, err := repository.Messages(ctx, MessageQuery{
		LineID:        "line_test",
		Peer:          "+818012345678",
		Limit:         3,
		Chronological: true,
	})
	if err != nil {
		t.Fatalf("query chronological messages: %v", err)
	}
	assertMessageIDs(t, chronological, []int64{2, 3, 4})
	if chronological[0].Direction != "outgoing" ||
		chronological[1].Direction != "incoming" ||
		chronological[2].Direction != "outgoing" {
		t.Fatalf("directions = %q, %q, %q", chronological[0].Direction, chronological[1].Direction, chronological[2].Direction)
	}
}

func TestMessageThreadsAssociateContactsOnlyByUnambiguousCanonicalNumber(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO contacts (id, display_name) VALUES
			('contact-unique', 'Unique'),
			('contact-shared-a', 'Shared A'),
			('contact-shared-b', 'Shared B');
		 INSERT INTO contact_phones (
			id, contact_id, original_number, canonical_e164, region, is_primary
		 ) VALUES
			('phone-unique', 'contact-unique', '09011112222', '+819011112222', 'JP', 1),
			('phone-shared-a', 'contact-shared-a', '09033334444', '+819033334444', 'JP', 1),
			('phone-shared-b', 'contact-shared-b', '09033334444', '+819033334444', 'JP', 1);
		 INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content, last_type
		 ) VALUES
			('line-main', '', '', '09011112222', 1, '2026-07-29 01:00:00', 'raw', 1),
			('line-main', '', '', '+819011112222', 2, '2026-07-29 01:01:00', 'unique', 1),
			('line-main', '', '', '+819033334444', 3, '2026-07-29 01:02:00', 'ambiguous', 1);`,
	); err != nil {
		t.Fatalf("seed contact identity threads: %v", err)
	}

	threads, err := repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() error = %v", err)
	}
	byPeer := make(map[string]MessageThread, len(threads))
	for _, thread := range threads {
		byPeer[thread.Peer] = thread
	}
	if byPeer["09011112222"].ContactID != "" ||
		byPeer["09011112222"].ContactName != "" {
		t.Fatalf("raw local thread matched a contact: %+v", byPeer["09011112222"])
	}
	if byPeer["+819011112222"].ContactID != "contact-unique" ||
		byPeer["+819011112222"].ContactName != "Unique" {
		t.Fatalf("unique canonical thread = %+v, want contact-unique", byPeer["+819011112222"])
	}
	if byPeer["+819033334444"].ContactID != "" ||
		byPeer["+819033334444"].ContactName != "" {
		t.Fatalf(
			"ambiguous canonical thread matched a contact: %+v",
			byPeer["+819033334444"],
		)
	}
}

func TestMessageThreadBatchStateIsIndependentAndAtomic(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content, last_type,
			unread_count
		 ) VALUES
			('line-main', '', '', '+818011111111', 1, '2026-07-29 01:00:00', 'one', 1, 2),
			('line-main', '', '', '+818022222222', 2, '2026-07-29 01:01:00', 'two', 1, 0)`,
	); err != nil {
		t.Fatalf("seed message threads: %v", err)
	}
	one := MessageThreadIdentity{LineID: "line-main", Peer: "+818011111111"}
	two := MessageThreadIdentity{LineID: "line-main", Peer: "+818022222222"}

	if err := repository.UpdateMessageThreads(
		ctx,
		[]MessageThreadIdentity{one, two},
		MessageThreadFavorite,
	); err != nil {
		t.Fatalf("favorite threads: %v", err)
	}
	if err := repository.UpdateMessageThreads(
		ctx,
		[]MessageThreadIdentity{two},
		MessageThreadMarkUnread,
	); err != nil {
		t.Fatalf("mark thread unread: %v", err)
	}

	threads, err := repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() error = %v", err)
	}
	byPeer := make(map[string]MessageThread, len(threads))
	for _, thread := range threads {
		byPeer[thread.Peer] = thread
	}
	if !byPeer[one.Peer].Favorite || !byPeer[two.Peer].Favorite {
		t.Fatalf("favorite state = %+v", byPeer)
	}
	if byPeer[one.Peer].MarkedUnread || !byPeer[two.Peer].MarkedUnread {
		t.Fatalf("manual unread state = %+v", byPeer)
	}

	if err := repository.UpdateMessageThreads(
		ctx,
		[]MessageThreadIdentity{one, two},
		MessageThreadMarkRead,
	); err != nil {
		t.Fatalf("mark threads read: %v", err)
	}
	threads, err = repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() after read error = %v", err)
	}
	for _, thread := range threads {
		if thread.UnreadCount != 0 || thread.MarkedUnread {
			t.Fatalf("thread remains unread: %+v", thread)
		}
		if !thread.Favorite {
			t.Fatalf("read action changed favorite state: %+v", thread)
		}
	}

	if err := repository.UpdateMessageThreads(
		ctx,
		[]MessageThreadIdentity{one, two},
		MessageThreadUnfavorite,
	); err != nil {
		t.Fatalf("unfavorite threads: %v", err)
	}
	err = repository.UpdateMessageThreads(
		ctx,
		[]MessageThreadIdentity{
			one,
			{LineID: "line-main", Peer: "+818099999999"},
		},
		MessageThreadFavorite,
	)
	if !errors.Is(err, ErrMessageThreadNotFound) {
		t.Fatalf("atomic update error = %v, want ErrMessageThreadNotFound", err)
	}
	threads, err = repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() after rollback error = %v", err)
	}
	for _, thread := range threads {
		if thread.Favorite {
			t.Fatalf("failed batch partially changed favorite state: %+v", thread)
		}
	}
}

func assertMessageIDs(t *testing.T, messages []Message, expected []int64) {
	t.Helper()
	if len(messages) != len(expected) {
		t.Fatalf("message count = %d, want %d", len(messages), len(expected))
	}
	for index, message := range messages {
		if message.ID != expected[index] {
			t.Fatalf("message %d ID = %d, want %d", index, message.ID, expected[index])
		}
	}
}
