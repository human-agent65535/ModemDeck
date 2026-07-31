package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type userRepository interface {
	Users(context.Context) ([]store.User, error)
	CreateMember(context.Context, store.CreateMemberInput) (store.User, error)
	UpdateMember(context.Context, string, store.UpdateMemberInput) (store.User, error)
	SetMemberPassword(context.Context, string, string) error
	SetProfileContact(context.Context, string) error
}

type createMemberRequest struct {
	Username          string   `json:"username"`
	Password          string   `json:"password"`
	IOSPairingEnabled bool     `json:"ios_pairing_enabled"`
	LineIDs           []string `json:"line_ids"`
}

type updateMemberRequest struct {
	Username          string   `json:"username"`
	Password          string   `json:"password,omitempty"`
	Enabled           bool     `json:"enabled"`
	IOSPairingEnabled bool     `json:"ios_pairing_enabled"`
	LineIDs           []string `json:"line_ids"`
	Revision          int64    `json:"revision"`
}

type setMemberPasswordRequest struct {
	Password string `json:"password"`
}

type profileContactRequest struct {
	ContactID string `json:"contact_id"`
}

type usersResponse struct {
	Users []store.User `json:"users"`
}

type userResponse struct {
	User store.User `json:"user"`
}

func (api *API) usersCollection(response http.ResponseWriter, request *http.Request) {
	if !api.requireAdmin(response, request) {
		return
	}
	repository, ok := api.repository.(userRepository)
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "users_unavailable", "User management is unavailable", "")
		return
	}
	switch request.Method {
	case http.MethodGet:
		users, err := repository.Users(request.Context())
		if err != nil {
			api.writeInternalError(response, request, "list users", err)
			return
		}
		writeJSON(response, http.StatusOK, usersResponse{Users: users})
	case http.MethodPost:
		var input createMemberRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		input.Username = strings.TrimSpace(input.Username)
		if !validMemberUsername(input.Username) {
			writeError(response, http.StatusUnprocessableEntity, "username_invalid", "Enter a valid username", "username")
			return
		}
		if err := auth.ValidateNewPassword(input.Password); err != nil {
			writePasswordValidationError(response, err, "password")
			return
		}
		passwordHash, err := auth.HashPassword(input.Password)
		if err != nil {
			api.writeInternalError(response, request, "hash member password", err)
			return
		}
		user, err := repository.CreateMember(request.Context(), store.CreateMemberInput{
			Username:          input.Username,
			PasswordHash:      passwordHash,
			IOSPairingEnabled: input.IOSPairingEnabled,
			LineIDs:           input.LineIDs,
		})
		if err != nil {
			api.writeUserError(response, request, "create member", err)
			return
		}
		api.notifyTelegramAccessChanged()
		writeJSON(response, http.StatusCreated, userResponse{User: user})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported", "")
	}
}

func (api *API) userResource(
	response http.ResponseWriter,
	request *http.Request,
	userID, action string,
) {
	if !api.requireAdmin(response, request) {
		return
	}
	repository, ok := api.repository.(userRepository)
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "users_unavailable", "User management is unavailable", "")
		return
	}
	if action == "password" {
		if request.Method != http.MethodPut {
			response.Header().Set("Allow", http.MethodPut)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT is supported", "")
			return
		}
		var input setMemberPasswordRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		if err := auth.ValidateNewPassword(input.Password); err != nil {
			writePasswordValidationError(response, err, "password")
			return
		}
		passwordHash, err := auth.HashPassword(input.Password)
		if err != nil {
			api.writeInternalError(response, request, "hash member password", err)
			return
		}
		if err := repository.SetMemberPassword(request.Context(), userID, passwordHash); err != nil {
			api.writeUserError(response, request, "set member password", err)
			return
		}
		api.publishRuntimeResources(runtimeevents.ResourceSession)
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if action != "" || request.Method != http.MethodPut {
		response.Header().Set("Allow", http.MethodPut)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT is supported", "")
		return
	}
	var input updateMemberRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !validMemberUsername(input.Username) {
		writeError(response, http.StatusUnprocessableEntity, "username_invalid", "Enter a valid username", "username")
		return
	}
	passwordHash := ""
	if input.Password != "" {
		if err := auth.ValidateNewPassword(input.Password); err != nil {
			writePasswordValidationError(response, err, "password")
			return
		}
		var err error
		passwordHash, err = auth.HashPassword(input.Password)
		if err != nil {
			api.writeInternalError(response, request, "hash member password", err)
			return
		}
	}
	user, err := repository.UpdateMember(request.Context(), userID, store.UpdateMemberInput{
		Username:          input.Username,
		PasswordHash:      passwordHash,
		Enabled:           input.Enabled,
		IOSPairingEnabled: input.IOSPairingEnabled,
		LineIDs:           input.LineIDs,
		Revision:          input.Revision,
	})
	if err != nil {
		api.writeUserError(response, request, "update member", err)
		return
	}
	api.notifyTelegramAccessChanged()
	api.publishRuntimeResources(
		runtimeevents.ResourceSession,
		runtimeevents.ResourceLines,
		runtimeevents.ResourceNetwork,
		runtimeevents.ResourceCalls,
		runtimeevents.ResourceMessages,
		runtimeevents.ResourceContacts,
		runtimeevents.ResourceRecordings,
	)
	writeJSON(response, http.StatusOK, userResponse{User: user})
}

func (api *API) accountContact(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		response.Header().Set("Allow", http.MethodPut)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT is supported", "")
		return
	}
	repository, ok := api.repository.(userRepository)
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "users_unavailable", "Account profiles are unavailable", "")
		return
	}
	var input profileContactRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if err := repository.SetProfileContact(request.Context(), strings.TrimSpace(input.ContactID)); err != nil {
		switch {
		case errors.Is(err, store.ErrContactNotFound):
			writeError(response, http.StatusNotFound, "contact_not_found", "Contact was not found", "contact_id")
		default:
			api.writeInternalError(response, request, "set account contact", err)
		}
		return
	}
	api.publishRuntimeResources(runtimeevents.ResourceSession)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) requireAdmin(response http.ResponseWriter, request *http.Request) bool {
	principal, exists := auth.PrincipalFromContext(request.Context())
	if !exists {
		return true
	}
	if principal.IsAdmin() {
		return true
	}
	writeError(response, http.StatusForbidden, "admin_required", "Administrator access is required", "")
	return false
}

func (api *API) notifyTelegramAccessChanged() {
	if api.telegram != nil {
		api.telegram.NotifyAccessChanged()
	}
}

func (api *API) writeUserError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeError(response, http.StatusNotFound, "user_not_found", "User was not found", "")
	case errors.Is(err, store.ErrUserRevisionConflict):
		writeError(response, http.StatusConflict, "revision_conflict", "User changed since it was loaded", "revision")
	case errors.Is(err, store.ErrUsernameConflict):
		writeError(response, http.StatusConflict, "username_conflict", "Username is already in use", "username")
	case errors.Is(err, store.ErrUserValidation):
		writeError(response, http.StatusBadRequest, "invalid_user", "User data is invalid", "")
	default:
		api.writeInternalError(response, request, operation, err)
	}
}

func validMemberUsername(username string) bool {
	return username != "" &&
		utf8.RuneCountInString(username) <= maxUsernameRunes &&
		!strings.ContainsAny(username, "\x00\r\n")
}

func writePasswordValidationError(
	response http.ResponseWriter,
	err error,
	field string,
) {
	switch {
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeError(response, http.StatusUnprocessableEntity, "password_too_short", "Password must contain at least 8 characters", field)
	case errors.Is(err, auth.ErrPasswordTooLong):
		writeError(response, http.StatusUnprocessableEntity, "password_too_long", "Password cannot exceed 1024 bytes", field)
	default:
		writeError(response, http.StatusUnprocessableEntity, "password_invalid", "Password contains unsupported characters", field)
	}
}

func userResourcePath(path string) (userID, action string, ok bool) {
	const prefix = "/api/v1/users/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")
	if len(parts) == 0 || len(parts) > 2 || parts[0] == "" ||
		len(parts[0]) > maxIdentifierLength {
		return "", "", false
	}
	if len(parts) == 2 {
		if parts[1] != "password" {
			return "", "", false
		}
		action = parts[1]
	}
	return parts[0], action, true
}

func adminOnlyAPIPath(path, method string) bool {
	if path == "/api/v1/about" ||
		path == "/api/v1/updates/check" {
		return true
	}
	if path == "/api/v1/network" ||
		strings.HasPrefix(path, "/api/v1/proxies") ||
		path == "/api/v1/settings/lines" ||
		path == "/api/v1/settings/system" ||
		path == "/api/v1/settings/recording" ||
		path == "/api/v1/settings/telegram" ||
		strings.HasPrefix(path, "/api/v1/settings/telegram/") ||
		strings.HasPrefix(path, "/api/v1/lines/") {
		return false
	}
	if path == "/api/v1/devices" {
		return method != http.MethodGet &&
			method != http.MethodHead &&
			method != http.MethodOptions
	}
	if strings.HasPrefix(path, "/api/v1/devices/") {
		if _, ok := deviceConfigurationResourceID(path); ok {
			return false
		}
		if _, _, ok := networkSelectionResource(path); ok {
			return false
		}
		if _, _, ok := lineServiceResource(path); ok {
			return false
		}
		return true
	}
	for _, prefix := range []string{
		"/api/v1/diagnostics",
		"/api/v1/settings/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
