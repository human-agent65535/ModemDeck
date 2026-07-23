package modemmanager

import (
	"context"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestProviderMutationsAreExplicitlyUnsupported(t *testing.T) {
	provider := &Provider{}
	tests := []struct {
		name      string
		operation string
		run       func() error
	}{
		{
			name:      "start call",
			operation: "start_call",
			run: func() error {
				_, err := provider.StartCall(context.Background(), domain.StartCallRequest{})
				return err
			},
		},
		{
			name:      "answer call",
			operation: "answer_call",
			run: func() error {
				_, err := provider.AnswerCall(context.Background(), "call-1")
				return err
			},
		},
		{
			name:      "hangup call",
			operation: "hangup_call",
			run: func() error {
				_, err := provider.HangupCall(context.Background(), "call-1")
				return err
			},
		},
		{
			name:      "send message",
			operation: "send_message",
			run: func() error {
				_, err := provider.SendMessage(context.Background(), domain.SendMessageRequest{})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			operationError, ok := domain.AsOperationError(err)
			if !ok {
				t.Fatalf("expected OperationError, got %T: %v", err, err)
			}
			if operationError.Code != domain.ErrorNotSupported || operationError.Operation != test.operation {
				t.Fatalf("unexpected operation error: %+v", operationError)
			}
		})
	}
}
