package agentmedia

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestOpenerStreamsNegotiatedFullDuplexPCM(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	playback := make(chan frame, 1)
	serverResult := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverResult <- err
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		request, err := http.ReadRequest(reader)
		if err != nil {
			serverResult <- err
			return
		}
		if request.URL.Path != "/v1/media/calls/endpoint-call-1" ||
			request.Header.Get("Upgrade") != upgradeProtocol {
			serverResult <- fmt.Errorf("unexpected request: %s %#v", request.URL.Path, request.Header)
			return
		}
		if _, err := fmt.Fprintf(
			connection,
			"HTTP/1.1 101 Switching Protocols\r\n"+
				"Connection: Upgrade\r\n"+
				"Upgrade: %s\r\n"+
				"ModemDeck-Media-Version: 2\r\n"+
				"ModemDeck-Media-Encoding: pcm\r\n"+
				"ModemDeck-Media-Resolution: s16le\r\n"+
				"ModemDeck-Media-Rate: 8000\r\n"+
				"ModemDeck-Media-Channels: 1\r\n"+
				"ModemDeck-Media-Frame-Duration-Ms: 20\r\n"+
				"ModemDeck-Media-Frame-Bytes: 320\r\n\r\n",
			upgradeProtocol,
		); err != nil {
			serverResult <- err
			return
		}
		start, err := readFrame(reader, maxControlBytes)
		if err != nil {
			serverResult <- err
			return
		}
		if start.Type != frameStart || start.Sequence != 0 || len(start.Payload) != 0 {
			serverResult <- fmt.Errorf("unexpected START frame: %+v", start)
			return
		}
		if err := writeFrame(connection, frame{Type: frameStarted}); err != nil {
			serverResult <- err
			return
		}
		value, err := readFrame(reader, 320)
		if err != nil {
			serverResult <- err
			return
		}
		playback <- value
		capture := bytes.Repeat([]byte{0x56, 0x78}, 160)
		if err := writeFrame(connection, frame{
			Type:     frameCapture,
			Sequence: 0,
			Payload:  capture,
		}); err != nil {
			serverResult <- err
			return
		}
		serverResult <- nil
	}()

	opener, err := New(socketPath, fixedCallStore{call: activeMediaCall()}, Options{
		OpenTimeout: time.Second,
		IOTimeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	endpoint, err := opener.Open(context.Background(), callmedia.ActiveCall{
		ID:    "call-1",
		State: callmedia.CallStateActive,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer endpoint.Close()
	if endpoint.Format().SampleRate != 8000 || endpoint.Format().FrameBytes() != 320 {
		t.Fatalf("format = %+v", endpoint.Format())
	}
	if err := endpoint.WritePCM(
		context.Background(),
		make([]byte, endpoint.Format().FrameBytes()),
	); !IsCode(err, ErrorProtocol) {
		t.Fatalf("WritePCM() before Start error = %v, want protocol error", err)
	}
	if err := endpoint.ReadPCM(
		context.Background(),
		make([]byte, endpoint.Format().FrameBytes()),
	); !IsCode(err, ErrorProtocol) {
		t.Fatalf("ReadPCM() before Start error = %v, want protocol error", err)
	}
	if err := endpoint.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := endpoint.Start(context.Background()); !IsCode(err, ErrorProtocol) {
		t.Fatalf("second Start() error = %v, want protocol error", err)
	}

	sent := bytes.Repeat([]byte{0x12, 0x34}, 160)
	if err := endpoint.WritePCM(context.Background(), sent); err != nil {
		t.Fatalf("WritePCM() error = %v", err)
	}
	got := <-playback
	if got.Type != framePlayback || got.Sequence != 0 || !bytes.Equal(got.Payload, sent) {
		t.Fatalf("playback frame = %+v", got)
	}
	received := make([]byte, 320)
	if err := endpoint.ReadPCM(context.Background(), received); err != nil {
		t.Fatalf("ReadPCM() error = %v", err)
	}
	if !bytes.Equal(received, bytes.Repeat([]byte{0x56, 0x78}, 160)) {
		t.Fatal("capture payload changed")
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestOpenerRejectsNonActiveCallBeforeDial(t *testing.T) {
	t.Parallel()

	call := activeMediaCall()
	call.Phase = "ringing"
	opener, err := New(
		filepath.Join(t.TempDir(), "missing.sock"),
		fixedCallStore{call: call},
		Options{},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = opener.Open(context.Background(), callmedia.ActiveCall{
		ID:    call.ID,
		State: callmedia.CallStateActive,
	})
	if err != callmedia.ErrCallNotActive {
		t.Fatalf("Open() error = %v, want ErrCallNotActive", err)
	}
}

func TestEndpointStartTimeoutAndHostErrorAreTyped(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		endpoint := newProtocolTestEndpoint(client, 25*time.Millisecond)
		serverResult := make(chan error, 1)
		go func() {
			defer server.Close()
			start, err := readFrame(server, maxControlBytes)
			if err == nil && start.Type != frameStart {
				err = fmt.Errorf("frame type = %d", start.Type)
			}
			time.Sleep(50 * time.Millisecond)
			serverResult <- err
		}()

		if err := endpoint.Start(context.Background()); !IsCode(err, ErrorTimeout) {
			t.Fatalf("Start() error = %v, want timeout", err)
		}
		if err := <-serverResult; err != nil {
			t.Fatalf("server error = %v", err)
		}
	})

	t.Run("host backend failure", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		endpoint := newProtocolTestEndpoint(client, time.Second)
		serverResult := make(chan error, 1)
		go func() {
			defer server.Close()
			start, err := readFrame(server, maxControlBytes)
			if err == nil && start.Type != frameStart {
				err = fmt.Errorf("frame type = %d", start.Type)
			}
			if err == nil {
				err = writeFrame(server, frame{
					Type:    frameError,
					Payload: []byte(ErrorBackendUnavailable),
				})
			}
			serverResult <- err
		}()

		if err := endpoint.Start(context.Background()); !IsCode(err, ErrorBackendUnavailable) {
			t.Fatalf("Start() error = %v, want backend unavailable", err)
		}
		if err := <-serverResult; err != nil {
			t.Fatalf("server error = %v", err)
		}
	})
}

func newProtocolTestEndpoint(connection net.Conn, startTimeout time.Duration) *endpoint {
	return &endpoint{
		connection: connection,
		reader:     bufio.NewReader(connection),
		format: callmedia.PCMFormat{
			Encoding:      callmedia.PCMEncodingS16LE,
			SampleRate:    8000,
			Channels:      1,
			FrameDuration: 20 * time.Millisecond,
		},
		frameBytes:   320,
		startTimeout: startTimeout,
		ioTimeout:    time.Second,
	}
}

type fixedCallStore struct {
	call store.Call
	err  error
}

func (s fixedCallStore) CallByID(context.Context, string) (store.Call, error) {
	return s.call, s.err
}

func activeMediaCall() store.Call {
	return store.Call{
		ID:              "call-1",
		EndpointCallID:  "endpoint-call-1",
		Phase:           "active",
		AudioPort:       "hw:2,0",
		AudioEncoding:   "pcm",
		AudioResolution: "s16le",
		AudioRate:       8000,
		MediaAvailable:  true,
	}
}
