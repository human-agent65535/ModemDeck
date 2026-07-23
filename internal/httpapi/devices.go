package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type renameDeviceRequest struct {
	Alias *string `json:"alias"`
}

type deviceResponse struct {
	Device store.Device `json:"device"`
}

func (api *API) devicesCollection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		api.devices(response, request)
	case http.MethodPost:
		var input store.DeviceInput
		if !decodeJSONBody(response, request, &input) {
			return
		}
		device, err := api.repository.CreateDevice(request.Context(), input)
		if err != nil {
			api.writeDeviceError(response, request, "create device", err)
			return
		}
		writeJSON(response, http.StatusCreated, deviceResponse{Device: device})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and POST are supported",
			"",
		)
	}
}

func (api *API) deviceResource(response http.ResponseWriter, request *http.Request, imei string) {
	if request.Method != http.MethodPatch {
		response.Header().Set("Allow", http.MethodPatch)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
		return
	}
	var input renameDeviceRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if input.Alias == nil {
		writeError(response, http.StatusBadRequest, "invalid_argument", "alias is required", "alias")
		return
	}
	device, err := api.repository.RenameDevice(request.Context(), imei, *input.Alias)
	if err != nil {
		api.writeDeviceError(response, request, "rename device", err)
		return
	}
	writeJSON(response, http.StatusOK, deviceResponse{Device: device})
}

func (api *API) writeDeviceError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, store.ErrDeviceValidation):
		writeError(response, http.StatusBadRequest, "invalid_device", "Device input is invalid", "")
	case errors.Is(err, store.ErrDeviceConflict):
		writeError(response, http.StatusConflict, "device_exists", "Device already exists", "imei")
	case errors.Is(err, store.ErrDeviceNotFound):
		writeError(response, http.StatusNotFound, "device_not_found", "Device was not found", "")
	default:
		api.writeInternalError(response, request, operation, err)
	}
}

func deviceResourceIMEI(path string) (string, bool) {
	const prefix = "/api/v1/devices/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	encoded := strings.TrimPrefix(path, prefix)
	if encoded == "" || strings.Contains(encoded, "/") || len(encoded) > 192 {
		return "", false
	}
	imei, err := url.PathUnescape(encoded)
	if err != nil {
		return "", false
	}
	imei = strings.TrimSpace(imei)
	if imei == "" || len(imei) > 64 {
		return "", false
	}
	return imei, true
}
