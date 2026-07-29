package modemmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestEmergencyHangupAllEndsModemManagerCalls(t *testing.T) {
	t.Parallel()
	objects := oneLineObjects("/org/freedesktop/ModemManager1/Call/1", 4)
	caller := newFakeCaller(objects)
	caller.terminateOnHangup = true
	provider := newTestProvider(caller)

	if err := provider.EmergencyHangupAll(context.Background(), "test"); err != nil {
		t.Fatalf("EmergencyHangupAll() error = %v", err)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		callInterface+".Hangup",
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestEmergencyHangupAllEndsQuectelATCalls(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	caller := newFakeCaller(objects)
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,0,0,0,"+818012345678",145`
	caller.terminateATOnHangup = true
	provider := newTestProvider(caller)

	if err := provider.EmergencyHangupAll(context.Background(), "test"); err != nil {
		t.Fatalf("EmergencyHangupAll() error = %v", err)
	}
	assertATInvocation(t, caller.invocations(), quectelCallListQuery)
	assertATInvocation(t, caller.invocations(), quectelHangupCall)
	if countMethod(caller.invocations(), modemInterface+".Reset") != 0 {
		t.Fatal("successful AT hangup reset the modem")
	}
}

func TestEmergencyHangupAllResetsUnverifiableATLine(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	caller := newFakeCaller(objects)
	caller.atCommandErrors[quectelCallListQuery] = errors.New("AT relay failed")
	caller.atCommandErrors[quectelHangupCall] = errors.New("AT hangup failed")
	provider := newTestProvider(caller)

	if err := provider.EmergencyHangupAll(context.Background(), "test"); err != nil {
		t.Fatalf("EmergencyHangupAll() error = %v", err)
	}
	if countMethod(caller.invocations(), modemInterface+".Reset") != 1 {
		t.Fatalf(
			"modem reset count = %d, want 1",
			countMethod(caller.invocations(), modemInterface+".Reset"),
		)
	}
}
