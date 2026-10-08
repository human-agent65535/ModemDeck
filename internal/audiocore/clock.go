//go:build cgo

// Package audiocore adapts the canonical portable audio clock. It owns no
// transport, device, or queue policy; all source-clock decisions remain in C.
package audiocore

/*
#include "audio_core.h"
*/
import "C"

import (
	"errors"
	"math"
	"time"
)

var ErrInvalidClock = errors.New("invalid audio source clock")
var ErrUnavailable = errors.New("portable audio core requires CGO")

var (
	FrameDuration = secondsDuration(float64(C.md_audio_frame_seconds()))
	Prebuffer     = secondsDuration(float64(C.md_audio_prebuffer_seconds()))
	MaxAge        = secondsDuration(float64(C.md_audio_max_age_seconds()))
	QueueCapacity = int(C.md_audio_queue_capacity())
	FrameSamples  = uint32(C.md_audio_frame_samples())
)

const Available = true

type Clock struct{ state C.md_audio_clock }
type Result struct {
	Accepted      bool
	PlayAt, Age   time.Duration
	Generation    uint64
	SourceSamples float64
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(math.Round(seconds*1e6)) * time.Microsecond
}

// Accept receives time relative to an adapter's monotonic epoch. Keeping the
// epoch outside C preserves time.Time's monotonic component and microsecond
// precision even when the host's absolute wall clock is far from Unix zero.
func (c *Clock) Accept(sequence, timestamp uint32, elapsed time.Duration) (Result, error) {
	return c.acceptSeconds(sequence, timestamp, elapsed.Seconds())
}

func (c *Clock) acceptSeconds(sequence, timestamp uint32, seconds float64) (Result, error) {
	status := C.md_audio_clock_accept(&c.state, C.uint32_t(sequence), C.uint32_t(timestamp), C.double(seconds))
	if status < 0 {
		return Result{}, ErrInvalidClock
	}
	return Result{Accepted: status == 1, PlayAt: secondsDuration(float64(C.md_audio_clock_play_at(&c.state))), Age: secondsDuration(float64(C.md_audio_clock_age(&c.state))), Generation: uint64(C.md_audio_clock_generation(&c.state)), SourceSamples: float64(C.md_audio_clock_source_samples(&c.state))}, nil
}
func (c *Clock) Reset() { C.md_audio_clock_reset(&c.state) }
func Expired(source, now time.Duration) bool {
	return C.md_audio_frame_expired(C.double(source.Seconds()), C.double(now.Seconds())) != 0
}
func PlaybackStart(source, now time.Duration) time.Duration {
	return secondsDuration(float64(C.md_audio_playback_start(C.double(source.Seconds()), C.double(now.Seconds()))))
}

// SourceSlot maps unwrapped media progress into a fixed device epoch. Packet
// timestamp wrap and source-clock drift cannot move or collapse its slots.
func SourceSlot(epoch time.Duration, relativeSourceSamples float64) time.Duration {
	return secondsDuration(float64(C.md_audio_source_slot(C.double(epoch.Seconds()), C.double(relativeSourceSamples))))
}
