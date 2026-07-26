package modemmanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	testNetworkManagerDevicePath = dbus.ObjectPath(
		"/org/freedesktop/NetworkManager/Devices/42",
	)
	testNetworkManagerSettingsPath = dbus.ObjectPath(
		"/org/freedesktop/NetworkManager/Settings/42",
	)
	testNetworkManagerActivePath = dbus.ObjectPath(
		"/org/freedesktop/NetworkManager/ActiveConnection/42",
	)
	testNetworkManagerBearerPath = dbus.ObjectPath(
		"/org/freedesktop/ModemManager1/Bearer/42",
	)
)

type configurationCaller struct {
	mu                   sync.Mutex
	objects              ManagedObjects
	calls                []dbusInvocation
	connectErr           error
	deactivateErr        error
	cancelCall           func()
	activeConnection     dbus.ObjectPath
	activeConnectionID   string
	activeState          uint32
	deviceState          uint32
	deviceStateReason    uint32
	negotiatedIPFamily   uint32
	activationState      uint32
	activationDisappears bool
	verificationMismatch bool
	externalBearers      map[dbus.ObjectPath]Properties
	externalCalls        map[dbus.ObjectPath]Properties
	objectSnapshots      []ManagedObjects
	objectSnapshotIndex  int
}

func (caller *configurationCaller) Call(
	ctx context.Context,
	destination string,
	path dbus.ObjectPath,
	method string,
	flags dbus.Flags,
	args ...any,
) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		objects := caller.objects
		if caller.objectSnapshotIndex < len(caller.objectSnapshots) {
			objects = caller.objectSnapshots[caller.objectSnapshotIndex]
			caller.objectSnapshotIndex++
		}
		return []any{cloneTestManagedObjects(objects)}, nil
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
	case modemInterface + ".Reset":
		return []any{}, nil
	case networkManagerInterface + ".GetDevices":
		return []any{[]dbus.ObjectPath{testNetworkManagerDevicePath}}, nil
	case propertiesInterface + ".GetAll":
		if destination == serviceName {
			if len(args) == 1 && args[0] == callInterface {
				properties, found := caller.externalCalls[path]
				if !found {
					return nil, dbus.NewError(dbusErrorPrefix+"UnknownObject", nil)
				}
				return []any{properties}, nil
			}
			properties, found := caller.externalBearers[path]
			if !found {
				return nil, dbus.NewError(dbusErrorPrefix+"UnknownObject", nil)
			}
			return []any{properties}, nil
		}
		if destination != networkManagerServiceName {
			return nil, fmt.Errorf("unexpected GetAll destination %s", destination)
		}
		switch path {
		case testNetworkManagerDevicePath:
			active := caller.activeConnection
			if active == "" {
				active = networkManagerNoObject
			}
			state := caller.deviceState
			if state == 0 {
				state = 30
			}
			return []any{Properties{
				"Udi":              dbus.MakeVariant(string(testModemPath)),
				"DeviceType":       dbus.MakeVariant(networkManagerDeviceTypeModem),
				"State":            dbus.MakeVariant(state),
				"StateReason":      dbus.MakeVariant([]any{state, caller.deviceStateReason}),
				"ActiveConnection": dbus.MakeVariant(active),
			}}, nil
		case testNetworkManagerActivePath:
			if caller.activeConnection != path {
				return nil, dbus.NewError(dbusErrorPrefix+"UnknownObject", nil)
			}
			return []any{Properties{
				"Id":    dbus.MakeVariant(caller.activeConnectionID),
				"State": dbus.MakeVariant(caller.activeState),
			}}, nil
		default:
			return nil, fmt.Errorf("unexpected NetworkManager object %s", path)
		}
	case networkManagerInterface + ".AddAndActivateConnection2":
		if caller.connectErr != nil {
			return nil, caller.connectErr
		}
		settings, ok := args[0].(map[string]map[string]dbus.Variant)
		if !ok {
			return nil, fmt.Errorf("NetworkManager connection settings are %T", args[0])
		}
		connectionID, _ := stringProperty(settings["connection"], "id")
		apn, _ := stringProperty(settings["gsm"], "apn")
		if caller.verificationMismatch {
			apn = "mismatch.example"
		}
		ipType := networkManagerSettingsIPFamily(settings)
		if caller.negotiatedIPFamily != 0 {
			ipType = caller.negotiatedIPFamily
		}
		ipv4 := map[string]dbus.Variant{}
		if ipType == bearerIPFamilyIPv4 || ipType == bearerIPFamilyIPv4V6 {
			ipv4 = map[string]dbus.Variant{
				"method":  dbus.MakeVariant(uint32(3)),
				"address": dbus.MakeVariant("10.0.0.2"),
				"prefix":  dbus.MakeVariant(uint32(30)),
				"gateway": dbus.MakeVariant("10.0.0.1"),
				"dns":     dbus.MakeVariant([]string{"1.1.1.1"}),
				"mtu":     dbus.MakeVariant(uint32(1500)),
			}
		}
		ipv6 := map[string]dbus.Variant{}
		if ipType == bearerIPFamilyIPv6 || ipType == bearerIPFamilyIPv4V6 {
			ipv6 = map[string]dbus.Variant{
				"method":  dbus.MakeVariant(uint32(3)),
				"address": dbus.MakeVariant("2001:db8::2"),
				"prefix":  dbus.MakeVariant(uint32(64)),
				"gateway": dbus.MakeVariant("2001:db8::1"),
				"dns":     dbus.MakeVariant([]string{"2606:4700:4700::1111"}),
				"mtu":     dbus.MakeVariant(uint32(1500)),
			}
		}
		caller.objects[testModemPath][modemInterface]["Bearers"] =
			dbus.MakeVariant([]dbus.ObjectPath{testNetworkManagerBearerPath})
		caller.objects[testNetworkManagerBearerPath] = Interfaces{
			bearerInterface: {
				"Connected": dbus.MakeVariant(true),
				"Interface": dbus.MakeVariant("wwan0"),
				"Properties": dbus.MakeVariant(map[string]dbus.Variant{
					"apn":      dbus.MakeVariant(apn),
					"apn-type": dbus.MakeVariant(domain.APNTypeDefault),
					"ip-type":  dbus.MakeVariant(ipType),
				}),
				"Ip4Config": dbus.MakeVariant(ipv4),
				"Ip6Config": dbus.MakeVariant(ipv6),
			},
		}
		caller.activeConnection = testNetworkManagerActivePath
		caller.activeConnectionID = connectionID
		caller.activeState = caller.activationState
		if caller.activeState == 0 {
			caller.activeState = networkManagerActiveActivated
		}
		if caller.activeState == networkManagerActiveActivated {
			caller.deviceState = networkManagerStateActivated
		} else {
			caller.deviceState = networkManagerStateFailed
		}
		if caller.activationDisappears {
			caller.activeConnection = networkManagerNoObject
		}
		if caller.cancelCall != nil {
			caller.cancelCall()
		}
		return []any{
			testNetworkManagerSettingsPath,
			testNetworkManagerActivePath,
			map[string]dbus.Variant{},
		}, nil
	case networkManagerInterface + ".DeactivateConnection":
		if caller.deactivateErr != nil {
			return nil, caller.deactivateErr
		}
		if caller.activationDisappears {
			return nil, dbus.NewError(
				networkManagerErrorPrefix+"ConnectionNotActive",
				nil,
			)
		}
		caller.activeConnection = networkManagerNoObject
		caller.activeConnectionID = ""
		caller.activeState = networkManagerActiveDeactivated
		caller.deviceState = 30
		delete(caller.objects, testNetworkManagerBearerPath)
		caller.objects[testModemPath][modemInterface]["Bearers"] =
			dbus.MakeVariant([]dbus.ObjectPath{})
		return []any{}, nil
	default:
		return nil, fmt.Errorf("unexpected D-Bus method %s", method)
	}
}

func networkManagerSettingsIPFamily(
	settings map[string]map[string]dbus.Variant,
) uint32 {
	ipv4, _ := stringProperty(settings["ipv4"], "method")
	ipv6, _ := stringProperty(settings["ipv6"], "method")
	switch {
	case ipv4 == "auto" && ipv6 == "auto":
		return bearerIPFamilyIPv4V6
	case ipv4 == "auto":
		return bearerIPFamilyIPv4
	case ipv6 == "auto":
		return bearerIPFamilyIPv6
	default:
		return bearerIPFamilyAny
	}
}

func TestNetworkManagerActiveStateValuesMatchDBusAPI(t *testing.T) {
	t.Parallel()
	if networkManagerActiveActivating != 1 ||
		networkManagerActiveActivated != 2 ||
		networkManagerActiveDeactivating != 3 ||
		networkManagerActiveDeactivated != 4 {
		t.Fatalf(
			"active connection states = %d/%d/%d/%d",
			networkManagerActiveActivating,
			networkManagerActiveActivated,
			networkManagerActiveDeactivating,
			networkManagerActiveDeactivated,
		)
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

func (caller *configurationCaller) methodCount(method string) int {
	caller.mu.Lock()
	defer caller.mu.Unlock()
	count := 0
	for _, call := range caller.calls {
		if call.Method == method {
			count++
		}
	}
	return count
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
	if configuration.Details.HardwareRevision != "fixture-hw-1" ||
		configuration.Details.PrimaryPort != "cdc-wdm0" ||
		configuration.Details.AccessTechnologies == nil ||
		*configuration.Details.AccessTechnologies != accessTechnologyLTE ||
		configuration.Details.SNR == nil ||
		*configuration.Details.SNR != 9.5 ||
		len(configuration.Details.Ports) != 2 ||
		configuration.Details.Ports[0].Type != "qmi" ||
		configuration.Details.Ports[1].Type != "at" {
		t.Fatalf("unexpected hardware details: %+v", configuration.Details)
	}
	if !configuration.Capabilities.Radio.Writable ||
		!configuration.Capabilities.Voice.Supported ||
		!configuration.Capabilities.Voice.Readable ||
		configuration.Capabilities.Voice.Writable ||
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

func TestParseDataConnectionPreservesAPNType(t *testing.T) {
	t.Parallel()

	connection, err := parseDataConnection("bearer-default", Properties{
		"Connected": dbus.MakeVariant(true),
		"Interface": dbus.MakeVariant("wwan0"),
		"Properties": dbus.MakeVariant(map[string]dbus.Variant{
			"apn":      dbus.MakeVariant("internet.example"),
			"apn-type": dbus.MakeVariant(domain.APNTypeDefault),
			"ip-type":  dbus.MakeVariant(uint32(1)),
		}),
		"Ip4Config": dbus.MakeVariant(map[string]dbus.Variant{}),
		"Ip6Config": dbus.MakeVariant(map[string]dbus.Variant{}),
	})
	if err != nil {
		t.Fatalf("parseDataConnection() error = %v", err)
	}
	if connection.APNType != domain.APNTypeDefault {
		t.Fatalf("APN type = %d, want default bit", connection.APNType)
	}
}

func TestParseDataConnectionUsesDefaultBearerTypeWithoutAPNType(t *testing.T) {
	t.Parallel()

	connection, err := parseDataConnection("bearer-default", Properties{
		"BearerType": dbus.MakeVariant(uint32(bearerTypeDefault)),
		"Connected":  dbus.MakeVariant(true),
		"Interface":  dbus.MakeVariant("wwan0"),
		"Properties": dbus.MakeVariant(map[string]dbus.Variant{
			"apn":     dbus.MakeVariant("internet.example"),
			"ip-type": dbus.MakeVariant(uint32(bearerIPFamilyIPv4V6)),
		}),
		"Ip4Config": dbus.MakeVariant(map[string]dbus.Variant{
			"method":  dbus.MakeVariant(uint32(3)),
			"address": dbus.MakeVariant("10.0.0.2"),
		}),
		"Ip6Config": dbus.MakeVariant(map[string]dbus.Variant{}),
	})
	if err != nil {
		t.Fatalf("parseDataConnection() error = %v", err)
	}
	if connection.BearerType != bearerTypeDefault {
		t.Fatalf("bearer type = %d, want default", connection.BearerType)
	}
	if !matchingConnectedData(
		[]domain.DataConnection{connection},
		"internet.example",
		bearerIPFamilyIPv4V6,
	) {
		t.Fatal("default bearer without APN type was not recognized as Internet data")
	}
}

func TestParseDataConnectionSupportsNumberedDNSProperties(t *testing.T) {
	t.Parallel()

	connection, err := parseDataConnection("bearer-default", Properties{
		"Ip4Config": dbus.MakeVariant(map[string]dbus.Variant{
			"method": dbus.MakeVariant(uint32(3)),
			"dns1":   dbus.MakeVariant("10.0.0.53"),
			"dns2":   dbus.MakeVariant(" 10.0.0.54 "),
		}),
		"Ip6Config": dbus.MakeVariant(map[string]dbus.Variant{}),
	})
	if err != nil {
		t.Fatalf("parseDataConnection() error = %v", err)
	}
	if got := strings.Join(connection.IPv4.DNS, ","); got != "10.0.0.53,10.0.0.54" {
		t.Fatalf("IPv4 DNS = %q", got)
	}
}

func TestDeviceConfigurationHydratesReferencedDataBearer(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	objects[testModemPath][modemInterface]["Bearers"] =
		dbus.MakeVariant([]dbus.ObjectPath{testNetworkManagerBearerPath})
	caller := &configurationCaller{
		objects: objects,
		externalBearers: map[dbus.ObjectPath]Properties{
			testNetworkManagerBearerPath: {
				"BearerType": dbus.MakeVariant(uint32(bearerTypeDefault)),
				"Connected":  dbus.MakeVariant(true),
				"Interface":  dbus.MakeVariant("wwan0"),
				"Properties": dbus.MakeVariant(map[string]dbus.Variant{
					"apn":     dbus.MakeVariant("automatic.example"),
					"ip-type": dbus.MakeVariant(uint32(bearerIPFamilyIPv4V6)),
				}),
				"Ip4Config": dbus.MakeVariant(map[string]dbus.Variant{
					"method":  dbus.MakeVariant(uint32(3)),
					"address": dbus.MakeVariant("10.0.0.2"),
				}),
				"Ip6Config": dbus.MakeVariant(map[string]dbus.Variant{}),
			},
		},
	}
	provider := newTestProvider(caller)

	configuration, err := provider.ReadDeviceConfiguration(
		context.Background(),
		parsedLineID(objects, provider.ids),
	)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	if !configuration.NetworkEnabled ||
		len(configuration.DataConnections) != 1 ||
		!configuration.DataConnections[0].Connected ||
		configuration.DataConnections[0].Interface != "wwan0" {
		t.Fatalf("hydrated data configuration = %+v", configuration)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		propertiesInterface+".GetAll",
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
		connected.DataConnections[0].APNType != domain.APNTypeDefault ||
		connected.DataConnections[0].IPFamily != "ipv4" {
		t.Fatalf("connected configuration = %+v", connected)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		networkManagerInterface+".GetDevices",
		propertiesInterface+".GetAll",
		networkManagerInterface+".AddAndActivateConnection2",
		propertiesInterface+".GetAll",
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

func TestApplyDeviceConfigurationRejectsRadioDisableForHydratedCall(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	callPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/42")
	callObjects := cloneTestManagedObjects(objects)
	callObjects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{callPath})
	caller := &configurationCaller{
		objects: objects,
		objectSnapshots: []ManagedObjects{
			objects,
			objects,
			callObjects,
		},
		externalCalls: map[dbus.ObjectPath]Properties{
			callPath: testCallProperties(4, 1),
		},
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil
	disabled := false

	_, err = provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "disable-radio-during-call",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetRadioEnabled,
			RadioEnabled:     &disabled,
		},
	)
	assertOperationError(t, err, domain.ErrorConflict, "apply_device_configuration")
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		objectManagerInterface+".GetManagedObjects",
		propertiesInterface+".GetAll",
	)
}

func TestApplyDeviceConfigurationRestartsModemExactlyOnce(t *testing.T) {
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

	restarted, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "restart-modem-1",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationRestartModem,
		},
	)
	if err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration(restart) error = %v", err)
	}
	if restarted.LineID != lineID {
		t.Fatalf("restarted line = %q, want %q", restarted.LineID, lineID)
	}

	resetCalls := 0
	for _, call := range caller.calls {
		if call.Method != modemInterface+".Reset" {
			continue
		}
		resetCalls++
		if call.Path != testModemPath {
			t.Fatalf("Reset path = %q, want %q", call.Path, testModemPath)
		}
		if len(call.Args) != 0 {
			t.Fatalf("Reset arguments = %v, want none", call.Args)
		}
	}
	if resetCalls != 1 {
		t.Fatalf("Reset calls = %d, methods = %v", resetCalls, caller.methods())
	}
}

func TestApplyDeviceConfigurationResolvesAutomaticAPN(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	if current.AutomaticAPN != "automatic.example" {
		t.Fatalf("automatic APN = %q", current.AutomaticAPN)
	}
	caller.calls = nil

	connected, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-auto",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	if err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration(connect auto) error = %v", err)
	}
	if len(connected.DataConnections) != 1 ||
		connected.DataConnections[0].APN != "automatic.example" {
		t.Fatalf("connected configuration = %+v", connected)
	}
	for _, call := range caller.calls {
		if call.Method != networkManagerInterface+".AddAndActivateConnection2" {
			continue
		}
		settings, ok := call.Args[0].(map[string]map[string]dbus.Variant)
		if !ok {
			t.Fatalf("NetworkManager settings = %T", call.Args[0])
		}
		apn, found := settings["gsm"]["apn"]
		if !found || apn.Value() != "automatic.example" {
			t.Fatalf("automatic NetworkManager APN = %+v", settings)
		}
		options, ok := call.Args[3].(map[string]dbus.Variant)
		if !ok || options["persist"].Value() != "volatile" {
			t.Fatalf("NetworkManager options = %+v", call.Args[3])
		}
		return
	}
	t.Fatal("NetworkManager activation was not called")
}

func TestDeviceConfigurationHydratesAutomaticAPNFromInitialEPSBearer(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	initialEPSBearerPath, _ := objectPathProperty(
		objects[testModemPath][modem3GPPInterface],
		"InitialEpsBearer",
	)
	externalProperties := objects[initialEPSBearerPath][bearerInterface]
	delete(objects, initialEPSBearerPath)
	caller := &configurationCaller{
		objects: objects,
		externalBearers: map[dbus.ObjectPath]Properties{
			initialEPSBearerPath: externalProperties,
		},
	}
	provider := newTestProvider(caller)

	configuration, err := provider.ReadDeviceConfiguration(
		context.Background(),
		parsedLineID(objects, provider.ids),
	)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	if configuration.AutomaticAPN != "automatic.example" {
		t.Fatalf("automatic APN = %q", configuration.AutomaticAPN)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		propertiesInterface+".GetAll",
	)
}

func TestDeviceConfigurationUsesInitialEPSBearerSettingsAutomaticAPN(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	properties := objects[testModemPath][modem3GPPInterface]
	properties["InitialEpsBearerSettings"] = dbus.MakeVariant(map[string]dbus.Variant{
		"apn":     dbus.MakeVariant("3gnet"),
		"ip-type": dbus.MakeVariant(uint32(bearerIPFamilyIPv4)),
	})
	initialEPSBearerPath, _ := objectPathProperty(properties, "InitialEpsBearer")
	delete(objects, initialEPSBearerPath)
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)

	configuration, err := provider.ReadDeviceConfiguration(
		context.Background(),
		parsedLineID(objects, provider.ids),
	)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	if configuration.AutomaticAPN != "3gnet" {
		t.Fatalf("automatic APN = %q", configuration.AutomaticAPN)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestApplyDeviceConfigurationUsesProviderAutoConfigWithoutResolvedAPN(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	initialEPSBearerPath, _ := objectPathProperty(
		objects[testModemPath][modem3GPPInterface],
		"InitialEpsBearer",
	)
	delete(objects, initialEPSBearerPath)
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
			RequestID:        "connect-data-auto-missing",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	if err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration(connect auto) error = %v", err)
	}
	if !connected.NetworkEnabled {
		t.Fatalf("connected configuration = %+v", connected)
	}
	for _, call := range caller.calls {
		if call.Method != networkManagerInterface+".AddAndActivateConnection2" {
			continue
		}
		settings := call.Args[0].(map[string]map[string]dbus.Variant)
		autoConfig, found := settings["gsm"]["auto-config"]
		if !found || autoConfig.Value() != true {
			t.Fatalf("automatic NetworkManager settings = %+v", settings)
		}
		if _, found := settings["gsm"]["apn"]; found {
			t.Fatalf("automatic settings unexpectedly forced APN = %+v", settings)
		}
		return
	}
	t.Fatal("NetworkManager activation was not called")
}

func TestApplyDeviceConfigurationAcceptsDualStackSingleFamilyFallback(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{
		objects:            objects,
		negotiatedIPFamily: bearerIPFamilyIPv4,
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}

	connected, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-dual-stack-fallback",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	if err != nil {
		t.Fatalf("ApplyGenericDeviceConfiguration(connect) error = %v", err)
	}
	if len(connected.DataConnections) != 1 ||
		connected.DataConnections[0].IPFamily != "ipv4" ||
		connected.DataConnections[0].IPv4.Address == "" {
		t.Fatalf("single-family fallback = %+v", connected.DataConnections)
	}
}

func TestApplyDeviceConfigurationReusesOwnedNetworkManagerConnection(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}

	connected, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-first",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			APN:              "internet.example",
			IPFamily:         "ipv4",
		},
	)
	if err != nil {
		t.Fatalf("first connect error = %v", err)
	}
	caller.calls = nil

	reused, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-reuse",
			LineID:           lineID,
			ExpectedRevision: connected.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			APN:              "internet.example",
			IPFamily:         "ipv4",
		},
	)
	if err != nil {
		t.Fatalf("second connect error = %v", err)
	}
	if !reused.NetworkEnabled {
		t.Fatalf("reused configuration = %+v", reused)
	}
	if count := caller.methodCount(
		networkManagerInterface + ".AddAndActivateConnection2",
	); count != 0 {
		t.Fatalf("second activation calls = %d, methods = %v", count, caller.methods())
	}
	if count := caller.methodCount(
		networkManagerInterface + ".DeactivateConnection",
	); count != 0 {
		t.Fatalf("second deactivation calls = %d, methods = %v", count, caller.methods())
	}
}

func TestApplyDeviceConfigurationDisconnectsOwnedNetworkManagerConnection(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	connected, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-before-disconnect",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	caller.calls = nil

	disconnected, err := provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "disconnect-owned-data",
			LineID:           lineID,
			ExpectedRevision: connected.Revision,
			Operation:        domain.DeviceConfigurationDisconnectData,
		},
	)
	if err != nil {
		t.Fatalf("disconnect error = %v", err)
	}
	if disconnected.NetworkEnabled || len(disconnected.DataConnections) != 0 {
		t.Fatalf("disconnected configuration = %+v", disconnected)
	}
	if count := caller.methodCount(
		networkManagerInterface + ".DeactivateConnection",
	); count != 1 {
		t.Fatalf("deactivation calls = %d, methods = %v", count, caller.methods())
	}
}

func TestApplyDeviceConfigurationDoesNotDisconnectExternalConnection(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{
		objects:            objects,
		activeConnection:   testNetworkManagerActivePath,
		activeConnectionID: "External cellular connection",
		activeState:        networkManagerActiveActivated,
		deviceState:        networkManagerStateActivated,
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	_, err = provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "disconnect-external-data",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationDisconnectData,
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorConflict {
		t.Fatalf("error = %#v, want conflict", err)
	}
	if count := caller.methodCount(
		networkManagerInterface + ".DeactivateConnection",
	); count != 0 {
		t.Fatalf("external deactivation calls = %d", count)
	}
}

func TestApplyDeviceConfigurationReportsNetworkManagerAPNRejection(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{
		objects:           objects,
		activationState:   networkManagerActiveDeactivated,
		deviceStateReason: 29,
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	_, err = provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-apn-rejected",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			APN:              "rejected.example",
			IPFamily:         "ipv4v6",
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorFailedPrecondition {
		t.Fatalf("error = %#v, want failed precondition", err)
	}
	if !strings.Contains(strings.ToLower(operationError.Message), "apn") {
		t.Fatalf("error message = %q, want APN diagnosis", operationError.Message)
	}
	if count := caller.methodCount(
		networkManagerInterface + ".DeactivateConnection",
	); count != 1 {
		t.Fatalf("cleanup calls = %d, methods = %v", count, caller.methods())
	}
}

func TestApplyDeviceConfigurationHandlesVanishedFailedActivation(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{
		objects:              objects,
		activationState:      networkManagerActiveDeactivated,
		activationDisappears: true,
		deviceStateReason:    29,
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	_, err = provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-vanished-activation",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			APN:              "rejected.example",
			IPFamily:         "ipv4v6",
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorFailedPrecondition {
		t.Fatalf("error = %#v, want failed precondition", err)
	}
	if strings.Contains(operationError.Message, "cleanup also failed") {
		t.Fatalf("error misreported completed cleanup: %q", operationError.Message)
	}
	if count := caller.methodCount(
		networkManagerInterface + ".DeactivateConnection",
	); count != 1 {
		t.Fatalf("cleanup calls = %d, methods = %v", count, caller.methods())
	}
}

func TestApplyDeviceConfigurationReportsNetworkManagerActivationFailure(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{
		objects: objects,
		connectErr: dbus.NewError(
			modemManagerCoreErrorPrefix+"WrongState",
			[]any{"fixture connect failure"},
		),
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	_, err = provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-failure",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorFailedPrecondition {
		t.Fatalf("error = %#v, want failed precondition", err)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		networkManagerInterface+".GetDevices",
		propertiesInterface+".GetAll",
		networkManagerInterface+".AddAndActivateConnection2",
	)
	if paths, _ := objectPathValuesProperty(
		caller.objects[testModemPath][modemInterface],
		"Bearers",
	); len(paths) != 0 {
		t.Fatalf("bearers after rollback = %v", paths)
	}
}

func TestApplyDeviceConfigurationReportsFailedNetworkManagerCleanup(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	caller := &configurationCaller{
		objects:              objects,
		verificationMismatch: true,
		deactivateErr: dbus.NewError(
			networkManagerErrorPrefix+"Failed",
			[]any{"fixture deactivation failure"},
		),
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	_, err = provider.ApplyGenericDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-cleanup-failure",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorVerification {
		t.Fatalf("error = %#v, want verification failure", err)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		networkManagerInterface+".GetDevices",
		propertiesInterface+".GetAll",
		networkManagerInterface+".AddAndActivateConnection2",
		propertiesInterface+".GetAll",
		objectManagerInterface+".GetManagedObjects",
		networkManagerInterface+".GetDevices",
		propertiesInterface+".GetAll",
		propertiesInterface+".GetAll",
		networkManagerInterface+".DeactivateConnection",
	)
	if paths, _ := objectPathValuesProperty(
		caller.objects[testModemPath][modemInterface],
		"Bearers",
	); len(paths) != 1 {
		t.Fatalf("bearers after failed cleanup = %v, want unresolved bearer", paths)
	}
}

func TestApplyDeviceConfigurationCleanupOutlivesCanceledRequest(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	ctx, cancel := context.WithCancel(context.Background())
	caller := &configurationCaller{
		objects:    objects,
		cancelCall: cancel,
	}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)
	current, err := provider.ReadDeviceConfiguration(ctx, lineID)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	caller.calls = nil

	_, err = provider.ApplyGenericDeviceConfiguration(
		ctx,
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "connect-data-canceled",
			LineID:           lineID,
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationConnectData,
			IPFamily:         "ipv4v6",
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorUnavailable {
		t.Fatalf("error = %#v, want unavailable", err)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
		networkManagerInterface+".GetDevices",
		propertiesInterface+".GetAll",
		networkManagerInterface+".AddAndActivateConnection2",
		networkManagerInterface+".DeactivateConnection",
		propertiesInterface+".GetAll",
	)
}

func TestDeviceConfigurationSkipsStaleBearerReference(t *testing.T) {
	t.Parallel()
	objects := configurationObjects()
	objects[testModemPath][modemInterface]["Bearers"] = dbus.MakeVariant([]dbus.ObjectPath{
		"/org/freedesktop/ModemManager1/Bearer/missing",
	})
	caller := &configurationCaller{objects: objects}
	provider := newTestProvider(caller)

	configuration, err := provider.ReadDeviceConfiguration(
		context.Background(),
		parsedLineID(objects, provider.ids),
	)
	if err != nil {
		t.Fatalf("ReadDeviceConfiguration() error = %v", err)
	}
	if len(configuration.DataConnections) != 0 {
		t.Fatalf("data connections = %+v", configuration.DataConnections)
	}
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
	initialEPSBearerPath := dbus.ObjectPath(
		"/org/freedesktop/ModemManager1/Bearer/initial_eps",
	)
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["Revision"] = dbus.MakeVariant("fixture-fw-1")
	objects[testModemPath][modemInterface]["HardwareRevision"] = dbus.MakeVariant("fixture-hw-1")
	objects[testModemPath][modemInterface]["AccessTechnologies"] =
		dbus.MakeVariant(accessTechnologyLTE)
	objects[testModemPath][modemInterface]["Ports"] = dbus.MakeVariant([][]any{
		{"ttyUSB2", uint32(3)},
		{"cdc-wdm0", uint32(6)},
	})
	objects[testModemPath][modemInterface]["PowerState"] = dbus.MakeVariant(uint32(3))
	objects[testModemPath][modemInterface]["Bearers"] = dbus.MakeVariant([]dbus.ObjectPath{})
	objects[testModemPath][simpleInterface] = Properties{}
	objects[testModemPath][signalInterface] = Properties{
		"Lte": dbus.MakeVariant(map[string]dbus.Variant{
			"snr": dbus.MakeVariant(float64(9.5)),
		}),
	}
	objects[testModemPath][modem3GPPInterface] = Properties{
		"InitialEpsBearer": dbus.MakeVariant(initialEPSBearerPath),
	}
	objects[initialEPSBearerPath] = Interfaces{
		bearerInterface: {
			"Properties": dbus.MakeVariant(map[string]dbus.Variant{
				"apn":     dbus.MakeVariant("automatic.example"),
				"ip-type": dbus.MakeVariant(uint32(bearerIPFamilyIPv4V6)),
			}),
		},
	}
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
