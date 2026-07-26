package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScannerDiscoversPortsBelowStablePhysicalDevice(t *testing.T) {
	root := t.TempDir()
	sysRoot := filepath.Join(root, "sys")
	devRoot := filepath.Join(root, "dev")
	udevRoot := filepath.Join(root, "run", "udev", "data")
	physical := filepath.Join(
		sysRoot,
		"devices",
		"pci0000:00",
		"0000:00:14.0",
		"usb1",
		"1-2",
	)
	portPath := filepath.Join(physical, "1-2:1.0", "ttyUSB0")

	for _, path := range []string{
		filepath.Join(sysRoot, "bus", "usb", "devices"),
		filepath.Join(sysRoot, "class", "tty"),
		portPath,
		devRoot,
		udevRoot,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{
		"idVendor":  "2c7c\n",
		"idProduct": "0125\n",
		"serial":    "stable-serial\n",
	} {
		if err := os.WriteFile(filepath.Join(physical, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(portPath, "dev"), []byte("188:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(udevRoot, "c188:0"), []byte("E:ID_MM_CANDIDATE=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, filepath.Join(sysRoot, "bus", "usb", "devices", "1-2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(portPath, filepath.Join(sysRoot, "class", "tty", "ttyUSB0")); err != nil {
		t.Fatal(err)
	}

	scanner, err := newSysfsScanner(sysRoot, devRoot, udevRoot)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := scanner.scanUSBDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %+v", devices)
	}
	device := devices[0]
	if device.Serial != "stable-serial" ||
		device.PortPath != "/devices/pci0000:00/0000:00:14.0/usb1/1-2" {
		t.Fatalf("device = %+v", device)
	}
	if len(device.Ports) != 1 ||
		device.Ports[0].Subsystem != "tty" ||
		device.Ports[0].Name != "ttyUSB0" ||
		device.Ports[0].UDevKey != "c188:0" {
		t.Fatalf("ports = %+v", device.Ports)
	}
}
