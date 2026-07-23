package store

import (
	"context"
	"fmt"
)

func (s *Store) RecoverInterruptedCommunicationOperations(ctx context.Context) error {
	if s == nil || s.database == nil {
		return ErrDatabaseRequired
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin communication recovery: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_incoming_call_actions
		 SET status = ?, error_code = CASE
			WHEN error_code = '' THEN 'process_interrupted'
			ELSE error_code END,
			updated_at = CURRENT_TIMESTAMP
		 WHERE status = ?`,
		IncomingCallActionIndeterminate,
		IncomingCallActionSending,
	); err != nil {
		return fmt.Errorf("recover incoming call actions: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_hardware_commands
		 SET status = ?, error_code = CASE
			WHEN error_code = '' THEN 'process_interrupted'
			ELSE error_code END,
			updated_at = CURRENT_TIMESTAMP
		 WHERE status = ?`,
		HardwareCommandIndeterminate,
		HardwareCommandPending,
	); err != nil {
		return fmt.Errorf("recover hardware commands: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit communication recovery: %w", err)
	}
	return nil
}
