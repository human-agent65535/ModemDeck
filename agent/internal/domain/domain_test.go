package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestOperationErrorPreservesTypedCodeAndCause(t *testing.T) {
	cause := errors.New("dbus failure")
	err := Unavailable("snapshot", "provider unavailable", cause)

	operationError, ok := AsOperationError(err)
	if !ok {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if operationError.Code != ErrorUnavailable ||
		operationError.Operation != "snapshot" ||
		operationError.Message != "provider unavailable" {
		t.Fatalf("unexpected OperationError: %+v", operationError)
	}
	if !errors.Is(err, cause) {
		t.Fatal("OperationError did not preserve its cause")
	}
}

func TestSnapshotJSONContractUsesArraysAndRequiredTruthFields(t *testing.T) {
	snapshot := Snapshot{
		Revision:   "sha256:abc",
		ObservedAt: time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC),
		Lines: []Line{{
			ID:                   "line_hash",
			IdentityPersistent:   true,
			IdentitySource:       "physical_device+equipment_identifier",
			SavedPolicySupported: true,
			VoiceVerification: &VoiceRuntimeVerification{
				USBConfiguration: "read_failed",
				MediaRouting:     "enabled",
			},
		}},
		Calls: []Call{{
			ID:              "call_epoch_hash",
			LineID:          "line_hash",
			Number:          "+818012345678",
			Direction:       "incoming",
			State:           "active",
			StateCode:       4,
			StateReason:     "accepted",
			StateReasonCode: 3,
			AudioPort:       "hw:2,0",
			AudioFormat: &CallAudioFormat{
				Encoding:   "pcm",
				Resolution: "s16le",
				Rate:       8000,
			},
			MediaAvailable:  true,
			MediaConfigured: true,
			MediaActive:     true,
			Bearer:          "",
		}},
		Messages: []Message{{
			ID:        "message_epoch_hash",
			LineID:    "line_hash",
			Number:    "+818012345678",
			Text:      "hello",
			Direction: "incoming",
			State:     "received",
			StateCode: 3,
			Timestamp: "2026-07-23T10:00:00+09:00",
		}},
		DeliveryReports: []MessageDeliveryReport{},
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"revision",
		"observed_at",
		"lines",
		"calls",
		"messages",
		"delivery_reports",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("snapshot JSON missing %q: %s", key, encoded)
		}
	}
	if _, ok := decoded["lines"].([]any); !ok {
		t.Fatalf("lines is not an array: %T", decoded["lines"])
	}
	if _, ok := decoded["delivery_reports"].([]any); !ok {
		t.Fatalf("delivery_reports is not an array: %T", decoded["delivery_reports"])
	}
	lines := decoded["lines"].([]any)
	line := lines[0].(map[string]any)
	for _, key := range []string{
		"identity_persistent",
		"identity_source",
		"saved_policy_supported",
		"radio_desired_enabled",
		"radio_desired_enabled_known",
		"voice_verification",
	} {
		if _, ok := line[key]; !ok {
			t.Fatalf("line JSON missing %q: %s", key, encoded)
		}
	}
	calls := decoded["calls"].([]any)
	call := calls[0].(map[string]any)
	for _, key := range []string{
		"state_reason",
		"state_reason_code",
		"multiparty",
		"audio_port",
		"audio_format",
		"media_available",
		"media_configured",
		"media_active",
		"bearer",
	} {
		if _, ok := call[key]; !ok {
			t.Fatalf("call JSON missing %q: %s", key, encoded)
		}
	}
}

func TestCommandReceiptContainsNoInferredResourceState(t *testing.T) {
	encoded, err := json.Marshal(CommandReceipt{
		RequestID:  "request-1",
		ResourceID: "call_epoch_hash",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != `{"request_id":"request-1","resource_id":"call_epoch_hash"}` {
		t.Fatalf("unexpected receipt JSON: %s", encoded)
	}
}
