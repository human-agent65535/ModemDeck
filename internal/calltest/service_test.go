package calltest

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
)

type localPush chan string

func (p localPush) SendAudioTestCall(_ context.Context, _, _, id string) error { p <- id; return nil }

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(12 * time.Second):
		t.Fatal("timed out waiting for local audio test")
		var zero T
		return zero
	}
}

// This covers the actual scheduled call -> answer -> authoritative registration
// -> authenticated WSS owner -> production Opus/PCM -> playback -> hangup path.
// The in-memory transport and push sender cannot contact a paired device.
func TestCallTestUsesProductionWSSRuntimeForAudioAndOwnership(t *testing.T) {
	push := make(localPush, 1)
	service := New(push, nil)
	t.Cleanup(service.Close)
	status, err := service.Start("user", "device")
	if err != nil {
		t.Fatal(err)
	}
	owner := "user\x00device"
	entry, _ := service.lookup(status.ID, owner)
	if _, err := service.Answer(status.ID, owner); !errors.Is(err, ErrEnded) {
		t.Fatalf("answer before ringing = %v", err)
	}
	if _, err := service.Answer(status.ID, "other-device"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign answer = %v", err)
	}
	if pushed := receive(t, push); pushed != status.ID {
		t.Fatalf("pushed ID = %q", pushed)
	}
	if _, err := service.Answer(status.ID, owner); err != nil {
		t.Fatal(err)
	}
	projection, _, err := service.Active(context.Background(), status.ID, owner)
	if err != nil || len(projection.Calls) != 1 || projection.Calls[0].ControlState != calllease.ControlOwned {
		t.Fatalf("answered projection = %+v, %v", projection, err)
	}
	factory, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	format := callmedia.PCMFormat{Encoding: callmedia.PCMEncodingS16LE, SampleRate: 16000, Channels: 1, FrameDuration: 20 * time.Millisecond}
	codec, err := factory.New(format)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); workers.Wait(); _ = codec.Close() })
	tone, playback := make(chan struct{}, 1), make(chan struct{}, 1)
	socket := newLocalSocket()
	if _, err := service.OpenSocket(ctx, status.ID, owner, "media-owner", socket); err != nil {
		t.Fatal(err)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			var frame []byte
			select {
			case frame = <-socket.outbound:
			case <-ctx.Done():
				return
			}
			_, _, payload, err := callmedia.ParseSocketFrame(frame)
			if err != nil {
				return
			}
			audio, err := codec.Decode(payload)
			if err != nil {
				return
			}
			low, high := spectralPower(audio.PCM, 440), spectralPower(audio.PCM, 880)
			if entry.audio.Phase() == "tone" && low > 1e5 && low > 4*high {
				select {
				case tone <- struct{}{}:
				default:
				}
			}
			if entry.audio.Phase() == "playback" && high > 1e5 && high > 4*low {
				select {
				case playback <- struct{}{}:
				default:
				}
			}
		}
	}()
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(format.FrameDuration)
		defer ticker.Stop()
		pcm := make([]byte, format.FrameBytes())
		for frame := 0; ; frame++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			for i := 0; i < len(pcm)/2; i++ {
				binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(6000*math.Sin(2*math.Pi*880*float64(frame*320+i)/16000))))
			}
			encoded, err := codec.Encode(pcm)
			if err != nil {
				return
			}
			select {
			case socket.inbound <- callmedia.SocketFrame(uint32(frame), uint32(frame*320), encoded):
			case <-ctx.Done():
				return
			}
		}
	}()
	receive(t, tone)
	if _, err := service.OpenSocket(ctx, status.ID, owner, "second-media-owner", newLocalSocket()); !errors.Is(err, mediaapp.ErrConflict) {
		t.Fatalf("second media owner = %v", err)
	}
	if err := service.Release(ctx, status.ID, owner, "wrong-media-owner"); !errors.Is(err, mediaapp.ErrConflict) {
		t.Fatalf("foreign media release = %v", err)
	}
	if _, err := service.Renew(ctx, status.ID, owner); err != nil {
		t.Fatal(err)
	}
	// A recognizable microphone waveform must survive Opus uplink, PCM capture,
	// delayed playback and Opus downlink, including rejected ownership attempts.
	receive(t, playback)
	quality, err := service.AudioStatus(status.ID, owner)
	if err != nil || quality.ReceivedPackets == 0 || quality.CapturedFrames < 150 || quality.CapturedDBFS < -25 || quality.CapturedDBFS > -10 {
		t.Fatalf("microphone level did not survive the production pipeline: %+v, %v", quality, err)
	}
	if err := service.End(status.ID, owner); err != nil {
		t.Fatal(err)
	}
	projection, _, err = service.Active(ctx, status.ID, owner)
	if err != nil || len(projection.Calls) != 0 {
		t.Fatalf("ended projection = %+v, %v", projection, err)
	}
	if _, err := service.OpenSocket(ctx, status.ID, owner, "late-owner", newLocalSocket()); !errors.Is(err, calllease.ErrCallNotActive) {
		t.Fatalf("late media attach = %v", err)
	}
	entry.audio.mu.Lock()
	cleared := entry.audio.closed && len(entry.audio.clip) == 0
	entry.audio.mu.Unlock()
	if !cleared {
		t.Fatal("ended test retained PCM audio")
	}
}

func spectralPower(pcm []byte, frequency float64) float64 {
	var real, imaginary float64
	for i := 0; i < len(pcm)/2; i++ {
		value := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
		angle := 2 * math.Pi * frequency * float64(i) / 16000
		real += value * math.Cos(angle)
		imaginary += value * math.Sin(angle)
	}
	return (real*real + imaginary*imaginary) / float64(len(pcm)*len(pcm))
}

// This transport is process-local. No network, relay service or paired device.
type localSocket struct {
	inbound, outbound chan []byte
	closed            chan struct{}
	once              sync.Once
}

func newLocalSocket() *localSocket {
	return &localSocket{inbound: make(chan []byte, 32), outbound: make(chan []byte, 32), closed: make(chan struct{})}
}
func (s *localSocket) Read(ctx context.Context) ([]byte, error) {
	select {
	case b := <-s.inbound:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, callmedia.ErrTransportClosed
	}
}
func (s *localSocket) Write(ctx context.Context, b []byte) error {
	select {
	case s.outbound <- b:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return callmedia.ErrTransportClosed
	}
}
func (s *localSocket) Finish(error, callmedia.AudioStatistics) {}
func (s *localSocket) InterruptRead()                          {}
func (s *localSocket) Close() error                            { s.once.Do(func() { close(s.closed) }); return nil }
