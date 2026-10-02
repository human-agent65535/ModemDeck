package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calltest"
)

// Tests are bound to the current paired device, never a browser-selected user
// or a SIM line. Their controls cannot reach the real-call control service.
func (api *API) mobileCallTest(response http.ResponseWriter, request *http.Request) {
	authentication, mobile := mobileAuthenticationFromContext(request.Context())
	principal, authenticated := auth.PrincipalFromContext(request.Context())
	if !mobile || !authenticated {
		writeError(response, http.StatusForbidden, "mobile_api_required", "Use the paired iOS app to start a call test", "")
		return
	}
	if api.callTests == nil {
		api.writeCallTestError(response, request, calltest.ErrUnavailable)
		return
	}
	repository, ok := api.repository.(iosTestCallRepository)
	if !ok {
		api.writeCallTestError(response, request, calltest.ErrUnavailable)
		return
	}
	credentialID, found, err := repository.IOSPairingCredentialIDByTokenDigest(request.Context(), authentication.Digest)
	if err != nil {
		api.writeInternalError(response, request, "read test call device", err)
		return
	}
	if !found {
		api.writeCallTestError(response, request, calltest.ErrNotFound)
		return
	}
	owner := principal.UserID + "\x00" + credentialID
	response.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/mobile/call-tests")
	if path == "" {
		api.postOnly(response, request, func(w http.ResponseWriter, r *http.Request) {
			status, err := api.callTests.Start(principal.UserID, credentialID)
			if err != nil {
				api.writeCallTestError(w, r, err)
				return
			}
			writeJSON(w, http.StatusAccepted, status)
		})
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" {
		api.writeCallTestError(response, request, calltest.ErrNotFound)
		return
	}
	id, action := parts[0], strings.Join(parts[1:], "/")
	method := http.MethodPost
	if action == "status" {
		method = http.MethodGet
	}
	if action == "media" && request.Method == http.MethodDelete {
		method = http.MethodDelete
	}
	if request.Method != method {
		response.Header().Set("Allow", method)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported call test method", "")
		return
	}
	switch action {
	case "status":
		status, err := api.callTests.Status(id, owner)
		if err != nil {
			api.writeCallTestError(response, request, err)
			return
		}
		writeJSON(response, http.StatusOK, status)
	case "answer":
		status, err := api.callTests.Answer(id, owner)
		if err != nil {
			api.writeCallTestError(response, request, err)
			return
		}
		writeJSON(response, http.StatusOK, status)
	case "hangup", "reject":
		if err := api.callTests.End(id, owner); err != nil {
			api.writeCallTestError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	case "media/ice":
		configuration, err := api.callTests.Configuration(request.Context(), id, owner)
		if err != nil {
			api.writeCallTestError(response, request, err)
			return
		}
		writeJSON(response, http.StatusOK, callMediaICEConfigurationResponse{ICEServers: configuration.ICEServers, ICETransportPolicy: "relay", ExpiresAt: configuration.ExpiresAt.UTC().Format(time.RFC3339)})
	case "media":
		if request.Method == http.MethodDelete {
			if err := api.callTests.End(id, owner); err != nil {
				api.writeCallTestError(response, request, err)
				return
			}
			response.WriteHeader(http.StatusNoContent)
			return
		}
		var input callMediaRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		answer, err := api.callTests.Exchange(request.Context(), id, owner, input.OwnerToken, input.OfferSDP)
		if err != nil {
			api.writeCallTestError(response, request, err)
			return
		}
		writeJSON(response, http.StatusOK, callMediaResponse{AnswerSDP: answer})
	default:
		api.writeCallTestError(response, request, calltest.ErrNotFound)
	}
}

func (api *API) writeCallTestError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, calltest.ErrNotFound):
		writeError(response, http.StatusNotFound, "test_call_not_found", "Test call not found", "")
	case errors.Is(err, calltest.ErrBusy):
		writeError(response, http.StatusTooManyRequests, "test_call_busy", "Wait for the current test to finish before starting another", "")
	case errors.Is(err, calltest.ErrEnded):
		writeError(response, http.StatusGone, "test_call_ended", "The test call has ended", "")
	case errors.Is(err, calltest.ErrUnavailable):
		writeError(response, http.StatusServiceUnavailable, "test_call_unavailable", "Call tests require configured Apple push and TURN", "")
	default:
		api.logger.Warn("test call media failed", "error", err)
		writeError(response, http.StatusBadGateway, "test_call_media_failed", "Test call audio could not connect", "")
	}
}
