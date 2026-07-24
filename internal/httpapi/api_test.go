package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/communication"
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
	messageReadICCID   string
	messageReadPeer    string
	messageReadError   error
	recordingQuery     store.RecordingQuery
	recordingEntries   []store.RecordingEntry
	recordingError     error
	lines              []store.LineSummary
	updateLineICCID    string
	updateLineLabel    string
	updateLineResult   store.LineSummary
	updateLineError    error
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

func (repository *fakeRepository) MarkMessageThreadRead(_ context.Context, iccid, peer string) error {
	repository.messageReadICCID = iccid
	repository.messageReadPeer = peer
	return repository.messageReadError
}

func (repository *fakeRepository) Calls(context.Context, store.CallQuery) ([]store.Call, error) {
	return []store.Call{}, nil
}

func (repository *fakeRepository) RecordingEntries(
	_ context.Context,
	query store.RecordingQuery,
) ([]store.RecordingEntry, error) {
	repository.recordingQuery = query
	return repository.recordingEntries, repository.recordingError
}

func (repository *fakeRepository) Devices(context.Context) ([]store.Device, error) {
	return []store.Device{}, nil
}

func (repository *fakeRepository) CreateDevice(
	context.Context,
	store.DeviceInput,
) (store.Device, error) {
	return store.Device{}, nil
}

func (repository *fakeRepository) RenameDevice(
	context.Context,
	string,
	string,
) (store.Device, error) {
	return store.Device{}, nil
}

func (repository *fakeRepository) Lines(context.Context) ([]store.LineSummary, error) {
	return repository.lines, nil
}

func (repository *fakeRepository) UpdateLineLabel(
	_ context.Context,
	iccid string,
	label string,
) (store.LineSummary, error) {
	repository.updateLineICCID = iccid
	repository.updateLineLabel = label
	return repository.updateLineResult, repository.updateLineError
}

func (repository *fakeRepository) LineSettings(context.Context) (store.LineSettings, error) {
	return store.LineSettings{DefaultDeviceIMEI: "", Revision: 1}, nil
}

func (repository *fakeRepository) UpdateLineSettings(
	context.Context,
	string,
	int64,
) (store.LineSettings, error) {
	return store.LineSettings{Revision: 2}, nil
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
	api, err := New(repository, Options{
		disableAuthentication: true,
		Capabilities:          fixedCapabilities{value: want},
		CallMedia:             &fakeCallMedia{},
	})
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

func TestBootstrapRequiresHostMediaCapabilityAndCallMediaService(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		hostMedia       bool
		callMedia       CallMediaService
		wantWebRTCAudio bool
	}{
		{
			name:            "no host binding",
			hostMedia:       false,
			callMedia:       &fakeCallMedia{},
			wantWebRTCAudio: false,
		},
		{
			name:            "no application media service",
			hostMedia:       true,
			callMedia:       nil,
			wantWebRTCAudio: false,
		},
		{
			name:            "both sides available",
			hostMedia:       true,
			callMedia:       &fakeCallMedia{},
			wantWebRTCAudio: true,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			api, err := New(
				&fakeRepository{lines: []store.LineSummary{}},
				Options{
					Communications: &fakeCommunications{status: communication.Status{
						Connected: true,
						Capabilities: agentclient.Capabilities{
							Media: test.hostMedia,
						},
						Lines: []store.LineSummary{},
					}},
					CallMedia:             test.callMedia,
					disableAuthentication: true,
				},
			)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := httptest.NewRecorder()
			api.ServeHTTP(
				response,
				httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil),
			)
			var body bootstrapResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode bootstrap: %v", err)
			}
			if body.Capabilities.WebRTCAudio != test.wantWebRTCAudio {
				t.Fatalf(
					"WebRTCAudio = %v, want %v",
					body.Capabilities.WebRTCAudio,
					test.wantWebRTCAudio,
				)
			}
		})
	}
}

func TestBootstrapMergesPersistedIdentityIntoLiveLines(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{{
		ICCID:       "8986010000000000001",
		LineLabel:   "主卡",
		IMSI:        "460010000000001",
		PhoneNumber: "+818000000001",
		Operator:    "China Unicom",
		DeviceIMEI:  "860000000000001",
		DeviceAlias: "主线路",
	}}}
	communications := &fakeCommunications{status: communication.Status{
		Connected: true,
		Lines: []store.LineSummary{{
			ID:         "line-1",
			ICCID:      "8986010000000000001",
			IMSI:       "460010000000001",
			Operator:   "46001",
			DeviceIMEI: "860000000000001",
			Model:      "QDC507",
			State:      "registered",
			Capabilities: store.LineCapabilities{
				Voice: true,
				Dial:  true,
			},
		}},
	}}
	api, err := New(repository, Options{
		Communications:        communications,
		disableAuthentication: true,
	})
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
	if len(body.Lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(body.Lines))
	}
	line := body.Lines[0]
	if line.LineLabel != "主卡" ||
		line.DeviceAlias != "主线路" ||
		line.PhoneNumber != "+818000000001" ||
		line.Model != "QDC507" ||
		line.State != "registered" ||
		!line.Capabilities.Voice {
		t.Fatalf("merged line = %+v", line)
	}
	if line.Operator != "46001" {
		t.Fatalf("operator = %q, want live value to remain authoritative", line.Operator)
	}
}

func TestBootstrapMergesPersistedLineLabelByIMSI(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{{
		ICCID:      "8986010000000000001",
		LineLabel:  "副卡",
		IMSI:       "460010000000001",
		DeviceIMEI: "860000000000001",
	}}}
	communications := &fakeCommunications{status: communication.Status{
		Connected: true,
		Lines: []store.LineSummary{{
			ID:         "line-without-iccid",
			IMSI:       "460010000000001",
			DeviceIMEI: "860000000000001",
			State:      "registered",
		}},
	}}
	api, err := New(repository, Options{
		Communications:        communications,
		disableAuthentication: true,
	})
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
	if len(body.Lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(body.Lines))
	}
	if body.Lines[0].LineLabel != "副卡" {
		t.Fatalf("line label = %q, want 副卡", body.Lines[0].LineLabel)
	}
	if body.Lines[0].ICCID != "" {
		t.Fatalf("live ICCID = %q, want empty ICCID to remain non-editable", body.Lines[0].ICCID)
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
