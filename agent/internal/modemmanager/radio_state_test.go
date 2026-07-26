package modemmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestRadioStateStorePersistsOnlyDisabledStableLines(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "radio-state.json")
	store, err := newRadioStateStore(stateFile)
	if err != nil {
		t.Fatalf("newRadioStateStore() error = %v", err)
	}
	if !store.enabled("line-a") {
		t.Fatal("new line did not default to enabled")
	}
	if err := store.setEnabled("line-a", false); err != nil {
		t.Fatalf("setEnabled(false) error = %v", err)
	}
	info, err := os.Stat(stateFile)
	if err != nil {
		t.Fatalf("stat radio state: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("radio state mode = %o", info.Mode().Perm())
	}

	reloaded, err := newRadioStateStore(stateFile)
	if err != nil {
		t.Fatalf("reload radio state: %v", err)
	}
	if reloaded.enabled("line-a") || !reloaded.enabled("line-b") {
		t.Fatalf("reloaded desired states are incorrect")
	}
	if err := reloaded.setEnabled("line-a", true); err != nil {
		t.Fatalf("setEnabled(true) error = %v", err)
	}
	enabledAgain, err := newRadioStateStore(stateFile)
	if err != nil {
		t.Fatalf("reload enabled radio state: %v", err)
	}
	if !enabledAgain.enabled("line-a") {
		t.Fatal("enabled line remained disabled after reload")
	}
}

func TestRadioStateStoreRejectsCorruptState(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "radio-state.json")
	if err := os.WriteFile(
		stateFile,
		[]byte(`{"version":1,"disabled_lines":["line-a","line-a"]}`),
		0o600,
	); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
	_, err := newRadioStateStore(stateFile)
	if err == nil || !strings.Contains(err.Error(), "duplicate disabled line") {
		t.Fatalf("newRadioStateStore() error = %v", err)
	}
}

func TestRadioReconcilerEnablesNewDisabledLineByDefault(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	objects[testModemPath][modemInterface]["State"] = dbus.MakeVariant(int32(modemStateDisabled))
	caller := &configurationCaller{objects: objects}
	provider, err := newProviderWithOptions(
		caller,
		newInstanceIDsForTest("boot-radio-default"),
		Options{RadioStateFile: filepath.Join(t.TempDir(), "radio-state.json")},
	)
	if err != nil {
		t.Fatalf("newProviderWithOptions() error = %v", err)
	}

	if err := provider.ReconcileRadioState(context.Background()); err != nil {
		t.Fatalf("ReconcileRadioState() error = %v", err)
	}
	if caller.methodCount(modemInterface+".Enable") != 1 {
		t.Fatalf("Enable calls = %d, want 1", caller.methodCount(modemInterface+".Enable"))
	}
	state, _ := int32Property(caller.objects[testModemPath][modemInterface], "State")
	if state < modemStateEnabled {
		t.Fatalf("modem state = %d, want enabled or registered", state)
	}
}

func TestRadioReconcilerPreservesPersistedUserDisable(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "radio-state.json")
	objects := configurationObjects()
	objects[testModemPath][modemInterface]["State"] = dbus.MakeVariant(int32(modemStateDisabled))
	caller := &configurationCaller{objects: objects}
	provider, err := newProviderWithOptions(
		caller,
		newInstanceIDsForTest("boot-radio-disabled"),
		Options{RadioStateFile: stateFile},
	)
	if err != nil {
		t.Fatalf("newProviderWithOptions() error = %v", err)
	}
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	disabled := false
	if _, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "persist-disabled-radio",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetRadioEnabled,
			RadioEnabled:     &disabled,
		},
	); err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration() error = %v", err)
	}

	reloaded, err := newProviderWithOptions(
		caller,
		newInstanceIDsForTest("boot-radio-disabled-reloaded"),
		Options{RadioStateFile: stateFile},
	)
	if err != nil {
		t.Fatalf("reload provider: %v", err)
	}
	enableCalls := caller.methodCount(modemInterface + ".Enable")
	if err := reloaded.ReconcileRadioState(context.Background()); err != nil {
		t.Fatalf("ReconcileRadioState() error = %v", err)
	}
	if caller.methodCount(modemInterface+".Enable") != enableCalls {
		t.Fatal("persisted user-disabled modem was re-enabled")
	}
}
