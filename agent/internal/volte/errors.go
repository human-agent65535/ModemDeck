package volte

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorUnsupported     ErrorCode = "unsupported"
	ErrorInvalidArgument ErrorCode = "invalid_argument"
	ErrorInvalidProfile  ErrorCode = "invalid_profile"
	ErrorInvalidPolicy   ErrorCode = "invalid_policy"
	ErrorUnavailable     ErrorCode = "unavailable"
	ErrorTransport       ErrorCode = "transport"
	ErrorDecode          ErrorCode = "decode"
	ErrorTimeout         ErrorCode = "timeout"
	ErrorCanceled        ErrorCode = "canceled"
	ErrorVerification    ErrorCode = "verification"
)

type Error struct {
	Code      ErrorCode
	Operation string
	ProfileID string
	Protocol  Protocol
	Message   string
	Cause     error
}

func (e *Error) Error() string {
	prefix := e.Operation
	if e.ProfileID != "" {
		if prefix != "" {
			prefix += " "
		}
		prefix += "[" + e.ProfileID + "]"
	}
	if prefix == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", prefix, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func AsError(err error) (*Error, bool) {
	var typed *Error
	if !errors.As(err, &typed) {
		return nil, false
	}
	return typed, true
}

func newError(
	code ErrorCode,
	operation string,
	profileID string,
	protocol Protocol,
	message string,
	cause error,
) error {
	return &Error{
		Code:      code,
		Operation: operation,
		ProfileID: profileID,
		Protocol:  protocol,
		Message:   message,
		Cause:     cause,
	}
}
