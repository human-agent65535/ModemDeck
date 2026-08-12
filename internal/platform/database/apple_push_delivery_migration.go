package database

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	applePushDeliveriesTable = "modemdeck_apple_push_deliveries"
	applePushPendingIndex    = "idx_modemdeck_apple_push_pending"
)

func migrateApplePushDeliverySchema(
	ctx context.Context,
	database *sql.DB,
	expected schemaShape,
	actual schemaShape,
) (bool, error) {
	_, tableExists := actual.tables[applePushDeliveriesTable]
	_, indexExists := actual.indexes[applePushPendingIndex]
	if _, credentialsExist := actual.tables[iosPairingCredentialsTable]; !credentialsExist {
		return false, nil
	}
	if tableExists && indexExists {
		return false, nil
	}

	previous := cloneSchemaShape(expected)
	if !tableExists {
		delete(previous.tables, applePushDeliveriesTable)
	}
	if !indexExists {
		delete(previous.indexes, applePushPendingIndex)
	}
	if !schemaContains(previous, actual) {
		return false, nil
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin Apple push delivery migration: %w", err)
	}
	defer transaction.Rollback()
	if !tableExists {
		if _, err := transaction.ExecContext(ctx, `
			CREATE TABLE modemdeck_apple_push_deliveries (
				event_key TEXT NOT NULL,
				credential_id TEXT NOT NULL,
				token_kind TEXT NOT NULL CHECK (token_kind IN ('apns', 'voip')),
				status TEXT NOT NULL DEFAULT 'pending' CHECK (
					status IN ('pending', 'sending', 'accepted', 'failed', 'cancelled', 'expired')
				),
				attempt_token TEXT NOT NULL DEFAULT '',
				attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
				next_attempt_at DATETIME NOT NULL,
				expires_at DATETIME NOT NULL,
				accepted_at DATETIME,
				last_error_class TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (event_key, credential_id, token_kind),
				FOREIGN KEY (event_key)
					REFERENCES modemdeck_notification_events(event_key)
					ON DELETE CASCADE ON UPDATE CASCADE,
				FOREIGN KEY (credential_id)
					REFERENCES modemdeck_ios_pairing_credentials(id)
					ON DELETE CASCADE ON UPDATE CASCADE
			);`,
		); err != nil {
			return false, fmt.Errorf("create Apple push deliveries: %w", err)
		}
	}
	if !indexExists {
		if _, err := transaction.ExecContext(ctx, `
			CREATE INDEX idx_modemdeck_apple_push_pending
				ON modemdeck_apple_push_deliveries(
					status, next_attempt_at, expires_at,
					created_at, event_key, credential_id
				);`,
		); err != nil {
			return false, fmt.Errorf("create Apple push pending index: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit Apple push delivery migration: %w", err)
	}
	return true, nil
}

func dropEmptyOrphanApplePushDeliverySchema(
	ctx context.Context,
	database *sql.DB,
	actual schemaShape,
) (bool, error) {
	_, tableExists := actual.tables[applePushDeliveriesTable]
	_, credentialsExist := actual.tables[iosPairingCredentialsTable]
	if !tableExists || credentialsExist {
		return false, nil
	}
	var count int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM modemdeck_apple_push_deliveries`,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect orphan Apple push deliveries: %w", err)
	}
	if count != 0 {
		return false, nil
	}
	if _, err := database.ExecContext(ctx, `
		DROP INDEX IF EXISTS idx_modemdeck_apple_push_pending;
		DROP TABLE modemdeck_apple_push_deliveries;`,
	); err != nil {
		return false, fmt.Errorf("drop empty orphan Apple push schema: %w", err)
	}
	return true, nil
}

func cloneSchemaShape(current schemaShape) schemaShape {
	result := schemaShape{
		tables:  make(map[string]map[string]struct{}, len(current.tables)),
		indexes: make(map[string]struct{}, len(current.indexes)),
	}
	for table, columns := range current.tables {
		copied := make(map[string]struct{}, len(columns))
		for column := range columns {
			copied[column] = struct{}{}
		}
		result.tables[table] = copied
	}
	for index := range current.indexes {
		result.indexes[index] = struct{}{}
	}
	return result
}
