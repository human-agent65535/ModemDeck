package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestFrameCodecRoundTripAndBounds(t *testing.T) {
	want := Frame{
		Type:     FramePlayback,
		Sequence: 42,
		Payload:  []byte{1, 2, 3, 4},
	}
	var encoded bytes.Buffer
	if err := WriteFrame(&encoded, want); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
	got, err := ReadFrame(&encoded, len(want.Payload))
	if err != nil {
		t.Fatalf("ReadFrame() error = %v", err)
	}
	if got.Type != want.Type ||
		got.Sequence != want.Sequence ||
		!bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("ReadFrame() = %+v, want %+v", got, want)
	}

	oversized := make([]byte, FrameHeaderBytes)
	copy(oversized[:4], frameMagic[:])
	binary.BigEndian.PutUint32(oversized[12:16], 641)
	if _, err := ReadFrame(bytes.NewReader(oversized), 640); !IsCode(err, ErrorProtocol) {
		t.Fatalf("oversized ReadFrame() error = %v", err)
	}

	reserved := make([]byte, FrameHeaderBytes)
	copy(reserved[:4], frameMagic[:])
	reserved[5] = 1
	if _, err := ReadFrame(bytes.NewReader(reserved), 0); !IsCode(err, ErrorProtocol) {
		t.Fatalf("reserved-bit ReadFrame() error = %v", err)
	}
}

func TestSessionBridgesFullDuplexFixedFrames(t *testing.T) {
	tests := []struct {
		name string
		rate uint32
	}{
		{name: "8khz", rate: 8000},
		{name: "16khz", rate: 16000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testSessionBridgesFullDuplexFixedFrames(t, test.rate)
		})
	}
}

func testSessionBridgesFullDuplexFixedFrames(t *testing.T, rate uint32) {
	t.Helper()
	session, mediaPeer := openProtocolSession(t, rate)
	serverConnection, clientConnection := net.Pipe()
	t.Cleanup(func() {
		_ = mediaPeer.Close()
		_ = clientConnection.Close()
	})

	result := make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), serverConnection)
	}()

	format := session.Format()
	startProtocolSession(t, clientConnection)
	playback := bytes.Repeat([]byte{0x31, 0x72}, format.FrameBytes/2)
	if err := WriteFrame(clientConnection, Frame{
		Type:     FramePlayback,
		Sequence: 0,
		Payload:  playback,
	}); err != nil {
		t.Fatalf("write playback frame: %v", err)
	}
	_ = mediaPeer.SetReadDeadline(time.Now().Add(time.Second))
	gotPlayback := make([]byte, format.FrameBytes)
	if _, err := io.ReadFull(mediaPeer, gotPlayback); err != nil {
		t.Fatalf("read playback PCM: %v", err)
	}
	if !bytes.Equal(gotPlayback, playback) {
		t.Fatal("playback PCM differs from the client frame")
	}

	capture := bytes.Repeat([]byte{0x55, 0xaa}, format.FrameBytes/2)
	if err := writeWithDeadline(mediaPeer, capture); err != nil {
		t.Fatalf("write capture PCM: %v", err)
	}
	_ = clientConnection.SetReadDeadline(time.Now().Add(time.Second))
	gotCapture, err := ReadFrame(clientConnection, format.FrameBytes)
	if err != nil {
		t.Fatalf("read capture frame: %v", err)
	}
	if gotCapture.Type != FrameCapture ||
		gotCapture.Sequence != 0 ||
		!bytes.Equal(gotCapture.Payload, capture) {
		t.Fatalf("capture frame = %+v", gotCapture)
	}

	if err := WriteFrame(clientConnection, Frame{
		Type:     FramePing,
		Sequence: 9,
		Payload:  []byte("health"),
	}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	pong, err := ReadFrame(clientConnection, maxControlBytes)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.Type != FramePong || pong.Sequence != 9 || string(pong.Payload) != "health" {
		t.Fatalf("pong = %+v", pong)
	}

	if err := WriteFrame(clientConnection, Frame{Type: FrameClose}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not stop after close frame")
	}
}

func TestSessionRejectsWrongFrameSizeAndSequence(t *testing.T) {
	tests := []struct {
		name  string
		frame func(int) Frame
	}{
		{
			name: "wrong duration",
			frame: func(frameBytes int) Frame {
				return Frame{Type: FramePlayback, Payload: make([]byte, frameBytes-2)}
			},
		},
		{
			name: "noncontiguous sequence",
			frame: func(frameBytes int) Frame {
				return Frame{Type: FramePlayback, Sequence: 1, Payload: make([]byte, frameBytes)}
			},
		},
		{
			name: "server-only frame type",
			frame: func(frameBytes int) Frame {
				return Frame{Type: FrameCapture, Payload: make([]byte, frameBytes)}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, mediaPeer := openProtocolSession(t, 8000)
			serverConnection, clientConnection := net.Pipe()
			defer mediaPeer.Close()
			defer clientConnection.Close()
			result := make(chan error, 1)
			go func() {
				result <- session.Serve(context.Background(), serverConnection)
			}()

			startProtocolSession(t, clientConnection)
			if err := WriteFrame(clientConnection, test.frame(session.Format().FrameBytes)); err != nil {
				t.Fatalf("WriteFrame() error = %v", err)
			}
			select {
			case err := <-result:
				if !IsCode(err, ErrorProtocol) {
					t.Fatalf("Serve() error = %v, want protocol error", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Serve() did not reject malformed client frame")
			}
		})
	}
}

func TestSessionRequiresStartBeforeAnyDeviceIO(t *testing.T) {
	session, mediaPeer := openProtocolSession(t, 8000)
	serverConnection, clientConnection := net.Pipe()
	defer mediaPeer.Close()
	defer clientConnection.Close()
	result := make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), serverConnection)
	}()

	_ = mediaPeer.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, err := mediaPeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("device produced I/O before START")
	}
	if session.Started() {
		t.Fatal("session reported active before START")
	}

	startProtocolSession(t, clientConnection)
	if !session.Started() {
		t.Fatal("session did not report active after STARTED")
	}
	if err := WriteFrame(clientConnection, Frame{Type: FrameClose}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestSessionRejectsOutOfOrderAndDuplicateStart(t *testing.T) {
	t.Run("playback before start", func(t *testing.T) {
		session, mediaPeer := openProtocolSession(t, 8000)
		serverConnection, clientConnection := net.Pipe()
		defer mediaPeer.Close()
		defer clientConnection.Close()
		result := make(chan error, 1)
		go func() {
			result <- session.Serve(context.Background(), serverConnection)
		}()
		if err := WriteFrame(clientConnection, Frame{Type: FramePlayback}); err != nil {
			t.Fatalf("write playback: %v", err)
		}
		response, err := ReadFrame(clientConnection, maxControlBytes)
		if err != nil {
			t.Fatalf("read start rejection: %v", err)
		}
		if response.Type != FrameError || string(response.Payload) != string(ErrorProtocol) {
			t.Fatalf("start rejection = %+v", response)
		}
		if err := <-result; !IsCode(err, ErrorProtocol) {
			t.Fatalf("Serve() error = %v, want protocol error", err)
		}
	})

	t.Run("duplicate start", func(t *testing.T) {
		session, mediaPeer := openProtocolSession(t, 8000)
		serverConnection, clientConnection := net.Pipe()
		defer mediaPeer.Close()
		defer clientConnection.Close()
		result := make(chan error, 1)
		go func() {
			result <- session.Serve(context.Background(), serverConnection)
		}()
		startProtocolSession(t, clientConnection)
		if err := WriteFrame(clientConnection, Frame{Type: FrameStart}); err != nil {
			t.Fatalf("write duplicate START: %v", err)
		}
		if err := <-result; !IsCode(err, ErrorProtocol) {
			t.Fatalf("Serve() error = %v, want protocol error", err)
		}
	})
}

func TestSessionRevalidatesAuthoritativeCallOnStart(t *testing.T) {
	var mu sync.Mutex
	call := testCall("call-1", "audio-1", 8000)
	device, mediaPeer := net.Pipe()
	defer mediaPeer.Close()
	manager := mustManager(
		t,
		CallSourceFunc(func(context.Context, string) (Call, error) {
			mu.Lock()
			defer mu.Unlock()
			return call, nil
		}),
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
	mu.Lock()
	call.State = "terminated"
	mu.Unlock()

	server, client := net.Pipe()
	defer client.Close()
	result := make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), server)
	}()
	if err := WriteFrame(client, Frame{Type: FrameStart}); err != nil {
		t.Fatalf("write START: %v", err)
	}
	rejection, err := ReadFrame(client, maxControlBytes)
	if err != nil {
		t.Fatalf("read rejection: %v", err)
	}
	if rejection.Type != FrameError || string(rejection.Payload) != string(ErrorNotActive) {
		t.Fatalf("START rejection = %+v", rejection)
	}
	if err := <-result; !IsCode(err, ErrorNotActive) {
		t.Fatalf("Serve() error = %v, want not active", err)
	}
}

func TestSessionStartDeadlineAndCloseAreBounded(t *testing.T) {
	session, mediaPeer := openProtocolSession(t, 8000)
	session.options.StartTimeout = 25 * time.Millisecond
	serverConnection, clientConnection := net.Pipe()
	defer mediaPeer.Close()
	defer clientConnection.Close()
	result := make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), serverConnection)
	}()
	select {
	case err := <-result:
		if !IsCode(err, ErrorTimeout) {
			t.Fatalf("Serve() error = %v, want timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not enforce the START deadline")
	}

	session, mediaPeer = openProtocolSession(t, 8000)
	serverConnection, clientConnection = net.Pipe()
	defer mediaPeer.Close()
	defer clientConnection.Close()
	result = make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), serverConnection)
	}()
	deadline := time.Now().Add(time.Second)
	for !session.running.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("Close() did not unblock a session waiting for START")
	}
}

func TestSessionBackpressureIsBounded(t *testing.T) {
	session := &Session{options: Options{BackpressureTimeout: 10 * time.Millisecond}}
	outbound := make(chan Frame, 1)
	outbound <- Frame{Type: FrameCapture}

	err := session.enqueue(context.Background(), outbound, Frame{Type: FrameCapture})
	if !IsCode(err, ErrorBackpressure) {
		t.Fatalf("enqueue() error = %v, want backpressure", err)
	}
}

func TestSessionCannotRunTwice(t *testing.T) {
	session, mediaPeer := openProtocolSession(t, 8000)
	serverConnection, clientConnection := net.Pipe()
	defer mediaPeer.Close()
	defer clientConnection.Close()
	result := make(chan error, 1)
	go func() {
		result <- session.Serve(context.Background(), serverConnection)
	}()

	deadline := time.Now().Add(time.Second)
	for !session.running.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	otherServer, otherClient := net.Pipe()
	defer otherServer.Close()
	defer otherClient.Close()
	if err := session.Serve(context.Background(), otherServer); !IsCode(err, ErrorConflict) {
		t.Fatalf("second Serve() error = %v, want conflict", err)
	}

	if err := WriteFrame(clientConnection, Frame{Type: FrameClose}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("first Serve() error = %v", err)
	}
}

func openProtocolSession(t *testing.T, rate uint32) (*Session, net.Conn) {
	t.Helper()
	device, peer := net.Pipe()
	backend := &recordingBackend{device: device}
	manager := mustManager(
		t,
		fixedCallSource(testCall("call-1", "audio-1", rate)),
		[]Binding{{
			AudioPort: "audio-1",
			Backend:   BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[BackendKind]Backend{BackendCharPCM: backend},
	)
	session, err := manager.Open(context.Background(), "call-1")
	if err != nil {
		_ = peer.Close()
		t.Fatalf("Open() error = %v", err)
	}
	return session, peer
}

func startProtocolSession(t *testing.T, connection net.Conn) {
	t.Helper()
	if err := WriteFrame(connection, Frame{Type: FrameStart}); err != nil {
		t.Fatalf("write START: %v", err)
	}
	response, err := ReadFrame(connection, maxControlBytes)
	if err != nil {
		t.Fatalf("read STARTED: %v", err)
	}
	if response.Type != FrameStarted || response.Sequence != 0 || len(response.Payload) != 0 {
		t.Fatalf("STARTED = %+v", response)
	}
}

func writeWithDeadline(connection net.Conn, payload []byte) error {
	if err := connection.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	_, err := connection.Write(payload)
	return err
}

func TestConnectionAndDeviceIOClassificationKeepDifferentEOFMeaning(t *testing.T) {
	if err := classifyConnectionIOError("read", io.EOF); !errors.Is(err, errPeerClosed) {
		t.Fatalf("connection EOF = %v, want peer close", err)
	}
	if err := classifyDeviceIOError("read", io.EOF); !IsCode(err, ErrorInternal) {
		t.Fatalf("device EOF = %v, want internal device failure", err)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
	_, err := server.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("Read() unexpectedly succeeded")
	}
	classified := classifyConnectionIOError("read", err)
	if !IsCode(classified, ErrorTimeout) {
		t.Fatalf("classifyIOError() = %v", classified)
	}
	if !errors.Is(classified, err) {
		t.Fatal("classified timeout does not preserve the cause")
	}
}
