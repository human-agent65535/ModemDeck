package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeSocket struct {
	inbound  chan []byte
	outbound chan []byte
	closed   chan struct{}
	once     sync.Once
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{inbound: make(chan []byte, 32), outbound: make(chan []byte, 32), closed: make(chan struct{})}
}
func (s *fakeSocket) Read(ctx context.Context) ([]byte, error) {
	select {
	case b := <-s.inbound:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, ErrTransportClosed
	}
}
func (s *fakeSocket) Write(ctx context.Context, b []byte) error {
	select {
	case s.outbound <- b:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return ErrTransportClosed
	}
}
func (s *fakeSocket) Finish(error, AudioStatistics) {}
func (s *fakeSocket) InterruptRead()                {}
func (s *fakeSocket) Close() error                  { s.once.Do(func() { close(s.closed) }); return nil }

func TestSocketSharedHubGatesAudioAndRetainsFinalStatistics(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			format := testFormat(rate)
			format.FrameDuration = 10 * time.Millisecond
			core, opener, _ := testCore(t, format)
			authorizeCall(t, core, "socket-call")
			socket := newFakeSocket()
			session, err := core.OpenSocket(context.Background(), ActiveCall{ID: "socket-call", State: CallStateActive}, "owner", socket)
			if err != nil {
				t.Fatal(err)
			}
			if opener.endpoint.startCalls.Load() != 0 {
				t.Fatal("hub started before native audio activation")
			}
			second, err := core.OpenSocket(context.Background(), ActiveCall{ID: "socket-call", State: CallStateActive}, "other", newFakeSocket())
			if second != nil || !errors.Is(err, ErrCallInUse) {
				t.Fatalf("second owner: %v", err)
			}
			socket.inbound <- SocketFrame(0, 0, []byte{20, 0x11})
			receive(t, opener.endpoint.started)
			pcm := receiveNonSilentPCM(t, opener.endpoint.writes)
			if len(pcm) != format.FrameBytes() {
				t.Fatal("hub PCM was not adapted to hardware frame size")
			}
			for i := 0; i < 2; i++ {
				frame := make([]byte, format.FrameBytes())
				for j := range frame {
					frame[j] = 0x33
				}
				opener.endpoint.read <- frame
			}
			packet := receive(t, socket.outbound)
			sequence, timestamp, payload, err := ParseSocketFrame(packet)
			if err != nil || sequence != 0 || timestamp != 0 || len(payload) == 0 {
				t.Fatalf("wire frame: %d %d %v", sequence, timestamp, err)
			}
			// A valid source jump arriving too early is counted by the actual
			// receive worker, then retained across release and reacquisition.
			socket.inbound <- SocketFrame(100, 32000, []byte{20, 0x44})
			eventually(t, func() bool { return session.Statistics().DroppedSourceEarlyPackets == 1 })
			if err := core.ReleaseOwner(context.Background(), "socket-call", "owner"); err != nil {
				t.Fatal(err)
			}
			stats := core.Statistics("socket-call")
			if stats.ReceivedPackets != 2 || stats.SentPackets != 1 || stats.State != "disconnected" || stats.DroppedPackets != 1 || stats.DroppedSourceEarlyPackets != 1 {
				t.Fatalf("lost final statistics: %+v", stats)
			}
			if opener.endpoint.closeCalls.Load() != 0 {
				t.Fatal("media owner close terminated shared hardware lifetime")
			}
			replacement, err := core.OpenSocket(context.Background(), ActiveCall{ID: "socket-call", State: CallStateActive}, "owner", newFakeSocket())
			if err != nil {
				t.Fatal(err)
			}
			if stats := core.Statistics("socket-call"); stats.ReceivedPackets != 2 || stats.DroppedPackets != 1 || stats.DroppedSourceEarlyPackets != 1 {
				t.Fatal("reconnect reset call counters")
			}
			if opener.opens.Load() != 1 {
				t.Fatal("reconnect opened a second modem endpoint")
			}
			if err := replacement.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			<-session.Done()
		})
	}
}

func TestSocketRejectsInvalidOrderingAndRetainsFailure(t *testing.T) {
	core, _, _ := testCore(t, testFormat(16000))
	authorizeCall(t, core, "call")
	socket := newFakeSocket()
	session, err := core.OpenSocket(context.Background(), ActiveCall{ID: "call", State: CallStateActive}, "owner", socket)
	if err != nil {
		t.Fatal(err)
	}
	socket.inbound <- SocketFrame(0, 0, []byte{20, 1})
	socket.inbound <- SocketFrame(0, 0, []byte{20, 1})
	receive(t, session.Done())
	eventually(t, func() bool { return core.Statistics("call").FailureCode == "invalid_audio" })
	if !errors.Is(session.Err(), ErrInvalidAudio) {
		t.Fatalf("error lost: %v", session.Err())
	}
}

func TestSocketHeaderValidationAndWrap(t *testing.T) {
	frame := SocketFrame(^uint32(0), ^uint32(0)-319, []byte{20, 1})
	seq, ts, payload, err := ParseSocketFrame(frame)
	if err != nil || seq != ^uint32(0) || ts != ^uint32(0)-319 || len(payload) != 2 {
		t.Fatal("header did not preserve uint32 values")
	}
	for _, offset := range []int{0, 2, 3} {
		invalid := append([]byte(nil), frame...)
		invalid[offset] ^= 0xff
		if _, _, _, err := ParseSocketFrame(invalid); err == nil {
			t.Fatal("invalid header accepted")
		}
	}
	if _, _, _, err := ParseSocketFrame(frame[:12]); err == nil {
		t.Fatal("empty payload accepted")
	}
}

func TestSocketDropsStaleAndBoundedBurstFrames(t *testing.T) {
	core, opener, _ := testCore(t, testFormat(16000))
	authorizeCall(t, core, "call")
	socket := newFakeSocket()
	session, err := core.OpenSocket(context.Background(), ActiveCall{ID: "call", State: CallStateActive}, "owner", socket)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the playback queue directly without transport receipt races.
	close(session.socket.firstPacket)
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 0x77}, playAt: time.Now().Add(-time.Second), generation: 1})
	receive(t, opener.endpoint.started)
	for i := 0; i < 3; i++ {
		pcm := receive(t, opener.endpoint.writes)
		if !allBytes(pcm, 0) {
			t.Fatal("stale microphone audio replayed")
		}
	}
	if socketQueueFrames != 7 {
		t.Fatal("uplink capacity must be a 100ms batch plus 40ms prebuffer")
	}
}

func TestSocketResamplePreservesMonoLevels(t *testing.T) {
	pcm := make([]byte, 320)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], uint16(1234))
	}
	up := socketResample(pcm, 8000, 16000)
	down := socketResample(up, 16000, 8000)
	if len(up) != 640 || string(down) != string(pcm) {
		t.Fatal("8k/16k PCM boundary changed constant input")
	}
}

func TestSocketSourceClockValidatesWrapAndTimestampSkips(t *testing.T) {
	base := time.Unix(1000, 0)
	clock := socketReceiveClock{}
	if accepted, err := clock.accept(^uint32(0), ^uint32(0)-319, base); err != nil || !accepted {
		t.Fatal(err)
	}
	if accepted, err := clock.accept(0, 0, base.Add(20*time.Millisecond)); err != nil || !accepted {
		t.Fatal("wrap failed", err)
	}
	if accepted, err := clock.accept(2, 640, base.Add(60*time.Millisecond)); err != nil || !accepted {
		t.Fatal("omitted capture frame failed", err)
	}
	if _, err := clock.accept(2, 640, base.Add(80*time.Millisecond)); !errors.Is(err, ErrInvalidAudio) {
		t.Fatal("duplicate accepted")
	}
	if _, err := clock.accept(3, 641, base.Add(80*time.Millisecond)); !errors.Is(err, ErrInvalidAudio) {
		t.Fatal("inconsistent timestamp accepted")
	}
}

func TestSocketInvalidFirstOpusDoesNotStartOrConnectHub(t *testing.T) {
	core, opener, _ := testCore(t, testFormat(16000))
	authorizeCall(t, core, "call")
	socket := newFakeSocket()
	session, err := core.OpenSocket(context.Background(), ActiveCall{ID: "call", State: CallStateActive}, "owner", socket)
	if err != nil {
		t.Fatal(err)
	}
	if session.Statistics().State != "connecting" {
		t.Fatal("unready socket falsely reported connected")
	}
	socket.inbound <- SocketFrame(0, 0, []byte{40, 1})
	receive(t, session.Done())
	if opener.endpoint.startCalls.Load() != 0 {
		t.Fatal("invalid Opus started modem/test source")
	}
	if !errors.Is(session.Err(), ErrInvalidAudio) {
		t.Fatal("invalid first frame accepted")
	}
}

func TestSocketOwnerCallbacksAndReplacementAreSerialized(t *testing.T) {
	core, _, _ := testCore(t, testFormat(16000))
	states := make(chan bool, 8)
	core.onOwnerStateChange = func(id string, connected bool) {
		if id != "owner-call" {
			panic("wrong callback call")
		}
		states <- connected
	}
	authorizeCall(t, core, "owner-call")
	call := ActiveCall{ID: "owner-call", State: CallStateActive}
	socket := newFakeSocket()
	session, err := core.OpenSocket(context.Background(), call, "first", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ReleaseOwner(context.Background(), call.ID, "foreign"); !errors.Is(err, ErrNotMediaOwner) {
		t.Fatalf("foreign release: %v", err)
	}
	select {
	case <-session.Done():
		t.Fatal("foreign release ended owner")
	default:
	}
	select {
	case <-states:
		t.Fatal("connected before valid audio")
	default:
	}
	socket.inbound <- SocketFrame(0, 0, []byte{20, 1})
	if !receive(t, states) {
		t.Fatal("missing connected callback")
	}
	if err := core.ReleaseOwner(context.Background(), call.ID, "first"); err != nil {
		t.Fatal(err)
	}
	replacementSocket := newFakeSocket()
	replacement, err := core.OpenSocket(context.Background(), call, "second", replacementSocket)
	if err != nil {
		t.Fatal(err)
	}
	replacementSocket.inbound <- SocketFrame(0, 0, []byte{20, 1})
	if receive(t, states) {
		t.Fatal("replacement connected before original disconnect")
	}
	if !receive(t, states) {
		t.Fatal("replacement did not connect")
	}
	if err := core.ReleaseOwner(context.Background(), call.ID, "first"); !errors.Is(err, ErrNotMediaOwner) {
		t.Fatalf("old owner released replacement: %v", err)
	}
	if err := replacement.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if receive(t, states) {
		t.Fatal("missing replacement disconnect")
	}
	select {
	case <-states:
		t.Fatal("duplicate owner callback")
	default:
	}
}

func TestSocketOwnerReleaseCancelsPreparationWithoutClosingCall(t *testing.T) {
	opener := &blockingEndpointOpener{started: make(chan struct{})}
	core, err := New(Options{EndpointOpener: opener, CodecFactory: &fakeCodecFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close(context.Background())
	authorizeCall(t, core, "preparing")
	opened := make(chan error, 1)
	go func() {
		_, err := core.OpenSocket(context.Background(), ActiveCall{ID: "preparing", State: CallStateActive}, "owner", newFakeSocket())
		opened <- err
	}()
	receive(t, opener.started)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.ReleaseOwner(ctx, "preparing", "wrong"); !errors.Is(err, ErrNotMediaOwner) {
		t.Fatalf("foreign preparation release: %v", err)
	}
	if err := core.ReleaseOwner(ctx, "preparing", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, opened); !errors.Is(err, ErrCanceled) {
		t.Fatalf("preparation error: %v", err)
	}
	core.mu.Lock()
	_, owned := core.owners["preparing"]
	lifetime := core.lifetimes["preparing"]
	core.mu.Unlock()
	if owned || lifetime == nil || lifetime.ctx.Err() != nil {
		t.Fatal("release removed call lifetime or retained owner")
	}
}
