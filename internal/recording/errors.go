package recording

import "errors"

var (
	ErrInvalidArgument  = errors.New("invalid recording argument")
	ErrNotFound         = errors.New("recording resource was not found")
	ErrConflict         = errors.New("recording state conflicts with another request")
	ErrRevisionConflict = errors.New("recording settings revision conflict")
	ErrRequestConflict  = errors.New("recording request id conflict")
	ErrNotReady         = errors.New("recording is not ready for download")
	ErrCallNotActive    = errors.New("call is not active")
	ErrMediaUnavailable = errors.New("call media is unavailable")
	ErrStorage          = errors.New("recording storage failed")
	ErrCodec            = errors.New("recording codec failed")
	ErrNoAudio          = errors.New("recording contains no audio")
	ErrBackpressure     = errors.New("recording queue capacity exceeded")
	ErrClosed           = errors.New("recording service is closed")
)
