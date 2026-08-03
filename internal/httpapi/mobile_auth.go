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
	if _, err := api.repository.ConfirmIOSPairingCredential(
		request.Context(),
		digest,
	); err != nil {
		api.logger.Error("confirm iOS pairing credential", "error", err)
		writeError(
			response,
			http.StatusServiceUnavailable,
			"authentication_unavailable",
			"Authentication is unavailable",
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
	case "/api/v1/mobile/session":
		return request.Method == http.MethodGet
	case "/api/v1/bootstrap":
		return request.Method == http.MethodGet
	case "/api/v1/account/contact":
		return request.Method == http.MethodPut
	case "/api/v1/contacts":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPost
	case "/api/v1/contacts/batch":
		return request.Method == http.MethodPatch
	case "/api/v1/messages/threads":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodDelete
	case "/api/v1/messages":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPost
	case "/api/v1/messages/read", "/api/v1/messages/threads/state":
		return request.Method == http.MethodPatch
	case "/api/v1/messages/events":
		return request.Method == http.MethodGet
	case "/api/v1/runtime/events":
		return request.Method == http.MethodGet
	case "/api/v1/mobile/pairing":
		return request.Method == http.MethodDelete
	case "/api/v1/calls":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPost
	case "/api/v1/calls/missed/read", "/api/v1/calls/batch":
		return request.Method == http.MethodPatch
	case "/api/v1/calls/active":
		return request.Method == http.MethodGet
	case "/api/v1/recordings":
		return request.Method == http.MethodGet
	case "/api/v1/recordings/batch":
		return request.Method == http.MethodPatch
	case "/api/v1/devices":
		return request.Method == http.MethodGet
	case "/api/v1/settings/calls":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPatch
	case "/api/v1/settings/lines", "/api/v1/settings/system":
		return request.Method == http.MethodPatch
	case "/api/v1/settings/recording":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPut
	}
	if _, ok := contactResourceID(request.URL.Path); ok {
		return request.Method == http.MethodGet ||
			request.Method == http.MethodPut ||
			request.Method == http.MethodDelete
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
	if resource, ok := parseRecordingResource(request.URL.Path); ok {
		switch resource.Kind {
		case recordingResourceToggle:
			return request.Method == http.MethodPut
		case recordingResourceList, recordingResourceDownload:
			return request.Method == http.MethodGet
		case recordingResourceDelete:
			return request.Method == http.MethodDelete
		}
	}
	if resource, ok := callRecordResource(request.URL.Path); ok {
		if resource.Action == "" {
			return request.Method == http.MethodDelete
		}
		return request.Method == http.MethodPatch
	}
	return false
}

func (api *API) mobileSession(
	response http.ResponseWriter,
	request *http.Request,
) {
	principal, exists := auth.PrincipalFromContext(request.Context())
	if !exists || principal.UserID == "" {
		writeError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required",
			"",
		)
		return
	}
	session := sessionResponse{
		Authenticated: true,
		SetupRequired: false,
		Language:      api.sessionLanguage(request.Context()),
	}
	applyPrincipalToSessionResponse(&session, principal)
	writeJSON(response, http.StatusOK, session)
}
