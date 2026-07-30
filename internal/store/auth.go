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
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin administrator creation: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
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
	if affected == 0 {
		return false, nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, must_change_password,
			ios_pairing_enabled
		 )
		 VALUES (?, ?, ?, 'admin', 1, 0, 1)`,
		auth.InitialAdminUserID,
		credentials.Username,
		credentials.PasswordHash,
	); err != nil {
		return false, fmt.Errorf("create initial administrator user: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_user_lines (user_id, line_id)
		 SELECT ?, line_id
		 FROM modemdeck_lines
		 ORDER BY line_id`,
		auth.InitialAdminUserID,
	); err != nil {
		return false, fmt.Errorf("assign existing lines to initial administrator: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_user_preferences (
			user_id,
			default_line_id,
			language,
			recording_default_enabled
		 )
		 SELECT ?,
			CASE
				WHEN EXISTS (
					SELECT 1 FROM modemdeck_user_lines
					WHERE user_id = ? AND line_id = settings.default_line_id
				) THEN settings.default_line_id
				ELSE COALESCE((
					SELECT line_id FROM modemdeck_user_lines
					WHERE user_id = ? ORDER BY line_id LIMIT 1
				), '')
			END,
			COALESCE((
				SELECT language FROM modemdeck_system_settings
				WHERE singleton = 1
			), 'auto'),
			COALESCE((
				SELECT default_enabled FROM modemdeck_recording_settings
				WHERE singleton = 1
			), 0)
		 FROM modemdeck_line_settings AS settings
		 WHERE settings.singleton = 1`,
		auth.InitialAdminUserID,
		auth.InitialAdminUserID,
		auth.InitialAdminUserID,
	); err != nil {
		return false, fmt.Errorf("create initial administrator preferences: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit administrator creation: %w", err)
	}
	return true, nil
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

func (s *Store) UserCredentialsByUsername(
	ctx context.Context,
	username string,
) (auth.UserCredentials, bool, error) {
	return s.userCredentials(
		ctx,
		`SELECT id, username, password_hash, role, enabled, must_change_password
		 FROM modemdeck_users
		 WHERE username = ? COLLATE NOCASE`,
		username,
	)
}

func (s *Store) UserCredentialsByID(
	ctx context.Context,
	userID string,
) (auth.UserCredentials, bool, error) {
	return s.userCredentials(
		ctx,
		`SELECT id, username, password_hash, role, enabled, must_change_password
		 FROM modemdeck_users
		 WHERE id = ?`,
		userID,
	)
}

func (s *Store) userCredentials(
	ctx context.Context,
	statement string,
	argument string,
) (auth.UserCredentials, bool, error) {
	var (
		credentials auth.UserCredentials
		role        string
		enabled     int64
		mustChange  int64
	)
	err := s.database.QueryRowContext(ctx, statement, argument).Scan(
		&credentials.ID,
		&credentials.Username,
		&credentials.PasswordHash,
		&role,
		&enabled,
		&mustChange,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.UserCredentials{}, false, nil
	}
	if err != nil {
		return auth.UserCredentials{}, false, fmt.Errorf("query user credentials: %w", err)
	}
	credentials.Role = auth.Role(role)
	credentials.Enabled = enabled != 0
	credentials.MustChangePassword = mustChange != 0
	return credentials, true, nil
}

func (s *Store) CreateUserSessionIfPasswordHash(
	ctx context.Context,
	expectedPasswordHash string,
	session auth.UserSessionRecord,
) (bool, error) {
	if expectedPasswordHash == "" || session.UserID == "" {
		return false, fmt.Errorf("create user session: credentials are incomplete")
	}
	if session.CreatedAt.IsZero() ||
		session.ExpiresAt.IsZero() ||
		session.ExpiresAt.Before(session.CreatedAt) {
		return false, ErrInvalidAuthSession
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin user session creation: %w", err)
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
			session_token_digest, csrf_token_digest, user_id,
			created_at_unix, expires_at_unix
		 )
		 SELECT ?, ?, id, ?, ?
		 FROM modemdeck_users
		 WHERE id = ? AND enabled = 1 AND password_hash = ?`,
		session.SessionTokenDigest[:],
		session.CSRFTokenDigest[:],
		session.CreatedAt.UTC().Unix(),
		session.ExpiresAt.UTC().Unix(),
		session.UserID,
		expectedPasswordHash,
	)
	if err != nil {
		return false, fmt.Errorf("create user session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read created user session count: %w", err)
	}
	if affected == 1 {
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM modemdeck_auth_sessions
			 WHERE session_token_digest IN (
				SELECT session_token_digest
				FROM modemdeck_auth_sessions
				WHERE user_id = ?
				ORDER BY created_at_unix DESC, rowid DESC
				LIMIT -1 OFFSET ?
			 )`,
			session.UserID,
			maxAdminSessions,
		); err != nil {
			return false, fmt.Errorf("limit active user sessions: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit user session creation: %w", err)
	}
	return affected == 1, nil
}

func (s *Store) UserSessionByTokenDigest(
	ctx context.Context,
	digest auth.SessionTokenDigest,
) (auth.UserSessionRecord, auth.Principal, bool, error) {
	var (
		record        auth.UserSessionRecord
		principal     auth.Principal
		sessionDigest []byte
		csrfDigest    []byte
		role          string
		mustChange    int64
		iosPairing    int64
		createdAt     int64
		expiresAt     int64
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT
			session.session_token_digest,
			session.csrf_token_digest,
			session.user_id,
			session.created_at_unix,
			session.expires_at_unix,
			user.username,
			user.role,
			user.must_change_password,
			user.ios_pairing_enabled,
			COALESCE(profile.contact_id, '')
		 FROM modemdeck_auth_sessions AS session
		 JOIN modemdeck_users AS user
			ON user.id = session.user_id AND user.enabled = 1
		 LEFT JOIN modemdeck_user_profile_contacts AS profile
			ON profile.user_id = user.id
		 WHERE session.session_token_digest = ?`,
		digest[:],
	).Scan(
		&sessionDigest,
		&csrfDigest,
		&record.UserID,
		&createdAt,
		&expiresAt,
		&principal.Username,
		&role,
		&mustChange,
		&iosPairing,
		&principal.ProfileContactID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.UserSessionRecord{}, auth.Principal{}, false, nil
	}
	if err != nil {
		return auth.UserSessionRecord{}, auth.Principal{}, false, fmt.Errorf("query user session: %w", err)
	}
	if len(sessionDigest) != len(auth.SessionTokenDigest{}) ||
		len(csrfDigest) != len(auth.CSRFTokenDigest{}) {
		return auth.UserSessionRecord{}, auth.Principal{}, false, ErrInvalidAuthSession
	}
	copy(record.SessionTokenDigest[:], sessionDigest)
	copy(record.CSRFTokenDigest[:], csrfDigest)
	record.CreatedAt = time.Unix(createdAt, 0).UTC()
	record.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	principal.UserID = record.UserID
	principal.Role = auth.Role(role)
	principal.MustChangePassword = mustChange != 0
	principal.IOSPairingEnabled = iosPairing != 0

	rows, err := s.database.QueryContext(
		ctx,
		`SELECT line_id
		 FROM modemdeck_user_lines
		 WHERE user_id = ?
		 ORDER BY line_id`,
		record.UserID,
	)
	if err != nil {
		return auth.UserSessionRecord{}, auth.Principal{}, false, fmt.Errorf("query user line access: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		if err := rows.Scan(&lineID); err != nil {
			return auth.UserSessionRecord{}, auth.Principal{}, false, fmt.Errorf("scan user line access: %w", err)
		}
		principal.AllowedLineIDs = append(principal.AllowedLineIDs, lineID)
	}
	if err := rows.Err(); err != nil {
		return auth.UserSessionRecord{}, auth.Principal{}, false, fmt.Errorf("iterate user line access: %w", err)
	}
	return record, principal, true, nil
}

func (s *Store) ReplaceUserPasswordHashIfCurrentAndRevokeSessions(
	ctx context.Context,
	userID, expectedPasswordHash, replacementPasswordHash string,
) (bool, error) {
	if userID == "" || expectedPasswordHash == "" || replacementPasswordHash == "" {
		return false, fmt.Errorf("replace user password hash: credentials are incomplete")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin user password replacement: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_users
		 SET password_hash = ?,
			must_change_password = 0,
			revision = revision + 1,
			updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND enabled = 1 AND password_hash = ?`,
		replacementPasswordHash,
		userID,
		expectedPasswordHash,
	)
	if err != nil {
		return false, fmt.Errorf("replace user password hash: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read replaced user password count: %w", err)
	}
	if affected == 0 {
		return false, nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		"DELETE FROM modemdeck_auth_sessions WHERE user_id = ?",
		userID,
	); err != nil {
		return false, fmt.Errorf("revoke user sessions: %w", err)
	}
	if userID == auth.InitialAdminUserID {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_admin_credentials
			 SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE singleton = 1`,
			replacementPasswordHash,
		); err != nil {
			return false, fmt.Errorf("synchronize administrator password: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit user password replacement: %w", err)
	}
	return true, nil
}
