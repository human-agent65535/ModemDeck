package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLineSettingsPersistDefaultAndContactPreference(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()

	initial, err := repository.LineSettings(ctx)
	if err != nil {
		t.Fatalf("LineSettings() error = %v", err)
	}
	if initial.DefaultDeviceIMEI != "" || initial.Revision != 1 {
		t.Fatalf("initial settings = %+v", initial)
	}

	observedAt := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-lines",
		Revision:   "snapshot-lines-1",
		ObservedAt: observedAt,
		Lines: []HardwareLine{
			{
				ID:                  "line-main",
				EquipmentIdentifier: "imei-main",
				ICCID:               "iccid-main",
				IMSI:                "imsi-main",
			},
			{
				ID:                  "line-travel",
				EquipmentIdentifier: "imei-travel",
				ICCID:               "iccid-travel",
				IMSI:                "imsi-travel",
			},
		},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}

	discovered, err := repository.LineSettings(ctx)
	if err != nil {
		t.Fatalf("LineSettings(after discovery) error = %v", err)
	}
	if discovered.DefaultDeviceIMEI != "imei-main" || discovered.Revision != 2 {
		t.Fatalf("discovered settings = %+v", discovered)
	}

	updated, err := repository.UpdateLineSettings(ctx, "imei-travel", discovered.Revision)
	if err != nil {
		t.Fatalf("UpdateLineSettings() error = %v", err)
	}
	if updated.DefaultDeviceIMEI != "imei-travel" || updated.Revision != 3 {
		t.Fatalf("updated settings = %+v", updated)
	}
	if _, err := repository.UpdateLineSettings(ctx, "imei-main", discovered.Revision); !errors.Is(
		err,
		ErrLineSettingsRevisionConflict,
	) {
		t.Fatalf("stale UpdateLineSettings() error = %v", err)
	}
	if _, err := repository.UpdateLineSettings(ctx, "imei-missing", updated.Revision); !errors.Is(
		err,
		ErrLineSettingsInvalidDevice,
	) {
		t.Fatalf("unknown UpdateLineSettings() error = %v", err)
	}

	contact, err := repository.CreateContact(ctx, ContactInput{
		DisplayName:         "Preferred line",
		PreferredDeviceIMEI: "imei-main",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 80 0000 0000", Primary: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateContact() error = %v", err)
	}
	if contact.PreferredDeviceIMEI != "imei-main" {
		t.Fatalf("contact = %+v", contact)
	}
	if _, err := repository.CreateContact(ctx, ContactInput{
		DisplayName:         "Unknown line",
		PreferredDeviceIMEI: "imei-missing",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 80 0000 0001", Primary: true},
		},
	}); !errors.Is(err, ErrContactValidation) {
		t.Fatalf("unknown preferred line error = %v", err)
	}
}
