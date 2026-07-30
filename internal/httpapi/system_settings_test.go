package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestSystemSettingsGetAndPatch(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{
		systemSettings: store.SystemSettings{
			Language: store.SystemLanguageZhCN,
			Revision: 4,
		},
		updateSystemResult: store.SystemSettings{
			Language: store.SystemLanguageEnUS,
			Revision: 5,
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	getRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/settings/system",
		nil,
	)
	getResponse := httptest.NewRecorder()
	api.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d; body = %s", getResponse.Code, getResponse.Body)
	}
	var getBody systemSettingsResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	if getBody.Settings != repository.systemSettings {
		t.Fatalf("GET settings = %+v", getBody.Settings)
	}

	input := updateSystemSettingsRequest{
		Language:         store.SystemLanguageEnUS,
		ExpectedRevision: 4,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	patchRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/settings/system",
		bytes.NewReader(payload),
	)
	patchRequest.Header.Set("Content-Type", "application/json")
	patchResponse := httptest.NewRecorder()
	api.ServeHTTP(patchResponse, patchRequest)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d; body = %s", patchResponse.Code, patchResponse.Body)
	}
	if repository.updateSystemInput != store.SystemLanguageEnUS ||
		repository.updateSystemRev != 4 {
		t.Fatalf(
			"update input = %q revision %d",
			repository.updateSystemInput,
			repository.updateSystemRev,
		)
	}
}

func TestSessionFollowsBrowserLanguageBeforeAuthentication(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{
		systemSettings: store.SystemSettings{
			Language: store.SystemLanguageEnUS,
			Revision: 2,
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body)
	}
	var body sessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Language != string(store.SystemLanguageAuto) {
		t.Fatalf("language = %q, want auto", body.Language)
	}
}

var _ Repository = (*fakeRepository)(nil)
