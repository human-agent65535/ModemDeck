package qdc507usb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeInhibitor struct {
	mu       sync.Mutex
	uids     []string
	released int
}

func (f *fakeInhibitor) Inhibit(
	_ context.Context,
	uid string,
) (func(context.Context) error, error) {
	f.mu.Lock()
	f.uids = append(f.uids, uid)
	f.mu.Unlock()
	return func(context.Context) error {
		f.mu.Lock()
		f.released++
		f.mu.Unlock()
		return nil
	}, nil
}

type fakeATResponse struct {
	response string
	err      error
}

type fakeATClient struct {
	mu        sync.Mutex
	responses map[string][]fakeATResponse
	commands  []string
}

func (f *fakeATClient) Command(
	_ context.Context,
	_ string,
	command string,
	_ time.Duration,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
	queue := f.responses[command]
	if len(queue) == 0 {
		return "", errors.New("unexpected AT command")
	}
	f.responses[command] = queue[1:]
	return queue[0].response, queue[0].err
}

type fakeDeviceAccess struct {
	initial      USBInterfaces
	post         USBInterfaces
	port         ATPort
	inspectCount int
	waitCount    int
}

func (f *fakeDeviceAccess) Inspect(string) (USBInterfaces, error) {
	f.inspectCount++
	if f.inspectCount > 1 && f.post.VendorID != 0 {
		return f.post, nil
	}
	return f.initial, nil
}

func (f *fakeDeviceAccess) ATPort(domain.Line) (ATPort, error) {
	return f.port, nil
}

func (f *fakeDeviceAccess) WaitForReenumeration(
	_ context.Context,
	_ string,
	previous ATPort,
	_ int,
	_ int,
	_ time.Duration,
) (ATPort, USBInterfaces, error) {
	f.waitCount++
	return previous, f.post, nil
}

func TestEnsurePreservesCompositionAndUsesFreshQADBKEYAfterReboot(t *testing.T) {
	t.Parallel()
	query := `AT+QCFG="USBCFG"`
	original := "+QCFG: \"usbcfg\",0x2C7C,0x0125,1,1,1,1,1,0,1\r\nOK\r\n"
	target := "+QCFG: \"usbcfg\",0x2C7C,0x0125,1,1,1,1,1,1,1\r\nOK\r\n"
	write := `AT+QCFG="USBCFG",0x2C7C,0x0125,1,1,1,1,1,1,1`
	firstPassword, err := legacyQADBUnlockPassword("12345678")
	if err != nil {
		t.Fatal(err)
	}
	secondPassword, err := legacyQADBUnlockPassword("15478726")
	if err != nil {
		t.Fatal(err)
	}
	at := &fakeATClient{responses: map[string][]fakeATResponse{
		query: {
			{response: original},
			{response: target},
			{response: target},
		},
		"AT+QADBKEY?": {
			{response: "+QADBKEY: 12345678\r\nOK\r\n"},
			{response: "+QADBKEY: 15478726\r\nOK\r\n"},
		},
		`AT+QADBKEY="` + firstPassword + `"`:  {{response: "OK\r\n"}},
		`AT+QADBKEY="` + secondPassword + `"`: {{response: "OK\r\n"}},
		write:                                 {{response: "OK\r\n"}},
		"AT+CFUN=1,1":                         {{err: errors.New("serial AT port disconnected")}},
	}}
	devices := &fakeDeviceAccess{
		initial: USBInterfaces{
			VendorID:     0x2c7c,
			ProductID:    0x0125,
			UACControl:   true,
			UACStreaming: true,
		},
		post: USBInterfaces{
			VendorID:     0x2c7c,
			ProductID:    0x0125,
			ADB:          true,
			UACControl:   true,
			UACStreaming: true,
		},
		port: ATPort{
			Device:          "/dev/ttyUSB6",
			Interface:       "1-10:1.2",
			SysfsDevicePath: "/sys/devices/pci0000:00/usb1/1-10/1-10:1.2/ttyUSB6",
		},
	}
	inhibitor := &fakeInhibitor{}
	manager, err := New(Options{
		Inhibitor:           inhibitor,
		ATClient:            at,
		DeviceAccess:        devices,
		ADBSettleDelay:      time.Nanosecond,
		PostAuthSettleDelay: time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := manager.Ensure(context.Background(), testLine())
	if err != nil || !changed {
		t.Fatalf("Ensure() changed = %t, error = %v", changed, err)
	}
	if devices.waitCount != 1 || inhibitor.released != 1 {
		t.Fatalf("waits = %d, releases = %d", devices.waitCount, inhibitor.released)
	}
	if !containsCommand(at.commands, write) {
		t.Fatalf("commands did not contain preserved write: %v", at.commands)
	}
	if got := countCommand(at.commands, "AT+QADBKEY?"); got != 2 {
		t.Fatalf("QADBKEY challenge count = %d, want 2", got)
	}
}

func TestEnsureAdoptsAlreadyLiveADBAndUACWithoutInhibition(t *testing.T) {
	t.Parallel()
	devices := &fakeDeviceAccess{initial: USBInterfaces{
		VendorID:     0x2c7c,
		ProductID:    0x0125,
		ADB:          true,
		UACControl:   true,
		UACStreaming: true,
	}}
	inhibitor := &fakeInhibitor{}
	at := &fakeATClient{}
	manager, err := New(Options{Inhibitor: inhibitor, ATClient: at, DeviceAccess: devices})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := manager.Ensure(context.Background(), testLine())
	if err != nil || changed {
		t.Fatalf("Ensure() changed = %t, error = %v", changed, err)
	}
	if len(inhibitor.uids) != 0 || len(at.commands) != 0 {
		t.Fatalf("already-ready device was mutated: uids=%v commands=%v", inhibitor.uids, at.commands)
	}
}

func TestEnsureDoesNotRecoveryRebootMatchingSavedComposition(t *testing.T) {
	t.Parallel()
	query := `AT+QCFG="USBCFG"`
	at := &fakeATClient{responses: map[string][]fakeATResponse{
		query: {{response: "+QCFG: \"usbcfg\",0x2C7C,0x0125,1,1,1,1,1,1,1\r\nOK\r\n"}},
	}}
	devices := &fakeDeviceAccess{
		initial: USBInterfaces{VendorID: 0x2c7c, ProductID: 0x0125, UACControl: true, UACStreaming: true},
		port:    ATPort{Device: "/dev/ttyUSB6", Interface: "1-10:1.2"},
	}
	inhibitor := &fakeInhibitor{}
	manager, err := New(Options{Inhibitor: inhibitor, ATClient: at, DeviceAccess: devices})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := manager.Ensure(context.Background(), testLine())
	if err == nil || changed || !strings.Contains(err.Error(), "will not reboot") {
		t.Fatalf("Ensure() changed = %t, error = %v", changed, err)
	}
	if containsCommand(at.commands, "AT+CFUN=1,1") {
		t.Fatalf("matching saved composition was rebooted: %v", at.commands)
	}
}

func TestLegacyQADBKeyDerivationMatchesPublishedVectors(t *testing.T) {
	t.Parallel()
	vectors := map[string]string{
		"12345678": "0jXKXQwSwMxYoeg",
		"15478726": "n9Qq0s1x4LtgAvt",
		"31711264": "SV3LHz1ynUZZmYU",
	}
	for challenge, expected := range vectors {
		actual, err := legacyQADBUnlockPassword(challenge)
		if err != nil {
			t.Fatalf("legacyQADBUnlockPassword(%q): %v", challenge, err)
		}
		if actual != expected {
			t.Fatalf("legacyQADBUnlockPassword(%q) = %q, want %q", challenge, actual, expected)
		}
	}
}

func TestUSBTargetAndReadbackPreserveUnrelatedFunctionBits(t *testing.T) {
	t.Parallel()
	original, err := parseUSBComposition(
		"AT+QCFG=\"USBCFG\"\r\n+QCFG: \"usbcfg\",0x2CA3,0x4006,1,0,1,1,1,0,0\r\nOK\r\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := original.voiceTarget()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(target.Flags, []int{1, 0, 1, 1, 1, 1, 1}) {
		t.Fatalf("target flags = %v", target.Flags)
	}
	if got := target.command(); got != `AT+QCFG="USBCFG",0x2CA3,0x4006,1,0,1,1,1,1,1` {
		t.Fatalf("target command = %q", got)
	}
	drift := target
	drift.Flags = append([]int(nil), target.Flags...)
	drift.Flags[1] = 1
	if err := validateReadBackBeforeReboot(original, target, drift); err == nil {
		t.Fatal("unrelated function-bit drift was accepted")
	}
}

func testLine() domain.Line {
	return domain.Line{
		ID:             "line-qdc507",
		Model:          "QDC507",
		Revision:       "QDC507GLEFM21",
		PhysicalDevice: "/sys/devices/pci0000:00/usb1/1-10",
		Ports: []domain.ModemPort{{
			Name: "ttyUSB6",
			Type: "at",
		}},
	}
}

func containsCommand(commands []string, wanted string) bool {
	return countCommand(commands, wanted) > 0
}

func countCommand(commands []string, wanted string) int {
	count := 0
	for _, command := range commands {
		if command == wanted {
			count++
		}
	}
	return count
}
