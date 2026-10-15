package modemmanager

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

// Capture call facts in the D-Bus receive order, before a method reply may
// complete a snapshot. Only the cheap wakeup processing goes through a queue.
type callSignalHandler struct {
	dbus.SignalHandler
	provider atomic.Pointer[Provider]
}

func newCallSignalHandler() *callSignalHandler {
	return &callSignalHandler{SignalHandler: dbus.NewSequentialSignalHandler()}
}

func (h *callSignalHandler) DeliverSignal(intf, name string, signal *dbus.Signal) {
	if p := h.provider.Load(); p != nil {
		p.recordCallSignal(signal)
	}
	h.SignalHandler.DeliverSignal(intf, name, signal)
}

func (h *callSignalHandler) AddSignal(ch chan<- *dbus.Signal) {
	h.SignalHandler.(dbus.SignalRegistrar).AddSignal(ch)
}

func (h *callSignalHandler) RemoveSignal(ch chan<- *dbus.Signal) {
	h.SignalHandler.(dbus.SignalRegistrar).RemoveSignal(ch)
}

func (h *callSignalHandler) Terminate() {
	h.SignalHandler.(dbus.Terminator).Terminate()
}

// A change notification may be coalesced, a lifecycle fact may not. Fold the
// ordered D-Bus state payload before publishing a wakeup, rather than fetching
// that state later. Call IDs include the ModemManager owner epoch.
type callLifecycle struct {
	mu        sync.Mutex
	revision  uint64
	clearedAt uint64
	calls     map[string]*observedCall
}

type observedCall struct {
	state, reason       int32
	reported            int32
	revision            uint64
	signalRevision      uint64
	source              string
	connected, ended    time.Time
	seen                time.Time
	projection          domain.Call
	linePath            dbus.ObjectPath
	associationConflict bool
}

func newCallLifecycle() *callLifecycle {
	return &callLifecycle{calls: make(map[string]*observedCall)}
}

func (l *callLifecycle) watermark() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.revision
}

func (l *callLifecycle) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	clear(l.calls)
	// Never recycle a watermark while a snapshot request is in flight.
	l.revision++
	l.clearedAt = l.revision
}

func callStateEstablished(state int32) bool { return state == 4 || state == 5 }

func (l *callLifecycle) observe(id string, state, reason int32, source string, at time.Time, watermark uint64) *observedCall {
	c := l.calls[id]
	if c == nil {
		c = &observedCall{}
		l.calls[id] = c
	}
	c.seen = at
	// A query begun before a signal cannot supersede that signal, even with a
	// different valid state (e.g. a stale active reply after a hold or hangup).
	if source == "snapshot" && c.signalRevision > watermark {
		return c
	}
	effective := state
	// Answering and ending are irreversible facts of this call identity.
	// Hold/resume remain valid transitions; a new call gets a different ID.
	if c.state == callStateTerminated ||
		(!c.connected.IsZero() && !callStateEstablished(state) && state != callStateTerminated) {
		effective = c.state
	}
	changed := c.source == "" || state != c.reported || effective != c.state || reason != c.reason
	if source != "snapshot" || changed {
		l.revision++
		c.revision = l.revision
		if source != "snapshot" {
			c.signalRevision = l.revision
		}
	}
	if callStateEstablished(effective) && c.connected.IsZero() && c.ended.IsZero() {
		c.connected = at
	}
	if state == callStateTerminated && c.ended.IsZero() {
		c.ended = at
	}
	c.reported = state
	c.state = effective
	if effective == state {
		c.reason = reason
	}
	if source != "snapshot" || changed {
		c.source = source
	}

	return c
}

// Log only the original payload from the existing asynchronous watcher. The
// ledger owns receive-time lifecycle evidence; logged_at is consumption time.
func (p *Provider) logCallSignal(signal *dbus.Signal) {
	if signal == nil || signal.Name != callInterface+".StateChanged" || len(signal.Body) != 3 || !validCallObject(signal.Path) {
		return
	}
	old, oldOK := signal.Body[0].(int32)
	state, stateOK := signal.Body[1].(int32)
	reason, reasonOK := signal.Body[2].(uint32)
	if !oldOK || !stateOK || !reasonOK || state < 0 || state > callStateTerminated {
		return
	}
	identity, err := p.ids.freeze()
	if err != nil || (signal.Sender != "" && !strings.HasSuffix(identity.providerEpoch(), "/"+signal.Sender)) {
		return
	}
	slog.Info("call state event received", "component", "call_connection", "stage", "hardware_state",
		"endpoint_call_id", identity.callID(signal.Path), "source", "state_changed",
		"logged_at", p.now().UTC(), "previous_state", callStateName(old),
		"reported_state", callStateName(state), "state_reason_code", reason)
}

func validCallObject(path dbus.ObjectPath) bool {
	return path.IsValid() && strings.HasPrefix(string(path), "/org/freedesktop/ModemManager1/Call/")
}

func validModemObject(path dbus.ObjectPath) bool {
	return path.IsValid() && strings.HasPrefix(string(path), "/org/freedesktop/ModemManager1/Modem/")
}

func (l *callLifecycle) entry(id string, at time.Time) *observedCall {
	c := l.calls[id]
	if c == nil {
		c = &observedCall{}
		l.calls[id] = c
	}
	c.seen = at
	return c
}

func (l *callLifecycle) associate(identity *instanceIDs, callPath, modemPath dbus.ObjectPath, at time.Time) {
	if !validCallObject(callPath) || !validModemObject(modemPath) {
		return
	}
	c := l.entry(identity.callID(callPath), at)
	if c.linePath != "" && c.linePath != modemPath {
		c.associationConflict = true
		return
	}
	c.linePath = modemPath
}

func (l *callLifecycle) associateVoiceProperties(identity *instanceIDs, modemPath dbus.ObjectPath, properties Properties, at time.Time) {
	if paths, known := objectPathValuesProperty(properties, "Calls"); known {
		for _, path := range paths {
			l.associate(identity, path, modemPath, at)
		}
	}
}

// recordCallSignal runs on the ordered bus receive callback. It only folds
// payloads; it never performs a D-Bus query, logs, or takes the call mutex.
func (p *Provider) recordCallSignal(signal *dbus.Signal) {
	if signal == nil || p.callLifecycle == nil {
		return
	}
	if signal.Name == busInterface+".NameOwnerChanged" && len(signal.Body) == 3 && signal.Body[0] == serviceName {
		p.callLifecycle.clear()
		return
	}
	identity, err := p.ids.freeze()
	if err != nil || (signal.Sender != "" && !strings.HasSuffix(identity.providerEpoch(), "/"+signal.Sender)) {
		return
	}
	at := p.now().UTC()
	l := p.callLifecycle
	l.mu.Lock()
	defer l.mu.Unlock()
	// Identity resolution may have advanced concurrently with this callback.
	if p.ids.providerEpoch() != identity.providerEpoch() {
		return
	}
	switch signal.Name {
	case objectManagerInterface + ".InterfacesAdded":
		if len(signal.Body) != 2 {
			return
		}
		path, pathOK := signal.Body[0].(dbus.ObjectPath)
		interfaces, interfacesOK := signal.Body[1].(map[string]map[string]dbus.Variant)
		if !pathOK || !interfacesOK {
			return
		}
		if properties, found := interfaces[voiceInterface]; found {
			l.associateVoiceProperties(identity, path, properties, at)
		}
		properties, found := interfaces[callInterface]
		if !found || !validCallObject(path) {
			return
		}
		id := identity.callID(path)
		c := l.entry(id, at)
		metadata := parseCall(path, c.projection.LineID, properties, identity)
		// State-only metadata updates cannot erase a previously known number
		// or direction. These values come only from the modem's actual payload.
		if metadata.Number == "" {
			metadata.Number = c.projection.Number
		}
		if metadata.Direction == "unknown" {
			metadata.Direction = c.projection.Direction
		}
		c.projection = metadata
		if state, known := int32Property(properties, "State"); known && state >= 0 && state <= callStateTerminated {
			reason, _ := int32Property(properties, "StateReason")
			l.observe(id, state, reason, "interfaces_added", at, 0)
		}
	case voiceInterface + ".CallAdded":
		if len(signal.Body) == 1 {
			if path, ok := signal.Body[0].(dbus.ObjectPath); ok {
				l.associate(identity, path, signal.Path, at)
			}
		}
	case propertiesInterface + ".PropertiesChanged":
		if len(signal.Body) != 3 || signal.Body[0] != voiceInterface {
			return
		}
		if properties, ok := signal.Body[1].(map[string]dbus.Variant); ok {
			l.associateVoiceProperties(identity, signal.Path, properties, at)
		}
	case callInterface + ".StateChanged":
		if len(signal.Body) != 3 || !validCallObject(signal.Path) {
			return
		}
		old, oldOK := signal.Body[0].(int32)
		state, stateOK := signal.Body[1].(int32)
		reason, reasonOK := signal.Body[2].(uint32)
		if !oldOK || !stateOK || !reasonOK || state < 0 || state > callStateTerminated {
			return
		}
		id := identity.callID(signal.Path)
		if callStateEstablished(old) {
			if c := l.calls[id]; c == nil || c.connected.IsZero() {
				l.observe(id, old, 0, "signal_previous", at, 0)
			}
		}
		l.observe(id, state, int32(reason), "state_changed", at, 0)
	}
}

// Seed only verified metadata once CreateCall returned its actual path. No
// connected or terminal state is invented; those still require source facts.
func (p *Provider) seedOutgoingCall(identity *instanceIDs, path dbus.ObjectPath, line domain.Line, modemPath dbus.ObjectPath, number string) {
	if p.callLifecycle == nil || !validCallObject(path) || line.ID == "" {
		return
	}
	l := p.callLifecycle
	l.mu.Lock()
	defer l.mu.Unlock()
	if p.ids.providerEpoch() != identity.providerEpoch() {
		return
	}
	at := p.now().UTC()
	l.associate(identity, path, modemPath, at)
	c := l.entry(identity.callID(path), at)
	c.projection.ID, c.projection.LineID = identity.callID(path), line.ID
	if c.projection.Number == "" {
		c.projection.Number = number
	}
	c.projection.Direction = "outgoing"
}

func (p *Provider) projectCallLifecycle(parsed *ParsedObjects, watermark uint64, at time.Time) bool {
	l := p.callLifecycle
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if watermark < l.clearedAt {
		return false
	}
	for _, c := range l.calls {
		if c.linePath == "" || c.associationConflict {
			continue
		}
		for lineID, modemPath := range parsed.LinePaths {
			if modemPath == c.linePath {
				if c.projection.LineID != "" && c.projection.LineID != lineID {
					c.associationConflict = true
					break
				}
				c.projection.LineID = lineID
				break
			}
		}
	}
	present := make(map[string]bool, len(parsed.Calls))
	for index := range parsed.Calls {
		call := &parsed.Calls[index]
		present[call.ID] = true
		c := l.observe(call.ID, call.StateCode, call.StateReasonCode, "snapshot", at, watermark)
		if call.Number == "" {
			call.Number = c.projection.Number
		}
		if call.Direction == "unknown" || call.Direction == "" {
			call.Direction = c.projection.Direction
		}
		call.StateCode, call.State = c.state, callStateName(c.state)
		call.StateReasonCode, call.StateReason = c.reason, callStateReasonName(c.reason)
		call.ConnectedAt, call.TerminatedAt = c.connected, c.ended
		call.StateRevision, call.StateSource, call.ReportedState = c.revision, c.source, callStateName(c.reported)
		if c.state == callStateTerminated {
			call.MediaAvailable = false
		}
		c.projection = *call
	}
	for id, c := range l.calls {
		if present[id] {
			continue
		}
		if at.Sub(c.seen) >= terminalCallRetention {
			delete(l.calls, id)
			continue
		}
		// Preserve a terminal signal even if its object disappeared before the
		// next coalesced snapshot. Do not resurrect an absent live object.
		if c.state != callStateTerminated || c.projection.ID == "" || c.associationConflict {
			continue
		}
		for index := range parsed.Lines {
			line := &parsed.Lines[index]
			if line.ID != c.projection.LineID {
				continue
			}
			call := c.projection
			call.StateCode, call.State = c.state, callStateName(c.state)
			call.StateReasonCode, call.StateReason = c.reason, callStateReasonName(c.reason)
			call.ConnectedAt, call.TerminatedAt = c.connected, c.ended
			call.StateRevision, call.StateSource, call.ReportedState = c.revision, c.source, callStateName(c.reported)
			call.MediaAvailable = false
			parsed.Calls = append(parsed.Calls, call)
			if !containsString(line.CallIDs, id) {
				line.CallIDs = append(line.CallIDs, id)
			}
			break
		}
	}
	for index := range parsed.Lines {
		sort.Strings(parsed.Lines[index].CallIDs)
	}
	sort.Slice(parsed.Calls, func(i, j int) bool { return parsed.Calls[i].ID < parsed.Calls[j].ID })
	return true
}
