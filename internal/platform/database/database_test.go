package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenConfiguresEveryPooledConnection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	database, err := Open(ctx, Config{
		TargetPath: filepath.Join(t.TempDir(), "modemdeck pool.db"),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if got := database.Stats().MaxOpenConnections; got != defaultMaxOpenConnections {
		t.Fatalf(
			"MaxOpenConnections = %d, want %d",
			got,
			defaultMaxOpenConnections,
		)
	}

	connections := make([]*sql.Conn, 0, defaultMaxOpenConnections)
	for index := 0; index < defaultMaxOpenConnections; index++ {
		connection, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection %d: %v", index, err)
		}
		connections = append(connections, connection)
	}
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	})

	for index, connection := range connections {
		assertSQLitePragmaInt(t, ctx, connection, index, "busy_timeout", sqliteBusyTimeoutMillis)
		assertSQLitePragmaInt(t, ctx, connection, index, "foreign_keys", 1)
		assertSQLitePragmaInt(t, ctx, connection, index, "synchronous", 1)

		var journalMode string
		if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("connection %d read journal_mode: %v", index, err)
		}
		if !strings.EqualFold(journalMode, "wal") {
			t.Fatalf("connection %d journal_mode = %q, want WAL", index, journalMode)
		}
	}
}

func TestOpenAllowsWriteWhileReadTransactionIsActive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	database, err := Open(ctx, Config{
		TargetPath: filepath.Join(t.TempDir(), "modemdeck.db"),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE pool_concurrency_probe (
			id INTEGER PRIMARY KEY
		)
	`); err != nil {
		t.Fatalf("create concurrency probe: %v", err)
	}

	readConnection, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire read connection: %v", err)
	}
	defer readConnection.Close()
	readTransaction, err := readConnection.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read transaction: %v", err)
	}
	defer readTransaction.Rollback()

	var count int
	if err := readTransaction.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM pool_concurrency_probe",
	).Scan(&count); err != nil {
		t.Fatalf("establish read snapshot: %v", err)
	}
	if count != 0 {
		t.Fatalf("initial row count = %d, want 0", count)
	}

	writeCtx, cancelWrite := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWrite()
	if _, err := database.ExecContext(
		writeCtx,
		"INSERT INTO pool_concurrency_probe (id) VALUES (1)",
	); err != nil {
		t.Fatalf("write while read transaction is active: %v", err)
	}

	if err := readTransaction.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM pool_concurrency_probe",
	).Scan(&count); err != nil {
		t.Fatalf("read original snapshot: %v", err)
	}
	if count != 0 {
		t.Fatalf("snapshot row count = %d, want 0", count)
	}
	if err := readTransaction.Commit(); err != nil {
		t.Fatalf("commit read transaction: %v", err)
	}

	if err := database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM pool_concurrency_probe",
	).Scan(&count); err != nil {
		t.Fatalf("read committed write: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed row count = %d, want 1", count)
	}
}

func assertSQLitePragmaInt(
	t *testing.T,
	ctx context.Context,
	connection *sql.Conn,
	connectionIndex int,
	pragma string,
	want int,
) {
	t.Helper()

	var got int
	if err := connection.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
		t.Fatalf("connection %d read %s: %v", connectionIndex, pragma, err)
	}
	if got != want {
		t.Fatalf("connection %d %s = %d, want %d", connectionIndex, pragma, got, want)
	}
}
