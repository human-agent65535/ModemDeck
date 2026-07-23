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

func (s *Store) AdminPasswordHash(ctx context.Context) (string, bool, error) {
	var passwordHash string
	err := s.database.QueryRowContext(
		ctx,
		"SELECT password_hash FROM modemdeck_admin_credentials WHERE singleton = 1",
	).Scan(&passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query admin password hash: %w", err)
	}
	return passwordHash, true, nil
}

func (s *Store) ReplaceAdminPasswordHashAndRevokeSessions(ctx context.Context, passwordHash string) error {
	if passwordHash == "" {
		return fmt.Errorf("replace admin password hash: password hash is empty")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin admin password replacement: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_admin_credentials (singleton, password_hash, updated_at)
		 VALUES (1, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(singleton) DO UPDATE SET
			password_hash = excluded.password_hash,
			updated_at = CURRENT_TIMESTAMP`,
		passwordHash,
	); err != nil {
		return fmt.Errorf("replace admin password hash: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM modemdeck_auth_sessions"); err != nil {
		return fmt.Errorf("revoke admin sessions: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit admin password replacement: %w", err)
	}
	return nil
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
