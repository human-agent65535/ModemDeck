package calltest

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
	"github.com/pion/turn/v5"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type localPush chan string

func (p localPush) SendAudioTestCall(_ context.Context, _, _, id string) error { p <- id; return nil }

type localRTC struct{ server rtcconfig.ICEServer }

func (r localRTC) Generate(context.Context) (rtcconfig.Configuration, error) {
	return rtcconfig.Configuration{ICEServers: []rtcconfig.ICEServer{r.server}}, nil
}

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
// -> relay-only SDP/ICE -> production Opus/PCM -> playback -> hangup path. Its
// TURN server and push sender are local; it cannot contact a paired device.
func TestCallTestUsesProductionRuntimeForRelayAudioAndOwnership(t *testing.T) {
	connection, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := turn.NewServer(turn.ServerConfig{
		Realm: "local-test",
		AuthHandler: func(attributes *turn.RequestAttributes) (string, []byte, bool) {
			return attributes.Username, turn.GenerateAuthKey("test", "local-test", "test-password"), attributes.Username == "test"
		},
		PacketConnConfigs: []turn.PacketConnConfig{{PacketConn: connection,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"}}},
	})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	provider := localRTC{server: rtcconfig.ICEServer{URLs: []string{"turn:" + connection.LocalAddr().String() + "?transport=udp"}, Username: "test", Credential: "test-password"}}
	push := make(localPush, 1)
	service := New(push, provider, nil)
	t.Cleanup(service.Close)
	status, err := service.Start("user", "device")
	if err != nil {
		t.Fatal(err)
	}
	owner := "user\x00device"
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
	configuration, err := service.Configuration(context.Background(), status.ID, owner)
	if err != nil || !configuration.RelayOnly {
		t.Fatalf("relay configuration = %+v, %v", configuration, err)
	}

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{ICETransportPolicy: webrtc.ICETransportPolicyRelay,
		ICEServers: []webrtc.ICEServer{{URLs: provider.server.URLs, Username: provider.server.Username, Credential: provider.server.Credential}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "local-test")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := peer.AddTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buffer := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buffer); err != nil {
				return
			}
		}
	}()
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
	t.Cleanup(func() { cancel(); _ = peer.Close(); workers.Wait(); _ = codec.Close() })
	tone, playback := make(chan struct{}, 1), make(chan struct{}, 1)
	peer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		workers.Add(1)
		defer workers.Done()
		for {
			packet, _, err := remote.ReadRTP()
			if err != nil {
				return
			}
			audio, err := codec.Decode(packet.Payload)
			if err != nil {
				return
			}
			low, high := spectralPower(audio.PCM, 440), spectralPower(audio.PCM, 880)
			if low > 1e5 && low > 4*high {
				select {
				case tone <- struct{}{}:
				default:
				}
			}
			if high > 1e5 && high > 4*low {
				select {
				case playback <- struct{}{}:
				default:
				}
			}
		}
	})
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	receive(t, gathered)
	offerSDP := peer.LocalDescription().SDP
	answer, err := service.Exchange(ctx, status.ID, owner, "media-owner", offerSDP)
	if err != nil {
		t.Fatalf("answered call cannot negotiate audio: %v", err)
	}
	if err := peer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
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
			if err := track.WriteSample(media.Sample{Data: encoded, Duration: format.FrameDuration}); err != nil {
				return
			}
		}
	}()
	receive(t, tone)
	if _, err := service.Exchange(ctx, status.ID, owner, "second-media-owner", offerSDP); !errors.Is(err, mediaapp.ErrConflict) {
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
	if err := service.End(status.ID, owner); err != nil {
		t.Fatal(err)
	}
	projection, _, err = service.Active(ctx, status.ID, owner)
	if err != nil || len(projection.Calls) != 0 {
		t.Fatalf("ended projection = %+v, %v", projection, err)
	}
	if _, err := service.Exchange(ctx, status.ID, owner, "late-owner", offerSDP); !errors.Is(err, calllease.ErrCallNotActive) {
		t.Fatalf("late negotiation = %v", err)
	}
	entry, _ := service.lookup(status.ID, owner)
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
