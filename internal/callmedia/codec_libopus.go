//go:build cgo

package callmedia

/*
#cgo LDFLAGS: -lopus -lm
#include <opus/opus.h>

static int modemdeck_opus_encoder_configure(OpusEncoder *encoder) {
	int result = opus_encoder_ctl(encoder, OPUS_SET_BITRATE(24000));
	if (result != OPUS_OK) return result;
	result = opus_encoder_ctl(encoder, OPUS_SET_VBR(1));
	if (result != OPUS_OK) return result;
	result = opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(1));
	if (result != OPUS_OK) return result;
	result = opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(8));
	if (result != OPUS_OK) return result;
	result = opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
	if (result != OPUS_OK) return result;
	result = opus_encoder_ctl(encoder, OPUS_SET_INBAND_FEC(1));
	if (result != OPUS_OK) return result;
	result = opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(10));
	if (result != OPUS_OK) return result;
	return opus_encoder_ctl(encoder, OPUS_SET_DTX(0));
}
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

	var encoderCode C.int
	encoder := C.opus_encoder_create(
		C.opus_int32(format.SampleRate),
		C.int(format.Channels),
		C.int(C.OPUS_APPLICATION_VOIP),
		&encoderCode,
	)
	if encoder == nil || encoderCode != C.OPUS_OK {
		if encoder != nil {
			C.opus_encoder_destroy(encoder)
		}
		return nil, opusError("create libopus encoder", encoderCode)
	}
	if code := C.modemdeck_opus_encoder_configure(encoder); code != C.OPUS_OK {
		C.opus_encoder_destroy(encoder)
		return nil, opusError("configure libopus encoder", code)
	}

	var decoderCode C.int
	decoder := C.opus_decoder_create(
		C.opus_int32(format.SampleRate),
		C.int(format.Channels),
		&decoderCode,
	)
	if decoder == nil || decoderCode != C.OPUS_OK {
		C.opus_encoder_destroy(encoder)
		if decoder != nil {
			C.opus_decoder_destroy(decoder)
		}
		return nil, opusError("create libopus decoder", decoderCode)
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
	encoder   *C.OpusEncoder

	decoderMu sync.Mutex
	decoder   *C.OpusDecoder

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
	written := C.opus_encode(
		c.encoder,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(c.format.FrameSamples()),
		(*C.uchar)(unsafe.Pointer(&encoded[0])),
		C.opus_int32(len(encoded)),
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
		return DecodedAudio{}, fmt.Errorf("decode Opus packet: %w", ErrInvalidRTP)
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
	samples := C.opus_decode(
		c.decoder,
		(*C.uchar)(unsafe.Pointer(&payload[0])),
		C.opus_int32(len(payload)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(c.format.samples(duration)),
		0,
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
		return 0, fmt.Errorf("inspect Opus packet: %w", ErrInvalidRTP)
	}
	samples := C.opus_packet_get_nb_samples(
		(*C.uchar)(unsafe.Pointer(&payload[0])),
		C.opus_int32(len(payload)),
		C.opus_int32(RTPClockRate),
	)
	if samples < 0 {
		return 0, opusError("inspect Opus packet", C.int(samples))
	}
	duration, err := durationFromSamples(int(samples), RTPClockRate)
	if err != nil {
		return 0, fmt.Errorf("inspect Opus packet: %w", err)
	}
	return duration, nil
}

func (c *libOpusCodec) Conceal(duration time.Duration) ([]byte, error) {
	if c == nil || !validOpusFrameDuration(duration) {
		return nil, fmt.Errorf("conceal Opus packet: duration: %w", ErrInvalidRTP)
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
	samples := C.opus_decode(
		c.decoder,
		nil,
		0,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(c.format.samples(duration)),
		0,
	)
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
			C.opus_encoder_destroy(c.encoder)
			c.encoder = nil
		}
		if c.decoder != nil {
			C.opus_decoder_destroy(c.decoder)
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
