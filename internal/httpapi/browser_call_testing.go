package httpapi

import (
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calltest"
)

// Browser tests are isolated by the authenticated login session. Neither the
// request body nor the URL can choose an owner, paired device, or SIM line.
func (api *API) browserCallTest(response http.ResponseWriter, request *http.Request) {
	if isMobileRequest(request) {
		writeError(response, http.StatusForbidden, "browser_api_required", "Use a browser session for this call test", "")
		return
	}
	if api.callTests == nil {
		api.writeCallTestError(response, request, calltest.ErrUnavailable)
		return
	}
	holder, err := api.callLeaseHolder(request.Context())
	if err != nil {
		api.writeCallLeaseError(response, request, "validate test call owner", err)
		return
	}
	owner := "web\x00" + holder
	response.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/call-tests")
	if path == "" {
		api.postOnly(response, request, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Language string `json:"language"`
			}
			if !decodeJSONBody(w, r, &input) {
				return
			}
			status, err := api.callTests.StartBrowser(owner, browserCallTestUser(r), callTestLanguage(input.Language))
			if err != nil {
				api.writeCallTestError(w, r, err)
				return
			}
			writeJSON(w, http.StatusAccepted, status)
		})
		return
	}
	api.callTestResource(response, request, owner, path)
}

// browserCallTestUser groups every login session of one account with its iOS
// tests. Single-administrator installs have no principal.
func browserCallTestUser(request *http.Request) string {
	if principal, ok := auth.PrincipalFromContext(request.Context()); ok && principal.UserID != "" {
		return calltest.UserKey(principal.UserID)
	}
	return calltest.LegacyAdminUser
}

// callTestLanguage maps a locale to the prompt languages bundled with tests.
func callTestLanguage(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "zh") {
		return "zh"
	}
	return "en"
}
