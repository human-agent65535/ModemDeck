package media

import "time"

const (
	EncodingPCM     = "pcm"
	ResolutionS16LE = "s16le"
	ChannelsMono    = 1
	FrameDuration   = 20 * time.Millisecond
)

type AdvertisedFormat struct {
	Encoding   string
	Resolution string
	Rate       uint32
}

type Format struct {
	Encoding      string
	Resolution    string
	Rate          uint32
	Channels      uint8
	FrameDuration time.Duration
	FrameBytes    int
}

func ParseFormat(advertised AdvertisedFormat) (Format, error) {
	const operation = "parse_media_format"

	if advertised.Encoding != EncodingPCM || advertised.Resolution != ResolutionS16LE {
		return Format{}, NewError(
			ErrorUnsupportedFormat,
			operation,
			"media format must be pcm/s16le",
			nil,
		)
	}
	if advertised.Rate != 8000 && advertised.Rate != 16000 {
		return Format{}, NewError(
			ErrorUnsupportedFormat,
			operation,
			"media sample rate must be 8000 or 16000 Hz",
			nil,
		)
	}

	samplesPerFrame := uint64(advertised.Rate) * uint64(FrameDuration/time.Millisecond) / 1000
	return Format{
		Encoding:      advertised.Encoding,
		Resolution:    advertised.Resolution,
		Rate:          advertised.Rate,
		Channels:      ChannelsMono,
		FrameDuration: FrameDuration,
		FrameBytes:    int(samplesPerFrame * 2),
	}, nil
}

func validateFormat(format Format) error {
	expected, err := ParseFormat(AdvertisedFormat{
		Encoding:   format.Encoding,
		Resolution: format.Resolution,
		Rate:       format.Rate,
	})
	if err != nil {
		return err
	}
	if format != expected {
		return NewError(
			ErrorUnsupportedFormat,
			"validate_media_format",
			"media format is not the canonical fixed-frame boundary",
			nil,
		)
	}
	return nil
}
