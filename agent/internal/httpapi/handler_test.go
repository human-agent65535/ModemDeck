package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/media"
)

type fakeProvider struct {
	health          domain.ProviderHealth
	snapshot        domain.Snapshot
	operationError  error
	contexts        []context.Context
	startRequests   []domain.StartCallRequest
	answerRequests  []domain.CallCommandRequest
	rejectRequests  []domain.CallCommandRequest
	hangupRequests  []domain.CallCommandRequest
	dtmfRequests    []domain.DTMFRequest
	messageRequests []domain.SendMessageRequest
	deletedMessages []domain.DeleteMessageRequest
}

type telemetryFakeProvider struct {
	*fakeProvider
	telemetry domain.TelemetrySnapshot
}

func (p *telemetryFakeProvider) Telemetry(
	ctx context.Context,
) (domain.TelemetrySnapshot, error) {
	p.contexts = append(p.contexts, ctx)
	if p.operationError != nil {
		return domain.TelemetrySnapshot{}, p.operationError
	}
	return p.telemetry, nil
}

func (p *fakeProvider) Health(ctx context.Context) (domain.ProviderHealth, error) {
	p.contexts = append(p.contexts, ctx)
	if p.operationError != nil {
		return domain.ProviderHealth{}, p.operationError
	}
	return p.health, nil
}

func (p *fakeProvider) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	p.contexts = append(p.contexts, ctx)
	if p.operationError != nil {
		return domain.Snapshot{}, p.operationError
	}
	return p.snapshot, nil
}

func (p *fakeProvider) StartCall(ctx context.Context, request domain.StartCallRequest) (domain.CommandReceipt, error) {
	p.contexts = append(p.contexts, ctx)
	p.startRequests = append(p.startRequests, request)
	if p.operationError != nil {
		return domain.CommandReceipt{}, p.operationError
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: "call_boot_x"}, nil
}

func (p *fakeProvider) AnswerCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	p.contexts = append(p.contexts, ctx)
	p.answerRequests = append(p.answerRequests, request)
	return p.callReceipt(request)
}

func (p *fakeProvider) RejectCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	p.contexts = append(p.contexts, ctx)
	p.rejectRequests = append(p.rejectRequests, request)
	return p.callReceipt(request)
}

func (p *fakeProvider) HangupCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	p.contexts = append(p.contexts, ctx)
	p.hangupRequests = append(p.hangupRequests, request)
	return p.callReceipt(request)
}

func (p *fakeProvider) SendDTMF(ctx context.Context, request domain.DTMFRequest) (domain.CommandReceipt, error) {
	p.contexts = append(p.contexts, ctx)
	p.dtmfRequests = append(p.dtmfRequests, request)
	if p.operationError != nil {
		return domain.CommandReceipt{}, p.operationError
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: request.CallID}, nil
}

func (p *fakeProvider) SendMessage(ctx context.Context, request domain.SendMessageRequest) (domain.CommandReceipt, error) {
	p.contexts = append(p.contexts, ctx)
	p.messageRequests = append(p.messageRequests, request)
	if p.operationError != nil {
		return domain.CommandReceipt{}, p.operationError
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: "message_boot_x"}, nil
}

func (p *fakeProvider) DeleteMessage(ctx context.Context, request domain.DeleteMessageRequest) error {
	p.contexts = append(p.contexts, ctx)
	p.deletedMessages = append(p.deletedMessages, request)
	return p.operationError
}

func (p *fakeProvider) callReceipt(request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	if p.operationError != nil {
		return domain.CommandReceipt{}, p.operationError
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: request.CallID}, nil
}

func TestHealthReportsCapabilitiesAndBootEpoch(t *testing.T) {
	provider := &fakeProvider{health: domain.ProviderHealth{
		Name:           "org.freedesktop.ModemManager1",
		Available:      true,
		BootEpoch:      "boot-1",
		RuntimeVersion: "1.26.0",
		Capabilities: domain.AgentCapabilities{
			Discovery:   true,
			Snapshot:    true,
			Dial:        true,
			AnswerCall:  true,
			RejectCall:  true,
			HangupCall:  true,
			SendDTMF:    true,
			SendMessage: true,
		},
	}}
	recorder := performRequest(New(provider, "test-version"), http.MethodGet, "/v1/health", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response healthResponse
	decodeResponse(t, recorder, &response)
	if response.Status != "ok" || response.APIVersion != "v1" ||
		response.AgentVersion != "test-version" || response.Provider.BootEpoch != "boot-1" ||
		response.Provider.RuntimeVersion != "1.26.0" {
		t.Fatalf("unexpected health response: %+v", response)
	}
	if !response.Provider.Capabilities.Snapshot || !response.Provider.Capabilities.SendDTMF {
		t.Fatalf("capabilities missing: %+v", response.Provider.Capabilities)
	}
}

func TestTelemetryReturnsLightweightRadioSnapshot(t *testing.T) {
	observedAt := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	provider := &telemetryFakeProvider{
		fakeProvider: &fakeProvider{},
		telemetry: domain.TelemetrySnapshot{
			BootEpoch:  "boot-1",
			ObservedAt: observedAt,
			Lines: []domain.LineTelemetry{{
				ID:                  "line-1",
				SignalQualityKnown:  true,
				SignalQualityRecent: true,
				SignalQuality:       72,
			}},
		},
	}
	recorder := performRequest(New(provider, "test-version"), http.MethodGet, "/v1/telemetry", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response domain.TelemetrySnapshot
	decodeResponse(t, recorder, &response)
	if response.BootEpoch != "boot-1" || !response.ObservedAt.Equal(observedAt) ||
		len(response.Lines) != 1 || response.Lines[0].SignalQuality != 72 {
		t.Fatalf("telemetry response = %+v", response)
	}
	healthRecorder := performRequest(New(provider, "test-version"), http.MethodGet, "/v1/health", nil)
	var health healthResponse
	decodeResponse(t, healthRecorder, &health)
	if !health.Provider.Capabilities.Telemetry {
		t.Fatalf("telemetry capability missing: %+v", health.Provider.Capabilities)
	}
}

func TestHealthAdvertisesMediaOnlyWithAConfiguredBinding(t *testing.T) {
	t.Parallel()

	source := media.CallSourceFunc(func(context.Context, string) (media.Call, error) {
		return media.Call{}, nil
	})
	empty, err := media.NewManager(source, nil, nil, media.Options{})
	if err != nil {
		t.Fatalf("NewManager(empty) error = %v", err)
	}
	configured, err := media.NewManager(
		source,
		[]media.Binding{{
			AudioPort: "audio-1",
			Backend:   media.BackendCharPCM,
			Endpoint:  "/dev/pcm0",
		}},
		map[media.BackendKind]media.Backend{
			media.BackendCharPCM: media.NewCharPCMBackend(nil),
		},
		media.Options{},
	)
	if err != nil {
		t.Fatalf("NewManager(configured) error = %v", err)
	}

	for name, manager := range map[string]*media.Manager{
		"empty":      empty,
		"configured": configured,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := performRequest(
				NewWithMedia(&fakeProvider{}, "test", manager),
				http.MethodGet,
				"/v1/health",
				nil,
			)
			var response healthResponse
			decodeResponse(t, recorder, &response)
			if response.Provider.Capabilities.Media != (name == "configured") {
				t.Fatalf("media capability = %v", response.Provider.Capabilities.Media)
			}
		})
	}
}

func TestSnapshotIsOnlyAuthoritativeReadRoute(t *testing.T) {
	observedAt := time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC)
	provider := &fakeProvider{snapshot: domain.Snapshot{
		Revision:   "sha256:abc",
		ObservedAt: observedAt,
		Lines:      []domain.Line{{ID: "line_x"}},
		Calls:      []domain.Call{{ID: "call_boot_x", LineID: "line_x", State: "active", StateCode: 4}},
		Messages:   []domain.Message{{ID: "message_boot_x", LineID: "line_x", State: "received", StateCode: 3}},
	}}
	handler := New(provider, "test")

	recorder := performRequest(handler, http.MethodGet, "/v1/snapshot", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response domain.Snapshot
	decodeResponse(t, recorder, &response)
	if response.Revision != "sha256:abc" || !response.ObservedAt.Equal(observedAt) ||
		len(response.Lines) != 1 || len(response.Calls) != 1 || len(response.Messages) != 1 {
		t.Fatalf("unexpected snapshot response: %+v", response)
	}

	legacy := performRequest(handler, http.MethodGet, "/v1/lines", nil)
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy read route status = %d, body = %s", legacy.Code, legacy.Body.String())
	}
}

func TestCommandRoutesRequireAndEchoRequestID(t *testing.T) {
	provider := &fakeProvider{}
	handler := New(provider, "test")

	start := performRequest(handler, http.MethodPost, "/v1/calls", []byte(`{
		"request_id":"request-start",
		"line_id":"line_x",
		"number":"+818012345678"
	}`))
	assertReceipt(t, start, http.StatusCreated, "request-start", "call_boot_x")

	answer := performRequest(handler, http.MethodPost, "/v1/calls/call_boot_x/answer", []byte(`{
		"request_id":"request-answer"
	}`))
	assertReceipt(t, answer, http.StatusOK, "request-answer", "call_boot_x")

	reject := performRequest(handler, http.MethodPost, "/v1/calls/call_boot_x/reject", []byte(`{
		"request_id":"request-reject"
	}`))
	assertReceipt(t, reject, http.StatusOK, "request-reject", "call_boot_x")

	hangup := performRequest(handler, http.MethodPost, "/v1/calls/call_boot_x/hangup", []byte(`{
		"request_id":"request-hangup"
	}`))
	assertReceipt(t, hangup, http.StatusOK, "request-hangup", "call_boot_x")

	dtmf := performRequest(handler, http.MethodPost, "/v1/calls/call_boot_x/dtmf", []byte(`{
		"request_id":"request-dtmf",
		"digits":"12#"
	}`))
	assertReceipt(t, dtmf, http.StatusOK, "request-dtmf", "call_boot_x")

	message := performRequest(handler, http.MethodPost, "/v1/messages", []byte(`{
		"request_id":"request-message",
		"line_id":"line_x",
		"number":"+818012345678",
		"text":"hello"
	}`))
	assertReceipt(t, message, http.StatusCreated, "request-message", "message_boot_x")

	deletedMessage := performRequest(
		handler,
		http.MethodDelete,
		"/v1/messages/message_boot_x",
		nil,
	)
	if deletedMessage.Code != http.StatusNoContent {
		t.Fatalf(
			"delete message status = %d, body = %s",
			deletedMessage.Code,
			deletedMessage.Body.String(),
		)
	}

	if len(provider.startRequests) != 1 || provider.startRequests[0].RequestID != "request-start" {
		t.Fatalf("start requests = %#v", provider.startRequests)
	}
	if len(provider.answerRequests) != 1 || provider.answerRequests[0].CallID != "call_boot_x" {
		t.Fatalf("answer requests = %#v", provider.answerRequests)
	}
	if len(provider.rejectRequests) != 1 || provider.rejectRequests[0].RequestID != "request-reject" {
		t.Fatalf("reject requests = %#v", provider.rejectRequests)
	}
	if len(provider.hangupRequests) != 1 || provider.hangupRequests[0].RequestID != "request-hangup" {
		t.Fatalf("hangup requests = %#v", provider.hangupRequests)
	}
	if len(provider.dtmfRequests) != 1 || provider.dtmfRequests[0].Digits != "12#" {
		t.Fatalf("DTMF requests = %#v", provider.dtmfRequests)
	}
	if len(provider.messageRequests) != 1 || provider.messageRequests[0].Text != "hello" {
		t.Fatalf("message requests = %#v", provider.messageRequests)
	}
	if len(provider.deletedMessages) != 1 ||
		provider.deletedMessages[0].MessageID != "message_boot_x" {
		t.Fatalf("deleted messages = %#v", provider.deletedMessages)
	}
}

func TestEveryCommandRouteRejectsMissingRequestIDBeforeProvider(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   []byte
	}{
		{name: "dial", target: "/v1/calls", body: []byte(`{"line_id":"line_x","number":"+818012345678"}`)},
		{name: "answer", target: "/v1/calls/call_x/answer", body: []byte(`{}`)},
		{name: "reject", target: "/v1/calls/call_x/reject", body: []byte(`{}`)},
		{name: "hangup", target: "/v1/calls/call_x/hangup", body: []byte(`{}`)},
		{name: "DTMF", target: "/v1/calls/call_x/dtmf", body: []byte(`{"digits":"1"}`)},
		{name: "message", target: "/v1/messages", body: []byte(`{"line_id":"line_x","number":"+818012345678","text":"hello"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{}
			recorder := performRequest(New(provider, "test"), http.MethodPost, test.target, test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if len(provider.contexts) != 0 {
				t.Fatalf("provider was called for missing request_id: %#v", provider.contexts)
			}
		})
	}
}

func TestHandlerPassesRequestContextToProvider(t *testing.T) {
	provider := &fakeProvider{}
	handler := New(provider, "test")
	ctx := context.WithValue(context.Background(), handlerContextKey{}, "request")
	request := httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(provider.contexts) != 1 || provider.contexts[0] != ctx {
		t.Fatalf("provider contexts = %#v", provider.contexts)
	}
}

func TestTypedProviderErrorsMapToHTTPAndEchoRequestID(t *testing.T) {
	tests := []struct {
		name   string
		code   domain.ErrorCode
		status int
	}{
		{name: "invalid", code: domain.ErrorInvalidArgument, status: http.StatusBadRequest},
		{name: "not found", code: domain.ErrorNotFound, status: http.StatusNotFound},
		{name: "conflict", code: domain.ErrorConflict, status: http.StatusConflict},
		{name: "unsupported", code: domain.ErrorNotSupported, status: http.StatusNotImplemented},
		{name: "permission", code: domain.ErrorPermissionDenied, status: http.StatusForbidden},
		{name: "failed precondition", code: domain.ErrorFailedPrecondition, status: http.StatusPreconditionFailed},
		{name: "network rejected", code: domain.ErrorNetworkRejected, status: http.StatusUnprocessableEntity},
		{name: "unavailable", code: domain.ErrorUnavailable, status: http.StatusServiceUnavailable},
		{name: "verification", code: domain.ErrorVerification, status: http.StatusBadGateway},
		{name: "internal", code: domain.ErrorInternal, status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{operationError: domain.NewOperationError(
				test.code,
				"start_call",
				"command failed",
				nil,
			)}
			recorder := performRequest(
				New(provider, "test"),
				http.MethodPost,
				"/v1/calls",
				[]byte(`{"request_id":"request-error","line_id":"line_x","number":"+818012345678"}`),
			)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var response errorBody
			decodeResponse(t, recorder, &response)
			if response.Error.Code != test.code || response.Error.Operation != "start_call" ||
				response.Error.RequestID != "request-error" {
				t.Fatalf("unexpected error response: %+v", response)
			}
		})
	}
}

func TestStrictJSONAndContentTypeRejectBeforeProvider(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			name:        "unknown field",
			contentType: "application/json",
			body:        []byte(`{"request_id":"x","line_id":"line_x","number":"1","text":"hello","fake_success":true}`),
		},
		{
			name:        "multiple objects",
			contentType: "application/json",
			body:        []byte(`{"request_id":"x","line_id":"line_x","number":"1","text":"hello"} {}`),
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        []byte(`{"request_id":"x","line_id":"line_x","number":"1","text":"hello"}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{}
			request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()
			New(provider, "test").ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if len(provider.messageRequests) != 0 {
				t.Fatalf("provider was called with invalid request: %#v", provider.messageRequests)
			}
		})
	}
}

type handlerContextKey struct{}

func performRequest(handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertReceipt(t *testing.T, recorder *httptest.ResponseRecorder, status int, requestID, resourceID string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var receipt domain.CommandReceipt
	decodeResponse(t, recorder, &receipt)
	if receipt.RequestID != requestID || receipt.ResourceID != resourceID {
		t.Fatalf("receipt = %+v", receipt)
	}
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}
