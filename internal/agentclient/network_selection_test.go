package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestNetworkSelectionClientContracts(t *testing.T) {
	t.Parallel()

	var scanRequest NetworkScanRequest
	var selectionRequest ApplyNetworkSelectionRequest
	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost &&
			request.URL.Path == "/v1/lines/line-1/network-scan":
			if err := json.NewDecoder(request.Body).Decode(&scanRequest); err != nil {
				t.Fatalf("decode scan request: %v", err)
			}
			_, _ = response.Write([]byte(`{
				"request_id":"scan-1",
				"line_id":"line-1",
				"observed_at":"2026-07-24T07:08:09Z",
				"networks":[{
					"status":"available",
					"operator_code":"44051",
					"operator_long":" KDDI ",
					"operator_short":"KDDI",
					"access_technologies":16384,
					"access_technology_names":[" lte "]
				}]
			}`))
		case request.Method == http.MethodPut &&
			request.URL.Path == "/v1/lines/line-1/network-selection":
			if err := json.NewDecoder(request.Body).Decode(&selectionRequest); err != nil {
				t.Fatalf("decode selection request: %v", err)
			}
			_, _ = response.Write([]byte(`{
				"request_id":"selection-1",
				"line_id":"line-1",
				"mode":"manual",
				"operator_code":"44051",
				"applied_at":"2026-07-24T07:09:10Z"
			}`))
		default:
			http.NotFound(response, request)
		}
	}))

	scan, err := client.ScanNetworks(context.Background(), "line-1", NetworkScanRequest{
		RequestID: "scan-1",
	})
	if err != nil {
		t.Fatalf("ScanNetworks() error = %v", err)
	}
	if scanRequest.RequestID != "scan-1" ||
		scan.LineID != "line-1" ||
		len(scan.Networks) != 1 ||
		scan.Networks[0].OperatorLong != "KDDI" ||
		len(scan.Networks[0].AccessTechnologyNames) != 1 ||
		scan.Networks[0].AccessTechnologyNames[0] != "lte" {
		t.Fatalf("ScanNetworks() = %+v; request = %+v", scan, scanRequest)
	}

	receipt, err := client.SetNetworkSelection(
		context.Background(),
		"line-1",
		ApplyNetworkSelectionRequest{
			RequestID:    "selection-1",
			Mode:         NetworkSelectionModeManual,
			OperatorCode: "44051",
		},
	)
	if err != nil {
		t.Fatalf("SetNetworkSelection() error = %v", err)
	}
	if selectionRequest.RequestID != "selection-1" ||
		selectionRequest.Mode != NetworkSelectionModeManual ||
		selectionRequest.OperatorCode != "44051" ||
		receipt.LineID != "line-1" ||
		receipt.AppliedAt.IsZero() {
		t.Fatalf("SetNetworkSelection() = %+v; request = %+v", receipt, selectionRequest)
	}
}

func TestNetworkSelectionClientRejectsInvalidInputsAndContradictoryReceipt(t *testing.T) {
	t.Parallel()

	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"request_id":"different",
			"line_id":"line-1",
			"mode":"auto",
			"applied_at":"2026-07-24T07:09:10Z"
		}`))
	}))
	if _, err := client.SetNetworkSelection(
		context.Background(),
		"line-1",
		ApplyNetworkSelectionRequest{
			RequestID:    "selection-1",
			Mode:         NetworkSelectionModeManual,
			OperatorCode: "44x51",
		},
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid SetNetworkSelection() error = %v", err)
	}
	if _, err := client.SetNetworkSelection(
		context.Background(),
		"line-1",
		ApplyNetworkSelectionRequest{
			RequestID: "selection-1",
			Mode:      NetworkSelectionModeAuto,
		},
	); !errors.Is(err, ErrProtocol) {
		t.Fatalf("contradictory SetNetworkSelection() error = %v", err)
	}
}
