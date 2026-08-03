package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

func TestMobileAPIAllowsAuthenticatedApplicationSurface(t *testing.T) {
	t.Parallel()

	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/mobile/session"},
		{http.MethodGet, "/api/v1/bootstrap"},
		{http.MethodPut, "/api/v1/account/contact"},
		{http.MethodGet, "/api/v1/contacts"},
		{http.MethodPost, "/api/v1/contacts"},
		{http.MethodPatch, "/api/v1/contacts/batch"},
		{http.MethodGet, "/api/v1/contacts/contact-1"},
		{http.MethodPut, "/api/v1/contacts/contact-1"},
		{http.MethodDelete, "/api/v1/contacts/contact-1"},
		{http.MethodGet, "/api/v1/messages/threads"},
		{http.MethodDelete, "/api/v1/messages/threads"},
		{http.MethodGet, "/api/v1/messages"},
		{http.MethodPost, "/api/v1/messages"},
		{http.MethodPatch, "/api/v1/messages/read"},
		{http.MethodPatch, "/api/v1/messages/threads/state"},
		{http.MethodGet, "/api/v1/messages/events"},
		{http.MethodGet, "/api/v1/runtime/events"},
		{http.MethodGet, "/api/v1/calls"},
		{http.MethodPost, "/api/v1/calls"},
		{http.MethodPatch, "/api/v1/calls/missed/read"},
		{http.MethodPatch, "/api/v1/calls/batch"},
		{http.MethodGet, "/api/v1/calls/active"},
		{http.MethodPatch, "/api/v1/calls/call-1/read"},
		{http.MethodDelete, "/api/v1/calls/call-1"},
		{http.MethodPut, "/api/v1/calls/call-1/lease"},
		{http.MethodPost, "/api/v1/calls/call-1/hangup"},
		{http.MethodPost, "/api/v1/calls/call-1/media/ice"},
		{http.MethodPost, "/api/v1/calls/call-1/media"},
		{http.MethodDelete, "/api/v1/calls/call-1/media"},
		{http.MethodGet, "/api/v1/recordings"},
		{http.MethodPatch, "/api/v1/recordings/batch"},
		{http.MethodPut, "/api/v1/calls/call-1/recording"},
		{http.MethodGet, "/api/v1/calls/call-1/recordings"},
		{http.MethodDelete, "/api/v1/calls/call-1/recordings/segment-1"},
		{http.MethodGet, "/api/v1/calls/call-1/recordings/segment-1/download"},
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodGet, "/api/v1/settings/calls"},
		{http.MethodPatch, "/api/v1/settings/calls"},
		{http.MethodPatch, "/api/v1/settings/lines"},
		{http.MethodPatch, "/api/v1/settings/system"},
		{http.MethodGet, "/api/v1/settings/recording"},
		{http.MethodPut, "/api/v1/settings/recording"},
		{http.MethodDelete, "/api/v1/mobile/pairing"},
		{http.MethodGet, "/api/v1/mobile/pairing"},
		{http.MethodPut, "/api/v1/mobile/push"},
		{http.MethodDelete, "/api/v1/mobile/push"},
		{http.MethodGet, "/api/v1/account/sessions"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/diagnostics"},
		{http.MethodGet, "/api/v1/settings/telegram"},
	}
	for _, testCase := range allowed {
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		if !mobileAPIRequestAllowed(request) {
			t.Errorf("%s %s was not allowed", testCase.method, testCase.path)
		}
	}

	forbidden := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/mobile/pairing"},
		{http.MethodPost, "/api/v1/setup"},
		{http.MethodPost, "/api/v1/login"},
		{http.MethodGet, "/api/v1/session"},
		{http.MethodGet, "/healthz"},
	}
	for _, testCase := range forbidden {
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		if mobileAPIRequestAllowed(request) {
			t.Errorf("%s %s was unexpectedly allowed", testCase.method, testCase.path)
		}
	}
}

func TestMobileBearerReturnsPrincipalSession(t *testing.T) {
	t.Parallel()

	token, digest, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Username:          "phone-user",
			Role:              auth.RoleMember,
			ProfileContactID:  "contact-1",
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mobile/session", nil)
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set(
		mobileDeviceNameHeader,
		"b64:"+base64.StdEncoding.EncodeToString([]byte("测试 iPhone")),
	)
	request.Header.Set(mobileDeviceModelHeader, "iPhone")
	request.Header.Set(mobileDeviceModelIdentifierHeader, "iPhone18,2")
	request.Header.Set(mobileOSNameHeader, "iOS")
	request.Header.Set(mobileOSVersionHeader, "26.0")
	request.Header.Set(mobileAppVersionHeader, "0.1.0")
	request.Header.Set(mobileAppBuildHeader, "1")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var session sessionResponse
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !session.Authenticated || session.SetupRequired ||
		session.UserID != "member-1" || session.Username != "phone-user" ||
		session.Role != string(auth.RoleMember) ||
		session.ProfileContactID != "contact-1" ||
		!session.IOSPairingEnabled || len(session.AllowedLineIDs) != 1 ||
		session.AllowedLineIDs[0] != "line-1" || session.CSRFToken != "" {
		t.Fatalf("session = %+v", session)
	}
	if repository.mobileConfirmedDigest != digest {
		t.Fatalf("confirmed digest = %x, want %x", repository.mobileConfirmedDigest, digest)
	}
	if repository.mobileConfirmedDevice != (mobilepairing.DeviceInfo{
		Name:            "测试 iPhone",
		Model:           "iPhone",
		ModelIdentifier: "iPhone18,2",
		OSName:          "iOS",
		OSVersion:       "26.0",
		AppVersion:      "0.1.0",
		AppBuild:        "1",
	}) {
		t.Fatalf("confirmed device = %+v", repository.mobileConfirmedDevice)
	}
}

func TestMobileBearerRegistersAndClearsApplePushTokens(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	registrationToken := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/mobile/push",
		bytes.NewBufferString(`{"apns_token":"`+registrationToken+`","voip_token":"`+registrationToken+`","bundle_id":"com.example.modemdeck"}`),
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("register status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.mobilePushRegistration.APNSToken != registrationToken ||
		repository.mobilePushRegistration.VoIPToken != registrationToken ||
		repository.mobilePushRegistration.Environment != "development" {
		t.Fatalf("registration = %+v", repository.mobilePushRegistration)
	}

	clearRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/mobile/push", nil)
	clearRequest.Header.Set("Authorization", "Bearer "+string(token))
	clearResponse := httptest.NewRecorder()
	api.ServeHTTP(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusNoContent || !repository.mobilePushCleared {
		t.Fatalf("clear status = %d, cleared = %v", clearResponse.Code, repository.mobilePushCleared)
	}
}

func TestMobileBearerAllowsCallAPIWithoutCSRF(t *testing.T) {
	t.Parallel()

	token, digest, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls",
		bytes.NewBufferString(`{}`),
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(
		t,
		response,
		http.StatusServiceUnavailable,
		"communications_unavailable",
	)
	if repository.mobileConfirmedDigest != digest {
		t.Fatalf(
			"confirmed digest = %x, want %x",
			repository.mobileConfirmedDigest,
			digest,
		)
	}
}

func TestMobileBearerCanAccessRoleCheckedSettingsAPI(t *testing.T) {
	t.Parallel()

	token, digest, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/settings/system",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.mobileConfirmedDigest != digest {
		t.Fatalf(
			"confirmed digest = %x, want %x",
			repository.mobileConfirmedDigest,
			digest,
		)
	}
}

func TestMobileBearerConfirmationFailureRejectsTheRequest(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
		},
		mobileConfirmError: errors.New("database unavailable"),
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(
		t,
		response,
		http.StatusServiceUnavailable,
		"authentication_unavailable",
	)
}

func TestMobileBearerCanRevokeItsOwnPairingCredential(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeMobilePairingRepository{
		fakeRepository: &fakeRepository{
			mobileFound: true,
			mobilePrincipal: auth.Principal{
				UserID:            "member-1",
				Role:              auth.RoleMember,
				IOSPairingEnabled: true,
			},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/mobile/pairing",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.revokedUserID != "member-1" {
		t.Fatalf("revoked user = %q", repository.revokedUserID)
	}
}

func TestRevokedMobileBearerIsRejected(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(
		&fakeRepository{},
		Options{disableAuthentication: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/bootstrap",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(
		t,
		response,
		http.StatusUnauthorized,
		"authentication_required",
	)
}
