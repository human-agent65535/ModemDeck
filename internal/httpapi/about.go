package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
	"github.com/human-agent65535/modemdeck/internal/updaterclient"
)

const (
	applicationName = "ModemDeck"
	repositoryURL   = "https://github.com/human-agent65535/ModemDeck"
	licenseName     = "PolyForm Noncommercial 1.0.0"
	licenseURL      = repositoryURL + "/blob/modemdeck/LICENSE"
	noticesURL      = repositoryURL + "/blob/modemdeck/THIRD_PARTY_NOTICES.md"
)

type aboutResponse struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	RepositoryURL string `json:"repository_url"`
	LicenseName   string `json:"license_name"`
	LicenseURL    string `json:"license_url"`
	NoticesURL    string `json:"notices_url"`
}

type versionResponse struct {
	Version string `json:"version"`
}

func (api *API) version(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, versionResponse{
		Version: api.applicationVersion,
	})
}

func (api *API) about(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, aboutResponse{
		Name:          applicationName,
		Version:       api.applicationVersion,
		RepositoryURL: repositoryURL,
		LicenseName:   licenseName,
		LicenseURL:    licenseURL,
		NoticesURL:    noticesURL,
	})
}

func (api *API) updateCheck(response http.ResponseWriter, request *http.Request) {
	if api.updateChecker == nil {
		writeJSON(response, http.StatusOK, updatecheck.Result{
			Status:         updatecheck.StatusUnavailable,
			CurrentVersion: api.applicationVersion,
			CheckedAt:      time.Now().UTC().Format(time.RFC3339),
			ErrorCode:      "update_checker_unavailable",
		})
		return
	}
	ctx := request.Context()
	if request.URL.Query().Get("refresh") == "1" {
		ctx = updatecheck.WithRefresh(ctx)
	}
	writeJSON(response, http.StatusOK, api.updateChecker.Check(ctx))
}

func (api *API) updateApply(response http.ResponseWriter, request *http.Request) {
	if api.updateManager == nil {
		writeError(response, http.StatusServiceUnavailable, "updater_unavailable", "Automatic updates are unavailable for this deployment", "")
		return
	}
	var input updatecheck.ApplyRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	operation, err := api.updateManager.Apply(request.Context(), input)
	switch {
	case errors.Is(err, updaterclient.ErrHardwareConfirmationRequired):
		writeError(response, http.StatusConflict, "hardware_confirmation_required", "Hardware update confirmation is required", "confirm_hardware")
	case errors.Is(err, updaterclient.ErrOperationRunning):
		writeError(response, http.StatusConflict, "update_in_progress", "A software update is already running", "")
	case err != nil:
		writeError(response, http.StatusServiceUnavailable, "updater_unavailable", "The software update could not be started", "")
	default:
		writeJSON(response, http.StatusAccepted, operation)
	}
}

func (api *API) updateStatus(response http.ResponseWriter, request *http.Request) {
	if api.updateManager == nil {
		writeError(response, http.StatusServiceUnavailable, "updater_unavailable", "Automatic updates are unavailable for this deployment", "")
		return
	}
	operation, err := api.updateManager.Status(request.Context())
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "updater_unavailable", "The software update status is unavailable", "")
		return
	}
	writeJSON(response, http.StatusOK, operation)
}

func normalizedApplicationVersion(value string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return "dev"
}
