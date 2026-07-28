package modemmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestQuectelATCallControlWithoutModemManagerVoice(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")

	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atCommandErrors[quectelPCMEnable] = errors.New("unsupported")
	caller.atResponses[quectelCallListQuery] = ""
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	line := snapshot.Lines[0]
	if line.Capabilities.VoiceInterface ||
		!line.Capabilities.Dial ||
		!line.Capabilities.AnswerCall ||
		!line.Capabilities.RejectCall ||
		!line.Capabilities.HangupCall ||
		!line.Capabilities.SendDTMF {
		t.Fatalf("AT call-control capabilities = %+v", line.Capabilities)
	}
	if line.Capabilities.Media {
		t.Fatalf("QPCMV rejection exposed media capability: %+v", line.Capabilities)
	}

	receipt, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "request-at-dial",
		LineID:    line.ID,
		Number:    "+81 (80) 1234-5678",
	})
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if receipt.ResourceID == "" {
		t.Fatal("StartCall() returned an empty call ID")
	}
	assertATInvocation(t, caller.invocations(), "ATD+818012345678;")

	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,2,0,0,"+818012345678",145`
	dialing, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("dialing Snapshot() error = %v", err)
	}
	if len(dialing.Calls) != 1 ||
		dialing.Calls[0].ID != receipt.ResourceID ||
		dialing.Calls[0].State != "dialing" {
		t.Fatalf("dialing calls = %+v", dialing.Calls)
	}
}

func TestQuectelATIncomingCallCommands(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("Quectel")
	properties["Model"] = dbus.MakeVariant("QDC507")
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")

	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atCommandErrors[quectelPCMEnable] = errors.New("unsupported")
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 2,1,4,0,0,"+818012345678",145`
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Calls) != 1 || snapshot.Calls[0].State != "ringing-in" {
		t.Fatalf("incoming calls = %+v", snapshot.Calls)
	}
	callID := snapshot.Calls[0].ID
	if _, err := provider.AnswerCall(
		context.Background(),
		domain.CallCommandRequest{RequestID: "request-at-answer", CallID: callID},
	); err != nil {
		t.Fatalf("AnswerCall() error = %v", err)
	}
	assertATInvocation(t, caller.invocations(), quectelAnswerCall)

	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 2,1,0,0,0,"+818012345678",145`
	if _, err := provider.SendDTMF(
		context.Background(),
		domain.DTMFRequest{
			RequestID: "request-at-dtmf",
			CallID:    callID,
			Digits:    "12#",
		},
	); err != nil {
		t.Fatalf("SendDTMF() error = %v", err)
	}
	for _, command := range []string{`AT+VTS="1"`, `AT+VTS="2"`, `AT+VTS="#"`} {
		assertATInvocation(t, caller.invocations(), command)
	}

	if _, err := provider.HangupCall(
		context.Background(),
		domain.CallCommandRequest{RequestID: "request-at-hangup", CallID: callID},
	); err != nil {
		t.Fatalf("HangupCall() error = %v", err)
	}
	assertATInvocation(t, caller.invocations(), quectelHangupCall)
}

func TestUSBCFGReadFailureDoesNotDiscardExistingVoiceControl(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("Quectel")
	properties["Model"] = dbus.MakeVariant("EG25-G")
	properties["Revision"] = dbus.MakeVariant("EG25GGCGA0.302")

	caller := newFakeCaller(objects)
	caller.atCommandErrors[quectelUSBVoiceQuery] = errors.New("firmware returned ERROR")
	caller.atCommandErrors[quectelCallListQuery] = errors.New("AT port busy")
	caller.atCommandErrors[quectelPCMEnable] = errors.New("not active")
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	line := snapshot.Lines[0]
	if !line.Capabilities.Dial ||
		line.VoiceVerification == nil ||
		line.VoiceVerification.USBConfiguration != voiceVerificationReadFailed {
		t.Fatalf("voice state after USBCFG read failure = %+v", line)
	}
}

func TestQDC507WhitelistDoesNotDependOnQuectelBrandText(t *testing.T) {
	t.Parallel()

	if !requiresQuectelPCMProbe(domain.Line{
		Manufacturer: "Baiwang",
		Model:        "QDC507",
		Revision:     "QDC507GLEFM21",
	}) {
		t.Fatal("QDC507 whitelist unexpectedly required a Quectel brand string")
	}
	if requiresQuectelPCMProbe(domain.Line{
		Manufacturer: "Unknown",
		Model:        "Generic LTE modem",
		Revision:     "1.0",
	}) {
		t.Fatal("generic modem entered the Quectel voice whitelist")
	}
}

func TestParseQuectelCLCC(t *testing.T) {
	t.Parallel()

	records, err := parseQuectelCLCC(
		"+CLCC: 1,0,3,0,0,\"+818012345678\",145\r\n" +
			"+CLCC: 2,1,5,0,1,\"0120\",129",
	)
	if err != nil {
		t.Fatalf("parseQuectelCLCC() error = %v", err)
	}
	if len(records) != 2 ||
		records[0].direction != "outgoing" ||
		records[0].stateCode != 2 ||
		records[1].direction != "incoming" ||
		records[1].stateCode != 6 ||
		!records[1].multiparty {
		t.Fatalf("records = %+v", records)
	}
}

func TestATCallNumberMatchingAcceptsInternationalPrefixForms(t *testing.T) {
	t.Parallel()

	if !atNumbersEqual("+8613800000001", "008618636812882") ||
		!atNumbersEqual("8618636812882", "+8613800000001") {
		t.Fatal("international prefix variants did not share one call identity")
	}
	if atNumbersEqual("+8613800000001", "+818012345678") {
		t.Fatal("different numbers shared one call identity")
	}
}

func TestQuectelDialCommandRejectsATInjection(t *testing.T) {
	t.Parallel()

	if command, number, ok := quectelDialCommand("+81 (80) 1234-5678"); !ok ||
		command != "ATD+818012345678;" ||
		number != "+818012345678" {
		t.Fatalf("normalized dial = %q, %q, %v", command, number, ok)
	}
	for _, value := range []string{
		"",
		"AT+CFUN=1",
		"+8180;AT+CFUN=1",
		"+8180\rAT+CFUN=1",
	} {
		if _, _, ok := quectelDialCommand(value); ok {
			t.Fatalf("quectelDialCommand(%q) accepted unsafe input", value)
		}
	}
}

func assertATInvocation(
	t *testing.T,
	invocations []dbusInvocation,
	command string,
) {
	t.Helper()
	for _, invocation := range invocations {
		if invocation.Method != modemInterface+".Command" ||
			len(invocation.Args) != 2 {
			continue
		}
		if actual, ok := invocation.Args[0].(string); ok && actual == command {
			return
		}
	}
	t.Fatalf("AT command %q was not invoked", command)
}
