package modemmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestBearerOwnershipStorePersistsExactProviderEpochAndPaths(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "bearers.json")
	store, err := newBearerOwnershipStore(stateFile)
	if err != nil {
		t.Fatalf("newBearerOwnershipStore() error = %v", err)
	}
	expected := ownedBearer{
		LineID:        "line-fixture",
		ProviderEpoch: ":1.42",
		ModemPath:     dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/7"),
		BearerPath:    dbus.ObjectPath("/org/freedesktop/ModemManager1/Bearer/9"),
		APN:           "internet.example",
		IPFamily:      bearerIPFamilyIPv4V6,
	}
	if err := store.put(expected); err != nil {
		t.Fatalf("put() error = %v", err)
	}
	info, err := os.Stat(stateFile)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o", info.Mode().Perm())
	}

	reloaded, err := newBearerOwnershipStore(stateFile)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	actual, found := reloaded.get(expected.LineID, expected.ProviderEpoch)
	if !found || actual != expected {
		t.Fatalf("reloaded bearer = %+v, found=%t", actual, found)
	}
	if _, found := reloaded.get(expected.LineID, ":1.43"); found {
		t.Fatal("bearer ownership crossed a ModemManager provider epoch")
	}
	if err := reloaded.remove(expected.LineID); err != nil {
		t.Fatalf("remove() error = %v", err)
	}
	if _, found := reloaded.get(expected.LineID, expected.ProviderEpoch); found {
		t.Fatal("removed bearer remained in memory")
	}
}

func TestBearerOwnershipStoreRejectsCorruptState(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "bearers.json")
	if err := os.WriteFile(
		stateFile,
		[]byte(`{"version":1,"bearers":[{"line_id":"line-fixture"}]}`),
		0o600,
	); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := newBearerOwnershipStore(stateFile)
	if err == nil || !strings.Contains(err.Error(), "invalid owned bearer") {
		t.Fatalf("newBearerOwnershipStore() error = %v", err)
	}
}
