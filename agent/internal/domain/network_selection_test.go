package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNetworkSelectionJSONContracts(t *testing.T) {
	observedAt := time.Date(2026, 7, 24, 7, 8, 9, 0, time.UTC)
	result := NetworkScanResult{
		RequestID:  "request-scan",
		LineID:     "line-main",
		ObservedAt: observedAt,
		Networks: []MobileNetwork{{
			Status:                NetworkAvailabilityAvailable,
			OperatorCode:          "44051",
			OperatorLong:          "KDDI",
			OperatorShort:         "KDDI",
			AccessTechnologies:    1 << 14,
			AccessTechnologyNames: []string{"lte"},
		}},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(result): %v", err)
	}
	const expected = `{"request_id":"request-scan","line_id":"line-main","observed_at":"2026-07-24T07:08:09Z","networks":[{"status":"available","operator_code":"44051","operator_long":"KDDI","operator_short":"KDDI","access_technologies":16384,"access_technology_names":["lte"]}]}`
	if string(encoded) != expected {
		t.Fatalf("result JSON = %s", encoded)
	}

	receipt := NetworkSelectionReceipt{
		RequestID: "request-auto",
		LineID:    "line-main",
		Mode:      NetworkSelectionModeAuto,
		AppliedAt: observedAt,
	}
	encoded, err = json.Marshal(receipt)
	if err != nil {
		t.Fatalf("Marshal(receipt): %v", err)
	}
	const expectedReceipt = `{"request_id":"request-auto","line_id":"line-main","mode":"auto","applied_at":"2026-07-24T07:08:09Z"}`
	if string(encoded) != expectedReceipt {
		t.Fatalf("receipt JSON = %s", encoded)
	}
}
