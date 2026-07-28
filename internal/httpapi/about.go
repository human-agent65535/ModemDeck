package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
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
	Commit        string `json:"commit"`
	BuildDate     string `json:"build_date"`
	RepositoryURL string `json:"repository_url"`
	LicenseName   string `json:"license_name"`
	LicenseURL    string `json:"license_url"`
	NoticesURL    string `json:"notices_url"`
}

func (api *API) about(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, aboutResponse{
		Name:          applicationName,
		Version:       api.applicationVersion,
		Commit:        api.buildCommit,
		BuildDate:     api.buildDate,
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
	writeJSON(response, http.StatusOK, api.updateChecker.Check(request.Context()))
}

func normalizedBuildValue(value, fallback string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return fallback
}
