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
	Discovery           bool `json:"discovery"`
	DeviceConfiguration bool `json:"device_configuration"`
	Network             bool `json:"network"`
	Proxy               bool `json:"proxy"`
	Dial                bool `json:"dial"`
	AnswerCall          bool `json:"answer_call"`
	HangupCall          bool `json:"hangup_call"`
	RejectCall          bool `json:"reject_call"`
	SendDTMF            bool `json:"send_dtmf"`
	SendMessage         bool `json:"send_message"`
	SIMManagement       bool `json:"sim_management"`
	ConnectionProfiles  bool `json:"connection_profiles"`
	USSD                bool `json:"ussd"`
	Snapshot            bool `json:"snapshot"`
	Media               bool `json:"media"`
}

type ProviderHealth struct {
	Name           string       `json:"name"`
	Available      bool         `json:"available"`
	BootEpoch      string       `json:"boot_epoch"`
	RuntimeVersion string       `json:"runtime_version"`
	Capabilities   Capabilities `json:"capabilities"`
}

type Health struct {
	Status       string         `json:"status"`
	APIVersion   string         `json:"api_version"`
	AgentVersion string         `json:"agent_version"`
	Provider     ProviderHealth `json:"provider"`
}

type LineCapabilities struct {
	ModemInterface     bool `json:"modem_interface"`
	SIMInterface       bool `json:"sim_interface"`
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
	ID                       string           `json:"id"`
	Manufacturer             string           `json:"manufacturer"`
	Model                    string           `json:"model"`
	Revision                 string           `json:"revision"`
	HardwareRevision         string           `json:"hardware_revision"`
	DeviceIdentifier         string           `json:"device_identifier"`
	EquipmentIdentifier      string           `json:"equipment_identifier"`
	IdentityPersistent       bool             `json:"identity_persistent"`
	IdentitySource           string           `json:"identity_source"`
	SavedPolicySupported     bool             `json:"saved_policy_supported"`
	UnsupportedPolicyReason  string           `json:"unsupported_policy_reason"`
	Device                   string           `json:"device"`
	PhysicalDevice           string           `json:"physical_device"`
	Drivers                  []string         `json:"drivers"`
	Plugin                   string           `json:"plugin"`
	PrimaryPort              string           `json:"primary_port"`
	Ports                    []ModemPort      `json:"ports"`
	State                    string           `json:"state"`
	StateCode                int32            `json:"state_code"`
	PowerStateCode           uint32           `json:"power_state_code"`
	AccessTechnologies       uint32           `json:"access_technologies"`
	AccessTechnologiesKnown  bool             `json:"access_technologies_known"`
	SignalQualityKnown       bool             `json:"signal_quality_known"`
	SignalQuality            uint32           `json:"signal_quality"`
	SignalQualityRecent      bool             `json:"signal_quality_recent"`
	SignalDBM                *float64         `json:"signal_dbm"`
	SignalRSRP               *float64         `json:"signal_rsrp"`
	SignalRSRQ               *float64         `json:"signal_rsrq"`
	SignalSNR                *float64         `json:"signal_snr"`
	OwnNumbers               []string         `json:"own_numbers"`
	SIMPresent               bool             `json:"sim_present"`
	SIMPath                  string           `json:"sim_path"`
	SIMIdentifier            string           `json:"sim_identifier"`
	IMSI                     string           `json:"imsi"`
	HomeOperatorCode         string           `json:"home_operator_code"`
	HomeOperatorName         string           `json:"home_operator_name"`
	ServingOperatorCode      string           `json:"serving_operator_code"`
	ServingOperatorName      string           `json:"serving_operator_name"`
	RegistrationStateKnown   bool             `json:"registration_state_known"`
	RegistrationStateCode    uint32           `json:"registration_state_code"`
	RegistrationState        string           `json:"registration_state"`
	Roaming                  bool             `json:"roaming"`
	OperatorIdentifier       string           `json:"operator_identifier"`
	OperatorName             string           `json:"operator_name"`
	EmergencyNumbers         []string         `json:"emergency_numbers"`
	EmergencyOnly            bool             `json:"emergency_only"`
	CallIDs                  []string         `json:"call_ids"`
	MessageIDs               []string         `json:"message_ids"`
	SupportedMessageStorages []uint32         `json:"supported_message_storages"`
	DefaultMessageStorage    uint32           `json:"default_message_storage"`
	Capabilities             LineCapabilities `json:"capabilities"`
}

type CallAudioFormat struct {
	Encoding   string `json:"encoding"`
	Resolution string `json:"resolution"`
	Rate       uint32 `json:"rate"`
}

type Call struct {
	ID              string           `json:"id"`
	LineID          string           `json:"line_id"`
	Number          string           `json:"number"`
	Direction       string           `json:"direction"`
	State           string           `json:"state"`
	StateCode       int32            `json:"state_code"`
	StateReason     string           `json:"state_reason"`
	StateReasonCode int32            `json:"state_reason_code"`
	Multiparty      bool             `json:"multiparty"`
	AudioPort       string           `json:"audio_port"`
	AudioFormat     *CallAudioFormat `json:"audio_format"`
	MediaAvailable  bool             `json:"media_available"`
	MediaConfigured bool             `json:"media_configured"`
	MediaActive     bool             `json:"media_active"`
	Bearer          string           `json:"bearer"`
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
	Revision   string    `json:"revision"`
	ObservedAt time.Time `json:"observed_at"`
	Lines      []Line    `json:"lines"`
	Calls      []Call    `json:"calls"`
	Messages   []Message `json:"messages"`
}

type CommandReceipt struct {
	RequestID  string `json:"request_id"`
	ResourceID string `json:"resource_id"`
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
	httpClient     *http.Client
	requestTimeout time.Duration
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
	return &Client{
		httpClient:     &http.Client{Transport: transport},
		requestTimeout: timeout,
	}, nil
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

func (client *Client) StartCall(ctx context.Context, request StartCallRequest) (CommandReceipt, error) {
	if strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(request.LineID) == "" ||
		strings.TrimSpace(request.Number) == "" {
		return CommandReceipt{}, ErrInvalidRequest
	}
	var receipt CommandReceipt
	if err := client.doJSON(ctx, http.MethodPost, "/v1/calls", request, http.StatusCreated, &receipt); err != nil {
		return CommandReceipt{}, err
	}
	return receipt, nil
}

func (client *Client) CallAction(
	ctx context.Context,
	callID string,
	action string,
	request CallActionRequest,
) (CommandReceipt, error) {
	callID = strings.TrimSpace(callID)
	action = strings.TrimSpace(action)
	if callID == "" || strings.TrimSpace(request.RequestID) == "" {
		return CommandReceipt{}, ErrInvalidRequest
	}
	switch action {
	case "answer", "reject", "hangup":
	default:
		return CommandReceipt{}, ErrInvalidRequest
	}
	var receipt CommandReceipt
	path := "/v1/calls/" + url.PathEscape(callID) + "/" + action
	if err := client.doJSON(ctx, http.MethodPost, path, request, http.StatusOK, &receipt); err != nil {
		return CommandReceipt{}, err
	}
	return receipt, nil
}

func (client *Client) SendDTMF(ctx context.Context, callID string, request DTMFRequest) (CommandReceipt, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" || strings.TrimSpace(request.RequestID) == "" || strings.TrimSpace(request.Digits) == "" {
		return CommandReceipt{}, ErrInvalidRequest
	}
	var receipt CommandReceipt
	path := "/v1/calls/" + url.PathEscape(callID) + "/dtmf"
	if err := client.doJSON(ctx, http.MethodPost, path, request, http.StatusOK, &receipt); err != nil {
		return CommandReceipt{}, err
	}
	return receipt, nil
}

func (client *Client) SendMessage(ctx context.Context, request SendMessageRequest) (CommandReceipt, error) {
	if strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(request.LineID) == "" ||
		strings.TrimSpace(request.Number) == "" ||
		strings.TrimSpace(request.Text) == "" {
		return CommandReceipt{}, ErrInvalidRequest
	}
	var receipt CommandReceipt
	if err := client.doJSON(ctx, http.MethodPost, "/v1/messages", request, http.StatusCreated, &receipt); err != nil {
		return CommandReceipt{}, err
	}
	return receipt, nil
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
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, client.requestTimeout)
		defer cancel()
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
