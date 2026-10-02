package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
