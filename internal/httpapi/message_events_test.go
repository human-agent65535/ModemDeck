package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

type streamAuthRepository struct {
	*apiAuthRepository
	principal auth.Principal
	found     bool
}

func (*streamAuthRepository) UserCredentialsByUsername(
	context.Context,
	string,
) (auth.UserCredentials, bool, error) {
	return auth.UserCredentials{}, false, nil
}

func (*streamAuthRepository) UserCredentialsByID(
	context.Context,
	string,
) (auth.UserCredentials, bool, error) {
	return auth.UserCredentials{}, false, nil
}

func (*streamAuthRepository) CreateUserSessionIfPasswordHash(
	context.Context,
	string,
	auth.UserSessionRecord,
) (bool, error) {
	return false, nil
}

func (repository *streamAuthRepository) UserSessionByTokenDigest(
	_ context.Context,
	_ auth.SessionTokenDigest,
) (auth.UserSessionRecord, auth.Principal, bool, error) {
	now := time.Now().UTC()
	return auth.UserSessionRecord{
		UserID:    repository.principal.UserID,
		CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour),
	}, repository.principal, repository.found, nil
}

func (*streamAuthRepository) ReplaceUserPasswordHashIfCurrentAndRevokeSessions(
	context.Context,
	string,
	string,
	string,
) (bool, error) {
	return false, nil
}

func TestMessageEventStreamReplaysLastEventID(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:1",
		MessageID: "1",
		ThreadKey: "line-main|+818000000001",
		LineID:    "line-main",
	})
	events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:2",
		MessageID: "2",
		ThreadKey: "line-main|+818000000002",
		LineID:    "line-main",
		ICCID:     "legacy-hardware-id",
	})
	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events", nil)
	request.Header.Set("Last-Event-ID", "1")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: sms") ||
		!strings.Contains(body, `"message_id":"2"`) ||
		!strings.Contains(body, `"line_id":"line-main"`) ||
		strings.Contains(body, `"message_id":"1"`) ||
		strings.Contains(body, `"iccid"`) ||
		!strings.Contains(body, "event: ready") {
		t.Fatalf("stream = %q", body)
	}
}

func TestMessageEventStreamInitialSubscriptionStartsAtCurrentWatermark(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:1",
		MessageID: "1",
		ThreadKey: "line-main|+818000000001",
		LineID:    "line-main",
	})
	second, _ := events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:2",
		MessageID: "2",
		ThreadKey: "line-main|+818000000002",
		LineID:    "line-main",
	})
	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "event: sms") ||
		!strings.Contains(body, "id: 2\nevent: ready") ||
		!strings.Contains(body, `"newest_id":2`) {
		t.Fatalf("stream = %q; want ready at watermark %d without replay", body, second.ID)
	}
}

func TestMessageEventStreamReplaysExplicitAfterCursor(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	events.Publish(messageevents.IncomingSMS{
		EventKey:  "sms:1",
		MessageID: "1",
		ThreadKey: "line-main|+818000000001",
		LineID:    "line-main",
	})
	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events?after=0", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, "event: sms") ||
		!strings.Contains(body, `"message_id":"1"`) {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestMessageEventStreamResetsCursorFromPreviousProcess(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	events.Publish(messageevents.IncomingSMS{EventKey: "sms:1", MessageID: "1"})
	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         events,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events", nil)
	request.Header.Set("Last-Event-ID", "99")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "id: 0\nevent: reset") ||
		strings.Contains(body, "event: sms") ||
		!strings.Contains(body, "id: 1\nevent: ready") {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestMessageEventStreamRejectsInvalidCursor(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{
		MessageEvents:         messageevents.NewBuffer(8),
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/messages/events?after=nope", nil),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}

func TestMessageEventStreamRechecksSessionAndLineAccess(t *testing.T) {
	t.Parallel()

	repository := &streamAuthRepository{
		apiAuthRepository: &apiAuthRepository{},
		principal: auth.Principal{
			UserID:         "user-member",
			Role:           auth.RoleMember,
			AllowedLineIDs: []string{"line-alpha"},
		},
		found: true,
	}
	authenticator, err := auth.NewService(repository)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events", nil)
	request.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: opaqueTestToken(21),
	})

	allowed, err := api.currentStreamCanAccessLine(request, "line-alpha")
	if err != nil || !allowed {
		t.Fatalf("assigned line access = %t, %v", allowed, err)
	}
	repository.principal.AllowedLineIDs = []string{"line-beta"}
	allowed, err = api.currentStreamCanAccessLine(request, "line-alpha")
	if err != nil || allowed {
		t.Fatalf("revoked line access = %t, %v", allowed, err)
	}
	repository.found = false
	if _, err := api.currentStreamCanAccessLine(request, "line-beta"); err == nil {
		t.Fatal("revoked session retained message stream access")
	}
}
