package messageevents

import (
	"sync"
	"time"
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

type Buffer struct {
	mu          sync.Mutex
	capacity    int
	nextClient  uint64
	subscribers map[uint64]chan IncomingSMS
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		capacity:    capacity,
		subscribers: make(map[uint64]chan IncomingSMS),
	}
}

func (b *Buffer) Publish(event IncomingSMS) {
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

func (b *Buffer) Subscribe() (<-chan IncomingSMS, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan IncomingSMS, b.capacity)
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
