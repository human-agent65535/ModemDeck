package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type controlLeaseStub struct {
	controllerID string
	expiresAt    time.Time
	released     bool
}

func (stub *controlLeaseStub) Renew(
	controllerID string,
) (domain.ControlLeaseStatus, error) {
	stub.controllerID = controllerID
	return domain.ControlLeaseStatus{
		ControllerID: controllerID,
		ExpiresAt:    stub.expiresAt,
	}, nil
}

func (stub *controlLeaseStub) Require(controllerID string) error {
	if controllerID == "" || controllerID != stub.controllerID {
		return domain.FailedPrecondition(
			"require_control_lease",
			"application control lease is missing or expired",
			nil,
		)
	}
	return nil
}

func (stub *controlLeaseStub) Release(_ context.Context, controllerID string) error {
	if err := stub.Require(controllerID); err != nil {
		return err
	}
	stub.released = true
	return nil
}

func (stub *controlLeaseStub) Shutdown(context.Context) error {
	return nil
}

func TestControlLeaseEndpointAndCommandGate(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{}
	expiresAt := time.Date(2026, 7, 28, 12, 0, 5, 0, time.UTC)
	lease := &controlLeaseStub{expiresAt: expiresAt}
	handler := NewWithOptions(provider, "test", Options{ControlLease: lease})

	renew := requestWithController(
		handler,
		http.MethodPut,
		"/v1/control-lease",
		nil,
		"app-1",
	)
	if renew.Code != http.StatusOK {
		t.Fatalf("renew status = %d, body = %s", renew.Code, renew.Body.String())
	}
	var status domain.ControlLeaseStatus
	decodeResponse(t, renew, &status)
	if status.ControllerID != "app-1" || !status.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("renew status = %+v", status)
	}

	missing := requestWithController(
		handler,
		http.MethodPost,
		"/v1/calls",
		[]byte(`{"request_id":"request-1","line_id":"line-1","number":"+818012345678"}`),
		"",
	)
	if missing.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing lease status = %d, body = %s", missing.Code, missing.Body.String())
	}
	if len(provider.startRequests) != 0 {
		t.Fatalf("start requests = %+v", provider.startRequests)
	}

	start := requestWithController(
		handler,
		http.MethodPost,
		"/v1/calls",
		[]byte(`{"request_id":"request-1","line_id":"line-1","number":"+818012345678"}`),
		"app-1",
	)
	if start.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body = %s", start.Code, start.Body.String())
	}
	if len(provider.startRequests) != 1 {
		t.Fatalf("start requests = %+v", provider.startRequests)
	}

	hangup := requestWithController(
		handler,
		http.MethodPost,
		"/v1/calls/call-1/hangup",
		[]byte(`{"request_id":"request-2"}`),
		"",
	)
	if hangup.Code != http.StatusOK {
		t.Fatalf("hangup status = %d, body = %s", hangup.Code, hangup.Body.String())
	}

	release := requestWithController(
		handler,
		http.MethodDelete,
		"/v1/control-lease",
		nil,
		"app-1",
	)
	if release.Code != http.StatusNoContent || !lease.released {
		t.Fatalf(
			"release status = %d released = %v body = %s",
			release.Code,
			lease.released,
			release.Body.String(),
		)
	}
}

func TestHealthAdvertisesControlLease(t *testing.T) {
	t.Parallel()
	recorder := performRequest(
		NewWithOptions(
			&fakeProvider{},
			"test",
			Options{ControlLease: &controlLeaseStub{}},
		),
		http.MethodGet,
		"/v1/health",
		nil,
	)
	var response healthResponse
	decodeResponse(t, recorder, &response)
	if !response.Provider.Capabilities.ControlLease {
		t.Fatalf("capabilities = %+v", response.Provider.Capabilities)
	}
}

func requestWithController(
	handler http.Handler,
	method string,
	target string,
	body []byte,
	controllerID string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if controllerID != "" {
		request.Header.Set(domain.ControlLeaseHeader, controllerID)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
