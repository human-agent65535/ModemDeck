package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeLineServices struct {
	simStatus      domain.SIMStatus
	simRequest     domain.SIMCommandRequest
	profiles       []domain.ConnectionProfile
	saveRequest    domain.SaveConnectionProfileRequest
	deleteRequest  domain.DeleteConnectionProfileRequest
	ussdStatus     domain.USSDStatus
	ussdRequest    domain.USSDRequest
	operationError error
}

func (service *fakeLineServices) SIMStatus(context.Context, string) (domain.SIMStatus, error) {
	return service.simStatus, service.operationError
}

func (service *fakeLineServices) SIMCommand(
	_ context.Context,
	request domain.SIMCommandRequest,
) (domain.CommandReceipt, error) {
	service.simRequest = request
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: request.LineID}, service.operationError
}

func (service *fakeLineServices) ConnectionProfiles(
	context.Context,
	string,
) ([]domain.ConnectionProfile, error) {
	return service.profiles, service.operationError
}

func (service *fakeLineServices) SaveConnectionProfile(
	_ context.Context,
	request domain.SaveConnectionProfileRequest,
) (domain.ConnectionProfile, error) {
	service.saveRequest = request
	return domain.ConnectionProfile{ProfileID: 2, ProfileName: request.ProfileName}, service.operationError
}

func (service *fakeLineServices) DeleteConnectionProfile(
	_ context.Context,
	request domain.DeleteConnectionProfileRequest,
) (domain.CommandReceipt, error) {
	service.deleteRequest = request
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: request.LineID}, service.operationError
}

func (service *fakeLineServices) USSDStatus(context.Context, string) (domain.USSDStatus, error) {
	return service.ussdStatus, service.operationError
}

func (service *fakeLineServices) USSDCommand(
	_ context.Context,
	request domain.USSDRequest,
) (domain.USSDResponse, error) {
	service.ussdRequest = request
	return domain.USSDResponse{Response: "accepted"}, service.operationError
}

func TestLineServiceRoutesForwardTypedRequestsWithoutEchoingSecrets(t *testing.T) {
	t.Parallel()
	const rawEID = "89049032000000000000000012345678"
	lineServices := &fakeLineServices{
		simStatus: domain.SIMStatus{
			LineID:              "line-1",
			Present:             true,
			UnlockRequired:      "sim-pin",
			SIMType:             domain.SIMTypeESIM,
			ESIMStatus:          domain.ESIMStatusWithProfiles,
			EIDMasked:           "****5678",
			SIMSlotsKnown:       true,
			PrimarySIMSlot:      1,
			PrimarySIMSlotKnown: true,
			CurrentSIMSlot:      1,
			CurrentSIMSlotKnown: true,
			SIMSlots: []domain.SIMSlot{{
				Index:      1,
				Present:    true,
				Current:    true,
				SIMType:    domain.SIMTypeESIM,
				ESIMStatus: domain.ESIMStatusWithProfiles,
				EIDMasked:  "****5678",
			}},
			ProfileManagement: domain.SIMProfileManagementCapability{
				Supported: false,
				Reason:    "ModemManager does not expose eUICC profile management",
			},
		},
		profiles: []domain.ConnectionProfile{{ProfileID: 2, ProfileName: "ims", APN: "ims"}},
		ussdStatus: domain.USSDStatus{
			LineID: "line-1",
			State:  "idle",
		},
	}
	handler := NewWithOptions(&fakeProvider{}, "test", Options{LineServices: lineServices})

	sim := performRequest(handler, http.MethodGet, "/v1/lines/line-1/sim", nil)
	if sim.Code != http.StatusOK ||
		!bytes.Contains(sim.Body.Bytes(), []byte(`"unlock_required":"sim-pin"`)) ||
		!bytes.Contains(sim.Body.Bytes(), []byte(`"sim_type":"esim"`)) ||
		!bytes.Contains(sim.Body.Bytes(), []byte(`"esim_status":"with_profiles"`)) ||
		!bytes.Contains(sim.Body.Bytes(), []byte(`"eid":"****5678"`)) ||
		!bytes.Contains(sim.Body.Bytes(), []byte(`"profile_management":{"supported":false`)) {
		t.Fatalf("SIM response = %d %s", sim.Code, sim.Body.String())
	}
	if bytes.Contains(sim.Body.Bytes(), []byte(rawEID)) ||
		bytes.Contains(sim.Body.Bytes(), []byte("/org/freedesktop/ModemManager1/SIM/")) {
		t.Fatalf("SIM response exposed a raw hardware identifier: %s", sim.Body.String())
	}

	pin := performRequest(
		handler,
		http.MethodPost,
		"/v1/lines/line-1/sim/commands",
		[]byte(`{"request_id":"pin-1","operation":"send_pin","pin":"1234"}`),
	)
	if pin.Code != http.StatusOK {
		t.Fatalf("PIN response = %d %s", pin.Code, pin.Body.String())
	}
	if lineServices.simRequest.LineID != "line-1" || lineServices.simRequest.PIN != "1234" {
		t.Fatalf("SIM request = %+v", lineServices.simRequest)
	}
	if bytes.Contains(pin.Body.Bytes(), []byte("1234")) {
		t.Fatalf("PIN response echoed secret: %s", pin.Body.String())
	}

	profiles := performRequest(handler, http.MethodGet, "/v1/lines/line-1/profiles", nil)
	if profiles.Code != http.StatusOK || !bytes.Contains(profiles.Body.Bytes(), []byte(`"profile_name":"ims"`)) {
		t.Fatalf("profiles response = %d %s", profiles.Code, profiles.Body.String())
	}

	ussd := performRequest(
		handler,
		http.MethodPost,
		"/v1/lines/line-1/ussd",
		[]byte(`{"request_id":"ussd-1","action":"initiate","command":"*123#"}`),
	)
	if ussd.Code != http.StatusOK || lineServices.ussdRequest.LineID != "line-1" {
		t.Fatalf("USSD response = %d %s; request = %+v", ussd.Code, ussd.Body.String(), lineServices.ussdRequest)
	}
}
