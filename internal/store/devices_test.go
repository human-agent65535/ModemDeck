package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUpdateLineLabelUsesSIMIdentityAndSurvivesHardwareRefresh(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	line := HardwareLine{
		ID:                  "line-label-fixture",
		Model:               "Fixture modem",
		Firmware:            "fixture-fw",
		EquipmentIdentifier: "990000000000001",
		ICCID:               "8986010000000000001",
		IMSI:                "460010000000001",
		Operator:            "Fixture Telecom",
	}
	snapshot := HardwareSnapshot{
		BootEpoch:  "boot-line-label",
		Revision:   "snapshot-line-label-1",
		ObservedAt: time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC),
		Lines:      []HardwareLine{line},
	}
	if err := repository.ApplyHardwareSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	if _, err := repository.RenameDevice(ctx, line.EquipmentIdentifier, "机房模组"); err != nil {
		t.Fatalf("RenameDevice() error = %v", err)
	}

	updated, err := repository.UpdateLineLabel(ctx, line.ICCID, "  主卡  ")
	if err != nil {
		t.Fatalf("UpdateLineLabel() error = %v", err)
	}
	if updated.LineLabel != "主卡" {
		t.Fatalf("line label = %q, want 主卡", updated.LineLabel)
	}
	if updated.DeviceAlias != "机房模组" {
		t.Fatalf("device alias = %q, want 机房模组", updated.DeviceAlias)
	}

	snapshot.Revision = "snapshot-line-label-2"
	snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Minute)
	if err := repository.ApplyHardwareSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("refresh ApplyHardwareSnapshot() error = %v", err)
	}
	lines, err := repository.Lines(ctx)
	if err != nil {
		t.Fatalf("Lines() error = %v", err)
	}
	if len(lines) != 1 || lines[0].LineLabel != "主卡" {
		t.Fatalf("lines = %+v, want persisted line label", lines)
	}
	if lines[0].Operator != "Fixture Telecom" ||
		lines[0].HomeOperatorName != "Fixture Telecom" ||
		lines[0].ServingOperatorName != "" ||
		lines[0].Roaming {
		t.Fatalf("persisted line operator semantics = %+v", lines[0])
	}
	devices, err := repository.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices() error = %v", err)
	}
	if len(devices) != 1 || devices[0].Alias != "机房模组" {
		t.Fatalf("devices = %+v, want unchanged device alias", devices)
	}
	if devices[0].SIM == nil ||
		devices[0].SIM.Operator != "Fixture Telecom" ||
		devices[0].SIM.HomeOperatorName != "Fixture Telecom" ||
		devices[0].SIM.ServingOperatorName != "" {
		t.Fatalf("persisted SIM operator semantics = %+v", devices[0].SIM)
	}
}

func TestUpdateLineLabelValidatesUnicodeLengthAndICCID(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if _, err := repository.UpdateLineLabel(ctx, "", "主卡"); !errors.Is(err, ErrLineValidation) {
		t.Fatalf("blank ICCID error = %v, want ErrLineValidation", err)
	}
	if _, err := repository.UpdateLineLabel(
		ctx,
		"8986010000000000001",
		strings.Repeat("卡", maxLineLabelLength+1),
	); !errors.Is(err, ErrLineValidation) {
		t.Fatalf("long label error = %v, want ErrLineValidation", err)
	}
	if _, err := repository.UpdateLineLabel(
		ctx,
		"8986010000000000001",
		strings.Repeat("卡", maxLineLabelLength),
	); !errors.Is(err, ErrLineNotFound) {
		t.Fatalf("unknown ICCID error = %v, want ErrLineNotFound", err)
	}
}
