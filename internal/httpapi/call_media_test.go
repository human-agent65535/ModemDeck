package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/mediaapp"
)

func TestCallMediaExchangeReturnsAnswer(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{answer: "answer-sdp"}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"owner-1","offer_sdp":"offer-sdp"}`),
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
		media.offer != "offer-sdp" {
		t.Fatalf("response = %+v, media = %+v", body, media)
	}
}

func TestCallMediaErrorsHaveStableMapping(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{err: mediaapp.ErrNotActive}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/media",
		bytes.NewBufferString(`{"owner_token":"owner-1","offer_sdp":"offer-sdp"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusConflict, "call_not_active")
}

func TestCallMediaDeleteReleasesMatchingOwner(t *testing.T) {
	t.Parallel()

	media := &fakeCallMedia{}
	api, err := New(&fakeRepository{}, Options{
		CallMedia:             media,
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
	answer         string
	err            error
	callID         string
	ownerToken     string
	offer          string
	releasedCallID string
	closed         string
}

func (f *fakeCallMedia) Exchange(
	_ context.Context,
	callID, ownerToken, offer string,
) (string, error) {
	f.callID = callID
	f.ownerToken = ownerToken
	f.offer = offer
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
