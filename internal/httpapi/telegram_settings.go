package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
)

type telegramUnitRequest struct {
	Revision    int64    `json:"revision"`
	DisplayName string   `json:"display_name"`
	Enabled     bool     `json:"enabled"`
	BotToken    *string  `json:"bot_token"`
	ChatID      string   `json:"chat_id"`
	AdminID     string   `json:"admin_id"`
	LineScopes  []string `json:"line_scopes"`
	IncomingSMS bool     `json:"incoming_sms"`
	MissedCalls bool     `json:"missed_calls"`
}

type telegramUnitsResponse struct {
	Units []telegramsettings.Unit `json:"units"`
}

type telegramUnitResponse struct {
	Unit telegramsettings.Unit `json:"unit"`
}

func (api *API) telegramCollection(response http.ResponseWriter, request *http.Request) {
	if api.telegram == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"telegram_settings_unavailable",
			"Telegram settings are unavailable",
			"",
		)
		return
	}
	switch request.Method {
	case http.MethodGet:
		units, err := api.telegram.List(request.Context())
		if err != nil {
			api.writeTelegramSettingsError(response, request, "list Telegram settings", err)
			return
		}
		writeJSON(response, http.StatusOK, telegramUnitsResponse{Units: units})
	case http.MethodPost:
		var input telegramUnitRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		token := ""
		if input.BotToken != nil {
			token = *input.BotToken
		}
		unit, err := api.telegram.Create(request.Context(), telegramsettings.CreateInput{
			DisplayName: input.DisplayName,
			Enabled:     input.Enabled,
			BotToken:    token,
			ChatID:      input.ChatID,
			AdminID:     input.AdminID,
			LineScopes:  input.LineScopes,
			IncomingSMS: input.IncomingSMS,
			MissedCalls: input.MissedCalls,
		})
		if err != nil {
			api.writeTelegramSettingsError(response, request, "create Telegram unit", err)
			return
		}
		writeJSON(response, http.StatusCreated, telegramUnitResponse{Unit: unit})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported", "")
	}
}

func (api *API) telegramResource(response http.ResponseWriter, request *http.Request, id string) {
	if api.telegram == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"telegram_settings_unavailable",
			"Telegram settings are unavailable",
			"",
		)
		return
	}
	switch request.Method {
	case http.MethodPut:
		var input telegramUnitRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		unit, err := api.telegram.Update(request.Context(), id, telegramsettings.UpdateInput{
			Revision:    input.Revision,
			DisplayName: input.DisplayName,
			Enabled:     input.Enabled,
			BotToken:    input.BotToken,
			ChatID:      input.ChatID,
			AdminID:     input.AdminID,
			LineScopes:  input.LineScopes,
			IncomingSMS: input.IncomingSMS,
			MissedCalls: input.MissedCalls,
		})
		if err != nil {
			api.writeTelegramSettingsError(response, request, "update Telegram unit", err)
			return
		}
		writeJSON(response, http.StatusOK, telegramUnitResponse{Unit: unit})
	case http.MethodDelete:
		revision, err := strconv.ParseInt(strings.TrimSpace(request.URL.Query().Get("revision")), 10, 64)
		if err != nil || revision <= 0 {
			writeError(response, http.StatusBadRequest, "invalid_argument", "A positive revision is required", "revision")
			return
		}
		if err := api.telegram.Delete(request.Context(), id, revision); err != nil {
			api.writeTelegramSettingsError(response, request, "delete Telegram unit", err)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	default:
		response.Header().Set("Allow", http.MethodPut+", "+http.MethodDelete)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT and DELETE are supported", "")
	}
}

func telegramResourceID(path string) (string, bool) {
	const prefix = "/api/v1/settings/telegram/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") || len(id) > maxIdentifierLength {
		return "", false
	}
	return id, true
}

func (api *API) writeTelegramSettingsError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	var settingsError *telegramsettings.Error
	if errors.As(err, &settingsError) {
		switch settingsError.Code {
		case telegramsettings.CodeInvalidArgument:
			writeError(response, http.StatusBadRequest, "invalid_argument", settingsError.Message, settingsError.Field)
		case telegramsettings.CodeNotFound:
			writeError(response, http.StatusNotFound, "not_found", settingsError.Message, settingsError.Field)
		case telegramsettings.CodeConflict:
			writeError(response, http.StatusConflict, "conflict", settingsError.Message, settingsError.Field)
		case telegramsettings.CodeUnavailable:
			writeError(
				response,
				http.StatusServiceUnavailable,
				"telegram_settings_unavailable",
				settingsError.Message,
				settingsError.Field,
			)
		default:
			api.writeInternalError(response, request, operation, err)
		}
		return
	}
	api.writeInternalError(response, request, operation, err)
}
