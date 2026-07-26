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
		return false, ValidateSchema(ctx, database)
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
	contactColumns, contactsExist := actual.tables["contacts"]
	_, avatarExists := contactColumns["avatar"]
	_, systemSettingsExist := actual.tables["modemdeck_system_settings"]
	needsAvatar := contactsExist && !avatarExists
	needsSystemSettings := !systemSettingsExist
	if !needsAvatar && !needsSystemSettings {
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
			if table == "contacts" && column == "avatar" {
				continue
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
