package ota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

func TestUpdaterHTTPBoundaryRequiresBearerTokenExceptForHealth(t *testing.T) {
	t.Parallel()
	controller, err := New(Options{
		Checker: fakeChecker{result: updatecheck.Result{
			Status:         updatecheck.StatusUpToDate,
			CurrentVersion: "v1.9.3",
			CheckedAt:      "2026-08-02T00:00:00Z",
		}},
		Manifests:  fakeManifestLoader{},
		Digests:    fakeResolver{},
		Runtime:    &fakeRuntime{},
		Operations: &memoryOperationStore{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler, err := NewHandler(controller, "test-token")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d; body = %s", health.Code, health.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(
		unauthorized,
		httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil),
	)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer test-token")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d; body = %s", authorized.Code, authorized.Body.String())
	}
}

func TestUpdaterHTTPApplyRejectsUnknownInput(t *testing.T) {
	t.Parallel()
	controller, err := New(Options{
		Checker:    fakeChecker{},
		Manifests:  fakeManifestLoader{},
		Digests:    fakeResolver{},
		Runtime:    &fakeRuntime{},
		Operations: &memoryOperationStore{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler, err := NewHandler(controller, "test-token")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/updates/apply",
		strings.NewReader(`{"version":"v1.9.3","unexpected":true}`),
	)
	request = request.WithContext(context.Background())
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestUpdaterHTTPForwardsManualRefresh(t *testing.T) {
	t.Parallel()
	refreshed := false
	controller, err := New(Options{
		Checker: fakeChecker{
			result: updatecheck.Result{Status: updatecheck.StatusUpToDate},
			observe: func(requested bool) {
				refreshed = requested
			},
		},
		Manifests:  fakeManifestLoader{},
		Digests:    fakeResolver{},
		Runtime:    &fakeRuntime{},
		Operations: &memoryOperationStore{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler, err := NewHandler(controller, "test-token")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/updates/check?refresh=1", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !refreshed {
		t.Fatalf("status = %d; refreshed = %t", response.Code, refreshed)
	}
}
