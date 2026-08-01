package database

import (
	"context"
	"database/sql"
	"fmt"
)

// migratePersistentAuthSessions converts the former expiring Web sessions to
// explicitly revoked, bounded sessions. Already-expired rows stay expired;
// sessions that were valid at migration time become persistent.
func migratePersistentAuthSessions(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	columns, exists := actual.tables["modemdeck_auth_sessions"]
	if !exists {
		return false, nil
	}
	_, hasUserAgent := columns["user_agent"]
	_, hasAccessHost := columns["access_host"]
	_, hasExpiry := columns["expires_at_unix"]
	if hasUserAgent && hasAccessHost && !hasExpiry {
		return false, nil
	}
	for _, required := range []string{
		"session_token_digest",
		"csrf_token_digest",
		"user_id",
		"created_at_unix",
		"expires_at_unix",
	} {
		if _, ok := columns[required]; !ok {
			return false, nil
		}
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin persistent authentication session migration: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, `
		ALTER TABLE modemdeck_auth_sessions
			RENAME TO modemdeck_auth_sessions_expiring;

		CREATE TABLE modemdeck_auth_sessions (
			session_token_digest BLOB PRIMARY KEY
				CHECK (length(session_token_digest) = 32),
			csrf_token_digest BLOB NOT NULL
				CHECK (length(csrf_token_digest) = 32),
			user_id TEXT NOT NULL DEFAULT 'user_admin',
			created_at_unix INTEGER NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			access_host TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);

		INSERT INTO modemdeck_auth_sessions (
			session_token_digest,
			csrf_token_digest,
			user_id,
			created_at_unix
		)
		SELECT
			session_token_digest,
			csrf_token_digest,
			user_id,
			created_at_unix
		FROM modemdeck_auth_sessions_expiring
		WHERE expires_at_unix > unixepoch();

		DROP TABLE modemdeck_auth_sessions_expiring;

		CREATE INDEX idx_modemdeck_auth_sessions_user_created
			ON modemdeck_auth_sessions(user_id, created_at_unix DESC);

		DELETE FROM modemdeck_auth_sessions
		WHERE rowid IN (
			SELECT rowid
			FROM (
				SELECT
					rowid,
					ROW_NUMBER() OVER (
						PARTITION BY user_id
						ORDER BY created_at_unix DESC, rowid DESC
					) AS session_position
				FROM modemdeck_auth_sessions
			)
			WHERE session_position > 8
		);
	`); err != nil {
		return false, fmt.Errorf("migrate persistent authentication sessions: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit persistent authentication session migration: %w", err)
	}
	return true, nil
}
