package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const iosPairingDeviceID = "ios-pairing"

type webSessionManager interface {
	WebSessions(context.Context, auth.SessionToken) ([]auth.WebSession, error)
	RevokeWebSession(
		context.Context,
		auth.SessionToken,
		string,
	) (auth.SessionTokenDigest, error)
	RevokeOtherWebSessions(
		context.Context,
		auth.SessionToken,
	) ([]auth.SessionTokenDigest, error)
}

type accountSessionRepository interface {
	IOSPairingStatus(context.Context, string) (store.IOSPairingStatus, error)
	RevokeIOSPairingCredentialWithDigest(
		context.Context,
		string,
	) (mobilepairing.TokenDigest, bool, error)
}

type accountSessionResponse struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	AccessIP   string `json:"access_ip,omitempty"`
	AccessHost string `json:"access_host,omitempty"`
	Current    bool   `json:"current"`
	Paired     bool   `json:"paired,omitempty"`
}

type accountSessionsResponse struct {
	Sessions []accountSessionResponse `json:"sessions"`
}

func (api *API) accountSessions(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET is supported",
			"",
		)
		return
	}
	manager, token, ok := api.webSessionManager(response, request)
	if !ok {
		return
	}
	sessions, err := manager.WebSessions(request.Context(), token)
	if err != nil {
		api.writeAccountSessionError(response, request, "list account sessions", err)
		return
	}
	result := make([]accountSessionResponse, 0, len(sessions)+1)
	for _, session := range sessions {
		lastSeenAt := session.LastSeenAt
		if lastSeenAt.IsZero() {
			lastSeenAt = session.CreatedAt
		}
		result = append(result, accountSessionResponse{
			ID:         session.ID,
			Kind:       "web",
			CreatedAt:  session.CreatedAt.UTC().Format(time.RFC3339),
			LastSeenAt: lastSeenAt.UTC().Format(time.RFC3339),
			UserAgent:  session.UserAgent,
			AccessIP:   session.AccessIP,
			AccessHost: session.AccessHost,
			Current:    session.Current,
		})
	}
	principal, hasPrincipal := auth.PrincipalFromContext(request.Context())
	pairingRepository, supportsPairing := api.repository.(accountSessionRepository)
	if hasPrincipal && supportsPairing {
		status, err := pairingRepository.IOSPairingStatus(
			request.Context(),
			principal.UserID,
		)
		if err != nil {
			api.logger.Error("read iOS account session", "error", err)
			writeError(
				response,
				http.StatusServiceUnavailable,
				"sessions_unavailable",
				"Signed-in devices are unavailable",
				"",
			)
			return
		}
		if status.HasCredential {
			createdAt := status.CredentialCreatedAt
			if status.PairedAt != "" {
				createdAt = status.PairedAt
			}
			result = append(result, accountSessionResponse{
				ID:        iosPairingDeviceID,
				Kind:      "ios",
				CreatedAt: createdAt,
				Paired:    status.Paired,
			})
		}
	}
	writeJSON(response, http.StatusOK, accountSessionsResponse{Sessions: result})
}

func (api *API) accountSessionResource(
	response http.ResponseWriter,
	request *http.Request,
	sessionID string,
) {
	if request.Method != http.MethodDelete {
		response.Header().Set("Allow", http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only DELETE is supported",
			"",
		)
		return
	}
	manager, token, ok := api.webSessionManager(response, request)
	if !ok {
		return
	}
	if sessionID == iosPairingDeviceID {
		api.revokeIOSAccountSession(response, request)
		return
	}
	digest, err := manager.RevokeWebSession(
		request.Context(),
		token,
		sessionID,
	)
	if err != nil {
		api.writeAccountSessionError(response, request, "revoke account session", err)
		return
	}
	api.endRevokedWebSessionCall(request.Context(), digest)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) revokeOtherAccountSessions(
	response http.ResponseWriter,
	request *http.Request,
) {
	if request.Method != http.MethodDelete {
		response.Header().Set("Allow", http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only DELETE is supported",
			"",
		)
		return
	}
	manager, token, ok := api.webSessionManager(response, request)
	if !ok {
		return
	}
	digests, err := manager.RevokeOtherWebSessions(request.Context(), token)
	if err != nil {
		api.writeAccountSessionError(response, request, "revoke other account sessions", err)
		return
	}
	for _, digest := range digests {
		api.endRevokedWebSessionCall(request.Context(), digest)
	}

	principal, hasPrincipal := auth.PrincipalFromContext(request.Context())
	pairingRepository, supportsPairing := api.repository.(accountSessionRepository)
	if hasPrincipal && supportsPairing {
		digest, revoked, err := pairingRepository.RevokeIOSPairingCredentialWithDigest(
			request.Context(),
			principal.UserID,
		)
		if err != nil {
			api.logger.Error("revoke iOS account session", "error", err)
			writeError(
				response,
				http.StatusServiceUnavailable,
				"sessions_unavailable",
				"Unable to log out other devices",
				"",
			)
			return
		}
		if revoked {
			api.endRevokedIOSSessionCall(request.Context(), digest)
		}
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) revokeIOSAccountSession(
	response http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := auth.PrincipalFromContext(request.Context())
	if !ok {
		writeError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required",
			"",
		)
		return
	}
	repository, ok := api.repository.(accountSessionRepository)
	if !ok {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"sessions_unavailable",
			"Signed-in devices are unavailable",
			"",
		)
		return
	}
	digest, revoked, err := repository.RevokeIOSPairingCredentialWithDigest(
		request.Context(),
		principal.UserID,
	)
	if err != nil {
		api.logger.Error("revoke iOS account session", "error", err)
		writeError(
			response,
			http.StatusServiceUnavailable,
			"sessions_unavailable",
			"Unable to log out this device",
			"",
		)
		return
	}
	if !revoked {
		writeError(
			response,
			http.StatusNotFound,
			"session_not_found",
			"This device is no longer signed in",
			"",
		)
		return
	}
	api.endRevokedIOSSessionCall(request.Context(), digest)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) webSessionManager(
	response http.ResponseWriter,
	request *http.Request,
) (webSessionManager, auth.SessionToken, bool) {
	manager, ok := api.authenticator.(webSessionManager)
	if !ok {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"sessions_unavailable",
			"Signed-in devices are unavailable",
			"",
		)
		return nil, "", false
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required",
			"",
		)
		return nil, "", false
	}
	return manager, auth.SessionToken(cookie.Value), true
}

func (api *API) writeAccountSessionError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, auth.ErrSessionNotFound):
		writeError(
			response,
			http.StatusNotFound,
			"session_not_found",
			"This device is no longer signed in",
			"",
		)
	case errors.Is(err, auth.ErrCurrentSession):
		writeError(
			response,
			http.StatusConflict,
			"current_session",
			"Use Log out to end the current session",
			"",
		)
	case errors.Is(err, auth.ErrUnauthenticated),
		errors.Is(err, auth.ErrInvalidSessionToken):
		writeError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required",
			"",
		)
	default:
		api.logger.Error(operation, "error", err, "path", request.URL.Path)
		writeError(
			response,
			http.StatusServiceUnavailable,
			"sessions_unavailable",
			"Signed-in devices are unavailable",
			"",
		)
	}
}

func (api *API) endRevokedWebSessionCall(
	ctx context.Context,
	digest auth.SessionTokenDigest,
) {
	if err := api.revokeCallHolder(
		ctx,
		callLeaseHolderForSessionDigest(digest),
	); err != nil {
		api.logger.Error("end call for revoked Web session", "error", err)
	}
}

func (api *API) endRevokedIOSSessionCall(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) {
	if err := api.revokeCallHolder(
		ctx,
		callLeaseHolderForMobileCredential(digest),
	); err != nil {
		api.logger.Error("end call for revoked iOS session", "error", err)
	}
}

func accountSessionResourceID(path string) (string, bool) {
	const prefix = "/api/v1/account/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(path, prefix)
	if id == "" || id == "others" || strings.ContainsRune(id, '/') {
		return "", false
	}
	return id, true
}
