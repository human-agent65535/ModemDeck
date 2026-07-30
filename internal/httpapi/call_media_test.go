package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

func TestCallMediaExchangeReturnsAnswer(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{answer: "answer-sdp"}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"owner-1","offer_sdp":"offer-sdp","holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body callMediaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AnswerSDP != "answer-sdp" ||
		media.callID != "call-1" ||
		media.ownerToken != "owner-1" ||
		media.offer != "offer-sdp" ||
		media.relayOnly {
		t.Fatalf("response = %+v, media = %+v", body, media)
	}
}

func TestCallMediaErrorsHaveStableMapping(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{err: mediaapp.ErrNotActive}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"owner-1","offer_sdp":"offer-sdp","holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusConflict, "call_not_active")
}

func TestCallMediaRejectsNonOwnerBeforeExchange(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		CallLeases:            &fakeCallLeases{err: calllease.ErrNotOwner},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"owner-2","offer_sdp":"offer-sdp","holder_id":"browser-2"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusConflict, "call_not_owned")
	if media.callID != "" {
		t.Fatalf("non-owner reached media exchange for call %q", media.callID)
	}
}

func TestCallMediaDeleteReleasesMatchingOwner(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"owner-1","holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if media.releasedCallID != "call-1" || media.ownerToken != "owner-1" {
		t.Fatalf("media release = %+v", media)
	}
}

func TestCallMediaDeleteReportsInvalidOwnerAsMediaRequest(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{err: mediaapp.ErrInvalidArgument}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"","holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_media_request")
}

func TestMobileCallMediaUsesRelayOnlyServerPeer(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	media := &fakeCallMedia{answer: "answer-sdp"}
	api, err := New(&fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
		callLineID: "line-1",
	}, Options{
		CallMedia:             media,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(
			`{"owner_token":"owner-1","offer_sdp":"offer-sdp","holder_id":"ios-1"}`,
		),
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !media.relayOnly {
		t.Fatalf(
			"status = %d, relay_only = %t; body = %s",
			response.Code,
			media.relayOnly,
			response.Body.String(),
		)
	}
}

func TestCloudflareWebCallMediaUsesRelayOnlyServerPeer(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{answer: "answer-sdp"}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:  media,
		CallLeases: &fakeCallLeases{},
		MobilePairing: fakeMobilePairingAvailability{
			webIngressHost: "deck.example.com",
		},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(
			`{"owner_token":"owner-1","offer_sdp":"offer-sdp","holder_id":"browser-1"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Host", "deck.example.com")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !media.relayOnly {
		t.Fatalf(
			"status = %d, relay_only = %t; body = %s",
			response.Code,
			media.relayOnly,
			response.Body.String(),
		)
	}
}

func TestMobileCallMediaICEConfigurationRequiresLease(t *testing.T) {
	t.Parallel()

	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	provider := &fakeRTCConfigurationProvider{
		configuration: rtcconfig.Configuration{
			ICEServers: []rtcconfig.ICEServer{{
				URLs:       []string{"turns:turn.example.test:443?transport=tcp"},
				Username:   "relay-user",
				Credential: "relay-credential",
			}},
			ExpiresAt: expiresAt,
			RelayOnly: true,
		},
	}
	leases := &fakeCallLeases{}
	api, err := New(&fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
		callLineID: "line-1",
	}, Options{
		CallLeases:            leases,
		RTCConfiguration:      provider,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media/ice",
		bytes.NewBufferString(`{"holder_id":"ios-1"}`),
	)
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body callMediaICEConfigurationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ICETransportPolicy != "relay" ||
		body.ExpiresAt != expiresAt.Format(time.RFC3339) ||
		len(body.ICEServers) != 1 ||
		provider.generated != 1 ||
		leases.requires != 1 {
		t.Fatalf(
			"response = %+v, generated = %d, requires = %d",
			body,
			provider.generated,
			leases.requires,
		)
	}
}

func TestCloudflareWebCallMediaICEConfigurationUsesTURN(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	provider := &fakeRTCConfigurationProvider{
		configuration: rtcconfig.Configuration{
			ICEServers: []rtcconfig.ICEServer{{
				URLs:       []string{"turns:turn.example.test:443?transport=tcp"},
				Username:   "relay-user",
				Credential: "relay-credential",
			}},
			ExpiresAt: expiresAt,
			RelayOnly: true,
		},
	}
	api, err := New(&fakeRepository{}, Options{
		CallLeases:       &fakeCallLeases{},
		RTCConfiguration: provider,
		MobilePairing: fakeMobilePairingAvailability{
			webIngressHost: "deck.example.com",
		},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media/ice",
		bytes.NewBufferString(`{"holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Host", "deck.example.com")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body callMediaICEConfigurationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ICETransportPolicy != "relay" ||
		body.ExpiresAt != expiresAt.Format(time.RFC3339) ||
		len(body.ICEServers) != 1 ||
		provider.generated != 1 {
		t.Fatalf(
			"response = %+v, generated = %d",
			body,
			provider.generated,
		)
	}
}

func TestLocalWebCallMediaICEConfigurationDoesNotRequireTURN(t *testing.T) {
	t.Parallel()

	provider := &fakeRTCConfigurationProvider{
		err: errors.New("TURN must not be requested for local Web"),
	}
	api, err := New(&fakeRepository{}, Options{
		CallLeases:       &fakeCallLeases{},
		RTCConfiguration: provider,
		MobilePairing: fakeMobilePairingAvailability{
			webIngressHost: "deck.example.com",
		},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media/ice",
		bytes.NewBufferString(`{"holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Host", "192.168.50.111:7577")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body callMediaICEConfigurationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ICETransportPolicy != "all" ||
		len(body.ICEServers) != 0 ||
		body.ExpiresAt != "" ||
		provider.generated != 0 {
		t.Fatalf(
			"response = %+v, generated = %d",
			body,
			provider.generated,
		)
	}
}

type fakeCallMedia struct {
	answer         string
	err            error
	callID         string
	ownerToken     string
	offer          string
	relayOnly      bool
	releasedCallID string
	closed         string
}

type fakeRTCConfigurationProvider struct {
	configuration rtcconfig.Configuration
	err           error
	generated     int
}

func (provider *fakeRTCConfigurationProvider) Generate(
	context.Context,
) (rtcconfig.Configuration, error) {
	provider.generated++
	return provider.configuration, provider.err
}

func (f *fakeCallMedia) Exchange(
	_ context.Context,
	callID, ownerToken, offer string,
	relayOnly bool,
) (string, error) {
	f.callID = callID
	f.ownerToken = ownerToken
	f.offer = offer
	f.relayOnly = relayOnly
	return f.answer, f.err
}

func (f *fakeCallMedia) ReleaseOwner(
	_ context.Context,
	callID, ownerToken string,
) error {
	f.releasedCallID = callID
	f.ownerToken = ownerToken
	return f.err
}

func (f *fakeCallMedia) CloseCall(_ context.Context, callID string) error {
	f.closed = callID
	return nil
}
