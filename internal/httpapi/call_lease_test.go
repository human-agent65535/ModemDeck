package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllease"
)

type fakeCallLeases struct {
	callID   string
	holderID string
	status   calllease.Status
	err      error
}

func (leases *fakeCallLeases) Renew(
	_ context.Context,
	callID string,
	holderID string,
) (calllease.Status, error) {
	leases.callID = callID
	leases.holderID = holderID
	return leases.status, leases.err
}

func TestRenewCallLease(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, time.July, 28, 12, 0, 15, 0, time.UTC)
	leases := &fakeCallLeases{status: calllease.Status{
		CallID:    "call-1",
		HolderID:  "browser-1",
		ExpiresAt: expiresAt,
	}}
	api, err := New(&fakeRepository{}, Options{
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/lease",
		bytes.NewBufferString(`{"holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if leases.callID != "call-1" || leases.holderID != "browser-1" {
		t.Fatalf("renewed lease = call %q holder %q", leases.callID, leases.holderID)
	}
}

func TestRenewCallLeaseRequiresConfiguredService(t *testing.T) {
	t.Parallel()
	api, err := New(
		&fakeRepository{},
		Options{disableAuthentication: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/lease",
		bytes.NewBufferString(`{"holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
