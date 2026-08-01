package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

var ErrIOSPairingNotAllowed = errors.New("iOS pairing is not allowed")

type IOSPairingStatus struct {
	Allowed             bool   `json:"allowed"`
	HasCredential       bool   `json:"has_credential"`
	CredentialCreatedAt string `json:"credential_created_at,omitempty"`
	Paired              bool   `json:"paired"`
	PairedAt            string `json:"paired_at,omitempty"`
}

func (s *Store) IOSPairingPrincipalByTokenDigest(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) (auth.Principal, bool, error) {
	var (
		principal auth.Principal
		role      string
		pairing   int64
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT
			user.id,
			user.username,
			user.role,
			user.ios_pairing_enabled,
			COALESCE(profile.contact_id, '')
		 FROM modemdeck_ios_pairing_credentials AS credential
		 JOIN modemdeck_users AS user
			ON user.id = credential.user_id
				AND user.enabled = 1
				AND user.ios_pairing_enabled = 1
		 LEFT JOIN modemdeck_user_profile_contacts AS profile
			ON profile.user_id = user.id
		 WHERE credential.token_digest = ?`,
		digest[:],
	).Scan(
		&principal.UserID,
		&principal.Username,
		&role,
		&pairing,
		&principal.ProfileContactID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, false, nil
	}
	if err != nil {
		return auth.Principal{}, false, fmt.Errorf(
			"query iOS pairing principal: %w",
			err,
		)
	}
	principal.Role = auth.Role(role)
	principal.IOSPairingEnabled = pairing != 0

	rows, err := s.database.QueryContext(
		ctx,
		`SELECT line_id
		 FROM modemdeck_user_lines
		 WHERE user_id = ?
		 ORDER BY line_id`,
		principal.UserID,
	)
	if err != nil {
		return auth.Principal{}, false, fmt.Errorf(
			"query iOS pairing line access: %w",
			err,
		)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		if err := rows.Scan(&lineID); err != nil {
			return auth.Principal{}, false, fmt.Errorf(
				"scan iOS pairing line access: %w",
				err,
			)
		}
		principal.AllowedLineIDs = append(
			principal.AllowedLineIDs,
			lineID,
		)
	}
	if err := rows.Err(); err != nil {
		return auth.Principal{}, false, fmt.Errorf(
			"iterate iOS pairing line access: %w",
			err,
		)
	}
	return principal, true, nil
}

func (s *Store) ConfirmIOSPairingCredential(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) (bool, error) {
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET activated_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		 WHERE token_digest = ?
			AND activated_at IS NULL
			AND EXISTS (
				SELECT 1
				FROM modemdeck_users AS user
				WHERE user.id = modemdeck_ios_pairing_credentials.user_id
					AND user.enabled = 1
					AND user.ios_pairing_enabled = 1
			)`,
		digest[:],
	)
	if err != nil {
		return false, fmt.Errorf("confirm iOS pairing credential: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf(
			"read iOS pairing confirmation result: %w",
			err,
		)
	}
	return updated > 0, nil
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
		pairedAt  sql.NullString
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT
			user.enabled,
			user.ios_pairing_enabled,
			credential.created_at,
			credential.activated_at
		 FROM modemdeck_users AS user
		 LEFT JOIN modemdeck_ios_pairing_credentials AS credential
			ON credential.user_id = user.id
		 WHERE user.id = ?`,
		userID,
	).Scan(
		&enabled,
		&pairing,
		&createdAt,
		&pairedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPairingStatus{}, ErrUserNotFound
	}
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("query iOS pairing status: %w", err)
	}
	status.Allowed = enabled != 0 && pairing != 0
	status.HasCredential = createdAt.Valid
	status.CredentialCreatedAt = iosPairingTimestamp(stringValue(createdAt))
	status.Paired = pairedAt.Valid
	status.PairedAt = iosPairingTimestamp(stringValue(pairedAt))
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
	var createdAt string
	if err := transaction.QueryRowContext(
		ctx,
		`INSERT INTO modemdeck_ios_pairing_credentials (
			user_id, token_digest, created_at, updated_at
		 ) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id) DO UPDATE SET
			token_digest = excluded.token_digest,
			activated_at = NULL,
			created_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		 RETURNING created_at`,
		userID,
		digest[:],
	).Scan(&createdAt); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("store iOS pairing credential: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("commit iOS pairing credential update: %w", err)
	}
	return IOSPairingStatus{
		Allowed:             true,
		HasCredential:       true,
		CredentialCreatedAt: iosPairingTimestamp(createdAt),
	}, nil
}

func (s *Store) RevokeIOSPairingCredential(
	ctx context.Context,
	userID string,
) error {
	_, _, err := s.RevokeIOSPairingCredentialWithDigest(ctx, userID)
	return err
}

func (s *Store) RevokeIOSPairingCredentialWithDigest(
	ctx context.Context,
	userID string,
) (mobilepairing.TokenDigest, bool, error) {
	var value []byte
	err := s.database.QueryRowContext(
		ctx,
		`DELETE FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ?
		 RETURNING token_digest`,
		userID,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return mobilepairing.TokenDigest{}, false, nil
	}
	if err != nil {
		return mobilepairing.TokenDigest{}, false, fmt.Errorf(
			"revoke iOS pairing credential: %w",
			err,
		)
	}
	if len(value) != len(mobilepairing.TokenDigest{}) {
		return mobilepairing.TokenDigest{}, false, fmt.Errorf(
			"revoke iOS pairing credential: invalid digest length %d",
			len(value),
		)
	}
	var digest mobilepairing.TokenDigest
	copy(digest[:], value)
	return digest, true, nil
}

func iosPairingTimestamp(value string) string {
	parsed, ok := parseDatabaseTime(value)
	if !ok {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}
