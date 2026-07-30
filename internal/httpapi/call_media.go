package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/mediaapp"
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
