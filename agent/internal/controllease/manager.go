package controllease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	defaultDuration       = 5 * time.Second
	defaultCheckInterval  = 250 * time.Millisecond
	defaultCommandTimeout = 5 * time.Second
	maxControllerIDLength = 128
)

type CallController interface {
	Snapshot(context.Context) (domain.Snapshot, error)
	HangupCall(context.Context, domain.CallCommandRequest) (domain.CommandReceipt, error)
}

type Options struct {
	Duration       time.Duration
	CheckInterval  time.Duration
	CommandTimeout time.Duration
	Now            func() time.Time
	Report         func(error)
}

type Manager struct {
	controller CallController
	duration   time.Duration
	checkEvery time.Duration
	timeout    time.Duration
	now        func() time.Time
	report     func(error)

	// gate lets validated provider commands finish before lease expiry, release,
	// startup recovery, or shutdown inspects and terminates calls. The lease is
	// validated while holding the shared side, closing the check/execute race.
	gate         sync.RWMutex
	mu           sync.Mutex
	controllerID string
	expiresAt    time.Time
	expiring     bool
	closed       bool
}

func New(controller CallController, options Options) (*Manager, error) {
	if controller == nil {
		return nil, errors.New("control lease requires a call controller")
	}
	if options.Duration < 0 || options.CheckInterval < 0 || options.CommandTimeout < 0 {
		return nil, errors.New("control lease durations cannot be negative")
	}
	duration := options.Duration
	if duration == 0 {
		duration = defaultDuration
	}
	checkEvery := options.CheckInterval
	if checkEvery == 0 {
		checkEvery = defaultCheckInterval
	}
	timeout := options.CommandTimeout
	if timeout == 0 {
		timeout = defaultCommandTimeout
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		controller: controller,
		duration:   duration,
		checkEvery: checkEvery,
		timeout:    timeout,
		now:        now,
		report:     options.Report,
	}, nil
}

func (m *Manager) Renew(controllerID string) (domain.ControlLeaseStatus, error) {
	controllerID, err := normalizeControllerID(controllerID)
	if err != nil {
		return domain.ControlLeaseStatus{}, err
	}
	now := m.now().UTC()
	expiresAt := now.Add(m.duration)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return domain.ControlLeaseStatus{}, domain.FailedPrecondition(
			"renew_control_lease",
			"control lease manager is shutting down",
			nil,
		)
	}
	if m.expiring {
		return domain.ControlLeaseStatus{}, domain.Conflict(
			"renew_control_lease",
			"expired call cleanup is still in progress",
		)
	}
	if m.controllerID == "" {
		m.controllerID = controllerID
		m.expiresAt = expiresAt
		return domain.ControlLeaseStatus{
			ControllerID: controllerID,
			ExpiresAt:    expiresAt,
		}, nil
	}
	if m.controllerID == controllerID && now.Before(m.expiresAt) {
		m.expiresAt = expiresAt
		return domain.ControlLeaseStatus{
			ControllerID: controllerID,
			ExpiresAt:    expiresAt,
		}, nil
	}
	if now.Before(m.expiresAt) {
		return domain.ControlLeaseStatus{}, domain.Conflict(
			"renew_control_lease",
			"another application instance owns the active control lease",
		)
	}
	return domain.ControlLeaseStatus{}, domain.Conflict(
		"renew_control_lease",
		"expired call cleanup is pending",
	)
}

func (m *Manager) Protect(controllerID string) (func(), error) {
	controllerID, err := normalizeControllerID(controllerID)
	if err != nil {
		return nil, err
	}
	m.gate.RLock()
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed ||
		m.controllerID != controllerID ||
		!now.Before(m.expiresAt) {
		m.gate.RUnlock()
		return nil, domain.FailedPrecondition(
			"require_control_lease",
			"application control lease is missing or expired",
			nil,
		)
	}
	return m.gate.RUnlock, nil
}

func (m *Manager) Release(ctx context.Context, controllerID string) error {
	controllerID, err := normalizeControllerID(controllerID)
	if err != nil {
		return err
	}
	m.gate.Lock()
	defer m.gate.Unlock()
	m.mu.Lock()
	if m.closed || m.expiring {
		m.mu.Unlock()
		return nil
	}
	if m.controllerID == "" {
		m.mu.Unlock()
		return nil
	}
	if m.controllerID != controllerID {
		m.mu.Unlock()
		return domain.Conflict(
			"release_control_lease",
			"the control lease belongs to another application instance",
		)
	}
	m.controllerID = ""
	m.expiresAt = time.Time{}
	m.expiring = true
	m.mu.Unlock()

	err = m.hangupAll(ctx, "released")
	m.finishExpiration(err == nil)
	return err
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.gate.Lock()
	defer m.gate.Unlock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.controllerID = ""
	m.expiresAt = time.Time{}
	m.expiring = true
	m.mu.Unlock()

	err := m.hangupAll(ctx, "agent_shutdown")
	return err
}

func (m *Manager) Recover(ctx context.Context) error {
	m.gate.Lock()
	defer m.gate.Unlock()
	return m.hangupAll(ctx, "agent_startup")
}

func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(m.checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := m.expire(ctx)
			if err != nil && m.report != nil {
				m.report(err)
			}
		}
	}
}

func (m *Manager) expire(ctx context.Context) (bool, error) {
	m.gate.Lock()
	defer m.gate.Unlock()
	if !m.beginExpiration() {
		return false, nil
	}
	err := m.hangupAll(ctx, "lease_expired")
	m.finishExpiration(err == nil)
	return true, err
}

func (m *Manager) beginExpiration() bool {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	if m.expiring {
		return true
	}
	if m.controllerID == "" || now.Before(m.expiresAt) {
		return false
	}
	m.controllerID = ""
	m.expiresAt = time.Time{}
	m.expiring = true
	return true
}

func (m *Manager) finishExpiration(success bool) {
	if !success {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.expiring = false
	}
	m.mu.Unlock()
}

func (m *Manager) hangupAll(ctx context.Context, reason string) error {
	ctx = normalizeContext(ctx)
	snapshotContext, cancel := context.WithTimeout(ctx, m.timeout)
	snapshot, err := m.controller.Snapshot(snapshotContext)
	cancel()
	if err != nil {
		return fmt.Errorf("inspect calls during %s: %w", reason, err)
	}

	var result error
	failedLines := make(map[string]struct{})
	for _, call := range activeCalls(snapshot.Calls) {
		if call.LineID != "" {
			if _, failed := failedLines[call.LineID]; failed {
				continue
			}
		}
		commandContext, commandCancel := context.WithTimeout(ctx, m.timeout)
		_, err := m.controller.HangupCall(
			commandContext,
			domain.CallCommandRequest{
				RequestID: safetyRequestID(reason, call.ID),
				CallID:    call.ID,
			},
		)
		commandCancel()
		if err != nil {
			if operationError, ok := domain.AsOperationError(err); ok &&
				operationError.Code == domain.ErrorNotFound {
				// The safety snapshot can race a remote hangup. Absence is the
				// desired postcondition for this target and must not suppress
				// cleanup of another still-live call on the same line.
				continue
			}
			if call.LineID != "" {
				failedLines[call.LineID] = struct{}{}
			}
			if errors.Is(err, domain.ErrForcedCallTermination) {
				incident := fmt.Errorf(
					"hang up call %s during %s required a forced modem reset: %w",
					call.ID,
					reason,
					err,
				)
				if m.report != nil {
					m.report(incident)
				} else {
					slog.Error(
						"control lease required forced modem reset",
						"component", "control_lease",
						"call_id", call.ID,
						"line_id", call.LineID,
						"reason", reason,
						"error", err,
					)
				}
				continue
			}
			result = errors.Join(
				result,
				fmt.Errorf("hang up call %s during %s: %w", call.ID, reason, err),
			)
			continue
		}
		slog.Warn(
			"call ended by control lease",
			"component", "control_lease",
			"call_id", call.ID,
			"line_id", call.LineID,
			"reason", reason,
		)
	}
	return result
}

func activeCalls(calls []domain.Call) []domain.Call {
	active := make([]domain.Call, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" || call.StateCode == 7 {
			continue
		}
		active = append(active, call)
	}
	return active
}

func safetyRequestID(reason, callID string) string {
	digest := sha256.Sum256([]byte(reason + "\x00" + callID))
	return "safety-" + hex.EncodeToString(digest[:12])
}

func normalizeControllerID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxControllerIDLength {
		return "", domain.InvalidArgument(
			"control_lease",
			"controller id is required and must be valid",
		)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", domain.InvalidArgument(
				"control_lease",
				"controller id is required and must be valid",
			)
		}
	}
	return value, nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
