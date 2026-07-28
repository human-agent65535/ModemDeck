package modemmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestQuectelVoiceModelingIsReadOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		revision     string
		usbConfig    string
		usbConfigErr error
		wantControl  bool
		wantUSB      string
		wantRouting  string
	}{
		{
			name:        "documented EG25 call control is modeled",
			revision:    "EG25GGCR07A02M1G",
			usbConfig:   `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`,
			wantControl: true,
			wantUSB:     voiceVerificationEnabled,
			wantRouting: voiceVerificationCallRequired,
		},
		{
			name:         "unsupported USB config query keeps ModemManager call control",
			revision:     "EG25GGCR07A02M1G",
			usbConfigErr: errors.New("AT command returned ERROR"),
			wantControl:  true,
			wantUSB:      voiceVerificationReadFailed,
			wantRouting:  voiceVerificationCallRequired,
		},
		{
			name:        "unrecognized USB config response is inconclusive",
			revision:    "EG25GGCR07A02M1G",
			usbConfig:   "OK",
			wantControl: true,
			wantUSB:     voiceVerificationInvalidResponse,
			wantRouting: voiceVerificationCallRequired,
		},
		{
			name:        "USB call control disabled",
			revision:    "EG25GGCR07A02M1G",
			usbConfig:   `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0`,
			wantUSB:     voiceVerificationDisabled,
			wantRouting: voiceVerificationDisabled,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := emptyLineObjects(true, false)
			properties := objects[testModemPath][modemInterface]
			properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
			properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
			properties["Revision"] = dbus.MakeVariant(test.revision)

			caller := newFakeCaller(objects)
			caller.owner = true
			caller.atResponses[quectelUSBVoiceQuery] = test.usbConfig
			if test.usbConfigErr != nil {
				caller.atCommandErrors[quectelUSBVoiceQuery] = test.usbConfigErr
			}
			provider := newTestProvider(caller)

			snapshot, err := provider.Snapshot(context.Background())
			if err != nil {
				t.Fatalf("Snapshot() error = %v", err)
			}
			if len(snapshot.Lines) != 1 {
				t.Fatalf("Snapshot() lines = %+v", snapshot.Lines)
			}
			line := snapshot.Lines[0]
			if !line.Capabilities.VoiceInterface {
				t.Fatalf("raw ModemManager Voice capability was lost: %+v", line.Capabilities)
			}
			if line.Capabilities.Dial != test.wantControl ||
				line.Capabilities.AnswerCall != test.wantControl ||
				line.Capabilities.RejectCall != test.wantControl ||
				line.Capabilities.HangupCall != test.wantControl ||
				line.Capabilities.SendDTMF != test.wantControl {
				t.Fatalf(
					"call capabilities = %+v, want available %v",
					line.Capabilities,
					test.wantControl,
				)
			}
			if line.Capabilities.Media {
				t.Fatalf("modeling exposed unverified media capability: %+v", line.Capabilities)
			}
			if line.VoiceVerification == nil ||
				line.VoiceVerification.USBConfiguration != test.wantUSB ||
				line.VoiceVerification.MediaRouting != test.wantRouting {
				t.Fatalf(
					"voice verification = %+v, want USB %q and media %q",
					line.VoiceVerification,
					test.wantUSB,
					test.wantRouting,
				)
			}
			assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)
			assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 0)
		})
	}
}

func TestQuectelPCMActivationRunsOnceWhenCallBecomesActive(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	callPath := testCallPath(71)
	addCall(objects, callPath, 2)

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atResponses[quectelPCMEnable] = ""
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	provider := newTestProvider(caller)

	dialing, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("dialing Snapshot() error = %v", err)
	}
	if dialing.Lines[0].Capabilities.Media ||
		dialing.Lines[0].VoiceVerification == nil ||
		dialing.Lines[0].VoiceVerification.MediaRouting != voiceVerificationCallRequired {
		t.Fatalf("dialing line = %+v", dialing.Lines[0])
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)

	objects[callPath][callInterface]["State"] = dbus.MakeVariant(int32(4))
	active, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("active Snapshot() error = %v", err)
	}
	if active.Lines[0].Capabilities.Media ||
		active.Lines[0].VoiceVerification == nil ||
		active.Lines[0].VoiceVerification.MediaRouting != voiceVerificationCallRequired {
		t.Fatalf("active line = %+v", active.Lines[0])
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)
	assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 0)

	activation, err := provider.ActivateCallMedia(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "activate-media-71",
			CallID:    active.Calls[0].ID,
		},
	)
	if err != nil {
		t.Fatalf("ActivateCallMedia() error = %v", err)
	}
	if activation.MediaRouting != voiceVerificationEnabled ||
		!activation.MediaAvailable ||
		activation.AudioPort != quectelUACPortPrefix+"/sys/devices/usb1/1-2" ||
		activation.AudioFormat == nil ||
		activation.AudioFormat.Encoding != "pcm" ||
		activation.AudioFormat.Resolution != "s16le" ||
		activation.AudioFormat.Rate != 8000 {
		t.Fatalf("activation = %+v", activation)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
	assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 1)

	repeated, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("repeated active Snapshot() error = %v", err)
	}
	if !repeated.Lines[0].Capabilities.Media {
		t.Fatalf("repeated active line = %+v", repeated.Lines[0])
	}
	if len(repeated.Calls) != 1 || !repeated.Calls[0].MediaAvailable {
		t.Fatalf("repeated active calls = %+v", repeated.Calls)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
	assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 1)
	repeatedActivation, err := provider.ActivateCallMedia(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "activate-media-71-repeat",
			CallID:    active.Calls[0].ID,
		},
	)
	if err != nil {
		t.Fatalf("repeated ActivateCallMedia() error = %v", err)
	}
	if !repeatedActivation.MediaAvailable {
		t.Fatalf("repeated activation = %+v", repeatedActivation)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
	assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 1)

	objects[callPath][callInterface]["State"] = dbus.MakeVariant(callStateTerminated)
	terminated, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("terminated Snapshot() error = %v", err)
	}
	if terminated.Lines[0].Capabilities.Media ||
		terminated.Lines[0].VoiceVerification == nil ||
		terminated.Lines[0].VoiceVerification.MediaRouting != voiceVerificationCallRequired {
		t.Fatalf("terminated line retained call-scoped media state: %+v", terminated.Lines[0])
	}

	delete(objects, callPath)
	nextCallPath := testCallPath(72)
	addCall(objects, nextCallPath, 4)
	next, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("next Snapshot() error = %v", err)
	}
	if next.Lines[0].Capabilities.Media ||
		next.Lines[0].VoiceVerification == nil ||
		next.Lines[0].VoiceVerification.MediaRouting != voiceVerificationCallRequired {
		t.Fatalf("next active line = %+v", next.Lines[0])
	}
	if _, err := provider.ActivateCallMedia(
		context.Background(),
		domain.CallCommandRequest{
			RequestID: "activate-media-72",
			CallID:    next.Calls[0].ID,
		},
	); err != nil {
		t.Fatalf("next ActivateCallMedia() error = %v", err)
	}
	assertATInvocationCount(t, caller.invocations(), quectelUSBVoiceQuery, 1)
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 2)
	assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 2)
}

func TestQuectelPCMActivationKeepsAuthoritativeModemManagerAudio(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	callPath := testCallPath(73)
	addCall(objects, callPath, 4)
	objects[callPath][callInterface]["AudioPort"] = dbus.MakeVariant("hw:mm,0")
	objects[callPath][callInterface]["AudioFormat"] = dbus.MakeVariant(map[string]dbus.Variant{
		"encoding":   dbus.MakeVariant("pcm"),
		"resolution": dbus.MakeVariant("s16le"),
		"rate":       dbus.MakeVariant(uint32(16000)),
	})

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atResponses[quectelPCMEnable] = ""
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Calls) != 1 ||
		snapshot.Calls[0].AudioPort != "hw:mm,0" ||
		snapshot.Calls[0].AudioFormat == nil ||
		snapshot.Calls[0].AudioFormat.Rate != 16000 ||
		!snapshot.Calls[0].MediaAvailable {
		t.Fatalf("call media = %+v", snapshot.Calls)
	}
}

func TestQuectelPCMActivationDoesNotReplaceIncompleteModemManagerAudio(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	callPath := testCallPath(74)
	addCall(objects, callPath, 4)
	objects[callPath][callInterface]["AudioPort"] = dbus.MakeVariant("reported-but-incomplete")

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atResponses[quectelPCMEnable] = ""
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Calls) != 1 ||
		snapshot.Calls[0].AudioPort != "reported-but-incomplete" ||
		snapshot.Calls[0].AudioFormat != nil ||
		snapshot.Calls[0].MediaAvailable {
		t.Fatalf("incomplete media was replaced or advertised: %+v", snapshot.Calls)
	}
}

func TestQuectelPCMActivationFailureIsScopedToActiveCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		enableErr   error
		statusErr   error
		status      string
		wantRouting string
	}{
		{
			name:        "firmware rejects routing command",
			enableErr:   errors.New("AT command returned ERROR"),
			wantRouting: voiceVerificationRejected,
		},
		{
			name:        "routing state cannot be read",
			statusErr:   errors.New("AT port temporarily busy"),
			wantRouting: voiceVerificationReadFailed,
		},
		{
			name:        "routing remains inactive",
			status:      "+QPCMV: 0,0",
			wantRouting: voiceVerificationInactive,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := emptyLineObjects(true, false)
			properties := objects[testModemPath][modemInterface]
			properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
			properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
			properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
			addCall(objects, testCallPath(72), 4)

			caller := newFakeCaller(objects)
			caller.owner = true
			caller.atResponses[quectelUSBVoiceQuery] =
				`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
			caller.atResponses[quectelPCMEnable] = ""
			caller.atResponses[quectelPCMStatusQuery] = test.status
			if test.enableErr != nil {
				caller.atCommandErrors[quectelPCMEnable] = test.enableErr
			}
			if test.statusErr != nil {
				caller.atCommandErrors[quectelPCMStatusQuery] = test.statusErr
			}
			provider := newTestProvider(caller)

			snapshot, err := provider.Snapshot(context.Background())
			if err != nil {
				t.Fatalf("Snapshot() error = %v", err)
			}
			if snapshot.Lines[0].VoiceVerification == nil ||
				snapshot.Lines[0].VoiceVerification.MediaRouting != voiceVerificationCallRequired {
				t.Fatalf("pre-activation line = %+v", snapshot.Lines[0])
			}
			activation, err := provider.ActivateCallMedia(
				context.Background(),
				domain.CallCommandRequest{
					RequestID: "activate-media-failure",
					CallID:    snapshot.Calls[0].ID,
				},
			)
			if err != nil {
				t.Fatalf("ActivateCallMedia() error = %v", err)
			}
			if activation.MediaRouting != test.wantRouting ||
				activation.MediaAvailable ||
				activation.AudioPort != "" ||
				activation.AudioFormat != nil {
				t.Fatalf("activation = %+v, want routing %q", activation, test.wantRouting)
			}
			projected, err := provider.Snapshot(context.Background())
			if err != nil {
				t.Fatalf("projected Snapshot() error = %v", err)
			}
			line := projected.Lines[0]
			if !line.Capabilities.Dial || line.Capabilities.Media ||
				line.VoiceVerification == nil ||
				line.VoiceVerification.MediaRouting != test.wantRouting {
				t.Fatalf("line = %+v, want routing %q", line, test.wantRouting)
			}
			if len(projected.Calls) != 1 ||
				projected.Calls[0].MediaAvailable ||
				projected.Calls[0].AudioPort != "" ||
				projected.Calls[0].AudioFormat != nil {
				t.Fatalf("failed route exposed call media: %+v", projected.Calls)
			}
			assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
			if test.enableErr == nil {
				assertATInvocationCount(t, caller.invocations(), quectelPCMStatusQuery, 1)
			}
		})
	}
}

func TestParseQuectelUSBCallControlRejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"OK",
		`+QCFG: "usbcfg"`,
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,2`,
		"+QCFG: \"usbcfg\",1\n+QCFG: \"usbcfg\",1",
	}
	for _, response := range tests {
		if _, err := parseQuectelUSBCallControl(response); err == nil {
			t.Fatalf("parseQuectelUSBCallControl(%q) accepted malformed response", response)
		}
	}
}

func TestQuectelVoiceModelIsCachedUntilManualReprobe(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	provider := newTestProvider(caller)

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	if !first.Lines[0].Capabilities.Dial || first.Lines[0].Capabilities.Media {
		t.Fatalf("first capabilities = %+v", first.Lines[0].Capabilities)
	}
	assertATInvocationCount(t, caller.invocations(), quectelUSBVoiceQuery, 1)
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)

	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0`

	cached, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("cached Snapshot() error = %v", err)
	}
	if !cached.Lines[0].Capabilities.Dial {
		t.Fatalf("cached capabilities = %+v", cached.Lines[0].Capabilities)
	}
	assertATInvocationCount(t, caller.invocations(), quectelUSBVoiceQuery, 1)

	if err := provider.ReprobeVoiceCapabilities(
		context.Background(),
		first.Lines[0].ID,
	); err != nil {
		t.Fatalf("ReprobeVoiceCapabilities() error = %v", err)
	}
	reprobed, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("reprobed Snapshot() error = %v", err)
	}
	if reprobed.Lines[0].Capabilities.Dial ||
		reprobed.Lines[0].VoiceVerification == nil ||
		reprobed.Lines[0].VoiceVerification.USBConfiguration != voiceVerificationDisabled {
		t.Fatalf("reprobed line = %+v", reprobed.Lines[0])
	}
	assertATInvocationCount(t, caller.invocations(), quectelUSBVoiceQuery, 2)
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)
}

func TestQuectelVoiceModelRebuildsWhenFirmwareChanges(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	provider := newTestProvider(caller)

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	if !first.Lines[0].Capabilities.Dial {
		t.Fatalf("first line = %+v", first.Lines[0])
	}

	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A03M1G")
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0`
	updated, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("updated Snapshot() error = %v", err)
	}
	if updated.Lines[0].Capabilities.Dial {
		t.Fatalf("updated line = %+v", updated.Lines[0])
	}
	assertATInvocationCount(t, caller.invocations(), quectelUSBVoiceQuery, 2)
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)
}

func assertATInvocationCount(
	t *testing.T,
	invocations []dbusInvocation,
	command string,
	want int,
) {
	t.Helper()
	count := 0
	for _, invocation := range invocations {
		if invocation.Method != modemInterface+".Command" || len(invocation.Args) == 0 {
			continue
		}
		if observed, ok := invocation.Args[0].(string); ok && observed == command {
			count++
		}
	}
	if count != want {
		t.Fatalf("AT command %q count = %d, want %d", command, count, want)
	}
}

func TestStartCallRejectsUnverifiedQuectelVoice(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0`
	provider := newTestProvider(caller)
	lineID := ParseManagedObjects(objects, provider.ids).Lines[0].ID

	_, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "request-qdc-voice",
		LineID:    lineID,
		Number:    "+818012345678",
	})
	assertOperationError(t, err, domain.ErrorNotSupported, "start_call")
	for _, invocation := range caller.invocations() {
		if invocation.Method == voiceInterface+".CreateCall" {
			t.Fatal("unverified QDC line reached ModemManager CreateCall")
		}
	}
}

func TestIncomingCallCommandsRejectUnverifiedQuectelVoice(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	callPath := testCallPath(42)
	addCall(objects, callPath, 3)

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0`
	provider := newTestProvider(caller)
	callID := ParseManagedObjects(objects, provider.ids).Calls[0].ID

	_, err := provider.AnswerCall(context.Background(), domain.CallCommandRequest{
		RequestID: "request-qdc-answer",
		CallID:    callID,
	})
	assertOperationError(t, err, domain.ErrorNotFound, "answer_call")
	for _, invocation := range caller.invocations() {
		if invocation.Method == callInterface+".Accept" {
			t.Fatal("unverified QDC call reached ModemManager Accept")
		}
	}
}
