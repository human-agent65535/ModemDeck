package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

type callMediaRequest struct {
	OwnerToken string `json:"owner_token"`
	OfferSDP   string `json:"offer_sdp"`
	HolderID   string `json:"holder_id"`
}

type callMediaReleaseRequest struct {
	OwnerToken string `json:"owner_token"`
	HolderID   string `json:"holder_id"`
}

type callMediaResponse struct {
	AnswerSDP string `json:"answer_sdp"`
}

type callMediaICEConfigurationRequest struct {
	HolderID string `json:"holder_id"`
}

type callMediaICEConfigurationResponse struct {
	ICEServers         []rtcconfig.ICEServer `json:"ice_servers"`
	ICETransportPolicy string                `json:"ice_transport_policy"`
	ExpiresAt          string                `json:"expires_at,omitempty"`
}

func (api *API) callMediaExchange(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) {
	if !api.requireCallAccess(response, request, callID) {
		return
	}
	switch request.Method {
	case http.MethodPost:
		api.exchangeCallMedia(response, request, callID)
	case http.MethodDelete:
		api.releaseCallMedia(response, request, callID)
	default:
		response.Header().Set("Allow", http.MethodPost+", "+http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only POST and DELETE are supported",
			"",
		)
	}
}

func (api *API) exchangeCallMedia(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) {
	if api.callMedia == nil {
		writeError(response, http.StatusServiceUnavailable, "media_unavailable", "Call media is unavailable", "")
		return
	}
	if api.callLeases == nil {
		writeError(response, http.StatusServiceUnavailable, "call_lease_unavailable", "Browser call ownership is unavailable", "")
		return
	}
	var input callMediaRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	holder, err := api.callLeaseHolder(request.Context(), input.HolderID)
	if err != nil {
		api.writeCallLeaseError(response, request, "validate browser call owner", err)
		return
	}
	if err := api.callLeases.Require(
		request.Context(),
		callID,
		holder.LeaseID,
	); err != nil {
		api.writeCallLeaseError(response, request, "authorize browser call media", err)
		return
	}
	answer, err := api.callMedia.Exchange(
		request.Context(),
		callID,
		input.OwnerToken,
		input.OfferSDP,
		api.callMediaRequiresRelay(request),
	)
	if err != nil {
		api.logger.Warn(
			"browser call media exchange failed",
			"component", "media",
			"call_id", callID,
			"error", err,
		)
		api.writeCallMediaError(response, request, err)
		return
	}
	api.logger.Info(
		"browser call media exchange established",
		"component", "media",
		"call_id", callID,
	)
	writeJSON(response, http.StatusOK, callMediaResponse{AnswerSDP: answer})
}

func (api *API) callMediaICEConfiguration(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only POST is supported",
			"",
		)
		return
	}
	if !api.requireCallAccess(response, request, callID) {
		return
	}
	if api.callLeases == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"call_lease_unavailable",
			"Call ownership is unavailable",
			"",
		)
		return
	}
	relayRequired := api.callMediaRequiresRelay(request)
	if !relayRequired {
		response.Header().Set("Cache-Control", "no-store")
		writeJSON(response, http.StatusOK, callMediaICEConfigurationResponse{
			ICEServers:         []rtcconfig.ICEServer{},
			ICETransportPolicy: "all",
		})
		return
	}
	if api.rtcConfiguration == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"turn_unavailable",
			"TURN is unavailable",
			"",
		)
		return
	}
	var input callMediaICEConfigurationRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	holder, err := api.callLeaseHolder(request.Context(), input.HolderID)
	if err != nil {
		api.writeCallLeaseError(
			response,
			request,
			"validate call owner",
			err,
		)
		return
	}
	if err := api.callLeases.Require(
		request.Context(),
		callID,
		holder.LeaseID,
	); err != nil {
		api.writeCallLeaseError(
			response,
			request,
			"authorize TURN configuration",
			err,
		)
		return
	}
	configuration, err := api.generateRTCConfiguration(request.Context())
	if err != nil {
		api.logger.Warn(
			"generate TURN configuration",
			"component", "media",
			"call_id", callID,
			"error", err,
		)
		writeError(
			response,
			http.StatusServiceUnavailable,
			"turn_unavailable",
			"TURN is unavailable",
			"",
		)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, callMediaICEConfigurationResponse{
		ICEServers:         configuration.ICEServers,
		ICETransportPolicy: "relay",
		ExpiresAt:          configuration.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func isMobileRequest(request *http.Request) bool {
	_, ok := mobileAuthenticationFromContext(request.Context())
	return ok
}

func (api *API) callMediaRequiresRelay(request *http.Request) bool {
	if isMobileRequest(request) {
		return true
	}
	matcher, ok := api.mobilePairingAvailability.(mobilepairing.WebIngressMatcher)
	requestHost := strings.TrimSpace(
		request.Header.Get("X-Forwarded-Host"),
	)
	if requestHost == "" {
		requestHost = request.Host
	}
	if ok && matcher.IsWebIngress(request.Context(), requestHost) {
		return true
	}
	return strings.TrimSpace(request.Header.Get("CF-Ray")) != "" ||
		strings.TrimSpace(request.Header.Get("CF-Connecting-IP")) != ""
}

func (api *API) releaseCallMedia(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) {
	if api.callMedia == nil {
		writeError(response, http.StatusServiceUnavailable, "media_unavailable", "Call media is unavailable", "")
		return
	}
	if api.callLeases == nil {
		writeError(response, http.StatusServiceUnavailable, "call_lease_unavailable", "Browser call ownership is unavailable", "")
		return
	}
	var input callMediaReleaseRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	holder, err := api.callLeaseHolder(request.Context(), input.HolderID)
	if err != nil {
		api.writeCallLeaseError(response, request, "validate browser call owner", err)
		return
	}
	if err := api.callLeases.Require(
		request.Context(),
		callID,
		holder.LeaseID,
	); err != nil {
		api.writeCallLeaseError(response, request, "authorize browser call media release", err)
		return
	}
	err = api.callMedia.ReleaseOwner(request.Context(), callID, input.OwnerToken)
	if err != nil {
		api.writeCallMediaError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) writeCallMediaError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, mediaapp.ErrInvalidArgument):
		writeError(response, http.StatusBadRequest, "invalid_media_request", err.Error(), "")
	case errors.Is(err, mediaapp.ErrNegotiation):
		writeError(response, http.StatusBadRequest, "invalid_media_offer", err.Error(), "offer_sdp")
	case errors.Is(err, mediaapp.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "Call was not found", "")
	case errors.Is(err, mediaapp.ErrNotActive):
		writeError(response, http.StatusConflict, "call_not_active", "Call is not active", "")
	case errors.Is(err, mediaapp.ErrConflict):
		writeError(response, http.StatusConflict, "media_in_use", "Call already has a browser audio owner", "")
	case errors.Is(err, mediaapp.ErrUnavailable):
		writeError(response, http.StatusServiceUnavailable, "media_unavailable", "Call media is unavailable", "")
	default:
		api.writeInternalError(response, request, "control call media owner", err)
	}
}

func callMediaResourceID(path string) (string, bool) {
	const prefix = "/api/v1/calls/"
	const suffix = "/media"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	id = strings.TrimSuffix(id, "/")
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || len(id) > maxIdentifierLength {
		return "", false
	}
	return id, true
}

func callMediaICEConfigurationResourceID(path string) (string, bool) {
	const prefix = "/api/v1/calls/"
	const suffix = "/media/ice"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	id = strings.TrimSuffix(id, "/")
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || len(id) > maxIdentifierLength {
		return "", false
	}
	return id, true
}
