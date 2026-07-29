package database

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/phone"
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
		if err := migratePhoneIdentities(ctx, database); err != nil {
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
	if schemaContains(expected, actual) {
		return nil
	}
	legacyExpected := legacyV1SchemaShape(expected)
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
	return migrateStableLineIdentity(ctx, database)
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
	smsColumns, smsExists := actual.tables["sms"]
	_, smsDeletedAtExists := smsColumns["deleted_at"]
	deviceColumns, devicesExist := actual.tables["devices"]
	_, deviceNameExists := deviceColumns["name"]
	_, deviceAliasExists := deviceColumns["alias"]
	_, systemSettingsExist := actual.tables["modemdeck_system_settings"]
	needsAvatar := contactsExist && !avatarExists
	needsLineColor := simCardsExist && !lineColorExists
	needsAdminUsername := adminCredentialsExist && !adminUsernameExists
	needsReportedRemoteNumber := callHistoryExists && !reportedRemoteNumberExists
	needsCallReadAt := callHistoryExists && !callReadAtExists
	needsSMSDeletedAt := smsExists && !smsDeletedAtExists
	needsDeviceName := devicesExist && !deviceNameExists && deviceAliasExists
	needsSystemSettings := !systemSettingsExist
	if !needsAvatar &&
		!needsLineColor &&
		!needsAdminUsername &&
		!needsReportedRemoteNumber &&
		!needsCallReadAt &&
		!needsSMSDeletedAt &&
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
			 ADD COLUMN reported_remote_number TEXT NOT NULL DEFAULT ''`,
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
	if needsSMSDeletedAt {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE sms ADD COLUMN deleted_at DATETIME`,
		); err != nil {
			return fmt.Errorf("migrate SMS deletion state: %w", err)
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
					CHECK (language IN ('auto', 'zh-CN', 'en-US')),
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
			if table == "sms" && column == "deleted_at" {
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

func migratePhoneIdentities(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin phone identity migration: %w", err)
	}
	defer transaction.Rollback()

	rows, err := transaction.QueryContext(
		ctx,
		`SELECT id, remote_number, reported_remote_number
		 FROM call_history`,
	)
	if err != nil {
		return fmt.Errorf("read call phone identities: %w", err)
	}
	type callIdentity struct {
		id        string
		canonical string
		reported  string
	}
	calls := make([]callIdentity, 0)
	for rows.Next() {
		var id, remote, reported string
		if err := rows.Scan(&id, &remote, &reported); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan call phone identity: %w", err)
		}
		originalReported := reported
		if strings.TrimSpace(reported) == "" {
			reported = remote
		}
		canonical := phone.NormalizeNetworkNumber(remote)
		reported = strings.TrimSpace(reported)
		if canonical == remote && reported == originalReported {
			continue
		}
		calls = append(calls, callIdentity{
			id:        id,
			canonical: canonical,
			reported:  reported,
		})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close call phone identities: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read call phone identities: %w", err)
	}
	for _, call := range calls {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE call_history
			 SET remote_number = ?, reported_remote_number = ?
			 WHERE id = ?`,
			call.canonical,
			call.reported,
			call.id,
		); err != nil {
			return fmt.Errorf("migrate call phone identity: %w", err)
		}
	}

	phoneRows, err := transaction.QueryContext(
		ctx,
		`SELECT id, original_number, canonical_e164
		 FROM contact_phones`,
	)
	if err != nil {
		return fmt.Errorf("read contact phone identities: %w", err)
	}
	type contactIdentity struct {
		id        string
		canonical string
	}
	contacts := make([]contactIdentity, 0)
	for phoneRows.Next() {
		var id, original, canonical string
		if err := phoneRows.Scan(&id, &original, &canonical); err != nil {
			_ = phoneRows.Close()
			return fmt.Errorf("scan contact phone identity: %w", err)
		}
		_, normalized, normalizeErr := phone.Normalize(original)
		if normalizeErr == nil && normalized != canonical {
			contacts = append(contacts, contactIdentity{id: id, canonical: normalized})
		}
	}
	if err := phoneRows.Close(); err != nil {
		return fmt.Errorf("close contact phone identities: %w", err)
	}
	if err := phoneRows.Err(); err != nil {
		return fmt.Errorf("read contact phone identities: %w", err)
	}
	for _, contact := range contacts {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE contact_phones SET canonical_e164 = ? WHERE id = ?`,
			contact.canonical,
			contact.id,
		); err != nil {
			return fmt.Errorf("migrate contact phone identity: %w", err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit phone identity migration: %w", err)
	}
	return nil
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
	struct {
		table string
		key   string
	}{table: "modemdeck_system_settings", key: "singleton"},
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
