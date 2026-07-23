package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLineServiceContracts(t *testing.T) {
	var simCommand SIMCommandRequest
	var savedProfile SaveConnectionProfileRequest
	var deletedProfile DeleteConnectionProfileRequest
	var ussdCommand USSDRequest
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/lines/line-1/sim":
			_, _ = response.Write([]byte(`{"sim":{"line_id":"line-1","present":true,"active":true,"identifier":"8986","imsi":"44051","operator_identifier":"44051","operator_name":"KDDI","unlock_required":"none","unlock_required_code":1,"unlock_retries":{"sim-pin":3},"observed_at":"2026-07-23T00:00:00Z"}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/lines/line-1/sim/commands":
			if err := json.NewDecoder(request.Body).Decode(&simCommand); err != nil {
				t.Fatalf("decode SIM command: %v", err)
			}
			_, _ = response.Write([]byte(`{"receipt":{"request_id":"sim-1","resource_id":"line-1"}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/lines/line-1/profiles":
			_, _ = response.Write([]byte(`{"profiles":[{"profile_id":1,"profile_name":"ims","apn":"ims","ip_family":"ipv4v6","ip_type":3,"apn_type":2,"allowed_auth":0,"access_type_preference":0,"roaming_allowance":0,"profile_source":0}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/v1/lines/line-1/profiles":
			if err := json.NewDecoder(request.Body).Decode(&savedProfile); err != nil {
				t.Fatalf("decode saved profile: %v", err)
			}
			_, _ = response.Write([]byte(`{"profile":{"profile_id":2,"profile_name":"data","apn":"internet","ip_family":"ipv4","ip_type":1,"apn_type":1,"allowed_auth":0,"access_type_preference":0,"roaming_allowance":0,"profile_source":0}}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/v1/lines/line-1/profiles":
			if err := json.NewDecoder(request.Body).Decode(&deletedProfile); err != nil {
				t.Fatalf("decode deleted profile: %v", err)
			}
			_, _ = response.Write([]byte(`{"receipt":{"request_id":"profile-delete-1","resource_id":"line-1"}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/lines/line-1/ussd":
			_, _ = response.Write([]byte(`{"ussd":{"line_id":"line-1","state":"idle","state_code":1,"observed_at":"2026-07-23T00:00:00Z"}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/lines/line-1/ussd":
			if err := json.NewDecoder(request.Body).Decode(&ussdCommand); err != nil {
				t.Fatalf("decode USSD command: %v", err)
			}
			_, _ = response.Write([]byte(`{"result":{"response":"balance"}}`))
		default:
			http.NotFound(response, request)
		}
	}))

	sim, err := client.SIMStatus(context.Background(), "line-1")
	if err != nil || sim.LineID != "line-1" || sim.UnlockRetries["sim-pin"] != 3 {
		t.Fatalf("SIMStatus() = %+v, %v", sim, err)
	}
	receipt, err := client.SIMCommand(context.Background(), "line-1", SIMCommandRequest{
		RequestID: "sim-1",
		Operation: SIMSendPIN,
		PIN:       "1234",
	})
	if err != nil || receipt.ResourceID != "line-1" || simCommand.PIN != "1234" {
		t.Fatalf("SIMCommand() = %+v, %v; request = %+v", receipt, err, simCommand)
	}
	profiles, err := client.ConnectionProfiles(context.Background(), "line-1")
	if err != nil || len(profiles) != 1 || profiles[0].ProfileName != "ims" {
		t.Fatalf("ConnectionProfiles() = %+v, %v", profiles, err)
	}
	profile, err := client.SaveConnectionProfile(context.Background(), "line-1", SaveConnectionProfileRequest{
		RequestID: "profile-save-1",
		APN:       "internet",
		IPFamily:  "ipv4",
		Password:  "secret",
	})
	if err != nil || profile.ProfileID != 2 || savedProfile.Password != "secret" {
		t.Fatalf("SaveConnectionProfile() = %+v, %v; request = %+v", profile, err, savedProfile)
	}
	profileID := int32(2)
	receipt, err = client.DeleteConnectionProfile(context.Background(), "line-1", DeleteConnectionProfileRequest{
		RequestID: "profile-delete-1",
		ProfileID: &profileID,
	})
	if err != nil || receipt.RequestID != "profile-delete-1" || deletedProfile.ProfileID == nil {
		t.Fatalf("DeleteConnectionProfile() = %+v, %v; request = %+v", receipt, err, deletedProfile)
	}
	status, err := client.USSDStatus(context.Background(), "line-1")
	if err != nil || status.State != "idle" {
		t.Fatalf("USSDStatus() = %+v, %v", status, err)
	}
	result, err := client.USSDCommand(context.Background(), "line-1", USSDRequest{
		RequestID: "ussd-1",
		Action:    USSDInitiate,
		Command:   "*123#",
	})
	if err != nil || result.Response != "balance" || ussdCommand.Command != "*123#" {
		t.Fatalf("USSDCommand() = %+v, %v; request = %+v", result, err, ussdCommand)
	}
}
