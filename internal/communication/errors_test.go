package communication

import (
	"errors"
	"net/http"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

func TestTranslateAgentErrorPreservesActionableModemManagerClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		want error
	}{
		{code: "invalid_argument", want: ErrInvalidArgument},
		{code: "not_found", want: ErrNotFound},
		{code: "conflict", want: ErrConflict},
		{code: "not_supported", want: ErrNotSupported},
		{code: "failed_precondition", want: ErrFailedPrecondition},
		{code: "network_rejected", want: ErrNetworkRejected},
		{code: "permission_denied", want: ErrPermissionDenied},
		{code: "unavailable", want: ErrUnavailable},
		{code: "verification_failed", want: ErrVerification},
		{code: "internal", want: ErrInternal},
		{code: "a code added by a newer Agent", want: ErrInternal},
	}
	for _, test := range tests {
		test := test
		t.Run(test.code, func(t *testing.T) {
			t.Parallel()
			err := translateAgentError("test command", &agentclient.OperationError{
				Status:  http.StatusBadRequest,
				Code:    test.code,
				Message: "classified failure",
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("translateAgentError() = %v, want %v", err, test.want)
			}
		})
	}
}
