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
	legacySchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tusername TEXT NOT NULL DEFAULT 'admin',",
		"",
		1,
	)
	if legacySchema == currentSchemaSQL {
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
	legacySchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tavatar TEXT NOT NULL DEFAULT '',",
		"",
		1,
	)
	if legacySchema == currentSchemaSQL {
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
	legacySchema := strings.Replace(
		currentSchemaSQL,
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
	if legacySchema == currentSchemaSQL {
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
	legacySchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\tline_color TEXT NOT NULL DEFAULT ''\n\t\t\t\tCHECK (line_color IN (\n\t\t\t\t\t'', 'teal', 'blue', 'indigo', 'violet',\n\t\t\t\t\t'green', 'amber', 'orange', 'red'\n\t\t\t\t)),",
		"",
		1,
	)
	if legacySchema == currentSchemaSQL {
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
	var label, color string
	if err := database.QueryRow(
		`SELECT line_label, line_color FROM sim_cards WHERE iccid = 'iccid-1'`,
	).Scan(&label, &color); err != nil {
		t.Fatal(err)
	}
	if label != "主卡" || color != "" {
		t.Fatalf("migrated line identity = (%q, %q), want (主卡, empty)", label, color)
	}
}

func TestOpenMigratesReportedAndCanonicalPhoneIdentities(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "without-reported-call-number.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := strings.Replace(
		currentSchemaSQL,
		"\n\t\t\treported_remote_number TEXT NOT NULL DEFAULT '',",
		"",
		1,
	)
	if legacySchema == currentSchemaSQL {
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
