package callmedia

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// The fixture is also consumed by the native client so arrival recovery and
// uint32 wrap behavior remain a cross-client transport contract.
func TestSocketSharedClockVectors(t *testing.T) {
	var fixture struct {
		Version int `json:"version"`
		Cases   []struct {
			Name   string `json:"name"`
			Frames []struct {
				Sequence   uint32 `json:"sequence"`
				Timestamp  uint32 `json:"timestamp"`
				Arrival    int64  `json:"arrival_us"`
				Accepted   bool   `json:"accepted"`
				Generation uint64 `json:"generation"`
				Play       *int64 `json:"play_us"`
			} `json:"frames"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/socket_clock_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) < 14 {
		t.Fatal("incomplete clock fixture")
	}
	base := time.Unix(1000, 0)
	for _, vector := range fixture.Cases {
		t.Run(vector.Name, func(t *testing.T) {
			var clock socketReceiveClock
			for _, frame := range vector.Frames {
				accepted, err := clock.accept(frame.Sequence, frame.Timestamp, base.Add(time.Duration(frame.Arrival)*time.Microsecond))
				if err != nil || accepted != frame.Accepted || clock.generation != frame.Generation {
					t.Fatalf("sequence%d: accept=%v generation=%d err=%v, want %v/%d", frame.Sequence, accepted, clock.generation, err, frame.Accepted, frame.Generation)
				}
				if accepted && (frame.Play == nil || clock.playAt.Sub(base) != time.Duration(*frame.Play)*time.Microsecond) {
					t.Fatalf("sequence%d: play=%s, want %v microseconds", frame.Sequence, clock.playAt.Sub(base), frame.Play)
				}
			}
		})
	}
}

func newSocketPlayoutForTest(t *testing.T, hardwarePeriod time.Duration) (*Session, *socketPlayout, *fakeCodecFactory) {
	t.Helper()
	format := testFormat(SocketSampleRate)
	factory := &fakeCodecFactory{}
	codec, err := factory.New(format)
	if err != nil {
		t.Fatal(err)
	}
	hardware := format
	hardware.FrameDuration = hardwarePeriod
	session := &Session{format: format, codec: codec, hub: &mediaHub{format: hardware}, socket: &socketAudio{codecFactory: factory}}
	playout := &socketPlayout{session: session, decoder: codec}
	t.Cleanup(func() { playout.close(); _ = codec.Close() })
	return session, playout, factory
}

func readSocketPlayout(t *testing.T, playout *socketPlayout, at time.Time) byte {
	t.Helper()
	pcm, err := playout.next(at, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != playout.session.hub.format.FrameBytes() {
		t.Fatal("incorrect hardware frame length")
	}
	if !allBytes(pcm, pcm[0]) {
		t.Fatal("mixed source slots in PCM")
	}
	return pcm[0]
}

func enqueueSocketClock(t *testing.T, session *Session, clock *socketReceiveClock, sequence uint32, arrival time.Time) bool {
	t.Helper()
	accepted, err := clock.accept(sequence, sequence*320, arrival)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		session.enqueueSocketPacket(socketPacket{
			payload: []byte{20, byte(sequence%250 + 1)}, sequence: sequence, sourceSamples: clock.sourceSamples,
			generation: clock.generation, playAt: clock.playAt,
		})
	} else {
		session.stats.recordSourceDrop(clock.age)
	}
	if len(session.socket.incoming.packets) > socketQueueFrames {
		t.Fatal("queue exceeded capacity")
	}
	return accepted
}

func TestSocketPlayoutUniformAndBatchJitterHaveContinuousSourceSlots(t *testing.T) {
	for _, hardwarePeriod := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
		for _, cadence := range []struct {
			name   string
			frames int
			jitter bool
		}{
			{"uniform", 1, false}, {"batch", 5, false}, {"batch_40ms_jitter", 5, true},
		} {
			t.Run(hardwarePeriod.String()+"/"+cadence.name, func(t *testing.T) {
				session, playout, _ := newSocketPlayoutForTest(t, hardwarePeriod)
				var clock socketReceiveClock
				base := time.Unix(1000, 0)
				// Deterministic arrival before playout at an identical timestamp
				// tests the exact 40ms buffer boundary without wall-clock flakes.
				for elapsed := time.Duration(0); elapsed < 2*time.Second; elapsed += hardwarePeriod {
					for batch := 0; batch < 100/cadence.frames; batch++ {
						arrival := time.Duration(batch*cadence.frames) * 20 * time.Millisecond
						if cadence.jitter && batch%2 == 1 {
							arrival += 40 * time.Millisecond
						}
						if elapsed == arrival {
							for j := 0; j < cadence.frames; j++ {
								if !enqueueSocketClock(t, session, &clock, uint32(batch*cadence.frames+j), base.Add(elapsed)) {
									t.Fatal("healthy source rejected")
								}
							}
						}
					}
					marker := readSocketPlayout(t, playout, base.Add(elapsed))
					expected := byte(0)
					if elapsed >= socketPrebuffer {
						expected = byte((elapsed-socketPrebuffer)/(20*time.Millisecond)%250 + 1)
					}
					if marker != expected {
						t.Fatalf("at%s: marker%d want%d", elapsed, marker, expected)
					}
				}
				if drops := session.stats.droppedPackets.Load(); drops != 0 {
					t.Fatalf("healthy stream dropped%d", drops)
				}
				if playout.offset != 0 || playout.starving {
					t.Fatal("normal batch cadence incorrectly rebuffered")
				}
			})
		}
	}
}

func TestSocketPlayoutLateBudgetUsesEachMediaSlot(t *testing.T) {
	session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
	base := time.Unix(1000, 0)
	var clock socketReceiveClock
	for i := uint32(0); i < 5; i++ {
		enqueueSocketClock(t, session, &clock, i, base)
	}
	// Delay first playout by 1ms. All five source slots still survive, including
	// the fifth at121ms, whose common arrival time is already over100ms old.
	for i := 0; i < 5; i++ {
		if got := readSocketPlayout(t, playout, base.Add(time.Duration(41+i*20)*time.Millisecond)); got != byte(i+1) {
			t.Fatalf("lost batch frame%d: %d", i, got)
		}
	}
	if drops := session.stats.droppedPackets.Load(); drops != 0 {
		t.Fatal("normal batch expired", drops)
	}
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 99}, sequence: 5, sourceSamples: float64(5) * 320, generation: 1, playAt: base.Add(140 * time.Millisecond)})
	if got := readSocketPlayout(t, playout, base.Add(500*time.Millisecond)); got != 0 {
		t.Fatal("old audio replayed after playback stall")
	}
	if drops := session.stats.droppedPackets.Load(); drops != 1 {
		t.Fatalf("expired packet stats=%d", drops)
	}
}

func TestSocketPlayoutCapacityAndFutureGap(t *testing.T) {
	session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
	base := time.Unix(1000, 0)
	for i := 0; i < socketQueueFrames+1; i++ {
		session.enqueueSocketPacket(socketPacket{payload: []byte{20, byte(i + 1)}, sequence: uint32(i), sourceSamples: float64(uint32(i)) * 320, generation: 1, playAt: base.Add(time.Duration(i) * 20 * time.Millisecond)})
	}
	if len(session.socket.incoming.packets) != socketQueueFrames || session.stats.droppedPackets.Load() != 1 {
		t.Fatal("capacity/drop invariant failed")
	}
	if got := readSocketPlayout(t, playout, base); got != 0 {
		t.Fatal("future audio played early")
	}
	if got := readSocketPlayout(t, playout, base.Add(20*time.Millisecond)); got != 2 {
		t.Fatal("oldest retained audio missing")
	}
	// A legitimate capture omission must leave its source slot silent.
	session.socket.incoming.packets = nil
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 42}, sequence: 3, sourceSamples: float64(3) * 320, generation: 1, playAt: base.Add(60 * time.Millisecond)})
	if got := readSocketPlayout(t, playout, base.Add(40*time.Millisecond)); got != 0 {
		t.Fatal("source timestamp gap collapsed")
	}
	if got := readSocketPlayout(t, playout, base.Add(60*time.Millisecond)); got != 42 {
		t.Fatal("source gap failed to resume")
	}
}

func TestSocketPlayoutGenerationInvalidatesQueueAndDecodedHalfFrame(t *testing.T) {
	session, playout, factory := newSocketPlayoutForTest(t, 10*time.Millisecond)
	base := time.Unix(1000, 0)
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 11}, sequence: 1, sourceSamples: float64(1) * 320, generation: 1, playAt: base})
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 12}, sequence: 2, sourceSamples: float64(2) * 320, generation: 1, playAt: base.Add(20 * time.Millisecond)})
	if got := readSocketPlayout(t, playout, base); got != 11 {
		t.Fatal("first half missing")
	}
	dropped := session.enqueueSocketPacket(socketPacket{payload: []byte{20, 77}, sequence: 50, sourceSamples: float64(50) * 320, generation: 2, playAt: base.Add(50 * time.Millisecond)})
	if dropped != 1 {
		t.Fatal("old generation queue not invalidated")
	}
	for ms := 10; ms < 50; ms += 10 {
		if got := readSocketPlayout(t, playout, base.Add(time.Duration(ms)*time.Millisecond)); got != 0 {
			t.Fatal("old decoded half replayed")
		}
	}
	if got := readSocketPlayout(t, playout, base.Add(50*time.Millisecond)); got != 77 {
		t.Fatal("new timeline failed to start")
	}
	if got := readSocketPlayout(t, playout, base.Add(60*time.Millisecond)); got != 77 {
		t.Fatal("new second half missing")
	}
	if len(factory.codecs) != 2 {
		t.Fatal("decoder predictive state crossed generation")
	}
	if session.stats.droppedPackets.Load() != 2 {
		t.Fatal("generation discard counters incorrect")
	}
}

func TestSocketPlayoutRepeatedUnderrunsReplaceBoundedEpoch(t *testing.T) {
	session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
	base := time.Unix(1000, 0)
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 1}, generation: 1, playAt: base})
	if got := readSocketPlayout(t, playout, base); got != 1 {
		t.Fatal("initial audio missing")
	}
	for epoch := 0; epoch < 20; epoch++ {
		start := base.Add(time.Duration(epoch*100+20) * time.Millisecond)
		if got := readSocketPlayout(t, playout, start); got != 0 || !playout.starving {
			t.Fatal("underrun not detected")
		}
		arrival := start.Add(20 * time.Millisecond)
		// Original media slot is20ms late. Resume adds one40ms prefill, using a
		// replacement60ms offset each time; it must not grow with epoch count.
		session.enqueueSocketPacket(socketPacket{payload: []byte{20, byte(epoch + 2)}, sequence: uint32(epoch + 1), generation: 1, playAt: start})
		if got := readSocketPlayout(t, playout, arrival); got != 0 {
			t.Fatal("rebuffer skipped prefill")
		}
		if playout.offset != 60*time.Millisecond {
			t.Fatalf("epoch%d accumulated offset%s", epoch, playout.offset)
		}
		if got := readSocketPlayout(t, playout, arrival.Add(20*time.Millisecond)); got != 0 {
			t.Fatal("short prefill")
		}
		if got := readSocketPlayout(t, playout, arrival.Add(40*time.Millisecond)); got != byte(epoch+2) {
			t.Fatal("resume frame missing")
		}
		if len(session.socket.incoming.packets) > socketQueueFrames {
			t.Fatal("rebuffer exceeded capacity")
		}
	}
	if session.stats.droppedPackets.Load() != 0 {
		t.Fatal("repeated underruns expired otherwise fresh source")
	}
	// Rebuffering cannot give an already90ms-late frame another40ms of life.
	now := base.Add(3 * time.Second)
	readSocketPlayout(t, playout, now)
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 99}, sequence: 100, sourceSamples: float64(100) * 320, generation: 1, playAt: now.Add(-90 * time.Millisecond)})
	if got := readSocketPlayout(t, playout, now); got != 0 || len(session.socket.incoming.packets) != 0 {
		t.Fatal("rebuffer renewed stale source deadline")
	}
	if session.stats.droppedPackets.Load() != 1 {
		t.Fatal("stale rebuffer drop missing")
	}
}

func TestSocketProductionHubPlaysEveryUniformAndBatchedFrame(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		for _, period := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
			for _, batchSize := range []int{1, 5} {
				t.Run(fmt.Sprintf("%d/%s/batch%d", rate, period, batchSize), func(t *testing.T) {
					format := testFormat(rate)
					format.FrameDuration = period
					core, opener, _ := testCore(t, format)
					authorizeCall(t, core, "batch-call")
					socket := newFakeSocket()
					session, err := core.OpenSocket(context.Background(), ActiveCall{ID: "batch-call", State: CallStateActive}, "owner", socket)
					if err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					sent := make(chan struct{})
					go func() {
						defer close(sent)
						start := time.Now()
						for first := 0; first < 40; first += batchSize {
							timer := time.NewTimer(time.Until(start.Add(time.Duration(first) * 20 * time.Millisecond)))
							select {
							case <-timer.C:
							case <-ctx.Done():
								timer.Stop()
								return
							}
							for j := 0; j < batchSize; j++ {
								sequence := uint32(first + j)
								select {
								case socket.inbound <- SocketFrame(sequence, sequence*320, []byte{20, byte(sequence + 1)}):
								case <-ctx.Done():
									return
								}
							}
						}
					}()
					deadline := time.NewTimer(3 * time.Second)
					defer deadline.Stop()
					// Deterministic tests above require every healthy source slot. This
					// real-timer integration also runs under race/build CPU contention: a
					// genuinely missed device slot must be dropped, never replayed later.
					observed := map[byte]int{}
					var last byte
					sentDone := sent
					var drained <-chan time.Time
					var drainTimer *time.Timer
					defer func() {
						if drainTimer != nil {
							drainTimer.Stop()
						}
					}()
				observe:
					for {
						select {
						case pcm := <-opener.endpoint.writes:
							if allBytes(pcm, 0) {
								continue
							}
							marker := pcm[0]
							if !allBytes(pcm, marker) || marker < last || marker < 1 || marker > 40 {
								t.Fatalf("out-of-order/mixed source PCM marker%d after%d", marker, last)
							}
							observed[marker]++
							last = marker
							if observed[marker] > int(defaultFramePeriod/period) {
								t.Fatalf("replayed source frame%d", marker)
							}
						case <-sentDone:
							sentDone = nil
							drainTimer = time.NewTimer(250 * time.Millisecond)
							drained = drainTimer.C
						case <-drained:
							break observe
						case <-deadline.C:
							t.Fatalf("PCM stalled: %+v", session.Statistics())
						case <-session.Done():
							t.Fatalf("media ended: %v", session.Err())
						}
					}
					stats := session.Statistics()
					if stats.ReceivedPackets != 40 {
						t.Fatalf("transport lost source frames: %+v", stats)
					}
					if uint64(40-len(observed)) > stats.DroppedPackets {
						t.Fatalf("unclassified lost source frames: observed%d stats%+v", len(observed), stats)
					}
					if stats.PlayoutMissedTicks == 0 && (len(observed) != 40 || stats.DroppedPackets != 0) {
						t.Fatalf("healthy cadence lost source frames: observed%d stats%+v", len(observed), stats)
					}

					if err := session.Close(context.Background()); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestSocketPlayoutSmallClockDriftDoesNotInsertHardwareFrame(t *testing.T) {
	session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
	base := time.Unix(1000, 0)
	// A100ppm input-clock correction puts the next source slot10us after the
	// hardware tick. Quantization must not defer it by an entire20ms frame.
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 1}, generation: 1, playAt: base.Add(40*time.Millisecond + 10*time.Microsecond)})
	if got := readSocketPlayout(t, playout, base.Add(20*time.Millisecond)); got != 0 {
		t.Fatal("prebuffer skipped a whole frame")
	}
	if got := readSocketPlayout(t, playout, base.Add(40*time.Millisecond)); got != 1 {
		t.Fatal("small drift inserted a silent hardware frame")
	}
}

func TestSocketPlayoutHardwarePhaseAndSlowClockDrift(t *testing.T) {
	for _, phase := range []time.Duration{time.Millisecond, 9 * time.Millisecond, 11 * time.Millisecond} {
		for _, drift := range []time.Duration{-2 * time.Microsecond, 2 * time.Microsecond} {
			for _, batch := range []int{1, 5} {
				t.Run(fmt.Sprintf("phase%s/drift%s/batch%d", phase, drift, batch), func(t *testing.T) {
					session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
					base := time.Unix(1000, 0)
					var clock socketReceiveClock
					nextSource := uint32(0)
					nextPCM := uint32(0)
					started := false
					silentAfterStart := 0
					// Thirty simulated seconds cover300 drift windows and a
					// hardware quantization crossing near the half-tick phase.
					for tick := 0; tick < 1500; tick++ {
						now := base.Add(phase + time.Duration(tick)*20*time.Millisecond)
						for {
							arrival := base.Add(time.Duration(nextSource) * (20*time.Millisecond + drift))
							if arrival.After(now) {
								break
							}
							for j := 0; j < batch; j++ {
								if !enqueueSocketClock(t, session, &clock, nextSource, arrival) {
									t.Fatal("slow drift rejected healthy audio")
								}
								nextSource++
							}
						}
						marker := readSocketPlayout(t, playout, now)
						if marker == 0 {
							if started {
								silentAfterStart++
							}
							continue
						}
						started = true
						if marker != byte(nextPCM%250+1) {
							t.Fatalf("source sequence collapsed: marker%d, next%d", marker, nextPCM)
						}
						nextPCM++
					}
					if drops := session.stats.droppedPackets.Load(); drops != 0 {
						t.Fatalf("phase/drift dropped%d", drops)
					}
					// A100ppm mismatch can cross one integer hardware-slot
					// boundary, but must never inject a gap every100ms window.
					if silentAfterStart > 1 {
						t.Fatalf("window drift created%d silent slots", silentAfterStart)
					}
					if playout.offset != 0 || playout.starving {
						t.Fatal("hardware phase manufactured a rebuffer epoch")
					}
				})
			}
		}
	}
}
