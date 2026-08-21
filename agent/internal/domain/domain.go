package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const APIVersion = "v1"

const ControlLeaseHeader = "X-ModemDeck-Controller"

var ErrForcedCallTermination = errors.New("call was terminated by forced modem reset")

type AgentCapabilities struct {
	Discovery           bool `json:"discovery"`
	Snapshot            bool `json:"snapshot"`
	Telemetry           bool `json:"telemetry"`
	Events              bool `json:"events"`
	ControlLease        bool `json:"control_lease"`
	DeviceConfiguration bool `json:"device_configuration"`
	Network             bool `json:"network"`
	NetworkSelection    bool `json:"network_selection"`
	Proxy               bool `json:"proxy"`
	Dial                bool `json:"dial"`
	AnswerCall          bool `json:"answer_call"`
	RejectCall          bool `json:"reject_call"`
	HangupCall          bool `json:"hangup_call"`
	SendDTMF            bool `json:"send_dtmf"`
	SendMessage         bool `json:"send_message"`
	SIMManagement       bool `json:"sim_management"`
	ConnectionProfiles  bool `json:"connection_profiles"`
	USSD                bool `json:"ussd"`
	Media               bool `json:"media"`
}

type ProviderHealth struct {
	Name           string            `json:"name"`
	Available      bool              `json:"available"`
	BootEpoch      string            `json:"boot_epoch"`
	RuntimeVersion string            `json:"runtime_version,omitempty"`
	Capabilities   AgentCapabilities `json:"capabilities"`
}

type LineCapabilities struct {
	ModemInterface     bool `json:"modem_interface"`
	SIMInterface       bool `json:"sim_interface"`
	VoiceInterface     bool `json:"voice_interface"`
	MessagingInterface bool `json:"messaging_interface"`
	Dial               bool `json:"dial"`
	AnswerCall         bool `json:"answer_call"`
	RejectCall         bool `json:"reject_call"`
	HangupCall         bool `json:"hangup_call"`
	SendDTMF           bool `json:"send_dtmf"`
	SendMessage        bool `json:"send_message"`
	Media              bool `json:"media"`
}

type VoiceRuntimeVerification struct {
	USBConfiguration string `json:"usb_configuration"`
	MediaRouting     string `json:"media_routing"`
}

// QDC507VoiceRuntimeStatus is the host Agent's current view of the
// module-resident QDC507 voice route. The runtime is restored after each
// module boot; it is intentionally independent from the lifetime of a call.
type QDC507VoiceRuntimeStatus struct {
	Configured     bool
	Ready          bool
	RuntimeVersion string
	Reason         string
}

type ModemPort struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	TypeCode uint32 `json:"type_code"`
}

// ServingRadio describes the radio access currently serving a registered line.
// It is live telemetry, not the modem's configured or supported band list.
type ServingRadio struct {
	AccessTechnology string  `json:"access_technology"`
	DuplexMode       string  `json:"duplex_mode,omitempty"`
	Band             string  `json:"band,omitempty"`
	Channel          *uint32 `json:"channel,omitempty"`
	ChannelType      string  `json:"channel_type,omitempty"`
	Source           string  `json:"source,omitempty"`
}

type Line struct {
	ID                       string      `json:"id"`
	Manufacturer             string      `json:"manufacturer"`
	Model                    string      `json:"model"`
	Revision                 string      `json:"revision"`
	HardwareRevision         string      `json:"hardware_revision,omitempty"`
	DeviceIdentifier         string      `json:"device_identifier"`
	EquipmentIdentifier      string      `json:"equipment_identifier"`
	IdentityPersistent       bool        `json:"identity_persistent"`
	IdentitySource           string      `json:"identity_source,omitempty"`
	SavedPolicySupported     bool        `json:"saved_policy_supported"`
	UnsupportedPolicyReason  string      `json:"unsupported_policy_reason,omitempty"`
	Device                   string      `json:"device"`
	PhysicalDevice           string      `json:"physical_device"`
	Drivers                  []string    `json:"drivers"`
	Plugin                   string      `json:"plugin"`
	PrimaryPort              string      `json:"primary_port"`
	Ports                    []ModemPort `json:"ports,omitempty"`
	State                    string      `json:"state"`
	StateCode                int32       `json:"state_code"`
	FailureReason            string      `json:"failure_reason,omitempty"`
	FailureReasonCode        uint32      `json:"failure_reason_code,omitempty"`
	PowerStateCode           uint32      `json:"power_state_code"`
	RadioDesiredEnabled      bool        `json:"radio_desired_enabled"`
	RadioDesiredEnabledKnown bool        `json:"radio_desired_enabled_known"`
	AccessTechnologies       uint32      `json:"access_technologies"`
	AccessTechnologiesKnown  bool        `json:"access_technologies_known"`
	SignalQualityKnown       bool        `json:"signal_quality_known"`
	SignalQuality            uint32      `json:"signal_quality"`
	SignalQualityRecent      bool        `json:"signal_quality_recent"`
	SignalMetricsRecent      bool        `json:"signal_metrics_recent"`
	SignalDBM                *float64    `json:"signal_dbm,omitempty"`
	SignalRSRP               *float64    `json:"signal_rsrp,omitempty"`
	SignalRSRQ               *float64    `json:"signal_rsrq,omitempty"`
	SignalSNR                *float64    `json:"signal_snr,omitempty"`
	OwnNumbers               []string    `json:"own_numbers"`
	SIMPresent               bool        `json:"sim_present"`
	SIMPath                  string      `json:"sim_path"`
	SIMIdentifier            string      `json:"sim_identifier"`
	IMSI                     string      `json:"imsi"`
	HomeOperatorCode         string      `json:"home_operator_code"`
	HomeOperatorName         string      `json:"home_operator_name"`
	HomeCountryISO           string      `json:"home_country_iso"`
	ServingOperatorCode      string      `json:"serving_operator_code"`
	ServingOperatorName      string      `json:"serving_operator_name"`
	ServingCountryISO        string      `json:"serving_country_iso"`
	RegistrationStateKnown   bool        `json:"registration_state_known"`
	RegistrationStateCode    uint32      `json:"registration_state_code"`
	RegistrationState        string      `json:"registration_state"`
	Roaming                  bool        `json:"roaming"`
	// OperatorIdentifier and OperatorName remain home-operator aliases for API compatibility.
	OperatorIdentifier       string                    `json:"operator_identifier"`
	OperatorName             string                    `json:"operator_name"`
	EmergencyNumbers         []string                  `json:"emergency_numbers"`
	EmergencyOnly            bool                      `json:"emergency_only"`
	CallIDs                  []string                  `json:"call_ids"`
	MessageIDs               []string                  `json:"message_ids"`
	SupportedMessageStorages []uint32                  `json:"supported_message_storages"`
	DefaultMessageStorage    uint32                    `json:"default_message_storage"`
	AudioPort                string                    `json:"audio_port,omitempty"`
	Capabilities             LineCapabilities          `json:"capabilities"`
	VoiceVerification        *VoiceRuntimeVerification `json:"voice_verification,omitempty"`
	ServingRadio             *ServingRadio             `json:"serving_radio,omitempty"`
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
	AudioPort       string           `json:"audio_port,omitempty"`
	AudioFormat     *CallAudioFormat `json:"audio_format,omitempty"`
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

// LineTelemetry contains sampled radio facts that do not participate in the
// durable call, message, or device lifecycle projection.
type LineTelemetry struct {
	ID                      string        `json:"id"`
	AccessTechnologies      uint32        `json:"access_technologies"`
	AccessTechnologiesKnown bool          `json:"access_technologies_known"`
	SignalQualityKnown      bool          `json:"signal_quality_known"`
	SignalQuality           uint32        `json:"signal_quality"`
	SignalQualityRecent     bool          `json:"signal_quality_recent"`
	SignalMetricsRecent     bool          `json:"signal_metrics_recent"`
	SignalDBM               *float64      `json:"signal_dbm,omitempty"`
	SignalRSRP              *float64      `json:"signal_rsrp,omitempty"`
	SignalRSRQ              *float64      `json:"signal_rsrq,omitempty"`
	SignalSNR               *float64      `json:"signal_snr,omitempty"`
	ServingRadio            *ServingRadio `json:"serving_radio,omitempty"`
}

type TelemetrySnapshot struct {
	BootEpoch  string          `json:"boot_epoch"`
	ObservedAt time.Time       `json:"observed_at"`
	Lines      []LineTelemetry `json:"lines"`
}

type TelemetryProvider interface {
	Telemetry(context.Context) (TelemetrySnapshot, error)
}

type ChangeSource interface {
	SubscribeChanges(context.Context) (<-chan struct{}, error)
}

type ControlLeaseStatus struct {
	ControllerID string    `json:"controller_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type ControlLease interface {
	Renew(string) (ControlLeaseStatus, error)
	Protect(string) (release func(), err error)
	Release(context.Context, string) error
	Shutdown(context.Context) error
}

type CallMediaActivation struct {
	CallID          string           `json:"call_id"`
	MediaRouting    string           `json:"media_routing"`
	MediaAvailable  bool             `json:"media_available"`
	MediaConfigured bool             `json:"media_configured"`
	AudioPort       string           `json:"audio_port"`
	AudioFormat     *CallAudioFormat `json:"audio_format,omitempty"`
	Reason          string           `json:"reason,omitempty"`
}

type CallMediaActivator interface {
	ActivateCallMedia(context.Context, CallCommandRequest) (CallMediaActivation, error)
}

type StartCallRequest struct {
	RequestID string `json:"request_id"`
	LineID    string `json:"line_id"`
	Number    string `json:"number"`
}

type CallCommandRequest struct {
	RequestID string `json:"request_id"`
	CallID    string `json:"-"`
}

type DTMFRequest struct {
	RequestID string `json:"request_id"`
	CallID    string `json:"-"`
	Digits    string `json:"digits"`
}

type SendMessageRequest struct {
	RequestID               string `json:"request_id"`
	LineID                  string `json:"line_id"`
	Number                  string `json:"number"`
	Text                    string `json:"text"`
	DeliveryReportRequested bool   `json:"delivery_report_requested"`
}

type DeleteMessageRequest struct {
	MessageID string `json:"-"`
}

type MessageDeleter interface {
	DeleteMessage(context.Context, DeleteMessageRequest) error
}

// CommandReceipt acknowledges a synchronous ModemManager command. Call and
// message state must always be read from Snapshot instead of inferred here.
type CommandReceipt struct {
	RequestID                 string `json:"request_id"`
	ResourceID                string `json:"resource_id"`
	DeliveryReportUnsupported bool   `json:"delivery_report_unsupported,omitempty"`
}

type Provider interface {
	Health(context.Context) (ProviderHealth, error)
	Snapshot(context.Context) (Snapshot, error)
	StartCall(context.Context, StartCallRequest) (CommandReceipt, error)
	AnswerCall(context.Context, CallCommandRequest) (CommandReceipt, error)
	RejectCall(context.Context, CallCommandRequest) (CommandReceipt, error)
	HangupCall(context.Context, CallCommandRequest) (CommandReceipt, error)
	SendDTMF(context.Context, DTMFRequest) (CommandReceipt, error)
	SendMessage(context.Context, SendMessageRequest) (CommandReceipt, error)
}

type ErrorCode string

const (
	ErrorInvalidArgument    ErrorCode = "invalid_argument"
	ErrorNotFound           ErrorCode = "not_found"
	ErrorConflict           ErrorCode = "conflict"
	ErrorNotSupported       ErrorCode = "not_supported"
	ErrorPermissionDenied   ErrorCode = "permission_denied"
	ErrorFailedPrecondition ErrorCode = "failed_precondition"
	ErrorNetworkRejected    ErrorCode = "network_rejected"
	ErrorUnavailable        ErrorCode = "unavailable"
	ErrorVerification       ErrorCode = "verification_failed"
	ErrorInternal           ErrorCode = "internal"
)

type OperationError struct {
	Code      ErrorCode
	Operation string
	Message   string
	Cause     error
}

func (e *OperationError) Error() string {
	if e.Operation == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Message)
}

func (e *OperationError) Unwrap() error {
	return e.Cause
}

func NewOperationError(code ErrorCode, operation, message string, cause error) error {
	return &OperationError{
		Code:      code,
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

func InvalidArgument(operation, message string) error {
	return NewOperationError(ErrorInvalidArgument, operation, message, nil)
}

func NotFound(operation, message string) error {
	return NewOperationError(ErrorNotFound, operation, message, nil)
}

func Conflict(operation, message string) error {
	return NewOperationError(ErrorConflict, operation, message, nil)
}

func NotSupported(operation, message string) error {
	return NewOperationError(ErrorNotSupported, operation, message, nil)
}

func PermissionDenied(operation, message string, cause error) error {
	return NewOperationError(ErrorPermissionDenied, operation, message, cause)
}

func FailedPrecondition(operation, message string, cause error) error {
	return NewOperationError(ErrorFailedPrecondition, operation, message, cause)
}

func NetworkRejected(operation, message string, cause error) error {
	return NewOperationError(ErrorNetworkRejected, operation, message, cause)
}

func Unavailable(operation, message string, cause error) error {
	return NewOperationError(ErrorUnavailable, operation, message, cause)
}

func VerificationFailed(operation, message string, cause error) error {
	return NewOperationError(ErrorVerification, operation, message, cause)
}

func Internal(operation, message string, cause error) error {
	return NewOperationError(ErrorInternal, operation, message, cause)
}

func AsOperationError(err error) (*OperationError, bool) {
	var operationError *OperationError
	if !errors.As(err, &operationError) {
		return nil, false
	}
	return operationError, true
}
