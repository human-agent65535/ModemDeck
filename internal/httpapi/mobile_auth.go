package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

var errMobileCredentialRevoked = errors.New(
	"iOS pairing credential is invalid or revoked",
)

type mobileAuthentication struct {
	Digest mobilepairing.TokenDigest
}

type mobileAuthenticationContextKey struct{}

func mobileAuthenticationFromContext(
	ctx context.Context,
) (mobileAuthentication, bool) {
	authentication, ok := ctx.Value(
		mobileAuthenticationContextKey{},
	).(mobileAuthentication)
	return authentication, ok
}

func (api *API) authorizeMobileAPI(
	response http.ResponseWriter,
	request *http.Request,
) bool {
	digest, err := mobileBearerDigest(request)
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
	principal, found, err := api.repository.IOSPairingPrincipalByTokenDigest(
		request.Context(),
		digest,
	)
	if err != nil {
		api.logger.Error("authenticate iOS pairing credential", "error", err)
		writeError(
			response,
			http.StatusServiceUnavailable,
			"authentication_unavailable",
			"Authentication is unavailable",
			"",
		)
		return false
	}
	if !found || principal.UserID == "" || !principal.IOSPairingEnabled {
		writeError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required",
			"",
		)
		return false
	}
	if !mobileAPIRequestAllowed(request) {
		writeError(
			response,
			http.StatusForbidden,
			"mobile_api_forbidden",
			"This API endpoint is unavailable to paired iOS clients",
			"",
		)
		return false
	}
	ctx := context.WithValue(
		request.Context(),
		mobileAuthenticationContextKey{},
		mobileAuthentication{Digest: digest},
	)
	ctx = contextWithCallLeaseMobileCredential(ctx, digest)
	ctx = auth.ContextWithPrincipal(ctx, principal)
	*request = *request.WithContext(ctx)
	return true
}

func (api *API) currentMobilePrincipal(
	ctx context.Context,
) (auth.Principal, error) {
	authentication, ok := mobileAuthenticationFromContext(ctx)
	if !ok {
		return auth.Principal{}, errMobileCredentialRevoked
	}
	principal, found, err := api.repository.IOSPairingPrincipalByTokenDigest(
		ctx,
		authentication.Digest,
	)
	if err != nil {
		return auth.Principal{}, err
	}
	if !found || principal.UserID == "" || !principal.IOSPairingEnabled {
		return auth.Principal{}, errMobileCredentialRevoked
	}
	return principal, nil
}

func mobileBearerDigest(
	request *http.Request,
) (mobilepairing.TokenDigest, error) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 {
		return mobilepairing.TokenDigest{}, mobilepairing.ErrInvalidToken
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return mobilepairing.TokenDigest{}, mobilepairing.ErrInvalidToken
	}
	return mobilepairing.Digest(mobilepairing.Token(fields[1]))
}

func mobileAPIRequestAllowed(request *http.Request) bool {
	switch request.URL.Path {
	case "/api/v1/bootstrap":
		return request.Method == http.MethodGet
	case "/api/v1/runtime/events":
		return request.Method == http.MethodGet
	case "/api/v1/mobile/pairing":
		return request.Method == http.MethodDelete
	case "/api/v1/calls":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPost
	case "/api/v1/calls/active":
		return request.Method == http.MethodGet
	}
	if _, ok := callLeaseResourceID(request.URL.Path); ok {
		return request.Method == http.MethodPut
	}
	if _, _, ok := callActionResource(request.URL.Path); ok {
		return request.Method == http.MethodPost
	}
	if _, ok := callMediaICEConfigurationResourceID(request.URL.Path); ok {
		return request.Method == http.MethodPost
	}
	if _, ok := callMediaResourceID(request.URL.Path); ok {
		return request.Method == http.MethodPost ||
			request.Method == http.MethodDelete
	}
	return false
}
