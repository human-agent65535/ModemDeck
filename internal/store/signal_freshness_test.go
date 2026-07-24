package store

import (
	"context"
	"testing"
	"time"
)

func TestHardwareSnapshotClearsStaleSignalTelemetry(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC)
	dbm := int64(-68)
	rsrp := int64(-94)
	rsrq := int64(-11)
	line := HardwareLine{
		ID:                  "line-signal-freshness",
		EquipmentIdentifier: "990000000000001",
		SignalKnown:         true,
		SignalQuality:       76,
		SignalDBM:           &dbm,
		SignalRSRP:          &rsrp,
		SignalRSRQ:          &rsrq,
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-signal-freshness",
		Revision:   "fresh",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("apply fresh signal snapshot: %v", err)
	}

	line.SignalKnown = false
	line.SignalDBM = nil
	line.SignalRSRP = nil
	line.SignalRSRQ = nil
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-signal-freshness",
		Revision:   "stale",
		ObservedAt: observed.Add(3 * time.Second),
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("apply stale signal snapshot: %v", err)
	}

	devices, err := repository.Devices(ctx)
	if err != nil {
		t.Fatalf("read devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	device := devices[0]
	if device.SignalQuality != nil || device.SignalDBM != nil ||
		device.SignalRSRP != nil || device.SignalRSRQ != nil {
		t.Fatalf("stale signal telemetry remained: %+v", device)
	}
}
