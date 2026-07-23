package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTelegramBaseURL    = "https://api.telegram.org"
	defaultRequestTimeout     = 10 * time.Second
	defaultLongPollGrace      = 5 * time.Second
	defaultMaxResponseBytes   = int64(1 << 20)
	absoluteMaxResponseBytes  = int64(8 << 20)
	maxTelegramLongPoll       = 50 * time.Second
	maxTelegramUpdatesPerCall = 100
)

type ClientOptions struct {
	// HTTPClient is injectable for transport tests. Its transport must not log
	// request URLs because Telegram embeds the bot token in the API path.
	HTTPClient      *http.Client
	BaseURL         string
	RequestTimeout  time.Duration
	LongPollGrace   time.Duration
	MaxResponseBody int64
}

type Client struct {
	httpClient      *http.Client
	baseURL         string
	token           string
	botID           int64
	requestTimeout  time.Duration
	longPollGrace   time.Duration
	maxResponseBody int64
}

func NewClient(token string, options ClientOptions) (*Client, error) {
	if err := validateBotToken(token); err != nil {
		return nil, &ConfigError{Field: "bot_token", Reason: err.Error()}
	}

	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = defaultTelegramBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil ||
		(parsedBaseURL.Scheme != "https" && parsedBaseURL.Scheme != "http") ||
		parsedBaseURL.Host == "" ||
		parsedBaseURL.User != nil ||
		parsedBaseURL.RawQuery != "" ||
		parsedBaseURL.Fragment != "" {
		return nil, &ConfigError{Field: "base_url", Reason: "must be an absolute HTTP(S) URL without credentials, query, or fragment"}
	}
	if parsedBaseURL.Scheme == "http" && !isLoopbackHost(parsedBaseURL.Hostname()) {
		return nil, &ConfigError{Field: "base_url", Reason: "plain HTTP is allowed only for loopback test endpoints"}
	}
	if !isLoopbackHost(parsedBaseURL.Hostname()) &&
		!strings.EqualFold(parsedBaseURL.Hostname(), "api.telegram.org") {
		return nil, &ConfigError{Field: "base_url", Reason: "must use the official Telegram API or a loopback test endpoint"}
	}

	requestTimeout := options.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = defaultRequestTimeout
	}
	if requestTimeout < time.Millisecond || requestTimeout > time.Minute {
		return nil, &ConfigError{Field: "request_timeout", Reason: "must be between 1ms and 1m"}
	}

	longPollGrace := options.LongPollGrace
	if longPollGrace == 0 {
		longPollGrace = defaultLongPollGrace
	}
	if longPollGrace < time.Second || longPollGrace > 30*time.Second {
		return nil, &ConfigError{Field: "long_poll_grace", Reason: "must be between 1s and 30s"}
	}

	maxResponseBody := options.MaxResponseBody
	if maxResponseBody == 0 {
		maxResponseBody = defaultMaxResponseBytes
	}
	if maxResponseBody < 1024 || maxResponseBody > absoluteMaxResponseBytes {
		return nil, &ConfigError{Field: "max_response_body", Reason: "must be between 1024 bytes and 8 MiB"}
	}

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		httpClient:      &clientCopy,
		baseURL:         strings.TrimRight(baseURL, "/"),
		token:           token,
		botID:           botIDFromToken(token),
		requestTimeout:  requestTimeout,
		longPollGrace:   longPollGrace,
		maxResponseBody: maxResponseBody,
	}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) GetMe(ctx context.Context) (BotUser, error) {
	var user BotUser
	if err := c.call(ctx, "getMe", struct{}{}, c.requestTimeout, &user); err != nil {
		return BotUser{}, err
	}
	if user.ID != c.botID || !user.IsBot || strings.TrimSpace(user.Username) == "" {
		return BotUser{}, &ProtocolError{Method: "getMe", Reason: "result does not match the token bot identity"}
	}
	return user, nil
}

func (c *Client) SendMessage(ctx context.Context, request SendMessageRequest) (Message, error) {
	if request.ChatID == 0 {
		return Message{}, fmt.Errorf("telegram sendMessage chat ID must be non-zero")
	}
	if strings.TrimSpace(request.Text) == "" {
		return Message{}, fmt.Errorf("telegram sendMessage text is required")
	}
	if utf8.RuneCountInString(request.Text) > MaxTelegramMessageRunes {
		return Message{}, fmt.Errorf("telegram sendMessage text exceeds %d runes", MaxTelegramMessageRunes)
	}
	if request.ReplyToMessageID < 0 {
		return Message{}, fmt.Errorf("telegram sendMessage reply message ID must not be negative")
	}

	payload := sendMessagePayload{
		ChatID: request.ChatID,
		Text:   request.Text,
	}
	if request.ReplyToMessageID > 0 {
		payload.ReplyParameters = &replyParameters{MessageID: request.ReplyToMessageID}
	}

	var message Message
	if err := c.call(ctx, "sendMessage", payload, c.requestTimeout, &message); err != nil {
		return Message{}, err
	}
	if message.MessageID <= 0 || message.Chat.ID == 0 {
		return Message{}, &ProtocolError{Method: "sendMessage", Reason: "result is missing message identity"}
	}
	return message, nil
}

func (c *Client) GetUpdates(ctx context.Context, request GetUpdatesRequest) ([]Update, error) {
	if request.Offset < 0 {
		return nil, fmt.Errorf("telegram getUpdates offset must not be negative")
	}
	if request.Limit < 1 || request.Limit > maxTelegramUpdatesPerCall {
		return nil, fmt.Errorf("telegram getUpdates limit must be between 1 and %d", maxTelegramUpdatesPerCall)
	}
	if request.Timeout < time.Second || request.Timeout > maxTelegramLongPoll {
		return nil, fmt.Errorf("telegram getUpdates timeout must be between 1s and %s", maxTelegramLongPoll)
	}

	timeoutSeconds := int((request.Timeout + time.Second - 1) / time.Second)
	payload := getUpdatesPayload{
		Offset:         request.Offset,
		Limit:          request.Limit,
		Timeout:        timeoutSeconds,
		AllowedUpdates: []string{"message"},
	}

	requestDeadline := request.Timeout + c.longPollGrace
	if requestDeadline > time.Minute {
		requestDeadline = time.Minute
	}
	var updates []Update
	if err := c.call(ctx, "getUpdates", payload, requestDeadline, &updates); err != nil {
		return nil, err
	}
	if len(updates) > request.Limit {
		return nil, &ProtocolError{Method: "getUpdates", Reason: "result exceeds requested limit"}
	}
	for _, update := range updates {
		if update.UpdateID < 0 {
			return nil, &ProtocolError{Method: "getUpdates", Reason: "result contains a negative update ID"}
		}
	}
	return updates, nil
}

func (c *Client) call(ctx context.Context, method string, payload any, timeout time.Duration, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return &ProtocolError{Method: method, Reason: "request encoding failed"}
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := c.baseURL + "/bot" + c.token + "/" + method
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return &ProtocolError{Method: method, Reason: "request creation failed"}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "ModemDeck-Telegram/1")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return &TransportError{Method: method, Err: err}
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBody+1))
	if err != nil {
		return &TransportError{Method: method, Err: err}
	}
	if int64(len(responseBody)) > c.maxResponseBody {
		return &ResponseTooLargeError{Limit: c.maxResponseBody}
	}

	var envelope apiEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return &HTTPStatusError{StatusCode: response.StatusCode}
		}
		return &ProtocolError{Method: method, Reason: "response is not valid JSON"}
	}
	if !envelope.OK {
		if envelope.ErrorCode == 0 {
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				return &HTTPStatusError{StatusCode: response.StatusCode}
			}
			return &ProtocolError{Method: method, Reason: "unsuccessful response has no error code"}
		}
		return newAPIError(envelope)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &HTTPStatusError{StatusCode: response.StatusCode}
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return &ProtocolError{Method: method, Reason: "response has no result"}
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return &ProtocolError{Method: method, Reason: "result has unexpected shape"}
	}
	return nil
}

func newAPIError(envelope apiEnvelope) *APIError {
	description := envelope.Description
	description = truncateRunes(description, 512)
	apiErr := &APIError{
		Code:            envelope.ErrorCode,
		Description:     description,
		MigrateToChatID: envelope.Parameters.MigrateToChatID,
	}
	if envelope.Parameters.RetryAfter > 0 {
		retryAfter := envelope.Parameters.RetryAfter
		if retryAfter > int64((24*time.Hour)/time.Second) {
			retryAfter = int64((24 * time.Hour) / time.Second)
		}
		apiErr.RetryAfter = time.Duration(retryAfter) * time.Second
	}
	return apiErr
}

type sendMessagePayload struct {
	ChatID          int64            `json:"chat_id"`
	Text            string           `json:"text"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

type replyParameters struct {
	MessageID int64 `json:"message_id"`
}

type getUpdatesPayload struct {
	Offset         int64    `json:"offset"`
	Limit          int      `json:"limit"`
	Timeout        int      `json:"timeout"`
	AllowedUpdates []string `json:"allowed_updates"`
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  apiParameters   `json:"parameters"`
}

type apiParameters struct {
	MigrateToChatID int64 `json:"migrate_to_chat_id"`
	RetryAfter      int64 `json:"retry_after"`
}
