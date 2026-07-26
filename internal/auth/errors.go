package auth

import "errors"

// ErrorCode identifies an authentication failure without requiring callers to
// inspect error text.
type ErrorCode string

const (
	CodeRepositoryRequired       ErrorCode = "repository_required"
	CodeUsernameInvalid          ErrorCode = "username_invalid"
	CodeAlreadyConfigured        ErrorCode = "already_configured"
	CodePasswordRequired         ErrorCode = "password_required"
	CodePasswordTooShort         ErrorCode = "password_too_short"
	CodePasswordTooLong          ErrorCode = "password_too_long"
	CodePasswordInvalid          ErrorCode = "password_invalid"
	CodePasswordUnchanged        ErrorCode = "password_unchanged"
	CodeInvalidPasswordHash      ErrorCode = "invalid_password_hash"
	CodePasswordHashTooExpensive ErrorCode = "password_hash_too_expensive"
	CodeInvalidCredentials       ErrorCode = "invalid_credentials"
	CodeInvalidSessionToken      ErrorCode = "invalid_session_token"
	CodeUnauthenticated          ErrorCode = "unauthenticated"
	CodeSessionExpired           ErrorCode = "session_expired"
	CodeInvalidSessionRecord     ErrorCode = "invalid_session_record"
	CodeInvalidCSRFToken         ErrorCode = "invalid_csrf_token"
	CodeRandomSource             ErrorCode = "random_source"
	CodeRepository               ErrorCode = "repository"
)

// Error is returned for all failures owned by this package. Its Code is stable
// for errors.Is checks; Op and Err retain diagnostic context.
type Error struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	message := string(e.Code)
	if e.Op != "" {
		message = e.Op + ": " + message
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && e.Code != "" && e.Code == other.Code
}

var (
	ErrRepositoryRequired       = &Error{Code: CodeRepositoryRequired}
	ErrUsernameInvalid          = &Error{Code: CodeUsernameInvalid}
	ErrAlreadyConfigured        = &Error{Code: CodeAlreadyConfigured}
	ErrPasswordRequired         = &Error{Code: CodePasswordRequired}
	ErrPasswordTooShort         = &Error{Code: CodePasswordTooShort}
	ErrPasswordTooLong          = &Error{Code: CodePasswordTooLong}
	ErrPasswordInvalid          = &Error{Code: CodePasswordInvalid}
	ErrPasswordUnchanged        = &Error{Code: CodePasswordUnchanged}
	ErrInvalidPasswordHash      = &Error{Code: CodeInvalidPasswordHash}
	ErrPasswordHashTooExpensive = &Error{Code: CodePasswordHashTooExpensive}
	ErrInvalidCredentials       = &Error{Code: CodeInvalidCredentials}
	ErrInvalidSessionToken      = &Error{Code: CodeInvalidSessionToken}
	ErrUnauthenticated          = &Error{Code: CodeUnauthenticated}
	ErrSessionExpired           = &Error{Code: CodeSessionExpired}
	ErrInvalidSessionRecord     = &Error{Code: CodeInvalidSessionRecord}
	ErrInvalidCSRFToken         = &Error{Code: CodeInvalidCSRFToken}
	ErrRandomSource             = &Error{Code: CodeRandomSource}
	ErrRepository               = &Error{Code: CodeRepository}
)

func newError(op string, code ErrorCode, err error) error {
	return &Error{Code: code, Op: op, Err: err}
}

func repositoryError(op string, err error) error {
	return newError(op, CodeRepository, err)
}

func invalidPasswordHash(op, reason string) error {
	return newError(op, CodeInvalidPasswordHash, errors.New(reason))
}
