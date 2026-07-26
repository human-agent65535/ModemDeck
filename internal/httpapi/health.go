package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

const readinessTimeout = 2 * time.Second

const (
	healthStatusOK          = "ok"
	healthStatusUnavailable = "unavailable"
	healthStatusNotChecked  = "not_checked"
)

type healthCheckResponse struct {
	Status string `json:"status"`
}

type healthChecksResponse struct {
	Application  *healthCheckResponse `json:"application,omitempty"`
	Database     *healthCheckResponse `json:"database,omitempty"`
	HostAgent    *healthCheckResponse `json:"host_agent,omitempty"`
	ModemManager *healthCheckResponse `json:"modem_manager,omitempty"`
}

type healthResponse struct {
	Status  string               `json:"status"`
	Kind    string               `json:"kind"`
	Code    string               `json:"code,omitempty"`
	Message string               `json:"message,omitempty"`
	Checks  healthChecksResponse `json:"checks"`
}

func (api *API) liveness(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, healthResponse{
		Status: healthStatusOK,
		Kind:   "liveness",
		Checks: healthChecksResponse{
			Application: healthCheck(healthStatusOK),
		},
	})
}

func (api *API) readiness(response http.ResponseWriter, request *http.Request) {
	checks := healthChecksResponse{
		Application:  healthCheck(healthStatusOK),
		Database:     healthCheck(healthStatusNotChecked),
		HostAgent:    healthCheck(healthStatusNotChecked),
		ModemManager: healthCheck(healthStatusNotChecked),
	}
	ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
	defer cancel()

	if err := api.repository.Ping(ctx); err != nil {
		checks.Database = healthCheck(healthStatusUnavailable)
		api.logger.Error("readiness check failed", "component", "database", "error", err)
		api.writeReadinessFailure(
			response,
			"database_unavailable",
			"Database is unavailable",
			checks,
		)
		return
	}
	checks.Database = healthCheck(healthStatusOK)

	if api.healthProbe == nil {
		checks.HostAgent = healthCheck(healthStatusUnavailable)
		api.logger.Error("readiness check failed", "component", "host_agent", "error", "health probe is not configured")
		api.writeReadinessFailure(
			response,
			"host_agent_unavailable",
			"Host Agent is unavailable",
			checks,
		)
		return
	}
	health, err := api.healthProbe.Health(ctx)
	if err != nil {
		checks.HostAgent = healthCheck(healthStatusUnavailable)
		api.logger.Error("readiness check failed", "component", "host_agent", "error", err)
		api.writeReadinessFailure(
			response,
			"host_agent_unavailable",
			"Host Agent is unavailable",
			checks,
		)
		return
	}
	if health.APIVersion != agentclient.APIVersion {
		checks.HostAgent = healthCheck(healthStatusUnavailable)
		api.logger.Error(
			"readiness check failed",
			"component",
			"host_agent",
			"error",
			"incompatible API version",
			"agent_api_version",
			health.APIVersion,
		)
		api.writeReadinessFailure(
			response,
			"host_agent_incompatible",
			"Host Agent API is incompatible",
			checks,
		)
		return
	}
	checks.HostAgent = healthCheck(healthStatusOK)

	if !health.Provider.Available ||
		strings.TrimSpace(health.Provider.BootEpoch) == "" ||
		health.Status != healthStatusOK {
		checks.ModemManager = healthCheck(healthStatusUnavailable)
		api.logger.Error(
			"readiness check failed",
			"component",
			"modem_manager",
			"provider_available",
			health.Provider.Available,
			"provider_status",
			health.Status,
		)
		api.writeReadinessFailure(
			response,
			"modemmanager_unavailable",
			"ModemManager provider is unavailable",
			checks,
		)
		return
	}
	checks.ModemManager = healthCheck(healthStatusOK)

	writeJSON(response, http.StatusOK, healthResponse{
		Status: healthStatusOK,
		Kind:   "readiness",
		Checks: checks,
	})
}

func (api *API) writeReadinessFailure(
	response http.ResponseWriter,
	code string,
	message string,
	checks healthChecksResponse,
) {
	writeJSON(response, http.StatusServiceUnavailable, healthResponse{
		Status:  healthStatusUnavailable,
		Kind:    "readiness",
		Code:    code,
		Message: message,
		Checks:  checks,
	})
}

func healthCheck(status string) *healthCheckResponse {
	return &healthCheckResponse{Status: status}
}
