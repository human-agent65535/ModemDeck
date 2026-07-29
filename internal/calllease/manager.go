package calllease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	defaultDuration       = 15 * time.Second
	defaultCheckInterval  = 250 * time.Millisecond
	defaultReleaseTimeout = 20 * time.Second
	maxHolderIDLength     = 128
	maxCallIDLength       = 256
	maxLineIDLength       = 256
)

var (
	ErrInvalidArgument     = errors.New("invalid browser call lease")
	ErrCallNotFound        = errors.New("browser call lease call not found")
	ErrCallNotActive       = errors.New("browser call lease call is not active")
	ErrCallOwned           = errors.New("browser call lease is owned by another browser")
	ErrHolderBusy          = errors.New("browser already owns another call lease")
	ErrNotOwner            = errors.New("browser does not own the call lease")
	ErrReservationNotFound = errors.New("outgoing call reservation not found")
)

type CallStore interface {
	CallByID(context.Context, string) (store.Call, error)
	ActiveCalls(context.Context) ([]store.Call, error)
}

type CallController interface {
	EndCall(context.Context, string) error
}

type Options struct {
	Duration       time.Duration
	CheckInterval  time.Duration
	ReleaseTimeout time.Duration
	Now            func() time.Time
	Report         func(error)
}

type Status struct {
	CallID    string    `json:"call_id"`
	HolderID  string    `json:"holder_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type OutgoingReservation struct {
	ID           string
	LineID       string
	HolderID     string
	CreatedAt    time.Time
	ControlState ControlState
}

type ControlState string

const (
	ControlAvailable ControlState = "available"
	ControlOwned     ControlState = "owned"
	ControlOccupied  ControlState = "occupied"
)

type callEntry struct {
	lineID           string
	holderID         string
	expiresAt        time.Time
	unclaimedExpires time.Time
	ending           bool
	attempt          uint64
}

type outgoingReservation struct {
	id        string
	lineID    string
	holderID  string
	createdAt time.Time
}

type Manager struct {
	calls          CallStore
	controller     CallController
	duration       time.Duration
	checkInterval  time.Duration
	releaseTimeout time.Duration
	now            func() time.Time
	report         func(error)

	mu           sync.Mutex
	entries      map[string]*callEntry
	reservations map[string]*outgoingReservation
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
		options.ReleaseTimeout < 0 {
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
	releaseTimeout := options.ReleaseTimeout
	if releaseTimeout == 0 {
		releaseTimeout = defaultReleaseTimeout
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		calls:          calls,
		controller:     controller,
		duration:       duration,
		checkInterval:  checkInterval,
		releaseTimeout: releaseTimeout,
		now:            now,
		report:         options.Report,
		entries:        make(map[string]*callEntry),
		reservations:   make(map[string]*outgoingReservation),
	}, nil
}

func (m *Manager) ReserveOutgoing(
	ctx context.Context,
	reservationID string,
	lineID string,
	holderID string,
) (OutgoingReservation, error) {
	reservationID, lineID, holderID, err := normalizeReservationIDs(
		reservationID,
		lineID,
		holderID,
	)
	if err != nil {
		return OutgoingReservation{}, err
	}
	activeCalls, err := m.calls.ActiveCalls(normalizeContext(ctx))
	if err != nil {
		return OutgoingReservation{}, fmt.Errorf("read active calls before reserving a line: %w", err)
	}

	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.reservations[reservationID]; existing != nil {
		if existing.lineID != lineID || existing.holderID != holderID {
			return OutgoingReservation{}, ErrCallOwned
		}
		return outgoingReservationStatus(existing, holderID), nil
	}
	for _, call := range activeCalls {
		if trackedPhase(call.Phase) && strings.TrimSpace(call.LineID) == lineID {
			return OutgoingReservation{}, ErrCallOwned
		}
	}
	for _, entry := range m.entries {
		if entry.lineID == lineID {
			return OutgoingReservation{}, ErrCallOwned
		}
		if entry.holderID == holderID &&
			!entry.ending &&
			now.Before(entry.expiresAt) {
			return OutgoingReservation{}, ErrHolderBusy
		}
	}
	for _, reservation := range m.reservations {
		if reservation.lineID == lineID {
			return OutgoingReservation{}, ErrCallOwned
		}
		if reservation.holderID == holderID {
			return OutgoingReservation{}, ErrHolderBusy
		}
	}
	reservation := &outgoingReservation{
		id:        reservationID,
		lineID:    lineID,
		holderID:  holderID,
		createdAt: now,
	}
	m.reservations[reservationID] = reservation
	return outgoingReservationStatus(reservation, holderID), nil
}

func (m *Manager) ActivateOutgoing(
	ctx context.Context,
	reservationID string,
	callID string,
	holderID string,
) (Status, error) {
	reservationID, holderID, err := normalizeReservationAndHolder(
		reservationID,
		holderID,
	)
	if err != nil {
		return Status{}, err
	}
	callID = strings.TrimSpace(callID)
	if callID == "" || len(callID) > maxCallIDLength {
		return Status{}, ErrInvalidArgument
	}
	call, err := m.leaseableCall(ctx, callID)
	if err != nil {
		return Status{}, err
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.duration)
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation := m.reservations[reservationID]
	if reservation == nil {
		return Status{}, ErrReservationNotFound
	}
	if reservation.holderID != holderID {
		return Status{}, ErrNotOwner
	}
	if strings.TrimSpace(call.LineID) != reservation.lineID {
		return Status{}, ErrInvalidArgument
	}
	entry := m.entries[callID]
	if entry == nil {
		entry = &callEntry{}
		m.entries[callID] = entry
	}
	if entry.ending {
		return Status{}, ErrCallNotActive
	}
	if entry.holderID != "" && entry.holderID != holderID {
		return Status{}, ErrCallOwned
	}
	for otherCallID, otherEntry := range m.entries {
		if otherCallID == callID || otherEntry.ending {
			continue
		}
		if otherEntry.lineID == reservation.lineID {
			return Status{}, ErrCallOwned
		}
		if otherEntry.holderID == holderID &&
			now.Before(otherEntry.expiresAt) {
			return Status{}, ErrHolderBusy
		}
	}
	entry.lineID = reservation.lineID
	entry.holderID = holderID
	entry.expiresAt = expiresAt
	entry.unclaimedExpires = time.Time{}
	delete(m.reservations, reservationID)
	return Status{
		CallID:    callID,
		HolderID:  holderID,
		ExpiresAt: expiresAt,
	}, nil
}

func (m *Manager) ReleaseOutgoing(
	reservationID string,
	holderID string,
) (bool, error) {
	reservationID, holderID, err := normalizeReservationAndHolder(
		reservationID,
		holderID,
	)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation := m.reservations[reservationID]
	if reservation == nil {
		return false, nil
	}
	if reservation.holderID != holderID {
		return false, ErrNotOwner
	}
	delete(m.reservations, reservationID)
	return true, nil
}

func (m *Manager) OutgoingReservations(
	holderID string,
) ([]OutgoingReservation, error) {
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]OutgoingReservation, 0, len(m.reservations))
	for _, reservation := range m.reservations {
		result = append(result, outgoingReservationStatus(reservation, holderID))
	}
	return result, nil
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
	if _, err := m.leaseableCall(ctx, callID); err != nil {
		return Status{}, err
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.duration)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[callID]
	if entry == nil {
		return Status{}, ErrNotOwner
	}
	if entry.ending {
		return Status{}, ErrCallNotActive
	}
	if entry.holderID != holderID {
		return Status{}, ErrNotOwner
	}
	if !now.Before(entry.expiresAt) {
		return Status{}, ErrCallNotActive
	}
	entry.expiresAt = expiresAt
	return Status{
		CallID:    callID,
		HolderID:  holderID,
		ExpiresAt: expiresAt,
	}, nil
}

func (m *Manager) Claim(
	ctx context.Context,
	callID string,
	holderID string,
) (Status, error) {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return Status{}, err
	}
	call, err := m.leaseableCall(ctx, callID)
	if err != nil {
		return Status{}, err
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.duration)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[callID]
	if entry == nil {
		entry = &callEntry{
			lineID:           strings.TrimSpace(call.LineID),
			unclaimedExpires: m.unclaimedDeadline(call, now),
		}
		m.entries[callID] = entry
	} else if entry.lineID == "" {
		entry.lineID = strings.TrimSpace(call.LineID)
	}
	if entry.ending {
		return Status{}, ErrCallNotActive
	}
	if entry.holderID != "" {
		if entry.holderID != holderID {
			return Status{}, ErrCallOwned
		}
		if !now.Before(entry.expiresAt) {
			return Status{}, ErrCallNotActive
		}
		entry.expiresAt = expiresAt
		return Status{
			CallID:    callID,
			HolderID:  holderID,
			ExpiresAt: expiresAt,
		}, nil
	}
	for otherCallID, otherEntry := range m.entries {
		if otherCallID == callID ||
			otherEntry.ending ||
			otherEntry.holderID != holderID ||
			!now.Before(otherEntry.expiresAt) {
			continue
		}
		return Status{}, ErrHolderBusy
	}
	if !entry.unclaimedExpires.IsZero() &&
		!now.Before(entry.unclaimedExpires) {
		return Status{}, ErrCallNotActive
	}
	entry.holderID = holderID
	entry.expiresAt = expiresAt
	entry.unclaimedExpires = time.Time{}
	return Status{
		CallID:    callID,
		HolderID:  holderID,
		ExpiresAt: expiresAt,
	}, nil
}

func (m *Manager) Require(
	ctx context.Context,
	callID string,
	holderID string,
) error {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return err
	}
	if _, err := m.leaseableCall(ctx, callID); err != nil {
		return err
	}

	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[callID]
	if entry == nil || entry.holderID != holderID {
		return ErrNotOwner
	}
	if entry.ending || !now.Before(entry.expiresAt) {
		return ErrCallNotActive
	}
	return nil
}

func (m *Manager) ControlState(
	ctx context.Context,
	callID string,
	holderID string,
) (ControlState, error) {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return "", err
	}
	call, err := m.leaseableCall(ctx, callID)
	if err != nil {
		return "", err
	}

	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[callID]
	if entry != nil {
		if entry.ending {
			return ControlOccupied, nil
		}
		if entry.holderID != "" {
			if !now.Before(entry.expiresAt) {
				return ControlOccupied, nil
			}
			if entry.holderID == holderID {
				return ControlOwned, nil
			}
			return ControlOccupied, nil
		}
	}
	if strings.EqualFold(strings.TrimSpace(call.Direction), "incoming") &&
		strings.EqualFold(strings.TrimSpace(call.Phase), "ringing") {
		return ControlAvailable, nil
	}
	return ControlOccupied, nil
}

func (m *Manager) Release(
	ctx context.Context,
	callID string,
	holderID string,
) error {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return err
	}
	call, err := m.leaseableCall(ctx, callID)
	if err != nil {
		return err
	}

	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[callID]
	if entry == nil || entry.holderID != holderID {
		return ErrNotOwner
	}
	if entry.ending {
		return ErrCallNotActive
	}
	entry.holderID = ""
	entry.expiresAt = time.Time{}
	entry.unclaimedExpires = m.unclaimedDeadline(call, now)
	return nil
}

func (m *Manager) ReconcileAuthoritativeCalls(
	ctx context.Context,
	calls []store.Call,
) error {
	now := m.now().UTC()
	active := make(map[string]struct{}, len(calls))
	type missingEntry struct {
		callID string
		entry  *callEntry
	}
	missing := make([]missingEntry, 0)
	m.mu.Lock()
	for _, call := range calls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" || !trackedPhase(call.Phase) {
			continue
		}
		active[callID] = struct{}{}
		entry := m.entries[callID]
		if entry == nil {
			m.entries[callID] = &callEntry{
				lineID:           strings.TrimSpace(call.LineID),
				unclaimedExpires: m.unclaimedDeadline(call, now),
			}
			continue
		}
		entry.lineID = strings.TrimSpace(call.LineID)
		if entry.holderID == "" &&
			entry.unclaimedExpires.IsZero() {
			entry.unclaimedExpires = m.unclaimedDeadline(call, now)
		}
	}
	for callID, entry := range m.entries {
		if _, found := active[callID]; !found {
			missing = append(missing, missingEntry{
				callID: callID,
				entry:  entry,
			})
		}
	}
	m.mu.Unlock()

	var result error
	for _, candidate := range missing {
		call, err := m.calls.CallByID(
			normalizeContext(ctx),
			candidate.callID,
		)
		if err == nil && trackedPhase(call.Phase) {
			continue
		}
		if err != nil && !errors.Is(err, store.ErrCallNotFound) {
			result = errors.Join(result, fmt.Errorf(
				"verify browser call lease removal for %s: %w",
				candidate.callID,
				err,
			))
			continue
		}

		m.mu.Lock()
		if m.entries[candidate.callID] == candidate.entry {
			delete(m.entries, candidate.callID)
		}
		m.mu.Unlock()
	}
	return result
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
		if entry.ending {
			continue
		}
		if entry.holderID != "" {
			if now.Before(entry.expiresAt) {
				continue
			}
			entry.holderID = ""
			entry.expiresAt = time.Time{}
			entry.unclaimedExpires = now
		}
		if entry.unclaimedExpires.IsZero() ||
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
	releaseContext, cancel := context.WithTimeout(
		context.WithoutCancel(normalizeContext(ctx)),
		m.releaseTimeout,
	)
	err := m.controller.EndCall(releaseContext, callID)
	cancel()
	if err != nil && m.report != nil {
		m.report(fmt.Errorf(
			"end call after browser lease expired for %s: %w",
			callID,
			err,
		))
	}
}

func normalizeIDs(callID, holderID string) (string, string, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" || len(callID) > maxCallIDLength {
		return "", "", ErrInvalidArgument
	}
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return "", "", err
	}
	return callID, holderID, nil
}

func normalizeReservationIDs(
	reservationID string,
	lineID string,
	holderID string,
) (string, string, string, error) {
	reservationID, holderID, err := normalizeReservationAndHolder(
		reservationID,
		holderID,
	)
	if err != nil {
		return "", "", "", err
	}
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || len(lineID) > maxLineIDLength {
		return "", "", "", ErrInvalidArgument
	}
	return reservationID, lineID, holderID, nil
}

func normalizeReservationAndHolder(
	reservationID string,
	holderID string,
) (string, string, error) {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" || len(reservationID) > maxCallIDLength {
		return "", "", ErrInvalidArgument
	}
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return "", "", err
	}
	return reservationID, holderID, nil
}

func outgoingReservationStatus(
	reservation *outgoingReservation,
	holderID string,
) OutgoingReservation {
	controlState := ControlOccupied
	if reservation.holderID == holderID {
		controlState = ControlOwned
	}
	return OutgoingReservation{
		ID:           reservation.id,
		LineID:       reservation.lineID,
		HolderID:     reservation.holderID,
		CreatedAt:    reservation.createdAt,
		ControlState: controlState,
	}
}

func NormalizeHolderID(holderID string) (string, error) {
	holderID = strings.TrimSpace(holderID)
	if holderID == "" || len(holderID) > maxHolderIDLength {
		return "", ErrInvalidArgument
	}
	return holderID, nil
}

func (m *Manager) leaseableCall(
	ctx context.Context,
	callID string,
) (store.Call, error) {
	call, err := m.calls.CallByID(normalizeContext(ctx), callID)
	if errors.Is(err, store.ErrCallNotFound) {
		return store.Call{}, ErrCallNotFound
	}
	if err != nil {
		return store.Call{}, fmt.Errorf("read browser-leased call: %w", err)
	}
	if !leaseablePhase(call.Phase) {
		return store.Call{}, ErrCallNotActive
	}
	return call, nil
}

func (m *Manager) unclaimedDeadline(
	call store.Call,
	now time.Time,
) time.Time {
	if strings.EqualFold(strings.TrimSpace(call.Direction), "incoming") &&
		strings.EqualFold(strings.TrimSpace(call.Phase), "ringing") {
		return time.Time{}
	}
	return now.Add(m.duration)
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

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
