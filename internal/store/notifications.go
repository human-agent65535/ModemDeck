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
	NotificationIncomingSMS  = "incoming_sms"
	NotificationIncomingCall = "incoming_call"
	NotificationMissedCall   = "missed_call"
	NotificationBadgeSync    = "badge_sync"

	NotificationPending       = "pending"
	NotificationSending       = "sending"
	NotificationSent          = "sent"
	NotificationFailed        = "failed"
	NotificationIndeterminate = "indeterminate"
	NotificationAccepted      = "accepted"
	NotificationCancelled     = "cancelled"
	NotificationExpired       = "expired"
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

type ApplePushDelivery struct {
	EventKey     string
	CredentialID string
	UserID       string
	TokenKind    IOSPushTokenKind
	DeviceToken  string
	Environment  string
	BundleID     string
	EventType    string
	ResourceID   string
	LineID       string
	Peer         string
	Body         string
	OccurredAt   time.Time
	ExpiresAt    time.Time
	AttemptCount int
	Eligible     bool
	ResourceLive bool
}

func (s *Store) PendingApplePushDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]ApplePushDelivery, error) {
	limit = boundedLimit(limit)
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT
			d.event_key, d.credential_id, credential.user_id, d.token_kind,
			CASE d.token_kind
				WHEN 'voip' THEN credential.voip_token
				ELSE credential.apns_token
			END,
			credential.push_environment, credential.push_bundle_id,
			e.event_type, e.resource_id, e.line_id, e.peer, e.body, e.occurred_at,
			d.expires_at, d.attempt_count,
			CASE WHEN
				credential.activated_at IS NOT NULL
				AND user.enabled = 1
				AND user.ios_pairing_enabled = 1
				AND CASE d.token_kind
					WHEN 'voip' THEN credential.voip_token
					ELSE credential.apns_token
				END <> ''
					AND (
						e.event_type = 'badge_sync'
						OR EXISTS (
							SELECT 1 FROM modemdeck_user_lines access
							WHERE access.user_id = user.id AND access.line_id = e.line_id
						)
					)
			THEN 1 ELSE 0 END,
			CASE WHEN e.event_type <> 'incoming_call' OR EXISTS (
				SELECT 1 FROM call_history call
				WHERE call.id = e.resource_id AND call.phase = 'ringing'
			) THEN 1 ELSE 0 END
		 FROM modemdeck_apple_push_deliveries d
		 JOIN modemdeck_notification_events e ON e.event_key = d.event_key
		 JOIN modemdeck_ios_pairing_credentials credential
			ON credential.id = d.credential_id
		 JOIN modemdeck_users user ON user.id = credential.user_id
		 WHERE d.status = ? AND julianday(d.next_attempt_at) <= julianday(?)
		 ORDER BY d.created_at ASC, d.event_key ASC, d.credential_id ASC
		 LIMIT ?`,
		NotificationPending,
		databaseTime(now),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending Apple push deliveries: %w", err)
	}
	defer rows.Close()
	deliveries := make([]ApplePushDelivery, 0)
	for rows.Next() {
		var delivery ApplePushDelivery
		var occurredAt, expiresAt string
		if err := rows.Scan(
			&delivery.EventKey,
			&delivery.CredentialID,
			&delivery.UserID,
			&delivery.TokenKind,
			&delivery.DeviceToken,
			&delivery.Environment,
			&delivery.BundleID,
			&delivery.EventType,
			&delivery.ResourceID,
			&delivery.LineID,
			&delivery.Peer,
			&delivery.Body,
			&occurredAt,
			&expiresAt,
			&delivery.AttemptCount,
			&delivery.Eligible,
			&delivery.ResourceLive,
		); err != nil {
			return nil, fmt.Errorf("scan pending Apple push delivery: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("parse Apple push occurrence: %w", err)
		}
		delivery.OccurredAt = parsed.UTC()
		parsed, err = time.Parse(time.RFC3339Nano, expiresAt)
		if err != nil {
			return nil, fmt.Errorf("parse Apple push expiry: %w", err)
		}
		delivery.ExpiresAt = parsed.UTC()
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rowsError("read pending Apple push deliveries", rows.Err())
}

func (s *Store) EnqueueAppleBadgeSync(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("enqueue Apple badge sync: user ID is required")
	}
	now := time.Now().UTC()
	eventKey := fmt.Sprintf("badge:%s:%d", userID, now.UnixNano())
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Apple badge sync: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, `
		UPDATE modemdeck_apple_push_deliveries
		SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE status = ? AND event_key IN (
			SELECT event_key FROM modemdeck_notification_events
			WHERE event_type = ? AND resource_id = ?
		)
	`, NotificationCancelled, NotificationPending, NotificationBadgeSync, userID); err != nil {
		return fmt.Errorf("cancel stale Apple badge sync: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO modemdeck_notification_events (
			event_key, event_type, resource_id, line_id, peer, body,
			occurred_at, created_at
		) VALUES (?, ?, ?, '', '', '', ?, CURRENT_TIMESTAMP)
	`, eventKey, NotificationBadgeSync, userID, databaseTime(now)); err != nil {
		return fmt.Errorf("insert Apple badge sync event: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO modemdeck_apple_push_deliveries (
			event_key, credential_id, token_kind, status,
			attempt_count, next_attempt_at, expires_at, created_at, updated_at
		)
		SELECT ?, credential.id, ?, ?, 0, ?, ?,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM modemdeck_ios_pairing_credentials AS credential
		JOIN modemdeck_users AS user
			ON user.id = credential.user_id
			AND user.enabled = 1
			AND user.ios_pairing_enabled = 1
		WHERE credential.user_id = ?
			AND credential.activated_at IS NOT NULL
			AND credential.apns_token <> ''
	`,
		eventKey,
		IOSPushTokenAPNS,
		NotificationPending,
		databaseTime(now),
		databaseTime(now.Add(10*time.Minute)),
		userID,
	); err != nil {
		return fmt.Errorf("allocate Apple badge sync deliveries: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Apple badge sync: %w", err)
	}
	return nil
}

func (s *Store) NextApplePushDeliveryAttempt(
	ctx context.Context,
) (time.Time, bool, error) {
	var raw sql.NullString
	if err := s.database.QueryRowContext(
		ctx,
		`SELECT MIN(next_attempt_at)
		 FROM modemdeck_apple_push_deliveries
		 WHERE status = ?`,
		NotificationPending,
	).Scan(&raw); err != nil {
		return time.Time{}, false, fmt.Errorf("query next Apple push attempt: %w", err)
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return time.Time{}, false, nil
	}
	next, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse next Apple push attempt: %w", err)
	}
	return next.UTC(), true, nil
}

func (s *Store) RequeueSendingApplePushDeliveries(ctx context.Context) error {
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_apple_push_deliveries
		 SET status = ?, attempt_token = '',
			next_attempt_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
			last_error_class = 'process_interrupted',
			updated_at = CURRENT_TIMESTAMP
		 WHERE status = ?`,
		NotificationPending,
		NotificationSending,
	); err != nil {
		return fmt.Errorf("requeue interrupted Apple pushes: %w", err)
	}
	return nil
}

func (s *Store) ClaimApplePushDelivery(
	ctx context.Context,
	eventKey, credentialID string,
	tokenKind IOSPushTokenKind,
	attemptToken string,
) (bool, error) {
	eventKey = strings.TrimSpace(eventKey)
	credentialID = strings.TrimSpace(credentialID)
	attemptToken = strings.TrimSpace(attemptToken)
	if eventKey == "" || credentialID == "" || attemptToken == "" ||
		(tokenKind != IOSPushTokenAPNS && tokenKind != IOSPushTokenVoIP) {
		return false, errors.New("claim Apple push: delivery identity is incomplete")
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_apple_push_deliveries
		 SET status = ?, attempt_token = ?, attempt_count = attempt_count + 1,
			last_error_class = '',
			updated_at = CURRENT_TIMESTAMP
		 WHERE event_key = ? AND credential_id = ? AND token_kind = ?
			AND status = ?`,
		NotificationSending,
		attemptToken,
		eventKey,
		credentialID,
		tokenKind,
		NotificationPending,
	)
	if err != nil {
		return false, fmt.Errorf("claim Apple push: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read Apple push claim count: %w", err)
	}
	return affected == 1, nil
}

func (s *Store) FinishApplePushDelivery(
	ctx context.Context,
	eventKey, credentialID string,
	tokenKind IOSPushTokenKind,
	attemptToken, status, errorClass string,
	nextAttempt time.Time,
) error {
	switch status {
	case NotificationPending, NotificationAccepted, NotificationFailed,
		NotificationCancelled, NotificationExpired:
	default:
		return fmt.Errorf("finish Apple push: invalid status %q", status)
	}
	if status == NotificationPending && nextAttempt.IsZero() {
		return errors.New("finish Apple push: pending delivery requires a next attempt")
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_apple_push_deliveries
		 SET status = ?, attempt_token = '', last_error_class = ?,
			next_attempt_at = CASE WHEN ? = ? THEN ? ELSE next_attempt_at END,
			accepted_at = CASE WHEN ? = ? THEN CURRENT_TIMESTAMP ELSE accepted_at END,
			updated_at = CURRENT_TIMESTAMP
		 WHERE event_key = ? AND credential_id = ? AND token_kind = ?
			AND status = ? AND attempt_token = ?`,
		status,
		strings.TrimSpace(errorClass),
		status,
		NotificationPending,
		databaseTime(nextAttempt),
		status,
		NotificationAccepted,
		strings.TrimSpace(eventKey),
		strings.TrimSpace(credentialID),
		tokenKind,
		NotificationSending,
		strings.TrimSpace(attemptToken),
	)
	if err != nil {
		return fmt.Errorf("finish Apple push: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Apple push finish count: %w", err)
	}
	if affected != 1 {
		return errors.New("finish Apple push: claim no longer owns the delivery")
	}
	return nil
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

func enqueueNotification(
	ctx context.Context,
	transaction *sql.Tx,
	eventKey, eventType, resourceID, lineID, peer, body string,
	occurredAt time.Time,
) error {
	eventKey = strings.TrimSpace(eventKey)
	resourceID = strings.TrimSpace(resourceID)
	lineID = strings.TrimSpace(lineID)
	if eventKey == "" || resourceID == "" || lineID == "" || occurredAt.IsZero() {
		return fmt.Errorf("enqueue notification: event identity is incomplete")
	}
	telegramColumn := ""
	appleTokenKind := IOSPushTokenKind("")
	switch eventType {
	case NotificationIncomingSMS:
		telegramColumn = "incoming_sms"
		appleTokenKind = IOSPushTokenAPNS
	case NotificationIncomingCall:
		appleTokenKind = IOSPushTokenVoIP
	case NotificationMissedCall:
		telegramColumn = "missed_calls"
	default:
		return fmt.Errorf("enqueue notification: unsupported event type %q", eventType)
	}
	normalizedPeer := strings.TrimSpace(peer)
	normalizedBody := strings.TrimSpace(body)
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
		normalizedPeer,
		normalizedBody,
		databaseTime(occurredAt),
	)
	if err != nil {
		return fmt.Errorf("insert notification event: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read notification event insert count: %w", err)
	}
	if inserted == 0 {
		var existingType, existingResource, existingLine, existingPeer, existingBody string
		var existingOccurredAt string
		if err := transaction.QueryRowContext(
			ctx,
			`SELECT event_type, resource_id, line_id, peer, body, occurred_at
			 FROM modemdeck_notification_events WHERE event_key = ?`,
			eventKey,
		).Scan(
			&existingType,
			&existingResource,
			&existingLine,
			&existingPeer,
			&existingBody,
			&existingOccurredAt,
		); err != nil {
			return fmt.Errorf("read existing notification event: %w", err)
		}
		if existingType != eventType || existingResource != resourceID ||
			existingLine != lineID || existingPeer != normalizedPeer ||
			existingBody != normalizedBody ||
			existingOccurredAt != databaseTime(occurredAt) {
			return errors.New("enqueue notification: event key conflicts with existing event")
		}
		return nil
	}
	if telegramColumn != "" {
		statement := `INSERT OR IGNORE INTO modemdeck_notification_deliveries (
			event_key, unit_id, status, created_at, updated_at
		)
		SELECT ?, units.id, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM modemdeck_telegram_units units
		WHERE units.enabled = 1 AND units.` + telegramColumn + ` = 1
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
	}
	if appleTokenKind != "" {
		expiresAt := occurredAt.Add(24 * time.Hour)
		if appleTokenKind == IOSPushTokenVoIP {
			expiresAt = occurredAt.Add(30 * time.Second)
		}
		tokenColumn := "credential.apns_token"
		if appleTokenKind == IOSPushTokenVoIP {
			tokenColumn = "credential.voip_token"
		}
		statement := `INSERT OR IGNORE INTO modemdeck_apple_push_deliveries (
			event_key, credential_id, token_kind, status,
			attempt_count, next_attempt_at, expires_at, created_at, updated_at
		)
		SELECT ?, credential.id, ?, ?, 0, ?, ?,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM modemdeck_ios_pairing_credentials credential
		JOIN modemdeck_users user
			ON user.id = credential.user_id
			AND user.enabled = 1
			AND user.ios_pairing_enabled = 1
		JOIN modemdeck_user_lines access
			ON access.user_id = user.id
		WHERE access.line_id = ?
			AND credential.activated_at IS NOT NULL
			AND ` + tokenColumn + ` <> ''`
		if _, err := transaction.ExecContext(
			ctx,
			statement,
			eventKey,
			appleTokenKind,
			NotificationPending,
			databaseTime(occurredAt),
			databaseTime(expiresAt),
			lineID,
		); err != nil {
			return fmt.Errorf("allocate Apple push deliveries: %w", err)
		}
	}
	return nil
}
