package media

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestALSACommandUsesQuectelUACTransferPeriods(t *testing.T) {
	type invocation struct {
		name string
		args []string
	}
	var invocations []invocation
	opener := &ALSACommandOpener{
		arecord: "arecord",
		aplay:   "aplay",
		command: func(name string, args ...string) *exec.Cmd {
			invocations = append(invocations, invocation{
				name: name,
				args: append([]string(nil), args...),
			})
			return exec.Command("sh", "-c", "cat")
		},
	}
	device, err := opener.OpenDuplexPCM(context.Background(), "hw:2,0", mustFormat(t, 8000))
	if err != nil {
		t.Fatalf("OpenDuplexPCM() error = %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })

	if len(invocations) != 2 {
		t.Fatalf("command count = %d, want 2", len(invocations))
	}
	if invocations[0].name != "arecord" ||
		!containsArgumentPair(invocations[0].args, "--period-size", "320") ||
		!containsArgumentPair(invocations[0].args, "--buffer-size", "640") {
		t.Fatalf("arecord invocation = %#v", invocations[0])
	}
	if invocations[1].name != "aplay" ||
		!containsArgumentPair(invocations[1].args, "--period-size", "800") ||
		!containsArgumentPair(invocations[1].args, "--buffer-size", "1600") ||
		!containsArgument(invocations[1].args, "--start-delay=-100000") {
		t.Fatalf("aplay invocation = %#v", invocations[1])
	}
}

func containsArgumentPair(args []string, name, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name && args[index+1] == value {
			return true
		}
	}
	return false
}

func containsArgument(args []string, value string) bool {
	for _, argument := range args {
		if argument == value {
			return true
		}
	}
	return false
}

func TestALSACommandDeviceUsesQuectelUACTransferBlocks(t *testing.T) {
	format := mustFormat(t, 8000)
	capture := bytes.Repeat([]byte{0x5a}, transferBytes(format, quectelUACCaptureDuration))
	playback := &writeBufferCloser{}
	device := &alsaCommandDevice{
		capture:          io.NopCloser(bytes.NewReader(capture)),
		playback:         playback,
		captureTransfer:  make([]byte, len(capture)),
		captureOffset:    len(capture),
		playbackTransfer: make([]byte, transferBytes(format, quectelUACPlaybackDuration)),
	}

	for frameIndex := 0; frameIndex < 2; frameIndex++ {
		frame := make([]byte, format.FrameBytes)
		read, err := device.Read(frame)
		if err != nil {
			t.Fatalf("Read(frame %d) error = %v", frameIndex, err)
		}
		if read != len(frame) || !bytes.Equal(frame, capture[frameIndex*len(frame):(frameIndex+1)*len(frame)]) {
			t.Fatalf("Read(frame %d) = %d bytes", frameIndex, read)
		}
	}

	frame := bytes.Repeat([]byte{0x31}, format.FrameBytes)
	for frameIndex := 0; frameIndex < 5; frameIndex++ {
		written, err := device.Write(frame)
		if err != nil {
			t.Fatalf("Write(frame %d) error = %v", frameIndex, err)
		}
		if written != len(frame) {
			t.Fatalf("Write(frame %d) = %d bytes, want %d", frameIndex, written, len(frame))
		}
		if frameIndex < 4 && playback.Len() != 0 {
			t.Fatalf("playback flushed after %d internal frames", frameIndex+1)
		}
	}
	if playback.Len() != transferBytes(format, quectelUACPlaybackDuration) {
		t.Fatalf("playback block = %d bytes", playback.Len())
	}
}

func TestALSACommandRejectsNonNativeQuectelUACRate(t *testing.T) {
	opener := &ALSACommandOpener{
		arecord: "arecord",
		aplay:   "aplay",
		command: exec.Command,
	}
	_, err := opener.OpenDuplexPCM(context.Background(), "hw:2,0", mustFormat(t, 16000))
	if !IsCode(err, ErrorUnsupportedFormat) {
		t.Fatalf("OpenDuplexPCM() error = %v, want unsupported format", err)
	}
}

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

type writeBufferCloser struct {
	bytes.Buffer
}

func (*writeBufferCloser) Close() error {
	return nil
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
