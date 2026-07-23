package modemmanager

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const missingStableLineIdentity = "physical_device, equipment_identifier, and device_identifier are all unavailable"

type instanceIDs struct {
	mu            sync.RWMutex
	providerOwner string
}

type lineIdentity struct {
	id                      string
	persistent              bool
	source                  string
	savedPolicySupported    bool
	unsupportedPolicyReason string
}

func newInstanceIDs() *instanceIDs {
	return &instanceIDs{}
}

func newInstanceIDsForTest(providerOwner string) *instanceIDs {
	ids := newInstanceIDs()
	if err := ids.setProviderOwner(providerOwner); err != nil {
		panic(err)
	}
	return ids
}

func (ids *instanceIDs) setProviderOwner(providerOwner string) error {
	providerOwner = strings.TrimSpace(providerOwner)
	if providerOwner == "" {
		return fmt.Errorf("ModemManager D-Bus unique owner is empty")
	}
	ids.mu.Lock()
	ids.providerOwner = providerOwner
	ids.mu.Unlock()
	return nil
}

func (ids *instanceIDs) clearProviderOwner() {
	ids.mu.Lock()
	ids.providerOwner = ""
	ids.mu.Unlock()
}

func (ids *instanceIDs) providerEpoch() string {
	if ids == nil {
		return ""
	}
	ids.mu.RLock()
	defer ids.mu.RUnlock()
	return ids.providerOwner
}

func (ids *instanceIDs) freeze() (*instanceIDs, error) {
	owner := ids.providerEpoch()
	if owner == "" {
		return nil, fmt.Errorf("ModemManager D-Bus unique owner is unavailable")
	}
	return newInstanceIDsForTest(owner), nil
}

// stableLineIdentity includes every available trusted component in a fixed
// order. PhysicalDevice is the slot/udev anchor; equipment and device IDs add
// hardware identity. ModemManager object paths and kernel port names are never
// accepted as persistent identity.
func stableLineIdentity(line domain.Line) lineIdentity {
	components := make([]string, 0, 3)
	sources := make([]string, 0, 3)
	if physicalDevice := normalizePhysicalDevice(line.PhysicalDevice); physicalDevice != "" {
		components = append(components, "physical_device="+physicalDevice)
		sources = append(sources, "physical_device")
	}
	if equipmentIdentifier := normalizeHardwareIdentifier(line.EquipmentIdentifier); equipmentIdentifier != "" {
		components = append(components, "equipment_identifier="+equipmentIdentifier)
		sources = append(sources, "equipment_identifier")
	}
	if deviceIdentifier := normalizeHardwareIdentifier(line.DeviceIdentifier); deviceIdentifier != "" {
		components = append(components, "device_identifier="+deviceIdentifier)
		sources = append(sources, "device_identifier")
	}
	if len(components) == 0 {
		return lineIdentity{
			unsupportedPolicyReason: missingStableLineIdentity,
		}
	}
	sum := sha256.Sum256([]byte("line\x00" + strings.Join(components, "\x00")))
	return lineIdentity{
		id:                   "line_" + base64.RawURLEncoding.EncodeToString(sum[:18]),
		persistent:           true,
		source:               strings.Join(sources, "+"),
		savedPolicySupported: true,
	}
}

func normalizePhysicalDevice(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func normalizeHardwareIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (ids *instanceIDs) callID(path dbus.ObjectPath) string {
	return ids.objectID("call", path)
}

func (ids *instanceIDs) messageID(path dbus.ObjectPath) string {
	return ids.objectID("message", path)
}

func (ids *instanceIDs) bearerID(path dbus.ObjectPath) string {
	return ids.objectID("bearer", path)
}

func (ids *instanceIDs) objectID(kind string, path dbus.ObjectPath) string {
	owner := ids.providerEpoch()
	if owner == "" || !path.IsValid() {
		return ""
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + owner + "\x00" + string(path)))
	return kind + "_" + base64.RawURLEncoding.EncodeToString(sum[:18])
}
