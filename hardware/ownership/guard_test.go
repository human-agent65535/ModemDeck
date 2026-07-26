package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestUnixSocketPathAcceptsOnlyOneAbsolutePath(t *testing.T) {
	got, err := unixSocketPath("unix:path=/run/host-dbus/system_bus_socket")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/run/host-dbus/system_bus_socket" {
		t.Fatalf("path = %q", got)
	}
	for _, invalid := range []string{
		"unix:path=relative",
		"unix:path=/run/bus,guid=abc",
		"tcp:host=127.0.0.1",
	} {
		if _, err := unixSocketPath(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestVariantPortNamesDecodesModemManagerPortTuples(t *testing.T) {
	value := dbus.MakeVariant([][]any{
		{"ttyUSB0", uint32(2)},
		{"wwan0", uint32(5)},
	})
	got := variantPortNames(value)
	want := []string{"ttyUSB0", "wwan0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %v, want %v", got, want)
	}
}

func TestForeignOpenerFailsAssignedDeviceClosed(t *testing.T) {
	procRoot := t.TempDir()
	processRoot := filepath.Join(procRoot, "4242")
	if err := os.MkdirAll(filepath.Join(processRoot, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"cgroup": "0::/external.slice\n",
		"comm":   "external-owner\n",
	} {
		if err := os.WriteFile(filepath.Join(processRoot, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/dev/ttyUSB7", filepath.Join(processRoot, "fd", "9")); err != nil {
		t.Fatal(err)
	}

	guard := &hostGuard{
		procRoot:  procRoot,
		ownCgroup: "0::/modemdeck.slice\n",
	}
	err := guard.checkForeignOpeners(map[string]physicalDevice{
		"/dev/ttyUSB7": {SysfsPath: "/sys/devices/assigned"},
	})
	if err == nil ||
		!strings.Contains(err.Error(), "assigned physical device /sys/devices/assigned is externally occupied") {
		t.Fatalf("error = %v", err)
	}
}

func TestForeignOpenerIgnoresHardwareContainerCgroup(t *testing.T) {
	procRoot := t.TempDir()
	processRoot := filepath.Join(procRoot, "4243")
	if err := os.MkdirAll(filepath.Join(processRoot, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	const ownCgroup = "0::/modemdeck.slice\n"
	for name, value := range map[string]string{
		"cgroup": ownCgroup,
		"comm":   "ModemManager\n",
	} {
		if err := os.WriteFile(filepath.Join(processRoot, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/dev/ttyUSB7", filepath.Join(processRoot, "fd", "9")); err != nil {
		t.Fatal(err)
	}

	guard := &hostGuard{procRoot: procRoot, ownCgroup: ownCgroup}
	if err := guard.checkForeignOpeners(map[string]physicalDevice{
		"/dev/ttyUSB7": {SysfsPath: "/sys/devices/assigned"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestForeignOpenerIgnoresUnassignedDevice(t *testing.T) {
	procRoot := t.TempDir()
	processRoot := filepath.Join(procRoot, "4244")
	if err := os.MkdirAll(filepath.Join(processRoot, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"cgroup": "0::/external.slice\n",
		"comm":   "external-owner\n",
	} {
		if err := os.WriteFile(filepath.Join(processRoot, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/dev/ttyUSB8", filepath.Join(processRoot, "fd", "9")); err != nil {
		t.Fatal(err)
	}

	guard := &hostGuard{
		procRoot:  procRoot,
		ownCgroup: "0::/modemdeck.slice\n",
	}
	if err := guard.checkForeignOpeners(map[string]physicalDevice{
		"/dev/ttyUSB7": {SysfsPath: "/sys/devices/assigned"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExternalManagedObjectFailsAssignedDeviceClosed(t *testing.T) {
	device := physicalDevice{
		SysfsPath: "/sys/devices/assigned",
		PortPath:  "/devices/assigned",
		Ports: []kernelPort{{
			Subsystem: "tty",
			Name:      "ttyUSB7",
		}},
	}
	selected := map[string]physicalDevice{
		device.SysfsPath: device,
		device.PortPath:  device,
	}

	for name, properties := range map[string]map[string]dbus.Variant{
		"physical path": {
			"Physdev": dbus.MakeVariant(device.SysfsPath),
		},
		"port": {
			"Ports": dbus.MakeVariant([][]any{{"ttyUSB7", uint32(2)}}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			objects := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
				"/org/freedesktop/ModemManager1/Modem/0": {
					modemManagerInterface: properties,
				},
			}
			err := externalManagedObjectConflict(selected, objects)
			if err == nil || !strings.Contains(err.Error(), "externally occupied") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExternalManagedObjectIgnoresUnassignedDevice(t *testing.T) {
	assigned := physicalDevice{
		SysfsPath: "/sys/devices/assigned",
		PortPath:  "/devices/assigned",
		Ports:     []kernelPort{{Subsystem: "tty", Name: "ttyUSB7"}},
	}
	selected := map[string]physicalDevice{
		assigned.SysfsPath: assigned,
		assigned.PortPath:  assigned,
	}
	objects := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		"/org/freedesktop/ModemManager1/Modem/0": {
			modemManagerInterface: {
				"Physdev": dbus.MakeVariant("/sys/devices/user-owned"),
				"Ports":   dbus.MakeVariant([][]any{{"ttyUSB8", uint32(2)}}),
			},
		},
	}
	if err := externalManagedObjectConflict(selected, objects); err != nil {
		t.Fatal(err)
	}
}
