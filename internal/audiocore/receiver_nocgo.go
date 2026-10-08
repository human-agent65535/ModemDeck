//go:build !cgo

package audiocore

import (
	"errors"
	"time"
)

var ErrBackpressure = errors.New("audio ingress resource limit reached")
var ErrInvalidPacket = errors.New("invalid NetEq packet")
var ErrReceiver = errors.New("NetEq receiver failed")

const Available = false
const IngressCapacity = 200
const SendCapacity = 100

type Receiver struct{}
type ReceiverStats struct {
	ConcealedSamples, ConcealmentEvents, InsertedSamples, RemovedSamples          uint64
	PacketsDiscarded, PacketsReceived, EmittedCount, DelayMSSum, TargetDelayMSSum uint64
	TargetDelayMS, CurrentDelayMS, InternalSampleRate                             uint32
	PlayoutTimestamp, PlayoutTimestampValid, FrameTimestamp, SpeechType           uint32
	RealOutputSamples, LastRealRenderUS, RenderErrors                             uint64
	IngressQueued                                                                 uint32
}

func NewReceiver(int) (*Receiver, error) { return nil, ErrUnavailable }

var ErrUnavailable = errors.New("NetEq audio requires CGO")

func (*Receiver) Enqueue([]byte, uint16, uint32, time.Duration) error { return ErrUnavailable }
func (*Receiver) Pull(time.Duration, []int16) error                   { return ErrUnavailable }
func (*Receiver) Statistics() ReceiverStats                           { return ReceiverStats{} }
func (*Receiver) Close()                                              {}
func (*Receiver) Render(time.Duration, []float32) error               { return ErrUnavailable }

func (*Receiver) ResetRender() error { return ErrUnavailable }
