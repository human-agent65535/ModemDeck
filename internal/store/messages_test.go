package store

import (
	"context"
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
