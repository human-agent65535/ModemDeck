package store

import (
	"context"
	"path/filepath"
	"testing"

	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
)

func TestEmptyDatabaseReturnsEmptyCollections(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	database, _, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(directory, "modemdeck.db"),
		LegacyPath: filepath.Join(directory, "vohive.db"),
	})
	if err != nil {
		t.Fatalf("open empty database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	contacts, err := repository.Contacts(context.Background(), ContactQuery{})
	assertEmpty(t, "contacts", len(contacts), contacts != nil, err)
	threads, err := repository.MessageThreads(context.Background(), ThreadQuery{})
	assertEmpty(t, "threads", len(threads), threads != nil, err)
	messages, err := repository.Messages(context.Background(), MessageQuery{})
	assertEmpty(t, "messages", len(messages), messages != nil, err)
	calls, err := repository.Calls(context.Background(), CallQuery{})
	assertEmpty(t, "calls", len(calls), calls != nil, err)
	devices, err := repository.Devices(context.Background())
	assertEmpty(t, "devices", len(devices), devices != nil, err)
	lines, err := repository.Lines(context.Background())
	assertEmpty(t, "lines", len(lines), lines != nil, err)
}

func TestBoundedLimit(t *testing.T) {
	t.Parallel()

	if got := boundedLimit(0); got != DefaultQueryLimit {
		t.Fatalf("boundedLimit(0) = %d, want %d", got, DefaultQueryLimit)
	}
	if got := boundedLimit(MaxQueryLimit + 1); got != MaxQueryLimit {
		t.Fatalf("boundedLimit(max+1) = %d, want %d", got, MaxQueryLimit)
	}
}

func assertEmpty(t *testing.T, name string, length int, nonNil bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	if !nonNil {
		t.Fatalf("%s result is nil", name)
	}
	if length != 0 {
		t.Fatalf("%s length = %d, want 0", name, length)
	}
}
