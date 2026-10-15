//go:build cgo

package callmedia

/*
#cgo LDFLAGS: -lmd_audio_core -lstdc++ -lm -pthread -Wl,--gc-sections
#include <opus/opus.h>
#include <md_neteq.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

type libOpusFactory struct{}

func NewProductionOpusFactory() (OpusCodecFactory, error) {
	return libOpusFactory{}, nil
}

func (libOpusFactory) New(format PCMFormat) (OpusCodec, error) {
	if err := format.Validate(); err != nil {
		return nil, err
	}

	encoder := C.md_opus_encoder_create(C.int(format.SampleRate), C.int(format.Channels))
	if encoder == nil {
		return nil, fmt.Errorf("create bundled Opus encoder: %w", ErrCodec)
	}
	decoder := C.md_opus_decoder_create(C.int(format.SampleRate), C.int(format.Channels))
	if decoder == nil {
		C.md_opus_encoder_destroy(encoder)
		return nil, fmt.Errorf("create bundled Opus decoder: %w", ErrCodec)
	}

	return &libOpusCodec{
		format:  format,
		encoder: encoder,
		decoder: decoder,
	}, nil
}

type libOpusCodec struct {
	format PCMFormat

	encoderMu sync.Mutex
	encoder   *C.md_opus_encoder

	decoderMu sync.Mutex
	decoder   *C.md_opus_decoder

	closeOnce sync.Once
}

func (c *libOpusCodec) Format() PCMFormat {
	if c == nil {
		return PCMFormat{}
	}
	return c.format
}

func (c *libOpusCodec) Encode(pcm []byte) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("encode Opus frame: %w", ErrCodec)
	}
	if err := validateEncodedFrame(c.format, pcm); err != nil {
		return nil, err
	}

	c.encoderMu.Lock()
	defer c.encoderMu.Unlock()
	if c.encoder == nil {
		return nil, fmt.Errorf("encode Opus frame: codec closed: %w", ErrCodec)
	}

	encoded := make([]byte, maxOpusPayloadBytes)
	written := C.md_opus_encode(
		c.encoder,
		(*C.int16_t)(unsafe.Pointer(&pcm[0])),
		C.int(c.format.FrameSamples()),
		(*C.uint8_t)(unsafe.Pointer(&encoded[0])),
		C.size_t(len(encoded)),
	)
	if written < 0 {
		return nil, opusError("encode Opus frame", C.int(written))
	}
	if written == 0 || int(written) > len(encoded) {
		return nil, fmt.Errorf("encode Opus frame: invalid output: %w", ErrCodec)
	}
	return encoded[:int(written)], nil
}

func (c *libOpusCodec) Decode(payload []byte) (DecodedAudio, error) {
	if c == nil || len(payload) == 0 || len(payload) > maxOpusPayloadBytes {
		return DecodedAudio{}, fmt.Errorf("decode Opus packet: %w", ErrInvalidAudio)
	}
	duration, err := c.PacketDuration(payload)
	if err != nil {
		return DecodedAudio{}, err
	}
	pcmBytes, err := pcmBytesForDuration(c.format, duration)
	if err != nil {
		return DecodedAudio{}, err
	}

	c.decoderMu.Lock()
	defer c.decoderMu.Unlock()
	if c.decoder == nil {
		return DecodedAudio{}, fmt.Errorf("decode Opus packet: codec closed: %w", ErrCodec)
	}

	pcm := make([]byte, pcmBytes)
	samples := C.md_opus_decode(
		c.decoder,
		(*C.uint8_t)(unsafe.Pointer(&payload[0])),
		C.size_t(len(payload)),
		(*C.int16_t)(unsafe.Pointer(&pcm[0])),
		C.int(c.format.samples(duration)),
	)
	if samples < 0 {
		return DecodedAudio{}, opusError("decode Opus packet", C.int(samples))
	}
	if int(samples) != c.format.samples(duration) {
		return DecodedAudio{}, fmt.Errorf("decode Opus packet: unexpected PCM duration: %w", ErrCodec)
	}
	return DecodedAudio{PCM: pcm, Duration: duration}, nil
}

func (c *libOpusCodec) PacketDuration(payload []byte) (time.Duration, error) {
	if c == nil || len(payload) == 0 || len(payload) > maxOpusPayloadBytes {
		return 0, fmt.Errorf("inspect Opus packet: %w", ErrInvalidAudio)
	}
	samples := C.md_opus_packet_samples(
		(*C.uint8_t)(unsafe.Pointer(&payload[0])),
		C.size_t(len(payload)),
		C.int(OpusClockRate),
	)
	if samples < 0 {
		return 0, opusError("inspect Opus packet", C.int(samples))
	}
	duration, err := durationFromSamples(int(samples), OpusClockRate)
	if err != nil {
		return 0, fmt.Errorf("inspect Opus packet: %w", err)
	}
	return duration, nil
}

func (c *libOpusCodec) Conceal(duration time.Duration) ([]byte, error) {
	if c == nil || !validOpusFrameDuration(duration) {
		return nil, fmt.Errorf("conceal Opus packet: duration: %w", ErrInvalidAudio)
	}
	pcmBytes, err := pcmBytesForDuration(c.format, duration)
	if err != nil {
		return nil, err
	}

	c.decoderMu.Lock()
	defer c.decoderMu.Unlock()
	if c.decoder == nil {
		return nil, fmt.Errorf("conceal Opus packet: codec closed: %w", ErrCodec)
	}

	pcm := make([]byte, pcmBytes)
	samples := C.md_opus_conceal(c.decoder, C.int(c.format.samples(duration)), (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.int(c.format.samples(duration)))
	if samples < 0 {
		return nil, opusError("conceal Opus packet", C.int(samples))
	}
	if int(samples) != c.format.samples(duration) {
		return nil, fmt.Errorf("conceal Opus packet: unexpected PCM duration: %w", ErrCodec)
	}
	return pcm, nil
}

func (c *libOpusCodec) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.encoderMu.Lock()
		c.decoderMu.Lock()
		if c.encoder != nil {
			C.md_opus_encoder_destroy(c.encoder)
			c.encoder = nil
		}
		if c.decoder != nil {
			C.md_opus_decoder_destroy(c.decoder)
			c.decoder = nil
		}
		c.decoderMu.Unlock()
		c.encoderMu.Unlock()
	})
	return nil
}

func opusError(operation string, code C.int) error {
	message := C.GoString(C.opus_strerror(code))
	if message == "" {
		message = "unknown libopus error"
	}
	return fmt.Errorf("%s: %s: %w", operation, message, ErrCodec)
}
