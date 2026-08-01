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
	maxSubjectIDLength    = 256
	maxCallIDLength       = 256
	maxLineIDLength       = 256
	restartOrphanHolderID = "system-restart-orphan"
)

var (
	ErrInvalidArgument     = errors.New("invalid call ownership request")
	ErrCallNotFound        = errors.New("owned call not found")
	ErrCallNotActive       = errors.New("owned call is not active")
	ErrCallOwned           = errors.New("call or line is owned by another session")
	ErrHolderBusy          = errors.New("session already owns another call")
	ErrNotOwner            = errors.New("session does not own the call")
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
	HolderID  string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
}

type OutgoingReservation struct {
	ID           string
	LineID       string
	HolderID     string
	Created      bool
	CreatedAt    time.Time
	ControlState ControlState
}

type ProjectedCall struct {
	Call         store.Call
	ControlState ControlState
}

type ActiveProjection struct {
	Calls        []ProjectedCall
	Reservations []OutgoingReservation
}

type ControlState string

const (
	ControlAvailable ControlState = "available"
	ControlOwned     ControlState = "owned"
	ControlOccupied  ControlState = "occupied"
)

// Owner identifies both the authenticated credential that owns one call and
// the signed-in user that credential belongs to. HolderID is the immutable
// call-control boundary. SubjectID is used only for explicit account-wide
// revocation such as an administrator disabling a user or replacing their
// password; it never permits another credential for that user to take over.
type Owner struct {
	HolderID  string
	SubjectID string
}

// record is the single object used from outgoing reservation through terminal
// call reconciliation. holderID is immutable once set. orphanAt is only a
// deadline for ending the call; it never makes the call available to another
// holder.
type record struct {
	requestID  string
	callID     string
	lineID     string
	holderID   string
	subjectID  string
	createdAt  time.Time
	orphanAt   time.Time
	mediaAlive bool
	ending     bool
}

type Manager struct {
	calls          CallStore
	controller     CallController
	duration       time.Duration
	checkInterval  time.Duration
	releaseTimeout time.Duration
	now            func() time.Time
	report         func(error)

	mu          sync.Mutex
	records     []*record
	initialized bool
}

func New(
	calls CallStore,
	controller CallController,
	options Options,
) (*Manager, error) {
	if calls == nil || controller == nil {
		return nil, errors.New("call ownership requires a call store and controller")
	}
	if options.Duration < 0 ||
		options.CheckInterval < 0 ||
		options.ReleaseTimeout < 0 {
		return nil, errors.New("call ownership durations cannot be negative")
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
	}, nil
}

func (m *Manager) ReserveOutgoing(
	ctx context.Context,
	requestID string,
	lineID string,
	holderID string,
) (OutgoingReservation, error) {
	return m.ReserveOutgoingFor(ctx, requestID, lineID, Owner{
		HolderID:  holderID,
		SubjectID: holderID,
	})
}

func (m *Manager) ReserveOutgoingFor(
	ctx context.Context,
	requestID string,
	lineID string,
	owner Owner,
) (OutgoingReservation, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return OutgoingReservation{}, err
	}
	requestID, lineID, holderID, err := normalizeReservationIDs(
		requestID,
		lineID,
		owner.HolderID,
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
	if existing := m.findRequestLocked(requestID); existing != nil {
		if existing.lineID != lineID ||
			existing.holderID != holderID ||
			existing.subjectID != owner.SubjectID {
			return OutgoingReservation{}, ErrCallOwned
		}
		return outgoingReservationStatus(existing, holderID, false), nil
	}
	for _, call := range activeCalls {
		if trackedPhase(call.Phase) && strings.TrimSpace(call.LineID) == lineID {
			return OutgoingReservation{}, ErrCallOwned
		}
	}
	for _, existing := range m.records {
		if existing.lineID == lineID {
			return OutgoingReservation{}, ErrCallOwned
		}
		if existing.holderID == holderID {
			return OutgoingReservation{}, ErrHolderBusy
		}
	}
	record := &record{
		requestID: requestID,
		lineID:    lineID,
		holderID:  holderID,
		subjectID: owner.SubjectID,
		createdAt: now,
	}
	m.records = append(m.records, record)
	return outgoingReservationStatus(record, holderID, true), nil
}

func (m *Manager) ActivateOutgoing(
	ctx context.Context,
	requestID string,
	callID string,
	holderID string,
) (Status, error) {
	requestID, holderID, err := normalizeReservationAndHolder(requestID, holderID)
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
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation := m.findRequestLocked(requestID)
	if reservation == nil {
		return Status{}, ErrReservationNotFound
	}
	if reservation.holderID != holderID {
		return Status{}, ErrNotOwner
	}
	if strings.TrimSpace(call.LineID) != reservation.lineID {
		return Status{}, ErrInvalidArgument
	}
	if reservation.callID != "" && reservation.callID != callID {
		return Status{}, ErrInvalidArgument
	}
	if reservation.ending {
		return Status{}, ErrCallNotActive
	}
	if existing := m.findCallLocked(callID); existing != nil && existing != reservation {
		if existing.holderID != "" || existing.ending {
			return Status{}, ErrCallOwned
		}
		m.removeRecordLocked(existing)
	}
	for _, existing := range m.records {
		if existing == reservation {
			continue
		}
		if existing.lineID == reservation.lineID && recordBlocksLine(existing) {
			return Status{}, ErrCallOwned
		}
		if existing.holderID == holderID {
			return Status{}, ErrHolderBusy
		}
	}
	reservation.callID = callID
	reservation.orphanAt = now.Add(m.duration)
	return statusFor(reservation), nil
}

func (m *Manager) ReleaseOutgoing(requestID string, holderID string) (bool, error) {
	requestID, holderID, err := normalizeReservationAndHolder(requestID, holderID)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation := m.findRequestLocked(requestID)
	if reservation == nil {
		return false, nil
	}
	if reservation.holderID != holderID {
		return false, ErrNotOwner
	}
	if reservation.callID != "" {
		return false, nil
	}
	m.removeRecordLocked(reservation)
	return true, nil
}

// AwaitOutgoingResolution keeps an indeterminate dial reservation closed until
// the next complete authoritative call snapshot. orphanAt is also a bounded
// fallback if authoritative snapshots remain unavailable.
func (m *Manager) AwaitOutgoingResolution(requestID string, holderID string) error {
	requestID, holderID, err := normalizeReservationAndHolder(requestID, holderID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.findRequestLocked(requestID)
	if record == nil {
		return ErrReservationNotFound
	}
	if record.holderID != holderID {
		return ErrNotOwner
	}
	if record.callID == "" {
		record.orphanAt = m.now().UTC().Add(m.duration)
	}
	return nil
}

func (m *Manager) OutgoingReservations(holderID string) ([]OutgoingReservation, error) {
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]OutgoingReservation, 0, len(m.records))
	for _, record := range m.records {
		if record.requestID == "" || record.callID != "" {
			continue
		}
		result = append(result, outgoingReservationStatus(record, holderID, false))
	}
	return result, nil
}

func (m *Manager) ProjectActive(calls []store.Call, holderID string) (ActiveProjection, error) {
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return ActiveProjection{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	projection := ActiveProjection{
		Calls:        make([]ProjectedCall, 0, len(calls)),
		Reservations: make([]OutgoingReservation, 0, len(m.records)),
	}
	matched := make(map[*record]struct{}, len(calls))
	for _, call := range calls {
		record := m.findCallLocked(strings.TrimSpace(call.ID))
		if record == nil {
			record = m.matchPendingCallLocked(call, m.initialized)
		}
		if record != nil {
			matched[record] = struct{}{}
		}
		projection.Calls = append(projection.Calls, ProjectedCall{
			Call:         call,
			ControlState: m.controlStateLocked(call, record, holderID),
		})
	}
	for _, record := range m.records {
		if record.requestID == "" || record.callID != "" {
			continue
		}
		if _, ok := matched[record]; ok {
			continue
		}
		projection.Reservations = append(
			projection.Reservations,
			outgoingReservationStatus(record, holderID, false),
		)
	}
	return projection, nil
}

func (m *Manager) Renew(ctx context.Context, callID string, holderID string) (Status, error) {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return Status{}, err
	}
	if _, err := m.leaseableCall(ctx, callID); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.findCallLocked(callID)
	if record == nil || record.holderID != holderID {
		return Status{}, ErrNotOwner
	}
	if record.ending {
		return Status{}, ErrCallNotActive
	}
	record.orphanAt = m.now().UTC().Add(m.duration)
	return statusFor(record), nil
}

// MediaConnected records the server-side WebRTC session as the strongest
// positive liveness signal. It never creates or transfers ownership.
func (m *Manager) MediaConnected(callID string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.findCallLocked(callID)
	if record == nil || record.holderID == "" || record.ending {
		return
	}
	record.mediaAlive = true
}

// MediaDisconnected starts a fresh orphan window for the existing immutable
// holder. A later media connection or control heartbeat from that same holder
// may recover it until ending begins.
func (m *Manager) MediaDisconnected(callID string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.findCallLocked(callID)
	if record == nil || record.holderID == "" || record.ending {
		return
	}
	record.mediaAlive = false
	record.orphanAt = m.now().UTC().Add(m.duration)
}

func (m *Manager) Claim(ctx context.Context, callID string, holderID string) (Status, error) {
	return m.ClaimFor(ctx, callID, Owner{
		HolderID:  holderID,
		SubjectID: holderID,
	})
}

func (m *Manager) ClaimFor(
	ctx context.Context,
	callID string,
	owner Owner,
) (Status, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return Status{}, err
	}
	holderID := owner.HolderID
	callID, holderID, err = normalizeIDs(callID, holderID)
	if err != nil {
		return Status{}, err
	}
	call, err := m.leaseableCall(ctx, callID)
	if err != nil {
		return Status{}, err
	}

	now := m.now().UTC()
	lineID := strings.TrimSpace(call.LineID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		// Until one complete post-start snapshot establishes the baseline, a
		// database call may predate this process and have an owner we cannot
		// recover. Fail closed instead of letting the first request claim it.
		return Status{}, ErrCallOwned
	}
	rec := m.findCallLocked(callID)
	if rec == nil {
		rec = &record{
			callID:    callID,
			lineID:    lineID,
			createdAt: now,
		}
		m.records = append(m.records, rec)
	} else if rec.lineID == "" {
		rec.lineID = lineID
	}
	if rec.ending {
		return Status{}, ErrCallNotActive
	}
	if rec.holderID != "" {
		if rec.holderID != holderID || rec.subjectID != owner.SubjectID {
			return Status{}, ErrCallOwned
		}
		rec.orphanAt = now.Add(m.duration)
		return statusFor(rec), nil
	}
	if unclaimedControlState(call) != ControlAvailable {
		return Status{}, ErrCallNotActive
	}
	for _, existing := range m.records {
		if existing == rec {
			continue
		}
		if existing.lineID == lineID && recordBlocksLine(existing) {
			return Status{}, ErrCallOwned
		}
		if existing.holderID == holderID {
			return Status{}, ErrHolderBusy
		}
	}
	rec.holderID = holderID
	rec.subjectID = owner.SubjectID
	rec.orphanAt = now.Add(m.duration)
	return statusFor(rec), nil
}

// RevokeHolder immediately makes every bound call owned by one authenticated
// credential inoperable. A pending dial keeps the same bounded orphan deadline;
// if it later appears in the authoritative snapshot, the ordinary expiry path
// ends it.
func (m *Manager) RevokeHolder(holderID string) ([]string, error) {
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return nil, err
	}
	return m.revokeOwned(func(record *record) bool {
		return record.holderID == holderID
	}), nil
}

// RevokeSubject applies an explicit account-wide credential revocation. It
// ends calls held by every Web or paired-client credential for that user, but
// it never transfers those calls to another credential.
func (m *Manager) RevokeSubject(subjectID string) ([]string, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" || len(subjectID) > maxSubjectIDLength {
		return nil, ErrInvalidArgument
	}
	return m.revokeOwned(func(record *record) bool {
		return record.subjectID == subjectID
	}), nil
}

func (m *Manager) revokeOwned(matches func(*record) bool) []string {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	callIDs := make([]string, 0)
	for _, record := range m.records {
		if record.holderID == "" || record.ending || !matches(record) {
			continue
		}
		record.mediaAlive = false
		if record.callID == "" {
			if record.orphanAt.IsZero() {
				record.orphanAt = now.Add(m.duration)
			}
			continue
		}
		record.ending = true
		callIDs = append(callIDs, record.callID)
	}
	return callIDs
}

func (m *Manager) Require(ctx context.Context, callID string, holderID string) error {
	callID, holderID, err := normalizeIDs(callID, holderID)
	if err != nil {
		return err
	}
	if _, err := m.leaseableCall(ctx, callID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.findCallLocked(callID)
	if record == nil || record.holderID != holderID {
		return ErrNotOwner
	}
	if record.ending {
		return ErrCallNotActive
	}
	// Every authenticated owner command is also a positive liveness signal.
	record.orphanAt = m.now().UTC().Add(m.duration)
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
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.controlStateLocked(call, m.findCallLocked(callID), holderID), nil
}

func (m *Manager) ReconcileAuthoritativeCalls(ctx context.Context, calls []store.Call) error {
	now := m.now().UTC()
	active := make(map[string]struct{}, len(calls))
	type missingRecord struct {
		callID string
		record *record
	}
	missing := make([]missingRecord, 0)
	m.mu.Lock()
	startupSnapshot := !m.initialized
	m.initialized = true
	for _, call := range calls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" || !trackedPhase(call.Phase) {
			continue
		}
		active[callID] = struct{}{}
		rec := m.findCallLocked(callID)
		if rec == nil {
			rec = m.matchPendingCallLocked(call, !startupSnapshot)
			if rec != nil {
				rec.callID = callID
			}
		}
		if rec == nil {
			rec = &record{
				callID:    callID,
				lineID:    strings.TrimSpace(call.LineID),
				createdAt: now,
			}
			if startupSnapshot {
				// Ownership cannot be restored across an App restart. Keep every
				// pre-existing call occupied and end it after the orphan grace;
				// never expose it as a newly claimable incoming call.
				rec.holderID = restartOrphanHolderID
				rec.orphanAt = now.Add(m.duration)
			}
			m.records = append(m.records, rec)
		}
		if rec.lineID == "" {
			rec.lineID = strings.TrimSpace(call.LineID)
		}
		if rec.holderID == "" {
			if unclaimedControlState(call) == ControlAvailable {
				rec.orphanAt = time.Time{}
			} else if rec.orphanAt.IsZero() {
				rec.orphanAt = now.Add(m.duration)
			}
		} else if rec.orphanAt.IsZero() {
			rec.orphanAt = now.Add(m.duration)
		}
	}
	for index := 0; index < len(m.records); {
		record := m.records[index]
		if record.callID == "" && !record.ending && !record.orphanAt.IsZero() {
			// This snapshot was captured after the dial became indeterminate and
			// contains no call that can belong to it. The complete absence resolves
			// the reservation without inventing another state.
			m.removeRecordLocked(record)
			continue
		}
		index++
	}
	for _, record := range m.records {
		if record.callID == "" {
			continue
		}
		if _, found := active[record.callID]; !found {
			missing = append(missing, missingRecord{callID: record.callID, record: record})
		}
	}
	m.mu.Unlock()

	var result error
	for _, candidate := range missing {
		call, err := m.calls.CallByID(normalizeContext(ctx), candidate.callID)
		if err == nil && trackedPhase(call.Phase) {
			continue
		}
		if err != nil && !errors.Is(err, store.ErrCallNotFound) {
			result = errors.Join(result, fmt.Errorf(
				"verify call ownership removal for %s: %w",
				candidate.callID,
				err,
			))
			continue
		}
		m.mu.Lock()
		if m.findCallLocked(candidate.callID) == candidate.record {
			m.removeRecordLocked(candidate.record)
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
				m.endExpiredCall(ctx, expired.callID, expired.record)
			}
		}
	}
}

type expiredCall struct {
	callID string
	record *record
}

func (m *Manager) expiredCalls() []expiredCall {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	expired := make([]expiredCall, 0)
	for index := 0; index < len(m.records); {
		record := m.records[index]
		if record.callID == "" && record.requestID != "" &&
			!record.orphanAt.IsZero() && !now.Before(record.orphanAt) {
			m.removeRecordLocked(record)
			continue
		}
		index++
		if record.callID == "" || record.ending ||
			record.mediaAlive ||
			record.orphanAt.IsZero() ||
			now.Before(record.orphanAt) {
			continue
		}
		record.ending = true
		expired = append(expired, expiredCall{callID: record.callID, record: record})
	}
	return expired
}

func (m *Manager) endExpiredCall(ctx context.Context, callID string, expected *record) {
	m.mu.Lock()
	valid := m.findCallLocked(callID) == expected && expected != nil && expected.ending
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
		m.report(fmt.Errorf("end orphaned call %s: %w", callID, err))
	}
}

func (m *Manager) controlStateLocked(
	call store.Call,
	record *record,
	holderID string,
) ControlState {
	if !m.initialized {
		return ControlOccupied
	}
	if record != nil {
		if record.ending {
			return ControlOccupied
		}
		if record.holderID != "" {
			if record.holderID == holderID {
				return ControlOwned
			}
			return ControlOccupied
		}
	}
	lineID := strings.TrimSpace(call.LineID)
	for _, existing := range m.records {
		if existing == record {
			continue
		}
		if existing.lineID == lineID && recordBlocksLine(existing) {
			return ControlOccupied
		}
		if existing.holderID == holderID {
			return ControlOccupied
		}
	}
	return unclaimedControlState(call)
}

func (m *Manager) findRequestLocked(requestID string) *record {
	for _, record := range m.records {
		if record.requestID == requestID {
			return record
		}
	}
	return nil
}

func (m *Manager) findCallLocked(callID string) *record {
	for _, record := range m.records {
		if record.callID == callID {
			return record
		}
	}
	return nil
}

func (m *Manager) matchPendingCallLocked(call store.Call, allowLineMatch bool) *record {
	requestID := strings.TrimSpace(call.RequestID)
	lineID := strings.TrimSpace(call.LineID)
	if lineID == "" {
		return nil
	}
	if requestID != "" {
		record := m.findRequestLocked(requestID)
		if record != nil && record.callID == "" && record.lineID == lineID {
			return record
		}
	}
	if !allowLineMatch || !strings.EqualFold(strings.TrimSpace(call.Direction), "outgoing") {
		return nil
	}
	var matched *record
	for _, record := range m.records {
		if record.callID != "" || record.lineID != lineID {
			continue
		}
		if matched != nil {
			return nil
		}
		matched = record
	}
	return matched
}

func (m *Manager) removeRecordLocked(target *record) {
	for index, record := range m.records {
		if record != target {
			continue
		}
		copy(m.records[index:], m.records[index+1:])
		m.records[len(m.records)-1] = nil
		m.records = m.records[:len(m.records)-1]
		return
	}
}

func statusFor(record *record) Status {
	return Status{
		CallID:    record.callID,
		HolderID:  record.holderID,
		ExpiresAt: record.orphanAt,
	}
}

func outgoingReservationStatus(
	record *record,
	holderID string,
	created bool,
) OutgoingReservation {
	controlState := ControlOccupied
	if record.holderID == holderID && !record.ending {
		controlState = ControlOwned
	}
	return OutgoingReservation{
		ID:           record.requestID,
		LineID:       record.lineID,
		HolderID:     record.holderID,
		Created:      created,
		CreatedAt:    record.createdAt,
		ControlState: controlState,
	}
}

func unclaimedControlState(call store.Call) ControlState {
	if strings.EqualFold(strings.TrimSpace(call.Direction), "incoming") &&
		strings.EqualFold(strings.TrimSpace(call.Phase), "ringing") {
		return ControlAvailable
	}
	return ControlOccupied
}

// An unclaimed incoming ringing call is the only tracked record that does not
// reserve its line. Every owned, pending, orphaned, or ending record keeps the
// line closed even when it has no recoverable holder.
func recordBlocksLine(record *record) bool {
	return record != nil && (record.holderID != "" ||
		record.requestID != "" ||
		!record.orphanAt.IsZero() ||
		record.ending)
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
	requestID string,
	lineID string,
	holderID string,
) (string, string, string, error) {
	requestID, holderID, err := normalizeReservationAndHolder(requestID, holderID)
	if err != nil {
		return "", "", "", err
	}
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || len(lineID) > maxLineIDLength {
		return "", "", "", ErrInvalidArgument
	}
	return requestID, lineID, holderID, nil
}

func normalizeReservationAndHolder(requestID string, holderID string) (string, string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || len(requestID) > maxCallIDLength {
		return "", "", ErrInvalidArgument
	}
	holderID, err := NormalizeHolderID(holderID)
	if err != nil {
		return "", "", err
	}
	return requestID, holderID, nil
}

func NormalizeHolderID(holderID string) (string, error) {
	holderID = strings.TrimSpace(holderID)
	if holderID == "" || len(holderID) > maxHolderIDLength {
		return "", ErrInvalidArgument
	}
	return holderID, nil
}

func normalizeOwner(owner Owner) (Owner, error) {
	holderID, err := NormalizeHolderID(owner.HolderID)
	if err != nil {
		return Owner{}, err
	}
	subjectID := strings.TrimSpace(owner.SubjectID)
	if subjectID == "" || len(subjectID) > maxSubjectIDLength {
		return Owner{}, ErrInvalidArgument
	}
	return Owner{HolderID: holderID, SubjectID: subjectID}, nil
}

func (m *Manager) leaseableCall(ctx context.Context, callID string) (store.Call, error) {
	call, err := m.calls.CallByID(normalizeContext(ctx), callID)
	if errors.Is(err, store.ErrCallNotFound) {
		return store.Call{}, ErrCallNotFound
	}
	if err != nil {
		return store.Call{}, fmt.Errorf("read owned call: %w", err)
	}
	if !leaseablePhase(call.Phase) {
		return store.Call{}, ErrCallNotActive
	}
	return call, nil
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
	return strings.EqualFold(strings.TrimSpace(phase), "unknown") || leaseablePhase(phase)
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
