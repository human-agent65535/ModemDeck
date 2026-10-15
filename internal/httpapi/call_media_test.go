package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		bytes.NewBufferString(`{"owner_token":"owner-1"}`),
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
		bytes.NewBufferString(`{"owner_token":""}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_media_request")
}

type fakeCallMedia struct {
	err            error
	ownerToken     string
	releasedCallID string
	closed         string
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

func TestLegacyMediaNegotiationRoutesAreRemoved(t *testing.T) {
	api, err := New(&fakeRepository{}, Options{CallMedia: &fakeCallMedia{}, CallLeases: &fakeCallLeases{}, disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path   string
		status int
	}{{"/api/v1/calls/call-1/media", http.StatusMethodNotAllowed}, {"/api/v1/calls/call-1/media/ice", http.StatusNotFound}} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, item.path, bytes.NewBufferString(`{"owner_token":"owner","offer_sdp":"ignored"}`))
		request.Header.Set("Content-Type", "application/json")
		api.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s status=%d body=%s", item.path, response.Code, response.Body.String())
		}
	}
}
func TestWSSCapabilityHasOnlyDerivedLegacyBooleanAlias(t *testing.T) {
	for _, available := range []bool{false, true} {
		encoded, err := json.Marshal(Capabilities{WSSAudio: available})
		if err != nil {
			t.Fatal(err)
		}
		var values map[string]any
		if err := json.Unmarshal(encoded, &values); err != nil {
			t.Fatal(err)
		}
		if values["wss_audio"] != available || values["webrtc_audio"] != available {
			t.Fatalf("capability alias=%s", encoded)
		}
	}
}
func TestCallMediaDeleteRejectsForeignLease(t *testing.T) {
	media := &fakeCallMedia{}
	api, err := New(&fakeRepository{}, Options{CallMedia: media, CallLeases: &fakeCallLeases{err: calllease.ErrNotOwner}, disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/calls/call-1/media", bytes.NewBufferString(`{"owner_token":"foreign"}`))
	request.Header.Set("Content-Type", "application/json")
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusConflict, "call_not_owned")
	if media.releasedCallID != "" {
		t.Fatal("foreign lease reached release")
	}
}
