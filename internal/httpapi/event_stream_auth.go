package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

var (
	errCurrentStreamPasswordChangeRequired = errors.New("current session requires a password change")
	errCurrentStreamAdminRequired          = errors.New("current session is not an administrator")
)

func (api *API) authorizeEventStream(
	response http.ResponseWriter,
	request *http.Request,
	requireAdmin bool,
) bool {
	if _, mobile := mobileAuthenticationFromContext(request.Context()); mobile {
		principal, err := api.currentMobilePrincipal(request.Context())
		if err != nil {
			writeError(
				response,
				http.StatusUnauthorized,
				"authentication_required",
				"Authentication is required",
				"",
			)
			return false
		}
		if principal.MustChangePassword {
			writeError(
				response,
				http.StatusForbidden,
				"password_change_required",
				"Change your temporary password before continuing",
				"",
			)
			return false
		}
		if requireAdmin && !principal.IsAdmin() {
			writeError(
				response,
				http.StatusForbidden,
				"admin_required",
				"Administrator access is required",
				"",
			)
			return false
		}
		return true
	}
	if api.authenticator == nil {
		return true
	}
	_, authentication, ok, _ := api.requestAuthentication(response, request, true)
	if !ok {
		return false
	}
	principal, scoped := authentication.Principal()
	if scoped && principal.MustChangePassword {
		writeError(
			response,
			http.StatusForbidden,
			"password_change_required",
			"Change your temporary password before continuing",
			"",
		)
		return false
	}
	if requireAdmin && scoped && !principal.IsAdmin() {
		writeError(
			response,
			http.StatusForbidden,
			"admin_required",
			"Administrator access is required",
			"",
		)
		return false
	}
	return true
}

func (api *API) currentStreamAccess(
	request *http.Request,
	requireAdmin bool,
) (auth.Principal, bool, error) {
	principal, scoped, err := api.currentStreamPrincipal(request)
	if err != nil {
		return auth.Principal{}, false, err
	}
	if scoped && principal.MustChangePassword {
		return auth.Principal{}, false, errCurrentStreamPasswordChangeRequired
	}
	if requireAdmin && scoped && !principal.IsAdmin() {
		return auth.Principal{}, false, errCurrentStreamAdminRequired
	}
	return principal, scoped, nil
}

func (api *API) currentStreamCanAccessLine(
	request *http.Request,
	lineID string,
) (bool, error) {
	principal, scoped, err := api.currentStreamAccess(request, false)
	if err != nil {
		return false, err
	}
	return !scoped || principal.CanAccessLine(strings.TrimSpace(lineID)), nil
}

func (api *API) currentStreamPrincipal(
	request *http.Request,
) (auth.Principal, bool, error) {
	if _, mobile := mobileAuthenticationFromContext(request.Context()); mobile {
		principal, err := api.currentMobilePrincipal(request.Context())
		return principal, err == nil, err
	}
	if api.authenticator == nil {
		principal, exists := auth.PrincipalFromContext(request.Context())
		return principal, exists, nil
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return auth.Principal{}, false, err
	}
	authentication, err := api.authenticator.Authenticate(
		request.Context(),
		auth.SessionToken(cookie.Value),
	)
	if err != nil {
		return auth.Principal{}, false, err
	}
	principal, exists := authentication.Principal()
	if !exists {
		if _, scoped := auth.PrincipalFromContext(request.Context()); scoped {
			return auth.Principal{}, false, errors.New("current session has no user principal")
		}
	}
	return principal, exists, nil
}
