//go:build !cgo

package callmedia

import (
	"errors"
	"testing"
)

func TestProductionCodecReportsTypedUnsupportedWithoutCGO(t *testing.T) {
	factory, err := NewProductionOpusFactory()
	if factory != nil {
		t.Fatal("non-CGO production factory is non-nil")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported error = %v", err)
	}
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("unsupported error type = %T", err)
	}

	core, err := New(Options{
		EndpointOpener: &fakeEndpointOpener{
			endpoint: newFakeEndpoint(testFormat(8000)),
		},
	})
	if core != nil {
		t.Fatal("non-CGO production core is non-nil")
	}
	if !errors.As(err, &unsupported) || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("non-CGO core error = %v", err)
	}
}
