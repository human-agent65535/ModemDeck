// Package fanout delivers events from one publisher to every current
// subscriber without letting a slow subscriber hold up the others.
package fanout

import "sync"

// Buffer gives each subscriber its own bounded queue. A subscriber whose queue
// is full is dropped and its channel closed: it has missed events and must
// subscribe again and reload its state.
type Buffer[T any] struct {
	mu          sync.Mutex
	capacity    int
	nextClient  uint64
	subscribers map[uint64]chan T
}

// New returns a Buffer whose subscribers each queue up to capacity events.
func New[T any](capacity int) *Buffer[T] {
	return &Buffer[T]{
		capacity:    capacity,
		subscribers: make(map[uint64]chan T),
	}
}

// Publish offers event to every subscriber and never blocks.
func (b *Buffer[T]) Publish(event T) {
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

// Subscribe returns a channel of events and a function that ends the
// subscription. The channel is closed when the subscription ends, whether by
// that function or because the subscriber fell behind.
func (b *Buffer[T]) Subscribe() (<-chan T, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan T, b.capacity)
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
