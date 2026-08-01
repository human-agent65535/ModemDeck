package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCloudflareOriginTLS struct {
	status         CloudflareOriginTLSStatus
	certificatePEM []byte
	privateKeyPEM  []byte
	installError   error
	disableError   error
	disabled       bool
}

func (settings *fakeCloudflareOriginTLS) Status(
	context.Context,
) CloudflareOriginTLSStatus {
	return settings.status
}

func (settings *fakeCloudflareOriginTLS) Install(
	_ context.Context,
	certificatePEM []byte,
	privateKeyPEM []byte,
) (CloudflareOriginTLSStatus, error) {
	settings.certificatePEM = append([]byte(nil), certificatePEM...)
	settings.privateKeyPEM = append([]byte(nil), privateKeyPEM...)
	return settings.status, settings.installError
}

func (settings *fakeCloudflareOriginTLS) Disable(
	context.Context,
) (CloudflareOriginTLSStatus, error) {
	settings.disabled = true
	return settings.status, settings.disableError
}

func TestCloudflareOriginTLSInstallsAndRemovesCertificate(t *testing.T) {
	t.Parallel()
	settings := &fakeCloudflareOriginTLS{status: testCloudflareOriginTLSStatus()}
	api := newCloudflareOriginTLSAPI(t, settings)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/external-access/origin-tls",
		bytes.NewBufferString(`{
			"certificate_pem":"-----BEGIN CERTIFICATE-----\ncertificate\n-----END CERTIFICATE-----",
			"private_key_pem":"-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("install status = %d; body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(settings.certificatePEM, []byte("certificate")) ||
		!bytes.Contains(settings.privateKeyPEM, []byte("secret")) ||
		strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("install response = %s", response.Body.String())
	}
	var envelope cloudflareOriginTLSResponse
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OriginTLS.Enabled || !envelope.OriginTLS.CoversRoutes {
		t.Fatalf("origin status = %+v", envelope.OriginTLS)
	}

	disable := httptest.NewRecorder()
	api.ServeHTTP(
		disable,
		httptest.NewRequest(
			http.MethodDelete,
			"/api/v1/external-access/origin-tls",
			nil,
		),
	)
	if disable.Code != http.StatusOK || !settings.disabled {
		t.Fatalf("disable status = %d, disabled = %t", disable.Code, settings.disabled)
	}
}

func TestCloudflareOriginTLSRejectsInvalidMaterial(t *testing.T) {
	t.Parallel()
	settings := &fakeCloudflareOriginTLS{
		installError: errors.Join(
			errors.New("wrong authority"),
			ErrCloudflareOriginTLSInvalidInput,
		),
	}
	api := newCloudflareOriginTLSAPI(t, settings)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/external-access/origin-tls",
		bytes.NewBufferString(`{
			"certificate_pem":"certificate",
			"private_key_pem":"private secret"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest ||
		!strings.Contains(
			response.Body.String(),
			`"code":"invalid_cloudflare_origin_certificate"`,
		) ||
		strings.Contains(response.Body.String(), "private secret") {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestCloudflareOriginTLSReportsActivationAndReplacementFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		err        error
		statusCode int
		code       string
	}{
		{
			name:       "activation",
			err:        ErrCloudflareOriginTLSActivation,
			statusCode: http.StatusServiceUnavailable,
			code:       "cloudflare_origin_tls_activation_failed",
		},
		{
			name:       "replacement",
			err:        ErrCloudflareOriginTLSAlreadyEnabled,
			statusCode: http.StatusConflict,
			code:       "cloudflare_origin_tls_already_enabled",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			api := newCloudflareOriginTLSAPI(t, &fakeCloudflareOriginTLS{
				installError: test.err,
			})
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/v1/external-access/origin-tls",
				bytes.NewBufferString(`{
					"certificate_pem":"certificate",
					"private_key_pem":"private secret"
				}`),
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			if response.Code != test.statusCode ||
				!strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) ||
				strings.Contains(response.Body.String(), "private secret") {
				t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCloudflareOriginTLSValidatesFieldsAndMethods(t *testing.T) {
	t.Parallel()
	api := newCloudflareOriginTLSAPI(t, &fakeCloudflareOriginTLS{})
	for _, body := range []string{
		`{"private_key_pem":"key"}`,
		`{"certificate_pem":"certificate"}`,
	} {
		request := httptest.NewRequest(
			http.MethodPut,
			"/api/v1/external-access/origin-tls",
			bytes.NewBufferString(body),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/external-access/origin-tls",
			nil,
		),
	)
	if response.Code != http.StatusMethodNotAllowed ||
		response.Header().Get("Allow") != "PUT, DELETE" {
		t.Fatalf(
			"status = %d, Allow = %q",
			response.Code,
			response.Header().Get("Allow"),
		)
	}
}

func newCloudflareOriginTLSAPI(
	t *testing.T,
	settings CloudflareOriginTLSService,
) *API {
	t.Helper()
	api, err := New(&fakeRepository{}, Options{
		CloudflareOriginTLS:   settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return api
}

func testCloudflareOriginTLSStatus() CloudflareOriginTLSStatus {
	return CloudflareOriginTLSStatus{
		Enabled:           true,
		CoversRoutes:      true,
		Subject:           "CN=*.example.com",
		Issuer:            "Cloudflare Origin SSL Certificate Authority",
		DNSNames:          []string{"*.example.com"},
		NotBefore:         "2026-08-01T00:00:00Z",
		NotAfter:          "2031-08-01T00:00:00Z",
		FingerprintSHA256: "AA:BB:CC",
	}
}
