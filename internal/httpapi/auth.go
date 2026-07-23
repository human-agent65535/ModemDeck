package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/auth"
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
	sessionToken, authentication, ok, handled := api.requestAuthentication(response, request, false)
	if !ok {
		if handled {
			return
		}
		writeJSON(response, http.StatusOK, sessionResponse{Authenticated: false})
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
		writeJSON(response, http.StatusOK, sessionResponse{Authenticated: false})
		return
	}
	writeJSON(response, http.StatusOK, sessionResponse{
		Authenticated: true,
		Username:      api.adminUsername,
		CSRFToken:     csrfCookie.Value,
	})
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
	if credentials.Username != api.adminUsername {
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect", "")
		return
	}
	select {
	case api.loginSlots <- struct{}{}:
		defer func() { <-api.loginSlots }()
	default:
		writeError(response, http.StatusTooManyRequests, "login_busy", "Another login is being verified", "")
		return
	}

	result, err := api.authenticator.Login(request.Context(), credentials.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrPasswordRequired) {
			writeError(response, http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect", "")
			return
		}
		api.logger.Error("login failed", "error", err)
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is unavailable", "")
		return
	}
	api.setAuthCookies(response, result)
	writeJSON(response, http.StatusOK, sessionResponse{
		Authenticated: true,
		Username:      api.adminUsername,
		CSRFToken:     string(result.CSRFToken),
	})
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

func (api *API) authorizeAPI(response http.ResponseWriter, request *http.Request) bool {
	if api.authenticator == nil {
		return true
	}
	_, authentication, ok, _ := api.requestAuthentication(response, request, true)
	if !ok {
		return false
	}
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
