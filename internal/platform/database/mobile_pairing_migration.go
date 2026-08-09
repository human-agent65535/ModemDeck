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

var iosPairingDeviceColumns = []struct {
	name       string
	definition string
}{
	{name: "device_name", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "device_model", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "device_model_identifier", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "os_name", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "os_version", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "app_version", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "app_build", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "apns_token", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "voip_token", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "push_environment", definition: "TEXT NOT NULL DEFAULT 'development'"},
	{name: "push_bundle_id", definition: "TEXT NOT NULL DEFAULT ''"},
	{name: "push_updated_at", definition: "DATETIME"},
	{name: "last_seen_at", definition: "DATETIME"},
}

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
	deviceColumnsCurrent := true
	for _, column := range iosPairingDeviceColumns {
		if _, exists := credentialColumns[column.name]; !exists {
			deviceColumnsCurrent = false
			break
		}
	}
	if pairingColumnExists && credentialsExist && activatedColumnExists && deviceColumnsCurrent {
		return false, nil
	}
	if pairingColumnExists && credentialsExist {
		previous := expected
		if !activatedColumnExists {
			previous = schemaWithoutColumn(
				previous,
				iosPairingCredentialsTable,
				iosPairingActivatedColumn,
			)
		}
		for _, column := range iosPairingDeviceColumns {
			if _, exists := credentialColumns[column.name]; !exists {
				previous = schemaWithoutColumn(
					previous,
					iosPairingCredentialsTable,
					column.name,
				)
			}
		}
		if !schemaContains(previous, actual) {
			return false, nil
		}
		transaction, err := database.BeginTx(ctx, nil)
		if err != nil {
			return false, fmt.Errorf(
				"begin iOS pairing device migration: %w",
				err,
			)
		}
		defer transaction.Rollback()
		if !activatedColumnExists {
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
		}
		for _, column := range iosPairingDeviceColumns {
			if _, exists := credentialColumns[column.name]; exists {
				continue
			}
			statement := fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s %s",
				iosPairingCredentialsTable,
				column.name,
				column.definition,
			)
			if _, err := transaction.ExecContext(ctx, statement); err != nil {
				return false, fmt.Errorf(
					"migrate iOS pairing device column %s: %w",
					column.name,
					err,
				)
			}
		}
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf(
				"commit iOS pairing device migration: %w",
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
			device_name TEXT NOT NULL DEFAULT '',
			device_model TEXT NOT NULL DEFAULT '',
			device_model_identifier TEXT NOT NULL DEFAULT '',
			os_name TEXT NOT NULL DEFAULT '',
			os_version TEXT NOT NULL DEFAULT '',
			app_version TEXT NOT NULL DEFAULT '',
			app_build TEXT NOT NULL DEFAULT '',
			apns_token TEXT NOT NULL DEFAULT '',
			voip_token TEXT NOT NULL DEFAULT '',
			push_environment TEXT NOT NULL DEFAULT 'development',
			push_bundle_id TEXT NOT NULL DEFAULT '',
			push_updated_at DATETIME,
			last_seen_at DATETIME,
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
