package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	_, hasLastSeenAt := columns["last_seen_at_unix"]
	_, hasAccessIP := columns["access_ip"]
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
			last_seen_at_unix INTEGER NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			access_ip TEXT NOT NULL DEFAULT '',
			access_host TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);
	`); err != nil {
		return false, fmt.Errorf("migrate persistent authentication sessions: %w", err)
	}

	insertColumns := []string{
		"session_token_digest",
		"csrf_token_digest",
		"user_id",
		"created_at_unix",
		"last_seen_at_unix",
		"user_agent",
		"access_ip",
		"access_host",
	}
	selectExpressions := []string{
		"session_token_digest",
		"csrf_token_digest",
		"user_id",
		"created_at_unix",
	}
	if hasLastSeenAt {
		selectExpressions = append(selectExpressions, "COALESCE(last_seen_at_unix, created_at_unix)")
	} else {
		selectExpressions = append(selectExpressions, "created_at_unix")
	}
	if hasUserAgent {
		selectExpressions = append(selectExpressions, "COALESCE(user_agent, '')")
	} else {
		selectExpressions = append(selectExpressions, "''")
	}
	if hasAccessIP {
		selectExpressions = append(selectExpressions, "COALESCE(access_ip, '')")
	} else {
		selectExpressions = append(selectExpressions, "''")
	}
	if hasAccessHost {
		selectExpressions = append(selectExpressions, "COALESCE(access_host, '')")
	} else {
		selectExpressions = append(selectExpressions, "''")
	}
	insertStatement := fmt.Sprintf(
		`INSERT INTO modemdeck_auth_sessions (%s)
		 SELECT %s
		 FROM modemdeck_auth_sessions_expiring
		 WHERE expires_at_unix > unixepoch()`,
		strings.Join(insertColumns, ", "),
		strings.Join(selectExpressions, ", "),
	)
	if _, err := transaction.ExecContext(ctx, insertStatement); err != nil {
		return false, fmt.Errorf("copy persistent authentication sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
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
		return false, fmt.Errorf("finish persistent authentication session migration: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit persistent authentication session migration: %w", err)
	}
	return true, nil
}

// migrateCurrentAuthSessionMetadata adds fields introduced after persistent
// sessions were established. Existing sessions retain their creation time;
// their latest metadata is filled on the next authenticated request.
func migrateCurrentAuthSessionMetadata(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	columns, exists := actual.tables["modemdeck_auth_sessions"]
	if !exists {
		return false, nil
	}
	missing := make([]string, 0, 4)
	for _, column := range []string{
		"last_seen_at_unix",
		"user_agent",
		"access_ip",
		"access_host",
	} {
		if _, found := columns[column]; !found {
			missing = append(missing, column)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	expectedColumns, expectedTableExists := expected.tables["modemdeck_auth_sessions"]
	if !expectedTableExists {
		return false, nil
	}
	missingColumns := make(map[string]struct{}, len(missing))
	for _, column := range missing {
		missingColumns[column] = struct{}{}
	}
	for column := range expectedColumns {
		if _, found := columns[column]; found {
			continue
		}
		if _, supported := missingColumns[column]; !supported {
			return false, nil
		}
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin authentication session metadata migration: %w", err)
	}
	defer transaction.Rollback()
	for _, column := range missing {
		definition := "TEXT NOT NULL DEFAULT ''"
		if column == "last_seen_at_unix" {
			definition = "INTEGER NOT NULL DEFAULT 0"
		}
		if _, err := transaction.ExecContext(
			ctx,
			"ALTER TABLE modemdeck_auth_sessions ADD COLUMN "+
				quoteIdentifier(column)+" "+definition,
		); err != nil {
			return false, fmt.Errorf("migrate authentication session %s: %w", column, err)
		}
	}
	if _, found := columns["last_seen_at_unix"]; !found {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_auth_sessions
			 SET last_seen_at_unix = created_at_unix
			 WHERE last_seen_at_unix = 0`,
		); err != nil {
			return false, fmt.Errorf("initialize authentication session last-seen time: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit authentication session metadata migration: %w", err)
	}
	return true, nil
}
