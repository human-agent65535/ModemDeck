package updaterclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

func TestClientChecksAndStartsUpdate(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(response, "missing token", http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/updates/check":
			_, _ = response.Write([]byte(`{"status":"update_available","current_version":"v1.0.0","latest_version":"v1.1.0","checked_at":"2026-08-01T00:00:00Z","apply_available":true,"hardware_confirmation_required":false}`))
		case "/v1/updates/apply":
			_, _ = response.Write([]byte(`{"id":"operation-1","state":"running","target_version":"v1.1.0","started_at":"2026-08-01T00:01:00Z"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL:        server.URL,
		CurrentVersion: "v1.0.0",
		Token:          "test-token",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := client.Check(context.Background())
	if result.Status != updatecheck.StatusUpdateAvailable || !result.ApplyAvailable {
		t.Fatalf("check = %+v", result)
	}
	operation, err := client.Apply(context.Background(), updatecheck.ApplyRequest{Version: "v1.1.0"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if operation.ID != "operation-1" || operation.State != updatecheck.OperationRunning {
		t.Fatalf("operation = %+v", operation)
	}
}

func TestClientMapsUpdaterFailureToUnavailableResult(t *testing.T) {
	t.Parallel()
	client, err := New(Options{
		BaseURL:        "http://updater.invalid",
		CurrentVersion: "v1.0.0",
		Client: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := client.Check(context.Background())
	if result.Status != updatecheck.StatusUnavailable || result.ErrorCode != "updater_unavailable" {
		t.Fatalf("result = %+v", result)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}
