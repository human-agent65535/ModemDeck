package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

var ErrInvalidAuthSession = errors.New("invalid authentication session record")

const maxAdminSessions = 32

func (s *Store) AdminCredentials(
	ctx context.Context,
) (auth.AdminCredentials, bool, error) {
	var credentials auth.AdminCredentials
	err := s.database.QueryRowContext(
		ctx,
		`SELECT username, password_hash
		 FROM modemdeck_admin_credentials
		 WHERE singleton = 1`,
	).Scan(&credentials.Username, &credentials.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.AdminCredentials{}, false, nil
	}
	if err != nil {
		return auth.AdminCredentials{}, false, fmt.Errorf("query admin credentials: %w", err)
	}
	return credentials, true, nil
}

func (s *Store) CreateAdminIfAbsent(
	ctx context.Context,
	credentials auth.AdminCredentials,
) (bool, error) {
	if credentials.Username == "" {
		return false, fmt.Errorf("create admin credentials: username is empty")
	}
	if credentials.PasswordHash == "" {
		return false, fmt.Errorf("create admin credentials: password hash is empty")
	}
	result, err := s.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_admin_credentials (
			singleton, username, password_hash, updated_at
		 )
		 VALUES (1, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(singleton) DO NOTHING`,
		credentials.Username,
		credentials.PasswordHash,
	)
	if err != nil {
		return false, fmt.Errorf("create admin credentials: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read created admin credential count: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("create admin credentials: unexpected row count %d", affected)
	}
	return affected == 1, nil
}

func (s *Store) ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
	ctx context.Context,
	expectedPasswordHash, replacementPasswordHash string,
) (bool, error) {
	if expectedPasswordHash == "" {
		return false, fmt.Errorf("replace admin password hash: expected password hash is empty")
	}
	if replacementPasswordHash == "" {
		return false, fmt.Errorf("replace admin password hash: replacement password hash is empty")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin conditional admin password replacement: %w", err)
	}
	defer transaction.Rollback()

	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_admin_credentials
		 SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND password_hash = ?`,
		replacementPasswordHash,
		expectedPasswordHash,
	)
	if err != nil {
		return false, fmt.Errorf("conditionally replace admin password hash: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read replaced admin password count: %w", err)
	}
	if affected == 0 {
		return false, nil
	}
	if affected != 1 {
		return false, fmt.Errorf("replace admin password hash: unexpected row count %d", affected)
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM modemdeck_auth_sessions"); err != nil {
		return false, fmt.Errorf("revoke admin sessions: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit conditional admin password replacement: %w", err)
	}
	return true, nil
}

func (s *Store) CreateSessionIfPasswordHash(
	ctx context.Context,
	expectedPasswordHash string,
	session auth.SessionRecord,
) (bool, error) {
	if expectedPasswordHash == "" {
		return false, fmt.Errorf("create authentication session: expected password hash is empty")
	}
	if err := validateAuthSession(session); err != nil {
		return false, err
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin authentication session creation: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		"DELETE FROM modemdeck_auth_sessions WHERE expires_at_unix <= ?",
		session.CreatedAt.UTC().Unix(),
	); err != nil {
		return false, fmt.Errorf("delete expired authentication sessions: %w", err)
	}
	result, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_auth_sessions (
			session_token_digest, csrf_token_digest, created_at_unix, expires_at_unix
		 )
		 SELECT ?, ?, ?, ?
		 WHERE EXISTS (
			SELECT 1 FROM modemdeck_admin_credentials
			WHERE singleton = 1 AND password_hash = ?
		 )`,
		session.SessionTokenDigest[:],
		session.CSRFTokenDigest[:],
		session.CreatedAt.UTC().Unix(),
		session.ExpiresAt.UTC().Unix(),
		expectedPasswordHash,
	)
	if err != nil {
		return false, fmt.Errorf("create authentication session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read created authentication session count: %w", err)
	}
	created := affected == 1
	if created {
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM modemdeck_auth_sessions
			 WHERE session_token_digest IN (
				SELECT session_token_digest
				FROM modemdeck_auth_sessions
				ORDER BY created_at_unix DESC, rowid DESC
				LIMIT -1 OFFSET ?
			 )`,
			maxAdminSessions,
		); err != nil {
			return false, fmt.Errorf("limit active authentication sessions: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit authentication session creation: %w", err)
	}
	return created, nil
}

func (s *Store) SessionByTokenDigest(
	ctx context.Context,
	digest auth.SessionTokenDigest,
) (auth.SessionRecord, bool, error) {
	var (
		sessionDigest []byte
		csrfDigest    []byte
		createdAt     int64
		expiresAt     int64
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT session_token_digest, csrf_token_digest, created_at_unix, expires_at_unix
		 FROM modemdeck_auth_sessions
		 WHERE session_token_digest = ?`,
		digest[:],
	).Scan(&sessionDigest, &csrfDigest, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.SessionRecord{}, false, nil
	}
	if err != nil {
		return auth.SessionRecord{}, false, fmt.Errorf("query authentication session: %w", err)
	}
	if len(sessionDigest) != len(auth.SessionTokenDigest{}) || len(csrfDigest) != len(auth.CSRFTokenDigest{}) {
		return auth.SessionRecord{}, false, ErrInvalidAuthSession
	}
	var result auth.SessionRecord
	copy(result.SessionTokenDigest[:], sessionDigest)
	copy(result.CSRFTokenDigest[:], csrfDigest)
	result.CreatedAt = time.Unix(createdAt, 0).UTC()
	result.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	if err := validateAuthSession(result); err != nil {
		return auth.SessionRecord{}, false, err
	}
	return result, true, nil
}

func (s *Store) DeleteSessionByTokenDigest(ctx context.Context, digest auth.SessionTokenDigest) error {
	if _, err := s.database.ExecContext(
		ctx,
		"DELETE FROM modemdeck_auth_sessions WHERE session_token_digest = ?",
		digest[:],
	); err != nil {
		return fmt.Errorf("delete authentication session: %w", err)
	}
	return nil
}

func validateAuthSession(session auth.SessionRecord) error {
	if session.CreatedAt.IsZero() || session.ExpiresAt.IsZero() || session.ExpiresAt.Before(session.CreatedAt) {
		return ErrInvalidAuthSession
	}
	return nil
}
