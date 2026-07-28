package callmedia

import "errors"

var (
	ErrInvalidArgument      = errors.New("invalid call media argument")
	ErrCallNotActive        = errors.New("call is not active")
	ErrCallInUse            = errors.New("call already has a browser media peer")
	ErrNotMediaOwner        = errors.New("media owner token does not own this call")
	ErrCoreClosed           = errors.New("call media core is closed")
	ErrUnsupported          = errors.New("call media feature is unsupported")
	ErrUnsupportedMedia     = errors.New("WebRTC offer contains unsupported media")
	ErrUnsupportedCodec     = errors.New("WebRTC offer does not support Opus")
	ErrUnsupportedDirection = errors.New("WebRTC audio must be full duplex")
	ErrInvalidOffer         = errors.New("invalid WebRTC offer")
	ErrNegotiation          = errors.New("WebRTC negotiation failed")
	ErrGatheringTimeout     = errors.New("WebRTC ICE gathering timed out")
	ErrEndpointUnavailable  = errors.New("PCM media endpoint is unavailable")
	ErrEndpointNotStarted   = errors.New("PCM media endpoint is not started")
	ErrEndpointIO           = errors.New("PCM media endpoint failed")
	ErrCodec                = errors.New("Opus codec failed")
	ErrInvalidRTP           = errors.New("invalid Opus RTP packet")
	ErrBackpressure         = errors.New("media queue capacity exceeded")
	ErrMediaGap             = errors.New("RTP media gap exceeded concealment limit")
	ErrTransportClosed      = errors.New("WebRTC transport closed")
	ErrSessionNotReady      = errors.New("call media session is still preparing")
	ErrCanceled             = errors.New("call media operation canceled")
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
