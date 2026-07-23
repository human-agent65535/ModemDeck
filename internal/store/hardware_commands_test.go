package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestHardwareCommandLedgerPreventsMutationReplay(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	digest := bytes.Repeat([]byte{0x11}, 32)
	command, created, err := repository.BeginHardwareCommand(
		ctx,
		"request-call-1",
		"start_call",
		digest,
	)
	if err != nil || !created || command.Status != HardwareCommandPending {
		t.Fatalf("BeginHardwareCommand() = %+v, created = %v, error = %v", command, created, err)
	}
	replayed, created, err := repository.BeginHardwareCommand(
		ctx,
		"request-call-1",
		"start_call",
		digest,
	)
	if err != nil || created || replayed.Status != HardwareCommandPending {
		t.Fatalf("replayed BeginHardwareCommand() = %+v, created = %v, error = %v", replayed, created, err)
	}
	if _, _, err := repository.BeginHardwareCommand(
		ctx,
		"request-call-1",
		"start_call",
		bytes.Repeat([]byte{0x22}, 32),
	); !errors.Is(err, ErrHardwareCommandConflict) {
		t.Fatalf("conflicting BeginHardwareCommand() error = %v", err)
	}

	if err := repository.FinishHardwareCommand(
		ctx,
		command.RequestID,
		HardwareCommandCompleted,
		"call-app-1",
		"",
	); err != nil {
		t.Fatalf("FinishHardwareCommand() error = %v", err)
	}
	if err := repository.FinishHardwareCommand(
		ctx,
		command.RequestID,
		HardwareCommandCompleted,
		"call-app-1",
		"",
	); err != nil {
		t.Fatalf("idempotent FinishHardwareCommand() error = %v", err)
	}
	finished, err := repository.HardwareCommand(ctx, command.RequestID)
	if err != nil {
		t.Fatalf("HardwareCommand() error = %v", err)
	}
	if finished.Status != HardwareCommandCompleted || finished.ResourceID != "call-app-1" {
		t.Fatalf("finished command = %+v", finished)
	}
}
