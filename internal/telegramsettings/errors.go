package telegramsettings

import (
	"errors"
	"fmt"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "conflict"
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
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && e.Code == other.Code
}

var (
	ErrInvalidArgument = &Error{Code: CodeInvalidArgument}
	ErrNotFound        = &Error{Code: CodeNotFound}
	ErrConflict        = &Error{Code: CodeConflict}
	ErrUnavailable     = &Error{Code: CodeUnavailable}
	ErrInternal        = &Error{Code: CodeInternal}
)

func operationError(code ErrorCode, field, message string, cause error) error {
	if message == "" {
		message = fmt.Sprintf("telegram settings %s", code)
	}
	return &Error{Code: code, Field: field, Message: message, Cause: cause}
}

func classifyStoreError(err error) error {
	switch {
	case errors.Is(err, store.ErrTelegramUnitNotFound):
		return operationError(CodeNotFound, "", "Telegram unit was not found", err)
	case errors.Is(err, store.ErrTelegramUnitRevisionConflict):
		return operationError(CodeConflict, "revision", "Telegram unit was changed by another request", err)
	default:
		return operationError(CodeInternal, "", "Telegram settings could not be stored", err)
	}
}
