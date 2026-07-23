package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestTelegramUnitsPreserveIndependentEncryptedConfigurations(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	first, err := repository.CreateTelegramUnit(ctx, TelegramUnitRecord{
		ID:                 "telegram-unit-1",
		DisplayName:        "Primary",
		Enabled:            true,
		BotID:              123456,
		BotTokenNonce:      []byte("nonce-1"),
		BotTokenCiphertext: []byte("ciphertext-1"),
		TokenHint:          "123456",
		ChatID:             -1001,
		AdminID:            42,
		LineScopes:         []string{"line-b", "line-a"},
		IncomingSMS:        true,
		MissedCalls:        false,
	})
	if err != nil {
		t.Fatalf("CreateTelegramUnit(first) error = %v", err)
	}
	second, err := repository.CreateTelegramUnit(ctx, TelegramUnitRecord{
		ID:                 "telegram-unit-2",
		DisplayName:        "Secondary",
		Enabled:            false,
		BotID:              654321,
		BotTokenNonce:      []byte("nonce-2"),
		BotTokenCiphertext: []byte("ciphertext-2"),
		TokenHint:          "654321",
		ChatID:             -1002,
		AdminID:            43,
		IncomingSMS:        false,
		MissedCalls:        true,
	})
	if err != nil {
		t.Fatalf("CreateTelegramUnit(second) error = %v", err)
	}
	if first.Revision != 1 || second.Revision != 1 {
		t.Fatalf("created revisions = %d, %d", first.Revision, second.Revision)
	}

	units, err := repository.TelegramUnits(ctx)
	if err != nil {
		t.Fatalf("TelegramUnits() error = %v", err)
	}
	if len(units) != 2 || units[0].ID != first.ID || units[1].ID != second.ID {
		t.Fatalf("units = %+v", units)
	}
	if !bytes.Equal(units[0].BotTokenCiphertext, []byte("ciphertext-1")) ||
		len(units[0].LineScopes) != 2 ||
		units[0].LineScopes[0] != "line-a" ||
		units[0].LineScopes[1] != "line-b" {
		t.Fatalf("first unit = %+v", units[0])
	}

	first.DisplayName = "Primary bot"
	first.LineScopes = []string{"line-c"}
	first.NextUpdateOffset = 99
	updated, err := repository.UpdateTelegramUnit(ctx, first, first.Revision)
	if err != nil {
		t.Fatalf("UpdateTelegramUnit() error = %v", err)
	}
	if updated.Revision != 2 || updated.DisplayName != "Primary bot" ||
		updated.NextUpdateOffset != 99 ||
		len(updated.LineScopes) != 1 || updated.LineScopes[0] != "line-c" {
		t.Fatalf("updated unit = %+v", updated)
	}
	if _, err := repository.UpdateTelegramUnit(ctx, first, first.Revision); !errors.Is(err, ErrTelegramUnitRevisionConflict) {
		t.Fatalf("stale UpdateTelegramUnit() error = %v", err)
	}
}

func TestTelegramCheckpointReplyBindingAndDelete(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	unit, err := repository.CreateTelegramUnit(ctx, TelegramUnitRecord{
		ID:                 "telegram-unit-1",
		DisplayName:        "Primary",
		BotID:              123456,
		BotTokenNonce:      []byte("nonce"),
		BotTokenCiphertext: []byte("ciphertext"),
		ChatID:             -1001,
		AdminID:            42,
	})
	if err != nil {
		t.Fatalf("CreateTelegramUnit() error = %v", err)
	}
	if err := repository.AdvanceTelegramOffset(ctx, unit.ID, 15); err != nil {
		t.Fatalf("AdvanceTelegramOffset() error = %v", err)
	}
	if err := repository.AdvanceTelegramOffset(ctx, unit.ID, 14); !errors.Is(err, ErrTelegramCheckpointRegression) {
		t.Fatalf("regressing AdvanceTelegramOffset() error = %v", err)
	}
	offset, err := repository.TelegramNextOffset(ctx, unit.ID)
	if err != nil || offset != 15 {
		t.Fatalf("TelegramNextOffset() = %d, error = %v", offset, err)
	}

	binding := TelegramReplyBinding{LineID: "line-1", Number: "+818012345678"}
	if err := repository.BindTelegramReply(ctx, unit.BotID, unit.ChatID, 7, binding); err != nil {
		t.Fatalf("BindTelegramReply() error = %v", err)
	}
	resolved, err := repository.ResolveTelegramReply(ctx, unit.BotID, unit.ChatID, 7)
	if err != nil || resolved != binding {
		t.Fatalf("ResolveTelegramReply() = %+v, error = %v", resolved, err)
	}
	if err := repository.PurgeTelegramReplyBindings(ctx, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("PurgeTelegramReplyBindings() error = %v", err)
	}
	if _, err := repository.ResolveTelegramReply(ctx, unit.BotID, unit.ChatID, 7); !errors.Is(err, ErrTelegramReplyBindingNotFound) {
		t.Fatalf("ResolveTelegramReply() after purge error = %v", err)
	}

	if err := repository.DeleteTelegramUnit(ctx, unit.ID, unit.Revision); err != nil {
		t.Fatalf("DeleteTelegramUnit() error = %v", err)
	}
	if _, err := repository.TelegramUnit(ctx, unit.ID); !errors.Is(err, ErrTelegramUnitNotFound) {
		t.Fatalf("TelegramUnit() after delete error = %v", err)
	}
}
