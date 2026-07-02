package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/device"
)

func TestSMSRateLimiterBlocksFourthSendDuringFirstHour(t *testing.T) {
	now := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	limiter := newSMSRateLimiter(now, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if result := limiter.Allow(); !result.Allowed {
			t.Fatalf("send %d allowed=%v message=%q", i+1, result.Allowed, result.Message)
		}
	}

	result := limiter.Allow()
	if result.Allowed {
		t.Fatal("fourth send during first hour should be blocked")
	}
	if result.Code != "first_hour_limit" {
		t.Fatalf("code=%q want first_hour_limit", result.Code)
	}
}

func TestSMSRateLimiterBlocksEleventhSendInSameDay(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	limiter := newSMSRateLimiter(now.Add(-2*time.Hour), func() time.Time { return now })

	for i := 0; i < 10; i++ {
		if result := limiter.Allow(); !result.Allowed {
			t.Fatalf("send %d allowed=%v message=%q", i+1, result.Allowed, result.Message)
		}
	}

	result := limiter.Allow()
	if result.Allowed {
		t.Fatal("eleventh send in same day should be blocked")
	}
	if result.Code != "daily_limit" {
		t.Fatalf("code=%q want daily_limit", result.Code)
	}
}

func TestSMSRateLimiterResetsDailyCountAtLocalMidnight(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	now := time.Date(2026, 7, 2, 23, 30, 0, 0, loc)
	limiter := newSMSRateLimiter(now.Add(-2*time.Hour), func() time.Time { return now })

	for i := 0; i < 10; i++ {
		if result := limiter.Allow(); !result.Allowed {
			t.Fatalf("send %d allowed=%v message=%q", i+1, result.Allowed, result.Message)
		}
	}
	if result := limiter.Allow(); result.Allowed {
		t.Fatal("eleventh send before midnight should be blocked")
	}

	now = time.Date(2026, 7, 3, 0, 1, 0, 0, loc)
	if result := limiter.Allow(); !result.Allowed {
		t.Fatalf("first send after midnight allowed=%v message=%q", result.Allowed, result.Message)
	}
}

func TestHandleSendSMSReturnsRateLimitBeforeBackendSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	pool := device.NewPool(&config.Config{})
	sender := &smsRateLimitBackendStub{}
	setNestedPrivateField(t, pool, []string{"workers"}, map[string]*device.Worker{
		"dev-1": {ID: "dev-1", Backend: sender},
	})
	server := &Server{
		pool:       pool,
		smsLimiter: newSMSRateLimiter(now, func() time.Time { return now }),
	}

	for i := 0; i < 3; i++ {
		rec := sendSMSTestRequest(server)
		if rec.Code != http.StatusOK {
			t.Fatalf("send %d status=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	if sender.sendCount != 3 {
		t.Fatalf("backend send count=%d want 3", sender.sendCount)
	}

	rec := sendSMSTestRequest(server)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status=%d body=%s", rec.Code, rec.Body.String())
	}
	if sender.sendCount != 3 {
		t.Fatalf("backend send count after limit=%d want 3", sender.sendCount)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "sms_rate_limited" || body["reason"] != "first_hour_limit" {
		t.Fatalf("response=%v, want sms_rate_limited first_hour_limit", body)
	}
}

func sendSMSTestRequest(server *Server) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/sms/send", strings.NewReader(`{
		"device_id": "dev-1",
		"phone": "+10086",
		"message": "hello"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleSendSMS(ctx)
	return rec
}

type smsRateLimitBackendStub struct {
	ussdDeviceBackendStub
	sendCount int
}

var _ backend.DeviceBackend = (*smsRateLimitBackendStub)(nil)

func (s *smsRateLimitBackendStub) SendSMS(ctx context.Context, to, body string) error {
	s.sendCount++
	return nil
}
