package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

type fakeHealthProbe struct {
	health agentclient.Health
	err    error
	calls  int
}

func (probe *fakeHealthProbe) Health(context.Context) (agentclient.Health, error) {
	probe.calls++
	return probe.health, probe.err
}

type fakeHealthCommunications struct {
	*fakeCommunications
	probe *fakeHealthProbe
}

func (service *fakeHealthCommunications) Health(
	ctx context.Context,
) (agentclient.Health, error) {
	return service.probe.Health(ctx)
}

func TestLivenessDoesNotDependOnDatabaseOrHardware(t *testing.T) {
	t.Parallel()

	probe := &fakeHealthProbe{err: errors.New("agent socket unavailable")}
	api, err := New(
		&fakeRepository{pingError: errors.New("database unavailable")},
		Options{HealthProbe: probe, disableAuthentication: true},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil),
	)

	body := decodeHealthResponse(t, response)
	if response.Code != http.StatusOK ||
		body.Status != healthStatusOK ||
		body.Kind != "liveness" ||
		body.Checks.Application == nil ||
		body.Checks.Application.Status != healthStatusOK {
		t.Fatalf("liveness response = %+v; status = %d", body, response.Code)
	}
	if probe.calls != 0 {
		t.Fatalf("Host Agent health calls = %d, want 0", probe.calls)
	}
}

func TestReadinessUsesCommunicationServiceHealthProbe(t *testing.T) {
	t.Parallel()

	probe := &fakeHealthProbe{health: readyAgentHealth()}
	communications := &fakeHealthCommunications{
		fakeCommunications: &fakeCommunications{},
		probe:              probe,
	}
	api, err := New(
		&fakeRepository{},
		Options{Communications: communications, disableAuthentication: true},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil),
	)

	body := decodeHealthResponse(t, response)
	if response.Code != http.StatusOK || body.Status != healthStatusOK {
		t.Fatalf("readiness response = %+v; status = %d", body, response.Code)
	}
	if probe.calls != 1 {
		t.Fatalf("communication health calls = %d, want 1", probe.calls)
	}
}

func TestReadinessAllowsNoPhysicalModems(t *testing.T) {
	t.Parallel()

	probe := &fakeHealthProbe{health: readyAgentHealth()}
	api, err := New(
		&fakeRepository{},
		Options{HealthProbe: probe, disableAuthentication: true},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, path := range []string{"/api/v1/health", "/api/v1/health/ready"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

			body := decodeHealthResponse(t, response)
			if response.Code != http.StatusOK ||
				body.Status != healthStatusOK ||
				body.Kind != "readiness" ||
				body.Checks.Database.Status != healthStatusOK ||
				body.Checks.HostAgent.Status != healthStatusOK ||
				body.Checks.ModemManager.Status != healthStatusOK {
				t.Fatalf("readiness response = %+v; status = %d", body, response.Code)
			}
		})
	}
}

func TestReadinessReportsDependencyFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		repository       *fakeRepository
		probe            HealthProbe
		wantCode         string
		wantDatabase     string
		wantHostAgent    string
		wantModemManager string
	}{
		{
			name:             "database unavailable",
			repository:       &fakeRepository{pingError: errors.New("database down")},
			probe:            &fakeHealthProbe{health: readyAgentHealth()},
			wantCode:         "database_unavailable",
			wantDatabase:     healthStatusUnavailable,
			wantHostAgent:    healthStatusNotChecked,
			wantModemManager: healthStatusNotChecked,
		},
		{
			name:             "health probe not configured",
			repository:       &fakeRepository{},
			wantCode:         "host_agent_unavailable",
			wantDatabase:     healthStatusOK,
			wantHostAgent:    healthStatusUnavailable,
			wantModemManager: healthStatusNotChecked,
		},
		{
			name:             "agent socket unavailable",
			repository:       &fakeRepository{},
			probe:            &fakeHealthProbe{err: errors.New("dial unix: connection refused")},
			wantCode:         "host_agent_unavailable",
			wantDatabase:     healthStatusOK,
			wantHostAgent:    healthStatusUnavailable,
			wantModemManager: healthStatusNotChecked,
		},
		{
			name:       "agent API incompatible",
			repository: &fakeRepository{},
			probe: &fakeHealthProbe{health: agentclient.Health{
				Status:     healthStatusOK,
				APIVersion: "v2",
				Provider: agentclient.ProviderHealth{
					Available: true,
					BootEpoch: "provider-1",
				},
			}},
			wantCode:         "host_agent_incompatible",
			wantDatabase:     healthStatusOK,
			wantHostAgent:    healthStatusUnavailable,
			wantModemManager: healthStatusNotChecked,
		},
		{
			name:       "ModemManager unavailable",
			repository: &fakeRepository{},
			probe: &fakeHealthProbe{health: agentclient.Health{
				Status:     "degraded",
				APIVersion: agentclient.APIVersion,
				Provider: agentclient.ProviderHealth{
					Available: false,
				},
			}},
			wantCode:         "modemmanager_unavailable",
			wantDatabase:     healthStatusOK,
			wantHostAgent:    healthStatusOK,
			wantModemManager: healthStatusUnavailable,
		},
		{
			name:       "provider identity missing",
			repository: &fakeRepository{},
			probe: &fakeHealthProbe{health: agentclient.Health{
				Status:     healthStatusOK,
				APIVersion: agentclient.APIVersion,
				Provider: agentclient.ProviderHealth{
					Available: true,
				},
			}},
			wantCode:         "modemmanager_unavailable",
			wantDatabase:     healthStatusOK,
			wantHostAgent:    healthStatusOK,
			wantModemManager: healthStatusUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			api, err := New(
				test.repository,
				Options{HealthProbe: test.probe, disableAuthentication: true},
			)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := httptest.NewRecorder()
			api.ServeHTTP(
				response,
				httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil),
			)

			body := decodeHealthResponse(t, response)
			if response.Code != http.StatusServiceUnavailable ||
				body.Status != healthStatusUnavailable ||
				body.Kind != "readiness" ||
				body.Code != test.wantCode {
				t.Fatalf("readiness response = %+v; status = %d", body, response.Code)
			}
			if body.Checks.Database.Status != test.wantDatabase ||
				body.Checks.HostAgent.Status != test.wantHostAgent ||
				body.Checks.ModemManager.Status != test.wantModemManager {
				t.Fatalf("readiness checks = %+v", body.Checks)
			}
		})
	}
}

func TestHealthEndpointsOnlyAllowGET(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, path := range []string{
		"/api/v1/health",
		"/api/v1/health/live",
		"/api/v1/health/ready",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			api.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func readyAgentHealth() agentclient.Health {
	return agentclient.Health{
		Status:     healthStatusOK,
		APIVersion: agentclient.APIVersion,
		Provider: agentclient.ProviderHealth{
			Name:      "org.freedesktop.ModemManager1",
			Available: true,
			BootEpoch: "provider-1",
		},
	}
}

func decodeHealthResponse(t *testing.T, response *httptest.ResponseRecorder) healthResponse {
	t.Helper()
	var body healthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v; body = %s", err, response.Body.String())
	}
	return body
}
