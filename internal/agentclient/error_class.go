package agentclient

import "errors"

// ErrorClass is the Host Agent's classification of a failed operation: the
// "code" of its error response. This is the one place the application names
// those codes; callers map a class to their own domain error.
type ErrorClass string

const (
	ClassInvalidArgument    ErrorClass = "invalid_argument"
	ClassNotFound           ErrorClass = "not_found"
	ClassConflict           ErrorClass = "conflict"
	ClassNotSupported       ErrorClass = "not_supported"
	ClassPermissionDenied   ErrorClass = "permission_denied"
	ClassFailedPrecondition ErrorClass = "failed_precondition"
	ClassNetworkRejected    ErrorClass = "network_rejected"
	ClassUnavailable        ErrorClass = "unavailable"
	ClassVerification       ErrorClass = "verification_failed"
	ClassInternal           ErrorClass = "internal"
)

// Class returns the Agent's classification. A code this application does not
// know is an internal failure of the Agent.
func (e *OperationError) Class() ErrorClass {
	if e == nil {
		return ClassInternal
	}
	switch class := ErrorClass(e.Code); class {
	case ClassInvalidArgument,
		ClassNotFound,
		ClassConflict,
		ClassNotSupported,
		ClassPermissionDenied,
		ClassFailedPrecondition,
		ClassNetworkRejected,
		ClassUnavailable,
		ClassVerification:
		return class
	default:
		return ClassInternal
	}
}

// ClassifyOperationError returns the Agent's classification of err. ok is false
// when err is not an answer from the Agent at all, such as a transport failure.
func ClassifyOperationError(err error) (class ErrorClass, operation *OperationError, ok bool) {
	if !errors.As(err, &operation) {
		return "", nil, false
	}
	return operation.Class(), operation, true
}
