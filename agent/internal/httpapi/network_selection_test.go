package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeNetworkSelectionProvider struct {
	scanResult       domain.NetworkScanResult
	selectionReceipt domain.NetworkSelectionReceipt
	scanRequests     []domain.NetworkScanRequest
	selectionInputs  []domain.ApplyNetworkSelectionRequest
	err              error
}

func (provider *fakeNetworkSelectionProvider) ScanNetworks(
	_ context.Context,
	request domain.NetworkScanRequest,
) (domain.NetworkScanResult, error) {
	provider.scanRequests = append(provider.scanRequests, request)
	return provider.scanResult, provider.err
}

func (provider *fakeNetworkSelectionProvider) SetNetworkSelection(
	_ context.Context,
	request domain.ApplyNetworkSelectionRequest,
) (domain.NetworkSelectionReceipt, error) {
	provider.selectionInputs = append(provider.selectionInputs, request)
	return provider.selectionReceipt, provider.err
}

type writeDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (recorder *writeDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	recorder.deadline = deadline
	return nil
}

func TestNetworkSelectionRoutesUseTypedWireContracts(t *testing.T) {
	observedAt := time.Date(2026, 7, 24, 8, 9, 10, 0, time.UTC)
	provider := &fakeNetworkSelectionProvider{
		scanResult: domain.NetworkScanResult{
			RequestID:  "request-scan",
			LineID:     "line-main",
			ObservedAt: observedAt,
			Networks: []domain.MobileNetwork{{
				Status:                domain.NetworkAvailabilityAvailable,
				OperatorCode:          "44051",
				OperatorLong:          "KDDI",
				OperatorShort:         "KDDI",
				AccessTechnologies:    1 << 14,
				AccessTechnologyNames: []string{"lte"},
			}},
		},
		selectionReceipt: domain.NetworkSelectionReceipt{
			RequestID:    "request-manual",
			LineID:       "line-main",
			Mode:         domain.NetworkSelectionModeManual,
			OperatorCode: "44051",
			AppliedAt:    observedAt,
		},
	}
	handler := NewWithOptions(
		&fakeProvider{},
		"test",
		Options{NetworkSelection: provider},
	)

	started := time.Now()
	scanRecorder := &writeDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	scanRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/lines/line-main/network-scan",
		bytes.NewBufferString(`{"request_id":"request-scan"}`),
	)
	scanRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(scanRecorder, scanRequest)
	if scanRecorder.Code != http.StatusOK {
		t.Fatalf("scan status = %d, body = %s", scanRecorder.Code, scanRecorder.Body.String())
	}
	if scanRecorder.deadline.Before(started.Add(124*time.Second)) ||
		scanRecorder.deadline.After(time.Now().Add(126*time.Second)) {
		t.Fatalf("scan write deadline = %v", scanRecorder.deadline)
	}
	var scanResult domain.NetworkScanResult
	decodeResponse(t, scanRecorder.ResponseRecorder, &scanResult)
	if scanResult.RequestID != "request-scan" ||
		scanResult.LineID != "line-main" ||
		len(scanResult.Networks) != 1 ||
		scanResult.Networks[0].OperatorCode != "44051" {
		t.Fatalf("scan result = %+v", scanResult)
	}
	if len(provider.scanRequests) != 1 ||
		provider.scanRequests[0].RequestID != "request-scan" ||
		provider.scanRequests[0].LineID != "line-main" {
		t.Fatalf("scan requests = %+v", provider.scanRequests)
	}

	selection := performRequest(
		handler,
		http.MethodPut,
		"/v1/lines/line-main/network-selection",
		[]byte(`{
			"request_id":"request-manual",
			"mode":"manual",
			"operator_code":"44051"
		}`),
	)
	if selection.Code != http.StatusOK {
		t.Fatalf("selection status = %d, body = %s", selection.Code, selection.Body.String())
	}
	var receipt domain.NetworkSelectionReceipt
	decodeResponse(t, selection, &receipt)
	if receipt.RequestID != "request-manual" ||
		receipt.Mode != domain.NetworkSelectionModeManual ||
		receipt.OperatorCode != "44051" {
		t.Fatalf("selection receipt = %+v", receipt)
	}
	if len(provider.selectionInputs) != 1 ||
		provider.selectionInputs[0].LineID != "line-main" ||
		provider.selectionInputs[0].OperatorCode != "44051" {
		t.Fatalf("selection inputs = %+v", provider.selectionInputs)
	}
}

func TestNetworkSelectionRoutesValidateInputAndCapability(t *testing.T) {
	provider := &fakeNetworkSelectionProvider{}
	withProvider := NewWithOptions(
		&fakeProvider{health: domain.ProviderHealth{Available: true}},
		"test",
		Options{NetworkSelection: provider},
	)
	for name, recorder := range map[string]*httptest.ResponseRecorder{
		"missing request id": performRequest(
			withProvider,
			http.MethodPost,
			"/v1/lines/line-main/network-scan",
			[]byte(`{}`),
		),
		"unknown field": performRequest(
			withProvider,
			http.MethodPut,
			"/v1/lines/line-main/network-selection",
			[]byte(`{"request_id":"request","mode":"auto","retry":true}`),
		),
	} {
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body = %s", name, recorder.Code, recorder.Body.String())
		}
	}
	if len(provider.scanRequests) != 0 || len(provider.selectionInputs) != 0 {
		t.Fatalf("invalid requests reached provider: scans=%+v selections=%+v", provider.scanRequests, provider.selectionInputs)
	}

	health := performRequest(withProvider, http.MethodGet, "/v1/health", nil)
	var healthBody healthResponse
	decodeResponse(t, health, &healthBody)
	if !healthBody.Provider.Capabilities.NetworkSelection {
		t.Fatalf("health capabilities = %+v", healthBody.Provider.Capabilities)
	}

	withoutProvider := New(&fakeProvider{}, "test")
	unavailable := performRequest(
		withoutProvider,
		http.MethodPost,
		"/v1/lines/line-main/network-scan",
		[]byte(`{"request_id":"request-scan"}`),
	)
	if unavailable.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, body = %s", unavailable.Code, unavailable.Body.String())
	}
}

func TestNetworkSelectionRoutePreservesTypedProviderError(t *testing.T) {
	provider := &fakeNetworkSelectionProvider{
		err: domain.InvalidArgument(
			"set_network_selection",
			"operator_code must contain 5 or 6 digits in manual mode",
		),
	}
	handler := NewWithOptions(
		&fakeProvider{},
		"test",
		Options{NetworkSelection: provider},
	)
	recorder := performRequest(
		handler,
		http.MethodPut,
		"/v1/lines/line-main/network-selection",
		[]byte(`{
			"request_id":"request-manual",
			"mode":"manual",
			"operator_code":"44A51"
		}`),
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body errorBody
	decodeResponse(t, recorder, &body)
	if body.Error.Code != domain.ErrorInvalidArgument ||
		body.Error.Operation != "set_network_selection" ||
		body.Error.RequestID != "request-manual" {
		t.Fatalf("error body = %+v", body)
	}
}

var _ domain.NetworkSelectionProvider = (*fakeNetworkSelectionProvider)(nil)
