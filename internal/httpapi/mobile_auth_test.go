package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

func TestMobileBearerAllowsCallAPIWithoutCSRF(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls",
		bytes.NewBufferString(`{}`),
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(
		t,
		response,
		http.StatusServiceUnavailable,
		"communications_unavailable",
	)
}

func TestMobileBearerCannotAccessWebSettingsAPI(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(&fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
		},
	}, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/settings/system",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(
		t,
		response,
		http.StatusForbidden,
		"mobile_api_forbidden",
	)
}

func TestRevokedMobileBearerIsRejected(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(
		&fakeRepository{},
		Options{disableAuthentication: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/bootstrap",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(
		t,
		response,
		http.StatusUnauthorized,
		"authentication_required",
	)
}
