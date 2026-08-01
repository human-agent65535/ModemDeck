package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/diagnostics"
)

func TestDiagnosticLogStreamReplaysSanitizedHardwareLogs(t *testing.T) {
	buffer := diagnostics.NewLogBuffer(4)
	logger := slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil)))
	logger.Warn("registration changed", "component", "modemmanager", "line_id", "line-1")

	handler := NewWithOptions(&fakeProvider{}, "test", Options{DiagnosticLogs: buffer})
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/diagnostics/logs/stream?after=99",
		nil,
	)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request.WithContext(ctx))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: reset") || !strings.Contains(body, "event: log") ||
		!strings.Contains(body, `"source":"hardware-agent"`) ||
		!strings.Contains(body, `"message":"registration changed"`) {
		t.Fatalf("stream = %q", body)
	}
}

func TestDiagnosticLogStreamRejectsInvalidCursor(t *testing.T) {
	handler := NewWithOptions(&fakeProvider{}, "test", Options{
		DiagnosticLogs: diagnostics.NewLogBuffer(4),
	})
	response := performRequest(
		handler,
		http.MethodGet,
		"/v1/diagnostics/logs/stream?after=nope",
		nil,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
