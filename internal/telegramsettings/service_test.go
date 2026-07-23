package telegramsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/secretbox"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const testBotToken = "123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestServiceKeepsBotTokenWriteOnlyAndEncrypted(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	created, err := service.Create(ctx, CreateInput{
		DisplayName: "Primary",
		Enabled:     true,
		BotToken:    testBotToken,
		ChatID:      "-100123",
		AdminID:     "42",
		LineScopes:  []string{"line-1"},
		IncomingSMS: true,
		MissedCalls: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.TokenConfigured || created.TokenHint != "123456789" || created.Revision != 1 {
		t.Fatalf("created unit = %+v", created)
	}
	assertChangeSignal(t, service)
	serialized, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal unit: %v", err)
	}
	if bytes.Contains(serialized, []byte(testBotToken)) {
		t.Fatalf("public unit leaked token: %s", serialized)
	}

	config, err := service.RuntimeConfig(ctx, created.ID)
	if err != nil {
		t.Fatalf("RuntimeConfig() error = %v", err)
	}
	if config.BotToken != testBotToken || config.ChatID != -100123 ||
		config.AdminID != 42 || !config.Enabled {
		t.Fatalf("runtime config = %+v", config)
	}

	updated, err := service.Update(ctx, created.ID, UpdateInput{
		Revision:    created.Revision,
		DisplayName: "Primary bot",
		Enabled:     true,
		ChatID:      created.ChatID,
		AdminID:     created.AdminID,
		LineScopes:  created.LineScopes,
		IncomingSMS: true,
		MissedCalls: false,
	})
	if err != nil {
		t.Fatalf("Update() without token error = %v", err)
	}
	config, err = service.RuntimeConfig(ctx, created.ID)
	if err != nil || config.BotToken != testBotToken {
		t.Fatalf("RuntimeConfig() after update token = %q, error = %v", config.BotToken, err)
	}
	if updated.Revision != 2 || updated.DisplayName != "Primary bot" || updated.MissedCalls {
		t.Fatalf("updated unit = %+v", updated)
	}
	assertChangeSignal(t, service)
}

func TestServiceRejectsDuplicateBotAndUnsafeCredentialClear(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	created, err := service.Create(ctx, CreateInput{
		DisplayName: "Primary",
		Enabled:     true,
		BotToken:    testBotToken,
		ChatID:      "-100123",
		AdminID:     "42",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Create(ctx, CreateInput{
		DisplayName: "Duplicate",
		Enabled:     false,
		BotToken:    testBotToken,
		ChatID:      "-100124",
		AdminID:     "43",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want conflict", err)
	}

	empty := ""
	_, err = service.Update(ctx, created.ID, UpdateInput{
		Revision:    created.Revision,
		DisplayName: created.DisplayName,
		Enabled:     true,
		BotToken:    &empty,
		ChatID:      created.ChatID,
		AdminID:     created.AdminID,
		LineScopes:  created.LineScopes,
		IncomingSMS: created.IncomingSMS,
		MissedCalls: created.MissedCalls,
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("enabled credential clear error = %v, want invalid argument", err)
	}
}

func TestServiceRequiresExactIdentifiersAndRevision(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	_, err := service.Create(ctx, CreateInput{
		DisplayName: "Invalid",
		Enabled:     true,
		BotToken:    testBotToken,
		ChatID:      "not-a-chat",
		AdminID:     "42",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid chat Create() error = %v", err)
	}
	created, err := service.Create(ctx, CreateInput{DisplayName: "Disabled"})
	if err != nil {
		t.Fatalf("disabled Create() error = %v", err)
	}
	_, err = service.Update(ctx, created.ID, UpdateInput{
		Revision:    created.Revision + 1,
		DisplayName: created.DisplayName,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Update() error = %v, want conflict", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	sqlite, err := database.Open(context.Background(), database.Config{
		TargetPath: filepath.Join(directory, "modemdeck.db"),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	repository, err := store.New(sqlite)
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	box, err := secretbox.New([]byte(strings.Repeat("k", secretbox.KeySize)))
	if err != nil {
		t.Fatalf("secretbox.New() error = %v", err)
	}
	service, err := New(repository, box)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service
}

func assertChangeSignal(t *testing.T, service *Service) {
	t.Helper()
	select {
	case <-service.Changes():
	default:
		t.Fatal("settings mutation did not emit a change signal")
	}
}
