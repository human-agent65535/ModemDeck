package telegram

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTypedErrorsAreSafeAndInspectable(t *testing.T) {
	t.Parallel()

	secret := errors.New("request https://api.telegram.org/botSECRET/getMe failed")
	transportErr := &TransportError{Method: "getMe", Err: secret}
	if strings.Contains(transportErr.Error(), "SECRET") {
		t.Fatalf("TransportError leaks secret: %v", transportErr)
	}
	if !errors.Is(transportErr, secret) {
		t.Fatal("TransportError does not unwrap")
	}

	operationErr := &OperationError{Operation: "send_sms", Kind: "dependency", Err: secret}
	if strings.Contains(operationErr.Error(), "SECRET") {
		t.Fatalf("OperationError leaks secret: %v", operationErr)
	}
	if !errors.Is(operationErr, secret) {
		t.Fatal("OperationError does not unwrap")
	}

	apiErr := &APIError{Code: 429, Description: "sensitive-looking detail", RetryAfter: time.Second}
	if strings.Contains(apiErr.Error(), apiErr.Description) {
		t.Fatalf("APIError text exposes description: %v", apiErr)
	}
	if !apiErr.Temporary() {
		t.Fatal("429 must be temporary")
	}
	if (&APIError{Code: 400}).Temporary() {
		t.Fatal("400 must not be temporary")
	}

	cases := []struct {
		err  error
		want string
	}{
		{err: apiErr, want: "telegram_api"},
		{err: &HTTPStatusError{StatusCode: 500}, want: "http_status"},
		{err: &ResponseTooLargeError{Limit: 10}, want: "response_too_large"},
		{err: &ProtocolError{Method: "getMe", Reason: "bad"}, want: "protocol"},
		{err: transportErr, want: "transport"},
		{err: operationErr, want: "dependency"},
		{err: &ReplyBindingNotFoundError{}, want: "reply_not_found"},
		{err: &CapabilityUnavailableError{Capability: "dial"}, want: "capability_unavailable"},
		{err: errors.New("other"), want: "dependency"},
		{err: nil, want: ""},
	}
	for _, test := range cases {
		if got := errorClass(test.err); got != test.want {
			t.Errorf("errorClass(%T) = %q, want %q", test.err, got, test.want)
		}
		if test.err != nil && test.err.Error() == "" {
			t.Errorf("%T has empty Error()", test.err)
		}
	}
}

func TestConfigurationAndCommandErrors(t *testing.T) {
	t.Parallel()

	configErr := &ConfigError{Field: "chat_id", Reason: "missing"}
	if got := configErr.Error(); !strings.Contains(got, "chat_id") || !strings.Contains(got, "missing") {
		t.Fatalf("ConfigError.Error() = %q", got)
	}
	commandErr := &CommandError{Code: "unknown"}
	if got := commandErr.Error(); !strings.Contains(got, "unknown") {
		t.Fatalf("CommandError.Error() = %q", got)
	}
	pollingErr := &PollingError{Failures: 3, Err: errors.New("offline")}
	if got := pollingErr.Error(); !strings.Contains(got, "3") {
		t.Fatalf("PollingError.Error() = %q", got)
	}
	if pollingErr.Unwrap() == nil {
		t.Fatal("PollingError.Unwrap() = nil")
	}
}
