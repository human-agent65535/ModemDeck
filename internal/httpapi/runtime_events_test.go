package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

func TestRuntimeEventStreamReplaysLastEventID(t *testing.T) {
	t.Parallel()

	events := runtimeevents.NewBuffer(8)
	events.Publish(runtimeevents.Event{
		EventKey:   "lines:1",
		Resources:  []runtimeevents.Resource{runtimeevents.ResourceLines},
		ObservedAt: time.Date(2026, time.July, 24, 3, 0, 0, 0, time.UTC),
	})
	events.Publish(runtimeevents.Event{
		EventKey:   "network:1",
		Resources:  []runtimeevents.Resource{runtimeevents.ResourceNetwork, runtimeevents.ResourceCalls},
		ObservedAt: time.Date(2026, time.July, 24, 3, 0, 1, 0, time.UTC),
	})
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	request.Header.Set("Last-Event-ID", "1")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: runtime") ||
		!strings.Contains(body, `"resources":["network","calls"]`) ||
		!strings.Contains(body, `"observed_at":"2026-07-24T03:00:01Z"`) ||
		strings.Contains(body, `"resources":["lines"]`) ||
		!strings.Contains(body, "event: ready") {
		t.Fatalf("stream = %q", body)
	}
}

func TestRuntimeEventStreamInitialSubscriptionStartsAtCurrentWatermark(t *testing.T) {
	t.Parallel()

	events := runtimeevents.NewBuffer(8)
	events.Publish(runtimeevents.Event{Resources: []runtimeevents.Resource{runtimeevents.ResourceLines}})
	second, _ := events.Publish(runtimeevents.Event{
		Resources: []runtimeevents.Resource{runtimeevents.ResourceNetwork},
	})
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		strings.Contains(body, "event: runtime") ||
		!strings.Contains(body, "id: 2\nevent: ready") ||
		!strings.Contains(body, `"newest_id":2`) {
		t.Fatalf("status = %d; stream = %q; want ready at watermark %d", response.Code, body, second.ID)
	}
}

func TestRuntimeEventStreamReplaysExplicitAfterCursor(t *testing.T) {
	t.Parallel()

	events := runtimeevents.NewBuffer(8)
	events.Publish(runtimeevents.Event{
		Resources: []runtimeevents.Resource{runtimeevents.ResourceLines},
	})
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events?after=0", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, "event: runtime") ||
		!strings.Contains(body, `"resources":["lines"]`) {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestMobileRuntimeEventStreamOnlyExposesSupportedResources(t *testing.T) {
	t.Parallel()

	events := runtimeevents.NewBuffer(8)
	events.Publish(runtimeevents.Event{
		Resources: []runtimeevents.Resource{
			runtimeevents.ResourceSession,
			runtimeevents.ResourceMessages,
			runtimeevents.ResourceCalls,
			runtimeevents.ResourceRecordings,
		},
	})
	events.Publish(runtimeevents.Event{
		Resources: []runtimeevents.Resource{
			runtimeevents.ResourceContacts,
		},
	})
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
	}
	api, err := New(repository, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/runtime/events?after=0",
		nil,
	)
	request = request.WithContext(context.WithValue(
		request.Context(),
		mobileAuthenticationContextKey{},
		mobileAuthentication{},
	))
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `"resources":["calls"]`) ||
		strings.Contains(body, `"session"`) ||
		strings.Contains(body, `"messages"`) ||
		strings.Contains(body, `"contacts"`) ||
		strings.Contains(body, `"recordings"`) {
		t.Fatalf("status = %d; mobile stream = %q", response.Code, body)
	}
}

func TestRuntimeEventStreamResetsCursorFromPreviousProcess(t *testing.T) {
	t.Parallel()

	events := runtimeevents.NewBuffer(8)
	events.Publish(runtimeevents.Event{
		Resources: []runtimeevents.Resource{runtimeevents.ResourceLines},
	})
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	request.Header.Set("Last-Event-ID", "99")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, "id: 0\nevent: reset") ||
		strings.Contains(body, "event: runtime") ||
		!strings.Contains(body, "id: 1\nevent: ready") {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestRuntimeEventStreamRejectsInvalidCursor(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         runtimeevents.NewBuffer(8),
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events?after=nope", nil),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}

func TestRuntimeEventStreamRequiresConfiguredSource(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil))
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"runtime_events_unavailable"`) {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestEventHeartbeatIsObservableWithoutChangingCursor(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	observedAt := time.Date(2026, time.July, 28, 7, 30, 0, 0, time.UTC)

	if !writeEventHeartbeat(response, response, observedAt) {
		t.Fatal("writeEventHeartbeat() = false")
	}

	body := response.Body.String()
	if !strings.Contains(body, "event: heartbeat") ||
		!strings.Contains(body, `"at":"2026-07-28T07:30:00Z"`) ||
		strings.Contains(body, "id:") {
		t.Fatalf("heartbeat = %q", body)
	}
}
