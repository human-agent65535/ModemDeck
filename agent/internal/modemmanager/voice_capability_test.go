package modemmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestQuectelVoiceSeparatesCallControlFromMediaProof(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		revision    string
		usbConfig   string
		enableErr   error
		status      string
		wantControl bool
		wantMedia   bool
	}{
		{
			name:        "documented EG25 path is verified",
			revision:    "EG25GGCR07A02M1G",
			usbConfig:   `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`,
			status:      "+QPCMV: 1,2",
			wantControl: true,
			wantMedia:   true,
		},
		{
			name:      "USB call control disabled",
			revision:  "EG25GGCR07A02M1G",
			usbConfig: `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0`,
		},
		{
			name:        "QDC firmware rejects PCM routing",
			revision:    "QDC507GLEFM21",
			usbConfig:   `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`,
			enableErr:   errors.New("AT command returned ERROR"),
			wantControl: true,
		},
		{
			name:        "PCM readback does not confirm routing",
			revision:    "EG25GGCR07A02M1G",
			usbConfig:   `+QCFG: "usbcfg",0x2C7C,0x125,1,1,1,1,1,0,1`,
			status:      "+QPCMV: 0,0",
			wantControl: true,
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
			caller.atResponses[quectelPCMEnable] = ""
			caller.atResponses[quectelPCMStatusQuery] = test.status
			if test.enableErr != nil {
				caller.atCommandErrors[quectelPCMEnable] = test.enableErr
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
				t.Fatalf(
					"media capability = %v, want %v",
					line.Capabilities.Media,
					test.wantMedia,
				)
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
