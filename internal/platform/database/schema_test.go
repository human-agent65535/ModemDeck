package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
				CHECK (language IN ('auto', 'zh-CN', 'en-US')),
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

func TestOpenMigratesReportedAndCanonicalPhoneIdentities(t *testing.T) {
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
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO call_history (id, direction, remote_number)
		 VALUES ('call-prefix-fixture', 'incoming', '00818000000001');
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
	if remote != "+818000000001" || reported != "00818000000001" {
		t.Fatalf("migrated call numbers = (%q, %q)", remote, reported)
	}

	var original, canonical string
	if err := database.QueryRow(
		`SELECT original_number, canonical_e164
		 FROM contact_phones WHERE id = 'phone-prefix-fixture'`,
	).Scan(&original, &canonical); err != nil {
		t.Fatal(err)
	}
	if original != "0081 80 0000 0001" || canonical != "+818000000001" {
		t.Fatalf("migrated contact number = (%q, %q)", original, canonical)
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

	replace("preferred_line_id TEXT NOT NULL DEFAULT ''", "preferred_device_imei TEXT NOT NULL DEFAULT ''")
	replace(
		"line_id TEXT NOT NULL DEFAULT '',\n\t\t\t\tendpoint_line_id TEXT NOT NULL DEFAULT '',\n\t\t\t\tendpoint_message_id",
		"line_id TEXT NOT NULL DEFAULT '',\n\t\t\t\tendpoint_message_id",
	)
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
	replace("\t\t\tendpoint_id TEXT NOT NULL DEFAULT '',\n\t\t\talias TEXT", "\t\t\talias TEXT")
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
	replace(
		"singleton, default_line_id, revision, updated_at",
		"singleton, default_device_imei, revision, updated_at",
	)
	return schema
}
