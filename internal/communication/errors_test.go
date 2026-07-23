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
		{code: "failed_precondition", want: ErrFailedPrecondition},
		{code: "network_rejected", want: ErrNetworkRejected},
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
