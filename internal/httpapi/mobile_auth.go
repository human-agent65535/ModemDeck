package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

var errMobileCredentialRevoked = errors.New(
	"iOS pairing credential is invalid or revoked",
)

const (
	mobileDeviceNameHeader            = "X-ModemDeck-Device-Name"
	mobileDeviceModelHeader           = "X-ModemDeck-Device-Model"
	mobileDeviceModelIdentifierHeader = "X-ModemDeck-Device-Model-Identifier"
	mobileOSNameHeader                = "X-ModemDeck-OS-Name"
	mobileOSVersionHeader             = "X-ModemDeck-OS-Version"
	mobileAppVersionHeader            = "X-ModemDeck-App-Version"
	mobileAppBuildHeader              = "X-ModemDeck-App-Build"
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
		mobileDeviceInfo(request),
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
	if !strings.HasPrefix(request.URL.Path, "/api/v1/") {
		return false
	}
	switch request.URL.Path {
	case "/api/v1/setup", "/api/v1/login", "/api/v1/session":
		return false
	case "/api/v1/mobile/pairing":
		return request.Method == http.MethodGet ||
			request.Method == http.MethodDelete
	}
	return true
}

func mobileDeviceInfo(request *http.Request) mobilepairing.DeviceInfo {
	return mobilepairing.DeviceInfo{
		Name:  mobileDeviceHeader(request, mobileDeviceNameHeader, 128),
		Model: mobileDeviceHeader(request, mobileDeviceModelHeader, 64),
		ModelIdentifier: mobileDeviceHeader(
			request,
			mobileDeviceModelIdentifierHeader,
			64,
		),
		OSName:     mobileDeviceHeader(request, mobileOSNameHeader, 64),
		OSVersion:  mobileDeviceHeader(request, mobileOSVersionHeader, 64),
		AppVersion: mobileDeviceHeader(request, mobileAppVersionHeader, 64),
		AppBuild:   mobileDeviceHeader(request, mobileAppBuildHeader, 64),
	}
}

func mobileDeviceHeader(request *http.Request, name string, maximumRunes int) string {
	value := strings.TrimSpace(request.Header.Get(name))
	if encoded, ok := strings.CutPrefix(value, "b64:"); ok {
		if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil &&
			utf8.Valid(decoded) {
			value = string(decoded)
		} else {
			value = ""
		}
	}
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	runes := []rune(value)
	if len(runes) > maximumRunes {
		value = string(runes[:maximumRunes])
	}
	return value
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
