package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type accountPreferencesRepository interface {
	UpdateAccountPreferences(
		context.Context,
		store.AccountPreferencesInput,
	) (store.AccountPreferences, error)
}

type updateAccountPreferencesRequest struct {
	ProfileContactID         string               `json:"profile_contact_id"`
	Language                 store.SystemLanguage `json:"language"`
	DefaultLineID            string               `json:"default_line_id"`
	ExpectedLanguageRevision int64                `json:"expected_language_revision"`
	ExpectedLineRevision     int64                `json:"expected_line_revision"`
}

type accountPreferencesResponse struct {
	Preferences store.AccountPreferences `json:"preferences"`
}

func (api *API) accountPreferences(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		response.Header().Set("Allow", http.MethodPut)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT is supported", "")
		return
	}
	repository, ok := api.repository.(accountPreferencesRepository)
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "preferences_unavailable", "Account preferences are unavailable", "")
		return
	}
	var input updateAccountPreferencesRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if input.ExpectedLanguageRevision <= 0 || input.ExpectedLineRevision <= 0 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "Positive language and line revisions are required", "")
		return
	}
	preferences, err := repository.UpdateAccountPreferences(
		request.Context(),
		store.AccountPreferencesInput{
			ProfileContactID:         strings.TrimSpace(input.ProfileContactID),
			Language:                 input.Language,
			DefaultLineID:            strings.TrimSpace(input.DefaultLineID),
			ExpectedLanguageRevision: input.ExpectedLanguageRevision,
			ExpectedLineRevision:     input.ExpectedLineRevision,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrContactNotFound):
			writeError(response, http.StatusNotFound, "contact_not_found", "Contact was not found", "profile_contact_id")
		case errors.Is(err, store.ErrSystemSettingsLanguageInvalid):
			writeError(response, http.StatusBadRequest, "invalid_language", "The selected language is not supported", "language")
		case errors.Is(err, store.ErrLineSettingsInvalidLine):
			writeError(response, http.StatusBadRequest, "invalid_line", "The selected default line is not assigned", "default_line_id")
		case errors.Is(err, store.ErrSystemSettingsRevisionConflict),
			errors.Is(err, store.ErrLineSettingsRevisionConflict):
			writeError(response, http.StatusConflict, "revision_conflict", "Account preferences changed since they were loaded", "")
		default:
			api.writeInternalError(response, request, "update account preferences", err)
		}
		return
	}
	api.publishDurableChange()
	writeJSON(response, http.StatusOK, accountPreferencesResponse{Preferences: preferences})
}
