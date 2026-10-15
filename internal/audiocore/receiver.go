//go:build cgo

package audiocore

/*
#cgo CFLAGS: -I${SRCDIR}/native/include
#cgo LDFLAGS: -lmd_audio_core -lstdc++ -lm -pthread -Wl,--gc-sections
#include "md_neteq.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

var ErrBackpressure = errors.New("audio ingress resource limit reached")
var ErrInvalidPacket = errors.New("invalid NetEq packet")
var ErrReceiver = errors.New("NetEq receiver failed")

const Available = true

var IngressCapacity = int(C.md_audio_ingress_capacity())
var SendCapacity = int(C.md_audio_send_capacity())

// Receiver owns NetEq's decoder/jitter buffer and official48k resampler.
// Enqueue has one producer, Pull has one consumer; they may run concurrently.
// Close waits for in-flight calls. No Go PCM queue or audio-age policy exists.
type Receiver struct {
	mu    sync.RWMutex
	state *C.md_neteq
	rate  int
	final ReceiverStats
}
type ReceiverStats struct {
	ConcealedSamples, ConcealmentEvents, InsertedSamples, RemovedSamples          uint64
	PacketsDiscarded, PacketsReceived, EmittedCount, DelayMSSum, TargetDelayMSSum uint64
	TargetDelayMS, CurrentDelayMS, InternalSampleRate                             uint32
	PlayoutTimestamp, PlayoutTimestampValid, FrameTimestamp, SpeechType           uint32
	RealOutputSamples, LastRealRenderUS, RenderErrors                             uint64
	IngressQueued                                                                 uint32
}

func NewReceiver(rate int) (*Receiver, error) {
	p := C.md_neteq_create(C.int(rate))
	if p == nil {
		return nil, fmt.Errorf("create NetEq rate%d: %w", rate, ErrReceiver)
	}
	return &Receiver{state: p, rate: rate}, nil
}
func (r *Receiver) Enqueue(payload []byte, sequence uint16, timestamp uint32, arrival time.Duration) error {
	if arrival < 0 {
		return ErrInvalidPacket
	}
	if r == nil || len(payload) == 0 {
		return ErrReceiver
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state == nil {
		return ErrReceiver
	}
	rc := C.md_neteq_enqueue(r.state, (*C.uint8_t)(unsafe.Pointer(&payload[0])), C.size_t(len(payload)), C.uint16_t(sequence), C.uint32_t(timestamp), C.int64_t(arrival.Microseconds()))
	if rc == C.MD_AUDIO_BACKPRESSURE {
		return ErrBackpressure
	}
	if rc == C.MD_AUDIO_INVALID {
		return ErrInvalidPacket
	}
	if rc != 0 {
		return fmt.Errorf("enqueue NetEq(%d): %w", rc, ErrReceiver)
	}
	return nil
}
func (r *Receiver) Pull(now time.Duration, pcm []int16) error {
	if now < 0 {
		return ErrReceiver
	}
	if r == nil || len(pcm) == 0 {
		return ErrReceiver
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state == nil {
		return ErrReceiver
	}
	n := C.md_neteq_pull(r.state, C.int64_t(now.Microseconds()), (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.size_t(len(pcm)))
	if int(n) != r.rate/100 {
		return fmt.Errorf("pull NetEq(%d): %w", n, ErrReceiver)
	}
	return nil
}
func (r *Receiver) Statistics() ReceiverStats {
	if r == nil {
		return ReceiverStats{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.statisticsLocked()
}
func (r *Receiver) statisticsLocked() ReceiverStats {
	if r.state == nil {
		return r.final
	}
	var s C.md_neteq_stats
	if C.md_neteq_get_stats(r.state, &s) != 0 {
		return ReceiverStats{}
	}
	return ReceiverStats{uint64(s.concealed_samples), uint64(s.concealment_events), uint64(s.inserted_samples), uint64(s.removed_samples), uint64(s.packets_discarded), uint64(s.packets_received), uint64(s.emitted_count), uint64(s.delay_ms_sum), uint64(s.target_delay_ms_sum), uint32(s.target_delay_ms), uint32(s.current_delay_ms), uint32(s.internal_sample_rate), uint32(s.playout_timestamp48k), uint32(s.playout_timestamp_valid), uint32(s.frame_timestamp), uint32(s.speech_type), uint64(s.real_output_samples), uint64(s.last_real_render_us), uint64(s.render_errors), uint32(s.ingress_queued)}
}
func (r *Receiver) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != nil {
		r.final = r.statisticsLocked()
		C.md_neteq_destroy(r.state)
		r.state = nil
	}
}

func (r *Receiver) Render(now time.Duration, pcm []float32) error {
	if r == nil || len(pcm) == 0 {
		return ErrReceiver
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state == nil {
		return ErrReceiver
	}
	n := C.md_neteq_render_float(r.state, C.int64_t(renderMicros(now)), (*C.float)(unsafe.Pointer(&pcm[0])), C.size_t(len(pcm)))
	if int(n) != len(pcm) {
		return fmt.Errorf("render NetEq(%d): %w", n, ErrReceiver)
	}
	return nil
}

// ResetRender is owner-only after stopping device callbacks.
func (r *Receiver) ResetRender() error {
	if r == nil {
		return ErrReceiver
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state == nil || C.md_neteq_reset_render(r.state) != 0 {
		return ErrReceiver
	}
	return nil
}

func renderMicros(now time.Duration) int64 {
	if now < 0 {
		return -1
	}
	return now.Microseconds()
}
