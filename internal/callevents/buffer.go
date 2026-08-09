package callevents

import (
	"sync"
	"time"
)

const DefaultCapacity = 32

type IncomingCall struct {
	CallID       string
	LineID       string
	RemoteNumber string
	DisplayName  string
	ObservedAt   time.Time
}

type Publisher interface {
	Publish(IncomingCall)
}

type Source interface {
	Subscribe() (<-chan IncomingCall, func())
}

type Buffer struct {
	mu          sync.Mutex
	capacity    int
	nextClient  uint64
	subscribers map[uint64]chan IncomingCall
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		capacity:    capacity,
		subscribers: make(map[uint64]chan IncomingCall),
	}
}

func (b *Buffer) Publish(event IncomingCall) {
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

func (b *Buffer) Subscribe() (<-chan IncomingCall, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan IncomingCall, b.capacity)
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
