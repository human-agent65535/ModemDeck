package httpapi

import (
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func (h *handler) activateCallMedia(w http.ResponseWriter, r *http.Request) {
	if h.callMedia == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"activate_call_media",
			"",
			"provider does not expose call media activation",
		)
		return
	}
	var request domain.CallCommandRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(
			w,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"activate_call_media",
			"",
			"invalid JSON request",
		)
		return
	}
	if requestID, ok := normalizeRequestID(request.RequestID); ok {
		request.RequestID = requestID
	} else {
		h.writeAPIError(
			w,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"activate_call_media",
			"",
			"request_id is required and must be valid",
		)
		return
	}
	request.CallID = strings.TrimSpace(r.PathValue("id"))
	if request.CallID == "" {
		h.writeAPIError(
			w,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"activate_call_media",
			request.RequestID,
			"call id is required",
		)
		return
	}
	activation, err := h.callMedia.ActivateCallMedia(r.Context(), request)
	if err != nil {
		h.writeError(w, err, request.RequestID)
		return
	}
	activation.MediaConfigured = h.media != nil && h.media.IsConfigured(activation.AudioPort)
	h.writeJSON(w, http.StatusOK, activation)
}
