package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

const (
	RecordingPreferenceDefault  = "default"
	RecordingPreferenceOverride = "override"

	RecordingStateOff       = "off"
	RecordingStatePending   = "pending"
	RecordingStateRecording = "recording"
	RecordingStateReady     = "ready"
	RecordingStateFailed    = "failed"

	RecordingSegmentPending   = "pending"
	RecordingSegmentRecording = "recording"
	RecordingSegmentReady     = "ready"
	RecordingSegmentFailed    = "failed"

	maxRecordingRequestIDBytes = 128
	maxRecordingIdentifier     = 128
	maxRecordingPathBytes      = 512
	maxRecordingCleanupBatch   = 1024
	sqliteTimestampLayout      = "2006-01-02 15:04:05"
)

var (
	ErrRecordingNotFound         = errors.New("recording not found")
	ErrRecordingRevisionConflict = errors.New("recording settings revision conflict")
	ErrRecordingRequestConflict  = errors.New("recording request conflicts with an existing request")
	ErrRecordingValidation       = errors.New("recording data is invalid")
	ErrRecordingInProgress       = errors.New("recording is still in progress")
)

type RecordingSettings struct {
	DefaultEnabled bool   `json:"default_enabled"`
	Revision       int64  `json:"revision"`
	UpdatedAt      string `json:"updated_at"`
}

type CallRecordingState struct {
	CallID          string `json:"call_id"`
	Preference      string `json:"preference"`
	Enabled         bool   `json:"enabled"`
	Generation      int64  `json:"generation"`
	Status          string `json:"status"`
	ActiveSegmentID string `json:"active_segment_id,omitempty"`
	LastErrorCode   string `json:"last_error_code,omitempty"`
	UpdatedAt       string `json:"updated_at"`
}

type RecordingTarget struct {
	CallID         string
	Phase          string
	MediaAvailable bool
	State          CallRecordingState
}

type RecordingSegment struct {
	ID           string  `json:"id"`
	CallID       string  `json:"call_id"`
	SegmentIndex int64   `json:"segment_index"`
	Status       string  `json:"status"`
	StartedAt    *string `json:"started_at,omitempty"`
	EndedAt      *string `json:"ended_at,omitempty"`
	DurationMS   int64   `json:"duration_ms"`
	SizeBytes    int64   `json:"size_bytes"`
	RelativePath string  `json:"-"`
	FailureCode  string  `json:"failure_code,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type RecordingQuery struct {
	Search string
	Limit  int
}

type RecordingCall struct {
	ID              string `json:"id"`
	LineID          string `json:"line_id"`
	EndpointLineID  string `json:"endpoint_line_id"`
	LocalPhone      string `json:"local_phone"`
	LineIMSI        string `json:"line_imsi"`
	LineICCID       string `json:"line_iccid"`
	Direction       string `json:"direction"`
	RemoteNumber    string `json:"remote_number"`
	ContactID       string `json:"contact_id,omitempty"`
	ContactName     string `json:"contact_name,omitempty"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	DurationSeconds int64  `json:"duration_seconds"`
	Missed          bool   `json:"missed"`
	EndReason       string `json:"end_reason,omitempty"`
	FailureCode     string `json:"failure_code,omitempty"`
}

type RecordingEntry struct {
	Segment  RecordingSegment `json:"segment"`
	Call     RecordingCall    `json:"call"`
	Playable bool             `json:"playable"`
	Favorite bool             `json:"favorite"`
}

type RecordingIdentity struct {
	CallID string
	ID     string
}

func (s *Store) RecordingSettings(ctx context.Context) (RecordingSettings, error) {
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		return readUserRecordingSettings(ctx, s.database, principal.UserID)
	}
	return readRecordingSettings(ctx, s.database)
}

func readRecordingSettings(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
) (RecordingSettings, error) {
	var (
		settings       RecordingSettings
		defaultEnabled sql.NullInt64
		revision       sql.NullInt64
		updatedAt      sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT default_enabled, revision, updated_at
		 FROM modemdeck_recording_settings WHERE singleton = 1`,
	).Scan(&defaultEnabled, &revision, &updatedAt)
	if err != nil {
		return RecordingSettings{}, fmt.Errorf("query recording settings: %w", err)
	}
	settings.DefaultEnabled = boolValue(defaultEnabled)
	settings.Revision = intValue(revision)
	settings.UpdatedAt = stringValue(updatedAt)
	return settings, nil
}

func (s *Store) UpdateRecordingSettings(
	ctx context.Context,
	defaultEnabled bool,
	revision int64,
) (RecordingSettings, error) {
	if revision <= 0 {
		return RecordingSettings{}, ErrRecordingValidation
	}
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		return s.updateUserRecordingSettings(
			ctx,
			principal.UserID,
			defaultEnabled,
			revision,
		)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_recording_settings
		 SET default_enabled = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND revision = ?`,
		defaultEnabled,
		revision,
	)
	if err != nil {
		return RecordingSettings{}, fmt.Errorf("update recording settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return RecordingSettings{}, fmt.Errorf("read recording settings update result: %w", err)
	}
	if affected != 1 {
		return RecordingSettings{}, ErrRecordingRevisionConflict
	}
	return s.RecordingSettings(ctx)
}

func (s *Store) updateUserRecordingSettings(
	ctx context.Context,
	userID string,
	defaultEnabled bool,
	revision int64,
) (RecordingSettings, error) {
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_user_preferences
		 SET recording_default_enabled = ?,
			recording_revision = recording_revision + 1,
			updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ? AND recording_revision = ?`,
		defaultEnabled,
		userID,
		revision,
	)
	if err != nil {
		return RecordingSettings{}, fmt.Errorf("update user recording settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return RecordingSettings{}, fmt.Errorf("read user recording settings update result: %w", err)
	}
	if affected != 1 {
		return RecordingSettings{}, ErrRecordingRevisionConflict
	}
	return readUserRecordingSettings(ctx, s.database, userID)
}

func readUserRecordingSettings(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	userID string,
) (RecordingSettings, error) {
	var (
		settings       RecordingSettings
		defaultEnabled sql.NullInt64
		revision       sql.NullInt64
		updatedAt      sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT recording_default_enabled, recording_revision, updated_at
		 FROM modemdeck_user_preferences
		 WHERE user_id = ?`,
		userID,
	).Scan(&defaultEnabled, &revision, &updatedAt)
	if err != nil {
		return RecordingSettings{}, fmt.Errorf("query user recording settings: %w", err)
	}
	settings.DefaultEnabled = boolValue(defaultEnabled)
	settings.Revision = intValue(revision)
	settings.UpdatedAt = stringValue(updatedAt)
	return settings, nil
}

func (s *Store) PrepareCallRecordingRequest(ctx context.Context, requestID string, enabled bool) error {
	requestID = strings.TrimSpace(requestID)
	if !validRecordingRequestID(requestID) {
		return ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin call recording request: %w", err)
	}
	defer transaction.Rollback()

	var (
		boundPreference sql.NullString
		boundRequested  sql.NullInt64
	)
	err = transaction.QueryRowContext(
		ctx,
		`SELECT state.preference, state.request_enabled
		 FROM call_history call
		 LEFT JOIN modemdeck_call_recording_state state ON state.call_id = call.id
		 WHERE call.request_id = ? AND call.request_id <> ''`,
		requestID,
	).Scan(&boundPreference, &boundRequested)
	switch {
	case err == nil:
		if stringValue(boundPreference) != RecordingPreferenceOverride ||
			!boundRequested.Valid ||
			boolValue(boundRequested) != enabled {
			return ErrRecordingRequestConflict
		}
		if _, err := transaction.ExecContext(
			ctx,
			"DELETE FROM modemdeck_call_recording_requests WHERE request_id = ?",
			requestID,
		); err != nil {
			return fmt.Errorf("consume bound call recording request: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit bound call recording request: %w", err)
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read bound call recording request: %w", err)
	}

	result, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_call_recording_requests (request_id, enabled, created_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(request_id) DO NOTHING`,
		requestID,
		enabled,
	)
	if err != nil {
		return fmt.Errorf("prepare call recording request: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read call recording request result: %w", err)
	}
	if affected == 1 {
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit call recording request: %w", err)
		}
		return nil
	}
	var existing sql.NullInt64
	if err := transaction.QueryRowContext(
		ctx,
		"SELECT enabled FROM modemdeck_call_recording_requests WHERE request_id = ?",
		requestID,
	).Scan(&existing); err != nil {
		return fmt.Errorf("read call recording request: %w", err)
	}
	if boolValue(existing) != enabled {
		return ErrRecordingRequestConflict
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit replayed call recording request: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredCallRecordingRequests(
	ctx context.Context,
	createdBefore time.Time,
	limit int,
) (int64, error) {
	if createdBefore.IsZero() || limit <= 0 || limit > maxRecordingCleanupBatch {
		return 0, ErrRecordingValidation
	}
	result, err := s.database.ExecContext(
		ctx,
		`DELETE FROM modemdeck_call_recording_requests
		 WHERE request_id IN (
			SELECT request.request_id
			FROM modemdeck_call_recording_requests request
			WHERE request.created_at < ?
			AND NOT EXISTS (
				SELECT 1 FROM call_history call
				WHERE call.request_id = request.request_id
				AND call.request_id <> ''
			)
			ORDER BY request.created_at ASC, request.request_id ASC
			LIMIT ?
		 )`,
		createdBefore.UTC().Format(sqliteTimestampLayout),
		limit,
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired call recording requests: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read expired call recording cleanup result: %w", err)
	}
	return deleted, nil
}

func (s *Store) EnsureCallRecordingState(ctx context.Context, callID string) (CallRecordingState, error) {
	callID = strings.TrimSpace(callID)
	if !validRecordingIdentifier(callID) {
		return CallRecordingState{}, ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return CallRecordingState{}, fmt.Errorf("begin call recording state: %w", err)
	}
	defer transaction.Rollback()
	if err := ensureCallRecordingState(ctx, transaction, callID); err != nil {
		return CallRecordingState{}, err
	}
	state, err := callRecordingState(ctx, transaction, callID)
	if err != nil {
		return CallRecordingState{}, err
	}
	if err := transaction.Commit(); err != nil {
		return CallRecordingState{}, fmt.Errorf("commit call recording state: %w", err)
	}
	return state, nil
}

func (s *Store) CallRecordingState(ctx context.Context, callID string) (CallRecordingState, error) {
	callID = strings.TrimSpace(callID)
	if !validRecordingIdentifier(callID) {
		return CallRecordingState{}, ErrRecordingValidation
	}
	state, err := callRecordingState(ctx, s.database, callID)
	if errors.Is(err, sql.ErrNoRows) {
		return CallRecordingState{}, ErrRecordingNotFound
	}
	return state, err
}

func (s *Store) SetCallRecordingEnabled(
	ctx context.Context,
	callID string,
	enabled bool,
) (CallRecordingState, error) {
	callID = strings.TrimSpace(callID)
	if !validRecordingIdentifier(callID) {
		return CallRecordingState{}, ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return CallRecordingState{}, fmt.Errorf("begin call recording update: %w", err)
	}
	defer transaction.Rollback()
	if err := ensureCallRecordingState(ctx, transaction, callID); err != nil {
		return CallRecordingState{}, err
	}
	current, err := callRecordingState(ctx, transaction, callID)
	if err != nil {
		return CallRecordingState{}, err
	}
	if current.Enabled != enabled {
		status := current.Status
		activeSegmentID := current.ActiveSegmentID
		lastErrorCode := current.LastErrorCode
		if enabled {
			status = RecordingStatePending
			activeSegmentID = ""
			lastErrorCode = ""
		} else if activeSegmentID == "" {
			status = RecordingStateOff
			lastErrorCode = ""
		}
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_call_recording_state
			 SET preference = ?, enabled = ?, generation = generation + 1,
				status = ?, active_segment_id = ?, last_error_code = ?,
				updated_at = CURRENT_TIMESTAMP
			 WHERE call_id = ?`,
			RecordingPreferenceOverride,
			enabled,
			status,
			activeSegmentID,
			lastErrorCode,
			callID,
		); err != nil {
			return CallRecordingState{}, fmt.Errorf("update call recording state: %w", err)
		}
	}
	state, err := callRecordingState(ctx, transaction, callID)
	if err != nil {
		return CallRecordingState{}, err
	}
	if err := transaction.Commit(); err != nil {
		return CallRecordingState{}, fmt.Errorf("commit call recording update: %w", err)
	}
	return state, nil
}

func (s *Store) ActiveRecordingTargets(ctx context.Context) ([]RecordingTarget, error) {
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT ch.id, ch.phase, ch.media_available,
			state.call_id, state.preference, state.enabled, state.generation,
			state.status, state.active_segment_id, state.last_error_code, state.updated_at
		 FROM call_history ch
		 JOIN modemdeck_call_recording_state state ON state.call_id = ch.id
		 WHERE ch.phase NOT IN ('ended', 'failed') AND COALESCE(ch.ended_at, '') = ''
		 ORDER BY ch.created_at ASC, ch.id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active recording targets: %w", err)
	}
	defer rows.Close()
	targets := make([]RecordingTarget, 0)
	for rows.Next() {
		var (
			target                                    RecordingTarget
			mediaAvailable, enabled, generation       sql.NullInt64
			callID, preference, status, activeSegment sql.NullString
			lastErrorCode, updatedAt                  sql.NullString
		)
		if err := rows.Scan(
			&target.CallID,
			&target.Phase,
			&mediaAvailable,
			&callID,
			&preference,
			&enabled,
			&generation,
			&status,
			&activeSegment,
			&lastErrorCode,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active recording target: %w", err)
		}
		target.MediaAvailable = boolValue(mediaAvailable)
		target.State = CallRecordingState{
			CallID:          stringValue(callID),
			Preference:      stringValue(preference),
			Enabled:         boolValue(enabled),
			Generation:      intValue(generation),
			Status:          stringValue(status),
			ActiveSegmentID: stringValue(activeSegment),
			LastErrorCode:   stringValue(lastErrorCode),
			UpdatedAt:       stringValue(updatedAt),
		}
		targets = append(targets, target)
	}
	return targets, rowsError("read active recording targets", rows.Err())
}

func (s *Store) CreateRecordingSegment(
	ctx context.Context,
	segment RecordingSegment,
) (RecordingSegment, error) {
	segment.ID = strings.TrimSpace(segment.ID)
	segment.CallID = strings.TrimSpace(segment.CallID)
	segment.RelativePath = strings.TrimSpace(segment.RelativePath)
	if !validRecordingIdentifier(segment.ID) ||
		!validRecordingIdentifier(segment.CallID) ||
		!validRelativeRecordingPath(segment.RelativePath) {
		return RecordingSegment{}, ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return RecordingSegment{}, fmt.Errorf("begin recording segment: %w", err)
	}
	defer transaction.Rollback()
	var segmentIndex int64
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(segment_index), 0) + 1
		 FROM modemdeck_call_recordings WHERE call_id = ?`,
		segment.CallID,
	).Scan(&segmentIndex); err != nil {
		return RecordingSegment{}, fmt.Errorf("allocate recording segment index: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_call_recordings (
			id, call_id, segment_index, status, relative_path, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		segment.ID,
		segment.CallID,
		segmentIndex,
		RecordingSegmentPending,
		segment.RelativePath,
	); err != nil {
		return RecordingSegment{}, fmt.Errorf("insert recording segment: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = ?, active_segment_id = ?, last_error_code = '',
			updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ?`,
		RecordingStatePending,
		segment.ID,
		segment.CallID,
	); err != nil {
		return RecordingSegment{}, fmt.Errorf("mark recording segment pending: %w", err)
	}
	stored, err := recordingSegment(ctx, transaction, segment.CallID, segment.ID)
	if err != nil {
		return RecordingSegment{}, err
	}
	if err := transaction.Commit(); err != nil {
		return RecordingSegment{}, fmt.Errorf("commit recording segment: %w", err)
	}
	return stored, nil
}

func (s *Store) MarkRecordingSegmentStarted(
	ctx context.Context,
	callID, segmentID string,
	startedAt time.Time,
) error {
	if !validRecordingIdentifier(callID) || !validRecordingIdentifier(segmentID) || startedAt.IsZero() {
		return ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recording segment start: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recordings
		 SET status = ?, started_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND id = ? AND status = ?`,
		RecordingSegmentRecording,
		databaseTime(startedAt),
		callID,
		segmentID,
		RecordingSegmentPending,
	)
	if err != nil {
		return fmt.Errorf("start recording segment: %w", err)
	}
	if err := requireOneRecordingRow(result); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = ?, active_segment_id = ?, last_error_code = '',
			updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ?`,
		RecordingStateRecording,
		segmentID,
		callID,
	); err != nil {
		return fmt.Errorf("mark call recording active: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit recording segment start: %w", err)
	}
	return nil
}

func (s *Store) CompleteRecordingSegment(
	ctx context.Context,
	callID, segmentID string,
	endedAt time.Time,
	durationMS, sizeBytes int64,
) error {
	if !validRecordingIdentifier(callID) || !validRecordingIdentifier(segmentID) ||
		endedAt.IsZero() || durationMS < 0 || sizeBytes < 0 {
		return ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recording segment completion: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recordings
		 SET status = ?, ended_at = ?, duration_ms = ?, size_bytes = ?,
			failure_code = '', updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND id = ? AND status IN (?, ?)`,
		RecordingSegmentReady,
		databaseTime(endedAt),
		durationMS,
		sizeBytes,
		callID,
		segmentID,
		RecordingSegmentPending,
		RecordingSegmentRecording,
	)
	if err != nil {
		return fmt.Errorf("complete recording segment: %w", err)
	}
	if err := requireOneRecordingRow(result); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = CASE WHEN enabled = 0 THEN ? ELSE ? END,
			active_segment_id = '', last_error_code = '',
			updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND active_segment_id = ?`,
		RecordingStateOff,
		RecordingStateReady,
		callID,
		segmentID,
	); err != nil {
		return fmt.Errorf("complete call recording state: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit recording segment completion: %w", err)
	}
	return nil
}

func (s *Store) FailRecordingSegment(
	ctx context.Context,
	callID, segmentID, failureCode string,
	endedAt time.Time,
) error {
	failureCode = strings.TrimSpace(failureCode)
	if !validRecordingIdentifier(callID) || !validRecordingIdentifier(segmentID) ||
		!validRecordingFailureCode(failureCode) || endedAt.IsZero() {
		return ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recording segment failure: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recordings
		 SET status = ?, ended_at = ?, failure_code = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND id = ? AND status IN (?, ?)`,
		RecordingSegmentFailed,
		databaseTime(endedAt),
		failureCode,
		callID,
		segmentID,
		RecordingSegmentPending,
		RecordingSegmentRecording,
	)
	if err != nil {
		return fmt.Errorf("fail recording segment: %w", err)
	}
	if err := requireOneRecordingRow(result); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = ?, active_segment_id = '', last_error_code = ?,
			updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND active_segment_id = ?`,
		RecordingStateFailed,
		failureCode,
		callID,
		segmentID,
	); err != nil {
		return fmt.Errorf("fail call recording state: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit recording segment failure: %w", err)
	}
	return nil
}

func (s *Store) RecordingSegments(ctx context.Context, callID string) ([]RecordingSegment, error) {
	if !validRecordingIdentifier(callID) {
		return nil, ErrRecordingValidation
	}
	rows, err := s.database.QueryContext(
		ctx,
		recordingSegmentColumns+`
		 FROM modemdeck_call_recordings
		 WHERE call_id = ?
		 ORDER BY segment_index ASC`,
		callID,
	)
	if err != nil {
		return nil, fmt.Errorf("query recording segments: %w", err)
	}
	defer rows.Close()
	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rowsError("read recording segments", rows.Err())
}

func (s *Store) RecordingEntries(
	ctx context.Context,
	query RecordingQuery,
) ([]RecordingEntry, error) {
	limit := boundedLimit(query.Limit)
	contactOwner := contactOwnerSQL(ctx, "contacts")
	favoriteExpression := "COALESCE(state.is_favorite, 0)"
	userStateJoin := ""
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		userID := strings.ReplaceAll(principal.UserID, "'", "''")
		favoriteExpression = "COALESCE(user_state.is_favorite, 0)"
		userStateJoin = ` LEFT JOIN modemdeck_user_recording_state AS user_state
			ON user_state.user_id = '` + userID + `'
			AND user_state.call_id = call.id`
	}
	statement := fmt.Sprintf(`SELECT
		recording.id, recording.call_id, recording.segment_index, recording.status,
		recording.started_at, recording.ended_at, recording.duration_ms,
		recording.size_bytes, recording.relative_path, recording.failure_code,
		recording.created_at, recording.updated_at,
		%s,
		call.line_id, call.endpoint_line_id, call.local_phone, call.line_imsi, call.line_iccid,
		call.direction, call.remote_number,
		%s, %s,
		call.created_at, call.active_at, call.ended_at,
		call.end_reason, call.failure_code
		FROM modemdeck_call_recordings recording
		JOIN call_history call ON call.id = recording.call_id
		LEFT JOIN modemdeck_call_recording_state state ON state.call_id = call.id%s`,
		favoriteExpression,
		fmt.Sprintf(contactIDForNumberSQL, "call.remote_number", contactOwner),
		fmt.Sprintf(contactNameForNumberSQL, "call.remote_number", contactOwner),
		userStateJoin,
	)
	arguments := make([]any, 0, 8)
	conditions := make([]string, 0, 2)
	if condition, values, scoped := principalLineScope(ctx, "call.line_id"); scoped {
		conditions = append(conditions, condition)
		arguments = append(arguments, values...)
	}
	if strings.TrimSpace(query.Search) != "" {
		pattern := searchPattern(query.Search)
		searchCondition := `(
			LOWER(COALESCE(call.remote_number, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(call.local_phone, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(call.line_id, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(call.endpoint_line_id, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(recording.id, '')) LIKE ? ESCAPE '\' OR
				EXISTS (
					SELECT 1
					FROM contact_phones
					JOIN contacts ON contacts.id = contact_phones.contact_id
					WHERE contact_phones.canonical_e164 = call.remote_number
					` + contactOwnerSQL(ctx, "contacts") + `
					AND LOWER(COALESCE(contacts.display_name, '')) LIKE ? ESCAPE '\'
				) OR
			EXISTS (
				SELECT 1
				FROM devices
				WHERE devices.endpoint_id = call.endpoint_line_id
					AND LOWER(COALESCE(devices.name, '')) LIKE ? ESCAPE '\'
			)
		)`
		conditions = append(conditions, searchCondition)
		arguments = append(
			arguments,
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
		)
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += ` ORDER BY
		COALESCE(recording.started_at, recording.created_at) DESC,
		recording.call_id DESC,
		recording.segment_index DESC,
		recording.id DESC
		LIMIT ?`
	arguments = append(arguments, limit)

	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query recording entries: %w", err)
	}
	defer rows.Close()

	entries := make([]RecordingEntry, 0)
	for rows.Next() {
		entry, err := scanRecordingEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rowsError("read recording entries", rows.Err())
}

func (s *Store) SetRecordingFavorites(
	ctx context.Context,
	recordings []RecordingIdentity,
	favorite bool,
) error {
	normalized := make([]RecordingIdentity, 0, len(recordings))
	seen := make(map[string]struct{}, len(recordings))
	for _, recording := range recordings {
		recording.CallID = strings.TrimSpace(recording.CallID)
		recording.ID = strings.TrimSpace(recording.ID)
		if !validRecordingIdentifier(recording.CallID) ||
			!validRecordingIdentifier(recording.ID) {
			return ErrRecordingValidation
		}
		key := recording.CallID + "\x00" + recording.ID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, recording)
	}
	if len(normalized) == 0 || len(normalized) > 100 {
		return ErrRecordingValidation
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recording favorite update: %w", err)
	}
	defer transaction.Rollback()

	callIDs := make([]string, 0, len(normalized))
	seenCalls := make(map[string]struct{}, len(normalized))
	for _, recording := range normalized {
		var lineID string
		if err := transaction.QueryRowContext(
			ctx,
			`SELECT call.line_id
			 FROM modemdeck_call_recordings AS recording
			 JOIN call_history AS call ON call.id = recording.call_id
			 WHERE recording.call_id = ? AND recording.id = ?`,
			recording.CallID,
			recording.ID,
		).Scan(&lineID); errors.Is(err, sql.ErrNoRows) {
			return ErrRecordingNotFound
		} else if err != nil {
			return fmt.Errorf("inspect recording favorite target: %w", err)
		}
		if !principalCanAccessLine(ctx, lineID) {
			return ErrRecordingNotFound
		}
		if _, exists := seenCalls[recording.CallID]; exists {
			continue
		}
		seenCalls[recording.CallID] = struct{}{}
		callIDs = append(callIDs, recording.CallID)
	}

	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		for _, callID := range callIDs {
			if _, err := transaction.ExecContext(ctx, `
				INSERT INTO modemdeck_user_recording_state (
					user_id, call_id, is_favorite, updated_at
				) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(user_id, call_id) DO UPDATE SET
					is_favorite = excluded.is_favorite,
					updated_at = CURRENT_TIMESTAMP
			`, principal.UserID, callID, favorite); err != nil {
				return fmt.Errorf("update user recording favorite: %w", err)
			}
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit user recording favorite update: %w", err)
		}
		return nil
	}

	arguments := make([]any, 0, len(callIDs)+1)
	arguments = append(arguments, favorite)
	for _, callID := range callIDs {
		arguments = append(arguments, callID)
	}
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET is_favorite = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE call_id IN (`+placeholders(len(callIDs))+`)`,
		arguments...,
	)
	if err != nil {
		return fmt.Errorf("update recording favorite state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read recording favorite update result: %w", err)
	}
	if affected != int64(len(callIDs)) {
		return ErrRecordingNotFound
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit recording favorite update: %w", err)
	}
	return nil
}

func (s *Store) RecordingSegment(
	ctx context.Context,
	callID, segmentID string,
) (RecordingSegment, error) {
	if !validRecordingIdentifier(callID) || !validRecordingIdentifier(segmentID) {
		return RecordingSegment{}, ErrRecordingValidation
	}
	segment, err := recordingSegment(ctx, s.database, callID, segmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return RecordingSegment{}, ErrRecordingNotFound
	}
	return segment, err
}

func (s *Store) DeleteRecordingSegment(
	ctx context.Context,
	callID, segmentID string,
) error {
	if !validRecordingIdentifier(callID) || !validRecordingIdentifier(segmentID) {
		return ErrRecordingValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recording segment deletion: %w", err)
	}
	defer transaction.Rollback()

	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM modemdeck_call_recordings
		 WHERE call_id = ? AND id = ? AND status NOT IN (?, ?)`,
		callID,
		segmentID,
		RecordingSegmentPending,
		RecordingSegmentRecording,
	)
	if err != nil {
		return fmt.Errorf("delete recording segment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted recording segment count: %w", err)
	}
	if affected == 1 {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_call_recording_state
			 SET is_favorite = 0, updated_at = CURRENT_TIMESTAMP
			 WHERE call_id = ?
			 AND NOT EXISTS (
				SELECT 1 FROM modemdeck_call_recordings
				WHERE call_id = ?
			 )`,
			callID,
			callID,
		); err != nil {
			return fmt.Errorf("clear empty recording favorite state: %w", err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM modemdeck_user_recording_state
			 WHERE call_id = ?
			 AND NOT EXISTS (
				SELECT 1 FROM modemdeck_call_recordings
				WHERE call_id = ?
			 )`,
			callID,
			callID,
		); err != nil {
			return fmt.Errorf("clear empty user recording state: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit recording segment deletion: %w", err)
		}
		return nil
	}
	var status string
	err = transaction.QueryRowContext(
		ctx,
		`SELECT status FROM modemdeck_call_recordings
		 WHERE call_id = ? AND id = ?`,
		callID,
		segmentID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRecordingNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect recording segment deletion conflict: %w", err)
	}
	return ErrRecordingInProgress
}

func (s *Store) InterruptedRecordingSegments(
	ctx context.Context,
) ([]RecordingSegment, error) {
	rows, err := s.database.QueryContext(
		ctx,
		recordingSegmentColumns+`
		 FROM modemdeck_call_recordings
		 WHERE status IN (?, ?)
		 ORDER BY created_at ASC, call_id ASC, segment_index ASC`,
		RecordingSegmentPending,
		RecordingSegmentRecording,
	)
	if err != nil {
		return nil, fmt.Errorf("query interrupted recording segments: %w", err)
	}
	defer rows.Close()
	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rowsError("read interrupted recording segments", rows.Err())
}

func (s *Store) FailInterruptedRecordingSegments(ctx context.Context) error {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin interrupted recording cleanup: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recordings
		 SET status = ?, ended_at = COALESCE(ended_at, CURRENT_TIMESTAMP),
			failure_code = 'interrupted', updated_at = CURRENT_TIMESTAMP
		 WHERE status IN (?, ?)`,
		RecordingSegmentFailed,
		RecordingSegmentPending,
		RecordingSegmentRecording,
	); err != nil {
		return fmt.Errorf("fail interrupted recording segments: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = ?, active_segment_id = '', last_error_code = 'interrupted',
			updated_at = CURRENT_TIMESTAMP
		 WHERE active_segment_id <> ''`,
		RecordingStateFailed,
	); err != nil {
		return fmt.Errorf("fail interrupted call recording state: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = ?, last_error_code = 'call_ended_before_recording',
			updated_at = CURRENT_TIMESTAMP
		 WHERE status = ? AND active_segment_id = ''
		 AND EXISTS (
			SELECT 1 FROM call_history call
			WHERE call.id = modemdeck_call_recording_state.call_id
			AND (call.phase IN ('ended', 'failed') OR COALESCE(call.ended_at, '') <> '')
		 )`,
		RecordingStateFailed,
		RecordingStatePending,
	); err != nil {
		return fmt.Errorf("finalize interrupted pending call recording states: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit interrupted recording cleanup: %w", err)
	}
	return nil
}

func (s *Store) FinalizePendingCallRecording(
	ctx context.Context,
	callID, failureCode string,
) error {
	callID = strings.TrimSpace(callID)
	failureCode = strings.TrimSpace(failureCode)
	if !validRecordingIdentifier(callID) || !validRecordingFailureCode(failureCode) {
		return ErrRecordingValidation
	}
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_call_recording_state
		 SET status = ?, active_segment_id = '', last_error_code = ?,
			updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND status = ? AND active_segment_id = ''`,
		RecordingStateFailed,
		failureCode,
		callID,
		RecordingStatePending,
	); err != nil {
		return fmt.Errorf("finalize pending call recording: %w", err)
	}
	return nil
}

func ensureCallRecordingState(ctx context.Context, transaction *sql.Tx, callID string) error {
	var requestID sql.NullString
	err := transaction.QueryRowContext(
		ctx,
		"SELECT request_id FROM call_history WHERE id = ?",
		callID,
	).Scan(&requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCallNotFound
	}
	if err != nil {
		return fmt.Errorf("query call for recording state: %w", err)
	}
	preference := RecordingPreferenceDefault
	var requestEnabled sql.NullBool
	enabled := false
	if preparedID := strings.TrimSpace(stringValue(requestID)); preparedID != "" {
		var requested sql.NullInt64
		err := transaction.QueryRowContext(
			ctx,
			`SELECT enabled FROM modemdeck_call_recording_requests WHERE request_id = ?`,
			preparedID,
		).Scan(&requested)
		switch {
		case err == nil:
			preference = RecordingPreferenceOverride
			enabled = boolValue(requested)
			requestEnabled = sql.NullBool{Bool: enabled, Valid: true}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("query prepared call recording request: %w", err)
		}
	}
	generation := 0
	status := RecordingStateOff
	if enabled {
		generation = 1
		status = RecordingStatePending
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_call_recording_state (
			call_id, preference, request_enabled, enabled, generation, status, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(call_id) DO NOTHING`,
		callID,
		preference,
		requestEnabled,
		enabled,
		generation,
		status,
	); err != nil {
		return fmt.Errorf("insert call recording state: %w", err)
	}
	if preparedID := strings.TrimSpace(stringValue(requestID)); preparedID != "" {
		if _, err := transaction.ExecContext(
			ctx,
			"DELETE FROM modemdeck_call_recording_requests WHERE request_id = ?",
			preparedID,
		); err != nil {
			return fmt.Errorf("consume call recording request: %w", err)
		}
	}
	return nil
}

type recordingStateQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func callRecordingState(
	ctx context.Context,
	queryer recordingStateQueryer,
	callID string,
) (CallRecordingState, error) {
	var (
		state                                        CallRecordingState
		preference, status, activeSegment, lastError sql.NullString
		enabled, generation                          sql.NullInt64
		updatedAt                                    sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT call_id, preference, enabled, generation, status,
			active_segment_id, last_error_code, updated_at
		 FROM modemdeck_call_recording_state WHERE call_id = ?`,
		callID,
	).Scan(
		&state.CallID,
		&preference,
		&enabled,
		&generation,
		&status,
		&activeSegment,
		&lastError,
		&updatedAt,
	)
	if err != nil {
		return CallRecordingState{}, err
	}
	state.Preference = stringValue(preference)
	state.Enabled = boolValue(enabled)
	state.Generation = intValue(generation)
	state.Status = stringValue(status)
	state.ActiveSegmentID = stringValue(activeSegment)
	state.LastErrorCode = stringValue(lastError)
	state.UpdatedAt = stringValue(updatedAt)
	return state, nil
}

const recordingSegmentColumns = `SELECT id, call_id, segment_index, status,
	started_at, ended_at, duration_ms, size_bytes, relative_path, failure_code,
	created_at, updated_at`

type recordingSegmentQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func recordingSegment(
	ctx context.Context,
	queryer recordingSegmentQueryer,
	callID, segmentID string,
) (RecordingSegment, error) {
	segment, err := scanRecordingSegment(queryer.QueryRowContext(
		ctx,
		recordingSegmentColumns+`
		 FROM modemdeck_call_recordings WHERE call_id = ? AND id = ?`,
		callID,
		segmentID,
	))
	if err != nil {
		return RecordingSegment{}, err
	}
	return segment, nil
}

type recordingSegmentScanner interface {
	Scan(...any) error
}

func scanRecordingSegment(scanner recordingSegmentScanner) (RecordingSegment, error) {
	var (
		segment                             RecordingSegment
		startedAt, endedAt, relativePath    sql.NullString
		failureCode, createdAt, updatedAt   sql.NullString
		segmentIndex, durationMS, sizeBytes sql.NullInt64
		status                              sql.NullString
	)
	if err := scanner.Scan(
		&segment.ID,
		&segment.CallID,
		&segmentIndex,
		&status,
		&startedAt,
		&endedAt,
		&durationMS,
		&sizeBytes,
		&relativePath,
		&failureCode,
		&createdAt,
		&updatedAt,
	); err != nil {
		return RecordingSegment{}, err
	}
	segment.SegmentIndex = intValue(segmentIndex)
	segment.Status = stringValue(status)
	segment.StartedAt = pointerValue(startedAt)
	segment.EndedAt = pointerValue(endedAt)
	segment.DurationMS = intValue(durationMS)
	segment.SizeBytes = intValue(sizeBytes)
	segment.RelativePath = stringValue(relativePath)
	segment.FailureCode = stringValue(failureCode)
	segment.CreatedAt = stringValue(createdAt)
	segment.UpdatedAt = stringValue(updatedAt)
	return segment, nil
}

func scanRecordingEntry(scanner recordingSegmentScanner) (RecordingEntry, error) {
	var (
		entry                                         RecordingEntry
		startedAt, endedAt, relativePath              sql.NullString
		segmentFailure, segmentCreated, segmentUpdate sql.NullString
		segmentIndex, durationMS, sizeBytes           sql.NullInt64
		segmentStatus                                 sql.NullString
		favorite                                      sql.NullInt64
		lineID, endpointLineID, localPhone            sql.NullString
		lineIMSI, lineICCID                           sql.NullString
		direction, remoteNumber                       sql.NullString
		contactID, contactName, callCreated           sql.NullString
		activeAt, callEnded, endReason, callFailure   sql.NullString
	)
	if err := scanner.Scan(
		&entry.Segment.ID,
		&entry.Segment.CallID,
		&segmentIndex,
		&segmentStatus,
		&startedAt,
		&endedAt,
		&durationMS,
		&sizeBytes,
		&relativePath,
		&segmentFailure,
		&segmentCreated,
		&segmentUpdate,
		&favorite,
		&lineID,
		&endpointLineID,
		&localPhone,
		&lineIMSI,
		&lineICCID,
		&direction,
		&remoteNumber,
		&contactID,
		&contactName,
		&callCreated,
		&activeAt,
		&callEnded,
		&endReason,
		&callFailure,
	); err != nil {
		return RecordingEntry{}, fmt.Errorf("scan recording entry: %w", err)
	}
	entry.Segment.SegmentIndex = intValue(segmentIndex)
	entry.Segment.Status = stringValue(segmentStatus)
	entry.Segment.StartedAt = pointerValue(startedAt)
	entry.Segment.EndedAt = pointerValue(endedAt)
	entry.Segment.DurationMS = intValue(durationMS)
	entry.Segment.SizeBytes = intValue(sizeBytes)
	entry.Segment.RelativePath = stringValue(relativePath)
	entry.Segment.FailureCode = stringValue(segmentFailure)
	entry.Segment.CreatedAt = stringValue(segmentCreated)
	entry.Segment.UpdatedAt = stringValue(segmentUpdate)
	entry.Playable = entry.Segment.Status == RecordingSegmentReady &&
		entry.Segment.RelativePath != ""
	entry.Favorite = boolValue(favorite)

	entry.Call = RecordingCall{
		ID:             entry.Segment.CallID,
		LineID:         stringValue(lineID),
		EndpointLineID: stringValue(endpointLineID),
		LocalPhone:     stringValue(localPhone),
		LineIMSI:       stringValue(lineIMSI),
		LineICCID:      stringValue(lineICCID),
		Direction:      stringValue(direction),
		RemoteNumber:   stringValue(remoteNumber),
		ContactID:      stringValue(contactID),
		ContactName:    stringValue(contactName),
		StartedAt:      stringValue(callCreated),
		EndedAt:        stringValue(callEnded),
		EndReason:      stringValue(endReason),
		FailureCode:    stringValue(callFailure),
	}
	if activeAt.Valid {
		entry.Call.DurationSeconds = durationSeconds(activeAt.String, entry.Call.EndedAt)
	}
	entry.Call.Missed = entry.Call.Direction == string(CallKindIncoming) &&
		!activeAt.Valid &&
		entry.Call.EndReason != "rejected" &&
		entry.Call.FailureCode != "rejected"
	return entry, nil
}

func requireOneRecordingRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read recording update result: %w", err)
	}
	if affected != 1 {
		return ErrRecordingNotFound
	}
	return nil
}

func validRecordingRequestID(value string) bool {
	if value == "" || len(value) > maxRecordingRequestIDBytes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validRecordingIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxRecordingIdentifier {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validRelativeRecordingPath(value string) bool {
	if value == "" || len(value) > maxRecordingPathBytes ||
		strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	return validRecordingIdentifier(parts[0]) &&
		strings.HasSuffix(parts[1], ".ogg") &&
		validRecordingIdentifier(strings.TrimSuffix(parts[1], ".ogg"))
}

func validRecordingFailureCode(value string) bool {
	return validRecordingIdentifier(value)
}
