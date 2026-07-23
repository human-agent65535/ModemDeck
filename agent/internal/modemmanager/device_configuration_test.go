package modemmanager

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type configurationCaller struct {
	mu      sync.Mutex
	objects ManagedObjects
	calls   []dbusInvocation
}

func (caller *configurationCaller) Call(
	ctx context.Context,
	destination string,
	path dbus.ObjectPath,
	method string,
	flags dbus.Flags,
	args ...any,
) ([]any, error) {
	caller.mu.Lock()
	defer caller.mu.Unlock()
	caller.calls = append(caller.calls, dbusInvocation{
		Context:     ctx,
		Destination: destination,
		Path:        path,
		Method:      method,
		Flags:       flags,
		Args:        append([]any(nil), args...),
	})
	switch method {
	case objectManagerInterface + ".GetManagedObjects":
		return []any{caller.objects}, nil
	case modemInterface + ".Enable":
		enabled, ok := args[0].(bool)
		if !ok {
			return nil, fmt.Errorf("Enable argument is %T", args[0])
		}
		if enabled {
			caller.objects[testModemPath][modemInterface]["State"] = dbus.MakeVariant(int32(8))
			caller.objects[testModemPath][modemInterface]["PowerState"] = dbus.MakeVariant(uint32(3))
		} else {
			caller.objects[testModemPath][modemInterface]["State"] = dbus.MakeVariant(int32(3))
			caller.objects[testModemPath][modemInterface]["PowerState"] = dbus.MakeVariant(uint32(2))
		}
		return []any{}, nil
	case simpleInterface + ".Connect":
		properties, ok := args[0].(map[string]dbus.Variant)
		if !ok {
			return nil, fmt.Errorf("Connect argument is %T", args[0])
		}
		bearerPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Bearer/7")
		caller.objects[testModemPath][modemInterface]["Bearers"] =
			dbus.MakeVariant([]dbus.ObjectPath{bearerPath})
		caller.objects[bearerPath] = Interfaces{
			bearerInterface: {
				"Connected":  dbus.MakeVariant(true),
				"Interface":  dbus.MakeVariant("wwan0"),
				"Properties": dbus.MakeVariant(properties),
				"Ip4Config": dbus.MakeVariant(map[string]dbus.Variant{
					"method":  dbus.MakeVariant(uint32(3)),
					"address": dbus.MakeVariant("10.0.0.2"),
					"prefix":  dbus.MakeVariant(uint32(30)),
					"gateway": dbus.MakeVariant("10.0.0.1"),
					"dns":     dbus.MakeVariant([]string{"1.1.1.1"}),
					"mtu":     dbus.MakeVariant(uint32(1500)),
				}),
				"Ip6Config": dbus.MakeVariant(map[string]dbus.Variant{}),
			},
		}
		return []any{bearerPath}, nil
	case simpleInterface + ".Disconnect":
		for objectPath, interfaces := range caller.objects {
			if properties, found := interfaces[bearerInterface]; found {
				properties["Connected"] = dbus.MakeVariant(false)
				caller.objects[objectPath] = interfaces
			}
		}
		return []any{}, nil
	default:
		return nil, fmt.Errorf("unexpected D-Bus method %s", method)
	}
}

func (caller *configurationCaller) methods() []string {
	caller.mu.Lock()
	defer caller.mu.Unlock()
	methods := make([]string, 0, len(caller.calls))
	for _, call := range caller.calls {
		methods = append(methods, call.Method)
	}
	return methods
}

func TestDeviceConfigurationReadsGenericModemManagerState(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)

	configuration, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	if !configuration.Radio.EnabledKnown || !configuration.Radio.Enabled ||
		configuration.FlightMode || configuration.NetworkEnabled {
		t.Fatalf("unexpected runtime state: %+v", configuration)
	}
	if !configuration.Capabilities.Radio.Writable ||
		!configuration.Capabilities.DataConnection.Readable ||
		!configuration.Capabilities.DataConnection.Writable {
		t.Fatalf("unexpected generic capabilities: %+v", configuration.Capabilities)
	}
	if configuration.Capabilities.VoWiFi.Supported ||
		configuration.Capabilities.ESIM.Supported ||
		configuration.Capabilities.ATTerminal.Supported ||
		configuration.Capabilities.VoLTE.Supported {
		t.Fatalf("vendor-only features were advertised: %+v", configuration.Capabilities)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestApplyDeviceConfigurationWritesOnceAndVerifiesReadBack(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	connected, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-1",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			APN:              "internet.example",
			IPFamily:         "ipv4",
		},
	)
	if err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration(connect) error = %v", err)
	}
	if !connected.NetworkEnabled || len(connected.DataConnections) != 1 ||
		connected.DataConnections[0].APN != "internet.example" ||
		connected.DataConnections[0].IPFamily != "ipv4" {
		t.Fatalf("connected configuration = %+v", connected)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		simpleInterface+".Connect",
		objectManagerInterface+".GetManagedObjects",
	)

	caller.calls = nil
	disabled := false
	updated, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "disable-radio-1",
			LineID:           lineID,
			ExpectedRevision: connected.Revision,
			Operation:        domain.DeviceConfigurationSetRadioEnabled,
			RadioEnabled:     &disabled,
		},
	)
	if err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration(disable) error = %v", err)
	}
	if !updated.Radio.EnabledKnown || updated.Radio.Enabled ||
		!updated.FlightModeKnown || !updated.FlightMode {
		t.Fatalf("disabled configuration = %+v", updated)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		modemInterface+".Enable",
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestApplyDeviceConfigurationRejectsStaleRevisionBeforeWrite(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	enabled := true

	_, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "stale-radio-1",
			LineID:           lineID,
			ExpectedRevision: "sha256:stale",
			Operation:        domain.DeviceConfigurationSetRadioEnabled,
			RadioEnabled:     &enabled,
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorConflict {
		t.Fatalf("error = %#v, want conflict", err)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestSnapshotAndConfigurationPreserveSixLines(t *testing.T) {
	t.Parallel()
	objects := multipleConfigurationObjects(6)
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 6 {
		t.Fatalf("Snapshot() lines = %d, want 6", len(snapshot.Lines))
	}
	target := snapshot.Lines[5].ID
	configuration, err := provider.ReadDeviceConfiguration(context.Background(), target)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration(sixth line) error = %v", err)
	}
	if configuration.LineID != target {
		t.Fatalf("configuration line = %q, want %q", configuration.LineID, target)
	}
}

func configurationObjects() ManagedObjects {
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["Revision"] = dbus.MakeVariant("fixture-fw-1")
	objects[testModemPath][modemInterface]["PowerState"] = dbus.MakeVariant(uint32(3))
	objects[testModemPath][modemInterface]["Bearers"] = dbus.MakeVariant([]dbus.ObjectPath{})
	objects[testModemPath][simpleInterface] = Properties{}
	return objects
}

func multipleConfigurationObjects(count int) ManagedObjects {
	objects := ManagedObjects{}
	for index := 0; index < count; index++ {
		modemPath := dbus.ObjectPath(fmt.Sprintf(
			"/org/freedesktop/ModemManager1/Modem/%d",
			index,
		))
		simPath := dbus.ObjectPath(fmt.Sprintf(
			"/org/freedesktop/ModemManager1/SIM/%d",
			index,
		))
		objects[modemPath] = Interfaces{
			modemInterface: {
				"Manufacturer":        dbus.MakeVariant("Fixture Vendor"),
				"Model":               dbus.MakeVariant(fmt.Sprintf("Model-%d", index)),
				"Revision":            dbus.MakeVariant("fixture-fw"),
				"EquipmentIdentifier": dbus.MakeVariant(fmt.Sprintf("99%013d", index)),
				"DeviceIdentifier":    dbus.MakeVariant(fmt.Sprintf("device-%d", index)),
				"Physdev":             dbus.MakeVariant(fmt.Sprintf("/sys/devices/usb/%d", index)),
				"PrimaryPort":         dbus.MakeVariant(fmt.Sprintf("cdc-wdm%d", index)),
				"State":               dbus.MakeVariant(int32(8)),
				"PowerState":          dbus.MakeVariant(uint32(3)),
				"Bearers":             dbus.MakeVariant([]dbus.ObjectPath{}),
				"Sim":                 dbus.MakeVariant(simPath),
			},
			simpleInterface: Properties{},
		}
		objects[simPath] = Interfaces{
			simInterface: {
				"SimIdentifier": dbus.MakeVariant(fmt.Sprintf("89%017d", index)),
			},
		}
	}
	return objects
}

func assertConfigurationMethods(t *testing.T, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("D-Bus methods = %v, want %v", got, want)
	}
}
