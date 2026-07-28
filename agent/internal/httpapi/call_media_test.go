package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeCallMediaProvider struct {
	*fakeProvider
	request domain.CallCommandRequest
	result  domain.CallMediaActivation
	err     error
}

func (provider *fakeCallMediaProvider) ActivateCallMedia(
	_ context.Context,
	request domain.CallCommandRequest,
) (domain.CallMediaActivation, error) {
	provider.request = request
	return provider.result, provider.err
}

func TestActivateCallMediaRoutesExplicitCommand(t *testing.T) {
	provider := &fakeCallMediaProvider{
		fakeProvider: &fakeProvider{},
		result: domain.CallMediaActivation{
			CallID:         "call-1",
			MediaRouting:   "enabled",
			MediaAvailable: true,
			AudioPort:      "quectel-uac:/sys/devices/usb1/1-2",
			AudioFormat: &domain.CallAudioFormat{
				Encoding:   "pcm",
				Resolution: "s16le",
				Rate:       8000,
			},
		},
	}
	recorder := performRequest(
		New(provider, "test"),
		http.MethodPost,
		"/v1/calls/call-1/media/activate",
		[]byte(`{"request_id":"media-request-1"}`),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if provider.request.CallID != "call-1" ||
		provider.request.RequestID != "media-request-1" {
		t.Fatalf("request = %+v", provider.request)
	}
	var activation domain.CallMediaActivation
	decodeResponse(t, recorder, &activation)
	if activation.CallID != "call-1" ||
		!activation.MediaAvailable ||
		activation.MediaConfigured {
		t.Fatalf("activation = %+v", activation)
	}
}

func TestActivateCallMediaRejectsProviderWithoutActivator(t *testing.T) {
	recorder := performRequest(
		New(&fakeProvider{}, "test"),
		http.MethodPost,
		"/v1/calls/call-1/media/activate",
		[]byte(`{"request_id":"media-request-1"}`),
	)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
