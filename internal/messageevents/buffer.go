package messageevents

import "sync"

const (
	DefaultCapacity    = 512
	subscriberCapacity = 64
)

type IncomingSMS struct {
	ID        uint64 `json:"id"`
	EventKey  string `json:"event_key"`
	MessageID string `json:"message_id"`
	ThreadKey string `json:"thread_key"`
	LineID    string `json:"line_id"`
	ICCID     string `json:"iccid"`
	Peer      string `json:"peer"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type Window struct {
	Events   []IncomingSMS
	OldestID uint64
	NewestID uint64
	Reset    bool
}

type Publisher interface {
	Publish(IncomingSMS) (IncomingSMS, bool)
}

type Source interface {
	Subscribe(after uint64) (Window, <-chan IncomingSMS, func())
}

type Buffer struct {
	mu          sync.Mutex
	capacity    int
	nextID      uint64
	events      []IncomingSMS
	eventIDs    map[string]uint64
	nextClient  uint64
	subscribers map[uint64]chan IncomingSMS
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		capacity:    capacity,
		events:      make([]IncomingSMS, 0, capacity),
		eventIDs:    make(map[string]uint64, capacity),
		subscribers: make(map[uint64]chan IncomingSMS),
	}
}

func (b *Buffer) Publish(event IncomingSMS) (IncomingSMS, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id, exists := b.eventIDs[event.EventKey]; exists {
		for _, current := range b.events {
			if current.ID == id {
				return current, false
			}
		}
	}

	b.nextID++
	event.ID = b.nextID
	if len(b.events) == b.capacity {
		delete(b.eventIDs, b.events[0].EventKey)
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = event
	} else {
		b.events = append(b.events, event)
	}
	b.eventIDs[event.EventKey] = event.ID

	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(b.subscribers, id)
			close(subscriber)
		}
	}
	return event, true
}

func (b *Buffer) Subscribe(after uint64) (Window, <-chan IncomingSMS, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan IncomingSMS, subscriberCapacity)
	b.subscribers[clientID] = updates
	window := b.windowLocked(after)
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
	return window, updates, cancel
}

func (b *Buffer) windowLocked(after uint64) Window {
	window := Window{Events: []IncomingSMS{}}
	if len(b.events) == 0 {
		window.Reset = after > 0
		return window
	}
	window.OldestID = b.events[0].ID
	window.NewestID = b.events[len(b.events)-1].ID
	window.Reset = after > window.NewestID || (after > 0 && after+1 < window.OldestID)
	if window.Reset {
		after = 0
	}
	for _, event := range b.events {
		if event.ID > after {
			window.Events = append(window.Events, event)
		}
	}
	return window
}
