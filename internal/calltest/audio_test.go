package calltest

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func TestGuidanceCaptureAndPlaybackLevels(t *testing.T) {
	for _, language := range []string{"zh", "en"} {
		e := newAudioEndpoint(language)
		guide, tone, capture, prompt, playback, end := e.boundaries()
		if guide < 25 || prompt-capture < 25 || len(e.guide)%640 != 0 || len(e.playbackPrompt)%640 != 0 {
			t.Fatal("missing or unaligned spoken prompts")
		}
		pcm := make([]byte, 640)
		for i := 0; i < len(pcm); i += 2 {
			binary.LittleEndian.PutUint16(pcm[i:], 3277)
		}
		for _, stage := range []struct {
			frame int64
			phase string
		}{{1, "guide"}, {guide + 1, "tone"}, {tone + 1, "speak"}, {capture + 1, "playback_prompt"}, {prompt + 1, "playback"}, {playback + 1, "pause"}} {
			e.started = time.Now().Add(-time.Duration(stage.frame) * 20 * time.Millisecond)
			if got := e.Snapshot(); got.Phase != stage.phase || got.RemainingMS <= 0 {
				t.Fatalf("phase %+v", got)
			}
			if err := e.WritePCM(context.Background(), pcm); err != nil {
				t.Fatal(err)
			}
		}
		status := e.Snapshot()
		if status.CapturedFrames != 1 || status.CapturedDBFS != -20 || status.CapturedPeakDBFS != -20 {
			t.Fatalf("capture: %+v", status)
		}
		e.started = time.Now().Add(-time.Duration(end+1) * 20 * time.Millisecond)
		_ = e.WritePCM(context.Background(), pcm)
		if e.Snapshot().CapturedFrames != 0 {
			t.Fatal("new cycle retained old recording")
		}
		_ = e.Close()
		if len(e.clip) != 0 || e.Snapshot().Phase != "completed" {
			t.Fatal("hangup retained audio")
		}
	}
}

func TestAudioStatusRetainsFinalCaptureMetricsWithoutRetainingAudio(t *testing.T) {
	endpoint := newAudioEndpoint()
	endpoint.mu.Lock()
	endpoint.capturedFrames = 200
	endpoint.samples = 320
	endpoint.sumSquares = float64(3277 * 3277 * 320)
	endpoint.peak = 3277
	endpoint.mu.Unlock()
	if err := endpoint.Close(); err != nil {
		t.Fatal(err)
	}
	final := endpoint.Snapshot()
	if final.Phase != "completed" || final.CapturedFrames != 200 || final.CapturedDBFS != -20 || final.CapturedPeakDBFS != -20 {
		t.Fatalf("lost final capture statistics: %+v", final)
	}
	if len(endpoint.clip) != 0 {
		t.Fatal("completed test retained captured audio")
	}
}
