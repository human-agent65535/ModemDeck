package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/communication"
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
	active       []store.Call
	activeError  error
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
	service.actionInput = input
	return service.call, service.actionError
}

func (service *fakeCommunications) ActiveCalls(context.Context) ([]store.Call, error) {
	return service.active, service.activeError
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
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls",
		bytes.NewBufferString(`{"request_id":"body-id","line_id":"line-1","number":"+818012345678"}`),
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
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	activeResponse := httptest.NewRecorder()
	api.ServeHTTP(activeResponse, httptest.NewRequest(http.MethodGet, "/api/v1/calls/active", nil))
	if activeResponse.Code != http.StatusOK {
		t.Fatalf("active status = %d; body = %s", activeResponse.Code, activeResponse.Body.String())
	}
	var active activeCallsResponse
	if err := json.Unmarshal(activeResponse.Body.Bytes(), &active); err != nil ||
		len(active.Calls) != 1 || active.Calls[0].Bearer != "volte" ||
		active.Calls[0].LineID != "line-stable" {
		t.Fatalf("active response = %+v, error = %v", active, err)
	}

	actionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-app-1/dtmf",
		bytes.NewBufferString(`{"request_id":"request-dtmf-1","digits":"12#"}`),
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
				disableAuthentication: true,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/calls",
				bytes.NewBufferString(`{"line_id":"line-1","number":"+818012345678"}`),
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
