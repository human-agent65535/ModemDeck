package modemmanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,2,0,0,"+818012345678",145`
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

func TestQuectelATDialTransportErrorUsesObservedCall(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atCommandErrors[quectelPCMEnable] = errors.New("unsupported")
	caller.atResponses[quectelCallListQuery] = ""
	dialCommand := "ATD+818012345678;"
	caller.atCommandErrors[dialCommand] = errors.New("AT transport disconnected")
	provider := newTestProvider(caller)
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	lineID := snapshot.Lines[0].ID
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,2,0,0,"+818012345678",145`

	receipt, err := provider.StartCall(
		context.Background(),
		domain.StartCallRequest{
			RequestID: "request-ambiguous-at-dial",
			LineID:    lineID,
			Number:    "+818012345678",
		},
	)
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if receipt.ResourceID == "" {
		t.Fatal("StartCall() returned an empty call ID")
	}
	invocations := caller.invocations()
	assertATInvocationCount(t, invocations, dialCommand, 1)
	assertATInvocationCount(t, invocations, quectelHangupCall, 0)
	if countMethod(invocations, modemInterface+".Reset") != 0 {
		t.Fatalf("Reset calls = %d, want 0", countMethod(invocations, modemInterface+".Reset"))
	}
}

func TestQuectelATDialRequiresCLCCAndRollsBackOnce(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
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
	lineID := snapshot.Lines[0].ID

	_, err = provider.StartCall(
		context.Background(),
		domain.StartCallRequest{
			RequestID: "request-unobserved-at-dial",
			LineID:    lineID,
			Number:    "+818012345678",
		},
	)
	assertOperationError(t, err, domain.ErrorVerification, "start_call")
	invocations := caller.invocations()
	assertATInvocationCount(t, invocations, "ATD+818012345678;", 1)
	assertATInvocationCount(t, invocations, quectelHangupCall, 1)
	if countMethod(invocations, modemInterface+".Reset") != 0 {
		t.Fatalf("Reset calls = %d, want 0", countMethod(invocations, modemInterface+".Reset"))
	}
	if provider.hasATLineActivity(lineID) {
		t.Fatal("failed dial retained synthetic AT call state")
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
	provider.observeATCalls(context.Background())

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
	provider.observeATCalls(context.Background())
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

	caller.atResponses[quectelCallListQuery] = ""
	if _, err := provider.HangupCall(
		context.Background(),
		domain.CallCommandRequest{RequestID: "request-at-hangup", CallID: callID},
	); err != nil {
		t.Fatalf("HangupCall() error = %v", err)
	}
	assertATInvocation(t, caller.invocations(), quectelHangupCall)
}

func TestQuectelATRejectWaitingCallPreservesActiveCall(t *testing.T) {
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
	caller.atResponses[quectelCallListQuery] = strings.Join([]string{
		`+CLCC: 1,0,0,0,0,"+818011111111",145`,
		`+CLCC: 2,1,5,0,0,"+818022222222",145`,
	}, "\r\n")
	provider := newTestProvider(caller)
	provider.observeATCalls(context.Background())

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	var waitingCallID string
	for _, call := range snapshot.Calls {
		if call.StateCode == 6 {
			waitingCallID = call.ID
		}
	}
	if waitingCallID == "" {
		t.Fatalf("waiting call was not projected: %+v", snapshot.Calls)
	}

	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,0,0,0,"+818011111111",145`
	before := len(caller.invocations())
	if _, err := provider.RejectCall(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "request-at-reject-waiting",
			CallID:    waitingCallID,
		},
	); err != nil {
		t.Fatalf("RejectCall() error = %v", err)
	}

	invocations := caller.invocations()[before:]
	assertATInvocationCount(t, invocations, quectelRejectWaitingCall, 1)
	assertATInvocationCount(t, invocations, quectelHangupCall, 0)
	if countMethod(invocations, modemInterface+".Reset") != 0 {
		t.Fatalf("Reset calls = %d, want 0", countMethod(invocations, modemInterface+".Reset"))
	}

	remaining, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("remaining Snapshot() error = %v", err)
	}
	if len(remaining.Calls) != 1 ||
		remaining.Calls[0].Number != "+818011111111" ||
		remaining.Calls[0].StateCode != 4 {
		t.Fatalf("remaining calls = %+v, want only the active call", remaining.Calls)
	}
}

func TestQuectelATHangupOKWithoutTerminationForcesModemReset(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atCommandErrors[quectelPCMEnable] = errors.New("unsupported")
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,0,0,0,"+818012345678",145`
	provider := newTestProvider(caller)
	provider.observeATCalls(context.Background())

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil || len(snapshot.Calls) != 1 {
		t.Fatalf("Snapshot() calls = %+v, error = %v", snapshot.Calls, err)
	}
	before := len(caller.invocations())
	_, err = provider.HangupCall(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "request-at-stuck-call",
			CallID:    snapshot.Calls[0].ID,
		},
	)
	assertOperationError(t, err, domain.ErrorVerification, "hangup_call")

	after := caller.invocations()[before:]
	assertATInvocation(t, after, quectelHangupCall)
	resetFound := false
	for _, invocation := range after {
		if invocation.Method == modemInterface+".Reset" &&
			invocation.Path == testModemPath {
			resetFound = true
		}
	}
	if !resetFound {
		t.Fatalf("stuck call did not force a modem reset: %+v", after)
	}
}

func TestQuectelATHangupFailureForcesModemReset(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atCommandErrors[quectelPCMEnable] = errors.New("unsupported")
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,0,0,0,"+818012345678",145`
	provider := newTestProvider(caller)
	provider.observeATCalls(context.Background())

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil || len(snapshot.Calls) != 1 {
		t.Fatalf("Snapshot() calls = %+v, error = %v", snapshot.Calls, err)
	}
	resetProbeKey := voiceProbeKey(provider.ids, snapshot.Lines[0], testModemPath)
	const otherProbeKey = "other-modem-voice-model"
	provider.voiceProbeMu.Lock()
	provider.voiceProbes[otherProbeKey] = voiceProbeResult{callControl: true}
	provider.voiceProbeMu.Unlock()
	caller.atCommandErrors[quectelHangupCall] = errors.New("AT+CHUP failed")
	before := len(caller.invocations())

	_, err = provider.HangupCall(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "request-at-force-reset",
			CallID:    snapshot.Calls[0].ID,
		},
	)
	assertOperationError(t, err, domain.ErrorVerification, "hangup_call")

	after := caller.invocations()[before:]
	resetFound := false
	for _, invocation := range after {
		if invocation.Method == modemInterface+".Reset" &&
			invocation.Path == testModemPath {
			resetFound = true
		}
	}
	if !resetFound {
		t.Fatalf("forced modem reset was not invoked: %+v", after)
	}
	if provider.hasATLineActivity(snapshot.Lines[0].ID) {
		t.Fatal("forced modem reset retained stale AT call state")
	}
	if _, found := provider.voiceProbeResult(resetProbeKey); found {
		t.Fatal("forced modem reset retained the affected voice model")
	}
	if _, found := provider.voiceProbeResult(otherProbeKey); !found {
		t.Fatal("forced modem reset cleared another modem's voice model")
	}
}

func TestQuectelATAnswerConfirmsPCMInitializedDuringModeling(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	caller.atResponses[quectelCallListQuery] = ""
	provider := newTestProvider(caller)

	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("initial Snapshot() error = %v", err)
	}
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,1,4,0,0,"+818012345678",145`
	provider.observeATCalls(context.Background())

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil || len(snapshot.Calls) != 1 {
		t.Fatalf("Snapshot() calls = %+v, error = %v", snapshot.Calls, err)
	}
	if _, err := provider.AnswerCall(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "answer-with-media",
			CallID:    snapshot.Calls[0].ID,
		},
	); err != nil {
		t.Fatalf("AnswerCall() error = %v", err)
	}

	lastEnable := -1
	answer := -1
	for index, invocation := range caller.invocations() {
		if invocation.Method != modemInterface+".Command" || len(invocation.Args) == 0 {
			continue
		}
		switch invocation.Args[0] {
		case quectelPCMEnable:
			lastEnable = index
		case quectelAnswerCall:
			answer = index
		}
	}
	if lastEnable < 0 || answer < 0 || lastEnable > answer {
		t.Fatalf("PCM enable index = %d, ATA index = %d", lastEnable, answer)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)

	caller.atResponses[quectelCallListQuery] = ""
	provider.observeATCalls(context.Background())
	assertATInvocationCount(t, caller.invocations(), "AT+QPCMV=0", 0)
}

func TestATCallObserverPublishesOnlyStateChanges(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, true)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	caller := newFakeCaller(objects)
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atCommandErrors[quectelPCMEnable] = errors.New("unsupported")
	caller.atResponses[quectelCallListQuery] = ""
	provider := newTestProvider(caller)
	events, err := provider.SubscribeChanges(context.Background())
	if err != nil {
		t.Fatalf("SubscribeChanges() error = %v", err)
	}

	provider.observeATCalls(context.Background())
	assertNoChangeEvent(t, events)

	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,1,4,0,0,"+818012345678",145`
	provider.observeATCalls(context.Background())
	assertChangeEvent(t, events)

	provider.observeATCalls(context.Background())
	assertNoChangeEvent(t, events)

	caller.atResponses[quectelCallListQuery] = ""
	provider.observeATCalls(context.Background())
	assertChangeEvent(t, events)
}

func assertChangeEvent(t *testing.T, events <-chan domain.ChangeEvent) {
	t.Helper()
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("expected call state change event")
	}
}

func assertNoChangeEvent(t *testing.T, events <-chan domain.ChangeEvent) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected call state change event: %+v", event)
	case <-time.After(10 * time.Millisecond):
	}
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
