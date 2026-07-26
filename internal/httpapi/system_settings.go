package httpapi

import (
	"errors"
	"net/http"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type updateSystemSettingsRequest struct {
	Language         store.SystemLanguage `json:"language"`
	ExpectedRevision int64                `json:"expected_revision"`
}

func (api *API) systemSettings(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		settings, err := api.repository.SystemSettings(request.Context())
		if err != nil {
			api.writeInternalError(response, request, "read system settings", err)
			return
		}
		writeJSON(response, http.StatusOK, systemSettingsResponse{Settings: settings})
	case http.MethodPatch:
		var input updateSystemSettingsRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		if input.ExpectedRevision <= 0 {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"a positive expected_revision is required",
				"expected_revision",
			)
			return
		}
		settings, err := api.repository.UpdateSystemSettings(
			request.Context(),
			input.Language,
			input.ExpectedRevision,
		)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrSystemSettingsLanguageInvalid):
				writeError(
					response,
					http.StatusBadRequest,
					"invalid_language",
					"The selected language is not supported",
					"language",
				)
			case errors.Is(err, store.ErrSystemSettingsRevisionConflict):
				writeError(
					response,
					http.StatusConflict,
					"revision_conflict",
					"System settings changed since they were loaded",
					"expected_revision",
				)
			default:
				api.writeInternalError(response, request, "update system settings", err)
			}
			return
		}
		writeJSON(response, http.StatusOK, systemSettingsResponse{Settings: settings})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPatch)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and PATCH are supported",
			"",
		)
	}
}
