package communication

import (
	"errors"
	"fmt"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "conflict"
	CodeNotSupported    ErrorCode = "not_supported"
	CodeUnavailable     ErrorCode = "unavailable"
	CodeInternal        ErrorCode = "internal"
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

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && e.Code == other.Code
}

var (
	ErrInvalidArgument = &Error{Code: CodeInvalidArgument}
	ErrNotFound        = &Error{Code: CodeNotFound}
	ErrConflict        = &Error{Code: CodeConflict}
	ErrNotSupported    = &Error{Code: CodeNotSupported}
	ErrUnavailable     = &Error{Code: CodeUnavailable}
	ErrInternal        = &Error{Code: CodeInternal}
)

func operationError(code ErrorCode, operation, message string, cause error) error {
	return &Error{Code: code, Operation: operation, Message: message, Cause: cause}
}

func translateAgentError(operation string, err error) error {
	var agentError *agentclient.OperationError
	if !errors.As(err, &agentError) {
		return operationError(CodeUnavailable, operation, "host agent is unavailable", err)
	}
	code := CodeInternal
	switch agentError.Code {
	case "invalid_argument":
		code = CodeInvalidArgument
	case "not_found":
		code = CodeNotFound
	case "conflict":
		code = CodeConflict
	case "not_supported":
		code = CodeNotSupported
	case "unavailable":
		code = CodeUnavailable
	}
	return operationError(code, operation, agentError.Message, err)
}
