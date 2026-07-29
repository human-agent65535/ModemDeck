package media

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestALSACommandDeviceDoesNotStartProcessesUntilStart(t *testing.T) {
	opener := &ALSACommandOpener{
		arecord: "arecord",
		aplay:   "aplay",
		command: func(string, ...string) *exec.Cmd {
			return exec.Command("sh", "-c", "cat")
		},
	}
	device, err := opener.OpenDuplexPCM(context.Background(), "hw:2,0", mustFormat(t, 8000))
	if err != nil {
		t.Fatalf("OpenDuplexPCM() error = %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })

	commandDevice, ok := device.(*alsaCommandDevice)
	if !ok {
		t.Fatalf("device type = %T", device)
	}
	if commandDevice.captureCommand.Process != nil || commandDevice.playbackCommand.Process != nil {
		t.Fatal("ALSA processes started during OPEN")
	}
	startContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := commandDevice.Start(startContext); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if commandDevice.captureCommand.Process == nil || commandDevice.playbackCommand.Process == nil {
		t.Fatal("ALSA processes did not start after START")
	}
	if err := commandDevice.Start(startContext); !IsCode(err, ErrorConflict) {
		t.Fatalf("second Start() error = %v, want conflict", err)
	}
}

func TestALSACommandStartFailureIsTypedAndClosesCleanly(t *testing.T) {
	commandIndex := 0
	opener := &ALSACommandOpener{
		arecord: "arecord",
		aplay:   "aplay",
		command: func(string, ...string) *exec.Cmd {
			commandIndex++
			if commandIndex == 1 {
				return exec.Command("sh", "-c", "cat")
			}
			return exec.Command("/modemdeck-test-command-does-not-exist")
		},
	}
	device, err := opener.OpenDuplexPCM(context.Background(), "hw:2,0", mustFormat(t, 8000))
	if err != nil {
		t.Fatalf("OpenDuplexPCM() error = %v", err)
	}
	commandDevice := device.(*alsaCommandDevice)
	if err := commandDevice.Start(context.Background()); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("Start() error = %v, want backend unavailable", err)
	}
	if err := commandDevice.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestALSACommandCaptureErrorIncludesBoundedToolOutput(t *testing.T) {
	commandIndex := 0
	opener := &ALSACommandOpener{
		arecord: "arecord",
		aplay:   "aplay",
		command: func(string, ...string) *exec.Cmd {
			commandIndex++
			if commandIndex == 1 {
				return exec.Command("sh", "-c", "printf 'UAC capture failed' >&2; exit 1")
			}
			return exec.Command("sh", "-c", "cat >/dev/null")
		},
	}
	device, err := opener.OpenDuplexPCM(context.Background(), "hw:2,0", mustFormat(t, 8000))
	if err != nil {
		t.Fatalf("OpenDuplexPCM() error = %v", err)
	}
	commandDevice := device.(*alsaCommandDevice)
	t.Cleanup(func() { _ = commandDevice.Close() })
	if err := commandDevice.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = commandDevice.Read(make([]byte, 320))
	if err == nil || !strings.Contains(err.Error(), "arecord: UAC capture failed") {
		t.Fatalf("Read() error = %v", err)
	}
}
