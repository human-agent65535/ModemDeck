package diagnostics

import (
	"io"
	"log/slog"
	"testing"
)

func TestLogBufferCapturesSanitizedAgentEntries(t *testing.T) {
	buffer := NewLogBuffer(4)
	logger := slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil))).
		With("component", "modemmanager", "line_id", "line-1")
	logger.Debug("poll completed", "sample_count", 1)
	logger.Info("call observed", "number", "+818012345678", "state", "ringing")

	window := buffer.Snapshot(0)
	if len(window.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(window.Entries))
	}
	entry := window.Entries[0]
	if entry.Source != "hardware-agent" || entry.Component != "modemmanager" ||
		entry.Fields["line_id"] != "line-1" || entry.Fields["number"] != "[redacted]" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestLogBufferReportsRestartCursorAndStreams(t *testing.T) {
	buffer := NewLogBuffer(2)
	logger := slog.New(buffer.Handler(slog.NewTextHandler(io.Discard, nil)))
	logger.Warn("before")

	window, updates, cancel := buffer.Subscribe(99)
	defer cancel()
	if !window.Truncated || len(window.Entries) != 1 {
		t.Fatalf("window = %+v", window)
	}
	logger.Error("after")
	select {
	case entry := <-updates:
		if entry.ID != 2 || entry.Message != "after" {
			t.Fatalf("entry = %+v", entry)
		}
	default:
		t.Fatal("subscriber did not receive entry")
	}
}
