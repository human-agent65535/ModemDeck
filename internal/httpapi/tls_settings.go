package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

const maxTLSSettingsBodyBytes = 2 << 20

var ErrTLSSettingsInvalidInput = errors.New("TLS settings input is invalid")

type TLSSettingsStatus struct {
	Mode                string   `json:"mode"`
	Subject             string   `json:"subject"`
	Issuer              string   `json:"issuer"`
	DNSNames            []string `json:"dns_names"`
	IPAddresses         []string `json:"ip_addresses"`
	NotBefore           string   `json:"not_before"`
	NotAfter            string   `json:"not_after"`
	FingerprintSHA256   string   `json:"fingerprint_sha256"`
	Expired             bool     `json:"expired"`
	RenewsAutomatically bool     `json:"renews_automatically"`
}

type TLSSettingsService interface {
	Status() TLSSettingsStatus
	InstallUser(certificatePEM, privateKeyPEM []byte) (TLSSettingsStatus, error)
	UseAutomatic() (TLSSettingsStatus, error)
}

type tlsSettingsResponse struct {
	TLS TLSSettingsStatus `json:"tls"`
}

type updateTLSSettingsRequest struct {
	Operation      string `json:"operation"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

func (api *API) tlsSettings(response http.ResponseWriter, request *http.Request) {
	if api.tlsSettingsService == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"tls_settings_unavailable",
			"TLS certificate settings are unavailable",
			"",
		)
		return
	}

	switch request.Method {
	case http.MethodGet:
		writeJSON(response, http.StatusOK, tlsSettingsResponse{
			TLS: api.tlsSettingsService.Status(),
		})
	case http.MethodPut:
		api.updateTLSSettings(response, request)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and PUT are supported",
			"",
		)
	}
}

func (api *API) updateTLSSettings(response http.ResponseWriter, request *http.Request) {
	var input updateTLSSettingsRequest
	if !decodeJSONBodyWithLimit(
		response,
		request,
		&input,
		maxTLSSettingsBodyBytes,
	) {
		return
	}

	var (
		status TLSSettingsStatus
		err    error
	)
	switch strings.TrimSpace(input.Operation) {
	case "install_user":
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
		status, err = api.tlsSettingsService.InstallUser(
			[]byte(input.CertificatePEM),
			[]byte(input.PrivateKeyPEM),
		)
	case "use_automatic":
		if input.CertificatePEM != "" || input.PrivateKeyPEM != "" {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"certificate_pem and private_key_pem must be omitted",
				"",
			)
			return
		}
		status, err = api.tlsSettingsService.UseAutomatic()
	default:
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"operation must be install_user or use_automatic",
			"operation",
		)
		return
	}
	if err != nil {
		if errors.Is(err, ErrTLSSettingsInvalidInput) {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_tls_certificate",
				"Certificate and private key must be a valid matching PEM pair",
				"certificate_pem",
			)
			return
		}
		api.writeInternalError(response, request, "update TLS certificate settings", err)
		return
	}
	writeJSON(response, http.StatusOK, tlsSettingsResponse{TLS: status})
}
