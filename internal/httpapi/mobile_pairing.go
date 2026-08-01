package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

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

type iosPairingAvailability string

const (
	iosPairingPermissionRequired iosPairingAvailability = "permission_required"
	iosPairingCloudflareRequired iosPairingAvailability = "cloudflare_required"
	iosPairingConnectorDown      iosPairingAvailability = "connector_unavailable"
	iosPairingRouteUnavailable   iosPairingAvailability = "route_unavailable"
	iosPairingReady              iosPairingAvailability = "ready"
)

type iosPairingStatusResponse struct {
	Allowed             bool                   `json:"allowed"`
	Availability        iosPairingAvailability `json:"availability"`
	HasCredential       bool                   `json:"has_credential"`
	CredentialCreatedAt string                 `json:"credential_created_at,omitempty"`
	Paired              bool                   `json:"paired"`
	PairedAt            string                 `json:"paired_at,omitempty"`
	ServerURLs          []string               `json:"server_urls,omitempty"`
}

type iosPairingResponse struct {
	Pairing iosPairingStatusResponse `json:"pairing"`
	Payload *mobilepairing.Payload   `json:"payload,omitempty"`
}

type createIOSPairingRequest struct {
	ServerURL string `json:"server_url"`
}

type externalAccessStatusResponse struct {
	Cloudflare mobilepairing.CloudflareStatus `json:"cloudflare"`
	TURN       turnAvailabilityStatus         `json:"turn"`
	OriginTLS  CloudflareOriginTLSStatus      `json:"origin_tls"`
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

func (api *API) externalAccessStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	cloudflareResult := make(chan mobilepairing.CloudflareStatus, 1)
	go func() {
		cloudflareResult <- api.cloudflareStatus(request.Context())
	}()
	turn := api.turnStatus(request.Context())
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, externalAccessStatusResponse{
		Cloudflare: <-cloudflareResult,
		TURN:       turn,
		OriginTLS:  api.cloudflareOriginTLSStatus(request.Context()),
	})
}

func (api *API) refreshExternalAccess(
	response http.ResponseWriter,
	request *http.Request,
) {
	refresher, ok := api.mobilePairingAvailability.(mobilepairing.Refresher)
	if !ok {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"external_access_refresh_unavailable",
			"External access refresh is unavailable",
			"",
		)
		return
	}
	cloudflareResult := make(chan mobilepairing.CloudflareStatus, 1)
	go func() {
		cloudflareResult <- refresher.Refresh(request.Context())
	}()
	turn := api.turnStatus(request.Context())
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, externalAccessStatusResponse{
		Cloudflare: <-cloudflareResult,
		TURN:       turn,
		OriginTLS:  api.cloudflareOriginTLSStatus(request.Context()),
	})
}

func (api *API) cloudflareOriginTLSStatus(
	ctx context.Context,
) CloudflareOriginTLSStatus {
	if api.cloudflareOriginTLSService == nil {
		return CloudflareOriginTLSStatus{DNSNames: []string{}}
	}
	status := api.cloudflareOriginTLSService.Status(ctx)
	if status.DNSNames == nil {
		status.DNSNames = []string{}
	}
	return status
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
		var input createIOSPairingRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		serverURL, err := selectIOSPairingServerURL(
			input.ServerURL,
			verifiedCloudflareAPIURLs(cloudflare),
		)
		if err != nil {
			switch {
			case errors.Is(err, errIOSPairingServerURLRequired):
				writeError(
					response,
					http.StatusUnprocessableEntity,
					"server_url_required",
					"Choose an API address for this iOS pairing",
					"server_url",
				)
			default:
				writeError(
					response,
					http.StatusUnprocessableEntity,
					"invalid_server_url",
					"The selected API address is unavailable",
					"server_url",
				)
			}
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
		payload := mobilepairing.NewPayload(serverURL, token)
		writeJSON(
			response,
			http.StatusCreated,
			iosPairingResponse{
				Pairing: iosPairingStatusForCloudflare(status, cloudflare),
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
	if !status.Allowed {
		return iosPairingStatusForCloudflare(
			status,
			mobilepairing.CloudflareStatus{},
		)
	}
	return iosPairingStatusForCloudflare(status, api.cloudflareStatus(ctx))
}

func iosPairingStatusForCloudflare(
	status store.IOSPairingStatus,
	cloudflare mobilepairing.CloudflareStatus,
) iosPairingStatusResponse {
	availability := iosPairingReady
	switch {
	case !status.Allowed:
		availability = iosPairingPermissionRequired
	case !cloudflare.Enabled:
		availability = iosPairingCloudflareRequired
	case !cloudflare.ConnectorConnected && !cloudflare.Connected:
		availability = iosPairingConnectorDown
	case !cloudflare.Connected:
		availability = iosPairingRouteUnavailable
	}
	return iosPairingStatusResponse{
		Allowed:             status.Allowed,
		Availability:        availability,
		HasCredential:       status.HasCredential,
		CredentialCreatedAt: status.CredentialCreatedAt,
		Paired:              status.Paired,
		PairedAt:            status.PairedAt,
		ServerURLs:          verifiedCloudflareAPIURLs(cloudflare),
	}
}

var (
	errIOSPairingServerURLRequired = errors.New(
		"iOS pairing server URL is required",
	)
	errIOSPairingServerURLInvalid = errors.New(
		"iOS pairing server URL is invalid",
	)
)

func verifiedCloudflareAPIURLs(
	status mobilepairing.CloudflareStatus,
) []string {
	urls := append([]string(nil), status.VerifiedAPIURLs...)
	if len(urls) == 0 && status.Connected && status.PublicURL != "" {
		urls = append(urls, status.PublicURL)
	}
	slices.Sort(urls)
	return slices.Compact(urls)
}

func selectIOSPairingServerURL(
	requested string,
	verified []string,
) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if len(verified) == 1 {
			return verified[0], nil
		}
		return "", errIOSPairingServerURLRequired
	}
	if !slices.Contains(verified, requested) {
		return "", errIOSPairingServerURLInvalid
	}
	return requested, nil
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
