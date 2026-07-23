package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestClientGetMeAndSendMessage(t *testing.T) {
	t.Parallel()

	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("HTTP method = %s, want POST", request.Method)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/getMe"):
			methods = append(methods, "getMe")
			writeJSON(response, `{"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"Deck","username":"deck_bot"}}`)
		case strings.HasSuffix(request.URL.Path, "/sendMessage"):
			methods = append(methods, "sendMessage")
			var payload sendMessagePayload
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Errorf("decode payload: %v", err)
			}
			if payload.ChatID != -100 || payload.Text != "hello" {
				t.Errorf("payload = %#v", payload)
			}
			if payload.ReplyParameters == nil || payload.ReplyParameters.MessageID != 22 {
				t.Errorf("reply parameters = %#v", payload.ReplyParameters)
			}
			writeJSON(response, `{"ok":true,"result":{"message_id":23,"chat":{"id":-100},"date":1,"text":"hello"}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := mustTestClient(t, server.URL, ClientOptions{})
	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if user.ID != testBotID || !user.IsBot || user.Username != "deck_bot" {
		t.Fatalf("GetMe() = %#v", user)
	}

	message, err := client.SendMessage(context.Background(), SendMessageRequest{
		ChatID:           -100,
		Text:             "hello",
		ReplyToMessageID: 22,
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if message.MessageID != 23 || message.Chat.ID != -100 {
		t.Fatalf("SendMessage() = %#v", message)
	}
	if got := strings.Join(methods, ","); got != "getMe,sendMessage" {
		t.Fatalf("methods = %q", got)
	}
}

func TestClientGetUpdatesBoundsAndPayload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload getUpdatesPayload
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Offset != 19 || payload.Limit != 2 || payload.Timeout != 2 {
			t.Errorf("payload = %#v", payload)
		}
		if len(payload.AllowedUpdates) != 1 || payload.AllowedUpdates[0] != "message" {
			t.Errorf("allowed_updates = %#v", payload.AllowedUpdates)
		}
		writeJSON(response, `{"ok":true,"result":[{"update_id":19},{"update_id":20}]}`)
	}))
	defer server.Close()

	client := mustTestClient(t, server.URL, ClientOptions{})
	updates, err := client.GetUpdates(context.Background(), GetUpdatesRequest{
		Offset:  19,
		Limit:   2,
		Timeout: 1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("GetUpdates() error = %v", err)
	}
	if len(updates) != 2 || updates[0].UpdateID != 19 || updates[1].UpdateID != 20 {
		t.Fatalf("updates = %#v", updates)
	}

	invalid := []GetUpdatesRequest{
		{Offset: -1, Limit: 1, Timeout: time.Second},
		{Limit: 0, Timeout: time.Second},
		{Limit: 101, Timeout: time.Second},
		{Limit: 1, Timeout: time.Millisecond},
		{Limit: 1, Timeout: 51 * time.Second},
	}
	for _, request := range invalid {
		if _, err := client.GetUpdates(context.Background(), request); err == nil {
			t.Fatalf("GetUpdates(%#v) returned nil error", request)
		}
	}
}

func TestClientTypedAPIErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTooManyRequests)
		writeJSON(response, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7,"migrate_to_chat_id":-99}}`)
	}))
	defer server.Close()

	client := mustTestClient(t, server.URL, ClientOptions{})
	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Code != 429 ||
		apiErr.Description != "Too Many Requests" ||
		apiErr.RetryAfter != 7*time.Second ||
		apiErr.MigrateToChatID != -99 ||
		!apiErr.Temporary() {
		t.Fatalf("APIError = %#v", apiErr)
	}
}

func TestClientResponseAndProtocolBounds(t *testing.T) {
	t.Parallel()

	t.Run("response too large", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(response, strings.Repeat("x", 1025))
		}))
		defer server.Close()

		client := mustTestClient(t, server.URL, ClientOptions{MaxResponseBody: 1024})
		_, err := client.GetMe(context.Background())
		var largeErr *ResponseTooLargeError
		if !errors.As(err, &largeErr) {
			t.Fatalf("error type = %T, want *ResponseTooLargeError", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(response, `{`)
		}))
		defer server.Close()

		client := mustTestClient(t, server.URL, ClientOptions{})
		_, err := client.GetMe(context.Background())
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("error type = %T, want *ProtocolError", err)
		}
	})

	t.Run("non bot getMe", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(response, `{"ok":true,"result":{"id":7,"is_bot":false}}`)
		}))
		defer server.Close()

		client := mustTestClient(t, server.URL, ClientOptions{})
		_, err := client.GetMe(context.Background())
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("error type = %T, want *ProtocolError", err)
		}
	})

	t.Run("wrong bot identity", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(response, `{"ok":true,"result":{"id":7,"is_bot":true}}`)
		}))
		defer server.Close()

		client := mustTestClient(t, server.URL, ClientOptions{})
		_, err := client.GetMe(context.Background())
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("error type = %T, want *ProtocolError", err)
		}
	})

	t.Run("result exceeds update limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(response, `{"ok":true,"result":[{"update_id":1},{"update_id":2}]}`)
		}))
		defer server.Close()

		client := mustTestClient(t, server.URL, ClientOptions{})
		_, err := client.GetUpdates(context.Background(), GetUpdatesRequest{
			Limit:   1,
			Timeout: time.Second,
		})
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("error type = %T, want *ProtocolError", err)
		}
	})
}

func TestClientTimeoutAndTokenRedaction(t *testing.T) {
	t.Parallel()

	t.Run("deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			writeJSON(response, `{"ok":true,"result":{"id":7,"is_bot":true}}`)
		}))
		defer server.Close()

		client := mustTestClient(t, server.URL, ClientOptions{RequestTimeout: 10 * time.Millisecond})
		_, err := client.GetMe(context.Background())
		var transportErr *TransportError
		if !errors.As(err, &transportErr) {
			t.Fatalf("error type = %T, want *TransportError", err)
		}
	})

	t.Run("transport text redacts token", func(t *testing.T) {
		httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("failed URL " + request.URL.String())
		})}
		client, err := NewClient(testBotToken, ClientOptions{HTTPClient: httpClient})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		_, err = client.GetMe(context.Background())
		if err == nil {
			t.Fatal("GetMe() returned nil error")
		}
		if strings.Contains(err.Error(), testBotToken) {
			t.Fatalf("error leaks token: %v", err)
		}
	})
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var redirected atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	client := mustTestClient(t, source.URL, ClientOptions{})
	_, err := client.GetMe(context.Background())
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusFound {
		t.Fatalf("error = %v, want HTTPStatusError 302", err)
	}
	if redirected.Load() != 0 {
		t.Fatal("client followed redirect and exposed credentials")
	}
}

func TestClientRejectsInsecureRemoteEndpoint(t *testing.T) {
	t.Parallel()

	_, err := NewClient(testBotToken, ClientOptions{BaseURL: "http://example.com"})
	var configErr *ConfigError
	if !errors.As(err, &configErr) || configErr.Field != "base_url" {
		t.Fatalf("error = %v, want base_url ConfigError", err)
	}
}

func TestClientOptionAndRequestValidation(t *testing.T) {
	t.Parallel()

	client := mustTestClient(t, "https://api.telegram.org", ClientOptions{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport called for invalid request")
			return nil, nil
		})},
	})
	invalidMessages := []SendMessageRequest{
		{Text: "hello"},
		{ChatID: 1},
		{ChatID: 1, Text: strings.Repeat("x", MaxTelegramMessageRunes+1)},
		{ChatID: 1, Text: "hello", ReplyToMessageID: -1},
	}
	for _, request := range invalidMessages {
		if _, err := client.SendMessage(context.Background(), request); err == nil {
			t.Fatalf("SendMessage(%#v) returned nil error", request)
		}
	}

	options := []ClientOptions{
		{BaseURL: "://bad"},
		{BaseURL: "ftp://example.com"},
		{BaseURL: "https://user:pass@example.com"},
		{BaseURL: "https://example.com?query=1"},
		{BaseURL: "https://example.com"},
		{RequestTimeout: time.Microsecond},
		{RequestTimeout: 2 * time.Minute},
		{LongPollGrace: time.Millisecond},
		{LongPollGrace: time.Minute},
		{MaxResponseBody: 100},
		{MaxResponseBody: absoluteMaxResponseBytes + 1},
	}
	for _, option := range options {
		if _, err := NewClient(testBotToken, option); err == nil {
			t.Fatalf("NewClient(%#v) returned nil error", option)
		}
	}
	if _, err := NewClient("invalid", ClientOptions{}); err == nil {
		t.Fatal("NewClient(invalid token) returned nil error")
	}
}

func TestClientAdditionalProtocolErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		errorType  any
	}{
		{
			name:       "plain HTTP failure",
			statusCode: http.StatusBadGateway,
			body:       "upstream failed",
			errorType:  &HTTPStatusError{},
		},
		{
			name:       "false response without code",
			statusCode: http.StatusOK,
			body:       `{"ok":false}`,
			errorType:  &ProtocolError{},
		},
		{
			name:       "missing result",
			statusCode: http.StatusOK,
			body:       `{"ok":true}`,
			errorType:  &ProtocolError{},
		},
		{
			name:       "invalid result shape",
			statusCode: http.StatusOK,
			body:       `{"ok":true,"result":"not-a-user"}`,
			errorType:  &ProtocolError{},
		},
		{
			name:       "successful envelope with failure status",
			statusCode: http.StatusBadGateway,
			body:       `{"ok":true,"result":{"id":1,"is_bot":true}}`,
			errorType:  &HTTPStatusError{},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.statusCode)
				writeJSON(response, test.body)
			}))
			defer server.Close()

			client := mustTestClient(t, server.URL, ClientOptions{})
			_, err := client.GetMe(context.Background())
			switch test.errorType.(type) {
			case *HTTPStatusError:
				var target *HTTPStatusError
				if !errors.As(err, &target) {
					t.Fatalf("error = %T, want *HTTPStatusError", err)
				}
			case *ProtocolError:
				var target *ProtocolError
				if !errors.As(err, &target) {
					t.Fatalf("error = %T, want *ProtocolError", err)
				}
			}
		})
	}
}

func TestClientCapsAPIErrorParameters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, `{"ok":false,"error_code":500,"description":"`+
			strings.Repeat("x", 600)+
			`","parameters":{"retry_after":999999}}`)
	}))
	defer server.Close()

	client := mustTestClient(t, server.URL, ClientOptions{})
	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T, want *APIError", err)
	}
	if utf8.RuneCountInString(apiErr.Description) != 512 || apiErr.RetryAfter != 24*time.Hour {
		t.Fatalf("APIError = %#v", apiErr)
	}
}

func mustTestClient(t *testing.T, baseURL string, overrides ClientOptions) *Client {
	t.Helper()
	overrides.BaseURL = baseURL
	client, err := NewClient(testBotToken, overrides)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func writeJSON(response http.ResponseWriter, body string) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(response, body)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
