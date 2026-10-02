package callmedia

import (
	"encoding/binary"
	"testing"
)

func TestPCMLevelsDistinguishSilenceQuietAndNormalInput(t *testing.T) {
	pcm := make([]byte, 640)
	for _, value := range []struct {
		sample   int16
		expected int
	}{{0, -96}, {32, -60}, {3277, -20}, {32767, 0}} {
		for i := 0; i < len(pcm); i += 2 {
			binary.LittleEndian.PutUint16(pcm[i:], uint16(value.sample))
		}
		rms, peak := PCMLevels(pcm)
		if rms != value.expected || peak != value.expected {
			t.Fatalf("%d => %d/%d", value.sample, rms, peak)
		}
	}
}
