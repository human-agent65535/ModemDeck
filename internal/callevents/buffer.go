package callevents

import (
	"time"

	"github.com/human-agent65535/modemdeck/internal/fanout"
)

const DefaultCapacity = 32

type Kind string

const (
	KindIncoming Kind = "incoming"
	KindTerminal Kind = "terminal"
)

type Event struct {
	Kind         Kind
	CallID       string
	LineID       string
	RemoteNumber string
	DisplayName  string
	Revision     int64
	Phase        string
	EndReason    string
	FailureCode  string
	WasAnswered  bool
	ObservedAt   time.Time
}

type Publisher interface {
	Publish(Event)
}

type Source interface {
	Subscribe() (<-chan Event, func())
}

// Buffer fans events out to subscribers; see fanout.Buffer for the delivery
// rule.
type Buffer struct {
	events *fanout.Buffer[Event]
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{events: fanout.New[Event](capacity)}
}

func (b *Buffer) Publish(event Event) {
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	} else {
		event.ObservedAt = event.ObservedAt.UTC()
	}
	b.events.Publish(event)
}

func (b *Buffer) Subscribe() (<-chan Event, func()) {
	return b.events.Subscribe()
}
