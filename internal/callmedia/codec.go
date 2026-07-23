package callmedia

import (
	"fmt"
	"time"
)

const (
	maxOpusPayloadBytes = 1275
	maxOpusPacketTime   = 120 * time.Millisecond
)

// DecodedAudio contains one complete Opus packet decoded to the endpoint's
// PCM format. Duration is derived from the Opus packet, not SDP ptime.
type DecodedAudio struct {
	PCM      []byte
	Duration time.Duration
}

// OpusCodec owns one encoder and one decoder for one call. Implementations
// must use OPUS_APPLICATION_VOIP and may assume Encode and Decode/Conceal are
// invoked from separate goroutines.
type OpusCodec interface {
	Format() PCMFormat
	Encode([]byte) ([]byte, error)
	Decode([]byte) (DecodedAudio, error)
	PacketDuration([]byte) (time.Duration, error)
	Conceal(time.Duration) ([]byte, error)
	Close() error
}

type OpusCodecFactory interface {
	New(PCMFormat) (OpusCodec, error)
}

func validateEncodedFrame(format PCMFormat, pcm []byte) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if len(pcm) != format.FrameBytes() {
		return fmt.Errorf("encode Opus frame: PCM size: %w", ErrInvalidArgument)
	}
	return nil
}

func durationFromSamples(samples, sampleRate int) (time.Duration, error) {
	if samples <= 0 || sampleRate <= 0 {
		return 0, ErrInvalidRTP
	}
	nanoseconds := int64(samples) * int64(time.Second) / int64(sampleRate)
	duration := time.Duration(nanoseconds)
	if duration > maxOpusPacketTime || !validOpusPacketDuration(duration) {
		return 0, ErrInvalidRTP
	}
	return duration, nil
}

func validOpusFrameDuration(duration time.Duration) bool {
	switch duration {
	case 2500 * time.Microsecond,
		5 * time.Millisecond,
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		60 * time.Millisecond:
		return true
	default:
		return false
	}
}

func validOpusPacketDuration(duration time.Duration) bool {
	if validOpusFrameDuration(duration) {
		return true
	}
	switch duration {
	case 80 * time.Millisecond, 100 * time.Millisecond, 120 * time.Millisecond:
		return true
	default:
		return false
	}
}

func pcmBytesForDuration(format PCMFormat, duration time.Duration) (int, error) {
	if !validOpusPacketDuration(duration) {
		return 0, ErrInvalidRTP
	}
	samples := format.samples(duration)
	if samples <= 0 {
		return 0, ErrInvalidRTP
	}
	return samples * format.Channels * bytesPerPCMSample, nil
}
