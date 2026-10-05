package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
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

func TestAudioFailureCodesPreserveBoundedDiagnosticReason(t *testing.T) {
	for _, value := range []struct {
		err  error
		code string
	}{{nil, ""}, {ErrInvalidRTP, "invalid_audio"}, {ErrCodec, "codec_failed"}, {ErrEndpointIO, "endpoint_failed"}, {ErrBackpressure, "backpressure"}, {ErrTransportClosed, "transport_closed"}, {errors.Join(ErrTransportClosed, context.DeadlineExceeded), "transport_timeout"}, {context.Canceled, "cancelled"}, {errors.New("private detail"), "media_failed"}} {
		if got := AudioFailureCode(value.err); got != value.code {
			t.Fatalf("failure code=%s want=%s", got, value.code)
		}
	}
}
