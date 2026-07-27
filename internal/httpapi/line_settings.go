package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type updateLineSettingsRequest struct {
	DefaultLineID    string `json:"default_line_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func (api *API) lineSettings(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		settings, err := api.repository.LineSettings(request.Context())
		if err != nil {
			api.writeInternalError(response, request, "read line settings", err)
			return
		}
		writeJSON(response, http.StatusOK, lineSettingsResponse{Settings: settings})
	case http.MethodPatch:
		var input updateLineSettingsRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		if strings.TrimSpace(input.DefaultLineID) == "" || input.ExpectedRevision <= 0 {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"default_line_id and a positive expected_revision are required",
				"",
			)
			return
		}
		settings, err := api.repository.UpdateLineSettings(
			request.Context(),
			input.DefaultLineID,
			input.ExpectedRevision,
		)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrLineSettingsInvalidLine):
				writeError(
					response,
					http.StatusBadRequest,
					"invalid_line",
					"The selected default line does not exist",
					"default_line_id",
				)
			case errors.Is(err, store.ErrLineSettingsRevisionConflict):
				writeError(
					response,
					http.StatusConflict,
					"revision_conflict",
					"Line settings changed since they were loaded",
					"expected_revision",
				)
			default:
				api.writeInternalError(response, request, "update line settings", err)
			}
			return
		}
		writeJSON(response, http.StatusOK, lineSettingsResponse{Settings: settings})
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
