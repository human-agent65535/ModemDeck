package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestOrdinaryBusinessResponsesOmitEndpointIdentity(t *testing.T) {
	t.Parallel()

	call := store.Call{
		ID:             "call-1",
		LineID:         "line-main",
		EndpointLineID: "endpoint-line-1",
		EndpointID:     "endpoint-1",
		Direction:      "incoming",
		RemoteNumber:   "+818012345678",
	}
	responses := map[string]any{
		"bootstrap line": lineSummaryResponseFromStore(store.LineSummary{
			ID:         "line-main",
			EndpointID: "endpoint-line-1",
		}),
		"message": messageResponseItemFromStore(store.Message{
			ID:             1,
			LineID:         "line-main",
			EndpointLineID: "endpoint-line-1",
			Peer:           "+818012345678",
		}),
		"call history": callRecordResponses([]store.Call{call}),
		"active call":  callSession(call, calllease.ControlOccupied),
		"recording": recordingEntryResponses([]store.RecordingEntry{{
			Call: store.RecordingCall{
				ID:             "call-1",
				LineID:         "line-main",
				EndpointLineID: "endpoint-line-1",
				Direction:      "incoming",
				RemoteNumber:   "+818012345678",
			},
		}}),
	}

	for name, response := range responses {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			body := string(encoded)
			for _, field := range []string{`"endpoint_id"`, `"endpoint_line_id"`} {
				if strings.Contains(body, field) {
					t.Fatalf("ordinary response leaked %s: %s", field, body)
				}
			}
			if !strings.Contains(body, `"line_id":"line-main"`) &&
				!strings.Contains(body, `"id":"line-main"`) {
				t.Fatalf("ordinary response omitted stable line_id: %s", body)
			}
		})
	}
}
