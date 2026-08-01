package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

type streamAuthRepository struct {
	*apiAuthRepository
	mu        sync.RWMutex
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
	digest auth.SessionTokenDigest,
) (auth.UserSessionRecord, auth.Principal, bool, error) {
	now := time.Now().UTC()
	repository.mu.RLock()
	defer repository.mu.RUnlock()

	return auth.UserSessionRecord{
		UserID:             repository.principal.UserID,
		SessionTokenDigest: digest,
		CSRFTokenDigest:    repository.session.CSRFTokenDigest,
		CreatedAt:          now.Add(-time.Minute),
	}, repository.principal.Copy(), repository.found, nil
}

func (repository *streamAuthRepository) DeleteSessionByTokenDigest(
	context.Context,
	auth.SessionTokenDigest,
) error {
	repository.setFound(false)
	return nil
}

func (*streamAuthRepository) ReplaceUserPasswordHashIfCurrentAndRevokeSessions(
	context.Context,
	string,
	string,
	string,
) (bool, error) {
	return false, nil
}

func (repository *streamAuthRepository) setFound(found bool) {
	repository.mu.Lock()
	repository.found = found
	repository.mu.Unlock()
}

func (repository *streamAuthRepository) setPrincipal(principal auth.Principal) {
	repository.mu.Lock()
	repository.principal = principal.Copy()
	repository.mu.Unlock()
}

func TestMessageEventStreamOnlyForwardsLiveMessages(t *testing.T) {
	t.Parallel()

	events := messageevents.NewBuffer(8)
	observedAt := time.Date(2026, time.July, 24, 7, 30, 5, 0, time.UTC)
	events.Publish(messageevents.IncomingSMS{
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
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/events?after=invalid", nil)
	request.Header.Set("Last-Event-ID", "99")
	ctx, cancel := context.WithCancel(request.Context())
	response := newEventStreamTestResponse()
	done := make(chan struct{})
	go func() {
		api.ServeHTTP(response, request.WithContext(ctx))
		close(done)
	}()
	waitForEventStreamFlush(t, response.flushed)
	events.Publish(messageevents.IncomingSMS{
		MessageID:  "2",
		ThreadKey:  "line-main|+818000000002",
		LineID:     "line-main",
		Peer:       "+818000000002",
		Content:    "hello",
		Timestamp:  "2026-07-24T07:30:00Z",
		ObservedAt: observedAt,
	})
	waitForMessageEvent(t, response, `"message_id":"2"`)
	cancel()
	waitForEventStreamClose(t, done, nil)

	if response.statusCode() != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.statusCode(), response.bodyString())
	}
	body := response.bodyString()
	if !strings.Contains(body, "event: sms") ||
		!strings.Contains(body, `"message_id":"2"`) ||
		!strings.Contains(body, `"line_id":"line-main"`) ||
		!strings.Contains(body, `"observed_at":"2026-07-24T07:30:05Z"`) ||
		strings.Contains(body, `"message_id":"1"`) ||
		strings.Contains(body, "id:") ||
		strings.Contains(body, `"event_key"`) ||
		strings.Contains(body, `"iccid"`) ||
		strings.Contains(body, "event: ready") ||
		strings.Contains(body, "event: reset") {
		t.Fatalf("stream = %q", body)
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
	repository.setPrincipal(auth.Principal{
		UserID:         "user-member",
		Role:           auth.RoleMember,
		AllowedLineIDs: []string{"line-beta"},
	})
	allowed, err = api.currentStreamCanAccessLine(request, "line-alpha")
	if err != nil || allowed {
		t.Fatalf("revoked line access = %t, %v", allowed, err)
	}
	repository.setFound(false)
	if _, err := api.currentStreamCanAccessLine(request, "line-beta"); err == nil {
		t.Fatal("revoked session retained message stream access")
	}
}

func waitForMessageEvent(
	t *testing.T,
	response *eventStreamTestResponse,
	value string,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(response.bodyString(), value) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("message event stream did not contain %q: %q", value, response.bodyString())
}
