package modemmanager

import (
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestStableLineIdentityUsesOnlyCanonicalHardwareIdentity(t *testing.T) {
	t.Parallel()

	first := stableLineIdentity(domain.Line{
		PhysicalDevice:      " /sys/devices/usb1/./1-2/ ",
		EquipmentIdentifier: " 867530900000001 ",
		DeviceIdentifier:    " DEVICE-ABC ",
		Device:              "/dev/cdc-wdm0",
		PrimaryPort:         "cdc-wdm0",
	})
	second := stableLineIdentity(domain.Line{
		PhysicalDevice:      "/sys/devices/usb1/1-2",
		EquipmentIdentifier: "867530900000001",
		DeviceIdentifier:    "device-abc",
		Device:              "/dev/cdc-wdm19",
		PrimaryPort:         "ttyUSB27",
	})
	if first.id == "" || first.id != second.id {
		t.Fatalf("stable line IDs differ: first=%+v second=%+v", first, second)
	}
	if !first.persistent || !first.savedPolicySupported ||
		first.source != "physical_device+equipment_identifier+device_identifier" ||
		first.unsupportedPolicyReason != "" {
		t.Fatalf("stable line identity metadata = %+v", first)
	}
}

func TestStableLineIdentitySeparatesPhysicalAndHardwareIdentifiers(t *testing.T) {
	t.Parallel()

	base := domain.Line{
		PhysicalDevice:      "/sys/devices/usb1/1-2",
		EquipmentIdentifier: "867530900000001",
		DeviceIdentifier:    "device-abc",
	}
	identities := []lineIdentity{
		stableLineIdentity(base),
		stableLineIdentity(domain.Line{
			PhysicalDevice:      "/sys/devices/usb1/1-3",
			EquipmentIdentifier: base.EquipmentIdentifier,
			DeviceIdentifier:    base.DeviceIdentifier,
		}),
		stableLineIdentity(domain.Line{
			PhysicalDevice:      base.PhysicalDevice,
			EquipmentIdentifier: "867530900000002",
			DeviceIdentifier:    base.DeviceIdentifier,
		}),
		stableLineIdentity(domain.Line{
			PhysicalDevice:      base.PhysicalDevice,
			EquipmentIdentifier: base.EquipmentIdentifier,
			DeviceIdentifier:    "device-def",
		}),
	}
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		if identity.id == "" {
			t.Fatalf("identity is empty: %+v", identity)
		}
		if _, duplicate := seen[identity.id]; duplicate {
			t.Fatalf("stable identity collision: %+v", identities)
		}
		seen[identity.id] = struct{}{}
	}
}

func TestStableLineIdentityRefusesTemporaryRouteFallback(t *testing.T) {
	t.Parallel()

	identity := stableLineIdentity(domain.Line{
		Device:      "/dev/cdc-wdm7",
		PrimaryPort: "ttyUSB9",
	})
	if identity.id != "" ||
		identity.persistent ||
		identity.savedPolicySupported ||
		identity.source != "" ||
		identity.unsupportedPolicyReason != missingStableLineIdentity {
		t.Fatalf("temporary route became a persistent identity: %+v", identity)
	}
}

func TestCallInstanceIDUsesProviderOwnerAndObjectPath(t *testing.T) {
	t.Parallel()

	path := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/7")
	firstAgent := newInstanceIDsForTest(":1.41")
	restartedAgent := newInstanceIDsForTest(":1.41")
	restartedProvider := newInstanceIDsForTest(":1.42")

	firstID := firstAgent.callID(path)
	if firstID == "" || restartedAgent.callID(path) != firstID {
		t.Fatalf("same ModemManager owner changed call ID: %q", firstID)
	}
	if restartedProvider.callID(path) == firstID {
		t.Fatalf("new ModemManager owner reused call ID: %q", firstID)
	}
	if firstAgent.callID("/org/freedesktop/ModemManager1/Call/8") == firstID {
		t.Fatalf("different call object path reused call ID: %q", firstID)
	}
}
