package media

import "testing"

func TestParseFormatAcceptsOnlyFixedMonoS16LEBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		rate       uint32
		frameBytes int
	}{
		{name: "narrowband", rate: 8000, frameBytes: 320},
		{name: "wideband", rate: 16000, frameBytes: 640},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			format, err := ParseFormat(AdvertisedFormat{
				Encoding:   EncodingPCM,
				Resolution: ResolutionS16LE,
				Rate:       test.rate,
			})
			if err != nil {
				t.Fatalf("ParseFormat() error = %v", err)
			}
			if format.Channels != ChannelsMono ||
				format.FrameDuration != FrameDuration ||
				format.FrameBytes != test.frameBytes {
				t.Fatalf("unexpected format: %+v", format)
			}
		})
	}
}

func TestParseFormatRejectsUnconfirmedOrUnsupportedFormats(t *testing.T) {
	tests := []struct {
		name   string
		format AdvertisedFormat
	}{
		{
			name: "encoding is exact",
			format: AdvertisedFormat{
				Encoding:   "PCM",
				Resolution: ResolutionS16LE,
				Rate:       8000,
			},
		},
		{
			name: "resolution is exact",
			format: AdvertisedFormat{
				Encoding:   EncodingPCM,
				Resolution: "S16_LE",
				Rate:       8000,
			},
		},
		{
			name: "sample rate is bounded",
			format: AdvertisedFormat{
				Encoding:   EncodingPCM,
				Resolution: ResolutionS16LE,
				Rate:       48000,
			},
		},
		{
			name: "missing rate",
			format: AdvertisedFormat{
				Encoding:   EncodingPCM,
				Resolution: ResolutionS16LE,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseFormat(test.format)
			if !IsCode(err, ErrorUnsupportedFormat) {
				t.Fatalf("ParseFormat() error = %v, want unsupported format", err)
			}
		})
	}
}
