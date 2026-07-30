package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

type fakeUpdateChecker struct {
	result updatecheck.Result
}

func (checker fakeUpdateChecker) Check(context.Context) updatecheck.Result {
	return checker.result
}

func TestAboutReportsProductMetadata(t *testing.T) {
	t.Parallel()
	api, err := New(&fakeRepository{}, Options{
		ApplicationVersion:    "v1.0.0",
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/about", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body aboutResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode about response: %v", err)
	}
	if body.Name != "ModemDeck" || body.Version != "v1.0.0" {
		t.Fatalf("identity = %q %q", body.Name, body.Version)
	}
	if body.LicenseName != "PolyForm Noncommercial 1.0.0" || body.NoticesURL == "" {
		t.Fatalf("legal metadata = %+v", body)
	}
}

func TestVersionReportsPublicUncachedBuildIdentity(t *testing.T) {
	t.Parallel()
	authenticator, _, _ := newAPIAuthenticator(t)
	api, err := New(&fakeRepository{}, Options{
		Authenticator:      authenticator,
		ApplicationVersion: "v1.8.6",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if cache := response.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cache)
	}
	if cache := response.Header().Get("Cloudflare-CDN-Cache-Control"); cache != "no-store" {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q, want no-store", cache)
	}
	var body versionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if body.Version != "v1.8.6" {
		t.Fatalf("version = %+v", body)
	}
}

func TestUpdateCheckReturnsCheckerResult(t *testing.T) {
	t.Parallel()
	expected := updatecheck.Result{
		Status:         updatecheck.StatusUpdateAvailable,
		CurrentVersion: "v1.0.0",
		LatestVersion:  "v1.1.0",
		CheckedAt:      "2026-07-28T08:00:00Z",
	}
	api, err := New(&fakeRepository{}, Options{
		UpdateChecker:         fakeUpdateChecker{result: expected},
		ApplicationVersion:    "v1.0.0",
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/updates/check", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body updatecheck.Result
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if body != expected {
		t.Fatalf("result = %+v; want %+v", body, expected)
	}
}

func TestAboutAndUpdateCheckAreGetOnly(t *testing.T) {
	t.Parallel()
	api, err := New(&fakeRepository{}, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, path := range []string{
		"/api/v1/version",
		"/api/v1/about",
		"/api/v1/updates/check",
	} {
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d", path, response.Code)
		}
		if response.Header().Get("Allow") != http.MethodGet {
			t.Errorf("%s Allow = %q", path, response.Header().Get("Allow"))
		}
	}
}
