package agentclient

import (
	"errors"
	"fmt"
	"testing"
)

func TestOperationErrorClassNamesEveryAgentCode(t *testing.T) {
	for _, class := range []ErrorClass{
		ClassInvalidArgument, ClassNotFound, ClassConflict, ClassNotSupported,
		ClassPermissionDenied, ClassFailedPrecondition, ClassNetworkRejected,
		ClassUnavailable, ClassVerification, ClassInternal,
	} {
		wrapped := fmt.Errorf("command: %w", &OperationError{Code: string(class), Message: "failed"})
		got, operation, ok := ClassifyOperationError(wrapped)
		if !ok || got != class || operation.Message != "failed" {
			t.Fatalf("ClassifyOperationError(%s) = %q, %+v, %t", class, got, operation, ok)
		}
	}
	if got := (&OperationError{Code: "a code added by a newer Agent"}).Class(); got != ClassInternal {
		t.Fatalf("unknown code class = %q, want internal", got)
	}
	if _, _, ok := ClassifyOperationError(errors.New("connection refused")); ok {
		t.Fatal("a transport failure was classified as an Agent answer")
	}
}
