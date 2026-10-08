package callmedia

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSocketCompleteWindowsRecoverPartialLatencyBatches(t *testing.T) {
	for _, shift := range []int{110, 130, 150, 170, 190, 200} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			var clock socketReceiveClock
			base := time.Unix(1000, 0)
			dropped := 0
			for batch := 0; batch < 40; batch++ {
				arrival := base.Add(time.Duration(batch) * 100 * time.Millisecond)
				if batch >= 4 {
					arrival = arrival.Add(time.Duration(shift) * time.Millisecond)
				}
				for j := 0; j < 5; j++ {
					sequence := uint32(batch*5 + j)
					accepted, err := clock.accept(sequence, sequence*320, arrival)
					if err != nil {
						t.Fatal(err)
					}
					if !accepted {
						dropped++
					}
					if sequence >= 35 && (!accepted || clock.generation != 2) {
						t.Fatalf("seq%d persistent delay did not settle: accept%v gen%d", sequence, accepted, clock.generation)
					}
				}
			}
			want := 3 * int((shift-100+19)/20)
			if want > 15 {
				want = 15
			}
			if dropped != want {
				t.Fatalf("transient drops%d, want%d", dropped, want)
			}
		})
	}
}

func TestSocketTickerPhaseDoesNotManufactureBatchOverflow(t *testing.T) {
	for _, phase := range []time.Duration{10 * time.Microsecond, 100 * time.Microsecond, time.Millisecond, 9 * time.Millisecond} {
		t.Run(phase.String(), func(t *testing.T) {
			session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
			var clock socketReceiveClock
			base := time.Unix(1000, 0)
			for i := uint32(0); i < 5; i++ {
				enqueueSocketClock(t, session, &clock, i, base)
			}
			if _, err := playout.next(base, base.Add(phase)); err != nil {
				t.Fatal(err)
			}
			nextSequence := uint32(5)
			expected := byte(1)
			for tick := 1; tick < 100; tick++ {
				at := base.Add(time.Duration(tick) * 20 * time.Millisecond)
				if tick%5 == 0 {
					for j := 0; j < 5; j++ {
						enqueueSocketClock(t, session, &clock, nextSequence, at)
						nextSequence++
					}
				}
				pcm, err := playout.next(at, at.Add(phase))
				if err != nil {
					t.Fatal(err)
				}
				marker := pcm[0]
				if marker != 0 {
					if marker != expected {
						t.Fatalf("logical tick%d skipped source frame%d (got%d)", tick, expected, marker)
					}
					expected++
				}
			}
			if drops := session.stats.droppedPackets.Load(); drops != 0 {
				t.Fatalf("phase manufactured%d drops", drops)
			}
		})
	}
}

// Every transport/hardware combination uses the same source clock and queue.
// Wall wake jitter must not become an additional logical hardware period.
func TestSocketCadenceFormatsBatchesAndWakeJitter(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		for _, period := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
			for _, batch := range []int{1, 5} {
				for _, phase := range []time.Duration{0, 10 * time.Microsecond, time.Millisecond, 9 * time.Millisecond, 11 * time.Millisecond} {
					t.Run(fmt.Sprintf("%d/%s/batch%d/phase%s", rate, period, batch, phase), func(t *testing.T) {
						session, playout, _ := newSocketPlayoutForTest(t, period)
						session.hub.format.SampleRate = rate
						base := time.Unix(1000, 0)
						cadence := socketPlaybackCadence{next: base.Add(phase), period: period}
						var clock socketReceiveClock
						nextSource, nextPCM := uint32(0), uint32(0)
						for tick := 0; tick < int(2200*time.Millisecond/period); tick++ {
							// A changing wake phase exercises actual now independently of the grid.
							wall := base.Add(phase + time.Duration(tick)*period + time.Duration(tick%4)*300*time.Microsecond)
							for nextSource < 100 {
								arrival := base.Add(time.Duration(nextSource) * 20 * time.Millisecond)
								if arrival.After(wall) {
									break
								}
								for j := 0; j < batch; j++ {
									enqueueSocketClock(t, session, &clock, nextSource, arrival)
									nextSource++
								}
							}
							slot, skipped := cadence.advance(wall)
							if skipped != 0 {
								t.Fatal("small wake jitter skipped a hardware slot")
							}
							pcm, err := playout.next(slot, wall)
							if err != nil {
								t.Fatal(err)
							}
							marker := pcm[0]
							if marker == 0 {
								continue
							}
							want := byte(nextPCM/uint32(defaultFramePeriod/period)%250 + 1)
							if marker != want {
								t.Fatalf("tick%d marker%d want%d", tick, marker, want)
							}
							nextPCM++
						}
						if nextPCM != uint32(100*defaultFramePeriod/period) {
							t.Fatalf("played%d hardware frames", nextPCM)
						}
						if session.stats.droppedPackets.Load() != 0 {
							t.Fatalf("healthy cadence lost audio: %+v", session.Statistics())
						}
					})
				}
			}
		}
	}
}

func TestSocketMergedTCPBatchesLoseBoundedAudioThenResumeOriginalClock(t *testing.T) {
	for _, batch := range []int{10, 15} {
		for _, rate := range []int{8000, 16000} {
			for _, period := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
				t.Run(fmt.Sprintf("batch%d/%d/%s", batch, rate, period), func(t *testing.T) {
					session, playout, _ := newSocketPlayoutForTest(t, period)
					session.hub.format.SampleRate = rate
					var clock socketReceiveClock
					base := time.Unix(1000, 0)
					mergedFrames := batch * 10
					total := mergedFrames + 100
					next := 0
					lastMarker := byte(0)
					for tick := 0; tick < int((time.Duration(total)*20*time.Millisecond+200*time.Millisecond)/period); tick++ {
						at := base.Add(time.Duration(tick) * period)
						if next < total && (next < mergedFrames && tick%int(time.Duration(batch)*20*time.Millisecond/period) == 0 || next >= mergedFrames && time.Duration(tick)*period >= time.Duration(next)*20*time.Millisecond && tick%int(100*time.Millisecond/period) == 0) {
							count := batch
							if next >= mergedFrames {
								count = 5
							}
							for j := 0; j < count && next < total; j++ {
								enqueueSocketClock(t, session, &clock, uint32(next), at)
								next++
							}
						}
						pcm, err := playout.next(at, at)
						if err != nil {
							t.Fatal(err)
						}
						if pcm[0] != 0 {
							lastMarker = pcm[0]
						}
						if len(session.socket.incoming.packets) > 7 || playout.offset > socketLateBudget {
							t.Fatal("unbounded merged-batch recovery")
						}
					}
					stats := session.Statistics()
					if stats.DroppedSourceEarlyPackets != uint64((batch-6)*10) {
						t.Fatalf("wrong stale-TCP rejection: %+v", stats)
					}
					if stats.ClockReanchors != 0 || clock.generation != 1 {
						t.Fatal("fast backlog established a new source timeline")
					}
					if lastMarker != byte((total-1)%250+1) {
						t.Fatalf("normal cadence never resumed: marker%d want%d", lastMarker, byte((total-1)%250+1))
					}
					if stats.DroppedPackets != stats.DroppedSourceEarlyPackets {
						t.Fatalf("unexplained loss after normal cadence: %+v", stats)
					}
				})
			}
		}
	}
}

func TestSocketLogicalCadenceSkipsMissedSlotsWithoutExtendingDeadline(t *testing.T) {
	base := time.Unix(1000, 0)
	c := socketPlaybackCadence{next: base, period: 20 * time.Millisecond}
	for _, step := range []struct {
		wake, slot time.Duration
		skipped    uint64
	}{
		{10 * time.Microsecond, 0, 0}, {21 * time.Millisecond, 20 * time.Millisecond, 0}, {99 * time.Millisecond, 80 * time.Millisecond, 2}, {100 * time.Millisecond, 100 * time.Millisecond, 0},
	} {
		slot, missed := c.advance(base.Add(step.wake))
		if slot.Sub(base) != step.slot || missed != step.skipped {
			t.Fatalf("wake%s => slot%s skip%d", step.wake, slot.Sub(base), missed)
		}
	}
	session, playout, _ := newSocketPlayoutForTest(t, 20*time.Millisecond)
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 42}, generation: 1, playAt: base})
	pcm, err := playout.next(base, base.Add(101*time.Millisecond))
	if err != nil || pcm[0] != 0 || session.Statistics().DroppedPlayoutPackets != 1 {
		t.Fatal("logical slot renewed expired real-time audio")
	}
}

func TestSocketDropBreakdownSurvivesReconnectAndFinalLog(t *testing.T) {
	session, playout, _ := newSocketPlayoutForTest(t, 10*time.Millisecond)
	base := time.Unix(1000, 0)
	session.stats.recordSourceDrop(-101 * time.Millisecond)
	session.stats.recordSourceDrop(101 * time.Millisecond)
	for i := 0; i < 8; i++ {
		session.enqueueSocketPacket(socketPacket{payload: []byte{20, 1}, sequence: uint32(i), sourceSamples: float64(uint32(i)) * 320, generation: 1, playAt: base})
	}
	// One overflow, seven queued invalidated, then one decoded half invalidated.
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 2}, generation: 2, playAt: base})
	readSocketPlayout(t, playout, base)
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 3}, generation: 3, playAt: base.Add(300 * time.Millisecond)})
	readSocketPlayout(t, playout, base.Add(10*time.Millisecond))
	readSocketPlayout(t, playout, base.Add(401*time.Millisecond))
	session.enqueueSocketPacket(socketPacket{payload: []byte{20, 4}, generation: 3, playAt: base.Add(420 * time.Millisecond)})
	readSocketPlayout(t, playout, base.Add(500*time.Millisecond))
	session.stats.clockReanchors.Add(2)
	session.stats.playoutMissedTicks.Add(3)
	stats := session.Statistics()
	if stats.DroppedSourceEarlyPackets != 1 || stats.DroppedSourceLatePackets != 1 || stats.DroppedQueueOverflowPackets != 1 || stats.DroppedReanchorPackets != 8 || stats.DroppedPlayoutPackets != 1 || stats.DroppedRebufferPackets != 1 || stats.DroppedPackets != 13 {
		t.Fatalf("drop categories: %+v", stats)
	}
	next := &Session{socket: &socketAudio{}, baseStats: stats}
	next.stats.recordSourceDrop(101 * time.Millisecond)
	after := next.Statistics()
	if after.DroppedPackets != 14 || after.DroppedSourceLatePackets != 2 || after.ClockReanchors != 2 || after.PlayoutMissedTicks != 3 {
		t.Fatalf("reconnect reset telemetry: %+v", after)
	}
	var log bytes.Buffer
	AudioStatsLogger(slog.New(slog.NewJSONHandler(&log, nil)))("synthetic", after)
	var logged map[string]any
	if err := json.Unmarshal(log.Bytes(), &logged); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"dropped_source_early_packets", "dropped_source_late_packets", "dropped_queue_overflow_packets", "dropped_reanchor_packets", "dropped_playout_packets", "dropped_rebuffer_packets", "clock_reanchors", "playout_underruns", "playout_silence_frames", "playout_missed_ticks"} {
		if _, ok := logged[field]; !ok {
			t.Fatal("missing final log field " + field)
		}
	}
	if strings.Contains(log.String(), "payload") {
		t.Fatal("audio content in telemetry")
	}
}

func TestSocketEarlierArrivalPlateauRecoversWithoutReplayingFastBacklog(t *testing.T) {
	for _, reduction := range []time.Duration{110 * time.Millisecond, 130 * time.Millisecond, 150 * time.Millisecond, 170 * time.Millisecond, 190 * time.Millisecond, 200 * time.Millisecond} {
		t.Run(reduction.String(), func(t *testing.T) {
			var clock socketReceiveClock
			base := time.Unix(1000, 0)
			previous := base
			dropped := 0
			for batch := 0; batch < 40; batch++ {
				arrival := base.Add(200*time.Millisecond + time.Duration(batch)*100*time.Millisecond)
				if batch >= 4 {
					arrival = arrival.Add(-reduction)
				}
				// A falling network delay cannot make arrivals run backwards. The first
				// newly earlier batch catches up at the last arrival, then real cadence
				// resumes. This distinguishes a genuine new plateau from fast TCP replay.
				if arrival.Before(previous) {
					arrival = previous
				}
				previous = arrival
				for j := 0; j < 5; j++ {
					seq := uint32(batch*5 + j)
					accepted, err := clock.accept(seq, seq*320, arrival)
					if err != nil {
						t.Fatal(err)
					}
					if !accepted {
						dropped++
					}
					if seq >= 50 && (!accepted || clock.generation != 2) {
						t.Fatalf("earlier plateau never recovered: seq%d accept%v gen%d", seq, accepted, clock.generation)
					}
				}
			}
			if dropped > 25 {
				t.Fatalf("unbounded initial loss: %d", dropped)
			}
		})
	}
}

func TestSocketMissedHardwareSlotsNeverReplayOldSourceFrames(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		for _, period := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
			t.Run(fmt.Sprintf("%d/%s", rate, period), func(t *testing.T) {
				s, p, _ := newSocketPlayoutForTest(t, period)
				s.hub.format.SampleRate = rate
				base := time.Unix(1000, 0)
				for i := 0; i < 5; i++ {
					s.enqueueSocketPacket(socketPacket{payload: []byte{20, byte(i + 1)}, sequence: uint32(i), sourceSamples: float64(uint32(i)) * 320, generation: 1, playAt: base.Add(time.Duration(40+i*20) * time.Millisecond)})
				}
				readSocketPlayout(t, p, base)
				if got := readSocketPlayout(t, p, base.Add(40*time.Millisecond)); got != 1 {
					t.Fatal("first source frame", got)
				}
				if period == 10*time.Millisecond {
					readSocketPlayout(t, p, base.Add(50*time.Millisecond))
				}
				// Lost60/80ms slots are gone even though the100ms TTL has not expired.
				if got := readSocketPlayout(t, p, base.Add(100*time.Millisecond)); got != 4 {
					t.Fatalf("logical100ms rendered old marker%d; want source3 marker4", got)
				}
				if stats := s.Statistics(); stats.DroppedPackets != 2 || stats.DroppedPlayoutPackets != 2 {
					t.Fatalf("missed-slot packet accounting: %+v", stats)
				}
			})
		}
	}
}

func TestSocketTenMillisecondResidualUsesItsAbsoluteSlot(t *testing.T) {
	for _, first := range []time.Duration{40 * time.Millisecond, 50 * time.Millisecond} {
		t.Run(first.String(), func(t *testing.T) {
			s, p, _ := newSocketPlayoutForTest(t, 10*time.Millisecond)
			base := time.Unix(1000, 0)
			s.enqueueSocketPacket(socketPacket{payload: []byte{20, 1}, sequence: 0, sourceSamples: float64(0) * 320, generation: 1, playAt: base.Add(40 * time.Millisecond)})
			s.enqueueSocketPacket(socketPacket{payload: []byte{20, 2}, sequence: 1, sourceSamples: float64(1) * 320, generation: 1, playAt: base.Add(60 * time.Millisecond)})
			readSocketPlayout(t, p, base)
			// At50ms only the second10ms belongs to seq0. If40ms was rendered, a
			// subsequent60ms wake must discard its missed50ms residual instead.
			if got := readSocketPlayout(t, p, base.Add(first)); got != 1 {
				t.Fatal("current half missing", got)
			}
			if got := readSocketPlayout(t, p, base.Add(60*time.Millisecond)); got != 2 {
				t.Fatal("old half occupied source1 slot", got)
			}
			if got := readSocketPlayout(t, p, base.Add(70*time.Millisecond)); got != 2 {
				t.Fatal("new half missing", got)
			}
			stats := s.Statistics()
			if stats.DroppedPackets != 1 || stats.DroppedPlayoutPackets != 1 {
				t.Fatalf("partial packet must count once: %+v", stats)
			}
		})
	}
}

func TestSocketFixedPlaybackEpochUsesUnwrappedSourceProgress(t *testing.T) {
	s, p, _ := newSocketPlayoutForTest(t, 10*time.Millisecond)
	base := time.Unix(1000, 0)
	source := float64(uint64(1) << 40)
	s.enqueueSocketPacket(socketPacket{payload: []byte{20, 11}, sequence: ^uint32(0), generation: 1, playAt: base.Add(40 * time.Millisecond), sourceSamples: source})
	// Wrapped sequence and a true two-frame source omission. A fresh age-clock
	// drift correction cannot move the device's already established sample grid.
	s.enqueueSocketPacket(socketPacket{payload: []byte{20, 12}, sequence: 2, generation: 1, playAt: base.Add(100*time.Millisecond + 100*time.Microsecond), sourceSamples: source + 960})
	readSocketPlayout(t, p, base)
	for tick := 40; tick <= 110; tick += 10 {
		got := readSocketPlayout(t, p, base.Add(time.Duration(tick)*time.Millisecond))
		want := byte(0)
		if tick < 60 {
			want = 11
		}
		if tick >= 100 {
			want = 12
		}
		if got != want {
			t.Fatalf("slot%d marker%d want%d", tick, got, want)
		}
	}
	if s.Statistics().DroppedPackets != 0 {
		t.Fatal("wrap/drift caused artificial loss")
	}
}
