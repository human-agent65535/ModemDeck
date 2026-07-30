package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeMobilePairingRepository struct {
	*fakeRepository
	pairing       store.IOSPairingStatus
	rotatedUserID string
	rotatedDigest mobilepairing.TokenDigest
	revokedUserID string
	pairingError  error
	rotateError   error
	revokeError   error
}

func (repository *fakeMobilePairingRepository) IOSPairingStatus(
	context.Context,
	string,
) (store.IOSPairingStatus, error) {
	return repository.pairing, repository.pairingError
}

func (repository *fakeMobilePairingRepository) RotateIOSPairingCredential(
	_ context.Context,
	userID string,
	digest mobilepairing.TokenDigest,
) (store.IOSPairingStatus, error) {
	repository.rotatedUserID = userID
	repository.rotatedDigest = digest
	return repository.pairing, repository.rotateError
}

func (repository *fakeMobilePairingRepository) RevokeIOSPairingCredential(
	_ context.Context,
	userID string,
) error {
	repository.revokedUserID = userID
	return repository.revokeError
}

type fakeMobilePairingAvailability struct {
	status mobilepairing.CloudflareStatus
}

func (availability fakeMobilePairingAvailability) Status(
	context.Context,
) mobilepairing.CloudflareStatus {
	return availability.status
}

func TestIOSPairingStatusReportsInstallationAndConnectorState(t *testing.T) {
	t.Parallel()

	repository := &fakeMobilePairingRepository{
		fakeRepository: &fakeRepository{},
		pairing: store.IOSPairingStatus{
			Allowed: true,
		},
	}
	api, err := New(repository, Options{
		disableAuthentication: true,
		MobilePairing: fakeMobilePairingAvailability{status: mobilepairing.CloudflareStatus{
			Enabled:   true,
			Connected: false,
			PublicURL: "https://phone.example.com",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/mobile/pairing", nil),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body iosPairingResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Pairing.Allowed ||
		!body.Pairing.Cloudflare.Enabled ||
		body.Pairing.Cloudflare.Connected ||
		body.Pairing.Cloudflare.PublicURL != "https://phone.example.com" {
		t.Fatalf("pairing = %+v", body.Pairing)
	}
}

func TestIOSPairingCreatesOnlyCurrentUsersCredentialThroughCloudflare(
	t *testing.T,
) {
	t.Parallel()

	repository := &fakeMobilePairingRepository{
		fakeRepository: &fakeRepository{},
		pairing: store.IOSPairingStatus{
			Allowed:             true,
			HasCredential:       true,
			CredentialCreatedAt: "2026-07-30 12:00:00",
		},
	}
	const publicURL = "https://phone.example.com"
	api, err := New(repository, Options{
		disableAuthentication: true,
		MobilePairing: fakeMobilePairingAvailability{status: mobilepairing.CloudflareStatus{
			Enabled:   true,
			Connected: true,
			PublicURL: publicURL,
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	principal := auth.Principal{
		UserID:            "user_member",
		Role:              auth.RoleMember,
		IOSPairingEnabled: true,
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/mobile/pairing",
		nil,
	)
	request = request.WithContext(
		auth.ContextWithPrincipal(request.Context(), principal),
	)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body iosPairingResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if repository.rotatedUserID != principal.UserID {
		t.Fatalf("rotated user = %q", repository.rotatedUserID)
	}
	if body.Payload == nil ||
		body.Payload.ServerURL != publicURL ||
		!strings.HasPrefix(string(body.Payload.Token), mobilepairing.TokenPrefix) {
		t.Fatalf("payload = %+v", body.Payload)
	}
	digest, err := mobilepairing.Digest(body.Payload.Token)
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if digest != repository.rotatedDigest {
		t.Fatalf("stored digest = %x, want %x", repository.rotatedDigest, digest)
	}
	if strings.Contains(response.Body.String(), "expires") {
		t.Fatalf("response unexpectedly contains expiry: %s", response.Body.String())
	}
}

func TestIOSPairingRequiresEnabledAndConnectedCloudflare(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		status     mobilepairing.CloudflareStatus
		wantStatus int
		wantCode   string
	}{
		{
			name:       "not installed",
			wantStatus: http.StatusConflict,
			wantCode:   "cloudflare_required",
		},
		{
			name: "connector down",
			status: mobilepairing.CloudflareStatus{
				Enabled:   true,
				Connected: false,
				PublicURL: "https://phone.example.com",
			},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "cloudflare_unavailable",
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repository := &fakeMobilePairingRepository{
				fakeRepository: &fakeRepository{},
				pairing: store.IOSPairingStatus{
					Allowed: true,
				},
			}
			api, err := New(repository, Options{
				disableAuthentication: true,
				MobilePairing: fakeMobilePairingAvailability{
					status: testCase.status,
				},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := httptest.NewRecorder()
			api.ServeHTTP(
				response,
				httptest.NewRequest(
					http.MethodPost,
					"/api/v1/mobile/pairing",
					nil,
				),
			)
			assertAPIError(
				t,
				response,
				testCase.wantStatus,
				testCase.wantCode,
			)
			if repository.rotatedUserID != "" {
				t.Fatalf("credential rotated for %q", repository.rotatedUserID)
			}
		})
	}
}

func TestIOSPairingCanBeRevokedWhileCloudflareIsDisabled(t *testing.T) {
	t.Parallel()

	repository := &fakeMobilePairingRepository{
		fakeRepository: &fakeRepository{},
		pairing: store.IOSPairingStatus{
			Allowed:       true,
			HasCredential: true,
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodDelete, "/api/v1/mobile/pairing", nil),
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.revokedUserID != auth.InitialAdminUserID {
		t.Fatalf("revoked user = %q", repository.revokedUserID)
	}
}
