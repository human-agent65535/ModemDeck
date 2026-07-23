package agentmedia

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorInvalidArgument    ErrorCode = "invalid_argument"
	ErrorNotFound           ErrorCode = "not_found"
	ErrorProtocol           ErrorCode = "protocol_error"
	ErrorTimeout            ErrorCode = "timeout"
	ErrorConflict           ErrorCode = "conflict"
	ErrorNotActive          ErrorCode = "not_active"
	ErrorBackendUnavailable ErrorCode = "backend_unavailable"
	ErrorMediaUnavailable   ErrorCode = "media_unavailable"
	ErrorUnsupportedFormat  ErrorCode = "unsupported_format"
	ErrorUnboundAudioPort   ErrorCode = "unbound_audio_port"
	ErrorBackpressure       ErrorCode = "backpressure"
	ErrorInternal           ErrorCode = "internal"
)

type Error struct {
	Code      ErrorCode
	Operation string
	Message   string
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Operation == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newError(code ErrorCode, operation, message string, cause error) error {
	return &Error{
		Code:      code,
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

func AsError(err error) (*Error, bool) {
	var mediaError *Error
	if !errors.As(err, &mediaError) {
		return nil, false
	}
	return mediaError, true
}

func IsCode(err error, code ErrorCode) bool {
	mediaError, ok := AsError(err)
	return ok && mediaError.Code == code
}
