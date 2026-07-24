package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Call struct {
	ID             string
	State          string
	AudioPort      string
	AudioFormat    *AdvertisedFormat
	MediaAvailable bool
}

const (
	callStateActive = "active"
	callStateHeld   = "held"
)

func callStateAllowsMedia(state string) bool {
	switch state {
	case callStateActive, callStateHeld:
		return true
	default:
		return false
	}
}

type CallSource interface {
	ResolveCall(context.Context, string) (Call, error)
}

type CallSourceFunc func(context.Context, string) (Call, error)

func (f CallSourceFunc) ResolveCall(ctx context.Context, callID string) (Call, error) {
	return f(ctx, callID)
}

type Binding struct {
	AudioPort string      `json:"audio_port"`
	Backend   BackendKind `json:"backend"`
	Endpoint  string      `json:"endpoint"`
}

type Options struct {
	OpenTimeout         time.Duration
	StartTimeout        time.Duration
	FrameIOTimeout      time.Duration
	BackpressureTimeout time.Duration
	ShutdownTimeout     time.Duration
	OutboundFrames      int
}

func (options Options) withDefaults() Options {
	if options.OpenTimeout <= 0 {
		options.OpenTimeout = 2 * time.Second
	}
	if options.StartTimeout <= 0 {
		options.StartTimeout = 15 * time.Second
	}
	if options.FrameIOTimeout <= 0 {
		options.FrameIOTimeout = 2 * time.Second
	}
	if options.BackpressureTimeout <= 0 {
		options.BackpressureTimeout = 100 * time.Millisecond
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = 500 * time.Millisecond
	}
	if options.OutboundFrames <= 0 {
		options.OutboundFrames = 4
	}
	return options
}

type Manager struct {
	source   CallSource
	bindings map[string]Binding
	backends map[BackendKind]Backend
	options  Options

	mu              sync.Mutex
	closed          bool
	activeCalls     map[string]struct{}
	activeEndpoints map[string]struct{}
	pendingOpens    map[string]*pendingOpen
	sessions        map[string]*Session
}

type backendOpenResult struct {
	call    Call
	binding Binding
	format  Format
	device  Device
	err     error
}

type pendingOpen struct {
	callID        string
	endpointLease string
	cancel        context.CancelFunc
	ready         chan struct{}

	opening   bool
	completed bool
	abandoned bool
	result    backendOpenResult
}

func NewManager(
	source CallSource,
	bindings []Binding,
	backends map[BackendKind]Backend,
	options Options,
) (*Manager, error) {
	if source == nil {
		return nil, NewError(ErrorInvalidArgument, "configure_media", "call source is required", nil)
	}
	if options.OpenTimeout < 0 ||
		options.StartTimeout < 0 ||
		options.FrameIOTimeout < 0 ||
		options.BackpressureTimeout < 0 ||
		options.ShutdownTimeout < 0 ||
		options.OutboundFrames < 0 {
		return nil, NewError(
			ErrorInvalidArgument,
			"configure_media",
			"media timeouts and capacities must not be negative",
			nil,
		)
	}

	options = options.withDefaults()
	if options.OutboundFrames > 64 {
		return nil, NewError(
			ErrorInvalidArgument,
			"configure_media",
			"outbound frame capacity must not exceed 64",
			nil,
		)
	}

	bindingMap := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		if strings.TrimSpace(binding.AudioPort) == "" ||
			strings.TrimSpace(binding.AudioPort) != binding.AudioPort {
			return nil, NewError(
				ErrorInvalidArgument,
				"configure_media",
				"binding audio_port must be explicit",
				nil,
			)
		}
		if strings.TrimSpace(binding.Endpoint) == "" ||
			strings.TrimSpace(binding.Endpoint) != binding.Endpoint {
			return nil, NewError(
				ErrorInvalidArgument,
				"configure_media",
				"binding endpoint must be explicit",
				nil,
			)
		}
		if binding.Backend != BackendCharPCM && binding.Backend != BackendALSAPCM {
			return nil, NewError(
				ErrorInvalidArgument,
				"configure_media",
				"binding backend must be char-pcm or alsa-pcm",
				nil,
			)
		}
		if backends[binding.Backend] == nil {
			return nil, NewError(
				ErrorBackendUnavailable,
				"configure_media",
				fmt.Sprintf("%s backend is not configured", binding.Backend),
				nil,
			)
		}
		if _, exists := bindingMap[binding.AudioPort]; exists {
			return nil, NewError(
				ErrorInvalidArgument,
				"configure_media",
				"audio_port may have only one explicit binding",
				nil,
			)
		}
		bindingMap[binding.AudioPort] = binding
	}

	backendMap := make(map[BackendKind]Backend, len(backends))
	for kind, backend := range backends {
		if backend != nil {
			backendMap[kind] = backend
		}
	}

	return &Manager{
		source:          source,
		bindings:        bindingMap,
		backends:        backendMap,
		options:         options,
		activeCalls:     make(map[string]struct{}),
		activeEndpoints: make(map[string]struct{}),
		pendingOpens:    make(map[string]*pendingOpen),
		sessions:        make(map[string]*Session),
	}, nil
}

func (m *Manager) Open(ctx context.Context, callID string) (*Session, error) {
	const operation = "open_media"

	if callID == "" || strings.TrimSpace(callID) != callID {
		return nil, NewError(ErrorInvalidArgument, operation, "call id is required", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	openContext, cancel := context.WithTimeout(ctx, m.options.OpenTimeout)
	defer cancel()

	pending, err := m.beginPendingOpen(callID, cancel)
	if err != nil {
		return nil, err
	}
	go m.runPendingOpen(openContext, pending)

	select {
	case <-pending.ready:
		return m.claimPendingOpen(openContext, pending)
	case <-openContext.Done():
		m.abandonPendingOpen(pending)
		if errors.Is(openContext.Err(), context.DeadlineExceeded) {
			return nil, NewError(
				ErrorTimeout,
				operation,
				"media backend did not open before the deadline",
				openContext.Err(),
			)
		}
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"media open canceled",
			openContext.Err(),
		)
	}
}

func (m *Manager) runPendingOpen(ctx context.Context, pending *pendingOpen) {
	result := m.openBackend(ctx, pending)
	m.finishPendingOpen(pending, result)
}

func (m *Manager) openBackend(ctx context.Context, pending *pendingOpen) backendOpenResult {
	const operation = "open_media"

	if err := ctx.Err(); err != nil {
		return backendOpenResult{
			err: NewError(ErrorBackendUnavailable, operation, "media open canceled", err),
		}
	}

	call, err := m.source.ResolveCall(ctx, pending.callID)
	if err != nil {
		if _, ok := AsError(err); ok {
			return backendOpenResult{err: err}
		}
		return backendOpenResult{
			err: NewError(ErrorBackendUnavailable, operation, "call state is unavailable", err),
		}
	}
	if call.ID != pending.callID {
		return backendOpenResult{
			err: NewError(ErrorNotFound, operation, "call was not found", nil),
		}
	}
	if !callStateAllowsMedia(call.State) {
		return backendOpenResult{
			err: NewError(
				ErrorNotActive,
				operation,
				"call state does not permit media",
				nil,
			),
		}
	}
	if !call.MediaAvailable {
		return backendOpenResult{
			err: NewError(
				ErrorMediaUnavailable,
				operation,
				"ModemManager has not confirmed media availability",
				nil,
			),
		}
	}
	if call.AudioPort == "" || call.AudioFormat == nil {
		return backendOpenResult{
			err: NewError(
				ErrorMediaUnavailable,
				operation,
				"ModemManager media metadata is incomplete",
				nil,
			),
		}
	}

	format, err := ParseFormat(*call.AudioFormat)
	if err != nil {
		return backendOpenResult{err: err}
	}
	binding, ok := m.bindings[call.AudioPort]
	if !ok {
		return backendOpenResult{
			err: NewError(
				ErrorUnboundAudioPort,
				operation,
				"audio_port has no explicit media binding",
				nil,
			),
		}
	}
	backend := m.backends[binding.Backend]
	if backend == nil {
		return backendOpenResult{
			err: NewError(
				ErrorBackendUnavailable,
				operation,
				"configured media backend is unavailable",
				nil,
			),
		}
	}

	endpointLease := string(binding.Backend) + "\x00" + binding.Endpoint
	if err := m.beginBackendOpen(ctx, pending, endpointLease); err != nil {
		return backendOpenResult{err: err}
	}
	device, err := backend.Open(ctx, binding.Endpoint, format)
	if err != nil {
		if device != nil {
			_ = device.Close()
			device = nil
		}
		if _, ok := AsError(err); ok {
			return backendOpenResult{err: err}
		}
		return backendOpenResult{
			err: NewError(ErrorBackendUnavailable, operation, "media backend open failed", err),
		}
	}
	if device == nil {
		return backendOpenResult{
			err: NewError(
				ErrorBackendUnavailable,
				operation,
				"media backend returned no device",
				nil,
			),
		}
	}
	return backendOpenResult{
		call:    call,
		binding: binding,
		format:  format,
		device:  device,
	}
}

func (m *Manager) beginPendingOpen(
	callID string,
	cancel context.CancelFunc,
) (*pendingOpen, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, NewError(ErrorConflict, "open_media", "media manager is closed", nil)
	}
	if _, exists := m.activeCalls[callID]; exists {
		return nil, NewError(
			ErrorConflict,
			"open_media",
			"call already has an active or pending media session",
			nil,
		)
	}
	pending := &pendingOpen{
		callID: callID,
		cancel: cancel,
		ready:  make(chan struct{}),
	}
	m.activeCalls[callID] = struct{}{}
	m.pendingOpens[callID] = pending
	return pending, nil
}

func (m *Manager) beginBackendOpen(
	ctx context.Context,
	pending *pendingOpen,
	endpointLease string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, exists := m.pendingOpens[pending.callID]
	if !exists || current != pending || pending.abandoned || m.closed {
		return NewError(ErrorConflict, "open_media", "media open is no longer owned", nil)
	}
	if err := ctx.Err(); err != nil {
		return NewError(ErrorBackendUnavailable, "open_media", "media open canceled", err)
	}
	if _, exists := m.activeEndpoints[endpointLease]; exists {
		return NewError(
			ErrorConflict,
			"open_media",
			"media endpoint is already leased",
			nil,
		)
	}
	pending.endpointLease = endpointLease
	pending.opening = true
	m.activeEndpoints[endpointLease] = struct{}{}
	return nil
}

func (m *Manager) finishPendingOpen(
	pending *pendingOpen,
	result backendOpenResult,
) {
	var closeDevice Device

	m.mu.Lock()
	current, exists := m.pendingOpens[pending.callID]
	if !exists || current != pending {
		closeDevice = result.device
	} else {
		pending.opening = false
		pending.completed = true
		pending.result = result
		if pending.abandoned || m.closed {
			delete(m.pendingOpens, pending.callID)
			m.releasePendingLeaseLocked(pending)
			closeDevice = result.device
			pending.result.device = nil
		}
	}
	m.mu.Unlock()

	if closeDevice != nil {
		_ = closeDevice.Close()
	}
	close(pending.ready)
}

func (m *Manager) claimPendingOpen(
	ctx context.Context,
	pending *pendingOpen,
) (*Session, error) {
	const operation = "open_media"

	var closeDevice Device
	m.mu.Lock()
	current, exists := m.pendingOpens[pending.callID]
	if !exists || current != pending || pending.abandoned || m.closed {
		m.mu.Unlock()
		return nil, NewError(ErrorConflict, operation, "media open is no longer owned", nil)
	}
	if err := ctx.Err(); err != nil {
		pending.abandoned = true
		delete(m.pendingOpens, pending.callID)
		m.releasePendingLeaseLocked(pending)
		closeDevice = pending.result.device
		pending.result.device = nil
		m.mu.Unlock()
		if closeDevice != nil {
			_ = closeDevice.Close()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, NewError(
				ErrorTimeout,
				operation,
				"media backend did not open before the deadline",
				err,
			)
		}
		return nil, NewError(ErrorBackendUnavailable, operation, "media open canceled", err)
	}

	result := pending.result
	if result.err != nil {
		delete(m.pendingOpens, pending.callID)
		m.releasePendingLeaseLocked(pending)
		closeDevice = result.device
		pending.result.device = nil
		m.mu.Unlock()
		if closeDevice != nil {
			_ = closeDevice.Close()
		}
		return nil, result.err
	}
	if result.device == nil {
		delete(m.pendingOpens, pending.callID)
		m.releasePendingLeaseLocked(pending)
		m.mu.Unlock()
		return nil, NewError(
			ErrorBackendUnavailable,
			operation,
			"media backend returned no device",
			nil,
		)
	}

	var session *Session
	release := func() {
		m.mu.Lock()
		delete(m.activeCalls, pending.callID)
		delete(m.activeEndpoints, pending.endpointLease)
		if m.sessions[pending.callID] == session {
			delete(m.sessions, pending.callID)
		}
		m.mu.Unlock()
	}
	session = &Session{
		callID:        pending.callID,
		audioPort:     result.call.AudioPort,
		backend:       result.binding.Backend,
		endpoint:      result.binding.Endpoint,
		format:        result.format,
		device:        result.device,
		options:       m.options,
		validateStart: m.startValidator(pending.callID, result.call.AudioPort, result.format),
		release:       release,
	}
	pending.result.device = nil
	delete(m.pendingOpens, pending.callID)
	m.sessions[pending.callID] = session
	m.mu.Unlock()
	return session, nil
}

func (m *Manager) abandonPendingOpen(pending *pendingOpen) {
	if pending == nil {
		return
	}
	pending.cancel()

	var closeDevice Device
	m.mu.Lock()
	current, exists := m.pendingOpens[pending.callID]
	if exists && current == pending {
		pending.abandoned = true
		if !pending.opening || pending.completed {
			delete(m.pendingOpens, pending.callID)
			m.releasePendingLeaseLocked(pending)
			closeDevice = pending.result.device
			pending.result.device = nil
		}
	}
	m.mu.Unlock()
	if closeDevice != nil {
		_ = closeDevice.Close()
	}
}

func (m *Manager) releasePendingLeaseLocked(pending *pendingOpen) {
	delete(m.activeCalls, pending.callID)
	if pending.endpointLease != "" {
		delete(m.activeEndpoints, pending.endpointLease)
	}
}

func (m *Manager) startValidator(
	callID string,
	audioPort string,
	expectedFormat Format,
) func(context.Context) error {
	return func(ctx context.Context) error {
		const operation = "start_media"

		call, err := m.source.ResolveCall(ctx, callID)
		if err != nil {
			if _, ok := AsError(err); ok {
				return err
			}
			if ctx.Err() != nil {
				return NewError(ErrorTimeout, operation, "call state revalidation timed out", ctx.Err())
			}
			return NewError(ErrorBackendUnavailable, operation, "call state is unavailable", err)
		}
		if call.ID != callID {
			return NewError(ErrorNotFound, operation, "call was not found", nil)
		}
		if !callStateAllowsMedia(call.State) {
			return NewError(
				ErrorNotActive,
				operation,
				"call state no longer permits media",
				nil,
			)
		}
		if !call.MediaAvailable || call.AudioFormat == nil {
			return NewError(
				ErrorMediaUnavailable,
				operation,
				"ModemManager no longer confirms media availability",
				nil,
			)
		}
		if call.AudioPort != audioPort {
			return NewError(
				ErrorConflict,
				operation,
				"call audio port changed after media OPEN",
				nil,
			)
		}
		currentFormat, err := ParseFormat(*call.AudioFormat)
		if err != nil {
			return err
		}
		if currentFormat != expectedFormat {
			return NewError(
				ErrorConflict,
				operation,
				"call audio format changed after media OPEN",
				nil,
			)
		}
		return nil
	}
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}

	var pendingCancels []context.CancelFunc
	var pendingDevices []Device
	m.mu.Lock()
	m.closed = true
	for callID, pending := range m.pendingOpens {
		pending.abandoned = true
		pendingCancels = append(pendingCancels, pending.cancel)
		if !pending.opening || pending.completed {
			delete(m.pendingOpens, callID)
			m.releasePendingLeaseLocked(pending)
			if pending.result.device != nil {
				pendingDevices = append(pendingDevices, pending.result.device)
				pending.result.device = nil
			}
		}
	}
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()

	var closeError error
	for _, cancel := range pendingCancels {
		cancel()
	}
	for _, device := range pendingDevices {
		closeError = errors.Join(closeError, device.Close())
	}
	for _, session := range sessions {
		closeError = errors.Join(closeError, session.Close())
	}
	return closeError
}

func (m *Manager) IsConfigured(audioPort string) bool {
	if m == nil {
		return false
	}
	binding, exists := m.bindings[strings.TrimSpace(audioPort)]
	return exists && m.backends[binding.Backend] != nil
}

func (m *Manager) Configured() bool {
	return m != nil && len(m.bindings) > 0
}

func (m *Manager) IsActive(callID string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(callID)]
	m.mu.Unlock()
	return session != nil && session.Started()
}

type Session struct {
	callID        string
	audioPort     string
	backend       BackendKind
	endpoint      string
	format        Format
	device        Device
	options       Options
	validateStart func(context.Context) error
	release       func()

	running   atomic.Bool
	started   atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error

	lifecycleMu sync.Mutex
	connection  interface {
		SetDeadline(time.Time) error
		Close() error
	}
	serveCancel context.CancelFunc
}

func (s *Session) CallID() string {
	return s.callID
}

func (s *Session) Format() Format {
	return s.format
}

func (s *Session) Backend() BackendKind {
	return s.backend
}

func (s *Session) Started() bool {
	return s != nil && s.started.Load() && !s.closed.Load()
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.lifecycleMu.Lock()
		connection := s.connection
		cancel := s.serveCancel
		s.lifecycleMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if connection != nil {
			_ = connection.SetDeadline(time.Now())
			_ = connection.Close()
		}
		s.closeErr = s.device.Close()
		s.release()
	})
	return s.closeErr
}
