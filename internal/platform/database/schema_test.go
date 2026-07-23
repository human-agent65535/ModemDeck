package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
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
