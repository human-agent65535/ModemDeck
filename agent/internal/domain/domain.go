package domain

import (
	"context"
	"errors"
	"fmt"
)

const APIVersion = "v1"

type AgentCapabilities struct {
	Discovery   bool `json:"discovery"`
	Dial        bool `json:"dial"`
	AnswerCall  bool `json:"answer_call"`
	HangupCall  bool `json:"hangup_call"`
	SendMessage bool `json:"send_message"`
}

type ProviderHealth struct {
	Name         string            `json:"name"`
	Available    bool              `json:"available"`
	Capabilities AgentCapabilities `json:"capabilities"`
}

type LineCapabilities struct {
	ModemInterface     bool `json:"modem_interface"`
	SIMInterface       bool `json:"sim_interface"`
	VoiceInterface     bool `json:"voice_interface"`
	MessagingInterface bool `json:"messaging_interface"`
	Dial               bool `json:"dial"`
	AnswerCall         bool `json:"answer_call"`
	HangupCall         bool `json:"hangup_call"`
	SendMessage        bool `json:"send_message"`
}

type Line struct {
	ID                       string           `json:"id"`
	Manufacturer             string           `json:"manufacturer"`
	Model                    string           `json:"model"`
	Revision                 string           `json:"revision"`
	DeviceIdentifier         string           `json:"device_identifier"`
	EquipmentIdentifier      string           `json:"equipment_identifier"`
	Device                   string           `json:"device"`
	PhysicalDevice           string           `json:"physical_device"`
	Drivers                  []string         `json:"drivers"`
	Plugin                   string           `json:"plugin"`
	PrimaryPort              string           `json:"primary_port"`
	State                    string           `json:"state"`
	StateCode                int32            `json:"state_code"`
	PowerStateCode           uint32           `json:"power_state_code"`
	AccessTechnologies       uint32           `json:"access_technologies"`
	SignalQualityKnown       bool             `json:"signal_quality_known"`
	SignalQuality            uint32           `json:"signal_quality"`
	SignalQualityRecent      bool             `json:"signal_quality_recent"`
	OwnNumbers               []string         `json:"own_numbers"`
	SIMPresent               bool             `json:"sim_present"`
	SIMPath                  string           `json:"sim_path"`
	SIMIdentifier            string           `json:"sim_identifier"`
	IMSI                     string           `json:"imsi"`
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

type StartCallRequest struct {
	LineID string `json:"line_id"`
	Number string `json:"number"`
}

type Call struct {
	ID     string `json:"id"`
	LineID string `json:"line_id"`
	Number string `json:"number"`
	State  string `json:"state"`
}

type SendMessageRequest struct {
	LineID string `json:"line_id"`
	Number string `json:"number"`
	Text   string `json:"text"`
}

type Message struct {
	ID     string `json:"id"`
	LineID string `json:"line_id"`
	Number string `json:"number"`
	State  string `json:"state"`
}

type Provider interface {
	Health(context.Context) (ProviderHealth, error)
	Lines(context.Context) ([]Line, error)
	StartCall(context.Context, StartCallRequest) (Call, error)
	AnswerCall(context.Context, string) (Call, error)
	HangupCall(context.Context, string) (Call, error)
	SendMessage(context.Context, SendMessageRequest) (Message, error)
}

type ErrorCode string

const (
	ErrorInvalidArgument ErrorCode = "invalid_argument"
	ErrorNotFound        ErrorCode = "not_found"
	ErrorConflict        ErrorCode = "conflict"
	ErrorNotSupported    ErrorCode = "not_supported"
	ErrorUnavailable     ErrorCode = "unavailable"
	ErrorInternal        ErrorCode = "internal"
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

func NotSupported(operation string) error {
	return NewOperationError(
		ErrorNotSupported,
		operation,
		"operation is not implemented by the ModemManager provider",
		nil,
	)
}

func AsOperationError(err error) (*OperationError, bool) {
	var operationError *OperationError
	if !errors.As(err, &operationError) {
		return nil, false
	}
	return operationError, true
}
