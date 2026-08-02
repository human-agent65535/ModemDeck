package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
)

type eventStreamTestResponse struct {
	header    http.Header
	mu        sync.Mutex
	status    int
	body      bytes.Buffer
	flushed   chan struct{}
	flushOnce sync.Once
}

func newEventStreamTestResponse() *eventStreamTestResponse {
	return &eventStreamTestResponse{
		header:  make(http.Header),
		flushed: make(chan struct{}),
	}
}

func (response *eventStreamTestResponse) Header() http.Header {
	return response.header
}

func (response *eventStreamTestResponse) WriteHeader(status int) {
	response.mu.Lock()
	defer response.mu.Unlock()
	if response.status == 0 {
		response.status = status
	}
}

func (response *eventStreamTestResponse) Write(data []byte) (int, error) {
	response.mu.Lock()
	defer response.mu.Unlock()
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return response.body.Write(data)
}

func (response *eventStreamTestResponse) Flush() {
	response.flushOnce.Do(func() {
		close(response.flushed)
	})
}

func (response *eventStreamTestResponse) statusCode() int {
	response.mu.Lock()
	defer response.mu.Unlock()
	return response.status
}

func (response *eventStreamTestResponse) bodyString() string {
	response.mu.Lock()
	defer response.mu.Unlock()
	return response.body.String()
}

func TestEventStreamLimiterEnforcesSessionAndGlobalLimits(t *testing.T) {
	t.Parallel()

	limiter := newEventStreamLimiter(eventStreamLimitPolicy{
		perSession: 2,
		global:     3,
	})
	sessionA := sha256.Sum256([]byte("session-a"))
	sessionB := sha256.Sum256([]byte("session-b"))

	releaseA1, ok := limiter.acquire(sessionA)
	if !ok {
		t.Fatal("first session A stream was rejected")
	}
	releaseA2, ok := limiter.acquire(sessionA)
	if !ok {
		t.Fatal("second session A stream was rejected")
	}
	if _, ok := limiter.acquire(sessionA); ok {
		t.Fatal("third session A stream bypassed the per-session limit")
	}
	releaseB1, ok := limiter.acquire(sessionB)
	if !ok {
		t.Fatal("first session B stream was rejected")
	}
	if _, ok := limiter.acquire(sessionB); ok {
		t.Fatal("fourth total stream bypassed the global limit")
	}

	releaseA1()
	releaseA1()
	releaseB2, ok := limiter.acquire(sessionB)
	if !ok {
		t.Fatal("released capacity was not reusable")
	}
	releaseA2()
	releaseB1()
	releaseB2()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.total != 0 || len(limiter.sessions) != 0 {
		t.Fatalf(
			"limiter retained released streams: total=%d sessions=%d",
			limiter.total,
			len(limiter.sessions),
		)
	}
}

func TestEventStreamSessionKeySeparatesMobileCredentials(t *testing.T) {
	t.Parallel()

	firstToken, firstDigest, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	secondToken, secondDigest, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if firstToken == secondToken {
		t.Fatal("generated duplicate mobile tokens")
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/runtime/events",
		nil,
	)
	firstContext := context.WithValue(
		request.Context(),
		mobileAuthenticationContextKey{},
		mobileAuthentication{Digest: firstDigest},
	)
	secondContext := context.WithValue(
		request.Context(),
		mobileAuthenticationContextKey{},
		mobileAuthentication{Digest: secondDigest},
	)
	firstKey := eventStreamSessionKey(request.WithContext(firstContext))
	secondKey := eventStreamSessionKey(request.WithContext(secondContext))
	if firstKey != [sha256.Size]byte(firstDigest) ||
		secondKey != [sha256.Size]byte(secondDigest) ||
		firstKey == secondKey {
		t.Fatalf(
			"mobile event stream keys = %x, %x",
			firstKey,
			secondKey,
		)
	}
}

func TestEventStreamConnectionLimitReleasesOnCancel(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         runtimeevents.NewHub(),
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	api.eventStreams = newEventStreamLimiter(eventStreamLimitPolicy{
		perSession: 1,
		global:     2,
	})

	firstRequest := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	firstContext, cancelFirst := context.WithCancel(firstRequest.Context())
	firstResponse := newEventStreamTestResponse()
	firstDone := make(chan struct{})
	go func() {
		api.ServeHTTP(firstResponse, firstRequest.WithContext(firstContext))
		close(firstDone)
	}()
	waitForEventStreamFlush(t, firstResponse.flushed)

	blockedResponse := httptest.NewRecorder()
	api.ServeHTTP(
		blockedResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil),
	)
	if blockedResponse.Code != http.StatusTooManyRequests ||
		!bytes.Contains(blockedResponse.Body.Bytes(), []byte(`"code":"stream_limit_reached"`)) {
		t.Fatalf(
			"blocked stream status = %d; body = %s",
			blockedResponse.Code,
			blockedResponse.Body.String(),
		)
	}

	cancelFirst()
	waitForEventStreamClose(t, firstDone, nil)

	replacementRequest := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	replacementContext, cancelReplacement := context.WithCancel(replacementRequest.Context())
	cancelReplacement()
	replacementResponse := httptest.NewRecorder()
	api.ServeHTTP(
		replacementResponse,
		replacementRequest.WithContext(replacementContext),
	)
	if replacementResponse.Code != http.StatusOK {
		t.Fatalf(
			"replacement stream status = %d; body = %s",
			replacementResponse.Code,
			replacementResponse.Body.String(),
		)
	}
}

func TestEventStreamsCloseWhenCurrentAccessIsRevoked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   string
		revoke func(*testing.T, *auth.Service, *streamAuthRepository, auth.SessionToken)
	}{
		{
			name: "logout",
			kind: "runtime",
			revoke: func(
				t *testing.T,
				service *auth.Service,
				_ *streamAuthRepository,
				token auth.SessionToken,
			) {
				t.Helper()
				if err := service.Logout(context.Background(), token); err != nil {
					t.Fatalf("Logout() error = %v", err)
				}
			},
		},
		{
			name: "expiry",
			kind: "runtime",
			revoke: func(
				_ *testing.T,
				_ *auth.Service,
				repository *streamAuthRepository,
				_ auth.SessionToken,
			) {
				repository.setFound(false)
			},
		},
		{
			name: "disabled user",
			kind: "messages",
			revoke: func(
				_ *testing.T,
				_ *auth.Service,
				repository *streamAuthRepository,
				_ auth.SessionToken,
			) {
				repository.setFound(false)
			},
		},
		{
			name: "administrator role downgrade",
			kind: "diagnostics",
			revoke: func(
				_ *testing.T,
				_ *auth.Service,
				repository *streamAuthRepository,
				_ auth.SessionToken,
			) {
				repository.setPrincipal(auth.Principal{
					UserID: "user-admin",
					Role:   auth.RoleMember,
				})
			},
		},
	}

	for index, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := &streamAuthRepository{
				apiAuthRepository: &apiAuthRepository{},
				principal: auth.Principal{
					UserID: "user-admin",
					Role:   auth.RoleAdmin,
				},
				found: true,
			}
			authenticator, err := auth.NewService(repository)
			if err != nil {
				t.Fatalf("auth.NewService() error = %v", err)
			}
			options := Options{Authenticator: authenticator}
			path := ""
			switch test.kind {
			case "runtime":
				options.RuntimeEvents = runtimeevents.NewHub()
				path = "/api/v1/runtime/events"
			case "messages":
				options.MessageEvents = messageevents.NewBuffer(8)
				path = "/api/v1/messages/events"
			case "diagnostics":
				options.DiagnosticLogs = diagnostics.NewLogBuffer(8)
				path = "/api/v1/diagnostics/logs/stream"
			default:
				t.Fatalf("unknown stream kind %q", test.kind)
			}
			api, err := New(&fakeRepository{}, options)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			api.streamAuthInterval = 10 * time.Millisecond

			token := auth.SessionToken(opaqueTestToken(byte(80 + index)))
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.AddCookie(&http.Cookie{
				Name:  sessionCookieName,
				Value: string(token),
			})
			requestContext, cancel := context.WithCancel(request.Context())
			response := newEventStreamTestResponse()
			done := make(chan struct{})
			go func() {
				api.ServeHTTP(response, request.WithContext(requestContext))
				close(done)
			}()
			waitForEventStreamFlush(t, response.flushed)

			test.revoke(t, authenticator, repository, token)
			waitForEventStreamClose(t, done, cancel)
			if response.statusCode() != http.StatusOK {
				t.Fatalf("stream status = %d, want 200", response.statusCode())
			}
		})
	}
}

func TestEventStreamPayloadsUseCachedAuthorization(t *testing.T) {
	t.Parallel()

	t.Run("runtime", func(t *testing.T) {
		t.Parallel()

		repository, authenticator := newEventStreamTestAuthenticator(t)
		events := runtimeevents.NewHub()
		api, err := New(&fakeRepository{}, Options{
			Authenticator: authenticator,
			RuntimeEvents: events,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		api.streamAuthInterval = time.Hour

		response, done, cancel := startAuthenticatedEventStream(
			t,
			api,
			"/api/v1/runtime/events",
			auth.SessionToken(opaqueTestToken(91)),
		)
		baseline := repository.authenticationLookups()
		for range 40 {
			events.Publish(runtimeevents.Change{Sections: runtimeevents.SectionNetwork})
		}
		waitForMessageEvent(t, response, `"revision":40`)
		if got := repository.authenticationLookups(); got != baseline {
			t.Fatalf("authorization lookups = %d after payload burst, want %d", got, baseline)
		}
		cancel()
		waitForEventStreamClose(t, done, nil)
	})

	t.Run("messages", func(t *testing.T) {
		t.Parallel()

		repository, authenticator := newEventStreamTestAuthenticator(t)
		repository.setPrincipal(auth.Principal{
			UserID:         "user-admin",
			Role:           auth.RoleAdmin,
			AllowedLineIDs: []string{"line-1"},
		})
		events := messageevents.NewBuffer(64)
		api, err := New(&fakeRepository{}, Options{
			Authenticator: authenticator,
			MessageEvents: events,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		api.streamAuthInterval = time.Hour

		response, done, cancel := startAuthenticatedEventStream(
			t,
			api,
			"/api/v1/messages/events",
			auth.SessionToken(opaqueTestToken(92)),
		)
		baseline := repository.authenticationLookups()
		for index := range 40 {
			events.Publish(messageevents.IncomingSMS{
				MessageID: fmt.Sprintf("message-%d", index),
				LineID:    "line-1",
			})
		}
		waitForMessageEvent(t, response, `"message_id":"message-39"`)
		if got := repository.authenticationLookups(); got != baseline {
			t.Fatalf("authorization lookups = %d after payload burst, want %d", got, baseline)
		}
		cancel()
		waitForEventStreamClose(t, done, nil)
	})

	t.Run("diagnostics", func(t *testing.T) {
		t.Parallel()

		repository, authenticator := newEventStreamTestAuthenticator(t)
		logs := diagnostics.NewLogBuffer(64)
		logger := slog.New(logs.Handler(nil))
		api, err := New(&fakeRepository{}, Options{
			Authenticator:  authenticator,
			DiagnosticLogs: logs,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		api.streamAuthInterval = time.Hour

		response, done, cancel := startAuthenticatedEventStream(
			t,
			api,
			"/api/v1/diagnostics/logs/stream",
			auth.SessionToken(opaqueTestToken(93)),
		)
		baseline := repository.authenticationLookups()
		for index := range 40 {
			logger.Info(fmt.Sprintf("diagnostic-%d", index))
		}
		waitForMessageEvent(t, response, "diagnostic-39")
		if got := repository.authenticationLookups(); got != baseline {
			t.Fatalf("authorization lookups = %d after payload burst, want %d", got, baseline)
		}
		cancel()
		waitForEventStreamClose(t, done, nil)
	})
}

func newEventStreamTestAuthenticator(
	t *testing.T,
) (*streamAuthRepository, *auth.Service) {
	t.Helper()
	repository := &streamAuthRepository{
		apiAuthRepository: &apiAuthRepository{},
		principal: auth.Principal{
			UserID: "user-admin",
			Role:   auth.RoleAdmin,
		},
		found: true,
	}
	authenticator, err := auth.NewService(repository)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	return repository, authenticator
}

func startAuthenticatedEventStream(
	t *testing.T,
	api *API,
	path string,
	token auth.SessionToken,
) (*eventStreamTestResponse, <-chan struct{}, context.CancelFunc) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: string(token),
	})
	requestContext, cancel := context.WithCancel(request.Context())
	response := newEventStreamTestResponse()
	done := make(chan struct{})
	go func() {
		api.ServeHTTP(response, request.WithContext(requestContext))
		close(done)
	}()
	waitForEventStreamFlush(t, response.flushed)
	return response, done, cancel
}

func waitForEventStreamFlush(t *testing.T, flushed <-chan struct{}) {
	t.Helper()
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("event stream did not flush its initial response")
	}
}

func waitForEventStreamClose(
	t *testing.T,
	done <-chan struct{},
	cancel context.CancelFunc,
) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		if cancel != nil {
			cancel()
		}
		<-done
		t.Fatal("event stream did not close after access was revoked")
	}
}
