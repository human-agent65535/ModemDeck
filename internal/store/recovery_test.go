package store

import (
	"bytes"
	"context"
	"testing"
)

func TestRecoverInterruptedCommunicationOperationsMarksHardwareCommandIndeterminate(
	t *testing.T,
) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	command, created, err := repository.BeginHardwareCommand(
		ctx,
		"request-interrupted",
		"start_call",
		bytes.Repeat([]byte{0x42}, 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || command.Status != HardwareCommandPending {
		t.Fatalf("created command = %+v, created = %v", command, created)
	}
	if err := repository.RecoverInterruptedCommunicationOperations(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = repository.HardwareCommand(ctx, command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if command.Status != HardwareCommandIndeterminate ||
		command.ErrorCode != "process_interrupted" {
		t.Fatalf("recovered command = %+v", command)
	}
}
