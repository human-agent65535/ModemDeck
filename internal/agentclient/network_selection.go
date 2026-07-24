package agentclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
}

type NetworkScanResult struct {
	RequestID  string          `json:"request_id"`
	LineID     string          `json:"line_id"`
	ObservedAt time.Time       `json:"observed_at"`
	Networks   []MobileNetwork `json:"networks"`
}

type ApplyNetworkSelectionRequest struct {
	RequestID    string               `json:"request_id"`
	Mode         NetworkSelectionMode `json:"mode"`
	OperatorCode string               `json:"operator_code,omitempty"`
}

type NetworkSelectionReceipt struct {
	RequestID    string               `json:"request_id"`
	LineID       string               `json:"line_id"`
	Mode         NetworkSelectionMode `json:"mode"`
	OperatorCode string               `json:"operator_code,omitempty"`
	AppliedAt    time.Time            `json:"applied_at"`
}

func (client *Client) ScanNetworks(
	ctx context.Context,
	lineID string,
	request NetworkScanRequest,
) (NetworkScanResult, error) {
	lineID = strings.TrimSpace(lineID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	if !validNetworkLineID(lineID) || !validNetworkRequestID(request.RequestID) {
		return NetworkScanResult{}, ErrInvalidRequest
	}
	var result NetworkScanResult
	path := "/v1/lines/" + url.PathEscape(lineID) + "/network-scan"
	if err := client.doJSON(
		ctx,
		http.MethodPost,
		path,
		request,
		http.StatusOK,
		&result,
	); err != nil {
		return NetworkScanResult{}, err
	}
	if err := validateNetworkScanResult(&result, lineID, request.RequestID); err != nil {
		return NetworkScanResult{}, err
	}
	return result, nil
}

func (client *Client) SetNetworkSelection(
	ctx context.Context,
	lineID string,
	request ApplyNetworkSelectionRequest,
) (NetworkSelectionReceipt, error) {
	lineID = strings.TrimSpace(lineID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.OperatorCode = strings.TrimSpace(request.OperatorCode)
	if !validNetworkLineID(lineID) ||
		!validNetworkRequestID(request.RequestID) ||
		!validNetworkSelection(request.Mode, request.OperatorCode) {
		return NetworkSelectionReceipt{}, ErrInvalidRequest
	}
	var receipt NetworkSelectionReceipt
	path := "/v1/lines/" + url.PathEscape(lineID) + "/network-selection"
	if err := client.doJSON(
		ctx,
		http.MethodPut,
		path,
		request,
		http.StatusOK,
		&receipt,
	); err != nil {
		return NetworkSelectionReceipt{}, err
	}
	if receipt.RequestID != request.RequestID ||
		strings.TrimSpace(receipt.LineID) != lineID ||
		receipt.Mode != request.Mode ||
		strings.TrimSpace(receipt.OperatorCode) != request.OperatorCode ||
		receipt.AppliedAt.IsZero() {
		return NetworkSelectionReceipt{}, fmt.Errorf(
			"%w: network selection receipt contradicts the request",
			ErrProtocol,
		)
	}
	receipt.LineID = lineID
	receipt.OperatorCode = request.OperatorCode
	receipt.AppliedAt = receipt.AppliedAt.UTC()
	return receipt, nil
}

func validateNetworkScanResult(
	result *NetworkScanResult,
	lineID string,
	requestID string,
) error {
	if result == nil ||
		strings.TrimSpace(result.RequestID) != requestID ||
		strings.TrimSpace(result.LineID) != lineID ||
		result.ObservedAt.IsZero() ||
		result.Networks == nil {
		return fmt.Errorf("%w: network scan result is incomplete", ErrProtocol)
	}
	seen := make(map[string]struct{}, len(result.Networks))
	for index := range result.Networks {
		network := &result.Networks[index]
		network.OperatorCode = strings.TrimSpace(network.OperatorCode)
		network.OperatorLong = strings.TrimSpace(network.OperatorLong)
		network.OperatorShort = strings.TrimSpace(network.OperatorShort)
		if !validOperatorCode(network.OperatorCode) ||
			!validNetworkAvailability(network.Status) ||
			network.AccessTechnologyNames == nil {
			return fmt.Errorf("%w: network scan entry is invalid", ErrProtocol)
		}
		if _, exists := seen[network.OperatorCode]; exists {
			return fmt.Errorf("%w: network scan contains duplicate operators", ErrProtocol)
		}
		seen[network.OperatorCode] = struct{}{}
		for nameIndex := range network.AccessTechnologyNames {
			name := strings.TrimSpace(network.AccessTechnologyNames[nameIndex])
			if name == "" || containsControl(name) {
				return fmt.Errorf(
					"%w: network scan contains an invalid access technology",
					ErrProtocol,
				)
			}
			network.AccessTechnologyNames[nameIndex] = name
		}
	}
	result.RequestID = requestID
	result.LineID = lineID
	result.ObservedAt = result.ObservedAt.UTC()
	return nil
}

func validNetworkSelection(mode NetworkSelectionMode, operatorCode string) bool {
	switch mode {
	case NetworkSelectionModeAuto:
		return operatorCode == ""
	case NetworkSelectionModeManual:
		return validOperatorCode(operatorCode)
	default:
		return false
	}
}

func validOperatorCode(value string) bool {
	if len(value) != 5 && len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validNetworkAvailability(value NetworkAvailability) bool {
	switch value {
	case NetworkAvailabilityUnknown,
		NetworkAvailabilityAvailable,
		NetworkAvailabilityCurrent,
		NetworkAvailabilityForbidden:
		return true
	default:
		return false
	}
}

func validNetworkLineID(value string) bool {
	return value != "" && len(value) <= 128 && !containsControl(value)
}

func validNetworkRequestID(value string) bool {
	return value != "" && len(value) <= 128 && !containsControl(value)
}
