package runtimeevents

import (
	"slices"
	"sync"
	"time"
)

const (
	DefaultCapacity    = 512
	subscriberCapacity = 64
)

type Resource string

const (
	ResourceLines    Resource = "lines"
	ResourceNetwork  Resource = "network"
	ResourceCalls    Resource = "calls"
	ResourceMessages Resource = "messages"
)

type Event struct {
	ID         uint64     `json:"id"`
	EventKey   string     `json:"-"`
	Resources  []Resource `json:"resources"`
	ObservedAt time.Time  `json:"observed_at"`
}

type Window struct {
	Events   []Event
	OldestID uint64
	NewestID uint64
	Reset    bool
}

type Publisher interface {
	Publish(Event) (Event, bool)
}

type Source interface {
	Subscribe(after uint64) (Window, <-chan Event, func())
	SubscribeCurrent() (Window, <-chan Event, func())
}

type Buffer struct {
	mu          sync.Mutex
	capacity    int
	nextID      uint64
	events      []Event
	eventIDs    map[string]uint64
	nextClient  uint64
	subscribers map[uint64]chan Event
}

func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		capacity:    capacity,
		events:      make([]Event, 0, capacity),
		eventIDs:    make(map[string]uint64, capacity),
		subscribers: make(map[uint64]chan Event),
	}
}

func (b *Buffer) Publish(event Event) (Event, bool) {
	event.Resources = normalizedResources(event.Resources)
	if len(event.Resources) == 0 {
		return Event{}, false
	}
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	} else {
		event.ObservedAt = event.ObservedAt.UTC()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if event.EventKey != "" {
		if id, exists := b.eventIDs[event.EventKey]; exists {
			for _, current := range b.events {
				if current.ID == id {
					return cloneEvent(current), false
				}
			}
		}
	}

	b.nextID++
	event.ID = b.nextID
	if len(b.events) == b.capacity {
		if oldestKey := b.events[0].EventKey; oldestKey != "" {
			delete(b.eventIDs, oldestKey)
		}
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = event
	} else {
		b.events = append(b.events, event)
	}
	if event.EventKey != "" {
		b.eventIDs[event.EventKey] = event.ID
	}

	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- cloneEvent(event):
		default:
			delete(b.subscribers, id)
			close(subscriber)
		}
	}
	return cloneEvent(event), true
}

func (b *Buffer) Subscribe(after uint64) (Window, <-chan Event, func()) {
	return b.subscribe(after, true)
}

func (b *Buffer) SubscribeCurrent() (Window, <-chan Event, func()) {
	return b.subscribe(0, false)
}

func (b *Buffer) subscribe(after uint64, replay bool) (Window, <-chan Event, func()) {
	b.mu.Lock()
	b.nextClient++
	clientID := b.nextClient
	updates := make(chan Event, subscriberCapacity)
	b.subscribers[clientID] = updates
	window := b.windowLocked(after, replay)
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

func (b *Buffer) windowLocked(after uint64, replay bool) Window {
	window := Window{Events: []Event{}}
	if len(b.events) == 0 {
		window.Reset = replay && after > 0
		return window
	}
	window.OldestID = b.events[0].ID
	window.NewestID = b.events[len(b.events)-1].ID
	if !replay {
		return window
	}
	window.Reset = after > window.NewestID || (after > 0 && after+1 < window.OldestID)
	if window.Reset {
		return window
	}
	for _, event := range b.events {
		if event.ID > after {
			window.Events = append(window.Events, cloneEvent(event))
		}
	}
	return window
}

func normalizedResources(resources []Resource) []Resource {
	normalized := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		switch resource {
		case ResourceLines, ResourceNetwork, ResourceCalls, ResourceMessages:
			if !slices.Contains(normalized, resource) {
				normalized = append(normalized, resource)
			}
		}
	}
	return normalized
}

func cloneEvent(event Event) Event {
	event.Resources = slices.Clone(event.Resources)
	return event
}
