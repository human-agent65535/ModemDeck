// Package calltest provides temporary, device-scoped calls without modem access.
package calltest

import (
	"context"
	"embed"
	"encoding/binary"
	"io"
	"math"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
)

// Spoken guidance, a beep, four seconds of capture, a playback announcement,
// four seconds of playback and a pause all use the production PCM endpoint.
// Only the short captured clip is mutable; it is cleared on every cycle/end.
//
//go:embed prompts/*.pcm
var prompts embed.FS

type AudioStatus struct {
	Phase            string `json:"phase"`
	RemainingMS      int    `json:"remaining_ms"`
	CapturedFrames   int    `json:"captured_frames"`
	CapturedDBFS     int    `json:"captured_dbfs"`
	CapturedPeakDBFS int    `json:"captured_peak_dbfs"`
	callmedia.AudioStatistics
}

type audioEndpoint struct {
	mu             sync.Mutex
	started        time.Time
	ticker         *time.Ticker
	done           chan struct{}
	closed         bool
	cycle          int64
	clip           []byte
	guide          []byte
	playbackPrompt []byte
	capturedFrames int
	sumSquares     float64
	samples        int
	peak           float64
}

func newAudioEndpoint(language ...string) *audioEndpoint {
	locale := "en"
	if len(language) > 0 && language[0] == "zh" {
		locale = "zh"
	}
	guide, _ := prompts.ReadFile("prompts/" + locale + "-guide.pcm")
	playback, _ := prompts.ReadFile("prompts/" + locale + "-playback.pcm")
	return &audioEndpoint{done: make(chan struct{}), cycle: -1, clip: make([]byte, 4*16000*2), guide: guide, playbackPrompt: playback}
}

func (e *audioEndpoint) boundaries() (guide, tone, capture, prompt, playback, end int64) {
	guide = int64(len(e.guide) / e.Format().FrameBytes())
	tone = guide + 25
	capture = tone + 200
	prompt = capture + int64(len(e.playbackPrompt)/e.Format().FrameBytes())
	playback = prompt + 200
	end = playback + 50
	return
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
	_, _, _, _, _, end := e.boundaries()
	if cycle := frame / end; cycle != e.cycle {
		clear(e.clip)
		e.cycle = cycle
		e.capturedFrames = 0
		e.sumSquares = 0
		e.samples = 0
		e.peak = 0
	}
	return frame % end
}
func (e *audioEndpoint) Phase() string { return e.Snapshot().Phase }

func (e *audioEndpoint) Snapshot() AudioStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	status := AudioStatus{Phase: "connecting", CapturedDBFS: -96, CapturedPeakDBFS: -96}
	if e.closed {
		status.Phase = "completed"
		return status
	}
	if e.started.IsZero() {
		return status
	}
	guide, tone, capture, prompt, playback, end := e.boundaries()
	frame := int64(time.Since(e.started)/e.Format().FrameDuration) % end
	boundaries := []struct {
		end   int64
		phase string
	}{{guide, "guide"}, {tone, "tone"}, {capture, "speak"}, {prompt, "playback_prompt"}, {playback, "playback"}, {end, "pause"}}
	for _, b := range boundaries {
		if frame < b.end {
			status.Phase = b.phase
			status.RemainingMS = int(b.end-frame) * 20
			break
		}
	}
	status.CapturedFrames = e.capturedFrames
	if e.samples > 0 {
		status.CapturedDBFS = toDBFS(math.Sqrt(e.sumSquares / float64(e.samples)))
	}
	status.CapturedPeakDBFS = toDBFS(e.peak)
	return status
}

func toDBFS(value float64) int {
	if value <= 0 {
		return -96
	}
	return int(math.Max(-96, math.Min(0, math.Round(20*math.Log10(value/32768)))))
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
	guide, tone, capture, prompt, playback, _ := e.boundaries()
	if frame < guide {
		copy(output, e.guide[int(frame)*len(output):])
	} else if frame < tone {
		for i := 0; i < len(output)/2; i++ {
			sample := int16(3500 * math.Sin(2*math.Pi*440*float64(frame*320+int64(i))/16000))
			binary.LittleEndian.PutUint16(output[i*2:], uint16(sample))
		}
	} else if frame >= capture && frame < prompt {
		copy(output, e.playbackPrompt[int(frame-capture)*len(output):])
	} else if frame >= prompt && frame < playback {
		copy(output, e.clip[int(frame-prompt)*len(output):])
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
	_, tone, capture, _, _, _ := e.boundaries()
	if frame >= tone && frame < capture {
		copy(e.clip[int(frame-tone)*len(input):], input)
		e.capturedFrames++
		for i := 0; i < len(input); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(input[i:])))
			e.sumSquares += v * v
			e.samples++
			e.peak = math.Max(e.peak, math.Abs(v))
		}
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
