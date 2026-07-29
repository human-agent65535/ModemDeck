package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeCommunications struct {
	status       communication.Status
	statusError  error
	message      store.Message
	messageInput communication.SendMessageInput
	messageError error
	call         store.Call
	startInput   communication.StartCallInput
	startError   error
	actionInput  communication.CallActionInput
	actionError  error
	actionCalls  int
	active       []store.Call
	activeError  error
	endCallID    string
	endCallError error
}

func (service *fakeCommunications) Status(context.Context) (communication.Status, error) {
	return service.status, service.statusError
}

func (service *fakeCommunications) SendMessage(
	_ context.Context,
	input communication.SendMessageInput,
) (store.Message, error) {
	service.messageInput = input
	return service.message, service.messageError
}

func (service *fakeCommunications) StartCall(
	_ context.Context,
	input communication.StartCallInput,
) (store.Call, error) {
	service.startInput = input
	return service.call, service.startError
}

func (service *fakeCommunications) CallAction(
	_ context.Context,
	input communication.CallActionInput,
) (store.Call, error) {
	service.actionCalls++
	service.actionInput = input
	return service.call, service.actionError
}

func (service *fakeCommunications) ActiveCalls(context.Context) ([]store.Call, error) {
	return service.active, service.activeError
}

func (service *fakeCommunications) EndCall(_ context.Context, callID string) error {
	service.endCallID = callID
	return service.endCallError
}

func TestMessageCommandForwardsExplicitLineAndIdempotencyKey(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{message: store.Message{
		ID:        7,
		LineID:    "line-1",
		Peer:      "+818012345678",
		Content:   "hello",
		Direction: "outgoing",
		State:     "sending",
	}}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/messages",
		bytes.NewBufferString(`{"line_id":"line-1","to":"+818012345678","content":"hello"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-message-1")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if communications.messageInput.RequestID != "request-message-1" ||
		communications.messageInput.LineID != "line-1" ||
		communications.messageInput.Number != "+818012345678" ||
		communications.messageInput.Text != "hello" {
		t.Fatalf("message input = %+v", communications.messageInput)
	}
	var body messageResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Message.ID != 7 {
		t.Fatalf("message response = %+v, error = %v", body, err)
	}
}

func TestMessageReadUsesExactLineAndPeer(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/messages/read",
		bytes.NewBufferString(`{"line_id":"  line-main  ","peer":"  +818012345678  "}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if repository.messageReadIdentity.LineID != "line-main" ||
		repository.messageReadIdentity.Peer != "+818012345678" {
		t.Fatalf(
			"message read identity = %q %q",
			repository.messageReadIdentity.LineID,
			repository.messageReadIdentity.Peer,
		)
	}
}

func TestMessageThreadDeleteUsesExactIdentityAndPublishesRuntimeEvent(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	events := runtimeevents.NewBuffer(8)
	api, err := New(repository, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/messages/threads",
		bytes.NewBufferString(`{"line_id":"  line-main  ","peer":"  +818012345678  "}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if repository.messageDeleteIdentity.LineID != "line-main" ||
		repository.messageDeleteIdentity.Peer != "+818012345678" {
		t.Fatalf("message deletion identity = %+v", repository.messageDeleteIdentity)
	}
	window, _, cancel := events.Subscribe(0)
	cancel()
	if len(window.Events) != 1 ||
		len(window.Events[0].Resources) != 1 ||
		window.Events[0].Resources[0] != runtimeevents.ResourceMessages {
		t.Fatalf("runtime events = %+v", window.Events)
	}
}

func TestMessageThreadStateUpdatesMultipleExactIdentities(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	events := runtimeevents.NewBuffer(8)
	api, err := New(repository, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/messages/threads/state",
		bytes.NewBufferString(
			`{"action":"favorite","threads":[{"line_id":" line-main ","peer":" +818012345678 "},{"line_id":"line-travel","peer":"+84900000000"}]}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if repository.messageUpdateAction != store.MessageThreadFavorite {
		t.Fatalf("action = %q, want favorite", repository.messageUpdateAction)
	}
	want := []store.MessageThreadIdentity{
		{LineID: "line-main", Peer: "+818012345678"},
		{LineID: "line-travel", Peer: "+84900000000"},
	}
	if len(repository.messageUpdateThreads) != len(want) {
		t.Fatalf("threads = %+v, want %+v", repository.messageUpdateThreads, want)
	}
	for index := range want {
		if repository.messageUpdateThreads[index] != want[index] {
			t.Fatalf("thread %d = %+v, want %+v", index, repository.messageUpdateThreads[index], want[index])
		}
	}
	window, _, cancel := events.Subscribe(0)
	cancel()
	if len(window.Events) != 1 ||
		len(window.Events[0].Resources) != 1 ||
		window.Events[0].Resources[0] != runtimeevents.ResourceMessages {
		t.Fatalf("runtime events = %+v", window.Events)
	}
}

func TestMissedCallsReadPersistsThroughRepository(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/calls/missed/read", nil)
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if repository.missedReadCalls != 1 {
		t.Fatalf("MarkMissedCallsRead() calls = %d, want 1", repository.missedReadCalls)
	}
}

func TestMissedCallsReadRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()

	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/calls/missed/read", nil),
	)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body = %s", response.Code, response.Body.String())
	}
	if repository.missedReadCalls != 0 {
		t.Fatalf("MarkMissedCallsRead() calls = %d, want 0", repository.missedReadCalls)
	}
}

func TestSingleMissedCallReadAndCallDeletionUseExactCallID(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	recordings := &fakeRecordingService{}
	api, err := New(repository, Options{
		Recording:             recordings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	readResponse := httptest.NewRecorder()
	api.ServeHTTP(
		readResponse,
		httptest.NewRequest(http.MethodPatch, "/api/v1/calls/call-history/read", nil),
	)
	if readResponse.Code != http.StatusNoContent {
		t.Fatalf("read status = %d; body = %s", readResponse.Code, readResponse.Body.String())
	}
	if len(repository.missedReadIDs) != 1 ||
		repository.missedReadIDs[0] != "call-history" {
		t.Fatalf("missed read IDs = %+v", repository.missedReadIDs)
	}

	deleteResponse := httptest.NewRecorder()
	api.ServeHTTP(
		deleteResponse,
		httptest.NewRequest(http.MethodDelete, "/api/v1/calls/call-history", nil),
	)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf(
			"delete status = %d; body = %s",
			deleteResponse.Code,
			deleteResponse.Body.String(),
		)
	}
	if recordings.deleteCallID != "call-history" {
		t.Fatalf("deleted call ID = %q", recordings.deleteCallID)
	}
}

func TestCallsBatchMarksUnreadAndDeduplicatesIDs(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/calls/batch",
		bytes.NewBufferString(
			`{"action":"unread","ids":[" call-one ","call-one","call-two"]}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if len(repository.missedUnreadIDs) != 2 ||
		repository.missedUnreadIDs[0] != "call-one" ||
		repository.missedUnreadIDs[1] != "call-two" {
		t.Fatalf("missed unread IDs = %+v", repository.missedUnreadIDs)
	}
}

func TestCallsBatchFavoritesDeduplicateIDsAndPersistTheRequestedState(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/calls/batch",
		bytes.NewBufferString(
			`{"action":"favorite","ids":[" call-one ","call-one","call-two"]}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if !repository.callFavorite ||
		len(repository.callFavoriteIDs) != 2 ||
		repository.callFavoriteIDs[0] != "call-one" ||
		repository.callFavoriteIDs[1] != "call-two" {
		t.Fatalf(
			"call favorite update = favorite %v IDs %+v",
			repository.callFavorite,
			repository.callFavoriteIDs,
		)
	}
}

func TestCallsBatchDeleteCascadesThroughRecordingService(t *testing.T) {
	t.Parallel()

	recordings := &fakeRecordingService{}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/calls/batch",
		bytes.NewBufferString(
			`{"action":"delete","ids":["call-one","call-two"]}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
	}
	if len(recordings.deleteCallIDs) != 2 ||
		recordings.deleteCallIDs[0] != "call-one" ||
		recordings.deleteCallIDs[1] != "call-two" {
		t.Fatalf("deleted call IDs = %+v", recordings.deleteCallIDs)
	}
}

func TestMessagesUseStableLineIdentity(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/messages?line_id=line-main&peer=%2B818012345678",
		nil,
	)
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if repository.messageQuery.LineID != "line-main" ||
		repository.messageQuery.Peer != "+818012345678" ||
		!repository.messageQuery.Chronological {
		t.Fatalf("message query = %+v", repository.messageQuery)
	}
}

func TestMessageReadRejectsLegacyHardwareIdentity(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/messages/read",
		bytes.NewBufferString(
			`{"local_phone":"  +81 90 1234 5678  ","iccid":"stale-iccid","peer":"  +818012345678  "}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
	if repository.messageReadIdentity != (store.MessageThreadIdentity{}) {
		t.Fatalf("repository was called with legacy identity: %+v", repository.messageReadIdentity)
	}
}

func TestMessageReadReportsIdentityFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		storeError error
		wantStatus int
		wantCode   string
	}{{
		name:       "missing exact thread",
		storeError: store.ErrMessageThreadNotFound,
		wantStatus: http.StatusNotFound,
		wantCode:   "message_thread_not_found",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{messageReadError: test.storeError}
			api, err := New(repository, Options{disableAuthentication: true})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := httptest.NewRequest(
				http.MethodPatch,
				"/api/v1/messages/read",
				bytes.NewBufferString(`{"line_id":"line-main","peer":"+818012345678"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			api.ServeHTTP(response, request)

			assertAPIError(t, response, test.wantStatus, test.wantCode)
		})
	}
}

func TestCommandRejectsConflictingRequestIdentities(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls",
		bytes.NewBufferString(`{"request_id":"body-id","line_id":"line-1","number":"+818012345678","holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "header-id")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusBadRequest, "invalid_argument")
	if communications.startInput.LineID != "" {
		t.Fatalf("communication service was invoked with %+v", communications.startInput)
	}
}

func TestCallControlAndActiveCallRoutes(t *testing.T) {
	t.Parallel()

	call := store.Call{
		ID:             "call-app-1",
		LineID:         "line-stable",
		EndpointLineID: "endpoint-1",
		Direction:      "incoming",
		RemoteNumber:   "+818012345678",
		Phase:          "active",
		CreatedAt:      "2026-07-23T12:00:00Z",
		Bearer:         "volte",
	}
	communications := &fakeCommunications{call: call, active: []store.Call{call}}
	leases := &fakeCallLeases{control: calllease.ControlOwned}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	activeResponse := httptest.NewRecorder()
	api.ServeHTTP(
		activeResponse,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/calls/active?holder_id=browser-1",
			nil,
		),
	)
	if activeResponse.Code != http.StatusOK {
		t.Fatalf("active status = %d; body = %s", activeResponse.Code, activeResponse.Body.String())
	}
	var active activeCallsResponse
	if err := json.Unmarshal(activeResponse.Body.Bytes(), &active); err != nil ||
		len(active.Calls) != 1 || active.Calls[0].Bearer != "volte" ||
		active.Calls[0].LineID != "line-stable" ||
		active.Calls[0].ControlState != string(calllease.ControlOwned) {
		t.Fatalf("active response = %+v, error = %v", active, err)
	}
	if leases.reads != 1 ||
		leases.callID != "call-app-1" ||
		leases.holderID != "browser-1" {
		t.Fatalf("ownership read = %+v", leases)
	}

	actionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-app-1/dtmf",
		bytes.NewBufferString(`{"request_id":"request-dtmf-1","digits":"12#","holder_id":"browser-1"}`),
	)
	actionRequest.Header.Set("Content-Type", "application/json")
	actionResponse := httptest.NewRecorder()
	api.ServeHTTP(actionResponse, actionRequest)
	if actionResponse.Code != http.StatusAccepted {
		t.Fatalf("action status = %d; body = %s", actionResponse.Code, actionResponse.Body.String())
	}
	var accepted map[string]string
	if err := json.Unmarshal(actionResponse.Body.Bytes(), &accepted); err != nil ||
		accepted["request_id"] != "request-dtmf-1" ||
		accepted["call_id"] != "call-app-1" {
		t.Fatalf("accepted action = %+v, error = %v", accepted, err)
	}
	if communications.actionInput.CallID != "call-app-1" ||
		communications.actionInput.Action != "dtmf" ||
		communications.actionInput.Digits != "12#" ||
		communications.actionInput.RequestID != "request-dtmf-1" {
		t.Fatalf("action input = %+v", communications.actionInput)
	}
}

func TestActiveCallReportsOccupiedToAnotherBrowser(t *testing.T) {
	t.Parallel()

	call := store.Call{
		ID:           "call-app-1",
		LineID:       "line-stable",
		Direction:    "incoming",
		RemoteNumber: "+818012345678",
		Phase:        "active",
	}
	leases := &fakeCallLeases{control: calllease.ControlOccupied}
	api, err := New(&fakeRepository{}, Options{
		Communications:        &fakeCommunications{active: []store.Call{call}},
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/calls/active?holder_id=browser-2",
			nil,
		),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var body activeCallsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Calls) != 1 ||
		body.Calls[0].ControlState != string(calllease.ControlOccupied) {
		t.Fatalf("active calls = %+v", body.Calls)
	}
	if leases.holderID != "browser-2" {
		t.Fatalf("ownership checked for holder %q", leases.holderID)
	}
}

func TestSecondBrowserCannotAnswerClaimedIncomingCall(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{}
	leases := &fakeCallLeases{err: calllease.ErrCallOwned}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-1/answer",
		bytes.NewBufferString(
			`{"request_id":"request-answer-2","holder_id":"browser-2"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusConflict, "line_in_use")
	if leases.claims != 1 {
		t.Fatalf("claim calls = %d, want 1", leases.claims)
	}
	if communications.actionInput.CallID != "" {
		t.Fatalf("second answer reached modem control: %+v", communications.actionInput)
	}
}

func TestStartCallClaimsTheDialingBrowser(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{call: store.Call{
		ID:           "call-app-1",
		LineID:       "line-stable",
		Direction:    "outgoing",
		RemoteNumber: "+818012345678",
		Phase:        "dialing",
	}}
	leases := &fakeCallLeases{}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls",
		bytes.NewBufferString(
			`{"line_id":"line-stable","number":"+818012345678","holder_id":"browser-1"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if leases.claims != 1 ||
		leases.callID != "call-app-1" ||
		leases.holderID != "browser-1" {
		t.Fatalf("claimed lease = %+v", leases)
	}
	var body callSessionEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Call.ControlState != string(calllease.ControlOwned) {
		t.Fatalf("control state = %q", body.Call.ControlState)
	}
}

func TestStartCallClaimFailureEndsOnlyCreatedCall(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{call: store.Call{
		ID:        "call-app-2",
		LineID:    "line-stable-2",
		Direction: "outgoing",
		Phase:     "dialing",
	}}
	leases := &fakeCallLeases{err: calllease.ErrCallNotActive}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls",
		bytes.NewBufferString(
			`{"line_id":"line-stable-2","number":"+818012345678","holder_id":"browser-1"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if communications.endCallID != "call-app-2" {
		t.Fatalf("ended call = %q, want call-app-2", communications.endCallID)
	}
}

func TestIncomingAnswerIsFirstClaimWins(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{}
	leases := &fakeCallLeases{err: calllease.ErrCallOwned}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-app-1/answer",
		bytes.NewBufferString(
			`{"request_id":"request-answer-1","holder_id":"browser-2"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusConflict, "line_in_use")
	if leases.claims != 1 {
		t.Fatalf("claim calls = %d, want 1", leases.claims)
	}
	if communications.actionCalls != 0 {
		t.Fatalf("modem answer calls = %d, want 0", communications.actionCalls)
	}
}

func TestFailedIncomingAnswerReleasesBrowserClaim(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{
		actionError: errors.New("modem answer failed"),
	}
	leases := &fakeCallLeases{}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-app-1/answer",
		bytes.NewBufferString(
			`{"request_id":"request-answer-1","holder_id":"browser-1"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if leases.claims != 1 || leases.releases != 1 {
		t.Fatalf("lease operations = claims %d, releases %d", leases.claims, leases.releases)
	}
	if communications.actionCalls != 1 {
		t.Fatalf("modem answer calls = %d, want 1", communications.actionCalls)
	}
}

func TestNonOwnerCannotControlActiveCall(t *testing.T) {
	t.Parallel()

	communications := &fakeCommunications{}
	leases := &fakeCallLeases{err: calllease.ErrNotOwner}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-app-1/hangup",
		bytes.NewBufferString(
			`{"request_id":"request-hangup-1","holder_id":"browser-2"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusConflict, "call_not_owned")
	if leases.requires != 1 {
		t.Fatalf("ownership checks = %d, want 1", leases.requires)
	}
	if communications.actionCalls != 0 {
		t.Fatalf("modem hangup calls = %d, want 0", communications.actionCalls)
	}
}

func TestCommunicationErrorsHaveStableHTTPMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "unsupported",
			err:        &communication.Error{Code: communication.CodeNotSupported, Message: "unsupported"},
			wantStatus: http.StatusNotImplemented,
			wantCode:   "not_supported",
		},
		{
			name:       "unavailable",
			err:        &communication.Error{Code: communication.CodeUnavailable, Message: "unavailable"},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "communications_unavailable",
		},
		{
			name:       "failed precondition",
			err:        &communication.Error{Code: communication.CodeFailedPrecondition, Message: "call is not active"},
			wantStatus: http.StatusPreconditionFailed,
			wantCode:   "failed_precondition",
		},
		{
			name:       "network rejected",
			err:        &communication.Error{Code: communication.CodeNetworkRejected, Message: "network rejected"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "network_rejected",
		},
		{
			name:       "unknown internal",
			err:        errors.New("secret dependency failure"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			communications := &fakeCommunications{startError: test.err}
			api, err := New(&fakeRepository{}, Options{
				Communications:        communications,
				CallLeases:            &fakeCallLeases{},
				disableAuthentication: true,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/calls",
				bytes.NewBufferString(`{"line_id":"line-1","number":"+818012345678","holder_id":"browser-1"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			assertAPIError(t, response, test.wantStatus, test.wantCode)
			if test.wantCode == "internal_error" &&
				bytes.Contains(response.Body.Bytes(), []byte("secret dependency failure")) {
				t.Fatalf("internal dependency error leaked: %s", response.Body.String())
			}
		})
	}
}
