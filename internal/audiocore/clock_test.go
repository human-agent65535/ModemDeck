//go:build cgo

package audiocore

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestClockLongCallSequenceTimestampWrapAndSourceSkips(t *testing.T) {
	var c Clock
	seq, ts := uint32(0xfffffff0), uint32(0xffffff00)
	elapsed := time.Duration(0)
	for i := 0; i < 100; i++ {
		r, err := c.Accept(seq, ts, elapsed)
		if err != nil || !r.Accepted || r.Generation != 1 || r.PlayAt != elapsed+40*time.Millisecond || r.SourceSamples != float64(i)*1000000*320 {
			t.Fatalf("step%d: %+v %v", i, r, err)
		}
		// Multiple timestamp wraps and sequence wrap over a23-day call, with
		// intentional gaps. Source progress is not reconstructed from timestamp
		// alone, nor truncated to32 bits or an individual window's frame count.
		seq += 1000000
		ts += 320000000
		elapsed += 20000 * time.Second
	}
}

func TestClockInvalidPacketDoesNotMutateState(t *testing.T) {
	invalid := []struct{ seq, ts uint32 }{{7, 320}, {6, 320}, {8, 321}, {0x80000007, 320}}
	for _, p := range invalid {
		var c Clock
		if _, err := c.Accept(7, 0, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Accept(p.seq, p.ts, 20*time.Millisecond); !errors.Is(err, ErrInvalidClock) {
			t.Fatalf("accepted malformed %v", p)
		}
		r, err := c.Accept(8, 320, 20*time.Millisecond)
		if err != nil || !r.Accepted || r.PlayAt != 60*time.Millisecond || r.Generation != 1 || r.SourceSamples != 320 {
			t.Fatalf("invalid packet changed source clock: %+v %v", r, err)
		}
	}
}

func TestClockMonotonicEpochOffsetDoesNotChangeFreshness(t *testing.T) {
	for _, offset := range []time.Duration{-2 * time.Hour, 0, 2 * time.Hour} {
		var c Clock
		drops := 0
		for i := 0; i < 60; i++ {
			arrival := time.Duration(i/5)*100*time.Millisecond + offset
			if i >= 20 {
				arrival += 130 * time.Millisecond
			}
			r, err := c.Accept(uint32(i), uint32(i*320), arrival)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Accepted {
				drops++
			}
			wantGen := uint64(1)
			wantPlay := time.Duration(i)*20*time.Millisecond + offset + 40*time.Millisecond
			if i >= 35 {
				wantGen = 2
				wantPlay += 130 * time.Millisecond
			}
			if r.Generation != wantGen || (r.Accepted && r.PlayAt != wantPlay) {
				t.Fatalf("offset%s seq%d: %+v", offset, i, r)
			}
		}
		if drops != 6 {
			t.Fatalf("offset changed partial-batch recovery: %d", drops)
		}
	}
}

func TestSharedBudgetAndOriginalDeadlineBoundaries(t *testing.T) {
	if FrameDuration != 20*time.Millisecond || Prebuffer != 40*time.Millisecond || MaxAge != 100*time.Millisecond || FrameSamples != 320 || QueueCapacity != 7 {
		t.Fatal("shared transport budgets changed")
	}
	for _, age := range []time.Duration{99 * time.Millisecond, 100 * time.Millisecond, 100*time.Millisecond + time.Microsecond} {
		if Expired(time.Second, time.Second+age) != (age > 100*time.Millisecond) {
			t.Fatal("expiry boundary", age)
		}
	}
	if start := PlaybackStart(time.Second, 2*time.Second); start != 2040*time.Millisecond {
		t.Fatal("rebuffer prefill", start)
	}
	if start := PlaybackStart(3*time.Second, 2*time.Second); start != 3*time.Second {
		t.Fatal("future source slot collapsed", start)
	}
}

func TestClockNonFiniteAndExtremeTimesAreInvalidWithoutMutation(t *testing.T) {
	for _, seconds := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e12 + 1, -1e12 - 1} {
		var c Clock
		if _, err := c.Accept(0, 0, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := c.acceptSeconds(1, 320, seconds); !errors.Is(err, ErrInvalidClock) {
			t.Fatalf("time%v was accepted", seconds)
		}
		r, err := c.Accept(1, 320, 20*time.Millisecond)
		if err != nil || !r.Accepted || r.PlayAt != 60*time.Millisecond || r.Generation != 1 || r.SourceSamples != 320 {
			t.Fatalf("invalid time mutated clock: %+v %v", r, err)
		}
	}
}

func TestSourceSamplesAdvanceOnStalePacketsAndSurviveReanchor(t *testing.T) {
	var c Clock
	stale, reanchored := false, false
	for i := 0; i < 60; i++ {
		arrival := time.Duration(i/5) * 100 * time.Millisecond
		if i >= 20 {
			arrival += 130 * time.Millisecond
		}
		r, err := c.Accept(uint32(i), uint32(i*320), arrival)
		if err != nil || r.SourceSamples != float64(i*320) {
			t.Fatalf("seq%d source progress changed: %+v %v", i, r, err)
		}
		stale = stale || !r.Accepted
		reanchored = reanchored || r.Generation == 2
		if _, err := c.Accept(uint32(i), uint32(i*320), arrival); !errors.Is(err, ErrInvalidClock) {
			t.Fatal("duplicate was accepted")
		}
	}
	if !stale || !reanchored {
		t.Fatal("fixture did not exercise stale receipt and reanchor")
	}
	c.Reset()
	r, err := c.Accept(100, 32000, time.Second)
	if err != nil || !r.Accepted || r.SourceSamples != 0 || r.Generation != 1 {
		t.Fatal("explicit reset failed", r, err)
	}
}

func TestSourceSlotsPreserveWrapGapsAndFixedEpoch(t *testing.T) {
	epoch := 40 * time.Millisecond
	for _, samples := range []uint64{0, 320, 960, 1 << 32, 1<<32 + 320, 100000000000} {
		// Integer quotient/remainder supplies an independent expectation without
		// overflowing nanoseconds or reconstructing a wrapped32-bit timestamp.
		seconds, remainder := samples/16000, samples%16000
		want := epoch + time.Duration(seconds)*time.Second + time.Duration(remainder*1000000/16000)*time.Microsecond
		got := SourceSlot(epoch, float64(samples))
		if got != want {
			t.Fatalf("source%d slot%s want%s", samples, got, want)
		}
	}
	if SourceSlot(epoch, 960)-SourceSlot(epoch, 320) != 40*time.Millisecond {
		t.Fatal("source omission collapsed")
	}
	if SourceSlot(epoch, 1<<32+320)-SourceSlot(epoch, 1<<32) != 20*time.Millisecond {
		t.Fatal("sample clock wrap changed device cadence")
	}
}
