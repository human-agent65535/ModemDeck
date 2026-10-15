//go:build cgo

package callmedia

import (
	"math"
	"testing"
	"time"
)

func TestLibOpusEncodesNativeTelephonePCMAndProvidesPLC(t *testing.T) {
	factory, err := NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	for _, sampleRate := range []int{8000, 16000} {
		format := testFormat(sampleRate)
		codec, err := factory.New(format)
		if err != nil {
			t.Fatalf("new %d Hz codec: %v", sampleRate, err)
		}
		pcm := sinePCM(format, 440)
		encoded, err := codec.Encode(pcm)
		if err != nil {
			t.Fatalf("encode %d Hz: %v", sampleRate, err)
		}
		if len(encoded) == 0 || len(encoded) > maxOpusPayloadBytes {
			t.Fatalf("encoded %d Hz payload size = %d", sampleRate, len(encoded))
		}
		duration, err := codec.PacketDuration(encoded)
		if err != nil {
			t.Fatalf("packet duration %d Hz: %v", sampleRate, err)
		}
		if duration != format.FrameDuration {
			t.Fatalf("packet duration = %v, want %v", duration, format.FrameDuration)
		}
		decoded, err := codec.Decode(encoded)
		if err != nil {
			t.Fatalf("decode %d Hz: %v", sampleRate, err)
		}
		if len(decoded.PCM) != format.FrameBytes() {
			t.Fatalf("decoded %d Hz PCM bytes = %d, want %d", sampleRate, len(decoded.PCM), format.FrameBytes())
		}
		concealed, err := codec.Conceal(20 * time.Millisecond)
		if err != nil {
			t.Fatalf("conceal %d Hz: %v", sampleRate, err)
		}
		if len(concealed) != format.FrameBytes() {
			t.Fatalf("concealed %d Hz PCM bytes = %d, want %d", sampleRate, len(concealed), format.FrameBytes())
		}
		if err := codec.Close(); err != nil {
			t.Fatal(err)
		}
		if err := codec.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func sinePCM(format PCMFormat, frequency float64) []byte {
	pcm := make([]byte, format.FrameBytes())
	for sample := 0; sample < format.FrameSamples(); sample++ {
		value := int16(math.Sin(2*math.Pi*frequency*float64(sample)/float64(format.SampleRate)) * 12000)
		pcm[sample*2] = byte(value)
		pcm[sample*2+1] = byte(uint16(value) >> 8)
	}
	return pcm
}
