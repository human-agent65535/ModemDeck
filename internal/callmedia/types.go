package callmedia

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	RTPClockRate       = 48000
	OpusPayloadType    = 111
	maxCallIDBytes     = 256
	maxOwnerTokenBytes = 128
	bitsPerPCMSample   = 16
	bytesPerPCMSample  = bitsPerPCMSample / 8
	defaultFramePeriod = 20 * time.Millisecond
)

// CallState is deliberately small: the media core needs only the server
// confirmed fact that a consumer call is active.
type CallState string

const (
	CallStateActive CallState = "active"
)

// ActiveCall is an opaque call binding. ID must not contain a phone number.
type ActiveCall struct {
	ID    string
	State CallState
}

type PCMEncoding string

const PCMEncodingS16LE PCMEncoding = "s16le"

// PCMFormat describes the only host media boundary accepted by this package.
// The clean rewrite currently supports mono S16LE telephone PCM at 8 or 16 kHz.
type PCMFormat struct {
	Encoding      PCMEncoding
	SampleRate    int
	Channels      int
	FrameDuration time.Duration
}

func (f PCMFormat) Validate() error {
	if f.Encoding != PCMEncodingS16LE {
		return fmt.Errorf("validate PCM format: encoding: %w", ErrInvalidArgument)
	}
	if f.SampleRate != 8000 && f.SampleRate != 16000 {
		return fmt.Errorf("validate PCM format: sample rate: %w", ErrInvalidArgument)
	}
	if f.Channels != 1 {
		return fmt.Errorf("validate PCM format: channels: %w", ErrInvalidArgument)
	}
	if f.FrameDuration != 10*time.Millisecond && f.FrameDuration != defaultFramePeriod {
		return fmt.Errorf("validate PCM format: frame duration: %w", ErrInvalidArgument)
	}
	if f.samples(f.FrameDuration) <= 0 || f.rtpSamples(f.FrameDuration) <= 0 {
		return fmt.Errorf("validate PCM format: frame size: %w", ErrInvalidArgument)
	}
	return nil
}

func (f PCMFormat) FrameSamples() int {
	return f.samples(f.FrameDuration)
}

func (f PCMFormat) FrameBytes() int {
	return f.FrameSamples() * f.Channels * bytesPerPCMSample
}

func (f PCMFormat) RTPFrameSamples() uint32 {
	return uint32(f.rtpSamples(f.FrameDuration))
}

func (f PCMFormat) samples(duration time.Duration) int {
	return int(int64(f.SampleRate) * int64(duration) / int64(time.Second))
}

func (f PCMFormat) rtpSamples(duration time.Duration) int {
	return int(int64(RTPClockRate) * int64(duration) / int64(time.Second))
}

// MediaEndpoint is a full-duplex, fixed-frame PCM stream owned by the
// host integration. Open establishes the lease without starting device I/O.
// Start begins device I/O exactly once after a real consumer is ready.
// ReadPCM and WritePCM may then run concurrently. Each call must consume or
// produce exactly Format().FrameBytes() bytes. Close must unblock both methods.
type MediaEndpoint interface {
	Format() PCMFormat
	Start(context.Context) error
	ReadPCM(context.Context, []byte) error
	WritePCM(context.Context, []byte) error
	Close() error
}

// MediaEndpointOpener opens the host endpoint for one server-confirmed ACTIVE
// call. Implementations must not perform call control or choose another call.
type MediaEndpointOpener interface {
	Open(context.Context, ActiveCall) (MediaEndpoint, error)
}

type Offer struct {
	Call       ActiveCall
	OwnerToken string
	SDP        string
}

type ExchangeResult struct {
	AnswerSDP string
	Session   *Session
}

func normalizeActiveCall(call ActiveCall) (ActiveCall, error) {
	call.ID = strings.TrimSpace(call.ID)
	if call.State != CallStateActive {
		return ActiveCall{}, ErrCallNotActive
	}
	if call.ID == "" || len(call.ID) > maxCallIDBytes {
		return ActiveCall{}, ErrInvalidArgument
	}
	for _, r := range call.ID {
		if unicode.IsControl(r) {
			return ActiveCall{}, ErrInvalidArgument
		}
	}
	return call, nil
}

func normalizeOwnerToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxOwnerTokenBytes {
		return "", ErrInvalidArgument
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", ErrInvalidArgument
		}
	}
	return value, nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
