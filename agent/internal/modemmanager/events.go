package modemmanager

import (
	"context"
	"fmt"
	"strings"
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

// SubscribeRadioLifecycle reports only provider and modem discovery boundaries
// where persisted radio intent needs to be applied again.
func (p *Provider) SubscribeRadioLifecycle(ctx context.Context) (<-chan struct{}, error) {
	if p == nil {
		return nil, domain.Unavailable("subscribe_radio_lifecycle", "provider is unavailable", nil)
	}
	return p.radioLifecycle.subscribe(ctx)
}

func (p *Provider) publishRadioLifecycle() {
	if p == nil {
		return
	}
	p.radioLifecycle.publish()
}

// SubscribeModemLifecycle reports provider and modem discovery boundaries.
// Consumers use it for hardware state that must be checked once when a modem
// appears or disappears, rather than with a periodic watchdog.
func (p *Provider) SubscribeModemLifecycle(ctx context.Context) (<-chan struct{}, error) {
	if p == nil {
		return nil, domain.Unavailable("subscribe_modem_lifecycle", "provider is unavailable", nil)
	}
	return p.modemLifecycle.subscribe(ctx)
}

func (p *Provider) publishModemLifecycle() {
	if p == nil {
		return
	}
	p.modemLifecycle.publish()
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
				if systemBusSignalRequiresRadioReconcile(signal) {
					p.publishRadioLifecycle()
				}
				if systemBusSignalRequiresModemReconcile(signal) {
					p.publishModemLifecycle()
				}
				if systemBusSignalAffectsSnapshot(signal) {
					p.publishChange()
				}
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

func systemBusSignalRequiresModemReconcile(signal *dbus.Signal) bool {
	if signal == nil {
		return false
	}
	switch signal.Name {
	case busInterface + ".NameOwnerChanged":
		return true
	case objectManagerInterface + ".InterfacesAdded":
		if len(signal.Body) < 2 {
			return true
		}
		interfaces, ok := signal.Body[1].(map[string]map[string]dbus.Variant)
		if !ok {
			return true
		}
		_, modemAdded := interfaces[modemInterface]
		return modemAdded
	case objectManagerInterface + ".InterfacesRemoved":
		if len(signal.Body) < 2 {
			return true
		}
		interfaces, ok := signal.Body[1].([]string)
		if !ok {
			return true
		}
		for _, interfaceName := range interfaces {
			if interfaceName == modemInterface {
				return true
			}
		}
	}
	return false
}

// systemBusSignalAffectsSnapshot keeps the Agent change stream aligned with
// the communication snapshot contract. ModemManager emits radio telemetry on
// the same D-Bus namespace as calls, messages, and modem lifecycle changes.
// Forwarding every telemetry sample would make the API rehydrate the complete
// call and message snapshot for each signal reading. Telemetry uses its
// lightweight endpoint; lifecycle and communication changes remain event driven.
func systemBusSignalAffectsSnapshot(signal *dbus.Signal) bool {
	if signal == nil {
		return false
	}
	switch signal.Name {
	case busInterface + ".NameOwnerChanged":
		return true
	case objectManagerInterface + ".InterfacesAdded":
		return addedInterfacesAffectSnapshot(signal.Body)
	case objectManagerInterface + ".InterfacesRemoved":
		return removedInterfacesAffectSnapshot(signal.Body)
	case propertiesInterface + ".PropertiesChanged":
		return changedPropertiesAffectSnapshot(signal.Body)
	}

	separator := strings.LastIndexByte(signal.Name, '.')
	if separator <= 0 {
		return false
	}
	return eventDrivenSnapshotInterface(signal.Name[:separator])
}

func systemBusSignalRequiresRadioReconcile(signal *dbus.Signal) bool {
	if signal == nil {
		return false
	}
	switch signal.Name {
	case busInterface + ".NameOwnerChanged":
		if len(signal.Body) < 3 {
			return true
		}
		newOwner, ok := signal.Body[2].(string)
		return !ok || strings.TrimSpace(newOwner) != ""
	case objectManagerInterface + ".InterfacesAdded":
		if len(signal.Body) < 2 {
			return true
		}
		interfaces, ok := signal.Body[1].(map[string]map[string]dbus.Variant)
		if !ok {
			return true
		}
		_, modemAdded := interfaces[modemInterface]
		return modemAdded
	default:
		return false
	}
}

func addedInterfacesAffectSnapshot(body []any) bool {
	if len(body) < 2 {
		return true
	}
	interfaces, ok := body[1].(map[string]map[string]dbus.Variant)
	if !ok {
		return true
	}
	for interfaceName := range interfaces {
		if eventDrivenSnapshotInterface(interfaceName) {
			return true
		}
	}
	return false
}

func removedInterfacesAffectSnapshot(body []any) bool {
	if len(body) < 2 {
		return true
	}
	interfaces, ok := body[1].([]string)
	if !ok {
		return true
	}
	for _, interfaceName := range interfaces {
		if eventDrivenSnapshotInterface(interfaceName) {
			return true
		}
	}
	return false
}

func changedPropertiesAffectSnapshot(body []any) bool {
	if len(body) < 3 {
		return true
	}
	interfaceName, ok := body[0].(string)
	if !ok {
		return true
	}
	if interfaceName == signalInterface {
		return false
	}
	if !eventDrivenSnapshotInterface(interfaceName) {
		return false
	}
	if interfaceName != modemInterface {
		return true
	}

	changed, changedOK := body[1].(map[string]dbus.Variant)
	invalidated, invalidatedOK := body[2].([]string)
	if !changedOK || !invalidatedOK {
		return true
	}
	for propertyName := range changed {
		if propertyName != "SignalQuality" {
			return true
		}
	}
	for _, propertyName := range invalidated {
		if propertyName != "SignalQuality" {
			return true
		}
	}
	return false
}

func eventDrivenSnapshotInterface(interfaceName string) bool {
	switch interfaceName {
	case modemInterface,
		modem3GPPInterface,
		simInterface,
		voiceInterface,
		callInterface,
		messagingInterface,
		smsInterface:
		return true
	default:
		return false
	}
}
