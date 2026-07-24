package media

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestManagerRequiresEveryAuthoritativeMediaGateBeforeOpen(t *testing.T) {
	validCall := testCall("call-1", "audio-1", 8000)
	tests := []struct {
		name string
		call Call
		code ErrorCode
	}{
		{
			name: "call identity mismatch",
			call: func() Call {
				call := validCall
				call.ID = "call-other"
				return call
			}(),
			code: ErrorNotFound,
		},
		{
			name: "call is not active",
			call: func() Call {
				call := validCall
				call.State = "ringing_out"
				return call
			}(),
			code: ErrorNotActive,
		},
		{
			name: "availability not confirmed",
			call: func() Call {
				call := validCall
				call.MediaAvailable = false
				return call
			}(),
			code: ErrorMediaUnavailable,
		},
		{
			name: "audio port missing",
			call: func() Call {
				call := validCall
				call.AudioPort = ""
				return call
			}(),
			code: ErrorMediaUnavailable,
		},
		{
			name: "audio format missing",
			call: func() Call {
				call := validCall
				call.AudioFormat = nil
				return call
			}(),
			code: ErrorMediaUnavailable,
		},
		{
			name: "format unsupported",
			call: func() Call {
				call := validCall
				call.AudioFormat = &AdvertisedFormat{
					Encoding:   EncodingPCM,
					Resolution: ResolutionS16LE,
					Rate:       48000,
				}
				return call
			}(),
			code: ErrorUnsupportedFormat,
		},
		{
			name: "audio port not bound",
			call: func() Call {
				call := validCall
				call.AudioPort = "audio-unbound"
				return call
			}(),
			code: ErrorUnboundAudioPort,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &recordingBackend{}
			manager := mustManager(
				t,
				CallSourceFunc(func(context.Context, string) (Call, error) {
					return test.call, nil
				}),
				[]Binding{{
					AudioPort: "audio-1",
					Backend:   BackendCharPCM,
					Endpoint:  "/dev/pcm0",
				}},
				map[BackendKind]Backend{BackendCharPCM: backend},
			)

			_, err := manager.Open(context.Background(), "call-1")
			if !IsCode(err, test.code) {
				t.Fatalf("Open() error = %v, want %s", err, test.code)
			}
			if backend.opens != 0 {
				t.Fatalf("backend opened %d times before media gates passed", backend.opens)
			}
		})
	}
}

func TestManagerUsesOneEligibilityRuleForOpenAndStart(t *testing.T) {
	for _, state := range []string{"active", "held"} {
		t.Run(state, func(t *testing.T) {
			var stateMu sync.Mutex
			currentState := state
			source := CallSourceFunc(func(context.Context, string) (Call, error) {
				stateMu.Lock()
				defer stateMu.Unlock()
				call := testCall("call-1", "audio-1", 8000)
				call.State = currentState
				return call, nil
			})
			device, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			manager := mustManager(
				t,
				source,
				[]Binding{{
					AudioPort: "audio-1",
					Backend:   BackendCharPCM,
					Endpoint:  "/dev/pcm0",
				}},
				map[BackendKind]Backend{
					BackendCharPCM: &recordingBackend{device: device},
				},
			)

			session, err := manager.Open(context.Background(), "call-1")
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() { _ = session.Close() })

			stateMu.Lock()
			currentState = "held"
			stateMu.Unlock()
			server, client := net.Pipe()
			t.Cleanup(func() { _ = client.Close() })
			result := make(chan error, 1)
			go func() {
				result <- session.Serve(context.Background(), server)
			}()
			if err := WriteFrame(client, Frame{Type: FrameStart}); err != nil {
				t.Fatalf("write START: %v", err)
			}
			started, err := ReadFrame(client, maxControlBytes)
			if err != nil {
				t.Fatalf("read STARTED: %v", err)
			}
			if started.Type != FrameStarted {
				t.Fatalf("start response = %+v", started)
			}
			if err := WriteFrame(client, Frame{Type: FrameClose}); err != nil {
				t.Fatalf("write CLOSE: %v", err)
			}
			if err := <-result; err != nil {
				t.Fatalf("Serve() error = %v", err)
			}
		})
	}

	for _, state := range []string{
		"unknown",
		"dialing",
		"ringing_out",
		"ringing_in",
		"waiting",
		"terminated",
	} {
		t.Run("reject_"+state, func(t *testing.T) {
			call := testCall("call-1", "audio-1", 8000)
			call.State = state
			manager := mustManager(
				t,
				fixedCallSource(call),
				[]Binding{{
					AudioPort: "audio-1",
					Backend:   BackendCharPCM,
					Endpoint:  "/dev/pcm0",
				}},
				map[BackendKind]Backend{
					BackendCharPCM: &recordingBackend{},
				},
			)
			if _, err := manager.Open(context.Background(), "call-1"); !IsCode(err, ErrorNotActive) {
				t.Fatalf("Open() error = %v, want not_active", err)
			}
		})
	}
}

func TestManagerSelectsOnlyTheExplicitBackendBinding(t *testing.T) {
	charDevice, charPeer := net.Pipe()
	alsaDevice, alsaPeer := net.Pipe()
	t.Cleanup(func() {
		_ = charPeer.Close()
		_ = alsaPeer.Close()
	})
	charBackend := &recordingBackend{device: charDevice}
	alsaBackend := &recordingBackend{device: alsaDevice}
	manager := mustManager(
		t,
		fixedCallSource(testCall("call-1", "modem-port-1", 16000)),
		[]Binding{{
			AudioPort: "modem-port-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/modem-pcm",
		}},
		map[BackendKind]Backend{
			BackendCharPCM: charBackend,
			BackendALSAPCM: alsaBackend,
		},
	)

	session, err := manager.Open(context.Background(), "call-1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if charBackend.opens != 1 ||
		charBackend.endpoint != "/dev/modem-pcm" ||
		charBackend.format.Rate != 16000 {
		t.Fatalf("char backend open = %+v", charBackend)
	}
	if alsaBackend.opens != 0 {
		t.Fatalf("ALSA fallback opened %d times", alsaBackend.opens)
	}
	if session.Backend() != BackendCharPCM {
		t.Fatalf("session backend = %q", session.Backend())
	}
}

func TestManagerLeasesCallsAndPhysicalEndpoints(t *testing.T) {
	var peersMu sync.Mutex
	var peers []net.Conn
	backend := &recordingBackend{
		open: func() Device {
			server, peer := net.Pipe()
			peersMu.Lock()
			peers = append(peers, peer)
			peersMu.Unlock()
			return server
		},
	}
	t.Cleanup(func() {
		peersMu.Lock()
		defer peersMu.Unlock()
		for _, peer := range peers {
			_ = peer.Close()
		}
	})
	source := CallSourceFunc(func(_ context.Context, callID string) (Call, error) {
		return testCall(callID, "shared-audio-port", 8000), nil
	})
	manager := mustManager(
		t,
		source,
		[]Binding{{
			AudioPort: "shared-audio-port",
			Backend:   BackendALSAPCM,
			Endpoint:  "hw:2,0",
		}},
		map[BackendKind]Backend{BackendALSAPCM: backend},
	)

	first, err := manager.Open(context.Background(), "call-1")
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if _, err := manager.Open(context.Background(), "call-1"); !IsCode(err, ErrorConflict) {
		t.Fatalf("second same-call Open() error = %v, want conflict", err)
	}
	if _, err := manager.Open(context.Background(), "call-2"); !IsCode(err, ErrorConflict) {
		t.Fatalf("shared endpoint Open() error = %v, want conflict", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	second, err := manager.Open(context.Background(), "call-2")
	if err != nil {
		t.Fatalf("Open() after release error = %v", err)
	}
	_ = second.Close()
}

func TestManagerReleasesLeaseWhenBackendOpenFails(t *testing.T) {
	backend := &recordingBackend{err: errors.New("open failed")}
	manager := mustManager(
		t,
		fixedCallSource(testCall("call-1", "audio-1", 8000)),
		[]Binding{{
			AudioPort: "audio-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[BackendKind]Backend{BackendCharPCM: backend},
	)

	if _, err := manager.Open(context.Background(), "call-1"); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("first Open() error = %v", err)
	}
	if _, err := manager.Open(context.Background(), "call-1"); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("second Open() error = %v, lease was not released", err)
	}
	if backend.opens != 2 {
		t.Fatalf("backend opens = %d, want 2", backend.opens)
	}
}

func TestManagerOpenTimeoutClosesLateDeviceAndRetainsLeaseUntilCleanup(t *testing.T) {
	device := newCloseSignalDevice()
	backend := newBlockingBackend(device)
	manager := mustManagerWithOptions(
		t,
		CallSourceFunc(func(_ context.Context, callID string) (Call, error) {
			return testCall(callID, "audio-1", 8000), nil
		}),
		backend,
		Options{
			OpenTimeout:         40 * time.Millisecond,
			StartTimeout:        time.Second,
			FrameIOTimeout:      time.Second,
			BackpressureTimeout: 100 * time.Millisecond,
			ShutdownTimeout:     time.Second,
			OutboundFrames:      2,
		},
	)

	result := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := manager.Open(context.Background(), "call-1")
		result <- err
	}()
	waitClosed(t, backend.entered, "backend entry")
	var openError error
	select {
	case openError = <-result:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Open() did not honor its timeout")
	}
	if !IsCode(openError, ErrorTimeout) {
		t.Fatalf("Open() error = %v, want timeout", openError)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Open() returned after %v, want bounded timeout", elapsed)
	}
	if _, err := manager.Open(context.Background(), "call-2"); !IsCode(err, ErrorConflict) {
		t.Fatalf("concurrent endpoint Open() error = %v, want conflict", err)
	}

	close(backend.release)
	waitClosed(t, device.closed, "late device")
	waitForManagerPendingOpens(t, manager, 0)
	manager.mu.Lock()
	sessionCount := len(manager.sessions)
	activeCallCount := len(manager.activeCalls)
	activeEndpointCount := len(manager.activeEndpoints)
	manager.mu.Unlock()
	if sessionCount != 0 {
		t.Fatalf("late open registered %d sessions", sessionCount)
	}
	if activeCallCount != 0 || activeEndpointCount != 0 {
		t.Fatalf(
			"late open retained %d calls and %d endpoints",
			activeCallCount,
			activeEndpointCount,
		)
	}
}

func TestManagerRequestCancellationReturnsBeforeLateBackendSuccess(t *testing.T) {
	device := newCloseSignalDevice()
	backend := newBlockingBackend(device)
	manager := mustManagerWithOptions(
		t,
		fixedCallSource(testCall("call-1", "audio-1", 8000)),
		backend,
		Options{
			OpenTimeout:         time.Second,
			StartTimeout:        time.Second,
			FrameIOTimeout:      time.Second,
			BackpressureTimeout: 100 * time.Millisecond,
			ShutdownTimeout:     time.Second,
			OutboundFrames:      2,
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Open(ctx, "call-1")
		result <- err
	}()
	waitClosed(t, backend.entered, "backend entry")

	cancel()
	select {
	case err := <-result:
		if !IsCode(err, ErrorBackendUnavailable) {
			t.Fatalf("Open() error = %v, want backend_unavailable cancellation", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Open() did not return after request cancellation")
	}

	close(backend.release)
	waitClosed(t, device.closed, "late device")
	waitForManagerPendingOpens(t, manager, 0)
}

func TestManagerCloseCancelsConcurrentPendingOpenWithoutWaitingForOpener(t *testing.T) {
	device := newCloseSignalDevice()
	backend := newBlockingBackend(device)
	manager := mustManagerWithOptions(
		t,
		fixedCallSource(testCall("call-1", "audio-1", 8000)),
		backend,
		Options{
			OpenTimeout:         5 * time.Second,
			StartTimeout:        time.Second,
			FrameIOTimeout:      time.Second,
			BackpressureTimeout: 100 * time.Millisecond,
			ShutdownTimeout:     time.Second,
			OutboundFrames:      2,
		},
	)
	openResult := make(chan error, 1)
	go func() {
		_, err := manager.Open(context.Background(), "call-1")
		openResult <- err
	}()
	waitClosed(t, backend.entered, "backend entry")

	started := time.Now()
	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Close() returned after %v, want bounded close", elapsed)
	}
	select {
	case err := <-openResult:
		if err == nil {
			t.Fatal("Open() succeeded after Manager.Close")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Open() did not return after Manager.Close")
	}

	close(backend.release)
	waitClosed(t, device.closed, "late device")
	waitForManagerPendingOpens(t, manager, 0)
	if manager.IsActive("call-1") {
		t.Fatal("late success became an active session")
	}
}

func TestManagerRejectsEmptyBackendDeviceAndClosesPartialFailure(t *testing.T) {
	manager := mustManager(
		t,
		fixedCallSource(testCall("call-1", "audio-1", 8000)),
		[]Binding{{
			AudioPort: "audio-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[BackendKind]Backend{BackendCharPCM: &recordingBackend{}},
	)
	if _, err := manager.Open(context.Background(), "call-1"); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("nil-device Open() error = %v", err)
	}

	device := &closeTrackingDevice{}
	manager = mustManager(
		t,
		fixedCallSource(testCall("call-1", "audio-1", 8000)),
		[]Binding{{
			AudioPort: "audio-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[BackendKind]Backend{BackendCharPCM: &recordingBackend{
			device: device,
			err:    errors.New("partial open"),
		}},
	)
	if _, err := manager.Open(context.Background(), "call-1"); !IsCode(err, ErrorBackendUnavailable) {
		t.Fatalf("partial Open() error = %v", err)
	}
	if !device.closed {
		t.Fatal("partially opened backend device was not closed")
	}
}

func TestManagerConfigurationIsExplicitAndBounded(t *testing.T) {
	source := fixedCallSource(testCall("call-1", "audio-1", 8000))
	backend := &recordingBackend{}
	tests := []struct {
		name     string
		bindings []Binding
		backends map[BackendKind]Backend
		options  Options
		code     ErrorCode
	}{
		{
			name:     "source required",
			backends: map[BackendKind]Backend{BackendCharPCM: backend},
			code:     ErrorInvalidArgument,
		},
		{
			name: "backend required",
			bindings: []Binding{{
				AudioPort: "audio-1",
				Backend:   BackendCharPCM,
				Endpoint:  "/dev/pcm0",
			}},
			code: ErrorBackendUnavailable,
		},
		{
			name: "duplicate port rejected",
			bindings: []Binding{
				{AudioPort: "audio-1", Backend: BackendCharPCM, Endpoint: "/dev/pcm0"},
				{AudioPort: "audio-1", Backend: BackendCharPCM, Endpoint: "/dev/pcm1"},
			},
			backends: map[BackendKind]Backend{BackendCharPCM: backend},
			code:     ErrorInvalidArgument,
		},
		{
			name:     "negative options rejected",
			backends: map[BackendKind]Backend{BackendCharPCM: backend},
			options:  Options{FrameIOTimeout: -time.Second},
			code:     ErrorInvalidArgument,
		},
		{
			name:     "negative start timeout rejected",
			backends: map[BackendKind]Backend{BackendCharPCM: backend},
			options:  Options{StartTimeout: -time.Second},
			code:     ErrorInvalidArgument,
		},
		{
			name:     "large queue rejected",
			backends: map[BackendKind]Backend{BackendCharPCM: backend},
			options:  Options{OutboundFrames: 65},
			code:     ErrorInvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testSource := CallSource(source)
			if test.name == "source required" {
				testSource = nil
			}
			_, err := NewManager(testSource, test.bindings, test.backends, test.options)
			if !IsCode(err, test.code) {
				t.Fatalf("NewManager() error = %v, want %s", err, test.code)
			}
		})
	}
}

type recordingBackend struct {
	mu       sync.Mutex
	opens    int
	endpoint string
	format   Format
	device   Device
	open     func() Device
	err      error
}

type closeTrackingDevice struct {
	closed bool
}

type closeSignalDevice struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newCloseSignalDevice() *closeSignalDevice {
	return &closeSignalDevice{closed: make(chan struct{})}
}

func (*closeSignalDevice) Read([]byte) (int, error) {
	return 0, errors.New("not implemented")
}

func (*closeSignalDevice) Write([]byte) (int, error) {
	return 0, errors.New("not implemented")
}

func (d *closeSignalDevice) Close() error {
	d.closeOnce.Do(func() {
		close(d.closed)
	})
	return nil
}

func (*closeSignalDevice) SetReadDeadline(time.Time) error {
	return nil
}

func (*closeSignalDevice) SetWriteDeadline(time.Time) error {
	return nil
}

type blockingBackend struct {
	device      Device
	entered     chan struct{}
	enteredOnce sync.Once
	release     chan struct{}
}

func newBlockingBackend(device Device) *blockingBackend {
	return &blockingBackend{
		device:  device,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingBackend) Open(context.Context, string, Format) (Device, error) {
	b.enteredOnce.Do(func() {
		close(b.entered)
	})
	<-b.release
	return b.device, nil
}

func (*closeTrackingDevice) Read([]byte) (int, error) {
	return 0, errors.New("not implemented")
}

func (*closeTrackingDevice) Write([]byte) (int, error) {
	return 0, errors.New("not implemented")
}

func (d *closeTrackingDevice) Close() error {
	d.closed = true
	return nil
}

func (*closeTrackingDevice) SetReadDeadline(time.Time) error {
	return nil
}

func (*closeTrackingDevice) SetWriteDeadline(time.Time) error {
	return nil
}

func (b *recordingBackend) Open(_ context.Context, endpoint string, format Format) (Device, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opens++
	b.endpoint = endpoint
	b.format = format
	if b.err != nil {
		return b.device, b.err
	}
	if b.open != nil {
		return b.open(), nil
	}
	return b.device, nil
}

func fixedCallSource(call Call) CallSource {
	return CallSourceFunc(func(context.Context, string) (Call, error) {
		return call, nil
	})
}

func testCall(id, audioPort string, rate uint32) Call {
	return Call{
		ID:             id,
		State:          "active",
		AudioPort:      audioPort,
		MediaAvailable: true,
		AudioFormat: &AdvertisedFormat{
			Encoding:   EncodingPCM,
			Resolution: ResolutionS16LE,
			Rate:       rate,
		},
	}
}

func mustManager(
	t *testing.T,
	source CallSource,
	bindings []Binding,
	backends map[BackendKind]Backend,
) *Manager {
	t.Helper()
	manager, err := NewManager(source, bindings, backends, Options{
		OpenTimeout:         time.Second,
		StartTimeout:        time.Second,
		FrameIOTimeout:      time.Second,
		BackpressureTimeout: 100 * time.Millisecond,
		ShutdownTimeout:     time.Second,
		OutboundFrames:      2,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func TestManagerConfiguredRequiresAtLeastOneValidatedBinding(t *testing.T) {
	t.Parallel()

	var absent *Manager
	if absent.Configured() {
		t.Fatal("nil manager reported configured media")
	}

	empty := mustManager(
		t,
		fixedCallSource(Call{}),
		nil,
		map[BackendKind]Backend{},
	)
	if empty.Configured() {
		t.Fatal("manager without bindings reported configured media")
	}

	configured := mustManager(
		t,
		fixedCallSource(Call{}),
		[]Binding{{
			AudioPort: "audio-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[BackendKind]Backend{
			BackendCharPCM: NewCharPCMBackend(nil),
		},
	)
	if !configured.Configured() {
		t.Fatal("manager with a validated binding did not report configured media")
	}
}

func mustManagerWithOptions(
	t *testing.T,
	source CallSource,
	backend Backend,
	options Options,
) *Manager {
	t.Helper()
	manager, err := NewManager(
		source,
		[]Binding{{
			AudioPort: "audio-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[BackendKind]Backend{BackendCharPCM: backend},
		options,
	)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func waitClosed(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("%s did not close", description)
	}
}

func waitForManagerPendingOpens(t *testing.T, manager *Manager, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.Lock()
		pending := len(manager.pendingOpens)
		manager.mu.Unlock()
		if pending == count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending opens = %d, want %d", pending, count)
		}
		time.Sleep(time.Millisecond)
	}
}
