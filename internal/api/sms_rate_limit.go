package api

import (
	"sync"
	"time"
)

const (
	smsFirstHourLimit = 3
	smsDailyLimit     = 10
)

type smsRateLimitResult struct {
	Allowed    bool
	Code       string
	Message    string
	RetryAfter time.Duration
}

type smsRateLimitDay struct {
	Year  int
	Month time.Month
	Day   int
}

type smsRateLimiter struct {
	mu             sync.Mutex
	startedAt      time.Time
	now            func() time.Time
	day            smsRateLimitDay
	firstHourCount int
	dailyCount     int
}

func newSMSRateLimiter(startedAt time.Time, now func() time.Time) *smsRateLimiter {
	if now == nil {
		now = time.Now
	}
	if startedAt.IsZero() {
		startedAt = now()
	}
	return &smsRateLimiter{
		startedAt: startedAt,
		now:       now,
		day:       smsRateLimitDayOf(startedAt),
	}
}

func smsRateLimitDayOf(t time.Time) smsRateLimitDay {
	y, m, d := t.Date()
	return smsRateLimitDay{Year: y, Month: m, Day: d}
}

func (l *smsRateLimiter) Allow() smsRateLimitResult {
	if l == nil {
		return smsRateLimitResult{Allowed: true}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	day := smsRateLimitDayOf(now)
	if day != l.day {
		l.day = day
		l.dailyCount = 0
	}

	if now.Before(l.startedAt.Add(time.Hour)) && l.firstHourCount >= smsFirstHourLimit {
		return smsRateLimitResult{
			Code:       "first_hour_limit",
			Message:    "首次运行 1 小时内最多只能发送 3 条短信，请稍后再试",
			RetryAfter: l.startedAt.Add(time.Hour).Sub(now),
		}
	}
	if l.dailyCount >= smsDailyLimit {
		return smsRateLimitResult{
			Code:       "daily_limit",
			Message:    "每日最多只能发送 10 条短信，请明天再试",
			RetryAfter: nextLocalMidnight(now).Sub(now),
		}
	}

	if now.Before(l.startedAt.Add(time.Hour)) {
		l.firstHourCount++
	}
	l.dailyCount++
	return smsRateLimitResult{Allowed: true}
}

func nextLocalMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}
