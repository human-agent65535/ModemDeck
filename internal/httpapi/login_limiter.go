package httpapi

import (
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureWindow             = 5 * time.Minute
	loginAccountFailureThreshold   = 5
	loginGlobalFailureThreshold    = 20
	loginFailureInitialBackoff     = time.Second
	loginFailureMaximumBackoff     = time.Minute
	loginFailureMaximumAccountKeys = 1024
)

type loginFailurePolicy struct {
	window           time.Duration
	accountThreshold uint
	globalThreshold  uint
	initialBackoff   time.Duration
	maximumBackoff   time.Duration
	maximumAccounts  int
}

var defaultLoginFailurePolicy = loginFailurePolicy{
	window:           loginFailureWindow,
	accountThreshold: loginAccountFailureThreshold,
	globalThreshold:  loginGlobalFailureThreshold,
	initialBackoff:   loginFailureInitialBackoff,
	maximumBackoff:   loginFailureMaximumBackoff,
	maximumAccounts:  loginFailureMaximumAccountKeys,
}

type loginFailureState struct {
	windowStarted time.Time
	blockedUntil  time.Time
	lastSeen      time.Time
	failures      uint
}

// loginFailureLimiter deliberately does not use client IP addresses. The API
// cannot safely trust forwarding headers unless its direct peer is verified as
// a trusted proxy; edge IP limiting remains the deployment layer's job.
type loginFailureLimiter struct {
	mu       sync.Mutex
	policy   loginFailurePolicy
	now      func() time.Time
	global   loginFailureState
	accounts map[[sha256.Size]byte]loginFailureState
}

func newLoginFailureLimiter(policy loginFailurePolicy) *loginFailureLimiter {
	return &loginFailureLimiter{
		policy:   policy,
		now:      time.Now,
		accounts: make(map[[sha256.Size]byte]loginFailureState),
	}
}

func (limiter *loginFailureLimiter) retryAfter(username string) time.Duration {
	now := limiter.now()
	accountKey := loginAccountKey(username)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	limiter.global = limiter.activeState(limiter.global, now)
	retryAfter := stateRetryAfter(limiter.global, now)

	account, found := limiter.accounts[accountKey]
	if !found {
		return retryAfter
	}
	account = limiter.activeState(account, now)
	if account.windowStarted.IsZero() {
		delete(limiter.accounts, accountKey)
		return retryAfter
	}
	limiter.accounts[accountKey] = account
	return maxDuration(retryAfter, stateRetryAfter(account, now))
}

func (limiter *loginFailureLimiter) recordFailure(username string) {
	now := limiter.now()
	accountKey := loginAccountKey(username)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	limiter.global = limiter.nextFailure(
		limiter.activeState(limiter.global, now),
		now,
		limiter.policy.globalThreshold,
	)

	account, found := limiter.accounts[accountKey]
	if !found && len(limiter.accounts) >= limiter.policy.maximumAccounts {
		limiter.evictOldestAccount()
	}
	account = limiter.nextFailure(
		limiter.activeState(account, now),
		now,
		limiter.policy.accountThreshold,
	)
	limiter.accounts[accountKey] = account
}

func (limiter *loginFailureLimiter) recordSuccess(username string) {
	limiter.mu.Lock()
	delete(limiter.accounts, loginAccountKey(username))
	limiter.mu.Unlock()
}

func (limiter *loginFailureLimiter) activeState(
	state loginFailureState,
	now time.Time,
) loginFailureState {
	if state.windowStarted.IsZero() ||
		now.Before(state.windowStarted) ||
		!now.Before(state.windowStarted.Add(limiter.policy.window)) {
		return loginFailureState{}
	}
	return state
}

func (limiter *loginFailureLimiter) nextFailure(
	state loginFailureState,
	now time.Time,
	threshold uint,
) loginFailureState {
	if state.windowStarted.IsZero() {
		state.windowStarted = now
	}
	state.failures++
	state.lastSeen = now
	delay := limiter.failureBackoff(state.failures, threshold)
	if delay > 0 {
		state.blockedUntil = now.Add(delay)
	}
	return state
}

func (limiter *loginFailureLimiter) failureBackoff(failures, threshold uint) time.Duration {
	if failures < threshold {
		return 0
	}
	delay := limiter.policy.initialBackoff
	for step := threshold; step < failures && delay < limiter.policy.maximumBackoff; step++ {
		if delay > limiter.policy.maximumBackoff/2 {
			return limiter.policy.maximumBackoff
		}
		delay *= 2
	}
	return minDuration(delay, limiter.policy.maximumBackoff)
}

func (limiter *loginFailureLimiter) evictOldestAccount() {
	var oldestKey [sha256.Size]byte
	var oldestTime time.Time
	found := false
	for key, state := range limiter.accounts {
		if !found || state.lastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = state.lastSeen
			found = true
		}
	}
	if found {
		delete(limiter.accounts, oldestKey)
	}
}

func loginAccountKey(username string) [sha256.Size]byte {
	usernameBytes := []byte(strings.TrimSpace(username))
	for index, character := range usernameBytes {
		if character >= 'A' && character <= 'Z' {
			usernameBytes[index] = character + ('a' - 'A')
		}
	}
	return sha256.Sum256(usernameBytes)
}

func stateRetryAfter(state loginFailureState, now time.Time) time.Duration {
	if !state.blockedUntil.After(now) {
		return 0
	}
	return state.blockedUntil.Sub(now)
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
