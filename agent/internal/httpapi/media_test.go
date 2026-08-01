package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/media"
)

func TestMediaHandlerRequiresVersionedUpgradeBeforeOpeningBackend(t *testing.T) {
	backend := &mediaTestBackend{}
	manager := newMediaTestManager(t, "active", true, backend)
	handler := NewMediaHandler(manager, MediaHandlerOptions{})

	request := httptest.NewRequest(http.MethodGet, "/v1/media/calls/call-1", nil)
	recorder := performRequestWithHandler(handler, request)
	if recorder.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUpgradeRequired)
	}
	if recorder.Header().Get("Upgrade") != media.UpgradeProtocol {
		t.Fatalf("Upgrade = %q", recorder.Header().Get("Upgrade"))
	}
	var response errorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != domain.ErrorInvalidArgument {
		t.Fatalf("error code = %q", response.Error.Code)
	}
	if backend.openCount() != 0 {
		t.Fatalf("backend opened %d times before upgrade validation", backend.openCount())
	}
}

func TestMediaHandlerRejectsNonActiveCallBeforeHijack(t *testing.T) {
	backend := &mediaTestBackend{}
	manager := newMediaTestManager(t, "ringing_out", true, backend)
	handler := NewMediaHandler(manager, MediaHandlerOptions{})

	request := httptest.NewRequest(http.MethodGet, "/v1/media/calls/call-1", nil)
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", media.UpgradeProtocol)
	recorder := performRequestWithHandler(handler, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if backend.openCount() != 0 {
		t.Fatalf("backend opened %d times for a non-active call", backend.openCount())
	}
}

func TestMediaHandlerStreamsFullDuplexOverUnixSocket(t *testing.T) {
	device, devicePeer := net.Pipe()
	t.Cleanup(func() { _ = devicePeer.Close() })
	backend := &mediaTestBackend{nextDevice: device}
	manager := newMediaTestManager(t, "active", true, backend)
	handler := NewMediaHandler(manager, MediaHandlerOptions{
		HandshakeTimeout: time.Second,
	})

	socketPath := filepath.Join(t.TempDir(), "media.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := &http.Server{Handler: handler}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		select {
		case err := <-serveResult:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("Serve() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("HTTP server did not stop")
		}
	})

	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	playback := bytes.Repeat([]byte{0x12, 0x34}, 160)
	var request bytes.Buffer
	if _, err := fmt.Fprintf(
		&request,
		"GET /v1/media/calls/call-1 HTTP/1.1\r\n"+
			"Host: modemdeck\r\n"+
			"Connection: Upgrade\r\n"+
			"Upgrade: %s\r\n\r\n",
		media.UpgradeProtocol,
	); err != nil {
		t.Fatalf("build upgrade request: %v", err)
	}
	if _, err := connection.Write(request.Bytes()); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}

	reader := bufio.NewReader(connection)
	status, headers := readUpgradeResponse(t, reader)
	if status != "HTTP/1.1 101 Switching Protocols" {
		t.Fatalf("status = %q", status)
	}
	if headers.Get("Upgrade") != media.UpgradeProtocol ||
		headers.Get("ModemDeck-Media-Version") != "2" ||
		headers.Get("ModemDeck-Media-Encoding") != media.EncodingPCM ||
		headers.Get("ModemDeck-Media-Resolution") != media.ResolutionS16LE ||
		headers.Get("ModemDeck-Media-Rate") != "8000" ||
		headers.Get("ModemDeck-Media-Frame-Duration-Ms") != "20" ||
		headers.Get("ModemDeck-Media-Frame-Bytes") != "320" {
		t.Fatalf("unexpected media headers: %#v", headers)
	}
	if err := media.WriteFrame(connection, media.Frame{Type: media.FrameStart}); err != nil {
		t.Fatalf("write START: %v", err)
	}
	started, err := media.ReadFrame(reader, 64)
	if err != nil {
		t.Fatalf("read STARTED: %v", err)
	}
	if started.Type != media.FrameStarted || started.Sequence != 0 || len(started.Payload) != 0 {
		t.Fatalf("STARTED = %+v", started)
	}
	if err := media.WriteFrame(connection, media.Frame{
		Type:    media.FramePlayback,
		Payload: playback,
	}); err != nil {
		t.Fatalf("write playback frame: %v", err)
	}

	gotPlayback := make([]byte, len(playback))
	if _, err := io.ReadFull(devicePeer, gotPlayback); err != nil {
		t.Fatalf("read playback device: %v", err)
	}
	if !bytes.Equal(gotPlayback, playback) {
		t.Fatal("playback payload changed across Unix media protocol")
	}

	capture := bytes.Repeat([]byte{0x56, 0x78}, 160)
	if _, err := devicePeer.Write(capture); err != nil {
		t.Fatalf("write capture device: %v", err)
	}
	gotCapture, err := media.ReadFrame(reader, len(capture))
	if err != nil {
		t.Fatalf("read capture frame: %v", err)
	}
	if gotCapture.Type != media.FrameCapture ||
		gotCapture.Sequence != 0 ||
		!bytes.Equal(gotCapture.Payload, capture) {
		t.Fatalf("capture frame = %+v", gotCapture)
	}

	if err := media.WriteFrame(connection, media.Frame{Type: media.FrameClose}); err != nil {
		t.Fatalf("write close frame: %v", err)
	}
}

func TestDecorateMediaSnapshotKeepsAvailabilityConfigurationAndSessionDistinct(t *testing.T) {
	device, devicePeer := net.Pipe()
	t.Cleanup(func() { _ = devicePeer.Close() })
	manager := newMediaTestManager(
		t,
		"active",
		true,
		&mediaTestBackend{nextDevice: device},
	)
	snapshot := domain.Snapshot{
		Lines: []domain.Line{
			{
				ID:        "line-configured",
				AudioPort: "hw:2,0",
				Capabilities: domain.LineCapabilities{
					Media: true,
				},
			},
			{
				ID:        "line-unbound",
				AudioPort: "hw:9,9",
				Capabilities: domain.LineCapabilities{
					Media: true,
				},
			},
			{
				ID:        "line-hardware-unavailable",
				AudioPort: "hw:2,0",
			},
		},
		Calls: []domain.Call{
			{
				ID:             "call-1",
				AudioPort:      "hw:2,0",
				MediaAvailable: true,
			},
			{
				ID:             "call-unbound",
				AudioPort:      "hw:9,9",
				MediaAvailable: true,
			},
			{
				ID:             "call-metadata-unavailable",
				AudioPort:      "hw:2,0",
				MediaAvailable: false,
			},
		},
	}
	decorateMediaSnapshot(&snapshot, manager)
	if !snapshot.Lines[0].Capabilities.Media {
		t.Fatalf("configured line = %+v", snapshot.Lines[0])
	}
	if snapshot.Lines[1].Capabilities.Media {
		t.Fatalf("unbound line = %+v", snapshot.Lines[1])
	}
	if snapshot.Lines[2].Capabilities.Media {
		t.Fatalf("hardware-unavailable line = %+v", snapshot.Lines[2])
	}
	if !snapshot.Calls[0].MediaAvailable ||
		!snapshot.Calls[0].MediaConfigured ||
		snapshot.Calls[0].MediaActive {
		t.Fatalf("configured idle call = %+v", snapshot.Calls[0])
	}
	if !snapshot.Calls[1].MediaAvailable ||
		snapshot.Calls[1].MediaConfigured ||
		snapshot.Calls[1].MediaActive {
		t.Fatalf("unbound call = %+v", snapshot.Calls[1])
	}
	if snapshot.Calls[2].MediaAvailable ||
		!snapshot.Calls[2].MediaConfigured ||
		snapshot.Calls[2].MediaActive {
		t.Fatalf("raw unavailable call = %+v", snapshot.Calls[2])
	}

	session, err := manager.Open(context.Background(), "call-1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = session.Close()
	})
	result := make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), server)
	}()
	if err := media.WriteFrame(client, media.Frame{Type: media.FrameStart}); err != nil {
		t.Fatalf("write START: %v", err)
	}
	started, err := media.ReadFrame(client, 64)
	if err != nil || started.Type != media.FrameStarted {
		t.Fatalf("STARTED = %+v, %v", started, err)
	}
	decorateMediaSnapshot(&snapshot, manager)
	if !snapshot.Calls[0].MediaActive {
		t.Fatalf("started call = %+v", snapshot.Calls[0])
	}
	if err := media.WriteFrame(client, media.Frame{Type: media.FrameClose}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	decorateMediaSnapshot(&snapshot, manager)
	if snapshot.Calls[0].MediaActive {
		t.Fatalf("closed call = %+v", snapshot.Calls[0])
	}
}

func readUpgradeResponse(t *testing.T, reader *bufio.Reader) (string, http.Header) {
	t.Helper()
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	headers := make(http.Header)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read response headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
		name, value, ok := strings.Cut(strings.TrimSuffix(line, "\r\n"), ":")
		if !ok {
			t.Fatalf("invalid response header: %q", line)
		}
		headers.Add(http.CanonicalHeaderKey(strings.TrimSpace(name)), strings.TrimSpace(value))
	}
	return strings.TrimSuffix(statusLine, "\r\n"), headers
}

type mediaTestBackend struct {
	mu         sync.Mutex
	opens      int
	nextDevice media.Device
}

func (b *mediaTestBackend) Open(
	_ context.Context,
	_ string,
	_ media.Format,
) (media.Device, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opens++
	return b.nextDevice, nil
}

func (b *mediaTestBackend) openCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opens
}

func newMediaTestManager(
	t *testing.T,
	state string,
	available bool,
	backend media.Backend,
) *media.Manager {
	t.Helper()
	manager, err := media.NewManager(
		media.CallSourceFunc(func(context.Context, string) (media.Call, error) {
			return media.Call{
				ID:             "call-1",
				State:          state,
				AudioPort:      "hw:2,0",
				MediaAvailable: available,
				AudioFormat: &media.AdvertisedFormat{
					Encoding:   media.EncodingPCM,
					Resolution: media.ResolutionS16LE,
					Rate:       8000,
				},
			}, nil
		}),
		[]media.Binding{{
			AudioPort: "hw:2,0",
			Backend:   media.BackendALSAPCM,
			Endpoint:  "hw:2,0",
		}},
		map[media.BackendKind]media.Backend{
			media.BackendALSAPCM: backend,
		},
		media.Options{
			OpenTimeout:         time.Second,
			FrameIOTimeout:      time.Second,
			BackpressureTimeout: 100 * time.Millisecond,
			ShutdownTimeout:     time.Second,
			OutboundFrames:      2,
		},
	)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func performRequestWithHandler(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
