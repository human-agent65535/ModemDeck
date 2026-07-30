package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

var ErrIOSPairingNotAllowed = errors.New("iOS pairing is not allowed")

type IOSPairingStatus struct {
	Allowed             bool   `json:"allowed"`
	HasCredential       bool   `json:"has_credential"`
	CredentialCreatedAt string `json:"credential_created_at,omitempty"`
}

func (s *Store) IOSPairingStatus(
	ctx context.Context,
	userID string,
) (IOSPairingStatus, error) {
	var (
		status    IOSPairingStatus
		enabled   int64
		pairing   int64
		createdAt sql.NullString
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT
			user.enabled,
			user.ios_pairing_enabled,
			credential.created_at
		 FROM modemdeck_users AS user
		 LEFT JOIN modemdeck_ios_pairing_credentials AS credential
			ON credential.user_id = user.id
		 WHERE user.id = ?`,
		userID,
	).Scan(
		&enabled,
		&pairing,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPairingStatus{}, ErrUserNotFound
	}
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("query iOS pairing status: %w", err)
	}
	status.Allowed = enabled != 0 && pairing != 0
	status.HasCredential = createdAt.Valid
	status.CredentialCreatedAt = stringValue(createdAt)
	return status, nil
}

func (s *Store) RotateIOSPairingCredential(
	ctx context.Context,
	userID string,
	digest mobilepairing.TokenDigest,
) (IOSPairingStatus, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("begin iOS pairing credential update: %w", err)
	}
	defer transaction.Rollback()

	var enabled, pairing int64
	err = transaction.QueryRowContext(
		ctx,
		`SELECT user.enabled, user.ios_pairing_enabled
		 FROM modemdeck_users AS user
		 WHERE user.id = ?`,
		userID,
	).Scan(&enabled, &pairing)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPairingStatus{}, ErrUserNotFound
	}
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("read user before iOS pairing: %w", err)
	}
	if enabled == 0 || pairing == 0 {
		return IOSPairingStatus{}, ErrIOSPairingNotAllowed
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_ios_pairing_credentials (
			user_id, token_digest, created_at, updated_at
		 ) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id) DO UPDATE SET
			token_digest = excluded.token_digest,
			created_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP`,
		userID,
		digest[:],
	); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("store iOS pairing credential: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("commit iOS pairing credential update: %w", err)
	}
	return s.IOSPairingStatus(ctx, userID)
}

func (s *Store) RevokeIOSPairingCredential(
	ctx context.Context,
	userID string,
) error {
	if _, err := s.database.ExecContext(
		ctx,
		"DELETE FROM modemdeck_ios_pairing_credentials WHERE user_id = ?",
		userID,
	); err != nil {
		return fmt.Errorf("revoke iOS pairing credential: %w", err)
	}
	return nil
}
