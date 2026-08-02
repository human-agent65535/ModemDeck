package agentclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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
	APIVersion                    = "v1"
	defaultRequestTimeout         = 2 * time.Second
	defaultEventIdleLimit         = 5 * time.Second
	defaultDiagnosticLogIdleLimit = 40 * time.Second
	maxResponseBodyBytes          = 4 << 20
	controlLeaseHeader            = "X-ModemDeck-Controller"
)

var (
	ErrInvalidSocketPath = errors.New("host agent socket path must be absolute")
	ErrInvalidRequest    = errors.New("host agent request is invalid")
	ErrProtocol          = errors.New("host agent protocol error")
)

type Capabilities struct {
	Discovery           bool `json:"discovery"`
	Telemetry           bool `json:"telemetry"`
	Events              bool `json:"events"`
	ControlLease        bool `json:"control_lease"`
	DeviceConfiguration bool `json:"device_configuration"`
	Network             bool `json:"network"`
	NetworkSelection    bool `json:"network_selection"`
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

type ServingRadio struct {
	AccessTechnology string  `json:"access_technology"`
	DuplexMode       string  `json:"duplex_mode"`
	Band             string  `json:"band"`
	Channel          *uint32 `json:"channel"`
	ChannelType      string  `json:"channel_type"`
	Source           string  `json:"source"`
}

type Line struct {
	ID                       string                    `json:"id"`
	Manufacturer             string                    `json:"manufacturer"`
	Model                    string                    `json:"model"`
	Revision                 string                    `json:"revision"`
	HardwareRevision         string                    `json:"hardware_revision"`
	DeviceIdentifier         string                    `json:"device_identifier"`
	EquipmentIdentifier      string                    `json:"equipment_identifier"`
	IdentityPersistent       bool                      `json:"identity_persistent"`
	IdentitySource           string                    `json:"identity_source"`
	SavedPolicySupported     bool                      `json:"saved_policy_supported"`
	UnsupportedPolicyReason  string                    `json:"unsupported_policy_reason"`
	Device                   string                    `json:"device"`
	PhysicalDevice           string                    `json:"physical_device"`
	Drivers                  []string                  `json:"drivers"`
	Plugin                   string                    `json:"plugin"`
	PrimaryPort              string                    `json:"primary_port"`
	AudioPort                string                    `json:"audio_port"`
	Ports                    []ModemPort               `json:"ports"`
	State                    string                    `json:"state"`
	StateCode                int32                     `json:"state_code"`
	FailureReason            string                    `json:"failure_reason"`
	FailureReasonCode        uint32                    `json:"failure_reason_code"`
	PowerStateCode           uint32                    `json:"power_state_code"`
	RadioDesiredEnabled      bool                      `json:"radio_desired_enabled"`
	RadioDesiredEnabledKnown bool                      `json:"radio_desired_enabled_known"`
	AccessTechnologies       uint32                    `json:"access_technologies"`
	AccessTechnologiesKnown  bool                      `json:"access_technologies_known"`
	ServingRadio             *ServingRadio             `json:"serving_radio"`
	SignalQualityKnown       bool                      `json:"signal_quality_known"`
	SignalQuality            uint32                    `json:"signal_quality"`
	SignalQualityRecent      bool                      `json:"signal_quality_recent"`
	SignalMetricsRecent      bool                      `json:"signal_metrics_recent"`
	SignalDBM                *float64                  `json:"signal_dbm"`
	SignalRSRP               *float64                  `json:"signal_rsrp"`
	SignalRSRQ               *float64                  `json:"signal_rsrq"`
	SignalSNR                *float64                  `json:"signal_snr"`
	OwnNumbers               []string                  `json:"own_numbers"`
	SIMPresent               bool                      `json:"sim_present"`
	SIMPath                  string                    `json:"sim_path"`
	SIMIdentifier            string                    `json:"sim_identifier"`
	IMSI                     string                    `json:"imsi"`
	HomeOperatorCode         string                    `json:"home_operator_code"`
	HomeOperatorName         string                    `json:"home_operator_name"`
	HomeCountryISO           string                    `json:"home_country_iso"`
	ServingOperatorCode      string                    `json:"serving_operator_code"`
	ServingOperatorName      string                    `json:"serving_operator_name"`
	ServingCountryISO        string                    `json:"serving_country_iso"`
	RegistrationStateKnown   bool                      `json:"registration_state_known"`
	RegistrationStateCode    uint32                    `json:"registration_state_code"`
	RegistrationState        string                    `json:"registration_state"`
	Roaming                  bool                      `json:"roaming"`
	OperatorIdentifier       string                    `json:"operator_identifier"`
	OperatorName             string                    `json:"operator_name"`
	EmergencyNumbers         []string                  `json:"emergency_numbers"`
	EmergencyOnly            bool                      `json:"emergency_only"`
	CallIDs                  []string                  `json:"call_ids"`
	MessageIDs               []string                  `json:"message_ids"`
	SupportedMessageStorages []uint32                  `json:"supported_message_storages"`
	DefaultMessageStorage    uint32                    `json:"default_message_storage"`
	VoiceVerification        *VoiceRuntimeVerification `json:"voice_verification,omitempty"`
	Capabilities             LineCapabilities          `json:"capabilities"`
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
	ID                    string `json:"id"`
	LineID                string `json:"line_id"`
	Number                string `json:"number"`
	Text                  string `json:"text"`
	Direction             string `json:"direction"`
	State                 string `json:"state"`
	StateCode             uint32 `json:"state_code"`
	MessageReference      uint32 `json:"message_reference"`
	MessageReferenceKnown bool   `json:"message_reference_known"`
	Timestamp             string `json:"timestamp"`
}

type MessageDeliveryReport struct {
	ID                    string `json:"id"`
	LineID                string `json:"line_id"`
	Number                string `json:"number"`
	MessageReference      uint32 `json:"message_reference"`
	MessageReferenceKnown bool   `json:"message_reference_known"`
	DeliveryState         uint32 `json:"delivery_state"`
	DeliveryStateKnown    bool   `json:"delivery_state_known"`
	Timestamp             string `json:"timestamp"`
}

type Snapshot struct {
	Revision        string                  `json:"revision"`
	ObservedAt      time.Time               `json:"observed_at"`
	Lines           []Line                  `json:"lines"`
	Calls           []Call                  `json:"calls"`
	Messages        []Message               `json:"messages"`
	DeliveryReports []MessageDeliveryReport `json:"delivery_reports"`
}

type LineTelemetry struct {
	ID                      string        `json:"id"`
	AccessTechnologies      uint32        `json:"access_technologies"`
	AccessTechnologiesKnown bool          `json:"access_technologies_known"`
	SignalQualityKnown      bool          `json:"signal_quality_known"`
	SignalQuality           uint32        `json:"signal_quality"`
	SignalQualityRecent     bool          `json:"signal_quality_recent"`
	SignalMetricsRecent     bool          `json:"signal_metrics_recent"`
	SignalDBM               *float64      `json:"signal_dbm"`
	SignalRSRP              *float64      `json:"signal_rsrp"`
	SignalRSRQ              *float64      `json:"signal_rsrq"`
	SignalSNR               *float64      `json:"signal_snr"`
	ServingRadio            *ServingRadio `json:"serving_radio"`
}

type TelemetrySnapshot struct {
	BootEpoch  string          `json:"boot_epoch"`
	ObservedAt time.Time       `json:"observed_at"`
	Lines      []LineTelemetry `json:"lines"`
}

type CommandReceipt struct {
	RequestID                 string `json:"request_id"`
	ResourceID                string `json:"resource_id"`
	DeliveryReportUnsupported bool   `json:"delivery_report_unsupported"`
}

type CallMediaActivation struct {
	CallID          string           `json:"call_id"`
	MediaRouting    string           `json:"media_routing"`
	MediaAvailable  bool             `json:"media_available"`
	MediaConfigured bool             `json:"media_configured"`
	AudioPort       string           `json:"audio_port"`
	AudioFormat     *CallAudioFormat `json:"audio_format"`
	Reason          string           `json:"reason"`
}

type ControlLeaseStatus struct {
	ControllerID string    `json:"controller_id"`
	ExpiresAt    time.Time `json:"expires_at"`
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
	RequestID               string `json:"request_id"`
	LineID                  string `json:"line_id"`
	Number                  string `json:"number"`
	Text                    string `json:"text"`
	DeliveryReportRequested bool   `json:"delivery_report_requested"`
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
	httpClient             *http.Client
	requestTimeout         time.Duration
	eventIdleLimit         time.Duration
	diagnosticLogIdleLimit time.Duration
	controllerID           string
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
	controllerID, err := newControllerID()
	if err != nil {
		return nil, err
	}
	return &Client{
		httpClient:             &http.Client{Transport: transport},
		requestTimeout:         timeout,
		eventIdleLimit:         defaultEventIdleLimit,
		diagnosticLogIdleLimit: defaultDiagnosticLogIdleLimit,
		controllerID:           controllerID,
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
	if snapshot.DeliveryReports == nil {
		snapshot.DeliveryReports = []MessageDeliveryReport{}
	}
	return snapshot, nil
}

func (client *Client) Telemetry(ctx context.Context) (TelemetrySnapshot, error) {
	var snapshot TelemetrySnapshot
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		"/v1/telemetry",
		nil,
		http.StatusOK,
		&snapshot,
	); err != nil {
		return TelemetrySnapshot{}, err
	}
	if snapshot.Lines == nil {
		snapshot.Lines = []LineTelemetry{}
	}
	return snapshot, nil
}

func (client *Client) WatchChanges(ctx context.Context, notify func()) error {
	if client == nil || client.httpClient == nil {
		return errors.New("host agent client is not initialized")
	}
	if notify == nil {
		return ErrInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://modemdeck-agent/v1/events",
		nil,
	)
	if err != nil {
		return fmt.Errorf("create host agent event request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set(controlLeaseHeader, client.controllerID)
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("request host agent events: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
		if readErr != nil {
			return fmt.Errorf("read host agent event error: %w", readErr)
		}
		if len(responseBody) > maxResponseBodyBytes {
			return fmt.Errorf("%w: host agent response is too large", ErrProtocol)
		}
		return decodeOperationError(response.StatusCode, responseBody)
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return fmt.Errorf("%w: host agent event stream has content type %q", ErrProtocol, contentType)
	}

	type scanResult struct {
		line string
		err  error
		done bool
	}
	scanContext, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	results := make(chan scanResult)
	scannerDone := make(chan struct{})
	go func() {
		defer close(scannerDone)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			select {
			case results <- scanResult{line: scanner.Text()}:
			case <-scanContext.Done():
				return
			}
		}
		result := scanResult{err: scanner.Err(), done: true}
		select {
		case results <- result:
		case <-scanContext.Done():
		}
	}()

	idleLimit := client.eventIdleLimit
	if idleLimit <= 0 {
		idleLimit = defaultEventIdleLimit
	}
	idle := time.NewTimer(idleLimit)
	defer idle.Stop()
	eventName := ""
	for {
		select {
		case <-ctx.Done():
			cancelScan()
			_ = response.Body.Close()
			<-scannerDone
			return nil
		case <-idle.C:
			cancelScan()
			_ = response.Body.Close()
			<-scannerDone
			return fmt.Errorf(
				"read host agent events: no activity for %s",
				idleLimit,
			)
		case result := <-results:
			if result.done {
				if ctx.Err() != nil {
					return nil
				}
				if result.err != nil {
					return fmt.Errorf("read host agent events: %w", result.err)
				}
				return io.ErrUnexpectedEOF
			}
			resetTimer(idle, idleLimit)
			line := strings.TrimSuffix(result.line, "\r")
			if line == "" {
				if eventName == "ready" || eventName == "change" {
					notify()
				}
				eventName = ""
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if value, found := strings.CutPrefix(line, "event:"); found {
				eventName = strings.TrimSpace(value)
			}
		}
	}
}

func (client *Client) RenewControlLease(
	ctx context.Context,
) (ControlLeaseStatus, error) {
	var status ControlLeaseStatus
	if err := client.doJSON(
		ctx,
		http.MethodPut,
		"/v1/control-lease",
		nil,
		http.StatusOK,
		&status,
	); err != nil {
		return ControlLeaseStatus{}, err
	}
	if status.ControllerID != client.controllerID || status.ExpiresAt.IsZero() {
		return ControlLeaseStatus{}, fmt.Errorf(
			"%w: invalid control lease response",
			ErrProtocol,
		)
	}
	return status, nil
}

func (client *Client) ReleaseControlLease(ctx context.Context) error {
	return client.doJSON(
		ctx,
		http.MethodDelete,
		"/v1/control-lease",
		nil,
		http.StatusNoContent,
		nil,
	)
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

func (client *Client) ActivateCallMedia(
	ctx context.Context,
	callID string,
	request CallActionRequest,
) (CallMediaActivation, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" || strings.TrimSpace(request.RequestID) == "" {
		return CallMediaActivation{}, ErrInvalidRequest
	}
	var activation CallMediaActivation
	path := "/v1/calls/" + url.PathEscape(callID) + "/media/activate"
	if err := client.doJSON(
		ctx,
		http.MethodPost,
		path,
		request,
		http.StatusOK,
		&activation,
	); err != nil {
		return CallMediaActivation{}, err
	}
	if activation.CallID != callID {
		return CallMediaActivation{}, fmt.Errorf(
			"%w: call media activation references %q instead of %q",
			ErrProtocol,
			activation.CallID,
			callID,
		)
	}
	return activation, nil
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

func (client *Client) DeleteMessage(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ErrInvalidRequest
	}
	return client.doJSON(
		ctx,
		http.MethodDelete,
		"/v1/messages/"+url.PathEscape(messageID),
		nil,
		http.StatusNoContent,
		nil,
	)
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
	request.Header.Set(controlLeaseHeader, client.controllerID)
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

func newControllerID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate host agent controller id: %w", err)
	}
	return "app-" + hex.EncodeToString(random), nil
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
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
