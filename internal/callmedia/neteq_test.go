//go:build cgo

package callmedia

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/audiocore"
)

// These traces enter the production C ABI and real bundled Opus decoder.
// Source times describe20ms capture, arrivals retain TCP ordering, and the
// device requests exactly10ms. No host receiver queue or age filter intervenes.
func TestNetEqProductionReceiverNetworkTraces(t *testing.T) {
	type trace struct {
		name    string
		batch   int
		jitter  []int
		step    int
		ppm     int
		seconds int
	}
	traces := []trace{
		{name: "uniform", batch: 1}, {name: "batch5", batch: 5}, {name: "batch10", batch: 10}, {name: "batch15", batch: 15},
		{name: "single41", batch: 5, step: 41}, {name: "single60", batch: 5, step: 60},
		{name: "single100", batch: 5, step: 100}, {name: "single150", batch: 5, step: 150},
		{name: "alternating60", batch: 5, jitter: []int{0, 60}},
		{name: "alternating100", batch: 5, jitter: []int{0, 100}},
		{name: "random100", batch: 5, jitter: []int{0, 41, 0, 60, 20, 0, 100, 10, 60, 0}},
		{name: "persistent100_then_return", batch: 5, step: 100},
		{name: "tcp1500", batch: 5, step: 1500},
		{name: "timestamp_wrap", batch: 5},
		{name: "source_gap", batch: 5},
		{name: "positive500ppm", batch: 5, ppm: 500, seconds: 120},
		{name: "negative500ppm", batch: 5, ppm: -500, seconds: 120},
	}
	for _, rate := range []int{8000, 16000} {
		for _, tr := range traces {
			t.Run(fmt.Sprintf("%d/%s", rate, tr.name), func(t *testing.T) {
				seconds := tr.seconds
				if seconds == 0 {
					seconds = 12
				}
				count := seconds * 50
				packets := neteqTestPackets(t, count)
				receiver, err := audiocore.NewReceiver(rate)
				if err != nil {
					t.Fatal(err)
				}
				defer receiver.Close()
				arrivals := make([]time.Duration, count)
				var previous time.Duration
				for i := 0; i < count; i++ {
					batch := i / tr.batch
					capture := float64((batch+1)*tr.batch*20) / (1 + float64(tr.ppm)/1e6)
					extra := 0
					if len(tr.jitter) > 0 {
						extra = tr.jitter[batch%len(tr.jitter)]
					}
					if tr.name == "persistent100_then_return" {
						if capture >= 2000 && capture < 5000 {
							extra = tr.step
						}
					} else if capture >= 2000 && capture < float64(2000+tr.batch*20) {
						extra = tr.step
					}
					arrival := time.Duration(math.Round((capture+50+float64(extra))*1000)) * time.Microsecond
					if arrival < previous {
						arrival = previous
					}
					arrivals[i] = arrival
					previous = arrival
				}
				var sequenceBase uint32
				var timestampBase uint32
				if tr.name == "timestamp_wrap" {
					sequenceBase = 65500
					timestampBase = ^uint32(0) - 48000
				}
				next := 0
				inserted := 0
				pcm := make([]int16, rate/100)
				var tailStart, tailEnd audiocore.ReceiverStats
				for now := time.Duration(0); now <= time.Duration(seconds+2)*time.Second; now += 10 * time.Millisecond {
					for next < count && arrivals[next] <= now {
						if tr.name == "source_gap" && next >= 100 && next < 105 {
							next++
							continue
						}
						if err := receiver.Enqueue(packets[next], uint16(sequenceBase+uint32(next)), timestampBase+uint32(next)*960, arrivals[next]); err != nil {
							t.Fatalf("packet%d: %v", next, err)
						}
						inserted++
						next++
					}
					if err := receiver.Pull(now, pcm); err != nil {
						t.Fatalf("pull%v: %v", now, err)
					}
					if now == time.Duration(seconds-4)*time.Second {
						tailStart = receiver.Statistics()
					}
					if now == time.Duration(seconds)*time.Second {
						tailEnd = receiver.Statistics()
					}
				}
				stats := receiver.Statistics()
				if next != count || stats.PacketsReceived != uint64(inserted) || stats.InternalSampleRate != 48000 {
					t.Fatalf("receiver lost ingress: sent%d stats%+v", next, stats)
				}
				// Baseline and transient/persistent disturbances must recover to useful
				// media. Do not impose a false lossless guarantee on300ms batches or jitter.
				if len(tr.jitter) == 0 && tr.batch <= 5 {
					concealed := tailEnd.ConcealedSamples - tailStart.ConcealedSamples
					if concealed > 4800 {
						t.Fatalf("permanent loss after recovery: tail concealed%d/192000, stats%+v", concealed, tailEnd)
					}
				}
				t.Logf("received=%d discarded=%d concealed48k=%d stretched=%d accelerated=%d target=%d current=%d tail_concealed48k=%d", stats.PacketsReceived, stats.PacketsDiscarded, stats.ConcealedSamples, stats.InsertedSamples, stats.RemovedSamples, stats.TargetDelayMS, stats.CurrentDelayMS, tailEnd.ConcealedSamples-tailStart.ConcealedSamples)
			})
		}
	}
}

func neteqTestPackets(t *testing.T, count int) [][]byte {
	t.Helper()
	factory, err := NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := factory.New(testFormat(16000))
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	packets := make([][]byte, count)
	pcm := make([]byte, 640)
	for frame := range packets {
		for i := 0; i < 320; i++ {
			sample := int16(7000 * math.Sin(float64(frame*320+i)*2*math.Pi*233/16000))
			pcm[i*2] = byte(sample)
			pcm[i*2+1] = byte(uint16(sample) >> 8)
		}
		packets[frame], err = codec.Encode(pcm)
		if err != nil {
			t.Fatal(err)
		}
	}
	return packets
}

func TestNetEqIngressResourceLimitAndDelayedDelivery(t *testing.T) {
	packets := neteqTestPackets(t, 1)
	r, err := audiocore.NewReceiver(16000)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pcm := make([]int16, 160)
	if err := r.Pull(100*time.Millisecond, pcm); err != nil {
		t.Fatal(err)
	}
	// Packet genuinely arrived before the previous render but callback delivery
	// was delayed. Preserve arrival metadata and never reverse the environment.
	if err := r.Enqueue(packets[0], 0, 0, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := r.Pull(110*time.Millisecond, pcm); err != nil {
		t.Fatal(err)
	}
	if r.Statistics().PacketsReceived != 1 {
		t.Fatal("delayed delivery rejected")
	}
	for i := 0; i < audiocore.IngressCapacity; i++ {
		if err := r.Enqueue(packets[0], uint16(i+1), uint32(i+1)*960, 120*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Enqueue(packets[0], 999, 999*960, 120*time.Millisecond); !errors.Is(err, audiocore.ErrBackpressure) {
		t.Fatalf("full ingress =%v", err)
	}
	if queued := r.Statistics().IngressQueued; queued != 200 {
		t.Fatalf("queue=%d", queued)
	}
	if err := r.Pull(120*time.Millisecond, pcm); err != nil {
		t.Fatal(err)
	}
	if r.Statistics().PacketsReceived != 201 {
		t.Fatal("resource cap silently replaced packet")
	}
	before := r.Statistics()
	if err := r.Pull(119*time.Millisecond, pcm); err == nil {
		t.Fatal("backwards pull accepted")
	}
	if err := r.Enqueue(packets[0], 999, 999*960, -time.Millisecond); err == nil {
		t.Fatal("negative arrival accepted")
	}
	if after := r.Statistics(); after != before {
		t.Fatal("invalid request changed receiver")
	}
	r.Close()
	if after := r.Statistics(); after != before {
		t.Fatal("close lost finalstats")
	}
}

func TestNetEqArbitraryRenderDemandMatchesTenMillisecondPulls(t *testing.T) {
	for _, rate := range []int{8000, 16000, 48000} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			a, err := audiocore.NewReceiver(rate)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := audiocore.NewReceiver(rate)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			packets := neteqTestPackets(t, 10)
			for i, p := range packets {
				if err := a.Enqueue(p, uint16(i), uint32(i)*960, 0); err != nil {
					t.Fatal(err)
				}
				if err := b.Enqueue(p, uint16(i), uint32(i)*960, 0); err != nil {
					t.Fatal(err)
				}
			}
			reference := make([]int16, rate/10)
			for i := 0; i < 10; i++ {
				if err := a.Pull(time.Duration(i)*10*time.Millisecond, reference[i*rate/100:]); err != nil {
					t.Fatal(err)
				}
			}
			rendered := make([]float32, len(reference))
			for at := 0; at < len(rendered); {
				n := min(128, len(rendered)-at)
				if err := b.Render(time.Duration(at)*time.Second/time.Duration(rate), rendered[at:at+n]); err != nil {
					t.Fatal(err)
				}
				at += n
			}
			for i, sample := range reference {
				if rendered[i] != float32(sample)/32768 {
					t.Fatalf("render remainder moved sample%d", i)
				}
			}
		})
	}
}

func TestNetEqRenderClockAndRouteRestart(t *testing.T) {
	for _, block := range []int{1, 128, 512, 1024, 2048} {
		t.Run(fmt.Sprint(block), func(t *testing.T) {
			r, err := audiocore.NewReceiver(16000)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			for i, p := range neteqTestPackets(t, 100) {
				if err := r.Enqueue(p, uint16(i), uint32(i)*960, 0); err != nil {
					t.Fatal(err)
				}
			}
			// A large device request advances NetEq's internal audio tick, never the
			// actual environment clock. A route restart5ms later must remain legal.
			if err := r.Render(100*time.Millisecond, make([]float32, block)); err != nil {
				t.Fatal(err)
			}
			if err := r.ResetRender(); err != nil {
				t.Fatal(err)
			}
			if err := r.Render(105*time.Millisecond, make([]float32, block)); err != nil {
				t.Fatalf("immediate route restart: %v", err)
			}
			if err := r.Render(105*time.Millisecond, make([]float32, block)); err != nil {
				t.Fatalf("same callback clock: %v", err)
			}
			before := r.Statistics()
			if err := r.Render(104*time.Millisecond, make([]float32, block)); err == nil {
				t.Fatal("backwards callback accepted")
			}
			if after := r.Statistics(); after.RenderErrors != before.RenderErrors+1 || after.PacketsReceived != before.PacketsReceived {
				t.Fatalf("unobservable RT error: before%+v after%+v", before, after)
			}
		})
	}
}

func TestNetEqSharedIngressEnforcesMonoTwentyMilliseconds(t *testing.T) {
	r, err := audiocore.NewReceiver(16000)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	payload := neteqTestPackets(t, 1)[0]
	stereo := append([]byte(nil), payload...)
	stereo[0] |= 4
	forty := append([]byte(nil), payload...)
	forty[0] = (forty[0] & 0xfc) | 1
	for name, p := range map[string][]byte{"stereo": stereo, "40ms": forty, "malformed": {0xff}, "too_large": make([]byte, 1276)} {
		if err := r.Enqueue(p, 0, 0, 0); !errors.Is(err, audiocore.ErrInvalidPacket) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if r.Statistics().IngressQueued != 0 {
		t.Fatal("invalid input occupied ingress")
	}
}

func TestNetEqHardwareEncoderWireClockAndDecoderRates(t *testing.T) {
	factory, err := NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []int{8000, 16000} {
		for _, target := range []int{8000, 16000, 48000} {
			t.Run(fmt.Sprintf("%d_to_%d", source, target), func(t *testing.T) {
				codec, err := factory.New(testFormat(source))
				if err != nil {
					t.Fatal(err)
				}
				defer codec.Close()
				pcm := make([]byte, source/50*2)
				for i := 0; i < source/50; i++ {
					v := int16(8000 * math.Sin(float64(i)*2*math.Pi*400/float64(source)))
					pcm[i*2] = byte(v)
					pcm[i*2+1] = byte(uint16(v) >> 8)
				}
				r, err := audiocore.NewReceiver(target)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				for i := 0; i < 10; i++ {
					p, err := codec.Encode(pcm)
					if err != nil {
						t.Fatal(err)
					}
					// Encoded bandwidth follows hardware; the MD clock stays16k and NetEq48k.
					_, ts, p, err := ParseSocketFrame(SocketFrame(uint32(i), uint32(i)*320, p))
					if err != nil {
						t.Fatal(err)
					}
					if err := r.Enqueue(p, uint16(i), ts*3, 0); err != nil {
						t.Fatal(err)
					}
				}
				out := make([]int16, target/100)
				nonzero := false
				for i := 0; i < 20; i++ {
					if err := r.Pull(time.Duration(i)*10*time.Millisecond, out); err != nil {
						t.Fatal(err)
					}
					for _, v := range out {
						if v != 0 {
							nonzero = true
						}
					}
				}
				if !nonzero || r.Statistics().InternalSampleRate != 48000 {
					t.Fatal("hardware bandwidth/transport clock adaptation failed")
				}
			})
		}
	}
}

func TestNetEqHubDeviceCadenceRecordsActualOutputAndDetachesOwner(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		for _, period := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
			t.Run(fmt.Sprintf("%d/%v", rate, period), func(t *testing.T) {
				format := testFormat(rate)
				format.FrameDuration = period
				endpoint := newFakeEndpoint(format)
				hub, err := newMediaHub(context.Background(), "hub-neteq", endpoint)
				if err != nil {
					t.Fatal(err)
				}
				defer hub.Close(context.Background())
				receiver, err := audiocore.NewReceiver(rate)
				if err != nil {
					t.Fatal(err)
				}
				defer receiver.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				session := &Session{hub: hub, ctx: ctx, cancel: cancel, socket: &socketAudio{receiver: receiver, epoch: time.Now()}}
				for i, p := range neteqTestPackets(t, 20) {
					if err := receiver.Enqueue(p, uint16(i), uint32(i)*960, 0); err != nil {
						t.Fatal(err)
					}
				}
				if err := hub.attachReceiver(session); err != nil {
					t.Fatal(err)
				}
				subscription, err := hub.Subscribe(16)
				if err != nil {
					t.Fatal(err)
				}
				defer subscription.Close()
				if err := subscription.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 8; i++ {
					written := receive(t, endpoint.writes)
					if len(written) != format.FrameBytes() {
						t.Fatal("device frame size changed")
					}
					endpoint.read <- make([]byte, format.FrameBytes())
					recorded, err := subscription.Next(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if string(recorded.UplinkPCM) != string(written) {
						t.Fatal("recording differs from hardware PCM (including PLC)")
					}
				}
				hub.detachReceiver(session)
				// One device write may already be in flight; no pending old decoded audio
				// can survive detach and later occupy a replacement owner's device slots.
				silent := false
				for i := 0; i < 4; i++ {
					written := receive(t, endpoint.writes)
					if allBytes(written, 0) {
						silent = true
						break
					}
				}
				if !silent {
					t.Fatal("detached receiver replayed buffered microphone PCM")
				}
				if err := session.Err(); err != nil {
					t.Fatalf("device consumption clock failed: %v", err)
				}
			})
		}
	}
}

// A hardware write may have handed PCM to the device while its completion is
// still being scheduled. The recorder must see a successful commit as a whole;
// this lock must never prevent media-owner detach or cancellation.
type neteqCommitEndpoint struct {
	*fakeEndpoint
	release  chan struct{}
	captured chan struct{}
}

func (e *neteqCommitEndpoint) WritePCM(ctx context.Context, pcm []byte) error {
	if err := e.fakeEndpoint.WritePCM(ctx, pcm); err != nil {
		return err
	}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (e *neteqCommitEndpoint) ReadPCM(ctx context.Context, pcm []byte) error {
	if err := e.fakeEndpoint.ReadPCM(ctx, pcm); err != nil {
		return err
	}
	select {
	case e.captured <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
func TestNetEqHubRecordingCommitDoesNotBlockOwnerDetach(t *testing.T) {
	format := testFormat(16000)
	endpoint := &neteqCommitEndpoint{fakeEndpoint: newFakeEndpoint(format), release: make(chan struct{}), captured: make(chan struct{}, 1)}
	hub, err := newMediaHub(context.Background(), "record-commit", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close(context.Background())
	r, err := audiocore.NewReceiver(16000)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := &Session{ctx: ctx, cancel: cancel, hub: hub, socket: &socketAudio{receiver: r, epoch: time.Now()}}
	for i, p := range neteqTestPackets(t, 20) {
		if err := r.Enqueue(p, uint16(i), uint32(i)*960, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := hub.attachReceiver(owner); err != nil {
		t.Fatal(err)
	}
	sub, err := hub.Subscribe(4)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := sub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	written := receive(t, endpoint.writes)
	endpoint.read <- make([]byte, format.FrameBytes())
	receive(t, endpoint.captured)
	detached := make(chan struct{})
	go func() { hub.detachReceiver(owner); close(detached) }()
	receive(t, detached)
	// Device completion is deliberately held. Capture cannot falsely publish
	// empty history, and detaching the owner already completed independently.
	select {
	case <-sub.frames:
		t.Fatal("recording published before successful device commit")
	case <-time.After(20 * time.Millisecond):
	}
	close(endpoint.release)
	recorded, err := sub.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(recorded.UplinkPCM) != string(written) {
		t.Fatal("recorded a fabricated silent frame across commit")
	}
}
