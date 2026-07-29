package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidMessagePolicy = errors.New("invalid message policy")

func (s *Store) MessageDeliveryPolicy(
	ctx context.Context,
	lineID string,
) (MessageDeliveryPolicy, error) {
	return messageDeliveryPolicy(ctx, s.database, lineID)
}

func (s *Store) UpdateMessageDeliveryPolicy(
	ctx context.Context,
	lineID string,
	enabled bool,
	expectedRevision int64,
) (MessageDeliveryPolicy, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return MessageDeliveryPolicy{}, ErrInvalidMessagePolicy
	}
	if expectedRevision <= 0 {
		return MessageDeliveryPolicy{}, ErrRevisionConflict
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("begin message delivery policy update: %w", err)
	}
	defer transaction.Rollback()

	statement := `UPDATE modemdeck_lines
		SET delivery_reports_enabled = ?,
			message_policy_revision = message_policy_revision + 1
		WHERE line_id = ? AND message_policy_revision = ?`
	if enabled {
		statement = `UPDATE modemdeck_lines
			SET delivery_reports_enabled = 1,
				delivery_reports_support = 'unknown',
				message_policy_revision = message_policy_revision + 1
			WHERE line_id = ? AND message_policy_revision = ?`
	}
	var result sql.Result
	if enabled {
		result, err = transaction.ExecContext(ctx, statement, lineID, expectedRevision)
	} else {
		result, err = transaction.ExecContext(
			ctx,
			statement,
			false,
			lineID,
			expectedRevision,
		)
	}
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("update message delivery policy: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("count updated message delivery policy: %w", err)
	}
	if affected != 1 {
		return MessageDeliveryPolicy{}, ErrRevisionConflict
	}
	policy, err := messageDeliveryPolicy(ctx, transaction, lineID)
	if err != nil {
		return MessageDeliveryPolicy{}, err
	}
	if err := transaction.Commit(); err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("commit message delivery policy update: %w", err)
	}
	return policy, nil
}

func (s *Store) MarkMessageDeliveryReportsUnsupported(
	ctx context.Context,
	lineID string,
	expectedRevision int64,
) (MessageDeliveryPolicy, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return MessageDeliveryPolicy{}, ErrInvalidMessagePolicy
	}
	if expectedRevision <= 0 {
		return MessageDeliveryPolicy{}, ErrRevisionConflict
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("begin unsupported message policy update: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_lines
		 SET delivery_reports_enabled = 0,
			delivery_reports_support = 'unsupported',
			message_policy_revision = message_policy_revision + 1
		 WHERE line_id = ? AND message_policy_revision = ?`,
		lineID,
		expectedRevision,
	)
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("mark delivery reports unsupported: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("count unsupported message policy update: %w", err)
	}
	if affected != 1 {
		return MessageDeliveryPolicy{}, ErrRevisionConflict
	}
	policy, err := messageDeliveryPolicy(ctx, transaction, lineID)
	if err != nil {
		return MessageDeliveryPolicy{}, err
	}
	if err := transaction.Commit(); err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("commit unsupported message policy update: %w", err)
	}
	return policy, nil
}

func messageDeliveryPolicy(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	lineID string,
) (MessageDeliveryPolicy, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return MessageDeliveryPolicy{}, ErrInvalidMessagePolicy
	}
	var (
		enabled  sql.NullInt64
		support  sql.NullString
		revision sql.NullInt64
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT delivery_reports_enabled, delivery_reports_support,
			message_policy_revision
		 FROM modemdeck_lines
		 WHERE line_id = ?`,
		lineID,
	).Scan(&enabled, &support, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageDeliveryPolicy{}, ErrInvalidMessagePolicy
	}
	if err != nil {
		return MessageDeliveryPolicy{}, fmt.Errorf("read message delivery policy: %w", err)
	}
	value := MessageDeliveryPolicy{
		LineID:                 lineID,
		DeliveryReportsEnabled: boolValue(enabled),
		DeliveryReportsSupport: MessageDeliveryReportSupport(stringValue(support)),
		Revision:               intValue(revision),
	}
	if value.DeliveryReportsSupport != MessageDeliveryReportSupportUnknown &&
		value.DeliveryReportsSupport != MessageDeliveryReportSupportUnsupported {
		return MessageDeliveryPolicy{}, fmt.Errorf(
			"read message delivery policy: invalid support state %q",
			value.DeliveryReportsSupport,
		)
	}
	if value.Revision <= 0 {
		return MessageDeliveryPolicy{}, fmt.Errorf(
			"read message delivery policy: invalid revision",
		)
	}
	return value, nil
}
