//go:build !cgo

package audiocore

import (
	"errors"
	"time"
)

var ErrInvalidClock = errors.New("invalid audio source clock")
var ErrUnavailable = errors.New("portable audio core requires CGO")

const Available = false

var FrameDuration, Prebuffer, MaxAge time.Duration
var QueueCapacity int
var FrameSamples uint32

type Clock struct{}
type Result struct {
	Accepted      bool
	PlayAt, Age   time.Duration
	Generation    uint64
	SourceSamples float64
}

func (*Clock) Accept(uint32, uint32, time.Duration) (Result, error) { return Result{}, ErrUnavailable }
func (*Clock) Reset()                                               {}
func Expired(time.Duration, time.Duration) bool                     { return true }
func PlaybackStart(time.Duration, time.Duration) time.Duration      { return 0 }

func SourceSlot(time.Duration, float64) time.Duration { return 0 }
