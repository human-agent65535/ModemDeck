package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type mobilePairingRepository interface {
	IOSPairingStatus(context.Context, string) (store.IOSPairingStatus, error)
	RotateIOSPairingCredential(
		context.Context,
		string,
		mobilepairing.TokenDigest,
	) (store.IOSPairingStatus, error)
	RevokeIOSPairingCredential(context.Context, string) error
}

type iosPairingStatusResponse struct {
	Allowed             bool                           `json:"allowed"`
	Cloudflare          mobilepairing.CloudflareStatus `json:"cloudflare"`
	TURN                turnAvailabilityStatus         `json:"turn"`
	HasCredential       bool                           `json:"has_credential"`
	CredentialCreatedAt string                         `json:"credential_created_at,omitempty"`
}

type iosPairingResponse struct {
	Pairing iosPairingStatusResponse `json:"pairing"`
	Payload *mobilepairing.Payload   `json:"payload,omitempty"`
}

func (api *API) mobileTunnelProbe(
	response http.ResponseWriter,
	request *http.Request,
) {
	probe, ok := api.mobilePairingAvailability.(mobilepairing.ProbeResponder)
	if request.Method != http.MethodGet || !ok {
		http.NotFound(response, request)
		return
	}
	proof, ok := probe.CloudflareProbeProof(request)
	if !ok {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
	response.Header().Set(mobilepairing.CloudflareProbeProofHeader, proof)
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) mobilePairing(
	response http.ResponseWriter,
	request *http.Request,
) {
	repository, ok := api.repository.(mobilePairingRepository)
	if !ok {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"mobile_pairing_unavailable",
			"iOS pairing is unavailable",
			"",
		)
		return
	}
	userID := auth.InitialAdminUserID
	if principal, exists := auth.PrincipalFromContext(request.Context()); exists {
		userID = principal.UserID
	}
	response.Header().Set("Cache-Control", "no-store")
	switch request.Method {
	case http.MethodGet:
		status, err := repository.IOSPairingStatus(request.Context(), userID)
		if err != nil {
			api.writeMobilePairingError(response, request, "read iOS pairing", err)
			return
		}
		writeJSON(
			response,
			http.StatusOK,
			iosPairingResponse{
				Pairing: api.iosPairingStatus(request.Context(), status),
			},
		)
	case http.MethodPost:
		current, err := repository.IOSPairingStatus(request.Context(), userID)
		if err != nil {
			api.writeMobilePairingError(response, request, "read iOS pairing", err)
			return
		}
		if !current.Allowed {
			api.writeMobilePairingError(
				response,
				request,
				"create iOS pairing",
				store.ErrIOSPairingNotAllowed,
			)
			return
		}
		cloudflare := api.cloudflareStatus(request.Context())
		if !cloudflare.Enabled {
			writeError(
				response,
				http.StatusConflict,
				"cloudflare_required",
				"Cloudflare Tunnel must be enabled during installation",
				"",
			)
			return
		}
		if !cloudflare.Connected {
			writeError(
				response,
				http.StatusServiceUnavailable,
				"cloudflare_unavailable",
				"Cloudflare Tunnel is not connected",
				"",
			)
			return
		}
		token, digest, err := mobilepairing.NewToken()
		if err != nil {
			api.writeInternalError(response, request, "generate iOS pairing token", err)
			return
		}
		status, err := repository.RotateIOSPairingCredential(
			request.Context(),
			userID,
			digest,
		)
		if err != nil {
			api.writeMobilePairingError(response, request, "create iOS pairing", err)
			return
		}
		payload := mobilepairing.NewPayload(cloudflare.PublicURL, token)
		writeJSON(
			response,
			http.StatusCreated,
			iosPairingResponse{
				Pairing: iosPairingStatusResponse{
					Allowed:             status.Allowed,
					Cloudflare:          cloudflare,
					TURN:                api.turnStatus(request.Context()),
					HasCredential:       status.HasCredential,
					CredentialCreatedAt: status.CredentialCreatedAt,
				},
				Payload: &payload,
			},
		)
	case http.MethodDelete:
		if err := repository.RevokeIOSPairingCredential(
			request.Context(),
			userID,
		); err != nil {
			api.writeMobilePairingError(response, request, "revoke iOS pairing", err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		response.Header().Set(
			"Allow",
			http.MethodGet+", "+http.MethodPost+", "+http.MethodDelete,
		)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET, POST, and DELETE are supported",
			"",
		)
	}
}

func (api *API) iosPairingStatus(
	ctx context.Context,
	status store.IOSPairingStatus,
) iosPairingStatusResponse {
	cloudflareResult := make(chan mobilepairing.CloudflareStatus, 1)
	go func() {
		cloudflareResult <- api.cloudflareStatus(ctx)
	}()
	turn := api.turnStatus(ctx)
	return iosPairingStatusResponse{
		Allowed:             status.Allowed,
		Cloudflare:          <-cloudflareResult,
		TURN:                turn,
		HasCredential:       status.HasCredential,
		CredentialCreatedAt: status.CredentialCreatedAt,
	}
}

func (api *API) cloudflareStatus(
	ctx context.Context,
) mobilepairing.CloudflareStatus {
	if api.mobilePairingAvailability == nil {
		return mobilepairing.CloudflareStatus{}
	}
	return api.mobilePairingAvailability.Status(ctx)
}

func (api *API) writeMobilePairingError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeError(
			response,
			http.StatusNotFound,
			"user_not_found",
			"User was not found",
			"",
		)
	case errors.Is(err, store.ErrIOSPairingNotAllowed):
		writeError(
			response,
			http.StatusForbidden,
			"ios_pairing_not_allowed",
			"An administrator has not enabled iOS pairing for this account",
			"",
		)
	default:
		api.writeInternalError(response, request, operation, err)
	}
}
