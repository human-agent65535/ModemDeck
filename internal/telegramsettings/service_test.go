package telegramsettings

import (
	"bytes"
	"context"
	"database/sql"
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
const testOwnerUserID = "user_admin"

func TestServiceNotifiesCommittedAccessChanges(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	service.NotifyAccessChanged()
	assertChangeSignal(t, service)
}

func TestServiceKeepsBotTokenWriteOnlyAndEncrypted(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	created, err := service.Create(ctx, CreateInput{
		DisplayName:    "Primary",
		Enabled:        true,
		BotToken:       testBotToken,
		ChatID:         "-100123",
		AdminID:        "42",
		AssignedUserID: testOwnerUserID,
		IncomingSMS:    true,
		MissedCalls:    true,
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
		Revision:       created.Revision,
		DisplayName:    "Primary bot",
		Enabled:        true,
		ChatID:         created.ChatID,
		AdminID:        created.AdminID,
		AssignedUserID: testOwnerUserID,
		LineScopes:     created.LineScopes,
		IncomingSMS:    true,
		MissedCalls:    false,
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
		DisplayName:    "Primary",
		Enabled:        true,
		BotToken:       testBotToken,
		ChatID:         "-100123",
		AdminID:        "42",
		AssignedUserID: testOwnerUserID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Create(ctx, CreateInput{
		DisplayName:    "Duplicate",
		Enabled:        false,
		BotToken:       testBotToken,
		ChatID:         "-100124",
		AdminID:        "43",
		AssignedUserID: testOwnerUserID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want conflict", err)
	}

	empty := ""
	_, err = service.Update(ctx, created.ID, UpdateInput{
		Revision:       created.Revision,
		DisplayName:    created.DisplayName,
		Enabled:        true,
		BotToken:       &empty,
		ChatID:         created.ChatID,
		AdminID:        created.AdminID,
		AssignedUserID: testOwnerUserID,
		LineScopes:     created.LineScopes,
		IncomingSMS:    created.IncomingSMS,
		MissedCalls:    created.MissedCalls,
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
		DisplayName:    "Invalid",
		Enabled:        true,
		BotToken:       testBotToken,
		ChatID:         "not-a-chat",
		AdminID:        "42",
		AssignedUserID: testOwnerUserID,
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid chat Create() error = %v", err)
	}
	created, err := service.Create(ctx, CreateInput{
		DisplayName:    "Disabled",
		AssignedUserID: testOwnerUserID,
	})
	if err != nil {
		t.Fatalf("disabled Create() error = %v", err)
	}
	_, err = service.Update(ctx, created.ID, UpdateInput{
		Revision:       created.Revision + 1,
		DisplayName:    created.DisplayName,
		AssignedUserID: testOwnerUserID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Update() error = %v, want conflict", err)
	}
}

func TestServiceAppliesUserOwnedLineScopes(t *testing.T) {
	t.Parallel()

	service, repository, sqlite := newTestServiceWithRepository(t)
	ctx := context.Background()
	for _, lineID := range []string{"line_alpha", "line_beta"} {
		if _, err := sqlite.Exec(
			`INSERT INTO modemdeck_lines (line_id, line_label) VALUES (?, ?)`,
			lineID,
			lineID,
		); err != nil {
			t.Fatalf("insert line %s: %v", lineID, err)
		}
	}
	member, err := repository.CreateMember(ctx, store.CreateMemberInput{
		Username:     "telegram-member",
		PasswordHash: "member-hash",
		LineIDs:      []string{"line_alpha"},
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}

	userUnit, err := service.Create(ctx, CreateInput{
		DisplayName:    "Member bot",
		Enabled:        true,
		BotToken:       testBotToken,
		ChatID:         "-100123",
		AdminID:        "42",
		AssignedUserID: member.ID,
		IncomingSMS:    true,
		MissedCalls:    true,
	})
	if err != nil {
		t.Fatalf("Create(all assigned lines) error = %v", err)
	}
	if !userUnit.AllAssignedLines {
		t.Fatal("user unit does not follow all assigned lines")
	}
	assertChangeSignal(t, service)
	userConfig, err := service.RuntimeConfig(ctx, userUnit.ID)
	if err != nil {
		t.Fatalf("RuntimeConfig(all assigned lines) error = %v", err)
	}
	if !userConfig.Enabled ||
		userConfig.LineScopeMode != "selected" ||
		len(userConfig.LineScopes) != 1 ||
		userConfig.LineScopes[0] != "line_alpha" ||
		!userConfig.ResolveContacts ||
		userConfig.Principal == nil ||
		userConfig.Principal.UserID != member.ID {
		t.Fatalf("all-assigned runtime config = %+v", userConfig)
	}

	member, err = repository.UpdateMember(ctx, member.ID, store.UpdateMemberInput{
		Username: member.Username,
		Enabled:  true,
		LineIDs:  []string{"line_alpha", "line_beta"},
		Revision: member.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateMember() error = %v", err)
	}
	userConfig, err = service.RuntimeConfig(ctx, userUnit.ID)
	if err != nil {
		t.Fatalf("RuntimeConfig(after assignment) error = %v", err)
	}
	if len(userConfig.LineScopes) != 2 ||
		userConfig.LineScopes[0] != "line_alpha" ||
		userConfig.LineScopes[1] != "line_beta" ||
		userConfig.Principal == nil ||
		len(userConfig.Principal.AllowedLineIDs) != 2 {
		t.Fatalf("updated all-assigned runtime config = %+v", userConfig)
	}

	selectedUnit, err := service.Create(ctx, CreateInput{
		DisplayName:    "Selected lines bot",
		Enabled:        true,
		BotToken:       "987654321:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi",
		ChatID:         "-100124",
		AdminID:        "43",
		AssignedUserID: member.ID,
		LineScopes:     []string{"line_beta"},
		IncomingSMS:    true,
		MissedCalls:    true,
	})
	if err != nil {
		t.Fatalf("Create(selected lines) error = %v", err)
	}
	if selectedUnit.AllAssignedLines {
		t.Fatal("selected-line unit follows all assigned lines")
	}
	assertChangeSignal(t, service)
	selectedConfig, err := service.RuntimeConfig(ctx, selectedUnit.ID)
	if err != nil {
		t.Fatalf("RuntimeConfig(selected lines) error = %v", err)
	}
	if !selectedConfig.Enabled ||
		selectedConfig.LineScopeMode != "selected" ||
		len(selectedConfig.LineScopes) != 1 ||
		selectedConfig.LineScopes[0] != "line_beta" ||
		!selectedConfig.ResolveContacts ||
		selectedConfig.Principal == nil ||
		selectedConfig.Principal.UserID != member.ID {
		t.Fatalf("selected-line runtime config = %+v", selectedConfig)
	}
	if _, err := service.Create(ctx, CreateInput{
		DisplayName:    "Unassigned line",
		AssignedUserID: member.ID,
		LineScopes:     []string{"line_gamma"},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Create(unassigned line) error = %v, want invalid argument", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, _, _ := newTestServiceWithRepository(t)
	return service
}

func newTestServiceWithRepository(t *testing.T) (*Service, *store.Store, *sql.DB) {
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
	if _, err := sqlite.Exec(`
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled
		) VALUES (?, 'admin', 'admin-hash', 'admin', 1);
		INSERT INTO modemdeck_user_preferences (user_id) VALUES (?)
	`, testOwnerUserID, testOwnerUserID); err != nil {
		t.Fatalf("seed test owner: %v", err)
	}
	box, err := secretbox.New([]byte(strings.Repeat("k", secretbox.KeySize)))
	if err != nil {
		t.Fatalf("secretbox.New() error = %v", err)
	}
	service, err := New(repository, box)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service, repository, sqlite
}

func assertChangeSignal(t *testing.T, service *Service) {
	t.Helper()
	select {
	case <-service.Changes():
	default:
		t.Fatal("settings mutation did not emit a change signal")
	}
}
