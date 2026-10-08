package callmedia

import "errors"

var (
	ErrInvalidArgument     = errors.New("invalid call media argument")
	ErrCallNotActive       = errors.New("call is not active")
	ErrCallInUse           = errors.New("call already has a media owner")
	ErrNotMediaOwner       = errors.New("media owner token does not own this call")
	ErrCoreClosed          = errors.New("call media core is closed")
	ErrUnsupported         = errors.New("call media feature is unsupported")
	ErrEndpointUnavailable = errors.New("PCM media endpoint is unavailable")
	ErrEndpointNotStarted  = errors.New("PCM media endpoint is not started")
	ErrEndpointIO          = errors.New("PCM media endpoint failed")
	ErrCodec               = errors.New("Opus codec failed")
	ErrInvalidAudio        = errors.New("invalid Opus audio frame")
	ErrBackpressure        = errors.New("media queue capacity exceeded")
	ErrTransportTimeout    = errors.New("call media transport timed out")
	ErrTransportClosed     = errors.New("call media transport closed")
	ErrSessionNotReady     = errors.New("call media session is still preparing")
	ErrCanceled            = errors.New("call media operation canceled")
)

// UnsupportedError identifies a capability that cannot exist in the current
// build or runtime. In particular, the non-CGO build returns this type when a
// production libopus factory is requested.
type UnsupportedError struct {
	Feature string
}

func (e *UnsupportedError) Error() string {
	if e == nil || e.Feature == "" {
		return ErrUnsupported.Error()
	}
	return e.Feature + ": " + ErrUnsupported.Error()
}

func (e *UnsupportedError) Is(target error) bool {
	return target == ErrUnsupported
}
