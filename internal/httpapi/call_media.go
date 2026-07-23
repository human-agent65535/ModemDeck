package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/mediaapp"
)

type callMediaRequest struct {
	OfferSDP string `json:"offer_sdp"`
}

type callMediaResponse struct {
	AnswerSDP string `json:"answer_sdp"`
}

func (api *API) callMediaExchange(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported", "")
		return
	}
	if api.callMedia == nil {
		writeError(response, http.StatusServiceUnavailable, "media_unavailable", "Call media is unavailable", "")
		return
	}
	var input callMediaRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	answer, err := api.callMedia.Exchange(request.Context(), callID, input.OfferSDP)
	if err != nil {
		switch {
		case errors.Is(err, mediaapp.ErrInvalidArgument),
			errors.Is(err, mediaapp.ErrNegotiation):
			writeError(response, http.StatusBadRequest, "invalid_media_offer", err.Error(), "offer_sdp")
		case errors.Is(err, mediaapp.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Call was not found", "")
		case errors.Is(err, mediaapp.ErrNotActive):
			writeError(response, http.StatusConflict, "call_not_active", "Call is not active", "")
		case errors.Is(err, mediaapp.ErrConflict):
			writeError(response, http.StatusConflict, "media_in_use", "Call already has a browser audio session", "")
		case errors.Is(err, mediaapp.ErrUnavailable):
			writeError(response, http.StatusServiceUnavailable, "media_unavailable", "Call media is unavailable", "")
		default:
			api.writeInternalError(response, request, "exchange call media", err)
		}
		return
	}
	writeJSON(response, http.StatusOK, callMediaResponse{AnswerSDP: answer})
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
