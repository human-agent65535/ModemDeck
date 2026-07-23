package httpapi

import (
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func (h *handler) getDeviceConfiguration(w http.ResponseWriter, r *http.Request) {
	if h.deviceConfigurations == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"read_device_configuration",
			"",
			"device configuration is unavailable",
		)
		return
	}
	configuration, err := h.deviceConfigurations.DeviceConfiguration(
		r.Context(),
		strings.TrimSpace(r.PathValue("id")),
	)
	if err != nil {
		h.writeError(w, err, "")
		return
	}
	h.writeJSON(w, http.StatusOK, configuration)
}

func (h *handler) patchDeviceConfiguration(w http.ResponseWriter, r *http.Request) {
	if h.deviceConfigurations == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"apply_device_configuration",
			"",
			"device configuration is unavailable",
		)
		return
	}
	var request domain.ApplyDeviceConfigurationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(
			w,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"apply_device_configuration",
			"",
			"invalid JSON request",
		)
		return
	}
	requestID, ok := normalizeRequestID(request.RequestID)
	if !ok {
		h.writeAPIError(
			w,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"apply_device_configuration",
			"",
			"request_id is required and must be valid",
		)
		return
	}
	request.RequestID = requestID
	request.LineID = strings.TrimSpace(r.PathValue("id"))
	configuration, err := h.deviceConfigurations.ApplyDeviceConfiguration(r.Context(), request)
	if err != nil {
		h.writeError(w, err, requestID)
		return
	}
	h.writeJSON(w, http.StatusOK, configuration)
}
