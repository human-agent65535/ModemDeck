package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeRepository struct {
	pingError          error
	contactLimit       int
	contact            store.Contact
	contactError       error
	createContactInput store.ContactInput
	createContactError error
	updateContactID    string
	updateContactInput store.ContactInput
	updateContactError error
	deleteContactID    string
	deleteContactRev   int64
	deleteContactError error
	lines              []store.LineSummary
}

func (repository *fakeRepository) Ping(context.Context) error {
	return repository.pingError
}

func (repository *fakeRepository) Contacts(_ context.Context, query store.ContactQuery) ([]store.Contact, error) {
	repository.contactLimit = query.Limit
	return []store.Contact{}, nil
}

func (repository *fakeRepository) Contact(context.Context, string) (store.Contact, error) {
	return repository.contact, repository.contactError
}

func (repository *fakeRepository) CreateContact(_ context.Context, input store.ContactInput) (store.Contact, error) {
	repository.createContactInput = input
	return repository.contact, repository.createContactError
}

func (repository *fakeRepository) UpdateContact(_ context.Context, id string, input store.ContactInput) (store.Contact, error) {
	repository.updateContactID = id
	repository.updateContactInput = input
	return repository.contact, repository.updateContactError
}

func (repository *fakeRepository) DeleteContact(_ context.Context, id string, revision int64) error {
	repository.deleteContactID = id
	repository.deleteContactRev = revision
	return repository.deleteContactError
}

func (repository *fakeRepository) MessageThreads(context.Context, store.ThreadQuery) ([]store.MessageThread, error) {
	return []store.MessageThread{}, nil
}

func (repository *fakeRepository) Messages(context.Context, store.MessageQuery) ([]store.Message, error) {
	return []store.Message{}, nil
}

func (repository *fakeRepository) MarkMessageThreadRead(context.Context, string, string) error {
	return nil
}

func (repository *fakeRepository) Calls(context.Context, store.CallQuery) ([]store.Call, error) {
	return []store.Call{}, nil
}

func (repository *fakeRepository) Devices(context.Context) ([]store.Device, error) {
	return []store.Device{}, nil
}

func (repository *fakeRepository) Lines(context.Context) ([]store.LineSummary, error) {
	return repository.lines, nil
}

type fixedCapabilities struct {
	value Capabilities
	err   error
}

func (source fixedCapabilities) Capabilities(context.Context) (Capabilities, error) {
	return source.value, source.err
}

func TestContactLimitIsClamped(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/contacts?limit=999999", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if repository.contactLimit != store.MaxQueryLimit {
		t.Fatalf("repository limit = %d, want %d", repository.contactLimit, store.MaxQueryLimit)
	}
	var body contactsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Meta.Limit != store.MaxQueryLimit {
		t.Fatalf("response limit = %d, want %d", body.Meta.Limit, store.MaxQueryLimit)
	}
}

func TestBootstrapGatesCapabilitiesUntilAgentConnected(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{}}
	api, err := New(repository, Options{disableAuthentication: true, Capabilities: fixedCapabilities{value: Capabilities{
		AgentConnected: false,
		Dial:           true,
		Message:        true,
		WebRTCAudio:    true,
		DeviceControl:  true,
		VoLTEControl:   true,
		VoWiFiControl:  true,
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body bootstrapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if body.Capabilities.AgentConnected || body.Capabilities.Dial || body.Capabilities.Message ||
		body.Capabilities.WebRTCAudio ||
		body.Capabilities.DeviceControl || body.Capabilities.VoLTEControl || body.Capabilities.VoWiFiControl {
		t.Fatalf("capabilities = %+v, want all capability flags false", body.Capabilities)
	}
	if body.Capabilities.UnavailableReasons["dial"] == "" || body.Capabilities.UnavailableReasons["message"] == "" {
		t.Fatalf("unavailable reasons = %+v, want dial and message reasons", body.Capabilities.UnavailableReasons)
	}
}

func TestBootstrapPreservesConnectedCapabilities(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{}}
	want := Capabilities{
		AgentConnected: true,
		Dial:           true,
		Message:        true,
		WebRTCAudio:    true,
		DeviceControl:  true,
		VoLTEControl:   true,
		VoWiFiControl:  false,
		UnavailableReasons: map[string]string{
			"vowifi": "not implemented",
		},
	}
	api, err := New(repository, Options{disableAuthentication: true, Capabilities: fixedCapabilities{value: want}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body bootstrapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if !body.Capabilities.AgentConnected || !body.Capabilities.Dial || !body.Capabilities.Message ||
		!body.Capabilities.WebRTCAudio || !body.Capabilities.DeviceControl ||
		!body.Capabilities.VoLTEControl || body.Capabilities.VoWiFiControl {
		t.Fatalf("capabilities = %+v, want connected source values", body.Capabilities)
	}
	if body.Capabilities.UnavailableReasons["vowifi"] != "not implemented" {
		t.Fatalf("unavailable reasons = %+v", body.Capabilities.UnavailableReasons)
	}
}

func TestStructuredErrors(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{pingError: errors.New("database down")}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "health unavailable", method: http.MethodGet, path: "/api/v1/health", wantStatus: 503, wantCode: "database_unavailable"},
		{name: "invalid call kind", method: http.MethodGet, path: "/api/v1/calls?kind=sideways", wantStatus: 400, wantCode: "invalid_argument"},
		{name: "missing message iccid", method: http.MethodGet, path: "/api/v1/messages?peer=%2B15550100", wantStatus: 400, wantCode: "invalid_argument"},
		{name: "missing message peer", method: http.MethodGet, path: "/api/v1/messages?iccid=8901000000000000001", wantStatus: 400, wantCode: "invalid_argument"},
		{name: "wrong method", method: http.MethodPatch, path: "/api/v1/contacts", wantStatus: 405, wantCode: "method_not_allowed"},
		{name: "missing endpoint", method: http.MethodGet, path: "/api/v1/missing", wantStatus: 404, wantCode: "not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			api.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			var body errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode structured error: %v", err)
			}
			if body.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", body.Code, test.wantCode)
			}
		})
	}
}
