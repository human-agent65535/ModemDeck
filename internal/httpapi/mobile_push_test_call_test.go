package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/applepush"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeIOSCallTestService struct {
	userID       string
	credentialID string
	result       applepush.TestCallResult
	err          error
}

func (service *fakeIOSCallTestService) SendTestCall(
	_ context.Context,
	userID, credentialID string,
) (applepush.TestCallResult, error) {
	service.userID = userID
	service.credentialID = credentialID
	return service.result, service.err
}

func TestMobilePushTestCallTargetsCurrentPrincipal(t *testing.T) {
	t.Parallel()
	acceptedAt := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	service := &fakeIOSCallTestService{result: applepush.TestCallResult{
		ID:         "test-call-example",
		AcceptedAt: acceptedAt,
	}}
	api, err := New(&fakeRepository{
		iosPairingStatus: store.IOSPairingStatus{
			Devices: []store.IOSPairingDevice{{ID: "ios-device-example"}},
		},
	}, Options{
		IOSCallTests:          service,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/mobile/push/test-call",
		nil,
	).WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID: "member-example",
		Role:   auth.RoleMember,
	}))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if service.userID != "member-example" {
		t.Fatalf("test call user = %q", service.userID)
	}
	if service.credentialID != "ios-device-example" {
		t.Fatalf("test call credential = %q", service.credentialID)
	}
	if body := response.Body.String(); !strings.Contains(body, `"id":"test-call-example"`) ||
		!strings.Contains(body, acceptedAt.Format(time.RFC3339Nano)) {
		t.Fatalf("body = %s", body)
	}
}

func TestMobilePushTestCallRequiresAndUsesOneOfMultipleCredentials(
	t *testing.T,
) {
	t.Parallel()
	service := &fakeIOSCallTestService{result: applepush.TestCallResult{
		ID:         "test-call-tablet",
		AcceptedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
	}}
	api, err := New(&fakeRepository{
		iosPairingStatus: store.IOSPairingStatus{
			Devices: []store.IOSPairingDevice{
				{ID: "ios-phone"},
				{ID: "ios-tablet"},
			},
		},
	}, Options{
		IOSCallTests:          service,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: "member-example", Role: auth.RoleMember}

	missing := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/mobile/push/test-call",
		nil,
	).WithContext(auth.ContextWithPrincipal(context.Background(), principal))
	api.ServeHTTP(missing, request)
	assertAPIError(
		t,
		missing,
		http.StatusUnprocessableEntity,
		"ios_pairing_credential_required",
	)
	if service.credentialID != "" {
		t.Fatalf("test call sent without a selected credential: %q", service.credentialID)
	}

	selected := httptest.NewRecorder()
	request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/mobile/push/test-call?credential_id=ios-tablet",
		nil,
	).WithContext(auth.ContextWithPrincipal(context.Background(), principal))
	api.ServeHTTP(selected, request)
	if selected.Code != http.StatusAccepted ||
		service.userID != "member-example" ||
		service.credentialID != "ios-tablet" {
		t.Fatalf(
			"selected test call = status %d, user %q, credential %q, body %s",
			selected.Code,
			service.userID,
			service.credentialID,
			selected.Body.String(),
		)
	}
}

func TestMobilePushTestCallReportsUnavailableStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		service    IOSCallTestService
		wantStatus int
		wantCode   string
	}{
		{
			name:       "provider unavailable",
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "apple_push_unavailable",
		},
		{
			name:       "token unavailable",
			service:    &fakeIOSCallTestService{err: applepush.ErrPushTargetUnavailable},
			wantStatus: http.StatusConflict,
			wantCode:   "pushkit_not_registered",
		},
		{
			name:       "topic mismatch",
			service:    &fakeIOSCallTestService{err: applepush.ErrPushTopicMismatch},
			wantStatus: http.StatusConflict,
			wantCode:   "pushkit_topic_mismatch",
		},
		{
			name:       "delivery failed",
			service:    &fakeIOSCallTestService{err: errors.New("upstream failed")},
			wantStatus: http.StatusBadGateway,
			wantCode:   "test_call_delivery_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api, err := New(&fakeRepository{
				iosPairingStatus: store.IOSPairingStatus{
					Devices: []store.IOSPairingDevice{{ID: "ios-device-example"}},
				},
			}, Options{
				IOSCallTests:          test.service,
				disableAuthentication: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/mobile/push/test-call",
				nil,
			)
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			assertAPIError(t, response, test.wantStatus, test.wantCode)
		})
	}
}
