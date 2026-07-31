package database

import (
	"context"
	"database/sql"
	"fmt"
)

const initialAdminUserID = "user_admin"

func migrateMultiUserSchema(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	if _, exists := actual.tables["modemdeck_users"]; exists {
		return false, nil
	}
	for _, table := range []string{
		"modemdeck_admin_credentials",
		"modemdeck_auth_sessions",
		"contacts",
		"sms_contacts",
		"call_history",
		"modemdeck_call_recording_state",
		"modemdeck_lines",
		"modemdeck_line_settings",
		"modemdeck_telegram_units",
		"modemdeck_telegram_line_scopes",
	} {
		if _, exists := actual.tables[table]; !exists {
			return false, nil
		}
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin multi-user schema migration: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, `
		CREATE TABLE modemdeck_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL COLLATE NOCASE UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
			enabled NUMERIC NOT NULL DEFAULT 1,
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled
		)
		SELECT ?, username, password_hash, 'admin', 1
		FROM modemdeck_admin_credentials
		WHERE singleton = 1;
		CREATE UNIQUE INDEX ux_modemdeck_single_admin
			ON modemdeck_users(role) WHERE role = 'admin';

		DELETE FROM modemdeck_auth_sessions;
		ALTER TABLE modemdeck_auth_sessions
			ADD COLUMN user_id TEXT NOT NULL DEFAULT 'user_admin';

		ALTER TABLE contacts
			ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT 'user_admin';
		CREATE INDEX idx_contacts_owner_display_name
			ON contacts(owner_user_id, display_name);

		CREATE TABLE modemdeck_user_profile_contacts (
			user_id TEXT PRIMARY KEY,
			contact_id TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE,
			FOREIGN KEY (contact_id) REFERENCES contacts(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);

		CREATE TABLE modemdeck_user_lines (
			user_id TEXT NOT NULL,
			line_id TEXT NOT NULL,
			assigned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, line_id),
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE,
			FOREIGN KEY (line_id) REFERENCES modemdeck_lines(line_id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);
		CREATE INDEX idx_modemdeck_user_lines_line
			ON modemdeck_user_lines(line_id, user_id);

		CREATE TABLE modemdeck_user_preferences (
			user_id TEXT PRIMARY KEY,
			default_line_id TEXT NOT NULL DEFAULT '',
			language TEXT NOT NULL DEFAULT 'auto' CHECK (
				language IN (
					'auto',
					'zh-CN',
					'zh-TW',
					'en-US',
					'ja-JP',
					'vi-VN',
					'es-ES',
					'de-DE',
					'fr-FR',
					'pt-BR'
				)
			),
			language_revision INTEGER NOT NULL DEFAULT 1 CHECK (language_revision > 0),
			recording_default_enabled NUMERIC NOT NULL DEFAULT 0,
			recording_revision INTEGER NOT NULL DEFAULT 1 CHECK (recording_revision > 0),
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);

		CREATE TABLE modemdeck_user_message_thread_state (
			user_id TEXT NOT NULL,
			line_id TEXT NOT NULL,
			peer TEXT NOT NULL,
			last_read_sms_id INTEGER NOT NULL DEFAULT 0,
			marked_unread NUMERIC NOT NULL DEFAULT 0,
			is_favorite NUMERIC NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, line_id, peer),
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);

		CREATE TABLE modemdeck_user_call_state (
			user_id TEXT NOT NULL,
			call_id TEXT NOT NULL,
			is_read NUMERIC NOT NULL DEFAULT 0,
			is_favorite NUMERIC NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, call_id),
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE,
			FOREIGN KEY (call_id) REFERENCES call_history(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);

		CREATE TABLE modemdeck_user_recording_state (
			user_id TEXT NOT NULL,
			call_id TEXT NOT NULL,
			is_favorite NUMERIC NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, call_id),
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE,
			FOREIGN KEY (call_id) REFERENCES call_history(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);

		ALTER TABLE modemdeck_telegram_units
			ADD COLUMN scope_source TEXT NOT NULL DEFAULT 'manual';
		ALTER TABLE modemdeck_telegram_units
			ADD COLUMN assigned_user_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE modemdeck_telegram_units
			ADD COLUMN manual_all_lines NUMERIC NOT NULL DEFAULT 0;
		UPDATE modemdeck_telegram_units
		SET manual_all_lines = NOT EXISTS (
			SELECT 1 FROM modemdeck_telegram_line_scopes AS scope
			WHERE scope.unit_id = modemdeck_telegram_units.id
		);
	`, initialAdminUserID); err != nil {
		return false, fmt.Errorf("create multi-user schema: %w", err)
	}

	var adminExists bool
	if err := transaction.QueryRowContext(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM modemdeck_users WHERE id = ?)",
		initialAdminUserID,
	).Scan(&adminExists); err != nil {
		return false, fmt.Errorf("check migrated administrator: %w", err)
	}
	if adminExists {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO modemdeck_user_lines (user_id, line_id)
			SELECT ?, line_id
			FROM modemdeck_lines
			ORDER BY line_id;

			INSERT INTO modemdeck_user_preferences (
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
					SELECT language
					FROM modemdeck_system_settings
					WHERE singleton = 1
				), 'auto'),
				COALESCE((
					SELECT default_enabled
					FROM modemdeck_recording_settings
					WHERE singleton = 1
				), 0)
			FROM modemdeck_line_settings AS settings
			WHERE settings.singleton = 1;

			INSERT INTO modemdeck_user_message_thread_state (
				user_id, line_id, peer, last_read_sms_id, marked_unread, is_favorite
			)
			SELECT
				?,
				thread.line_id,
				thread.peer,
					CASE
						WHEN thread.unread_count = 0 THEN COALESCE((
							SELECT MAX(message.id)
							FROM sms AS message
							WHERE message.line_id = thread.line_id
								AND message.peer = thread.peer
						), 0)
						ELSE COALESCE((
							SELECT MAX(candidate.id)
							FROM sms AS candidate
							WHERE candidate.line_id = thread.line_id
								AND candidate.peer = thread.peer
								AND candidate.type = 1
								AND candidate.deleted_at IS NULL
								AND (
									SELECT COUNT(*)
									FROM sms AS newer
									WHERE newer.line_id = candidate.line_id
										AND newer.peer = candidate.peer
										AND newer.type = 1
										AND newer.deleted_at IS NULL
										AND newer.id > candidate.id
								) >= thread.unread_count
						), 0)
					END,
				thread.marked_unread,
				thread.is_favorite
			FROM sms_contacts AS thread;

			INSERT INTO modemdeck_user_call_state (
				user_id, call_id, is_read, is_favorite
			)
			SELECT ?, id, read_at IS NOT NULL, is_favorite
			FROM call_history;

			INSERT INTO modemdeck_user_recording_state (
				user_id, call_id, is_favorite
			)
			SELECT ?, call_id, is_favorite
			FROM modemdeck_call_recording_state
			WHERE is_favorite = 1;
		`,
			initialAdminUserID,
			initialAdminUserID,
			initialAdminUserID,
			initialAdminUserID,
			initialAdminUserID,
			initialAdminUserID,
			initialAdminUserID,
			initialAdminUserID,
		); err != nil {
			return false, fmt.Errorf("migrate administrator personal state: %w", err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit multi-user schema migration: %w", err)
	}
	return true, nil
}

func dropDeprecatedPasswordChangeColumn(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	columns, exists := actual.tables["modemdeck_users"]
	if !exists {
		return false, nil
	}
	if _, exists := columns["must_change_password"]; !exists {
		return false, nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin deprecated password column migration: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		"ALTER TABLE modemdeck_users DROP COLUMN must_change_password",
	); err != nil {
		return false, fmt.Errorf("drop deprecated password change column: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit deprecated password column migration: %w", err)
	}
	return true, nil
}

func migrateUserPreferenceRevision(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	columns, exists := actual.tables["modemdeck_user_preferences"]
	if !exists {
		return false, nil
	}
	if _, exists := columns["revision"]; exists {
		return false, nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin user preference revision migration: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		`ALTER TABLE modemdeck_user_preferences
		 ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0)`,
	); err != nil {
		return false, fmt.Errorf("add user preference revision: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO modemdeck_user_lines (user_id, line_id)
		 SELECT ?, line_id
		 FROM modemdeck_lines
		 WHERE EXISTS (
			SELECT 1 FROM modemdeck_users
			WHERE id = ? AND role = 'admin'
		 )`,
		initialAdminUserID,
		initialAdminUserID,
	); err != nil {
		return false, fmt.Errorf("backfill initial administrator lines: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_user_preferences
		 SET default_line_id = CASE
				WHEN EXISTS (
					SELECT 1 FROM modemdeck_user_lines
					WHERE user_id = ? AND line_id = default_line_id
				) THEN default_line_id
				ELSE COALESCE((
					SELECT line_id FROM modemdeck_user_lines
					WHERE user_id = ? ORDER BY line_id LIMIT 1
				), '')
			END,
			updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ?`,
		initialAdminUserID,
		initialAdminUserID,
		initialAdminUserID,
	); err != nil {
		return false, fmt.Errorf("backfill initial administrator default line: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit user preference revision migration: %w", err)
	}
	return true, nil
}

func migrateUserPreferenceValues(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	columns, exists := actual.tables["modemdeck_user_preferences"]
	if !exists {
		return false, nil
	}
	_, languageExists := columns["language"]
	_, languageRevisionExists := columns["language_revision"]
	_, recordingExists := columns["recording_default_enabled"]
	_, recordingRevisionExists := columns["recording_revision"]
	if languageExists &&
		languageRevisionExists &&
		recordingExists &&
		recordingRevisionExists {
		return false, nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin user preference values migration: %w", err)
	}
	defer transaction.Rollback()
	if !languageExists {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_user_preferences
			 ADD COLUMN language TEXT NOT NULL DEFAULT 'auto' CHECK (
				language IN (
					'auto',
					'zh-CN',
					'zh-TW',
					'en-US',
					'ja-JP',
					'vi-VN',
					'es-ES',
					'de-DE',
					'fr-FR',
					'pt-BR'
				)
			 )`,
		); err != nil {
			return false, fmt.Errorf("add user language preference: %w", err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_user_preferences
			 SET language = COALESCE((
				SELECT language FROM modemdeck_system_settings
				WHERE singleton = 1
			 ), 'auto')`,
		); err != nil {
			return false, fmt.Errorf("backfill user language preference: %w", err)
		}
	}
	if !languageRevisionExists {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_user_preferences
			 ADD COLUMN language_revision INTEGER NOT NULL DEFAULT 1
			 CHECK (language_revision > 0)`,
		); err != nil {
			return false, fmt.Errorf("add user language preference revision: %w", err)
		}
	}
	if !recordingExists {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_user_preferences
			 ADD COLUMN recording_default_enabled NUMERIC NOT NULL DEFAULT 0`,
		); err != nil {
			return false, fmt.Errorf("add user recording preference: %w", err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_user_preferences
			 SET recording_default_enabled = COALESCE((
				SELECT default_enabled FROM modemdeck_recording_settings
				WHERE singleton = 1
			 ), 0)`,
		); err != nil {
			return false, fmt.Errorf("backfill user recording preference: %w", err)
		}
	}
	if !recordingRevisionExists {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_user_preferences
			 ADD COLUMN recording_revision INTEGER NOT NULL DEFAULT 1
			 CHECK (recording_revision > 0)`,
		); err != nil {
			return false, fmt.Errorf("add user recording preference revision: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit user preference values migration: %w", err)
	}
	return true, nil
}

func migrateTelegramUserScopeColumns(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	columns, exists := actual.tables["modemdeck_telegram_units"]
	if !exists {
		return false, nil
	}
	_, scopeSourceExists := columns["scope_source"]
	_, assignedUserExists := columns["assigned_user_id"]
	_, manualAllExists := columns["manual_all_lines"]
	if scopeSourceExists && assignedUserExists && manualAllExists {
		return false, nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin Telegram user scope migration: %w", err)
	}
	defer transaction.Rollback()
	if !scopeSourceExists {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_telegram_units
			 ADD COLUMN scope_source TEXT NOT NULL DEFAULT 'manual'`,
		); err != nil {
			return false, fmt.Errorf("add Telegram scope source: %w", err)
		}
	}
	if !assignedUserExists {
		if _, err := transaction.ExecContext(
			ctx,
			`ALTER TABLE modemdeck_telegram_units
			 ADD COLUMN assigned_user_id TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return false, fmt.Errorf("add Telegram assigned user: %w", err)
		}
	}
	if !manualAllExists {
		if _, err := transaction.ExecContext(ctx, `
			ALTER TABLE modemdeck_telegram_units
				ADD COLUMN manual_all_lines NUMERIC NOT NULL DEFAULT 0;
			UPDATE modemdeck_telegram_units
			SET manual_all_lines = NOT EXISTS (
				SELECT 1 FROM modemdeck_telegram_line_scopes AS scope
				WHERE scope.unit_id = modemdeck_telegram_units.id
			)
		`); err != nil {
			return false, fmt.Errorf("add Telegram manual all-lines state: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit Telegram user scope migration: %w", err)
	}
	return true, nil
}

func migrateTelegramOwnership(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) error {
	unitColumns, unitsExist := actual.tables["modemdeck_telegram_units"]
	_, usersExist := actual.tables["modemdeck_users"]
	if !unitsExist || !usersExist {
		return nil
	}
	for _, column := range []string{"scope_source", "assigned_user_id", "manual_all_lines"} {
		if _, exists := unitColumns[column]; !exists {
			return nil
		}
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE modemdeck_telegram_units
		SET
			scope_source = 'user',
			assigned_user_id = CASE
				WHEN EXISTS (
					SELECT 1 FROM modemdeck_users
					WHERE id = modemdeck_telegram_units.assigned_user_id
				) THEN assigned_user_id
				ELSE ?
			END,
			manual_all_lines = 0
		WHERE EXISTS (
			SELECT 1 FROM modemdeck_users WHERE id = ?
		)
			AND (
				scope_source <> 'user'
				OR assigned_user_id = ''
				OR NOT EXISTS (
					SELECT 1 FROM modemdeck_users
					WHERE id = modemdeck_telegram_units.assigned_user_id
				)
				OR manual_all_lines <> 0
			)
	`, initialAdminUserID, initialAdminUserID); err != nil {
		return fmt.Errorf("migrate Telegram bot ownership: %w", err)
	}
	return nil
}
