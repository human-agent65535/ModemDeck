//go:build !cgo

package callmedia

func NewProductionOpusFactory() (OpusCodecFactory, error) {
	return nil, &UnsupportedError{Feature: "libopus CGO codec"}
}
