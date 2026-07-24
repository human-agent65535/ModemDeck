package domain

import (
	"context"
	"time"
)

type NetworkSelectionMode string

const (
	NetworkSelectionModeAuto   NetworkSelectionMode = "auto"
	NetworkSelectionModeManual NetworkSelectionMode = "manual"
)

type NetworkAvailability string

const (
	NetworkAvailabilityUnknown   NetworkAvailability = "unknown"
	NetworkAvailabilityAvailable NetworkAvailability = "available"
	NetworkAvailabilityCurrent   NetworkAvailability = "current"
	NetworkAvailabilityForbidden NetworkAvailability = "forbidden"
)

type MobileNetwork struct {
	Status                NetworkAvailability `json:"status"`
	OperatorCode          string              `json:"operator_code"`
	OperatorLong          string              `json:"operator_long"`
	OperatorShort         string              `json:"operator_short"`
	AccessTechnologies    uint32              `json:"access_technologies"`
	AccessTechnologyNames []string            `json:"access_technology_names"`
}

type NetworkScanRequest struct {
	RequestID string `json:"request_id"`
	LineID    string `json:"-"`
}

type NetworkScanResult struct {
	RequestID  string          `json:"request_id"`
	LineID     string          `json:"line_id"`
	ObservedAt time.Time       `json:"observed_at"`
	Networks   []MobileNetwork `json:"networks"`
}

type ApplyNetworkSelectionRequest struct {
	RequestID    string               `json:"request_id"`
	LineID       string               `json:"-"`
	Mode         NetworkSelectionMode `json:"mode"`
	OperatorCode string               `json:"operator_code,omitempty"`
}

// NetworkSelectionReceipt acknowledges the submitted registration command.
// Registration state remains authoritative in Snapshot.
type NetworkSelectionReceipt struct {
	RequestID    string               `json:"request_id"`
	LineID       string               `json:"line_id"`
	Mode         NetworkSelectionMode `json:"mode"`
	OperatorCode string               `json:"operator_code,omitempty"`
	AppliedAt    time.Time            `json:"applied_at"`
}

type NetworkSelectionProvider interface {
	ScanNetworks(context.Context, NetworkScanRequest) (NetworkScanResult, error)
	SetNetworkSelection(
		context.Context,
		ApplyNetworkSelectionRequest,
	) (NetworkSelectionReceipt, error)
}
