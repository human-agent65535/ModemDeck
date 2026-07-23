package telegram

import (
	"errors"
	"fmt"
	"time"
)

type ReplyBindingNotFoundError struct{}

func (*ReplyBindingNotFoundError) Error() string {
	return "telegram reply binding not found"
}

type CapabilityUnavailableError struct {
	Capability string
}

func (e *CapabilityUnavailableError) Error() string {
	if e == nil || e.Capability == "" {
		return "telegram capability unavailable"
	}
	return fmt.Sprintf("telegram capability unavailable: %s", e.Capability)
}

// APIError is an error returned by the Telegram Bot API. Description is
// Telegram-generated and never contains a locally supplied token or message
// body.
type APIError struct {
	Code            int
	Description     string
	RetryAfter      time.Duration
	MigrateToChatID int64
}

func (e *APIError) Error() string {
	if e == nil {
		return "telegram API error"
	}
	if e.Code == 0 {
		return "telegram API error"
	}
	return fmt.Sprintf("telegram API error %d", e.Code)
}

func (e *APIError) Temporary() bool {
	return e != nil && (e.Code == 429 || e.Code >= 500)
}

type HTTPStatusError struct {
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("telegram HTTP status %d", e.StatusCode)
}

type ResponseTooLargeError struct {
	Limit int64
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("telegram response exceeds %d bytes", e.Limit)
}

type ProtocolError struct {
	Method string
	Reason string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("telegram %s response is invalid: %s", e.Method, e.Reason)
}

// TransportError deliberately omits the underlying error text because standard
// net/http URL errors can include the bot token in the request URL. Callers may
// inspect the wrapped error programmatically with errors.Is/errors.As.
type TransportError struct {
	Method string
	Err    error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("telegram %s transport failed", e.Method)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

type OperationError struct {
	Operation string
	Kind      string
	Err       error
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("telegram %s failed (%s)", e.Operation, e.Kind)
}

func (e *OperationError) Unwrap() error {
	return e.Err
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *APIError
	var statusErr *HTTPStatusError
	var largeErr *ResponseTooLargeError
	var protocolErr *ProtocolError
	var transportErr *TransportError
	var operationErr *OperationError
	var replyNotFoundErr *ReplyBindingNotFoundError
	var capabilityErr *CapabilityUnavailableError
	switch {
	case errors.As(err, &apiErr):
		return "telegram_api"
	case errors.As(err, &statusErr):
		return "http_status"
	case errors.As(err, &largeErr):
		return "response_too_large"
	case errors.As(err, &protocolErr):
		return "protocol"
	case errors.As(err, &transportErr):
		return "transport"
	case errors.As(err, &operationErr):
		return operationErr.Kind
	case errors.As(err, &replyNotFoundErr):
		return "reply_not_found"
	case errors.As(err, &capabilityErr):
		return "capability_unavailable"
	default:
		return "dependency"
	}
}
