package runtimeevents

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Change describes why the current live projection should be sent again.
// Durable is deliberately coarse: live state is carried by the SSE snapshot,
// while persisted collections only need one revision to wake their readers.
type Change struct {
	Durable    bool
	ObservedAt time.Time
	Sections   Section
}

// Section identifies one independently replaceable part of the live runtime
// projection. It is a bit mask so a slow subscriber can receive the union of
// changes it missed without replaying intermediate events.
type Section uint8

const (
	SectionCommunication Section = 1 << iota
	SectionNetwork
	SectionCalls

	AllSections = SectionCommunication | SectionNetwork | SectionCalls
)

// Signal is a process-local watermark. It is not an event log: subscribers
// always receive the newest value and reconnecting clients rebuild from the
// current authoritative state.
type Signal struct {
	Epoch        string    `json:"epoch"`
	Revision     uint64    `json:"revision"`
	DataRevision uint64    `json:"data_revision"`
	ObservedAt   time.Time `json:"observed_at"`
	Sections     Section   `json:"-"`
}

type Publisher interface {
	Publish(Change) Signal
}

type Source interface {
	Subscribe() (Signal, <-chan Signal, func())
}

type Hub struct {
	mu          sync.Mutex
	current     Signal
	nextClient  uint64
	subscribers map[uint64]chan Signal
}

func NewHub() *Hub {
	return &Hub{
		current:     Signal{Epoch: newEpoch()},
		subscribers: make(map[uint64]chan Signal),
	}
}

func (h *Hub) Publish(change Change) Signal {
	h.mu.Lock()
	defer h.mu.Unlock()
	if change.Sections == 0 && !change.Durable {
		return h.current
	}

	h.current.Revision++
	if change.Durable {
		h.current.DataRevision++
	}
	if change.ObservedAt.IsZero() {
		h.current.ObservedAt = time.Now().UTC()
	} else {
		h.current.ObservedAt = change.ObservedAt.UTC()
	}
	h.current.Sections = change.Sections
	current := h.current
	for _, subscriber := range h.subscribers {
		// One buffered value is enough. A slow client needs the newest
		// authoritative state, not every intermediate notification.
		select {
		case subscriber <- current:
		default:
			pendingSections := Section(0)
			select {
			case pending := <-subscriber:
				pendingSections = pending.Sections
			default:
			}
			coalesced := current
			coalesced.Sections |= pendingSections
			subscriber <- coalesced
		}
	}
	return current
}

func (h *Hub) Subscribe() (Signal, <-chan Signal, func()) {
	h.mu.Lock()
	h.nextClient++
	clientID := h.nextClient
	updates := make(chan Signal, 1)
	h.subscribers[clientID] = updates
	current := h.current
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if subscriber, exists := h.subscribers[clientID]; exists {
				delete(h.subscribers, clientID)
				close(subscriber)
			}
			h.mu.Unlock()
		})
	}
	return current, updates, cancel
}

func newEpoch() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err == nil {
		return hex.EncodeToString(value)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}
