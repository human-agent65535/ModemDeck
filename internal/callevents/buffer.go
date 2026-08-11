package callevents

import (
	"sync"
	"time"
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

type Buffer struct {
	mu          sync.Mutex
	capacity    int
	nextClient  uint64
	subscribers map[uint64]chan Event
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		capacity:    capacity,
		subscribers: make(map[uint64]chan Event),
	}
}

func (b *Buffer) Publish(event Event) {
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	} else {
		event.ObservedAt = event.ObservedAt.UTC()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(b.subscribers, id)
			close(subscriber)
		}
	}
}

func (b *Buffer) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan Event, b.capacity)
	b.subscribers[clientID] = updates
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if current, exists := b.subscribers[clientID]; exists {
				delete(b.subscribers, clientID)
				close(current)
			}
			b.mu.Unlock()
		})
	}
	return updates, cancel
}
