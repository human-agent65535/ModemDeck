package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	NotificationIncomingSMS = "incoming_sms"
	NotificationMissedCall  = "missed_call"

	NotificationPending       = "pending"
	NotificationSending       = "sending"
	NotificationSent          = "sent"
	NotificationFailed        = "failed"
	NotificationIndeterminate = "indeterminate"
)

type TelegramNotificationDelivery struct {
	EventKey   string
	UnitID     string
	EventType  string
	ResourceID string
	LineID     string
	Peer       string
	Body       string
	OccurredAt time.Time
}

func (s *Store) PendingTelegramNotificationDeliveries(
	ctx context.Context,
	limit int,
) ([]TelegramNotificationDelivery, error) {
	limit = boundedLimit(limit)
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT
			d.event_key, d.unit_id, e.event_type, e.resource_id,
			e.line_id, e.peer, e.body, e.occurred_at
		 FROM modemdeck_notification_deliveries d
		 JOIN modemdeck_notification_events e ON e.event_key = d.event_key
		 WHERE d.status = ?
		 ORDER BY d.created_at ASC, d.event_key ASC, d.unit_id ASC
		 LIMIT ?`,
		NotificationPending,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending Telegram notifications: %w", err)
	}
	defer rows.Close()
	deliveries := make([]TelegramNotificationDelivery, 0)
	for rows.Next() {
		var delivery TelegramNotificationDelivery
		var occurredAt string
		if err := rows.Scan(
			&delivery.EventKey,
			&delivery.UnitID,
			&delivery.EventType,
			&delivery.ResourceID,
			&delivery.LineID,
			&delivery.Peer,
			&delivery.Body,
			&occurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending Telegram notification: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("parse Telegram notification occurrence: %w", err)
		}
		delivery.OccurredAt = parsed.UTC()
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rowsError("read pending Telegram notifications", rows.Err())
}

func (s *Store) MarkSendingTelegramNotificationsIndeterminate(ctx context.Context) error {
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_notification_deliveries
		 SET status = ?, attempt_token = '', last_error_class = 'process_interrupted',
			updated_at = CURRENT_TIMESTAMP
		 WHERE status = ?`,
		NotificationIndeterminate,
		NotificationSending,
	); err != nil {
		return fmt.Errorf("mark interrupted Telegram notifications indeterminate: %w", err)
	}
	return nil
}

func (s *Store) ClaimTelegramNotificationDelivery(
	ctx context.Context,
	eventKey, unitID, attemptToken string,
) (bool, error) {
	eventKey = strings.TrimSpace(eventKey)
	unitID = strings.TrimSpace(unitID)
	attemptToken = strings.TrimSpace(attemptToken)
	if eventKey == "" || unitID == "" || attemptToken == "" {
		return false, fmt.Errorf("claim Telegram notification: identity and attempt token are required")
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_notification_deliveries
		 SET status = ?, attempt_token = ?, last_error_class = '',
			updated_at = CURRENT_TIMESTAMP
		 WHERE event_key = ? AND unit_id = ? AND status = ?`,
		NotificationSending,
		attemptToken,
		eventKey,
		unitID,
		NotificationPending,
	)
	if err != nil {
		return false, fmt.Errorf("claim Telegram notification: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read Telegram notification claim count: %w", err)
	}
	return affected == 1, nil
}

func (s *Store) FinishTelegramNotificationDelivery(
	ctx context.Context,
	eventKey, unitID, attemptToken, status, errorClass string,
) error {
	switch status {
	case NotificationSent, NotificationFailed, NotificationIndeterminate:
	default:
		return fmt.Errorf("finish Telegram notification: invalid status %q", status)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_notification_deliveries
		 SET status = ?, attempt_token = '', last_error_class = ?,
			updated_at = CURRENT_TIMESTAMP
		 WHERE event_key = ? AND unit_id = ? AND status = ? AND attempt_token = ?`,
		status,
		strings.TrimSpace(errorClass),
		strings.TrimSpace(eventKey),
		strings.TrimSpace(unitID),
		NotificationSending,
		strings.TrimSpace(attemptToken),
	)
	if err != nil {
		return fmt.Errorf("finish Telegram notification: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Telegram notification finish count: %w", err)
	}
	if affected != 1 {
		return errors.New("finish Telegram notification: claim no longer owns the delivery")
	}
	return nil
}

func enqueueTelegramNotification(
	ctx context.Context,
	transaction *sql.Tx,
	eventKey, eventType, resourceID, lineID, peer, body string,
	occurredAt time.Time,
) error {
	eventKey = strings.TrimSpace(eventKey)
	resourceID = strings.TrimSpace(resourceID)
	lineID = strings.TrimSpace(lineID)
	if eventKey == "" || resourceID == "" || lineID == "" || occurredAt.IsZero() {
		return fmt.Errorf("enqueue Telegram notification: event identity is incomplete")
	}
	notificationColumn := ""
	switch eventType {
	case NotificationIncomingSMS:
		notificationColumn = "incoming_sms"
	case NotificationMissedCall:
		notificationColumn = "missed_calls"
	default:
		return fmt.Errorf("enqueue Telegram notification: unsupported event type %q", eventType)
	}
	result, err := transaction.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO modemdeck_notification_events (
			event_key, event_type, resource_id, line_id, peer, body,
			occurred_at, created_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		eventKey,
		eventType,
		resourceID,
		lineID,
		strings.TrimSpace(peer),
		strings.TrimSpace(body),
		databaseTime(occurredAt),
	)
	if err != nil {
		return fmt.Errorf("insert Telegram notification event: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Telegram notification event count: %w", err)
	}
	if inserted == 0 {
		return nil
	}
	statement := `INSERT INTO modemdeck_notification_deliveries (
			event_key, unit_id, status, created_at, updated_at
		)
		SELECT ?, units.id, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM modemdeck_telegram_units units
		WHERE units.enabled = 1 AND units.` + notificationColumn + ` = 1
		AND (
			(
				units.scope_source = 'manual'
				AND (
					units.manual_all_lines = 1
					OR EXISTS (
						SELECT 1 FROM modemdeck_telegram_line_scopes scopes
						WHERE scopes.unit_id = units.id AND scopes.line_id = ?
					)
				)
			)
			OR (
				units.scope_source = 'user'
				AND EXISTS (
					SELECT 1
					FROM modemdeck_users user
					WHERE user.id = units.assigned_user_id
						AND user.enabled = 1
						AND EXISTS (
							SELECT 1 FROM modemdeck_user_lines access
							WHERE access.user_id = user.id
								AND access.line_id = ?
						)
						AND (
							NOT EXISTS (
								SELECT 1 FROM modemdeck_telegram_line_scopes scopes
								WHERE scopes.unit_id = units.id
							)
							OR EXISTS (
								SELECT 1 FROM modemdeck_telegram_line_scopes scopes
								WHERE scopes.unit_id = units.id
									AND scopes.line_id = ?
							)
						)
				)
			)
		)`
	if _, err := transaction.ExecContext(
		ctx,
		statement,
		eventKey,
		NotificationPending,
		lineID,
		lineID,
		lineID,
	); err != nil {
		return fmt.Errorf("allocate Telegram notification deliveries: %w", err)
	}
	return nil
}
