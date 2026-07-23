package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const (
	APIVersion            = "v1"
	defaultRequestTimeout = 2 * time.Second
	maxResponseBodyBytes  = 4 << 20
)

var (
	ErrInvalidSocketPath = errors.New("host agent socket path must be absolute")
	ErrInvalidRequest    = errors.New("host agent request is invalid")
	ErrProtocol          = errors.New("host agent protocol error")
)

type Capabilities struct {
	Discovery   bool `json:"discovery"`
	Dial        bool `json:"dial"`
	AnswerCall  bool `json:"answer_call"`
	HangupCall  bool `json:"hangup_call"`
	RejectCall  bool `json:"reject_call"`
	SendDTMF    bool `json:"send_dtmf"`
	SendMessage bool `json:"send_message"`
	Snapshot    bool `json:"snapshot"`
}

type ProviderHealth struct {
	Name         string       `json:"name"`
	Available    bool         `json:"available"`
	Capabilities Capabilities `json:"capabilities"`
}

type Health struct {
	Status       string         `json:"status"`
	APIVersion   string         `json:"api_version"`
	AgentVersion string         `json:"agent_version"`
	Provider     ProviderHealth `json:"provider"`
}

type LineCapabilities struct {
	VoiceInterface     bool `json:"voice_interface"`
	MessagingInterface bool `json:"messaging_interface"`
	Dial               bool `json:"dial"`
	AnswerCall         bool `json:"answer_call"`
	HangupCall         bool `json:"hangup_call"`
	RejectCall         bool `json:"reject_call"`
	SendDTMF           bool `json:"send_dtmf"`
	SendMessage        bool `json:"send_message"`
	Media              bool `json:"media"`
}

type Line struct {
	ID                  string           `json:"id"`
	Manufacturer        string           `json:"manufacturer"`
	Model               string           `json:"model"`
	Revision            string           `json:"revision"`
	DeviceIdentifier    string           `json:"device_identifier"`
	EquipmentIdentifier string           `json:"equipment_identifier"`
	Device              string           `json:"device"`
	PhysicalDevice      string           `json:"physical_device"`
	Plugin              string           `json:"plugin"`
	PrimaryPort         string           `json:"primary_port"`
	State               string           `json:"state"`
	StateCode           int32            `json:"state_code"`
	AccessTechnologies  uint32           `json:"access_technologies"`
	SignalQualityKnown  bool             `json:"signal_quality_known"`
	SignalQuality       uint32           `json:"signal_quality"`
	OwnNumbers          []string         `json:"own_numbers"`
	SIMIdentifier       string           `json:"sim_identifier"`
	IMSI                string           `json:"imsi"`
	OperatorIdentifier  string           `json:"operator_identifier"`
	OperatorName        string           `json:"operator_name"`
	Capabilities        LineCapabilities `json:"capabilities"`
}

type Call struct {
	ID        string `json:"id"`
	LineID    string `json:"line_id"`
	Number    string `json:"number"`
	Direction string `json:"direction"`
	State     string `json:"state"`
	StateCode int32  `json:"state_code"`
	Bearer    string `json:"bearer"`
}

type Message struct {
	ID        string `json:"id"`
	LineID    string `json:"line_id"`
	Number    string `json:"number"`
	Text      string `json:"text"`
	Direction string `json:"direction"`
	State     string `json:"state"`
	StateCode uint32 `json:"state_code"`
	Timestamp string `json:"timestamp"`
}

type Snapshot struct {
	Revision   uint64    `json:"revision"`
	ObservedAt time.Time `json:"observed_at"`
	Lines      []Line    `json:"lines"`
	Calls      []Call    `json:"calls"`
	Messages   []Message `json:"messages"`
}

type StartCallRequest struct {
	RequestID string `json:"request_id"`
	LineID    string `json:"line_id"`
	Number    string `json:"number"`
}

type CallActionRequest struct {
	RequestID string `json:"request_id"`
}

type DTMFRequest struct {
	RequestID string `json:"request_id"`
	Digits    string `json:"digits"`
}

type SendMessageRequest struct {
	RequestID string `json:"request_id"`
	LineID    string `json:"line_id"`
	Number    string `json:"number"`
	Text      string `json:"text"`
}

type OperationError struct {
	Status    int
	Code      string
	Operation string
	Message   string
}

func (e *OperationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Operation == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Message)
}

type Client struct {
	httpClient *http.Client
}

func New(socketPath string, timeout time.Duration) (*Client, error) {
	socketPath = filepath.Clean(socketPath)
	if !filepath.IsAbs(socketPath) {
		return nil, ErrInvalidSocketPath
	}
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Client{httpClient: &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}}, nil
}

func (client *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	if err := client.doJSON(ctx, http.MethodGet, "/v1/health", nil, http.StatusOK, &health); err != nil {
		return Health{}, err
	}
	if health.APIVersion != APIVersion {
		return Health{}, fmt.Errorf("host agent API version %q is incompatible with %q", health.APIVersion, APIVersion)
	}
	return health, nil
}

func (client *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	if err := client.doJSON(ctx, http.MethodGet, "/v1/snapshot", nil, http.StatusOK, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if snapshot.Lines == nil {
		snapshot.Lines = []Line{}
	}
	if snapshot.Calls == nil {
		snapshot.Calls = []Call{}
	}
	if snapshot.Messages == nil {
		snapshot.Messages = []Message{}
	}
	return snapshot, nil
}

func (client *Client) StartCall(ctx context.Context, request StartCallRequest) (Call, error) {
	if strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(request.LineID) == "" ||
		strings.TrimSpace(request.Number) == "" {
		return Call{}, ErrInvalidRequest
	}
	var call Call
	if err := client.doJSON(ctx, http.MethodPost, "/v1/calls", request, http.StatusCreated, &call); err != nil {
		return Call{}, err
	}
	return call, nil
}

func (client *Client) CallAction(
	ctx context.Context,
	callID string,
	action string,
	request CallActionRequest,
) (Call, error) {
	callID = strings.TrimSpace(callID)
	action = strings.TrimSpace(action)
	if callID == "" || strings.TrimSpace(request.RequestID) == "" {
		return Call{}, ErrInvalidRequest
	}
	switch action {
	case "answer", "reject", "hangup":
	default:
		return Call{}, ErrInvalidRequest
	}
	var call Call
	path := "/v1/calls/" + url.PathEscape(callID) + "/" + action
	if err := client.doJSON(ctx, http.MethodPost, path, request, http.StatusOK, &call); err != nil {
		return Call{}, err
	}
	return call, nil
}

func (client *Client) SendDTMF(ctx context.Context, callID string, request DTMFRequest) (Call, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" || strings.TrimSpace(request.RequestID) == "" || strings.TrimSpace(request.Digits) == "" {
		return Call{}, ErrInvalidRequest
	}
	var call Call
	path := "/v1/calls/" + url.PathEscape(callID) + "/dtmf"
	if err := client.doJSON(ctx, http.MethodPost, path, request, http.StatusOK, &call); err != nil {
		return Call{}, err
	}
	return call, nil
}

func (client *Client) SendMessage(ctx context.Context, request SendMessageRequest) (Message, error) {
	if strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(request.LineID) == "" ||
		strings.TrimSpace(request.Number) == "" ||
		strings.TrimSpace(request.Text) == "" {
		return Message{}, ErrInvalidRequest
	}
	var message Message
	if err := client.doJSON(ctx, http.MethodPost, "/v1/messages", request, http.StatusCreated, &message); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	input any,
	expectedStatus int,
	output any,
) error {
	if client == nil || client.httpClient == nil {
		return errors.New("host agent client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode host agent request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://modemdeck-agent"+path, body)
	if err != nil {
		return fmt.Errorf("create host agent request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request host agent: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read host agent response: %w", err)
	}
	if len(responseBody) > maxResponseBodyBytes {
		return fmt.Errorf("%w: host agent response is too large", ErrProtocol)
	}
	if response.StatusCode != expectedStatus {
		return decodeOperationError(response.StatusCode, responseBody)
	}
	if output == nil {
		if len(bytes.TrimSpace(responseBody)) != 0 {
			return fmt.Errorf("%w: unexpected response body", ErrProtocol)
		}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrProtocol, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("%w: response contains multiple JSON values", ErrProtocol)
	}
	return nil
}

func decodeOperationError(status int, body []byte) error {
	var response struct {
		Error struct {
			Code      string `json:"code"`
			Operation string `json:"operation"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil || strings.TrimSpace(response.Error.Code) == "" {
		return fmt.Errorf("%w: host agent returned HTTP %d", ErrProtocol, status)
	}
	return &OperationError{
		Status:    status,
		Code:      response.Error.Code,
		Operation: response.Error.Operation,
		Message:   response.Error.Message,
	}
}

func (client *Client) CloseIdleConnections() {
	if client == nil || client.httpClient == nil {
		return
	}
	client.httpClient.CloseIdleConnections()
}
