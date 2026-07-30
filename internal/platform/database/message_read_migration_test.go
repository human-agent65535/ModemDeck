package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMultiUserMigrationUsesMessageIngestionWatermark(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "message-read-watermark.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(singleUserSchemaFixture(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_admin_credentials (
			singleton, username, password_hash
		) VALUES (1, 'legacy-admin', 'legacy-hash');
		INSERT INTO modemdeck_lines (
			line_id, phone_number, line_label
		) VALUES ('line_watermark', '+819055500000', 'Watermark line');
		INSERT INTO sms (
			id, line_id, peer, content, type, timestamp, created_at
		) VALUES
			(100, 'line_watermark', '+819055501234', 'displayed latest', 1,
				'2026-07-30 10:00:00', '2026-07-30 10:00:00'),
			(101, 'line_watermark', '+819055501234', 'historical backfill', 1,
				'2026-07-30 09:00:00', '2026-07-30 11:00:00');
		INSERT INTO sms_contacts (
			line_id, imsi, peer, last_sms_id, last_timestamp,
			last_content, last_type, unread_count, created_at, updated_at
		) VALUES (
			'line_watermark', '', '+819055501234', 100,
			'2026-07-30 10:00:00', 'displayed latest', 1, 0,
			'2026-07-30 10:00:00', '2026-07-30 11:00:00'
		);
	`); err != nil {
		t.Fatal(err)
	}

	actual, err := readSchemaShape(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateMultiUserSchema(
		context.Background(),
		database,
		actual,
	)
	if err != nil {
		t.Fatalf("migrateMultiUserSchema() error = %v", err)
	}
	if !migrated {
		t.Fatal("migrateMultiUserSchema() did not migrate")
	}

	var readThrough int64
	if err := database.QueryRow(`
		SELECT last_read_sms_id
		FROM modemdeck_user_message_thread_state
		WHERE user_id = ? AND line_id = 'line_watermark'
			AND peer = '+819055501234'
	`, initialAdminUserID).Scan(&readThrough); err != nil {
		t.Fatal(err)
	}
	if readThrough != 101 {
		t.Fatalf("migrated read watermark = %d, want 101", readThrough)
	}
}
