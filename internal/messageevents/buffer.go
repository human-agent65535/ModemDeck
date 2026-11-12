package messageevents

import (
	"time"

	"github.com/human-agent65535/modemdeck/internal/fanout"
)

const DefaultCapacity = 64

type IncomingSMS struct {
	MessageID  string    `json:"message_id"`
	ThreadKey  string    `json:"thread_key"`
	LineID     string    `json:"line_id"`
	Peer       string    `json:"peer"`
	Content    string    `json:"content"`
	Timestamp  string    `json:"timestamp"`
	ObservedAt time.Time `json:"observed_at"`
}

type Publisher interface {
	Publish(IncomingSMS)
}

type Source interface {
	Subscribe() (<-chan IncomingSMS, func())
}

// Buffer fans events out to subscribers; see fanout.Buffer for the delivery
// rule.
type Buffer struct {
	events *fanout.Buffer[IncomingSMS]
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{events: fanout.New[IncomingSMS](capacity)}
}

func (b *Buffer) Publish(event IncomingSMS) {
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	} else {
		event.ObservedAt = event.ObservedAt.UTC()
	}
	b.events.Publish(event)
}

func (b *Buffer) Subscribe() (<-chan IncomingSMS, func()) {
	return b.events.Subscribe()
}
