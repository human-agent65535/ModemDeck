package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type fakeTLSSettings struct {
	status            TLSSettingsStatus
	certificatePEM    []byte
	privateKeyPEM     []byte
	installError      error
	automaticError    error
	automaticSelected bool
}

func (settings *fakeTLSSettings) Status() TLSSettingsStatus {
	return settings.status
}

func (settings *fakeTLSSettings) InstallUser(
	certificatePEM, privateKeyPEM []byte,
) (TLSSettingsStatus, error) {
	settings.certificatePEM = append([]byte(nil), certificatePEM...)
	settings.privateKeyPEM = append([]byte(nil), privateKeyPEM...)
	return settings.status, settings.installError
}

func (settings *fakeTLSSettings) UseAutomatic() (TLSSettingsStatus, error) {
	settings.automaticSelected = true
	return settings.status, settings.automaticError
}

func TestTLSSettingsReturnsCertificateMetadata(t *testing.T) {
	t.Parallel()

	settings := &fakeTLSSettings{status: testTLSStatus()}
	api := newTLSSettingsAPI(t, settings)
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/settings/tls", nil),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var envelope tlsSettingsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(envelope.TLS, settings.status) {
		t.Fatalf("status = %+v; want %+v", envelope.TLS, settings.status)
	}
	if strings.Contains(response.Body.String(), "PRIVATE KEY") {
		t.Fatal("response exposed private key material")
	}
}

func TestTLSSettingsInstallsUserCertificate(t *testing.T) {
	t.Parallel()

	settings := &fakeTLSSettings{status: testTLSStatus()}
	api := newTLSSettingsAPI(t, settings)
	body := bytes.NewBufferString(`{
		"operation":"install_user",
		"certificate_pem":"-----BEGIN CERTIFICATE-----\ncertificate\n-----END CERTIFICATE-----",
		"private_key_pem":"-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"
	}`)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/tls", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(settings.certificatePEM, []byte("certificate")) ||
		!bytes.Contains(settings.privateKeyPEM, []byte("secret")) {
		t.Fatalf(
			"install input = certificate %q, private key %q",
			settings.certificatePEM,
			settings.privateKeyPEM,
		)
	}
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatal("response exposed private key material")
	}
}

func TestTLSSettingsSelectsAutomaticCertificate(t *testing.T) {
	t.Parallel()

	settings := &fakeTLSSettings{status: testTLSStatus()}
	api := newTLSSettingsAPI(t, settings)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/tls",
		bytes.NewBufferString(`{"operation":"use_automatic"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !settings.automaticSelected {
		t.Fatalf(
			"status = %d, selected = %t; body = %s",
			response.Code,
			settings.automaticSelected,
			response.Body.String(),
		)
	}
}

func TestTLSSettingsRejectsInvalidInputWithoutEchoingIt(t *testing.T) {
	t.Parallel()

	settings := &fakeTLSSettings{
		status:       testTLSStatus(),
		installError: fmtError(ErrTLSSettingsInvalidInput),
	}
	api := newTLSSettingsAPI(t, settings)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/tls",
		bytes.NewBufferString(`{
			"operation":"install_user",
			"certificate_pem":"invalid certificate",
			"private_key_pem":"private secret"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"invalid_tls_certificate"`) ||
		strings.Contains(response.Body.String(), "private secret") {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestTLSSettingsValidatesOperationAndFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		field string
	}{
		{name: "unknown operation", body: `{"operation":"rotate"}`, field: "operation"},
		{
			name:  "missing certificate",
			body:  `{"operation":"install_user","private_key_pem":"key"}`,
			field: "certificate_pem",
		},
		{
			name:  "missing key",
			body:  `{"operation":"install_user","certificate_pem":"certificate"}`,
			field: "private_key_pem",
		},
		{
			name: "automatic with certificate",
			body: `{"operation":"use_automatic","certificate_pem":"certificate"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			api := newTLSSettingsAPI(t, &fakeTLSSettings{status: testTLSStatus()})
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/v1/settings/tls",
				bytes.NewBufferString(test.body),
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
			}
			if test.field != "" &&
				!strings.Contains(response.Body.String(), `"field":"`+test.field+`"`) {
				t.Fatalf("body = %s", response.Body.String())
			}
		})
	}
}

func TestTLSSettingsUnavailableAndMethodBoundary(t *testing.T) {
	t.Parallel()

	api := newTLSSettingsAPI(t, nil)
	unavailable := httptest.NewRecorder()
	api.ServeHTTP(
		unavailable,
		httptest.NewRequest(http.MethodGet, "/api/v1/settings/tls", nil),
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", unavailable.Code)
	}

	api = newTLSSettingsAPI(t, &fakeTLSSettings{status: testTLSStatus()})
	method := httptest.NewRecorder()
	api.ServeHTTP(
		method,
		httptest.NewRequest(http.MethodDelete, "/api/v1/settings/tls", nil),
	)
	if method.Code != http.StatusMethodNotAllowed ||
		method.Header().Get("Allow") != "GET, PUT" {
		t.Fatalf(
			"method status = %d, Allow = %q",
			method.Code,
			method.Header().Get("Allow"),
		)
	}
}

func newTLSSettingsAPI(t *testing.T, settings TLSSettingsService) *API {
	t.Helper()
	api, err := New(&fakeRepository{}, Options{
		TLSSettings:           settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return api
}

func testTLSStatus() TLSSettingsStatus {
	return TLSSettingsStatus{
		Mode:                "user",
		Subject:             "CN=modemdeck.example",
		Issuer:              "CN=Example CA",
		DNSNames:            []string{"modemdeck.example"},
		IPAddresses:         []string{"192.0.2.10"},
		NotBefore:           "2026-07-24T00:00:00Z",
		NotAfter:            "2027-07-24T00:00:00Z",
		FingerprintSHA256:   "AA:BB:CC",
		Expired:             false,
		RenewsAutomatically: false,
	}
}

func fmtError(target error) error {
	return errors.Join(errors.New("invalid certificate"), target)
}
