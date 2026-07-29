package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type commandFactory func(string, ...string) *exec.Cmd

const maxALSAErrorBytes = 4 << 10

// ALSACommandOpener uses the established alsa-utils PCM tools as an explicit
// full-duplex backend. It never discovers a device: the configured ALSA name
// is passed verbatim to both processes.
type ALSACommandOpener struct {
	arecord string
	aplay   string
	command commandFactory
}

func NewALSACommandOpener() (*ALSACommandOpener, error) {
	arecord, err := exec.LookPath("arecord")
	if err != nil {
		return nil, NewError(
			ErrorBackendUnavailable,
			"configure_alsa_pcm",
			"arecord is not installed",
			err,
		)
	}
	aplay, err := exec.LookPath("aplay")
	if err != nil {
		return nil, NewError(
			ErrorBackendUnavailable,
			"configure_alsa_pcm",
			"aplay is not installed",
			err,
		)
	}
	return &ALSACommandOpener{
		arecord: arecord,
		aplay:   aplay,
		command: exec.Command,
	}, nil
}

func (o *ALSACommandOpener) OpenDuplexPCM(
	ctx context.Context,
	endpoint string,
	format Format,
) (Device, error) {
	if o == nil || o.command == nil || o.arecord == "" || o.aplay == "" {
		return nil, NewError(
			ErrorBackendUnavailable,
			"open_alsa_pcm",
			"alsa-utils command backend is not configured",
			nil,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, NewError(ErrorBackendUnavailable, "open_alsa_pcm", "media open canceled", err)
	}
	args := []string{
		"--quiet",
		"--device", endpoint,
		"--file-type", "raw",
		"--format", "S16_LE",
		"--channels", strconv.Itoa(int(format.Channels)),
		"--rate", strconv.FormatUint(uint64(format.Rate), 10),
	}
	captureCommand := o.command(o.arecord, args...)
	captureErrors := &boundedCommandOutput{limit: maxALSAErrorBytes}
	captureCommand.Stderr = captureErrors
	capture, err := captureCommand.StdoutPipe()
	if err != nil {
		return nil, NewError(ErrorBackendUnavailable, "open_alsa_pcm", "create arecord output", err)
	}
	playbackCommand := o.command(o.aplay, args...)
	playbackErrors := &boundedCommandOutput{limit: maxALSAErrorBytes}
	playbackCommand.Stderr = playbackErrors
	playback, err := playbackCommand.StdinPipe()
	if err != nil {
		_ = capture.Close()
		return nil, NewError(ErrorBackendUnavailable, "open_alsa_pcm", "create aplay input", err)
	}
	return &alsaCommandDevice{
		capture:         capture,
		playback:        playback,
		captureCommand:  captureCommand,
		playbackCommand: playbackCommand,
		captureErrors:   captureErrors,
		playbackErrors:  playbackErrors,
	}, nil
}

type alsaCommandDevice struct {
	capture         io.ReadCloser
	playback        io.WriteCloser
	captureCommand  *exec.Cmd
	playbackCommand *exec.Cmd
	captureErrors   *boundedCommandOutput
	playbackErrors  *boundedCommandOutput

	closeOnce sync.Once
	closeErr  error
	startMu   sync.Mutex
	started   bool
	closed    bool
}

func (d *alsaCommandDevice) Read(destination []byte) (int, error) {
	read, err := d.capture.Read(destination)
	if err != nil {
		err = commandIOError("arecord", d.captureErrors, err)
	}
	return read, err
}

func (d *alsaCommandDevice) Write(source []byte) (int, error) {
	written, err := d.playback.Write(source)
	if err != nil {
		err = commandIOError("aplay", d.playbackErrors, err)
	}
	return written, err
}

func (d *alsaCommandDevice) SetReadDeadline(deadline time.Time) error {
	deadlineReader, ok := d.capture.(interface {
		SetReadDeadline(time.Time) error
	})
	if !ok {
		return NewError(
			ErrorBackendUnavailable,
			"configure_alsa_pcm",
			"arecord pipe does not support deadlines",
			nil,
		)
	}
	return deadlineReader.SetReadDeadline(deadline)
}

func (d *alsaCommandDevice) SetWriteDeadline(deadline time.Time) error {
	deadlineWriter, ok := d.playback.(interface {
		SetWriteDeadline(time.Time) error
	})
	if !ok {
		return NewError(
			ErrorBackendUnavailable,
			"configure_alsa_pcm",
			"aplay pipe does not support deadlines",
			nil,
		)
	}
	return deadlineWriter.SetWriteDeadline(deadline)
}

func (d *alsaCommandDevice) Start(ctx context.Context) error {
	const operation = "start_alsa_pcm"

	d.startMu.Lock()
	defer d.startMu.Unlock()
	if d.closed {
		return NewError(ErrorConflict, operation, "alsa-pcm device is closed", nil)
	}
	if d.started {
		return NewError(ErrorConflict, operation, "alsa-pcm device is already started", nil)
	}
	if err := ctx.Err(); err != nil {
		return NewError(ErrorTimeout, operation, "alsa-pcm start canceled", err)
	}
	if err := d.captureCommand.Start(); err != nil {
		return NewError(ErrorBackendUnavailable, operation, "start arecord", err)
	}
	if err := ctx.Err(); err != nil {
		_ = stopCommand(d.captureCommand)
		d.captureCommand = nil
		return NewError(ErrorTimeout, operation, "alsa-pcm start canceled", err)
	}
	if err := d.playbackCommand.Start(); err != nil {
		_ = stopCommand(d.captureCommand)
		d.captureCommand = nil
		return NewError(ErrorBackendUnavailable, operation, "start aplay", err)
	}
	d.started = true
	return nil
}

func (d *alsaCommandDevice) Close() error {
	d.closeOnce.Do(func() {
		d.startMu.Lock()
		d.closed = true
		d.startMu.Unlock()
		d.closeErr = errors.Join(
			closePipe(d.capture),
			closePipe(d.playback),
			stopCommand(d.captureCommand),
			stopCommand(d.playbackCommand),
		)
	})
	return d.closeErr
}

func closePipe(closer io.Closer) error {
	if closer == nil {
		return nil
	}
	err := closer.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func stopCommand(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	killErr := command.Process.Kill()
	waitErr := command.Wait()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	var exitError *exec.ExitError
	if errors.As(waitErr, &exitError) {
		waitErr = nil
	}
	return errors.Join(killErr, waitErr)
}

type boundedCommandOutput struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (output *boundedCommandOutput) Write(payload []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if remaining := output.limit - len(output.data); remaining > 0 {
		if len(payload) < remaining {
			remaining = len(payload)
		}
		output.data = append(output.data, payload[:remaining]...)
	}
	return len(payload), nil
}

func (output *boundedCommandOutput) String() string {
	if output == nil {
		return ""
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	return strings.TrimSpace(string(output.data))
}

func commandIOError(command string, output *boundedCommandOutput, err error) error {
	if detail := output.String(); detail != "" {
		return fmt.Errorf("%s: %s: %w", command, detail, err)
	}
	return err
}
