package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestDiagnosticsReportsRuntimeAndLineCapabilities(t *testing.T) {
	t.Parallel()
	communications := &fakeCommunications{
		status: communication.Status{
			Connected:      true,
			BootEpoch:      "boot-1",
			Revision:       "snapshot-2",
			ObservedAt:     time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
			AgentVersion:   "agent-1",
			ProviderName:   "org.freedesktop.ModemManager1",
			RuntimeVersion: "1.24.0",
			Capabilities: agentclient.Capabilities{
				Discovery:          true,
				SIMManagement:      true,
				ConnectionProfiles: true,
				USSD:               true,
			},
			Lines: []store.LineSummary{{
				ID:                "line-1",
				EndpointID:        "endpoint-1",
				Model:             "QUECTEL Mobile Broadband Module",
				Firmware:          "QDC507GLEFM21",
				State:             "failed",
				FailureReason:     "unknown-capabilities",
				FailureReasonCode: 4,
				Capabilities: store.LineCapabilities{
					Modem: true, SIM: true, Voice: true, Messaging: true, Dial: true,
				},
			}},
		},
		active: []store.Call{{
			ID:             "call-1",
			LineID:         "line-stable",
			EndpointLineID: "endpoint-1",
			Phase:          "active",
			Bearer:         "volte",
			MediaAvailable: true,
			AudioEncoding:  "pcm",
			AudioRate:      8000,
		}},
	}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body diagnosticsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if body.Status != "ok" || !body.HostAgent.Connected || len(body.Lines) != 1 {
		t.Fatalf("diagnostics = %+v", body)
	}
	if !body.Lines[0].Capabilities.Voice || !body.Lines[0].Capabilities.SIM {
		t.Fatalf("line capabilities = %+v", body.Lines[0].Capabilities)
	}
	if body.Lines[0].EndpointID != "endpoint-1" {
		t.Fatalf("diagnostic endpoint identity = %q", body.Lines[0].EndpointID)
	}
	if body.Lines[0].State != "failed" ||
		body.Lines[0].FailureReason != "unknown-capabilities" ||
		body.Lines[0].FailureReasonCode != 4 {
		t.Fatalf("diagnostic failure evidence = %+v", body.Lines[0])
	}
	if !body.HostAgent.Capabilities.SIMManagement ||
		!body.HostAgent.Capabilities.ConnectionProfiles ||
		!body.HostAgent.Capabilities.USSD {
		t.Fatalf("agent capabilities = %+v", body.HostAgent.Capabilities)
	}
	if len(body.ActiveCalls) != 1 || body.ActiveCalls[0].AudioRate != 8000 ||
		body.ActiveCalls[0].LineID != "line-stable" ||
		body.ActiveCalls[0].EndpointLineID != "endpoint-1" {
		t.Fatalf("active calls = %+v", body.ActiveCalls)
	}
}

func TestDiagnosticsKeepsDegradedSnapshotWhenRefreshFails(t *testing.T) {
	t.Parallel()
	communications := &fakeCommunications{
		status: communication.Status{
			LastError: "agent socket unavailable",
			Lines:     []store.LineSummary{{ID: "stale-line"}},
		},
		statusError: errors.New("live hardware state is unavailable"),
	}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil))
	var body diagnosticsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if body.Status != "degraded" || body.HostAgent.LastError == "" || len(body.Lines) != 1 {
		t.Fatalf("diagnostics = %+v", body)
	}
}

func TestDiagnosticLogHistoryFiltersAndRedacts(t *testing.T) {
	t.Parallel()
	buffer := diagnostics.NewLogBuffer(8)
	logger := slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil)))
	logger.Info("started", "component", "app")
	logger.Warn("modem timeout", "component", "communications", "auth_token", "secret")

	api, err := New(&fakeRepository{}, Options{
		DiagnosticLogs:        buffer,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/logs?level=warn&component=communications", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body diagnosticLogResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Message != "modem timeout" {
		t.Fatalf("logs = %+v", body.Entries)
	}
	if body.Entries[0].Fields["auth_token"] != "[redacted]" {
		t.Fatalf("fields = %+v", body.Entries[0].Fields)
	}
}

func TestDiagnosticLogStreamReplaysCursor(t *testing.T) {
	t.Parallel()
	buffer := diagnostics.NewLogBuffer(8)
	logger := slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil)))
	logger.Info("first")
	logger.Info("second")

	api, err := New(&fakeRepository{}, Options{
		DiagnosticLogs:        buffer,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/logs/stream?after=1", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request.WithContext(ctx))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "event: log") ||
		!strings.Contains(response.Body.String(), `"message":"second"`) ||
		strings.Contains(response.Body.String(), `"message":"first"`) {
		t.Fatalf("stream = %q", response.Body.String())
	}
}
