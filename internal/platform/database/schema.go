package database

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

//go:embed schema.sql
var currentSchemaSQL string

var ErrSchemaOutdated = errors.New("database schema is not current")

type schemaShape struct {
	tables  map[string]map[string]struct{}
	indexes map[string]struct{}
}

// InitializeSchema creates the current schema when the database is empty.
// Existing databases receive only explicitly supported, shape-checked migrations
// before the resulting schema is validated.
func InitializeSchema(ctx context.Context, database *sql.DB) (bool, error) {
	if database == nil {
		return false, errors.New("initialize schema: nil database")
	}
	empty, err := databaseIsEmpty(ctx, database)
	if err != nil {
		return false, err
	}
	if !empty {
		if err := migrateSchema(ctx, database); err != nil {
			return false, err
		}
		if err := ValidateSchema(ctx, database); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := createSchema(ctx, database); err != nil {
		return false, err
	}
	return true, nil
}

func migrateSchema(ctx context.Context, database *sql.DB) error {
	expected, err := expectedSchemaShape(ctx)
	if err != nil {
		return err
	}
	actual, err := readSchemaShape(ctx, database)
	if err != nil {
		return err
	}
	migratedMultiUser, err := migrateMultiUserSchema(ctx, database, actual)
	if err != nil {
		return err
	}
	if migratedMultiUser {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedAuthSessions, err := migratePersistentAuthSessions(ctx, database, actual)
	if err != nil {
		return err
	}
	if migratedAuthSessions {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedAuthSessionMetadata, err := migrateCurrentAuthSessionMetadata(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedAuthSessionMetadata {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	droppedPasswordChangeColumn, err := dropDeprecatedPasswordChangeColumn(
		ctx,
		database,
		actual,
	)
	if err != nil {
		return err
	}
	if droppedPasswordChangeColumn {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedUserPreferenceRevision, err := migrateUserPreferenceRevision(
		ctx,
		database,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedUserPreferenceRevision {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedUserPreferenceValues, err := migrateUserPreferenceValues(
		ctx,
		database,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedUserPreferenceValues {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedTelegramUsers, err := migrateTelegramUserScopeColumns(ctx, database, actual)
	if err != nil {
		return err
	}
	if migratedTelegramUsers {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	if err := migrateTelegramOwnership(ctx, database, actual); err != nil {
		return err
	}
	if err := migrateSystemSettingsLanguages(ctx, database, actual); err != nil {
		return err
	}
	migratedContactPhoneIndex, err := migrateContactPhoneCanonicalIndex(
		ctx,
		database,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedContactPhoneIndex {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedDeviceName, err := migrateCurrentDeviceNameColumn(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedDeviceName {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedCallReadAt, err := migrateCurrentCallReadAtColumn(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedCallReadAt {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedSMSDeletedAt, err := migrateCurrentSMSDeletedAtColumn(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedSMSDeletedAt {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedPhoneIdentity, err := migrateCurrentPhoneIdentityColumns(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedPhoneIdentity {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedCommunicationState, err := migrateCurrentCommunicationStateColumns(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedCommunicationState {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedSMSDelivery, err := migrateCurrentSMSDeliveryColumns(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedSMSDelivery {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedSMSLineIndexes, err := migrateSMSLineIndexes(ctx, database, actual)
	if err != nil {
		return err
	}
	if migratedSMSLineIndexes {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedPaginationIndexes, err := migratePaginationIndexes(
		ctx,
		database,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedPaginationIndexes {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedMobilePairing, err := migrateMobilePairingSchema(
		ctx,
		database,
		expected,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedMobilePairing {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	if schemaContains(expected, actual) {
		return nil
	}
	legacyExpected := legacyV1SchemaShape(schemaBeforeMobilePairing(expected))
	if err := migrateLegacySchemaAdditions(
		ctx,
		database,
		legacyExpected,
		actual,
	); err != nil {
		return err
	}
	actual, err = readSchemaShape(ctx, database)
	if err != nil {
		return err
	}
	if !schemaShapesEqual(legacyExpected, actual) {
		return nil
	}
	if current, err := requiredRowsAreCurrent(ctx, database, legacyRequiredSchemaRows); err != nil {
		return err
	} else if !current {
		return nil
	}
	if err := migrateStableLineIdentity(ctx, database); err != nil {
		return err
	}
	actual, err = readSchemaShape(ctx, database)
	if err != nil {
		return err
	}
	migratedSMSLineIndexes, err = migrateSMSLineIndexes(ctx, database, actual)
	if err != nil {
		return err
	}
	if migratedSMSLineIndexes {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	migratedPaginationIndexes, err = migratePaginationIndexes(
		ctx,
		database,
		actual,
	)
	if err != nil {
		return err
	}
	if migratedPaginationIndexes {
		actual, err = readSchemaShape(ctx, database)
		if err != nil {
			return err
		}
	}
	_, err = migrateMobilePairingSchema(ctx, database, expected, actual)
	return err
}

func migratePaginationIndexes(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	type paginationIndex struct {
		name       string
		table      string
		columns    []string
		definition string
	}
	indexes := []paginationIndex{
		{
			name:    "idx_contacts_owner_cursor",
			table:   "contacts",
			columns: []string{"owner_user_id", "display_name", "id"},
			definition: `CREATE INDEX idx_contacts_owner_cursor ON contacts(
				owner_user_id,
				COALESCE(display_name, '') COLLATE NOCASE,
				id
			)`,
		},
		{
			name:    "idx_contacts_cursor",
			table:   "contacts",
			columns: []string{"display_name", "id"},
			definition: `CREATE INDEX idx_contacts_cursor ON contacts(
				COALESCE(display_name, '') COLLATE NOCASE,
				id
			)`,
		},
		{
			name:    "idx_sms_line_peer_cursor",
			table:   "sms",
			columns: []string{"line_id", "peer", "timestamp", "id"},
			definition: `CREATE INDEX idx_sms_line_peer_cursor ON sms(
				line_id,
				peer,
				COALESCE(timestamp, '') DESC,
				id DESC
			)`,
		},
		{
			name:    "idx_sms_contacts_cursor",
			table:   "sms_contacts",
			columns: []string{"last_timestamp", "last_sms_id", "line_id", "peer"},
			definition: `CREATE INDEX idx_sms_contacts_cursor ON sms_contacts(
				COALESCE(last_timestamp, '') DESC,
				last_sms_id DESC,
				line_id,
				peer
			)`,
		},
		{
			name:    "idx_sms_contacts_line_cursor",
			table:   "sms_contacts",
			columns: []string{"line_id", "last_timestamp", "last_sms_id", "peer"},
			definition: `CREATE INDEX idx_sms_contacts_line_cursor ON sms_contacts(
				line_id,
				COALESCE(last_timestamp, '') DESC,
				last_sms_id DESC,
				peer
			)`,
		},
		{
			name:    "idx_call_history_ended_at_id",
			table:   "call_history",
			columns: []string{"ended_at", "id"},
			definition: `CREATE INDEX idx_call_history_ended_at_id
				ON call_history(COALESCE(ended_at, '') DESC, id DESC)`,
		},
		{
			name:    "idx_call_history_line_ended_at_id",
			table:   "call_history",
			columns: []string{"line_id", "ended_at", "id"},
			definition: `CREATE INDEX idx_call_history_line_ended_at_id
				ON call_history(
					line_id,
					COALESCE(ended_at, '') DESC,
					id DESC
				)`,
		},
		{
			name:  "idx_modemdeck_call_recordings_cursor",
			table: "modemdeck_call_recordings",
			columns: []string{
				"started_at",
				"created_at",
				"call_id",
				"segment_index",
				"id",
			},
			definition: `CREATE INDEX idx_modemdeck_call_recordings_cursor
				ON modemdeck_call_recordings(
					COALESCE(started_at, created_at) DESC,
					call_id DESC,
					segment_index DESC,
					id DESC
				)`,
		},
	}

	migrated := false
	for _, index := range indexes {
		if _, exists := actual.indexes[index.name]; exists {
			continue
		}
		tableColumns, exists := actual.tables[index.table]
		if !exists {
			continue
		}
		complete := true
		for _, column := range index.columns {
			if _, exists := tableColumns[column]; !exists {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		if _, err := database.ExecContext(ctx, index.definition); err != nil {
			return false, fmt.Errorf(
				"create pagination index %s: %w",
				index.name,
				err,
			)
		}
		migrated = true
	}
	return migrated, nil
}

func migrateSMSLineIndexes(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	requiredSMSColumns := []string{"id", "line_id", "peer", "timestamp", "type"}
	smsColumns, smsExists := actual.tables["sms"]
	if !smsExists {
		return false, nil
	}
	for _, column := range requiredSMSColumns {
		if _, exists := smsColumns[column]; !exists {
			return false, nil
		}
	}
	smsContactColumns, smsContactsExist := actual.tables["sms_contacts"]
	if !smsContactsExist {
		return false, nil
	}
	for _, column := range []string{"line_id", "last_timestamp", "last_sms_id", "peer"} {
		if _, exists := smsContactColumns[column]; !exists {
			return false, nil
		}
	}

	indexes := []struct {
		name       string
		definition string
	}{
		{
			name: "idx_sms_line_peer_timestamp",
			definition: `CREATE INDEX idx_sms_line_peer_timestamp
				ON sms(line_id, peer, timestamp DESC, id DESC)`,
		},
		{
			name: "idx_sms_line_peer_id",
			definition: `CREATE INDEX idx_sms_line_peer_id
				ON sms(line_id, peer, id)`,
		},
		{
			name: "idx_sms_incoming_unread_line_peer_id",
			definition: `CREATE INDEX idx_sms_incoming_unread_line_peer_id
				ON sms(line_id, peer, type, id)`,
		},
		{
			name: "idx_sms_contacts_line_timestamp",
			definition: `CREATE INDEX idx_sms_contacts_line_timestamp
				ON sms_contacts(line_id, last_timestamp DESC, last_sms_id DESC, peer)`,
		},
	}
	migrated := false
	for _, index := range indexes {
		if _, exists := actual.indexes[index.name]; exists {
			continue
		}
		if _, err := database.ExecContext(ctx, index.definition); err != nil {
			return false, fmt.Errorf("create SMS line index %s: %w", index.name, err)
		}
		migrated = true
	}
	return migrated, nil
}

func migrateSystemSettingsLanguages(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) error {
	if _, exists := actual.tables["modemdeck_system_settings"]; !exists {
		return nil
	}
	var definition string
	if err := database.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_master
		 WHERE type = 'table' AND name = 'modemdeck_system_settings'`,
	).Scan(&definition); err != nil {
		return fmt.Errorf("read system settings schema: %w", err)
	}
	supportedLanguages := []string{
		"'zh-TW'",
		"'ja-JP'",
		"'vi-VN'",
		"'es-ES'",
		"'de-DE'",
		"'fr-FR'",
		"'pt-BR'",
	}
	current := true
	for _, language := range supportedLanguages {
		if !strings.Contains(definition, language) {
			current = false
			break
		}
	}
	if current {
		return nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin system language migration: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		`CREATE TABLE modemdeck_system_settings_new (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			language TEXT NOT NULL DEFAULT 'auto'
				CHECK (language IN (
					'auto', 'zh-CN', 'zh-TW', 'en-US', 'ja-JP',
					'vi-VN', 'es-ES', 'de-DE', 'fr-FR', 'pt-BR'
				)),
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO modemdeck_system_settings_new (
			singleton, language, revision, updated_at
		)
		SELECT singleton, language, revision, updated_at
		FROM modemdeck_system_settings;
		DROP TABLE modemdeck_system_settings;
		ALTER TABLE modemdeck_system_settings_new
		RENAME TO modemdeck_system_settings;`,
	); err != nil {
		return fmt.Errorf("migrate system settings languages: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit system language migration: %w", err)
	}
	return nil
}

func migrateContactPhoneCanonicalIndex(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	if _, exists := actual.tables["contact_phones"]; !exists {
		return false, nil
	}
	if _, legacyExists := actual.indexes["ux_contact_phones_canonical_e164"]; !legacyExists {
		return false, nil
	}
	if _, err := database.ExecContext(
		ctx,
		`DROP INDEX ux_contact_phones_canonical_e164;
		 CREATE INDEX IF NOT EXISTS idx_contact_phones_canonical_e164
		 ON contact_phones(canonical_e164)`,
	); err != nil {
		return false, fmt.Errorf("migrate contact phone identity index: %w", err)
	}
	return true, nil
}

func migrateCurrentDeviceNameColumn(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	legacy := legacyDeviceNameSchemaShape(expected)
	if callColumns := actual.tables["call_history"]; callColumns != nil {
		if _, readAtExists := callColumns["read_at"]; !readAtExists {
			legacy = schemaWithoutColumn(legacy, "call_history", "read_at")
		}
	}
	deviceColumns := actual.tables["devices"]
	_, hasLegacyName := deviceColumns["alias"]
	_, hasCurrentName := deviceColumns["name"]
	if !hasLegacyName || hasCurrentName || !schemaContains(legacy, actual) {
		return false, nil
	}
	if _, err := database.ExecContext(
		ctx,
		`ALTER TABLE devices RENAME COLUMN alias TO name`,
	); err != nil {
		return false, fmt.Errorf("migrate device name: %w", err)
	}
	return true, nil
}

func migrateCurrentCallReadAtColumn(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	callColumns, callHistoryExists := actual.tables["call_history"]
	if !callHistoryExists {
		return false, nil
	}
	if _, readAtExists := callColumns["read_at"]; readAtExists {
		return false, nil
	}
	previous := schemaWithoutColumn(expected, "call_history", "read_at")
	if !schemaContains(previous, actual) {
		return false, nil
	}
	if _, err := database.ExecContext(
		ctx,
		`ALTER TABLE call_history ADD COLUMN read_at DATETIME`,
	); err != nil {
		return false, fmt.Errorf("migrate missed call read state: %w", err)
	}
	return true, nil
}

func migrateCurrentSMSDeletedAtColumn(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	smsColumns, smsExists := actual.tables["sms"]
	if !smsExists {
		return false, nil
	}
	if _, deletedAtExists := smsColumns["deleted_at"]; deletedAtExists {
		return false, nil
	}
	previous := schemaWithoutColumn(expected, "sms", "deleted_at")
	if !schemaContains(previous, actual) {
		return false, nil
	}
	if _, err := database.ExecContext(
		ctx,
		`ALTER TABLE sms ADD COLUMN deleted_at DATETIME`,
	); err != nil {
		return false, fmt.Errorf("migrate SMS deletion state: %w", err)
	}
	return true, nil
}

func migrateCurrentPhoneIdentityColumns(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	contactColumns, contactsExist := actual.tables["contact_phones"]
	smsColumns, smsExists := actual.tables["sms"]
	lineColumns, linesExist := actual.tables["modemdeck_lines"]
	subscriptionColumns, subscriptionsExist := actual.tables["sim_subscriptions"]
	_, hasContactRegion := contactColumns["region"]
	_, hasReportedPeer := smsColumns["reported_peer"]
	_, hasLineHomeCountry := lineColumns["home_country_iso"]
	_, hasHomeOperatorCode := subscriptionColumns["home_operator_code"]
	_, hasSubscriptionHomeCountry := subscriptionColumns["home_country_iso"]
	if (!contactsExist || hasContactRegion) &&
		(!smsExists || hasReportedPeer) &&
		(!linesExist || hasLineHomeCountry) &&
		(!subscriptionsExist || hasHomeOperatorCode) &&
		(!subscriptionsExist || hasSubscriptionHomeCountry) {
		return false, nil
	}
	previous := expected
	if contactsExist && !hasContactRegion {
		previous = schemaWithoutColumn(previous, "contact_phones", "region")
	}
	if smsExists && !hasReportedPeer {
		previous = schemaWithoutColumn(previous, "sms", "reported_peer")
	}
	if linesExist && !hasLineHomeCountry {
		previous = schemaWithoutColumn(previous, "modemdeck_lines", "home_country_iso")
	}
	if subscriptionsExist && !hasHomeOperatorCode {
		previous = schemaWithoutColumn(
			previous,
			"sim_subscriptions",
			"home_operator_code",
		)
	}
	if subscriptionsExist && !hasSubscriptionHomeCountry {
		previous = schemaWithoutColumn(
			previous,
			"sim_subscriptions",
			"home_country_iso",
		)
	}
	if !schemaContains(previous, actual) {
		return false, nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin phone identity metadata migration: %w", err)
	}
	defer transaction.Rollback()
	if contactsExist && !hasContactRegion {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE contact_phones
			 ADD COLUMN region TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return false, fmt.Errorf("migrate contact phone region: %w", err)
		}
	}
	if smsExists && !hasReportedPeer {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms
			 ADD COLUMN reported_peer TEXT NOT NULL DEFAULT '';
			 UPDATE sms
			 SET reported_peer = peer
			 WHERE reported_peer = ''`,
		); err != nil {
			return false, fmt.Errorf("migrate reported SMS peer: %w", err)
		}
	}
	if linesExist && !hasLineHomeCountry {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_lines
			 ADD COLUMN home_country_iso TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return false, fmt.Errorf("migrate stable line home country: %w", err)
		}
	}
	if subscriptionsExist && !hasHomeOperatorCode {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sim_subscriptions
			 ADD COLUMN home_operator_code TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return false, fmt.Errorf("migrate SIM home operator code: %w", err)
		}
	}
	if subscriptionsExist && !hasSubscriptionHomeCountry {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sim_subscriptions
			 ADD COLUMN home_country_iso TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return false, fmt.Errorf("migrate SIM home country: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit phone identity metadata migration: %w", err)
	}
	return true, nil
}

func migrateCurrentCommunicationStateColumns(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	threadColumns, threadsExist := actual.tables["sms_contacts"]
	callColumns, callsExist := actual.tables["call_history"]
	recordingStateColumns, recordingStateExists :=
		actual.tables["modemdeck_call_recording_state"]
	if !threadsExist || !callsExist || !recordingStateExists {
		return false, nil
	}
	_, hasMarkedUnread := threadColumns["marked_unread"]
	_, hasMessageFavorite := threadColumns["is_favorite"]
	_, hasCallFavorite := callColumns["is_favorite"]
	_, hasRecordingFavorite := recordingStateColumns["is_favorite"]
	if hasMarkedUnread && hasMessageFavorite &&
		hasCallFavorite && hasRecordingFavorite {
		return false, nil
	}
	previous := expected
	if !hasMarkedUnread {
		previous = schemaWithoutColumn(previous, "sms_contacts", "marked_unread")
	}
	if !hasMessageFavorite {
		previous = schemaWithoutColumn(previous, "sms_contacts", "is_favorite")
	}
	if !hasCallFavorite {
		previous = schemaWithoutColumn(previous, "call_history", "is_favorite")
	}
	if !hasRecordingFavorite {
		previous = schemaWithoutColumn(
			previous,
			"modemdeck_call_recording_state",
			"is_favorite",
		)
	}
	if !schemaContains(previous, actual) {
		return false, nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin communication state migration: %w", err)
	}
	defer transaction.Rollback()
	if !hasMarkedUnread {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms_contacts
			 ADD COLUMN marked_unread NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return false, fmt.Errorf("migrate manual message unread state: %w", err)
		}
	}
	if !hasMessageFavorite {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms_contacts
			 ADD COLUMN is_favorite NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return false, fmt.Errorf("migrate message thread favorite state: %w", err)
		}
	}
	if !hasCallFavorite {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE call_history
			 ADD COLUMN is_favorite NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return false, fmt.Errorf("migrate call favorite state: %w", err)
		}
	}
	if !hasRecordingFavorite {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_call_recording_state
			 ADD COLUMN is_favorite NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return false, fmt.Errorf("migrate recording favorite state: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit communication state migration: %w", err)
	}
	return true, nil
}

func migrateCurrentSMSDeliveryColumns(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	smsColumns, smsExists := actual.tables["sms"]
	lineColumns, linesExist := actual.tables["modemdeck_lines"]
	smsColumnDefinitions := []struct {
		name       string
		definition string
	}{
		{
			name: "delivery_status",
			definition: `TEXT NOT NULL DEFAULT ''
				CHECK (delivery_status IN ('', 'submitted', 'delivered', 'failed'))`,
		},
		{name: "message_reference", definition: "INTEGER"},
		{name: "delivery_report_requested", definition: "NUMERIC NOT NULL DEFAULT 0"},
		{name: "delivery_report_trackable", definition: "NUMERIC NOT NULL DEFAULT 0"},
		{name: "delivery_report_code", definition: "INTEGER"},
	}
	lineColumnDefinitions := []struct {
		name       string
		definition string
	}{
		{name: "delivery_reports_enabled", definition: "NUMERIC NOT NULL DEFAULT 0"},
		{
			name: "delivery_reports_support",
			definition: `TEXT NOT NULL DEFAULT 'unknown'
				CHECK (delivery_reports_support IN ('unknown', 'unsupported'))`,
		},
		{
			name:       "message_policy_revision",
			definition: "INTEGER NOT NULL DEFAULT 1 CHECK (message_policy_revision > 0)",
		},
	}

	previous := expected
	needsMigration := false
	for _, column := range smsColumnDefinitions {
		if smsExists {
			if _, found := smsColumns[column.name]; !found {
				previous = schemaWithoutColumn(previous, "sms", column.name)
				needsMigration = true
			}
		}
	}
	for _, column := range lineColumnDefinitions {
		if linesExist {
			if _, found := lineColumns[column.name]; !found {
				previous = schemaWithoutColumn(previous, "modemdeck_lines", column.name)
				needsMigration = true
			}
		}
	}
	if !needsMigration {
		return false, nil
	}
	supported := schemaContains(previous, actual)
	if !supported && !linesExist {
		legacyPrevious := legacyV1SchemaShape(expected)
		for _, column := range smsColumnDefinitions {
			if _, found := smsColumns[column.name]; !found {
				legacyPrevious = schemaWithoutColumn(
					legacyPrevious,
					"sms",
					column.name,
				)
			}
		}
		supported = schemaContains(legacyPrevious, actual)
	}
	if !supported {
		return false, nil
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin SMS delivery migration: %w", err)
	}
	defer transaction.Rollback()
	for _, column := range smsColumnDefinitions {
		if _, found := smsColumns[column.name]; found {
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			"ALTER TABLE sms ADD COLUMN "+
				quoteIdentifier(column.name)+" "+column.definition,
		); err != nil {
			return false, fmt.Errorf("migrate SMS delivery column %s: %w", column.name, err)
		}
	}
	if _, found := smsColumns["delivery_status"]; !found {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE sms SET delivery_status = 'submitted' WHERE type = 2`,
		); err != nil {
			return false, fmt.Errorf("initialize outgoing SMS delivery state: %w", err)
		}
	}
	if linesExist {
		for _, column := range lineColumnDefinitions {
			if _, found := lineColumns[column.name]; found {
				continue
			}
			if _, err := transaction.ExecContext(
				ctx,
				"ALTER TABLE modemdeck_lines ADD COLUMN "+
					quoteIdentifier(column.name)+" "+column.definition,
			); err != nil {
				return false, fmt.Errorf(
					"migrate line message policy column %s: %w",
					column.name,
					err,
				)
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit SMS delivery migration: %w", err)
	}
	return true, nil
}

func schemaWithoutColumn(current schemaShape, table, column string) schemaShape {
	result := schemaShape{
		tables:  make(map[string]map[string]struct{}, len(current.tables)),
		indexes: make(map[string]struct{}, len(current.indexes)),
	}
	for tableName, columns := range current.tables {
		copyColumns := make(map[string]struct{}, len(columns))
		for columnName := range columns {
			copyColumns[columnName] = struct{}{}
		}
		result.tables[tableName] = copyColumns
	}
	for index := range current.indexes {
		result.indexes[index] = struct{}{}
	}
	delete(result.tables[table], column)
	return result
}

func legacyDeviceNameSchemaShape(current schemaShape) schemaShape {
	legacy := schemaShape{
		tables:  make(map[string]map[string]struct{}, len(current.tables)),
		indexes: make(map[string]struct{}, len(current.indexes)),
	}
	for table, columns := range current.tables {
		copyColumns := make(map[string]struct{}, len(columns))
		for column := range columns {
			copyColumns[column] = struct{}{}
		}
		legacy.tables[table] = copyColumns
	}
	for index := range current.indexes {
		legacy.indexes[index] = struct{}{}
	}
	delete(legacy.tables["devices"], "name")
	legacy.tables["devices"]["alias"] = struct{}{}
	return legacy
}

func migrateLegacySchemaAdditions(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) error {
	contactColumns, contactsExist := actual.tables["contacts"]
	_, avatarExists := contactColumns["avatar"]
	simCardColumns, simCardsExist := actual.tables["sim_cards"]
	_, lineColorExists := simCardColumns["line_color"]
	adminColumns, adminCredentialsExist := actual.tables["modemdeck_admin_credentials"]
	_, adminUsernameExists := adminColumns["username"]
	callColumns, callHistoryExists := actual.tables["call_history"]
	_, reportedRemoteNumberExists := callColumns["reported_remote_number"]
	_, callReadAtExists := callColumns["read_at"]
	_, callFavoriteExists := callColumns["is_favorite"]
	smsColumns, smsExists := actual.tables["sms"]
	_, smsDeletedAtExists := smsColumns["deleted_at"]
	_, smsReportedPeerExists := smsColumns["reported_peer"]
	threadColumns, messageThreadsExist := actual.tables["sms_contacts"]
	_, messageMarkedUnreadExists := threadColumns["marked_unread"]
	_, messageFavoriteExists := threadColumns["is_favorite"]
	recordingStateColumns, recordingStateExists :=
		actual.tables["modemdeck_call_recording_state"]
	_, recordingFavoriteExists := recordingStateColumns["is_favorite"]
	contactPhoneColumns, contactPhonesExist := actual.tables["contact_phones"]
	_, contactPhoneRegionExists := contactPhoneColumns["region"]
	lineColumns, linesExist := actual.tables["modemdeck_lines"]
	_, lineHomeCountryExists := lineColumns["home_country_iso"]
	subscriptionColumns, subscriptionsExist := actual.tables["sim_subscriptions"]
	_, homeOperatorCodeExists := subscriptionColumns["home_operator_code"]
	_, homeCountryISOExists := subscriptionColumns["home_country_iso"]
	deviceColumns, devicesExist := actual.tables["devices"]
	_, deviceNameExists := deviceColumns["name"]
	_, deviceAliasExists := deviceColumns["alias"]
	_, systemSettingsExist := actual.tables["modemdeck_system_settings"]
	needsAvatar := contactsExist && !avatarExists
	needsLineColor := simCardsExist && !lineColorExists
	needsAdminUsername := adminCredentialsExist && !adminUsernameExists
	needsReportedRemoteNumber := callHistoryExists && !reportedRemoteNumberExists
	needsCallReadAt := callHistoryExists && !callReadAtExists
	needsCallFavorite := callHistoryExists && !callFavoriteExists
	needsSMSDeletedAt := smsExists && !smsDeletedAtExists
	needsSMSReportedPeer := smsExists && !smsReportedPeerExists
	needsMessageMarkedUnread := messageThreadsExist && !messageMarkedUnreadExists
	needsMessageFavorite := messageThreadsExist && !messageFavoriteExists
	needsRecordingFavorite := recordingStateExists && !recordingFavoriteExists
	needsContactPhoneRegion := contactPhonesExist && !contactPhoneRegionExists
	needsLineHomeCountry := linesExist && !lineHomeCountryExists
	needsHomeOperatorCode := subscriptionsExist && !homeOperatorCodeExists
	needsHomeCountryISO := subscriptionsExist && !homeCountryISOExists
	needsDeviceName := devicesExist && !deviceNameExists && deviceAliasExists
	needsSystemSettings := !systemSettingsExist
	if !needsAvatar &&
		!needsLineColor &&
		!needsAdminUsername &&
		!needsReportedRemoteNumber &&
		!needsCallReadAt &&
		!needsCallFavorite &&
		!needsSMSDeletedAt &&
		!needsSMSReportedPeer &&
		!needsMessageMarkedUnread &&
		!needsMessageFavorite &&
		!needsRecordingFavorite &&
		!needsContactPhoneRegion &&
		!needsLineHomeCountry &&
		!needsHomeOperatorCode &&
		!needsHomeCountryISO &&
		!needsDeviceName &&
		!needsSystemSettings {
		return nil
	}
	if !schemaMatchesSupportedMigration(expected, actual) {
		return nil
	}
	if current, err := requiredRowsAreCurrent(ctx, database, legacyRequiredSchemaRows); err != nil {
		return err
	} else if !current {
		return nil
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer transaction.Rollback()
	if needsAvatar {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE contacts ADD COLUMN avatar TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return fmt.Errorf("migrate contacts avatar: %w", err)
		}
	}
	if needsLineColor {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sim_cards ADD COLUMN line_color TEXT NOT NULL DEFAULT ''
				CHECK (line_color IN (
					'', 'teal', 'blue', 'indigo', 'violet',
					'green', 'amber', 'orange', 'red'
				))`,
		); err != nil {
			return fmt.Errorf("migrate SIM line color: %w", err)
		}
	}
	if needsAdminUsername {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_admin_credentials
			 ADD COLUMN username TEXT NOT NULL DEFAULT 'admin'`,
		); err != nil {
			return fmt.Errorf("migrate administrator username: %w", err)
		}
	}
	if needsReportedRemoteNumber {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE call_history
			 ADD COLUMN reported_remote_number TEXT NOT NULL DEFAULT '';
			 UPDATE call_history
			 SET reported_remote_number = remote_number
			 WHERE reported_remote_number = ''`,
		); err != nil {
			return fmt.Errorf("migrate reported remote call number: %w", err)
		}
	}
	if needsCallReadAt {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE call_history ADD COLUMN read_at DATETIME`,
		); err != nil {
			return fmt.Errorf("migrate missed call read state: %w", err)
		}
	}
	if needsCallFavorite {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE call_history
			 ADD COLUMN is_favorite NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return fmt.Errorf("migrate call favorite state: %w", err)
		}
	}
	if needsSMSDeletedAt {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms ADD COLUMN deleted_at DATETIME`,
		); err != nil {
			return fmt.Errorf("migrate SMS deletion state: %w", err)
		}
	}
	if needsSMSReportedPeer {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms
			 ADD COLUMN reported_peer TEXT NOT NULL DEFAULT '';
			 UPDATE sms
			 SET reported_peer = peer
			 WHERE reported_peer = ''`,
		); err != nil {
			return fmt.Errorf("migrate reported SMS peer: %w", err)
		}
	}
	if needsMessageMarkedUnread {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms_contacts
			 ADD COLUMN marked_unread NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return fmt.Errorf("migrate manual message unread state: %w", err)
		}
	}
	if needsMessageFavorite {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms_contacts
			 ADD COLUMN is_favorite NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return fmt.Errorf("migrate message thread favorite state: %w", err)
		}
	}
	if needsRecordingFavorite {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_call_recording_state
			 ADD COLUMN is_favorite NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return fmt.Errorf("migrate recording favorite state: %w", err)
		}
	}
	if needsContactPhoneRegion {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE contact_phones
			 ADD COLUMN region TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return fmt.Errorf("migrate contact phone region: %w", err)
		}
	}
	if needsLineHomeCountry {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_lines
			 ADD COLUMN home_country_iso TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return fmt.Errorf("migrate stable line home country: %w", err)
		}
	}
	if needsHomeOperatorCode {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sim_subscriptions
			 ADD COLUMN home_operator_code TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return fmt.Errorf("migrate SIM home operator code: %w", err)
		}
	}
	if needsHomeCountryISO {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sim_subscriptions
			 ADD COLUMN home_country_iso TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return fmt.Errorf("migrate SIM home country: %w", err)
		}
	}
	if needsDeviceName {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE devices RENAME COLUMN alias TO name`,
		); err != nil {
			return fmt.Errorf("migrate device name: %w", err)
		}
	}
	if needsSystemSettings {
		if _, err := transaction.ExecContext(
			ctx,
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
			INSERT INTO modemdeck_system_settings (
				singleton, language, revision, updated_at
			) VALUES (1, 'auto', 1, CURRENT_TIMESTAMP);`,
		); err != nil {
			return fmt.Errorf("migrate system settings: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

func schemaContains(expected, actual schemaShape) bool {
	for table, expectedColumns := range expected.tables {
		actualColumns, exists := actual.tables[table]
		if !exists {
			return false
		}
		for column := range expectedColumns {
			if _, exists := actualColumns[column]; !exists {
				return false
			}
		}
	}
	for index := range expected.indexes {
		if _, exists := actual.indexes[index]; !exists {
			return false
		}
	}
	return true
}

func schemaShapesEqual(expected, actual schemaShape) bool {
	if len(expected.tables) != len(actual.tables) || len(expected.indexes) != len(actual.indexes) {
		return false
	}
	for table, expectedColumns := range expected.tables {
		actualColumns, exists := actual.tables[table]
		if !exists || len(expectedColumns) != len(actualColumns) {
			return false
		}
		for column := range expectedColumns {
			if _, exists := actualColumns[column]; !exists {
				return false
			}
		}
	}
	for index := range expected.indexes {
		if _, exists := actual.indexes[index]; !exists {
			return false
		}
	}
	return true
}

func legacyV1SchemaShape(current schemaShape) schemaShape {
	legacy := schemaShape{
		tables:  make(map[string]map[string]struct{}, len(current.tables)),
		indexes: make(map[string]struct{}, len(current.indexes)),
	}
	for table, columns := range current.tables {
		if table == "modemdeck_lines" || table == "modemdeck_legacy_endpoint_lines" {
			continue
		}
		copyColumns := make(map[string]struct{}, len(columns))
		for column := range columns {
			copyColumns[column] = struct{}{}
		}
		legacy.tables[table] = copyColumns
	}
	replaceColumns := func(table string, remove []string, add []string) {
		columns := legacy.tables[table]
		for _, column := range remove {
			delete(columns, column)
		}
		for _, column := range add {
			columns[column] = struct{}{}
		}
	}
	replaceColumns("contacts", []string{"preferred_line_id"}, []string{"preferred_device_imei"})
	replaceColumns("sms", []string{"endpoint_line_id"}, nil)
	replaceColumns("sms_contacts", []string{"line_id"}, nil)
	replaceColumns(
		"call_history",
		[]string{"line_id", "endpoint_line_id"},
		[]string{"device_id"},
	)
	replaceColumns(
		"modemdeck_line_settings",
		[]string{"default_line_id"},
		[]string{"default_device_imei"},
	)
	replaceColumns("modemdeck_incoming_call_actions", []string{"endpoint_line_id"}, nil)
	replaceColumns("devices", []string{"endpoint_id"}, nil)
	replaceColumns(
		"sim_cards",
		[]string{"line_id"},
		[]string{"line_label", "line_color"},
	)
	replaceColumns("sim_subscriptions", []string{"line_id"}, nil)
	replaceColumns(
		"modemdeck_network_counter_checkpoints",
		[]string{"endpoint_scope_id"},
		nil,
	)

	for index := range current.indexes {
		legacy.indexes[index] = struct{}{}
	}
	for _, index := range []string{
		"idx_contacts_preferred_line",
		"ux_sms_endpoint_line_message",
		"idx_call_history_line_ended_at",
		"idx_call_history_endpoint_line_ended_at",
		"ux_modemdeck_lines_phone_number",
		"ux_devices_endpoint_id",
		"ux_devices_current_iccid",
		"idx_sim_cards_line_id",
		"ux_sim_cards_current_imei",
		"idx_sim_subscriptions_line_id",
		"idx_sms_line_peer_timestamp",
		"idx_sms_line_peer_id",
		"idx_sms_incoming_unread_line_peer_id",
		"idx_sms_contacts_line_timestamp",
		"idx_sms_contacts_cursor",
		"idx_sms_contacts_line_cursor",
		"idx_call_history_line_ended_at_id",
	} {
		delete(legacy.indexes, index)
	}
	for _, index := range []string{
		"idx_contacts_preferred_device",
		"ux_sms_endpoint_message",
		"idx_call_history_device_ended_at",
	} {
		legacy.indexes[index] = struct{}{}
	}
	return legacy
}

func schemaMatchesSupportedMigration(expected schemaShape, actual schemaShape) bool {
	for table, expectedColumns := range expected.tables {
		actualColumns, exists := actual.tables[table]
		if !exists {
			if table == "modemdeck_system_settings" {
				continue
			}
			return false
		}
		for column := range expectedColumns {
			if table == "modemdeck_admin_credentials" && column == "username" {
				continue
			}
			if table == "contacts" && column == "avatar" {
				continue
			}
			if table == "sim_cards" && column == "line_color" {
				continue
			}
			if table == "call_history" && column == "reported_remote_number" {
				continue
			}
			if table == "call_history" && column == "read_at" {
				continue
			}
			if table == "call_history" && column == "is_favorite" {
				continue
			}
			if table == "sms" && column == "deleted_at" {
				continue
			}
			if table == "sms" && column == "reported_peer" {
				continue
			}
			if table == "sms_contacts" &&
				(column == "marked_unread" || column == "is_favorite") {
				continue
			}
			if table == "modemdeck_call_recording_state" &&
				column == "is_favorite" {
				continue
			}
			if table == "contact_phones" && column == "region" {
				continue
			}
			if table == "modemdeck_lines" && column == "home_country_iso" {
				continue
			}
			if table == "sim_subscriptions" &&
				(column == "home_operator_code" || column == "home_country_iso") {
				continue
			}
			if table == "devices" && column == "name" {
				if _, legacyNameExists := actualColumns["alias"]; legacyNameExists {
					continue
				}
			}
			if _, exists := actualColumns[column]; !exists {
				return false
			}
		}
	}
	for index := range expected.indexes {
		if _, exists := actual.indexes[index]; !exists {
			return false
		}
	}
	return true
}

func requiredRowsAreCurrent(
	ctx context.Context,
	database *sql.DB,
	requiredRows []struct {
		table string
		key   string
	},
) (bool, error) {
	for _, requiredRow := range requiredRows {
		var count int
		query := "SELECT COUNT(*) FROM " + quoteIdentifier(requiredRow.table) +
			" WHERE " + quoteIdentifier(requiredRow.key) + " = 1"
		if err := database.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return false, fmt.Errorf("validate required row in %s: %w", requiredRow.table, err)
		}
		if count != 1 {
			return false, nil
		}
	}
	return true, nil
}

var legacyRequiredSchemaRows = []struct {
	table string
	key   string
}{
	{table: "modemdeck_call_settings", key: "singleton"},
	{table: "modemdeck_line_settings", key: "singleton"},
	{table: "modemdeck_recording_settings", key: "singleton"},
}

var requiredSchemaRows = append(
	legacyRequiredSchemaRows,
	[]struct {
		table string
		key   string
	}{
		{table: "modemdeck_system_settings", key: "singleton"},
	}...,
)

func ValidateSchema(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("validate schema: nil database")
	}
	expected, err := expectedSchemaShape(ctx)
	if err != nil {
		return err
	}
	actual, err := readSchemaShape(ctx, database)
	if err != nil {
		return err
	}
	for table, expectedColumns := range expected.tables {
		actualColumns, exists := actual.tables[table]
		if !exists {
			return fmt.Errorf("%w: table %s is missing", ErrSchemaOutdated, table)
		}
		for column := range expectedColumns {
			if _, exists := actualColumns[column]; !exists {
				return fmt.Errorf(
					"%w: column %s.%s is missing",
					ErrSchemaOutdated,
					table,
					column,
				)
			}
		}
	}
	for index := range expected.indexes {
		if _, exists := actual.indexes[index]; !exists {
			return fmt.Errorf("%w: index %s is missing", ErrSchemaOutdated, index)
		}
	}
	for _, requiredRow := range requiredSchemaRows {
		var count int
		query := "SELECT COUNT(*) FROM " + quoteIdentifier(requiredRow.table) +
			" WHERE " + quoteIdentifier(requiredRow.key) + " = 1"
		if err := database.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("validate required row in %s: %w", requiredRow.table, err)
		}
		if count != 1 {
			return fmt.Errorf(
				"%w: required row %s.%s=1 is missing",
				ErrSchemaOutdated,
				requiredRow.table,
				requiredRow.key,
			)
		}
	}
	return nil
}

func createSchema(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema creation: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, currentSchemaSQL); err != nil {
		return fmt.Errorf("create current schema: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit schema creation: %w", err)
	}
	return nil
}

func expectedSchemaShape(ctx context.Context) (schemaShape, error) {
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return schemaShape{}, fmt.Errorf("open schema reference database: %w", err)
	}
	reference.SetMaxOpenConns(1)
	defer reference.Close()
	if _, err := reference.ExecContext(ctx, currentSchemaSQL); err != nil {
		return schemaShape{}, fmt.Errorf("build schema reference database: %w", err)
	}
	return readSchemaShape(ctx, reference)
}

func databaseIsEmpty(ctx context.Context, database *sql.DB) (bool, error) {
	var count int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM sqlite_schema
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect database schema: %w", err)
	}
	return count == 0, nil
}

func readSchemaShape(ctx context.Context, database *sql.DB) (schemaShape, error) {
	shape := schemaShape{
		tables:  make(map[string]map[string]struct{}),
		indexes: make(map[string]struct{}),
	}
	tableRows, err := database.QueryContext(
		ctx,
		`SELECT name
		 FROM sqlite_schema
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`,
	)
	if err != nil {
		return schemaShape{}, fmt.Errorf("list schema tables: %w", err)
	}
	tableNames := make([]string, 0)
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			_ = tableRows.Close()
			return schemaShape{}, fmt.Errorf("scan schema table: %w", err)
		}
		tableNames = append(tableNames, name)
	}
	if err := tableRows.Err(); err != nil {
		_ = tableRows.Close()
		return schemaShape{}, fmt.Errorf("read schema tables: %w", err)
	}
	if err := tableRows.Close(); err != nil {
		return schemaShape{}, fmt.Errorf("close schema tables: %w", err)
	}
	for _, table := range tableNames {
		columns, err := tableColumns(ctx, database, table)
		if err != nil {
			return schemaShape{}, err
		}
		shape.tables[table] = columns
	}

	indexRows, err := database.QueryContext(
		ctx,
		`SELECT name
		 FROM sqlite_schema
		 WHERE type = 'index' AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`,
	)
	if err != nil {
		return schemaShape{}, fmt.Errorf("list schema indexes: %w", err)
	}
	defer indexRows.Close()
	for indexRows.Next() {
		var name string
		if err := indexRows.Scan(&name); err != nil {
			return schemaShape{}, fmt.Errorf("scan schema index: %w", err)
		}
		shape.indexes[name] = struct{}{}
	}
	if err := indexRows.Err(); err != nil {
		return schemaShape{}, fmt.Errorf("read schema indexes: %w", err)
	}
	return shape, nil
}

func tableColumns(
	ctx context.Context,
	database *sql.DB,
	table string,
) (map[string]struct{}, error) {
	rows, err := database.QueryContext(
		ctx,
		"PRAGMA table_info("+quoteIdentifier(table)+")",
	)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			sequence     int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(
			&sequence,
			&name,
			&columnType,
			&notNull,
			&defaultValue,
			&primaryKey,
		); err != nil {
			return nil, fmt.Errorf("scan table metadata for %s: %w", table, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read table metadata for %s: %w", table, err)
	}
	return columns, nil
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
