package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeRepository struct {
	pingError             error
	contactLimit          int
	contactQuery          store.ContactQuery
	contacts              []store.Contact
	contact               store.Contact
	contactError          error
	createContactInput    store.ContactInput
	createContactError    error
	updateContactID       string
	updateContactInput    store.ContactInput
	updateContactError    error
	deleteContactID       string
	deleteContactRev      int64
	deleteContactError    error
	deleteContacts        []store.ContactRevision
	messageQuery          store.MessageQuery
	messages              []store.Message
	threadQuery           store.ThreadQuery
	threads               []store.MessageThread
	messageReadIdentity   store.MessageThreadIdentity
	messageReadError      error
	messageUpdateAction   store.MessageThreadAction
	messageUpdateThreads  []store.MessageThreadIdentity
	messageUpdateError    error
	messageDeleteIdentity store.MessageThreadIdentity
	messageDeleteError    error
	missedReadCalls       int
	missedReadIDs         []string
	missedUnreadIDs       []string
	missedReadError       error
	callFavoriteIDs       []string
	callFavorite          bool
	callFavoriteError     error
	callQuery             store.CallQuery
	calls                 []store.Call
	recordingQuery        store.RecordingQuery
	recordingEntries      []store.RecordingEntry
	recordingError        error
	recordingFavorites    []store.RecordingIdentity
	recordingFavorite     bool
	recordingFavoriteErr  error
	devices               []store.Device
	deleteDeviceIMEI      string
	deleteDeviceError     error
	lines                 []store.LineSummary
	updateLineID          string
	updateLineLabel       string
	updateLineColor       *store.LineColor
	updateLineResult      store.LineSummary
	updateLineError       error
	systemSettings        store.SystemSettings
	principalLanguage     store.SystemLanguage
	systemSettingsError   error
	updateSystemInput     store.SystemLanguage
	updateSystemRev       int64
	updateSystemResult    store.SystemSettings
	updateSystemError     error
	mobilePrincipal       auth.Principal
	mobileFound           bool
	mobileError           error
	mobileConfirmedDigest mobilepairing.TokenDigest
	mobileConfirmError    error
	iosPairingStatus      store.IOSPairingStatus
	iosPairingStatusError error
	iosRevokedUserID      string
	iosRevokedDigest      mobilepairing.TokenDigest
	iosRevokeError        error
	callLineID            string
	callLineError         error
}

func (repository *fakeRepository) IOSPairingStatus(
	context.Context,
	string,
) (store.IOSPairingStatus, error) {
	return repository.iosPairingStatus, repository.iosPairingStatusError
}

func (repository *fakeRepository) RevokeIOSPairingCredentialWithDigest(
	_ context.Context,
	userID string,
) (mobilepairing.TokenDigest, bool, error) {
	if repository.iosRevokeError != nil {
		return mobilepairing.TokenDigest{}, false, repository.iosRevokeError
	}
	if !repository.iosPairingStatus.HasCredential {
		return mobilepairing.TokenDigest{}, false, nil
	}
	repository.iosRevokedUserID = userID
	repository.iosPairingStatus.HasCredential = false
	return repository.iosRevokedDigest, true, nil
}

func (repository *fakeRepository) IOSPairingPrincipalByTokenDigest(
	context.Context,
	mobilepairing.TokenDigest,
) (auth.Principal, bool, error) {
	return repository.mobilePrincipal, repository.mobileFound, repository.mobileError
}

func (repository *fakeRepository) ConfirmIOSPairingCredential(
	_ context.Context,
	digest mobilepairing.TokenDigest,
) (bool, error) {
	repository.mobileConfirmedDigest = digest
	return repository.mobileConfirmError == nil, repository.mobileConfirmError
}

func (repository *fakeRepository) CallLineID(
	context.Context,
	string,
) (string, error) {
	return repository.callLineID, repository.callLineError
}

func (repository *fakeRepository) Ping(context.Context) error {
	return repository.pingError
}

func (repository *fakeRepository) Contacts(_ context.Context, query store.ContactQuery) ([]store.Contact, error) {
	repository.contactLimit = query.Limit
	repository.contactQuery = query
	return repository.contacts, nil
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

func (repository *fakeRepository) DeleteContacts(
	_ context.Context,
	contacts []store.ContactRevision,
) error {
	repository.deleteContacts = append([]store.ContactRevision(nil), contacts...)
	return repository.deleteContactError
}

func (repository *fakeRepository) MessageThreads(
	_ context.Context,
	query store.ThreadQuery,
) ([]store.MessageThread, error) {
	repository.threadQuery = query
	return repository.threads, nil
}

func (repository *fakeRepository) Messages(_ context.Context, query store.MessageQuery) ([]store.Message, error) {
	repository.messageQuery = query
	return repository.messages, nil
}

func (repository *fakeRepository) MarkMessageThreadRead(
	_ context.Context,
	identity store.MessageThreadIdentity,
) error {
	repository.messageReadIdentity = identity
	return repository.messageReadError
}

func (repository *fakeRepository) UpdateMessageThreads(
	_ context.Context,
	threads []store.MessageThreadIdentity,
	action store.MessageThreadAction,
) error {
	repository.messageUpdateThreads = append(
		[]store.MessageThreadIdentity(nil),
		threads...,
	)
	repository.messageUpdateAction = action
	return repository.messageUpdateError
}

func (repository *fakeRepository) DeleteMessageThread(
	_ context.Context,
	identity store.MessageThreadIdentity,
) error {
	repository.messageDeleteIdentity = identity
	return repository.messageDeleteError
}

func (repository *fakeRepository) Calls(
	_ context.Context,
	query store.CallQuery,
) ([]store.Call, error) {
	repository.callQuery = query
	return repository.calls, nil
}

func (repository *fakeRepository) MarkMissedCallsRead(context.Context) error {
	repository.missedReadCalls++
	return repository.missedReadError
}

func (repository *fakeRepository) MarkMissedCallsReadByIDs(
	_ context.Context,
	callIDs []string,
) error {
	repository.missedReadIDs = append([]string(nil), callIDs...)
	return repository.missedReadError
}

func (repository *fakeRepository) MarkMissedCallsUnreadByIDs(
	_ context.Context,
	callIDs []string,
) error {
	repository.missedUnreadIDs = append([]string(nil), callIDs...)
	return repository.missedReadError
}

func (repository *fakeRepository) SetCallFavoritesByIDs(
	_ context.Context,
	callIDs []string,
	favorite bool,
) error {
	repository.callFavoriteIDs = append([]string(nil), callIDs...)
	repository.callFavorite = favorite
	return repository.callFavoriteError
}

func (repository *fakeRepository) RecordingEntries(
	_ context.Context,
	query store.RecordingQuery,
) ([]store.RecordingEntry, error) {
	repository.recordingQuery = query
	return repository.recordingEntries, repository.recordingError
}

func (repository *fakeRepository) SetRecordingFavorites(
	_ context.Context,
	recordings []store.RecordingIdentity,
	favorite bool,
) error {
	repository.recordingFavorites = append(
		[]store.RecordingIdentity(nil),
		recordings...,
	)
	repository.recordingFavorite = favorite
	return repository.recordingFavoriteErr
}

func (repository *fakeRepository) Devices(context.Context) ([]store.Device, error) {
	return repository.devices, nil
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

func (repository *fakeRepository) DeleteDevice(_ context.Context, imei string) error {
	repository.deleteDeviceIMEI = imei
	return repository.deleteDeviceError
}

func (repository *fakeRepository) Lines(context.Context) ([]store.LineSummary, error) {
	return repository.lines, nil
}

func (repository *fakeRepository) UpdateLineLabel(
	_ context.Context,
	lineID string,
	label string,
	color *store.LineColor,
) (store.LineSummary, error) {
	repository.updateLineID = lineID
	repository.updateLineLabel = label
	repository.updateLineColor = color
	return repository.updateLineResult, repository.updateLineError
}

func (repository *fakeRepository) LineSettings(context.Context) (store.LineSettings, error) {
	return store.LineSettings{DefaultLineID: "", Revision: 1}, nil
}

func (repository *fakeRepository) UpdateLineSettings(
	context.Context,
	string,
	int64,
) (store.LineSettings, error) {
	return store.LineSettings{Revision: 2}, nil
}

func (repository *fakeRepository) SystemSettings(ctx context.Context) (store.SystemSettings, error) {
	if _, exists := auth.PrincipalFromContext(ctx); exists &&
		repository.principalLanguage != "" {
		return store.SystemSettings{
			Language: repository.principalLanguage,
			Revision: 1,
		}, repository.systemSettingsError
	}
	if repository.systemSettings.Language == "" {
		return store.SystemSettings{
			Language: store.SystemLanguageAuto,
			Revision: 1,
		}, repository.systemSettingsError
	}
	return repository.systemSettings, repository.systemSettingsError
}

func (repository *fakeRepository) UpdateSystemSettings(
	_ context.Context,
	language store.SystemLanguage,
	expectedRevision int64,
) (store.SystemSettings, error) {
	repository.updateSystemInput = language
	repository.updateSystemRev = expectedRevision
	return repository.updateSystemResult, repository.updateSystemError
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
		lineMedia       bool
		hostMedia       bool
		callMedia       CallMediaService
		wantWebRTCAudio bool
	}{
		{
			name:            "no host binding",
			lineMedia:       true,
			hostMedia:       false,
			callMedia:       &fakeCallMedia{},
			wantWebRTCAudio: false,
		},
		{
			name:            "no line binding",
			lineMedia:       false,
			hostMedia:       true,
			callMedia:       &fakeCallMedia{},
			wantWebRTCAudio: false,
		},
		{
			name:            "no application media service",
			lineMedia:       true,
			hostMedia:       true,
			callMedia:       nil,
			wantWebRTCAudio: false,
		},
		{
			name:            "both sides available",
			lineMedia:       true,
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
						Lines: []store.LineSummary{{
							Capabilities: store.LineCapabilities{
								Media: test.lineMedia,
							},
						}},
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
		ID:          "line-1",
		ICCID:       "8986010000000000001",
		LineLabel:   "主卡",
		IMSI:        "460010000000001",
		PhoneNumber: "+818000000001",
		Operator:    "China Unicom",
		DeviceIMEI:  "860000000000001",
		DeviceName:  "主线路",
	}}}
	communications := &fakeCommunications{status: communication.Status{
		Connected: true,
		Lines: []store.LineSummary{{
			ID:         "line-1",
			ICCID:      "8986010000000000999",
			IMSI:       "460010000000999",
			Operator:   "46001",
			DeviceIMEI: "860000000000999",
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
		line.DeviceName != "主线路" ||
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

func TestBootstrapDisconnectedFallbackHidesOrphanedStableLines(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{
		{
			ID:         "line-attached",
			EndpointID: "endpoint-attached",
			DeviceIMEI: "860000000000001",
			LineLabel:  "Attached",
		},
		{
			ID:        "line-orphaned",
			LineLabel: "Historical orphan",
		},
	}}
	api, err := New(repository, Options{
		Communications: &fakeCommunications{
			statusError: errors.New("live hardware state is unavailable"),
		},
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
	if len(body.Lines) != 1 || body.Lines[0].ID != "line-attached" {
		t.Fatalf("fallback lines = %+v, want only attached persisted line", body.Lines)
	}
	if len(body.LineCatalog) != 2 ||
		body.LineCatalog[0].ID != "line-attached" ||
		body.LineCatalog[1].ID != "line-orphaned" {
		t.Fatalf("line catalog = %+v, want every persisted stable line", body.LineCatalog)
	}
	if body.Capabilities.AgentConnected {
		t.Fatalf("capabilities = %+v, want disconnected", body.Capabilities)
	}
}

func TestDevicesExposeLiveServingNetworkWithoutReplacingHomeOperator(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{devices: []store.Device{{
		IMEI:         "867530900000099",
		CurrentICCID: "89840400000000000099",
		SIM: &store.SIMCard{
			ICCID:            "89840400000000000099",
			IMSI:             "452040000000001",
			Operator:         "Viettel Mobile",
			HomeOperatorName: "Viettel Mobile",
		},
	}}}
	communications := &fakeCommunications{status: communication.Status{
		Connected: true,
		Lines: []store.LineSummary{{
			ID:                     "line-roaming",
			ICCID:                  "89840400000000000099",
			DeviceIMEI:             "867530900000099",
			Operator:               "Viettel Mobile",
			HomeOperatorCode:       "45204",
			HomeOperatorName:       "Viettel Mobile",
			ServingOperatorCode:    "44010",
			ServingOperatorName:    "NTT DOCOMO",
			RegistrationStateKnown: true,
			RegistrationStateCode:  5,
			RegistrationState:      "roaming",
			Roaming:                true,
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
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body devicesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode devices: %v", err)
	}
	if len(body.Devices) != 1 || body.Devices[0].SIM == nil {
		t.Fatalf("devices = %+v, want one SIM-backed device", body.Devices)
	}
	sim := body.Devices[0].SIM
	if sim.Operator != "Viettel Mobile" ||
		sim.HomeOperatorCode != "45204" ||
		sim.HomeOperatorName != "Viettel Mobile" ||
		sim.ServingOperatorCode != "44010" ||
		sim.ServingOperatorName != "NTT DOCOMO" ||
		!sim.RegistrationStateKnown ||
		sim.RegistrationStateCode != 5 ||
		sim.RegistrationState != "roaming" ||
		sim.RegStatus != 5 ||
		sim.RegStatusText != "roaming" ||
		!sim.Roaming {
		t.Fatalf("SIM network state = %+v", sim)
	}
	if repository.devices[0].SIM.ServingOperatorName != "" {
		t.Fatalf("repository-owned SIM was mutated: %+v", repository.devices[0].SIM)
	}
}

func TestBootstrapDoesNotMergePersistedLineLabelByIMSI(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{{
		ID:         "line-persisted",
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
	if body.Lines[0].LineLabel != "" {
		t.Fatalf("line label = %q, want no metadata merged across stable line IDs", body.Lines[0].LineLabel)
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
		{name: "missing message line identity", method: http.MethodGet, path: "/api/v1/messages?peer=%2B15550100", wantStatus: 400, wantCode: "invalid_argument"},
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

func TestWriteInternalErrorIgnoresCanceledRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context context.Context
		err     error
	}{
		{
			name:    "wrapped cancellation",
			context: context.Background(),
			err:     errors.Join(errors.New("scan message thread"), context.Canceled),
		},
		{
			name: "canceled request context",
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			err: errors.New("repository operation stopped"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logs := diagnostics.NewLogBuffer(8)
			api := &API{logger: slog.New(logs.Handler(nil))}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/threads", nil).
				WithContext(test.context)
			response := httptest.NewRecorder()
			response.Code = 0

			api.writeInternalError(response, request, "list message threads", test.err)

			if response.Code != 0 || response.Body.Len() != 0 {
				t.Fatalf("canceled request response = %d %q, want no response", response.Code, response.Body.String())
			}
			if entries := logs.Snapshot(0).Entries; len(entries) != 0 {
				t.Fatalf("canceled request logs = %+v, want none", entries)
			}
		})
	}
}

func TestWriteInternalErrorPreservesRealFailures(t *testing.T) {
	t.Parallel()

	logs := diagnostics.NewLogBuffer(8)
	api := &API{logger: slog.New(logs.Handler(nil))}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/threads", nil)
	response := httptest.NewRecorder()

	api.writeInternalError(
		response,
		request,
		"list message threads",
		context.DeadlineExceeded,
	)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	entries := logs.Snapshot(0).Entries
	if len(entries) != 1 || entries[0].Message != "list message threads" {
		t.Fatalf("failure logs = %+v, want one internal error", entries)
	}
}
