package httpapi

import (
	"sync"
	"testing"
	"time"
)

func TestLoginFailureLimiterAppliesProgressiveAccountBackoff(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	limiter := newLoginFailureLimiter(loginFailurePolicy{
		window:           time.Minute,
		accountThreshold: 2,
		globalThreshold:  100,
		initialBackoff:   2 * time.Second,
		maximumBackoff:   8 * time.Second,
		maximumAccounts:  8,
	})
	limiter.now = func() time.Time { return now }

	limiter.recordFailure("Owner")
	if retryAfter := limiter.retryAfter("owner"); retryAfter != 0 {
		t.Fatalf("retry after first failure = %s, want 0", retryAfter)
	}

	limiter.recordFailure("owner")
	if retryAfter := limiter.retryAfter("OWNER"); retryAfter != 2*time.Second {
		t.Fatalf("retry after second failure = %s, want 2s", retryAfter)
	}

	now = now.Add(2 * time.Second)
	if retryAfter := limiter.retryAfter("owner"); retryAfter != 0 {
		t.Fatalf("retry after first backoff = %s, want 0", retryAfter)
	}
	limiter.recordFailure("owner")
	if retryAfter := limiter.retryAfter("owner"); retryAfter != 4*time.Second {
		t.Fatalf("retry after third failure = %s, want 4s", retryAfter)
	}

	limiter.recordSuccess("OWNER")
	if retryAfter := limiter.retryAfter("owner"); retryAfter != 0 {
		t.Fatalf("retry after success = %s, want 0", retryAfter)
	}
	limiter.recordFailure("owner")
	if retryAfter := limiter.retryAfter("owner"); retryAfter != 0 {
		t.Fatalf("retry after reset first failure = %s, want 0", retryAfter)
	}
}

func TestLoginFailureLimiterCapsGlobalFailuresAndResetsWindow(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	limiter := newLoginFailureLimiter(loginFailurePolicy{
		window:           10 * time.Second,
		accountThreshold: 100,
		globalThreshold:  3,
		initialBackoff:   time.Second,
		maximumBackoff:   4 * time.Second,
		maximumAccounts:  8,
	})
	limiter.now = func() time.Time { return now }

	for _, username := range []string{"one", "two", "three"} {
		limiter.recordFailure(username)
	}
	if retryAfter := limiter.retryAfter("new-account"); retryAfter != time.Second {
		t.Fatalf("global retry after = %s, want 1s", retryAfter)
	}

	now = now.Add(time.Second)
	limiter.recordFailure("four")
	if retryAfter := limiter.retryAfter("new-account"); retryAfter != 2*time.Second {
		t.Fatalf("escalated global retry after = %s, want 2s", retryAfter)
	}

	now = now.Add(10 * time.Second)
	if retryAfter := limiter.retryAfter("new-account"); retryAfter != 0 {
		t.Fatalf("retry after window expiry = %s, want 0", retryAfter)
	}
}

func TestLoginFailureLimiterBoundsTrackedAccounts(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	limiter := newLoginFailureLimiter(loginFailurePolicy{
		window:           time.Minute,
		accountThreshold: 100,
		globalThreshold:  100,
		initialBackoff:   time.Second,
		maximumBackoff:   time.Minute,
		maximumAccounts:  2,
	})
	limiter.now = func() time.Time { return now }

	limiter.recordFailure("oldest")
	now = now.Add(time.Second)
	limiter.recordFailure("middle")
	now = now.Add(time.Second)
	limiter.recordFailure("newest")

	if len(limiter.accounts) != 2 {
		t.Fatalf("tracked accounts = %d, want 2", len(limiter.accounts))
	}
	if _, found := limiter.accounts[loginAccountKey("oldest")]; found {
		t.Fatal("oldest account was not evicted")
	}
	for _, username := range []string{"middle", "newest"} {
		if _, found := limiter.accounts[loginAccountKey(username)]; !found {
			t.Fatalf("tracked account %q was evicted", username)
		}
	}
}

func TestLoginFailureLimiterIsSafeForConcurrentRequests(t *testing.T) {
	limiter := newLoginFailureLimiter(defaultLoginFailurePolicy)
	var waitGroup sync.WaitGroup
	for index := range 64 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			username := string(rune('a' + index%26))
			limiter.recordFailure(username)
			_ = limiter.retryAfter(username)
			if index%3 == 0 {
				limiter.recordSuccess(username)
			}
		}()
	}
	waitGroup.Wait()
	if len(limiter.accounts) > loginFailureMaximumAccountKeys {
		t.Fatalf(
			"tracked accounts = %d, want at most %d",
			len(limiter.accounts),
			loginFailureMaximumAccountKeys,
		)
	}
}
