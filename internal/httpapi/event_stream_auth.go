package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

var (
	errCurrentStreamAdminRequired = errors.New("current session is not an administrator")
)

const eventStreamAuthenticationInterval = 15 * time.Second

func (api *API) authorizeEventStream(
	response http.ResponseWriter,
	request *http.Request,
	requireAdmin bool,
) (auth.Principal, bool, bool) {
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
			return auth.Principal{}, false, false
		}
		if requireAdmin && !principal.IsAdmin() {
			writeError(
				response,
				http.StatusForbidden,
				"admin_required",
				"Administrator access is required",
				"",
			)
			return auth.Principal{}, false, false
		}
		return principal, true, true
	}
	if api.authenticator == nil {
		principal, scoped := auth.PrincipalFromContext(request.Context())
		if requireAdmin && scoped && !principal.IsAdmin() {
			writeError(
				response,
				http.StatusForbidden,
				"admin_required",
				"Administrator access is required",
				"",
			)
			return auth.Principal{}, false, false
		}
		return principal, scoped, true
	}
	_, authentication, ok, _ := api.requestAuthentication(response, request, true)
	if !ok {
		return auth.Principal{}, false, false
	}
	principal, scoped := authentication.Principal()
	if requireAdmin && scoped && !principal.IsAdmin() {
		writeError(
			response,
			http.StatusForbidden,
			"admin_required",
			"Administrator access is required",
			"",
		)
		return auth.Principal{}, false, false
	}
	return principal, scoped, true
}

func (api *API) currentStreamAccess(
	request *http.Request,
	requireAdmin bool,
) (auth.Principal, bool, error) {
	principal, scoped, err := api.currentStreamPrincipal(request)
	if err != nil {
		return auth.Principal{}, false, err
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
	return streamCanAccessLine(principal, scoped, lineID), nil
}

func streamCanAccessLine(principal auth.Principal, scoped bool, lineID string) bool {
	return !scoped || principal.CanAccessLine(strings.TrimSpace(lineID))
}

func sameStreamAccess(
	left auth.Principal,
	leftScoped bool,
	right auth.Principal,
	rightScoped bool,
) bool {
	return leftScoped == rightScoped &&
		left.UserID == right.UserID &&
		left.Username == right.Username &&
		left.Role == right.Role &&
		left.ProfileContactID == right.ProfileContactID &&
		left.IOSPairingEnabled == right.IOSPairingEnabled &&
		slices.Equal(left.AllowedLineIDs, right.AllowedLineIDs)
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
