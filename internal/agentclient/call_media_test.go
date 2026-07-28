package agentclient

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestActivateCallMediaUsesExplicitAgentEndpoint(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost ||
			request.URL.Path != "/v1/calls/call-1/media/activate" {
			http.NotFound(response, request)
			return
		}
		var body CallActionRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.RequestID != "media-request-1" {
			t.Fatalf("request = %+v", body)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"call_id":"call-1",
			"media_routing":"enabled",
			"media_available":true,
			"media_configured":true,
			"audio_port":"quectel-uac:/sys/devices/usb1/1-2",
			"audio_format":{"encoding":"pcm","resolution":"s16le","rate":8000}
		}`))
	}))

	activation, err := client.ActivateCallMedia(
		t.Context(),
		"call-1",
		CallActionRequest{RequestID: "media-request-1"},
	)
	if err != nil {
		t.Fatalf("ActivateCallMedia() error = %v", err)
	}
	if activation.CallID != "call-1" ||
		activation.MediaRouting != "enabled" ||
		!activation.MediaAvailable ||
		!activation.MediaConfigured ||
		activation.AudioFormat == nil ||
		activation.AudioFormat.Rate != 8000 {
		t.Fatalf("activation = %+v", activation)
	}
}
