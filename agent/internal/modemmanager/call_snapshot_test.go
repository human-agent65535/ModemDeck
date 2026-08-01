package modemmanager

import (
	"context"
	"strconv"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestSnapshotHydratesCallMissingFromManagedObjects(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	callPath := testCallPath(7)
	objects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{callPath})
	caller := newFakeCaller(objects)
	caller.externalCalls[callPath] = testCallProperties(3, 1)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Calls) != 1 {
		t.Fatalf("calls = %+v, want one hydrated call", snapshot.Calls)
	}
	if snapshot.Calls[0].State != "ringing-in" ||
		snapshot.Calls[0].Direction != "incoming" {
		t.Fatalf("hydrated call = %+v", snapshot.Calls[0])
	}
	if len(snapshot.Lines) != 1 ||
		len(snapshot.Lines[0].CallIDs) != 1 ||
		snapshot.Lines[0].CallIDs[0] != snapshot.Calls[0].ID {
		t.Fatalf("line call references = %+v", snapshot.Lines)
	}

	calls := caller.invocations()
	assertInvocationPresent(
		t,
		calls,
		callPath,
		propertiesInterface+".GetAll",
		callInterface,
	)
}

func TestSnapshotFailsWhenVoiceModemStateIsUnknown(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		state *dbus.Variant
	}{
		{name: "missing"},
		{name: "wrong type", state: variantPointer(dbus.MakeVariant(uint32(8)))},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := emptyLineObjects(true, false)
			if test.state == nil {
				delete(objects[testModemPath][modemInterface], "State")
			} else {
				objects[testModemPath][modemInterface]["State"] = *test.state
			}
			provider := newTestProvider(newFakeCaller(objects))
			_, err := provider.Snapshot(context.Background())
			assertOperationError(t, err, domain.ErrorUnavailable, "snapshot")
		})
	}
}

func variantPointer(value dbus.Variant) *dbus.Variant {
	return &value
}

func TestSnapshotRefreshesHydratedCallProperties(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	callPath := testCallPath(8)
	objects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{callPath})
	caller := newFakeCaller(objects)
	caller.externalCalls[callPath] = testCallProperties(2, 2)
	provider := newTestProvider(caller)

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	caller.externalCalls[callPath] = testCallProperties(4, 2)
	second, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}

	if len(first.Calls) != 1 || first.Calls[0].State != "ringing-out" {
		t.Fatalf("first calls = %+v", first.Calls)
	}
	if len(second.Calls) != 1 || second.Calls[0].State != "active" {
		t.Fatalf("second calls = %+v", second.Calls)
	}
	if countInvocations(
		caller.invocations(),
		callPath,
		propertiesInterface+".GetAll",
	) != 2 {
		t.Fatalf("call properties were not refreshed on every snapshot")
	}
}

func TestSnapshotListsCallsWhenVoicePropertyIsMissing(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	delete(objects[testModemPath][voiceInterface], "Calls")
	callPath := testCallPath(9)
	caller := newFakeCaller(objects)
	caller.callLists[testModemPath] = []dbus.ObjectPath{callPath}
	caller.externalCalls[callPath] = testCallProperties(3, 1)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Calls) != 1 || snapshot.Calls[0].State != "ringing-in" {
		t.Fatalf("calls = %+v, want listed incoming call", snapshot.Calls)
	}
	assertInvocationPresent(
		t,
		caller.invocations(),
		testModemPath,
		voiceInterface+".ListCalls",
	)
}

func TestSnapshotSkipsCallRemovedDuringHydration(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	callPath := testCallPath(10)
	objects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{callPath})
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Calls) != 0 || len(snapshot.Lines[0].CallIDs) != 0 {
		t.Fatalf("snapshot retained a vanished call: %+v", snapshot)
	}
}

func TestStartCallRejectsHydratedExistingCall(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	callPath := testCallPath(11)
	objects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{callPath})
	caller := newFakeCaller(objects)
	caller.externalCalls[callPath] = testCallProperties(2, 2)
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)

	_, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "request-1",
		LineID:    lineID,
		Number:    "+818012345678",
	})
	assertOperationError(t, err, domain.ErrorConflict, "start_call")
	if countInvocations(
		caller.invocations(),
		testModemPath,
		voiceInterface+".CreateCall",
	) != 0 {
		t.Fatalf("CreateCall was invoked while an existing call was active")
	}
}

func TestAnswerCallFindsHydratedIncomingCall(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	callPath := testCallPath(12)
	objects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{callPath})
	caller := newFakeCaller(objects)
	caller.externalCalls[callPath] = testCallProperties(3, 1)
	provider := newTestProvider(caller)
	parsed := ParseManagedObjects(ManagedObjects{
		testModemPath: objects[testModemPath],
		callPath: {
			callInterface: caller.externalCalls[callPath],
		},
	}, provider.ids)

	receipt, err := provider.AnswerCall(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "request-1",
			CallID:    parsed.Calls[0].ID,
		},
	)
	if err != nil {
		t.Fatalf("AnswerCall() error = %v", err)
	}
	if receipt.ResourceID != parsed.Calls[0].ID {
		t.Fatalf("receipt = %+v", receipt)
	}
	assertInvocationPresent(
		t,
		caller.invocations(),
		callPath,
		callInterface+".Accept",
	)
}

func testCallPath(id int) dbus.ObjectPath {
	return dbus.ObjectPath(
		"/org/freedesktop/ModemManager1/Call/" + strconv.Itoa(id),
	)
}

func testCallProperties(state, direction int32) Properties {
	return Properties{
		"Number":    dbus.MakeVariant("+818012345678"),
		"Direction": dbus.MakeVariant(direction),
		"State":     dbus.MakeVariant(state),
	}
}

func assertInvocationPresent(
	t *testing.T,
	calls []dbusInvocation,
	path dbus.ObjectPath,
	method string,
	args ...any,
) {
	t.Helper()
	for _, call := range calls {
		if call.Path != path || call.Method != method {
			continue
		}
		if len(call.Args) != len(args) {
			continue
		}
		matches := true
		for index := range args {
			if call.Args[index] != args[index] {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("D-Bus invocation %s on %s with args %#v was not found", method, path, args)
}

func countInvocations(
	calls []dbusInvocation,
	path dbus.ObjectPath,
	method string,
) int {
	count := 0
	for _, call := range calls {
		if call.Path == path && call.Method == method {
			count++
		}
	}
	return count
}
