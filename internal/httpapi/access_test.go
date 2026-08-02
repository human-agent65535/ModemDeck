package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestCommunicationDeletionUsesAssignedLineScopeForMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   func(string) string
		called func(*fakeRepository, *fakeRecordingService) bool
	}{
		{
			name:   "message thread",
			method: http.MethodDelete,
			path:   "/api/v1/messages/threads",
			body: func(lineID string) string {
				return `{"line_id":"` + lineID + `","peer":"+818012345678"}`
			},
			called: func(repository *fakeRepository, _ *fakeRecordingService) bool {
				return repository.messageDeleteIdentity.Peer == "+818012345678"
			},
		},
		{
			name:   "message thread batch",
			method: http.MethodPatch,
			path:   "/api/v1/messages/threads/state",
			body: func(lineID string) string {
				return `{"action":"delete","threads":[{"line_id":"` +
					lineID + `","peer":"+818012345678"}]}`
			},
			called: func(repository *fakeRepository, _ *fakeRecordingService) bool {
				return repository.messageUpdateAction == store.MessageThreadDelete
			},
		},
		{
			name:   "call",
			method: http.MethodDelete,
			path:   "/api/v1/calls/call-one",
			body:   func(string) string { return "" },
			called: func(_ *fakeRepository, recordings *fakeRecordingService) bool {
				return recordings.deleteCallID == "call-one"
			},
		},
		{
			name:   "call batch",
			method: http.MethodPatch,
			path:   "/api/v1/calls/batch",
			body: func(string) string {
				return `{"action":"delete","ids":["call-one"]}`
			},
			called: func(_ *fakeRepository, recordings *fakeRecordingService) bool {
				return len(recordings.deleteCallIDs) == 1 &&
					recordings.deleteCallIDs[0] == "call-one"
			},
		},
		{
			name:   "recording",
			method: http.MethodDelete,
			path:   "/api/v1/calls/call-one/recordings/segment-one",
			body:   func(string) string { return "" },
			called: func(_ *fakeRepository, recordings *fakeRecordingService) bool {
				return recordings.deleteRecordingCallID == "call-one" &&
					recordings.deleteRecordingSegmentID == "segment-one"
			},
		},
		{
			name:   "recording batch",
			method: http.MethodPatch,
			path:   "/api/v1/recordings/batch",
			body: func(string) string {
				return `{"action":"delete","recordings":[{"call_id":"call-one","id":"segment-one"}]}`
			},
			called: func(_ *fakeRepository, recordings *fakeRecordingService) bool {
				return recordings.deleteRecordingCallID == "call-one" &&
					recordings.deleteRecordingSegmentID == "segment-one"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, scope := range []struct {
				name       string
				lineID     string
				wantStatus int
				wantCalled bool
			}{
				{name: "assigned", lineID: "line-allowed", wantStatus: http.StatusNoContent, wantCalled: true},
				{name: "unassigned", lineID: "line-hidden", wantStatus: http.StatusNotFound, wantCalled: false},
			} {
				t.Run(scope.name, func(t *testing.T) {
					repository := &fakeRepository{callLineID: scope.lineID}
					recordings := &fakeRecordingService{}
					api, err := New(repository, Options{
						Recording:             recordings,
						disableAuthentication: true,
					})
					if err != nil {
						t.Fatalf("New() error = %v", err)
					}
					request := httptest.NewRequest(
						test.method,
						test.path,
						bytes.NewBufferString(test.body(scope.lineID)),
					)
					request.Header.Set("Content-Type", "application/json")
					request = request.WithContext(auth.ContextWithPrincipal(
						request.Context(),
						auth.Principal{
							UserID:         "user-member",
							Role:           auth.RoleMember,
							AllowedLineIDs: []string{"line-allowed"},
						},
					))
					response := httptest.NewRecorder()

					api.ServeHTTP(response, request)

					if response.Code != scope.wantStatus {
						t.Fatalf(
							"status = %d, want %d; body = %s",
							response.Code,
							scope.wantStatus,
							response.Body.String(),
						)
					}
					if called := test.called(repository, recordings); called != scope.wantCalled {
						t.Fatalf("deletion called = %t, want %t", called, scope.wantCalled)
					}
				})
			}
		})
	}
}

func TestLineInventoryAndDevicesFollowExplicitAssignmentsForAdminAndMember(t *testing.T) {
	t.Parallel()

	lines := []store.LineSummary{
		{ID: "line-allowed"},
		{ID: "line-hidden"},
	}
	devices := []store.Device{
		{IMEI: "allowed", SIM: &store.SIMCard{LineID: "line-allowed"}},
		{IMEI: "hidden", SIM: &store.SIMCard{LineID: "line-hidden"}},
		{IMEI: "inventory-only"},
	}
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleMember} {
		ctx := auth.ContextWithPrincipal(context.Background(), auth.Principal{
			UserID:         "user-test",
			Role:           role,
			AllowedLineIDs: []string{"line-allowed"},
		})
		visibleLines := filterLinesForPrincipal(ctx, lines)
		if len(visibleLines) != 1 || visibleLines[0].ID != "line-allowed" {
			t.Fatalf("%s visible lines = %+v", role, visibleLines)
		}
		visibleDevices := filterDevicesForPrincipal(ctx, devices)
		if len(visibleDevices) != 1 || visibleDevices[0].IMEI != "allowed" {
			t.Fatalf("%s visible devices = %+v", role, visibleDevices)
		}
	}
}

func TestAdminOnlyRoutesExcludeLineOwnedConfiguration(t *testing.T) {
	t.Parallel()

	for _, route := range []struct {
		path   string
		method string
	}{
		{"/api/v1/network", http.MethodGet},
		{"/api/v1/proxies", http.MethodPost},
		{"/api/v1/proxies/proxy-1", http.MethodPatch},
		{"/api/v1/settings/lines", http.MethodPatch},
		{"/api/v1/settings/system", http.MethodPatch},
		{"/api/v1/settings/recording", http.MethodPut},
		{"/api/v1/settings/telegram", http.MethodPost},
		{"/api/v1/settings/telegram/bot-1", http.MethodPut},
		{"/api/v1/lines/line-1/label", http.MethodPatch},
		{"/api/v1/devices/line-1/configuration", http.MethodPatch},
		{"/api/v1/devices/line-1/network-selection", http.MethodPut},
		{"/api/v1/devices/line-1/profiles", http.MethodPut},
		{"/api/v1/mobile/pairing", http.MethodGet},
		{"/api/v1/mobile/pairing", http.MethodPost},
		{"/api/v1/mobile/pairing", http.MethodDelete},
	} {
		if adminOnlyAPIPath(route.path, route.method) {
			t.Fatalf("%s %s is incorrectly admin-only", route.method, route.path)
		}
	}
	for _, route := range []struct {
		path   string
		method string
	}{
		{"/api/v1/devices", http.MethodPost},
		{"/api/v1/devices/867530900000001", http.MethodPatch},
		{"/api/v1/about", http.MethodGet},
		{"/api/v1/updates/check", http.MethodGet},
		{"/api/v1/updates/apply", http.MethodPost},
		{"/api/v1/updates/status", http.MethodGet},
		{"/api/v1/updates/events", http.MethodGet},
		{"/api/v1/external-access/status", http.MethodGet},
		{"/api/v1/external-access/refresh", http.MethodPost},
		{"/api/v1/external-access/origin-tls", http.MethodPut},
		{"/api/v1/external-access/origin-tls", http.MethodDelete},
		{"/api/v1/diagnostics", http.MethodGet},
		{"/api/v1/diagnostics/devices/line-1/configuration", http.MethodGet},
		{"/api/v1/diagnostics/devices/line-1/configuration", http.MethodPatch},
		{"/api/v1/settings/tls", http.MethodGet},
		{"/api/v1/settings/tls", http.MethodPut},
		{"/api/v1/settings/tls/ca", http.MethodGet},
	} {
		if !adminOnlyAPIPath(route.path, route.method) {
			t.Fatalf("%s %s must remain admin-only", route.method, route.path)
		}
	}
}
