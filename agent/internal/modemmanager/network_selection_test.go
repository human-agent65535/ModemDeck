package modemmanager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type networkSelectionCaller struct {
	mu          sync.Mutex
	objects     ManagedObjects
	scanBody    []any
	scanError   error
	registerErr error
	scanStarted chan struct{}
	scanRelease <-chan struct{}
	startOnce   sync.Once
	calls       []dbusInvocation
}

func (caller *networkSelectionCaller) Call(
	ctx context.Context,
	destination string,
	path dbus.ObjectPath,
	method string,
	flags dbus.Flags,
	args ...any,
) ([]any, error) {
	caller.mu.Lock()
	caller.calls = append(caller.calls, dbusInvocation{
		Context:     ctx,
		Destination: destination,
		Path:        path,
		Method:      method,
		Flags:       flags,
		Args:        append([]any(nil), args...),
	})
	caller.mu.Unlock()

	switch method {
	case objectManagerInterface + ".GetManagedObjects":
		return []any{caller.objects}, nil
	case modem3GPPInterface + ".Scan":
		if caller.scanStarted != nil {
			caller.startOnce.Do(func() {
				close(caller.scanStarted)
			})
		}
		if caller.scanRelease != nil {
			select {
			case <-caller.scanRelease:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return caller.scanBody, caller.scanError
	case modem3GPPInterface + ".Register":
		return []any{}, caller.registerErr
	default:
		return nil, errors.New("unexpected D-Bus method: " + method)
	}
}

func (caller *networkSelectionCaller) invocations() []dbusInvocation {
	caller.mu.Lock()
	defer caller.mu.Unlock()
	return append([]dbusInvocation(nil), caller.calls...)
}

func TestScanNetworksParsesDeduplicatesAndSortsResults(t *testing.T) {
	objects := networkSelectionObjects()
	ids := newInstanceIDsForTest("boot-network-scan")
	lineID := ParseManagedObjects(objects, ids).Lines[0].ID
	now := time.Date(2026, 7, 24, 8, 9, 10, 0, time.UTC)
	caller := &networkSelectionCaller{
		objects: objects,
		scanBody: []any{[]map[string]dbus.Variant{
			{
				"status":            dbus.MakeVariant(uint32(3)),
				"operator-code":     dbus.MakeVariant("44010"),
				"operator-long":     dbus.MakeVariant("NTT DOCOMO"),
				"operator-short":    dbus.MakeVariant("DOCOMO"),
				"access-technology": dbus.MakeVariant(uint32(1 << 5)),
			},
			{
				"status":            dbus.MakeVariant(uint32(1)),
				"operator-code":     dbus.MakeVariant("44051"),
				"operator-long":     dbus.MakeVariant("KDDI"),
				"operator-short":    dbus.MakeVariant("KDDI"),
				"access-technology": dbus.MakeVariant(uint32(1 << 14)),
			},
			{
				"status":            dbus.MakeVariant(uint32(2)),
				"operator-code":     dbus.MakeVariant("44051"),
				"access-technology": dbus.MakeVariant(uint32(1 << 4)),
			},
		}},
	}
	provider := newProvider(caller, ids)
	provider.now = func() time.Time { return now }

	result, err := provider.ScanNetworks(context.Background(), domain.NetworkScanRequest{
		RequestID: "request-scan",
		LineID:    lineID,
	})
	if err != nil {
		t.Fatalf("ScanNetworks() error = %v", err)
	}
	if result.RequestID != "request-scan" ||
		result.LineID != lineID ||
		!result.ObservedAt.Equal(now) ||
		len(result.Networks) != 2 {
		t.Fatalf("result = %+v", result)
	}
	current := result.Networks[0]
	if current.Status != domain.NetworkAvailabilityCurrent ||
		current.OperatorCode != "44051" ||
		current.OperatorLong != "KDDI" ||
		current.OperatorShort != "KDDI" ||
		current.AccessTechnologies != (1<<4)|(1<<14) ||
		len(current.AccessTechnologyNames) != 2 ||
		current.AccessTechnologyNames[0] != "edge" ||
		current.AccessTechnologyNames[1] != "lte" {
		t.Fatalf("current network = %+v", current)
	}
	if result.Networks[1].Status != domain.NetworkAvailabilityForbidden ||
		result.Networks[1].OperatorCode != "44010" {
		t.Fatalf("forbidden network = %+v", result.Networks[1])
	}

	invocations := caller.invocations()
	assertMethods(
		t,
		invocations,
		objectManagerInterface+".GetManagedObjects",
		modem3GPPInterface+".Scan",
	)
	if invocations[1].Path != testModemPath ||
		invocations[1].Flags != dbus.FlagNoAutoStart ||
		len(invocations[1].Args) != 0 {
		t.Fatalf("Scan invocation = %+v", invocations[1])
	}
}

func TestSetNetworkSelectionUsesEmptyOperatorForAutoAndMCCMNCForManual(t *testing.T) {
	for _, test := range []struct {
		name         string
		mode         domain.NetworkSelectionMode
		operatorCode string
		wantArgument string
	}{
		{
			name:         "auto",
			mode:         domain.NetworkSelectionModeAuto,
			wantArgument: "",
		},
		{
			name:         "manual",
			mode:         domain.NetworkSelectionModeManual,
			operatorCode: "310260",
			wantArgument: "310260",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := networkSelectionObjects()
			ids := newInstanceIDsForTest("boot-network-" + test.name)
			lineID := ParseManagedObjects(objects, ids).Lines[0].ID
			caller := &networkSelectionCaller{objects: objects}
			provider := newProvider(caller, ids)
			now := time.Date(2026, 7, 24, 8, 10, 0, 0, time.UTC)
			provider.now = func() time.Time { return now }

			receipt, err := provider.SetNetworkSelection(
				context.Background(),
				domain.ApplyNetworkSelectionRequest{
					RequestID:    "request-" + test.name,
					LineID:       lineID,
					Mode:         test.mode,
					OperatorCode: test.operatorCode,
				},
			)
			if err != nil {
				t.Fatalf("SetNetworkSelection() error = %v", err)
			}
			if receipt.RequestID != "request-"+test.name ||
				receipt.LineID != lineID ||
				receipt.Mode != test.mode ||
				receipt.OperatorCode != test.operatorCode ||
				!receipt.AppliedAt.Equal(now) {
				t.Fatalf("receipt = %+v", receipt)
			}
			invocations := caller.invocations()
			assertMethods(
				t,
				invocations,
				objectManagerInterface+".GetManagedObjects",
				modem3GPPInterface+".Register",
			)
			if got := invocations[1].Args; len(got) != 1 || got[0] != test.wantArgument {
				t.Fatalf("Register args = %#v, want %q", got, test.wantArgument)
			}
		})
	}
}

func TestSetNetworkSelectionRejectsInvalidRequestsBeforeDBus(t *testing.T) {
	tests := []domain.ApplyNetworkSelectionRequest{
		{RequestID: "request", LineID: "line", Mode: "automatic"},
		{RequestID: "request", LineID: "line", Mode: domain.NetworkSelectionModeAuto, OperatorCode: "44051"},
		{RequestID: "request", LineID: "line", Mode: domain.NetworkSelectionModeManual},
		{RequestID: "request", LineID: "line", Mode: domain.NetworkSelectionModeManual, OperatorCode: "44A51"},
		{RequestID: "request", LineID: "line", Mode: domain.NetworkSelectionModeManual, OperatorCode: "4405"},
	}
	for index, request := range tests {
		caller := &networkSelectionCaller{objects: networkSelectionObjects()}
		provider := newProvider(caller, newInstanceIDsForTest("boot-invalid"))
		_, err := provider.SetNetworkSelection(context.Background(), request)
		assertOperationErrorCode(t, err, domain.ErrorInvalidArgument)
		if invocations := caller.invocations(); len(invocations) != 0 {
			t.Fatalf("case %d reached D-Bus: %+v", index, invocations)
		}
	}
}

func TestScanNetworksRejectsMalformedDBusResponses(t *testing.T) {
	tests := []struct {
		name string
		body []any
	}{
		{
			name: "outer body",
			body: []any{"not an array of dictionaries"},
		},
		{
			name: "status",
			body: []any{[]map[string]dbus.Variant{{
				"status":        dbus.MakeVariant("available"),
				"operator-code": dbus.MakeVariant("44051"),
			}}},
		},
		{
			name: "operator code",
			body: []any{[]map[string]dbus.Variant{{
				"status":        dbus.MakeVariant(uint32(1)),
				"operator-code": dbus.MakeVariant("44A51"),
			}}},
		},
		{
			name: "optional access technology",
			body: []any{[]map[string]dbus.Variant{{
				"status":            dbus.MakeVariant(uint32(1)),
				"operator-code":     dbus.MakeVariant("44051"),
				"access-technology": dbus.MakeVariant("lte"),
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := networkSelectionObjects()
			ids := newInstanceIDsForTest("boot-malformed")
			lineID := ParseManagedObjects(objects, ids).Lines[0].ID
			caller := &networkSelectionCaller{objects: objects, scanBody: test.body}
			provider := newProvider(caller, ids)
			_, err := provider.ScanNetworks(context.Background(), domain.NetworkScanRequest{
				RequestID: "request-scan",
				LineID:    lineID,
			})
			assertOperationErrorCode(t, err, domain.ErrorInternal)
		})
	}
}

func TestNetworkSelectionRejectsUnsupportedLine(t *testing.T) {
	objects := emptyLineObjects(false, false)
	ids := newInstanceIDsForTest("boot-unsupported")
	lineID := ParseManagedObjects(objects, ids).Lines[0].ID
	caller := &networkSelectionCaller{objects: objects}
	provider := newProvider(caller, ids)

	_, err := provider.ScanNetworks(context.Background(), domain.NetworkScanRequest{
		RequestID: "request-scan",
		LineID:    lineID,
	})
	assertOperationErrorCode(t, err, domain.ErrorNotSupported)
	assertMethods(t, caller.invocations(), objectManagerInterface+".GetManagedObjects")
}

func TestScanNetworksHonorsCallerDeadline(t *testing.T) {
	objects := networkSelectionObjects()
	ids := newInstanceIDsForTest("boot-timeout")
	lineID := ParseManagedObjects(objects, ids).Lines[0].ID
	neverRelease := make(chan struct{})
	caller := &networkSelectionCaller{
		objects:     objects,
		scanRelease: neverRelease,
	}
	provider := newProvider(caller, ids)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := provider.ScanNetworks(ctx, domain.NetworkScanRequest{
		RequestID: "request-timeout",
		LineID:    lineID,
	})
	assertOperationErrorCode(t, err, domain.ErrorUnavailable)
}

func TestNetworkSelectionOperationsConflictOnlyOnSameLine(t *testing.T) {
	objects := networkSelectionObjects()
	secondModemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/1")
	objects[secondModemPath] = Interfaces{
		modemInterface: {
			"Manufacturer":        dbus.MakeVariant("Quectel"),
			"Model":               dbus.MakeVariant("EG25-G"),
			"EquipmentIdentifier": dbus.MakeVariant("867530900000002"),
			"DeviceIdentifier":    dbus.MakeVariant("device-1"),
			"Physdev":             dbus.MakeVariant("/sys/devices/usb1/1-3"),
			"PrimaryPort":         dbus.MakeVariant("cdc-wdm1"),
			"State":               dbus.MakeVariant(int32(8)),
		},
		modem3GPPInterface: {
			"RegistrationState": dbus.MakeVariant(uint32(2)),
		},
	}
	ids := newInstanceIDsForTest("boot-concurrent")
	parsed := ParseManagedObjects(objects, ids)
	var lineID string
	var secondLineID string
	for id, path := range parsed.LinePaths {
		switch path {
		case testModemPath:
			lineID = id
		case secondModemPath:
			secondLineID = id
		}
	}
	if lineID == "" || secondLineID == "" {
		t.Fatalf("line mappings = %+v", parsed.LinePaths)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	caller := &networkSelectionCaller{
		objects:     objects,
		scanBody:    []any{[]map[string]dbus.Variant{}},
		scanStarted: started,
		scanRelease: release,
	}
	provider := newProvider(caller, ids)
	scanResult := make(chan error, 1)
	go func() {
		_, err := provider.ScanNetworks(context.Background(), domain.NetworkScanRequest{
			RequestID: "request-scan",
			LineID:    lineID,
		})
		scanResult <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scan did not reach D-Bus")
	}

	_, err := provider.SetNetworkSelection(
		context.Background(),
		domain.ApplyNetworkSelectionRequest{
			RequestID: "request-auto",
			LineID:    lineID,
			Mode:      domain.NetworkSelectionModeAuto,
		},
	)
	assertOperationErrorCode(t, err, domain.ErrorConflict)

	receipt, err := provider.SetNetworkSelection(
		context.Background(),
		domain.ApplyNetworkSelectionRequest{
			RequestID: "request-other-line",
			LineID:    secondLineID,
			Mode:      domain.NetworkSelectionModeAuto,
		},
	)
	if err != nil {
		t.Fatalf("SetNetworkSelection(second line) error = %v", err)
	}
	if receipt.LineID != secondLineID {
		t.Fatalf("second-line receipt = %+v", receipt)
	}

	close(release)
	if err := <-scanResult; err != nil {
		t.Fatalf("ScanNetworks() error = %v", err)
	}
	registers := 0
	for _, invocation := range caller.invocations() {
		if invocation.Method == modem3GPPInterface+".Register" {
			registers++
			if invocation.Path != secondModemPath {
				t.Fatalf("same-line conflicting request reached Register: %+v", invocation)
			}
		}
	}
	if registers != 1 {
		t.Fatalf("Register invocation count = %d, want 1", registers)
	}
}

func networkSelectionObjects() ManagedObjects {
	objects := emptyLineObjects(false, false)
	objects[testModemPath][modem3GPPInterface] = Properties{
		"RegistrationState": dbus.MakeVariant(uint32(2)),
	}
	return objects
}

func assertOperationErrorCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	operationError, ok := domain.AsOperationError(err)
	if !ok {
		t.Fatalf("error = %v, want OperationError(%s)", err, code)
	}
	if operationError.Code != code {
		t.Fatalf("error code = %s, want %s: %v", operationError.Code, code, err)
	}
}
