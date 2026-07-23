package media

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type BackendKind string

const (
	BackendCharPCM BackendKind = "char-pcm"
	BackendALSAPCM BackendKind = "alsa-pcm"
)

type Device interface {
	io.Reader
	io.Writer
	io.Closer
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

// DeviceStarter is implemented by prepared devices whose underlying media I/O
// must not begin until the peer sends START. Character devices do not need it;
// the ALSA command backend does.
type DeviceStarter interface {
	Start(context.Context) error
}

type Backend interface {
	Open(context.Context, string, Format) (Device, error)
}

type CharOpenFunc func(string, int, fs.FileMode) (Device, error)

type CharPCMBackend struct {
	lstat            func(string) (fs.FileInfo, error)
	open             CharOpenFunc
	sameFile         func(fs.FileInfo, fs.FileInfo) bool
	clearNonblocking func(Device) error
}

func NewCharPCMBackend(open CharOpenFunc) *CharPCMBackend {
	return newCharPCMBackend(os.Lstat, open)
}

func newCharPCMBackend(
	lstat func(string) (fs.FileInfo, error),
	open CharOpenFunc,
) *CharPCMBackend {
	if lstat == nil {
		lstat = os.Lstat
	}
	if open == nil {
		open = func(path string, flag int, mode fs.FileMode) (Device, error) {
			return os.OpenFile(path, flag, mode)
		}
	}
	return &CharPCMBackend{
		lstat:            lstat,
		open:             open,
		sameFile:         os.SameFile,
		clearNonblocking: clearDeviceNonblocking,
	}
}

func (b *CharPCMBackend) Open(ctx context.Context, endpoint string, format Format) (Device, error) {
	const operation = "open_char_pcm"

	if err := ctx.Err(); err != nil {
		return nil, NewError(ErrorBackendUnavailable, operation, "media open canceled", err)
	}
	if endpoint == "" ||
		!filepath.IsAbs(endpoint) ||
		filepath.Clean(endpoint) != endpoint ||
		strings.TrimSpace(endpoint) != endpoint {
		return nil, NewError(
			ErrorInvalidArgument,
			operation,
			"char-pcm endpoint must be an explicit canonical absolute path",
			nil,
		)
	}
	if err := validateFormat(format); err != nil {
		return nil, err
	}

	pathInfo, err := b.lstat(endpoint)
	if err != nil {
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"char-pcm endpoint could not be inspected",
			err,
		)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, NewError(
			ErrorInvalidArgument,
			operation,
			"char-pcm endpoint must not be a symbolic link",
			nil,
		)
	}
	if !isCharacterDevice(pathInfo.Mode()) {
		return nil, NewError(
			ErrorInvalidArgument,
			operation,
			"char-pcm endpoint is not a character device",
			nil,
		)
	}

	device, err := b.open(
		endpoint,
		os.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		if device != nil {
			_ = device.Close()
		}
		return nil, NewError(ErrorBackendUnavailable, operation, "char-pcm endpoint could not be opened", err)
	}
	if device == nil {
		return nil, NewError(ErrorBackendUnavailable, operation, "char-pcm opener returned no device", nil)
	}
	statter, ok := device.(interface {
		Stat() (fs.FileInfo, error)
	})
	if !ok {
		_ = device.Close()
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"opened char-pcm endpoint cannot be verified",
			nil,
		)
	}
	openedInfo, err := statter.Stat()
	if err != nil {
		_ = device.Close()
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"opened char-pcm endpoint could not be inspected",
			err,
		)
	}
	if !isCharacterDevice(openedInfo.Mode()) || !b.sameFile(pathInfo, openedInfo) {
		_ = device.Close()
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"opened char-pcm endpoint failed character-device verification",
			nil,
		)
	}
	if err := b.clearNonblocking(device); err != nil {
		_ = device.Close()
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"opened char-pcm endpoint could not enter blocking I/O mode",
			err,
		)
	}
	return device, nil
}

func isCharacterDevice(mode fs.FileMode) bool {
	return mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0
}

func clearDeviceNonblocking(device Device) error {
	connection, ok := device.(syscall.Conn)
	if !ok {
		return errors.New("opened char-pcm endpoint does not expose a file descriptor")
	}
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var operationError error
	if err := rawConnection.Control(func(fd uintptr) {
		operationError = syscall.SetNonblock(int(fd), false)
	}); err != nil {
		return err
	}
	return operationError
}

type ALSAOpener interface {
	OpenDuplexPCM(context.Context, string, Format) (Device, error)
}

type ALSAPCMBackend struct {
	opener ALSAOpener
}

func NewALSAPCMBackend(opener ALSAOpener) *ALSAPCMBackend {
	return &ALSAPCMBackend{opener: opener}
}

func (b *ALSAPCMBackend) Open(ctx context.Context, endpoint string, format Format) (Device, error) {
	const operation = "open_alsa_pcm"

	if err := ctx.Err(); err != nil {
		return nil, NewError(ErrorBackendUnavailable, operation, "media open canceled", err)
	}
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(endpoint) != endpoint {
		return nil, NewError(
			ErrorInvalidArgument,
			operation,
			"alsa-pcm endpoint must be an explicit device name",
			nil,
		)
	}
	if b.opener == nil {
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"alsa-pcm opener is not configured",
			nil,
		)
	}
	if err := validateFormat(format); err != nil {
		return nil, err
	}

	device, err := b.opener.OpenDuplexPCM(ctx, endpoint, format)
	if err != nil {
		if device != nil {
			_ = device.Close()
		}
		return nil, NewError(ErrorBackendUnavailable, operation, "alsa-pcm endpoint could not be opened", err)
	}
	if device == nil {
		return nil, NewError(ErrorBackendUnavailable, operation, "alsa-pcm opener returned no device", nil)
	}
	return device, nil
}
