package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	sessionCookieName = "modemdeck_session"
	csrfCookieName    = "modemdeck_csrf"
	csrfHeaderName    = "X-ModemDeck-CSRF"
	maxUsernameRunes  = 64
	maxPasswordBytes  = 1024
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (api *API) session(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		api.getSession(response, request)
	case http.MethodPost:
		api.login(response, request)
	case http.MethodDelete:
		if api.authorizeAPI(response, request) {
			api.logout(response, request)
		}
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost+", "+http.MethodDelete)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET, POST, and DELETE are supported", "")
	}
}

func (api *API) getSession(response http.ResponseWriter, request *http.Request) {
	language := string(store.SystemLanguageAuto)
	if api.authenticator == nil {
		writeJSON(response, http.StatusOK, sessionResponse{
			Authenticated: false,
			SetupRequired: false,
			Language:      language,
		})
		return
	}
	status, err := api.authenticator.Status(request.Context())
	if err != nil {
		api.logger.Error("read administrator status", "error", err)
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
		return
	}
	if !status.Configured {
		api.clearAuthCookies(response)
		writeJSON(response, http.StatusOK, sessionResponse{
			Authenticated: false,
			SetupRequired: true,
			Language:      language,
		})
		return
	}
	sessionToken, authentication, ok, handled := api.requestAuthentication(response, request, false)
	if !ok {
		if handled {
			return
		}
		writeJSON(response, http.StatusOK, sessionResponse{
			Authenticated: false,
			SetupRequired: false,
			Language:      language,
		})
		return
	}
	csrfCookie, err := request.Cookie(csrfCookieName)
	if err != nil || authentication.VerifyCSRF(auth.CSRFToken(csrfCookie.Value)) != nil {
		if err := api.authenticator.Logout(request.Context(), sessionToken); err != nil {
			api.logger.Error("revoke session with invalid CSRF cookie", "error", err)
			writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
			return
		}
		api.clearAuthCookies(response)
		writeJSON(response, http.StatusOK, sessionResponse{
			Authenticated: false,
			SetupRequired: false,
			Language:      language,
		})
		return
	}
	session := sessionResponse{
		Authenticated: true,
		SetupRequired: false,
		Username:      status.Username,
		CSRFToken:     csrfCookie.Value,
		Language:      language,
	}
	if principal, exists := authentication.Principal(); exists {
		applyPrincipalToSessionResponse(&session, principal)
		session.Language = api.sessionLanguage(
			auth.ContextWithPrincipal(request.Context(), principal),
		)
	}
	writeJSON(response, http.StatusOK, session)
}

func (api *API) setup(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported", "")
		return
	}

	var input setupRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" ||
		utf8.RuneCountInString(input.Username) > maxUsernameRunes ||
		len(input.Password) == 0 ||
		len(input.Password) > maxPasswordBytes {
		writeError(response, http.StatusUnprocessableEntity, "invalid_setup", "Enter a valid username and password", "")
		return
	}

	select {
	case api.loginSlots <- struct{}{}:
		defer func() { <-api.loginSlots }()
	default:
		writeError(response, http.StatusTooManyRequests, "setup_busy", "Another setup is in progress", "")
		return
	}

	if err := api.authenticator.Setup(request.Context(), input.Username, input.Password); err != nil {
		switch {
		case errors.Is(err, auth.ErrAlreadyConfigured):
			writeError(response, http.StatusConflict, "setup_complete", "Administrator setup is already complete", "")
		case errors.Is(err, auth.ErrUsernameInvalid):
			writeError(response, http.StatusUnprocessableEntity, "username_invalid", "Enter a valid username", "username")
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeError(response, http.StatusUnprocessableEntity, "password_too_short", "Password must contain at least 8 characters", "password")
		case errors.Is(err, auth.ErrPasswordTooLong):
			writeError(response, http.StatusUnprocessableEntity, "password_too_long", "Password cannot exceed 1024 bytes", "password")
		case errors.Is(err, auth.ErrPasswordInvalid):
			writeError(response, http.StatusUnprocessableEntity, "password_invalid", "Password contains unsupported characters", "password")
		default:
			api.logger.Error("set up administrator", "error", err)
			writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
		}
		return
	}

	result, err := api.authenticator.Login(request.Context(), input.Username, input.Password)
	if err != nil {
		api.logger.Error("start administrator session after setup", "error", err)
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
		return
	}
	api.setAuthCookies(response, result)
	session := sessionResponse{
		Authenticated: true,
		SetupRequired: false,
		Username:      input.Username,
		CSRFToken:     string(result.CSRFToken),
		Language:      api.sessionLanguageForPrincipal(request.Context(), result.Principal),
	}
	if result.Principal != nil {
		applyPrincipalToSessionResponse(&session, *result.Principal)
	}
	writeJSON(response, http.StatusCreated, session)
}

func (api *API) login(response http.ResponseWriter, request *http.Request) {
	var credentials loginRequest
	if !decodeJSONBody(response, request, &credentials) {
		return
	}
	credentials.Username = strings.TrimSpace(credentials.Username)
	if credentials.Username == "" ||
		utf8.RuneCountInString(credentials.Username) > maxUsernameRunes ||
		len(credentials.Password) == 0 ||
		len(credentials.Password) > maxPasswordBytes {
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect", "")
		return
	}
	if retryAfter := api.loginFailures.retryAfter(credentials.Username); retryAfter > 0 {
		writeLoginRateLimit(response, retryAfter)
		return
	}
	select {
	case api.loginSlots <- struct{}{}:
		defer func() { <-api.loginSlots }()
	default:
		writeError(response, http.StatusTooManyRequests, "login_busy", "Another login is being verified", "")
		return
	}

	result, err := api.authenticator.Login(
		request.Context(),
		credentials.Username,
		credentials.Password,
	)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrPasswordRequired) {
			api.loginFailures.recordFailure(credentials.Username)
			writeError(response, http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect", "")
			return
		}
		api.logger.Error("login failed", "error", err)
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
		return
	}
	api.loginFailures.recordSuccess(credentials.Username)
	api.setAuthCookies(response, result)
	session := sessionResponse{
		Authenticated: true,
		SetupRequired: false,
		Username:      credentials.Username,
		CSRFToken:     string(result.CSRFToken),
		Language:      api.sessionLanguageForPrincipal(request.Context(), result.Principal),
	}
	if result.Principal != nil {
		applyPrincipalToSessionResponse(&session, *result.Principal)
	}
	writeJSON(response, http.StatusOK, session)
}

func writeLoginRateLimit(response http.ResponseWriter, retryAfter time.Duration) {
	retryAfterSeconds := (retryAfter + time.Second - 1) / time.Second
	response.Header().Set("Retry-After", strconv.FormatInt(int64(retryAfterSeconds), 10))
	writeError(
		response,
		http.StatusTooManyRequests,
		"login_rate_limited",
		"Too many failed login attempts; try again later",
		"",
	)
}

func applyPrincipalToSessionResponse(response *sessionResponse, principal auth.Principal) {
	if response == nil || principal.UserID == "" {
		return
	}
	response.UserID = principal.UserID
	response.Username = principal.Username
	response.Role = string(principal.Role)
	response.ProfileContactID = principal.ProfileContactID
	response.IOSPairingEnabled = principal.IOSPairingEnabled
	response.AllowedLineIDs = append([]string(nil), principal.AllowedLineIDs...)
}

func (api *API) sessionLanguage(ctx context.Context) string {
	settings, err := api.repository.SystemSettings(ctx)
	if err != nil {
		api.logger.Warn("system language is unavailable", "error", err)
		return string(store.SystemLanguageAuto)
	}
	return string(settings.Language)
}

func (api *API) sessionLanguageForPrincipal(
	ctx context.Context,
	principal *auth.Principal,
) string {
	if principal == nil {
		return string(store.SystemLanguageAuto)
	}
	ctx = auth.ContextWithPrincipal(ctx, *principal)
	return api.sessionLanguage(ctx)
}

func (api *API) logout(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(sessionCookieName)
	if err == nil {
		if err := api.authenticator.Logout(request.Context(), auth.SessionToken(cookie.Value)); err != nil {
			api.logger.Error("logout failed", "error", err)
			writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
			return
		}
	}
	api.clearAuthCookies(response)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) accountPassword(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		response.Header().Set("Allow", http.MethodPut)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT is supported", "")
		return
	}

	var input changePasswordRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}

	select {
	case api.loginSlots <- struct{}{}:
		defer func() { <-api.loginSlots }()
	default:
		writeError(response, http.StatusTooManyRequests, "password_change_busy", "Another password operation is in progress", "")
		return
	}

	err := api.authenticator.ChangePassword(
		request.Context(),
		input.CurrentPassword,
		input.NewPassword,
	)
	switch {
	case err == nil:
		api.publishRuntimeResources(runtimeevents.ResourceSession)
		api.clearAuthCookies(response)
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(response, http.StatusBadRequest, "invalid_current_password", "Current password is incorrect", "current_password")
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeError(response, http.StatusUnprocessableEntity, "password_too_short", "New password must contain at least 8 characters", "new_password")
	case errors.Is(err, auth.ErrPasswordTooLong):
		writeError(response, http.StatusUnprocessableEntity, "password_too_long", "New password cannot exceed 1024 bytes", "new_password")
	case errors.Is(err, auth.ErrPasswordInvalid):
		writeError(response, http.StatusUnprocessableEntity, "password_invalid", "New password contains unsupported characters", "new_password")
	case errors.Is(err, auth.ErrPasswordUnchanged):
		writeError(response, http.StatusUnprocessableEntity, "password_unchanged", "New password must differ from the current password", "new_password")
	default:
		api.logger.Error("change administrator password", "error", err)
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
	}
}

func (api *API) authorizeAPI(response http.ResponseWriter, request *http.Request) bool {
	if len(request.Header.Values("Authorization")) > 0 {
		return api.authorizeMobileAPI(response, request)
	}
	if api.authenticator == nil {
		return true
	}
	sessionToken, authentication, ok, _ := api.requestAuthentication(
		response,
		request,
		true,
	)
	if !ok {
		return false
	}
	ctx := contextWithCallLeaseSession(request.Context(), sessionToken)
	if principal, exists := authentication.Principal(); exists {
		ctx = auth.ContextWithPrincipal(ctx, principal)
	}
	*request = *request.WithContext(ctx)
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return true
	}
	if err := authentication.VerifyCSRF(auth.CSRFToken(request.Header.Get(csrfHeaderName))); err != nil {
		writeError(response, http.StatusForbidden, "csrf_failed", "CSRF token is missing or invalid", "")
		return false
	}
	return true
}

func (api *API) requestAuthentication(
	response http.ResponseWriter,
	request *http.Request,
	writeFailure bool,
) (auth.SessionToken, auth.Authentication, bool, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		if writeFailure {
			writeError(response, http.StatusUnauthorized, "authentication_required", "Authentication is required", "")
		}
		return "", auth.Authentication{}, false, writeFailure
	}
	token := auth.SessionToken(cookie.Value)
	authentication, err := api.authenticator.Authenticate(request.Context(), token)
	if err == nil {
		return token, authentication, true, false
	}
	if request.Context().Err() != nil || errors.Is(err, context.Canceled) {
		return "", auth.Authentication{}, false, true
	}
	if errors.Is(err, auth.ErrUnauthenticated) ||
		errors.Is(err, auth.ErrSessionExpired) ||
		errors.Is(err, auth.ErrInvalidSessionToken) ||
		errors.Is(err, auth.ErrInvalidSessionRecord) {
		_ = api.authenticator.Logout(request.Context(), token)
		api.clearAuthCookies(response)
		if writeFailure {
			writeError(response, http.StatusUnauthorized, "authentication_required", "Authentication is required", "")
		}
		return "", auth.Authentication{}, false, writeFailure
	}
	api.logger.Error("authenticate request", "error", err)
	writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
	return "", auth.Authentication{}, false, true
}

func (api *API) setAuthCookies(response http.ResponseWriter, result auth.LoginResult) {
	maxAge := int(auth.SessionLifetime.Seconds())
	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookieName,
		Value:    string(result.SessionToken),
		Path:     "/",
		Expires:  result.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   api.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(response, &http.Cookie{
		Name:     csrfCookieName,
		Value:    string(result.CSRFToken),
		Path:     "/",
		Expires:  result.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   api.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (api *API) clearAuthCookies(response http.ResponseWriter) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(response, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == sessionCookieName,
			Secure:   api.secureCookies,
			SameSite: http.SameSiteStrictMode,
		})
	}
}
