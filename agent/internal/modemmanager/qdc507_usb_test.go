package modemmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type recordingQDC507USBProvisioner struct {
	lines   []domain.Line
	changed bool
	err     error
}

func (p *recordingQDC507USBProvisioner) Ensure(
	_ context.Context,
	line domain.Line,
) (bool, error) {
	p.lines = append(p.lines, line)
	return p.changed, p.err
}

func TestEnsureQDC507VoiceUSBPreflightsAndDelegatesExactLine(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Model"] = dbus.MakeVariant("QDC507")
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	properties["Physdev"] = dbus.MakeVariant("/sys/devices/pci0000:00/usb1/1-10")
	properties["Device"] = dbus.MakeVariant("/sys/devices/pci0000:00/usb1/1-10")
	properties["Ports"] = dbus.MakeVariant([][]any{
		{"ttyUSB6", uint32(3)},
		{"cdc-wdm1", uint32(6)},
	})
	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelCallListQuery] = ""
	provisioner := &recordingQDC507USBProvisioner{changed: true}
	provider, err := newProviderWithOptions(
		caller,
		newInstanceIDsForTest("boot-qdc507-usb"),
		Options{QDC507USBProvisioner: provisioner},
	)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := provider.QDC507VoiceLines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := provider.EnsureQDC507VoiceUSB(context.Background(), lines)
	if err != nil || !changed {
		t.Fatalf("EnsureQDC507VoiceUSB() changed = %t, error = %v", changed, err)
	}
	if len(provisioner.lines) != 1 || provisioner.lines[0].PhysicalDevice != "/sys/devices/pci0000:00/usb1/1-10" {
		t.Fatalf("provisioned lines = %+v", provisioner.lines)
	}
	assertATInvocationCount(t, caller.invocations(), quectelCallListQuery, 1)
}

func TestEnsureQDC507VoiceUSBRejectsConnectedBearer(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, false)
	properties := objects[testModemPath][modemInterface]
	properties["Model"] = dbus.MakeVariant("QDC507")
	properties["Revision"] = dbus.MakeVariant("QDC507GLEFM21")
	bearerPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Bearer/507")
	properties["Bearers"] = dbus.MakeVariant([]dbus.ObjectPath{bearerPath})
	objects[bearerPath] = Interfaces{bearerInterface: Properties{
		"Connected": dbus.MakeVariant(true),
		"Interface": dbus.MakeVariant("wwan0"),
	}}
	caller := newFakeCaller(objects)
	caller.owner = true
	caller.atResponses[quectelCallListQuery] = ""
	provisioner := &recordingQDC507USBProvisioner{}
	provider, err := newProviderWithOptions(
		caller,
		newInstanceIDsForTest("boot-qdc507-bearer"),
		Options{QDC507USBProvisioner: provisioner},
	)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := provider.QDC507VoiceLines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := provider.EnsureQDC507VoiceUSB(context.Background(), lines)
	if err == nil || changed || !strings.Contains(err.Error(), "connected data bearer") {
		t.Fatalf("EnsureQDC507VoiceUSB() changed = %t, error = %v", changed, err)
	}
	if len(provisioner.lines) != 0 {
		t.Fatalf("busy line reached provisioner: %+v", provisioner.lines)
	}
}
