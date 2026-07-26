package usbrecovery

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type characterDeviceInfo struct {
	fs.FileInfo
}

func (characterDeviceInfo) Name() string       { return "usb-device" }
func (characterDeviceInfo) Size() int64        { return 0 }
func (characterDeviceInfo) Mode() fs.FileMode  { return os.ModeCharDevice | 0o660 }
func (characterDeviceInfo) ModTime() time.Time { return time.Time{} }
func (characterDeviceInfo) IsDir() bool        { return false }
func (characterDeviceInfo) Sys() any           { return nil }

func TestCapabilityResolvesUSBInterfaceToDeviceNode(t *testing.T) {
	sysRoot, physicalDevice, deviceRoot, deviceNode := testUSBTree(t, "00")
	controller := newController(controllerOptions{
		sysDevicesRoot: sysRoot,
		usbDeviceRoot:  deviceRoot,
		stat: func(path string) (fs.FileInfo, error) {
			if path != deviceNode {
				t.Fatalf("stat path = %q, want %q", path, deviceNode)
			}
			return characterDeviceInfo{}, nil
		},
		writable: func(path string) error {
			if path != deviceNode {
				t.Fatalf("writable path = %q, want %q", path, deviceNode)
			}
			return nil
		},
	})

	capability := controller.Capability(physicalDevice)
	if !capability.Supported || !capability.Implemented ||
		!capability.Readable || !capability.Writable {
		t.Fatalf("Capability() = %#v, want fully available", capability)
	}
	if capability.Backend != "linux_usbfs" || capability.Reason != "" {
		t.Fatalf("Capability() = %#v, want linux_usbfs without reason", capability)
	}
}

func TestCapabilityRejectsNonUSBAndHostControllerPaths(t *testing.T) {
	t.Run("outside sysfs root", func(t *testing.T) {
		sysRoot := t.TempDir()
		outside := t.TempDir()
		controller := newController(controllerOptions{sysDevicesRoot: sysRoot})

		capability := controller.Capability(outside)
		if capability.Supported || capability.Writable {
			t.Fatalf("Capability() = %#v, want unsupported", capability)
		}
		if capability.Reason != "ModemManager physical device path is outside the host sysfs device tree" {
			t.Fatalf("reason = %q", capability.Reason)
		}
	})

	t.Run("root hub", func(t *testing.T) {
		sysRoot, physicalDevice, deviceRoot, _ := testUSBTree(t, "09")
		controller := newController(controllerOptions{
			sysDevicesRoot: sysRoot,
			usbDeviceRoot:  deviceRoot,
		})

		capability := controller.Capability(physicalDevice)
		if capability.Supported || capability.Writable {
			t.Fatalf("Capability() = %#v, want unsupported", capability)
		}
		if capability.Reason != "resolved sysfs object is a USB host controller" {
			t.Fatalf("reason = %q", capability.Reason)
		}
	})
}

func TestCapabilityReportsMissingWriteAccess(t *testing.T) {
	sysRoot, physicalDevice, deviceRoot, deviceNode := testUSBTree(t, "00")
	controller := newController(controllerOptions{
		sysDevicesRoot: sysRoot,
		usbDeviceRoot:  deviceRoot,
		stat: func(string) (fs.FileInfo, error) {
			return characterDeviceInfo{}, nil
		},
		writable: func(string) error {
			return fs.ErrPermission
		},
	})

	capability := controller.Capability(physicalDevice)
	if !capability.Supported || !capability.Readable || capability.Writable {
		t.Fatalf("Capability() = %#v, want detected but not writable", capability)
	}
	if capability.Reason != "host agent does not have write access to the USB device node" {
		t.Fatalf("reason = %q", capability.Reason)
	}
	if deviceNode == "" {
		t.Fatal("device node fixture is empty")
	}
}

func TestResetUsesResolvedNodeAndEnforcesCooldown(t *testing.T) {
	sysRoot, physicalDevice, deviceRoot, deviceNode := testUSBTree(t, "00")
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	var resetPaths []string
	controller := newController(controllerOptions{
		sysDevicesRoot: sysRoot,
		usbDeviceRoot:  deviceRoot,
		cooldown:       2 * time.Minute,
		now:            func() time.Time { return now },
		stat: func(string) (fs.FileInfo, error) {
			return characterDeviceInfo{}, nil
		},
		writable: func(string) error { return nil },
		reset: func(_ context.Context, path string) error {
			resetPaths = append(resetPaths, path)
			return nil
		},
	})

	if err := controller.Reset(context.Background(), physicalDevice); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if len(resetPaths) != 1 || resetPaths[0] != deviceNode {
		t.Fatalf("reset paths = %#v, want [%q]", resetPaths, deviceNode)
	}
	err := controller.Reset(context.Background(), physicalDevice)
	var cooldown *CooldownError
	if !errors.As(err, &cooldown) {
		t.Fatalf("second Reset() error = %v, want CooldownError", err)
	}
	if cooldown.Remaining != 2*time.Minute {
		t.Fatalf("remaining = %s, want 2m", cooldown.Remaining)
	}

	now = now.Add(2 * time.Minute)
	if err := controller.Reset(context.Background(), physicalDevice); err != nil {
		t.Fatalf("Reset() after cooldown error = %v", err)
	}
	if len(resetPaths) != 2 {
		t.Fatalf("reset count = %d, want 2", len(resetPaths))
	}
}

func TestResetAllowsOnlyOneUSBOperationAtATime(t *testing.T) {
	sysRoot, physicalDevice, deviceRoot, _ := testUSBTree(t, "00")
	started := make(chan struct{})
	release := make(chan struct{})
	controller := newController(controllerOptions{
		sysDevicesRoot: sysRoot,
		usbDeviceRoot:  deviceRoot,
		stat: func(string) (fs.FileInfo, error) {
			return characterDeviceInfo{}, nil
		},
		writable: func(string) error { return nil },
		reset: func(context.Context, string) error {
			close(started)
			<-release
			return nil
		},
	})

	firstResult := make(chan error, 1)
	go func() {
		firstResult <- controller.Reset(context.Background(), physicalDevice)
	}()
	<-started
	if err := controller.Reset(context.Background(), physicalDevice); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent Reset() error = %v, want ErrBusy", err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first Reset() error = %v", err)
	}
}

func testUSBTree(t *testing.T, deviceClass string) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	sysRoot := filepath.Join(root, "sys", "devices")
	usbDevice := filepath.Join(sysRoot, "pci0000:00", "0000:00:14.0", "usb2", "2-3")
	physicalDevice := filepath.Join(usbDevice, "2-3:1.4")
	if err := os.MkdirAll(physicalDevice, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"busnum":       "2\n",
		"devnum":       "7\n",
		"idVendor":     "2c7c\n",
		"idProduct":    "0800\n",
		"bDeviceClass": deviceClass + "\n",
	} {
		if err := os.WriteFile(filepath.Join(usbDevice, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deviceRoot := filepath.Join(root, "dev", "bus", "usb")
	deviceNode := filepath.Join(deviceRoot, "002", "007")
	return sysRoot, physicalDevice, deviceRoot, deviceNode
}
