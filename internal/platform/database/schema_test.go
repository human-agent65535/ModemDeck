package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesAndReopensCurrentSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "modemdeck.db")
	database, err := Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() create error = %v", err)
	}
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after create error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close created database: %v", err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() existing error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
}

func TestOpenAddsStableLineMessageIndexes(t *testing.T) {
	t.Parallel()

	const (
		linePeerTimestamp = "idx_sms_line_peer_timestamp"
		linePeerID        = "idx_sms_line_peer_id"
		unreadLinePeerID  = "idx_sms_incoming_unread_line_peer_id"
		threadLineTime    = "idx_sms_contacts_line_timestamp"
	)
	previousSchema := currentSchemaSQL
	for _, statement := range []string{
		"CREATE INDEX " + linePeerTimestamp + " ON sms(line_id, peer, timestamp DESC, id DESC);\n\n",
		"CREATE INDEX " + linePeerID + " ON sms(line_id, peer, id);\n\n",
		"CREATE INDEX " + unreadLinePeerID + " ON sms(line_id, peer, type, id);\n\n",
		"CREATE INDEX " + threadLineTime + " ON sms_contacts(line_id, last_timestamp DESC, last_sms_id DESC, peer);\n\n",
	} {
		previousSchema = strings.Replace(previousSchema, statement, "", 1)
	}

	path := filepath.Join(t.TempDir(), "before-message-line-indexes.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for _, index := range []string{
		linePeerTimestamp,
		linePeerID,
		unreadLinePeerID,
		threadLineTime,
	} {
		var definition string
		if err := database.QueryRow(
			`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
			index,
		).Scan(&definition); err != nil {
			t.Fatalf("read migrated index %s: %v", index, err)
		}
		if strings.TrimSpace(definition) == "" {
			t.Fatalf("migrated index %s has no definition", index)
		}
	}
}

func TestOpenAddsCursorPaginationIndexes(t *testing.T) {
	t.Parallel()

	indexes := []string{
		"idx_contacts_owner_cursor",
		"idx_contacts_cursor",
		"idx_sms_line_peer_cursor",
		"idx_sms_contacts_cursor",
		"idx_sms_contacts_line_cursor",
		"idx_call_history_ended_at_id",
		"idx_call_history_line_ended_at_id",
		"idx_modemdeck_call_recordings_cursor",
	}
	path := filepath.Join(t.TempDir(), "before-cursor-indexes.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(currentSchemaSQL); err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if _, err := database.Exec(`DROP INDEX ` + index); err != nil {
			t.Fatalf("drop %s: %v", index, err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for _, index := range indexes {
		var definition string
		if err := database.QueryRow(
			`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
			index,
		).Scan(&definition); err != nil {
			t.Fatalf("read migrated index %s: %v", index, err)
		}
		if strings.TrimSpace(definition) == "" {
			t.Fatalf("migrated index %s has no definition", index)
		}
	}
}

func TestCommunicationIndexesCoverHotQueries(t *testing.T) {
	t.Parallel()

	database, err := Open(context.Background(), Config{
		TargetPath: filepath.Join(t.TempDir(), "message-query-plans.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	testCases := []struct {
		name      string
		statement string
		arguments []any
		index     string
	}{
		{
			name: "message timeline",
			statement: `SELECT id FROM sms
				WHERE deleted_at IS NULL AND line_id = ? AND peer = ?
				ORDER BY COALESCE(timestamp, '') DESC, id DESC LIMIT ?`,
			arguments: []any{"line-a", "+819012345678", 50},
			index:     "idx_sms_line_peer_cursor",
		},
		{
			name: "read watermark",
			statement: `SELECT MAX(id) FROM sms
				WHERE line_id = ? AND peer = ?`,
			arguments: []any{"line-a", "+819012345678"},
			index:     "idx_sms_line_peer_id",
		},
		{
			name: "unread count",
			statement: `SELECT COUNT(*) FROM sms
				WHERE line_id = ? AND peer = ? AND type = 1
					AND deleted_at IS NULL AND id > ?`,
			arguments: []any{"line-a", "+819012345678", 100},
			index:     "idx_sms_incoming_unread_line_peer_id",
		},
		{
			name: "thread list",
			statement: `SELECT peer FROM sms_contacts
				WHERE line_id = ?
				ORDER BY COALESCE(last_timestamp, '') DESC,
					last_sms_id DESC, peer ASC LIMIT ?`,
			arguments: []any{"line-a", 50},
			index:     "idx_sms_contacts_line_cursor",
		},
		{
			name: "contact list",
			statement: `SELECT id FROM contacts
				WHERE owner_user_id = ?
				ORDER BY COALESCE(display_name, '') COLLATE NOCASE, id LIMIT ?`,
			arguments: []any{"user_admin", 50},
			index:     "idx_contacts_owner_cursor",
		},
		{
			name: "call list",
			statement: `SELECT id FROM call_history
				WHERE line_id = ?
				ORDER BY COALESCE(ended_at, '') DESC, id DESC LIMIT ?`,
			arguments: []any{"line-a", 50},
			index:     "idx_call_history_line_ended_at_id",
		},
		{
			name: "recording list",
			statement: `SELECT id FROM modemdeck_call_recordings
				ORDER BY COALESCE(started_at, created_at) DESC,
					call_id DESC, segment_index DESC, id DESC LIMIT ?`,
			arguments: []any{50},
			index:     "idx_modemdeck_call_recordings_cursor",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rows, err := database.Query(
				"EXPLAIN QUERY PLAN "+testCase.statement,
				testCase.arguments...,
			)
			if err != nil {
				t.Fatal(err)
			}
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					_ = rows.Close()
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(details, "\n"), testCase.index) {
				t.Fatalf(
					"query plan = %q, want index %s",
					details,
					testCase.index,
				)
			}
		})
	}
}

func TestOpenMigratesCurrentSchemaBeforeIOSPairing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "before-ios-pairing.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(schemaBeforeMobilePairingFixture(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled
		) VALUES
			('user_admin', 'owner', 'owner-hash', 'admin', 1),
			('user_member', 'member', 'member-hash', 'member', 1);
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after migration error = %v", err)
	}

	var adminPairing, memberPairing bool
	if err := database.QueryRow(`
		SELECT
			(SELECT ios_pairing_enabled FROM modemdeck_users
			 WHERE id = 'user_admin'),
			(SELECT ios_pairing_enabled FROM modemdeck_users
			 WHERE id = 'user_member')
	`).Scan(&adminPairing, &memberPairing); err != nil {
		t.Fatal(err)
	}
	if !adminPairing || memberPairing {
		t.Fatalf(
			"migrated pairing permissions = admin %t, member %t",
			adminPairing,
			memberPairing,
		)
	}

}

func TestOpenAddsIOSPairingConfirmationAndPreservesExistingPairing(
	t *testing.T,
) {
	t.Parallel()

	previousSchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tactivated_at DATETIME,",
		"",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"CREATE INDEX idx_modemdeck_ios_pairing_user\n"+
			"\tON modemdeck_ios_pairing_credentials(user_id, activated_at, created_at);\n\n",
		"",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"CREATE UNIQUE INDEX ux_modemdeck_ios_pairing_pending_user\n"+
			"\tON modemdeck_ios_pairing_credentials(user_id) WHERE activated_at IS NULL;\n\n",
		"",
		1,
	)
	if previousSchema == currentSchemaSQL {
		t.Fatal("previous schema fixture did not remove activated_at")
	}
	path := filepath.Join(t.TempDir(), "before-ios-pairing-confirmation.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, ios_pairing_enabled
		) VALUES (
			'user_admin', 'owner', 'owner-hash', 'admin', 1, 1
		);
		INSERT INTO modemdeck_ios_pairing_credentials (
			user_id, token_digest, created_at, updated_at
		) VALUES (
			'user_admin', randomblob(32),
			'2026-07-30 12:00:00', '2026-07-30 12:00:00'
		);
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after migration error = %v", err)
	}
	var createdAt, activatedAt string
	if err := database.QueryRow(`
		SELECT created_at, activated_at
		FROM modemdeck_ios_pairing_credentials
		WHERE user_id = 'user_admin'
	`).Scan(&createdAt, &activatedAt); err != nil {
		t.Fatal(err)
	}
	if activatedAt != createdAt {
		t.Fatalf(
			"activated_at = %q, want existing created_at %q",
			activatedAt,
			createdAt,
		)
	}
}

func TestOpenAddsIOSPairingDeviceMetadataAndPreservesExistingPairing(
	t *testing.T,
) {
	t.Parallel()

	previousSchema := currentSchemaSQL
	for _, definition := range []string{
		"\n\t\t\tdevice_name TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tdevice_model TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tdevice_model_identifier TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tos_name TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tos_version TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tapp_version TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tapp_build TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tpush_bundle_id TEXT NOT NULL DEFAULT '',",
		"\n\t\t\tlast_seen_at DATETIME,",
	} {
		updated := strings.Replace(previousSchema, definition, "", 1)
		if updated == previousSchema {
			t.Fatalf("previous schema fixture did not remove %q", definition)
		}
		previousSchema = updated
	}
	path := filepath.Join(t.TempDir(), "before-ios-pairing-device.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, ios_pairing_enabled
		) VALUES (
			'user_admin', 'owner', 'owner-hash', 'admin', 1, 1
		);
		INSERT INTO modemdeck_ios_pairing_credentials (
			user_id, token_digest, activated_at, created_at, updated_at
		) VALUES (
			'user_admin', randomblob(32),
			'2026-08-01 12:01:00', '2026-08-01 12:00:00',
			'2026-08-01 12:01:00'
		);
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after migration error = %v", err)
	}
	var activatedAt, deviceName, pushBundleID, lastSeenAt sql.NullString
	if err := database.QueryRow(`
		SELECT activated_at, device_name, push_bundle_id, last_seen_at
		FROM modemdeck_ios_pairing_credentials
		WHERE user_id = 'user_admin'
	`).Scan(&activatedAt, &deviceName, &pushBundleID, &lastSeenAt); err != nil {
		t.Fatal(err)
	}
	activated, err := time.Parse(time.RFC3339, activatedAt.String)
	if err != nil {
		t.Fatalf("migrated activated_at = %q: %v", activatedAt.String, err)
	}
	if !activated.Equal(time.Date(2026, 8, 1, 12, 1, 0, 0, time.UTC)) ||
		deviceName.String != "" ||
		pushBundleID.String != "" ||
		lastSeenAt.Valid {
		t.Fatalf(
			"migrated pairing = activated %q, device %q, bundle %q, last seen %q",
			activatedAt.String,
			deviceName.String,
			pushBundleID.String,
			lastSeenAt.String,
		)
	}
}

func TestOpenMigratesSingleDeviceIOSPairingToMultipleCredentials(
	t *testing.T,
) {
	t.Parallel()

	previousSchema := strings.Replace(
		currentSchemaSQL,
		"CREATE TABLE modemdeck_ios_pairing_credentials (\n"+
			"\t\t\tid TEXT PRIMARY KEY,\n"+
			"\t\t\tuser_id TEXT NOT NULL,",
		"CREATE TABLE modemdeck_ios_pairing_credentials (\n"+
			"\t\t\tuser_id TEXT PRIMARY KEY,",
		1,
	)
	for _, index := range []string{
		"CREATE INDEX idx_modemdeck_ios_pairing_user\n" +
			"\tON modemdeck_ios_pairing_credentials(user_id, activated_at, created_at);\n\n",
		"CREATE UNIQUE INDEX ux_modemdeck_ios_pairing_pending_user\n" +
			"\tON modemdeck_ios_pairing_credentials(user_id) WHERE activated_at IS NULL;\n\n",
	} {
		updated := strings.Replace(previousSchema, index, "", 1)
		if updated == previousSchema {
			t.Fatalf("single-device fixture did not remove %q", index)
		}
		previousSchema = updated
	}
	if previousSchema == currentSchemaSQL {
		t.Fatal("single-device fixture did not remove credential id")
	}

	path := filepath.Join(t.TempDir(), "single-ios-pairing-device.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, ios_pairing_enabled
		) VALUES (
			'user_admin', 'owner', 'owner-hash', 'admin', 1, 1
		);
		INSERT INTO modemdeck_ios_pairing_credentials (
			user_id, token_digest, activated_at,
			device_name, device_model, os_name, os_version,
			app_version, apns_token, voip_token, push_environment,
			push_bundle_id, created_at, updated_at
		) VALUES (
			'user_admin', randomblob(32), '2026-08-01 12:01:00',
			'Migrated iPhone', 'iPhone', 'iOS', '26.0',
			'0.1.0', 'legacy-apns', 'legacy-voip', 'production',
			'com.example.modemdeck',
			'2026-08-01 12:00:00', '2026-08-01 12:01:00'
		);
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after migration error = %v", err)
	}

	var credentialID, deviceName, apnsToken, bundleID string
	if err := database.QueryRow(`
		SELECT id, device_name, apns_token, push_bundle_id
		FROM modemdeck_ios_pairing_credentials
		WHERE user_id = 'user_admin'
	`).Scan(&credentialID, &deviceName, &apnsToken, &bundleID); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(credentialID, "ios-") ||
		deviceName != "Migrated iPhone" ||
		apnsToken != "legacy-apns" ||
		bundleID != "com.example.modemdeck" {
		t.Fatalf(
			"migrated credential = id %q, device %q, APNs %q, bundle %q",
			credentialID,
			deviceName,
			apnsToken,
			bundleID,
		)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_ios_pairing_credentials (
			id, user_id, token_digest, activated_at
		) VALUES (
			'ios-second-device', 'user_admin', randomblob(32), CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("insert second active credential after migration: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_ios_pairing_credentials (
			id, user_id, token_digest
		) VALUES ('ios-pending-one', 'user_admin', randomblob(32))
	`); err != nil {
		t.Fatalf("insert first pending credential after migration: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_ios_pairing_credentials (
			id, user_id, token_digest
		) VALUES ('ios-pending-two', 'user_admin', randomblob(32))
	`); err == nil {
		t.Fatal("inserted a second pending credential for one user")
	}
}

func TestOpenMigratesSingleUserDataToInitialAdministrator(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "single-user.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(singleUserSchemaFixture(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_admin_credentials (
			singleton, username, password_hash
		) VALUES (1, 'legacy-admin', 'legacy-hash');
		INSERT INTO modemdeck_auth_sessions (
			session_token_digest, csrf_token_digest,
			created_at_unix, expires_at_unix
		) VALUES (zeroblob(32), randomblob(32), 100, 200);
		INSERT INTO modemdeck_lines (
			line_id, phone_number, line_label
		) VALUES ('line_legacy', '+818012345678', 'Legacy line');
		UPDATE modemdeck_line_settings
		SET default_line_id = 'line_legacy', revision = 4
		WHERE singleton = 1;
		UPDATE modemdeck_system_settings
		SET language = 'en-US', revision = 3
		WHERE singleton = 1;
		UPDATE modemdeck_recording_settings
		SET default_enabled = 1, revision = 2
		WHERE singleton = 1;
		INSERT INTO contacts (
			id, display_name, preferred_line_id, is_favorite
		) VALUES ('contact-legacy', 'Legacy Contact', 'line_legacy', 1);
		INSERT INTO contact_phones (
			id, contact_id, original_number, canonical_e164, is_primary
		) VALUES (
			'phone-legacy', 'contact-legacy',
			'+81 80 1234 5678', '+818012345678', 1
		);
		INSERT INTO sms (
			id, line_id, peer, content, type, timestamp, created_at
		) VALUES
			(1001, 'line_legacy', '+818012345678', 'first', 1,
				'2026-07-28 05:00:00', '2026-07-28 05:00:00'),
			(1002, 'line_legacy', '+818012345678', 'second', 1,
				'2026-07-28 05:01:00', '2026-07-28 05:01:00');
		INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp,
			last_content, last_type, unread_count, marked_unread, is_favorite
		) VALUES (
			'line_legacy', '', '', '+818012345678', 1002,
			'2026-07-28 05:01:00', 'second', 1, 1, 1, 1
		);
		INSERT INTO call_history (
			id, line_id, direction, remote_number, phase,
			created_at, ended_at, read_at, is_favorite
		) VALUES (
			'call-legacy', 'line_legacy', 'incoming', '+818012345678',
			'ended', '2026-07-28 06:00:00', '2026-07-28 06:01:00',
			'2026-07-28 06:02:00', 1
		);
		INSERT INTO modemdeck_call_recording_state (
			call_id, is_favorite
		) VALUES ('call-legacy', 1);
		INSERT INTO modemdeck_telegram_units (
			id, display_name
		) VALUES ('telegram-legacy', 'Legacy bot');
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after migration error = %v", err)
	}

	var (
		username     string
		passwordHash string
		role         string
		enabled      bool
	)
	if err := database.QueryRow(
		`SELECT username, password_hash, role, enabled
		 FROM modemdeck_users WHERE id = ?`,
		initialAdminUserID,
	).Scan(
		&username,
		&passwordHash,
		&role,
		&enabled,
	); err != nil {
		t.Fatal(err)
	}
	if username != "legacy-admin" || passwordHash != "legacy-hash" ||
		role != "admin" || !enabled {
		t.Fatalf(
			"migrated administrator = %q hash %q role %q enabled %t",
			username,
			passwordHash,
			role,
			enabled,
		)
	}

	var sessionCount int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM modemdeck_auth_sessions`,
	).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Fatalf("migrated sessions = %d, want 0", sessionCount)
	}

	var (
		contactOwner string
		defaultLine  string
		assignedLine string
		language     string
		recording    bool
	)
	if err := database.QueryRow(
		`SELECT
			(SELECT owner_user_id FROM contacts WHERE id = 'contact-legacy'),
			(SELECT default_line_id FROM modemdeck_user_preferences
			 WHERE user_id = ?),
			(SELECT line_id FROM modemdeck_user_lines
			 WHERE user_id = ? AND line_id = 'line_legacy'),
			(SELECT language FROM modemdeck_user_preferences
			 WHERE user_id = ?),
			(SELECT recording_default_enabled
			 FROM modemdeck_user_preferences WHERE user_id = ?)`,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
	).Scan(
		&contactOwner,
		&defaultLine,
		&assignedLine,
		&language,
		&recording,
	); err != nil {
		t.Fatal(err)
	}
	if contactOwner != initialAdminUserID ||
		defaultLine != "line_legacy" ||
		assignedLine != "line_legacy" ||
		language != "en-US" ||
		!recording {
		t.Fatalf(
			"migrated personal data = owner %q default %q assigned %q language %q recording %t",
			contactOwner,
			defaultLine,
			assignedLine,
			language,
			recording,
		)
	}

	var (
		lastReadSMSID   int64
		markedUnread    bool
		messageFavorite bool
		callRead        bool
		callFavorite    bool
		recordFavorite  bool
	)
	if err := database.QueryRow(
		`SELECT
			(SELECT last_read_sms_id
			 FROM modemdeck_user_message_thread_state
			 WHERE user_id = ? AND line_id = 'line_legacy'
			   AND peer = '+818012345678'),
			(SELECT marked_unread
			 FROM modemdeck_user_message_thread_state
			 WHERE user_id = ? AND line_id = 'line_legacy'
			   AND peer = '+818012345678'),
			(SELECT is_favorite
			 FROM modemdeck_user_message_thread_state
			 WHERE user_id = ? AND line_id = 'line_legacy'
			   AND peer = '+818012345678'),
			(SELECT is_read FROM modemdeck_user_call_state
			 WHERE user_id = ? AND call_id = 'call-legacy'),
			(SELECT is_favorite FROM modemdeck_user_call_state
			 WHERE user_id = ? AND call_id = 'call-legacy'),
			(SELECT is_favorite FROM modemdeck_user_recording_state
			 WHERE user_id = ? AND call_id = 'call-legacy')`,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
	).Scan(
		&lastReadSMSID,
		&markedUnread,
		&messageFavorite,
		&callRead,
		&callFavorite,
		&recordFavorite,
	); err != nil {
		t.Fatal(err)
	}
	if lastReadSMSID != 1001 || !markedUnread || !messageFavorite ||
		!callRead || !callFavorite || !recordFavorite {
		t.Fatalf(
			"migrated communication state = read SMS %d marked %t favorites %t/%t/%t call read %t",
			lastReadSMSID,
			markedUnread,
			messageFavorite,
			callFavorite,
			recordFavorite,
			callRead,
		)
	}

	var (
		scopeSource    string
		assignedUserID string
		manualAllLines bool
	)
	if err := database.QueryRow(
		`SELECT scope_source, assigned_user_id, manual_all_lines
		 FROM modemdeck_telegram_units
		 WHERE id = 'telegram-legacy'`,
	).Scan(&scopeSource, &assignedUserID, &manualAllLines); err != nil {
		t.Fatal(err)
	}
	if scopeSource != "user" || assignedUserID != "user_admin" || manualAllLines {
		t.Fatalf(
			"migrated Telegram scope = %q user %q all lines %t",
			scopeSource,
			assignedUserID,
			manualAllLines,
		)
	}
}

func TestMigratePersistentAuthSessionsKeepsNewestValidEight(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "auth-session-migration.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE modemdeck_users (
			id TEXT PRIMARY KEY
		);
		INSERT INTO modemdeck_users (id) VALUES ('user-1');
		CREATE TABLE modemdeck_auth_sessions (
			session_token_digest BLOB PRIMARY KEY CHECK (length(session_token_digest) = 32),
			csrf_token_digest BLOB NOT NULL CHECK (length(csrf_token_digest) = 32),
			user_id TEXT NOT NULL,
			created_at_unix INTEGER NOT NULL,
			last_seen_at_unix INTEGER,
			user_agent TEXT NOT NULL DEFAULT '',
			access_ip TEXT NOT NULL DEFAULT '',
			access_host TEXT NOT NULL DEFAULT '',
			expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix >= created_at_unix),
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id) ON DELETE CASCADE ON UPDATE CASCADE
		);
		CREATE INDEX idx_modemdeck_auth_sessions_expiry
			ON modemdeck_auth_sessions(expires_at_unix);
	`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Unix()
	for index := 1; index <= 10; index++ {
		digest := make([]byte, 32)
		digest[0] = byte(index)
		csrf := make([]byte, 32)
		csrf[0] = byte(index + 20)
		if _, err := database.Exec(
			`INSERT INTO modemdeck_auth_sessions (
				session_token_digest, csrf_token_digest, user_id,
				created_at_unix, expires_at_unix
			 ) VALUES (?, ?, 'user-1', ?, ?)`,
			digest,
			csrf,
			now+int64(index),
			now+3600,
		); err != nil {
			t.Fatalf("insert session %d: %v", index, err)
		}
	}
	metadataDigest := make([]byte, 32)
	metadataDigest[0] = 10
	if _, err := database.Exec(
		`UPDATE modemdeck_auth_sessions
		 SET last_seen_at_unix = ?, user_agent = ?, access_ip = ?, access_host = ?
		 WHERE session_token_digest = ?`,
		now+600,
		"Migrated Browser",
		"198.51.100.44",
		"legacy.example.test",
		metadataDigest,
	); err != nil {
		t.Fatal(err)
	}
	expiredDigest := make([]byte, 32)
	expiredDigest[0] = 99
	if _, err := database.Exec(
		`INSERT INTO modemdeck_auth_sessions (
			session_token_digest, csrf_token_digest, user_id,
			created_at_unix, expires_at_unix
		 ) VALUES (?, randomblob(32), 'user-1', ?, ?)`,
		expiredDigest,
		now-7200,
		now-3600,
	); err != nil {
		t.Fatal(err)
	}
	actual, err := readSchemaShape(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := migratePersistentAuthSessions(
		context.Background(),
		database,
		actual,
	)
	if err != nil || !migrated {
		t.Fatalf("migratePersistentAuthSessions() = %v, %v", migrated, err)
	}

	actual, err = readSchemaShape(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	columns := actual.tables["modemdeck_auth_sessions"]
	if _, exists := columns["expires_at_unix"]; exists {
		t.Fatal("migrated session table still has expires_at_unix")
	}
	for _, column := range []string{"last_seen_at_unix", "user_agent", "access_ip", "access_host"} {
		if _, exists := columns[column]; !exists {
			t.Fatalf("migrated session table is missing %s", column)
		}
	}
	var count, oldCount, populatedMetadata int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM modemdeck_auth_sessions",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM modemdeck_auth_sessions
		 WHERE created_at_unix <= ?`,
		now+2,
	).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM modemdeck_auth_sessions
		 WHERE user_agent <> '' OR access_ip <> '' OR access_host <> ''`,
	).Scan(&populatedMetadata); err != nil {
		t.Fatal(err)
	}
	if count != 8 || oldCount != 0 || populatedMetadata != 1 {
		t.Fatalf(
			"migrated sessions = count %d, old %d, metadata %d",
			count,
			oldCount,
			populatedMetadata,
		)
	}
}

func TestOpenAddsCurrentAuthSessionMetadata(t *testing.T) {
	t.Parallel()

	previousSchema := currentSchemaSQL
	for _, definition := range []string{
		"\n\t\t\tlast_seen_at_unix INTEGER NOT NULL,",
		"\n\t\t\taccess_ip TEXT NOT NULL DEFAULT '',",
	} {
		updated := strings.Replace(previousSchema, definition, "", 1)
		if updated == previousSchema {
			t.Fatalf("previous schema fixture did not remove %q", definition)
		}
		previousSchema = updated
	}
	path := filepath.Join(t.TempDir(), "before-auth-session-metadata.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, ios_pairing_enabled
		) VALUES ('user_admin', 'owner', 'owner-hash', 'admin', 1, 1);
		INSERT INTO modemdeck_auth_sessions (
			session_token_digest, csrf_token_digest, user_id,
			created_at_unix, user_agent, access_host
		) VALUES (zeroblob(32), randomblob(32), 'user_admin', 123,
			'Legacy Browser', 'legacy.example.test');
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ValidateSchema(context.Background(), database); err != nil {
		t.Fatalf("ValidateSchema() after migration error = %v", err)
	}
	var lastSeenAt int64
	var userAgent, accessIP, accessHost string
	if err := database.QueryRow(`
		SELECT last_seen_at_unix, user_agent, access_ip, access_host
		FROM modemdeck_auth_sessions
	`).Scan(&lastSeenAt, &userAgent, &accessIP, &accessHost); err != nil {
		t.Fatal(err)
	}
	if lastSeenAt != 123 || userAgent != "Legacy Browser" ||
		accessIP != "" || accessHost != "legacy.example.test" {
		t.Fatalf(
			"migrated auth metadata = seen %d, UA %q, IP %q, host %q",
			lastSeenAt,
			userAgent,
			accessIP,
			accessHost,
		)
	}
}

func TestOpenDropsDeprecatedPasswordChangeColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "password-policy.db")
	database, err := Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := database.Exec(`
		ALTER TABLE modemdeck_users
			ADD COLUMN must_change_password NUMERIC NOT NULL DEFAULT 0;
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, must_change_password
		) VALUES (
			'user_member_password_policy',
			'member-password-policy',
			'password-hash',
			'member',
			1,
			1
		)
	`); err != nil {
		t.Fatalf("insert legacy password requirement: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var (
		username     string
		passwordHash string
		enabled      bool
	)
	if err := database.QueryRow(`
		SELECT username, password_hash, enabled
		FROM modemdeck_users
		WHERE id = 'user_member_password_policy'`,
	).Scan(&username, &passwordHash, &enabled); err != nil {
		t.Fatalf("read migrated password requirement: %v", err)
	}
	if username != "member-password-policy" || passwordHash != "password-hash" || !enabled {
		t.Fatalf(
			"migrated member = %q hash %q enabled %t",
			username,
			passwordHash,
			enabled,
		)
	}
	columns, err := tableColumns(context.Background(), database, "modemdeck_users")
	if err != nil {
		t.Fatalf("read migrated user columns: %v", err)
	}
	if _, exists := columns["must_change_password"]; exists {
		t.Fatal("deprecated password change column still exists")
	}
}

func TestOpenMigratesDeviceAliasToNameAndPreservesValue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "device-name.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tname TEXT NOT NULL DEFAULT '',",
		"\n\t\t\talias TEXT NOT NULL DEFAULT '',",
		1,
	)
	if legacySchema == currentSchemaSQL {
		t.Fatal("legacy schema fixture did not replace the device name column")
	}
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO devices (imei, alias) VALUES ('860000000000001', '机房模组')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`CREATE INDEX idx_devices_model_legacy_extra ON devices(model)`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var name string
	if err := database.QueryRow(
		`SELECT name FROM devices WHERE imei = '860000000000001'`,
	).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "机房模组" {
		t.Fatalf("migrated device name = %q, want 机房模组", name)
	}
	var aliasColumns int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name = 'alias'`,
	).Scan(&aliasColumns); err != nil {
		t.Fatal(err)
	}
	if aliasColumns != 0 {
		t.Fatalf("legacy alias columns = %d, want 0", aliasColumns)
	}
	var extraIndexes int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master
		 WHERE type = 'index' AND name = 'idx_devices_model_legacy_extra'`,
	).Scan(&extraIndexes); err != nil {
		t.Fatal(err)
	}
	if extraIndexes != 1 {
		t.Fatalf("legacy extra indexes = %d, want preserved", extraIndexes)
	}
}

func TestOpenMigratesMissedCallReadState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-call-read-at.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousSchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tread_at DATETIME,",
		"",
		1,
	)
	if previousSchema == currentSchemaSQL {
		t.Fatal("previous schema fixture did not remove call_history.read_at")
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO call_history (
			id, direction, remote_number, phase, created_at, ended_at
		 ) VALUES (
			'call-unread', 'incoming', '+818012345678', 'ended',
			'2026-07-28 05:00:00', '2026-07-28 05:00:10'
		 )`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var readAt sql.NullString
	if err := database.QueryRow(
		`SELECT read_at FROM call_history WHERE id = 'call-unread'`,
	).Scan(&readAt); err != nil {
		t.Fatal(err)
	}
	if readAt.Valid {
		t.Fatalf("migrated read_at = %q, want NULL", readAt.String)
	}
}

func TestOpenMigratesSMSDeletionState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-sms-deleted-at.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousSchema := strings.Replace(
		currentSchemaSQL,
		",\n\t\t\t\tdeleted_at DATETIME",
		"",
		1,
	)
	if previousSchema == currentSchemaSQL {
		t.Fatal("previous schema fixture did not remove sms.deleted_at")
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO sms (
			line_id, peer, content, type, timestamp, created_at
		 ) VALUES (
			'line-main', '+818012345678', 'preserved', 1,
			'2026-07-29 05:00:00', '2026-07-29 05:00:00'
		 )`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var deletedAt sql.NullString
	if err := database.QueryRow(
		`SELECT deleted_at FROM sms WHERE content = 'preserved'`,
	).Scan(&deletedAt); err != nil {
		t.Fatal(err)
	}
	if deletedAt.Valid {
		t.Fatalf("migrated deleted_at = %q, want NULL", deletedAt.String)
	}
}

func TestOpenMigratesMessageThreadStateAndPreservesThreads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-message-thread-state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousSchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\t\tmarked_unread NUMERIC NOT NULL DEFAULT 0,",
		"",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"\n\t\t\t\tis_favorite NUMERIC NOT NULL DEFAULT 0,",
		"",
		1,
	)
	if previousSchema == currentSchemaSQL {
		t.Fatal("previous schema fixture did not remove message thread state")
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count
		 ) VALUES (
			'line-main', '', '', '+818012345678', 7, '2026-07-29 05:00:00',
			'preserved', 1, 3
		 )`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var (
		content      string
		unreadCount  int
		markedUnread bool
		favorite     bool
	)
	if err := database.QueryRow(
		`SELECT last_content, unread_count, marked_unread, is_favorite
		 FROM sms_contacts
		 WHERE line_id = 'line-main' AND peer = '+818012345678'`,
	).Scan(&content, &unreadCount, &markedUnread, &favorite); err != nil {
		t.Fatal(err)
	}
	if content != "preserved" || unreadCount != 3 || markedUnread || favorite {
		t.Fatalf(
			"migrated thread = content %q unread %d marked %t favorite %t",
			content,
			unreadCount,
			markedUnread,
			favorite,
		)
	}
}

func TestOpenMigratesCallAndRecordingFavoriteState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-call-recording-favorites.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousSchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tread_at DATETIME,\n\t\t\tis_favorite NUMERIC NOT NULL DEFAULT 0,",
		"\n\t\t\tread_at DATETIME,",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"\n\t\t\t\tlast_error_code TEXT NOT NULL DEFAULT '',\n\t\t\t\tis_favorite NUMERIC NOT NULL DEFAULT 0,",
		"\n\t\t\t\tlast_error_code TEXT NOT NULL DEFAULT '',",
		1,
	)
	if previousSchema == currentSchemaSQL {
		t.Fatal("previous schema fixture did not remove communication favorite state")
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO call_history (
			id, direction, remote_number, phase, created_at, ended_at
		 ) VALUES (
			'call-favorite-migration', 'incoming', '+818012345678', 'ended',
			'2026-07-29 05:00:00', '2026-07-29 05:01:00'
		 );
		 INSERT INTO modemdeck_call_recording_state (call_id)
		 VALUES ('call-favorite-migration');
		 INSERT INTO modemdeck_call_recordings (
			id, call_id, segment_index, status, relative_path
		 ) VALUES (
			'recording-favorite-migration', 'call-favorite-migration', 1,
			'ready', 'call-favorite-migration/recording-favorite-migration.ogg'
		 )`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var callFavorite, recordingFavorite bool
	if err := database.QueryRow(
		`SELECT call_history.is_favorite, state.is_favorite
		 FROM call_history
		 JOIN modemdeck_call_recording_state state ON state.call_id = call_history.id
		 WHERE call_history.id = 'call-favorite-migration'`,
	).Scan(&callFavorite, &recordingFavorite); err != nil {
		t.Fatal(err)
	}
	if callFavorite || recordingFavorite {
		t.Fatalf(
			"migrated favorites = call %t recording %t, want false",
			callFavorite,
			recordingFavorite,
		)
	}
}

func TestOpenMigratesCommunicationStateFromPreviousRelease(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "previous-release.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousSchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\t\tmarked_unread NUMERIC NOT NULL DEFAULT 0,",
		"",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"\n\t\t\t\tis_favorite NUMERIC NOT NULL DEFAULT 0,",
		"",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"\n\t\t\tread_at DATETIME,\n\t\t\tis_favorite NUMERIC NOT NULL DEFAULT 0,",
		"\n\t\t\tread_at DATETIME,",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		"\n\t\t\t\tlast_error_code TEXT NOT NULL DEFAULT '',\n\t\t\t\tis_favorite NUMERIC NOT NULL DEFAULT 0,",
		"\n\t\t\t\tlast_error_code TEXT NOT NULL DEFAULT '',",
		1,
	)
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count
		 ) VALUES (
			'line-main', '', '', '+818012345678', 7, '2026-07-29 05:00:00',
			'preserved', 1, 3
		 );
		 INSERT INTO call_history (
			id, direction, remote_number, phase, created_at, ended_at
		 ) VALUES (
			'call-previous-release', 'incoming', '+818012345678', 'ended',
			'2026-07-29 05:00:00', '2026-07-29 05:01:00'
		 );
		 INSERT INTO modemdeck_call_recording_state (call_id)
		 VALUES ('call-previous-release')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var (
		content           string
		unreadCount       int
		markedUnread      bool
		messageFavorite   bool
		callFavorite      bool
		recordingFavorite bool
	)
	if err := database.QueryRow(
		`SELECT thread.last_content, thread.unread_count,
			thread.marked_unread, thread.is_favorite,
			call.is_favorite, recording.is_favorite
		 FROM sms_contacts thread
		 JOIN call_history call ON call.id = 'call-previous-release'
		 JOIN modemdeck_call_recording_state recording
		   ON recording.call_id = call.id
		 WHERE thread.line_id = 'line-main'
		   AND thread.peer = '+818012345678'`,
	).Scan(
		&content,
		&unreadCount,
		&markedUnread,
		&messageFavorite,
		&callFavorite,
		&recordingFavorite,
	); err != nil {
		t.Fatal(err)
	}
	if content != "preserved" || unreadCount != 3 ||
		markedUnread || messageFavorite || callFavorite || recordingFavorite {
		t.Fatalf(
			"migrated state = content %q unread %d marked %t favorites %t/%t/%t",
			content,
			unreadCount,
			markedUnread,
			messageFavorite,
			callFavorite,
			recordingFavorite,
		)
	}
}

func TestOpenMigratesSMSDeliveryStateAndMessagePolicy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "without-sms-delivery.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousSchema := strings.Replace(
		currentSchemaSQL,
		`				delivery_status TEXT NOT NULL DEFAULT ''
					CHECK (delivery_status IN ('', 'submitted', 'delivered', 'failed')),
				message_reference INTEGER,
				delivery_report_requested NUMERIC NOT NULL DEFAULT 0,
				delivery_report_trackable NUMERIC NOT NULL DEFAULT 0,
				delivery_report_code INTEGER,
`,
		"",
		1,
	)
	previousSchema = strings.Replace(
		previousSchema,
		`			delivery_reports_enabled NUMERIC NOT NULL DEFAULT 0,
			delivery_reports_support TEXT NOT NULL DEFAULT 'unknown'
				CHECK (delivery_reports_support IN ('unknown', 'unsupported')),
			message_policy_revision INTEGER NOT NULL DEFAULT 1
				CHECK (message_policy_revision > 0),
`,
		"",
		1,
	)
	if previousSchema == currentSchemaSQL {
		t.Fatal("previous schema fixture did not remove SMS delivery columns")
	}
	if _, err := database.Exec(previousSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO modemdeck_lines (line_id) VALUES ('line_delivery');
		 INSERT INTO sms (
			line_id, peer, content, type, timestamp, created_at
		 ) VALUES (
			'line_delivery', '+818012345678', 'preserved outgoing', 2,
			'2026-07-29T05:00:00Z', '2026-07-29T05:00:00Z'
		 )`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var (
		deliveryStatus   string
		messageReference sql.NullInt64
		reportRequested  bool
		reportTrackable  bool
		reportCode       sql.NullInt64
	)
	if err := database.QueryRow(
		`SELECT delivery_status, message_reference, delivery_report_requested,
			delivery_report_trackable, delivery_report_code
		 FROM sms WHERE content = 'preserved outgoing'`,
	).Scan(
		&deliveryStatus,
		&messageReference,
		&reportRequested,
		&reportTrackable,
		&reportCode,
	); err != nil {
		t.Fatal(err)
	}
	if deliveryStatus != "submitted" ||
		messageReference.Valid ||
		reportRequested ||
		reportTrackable ||
		reportCode.Valid {
		t.Fatalf(
			"migrated message = status %q reference %+v requested %t trackable %t code %+v",
			deliveryStatus,
			messageReference,
			reportRequested,
			reportTrackable,
			reportCode,
		)
	}
	var (
		enabled  bool
		support  string
		revision int64
	)
	if err := database.QueryRow(
		`SELECT delivery_reports_enabled, delivery_reports_support,
			message_policy_revision
		 FROM modemdeck_lines WHERE line_id = 'line_delivery'`,
	).Scan(&enabled, &support, &revision); err != nil {
		t.Fatal(err)
	}
	if enabled || support != "unknown" || revision != 1 {
		t.Fatalf(
			"migrated message policy = enabled %t support %q revision %d",
			enabled,
			support,
			revision,
		)
	}
}

func TestOpenRejectsOutdatedSchemaWithoutAlteringIt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "outdated.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE contacts (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(context.Background(), Config{TargetPath: path})
	if !errors.Is(err, ErrSchemaOutdated) {
		t.Fatalf("Open() error = %v, want ErrSchemaOutdated", err)
	}

	database, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var addedColumns int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('contacts')
		 WHERE name <> 'id'`,
	).Scan(&addedColumns); err != nil {
		t.Fatal(err)
	}
	if addedColumns != 0 {
		t.Fatalf("outdated schema was altered: added columns = %d", addedColumns)
	}
}

func TestOpenMigratesAdministratorUsernameAndPreservesPassword(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-admin-username.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	v1Schema := legacyV1SchemaFixture(t)
	legacySchema := strings.Replace(
		v1Schema,
		"\n\t\t\tusername TEXT NOT NULL DEFAULT 'admin',",
		"",
		1,
	)
	if legacySchema == v1Schema {
		t.Fatal("legacy schema fixture did not remove administrator username")
	}
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO modemdeck_admin_credentials (singleton, password_hash)
		 VALUES (1, 'legacy-hash')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var username, passwordHash string
	if err := database.QueryRow(
		`SELECT username, password_hash
		 FROM modemdeck_admin_credentials WHERE singleton = 1`,
	).Scan(&username, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if username != "admin" || passwordHash != "legacy-hash" {
		t.Fatalf("migrated credentials = (%q, %q)", username, passwordHash)
	}
}

func TestOpenMigratesContactAvatarColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-avatar.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	v1Schema := legacyV1SchemaFixture(t)
	legacySchema := strings.Replace(
		v1Schema,
		"\n\t\t\tavatar TEXT NOT NULL DEFAULT '',",
		"",
		1,
	)
	if legacySchema == v1Schema {
		t.Fatal("legacy schema fixture did not remove contacts.avatar")
	}
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO contacts (id, display_name) VALUES ('contact-1', 'Aiko')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var avatar string
	if err := database.QueryRow(
		`SELECT avatar FROM contacts WHERE id = 'contact-1'`,
	).Scan(&avatar); err != nil {
		t.Fatal(err)
	}
	if avatar != "" {
		t.Fatalf("migrated avatar = %q, want empty", avatar)
	}
}

func TestOpenMigratesSystemSettingsAndPreservesExistingData(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-system-settings.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	v1Schema := legacyV1SchemaFixture(t)
	legacySchema := strings.Replace(
		v1Schema,
		`CREATE TABLE modemdeck_system_settings (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			language TEXT NOT NULL DEFAULT 'auto'
				CHECK (language IN (
					'auto', 'zh-CN', 'zh-TW', 'en-US', 'ja-JP',
					'vi-VN', 'es-ES', 'de-DE', 'fr-FR', 'pt-BR'
				)),
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

`,
		"",
		1,
	)
	legacySchema = strings.Replace(
		legacySchema,
		`INSERT INTO modemdeck_system_settings (
	singleton, language, revision, updated_at
) VALUES (1, 'auto', 1, CURRENT_TIMESTAMP);

`,
		"",
		1,
	)
	legacySchema = strings.Replace(
		legacySchema,
		"\n\t\t\tavatar TEXT NOT NULL DEFAULT '',",
		"",
		1,
	)
	if legacySchema == v1Schema {
		t.Fatal("legacy schema fixture did not remove system settings")
	}
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO contacts (id, display_name) VALUES ('contact-1', 'Aiko')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var (
		language string
		revision int64
		name     string
		avatar   string
	)
	if err := database.QueryRow(
		`SELECT language, revision FROM modemdeck_system_settings WHERE singleton = 1`,
	).Scan(&language, &revision); err != nil {
		t.Fatal(err)
	}
	if language != "auto" || revision != 1 {
		t.Fatalf("migrated settings = %q revision %d", language, revision)
	}
	if err := database.QueryRow(
		`SELECT display_name, avatar FROM contacts WHERE id = 'contact-1'`,
	).Scan(&name, &avatar); err != nil {
		t.Fatal(err)
	}
	if name != "Aiko" {
		t.Fatalf("preserved contact name = %q, want Aiko", name)
	}
	if avatar != "" {
		t.Fatalf("migrated contact avatar = %q, want empty", avatar)
	}
}

func TestInitializeSchemaExpandsExistingSystemLanguageConstraint(t *testing.T) {
	t.Parallel()

	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "languages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	legacySchema := strings.Replace(
		currentSchemaSQL,
		`CHECK (language IN (
					'auto', 'zh-CN', 'zh-TW', 'en-US', 'ja-JP',
					'vi-VN', 'es-ES', 'de-DE', 'fr-FR', 'pt-BR'
				))`,
		`CHECK (language IN ('auto', 'zh-CN', 'en-US', 'ja-JP', 'vi-VN'))`,
		1,
	)
	if legacySchema == currentSchemaSQL {
		t.Fatal("legacy schema fixture did not narrow supported languages")
	}
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE modemdeck_system_settings
		 SET language = 'en-US', revision = 7`,
	); err != nil {
		t.Fatal(err)
	}

	created, err := InitializeSchema(context.Background(), database)
	if err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}
	if created {
		t.Fatal("InitializeSchema() created an existing database")
	}
	var (
		language string
		revision int64
	)
	if err := database.QueryRow(
		`SELECT language, revision
		 FROM modemdeck_system_settings
		 WHERE singleton = 1`,
	).Scan(&language, &revision); err != nil {
		t.Fatal(err)
	}
	if language != "en-US" || revision != 7 {
		t.Fatalf("migrated settings = %q revision %d", language, revision)
	}
	for _, supported := range []string{
		"zh-TW",
		"es-ES",
		"de-DE",
		"fr-FR",
		"pt-BR",
	} {
		if _, err := database.Exec(
			`UPDATE modemdeck_system_settings SET language = ?`,
			supported,
		); err != nil {
			t.Fatalf("save migrated language %q: %v", supported, err)
		}
	}
}

func TestOpenMigratesSIMLineColorAndPreservesExistingData(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-line-color.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	v1Schema := legacyV1SchemaFixture(t)
	legacySchema := strings.Replace(
		v1Schema,
		"\n\t\t\tline_color TEXT NOT NULL DEFAULT ''\n\t\t\t\tCHECK (line_color IN (\n\t\t\t\t\t'', 'teal', 'blue', 'indigo', 'violet',\n\t\t\t\t\t'green', 'amber', 'orange', 'red'\n\t\t\t\t)),",
		"",
		1,
	)
	if legacySchema == v1Schema {
		t.Fatal("legacy schema fixture did not remove sim_cards.line_color")
	}
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO sim_cards (iccid, line_label) VALUES ('iccid-1', '主卡')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var lineID, label, color string
	if err := database.QueryRow(
		`SELECT sim_cards.line_id, lines.line_label, lines.line_color
		 FROM sim_cards
		 JOIN modemdeck_lines lines ON lines.line_id = sim_cards.line_id
		 WHERE sim_cards.iccid = 'iccid-1'`,
	).Scan(&lineID, &label, &color); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lineID, "line_") {
		t.Fatalf("migrated stable line ID = %q", lineID)
	}
	if label != "主卡" || color != "" {
		t.Fatalf("migrated line identity = (%q, %q), want (主卡, empty)", label, color)
	}
}

func TestOpenMigratesMergedLineWithOnlyLatestSIMAttachment(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "merged-line-attachments.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(legacyV1SchemaFixture(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO devices (
			imei, iccid, sim_inserted, last_seen, created_at, updated_at
		 ) VALUES
			('imei-old', 'iccid-old', 1, '2026-07-20 10:00:00',
				'2026-07-20 10:00:00', '2026-07-20 10:00:00'),
			('imei-new', 'iccid-new', 1, '2026-07-21 10:00:00',
				'2026-07-21 10:00:00', '2026-07-21 10:00:00');
		 INSERT INTO sim_cards (
			iccid, imsi, current_imei, last_seen, created_at, updated_at
		 ) VALUES
			('iccid-old', 'imsi-old', 'imei-old', '2026-07-20 10:00:00',
				'2026-07-20 10:00:00', '2026-07-20 10:00:00'),
			('iccid-new', 'imsi-new', 'imei-new', '2026-07-21 10:00:00',
				'2026-07-21 10:00:00', '2026-07-21 10:00:00');
		 INSERT INTO sim_subscriptions (
			imsi, current_iccid, phone_number, last_seen, created_at, updated_at
		 ) VALUES
			('imsi-old', 'iccid-old', '+819012345678', '2026-07-20 10:00:00',
				'2026-07-20 10:00:00', '2026-07-20 10:00:00'),
			('imsi-new', 'iccid-new', '+81 90-1234-5678', '2026-07-21 10:00:00',
				'2026-07-21 10:00:00', '2026-07-21 10:00:00');`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	type simAttachment struct {
		lineID      string
		currentIMEI string
	}
	attachments := make(map[string]simAttachment)
	rows, err := database.Query(
		`SELECT iccid, line_id, COALESCE(current_imei, '')
		 FROM sim_cards WHERE iccid IN ('iccid-old', 'iccid-new')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var iccid string
		var attachment simAttachment
		if err := rows.Scan(&iccid, &attachment.lineID, &attachment.currentIMEI); err != nil {
			t.Fatal(err)
		}
		attachments[iccid] = attachment
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	oldAttachment := attachments["iccid-old"]
	newAttachment := attachments["iccid-new"]
	if oldAttachment.lineID == "" || oldAttachment.lineID != newAttachment.lineID {
		t.Fatalf("migrated stable line IDs = old %q, new %q", oldAttachment.lineID, newAttachment.lineID)
	}
	if oldAttachment.currentIMEI != "" || newAttachment.currentIMEI != "imei-new" {
		t.Fatalf(
			"migrated SIM attachments = old %q, new %q",
			oldAttachment.currentIMEI,
			newAttachment.currentIMEI,
		)
	}

	var (
		oldICCID, newICCID       sql.NullString
		oldInserted, newInserted bool
	)
	if err := database.QueryRow(
		`SELECT
			(SELECT iccid FROM devices WHERE imei = 'imei-old'),
			(SELECT sim_inserted FROM devices WHERE imei = 'imei-old'),
			(SELECT iccid FROM devices WHERE imei = 'imei-new'),
			(SELECT sim_inserted FROM devices WHERE imei = 'imei-new')`,
	).Scan(&oldICCID, &oldInserted, &newICCID, &newInserted); err != nil {
		t.Fatal(err)
	}
	if oldICCID.Valid || oldInserted {
		t.Fatalf("older device SIM attachment = ICCID %+v, inserted %v", oldICCID, oldInserted)
	}
	if !newICCID.Valid || newICCID.String != "iccid-new" || !newInserted {
		t.Fatalf("latest device SIM attachment = ICCID %+v, inserted %v", newICCID, newInserted)
	}
}

func TestOpenDropsUnattributedEndpointOnlyReferences(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "endpoint-only-line.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(legacyV1SchemaFixture(t)); err != nil {
		t.Fatal(err)
	}
	const endpointID = "line_legacy_endpoint"
	if _, err := database.Exec(
		`INSERT INTO modemdeck_telegram_units (id, enabled)
		 VALUES ('unit-endpoint-only', 1);
		 INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 VALUES ('unit-endpoint-only', ?);
		 INSERT INTO modemdeck_telegram_units (id, enabled)
		 VALUES ('unit-invalid-endpoint', 1);
		 INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 VALUES ('unit-invalid-endpoint', 'legacy-endpoint-invalid');
		 INSERT INTO modemdeck_line_call_policies (line_id, policy, revision)
		 VALUES (?, 'do_not_disturb', 4);
		 UPDATE modemdeck_line_settings
		 SET default_device_imei = ?, revision = 2
		 WHERE singleton = 1`,
		endpointID,
		endpointID,
		endpointID,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var (
		lineCount, scopeCount, policyCount, mappingCount int
		enabledUnitCount, unitCount                      int
		defaultLineID                                    string
	)
	if err := database.QueryRow(
		`SELECT
			(SELECT COUNT(*) FROM modemdeck_lines),
			(SELECT COUNT(*) FROM modemdeck_telegram_line_scopes),
			(SELECT COUNT(*) FROM modemdeck_line_call_policies),
			(SELECT COUNT(*) FROM modemdeck_legacy_endpoint_lines),
			(SELECT COUNT(*) FROM modemdeck_telegram_units WHERE enabled = 1),
			(SELECT default_line_id FROM modemdeck_line_settings WHERE singleton = 1),
			(SELECT COUNT(*) FROM modemdeck_telegram_units)`,
	).Scan(
		&lineCount,
		&scopeCount,
		&policyCount,
		&mappingCount,
		&enabledUnitCount,
		&defaultLineID,
		&unitCount,
	); err != nil {
		t.Fatalf("read endpoint-only migration result: %v", err)
	}
	if lineCount != 0 || scopeCount != 0 || policyCount != 0 ||
		mappingCount != 0 || enabledUnitCount != 0 || unitCount != 2 ||
		defaultLineID != "" {
		t.Fatalf(
			"endpoint-only migration = lines %d, scopes %d, policies %d, mappings %d, enabled units %d/%d, default %q",
			lineCount,
			scopeCount,
			policyCount,
			mappingCount,
			enabledUnitCount,
			unitCount,
			defaultLineID,
		)
	}
}

func TestOpenKeepsReusedSIMPhoneHistoriesOnSeparateLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "reused-sim-phone-history.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(legacyV1SchemaFixture(t)); err != nil {
		t.Fatal(err)
	}
	const (
		oldPhone = "+819011111111"
		newPhone = "+819022222222"
	)
	if _, err := database.Exec(
		`INSERT INTO devices (
			imei, iccid, sim_inserted, last_seen, created_at, updated_at
		 ) VALUES (
			'imei-reused', 'iccid-reused', 1, '2026-07-27 12:00:00',
			'2026-07-20 12:00:00', '2026-07-27 12:00:00'
		 );
		 INSERT INTO sim_cards (
			iccid, imsi, current_imei, line_label, line_color,
			last_seen, created_at, updated_at
		 ) VALUES (
			'iccid-reused', 'imsi-reused', 'imei-reused', 'Current number', 'blue',
			'2026-07-27 12:00:00', '2026-07-20 12:00:00', '2026-07-27 12:00:00'
		 );
		 INSERT INTO sim_subscriptions (
			imsi, current_iccid, phone_number, last_seen, created_at, updated_at
		 ) VALUES (
			'imsi-reused', 'iccid-reused', '+819022222222', '2026-07-27 12:00:00',
			'2026-07-20 12:00:00', '2026-07-27 12:00:00'
		 );
		 INSERT INTO sms (
			line_id, endpoint_message_id, imsi, iccid, peer, local_phone,
			content, type, state, timestamp, created_at
		 ) VALUES
			('line_reused_sim', 'message-old-phone', 'imsi-reused', 'iccid-reused',
				'+818012345678', '+819011111111', 'old phone history', 1, 'received',
				'2026-07-21 12:00:00', '2026-07-21 12:00:00'),
			('line_reused_sim', 'message-new-phone', 'imsi-reused', 'iccid-reused',
				'+818012345678', '+819022222222', 'new phone history', 1, 'received',
				'2026-07-27 12:00:00', '2026-07-27 12:00:00');
		 INSERT INTO modemdeck_telegram_units (id) VALUES ('unit-reused-sim');
		 INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 VALUES ('unit-reused-sim', 'line_reused_sim');
		 INSERT INTO contacts (id, display_name, preferred_device_imei)
		 VALUES ('contact-reused-sim', 'Reused SIM', 'imei-reused');
		 UPDATE modemdeck_line_settings
		 SET default_device_imei = 'imei-reused', revision = 2
		 WHERE singleton = 1`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	phoneLines := make(map[string]string)
	lineCount := 0
	rows, err := database.Query(`SELECT phone_number, line_id FROM modemdeck_lines`)
	if err != nil {
		t.Fatalf("read migrated phone lines: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var phoneNumber, lineID string
		if err := rows.Scan(&phoneNumber, &lineID); err != nil {
			t.Fatalf("scan migrated phone line: %v", err)
		}
		lineCount++
		phoneLines[phoneNumber] = lineID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read migrated phone lines: %v", err)
	}
	oldLineID := phoneLines[oldPhone]
	newLineID := phoneLines[newPhone]
	if lineCount != 2 || oldLineID == "" || newLineID == "" || oldLineID == newLineID {
		t.Fatalf(
			"migrated phone lines = count %d, old %q, new %q, all %+v",
			lineCount,
			oldLineID,
			newLineID,
			phoneLines,
		)
	}

	var oldMessageLineID, newMessageLineID string
	if err := database.QueryRow(
		`SELECT
			(SELECT line_id FROM sms WHERE endpoint_message_id = 'message-old-phone'),
			(SELECT line_id FROM sms WHERE endpoint_message_id = 'message-new-phone')`,
	).Scan(&oldMessageLineID, &newMessageLineID); err != nil {
		t.Fatalf("read migrated message lines: %v", err)
	}
	if oldMessageLineID != oldLineID || newMessageLineID != newLineID {
		t.Fatalf(
			"migrated message lines = old %q, new %q; want %q, %q",
			oldMessageLineID,
			newMessageLineID,
			oldLineID,
			newLineID,
		)
	}

	var (
		label, color, scopeLineID, simLineID, subscriptionLineID string
		defaultLineID, preferredLineID                           string
	)
	if err := database.QueryRow(
		`SELECT
			(SELECT line_label FROM modemdeck_lines WHERE line_id = ?),
			(SELECT line_color FROM modemdeck_lines WHERE line_id = ?),
			(SELECT line_id FROM modemdeck_telegram_line_scopes
				WHERE unit_id = 'unit-reused-sim'),
			(SELECT line_id FROM sim_cards WHERE iccid = 'iccid-reused'),
			(SELECT line_id FROM sim_subscriptions WHERE imsi = 'imsi-reused'),
			(SELECT default_line_id FROM modemdeck_line_settings WHERE singleton = 1),
			(SELECT preferred_line_id FROM contacts WHERE id = 'contact-reused-sim')`,
		newLineID,
		newLineID,
	).Scan(
		&label,
		&color,
		&scopeLineID,
		&simLineID,
		&subscriptionLineID,
		&defaultLineID,
		&preferredLineID,
	); err != nil {
		t.Fatalf("read migrated current line references: %v", err)
	}
	if label != "Current number" || color != "blue" ||
		scopeLineID != newLineID || simLineID != newLineID ||
		subscriptionLineID != newLineID || defaultLineID != newLineID ||
		preferredLineID != newLineID {
		t.Fatalf(
			"current migrated line = label %q, color %q, scope %q, SIM %q, subscription %q, default %q, preferred %q; want %q",
			label,
			color,
			scopeLineID,
			simLineID,
			subscriptionLineID,
			defaultLineID,
			preferredLineID,
			newLineID,
		)
	}
}

func TestBuildStableLineMigrationSeparatesPhonesReusingSIM(t *testing.T) {
	t.Parallel()

	lines, identities, endpoints, err := buildStableLineMigration(
		[]stableLineEvidence{
			{
				endpointID: "line_reused_sim",
				iccid:      "iccid-reused",
				imsi:       "imsi-reused",
				phone:      "+819011111111",
				observedAt: parseMigrationTime("2026-07-21 12:00:00"),
				ordinal:    0,
			},
			{
				endpointID: "line_reused_sim",
				iccid:      "iccid-reused",
				imsi:       "imsi-reused",
				phone:      "+819022222222",
				label:      "Current number",
				color:      "blue",
				observedAt: parseMigrationTime("2026-07-27 12:00:00"),
				ordinal:    1,
			},
		},
	)
	if err != nil {
		t.Fatalf("buildStableLineMigration() error = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("stable lines = %+v, want two phone lines", lines)
	}
	oldLineID := identities["phone:+819011111111"]
	newLineID := identities["phone:+819022222222"]
	if oldLineID == "" || newLineID == "" || oldLineID == newLineID {
		t.Fatalf("phone identities = old %q, new %q", oldLineID, newLineID)
	}
	if identities["iccid:iccid-reused"] != newLineID ||
		identities["imsi:imsi-reused"] != newLineID {
		t.Fatalf("current SIM identities = %+v, want %q", identities, newLineID)
	}
	if endpoints["line_reused_sim"].lineID != newLineID {
		t.Fatalf("current endpoint = %+v, want line %q", endpoints["line_reused_sim"], newLineID)
	}
}

func TestBuildStableLineMigrationDoesNotGlobalizeUnscopedNumbers(t *testing.T) {
	t.Parallel()

	for _, reported := range []string{"13800138000", "unknown"} {
		reported := reported
		t.Run(reported, func(t *testing.T) {
			lines, identities, endpoints, err := buildStableLineMigration(
				[]stableLineEvidence{
					{
						endpointID: "endpoint-one",
						iccid:      "iccid-one",
						imsi:       "imsi-one",
						phone:      reported,
						ordinal:    0,
					},
					{
						endpointID: "endpoint-two",
						iccid:      "iccid-two",
						imsi:       "imsi-two",
						phone:      reported,
						ordinal:    1,
					},
				},
			)
			if err != nil {
				t.Fatalf("buildStableLineMigration() error = %v", err)
			}
			if len(lines) != 2 {
				t.Fatalf("stable lines = %+v, want two SIM identities", lines)
			}
			for _, line := range lines {
				if line.phone != "" {
					t.Fatalf("stable line phone = %q, want empty", line.phone)
				}
			}
			if _, exists := identities["phone:"+reported]; exists {
				t.Fatalf("unscoped phone became global identity: %+v", identities)
			}
			if endpoints["endpoint-one"].lineID == "" ||
				endpoints["endpoint-two"].lineID == "" ||
				endpoints["endpoint-one"].lineID == endpoints["endpoint-two"].lineID {
				t.Fatalf("endpoint bindings = %+v, want distinct lines", endpoints)
			}
		})
	}
}

func TestOpenPreservesReportedPhoneValuesWithoutGuessingTheirRegion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-reported-call-number.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	v1Schema := legacyV1SchemaFixture(t)
	legacySchema := strings.Replace(
		v1Schema,
		"\n\t\t\treported_remote_number TEXT NOT NULL DEFAULT '',",
		"",
		1,
	)
	if legacySchema == v1Schema {
		t.Fatal("legacy schema fixture did not remove reported call number")
	}
	withoutReportedSMS := strings.Replace(
		legacySchema,
		"\n\t\t\t\treported_peer TEXT NOT NULL DEFAULT '',",
		"",
		1,
	)
	if withoutReportedSMS == legacySchema {
		t.Fatal("legacy schema fixture did not remove reported SMS peer")
	}
	legacySchema = withoutReportedSMS
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO call_history (id, direction, remote_number)
		 VALUES ('call-prefix-fixture', 'incoming', '00818000000001');
		 INSERT INTO sms (id, peer, content, timestamp)
		 VALUES (9001, '00818000000001', 'fixture', '2026-07-29 01:00:00');
		 INSERT INTO contacts (id, display_name)
		 VALUES ('contact-prefix-fixture', 'Prefix Fixture');
		 INSERT INTO contact_phones (
			id, contact_id, label, original_number, canonical_e164, is_primary
		 ) VALUES (
			'phone-prefix-fixture', 'contact-prefix-fixture', 'mobile',
			'0081 80 0000 0001', '00818000000001', 1
		 );`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var remote, reported string
	if err := database.QueryRow(
		`SELECT remote_number, reported_remote_number
		 FROM call_history WHERE id = 'call-prefix-fixture'`,
	).Scan(&remote, &reported); err != nil {
		t.Fatal(err)
	}
	if remote != "00818000000001" || reported != "00818000000001" {
		t.Fatalf("migrated call numbers = (%q, %q)", remote, reported)
	}

	var peer, reportedPeer string
	if err := database.QueryRow(
		`SELECT peer, reported_peer FROM sms WHERE id = 9001`,
	).Scan(&peer, &reportedPeer); err != nil {
		t.Fatal(err)
	}
	if peer != "00818000000001" || reportedPeer != "00818000000001" {
		t.Fatalf("migrated SMS numbers = (%q, %q)", peer, reportedPeer)
	}

	var original, canonical string
	if err := database.QueryRow(
		`SELECT original_number, canonical_e164
		 FROM contact_phones WHERE id = 'phone-prefix-fixture'`,
	).Scan(&original, &canonical); err != nil {
		t.Fatal(err)
	}
	if original != "0081 80 0000 0001" || canonical != "00818000000001" {
		t.Fatalf("migrated contact number = (%q, %q)", original, canonical)
	}
}

func TestOpenAllowsDuplicateCanonicalContactsAfterLegacyIndexMigration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "duplicate-contact-phone-identities.db")
	database, err := Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`DROP INDEX idx_contact_phones_canonical_e164;
		 CREATE UNIQUE INDEX ux_contact_phones_canonical_e164
		 ON contact_phones(canonical_e164);
		 INSERT INTO contacts (id, display_name) VALUES
			('contact-legacy-idd', 'Legacy IDD'),
			('contact-e164', 'Canonical E164');
		 INSERT INTO contact_phones (
			id, contact_id, original_number, canonical_e164, region, is_primary
		 ) VALUES
			(
				'phone-legacy-idd', 'contact-legacy-idd',
				'0081 90 1234 5678', '00819012345678', 'CN', 1
			),
			(
				'phone-e164', 'contact-e164',
				'+81 90 1234 5678', '+819012345678', 'JP', 1
			);`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.Exec(
		`INSERT INTO contacts (id, display_name)
		 VALUES ('contact-shared', 'Shared Number');
		 INSERT INTO contact_phones (
			id, contact_id, original_number, canonical_e164, region, is_primary
		 ) VALUES (
			'phone-shared', 'contact-shared',
			'090 1234 5678', '+819012345678', 'JP', 1
		 )`,
	); err != nil {
		t.Fatalf("insert duplicate canonical phone after migration: %v", err)
	}

	var canonicalCount int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM contact_phones
		 WHERE canonical_e164 = '+819012345678'`,
	).Scan(&canonicalCount); err != nil {
		t.Fatal(err)
	}
	if canonicalCount != 2 {
		t.Fatalf("canonical contact phones = %d, want duplicate rows", canonicalCount)
	}

	var currentIndex, legacyIndex int
	if err := database.QueryRow(
		`SELECT
			EXISTS(
				SELECT 1 FROM sqlite_master
				WHERE type = 'index' AND name = 'idx_contact_phones_canonical_e164'
			),
			EXISTS(
				SELECT 1 FROM sqlite_master
				WHERE type = 'index' AND name = 'ux_contact_phones_canonical_e164'
			)`,
	).Scan(&currentIndex, &legacyIndex); err != nil {
		t.Fatal(err)
	}
	if currentIndex != 1 || legacyIndex != 0 {
		t.Fatalf(
			"contact phone indexes = current %d, legacy %d",
			currentIndex,
			legacyIndex,
		)
	}
}

func TestOpenDoesNotRepairMissingCurrentIndex(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing-index.db")
	database, err := Open(context.Background(), Config{TargetPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DROP INDEX idx_contacts_display_name`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(context.Background(), Config{TargetPath: path})
	if !errors.Is(err, ErrSchemaOutdated) {
		t.Fatalf("Open() error = %v, want ErrSchemaOutdated", err)
	}
}

func legacyV1SchemaFixture(t *testing.T) string {
	t.Helper()
	schema := currentSchemaSQL
	replace := func(current, legacy string) {
		t.Helper()
		updated := strings.Replace(schema, current, legacy, 1)
		if updated == schema {
			t.Fatalf("V1 schema fixture transformation did not find %q", current)
		}
		schema = updated
	}
	removeBlock := func(start, end string) {
		t.Helper()
		startIndex := strings.Index(schema, start)
		if startIndex < 0 {
			t.Fatalf("V1 schema fixture transformation did not find block start %q", start)
		}
		endOffset := strings.Index(schema[startIndex:], end)
		if endOffset < 0 {
			t.Fatalf("V1 schema fixture transformation did not find block end %q", end)
		}
		endIndex := startIndex + endOffset
		schema = schema[:startIndex] + schema[endIndex:]
	}

	replace("\n\t\t\tios_pairing_enabled NUMERIC NOT NULL DEFAULT 0,", "")
	removeBlock(
		"CREATE TABLE modemdeck_ios_pairing_credentials (",
		"CREATE TABLE contacts",
	)
	replace(
		"CREATE INDEX idx_modemdeck_ios_pairing_user\n"+
			"\tON modemdeck_ios_pairing_credentials(user_id, activated_at, created_at);\n\n",
		"",
	)
	replace(
		"CREATE UNIQUE INDEX ux_modemdeck_ios_pairing_pending_user\n"+
			"\tON modemdeck_ios_pairing_credentials(user_id) WHERE activated_at IS NULL;\n\n",
		"",
	)
	replace("preferred_line_id TEXT NOT NULL DEFAULT ''", "preferred_device_imei TEXT NOT NULL DEFAULT ''")
	replace(
		"line_id TEXT NOT NULL DEFAULT '',\n\t\t\t\tendpoint_line_id TEXT NOT NULL DEFAULT '',\n\t\t\t\tendpoint_message_id",
		"line_id TEXT NOT NULL DEFAULT '',\n\t\t\t\tendpoint_message_id",
	)
	replace(",\n\t\t\t\tdeleted_at DATETIME", "")
	replace("\t\t\t\tline_id TEXT NOT NULL,\n\t\t\t\timsi TEXT NOT NULL,", "\t\t\t\timsi TEXT NOT NULL,")
	replace("PRIMARY KEY (line_id, peer)", "PRIMARY KEY (imsi, peer)")
	replace(
		"line_id TEXT NOT NULL DEFAULT '',\n\t\t\tendpoint_line_id TEXT NOT NULL DEFAULT '',\n\t\t\tlocal_phone",
		"device_id TEXT NOT NULL DEFAULT '',\n\t\t\tlocal_phone",
	)
	replace("default_line_id TEXT NOT NULL DEFAULT ''", "default_device_imei TEXT NOT NULL DEFAULT ''")
	replace(
		"line_id TEXT NOT NULL,\n\t\t\tendpoint_line_id TEXT NOT NULL,\n\t\t\tendpoint_call_id",
		"line_id TEXT NOT NULL,\n\t\t\tendpoint_call_id",
	)
	removeBlock("CREATE TABLE modemdeck_lines (", "CREATE TABLE devices")
	replace("\t\t\tendpoint_id TEXT NOT NULL DEFAULT '',\n\t\t\tname TEXT", "\t\t\talias TEXT")
	replace(
		`			line_id TEXT NOT NULL,
			imsi TEXT NOT NULL DEFAULT '',`,
		`			imsi TEXT NOT NULL DEFAULT '',
			line_label TEXT NOT NULL DEFAULT '',
			line_color TEXT NOT NULL DEFAULT ''
				CHECK (line_color IN (
					'', 'teal', 'blue', 'indigo', 'violet',
					'green', 'amber', 'orange', 'red'
				)),`,
	)
	replace(
		"imsi TEXT PRIMARY KEY,\n\t\t\tline_id TEXT NOT NULL,\n\t\t\tcurrent_iccid",
		"imsi TEXT PRIMARY KEY,\n\t\t\tcurrent_iccid",
	)
	replace(
		",\n\t\t\tFOREIGN KEY (line_id) REFERENCES modemdeck_lines(line_id)",
		"",
	)
	replace(
		",\n\t\t\tFOREIGN KEY (line_id) REFERENCES modemdeck_lines(line_id)",
		"",
	)
	replace("\t\t\tendpoint_scope_id TEXT NOT NULL,\n\t\t\tepoch TEXT", "\t\t\tepoch TEXT")
	replace("PRIMARY KEY (scope_kind, endpoint_scope_id)", "PRIMARY KEY (scope_kind, scope_id)")
	replace(
		"CREATE INDEX idx_contacts_preferred_line ON contacts(preferred_line_id);",
		"CREATE INDEX idx_contacts_preferred_device ON contacts(preferred_device_imei);",
	)
	replace(
		"CREATE UNIQUE INDEX ux_sms_endpoint_line_message ON sms(endpoint_line_id, endpoint_message_id) WHERE endpoint_line_id <> '' AND endpoint_message_id <> '';",
		"CREATE UNIQUE INDEX ux_sms_endpoint_message ON sms(line_id, endpoint_message_id) WHERE line_id <> '' AND endpoint_message_id <> '';",
	)
	replace(
		`CREATE INDEX idx_call_history_line_ended_at ON call_history(line_id, ended_at DESC);

CREATE INDEX idx_call_history_endpoint_line_ended_at ON call_history(endpoint_line_id, ended_at DESC);`,
		"CREATE INDEX idx_call_history_device_ended_at ON call_history(device_id, ended_at DESC);",
	)
	replace(
		"CREATE UNIQUE INDEX ux_modemdeck_lines_phone_number ON modemdeck_lines(phone_number) WHERE phone_number <> '';\n\n",
		"",
	)
	replace("CREATE UNIQUE INDEX ux_devices_endpoint_id ON devices(endpoint_id) WHERE endpoint_id <> '';\n\n", "")
	replace("CREATE UNIQUE INDEX ux_devices_current_iccid ON devices(iccid) WHERE COALESCE(iccid, '') <> '';\n\n", "")
	replace("CREATE INDEX idx_sim_cards_line_id ON sim_cards(line_id);\n\n", "")
	replace("CREATE UNIQUE INDEX ux_sim_cards_current_imei ON sim_cards(current_imei) WHERE COALESCE(current_imei, '') <> '';\n\n", "")
	replace("CREATE INDEX idx_sim_subscriptions_line_id ON sim_subscriptions(line_id);\n\n", "")
	replace("CREATE INDEX idx_sms_line_peer_timestamp ON sms(line_id, peer, timestamp DESC, id DESC);\n\n", "")
	replace("CREATE INDEX idx_sms_line_peer_id ON sms(line_id, peer, id);\n\n", "")
	replace("CREATE INDEX idx_sms_incoming_unread_line_peer_id ON sms(line_id, peer, type, id);\n\n", "")
	replace("CREATE INDEX idx_sms_contacts_line_timestamp ON sms_contacts(line_id, last_timestamp DESC, last_sms_id DESC, peer);\n\n", "")
	replace("CREATE INDEX idx_sms_contacts_cursor ON sms_contacts(COALESCE(last_timestamp, '') DESC, last_sms_id DESC, line_id, peer);\n\n", "")
	replace("CREATE INDEX idx_sms_contacts_line_cursor ON sms_contacts(line_id, COALESCE(last_timestamp, '') DESC, last_sms_id DESC, peer);\n\n", "")
	replace("CREATE INDEX idx_call_history_line_ended_at_id ON call_history(line_id, COALESCE(ended_at, '') DESC, id DESC);\n\n", "")
	replace(
		"singleton, default_line_id, revision, updated_at",
		"singleton, default_device_imei, revision, updated_at",
	)
	return schema
}

func schemaBeforeMobilePairingFixture(t *testing.T) string {
	t.Helper()
	schema := currentSchemaSQL
	remove := func(fragment string) {
		t.Helper()
		updated := strings.Replace(schema, fragment, "", 1)
		if updated == schema {
			t.Fatalf(
				"pre-iOS schema fixture did not find fragment %q",
				fragment,
			)
		}
		schema = updated
	}
	removeTable := func(name string) {
		t.Helper()
		start := "CREATE TABLE " + name + " ("
		startIndex := strings.Index(schema, start)
		if startIndex < 0 {
			t.Fatalf("pre-iOS schema fixture did not find table %q", name)
		}
		endOffset := strings.Index(schema[startIndex:], ");\n\n")
		if endOffset < 0 {
			t.Fatalf("pre-iOS schema fixture did not find end of table %q", name)
		}
		endIndex := startIndex + endOffset + len(");\n\n")
		schema = schema[:startIndex] + schema[endIndex:]
	}

	remove("\n\t\t\tios_pairing_enabled NUMERIC NOT NULL DEFAULT 0,")
	removeTable("modemdeck_ios_pairing_credentials")
	remove("CREATE INDEX idx_modemdeck_ios_pairing_user\n" +
		"\tON modemdeck_ios_pairing_credentials(user_id, activated_at, created_at);\n\n")
	remove("CREATE UNIQUE INDEX ux_modemdeck_ios_pairing_pending_user\n" +
		"\tON modemdeck_ios_pairing_credentials(user_id) WHERE activated_at IS NULL;\n\n")
	return schema
}

func singleUserSchemaFixture(t *testing.T) string {
	t.Helper()
	schema := currentSchemaSQL
	removeTable := func(name string) {
		t.Helper()
		start := "CREATE TABLE " + name + " ("
		startIndex := strings.Index(schema, start)
		if startIndex < 0 {
			t.Fatalf("single-user schema fixture did not find table %q", name)
		}
		endOffset := strings.Index(schema[startIndex:], ");\n\n")
		if endOffset < 0 {
			t.Fatalf("single-user schema fixture did not find end of table %q", name)
		}
		endIndex := startIndex + endOffset + len(");\n\n")
		schema = schema[:startIndex] + schema[endIndex:]
	}
	remove := func(fragment string) {
		t.Helper()
		updated := strings.Replace(schema, fragment, "", 1)
		if updated == schema {
			t.Fatalf(
				"single-user schema fixture did not find fragment %q",
				fragment,
			)
		}
		schema = updated
	}
	replace := func(current, legacy string) {
		t.Helper()
		updated := strings.Replace(schema, current, legacy, 1)
		if updated == schema {
			t.Fatalf(
				"single-user schema fixture did not find fragment %q",
				current,
			)
		}
		schema = updated
	}

	for _, table := range []string{
		"modemdeck_users",
		"modemdeck_ios_pairing_credentials",
		"modemdeck_user_profile_contacts",
		"modemdeck_user_message_thread_state",
		"modemdeck_user_call_state",
		"modemdeck_user_recording_state",
		"modemdeck_user_lines",
		"modemdeck_user_preferences",
	} {
		removeTable(table)
	}
	remove("CREATE INDEX idx_modemdeck_ios_pairing_user\n" +
		"\tON modemdeck_ios_pairing_credentials(user_id, activated_at, created_at);\n\n")
	remove("CREATE UNIQUE INDEX ux_modemdeck_ios_pairing_pending_user\n" +
		"\tON modemdeck_ios_pairing_credentials(user_id) WHERE activated_at IS NULL;\n\n")
	replace(
		"user_id TEXT NOT NULL DEFAULT 'user_admin',\n"+
			"\t\t\tcreated_at_unix INTEGER NOT NULL,\n"+
			"\t\t\tlast_seen_at_unix INTEGER NOT NULL,\n"+
			"\t\t\tuser_agent TEXT NOT NULL DEFAULT '',\n"+
			"\t\t\taccess_ip TEXT NOT NULL DEFAULT '',\n"+
			"\t\t\taccess_host TEXT NOT NULL DEFAULT '',\n"+
			"\t\t\tFOREIGN KEY (user_id) REFERENCES modemdeck_users(id) "+
			"ON DELETE CASCADE ON UPDATE CASCADE",
		"created_at_unix INTEGER NOT NULL,\n"+
			"\t\t\texpires_at_unix INTEGER NOT NULL "+
			"CHECK (expires_at_unix >= created_at_unix)",
	)
	replace(
		"CREATE INDEX idx_modemdeck_auth_sessions_user_created\n"+
			"\tON modemdeck_auth_sessions(user_id, created_at_unix DESC);",
		"CREATE INDEX idx_modemdeck_auth_sessions_expiry "+
			"ON modemdeck_auth_sessions(expires_at_unix);",
	)
	remove("\n\t\t\towner_user_id TEXT NOT NULL DEFAULT 'user_admin',")
	remove(
		"\n\t\t\tscope_source TEXT NOT NULL DEFAULT 'manual'\n" +
			"\t\t\t\tCHECK (scope_source IN ('manual', 'user'))," +
			"\n\t\t\tassigned_user_id TEXT NOT NULL DEFAULT ''," +
			"\n\t\t\tmanual_all_lines NUMERIC NOT NULL DEFAULT 1,",
	)
	for _, index := range []string{
		"CREATE UNIQUE INDEX ux_modemdeck_single_admin " +
			"ON modemdeck_users(role) WHERE role = 'admin';\n\n",
		"CREATE INDEX idx_modemdeck_user_lines_line " +
			"ON modemdeck_user_lines(line_id, user_id);\n\n",
		"CREATE INDEX idx_contacts_owner_display_name " +
			"ON contacts(owner_user_id, display_name);\n\n",
		"CREATE INDEX idx_contacts_owner_cursor " +
			"ON contacts(owner_user_id, COALESCE(display_name, '') COLLATE NOCASE, id);\n\n",
	} {
		remove(index)
	}
	return schema
}
