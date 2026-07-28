package calllease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	defaultDuration      = 15 * time.Second
	defaultCheckInterval = 250 * time.Millisecond
	defaultHangupTimeout = 5 * time.Second
	defaultRetryDelay    = time.Second
	maxHolderIDLength    = 128
	maxCallIDLength      = 256
)

var (
	ErrInvalidArgument = errors.New("invalid browser call lease")
	ErrCallNotFound    = errors.New("browser call lease call not found")
	ErrCallNotActive   = errors.New("browser call lease call is not active")
)

type CallStore interface {
	CallByID(context.Context, string) (store.Call, error)
}

type CallController interface {
	CallAction(context.Context, communication.CallActionInput) (store.Call, error)
}

type Options struct {
	Duration      time.Duration
	CheckInterval time.Duration
	HangupTimeout time.Duration
	RetryDelay    time.Duration
	Now           func() time.Time
	Report        func(error)
}

type Status struct {
	CallID    string    `json:"call_id"`
	HolderID  string    `json:"holder_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type callEntry struct {
	holders          map[string]time.Time
	unclaimedExpires time.Time
	ending           bool
	attempt          uint64
}

type Manager struct {
	calls         CallStore
	controller    CallController
	duration      time.Duration
	checkInterval time.Duration
	hangupTimeout time.Duration
	retryDelay    time.Duration
	now           func() time.Time
	report        func(error)

	mu      sync.Mutex
	entries map[string]*callEntry
}

func New(
	calls CallStore,
	controller CallController,
	options Options,
) (*Manager, error) {
	if calls == nil || controller == nil {
		return nil, errors.New("browser call lease requires a call store and controller")
	}
	if options.Duration < 0 ||
		options.CheckInterval < 0 ||
		options.HangupTimeout < 0 ||
		options.RetryDelay < 0 {
		return nil, errors.New("browser call lease durations cannot be negative")
	}
	duration := options.Duration
	if duration == 0 {
		duration = defaultDuration
	}
	checkInterval := options.CheckInterval
	if checkInterval == 0 {
		checkInterval = defaultCheckInterval
	}
	hangupTimeout := options.HangupTimeout
	if hangupTimeout == 0 {
		hangupTimeout = defaultHangupTimeout
	}
	retryDelay := options.RetryDelay
	if retryDelay == 0 {
		retryDelay = defaultRetryDelay
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		calls:         calls,
		controller:    controller,
		duration:      duration,
		checkInterval: checkInterval,
		hangupTimeout: hangupTimeout,
		retryDelay:    retryDelay,
		now:           now,
		report:        options.Report,
		entries:       make(map[string]*callEntry),
	}, nil
}

func (m *Manager) Renew(
	ctx context.Context,
	callID string,
	holderID string,
) (Status, error) {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return Status{}, err
	}
	call, err := m.calls.CallByID(normalizeContext(ctx), callID)
	if errors.Is(err, store.ErrCallNotFound) {
		return Status{}, ErrCallNotFound
	}
	if err != nil {
		return Status{}, fmt.Errorf("read browser-leased call: %w", err)
	}
	if !leaseablePhase(call.Phase) {
		return Status{}, ErrCallNotActive
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.duration)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[callID]
	if entry == nil {
		entry = &callEntry{
			holders:          make(map[string]time.Time),
			unclaimedExpires: expiresAt,
		}
		m.entries[callID] = entry
	}
	if entry.ending {
		return Status{}, ErrCallNotActive
	}
	entry.holders[holderID] = expiresAt
	return Status{
		CallID:    callID,
		HolderID:  holderID,
		ExpiresAt: expiresAt,
	}, nil
}

func (m *Manager) ReconcileAuthoritativeCalls(
	_ context.Context,
	calls []store.Call,
) error {
	now := m.now().UTC()
	active := make(map[string]struct{}, len(calls))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, call := range calls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" || !trackedPhase(call.Phase) {
			continue
		}
		active[callID] = struct{}{}
		if m.entries[callID] == nil {
			m.entries[callID] = &callEntry{
				holders:          make(map[string]time.Time),
				unclaimedExpires: now.Add(m.duration),
			}
		}
	}
	for callID := range m.entries {
		if _, found := active[callID]; !found {
			delete(m.entries, callID)
		}
	}
	return nil
}

func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, expired := range m.expiredCalls() {
				m.endExpiredCall(ctx, expired.callID, expired.attempt)
			}
		}
	}
}

type expiredCall struct {
	callID  string
	attempt uint64
}

func (m *Manager) expiredCalls() []expiredCall {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	expired := make([]expiredCall, 0)
	for callID, entry := range m.entries {
		for holderID, expiresAt := range entry.holders {
			if !now.Before(expiresAt) {
				delete(entry.holders, holderID)
			}
		}
		if entry.ending ||
			len(entry.holders) > 0 ||
			now.Before(entry.unclaimedExpires) {
			continue
		}
		entry.ending = true
		entry.attempt++
		expired = append(expired, expiredCall{
			callID:  callID,
			attempt: entry.attempt,
		})
	}
	return expired
}

func (m *Manager) endExpiredCall(
	ctx context.Context,
	callID string,
	attempt uint64,
) {
	m.mu.Lock()
	entry := m.entries[callID]
	valid := entry != nil && entry.ending && entry.attempt == attempt
	m.mu.Unlock()
	if !valid {
		return
	}
	commandContext, cancel := context.WithTimeout(
		normalizeContext(ctx),
		m.hangupTimeout,
	)
	_, err := m.controller.CallAction(
		commandContext,
		communication.CallActionInput{
			RequestID: expirationRequestID(callID, attempt),
			CallID:    callID,
			Action:    "hangup",
		},
	)
	cancel()
	m.mu.Lock()
	if entry := m.entries[callID]; entry != nil && entry.attempt == attempt {
		entry.ending = false
		entry.unclaimedExpires = m.now().UTC().Add(m.retryDelay)
	}
	m.mu.Unlock()
	if err != nil && m.report != nil {
		m.report(fmt.Errorf("end browser-disconnected call %s: %w", callID, err))
	}
}

func normalizeIDs(callID, holderID string) (string, string, error) {
	callID = strings.TrimSpace(callID)
	holderID = strings.TrimSpace(holderID)
	if callID == "" ||
		holderID == "" ||
		len(callID) > maxCallIDLength ||
		len(holderID) > maxHolderIDLength {
		return "", "", ErrInvalidArgument
	}
	return callID, holderID, nil
}

func leaseablePhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "dialing", "ringing", "connecting", "active":
		return true
	default:
		return false
	}
}

func trackedPhase(phase string) bool {
	return strings.EqualFold(strings.TrimSpace(phase), "unknown") ||
		leaseablePhase(phase)
}

func expirationRequestID(callID string, attempt uint64) string {
	digest := sha256.Sum256([]byte(callID))
	return fmt.Sprintf(
		"browser-expiry-%s-%d",
		hex.EncodeToString(digest[:10]),
		attempt,
	)
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
