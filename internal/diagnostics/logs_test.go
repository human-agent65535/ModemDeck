package diagnostics

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestLogHandlerCapturesStructuredAndRedactedFields(t *testing.T) {
	buffer := NewLogBuffer(8)
	logger := slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil))).
		With("component", "communications", "line_id", "line-1", "api_token", "secret")

	logger.WarnContext(
		context.Background(),
		"snapshot unavailable",
		slog.Group("provider", "name", "modemmanager"),
		"error",
		"timeout",
	)

	window := buffer.Snapshot(0)
	if len(window.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(window.Entries))
	}
	entry := window.Entries[0]
	if entry.ID != 1 || entry.Level != "warn" || entry.Component != "communications" {
		t.Fatalf("entry identity = %+v", entry)
	}
	if entry.Message != "snapshot unavailable" {
		t.Fatalf("message = %q", entry.Message)
	}
	if got := entry.Fields["line_id"]; got != "line-1" {
		t.Fatalf("line_id = %#v", got)
	}
	if got := entry.Fields["api_token"]; got != "[redacted]" {
		t.Fatalf("api_token = %#v", got)
	}
	if got := entry.Fields["provider.name"]; got != "modemmanager" {
		t.Fatalf("provider.name = %#v", got)
	}
}

func TestLogBufferBoundsHistoryAndReportsGap(t *testing.T) {
	buffer := NewLogBuffer(2)
	logger := slog.New(buffer.Handler(slog.NewTextHandler(io.Discard, nil)))
	logger.Info("first")
	logger.Info("second")
	logger.Info("third")
	logger.Info("fourth")

	window := buffer.Snapshot(0)
	if len(window.Entries) != 2 || window.OldestID != 3 || window.NewestID != 4 {
		t.Fatalf("window = %+v", window)
	}
	if window.Entries[0].Message != "third" || window.Entries[1].Message != "fourth" {
		t.Fatalf("entries = %+v", window.Entries)
	}
	resumed := buffer.Snapshot(1)
	if !resumed.Truncated {
		t.Fatal("resume gap was not reported")
	}
}

func TestLogBufferSubscribeReplaysThenStreams(t *testing.T) {
	buffer := NewLogBuffer(4)
	logger := slog.New(buffer.Handler(slog.NewTextHandler(io.Discard, nil)))
	logger.Info("before")

	window, updates, cancel := buffer.Subscribe(0)
	if len(window.Entries) != 1 || window.Entries[0].Message != "before" {
		t.Fatalf("initial window = %+v", window)
	}
	logger.Info("after")
	select {
	case entry := <-updates:
		if entry.Message != "after" || entry.ID != 2 {
			t.Fatalf("update = %+v", entry)
		}
	default:
		t.Fatal("subscriber did not receive update")
	}
	cancel()
	if _, ok := <-updates; ok {
		t.Fatal("subscriber channel remained open")
	}
}
