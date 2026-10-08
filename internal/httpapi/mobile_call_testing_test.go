package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/calltest"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

type localAudioTestPush chan string

func (p localAudioTestPush) SendAudioTestCall(_ context.Context, _, _, id string) error {
	p <- id
	return nil
}

func TestMobileAudioTestUsesSharedActiveAndLeaseContracts(t *testing.T) {
	push := make(localAudioTestPush, 1)
	service := calltest.New(push, nil)
	t.Cleanup(service.Close)
	repository := &fakeRepository{
		mobileFound:            true,
		mobilePrincipal:        auth.Principal{UserID: "user", Role: auth.RoleMember, IOSPairingEnabled: true},
		iosCurrentCredentialID: "phone",
	}
	api, err := New(repository, Options{CallTests: service, disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path, bearer string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, "/api/v1/mobile/call-tests"+path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("%s %s = %d: %s", method, path, response.Code, response.Body.String())
		}
		return response
	}
	send(http.MethodPost, "", "", http.StatusForbidden)
	response := send(http.MethodPost, "", string(token), http.StatusAccepted)
	var started calltest.Status
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-push:
		if id != started.ID {
			t.Fatalf("push ID = %q", id)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("local push was not scheduled")
	}
	path := "/" + started.ID
	send(http.MethodPost, path+"/answer", string(token), http.StatusOK)
	response = send(http.MethodGet, path+"/active", string(token), http.StatusOK)
	var active activeCallsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &active); err != nil {
		t.Fatal(err)
	}
	if len(active.Calls) != 1 || active.Calls[0].ID != started.ID || active.Calls[0].Phase != "active" ||
		active.Calls[0].ControlState != "owned" || !active.Calls[0].MediaAvailable || active.Reservations == nil {
		t.Fatalf("shared active response = %+v", active)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("call state can be cached")
	}
	response = send(http.MethodPut, path+"/lease", string(token), http.StatusOK)
	var lease calllease.Status
	if err := json.Unmarshal(response.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if lease.CallID != started.ID || !lease.ExpiresAt.After(time.Now()) {
		t.Fatalf("shared lease response = %+v", lease)
	}
	send(http.MethodGet, path+"/ice-servers", string(token), http.StatusNotFound)
	send(http.MethodPost, path+"/media", string(token), http.StatusMethodNotAllowed)
	send(http.MethodGet, path+"/lease", string(token), http.StatusMethodNotAllowed)
	send(http.MethodDelete, path+"/media", string(token), http.StatusBadRequest)
	repository.iosCurrentCredentialID = "other-phone"
	for _, action := range []string{"active", "lease", "hangup"} {
		method := map[string]string{"active": http.MethodGet, "lease": http.MethodPut, "hangup": http.MethodPost}[action]
		send(method, path+"/"+action, string(token), http.StatusNotFound)
	}
	repository.iosCurrentCredentialID = "phone"
	send(http.MethodPost, path+"/hangup", string(token), http.StatusNoContent)
	response = send(http.MethodGet, path+"/active", string(token), http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &active); err != nil {
		t.Fatal(err)
	}
	if active.Calls == nil || len(active.Calls) != 0 {
		t.Fatalf("ended call remains active: %+v", active)
	}
	send(http.MethodPut, path+"/lease", string(token), http.StatusConflict)
}
