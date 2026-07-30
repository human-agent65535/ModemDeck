package database

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	iosPairingCredentialsTable = "modemdeck_ios_pairing_credentials"
	iosPairingEnabledColumn    = "ios_pairing_enabled"
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
	_, credentialsExist := actual.tables[iosPairingCredentialsTable]
	if pairingColumnExists && credentialsExist {
		return false, nil
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
