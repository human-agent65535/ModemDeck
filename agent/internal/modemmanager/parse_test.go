package modemmanager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestParseManagedObjectsMapsLineCallsAndMessages(t *testing.T) {
	ids := newInstanceIDsForTest(":1.41")
	modemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/7")
	simPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SIM/3")
	incomingCallPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/4")
	terminatedCallPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/5")
	incomingMessagePath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/8")
	outgoingMessagePath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/9")

	objects := ManagedObjects{
		modemPath: {
			modemInterface: {
				"Manufacturer":        dbus.MakeVariant("Quectel"),
				"Model":               dbus.MakeVariant("EG25-G"),
				"Revision":            dbus.MakeVariant("EG25GGBR07A08M2G"),
				"DeviceIdentifier":    dbus.MakeVariant("device-identifier"),
				"EquipmentIdentifier": dbus.MakeVariant("867530900000001"),
				"Device":              dbus.MakeVariant("/sys/devices/test"),
				"Physdev":             dbus.MakeVariant("/sys/devices/usb1/1-2"),
				"Drivers":             dbus.MakeVariant([]string{"qmi_wwan", "option"}),
				"Plugin":              dbus.MakeVariant("quectel"),
				"PrimaryPort":         dbus.MakeVariant("cdc-wdm0"),
				"State":               dbus.MakeVariant(int32(8)),
				"PowerState":          dbus.MakeVariant(uint32(3)),
				"AccessTechnologies":  dbus.MakeVariant(uint32(1 << 14)),
				"SignalQuality":       dbus.MakeVariant([]any{uint32(76), true}),
				"OwnNumbers":          dbus.MakeVariant([]string{"+818012345678"}),
				"Sim":                 dbus.MakeVariant(simPath),
			},
			voiceInterface: {
				"Calls": dbus.MakeVariant([]dbus.ObjectPath{
					incomingCallPath,
					terminatedCallPath,
				}),
				"EmergencyOnly": dbus.MakeVariant(false),
			},
			messagingInterface: {
				"Messages": dbus.MakeVariant([]dbus.ObjectPath{
					incomingMessagePath,
					outgoingMessagePath,
				}),
				"SupportedStorages": dbus.MakeVariant([]uint32{1, 2}),
				"DefaultStorage":    dbus.MakeVariant(uint32(2)),
			},
		},
		simPath: {
			simInterface: {
				"SimIdentifier":      dbus.MakeVariant("8986012345678901234"),
				"Imsi":               dbus.MakeVariant("440511234567890"),
				"OperatorIdentifier": dbus.MakeVariant("44051"),
				"OperatorName":       dbus.MakeVariant("KDDI"),
				"EmergencyNumbers":   dbus.MakeVariant([]string{"110", "119"}),
			},
		},
		incomingCallPath: {
			callInterface: {
				"Number":      dbus.MakeVariant("+818000000001"),
				"Direction":   dbus.MakeVariant(int32(1)),
				"State":       dbus.MakeVariant(int32(3)),
				"StateReason": dbus.MakeVariant(int32(2)),
				"Multiparty":  dbus.MakeVariant(true),
				"AudioPort":   dbus.MakeVariant("hw:2,0"),
				"AudioFormat": dbus.MakeVariant(map[string]dbus.Variant{
					"encoding":   dbus.MakeVariant("pcm"),
					"resolution": dbus.MakeVariant("s16le"),
					"rate":       dbus.MakeVariant(uint32(8000)),
				}),
			},
		},
		terminatedCallPath: {
			callInterface: {
				"Number":      dbus.MakeVariant("+818000000002"),
				"Direction":   dbus.MakeVariant(int32(2)),
				"State":       dbus.MakeVariant(int32(7)),
				"StateReason": dbus.MakeVariant(int32(4)),
			},
		},
		incomingMessagePath: {
			smsInterface: {
				"Number":    dbus.MakeVariant("+818000000003"),
				"Text":      dbus.MakeVariant("incoming"),
				"PduType":   dbus.MakeVariant(uint32(1)),
				"State":     dbus.MakeVariant(uint32(3)),
				"Timestamp": dbus.MakeVariant("2026-07-23T10:00:00+09:00"),
			},
		},
		outgoingMessagePath: {
			smsInterface: {
				"Number":    dbus.MakeVariant("+818000000004"),
				"Text":      dbus.MakeVariant("outgoing"),
				"PduType":   dbus.MakeVariant(uint32(2)),
				"State":     dbus.MakeVariant(uint32(5)),
				"Timestamp": dbus.MakeVariant("2026-07-23T10:01:00+09:00"),
			},
		},
	}

	parsed := ParseManagedObjects(objects, ids)
	if len(parsed.Lines) != 1 || len(parsed.Calls) != 2 || len(parsed.Messages) != 2 {
		t.Fatalf("unexpected snapshot sizes: lines=%d calls=%d messages=%d", len(parsed.Lines), len(parsed.Calls), len(parsed.Messages))
	}

	line := parsed.Lines[0]
	if line.ID == string(modemPath) || !strings.HasPrefix(line.ID, "line_") {
		t.Fatalf("line id is not opaque: %q", line.ID)
	}
	if parsed.LinePaths[line.ID] != modemPath {
		t.Fatalf("line path mapping = %q", parsed.LinePaths[line.ID])
	}
	if line.State != "registered" || line.StateCode != 8 ||
		line.Manufacturer != "Quectel" || line.Model != "EG25-G" ||
		line.PrimaryPort != "cdc-wdm0" {
		t.Fatalf("unexpected line: %+v", line)
	}
	if !line.SignalQualityKnown || line.SignalQuality != 76 || !line.SignalQualityRecent {
		t.Fatalf("unexpected signal quality: %+v", line)
	}
	if !line.SIMPresent || line.SIMIdentifier != "8986012345678901234" ||
		line.OperatorIdentifier != "44051" || line.OperatorName != "KDDI" {
		t.Fatalf("unexpected SIM mapping: %+v", line)
	}
	if !line.Capabilities.Dial || !line.Capabilities.AnswerCall ||
		!line.Capabilities.RejectCall || !line.Capabilities.HangupCall ||
		!line.Capabilities.SendDTMF || !line.Capabilities.SendMessage {
		t.Fatalf("implemented line capabilities were not advertised: %+v", line.Capabilities)
	}

	var incomingCall, terminatedCall = parsed.Calls[0], parsed.Calls[1]
	if incomingCall.Number != "+818000000001" {
		incomingCall, terminatedCall = terminatedCall, incomingCall
	}
	if incomingCall.ID == string(incomingCallPath) ||
		!strings.HasPrefix(incomingCall.ID, "call_") ||
		strings.Contains(incomingCall.ID, string(incomingCallPath)) {
		t.Fatalf("call id is not owner-scoped and opaque: %q", incomingCall.ID)
	}
	if parsed.CallPaths[incomingCall.ID] != incomingCallPath {
		t.Fatalf("call path mapping = %q", parsed.CallPaths[incomingCall.ID])
	}
	if incomingCall.LineID != line.ID || incomingCall.Direction != "incoming" ||
		incomingCall.State != "ringing-in" || incomingCall.StateCode != 3 ||
		incomingCall.StateReason != "incoming_new" || incomingCall.StateReasonCode != 2 ||
		!incomingCall.Multiparty || incomingCall.AudioPort != "hw:2,0" ||
		incomingCall.AudioFormat == nil || incomingCall.AudioFormat.Encoding != "pcm" ||
		incomingCall.AudioFormat.Resolution != "s16le" || incomingCall.AudioFormat.Rate != 8000 ||
		!incomingCall.MediaAvailable || incomingCall.Bearer != "" {
		t.Fatalf("unexpected incoming call: %+v", incomingCall)
	}
	if terminatedCall.State != "terminated" || terminatedCall.StateCode != 7 ||
		terminatedCall.StateReason != "terminated" || terminatedCall.MediaAvailable ||
		terminatedCall.Bearer != "" {
		t.Fatalf("unexpected terminated call: %+v", terminatedCall)
	}
	wantCallIDs := []string{incomingCall.ID, terminatedCall.ID}
	if wantCallIDs[0] > wantCallIDs[1] {
		wantCallIDs[0], wantCallIDs[1] = wantCallIDs[1], wantCallIDs[0]
	}
	if !reflect.DeepEqual(line.CallIDs, wantCallIDs) {
		t.Fatalf("call IDs = %#v", line.CallIDs)
	}

	var incoming, outgoing = parsed.Messages[0], parsed.Messages[1]
	if incoming.Text != "incoming" {
		incoming, outgoing = outgoing, incoming
	}
	if incoming.ID == string(incomingMessagePath) ||
		!strings.HasPrefix(incoming.ID, "message_") ||
		strings.Contains(incoming.ID, string(incomingMessagePath)) {
		t.Fatalf("message id is not owner-scoped and opaque: %q", incoming.ID)
	}
	if incoming.LineID != line.ID || incoming.Direction != "incoming" ||
		incoming.State != "received" || incoming.Timestamp == "" {
		t.Fatalf("unexpected incoming message: %+v", incoming)
	}
	if outgoing.Direction != "outgoing" || outgoing.State != "sent" {
		t.Fatalf("unexpected outgoing message: %+v", outgoing)
	}
	wantMessageIDs := []string{incoming.ID, outgoing.ID}
	if wantMessageIDs[0] > wantMessageIDs[1] {
		wantMessageIDs[0], wantMessageIDs[1] = wantMessageIDs[1], wantMessageIDs[0]
	}
	if !reflect.DeepEqual(line.MessageIDs, wantMessageIDs) {
		t.Fatalf("message IDs = %#v", line.MessageIDs)
	}
}

func TestCallStateNameUsesCanonicalNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code int32
		want string
	}{
		{code: 2, want: "ringing-out"},
		{code: 3, want: "ringing-in"},
		{code: 4, want: "active"},
	}
	for _, test := range tests {
		if got := callStateName(test.code); got != test.want {
			t.Errorf("callStateName(%d) = %q, want %q", test.code, got, test.want)
		}
	}
}

func TestCallMediaRequiresExplicitPortAndCompleteFormat(t *testing.T) {
	ids := newInstanceIDsForTest(":1.41")
	callPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/1")
	objects := oneLineObjects(callPath, 4)
	properties := objects[callPath][callInterface]
	properties["AudioPort"] = dbus.MakeVariant("hw:2,0")
	properties["AudioFormat"] = dbus.MakeVariant(map[string]dbus.Variant{
		"encoding": dbus.MakeVariant("pcm"),
		"rate":     dbus.MakeVariant(uint32(8000)),
	})

	call := ParseManagedObjects(objects, ids).Calls[0]
	if call.MediaAvailable {
		t.Fatalf("incomplete audio format advertised media availability: %+v", call)
	}
	if call.AudioFormat == nil || call.AudioFormat.Resolution != "" {
		t.Fatalf("explicit partial audio format was not projected: %+v", call.AudioFormat)
	}
}

func TestMessageStateNamesMatchModemManagerStatesZeroThroughFive(t *testing.T) {
	tests := []struct {
		code uint32
		want string
	}{
		{code: 0, want: "unknown"},
		{code: 1, want: "stored"},
		{code: 2, want: "receiving"},
		{code: 3, want: "received"},
		{code: 4, want: "sending"},
		{code: 5, want: "sent"},
		{code: 6, want: "unknown"},
	}
	for _, test := range tests {
		if got := messageStateName(test.code); got != test.want {
			t.Fatalf("message state %d = %q, want %q", test.code, got, test.want)
		}
	}
}

func TestOpaqueObjectIDsFollowModemManagerOwnerEpoch(t *testing.T) {
	path := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/1")
	objects := oneLineObjects(path, 4)
	modemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/0")
	messagePath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/1")
	objects[modemPath][messagingInterface] = Properties{
		"Messages": dbus.MakeVariant([]dbus.ObjectPath{messagePath}),
	}
	objects[messagePath] = Interfaces{
		smsInterface: {
			"Number":  dbus.MakeVariant("+818000000001"),
			"Text":    dbus.MakeVariant("hello"),
			"PduType": dbus.MakeVariant(uint32(1)),
			"State":   dbus.MakeVariant(uint32(3)),
		},
	}
	firstIDs := newInstanceIDsForTest(":1.41")
	restartedAgentIDs := newInstanceIDsForTest(":1.41")
	restartedModemManagerIDs := newInstanceIDsForTest(":1.42")

	first := ParseManagedObjects(objects, firstIDs)
	again := ParseManagedObjects(objects, firstIDs)
	restartedAgent := ParseManagedObjects(objects, restartedAgentIDs)
	restartedModemManager := ParseManagedObjects(objects, restartedModemManagerIDs)

	if first.Calls[0].ID != again.Calls[0].ID {
		t.Fatalf("same-process call ID changed: %q != %q", first.Calls[0].ID, again.Calls[0].ID)
	}
	if first.Calls[0].ID != restartedAgent.Calls[0].ID {
		t.Fatalf("agent restart changed call ID under the same ModemManager owner: %q != %q", first.Calls[0].ID, restartedAgent.Calls[0].ID)
	}
	if first.Calls[0].ID == restartedModemManager.Calls[0].ID {
		t.Fatalf("ModemManager owner change reused call ID %q", first.Calls[0].ID)
	}
	if first.Messages[0].ID != again.Messages[0].ID {
		t.Fatalf("same-process message ID changed: %q != %q", first.Messages[0].ID, again.Messages[0].ID)
	}
	if first.Messages[0].ID != restartedAgent.Messages[0].ID {
		t.Fatalf("agent restart changed message ID under the same ModemManager owner: %q != %q", first.Messages[0].ID, restartedAgent.Messages[0].ID)
	}
	if first.Messages[0].ID == restartedModemManager.Messages[0].ID {
		t.Fatalf("ModemManager owner change reused message ID %q", first.Messages[0].ID)
	}
	if first.Lines[0].ID != restartedModemManager.Lines[0].ID {
		t.Fatalf("hardware line ID changed with provider owner: %q != %q", first.Lines[0].ID, restartedModemManager.Lines[0].ID)
	}
}

func TestLineIDDoesNotDependOnTTYPortName(t *testing.T) {
	ids := newInstanceIDsForTest(":1.41")
	objects := oneLineObjects("/org/freedesktop/ModemManager1/Call/1", 7)
	modemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/0")
	objects[modemPath][modemInterface]["PrimaryPort"] = dbus.MakeVariant("ttyUSB2")
	first := ParseManagedObjects(objects, ids).Lines[0].ID

	objects[modemPath][modemInterface]["PrimaryPort"] = dbus.MakeVariant("ttyUSB9")
	second := ParseManagedObjects(objects, ids).Lines[0].ID
	if first != second {
		t.Fatalf("line ID changed with tty port name: %q != %q", first, second)
	}
}

func TestLineIDDoesNotDependOnModemObjectPathOrTemporaryRoute(t *testing.T) {
	t.Parallel()

	ids := newInstanceIDsForTest(":1.41")
	firstObjects := oneLineObjects("/org/freedesktop/ModemManager1/Call/1", 7)
	firstPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/0")
	firstObjects[firstPath][modemInterface]["DeviceIdentifier"] = dbus.MakeVariant("device-stable")
	firstObjects[firstPath][modemInterface]["Device"] = dbus.MakeVariant("/dev/cdc-wdm0")
	firstObjects[firstPath][modemInterface]["PrimaryPort"] = dbus.MakeVariant("cdc-wdm0")
	first := ParseManagedObjects(firstObjects, ids)

	secondObjects := oneLineObjects("/org/freedesktop/ModemManager1/Call/1", 7)
	secondPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/19")
	secondObjects[secondPath] = secondObjects[firstPath]
	delete(secondObjects, firstPath)
	secondObjects[secondPath][modemInterface]["DeviceIdentifier"] = dbus.MakeVariant("device-stable")
	secondObjects[secondPath][modemInterface]["Device"] = dbus.MakeVariant("/dev/cdc-wdm19")
	secondObjects[secondPath][modemInterface]["PrimaryPort"] = dbus.MakeVariant("ttyUSB27")
	second := ParseManagedObjects(secondObjects, ids)

	if len(first.Lines) != 1 || len(second.Lines) != 1 ||
		first.Lines[0].ID == "" ||
		first.Lines[0].ID != second.Lines[0].ID {
		t.Fatalf("line identity changed with route: first=%+v second=%+v", first.Lines, second.Lines)
	}
	if first.LinePaths[first.Lines[0].ID] != firstPath ||
		second.LinePaths[second.Lines[0].ID] != secondPath {
		t.Fatalf("current D-Bus routes were not kept separate from persistent ID")
	}
}

func TestUnidentifiedLineIsExplicitlyNonPersistentAndNotRoutable(t *testing.T) {
	t.Parallel()

	modemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/12")
	callPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/12")
	objects := ManagedObjects{
		modemPath: {
			modemInterface: {
				"Device":      dbus.MakeVariant("/dev/cdc-wdm12"),
				"PrimaryPort": dbus.MakeVariant("ttyUSB12"),
				"State":       dbus.MakeVariant(int32(8)),
			},
			voiceInterface: {
				"Calls": dbus.MakeVariant([]dbus.ObjectPath{callPath}),
			},
		},
		callPath: {
			callInterface: {
				"Direction": dbus.MakeVariant(int32(1)),
				"State":     dbus.MakeVariant(int32(3)),
			},
		},
	}
	parsed := ParseManagedObjects(objects, newInstanceIDsForTest(":1.41"))
	if len(parsed.Lines) != 1 {
		t.Fatalf("lines = %+v", parsed.Lines)
	}
	line := parsed.Lines[0]
	if line.ID != "" ||
		line.IdentityPersistent ||
		line.SavedPolicySupported ||
		line.IdentitySource != "" ||
		line.UnsupportedPolicyReason != missingStableLineIdentity {
		t.Fatalf("unidentified line metadata = %+v", line)
	}
	if !line.Capabilities.VoiceInterface ||
		line.Capabilities.Dial ||
		line.Capabilities.AnswerCall ||
		line.Capabilities.RejectCall ||
		line.Capabilities.HangupCall ||
		len(parsed.LinePaths) != 0 ||
		len(parsed.CallPaths) != 0 ||
		len(parsed.Calls) != 0 {
		t.Fatalf("unidentified line exposed a persistent control route: parsed=%+v", parsed)
	}
}

func TestParseManagedObjectsSortsAndUsesEmptyArrays(t *testing.T) {
	ids := newInstanceIDsForTest(":1.41")
	objects := ManagedObjects{
		"/org/freedesktop/ModemManager1/Modem/9": {
			modemInterface: {
				"EquipmentIdentifier": dbus.MakeVariant("imei-9"),
				"State":               dbus.MakeVariant(int32(1234)),
			},
		},
		"/org/freedesktop/ModemManager1/Modem/2": {
			modemInterface: {
				"EquipmentIdentifier": dbus.MakeVariant("imei-2"),
				"State":               dbus.MakeVariant(int32(-1)),
			},
		},
		"/org/freedesktop/ModemManager1/SMS/1": {
			smsInterface: {},
		},
	}

	parsed := ParseManagedObjects(objects, ids)
	if len(parsed.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(parsed.Lines))
	}
	for _, line := range parsed.Lines {
		if line.OwnNumbers == nil || line.CallIDs == nil || line.MessageIDs == nil {
			t.Fatalf("empty collections must be encoded as arrays: %+v", line)
		}
	}
	if parsed.Calls == nil || parsed.Messages == nil {
		t.Fatal("snapshot collections must not be nil")
	}
}

func oneLineObjects(callPath dbus.ObjectPath, callState int32) ManagedObjects {
	modemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/0")
	return ManagedObjects{
		modemPath: {
			modemInterface: {
				"EquipmentIdentifier": dbus.MakeVariant("867530900000001"),
				"Physdev":             dbus.MakeVariant("/sys/devices/usb1/1-2"),
				"State":               dbus.MakeVariant(int32(8)),
			},
			voiceInterface: {
				"Calls": dbus.MakeVariant([]dbus.ObjectPath{callPath}),
			},
		},
		callPath: {
			callInterface: {
				"Number":    dbus.MakeVariant("+818000000001"),
				"Direction": dbus.MakeVariant(int32(1)),
				"State":     dbus.MakeVariant(callState),
			},
		},
	}
}

func TestParseManagedObjectsDoesNotTruncateSixLines(t *testing.T) {
	t.Parallel()
	const lineCount = 6
	objects := ManagedObjects{}
	for index := 0; index < lineCount; index++ {
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
				"EquipmentIdentifier": dbus.MakeVariant(fmt.Sprintf("99%013d", index)),
				"DeviceIdentifier":    dbus.MakeVariant(fmt.Sprintf("device-%d", index)),
				"Physdev":             dbus.MakeVariant(fmt.Sprintf("/sys/devices/usb/%d", index)),
				"PrimaryPort":         dbus.MakeVariant(fmt.Sprintf("cdc-wdm%d", index)),
				"State":               dbus.MakeVariant(int32(8)),
				"Sim":                 dbus.MakeVariant(simPath),
			},
		}
		objects[simPath] = Interfaces{
			simInterface: {
				"SimIdentifier": dbus.MakeVariant(fmt.Sprintf("89%017d", index)),
			},
		}
	}
	parsed := ParseManagedObjects(
		objects,
		newInstanceIDsForTest(":1.46"),
	)
	if len(parsed.Lines) != lineCount {
		t.Fatalf("parsed lines = %d, want %d", len(parsed.Lines), lineCount)
	}
	for index, line := range parsed.Lines {
		if line.ID == "" {
			t.Fatalf("line %d has an empty id: %+v", index, line)
		}
	}
}
