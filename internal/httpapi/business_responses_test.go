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
			ID:                "line-main",
			EndpointID:        "endpoint-line-1",
			FailureReason:     "unknown-capabilities",
			FailureReasonCode: 4,
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
			for _, field := range []string{
				`"endpoint_id"`,
				`"endpoint_line_id"`,
				`"failure_reason"`,
				`"failure_reason_code"`,
			} {
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

func TestLineSummaryResponseIncludesServingRadioTelemetry(t *testing.T) {
	t.Parallel()

	channel := uint32(100)
	response := lineSummaryResponseFromStore(store.LineSummary{
		ID: "line-main",
		ServingRadio: &store.ServingRadio{
			AccessTechnology: "lte",
			DuplexMode:       "fdd",
			Band:             "B1",
			Channel:          &channel,
			ChannelType:      "earfcn",
			Source:           "quectel-qnwinfo",
		},
	})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal line response: %v", err)
	}
	body := string(encoded)
	for _, evidence := range []string{
		`"access_technology":"lte"`,
		`"duplex_mode":"fdd"`,
		`"band":"B1"`,
		`"channel":100`,
		`"channel_type":"earfcn"`,
	} {
		if !strings.Contains(body, evidence) {
			t.Fatalf("line response omitted %s: %s", evidence, body)
		}
	}
}

func TestFavoritesAppearOnCallsAndRecordingEntriesButNotSegments(t *testing.T) {
	t.Parallel()

	callPayload := callRecordResponses([]store.Call{{
		ID:       "call-favorite",
		Favorite: true,
	}})
	if len(callPayload) != 1 || !callPayload[0].Favorite {
		t.Fatalf("call favorite response = %+v", callPayload)
	}

	encoded, err := json.Marshal(recordingEntryResponses([]store.RecordingEntry{{
		Segment: store.RecordingSegment{
			ID:     "segment-1",
			CallID: "call-favorite",
		},
		Favorite: true,
	}}))
	if err != nil {
		t.Fatalf("marshal recording response: %v", err)
	}
	var payload []struct {
		Favorite bool           `json:"favorite"`
		Segment  map[string]any `json:"segment"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal recording response: %v", err)
	}
	if len(payload) != 1 || !payload[0].Favorite {
		t.Fatalf("recording favorite response = %s", encoded)
	}
	if _, exists := payload[0].Segment["favorite"]; exists {
		t.Fatalf("recording segment exposed favorite state: %s", encoded)
	}
}
