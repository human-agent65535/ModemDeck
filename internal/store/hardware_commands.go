package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	HardwareCommandPending       = "pending"
	HardwareCommandCompleted     = "completed"
	HardwareCommandFailed        = "failed"
	HardwareCommandIndeterminate = "indeterminate"
)

var ErrHardwareCommandConflict = errors.New("hardware command request conflicts with an existing request")

func (s *Store) BeginHardwareCommand(
	ctx context.Context,
	requestID, operation string,
	payloadDigest []byte,
) (HardwareCommand, bool, error) {
	requestID = strings.TrimSpace(requestID)
	operation = strings.TrimSpace(operation)
	if requestID == "" || operation == "" || len(payloadDigest) != 32 {
		return HardwareCommand{}, false, fmt.Errorf("begin hardware command: invalid identity")
	}
	result, err := s.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_hardware_commands (
			request_id, operation, payload_digest, status, resource_id,
			error_code, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, '', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(request_id) DO NOTHING`,
		requestID,
		operation,
		payloadDigest,
		HardwareCommandPending,
	)
	if err != nil {
		return HardwareCommand{}, false, fmt.Errorf("insert hardware command: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return HardwareCommand{}, false, fmt.Errorf("read inserted hardware command count: %w", err)
	}
	command, err := s.HardwareCommand(ctx, requestID)
	if err != nil {
		return HardwareCommand{}, false, err
	}
	if command.Operation != operation || !bytes.Equal(command.PayloadDigest, payloadDigest) {
		return HardwareCommand{}, false, ErrHardwareCommandConflict
	}
	return command, affected == 1, nil
}

func (s *Store) HardwareCommand(ctx context.Context, requestID string) (HardwareCommand, error) {
	var (
		command               HardwareCommand
		resourceID, errorCode sql.NullString
		createdAt, updatedAt  sql.NullString
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT request_id, operation, payload_digest, status, resource_id,
			error_code, created_at, updated_at
		 FROM modemdeck_hardware_commands WHERE request_id = ?`,
		strings.TrimSpace(requestID),
	).Scan(
		&command.RequestID,
		&command.Operation,
		&command.PayloadDigest,
		&command.Status,
		&resourceID,
		&errorCode,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return HardwareCommand{}, ErrHardwareCommandConflict
	}
	if err != nil {
		return HardwareCommand{}, fmt.Errorf("query hardware command: %w", err)
	}
	command.PayloadDigest = append([]byte(nil), command.PayloadDigest...)
	command.ResourceID = stringValue(resourceID)
	command.ErrorCode = stringValue(errorCode)
	command.CreatedAt = stringValue(createdAt)
	command.UpdatedAt = stringValue(updatedAt)
	return command, nil
}

func (s *Store) FinishHardwareCommand(
	ctx context.Context,
	requestID, status, resourceID, errorCode string,
) error {
	switch status {
	case HardwareCommandCompleted, HardwareCommandFailed, HardwareCommandIndeterminate:
	default:
		return fmt.Errorf("finish hardware command: invalid status")
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_hardware_commands SET
			status = ?, resource_id = ?, error_code = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE request_id = ? AND status = ?`,
		status,
		strings.TrimSpace(resourceID),
		strings.TrimSpace(errorCode),
		strings.TrimSpace(requestID),
		HardwareCommandPending,
	)
	if err != nil {
		return fmt.Errorf("finish hardware command: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read finished hardware command count: %w", err)
	}
	if affected == 1 {
		return nil
	}
	command, err := s.HardwareCommand(ctx, requestID)
	if err != nil {
		return err
	}
	if command.Status == status &&
		command.ResourceID == strings.TrimSpace(resourceID) &&
		command.ErrorCode == strings.TrimSpace(errorCode) {
		return nil
	}
	return ErrHardwareCommandConflict
}

func (s *Store) MessageByRequestID(ctx context.Context, requestID string) (Message, error) {
	var id int64
	err := s.database.QueryRowContext(
		ctx,
		"SELECT id FROM sms WHERE request_id = ?",
		strings.TrimSpace(requestID),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("query message request identity: %w", err)
	}
	return messageByID(ctx, s.database, id)
}

func (s *Store) CallByRequestID(ctx context.Context, requestID string) (Call, error) {
	var id string
	err := s.database.QueryRowContext(
		ctx,
		"SELECT id FROM call_history WHERE request_id = ?",
		strings.TrimSpace(requestID),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Call{}, ErrCallNotFound
	}
	if err != nil {
		return Call{}, fmt.Errorf("query call request identity: %w", err)
	}
	return callByID(ctx, s.database, id)
}

func (s *Store) CallByID(ctx context.Context, id string) (Call, error) {
	return callByID(ctx, s.database, strings.TrimSpace(id))
}
