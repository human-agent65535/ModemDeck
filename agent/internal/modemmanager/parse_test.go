package modemmanager

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestParseManagedObjectsMapsModemSIMVoiceAndMessaging(t *testing.T) {
	modemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/7")
	simPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SIM/3")
	callPaths := []dbus.ObjectPath{
		"/org/freedesktop/ModemManager1/Call/4",
	}
	messagePaths := []dbus.ObjectPath{
		"/org/freedesktop/ModemManager1/SMS/8",
		"/org/freedesktop/ModemManager1/SMS/9",
	}

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
				"Calls":         dbus.MakeVariant(callPaths),
				"EmergencyOnly": dbus.MakeVariant(false),
			},
			messagingInterface: {
				"Messages":          dbus.MakeVariant(messagePaths),
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
	}

	lines := ParseManagedObjects(objects)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	line := lines[0]
	if line.ID != string(modemPath) || line.State != "registered" || line.StateCode != 8 {
		t.Fatalf("unexpected line identity/state: %+v", line)
	}
	if line.Manufacturer != "Quectel" || line.Model != "EG25-G" || line.PrimaryPort != "cdc-wdm0" {
		t.Fatalf("unexpected modem properties: %+v", line)
	}
	if !line.SignalQualityKnown || line.SignalQuality != 76 || !line.SignalQualityRecent {
		t.Fatalf("unexpected signal quality: %+v", line)
	}
	if !line.SIMPresent || line.SIMPath != string(simPath) || line.SIMIdentifier != "8986012345678901234" {
		t.Fatalf("unexpected SIM mapping: %+v", line)
	}
	if line.OperatorIdentifier != "44051" || line.OperatorName != "KDDI" {
		t.Fatalf("unexpected operator mapping: %+v", line)
	}
	if !reflect.DeepEqual(line.CallIDs, []string{string(callPaths[0])}) {
		t.Fatalf("call ids = %#v", line.CallIDs)
	}
	if !reflect.DeepEqual(line.MessageIDs, []string{string(messagePaths[0]), string(messagePaths[1])}) {
		t.Fatalf("message ids = %#v", line.MessageIDs)
	}
	if !line.Capabilities.ModemInterface || !line.Capabilities.SIMInterface ||
		!line.Capabilities.VoiceInterface || !line.Capabilities.MessagingInterface {
		t.Fatalf("read interfaces were not advertised: %+v", line.Capabilities)
	}
	if line.Capabilities.Dial || line.Capabilities.AnswerCall ||
		line.Capabilities.HangupCall || line.Capabilities.SendMessage {
		t.Fatalf("unimplemented mutations were advertised: %+v", line.Capabilities)
	}
}

func TestParseManagedObjectsSortsLinesAndIgnoresOtherObjects(t *testing.T) {
	objects := ManagedObjects{
		"/org/freedesktop/ModemManager1/Modem/9": {
			modemInterface: {"State": dbus.MakeVariant(int32(1234))},
		},
		"/org/freedesktop/ModemManager1/Modem/2": {
			modemInterface: {"State": dbus.MakeVariant(int32(-1))},
		},
		"/org/freedesktop/ModemManager1/SMS/1": {
			"org.freedesktop.ModemManager1.Sms": {},
		},
	}

	lines := ParseManagedObjects(objects)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].ID != "/org/freedesktop/ModemManager1/Modem/2" || lines[0].State != "failed" {
		t.Fatalf("unexpected first line: %+v", lines[0])
	}
	if lines[1].ID != "/org/freedesktop/ModemManager1/Modem/9" || lines[1].State != "unknown" {
		t.Fatalf("unexpected second line: %+v", lines[1])
	}
	if lines[1].OwnNumbers == nil || lines[1].CallIDs == nil || lines[1].MessageIDs == nil {
		t.Fatalf("empty collections must be encoded as arrays: %+v", lines[1])
	}
}
