package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

func TestMobileDiagnosticsAuthenticateValidateAndLog(t *testing.T) {
	var logs bytes.Buffer
	repository := &fakeRepository{mobileFound: true,
		mobilePrincipal:        auth.Principal{UserID: "owner", Role: auth.RoleMember, IOSPairingEnabled: true},
		iosCurrentCredentialID: "paired-phone"}
	api, err := New(repository, Options{disableAuthentication: true, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	batch := mobileDiagnosticBatch{Schema: 1, AppVersion: "0.1.0", AppBuild: "24", OSVersion: "27.2", Dropped: 3}
	for _, category := range strings.Fields("app network api pairing push callkit audio storage permissions") {
		batch.Events = append(batch.Events, mobileDiagnosticEvent{ID: "03c40c39-beb7-49a2-b3f8-c8b50c092bec",
			TimeMS: time.Now().UnixMilli(), Category: category, Name: "turn_gather_failed", Network: "cellular",
			CallID: "test-03c40c39-beb7-49a2-b3f8-c8b50c092bec",
			Fields: map[string]string{"error_code": "701", "turn_transport": "tls", "turn_port": "443",
				"turn_host": "cloudflare", "stage_elapsed_ms": "8001", "reason": "dns",
				"route": "/api/v1/calls/:id/media/ice", "ipv6": "true"}})
	}
	send := func(method, bearer string, value any, expected int) {
		t.Helper()
		var body []byte
		if raw, ok := value.(string); ok {
			body = []byte(raw)
		} else {
			body, err = json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
		}
		request := httptest.NewRequest(method, "/api/v1/mobile/diagnostics", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("status = %d, want %d: %s", response.Code, expected, response.Body.String())
		}
	}
	batch.Events[0].CallID = "call_aBcDeFgHiJkLmNoPqRsTuVwX"
	batch.Events[6].Fields = map[string]string{
		"microphone_dbfs": "-37", "received_packets": "250", "sent_packets": "240", "sent_bytes": "18000",
		"input_route": "microphone", "output_route": "receiver", "input_available": "true",
		"input_gain_settable": "false", "input_gain_percent": "100", "output_volume_percent": "60",
		"audio_enabled": "true", "microphone_track_enabled": "true", "sample_rate": "48000", "channels": "1",
		"http_protocol": "h3", "reused_connection": "true",
	}
	send(http.MethodPost, "", batch, http.StatusForbidden)
	send(http.MethodGet, string(token), batch, http.StatusMethodNotAllowed)
	send(http.MethodPost, string(token), batch, http.StatusNoContent)
	if strings.Count(logs.String(), `"msg":"iOS turn_gather_failed"`) != 9 ||
		!strings.Contains(logs.String(), `"device_id":"paired-phone"`) ||
		!strings.Contains(logs.String(), `"user_id":"owner"`) ||
		!strings.Contains(logs.String(), `"error_code":"701"`) || strings.Contains(logs.String(), string(token)) {
		t.Fatalf("missing authenticated diagnostic metadata: %s", logs.String())
	}
	for _, mutation := range []func(*mobileDiagnosticBatch){
		func(b *mobileDiagnosticBatch) { b.Events[8].Fields["sdp"] = "secret" },
		func(b *mobileDiagnosticBatch) { b.Events[8].Fields["error_code"] = "secret" },
		func(b *mobileDiagnosticBatch) { b.Events[8].Fields["route"] = "/api/v1/messages/+12025550101" },
		func(b *mobileDiagnosticBatch) { b.Events[8].Fields["reason"] = "private IP 192.0.2.1" },
		func(b *mobileDiagnosticBatch) { b.Events[6].Fields["input_route"] = "personal headset name" },
		func(b *mobileDiagnosticBatch) { b.Events[8].CallID = "+12025550101" },
		func(b *mobileDiagnosticBatch) { b.Events[8].Category = "unknown" },
		func(b *mobileDiagnosticBatch) { b.AppBuild = "secret" },
		func(b *mobileDiagnosticBatch) { b.Schema = 2 },
		func(b *mobileDiagnosticBatch) { b.Events = nil },
		func(b *mobileDiagnosticBatch) { b.Events = append(b.Events, append(b.Events, b.Events...)...) },
	} {
		raw, _ := json.Marshal(batch)
		var invalid mobileDiagnosticBatch
		if err := json.Unmarshal(raw, &invalid); err != nil {
			t.Fatal(err)
		}
		mutation(&invalid)
		logs.Reset()
		send(http.MethodPost, string(token), invalid, http.StatusBadRequest)
		if strings.Contains(logs.String(), `"source":"ios"`) {
			t.Fatal("invalid batch was partially logged")
		}
	}
	send(http.MethodPost, string(token), `{"schema":1,"user_id":"other"}`, http.StatusBadRequest)
	send(http.MethodPost, string(token), `{"schema":1,"app_version":"`+strings.Repeat("0", 33<<10)+`"}`, http.StatusRequestEntityTooLarge)
	repository.iosCurrentCredentialID = ""
	send(http.MethodPost, string(token), batch, http.StatusUnauthorized)
}

// The fixture is JSONEncoder output from the production Swift sanitizer and
// Codable types. Its generator and source fingerprint keep the cross-client
// contract reproducible even when this test runs in the Linux Go toolchain.
func TestMobileDiagnosticsAcceptsSwiftWSSContract(t *testing.T) {
	body, err := os.ReadFile("testdata/mobile_diagnostics_wss.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../ios-client/ios/App/App/ModemDeckDiagnostics.swift")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := os.ReadFile("testdata/mobile_diagnostics_wss.source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(source)) != strings.TrimSpace(string(fingerprint)) {
		t.Fatal("Swift diagnostics source changed; regenerate the fixture using testdata/mobile_diagnostics_wss.swift")
	}
	var batch mobileDiagnosticBatch
	if err := json.Unmarshal(body, &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 4 || batch.Events[0].Fields["stage"] != "connecting_wss" ||
		batch.Events[2].Fields["stage"] != "reconnecting_wss" ||
		batch.Events[1].Fields["captured_frames"] != "250" || batch.Events[1].Fields["dropped_frames"] != "7" ||
		batch.Events[1].Fields["server_received_packets"] != "240" ||
		batch.Events[1].Fields["playback_pending"] != "3" || batch.Events[1].Fields["playback_underruns"] != "2" ||
		batch.Events[1].Fields["playback_resets"] != "1" {
		t.Fatal("Swift fixture lost WSS stages or audio counters")
	}
	if bytes.Contains(body, []byte("synthetic-private-value")) {
		t.Fatal("Swift sanitizer did not remove private fixture input")
	}
	var logs bytes.Buffer
	repository := &fakeRepository{mobileFound: true,
		mobilePrincipal:        auth.Principal{UserID: "owner", Role: auth.RoleMember, IOSPairingEnabled: true},
		iosCurrentCredentialID: "paired-phone"}
	api, err := New(repository, Options{disableAuthentication: true, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	send := func(t *testing.T, payload []byte, expected int) {
		t.Helper()
		logs.Reset()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/diagnostics", bytes.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+string(token))
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("status = %d, want %d: %s", response.Code, expected, response.Body.String())
		}
		if expected == http.StatusNoContent && response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("diagnostic response permits caching")
		}
		if expected == http.StatusBadRequest && strings.Contains(logs.String(), `"source":"ios"`) {
			t.Fatal("rejected batch partially logged client diagnostics")
		}
	}
	send(t, body, http.StatusNoContent)
	decoder := json.NewDecoder(&logs)
	for _, event := range batch.Events {
		var logged map[string]any
		if err := decoder.Decode(&logged); err != nil {
			t.Fatal(err)
		}
		if logged["msg"] != "iOS "+event.Name || logged["source"] != "ios" || logged["component"] != "ios.audio" ||
			logged["user_id"] != "owner" || logged["device_id"] != "paired-phone" || logged["call_id"] != event.CallID {
			t.Fatalf("lost authenticated Swift event identity: %v", logged)
		}
		for key, value := range event.Fields {
			if logged[key] != value {
				t.Fatalf("lost Swift field %s = %s in %s", key, value, event.Name)
			}
		}
	}
	// These new fields retain the existing bounded integer policy. A malformed
	// event must reject the whole batch, including earlier otherwise valid events.
	for _, field := range []string{"captured_frames", "dropped_frames", "server_received_packets", "playback_pending", "playback_underruns", "playback_resets"} {
		for _, value := range []string{"1000000001", "-1000000001", "9223372036854775808", "1.5", "private value"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				var invalid mobileDiagnosticBatch
				if err := json.Unmarshal(body, &invalid); err != nil {
					t.Fatal(err)
				}
				invalid.Events[1].Fields[field] = value
				payload, err := json.Marshal(invalid)
				if err != nil {
					t.Fatal(err)
				}
				send(t, payload, http.StatusBadRequest)
			})
		}
	}
	for _, value := range []string{"wss_secret_stage", "connected private.example"} {
		t.Run("stage/"+value, func(t *testing.T) {
			var invalid mobileDiagnosticBatch
			if err := json.Unmarshal(body, &invalid); err != nil {
				t.Fatal(err)
			}
			invalid.Events[2].Fields["stage"] = value
			payload, err := json.Marshal(invalid)
			if err != nil {
				t.Fatal(err)
			}
			send(t, payload, http.StatusBadRequest)
		})
	}
}
