package database

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	iosPairingCredentialsTable = "modemdeck_ios_pairing_credentials"
	iosPairingEnabledColumn    = "ios_pairing_enabled"
	iosPairingActivatedColumn  = "activated_at"
)

func schemaBeforeMobilePairing(current schemaShape) schemaShape {
	previous := schemaWithoutColumn(
		current,
		"modemdeck_users",
		iosPairingEnabledColumn,
	)
	delete(previous.tables, iosPairingCredentialsTable)
	return previous
}

func migrateMobilePairingSchema(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	userColumns, usersExist := actual.tables["modemdeck_users"]
	_, pairingColumnExists := userColumns[iosPairingEnabledColumn]
	credentialColumns, credentialsExist := actual.tables[iosPairingCredentialsTable]
	_, activatedColumnExists := credentialColumns[iosPairingActivatedColumn]
	if pairingColumnExists && credentialsExist && activatedColumnExists {
		return false, nil
	}
	if pairingColumnExists && credentialsExist && !activatedColumnExists {
		beforeConfirmation := schemaWithoutColumn(
			expected,
			iosPairingCredentialsTable,
			iosPairingActivatedColumn,
		)
		if !schemaContains(beforeConfirmation, actual) {
			return false, nil
		}
		transaction, err := database.BeginTx(ctx, nil)
		if err != nil {
			return false, fmt.Errorf(
				"begin iOS pairing confirmation migration: %w",
				err,
			)
		}
		defer transaction.Rollback()
		if _, err := transaction.ExecContext(ctx, `
			ALTER TABLE modemdeck_ios_pairing_credentials
				ADD COLUMN activated_at DATETIME;
			UPDATE modemdeck_ios_pairing_credentials
			SET activated_at = created_at
			WHERE activated_at IS NULL;
		`); err != nil {
			return false, fmt.Errorf(
				"migrate iOS pairing confirmation: %w",
				err,
			)
		}
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf(
				"commit iOS pairing confirmation migration: %w",
				err,
			)
		}
		return true, nil
	}
	if !usersExist ||
		pairingColumnExists ||
		credentialsExist ||
		!schemaContains(schemaBeforeMobilePairing(expected), actual) {
		return false, nil
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin mobile pairing schema migration: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, `
		ALTER TABLE modemdeck_users
			ADD COLUMN ios_pairing_enabled NUMERIC NOT NULL DEFAULT 0;
		UPDATE modemdeck_users
		SET ios_pairing_enabled = 1
		WHERE role = 'admin';

		CREATE TABLE modemdeck_ios_pairing_credentials (
			user_id TEXT PRIMARY KEY,
			token_digest BLOB NOT NULL CHECK (length(token_digest) = 32),
			activated_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES modemdeck_users(id)
				ON DELETE CASCADE ON UPDATE CASCADE
		);`,
	); err != nil {
		return false, fmt.Errorf("migrate mobile pairing schema: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit mobile pairing schema migration: %w", err)
	}
	return true, nil
}
