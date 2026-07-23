package callmedia

import (
	"errors"
	"testing"
	"time"
)

func TestPCMFormatIsExplicitAndBounded(t *testing.T) {
	for _, sampleRate := range []int{8000, 16000} {
		format := testFormat(sampleRate)
		if err := format.Validate(); err != nil {
			t.Fatalf("validate %d Hz: %v", sampleRate, err)
		}
		if got, want := format.FrameSamples(), sampleRate/50; got != want {
			t.Fatalf("frame samples = %d, want %d", got, want)
		}
		if got, want := format.FrameBytes(), sampleRate/50*2; got != want {
			t.Fatalf("frame bytes = %d, want %d", got, want)
		}
		if got := format.RTPFrameSamples(); got != 960 {
			t.Fatalf("RTP frame samples = %d, want 960", got)
		}
	}

	invalid := []PCMFormat{
		{Encoding: "s16be", SampleRate: 8000, Channels: 1, FrameDuration: 20 * time.Millisecond},
		{Encoding: PCMEncodingS16LE, SampleRate: 48000, Channels: 1, FrameDuration: 20 * time.Millisecond},
		{Encoding: PCMEncodingS16LE, SampleRate: 8000, Channels: 2, FrameDuration: 20 * time.Millisecond},
		{Encoding: PCMEncodingS16LE, SampleRate: 8000, Channels: 1, FrameDuration: 30 * time.Millisecond},
	}
	for _, format := range invalid {
		if !errors.Is(format.Validate(), ErrInvalidArgument) {
			t.Fatalf("expected invalid format error for %+v", format)
		}
	}
}

func TestNormalizeActiveCallRejectsInactiveAndControlCharacters(t *testing.T) {
	if _, err := normalizeActiveCall(ActiveCall{ID: "call-1"}); !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("inactive call error = %v", err)
	}
	if _, err := normalizeActiveCall(ActiveCall{
		ID:    "call\n1",
		State: CallStateActive,
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("control-character call ID error = %v", err)
	}
}
