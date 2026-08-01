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
	if entry.Source != "application" {
		t.Fatalf("source = %q", entry.Source)
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

func TestLogBufferImportsExternalEntriesWithLocalCursorAndPrivacy(t *testing.T) {
	buffer := NewLogBuffer(8)
	buffer.AppendExternal(LogEntry{
		ID:        91,
		Level:     "NOTICE",
		Source:    "hardware-agent",
		Component: "modemmanager",
		Message:   "  line state changed  ",
		Fields: map[string]any{
			"line_id": "line-1",
			"number":  "+818012345678",
			"imsi":    "440101234567890",
		},
	})

	window := buffer.Snapshot(0)
	if len(window.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(window.Entries))
	}
	entry := window.Entries[0]
	if entry.ID != 1 || entry.Level != "info" || entry.Source != "hardware-agent" ||
		entry.Component != "modemmanager" || entry.Message != "line state changed" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Fields["line_id"] != "line-1" || entry.Fields["number"] != "[redacted]" ||
		entry.Fields["imsi"] != "[redacted]" {
		t.Fatalf("fields = %+v", entry.Fields)
	}
}

func TestLogBufferReplaysWhenCursorComesFromRestartedProcess(t *testing.T) {
	buffer := NewLogBuffer(2)
	logger := slog.New(buffer.Handler(slog.NewTextHandler(io.Discard, nil)))
	logger.Info("first")
	logger.Info("second")

	window := buffer.Snapshot(99)
	if !window.Truncated || len(window.Entries) != 2 {
		t.Fatalf("window = %+v", window)
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
