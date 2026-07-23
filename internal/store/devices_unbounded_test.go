package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDevicesAndLinesAreNotTruncatedAtMaxQueryLimit(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	count := MaxQueryLimit + 7
	lines := make([]HardwareLine, 0, count)
	for index := 0; index < count; index++ {
		lines = append(lines, HardwareLine{
			ID:                  fmt.Sprintf("line-unbounded-%03d", index),
			Model:               "Fixture modem",
			Firmware:            "fixture-fw",
			EquipmentIdentifier: fmt.Sprintf("99%013d", index),
			ICCID:               fmt.Sprintf("89%017d", index),
			IMSI:                fmt.Sprintf("44%013d", index),
		})
	}
	if err := repository.ApplyHardwareSnapshot(context.Background(), HardwareSnapshot{
		BootEpoch:  "boot-unbounded",
		Revision:   "snapshot-unbounded-1",
		ObservedAt: time.Date(2026, time.July, 23, 5, 0, 0, 0, time.UTC),
		Lines:      lines,
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	devices, err := repository.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices() error = %v", err)
	}
	if len(devices) != count {
		t.Fatalf("Devices() count = %d, want %d (> MaxQueryLimit)", len(devices), count)
	}
	storedLines, err := repository.Lines(context.Background())
	if err != nil {
		t.Fatalf("Lines() error = %v", err)
	}
	if len(storedLines) != count {
		t.Fatalf("Lines() count = %d, want %d (> MaxQueryLimit)", len(storedLines), count)
	}
}
