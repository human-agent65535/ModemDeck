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
	hangupAttempts        = 3
	hangupRetryDelay      = time.Second
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
	if m.controllerID != "" &&
		m.controllerID != controllerID &&
		now.Before(m.expiresAt) {
		return domain.ControlLeaseStatus{}, domain.Conflict(
			"renew_control_lease",
			"another application instance owns the active control lease",
		)
	}
	m.controllerID = controllerID
	m.expiresAt = expiresAt
	m.expiring = false
	return domain.ControlLeaseStatus{
		ControllerID: controllerID,
		ExpiresAt:    expiresAt,
	}, nil
}

func (m *Manager) Require(controllerID string) error {
	controllerID, err := normalizeControllerID(controllerID)
	if err != nil {
		return err
	}
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed ||
		m.controllerID != controllerID ||
		!now.Before(m.expiresAt) {
		return domain.FailedPrecondition(
			"require_control_lease",
			"application control lease is missing or expired",
			nil,
		)
	}
	return nil
}

func (m *Manager) Release(ctx context.Context, controllerID string) error {
	controllerID, err := normalizeControllerID(controllerID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed || m.expiring {
		m.mu.Unlock()
		return nil
	}
	if m.controllerID != "" && m.controllerID != controllerID {
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
	m.finishExpiration()
	return err
}

func (m *Manager) Shutdown(ctx context.Context) error {
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
	m.finishExpiration()
	return err
}

func (m *Manager) Recover(ctx context.Context) error {
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
			if !m.beginExpiration() {
				continue
			}
			if err := m.hangupAll(ctx, "lease_expired"); err != nil && m.report != nil {
				m.report(err)
			}
			m.finishExpiration()
		}
	}
}

func (m *Manager) beginExpiration() bool {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.controllerID == "" || m.expiring || now.Before(m.expiresAt) {
		return false
	}
	m.controllerID = ""
	m.expiresAt = time.Time{}
	m.expiring = true
	return true
}

func (m *Manager) finishExpiration() {
	m.mu.Lock()
	if !m.closed {
		m.expiring = false
	}
	m.mu.Unlock()
}

func (m *Manager) hangupAll(ctx context.Context, reason string) error {
	ctx = normalizeContext(ctx)
	var result error
	for attempt := 1; attempt <= hangupAttempts; attempt++ {
		attemptContext, cancel := context.WithTimeout(ctx, m.timeout)
		snapshot, err := m.controller.Snapshot(attemptContext)
		cancel()
		if err != nil {
			result = errors.Join(result, fmt.Errorf("inspect calls during %s: %w", reason, err))
		} else {
			active := activeCalls(snapshot.Calls)
			if len(active) == 0 {
				return nil
			}
			for _, call := range active {
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
		}
		if attempt == hangupAttempts {
			break
		}
		timer := time.NewTimer(hangupRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(result, ctx.Err())
		case <-timer.C:
		}
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
