package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeProvider struct {
	health          domain.ProviderHealth
	lines           []domain.Line
	mutationError   error
	startRequests   []domain.StartCallRequest
	answerCallIDs   []string
	hangupCallIDs   []string
	messageRequests []domain.SendMessageRequest
}

func (p *fakeProvider) Health(context.Context) (domain.ProviderHealth, error) {
	return p.health, nil
}

func (p *fakeProvider) Lines(context.Context) ([]domain.Line, error) {
	return p.lines, nil
}

func (p *fakeProvider) StartCall(_ context.Context, request domain.StartCallRequest) (domain.Call, error) {
	p.startRequests = append(p.startRequests, request)
	if p.mutationError != nil {
		return domain.Call{}, p.mutationError
	}
	return domain.Call{ID: "call-1", LineID: request.LineID, Number: request.Number, State: "created"}, nil
}

func (p *fakeProvider) AnswerCall(_ context.Context, id string) (domain.Call, error) {
	p.answerCallIDs = append(p.answerCallIDs, id)
	if p.mutationError != nil {
		return domain.Call{}, p.mutationError
	}
	return domain.Call{ID: id, State: "active"}, nil
}

func (p *fakeProvider) HangupCall(_ context.Context, id string) (domain.Call, error) {
	p.hangupCallIDs = append(p.hangupCallIDs, id)
	if p.mutationError != nil {
		return domain.Call{}, p.mutationError
	}
	return domain.Call{ID: id, State: "terminated"}, nil
}

func (p *fakeProvider) SendMessage(_ context.Context, request domain.SendMessageRequest) (domain.Message, error) {
	p.messageRequests = append(p.messageRequests, request)
	if p.mutationError != nil {
		return domain.Message{}, p.mutationError
	}
	return domain.Message{ID: "message-1", LineID: request.LineID, Number: request.Number, State: "created"}, nil
}

func TestHealthReportsDiscoveryWithoutMutationCapabilities(t *testing.T) {
	provider := &fakeProvider{health: domain.ProviderHealth{
		Name:      "org.freedesktop.ModemManager1",
		Available: true,
		Capabilities: domain.AgentCapabilities{
			Discovery: true,
		},
	}}
	recorder := performRequest(New(provider, "test-version"), http.MethodGet, "/v1/health", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response healthResponse
	decodeResponse(t, recorder, &response)
	if response.Status != "ok" || response.APIVersion != "v1" || response.AgentVersion != "test-version" {
		t.Fatalf("unexpected health response: %+v", response)
	}
	capabilities := response.Provider.Capabilities
	if !capabilities.Discovery {
		t.Fatal("discovery should be true")
	}
	if capabilities.Dial || capabilities.AnswerCall || capabilities.HangupCall || capabilities.SendMessage {
		t.Fatalf("unimplemented mutations were advertised: %+v", capabilities)
	}
}

func TestLinesReturnsFakeProviderData(t *testing.T) {
	provider := &fakeProvider{lines: []domain.Line{{
		ID:    "/org/freedesktop/ModemManager1/Modem/0",
		State: "registered",
	}}}
	recorder := performRequest(New(provider, "test"), http.MethodGet, "/v1/lines", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response linesResponse
	decodeResponse(t, recorder, &response)
	if len(response.Lines) != 1 || response.Lines[0].State != "registered" {
		t.Fatalf("unexpected lines response: %+v", response)
	}
}

func TestMutationRoutesUseProviderContract(t *testing.T) {
	provider := &fakeProvider{}
	handler := New(provider, "test")

	start := performRequest(handler, http.MethodPost, "/v1/calls", []byte(`{
		"line_id":" /org/freedesktop/ModemManager1/Modem/0 ",
		"number":" +818012345678 "
	}`))
	if start.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body = %s", start.Code, start.Body.String())
	}
	if len(provider.startRequests) != 1 || provider.startRequests[0].LineID != "/org/freedesktop/ModemManager1/Modem/0" || provider.startRequests[0].Number != "+818012345678" {
		t.Fatalf("unexpected start request: %#v", provider.startRequests)
	}

	answer := performRequest(handler, http.MethodPost, "/v1/calls/call-1/answer", []byte(`{}`))
	if answer.Code != http.StatusOK || len(provider.answerCallIDs) != 1 || provider.answerCallIDs[0] != "call-1" {
		t.Fatalf("answer status/body/ids = %d %s %#v", answer.Code, answer.Body.String(), provider.answerCallIDs)
	}

	hangup := performRequest(handler, http.MethodPost, "/v1/calls/call-1/hangup", []byte(`{}`))
	if hangup.Code != http.StatusOK || len(provider.hangupCallIDs) != 1 || provider.hangupCallIDs[0] != "call-1" {
		t.Fatalf("hangup status/body/ids = %d %s %#v", hangup.Code, hangup.Body.String(), provider.hangupCallIDs)
	}

	message := performRequest(handler, http.MethodPost, "/v1/messages", []byte(`{
		"line_id":"/org/freedesktop/ModemManager1/Modem/0",
		"number":"+818012345678",
		"text":"hello"
	}`))
	if message.Code != http.StatusCreated || len(provider.messageRequests) != 1 || provider.messageRequests[0].Text != "hello" {
		t.Fatalf("message status/body/requests = %d %s %#v", message.Code, message.Body.String(), provider.messageRequests)
	}
}

func TestNotSupportedErrorIsStructured(t *testing.T) {
	provider := &fakeProvider{mutationError: domain.NotSupported("start_call")}
	recorder := performRequest(
		New(provider, "test"),
		http.MethodPost,
		"/v1/calls",
		[]byte(`{"line_id":"line-1","number":"+818012345678"}`),
	)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response errorBody
	decodeResponse(t, recorder, &response)
	if response.Error.Code != domain.ErrorNotSupported || response.Error.Operation != "start_call" {
		t.Fatalf("unexpected error response: %+v", response)
	}
}

func TestStrictJSONRejectsUnknownFieldsBeforeProvider(t *testing.T) {
	provider := &fakeProvider{}
	recorder := performRequest(
		New(provider, "test"),
		http.MethodPost,
		"/v1/messages",
		[]byte(`{"line_id":"line-1","number":"1","text":"hello","fake_success":true}`),
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(provider.messageRequests) != 0 {
		t.Fatalf("provider was called with invalid request: %#v", provider.messageRequests)
	}
}

func performRequest(handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}
