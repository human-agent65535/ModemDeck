package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRevisionConflict        = errors.New("revision conflict")
	ErrInvalidCallPolicy       = errors.New("invalid call policy")
	ErrIncomingCallActionState = errors.New("incoming call action is not sending")
)

func (s *Store) GlobalCallSettings(ctx context.Context) (GlobalCallSettings, error) {
	return globalCallSettings(ctx, s.database)
}

func (s *Store) UpdateGlobalCallSettings(
	ctx context.Context,
	receiveCalls bool,
	expectedRevision int64,
) (GlobalCallSettings, error) {
	if expectedRevision <= 0 {
		return GlobalCallSettings{}, ErrRevisionConflict
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return GlobalCallSettings{}, fmt.Errorf("begin global call settings update: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_call_settings
		 SET receive_calls = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND revision = ?`,
		receiveCalls,
		expectedRevision,
	)
	if err != nil {
		return GlobalCallSettings{}, fmt.Errorf("update global call settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return GlobalCallSettings{}, fmt.Errorf("count updated global call settings: %w", err)
	}
	if affected != 1 {
		return GlobalCallSettings{}, ErrRevisionConflict
	}
	settings, err := globalCallSettings(ctx, transaction)
	if err != nil {
		return GlobalCallSettings{}, err
	}
	if err := transaction.Commit(); err != nil {
		return GlobalCallSettings{}, fmt.Errorf("commit global call settings update: %w", err)
	}
	return settings, nil
}

func (s *Store) LineCallPolicy(ctx context.Context, lineID string) (LineCallPolicy, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return LineCallPolicy{}, ErrInvalidCallPolicy
	}
	if _, err := s.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_line_call_policies (line_id, policy, revision, updated_at)
		 VALUES (?, 'follow_global', 1, CURRENT_TIMESTAMP)
		 ON CONFLICT(line_id) DO NOTHING`,
		lineID,
	); err != nil {
		return LineCallPolicy{}, fmt.Errorf("ensure line call policy: %w", err)
	}
	return lineCallPolicy(ctx, s.database, lineID)
}

func (s *Store) UpdateLineCallPolicy(
	ctx context.Context,
	lineID string,
	policy LineCallPolicyValue,
	expectedRevision int64,
) (LineCallPolicy, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || !validLineCallPolicy(policy) {
		return LineCallPolicy{}, ErrInvalidCallPolicy
	}
	if expectedRevision <= 0 {
		return LineCallPolicy{}, ErrRevisionConflict
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return LineCallPolicy{}, fmt.Errorf("begin line call policy update: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_line_call_policies
		 SET policy = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ? AND revision = ?`,
		string(policy),
		lineID,
		expectedRevision,
	)
	if err != nil {
		return LineCallPolicy{}, fmt.Errorf("update line call policy: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return LineCallPolicy{}, fmt.Errorf("count updated line call policy: %w", err)
	}
	if affected != 1 {
		return LineCallPolicy{}, ErrRevisionConflict
	}
	updated, err := lineCallPolicy(ctx, transaction, lineID)
	if err != nil {
		return LineCallPolicy{}, err
	}
	if err := transaction.Commit(); err != nil {
		return LineCallPolicy{}, fmt.Errorf("commit line call policy update: %w", err)
	}
	return updated, nil
}

func (s *Store) EffectiveCallPolicy(
	ctx context.Context,
	lineID string,
) (EffectiveCallPolicy, error) {
	configuration, err := s.CallPolicyConfiguration(ctx, lineID)
	if err != nil {
		return EffectiveCallPolicy{}, err
	}
	return configuration.Effective, nil
}

func (s *Store) CallPolicyConfiguration(
	ctx context.Context,
	lineID string,
) (CallPolicyConfiguration, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return CallPolicyConfiguration{}, ErrInvalidCallPolicy
	}
	if _, err := s.LineCallPolicy(ctx, lineID); err != nil {
		return CallPolicyConfiguration{}, err
	}
	transaction, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CallPolicyConfiguration{}, fmt.Errorf("begin call policy configuration read: %w", err)
	}
	defer transaction.Rollback()
	configuration, err := callPolicyConfiguration(ctx, transaction, lineID)
	if err != nil {
		return CallPolicyConfiguration{}, err
	}
	if err := transaction.Commit(); err != nil {
		return CallPolicyConfiguration{}, fmt.Errorf("commit call policy configuration read: %w", err)
	}
	return configuration, nil
}

func (s *Store) ClaimIncomingCallActions(
	ctx context.Context,
	limit int,
) ([]IncomingCallAction, error) {
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("claim incoming call actions: invalid limit")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin incoming call action claim: %w", err)
	}
	defer transaction.Rollback()
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT call_id, line_id, endpoint_call_id, effective_policy,
			global_revision, line_revision, request_id, status, error_code,
			created_at, updated_at
		 FROM modemdeck_incoming_call_actions
		 WHERE status = 'pending'
		 ORDER BY created_at ASC, call_id ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending incoming call actions: %w", err)
	}
	candidates := make([]IncomingCallAction, 0)
	for rows.Next() {
		action, err := scanIncomingCallAction(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, action)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("read pending incoming call actions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close pending incoming call actions: %w", err)
	}

	claimed := make([]IncomingCallAction, 0, len(candidates))
	for _, action := range candidates {
		result, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_incoming_call_actions
			 SET status = 'sending', updated_at = CURRENT_TIMESTAMP
			 WHERE call_id = ? AND status = 'pending'`,
			action.CallID,
		)
		if err != nil {
			return nil, fmt.Errorf("claim incoming call action: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("count claimed incoming call action: %w", err)
		}
		if affected != 1 {
			continue
		}
		action.Status = IncomingCallActionSending
		claimed = append(claimed, action)
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit incoming call action claim: %w", err)
	}
	return claimed, nil
}

func (s *Store) FinishIncomingCallAction(
	ctx context.Context,
	callID string,
	status string,
	errorCode string,
) error {
	callID = strings.TrimSpace(callID)
	errorCode = strings.TrimSpace(errorCode)
	if callID == "" || !terminalIncomingCallActionStatus(status) || len(errorCode) > 128 {
		return ErrIncomingCallActionState
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_incoming_call_actions
		 SET status = ?, error_code = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE call_id = ? AND status = 'sending'`,
		status,
		errorCode,
		callID,
	)
	if err != nil {
		return fmt.Errorf("finish incoming call action: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count finished incoming call action: %w", err)
	}
	if affected != 1 {
		return ErrIncomingCallActionState
	}
	return nil
}

func (s *Store) IncomingCallAction(
	ctx context.Context,
	callID string,
) (IncomingCallAction, error) {
	return incomingCallAction(ctx, s.database, strings.TrimSpace(callID))
}

func (s *Store) LatestIncomingCallAction(
	ctx context.Context,
	lineID string,
) (*IncomingCallAction, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return nil, ErrInvalidCallPolicy
	}
	row := s.database.QueryRowContext(
		ctx,
		`SELECT call_id, line_id, endpoint_call_id, effective_policy,
			global_revision, line_revision, request_id, status, error_code,
			created_at, updated_at
		 FROM modemdeck_incoming_call_actions
		 WHERE line_id = ?
		 ORDER BY updated_at DESC, call_id DESC
		 LIMIT 1`,
		lineID,
	)
	action, err := scanIncomingCallAction(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read latest incoming call action: %w", err)
	}
	return &action, nil
}

func ensureLineCallPolicy(ctx context.Context, transaction *sql.Tx, lineID string) error {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return fmt.Errorf("%w: line id is empty", ErrSnapshotInvalid)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_line_call_policies (line_id, policy, revision, updated_at)
		 VALUES (?, 'follow_global', 1, CURRENT_TIMESTAMP)
		 ON CONFLICT(line_id) DO NOTHING`,
		lineID,
	); err != nil {
		return fmt.Errorf("ensure hardware line call policy: %w", err)
	}
	return nil
}

func enqueueIncomingCallAction(
	ctx context.Context,
	transaction *sql.Tx,
	call HardwareCall,
) error {
	if err := ensureLineCallPolicy(ctx, transaction, call.LineID); err != nil {
		return err
	}
	effective, err := effectiveCallPolicy(ctx, transaction, call.LineID)
	if err != nil {
		return err
	}
	if effective.Policy != EffectiveCallPolicyDND {
		return nil
	}
	digest := sha256.Sum256([]byte(call.AppID + "\x00" + call.EndpointCallID))
	requestID := "dnd_reject_" + hex.EncodeToString(digest[:16])
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_incoming_call_actions (
			call_id, line_id, endpoint_call_id, effective_policy,
			global_revision, line_revision, request_id, status, error_code,
			created_at, updated_at
		 ) VALUES (?, ?, ?, 'do_not_disturb', ?, ?, ?, 'pending', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(call_id) DO NOTHING`,
		call.AppID,
		call.LineID,
		call.EndpointCallID,
		effective.GlobalRevision,
		effective.LineRevision,
		requestID,
	); err != nil {
		return fmt.Errorf("enqueue incoming call reject action: %w", err)
	}
	return nil
}

type rowQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func globalCallSettings(ctx context.Context, query rowQuery) (GlobalCallSettings, error) {
	var (
		settings     GlobalCallSettings
		receiveCalls int64
	)
	err := query.QueryRowContext(
		ctx,
		`SELECT receive_calls, revision, updated_at
		 FROM modemdeck_call_settings
		 WHERE singleton = 1`,
	).Scan(&receiveCalls, &settings.Revision, &settings.UpdatedAt)
	if err != nil {
		return GlobalCallSettings{}, fmt.Errorf("read global call settings: %w", err)
	}
	settings.ReceiveCalls = receiveCalls != 0
	return settings, nil
}

func lineCallPolicy(
	ctx context.Context,
	query rowQuery,
	lineID string,
) (LineCallPolicy, error) {
	var policy LineCallPolicy
	var value string
	err := query.QueryRowContext(
		ctx,
		`SELECT line_id, policy, revision, updated_at
		 FROM modemdeck_line_call_policies
		 WHERE line_id = ?`,
		lineID,
	).Scan(&policy.LineID, &value, &policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return LineCallPolicy{}, fmt.Errorf("read line call policy: %w", err)
	}
	policy.Policy = LineCallPolicyValue(value)
	if !validLineCallPolicy(policy.Policy) {
		return LineCallPolicy{}, fmt.Errorf("read line call policy: %w", ErrInvalidCallPolicy)
	}
	return policy, nil
}

func effectiveCallPolicy(
	ctx context.Context,
	query rowQuery,
	lineID string,
) (EffectiveCallPolicy, error) {
	configuration, err := callPolicyConfiguration(ctx, query, lineID)
	if err != nil {
		return EffectiveCallPolicy{}, err
	}
	return configuration.Effective, nil
}

func callPolicyConfiguration(
	ctx context.Context,
	query rowQuery,
	lineID string,
) (CallPolicyConfiguration, error) {
	global, err := globalCallSettings(ctx, query)
	if err != nil {
		return CallPolicyConfiguration{}, err
	}
	line, err := lineCallPolicy(ctx, query, lineID)
	if err != nil {
		return CallPolicyConfiguration{}, err
	}
	effective := EffectiveCallPolicyReceive
	switch line.Policy {
	case LineCallPolicyReceive:
		effective = EffectiveCallPolicyReceive
	case LineCallPolicyDND:
		effective = EffectiveCallPolicyDND
	case LineCallPolicyFollowGlobal:
		if !global.ReceiveCalls {
			effective = EffectiveCallPolicyDND
		}
	default:
		return CallPolicyConfiguration{}, ErrInvalidCallPolicy
	}
	return CallPolicyConfiguration{
		Global: global,
		Line:   line,
		Effective: EffectiveCallPolicy{
			LineID:         lineID,
			Policy:         effective,
			GlobalRevision: global.Revision,
			LineRevision:   line.Revision,
		},
	}, nil
}

func incomingCallAction(
	ctx context.Context,
	query rowQuery,
	callID string,
) (IncomingCallAction, error) {
	if callID == "" {
		return IncomingCallAction{}, sql.ErrNoRows
	}
	row := query.QueryRowContext(
		ctx,
		`SELECT call_id, line_id, endpoint_call_id, effective_policy,
			global_revision, line_revision, request_id, status, error_code,
			created_at, updated_at
		 FROM modemdeck_incoming_call_actions
		 WHERE call_id = ?`,
		callID,
	)
	return scanIncomingCallAction(row)
}

type rowScanner interface {
	Scan(...any) error
}

func scanIncomingCallAction(scanner rowScanner) (IncomingCallAction, error) {
	var (
		action IncomingCallAction
		policy string
	)
	if err := scanner.Scan(
		&action.CallID,
		&action.LineID,
		&action.EndpointCallID,
		&policy,
		&action.GlobalRevision,
		&action.LineRevision,
		&action.RequestID,
		&action.Status,
		&action.ErrorCode,
		&action.CreatedAt,
		&action.UpdatedAt,
	); err != nil {
		return IncomingCallAction{}, err
	}
	action.EffectivePolicy = EffectiveCallPolicyValue(policy)
	return action, nil
}

func validLineCallPolicy(policy LineCallPolicyValue) bool {
	switch policy {
	case LineCallPolicyFollowGlobal, LineCallPolicyReceive, LineCallPolicyDND:
		return true
	default:
		return false
	}
}

func terminalIncomingCallActionStatus(status string) bool {
	switch status {
	case IncomingCallActionSucceeded,
		IncomingCallActionFailed,
		IncomingCallActionIndeterminate,
		IncomingCallActionSkipped:
		return true
	default:
		return false
	}
}
