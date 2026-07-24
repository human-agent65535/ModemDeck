package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNetworkSelectionPolicyNotFound   = errors.New("network selection policy not found")
	ErrNetworkSelectionRevisionConflict = errors.New(
		"network selection policy revision conflict",
	)
)

func (s *Store) NetworkSelectionPolicy(
	ctx context.Context,
	lineID string,
) (NetworkSelectionPolicyRecord, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return NetworkSelectionPolicyRecord{}, ErrNetworkSelectionPolicyNotFound
	}
	return s.networkSelectionPolicy(ctx, lineID)
}

func (s *Store) EnsureNetworkSelectionPolicy(
	ctx context.Context,
	lineID string,
) (NetworkSelectionPolicyRecord, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return NetworkSelectionPolicyRecord{}, ErrNetworkSelectionPolicyNotFound
	}
	if _, err := s.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_network_selection_policies (
			line_id, mode, operator_code, revision, applied_revision,
			applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 ) VALUES (?, 'auto', '', 1, 0, '', NULL, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(line_id) DO NOTHING`,
		lineID,
	); err != nil {
		return NetworkSelectionPolicyRecord{}, fmt.Errorf(
			"initialize network selection policy: %w",
			err,
		)
	}
	return s.networkSelectionPolicy(ctx, lineID)
}

func (s *Store) NetworkSelectionPolicies(
	ctx context.Context,
) ([]NetworkSelectionPolicyRecord, error) {
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT line_id, mode, operator_code, revision, applied_revision,
		        applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 FROM modemdeck_network_selection_policies
		 ORDER BY line_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list network selection policies: %w", err)
	}
	defer rows.Close()

	policies := make([]NetworkSelectionPolicyRecord, 0)
	for rows.Next() {
		policy, err := scanNetworkSelectionPolicy(rows)
		if err != nil {
			return nil, fmt.Errorf("scan network selection policy: %w", err)
		}
		policies = append(policies, policy)
	}
	return policies, rowsError("read network selection policies", rows.Err())
}

func (s *Store) UpdateNetworkSelectionPolicy(
	ctx context.Context,
	lineID string,
	mode string,
	operatorCode string,
	expectedRevision int64,
) (NetworkSelectionPolicyRecord, error) {
	lineID = strings.TrimSpace(lineID)
	mode = strings.TrimSpace(mode)
	operatorCode = strings.TrimSpace(operatorCode)
	if lineID == "" || expectedRevision <= 0 {
		return NetworkSelectionPolicyRecord{}, ErrNetworkSelectionRevisionConflict
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_network_selection_policies
		 SET mode = ?,
		     operator_code = ?,
		     revision = revision + 1,
		     last_error = '',
		     updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ? AND revision = ?`,
		mode,
		operatorCode,
		lineID,
		expectedRevision,
	)
	if err != nil {
		return NetworkSelectionPolicyRecord{}, fmt.Errorf(
			"update network selection policy: %w",
			err,
		)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return NetworkSelectionPolicyRecord{}, fmt.Errorf(
			"inspect network selection policy update: %w",
			err,
		)
	}
	if affected == 0 {
		if _, err := s.networkSelectionPolicy(ctx, lineID); errors.Is(
			err,
			ErrNetworkSelectionPolicyNotFound,
		) {
			return NetworkSelectionPolicyRecord{}, ErrNetworkSelectionPolicyNotFound
		} else if err != nil {
			return NetworkSelectionPolicyRecord{}, err
		}
		return NetworkSelectionPolicyRecord{}, ErrNetworkSelectionRevisionConflict
	}
	return s.networkSelectionPolicy(ctx, lineID)
}

func (s *Store) MarkNetworkSelectionApplied(
	ctx context.Context,
	lineID string,
	revision int64,
	bootEpoch string,
	appliedAt time.Time,
) (bool, error) {
	lineID = strings.TrimSpace(lineID)
	bootEpoch = strings.TrimSpace(bootEpoch)
	if lineID == "" || revision <= 0 || bootEpoch == "" {
		return true, errors.New("mark network selection applied: invalid apply token")
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_network_selection_policies
		 SET applied_revision = revision,
		     applied_boot_epoch = ?,
		     applied_at = ?,
		     last_error = '',
		     updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ? AND revision = ?`,
		bootEpoch,
		databaseTime(appliedAt),
		lineID,
		revision,
	)
	if err != nil {
		return true, fmt.Errorf("mark network selection applied: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return true, fmt.Errorf("inspect network selection apply: %w", err)
	}
	return affected == 0, nil
}

func (s *Store) MarkNetworkSelectionApplyFailed(
	ctx context.Context,
	lineID string,
	revision int64,
	message string,
) error {
	lineID = strings.TrimSpace(lineID)
	message = strings.TrimSpace(message)
	if len(message) > 512 {
		message = message[:512]
	}
	if lineID == "" || revision <= 0 || message == "" {
		return errors.New("mark network selection apply failed: invalid failure")
	}
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_network_selection_policies
		 SET last_error = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ? AND revision = ?`,
		message,
		lineID,
		revision,
	); err != nil {
		return fmt.Errorf("mark network selection apply failed: %w", err)
	}
	return nil
}

func (s *Store) networkSelectionPolicy(
	ctx context.Context,
	lineID string,
) (NetworkSelectionPolicyRecord, error) {
	policy, err := scanNetworkSelectionPolicy(s.database.QueryRowContext(
		ctx,
		`SELECT line_id, mode, operator_code, revision, applied_revision,
		        applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 FROM modemdeck_network_selection_policies
		 WHERE line_id = ?`,
		lineID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return NetworkSelectionPolicyRecord{}, ErrNetworkSelectionPolicyNotFound
	}
	if err != nil {
		return NetworkSelectionPolicyRecord{}, fmt.Errorf(
			"load network selection policy: %w",
			err,
		)
	}
	return policy, nil
}

type networkSelectionScanner interface {
	Scan(...any) error
}

func scanNetworkSelectionPolicy(
	scanner networkSelectionScanner,
) (NetworkSelectionPolicyRecord, error) {
	var (
		policy                                       NetworkSelectionPolicyRecord
		lineID, mode, operatorCode, appliedBootEpoch sql.NullString
		appliedAt, lastError, createdAt, updatedAt   sql.NullString
		revision, appliedRevision                    sql.NullInt64
	)
	if err := scanner.Scan(
		&lineID,
		&mode,
		&operatorCode,
		&revision,
		&appliedRevision,
		&appliedBootEpoch,
		&appliedAt,
		&lastError,
		&createdAt,
		&updatedAt,
	); err != nil {
		return NetworkSelectionPolicyRecord{}, err
	}
	policy.LineID = stringValue(lineID)
	policy.Mode = stringValue(mode)
	policy.OperatorCode = stringValue(operatorCode)
	policy.Revision = intValue(revision)
	policy.AppliedRevision = intValue(appliedRevision)
	policy.AppliedBootEpoch = stringValue(appliedBootEpoch)
	policy.AppliedAt = stringValue(appliedAt)
	policy.LastError = stringValue(lastError)
	policy.CreatedAt = stringValue(createdAt)
	policy.UpdatedAt = stringValue(updatedAt)
	return policy, nil
}
