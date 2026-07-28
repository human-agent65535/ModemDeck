package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestControlLeaseUsesStableControllerHeader(t *testing.T) {
	t.Parallel()
	var controllerID string
	expiresAt := time.Date(2026, 7, 28, 12, 0, 5, 0, time.UTC)
	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		current := request.Header.Get("X-ModemDeck-Controller")
		if current == "" {
			t.Error("controller header is empty")
		}
		if controllerID == "" {
			controllerID = current
		} else if current != controllerID {
			t.Errorf("controller header changed from %q to %q", controllerID, current)
		}
		switch {
		case request.Method == http.MethodPut && request.URL.Path == "/v1/control-lease":
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(ControlLeaseStatus{
				ControllerID: current,
				ExpiresAt:    expiresAt,
			})
		case request.Method == http.MethodPost && request.URL.Path == "/v1/calls":
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"request_id":"request-1","resource_id":"call-1"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/v1/control-lease":
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))

	status, err := client.RenewControlLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.ControllerID != controllerID || !status.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("RenewControlLease() = %+v", status)
	}
	if _, err := client.StartCall(context.Background(), StartCallRequest{
		RequestID: "request-1",
		LineID:    "line-1",
		Number:    "+818012345678",
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.ReleaseControlLease(context.Background()); err != nil {
		t.Fatal(err)
	}
}
