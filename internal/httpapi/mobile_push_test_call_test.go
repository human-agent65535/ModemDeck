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
)

type fakeIOSCallTestService struct {
	userID string
	result applepush.TestCallResult
	err    error
}

func (service *fakeIOSCallTestService) SendTestCall(
	_ context.Context,
	userID string,
) (applepush.TestCallResult, error) {
	service.userID = userID
	return service.result, service.err
}

func TestMobilePushTestCallTargetsCurrentPrincipal(t *testing.T) {
	t.Parallel()
	acceptedAt := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	service := &fakeIOSCallTestService{result: applepush.TestCallResult{
		ID:         "test-call-example",
		AcceptedAt: acceptedAt,
	}}
	api, err := New(&fakeRepository{}, Options{
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
	if body := response.Body.String(); !strings.Contains(body, `"id":"test-call-example"`) ||
		!strings.Contains(body, acceptedAt.Format(time.RFC3339Nano)) {
		t.Fatalf("body = %s", body)
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
			api, err := New(&fakeRepository{}, Options{
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
