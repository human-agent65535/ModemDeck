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
	CreateIOSPairingCredential(
		context.Context,
		string,
		mobilepairing.TokenDigest,
	) (store.IOSPairingStatus, error)
	RevokeIOSPairingCredentialWithDigest(
		context.Context,
		string,
		string,
	) (mobilepairing.TokenDigest, bool, error)
	RevokeIOSPairingCredentialByTokenDigest(
		context.Context,
		mobilepairing.TokenDigest,
	) (bool, error)
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
	Allowed             bool                       `json:"allowed"`
	Availability        iosPairingAvailability     `json:"availability"`
	HasCredential       bool                       `json:"has_credential"`
	CredentialCreatedAt string                     `json:"credential_created_at,omitempty"`
	Paired              bool                       `json:"paired"`
	PairedAt            string                     `json:"paired_at,omitempty"`
	Device              *mobilepairing.DeviceInfo  `json:"device,omitempty"`
	LastSeenAt          string                     `json:"last_seen_at,omitempty"`
	Devices             []iosPairingDeviceResponse `json:"devices"`
	Pending             *iosPairingPendingResponse `json:"pending,omitempty"`
	DeviceLimit         int                        `json:"device_limit"`
	ServerURLs          []string                   `json:"server_urls,omitempty"`
}

type iosPairingDeviceResponse struct {
	ID                  string                    `json:"id"`
	CredentialCreatedAt string                    `json:"credential_created_at"`
	PairedAt            string                    `json:"paired_at"`
	Device              *mobilepairing.DeviceInfo `json:"device,omitempty"`
	LastSeenAt          string                    `json:"last_seen_at,omitempty"`
}

type iosPairingPendingResponse struct {
	ID                  string `json:"id"`
	CredentialCreatedAt string `json:"credential_created_at"`
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
		status, err := repository.CreateIOSPairingCredential(
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
		if authentication, mobile := mobileAuthenticationFromContext(request.Context()); mobile {
			revoked, err := repository.RevokeIOSPairingCredentialByTokenDigest(
				request.Context(),
				authentication.Digest,
			)
			if err != nil {
				api.writeMobilePairingError(response, request, "revoke current iOS pairing", err)
				return
			}
			if !revoked {
				api.writeMobilePairingError(
					response,
					request,
					"revoke current iOS pairing",
					store.ErrIOSPairingCredentialNotFound,
				)
				return
			}
			api.endRevokedIOSSessionCall(request.Context(), authentication.Digest)
			response.WriteHeader(http.StatusNoContent)
			return
		}
		credentialID := strings.TrimSpace(request.URL.Query().Get("credential_id"))
		if credentialID == "" {
			current, err := repository.IOSPairingStatus(request.Context(), userID)
			if err != nil {
				api.writeMobilePairingError(response, request, "read iOS pairing", err)
				return
			}
			switch {
			case current.Pending != nil && len(current.Devices) == 0:
				credentialID = current.Pending.ID
			case current.Pending == nil && len(current.Devices) == 1:
				credentialID = current.Devices[0].ID
			default:
				writeError(
					response,
					http.StatusUnprocessableEntity,
					"ios_pairing_credential_required",
					"Choose an Apple device to revoke",
					"credential_id",
				)
				return
			}
		}
		digest, revoked, err := repository.RevokeIOSPairingCredentialWithDigest(
			request.Context(),
			userID,
			credentialID,
		)
		if err != nil {
			api.writeMobilePairingError(response, request, "revoke iOS pairing", err)
			return
		}
		if !revoked {
			api.writeMobilePairingError(
				response,
				request,
				"revoke iOS pairing",
				store.ErrIOSPairingCredentialNotFound,
			)
			return
		}
		api.endRevokedIOSSessionCall(request.Context(), digest)
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
	devices := make([]iosPairingDeviceResponse, 0, len(status.Devices))
	for _, device := range status.Devices {
		devices = append(devices, iosPairingDeviceResponse{
			ID:                  device.ID,
			CredentialCreatedAt: device.CredentialCreatedAt,
			PairedAt:            device.PairedAt,
			Device:              iosPairingDeviceInfo(device.Device),
			LastSeenAt:          device.LastSeenAt,
		})
	}
	var pending *iosPairingPendingResponse
	if status.Pending != nil {
		pending = &iosPairingPendingResponse{
			ID:                  status.Pending.ID,
			CredentialCreatedAt: status.Pending.CredentialCreatedAt,
		}
	}
	deviceLimit := status.DeviceLimit
	if deviceLimit <= 0 {
		deviceLimit = store.MaxIOSPairingDevices
	}
	return iosPairingStatusResponse{
		Allowed:             status.Allowed,
		Availability:        availability,
		HasCredential:       status.HasCredential,
		CredentialCreatedAt: status.CredentialCreatedAt,
		Paired:              status.Paired,
		PairedAt:            status.PairedAt,
		Device:              iosPairingDeviceInfo(status.Device),
		LastSeenAt:          status.LastSeenAt,
		Devices:             devices,
		Pending:             pending,
		DeviceLimit:         deviceLimit,
		ServerURLs:          verifiedCloudflareAPIURLs(cloudflare),
	}
}

func iosPairingDeviceInfo(
	device mobilepairing.DeviceInfo,
) *mobilepairing.DeviceInfo {
	if device == (mobilepairing.DeviceInfo{}) {
		return nil
	}
	return &device
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
	case errors.Is(err, store.ErrIOSPairingDeviceLimit):
		writeError(
			response,
			http.StatusConflict,
			"ios_pairing_device_limit",
			"Revoke an Apple device before pairing another one",
			"",
		)
	case errors.Is(err, store.ErrIOSPairingCredentialNotFound):
		writeError(
			response,
			http.StatusNotFound,
			"ios_pairing_credential_not_found",
			"The paired Apple device was not found",
			"credential_id",
		)
	default:
		api.writeInternalError(response, request, operation, err)
	}
}
