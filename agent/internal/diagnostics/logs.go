// Package diagnostics keeps a bounded, sanitized copy of the Hardware Agent's
// structured runtime logs for the application process to consume over the
// permission-scoped Unix socket.
package diagnostics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	DefaultLogCapacity = 1000
	subscriberCapacity = 128
)

type LogEntry struct {
	ID        uint64         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Source    string         `json:"source"`
	Component string         `json:"component"`
	Caller    string         `json:"caller,omitempty"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type LogWindow struct {
	Entries   []LogEntry `json:"entries"`
	OldestID  uint64     `json:"oldest_id"`
	NewestID  uint64     `json:"newest_id"`
	Truncated bool       `json:"truncated"`
}

type LogSource interface {
	Snapshot(after uint64) LogWindow
	Subscribe(after uint64) (LogWindow, <-chan LogEntry, func())
}

type LogBuffer struct {
	mu          sync.Mutex
	capacity    int
	nextID      uint64
	entries     []LogEntry
	nextClient  uint64
	subscribers map[uint64]chan LogEntry
}

func NewLogBuffer(capacity int) *LogBuffer {
	if capacity <= 0 {
		capacity = DefaultLogCapacity
	}
	return &LogBuffer{
		capacity:    capacity,
		entries:     make([]LogEntry, 0, capacity),
		subscribers: make(map[uint64]chan LogEntry),
	}
}

func (b *LogBuffer) Handler(next slog.Handler) slog.Handler {
	if next == nil {
		next = slog.NewTextHandler(io.Discard, nil)
	}
	return &logHandler{next: next, buffer: b}
}

func (b *LogBuffer) Snapshot(after uint64) LogWindow {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.windowLocked(after)
}

func (b *LogBuffer) Subscribe(after uint64) (LogWindow, <-chan LogEntry, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan LogEntry, subscriberCapacity)
	b.subscribers[clientID] = updates
	window := b.windowLocked(after)
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if current, ok := b.subscribers[clientID]; ok {
				delete(b.subscribers, clientID)
				close(current)
			}
			b.mu.Unlock()
		})
	}
	return window, updates, cancel
}

func (b *LogBuffer) append(entry LogEntry) {
	b.mu.Lock()
	b.nextID++
	entry.ID = b.nextID
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	} else {
		entry.Timestamp = entry.Timestamp.UTC()
	}
	if len(b.entries) == b.capacity {
		copy(b.entries, b.entries[1:])
		b.entries[len(b.entries)-1] = entry
	} else {
		b.entries = append(b.entries, entry)
	}
	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- cloneLogEntry(entry):
		default:
			delete(b.subscribers, id)
			close(subscriber)
		}
	}
	b.mu.Unlock()
}

func (b *LogBuffer) windowLocked(after uint64) LogWindow {
	window := LogWindow{Entries: []LogEntry{}}
	if len(b.entries) == 0 {
		return window
	}
	window.OldestID = b.entries[0].ID
	window.NewestID = b.entries[len(b.entries)-1].ID
	if after > window.NewestID {
		window.Truncated = true
		after = 0
	} else {
		window.Truncated = after > 0 && after+1 < window.OldestID
	}
	for _, entry := range b.entries {
		if entry.ID > after {
			window.Entries = append(window.Entries, cloneLogEntry(entry))
		}
	}
	return window
}

type logHandler struct {
	next   slog.Handler
	buffer *LogBuffer
	attrs  []boundAttr
	groups []string
}

type boundAttr struct {
	groups []string
	attr   slog.Attr
}

func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *logHandler) Handle(ctx context.Context, record slog.Record) error {
	fields := make(map[string]any)
	for _, bound := range h.attrs {
		addAttr(fields, bound.groups, bound.attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(fields, h.groups, attr)
		return true
	})
	component := "agent"
	if value, ok := fields["component"].(string); ok && strings.TrimSpace(value) != "" {
		component = strings.TrimSpace(value)
	}
	delete(fields, "component")
	h.buffer.append(LogEntry{
		Timestamp: record.Time,
		Level:     strings.ToLower(record.Level.String()),
		Source:    "hardware-agent",
		Component: component,
		Caller:    caller(record.PC),
		Message:   record.Message,
		Fields:    fields,
	})
	return h.next.Handle(ctx, record)
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append([]boundAttr(nil), h.attrs...)
	for _, attr := range attrs {
		combined = append(combined, boundAttr{
			groups: append([]string(nil), h.groups...),
			attr:   attr,
		})
	}
	return &logHandler{
		next:   h.next.WithAttrs(attrs),
		buffer: h.buffer,
		attrs:  combined,
		groups: append([]string(nil), h.groups...),
	}
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	name = strings.TrimSpace(name)
	if name == "" {
		return h
	}
	groups := append(append([]string(nil), h.groups...), name)
	return &logHandler{
		next:   h.next.WithGroup(name),
		buffer: h.buffer,
		attrs:  append([]boundAttr(nil), h.attrs...),
		groups: groups,
	}
}

func addAttr(fields map[string]any, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := groups
		if strings.TrimSpace(attr.Key) != "" {
			group = append(append([]string(nil), groups...), attr.Key)
		}
		for _, nested := range attr.Value.Group() {
			addAttr(fields, group, nested)
		}
		return
	}
	key := strings.Trim(strings.Join(append(append([]string(nil), groups...), attr.Key), "."), ".")
	if key == "" {
		return
	}
	if sensitiveKey(key) {
		fields[key] = "[redacted]"
		return
	}
	fields[key] = logValue(attr.Value)
}

func logValue(value slog.Value) any {
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindBool:
		return value.Bool()
	case slog.KindInt64:
		return value.Int64()
	case slog.KindUint64:
		return value.Uint64()
	case slog.KindFloat64:
		return value.Float64()
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().UTC()
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			return err.Error()
		}
		return fmt.Sprint(value.Any())
	default:
		return value.String()
	}
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, segment := range strings.FieldsFunc(key, func(r rune) bool {
		return r == '.' || r == '_' || r == '-'
	}) {
		switch segment {
		case "authorization", "cookie", "credential", "password", "secret", "token",
			"number", "phone", "peer", "recipient", "sender", "content", "text",
			"imsi", "iccid", "imei", "sdp", "digits":
			return true
		}
	}
	return false
}

func caller(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
}

func cloneLogEntry(entry LogEntry) LogEntry {
	cloned := entry
	if entry.Fields != nil {
		cloned.Fields = make(map[string]any, len(entry.Fields))
		for key, value := range entry.Fields {
			cloned.Fields[key] = value
		}
	}
	return cloned
}
