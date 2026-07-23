package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
)

type fakeLineServices struct {
	simStatus      agentclient.SIMStatus
	simRequest     agentclient.SIMCommandRequest
	profiles       []agentclient.ConnectionProfile
	profileRequest agentclient.SaveConnectionProfileRequest
	deleteRequest  agentclient.DeleteConnectionProfileRequest
	ussdStatus     agentclient.USSDStatus
	ussdRequest    agentclient.USSDRequest
}

func (service *fakeLineServices) SIMStatus(
	context.Context,
	string,
) (agentclient.SIMStatus, error) {
	return service.simStatus, nil
}

func (service *fakeLineServices) SIMCommand(
	_ context.Context,
	lineID string,
	request agentclient.SIMCommandRequest,
) (agentclient.CommandReceipt, error) {
	service.simRequest = request
	return agentclient.CommandReceipt{RequestID: request.RequestID, ResourceID: lineID}, nil
}

func (service *fakeLineServices) ConnectionProfiles(
	context.Context,
	string,
) ([]agentclient.ConnectionProfile, error) {
	return service.profiles, nil
}

func (service *fakeLineServices) SaveConnectionProfile(
	_ context.Context,
	_ string,
	request agentclient.SaveConnectionProfileRequest,
) (agentclient.ConnectionProfile, error) {
	service.profileRequest = request
	return agentclient.ConnectionProfile{
		ProfileID:   4,
		ProfileName: request.ProfileName,
		APN:         request.APN,
	}, nil
}

func (service *fakeLineServices) DeleteConnectionProfile(
	_ context.Context,
	lineID string,
	request agentclient.DeleteConnectionProfileRequest,
) (agentclient.CommandReceipt, error) {
	service.deleteRequest = request
	return agentclient.CommandReceipt{RequestID: request.RequestID, ResourceID: lineID}, nil
}

func (service *fakeLineServices) USSDStatus(
	context.Context,
	string,
) (agentclient.USSDStatus, error) {
	return service.ussdStatus, nil
}

func (service *fakeLineServices) USSDCommand(
	_ context.Context,
	_ string,
	request agentclient.USSDRequest,
) (agentclient.USSDResponse, error) {
	service.ussdRequest = request
	return agentclient.USSDResponse{Response: "balance"}, nil
}

func TestLineServiceRoutesExposeTypedOperationsWithoutLoggingSecrets(t *testing.T) {
	observedAt := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	service := &fakeLineServices{
		simStatus: agentclient.SIMStatus{
			LineID:     "line-1",
			Present:    true,
			ObservedAt: observedAt,
		},
		profiles: []agentclient.ConnectionProfile{{
			ProfileID:   1,
			ProfileName: "ims",
			APN:         "ims",
		}},
		ussdStatus: agentclient.USSDStatus{
			LineID:     "line-1",
			State:      "idle",
			ObservedAt: observedAt,
		},
	}
	buffer := diagnostics.NewLogBuffer(32)
	api, err := New(&fakeRepository{}, Options{
		LineServices:          service,
		Logger:                slog.New(buffer.Handler(slog.NewTextHandler(io.Discard, nil))),
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	assertRequestStatus(t, api, http.MethodGet, "/api/v1/devices/line-1/sim", "", http.StatusOK)
	assertRequestStatus(
		t,
		api,
		http.MethodPost,
		"/api/v1/devices/line-1/sim/commands",
		`{"request_id":"sim-1","operation":"send_pin","pin":"1234"}`,
		http.StatusOK,
	)
	if service.simRequest.PIN != "1234" {
		t.Fatalf("SIM command = %+v", service.simRequest)
	}
	assertRequestStatus(t, api, http.MethodGet, "/api/v1/devices/line-1/profiles", "", http.StatusOK)
	assertRequestStatus(
		t,
		api,
		http.MethodPut,
		"/api/v1/devices/line-1/profiles",
		`{"request_id":"profile-1","profile_name":"data","apn":"internet","password":"profile-secret"}`,
		http.StatusOK,
	)
	if service.profileRequest.Password != "profile-secret" {
		t.Fatalf("profile request = %+v", service.profileRequest)
	}
	assertRequestStatus(t, api, http.MethodGet, "/api/v1/devices/line-1/ussd", "", http.StatusOK)
	assertRequestStatus(
		t,
		api,
		http.MethodPost,
		"/api/v1/devices/line-1/ussd",
		`{"request_id":"ussd-1","action":"initiate","command":"*123#"}`,
		http.StatusOK,
	)
	if service.ussdRequest.Command != "*123#" {
		t.Fatalf("USSD request = %+v", service.ussdRequest)
	}

	encoded, err := json.Marshal(buffer.Snapshot(0))
	if err != nil {
		t.Fatalf("marshal logs: %v", err)
	}
	for _, secret := range []string{"1234", "profile-secret", "*123#"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("diagnostic logs contain secret %q: %s", secret, encoded)
		}
	}
}

func assertRequestStatus(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	status int,
) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s status = %d; body = %s", method, path, response.Code, response.Body.String())
	}
}
