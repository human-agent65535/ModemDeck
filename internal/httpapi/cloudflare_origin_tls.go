package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

const maxCloudflareOriginTLSBodyBytes = 3 << 20

var ErrCloudflareOriginTLSInvalidInput = errors.New(
	"Cloudflare origin TLS input is invalid",
)

var ErrCloudflareOriginTLSActivation = errors.New(
	"Cloudflare origin TLS activation failed",
)

var ErrCloudflareOriginTLSAlreadyEnabled = errors.New(
	"Cloudflare origin TLS is already enabled",
)

type CloudflareOriginTLSStatus struct {
	Enabled           bool     `json:"enabled"`
	CoversRoutes      bool     `json:"covers_routes"`
	Subject           string   `json:"subject"`
	Issuer            string   `json:"issuer"`
	DNSNames          []string `json:"dns_names"`
	NotBefore         string   `json:"not_before"`
	NotAfter          string   `json:"not_after"`
	FingerprintSHA256 string   `json:"fingerprint_sha256"`
	Expired           bool     `json:"expired"`
}

type CloudflareOriginTLSService interface {
	Status(context.Context) CloudflareOriginTLSStatus
	Install(
		context.Context,
		[]byte,
		[]byte,
	) (CloudflareOriginTLSStatus, error)
	Disable(context.Context) (CloudflareOriginTLSStatus, error)
}

type cloudflareOriginTLSResponse struct {
	OriginTLS CloudflareOriginTLSStatus `json:"origin_tls"`
}

type installCloudflareOriginTLSRequest struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

func (api *API) cloudflareOriginTLS(
	response http.ResponseWriter,
	request *http.Request,
) {
	if api.cloudflareOriginTLSService == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"cloudflare_origin_tls_unavailable",
			"Cloudflare origin TLS settings are unavailable",
			"",
		)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	switch request.Method {
	case http.MethodPut:
		api.installCloudflareOriginTLS(response, request)
	case http.MethodDelete:
		status, err := api.cloudflareOriginTLSService.Disable(request.Context())
		if err != nil {
			api.writeInternalError(
				response,
				request,
				"disable Cloudflare origin TLS",
				err,
			)
			return
		}
		writeJSON(response, http.StatusOK, cloudflareOriginTLSResponse{
			OriginTLS: status,
		})
	default:
		response.Header().Set("Allow", http.MethodPut+", "+http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only PUT and DELETE are supported",
			"",
		)
	}
}

func (api *API) installCloudflareOriginTLS(
	response http.ResponseWriter,
	request *http.Request,
) {
	var input installCloudflareOriginTLSRequest
	if !decodeJSONBodyWithLimit(
		response,
		request,
		&input,
		maxCloudflareOriginTLSBodyBytes,
	) {
		return
	}
	if strings.TrimSpace(input.CertificatePEM) == "" {
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"certificate_pem is required",
			"certificate_pem",
		)
		return
	}
	if strings.TrimSpace(input.PrivateKeyPEM) == "" {
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"private_key_pem is required",
			"private_key_pem",
		)
		return
	}
	status, err := api.cloudflareOriginTLSService.Install(
		request.Context(),
		[]byte(input.CertificatePEM),
		[]byte(input.PrivateKeyPEM),
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrCloudflareOriginTLSInvalidInput):
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_cloudflare_origin_certificate",
				"Certificate and private key must be a current matching Cloudflare Origin CA PEM pair covering the configured routes",
				"certificate_pem",
			)
			return
		case errors.Is(err, ErrCloudflareOriginTLSAlreadyEnabled):
			writeError(
				response,
				http.StatusConflict,
				"cloudflare_origin_tls_already_enabled",
				"Delete the installed Cloudflare Origin CA certificate before installing another one",
				"",
			)
			return
		case errors.Is(err, ErrCloudflareOriginTLSActivation):
			writeError(
				response,
				http.StatusServiceUnavailable,
				"cloudflare_origin_tls_activation_failed",
				"Origin HTTPS and HTTP/2 did not start; the certificate was not saved",
				"",
			)
			return
		}
		api.writeInternalError(
			response,
			request,
			"install Cloudflare origin TLS certificate",
			err,
		)
		return
	}
	writeJSON(response, http.StatusOK, cloudflareOriginTLSResponse{
		OriginTLS: status,
	})
}
