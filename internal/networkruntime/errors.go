package networkruntime

import (
	"errors"
	"fmt"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "revision_conflict"
	CodeUnavailable     ErrorCode = "unavailable"
	CodeInternal        ErrorCode = "internal"
)

type Error struct {
	Code    ErrorCode
	Field   string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func operationError(code ErrorCode, field, message string, cause error) error {
	return &Error{Code: code, Field: field, Message: message, Cause: cause}
}

func classifyStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrProxyInstanceNotFound):
		return operationError(CodeNotFound, "id", "Proxy was not found", err)
	case errors.Is(err, store.ErrProxyInstanceRevisionConflict):
		return operationError(
			CodeConflict,
			"revision",
			"Proxy changed since it was loaded",
			err,
		)
	default:
		return operationError(CodeInternal, "", "Proxy settings could not be saved", err)
	}
}
