package modemmanager

import (
	"context"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const changeSubscriberBuffer = 1

type changeHub struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan struct{}
}

func newChangeHub() *changeHub {
	return &changeHub{subscribers: make(map[uint64]chan struct{})}
}

func (h *changeHub) subscribe(ctx context.Context) (<-chan struct{}, error) {
	if h == nil {
		return nil, domain.Unavailable("subscribe_changes", "change source is unavailable", nil)
	}
	if ctx == nil {
		return nil, domain.InvalidArgument("subscribe_changes", "request context is required")
	}
	events := make(chan struct{}, changeSubscriberBuffer)
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.subscribers[id] = events
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.mu.Lock()
		if current, found := h.subscribers[id]; found {
			delete(h.subscribers, id)
			close(current)
		}
		h.mu.Unlock()
	}()
	return events, nil
}

func (h *changeHub) publish() {
	if h == nil {
		return
	}
	h.mu.Lock()
	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

func (p *Provider) SubscribeChanges(ctx context.Context) (<-chan struct{}, error) {
	if p == nil {
		return nil, domain.Unavailable("subscribe_changes", "provider is unavailable", nil)
	}
	return p.changes.subscribe(ctx)
}

func (p *Provider) publishChange() {
	if p == nil {
		return
	}
	p.changes.publish()
}

func (p *Provider) startSystemBusChangeWatcher(conn *dbus.Conn) (func(), error) {
	if conn == nil {
		return nil, fmt.Errorf("watch ModemManager changes: system D-Bus connection is required")
	}
	signalContext, cancel := context.WithCancel(context.Background())
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)

	matchRules := [][]dbus.MatchOption{
		{
			dbus.WithMatchSender(serviceName),
			dbus.WithMatchPathNamespace(managerPath),
		},
		{
			dbus.WithMatchSender(busServiceName),
			dbus.WithMatchInterface(busInterface),
			dbus.WithMatchMember("NameOwnerChanged"),
			dbus.WithMatchArg(0, serviceName),
		},
	}
	added := 0
	for _, rule := range matchRules {
		if err := conn.AddMatchSignalContext(signalContext, rule...); err != nil {
			for index := 0; index < added; index++ {
				_ = conn.RemoveMatchSignal(matchRules[index]...)
			}
			conn.RemoveSignal(signals)
			cancel()
			return nil, fmt.Errorf("watch ModemManager changes: %w", err)
		}
		added++
	}

	var once sync.Once
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-signalContext.Done():
				return
			case signal, open := <-signals:
				if !open {
					return
				}
				if signal == nil {
					continue
				}
				p.publishChange()
			}
		}
	}()

	stop := func() {
		once.Do(func() {
			cancel()
			conn.RemoveSignal(signals)
			for _, rule := range matchRules {
				_ = conn.RemoveMatchSignal(rule...)
			}
			<-stopped
		})
	}
	return stop, nil
}
