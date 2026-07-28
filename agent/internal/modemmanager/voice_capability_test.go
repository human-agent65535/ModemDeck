package modemmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestQuectelVoiceModelingProbesCallAudioOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		revision       string
		usbConfig      string
		usbConfigErr   error
		pcmEnableErr   error
		pcmStatus      string
		wantControl    bool
		wantMedia      bool
		wantUSB        string
		wantRouting    string
		wantMediaProbe bool
	}{
		{
			name:           "documented EG25 call audio is verified",
			revision:       "EG25GGCR07A02M1G",
			usbConfig:      `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`,
			pcmStatus:      quectelPCMReadyStatus,
			wantControl:    true,
			wantMedia:      true,
			wantUSB:        voiceVerificationEnabled,
			wantRouting:    voiceVerificationSupported,
			wantMediaProbe: true,
		},
		{
			name:           "unsupported USB config query keeps ModemManager control and probes audio",
			revision:       "EG25GGCR07A02M1G",
			usbConfigErr:   errors.New("AT command returned ERROR"),
			pcmStatus:      quectelPCMReadyStatus,
			wantControl:    true,
			wantMedia:      true,
			wantUSB:        voiceVerificationReadFailed,
			wantRouting:    voiceVerificationSupported,
			wantMediaProbe: true,
		},
		{
			name:           "firmware can expose call control without call audio",
			revision:       "QDC507GLEFM21",
			usbConfig:      `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`,
			pcmEnableErr:   errors.New("AT command returned ERROR"),
			wantControl:    true,
			wantUSB:        voiceVerificationEnabled,
			wantRouting:    voiceVerificationRejected,
			wantMediaProbe: true,
		},
		{
			name:        "USB call control disabled skips audio probe",
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
			caller.atResponses[quectelPCMStatusQuery] = test.pcmStatus
			if test.usbConfigErr != nil {
				caller.atCommandErrors[quectelUSBVoiceQuery] = test.usbConfigErr
			}
			if test.pcmEnableErr != nil {
				caller.atCommandErrors[quectelPCMEnable] = test.pcmEnableErr
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
			if line.Capabilities.Media != test.wantMedia {
				t.Fatalf("media capability = %v, want %v", line.Capabilities.Media, test.wantMedia)
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
			wantProbeCount := 0
			if test.wantMediaProbe {
				wantProbeCount = 1
			}
			assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, wantProbeCount)
			if test.wantMediaProbe && test.pcmEnableErr == nil {
				assertATInvocationCount(t, caller.invocations(), quectelPCMDisable, 1)
			}
		})
	}
}

func TestQuectelPCMIsPreparedBeforeOutgoingCallStarts(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Manufacturer"] = dbus.MakeVariant("QUALCOMM INCORPORATED")
	properties["Model"] = dbus.MakeVariant("QUECTEL Mobile Broadband Module")
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	callPath := testCallPath(71)

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.createdCallPath = callPath
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atResponses[quectelPCMEnable] = ""
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	provider := newTestProvider(caller)
	lineID := ParseManagedObjects(objects, provider.ids).Lines[0].ID

	receipt, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "start-media-71",
		LineID:    lineID,
		Number:    "+818012345678",
	})
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if receipt.ResourceID == "" {
		t.Fatalf("StartCall() receipt = %+v", receipt)
	}
	invocations := caller.invocations()
	lastEnable := -1
	startCall := -1
	for index, invocation := range invocations {
		if invocation.Method == modemInterface+".Command" &&
			len(invocation.Args) > 0 &&
			invocation.Args[0] == quectelPCMEnable {
			lastEnable = index
		}
		if invocation.Method == callInterface+".Start" {
			startCall = index
		}
	}
	if lastEnable < 0 || startCall < 0 || lastEnable > startCall {
		t.Fatalf("PCM enable index = %d, call start index = %d", lastEnable, startCall)
	}
	assertATInvocationCount(t, invocations, quectelPCMEnable, 2)
	assertATInvocationCount(t, invocations, quectelPCMStatusQuery, 2)
	assertATInvocationCount(t, invocations, quectelPCMDisable, 1)

	addCall(objects, callPath, 4)
	active, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("active Snapshot() error = %v", err)
	}
	if len(active.Calls) != 1 ||
		!active.Calls[0].MediaAvailable ||
		active.Calls[0].AudioPort != quectelUACPortPrefix+"/sys/devices/usb1/1-2" ||
		active.Calls[0].AudioFormat == nil ||
		active.Calls[0].AudioFormat.Rate != 8000 {
		t.Fatalf("active call = %+v", active.Calls)
	}
	activation, err := provider.ActivateCallMedia(
		context.Background(),
		domain.CallCommandRequest{RequestID: "inspect-media-71", CallID: receipt.ResourceID},
	)
	if err != nil {
		t.Fatalf("ActivateCallMedia() error = %v", err)
	}
	if !activation.MediaAvailable || activation.MediaRouting != voiceVerificationEnabled {
		t.Fatalf("activation = %+v", activation)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 2)
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

func TestQuectelPCMCapabilityProbeFailureKeepsCallControl(t *testing.T) {
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
				snapshot.Lines[0].VoiceVerification.MediaRouting != test.wantRouting ||
				!snapshot.Lines[0].Capabilities.Dial ||
				snapshot.Lines[0].Capabilities.Media {
				t.Fatalf("line = %+v, want routing %q", snapshot.Lines[0], test.wantRouting)
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
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	provider := newTestProvider(caller)

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	if !first.Lines[0].Capabilities.Dial || !first.Lines[0].Capabilities.Media {
		t.Fatalf("first capabilities = %+v", first.Lines[0].Capabilities)
	}
	assertATInvocationCount(t, caller.invocations(), quectelUSBVoiceQuery, 1)
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)

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
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
}

func TestBusyQuectelVoiceModelWaitsForManualReprobe(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Revision"] = dbus.MakeVariant("EG25GGCR07A02M1G")
	callPath := testCallPath(76)
	addCall(objects, callPath, 4)

	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelUSBVoiceQuery] =
		`+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`
	caller.atResponses[quectelCallListQuery] =
		`+CLCC: 1,0,0,0,0,"+818012345678",145`
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
	provider := newTestProvider(caller)

	busy, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("busy Snapshot() error = %v", err)
	}
	if busy.Lines[0].VoiceVerification == nil ||
		busy.Lines[0].VoiceVerification.MediaRouting != voiceVerificationProbePending {
		t.Fatalf("busy voice verification = %+v", busy.Lines[0].VoiceVerification)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)

	err = provider.ReprobeVoiceCapabilities(
		context.Background(),
		busy.Lines[0].ID,
	)
	assertOperationError(
		t,
		err,
		domain.ErrorConflict,
		"reprobe_voice_capabilities",
	)
	stillBusy, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() after rejected reprobe error = %v", err)
	}
	if stillBusy.Lines[0].VoiceVerification == nil ||
		stillBusy.Lines[0].VoiceVerification.MediaRouting != voiceVerificationProbePending {
		t.Fatalf(
			"rejected reprobe changed cached voice verification = %+v",
			stillBusy.Lines[0].VoiceVerification,
		)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)

	delete(objects, callPath)
	objects[testModemPath][voiceInterface]["Calls"] =
		dbus.MakeVariant([]dbus.ObjectPath{})
	caller.atResponses[quectelCallListQuery] = ""

	idle, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("idle Snapshot() error = %v", err)
	}
	if idle.Lines[0].VoiceVerification == nil ||
		idle.Lines[0].VoiceVerification.MediaRouting != voiceVerificationProbePending {
		t.Fatalf("cached voice verification = %+v", idle.Lines[0].VoiceVerification)
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 0)

	if err := provider.ReprobeVoiceCapabilities(
		context.Background(),
		idle.Lines[0].ID,
	); err != nil {
		t.Fatalf("ReprobeVoiceCapabilities() error = %v", err)
	}
	reprobed, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("reprobed Snapshot() error = %v", err)
	}
	if !reprobed.Lines[0].Capabilities.Media ||
		reprobed.Lines[0].VoiceVerification == nil ||
		reprobed.Lines[0].VoiceVerification.MediaRouting != voiceVerificationSupported {
		t.Fatalf("reprobed line = %+v", reprobed.Lines[0])
	}
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
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
	caller.atResponses[quectelPCMStatusQuery] = quectelPCMReadyStatus
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
	assertATInvocationCount(t, caller.invocations(), quectelPCMEnable, 1)
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
