// Package calltest provides temporary, device-scoped calls without modem access.
package calltest

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
)

// A ten-second cycle plays a tone, captures four seconds, plays them back,
// then pauses. The clip is bounded, kept only in memory, and cleared each cycle.
type audioEndpoint struct {
	mu      sync.Mutex
	started time.Time
	ticker  *time.Ticker
	done    chan struct{}
	closed  bool
	cycle   int64
	clip    []byte
}

func newAudioEndpoint() *audioEndpoint {
	return &audioEndpoint{done: make(chan struct{}), cycle: -1, clip: make([]byte, 4*16000*2)}
}

func (e *audioEndpoint) Open(context.Context, callmedia.ActiveCall) (callmedia.MediaEndpoint, error) {
	return e, nil
}
func (e *audioEndpoint) Format() callmedia.PCMFormat {
	return callmedia.PCMFormat{Encoding: callmedia.PCMEncodingS16LE, SampleRate: 16000, Channels: 1, FrameDuration: 20 * time.Millisecond}
}
func (e *audioEndpoint) Start(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return io.EOF
	}
	if e.started.IsZero() {
		e.started = time.Now()
		e.ticker = time.NewTicker(e.Format().FrameDuration)
	}
	return nil
}
func (e *audioEndpoint) frameLocked() int64 {
	frame := int64(time.Since(e.started) / e.Format().FrameDuration)
	if cycle := frame / 500; cycle != e.cycle {
		clear(e.clip)
		e.cycle = cycle
	}
	return frame % 500
}
func (e *audioEndpoint) Phase() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return "completed"
	}
	if e.started.IsZero() {
		return "connecting"
	}
	frame := int64(time.Since(e.started)/e.Format().FrameDuration) % 500
	switch {
	case frame < 50:
		return "tone"
	case frame < 250:
		return "speak"
	case frame < 450:
		return "playback"
	default:
		return "pause"
	}
}
func (e *audioEndpoint) ReadPCM(ctx context.Context, output []byte) error {
	if len(output) != e.Format().FrameBytes() {
		return callmedia.ErrInvalidArgument
	}
	e.mu.Lock()
	ticker := e.ticker
	e.mu.Unlock()
	if ticker == nil {
		return io.EOF
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.done:
		return io.EOF
	case <-ticker.C:
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return io.EOF
	}
	clear(output)
	frame := e.frameLocked()
	if frame < 50 {
		for i := 0; i < len(output)/2; i++ {
			sample := int16(3500 * math.Sin(2*math.Pi*440*float64(frame*320+int64(i))/16000))
			binary.LittleEndian.PutUint16(output[i*2:], uint16(sample))
		}
	} else if frame >= 250 && frame < 450 {
		copy(output, e.clip[int(frame-250)*len(output):])
	}
	return nil
}
func (e *audioEndpoint) WritePCM(ctx context.Context, input []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(input) != e.Format().FrameBytes() {
		return callmedia.ErrInvalidArgument
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return io.EOF
	}
	if e.started.IsZero() {
		return nil
	}
	frame := e.frameLocked()
	if frame >= 50 && frame < 250 {
		copy(e.clip[int(frame-50)*len(input):], input)
	}
	return nil
}
func (e *audioEndpoint) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.closed {
		e.closed = true
		if e.ticker != nil {
			e.ticker.Stop()
		}
		clear(e.clip)
		e.clip = nil
		close(e.done)
	}
	return nil
}
