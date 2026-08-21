package qdc507voice

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeClient struct {
	mu            sync.Mutex
	devices       []Device
	bootID        string
	routeLaunched bool
	soundReady    bool
	pushes        []string
	commands      []string
}

func (f *fakeClient) Devices(context.Context) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.devices...), nil
}

func (f *fakeClient) Shell(
	_ context.Context,
	_ string,
	command string,
	_ time.Duration,
) (ShellResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
	switch {
	case command == "id -u":
		return ShellResult{Output: "0"}, nil
	case command == "uname -r":
		return ShellResult{Output: "3.18.44"}, nil
	case command == "cat /proc/sys/kernel/random/boot_id":
		return ShellResult{Output: f.bootID}, nil
	case strings.Contains(command, "VoLTE route session active on hw:0,4"):
		if f.routeLaunched {
			return ShellResult{}, nil
		}
		return ShellResult{Status: 1}, nil
	case strings.HasPrefix(command, "test -c '/dev/snd/controlC0'"):
		if f.soundReady {
			return ShellResult{}, nil
		}
		return ShellResult{Status: 1}, nil
	case strings.Contains(command, "nohup '") && strings.Contains(command, "--voice-route-session"):
		f.routeLaunched = true
		return ShellResult{}, nil
	default:
		return ShellResult{}, nil
	}
}

func (f *fakeClient) Push(
	_ context.Context,
	_ string,
	_ string,
	remote string,
	_ fs.FileMode,
	_ time.Duration,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, remote)
	return nil
}

func TestParseDevicesPreservesUSBTopology(t *testing.T) {
	t.Parallel()
	devices := parseDevices(`List of devices attached
QDC507-1 device usb:1-10 product:qdc507 transport_id:2
phone-1 unauthorized usb:2-3 transport_id:3
`)
	if len(devices) != 2 || devices[0].USB != "usb:1-10" ||
		devices[0].State != "device" || devices[0].Selector != "transport:2" ||
		devices[1].State != "unauthorized" {
		t.Fatalf("parseDevices() = %+v", devices)
	}
}

func TestParseDevicesSupportsMissingSerialNumber(t *testing.T) {
	t.Parallel()
	devices := parseDevices(`List of devices attached
(no serial number)     device usb:1-10 transport_id:1
`)
	if len(devices) != 1 || devices[0].Selector != "transport:1" ||
		devices[0].State != "device" || devices[0].USB != "usb:1-10" {
		t.Fatalf("parseDevices() = %+v", devices)
	}
	arguments, err := adbTargetArguments(devices[0].Selector)
	if err != nil || strings.Join(arguments, " ") != "-t 1" {
		t.Fatalf("adbTargetArguments() = %q, %v", arguments, err)
	}
}

func TestReconcileAdoptsResidentRouteAndKeysItByModuleBoot(t *testing.T) {
	t.Parallel()
	client := &fakeClient{
		devices:       []Device{{Selector: "transport:2", State: "device", USB: "usb:1-10"}},
		bootID:        "module-boot-1",
		routeLaunched: true,
	}
	manager := testManager(client)
	line := testLine()
	changed, err := manager.Reconcile(context.Background(), []domain.Line{line})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !changed {
		t.Fatal("first resident-route adoption did not change state")
	}
	status := manager.Status(line)
	if !status.Configured || !status.Ready || status.RuntimeVersion != runtimeVersion {
		t.Fatalf("Status() = %+v", status)
	}
	if len(client.pushes) != 0 {
		t.Fatalf("resident route unexpectedly pushed files: %+v", client.pushes)
	}

	changed, err = manager.Reconcile(context.Background(), []domain.Line{line})
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if changed {
		t.Fatal("unchanged resident route changed state")
	}
}

func TestReconcileInitializesRouteOnceAndLeavesItResident(t *testing.T) {
	t.Parallel()
	client := &fakeClient{
		devices:    []Device{{Selector: "transport:2", State: "device", USB: "usb:1-10"}},
		bootID:     "module-boot-2",
		soundReady: true,
	}
	manager := testManager(client)
	line := testLine()
	changed, err := manager.Reconcile(context.Background(), []domain.Line{line})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !changed || !manager.Status(line).Ready {
		t.Fatalf("route status = %+v, changed = %t", manager.Status(line), changed)
	}
	if len(client.pushes) != len(expectedManifest.Files) {
		t.Fatalf("pushes = %+v", client.pushes)
	}
	launches := 0
	for _, command := range client.commands {
		if strings.Contains(command, "--voice-route-session") && strings.Contains(command, "nohup") {
			launches++
		}
		if strings.Contains(command, "kill -TERM") && strings.Contains(command, routePIDFile) {
			t.Fatalf("reconciler stopped its resident route: %q", command)
		}
	}
	if launches != 1 {
		t.Fatalf("route launch count = %d", launches)
	}

	if _, err := manager.Reconcile(context.Background(), []domain.Line{line}); err != nil {
		t.Fatalf("resident Reconcile() error = %v", err)
	}
	if len(client.pushes) != len(expectedManifest.Files) {
		t.Fatalf("resident route was reprovisioned: %+v", client.pushes)
	}
}

func TestReconcileFailsClosedWithoutMatchingUSBTopology(t *testing.T) {
	t.Parallel()
	client := &fakeClient{
		devices: []Device{{Selector: "transport:3", State: "device", USB: "usb:1-7"}},
		bootID:  "module-boot-3",
	}
	manager := testManager(client)
	manager.deviceWait = 5 * time.Millisecond
	manager.pollInterval = time.Millisecond
	line := testLine()
	changed, err := manager.Reconcile(context.Background(), []domain.Line{line})
	if err == nil || !changed {
		t.Fatalf("Reconcile() changed = %t, error = %v", changed, err)
	}
	status := manager.Status(line)
	if status.Ready || !strings.Contains(status.Reason, "usb:1-10") {
		t.Fatalf("Status() = %+v", status)
	}
}

func TestDisabledRuntimeDoesNotClaimQDC507(t *testing.T) {
	t.Parallel()
	manager, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if manager.Enabled() || manager.Status(testLine()).Configured {
		t.Fatalf("disabled manager claimed runtime: %+v", manager.Status(testLine()))
	}
}

func testManager(client Client) *Manager {
	return &Manager{
		runtimeDirectory: "/reviewed-runtime",
		manifest:         expectedManifest,
		client:           client,
		deviceWait:       50 * time.Millisecond,
		pollInterval:     time.Millisecond,
		states:           make(map[string]lineState),
	}
}

func testLine() domain.Line {
	return domain.Line{
		ID:             "line-qdc507",
		Model:          "QDC507",
		Revision:       "QDC507GLEFM21",
		PhysicalDevice: "/sys/devices/pci0000:00/usb1/1-10",
	}
}
