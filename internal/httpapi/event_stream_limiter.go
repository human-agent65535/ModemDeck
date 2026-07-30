package httpapi

import (
	"crypto/sha256"
	"net/http"
	"sync"
)

const (
	eventStreamMaximumConnectionsPerSession = 6
	eventStreamMaximumConnectionsGlobal     = 128
)

type eventStreamLimitPolicy struct {
	perSession int
	global     int
}

var defaultEventStreamLimitPolicy = eventStreamLimitPolicy{
	perSession: eventStreamMaximumConnectionsPerSession,
	global:     eventStreamMaximumConnectionsGlobal,
}

type eventStreamLimiter struct {
	mu       sync.Mutex
	policy   eventStreamLimitPolicy
	total    int
	sessions map[[sha256.Size]byte]int
}

func newEventStreamLimiter(policy eventStreamLimitPolicy) *eventStreamLimiter {
	return &eventStreamLimiter{
		policy:   policy,
		sessions: make(map[[sha256.Size]byte]int),
	}
}

func (limiter *eventStreamLimiter) acquire(
	sessionKey [sha256.Size]byte,
) (func(), bool) {
	limiter.mu.Lock()
	if limiter.total >= limiter.policy.global ||
		limiter.sessions[sessionKey] >= limiter.policy.perSession {
		limiter.mu.Unlock()
		return nil, false
	}
	limiter.total++
	limiter.sessions[sessionKey]++
	limiter.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			limiter.mu.Lock()
			defer limiter.mu.Unlock()

			limiter.total--
			remaining := limiter.sessions[sessionKey] - 1
			if remaining <= 0 {
				delete(limiter.sessions, sessionKey)
				return
			}
			limiter.sessions[sessionKey] = remaining
		})
	}, true
}

func (api *API) acquireEventStream(
	response http.ResponseWriter,
	request *http.Request,
) (func(), bool) {
	sessionKey := eventStreamSessionKey(request)
	release, ok := api.eventStreams.acquire(sessionKey)
	if ok {
		return release, true
	}
	writeError(
		response,
		http.StatusTooManyRequests,
		"stream_limit_reached",
		"Too many event streams are already open",
		"",
	)
	return nil, false
}

func eventStreamSessionKey(request *http.Request) [sha256.Size]byte {
	if authentication, ok := mobileAuthenticationFromContext(
		request.Context(),
	); ok {
		return [sha256.Size]byte(authentication.Digest)
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		return sha256.Sum256([]byte(cookie.Value))
	}
	return sha256.Sum256([]byte("modemdeck:event-stream:unscoped"))
}
