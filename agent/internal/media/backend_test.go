package media

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCharPCMBackendUsesOnlyTheConfiguredPath(t *testing.T) {
	server, peer := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = peer.Close()
	})

	var gotPath string
	var gotFlags int
	var gotMode fs.FileMode
	backend := newTestCharPCMBackend(func(path string, flags int, mode fs.FileMode) (Device, error) {
		gotPath = path
		gotFlags = flags
		gotMode = mode
		return &statDevice{Device: server, info: characterDeviceInfo{}}, nil
	})
	format := mustFormat(t, 8000)

	device, err := backend.Open(context.Background(), "/dev/modemdeck-pcm0", format)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	verified, ok := device.(*statDevice)
	if !ok || verified.Device != server {
		t.Fatal("Open() did not return the verified injected char device")
	}
	if gotPath != "/dev/modemdeck-pcm0" ||
		gotFlags != os.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC ||
		gotMode != 0 {
		t.Fatalf("open = (%q, %d, %v)", gotPath, gotFlags, gotMode)
	}
}

func TestCharPCMBackendClearsNonblockingOnTheVerifiedDescriptor(t *testing.T) {
	backend := NewCharPCMBackend(nil)
	device, err := backend.Open(context.Background(), "/dev/null", mustFormat(t, 8000))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })

	connection, ok := device.(syscall.Conn)
	if !ok {
		t.Fatal("opened char device does not expose syscall.Conn")
	}
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn() error = %v", err)
	}
	var flags int
	var operationError error
	if err := rawConnection.Control(func(fd uintptr) {
		value, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		if errno != 0 {
			operationError = errno
			return
		}
		flags = int(value)
	}); err != nil {
		t.Fatalf("inspect descriptor: %v", err)
	}
	if operationError != nil {
		t.Fatalf("fcntl(F_GETFL): %v", operationError)
	}
	if flags&syscall.O_NONBLOCK != 0 {
		t.Fatalf("descriptor flags = %#x, O_NONBLOCK was not cleared", flags)
	}
}

func TestCharPCMBackendRejectsRelativePathWithoutOpening(t *testing.T) {
	opened := false
	backend := NewCharPCMBackend(func(string, int, fs.FileMode) (Device, error) {
		opened = true
		return nil, nil
	})

	_, err := backend.Open(context.Background(), "dev/modemdeck-pcm0", mustFormat(t, 8000))
	if !IsCode(err, ErrorInvalidArgument) {
		t.Fatalf("Open() error = %v, want invalid argument", err)
	}
	if opened {
		t.Fatal("relative char path reached the opener")
	}
}

func TestCharPCMBackendRejectsRegularFilesAndSymlinksBeforeOpen(t *testing.T) {
	directory := t.TempDir()
	regularPath := filepath.Join(directory, "pcm")
	if err := os.WriteFile(regularPath, []byte("not a device"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	symlinkPath := filepath.Join(directory, "pcm-link")
	if err := os.Symlink(regularPath, symlinkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	for _, endpoint := range []string{regularPath, symlinkPath} {
		opened := false
		backend := NewCharPCMBackend(func(string, int, fs.FileMode) (Device, error) {
			opened = true
			return nil, nil
		})
		if _, err := backend.Open(
			context.Background(),
			endpoint,
			mustFormat(t, 8000),
		); !IsCode(err, ErrorInvalidArgument) {
			t.Fatalf("Open(%q) error = %v, want invalid argument", endpoint, err)
		}
		if opened {
			t.Fatalf("unsafe endpoint %q reached the opener", endpoint)
		}
	}
}

func TestCharPCMBackendVerifiesOpenedFileDescriptor(t *testing.T) {
	server, peer := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = peer.Close()
	})
	backend := newTestCharPCMBackend(func(string, int, fs.FileMode) (Device, error) {
		return &statDevice{
			Device: server,
			info:   regularFileInfo{},
		}, nil
	})

	_, err := backend.Open(context.Background(), "/dev/pcm0", mustFormat(t, 8000))
	if !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("Open() error = %v, want backend unavailable", err)
	}
}

func TestCharPCMBackendClosesDescriptorWhenBlockingModeCannotBeRestored(t *testing.T) {
	device := &closeTrackingDevice{}
	backend := newTestCharPCMBackend(func(string, int, fs.FileMode) (Device, error) {
		return &statDevice{
			Device: device,
			info:   characterDeviceInfo{},
		}, nil
	})
	backend.clearNonblocking = func(Device) error {
		return errors.New("fcntl failed")
	}

	if _, err := backend.Open(
		context.Background(),
		"/dev/pcm0",
		mustFormat(t, 8000),
	); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("Open() error = %v, want backend unavailable", err)
	}
	if !device.closed {
		t.Fatal("descriptor was not closed after fcntl failure")
	}
}

func TestALSAPCMBackendDelegatesToInjectedDuplexOpener(t *testing.T) {
	server, peer := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = peer.Close()
	})
	opener := &recordingALSAOpener{device: server}
	backend := NewALSAPCMBackend(opener)
	format := mustFormat(t, 16000)

	device, err := backend.Open(context.Background(), "hw:2,0", format)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if device != server || opener.endpoint != "hw:2,0" || opener.format != format {
		t.Fatalf("unexpected ALSA delegation: endpoint=%q format=%+v", opener.endpoint, opener.format)
	}
}

func TestALSAPCMBackendRequiresInjectedOpener(t *testing.T) {
	_, err := NewALSAPCMBackend(nil).Open(
		context.Background(),
		"hw:2,0",
		mustFormat(t, 8000),
	)
	if !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("Open() error = %v, want backend unavailable", err)
	}
}

func TestBackendsRejectNoncanonicalFormatBeforeOpening(t *testing.T) {
	charOpened := false
	charBackend := newTestCharPCMBackend(func(string, int, fs.FileMode) (Device, error) {
		charOpened = true
		return nil, nil
	})
	format := mustFormat(t, 8000)
	format.FrameBytes++
	if _, err := charBackend.Open(context.Background(), "/dev/pcm0", format); !IsCode(err, ErrorUnsupportedFormat) {
		t.Fatalf("char Open() error = %v", err)
	}
	if charOpened {
		t.Fatal("char opener received a noncanonical format")
	}

	alsaOpener := &recordingALSAOpener{}
	if _, err := NewALSAPCMBackend(alsaOpener).Open(
		context.Background(),
		"hw:2,0",
		format,
	); !IsCode(err, ErrorUnsupportedFormat) {
		t.Fatalf("ALSA Open() error = %v", err)
	}
	if alsaOpener.endpoint != "" {
		t.Fatal("ALSA opener received a noncanonical format")
	}
}

func TestBackendsClosePartiallyOpenedDevices(t *testing.T) {
	charDevice := &closeTrackingDevice{}
	charBackend := newTestCharPCMBackend(func(string, int, fs.FileMode) (Device, error) {
		return charDevice, errors.New("partial char open")
	})
	if _, err := charBackend.Open(
		context.Background(),
		"/dev/pcm0",
		mustFormat(t, 8000),
	); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("char Open() error = %v", err)
	}
	if !charDevice.closed {
		t.Fatal("partially opened char device was not closed")
	}

	alsaDevice := &closeTrackingDevice{}
	alsaBackend := NewALSAPCMBackend(&recordingALSAOpener{
		device: alsaDevice,
		err:    errors.New("partial ALSA open"),
	})
	if _, err := alsaBackend.Open(
		context.Background(),
		"hw:2,0",
		mustFormat(t, 16000),
	); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("ALSA Open() error = %v", err)
	}
	if !alsaDevice.closed {
		t.Fatal("partially opened ALSA device was not closed")
	}
}

type recordingALSAOpener struct {
	endpoint string
	format   Format
	device   Device
	err      error
}

func (o *recordingALSAOpener) OpenDuplexPCM(
	_ context.Context,
	endpoint string,
	format Format,
) (Device, error) {
	o.endpoint = endpoint
	o.format = format
	return o.device, o.err
}

type statDevice struct {
	Device
	info fs.FileInfo
	err  error
}

func (d *statDevice) Stat() (fs.FileInfo, error) {
	return d.info, d.err
}

type characterDeviceInfo struct{}

func (characterDeviceInfo) Name() string       { return "pcm" }
func (characterDeviceInfo) Size() int64        { return 0 }
func (characterDeviceInfo) Mode() fs.FileMode  { return os.ModeDevice | os.ModeCharDevice | 0o600 }
func (characterDeviceInfo) ModTime() time.Time { return time.Time{} }
func (characterDeviceInfo) IsDir() bool        { return false }
func (characterDeviceInfo) Sys() any           { return nil }

type regularFileInfo struct{ characterDeviceInfo }

func (regularFileInfo) Mode() fs.FileMode { return 0o600 }

func newTestCharPCMBackend(open CharOpenFunc) *CharPCMBackend {
	backend := newCharPCMBackend(
		func(string) (fs.FileInfo, error) {
			return characterDeviceInfo{}, nil
		},
		open,
	)
	backend.sameFile = func(fs.FileInfo, fs.FileInfo) bool {
		return true
	}
	backend.clearNonblocking = func(Device) error {
		return nil
	}
	return backend
}

func mustFormat(t *testing.T, rate uint32) Format {
	t.Helper()
	format, err := ParseFormat(AdvertisedFormat{
		Encoding:   EncodingPCM,
		Resolution: ResolutionS16LE,
		Rate:       rate,
	})
	if err != nil {
		t.Fatalf("ParseFormat() error = %v", err)
	}
	return format
}
