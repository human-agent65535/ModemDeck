package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

const contactIDForNumberSQL = `COALESCE((
	SELECT contacts.id
	FROM contact_phones
	JOIN contacts ON contacts.id = contact_phones.contact_id
	WHERE contact_phones.canonical_e164 = %s%s
	GROUP BY contact_phones.canonical_e164
	HAVING COUNT(DISTINCT contacts.id) = 1
	ORDER BY contact_phones.is_primary DESC, contacts.id ASC
	LIMIT 1
), '')`

const contactNameForNumberSQL = `COALESCE((
	SELECT contacts.display_name
	FROM contact_phones
	JOIN contacts ON contacts.id = contact_phones.contact_id
	WHERE contact_phones.canonical_e164 = %s%s
	GROUP BY contact_phones.canonical_e164
	HAVING COUNT(DISTINCT contacts.id) = 1
	ORDER BY contact_phones.is_primary DESC, contacts.id ASC
	LIMIT 1
), '')`

func (s *Store) MessageThreads(ctx context.Context, query ThreadQuery) ([]MessageThread, error) {
	limit := boundedLimit(query.Limit)
	contactOwner := contactOwnerSQL(ctx, "contacts")
	stateJoin := ""
	unreadExpression := "sc.unread_count"
	markedUnreadExpression := "sc.marked_unread"
	favoriteExpression := "sc.is_favorite"
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		userID := strings.ReplaceAll(principal.UserID, "'", "''")
		stateJoin = ` LEFT JOIN modemdeck_user_message_thread_state AS user_state
			ON user_state.user_id = '` + userID + `'
			AND user_state.line_id = sc.line_id
			AND user_state.peer = sc.peer`
		unreadExpression = `(
			SELECT COUNT(*)
			FROM sms AS unread_message
			WHERE unread_message.line_id = sc.line_id
				AND unread_message.peer = sc.peer
				AND unread_message.type = 1
				AND unread_message.deleted_at IS NULL
				AND unread_message.id > COALESCE(user_state.last_read_sms_id, 0)
		)`
		markedUnreadExpression = "COALESCE(user_state.marked_unread, 0)"
		favoriteExpression = "COALESCE(user_state.is_favorite, 0)"
	}
	statement := `SELECT
			sc.line_id || '|' || sc.peer,
			sc.imsi,
			sc.iccid,
			COALESCE((
				SELECT sms.local_phone FROM sms
				WHERE sms.id = sc.last_sms_id
				LIMIT 1
			), '') AS local_phone,
			sc.line_id,
			sc.peer,
			` + fmt.Sprintf(contactIDForNumberSQL, "sc.peer", contactOwner) + `,
			` + fmt.Sprintf(contactNameForNumberSQL, "sc.peer", contactOwner) + `,
			sc.last_sms_id,
			sc.last_timestamp,
			sc.last_content,
			sc.last_type,
			` + unreadExpression + `,
			` + markedUnreadExpression + `,
			` + favoriteExpression + `
		FROM sms_contacts sc` + stateJoin
	arguments := []any{}
	conditions := make([]string, 0, 2)
	if condition, values, scoped := principalLineScope(ctx, "sc.line_id"); scoped {
		conditions = append(conditions, condition)
		arguments = append(arguments, values...)
	}
	if strings.TrimSpace(query.Search) != "" {
		pattern := searchPattern(query.Search)
		searchCondition := `(
			LOWER(COALESCE(sc.peer, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(sc.last_content, '')) LIKE ? ESCAPE '\' OR
			EXISTS (
				SELECT 1 FROM contact_phones
				JOIN contacts ON contacts.id = contact_phones.contact_id
				WHERE contact_phones.canonical_e164 = sc.peer
				` + contactOwnerSQL(ctx, "contacts") + `
				AND LOWER(COALESCE(contacts.display_name, '')) LIKE ? ESCAPE '\'
			)
		)`
		conditions = append(conditions, searchCondition)
		arguments = append(arguments, pattern, pattern, pattern)
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += ` ORDER BY sc.last_timestamp DESC, sc.last_sms_id DESC, sc.peer ASC LIMIT ?`
	arguments = append(arguments, limit)

	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query message threads: %w", err)
	}
	defer rows.Close()

	threads := make([]MessageThread, 0)
	for rows.Next() {
		var (
			thread                                                             MessageThread
			key, imsi, iccid, localPhone, lineID, peer, contactID, contactName sql.NullString
			lastID, lastType, unread, markedUnread, favorite                   sql.NullInt64
			lastTimestamp, lastContent                                         sql.NullString
		)
		if err := rows.Scan(
			&key, &imsi, &iccid, &localPhone, &lineID, &peer, &contactID, &contactName,
			&lastID, &lastTimestamp, &lastContent, &lastType, &unread,
			&markedUnread, &favorite,
		); err != nil {
			return nil, fmt.Errorf("scan message thread: %w", err)
		}
		thread.Key = stringValue(key)
		thread.IMSI = stringValue(imsi)
		thread.ICCID = stringValue(iccid)
		thread.LocalPhone = stringValue(localPhone)
		thread.LineID = stringValue(lineID)
		thread.Peer = stringValue(peer)
		thread.ContactID = stringValue(contactID)
		thread.ContactName = stringValue(contactName)
		thread.LastMessageID = intValue(lastID)
		thread.LastTimestamp = stringValue(lastTimestamp)
		thread.LastContent = stringValue(lastContent)
		thread.LastType = intValue(lastType)
		thread.UnreadCount = intValue(unread)
		thread.MarkedUnread = boolValue(markedUnread)
		thread.Favorite = boolValue(favorite)
		threads = append(threads, thread)
	}
	return threads, rowsError("read message threads", rows.Err())
}

func (s *Store) Messages(ctx context.Context, query MessageQuery) ([]Message, error) {
	limit := boundedLimit(query.Limit)
	statement := `SELECT id, request_id, line_id, endpoint_line_id, endpoint_message_id,
		imsi, iccid, peer, reported_peer, local_phone, sender, recipient,
		content, type, status, state, delivery_status, message_reference,
		delivery_report_requested, delivery_report_trackable, delivery_report_code,
		failure_code, revision, timestamp, created_at
		FROM sms`
	conditions := []string{"deleted_at IS NULL"}
	arguments := make([]any, 0, 5)
	if condition, values, scoped := principalLineScope(ctx, "line_id"); scoped {
		conditions = append(conditions, condition)
		arguments = append(arguments, values...)
	}
	if lineID := strings.TrimSpace(query.LineID); lineID != "" {
		conditions = append(conditions, "line_id = ?")
		arguments = append(arguments, lineID)
	}
	if len(query.LineIDs) > 0 {
		lineIDs := uniqueNonEmptyStrings(query.LineIDs)
		if len(lineIDs) == 0 {
			return []Message{}, nil
		}
		if len(lineIDs) > MaxQueryLimit {
			return nil, fmt.Errorf("query messages: too many line IDs")
		}
		conditions = append(conditions, "line_id IN ("+placeholders(len(lineIDs))+")")
		for _, lineID := range lineIDs {
			arguments = append(arguments, lineID)
		}
	}
	if peer := strings.TrimSpace(query.Peer); peer != "" {
		conditions = append(conditions, "peer = ?")
		arguments = append(arguments, peer)
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY timestamp DESC, id DESC LIMIT ?"
	arguments = append(arguments, limit)

	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var (
			message                                                          Message
			requestID, lineID, endpointLineID, endpointID, imsi, iccid, peer sql.NullString
			reportedPeer, local, sender, recipient, content, state           sql.NullString
			deliveryStatus, failureCode                                      sql.NullString
			messageType, status, messageReference, reportRequested           sql.NullInt64
			reportTrackable, reportCode, revision                            sql.NullInt64
			timestamp, createdAt                                             sql.NullString
		)
		if err := rows.Scan(
			&message.ID, &requestID, &lineID, &endpointLineID, &endpointID,
			&imsi, &iccid, &peer, &reportedPeer, &local, &sender, &recipient,
			&content, &messageType, &status, &state, &deliveryStatus,
			&messageReference, &reportRequested, &reportTrackable, &reportCode,
			&failureCode, &revision,
			&timestamp, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		message.RequestID = stringValue(requestID)
		message.LineID = stringValue(lineID)
		message.EndpointLineID = stringValue(endpointLineID)
		message.EndpointMessageID = stringValue(endpointID)
		message.IMSI = stringValue(imsi)
		message.ICCID = stringValue(iccid)
		message.Peer = stringValue(peer)
		message.ReportedPeer = stringValue(reportedPeer)
		message.LocalPhone = stringValue(local)
		message.Sender = stringValue(sender)
		message.Recipient = stringValue(recipient)
		message.Content = stringValue(content)
		message.Type = intValue(messageType)
		switch message.Type {
		case 1:
			message.Direction = "incoming"
		case 2:
			message.Direction = "outgoing"
		}
		message.Status = intValue(status)
		message.State = stringValue(state)
		message.DeliveryStatus = MessageDeliveryStatus(stringValue(deliveryStatus))
		message.MessageReference = nullableIntValue(messageReference)
		message.DeliveryReportRequested = boolValue(reportRequested)
		message.DeliveryReportTrackable = boolValue(reportTrackable)
		message.DeliveryReportCode = nullableIntValue(reportCode)
		message.FailureCode = stringValue(failureCode)
		message.Revision = intValue(revision)
		message.Timestamp = stringValue(timestamp)
		message.CreatedAt = stringValue(createdAt)
		messages = append(messages, message)
	}
	if err := rowsError("read messages", rows.Err()); err != nil {
		return nil, err
	}
	if query.Chronological {
		for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
			messages[left], messages[right] = messages[right], messages[left]
		}
	}
	return messages, nil
}

func (s *Store) DeleteMessageThread(
	ctx context.Context,
	identity MessageThreadIdentity,
) error {
	return s.UpdateMessageThreads(ctx, []MessageThreadIdentity{identity}, MessageThreadDelete)
}

func (s *Store) UpdateMessageThreads(
	ctx context.Context,
	identities []MessageThreadIdentity,
	action MessageThreadAction,
) error {
	normalized, err := normalizeMessageThreadIdentities(identities)
	if err != nil {
		return err
	}
	switch action {
	case MessageThreadMarkRead,
		MessageThreadMarkUnread,
		MessageThreadFavorite,
		MessageThreadUnfavorite,
		MessageThreadDelete:
	default:
		return fmt.Errorf("update message threads: unsupported action %q", action)
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin message thread update: %w", err)
	}
	defer transaction.Rollback()

	for _, identity := range normalized {
		if !principalCanAccessLine(ctx, identity.LineID) {
			return ErrMessageThreadNotFound
		}
		var exists int
		if err := transaction.QueryRowContext(
			ctx,
			`SELECT EXISTS(
				SELECT 1 FROM sms_contacts WHERE line_id = ? AND peer = ?
			 )`,
			identity.LineID,
			identity.Peer,
		).Scan(&exists); err != nil {
			return fmt.Errorf("inspect message thread update: %w", err)
		}
		if exists == 0 {
			return ErrMessageThreadNotFound
		}
		if principal, scoped := auth.PrincipalFromContext(ctx); scoped &&
			action != MessageThreadDelete {
			if err := updateUserMessageThreadState(
				ctx,
				transaction,
				principal.UserID,
				identity,
				action,
			); err != nil {
				return err
			}
			continue
		}
		var statement string
		switch action {
		case MessageThreadMarkRead:
			statement = `UPDATE sms_contacts
				SET unread_count = 0, marked_unread = 0, updated_at = CURRENT_TIMESTAMP
				WHERE line_id = ? AND peer = ?`
		case MessageThreadMarkUnread:
			statement = `UPDATE sms_contacts
				SET marked_unread = 1, updated_at = CURRENT_TIMESTAMP
				WHERE line_id = ? AND peer = ?`
		case MessageThreadFavorite:
			statement = `UPDATE sms_contacts
				SET is_favorite = 1, updated_at = CURRENT_TIMESTAMP
				WHERE line_id = ? AND peer = ?`
		case MessageThreadUnfavorite:
			statement = `UPDATE sms_contacts
				SET is_favorite = 0, updated_at = CURRENT_TIMESTAMP
				WHERE line_id = ? AND peer = ?`
		case MessageThreadDelete:
			statement = `DELETE FROM sms_contacts WHERE line_id = ? AND peer = ?`
		}
		if _, err := transaction.ExecContext(
			ctx,
			statement,
			identity.LineID,
			identity.Peer,
		); err != nil {
			return fmt.Errorf("apply message thread action %q: %w", action, err)
		}
		if action == MessageThreadDelete {
			if _, err := transaction.ExecContext(
				ctx,
				`UPDATE sms
				 SET deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP)
				 WHERE line_id = ? AND peer = ? AND deleted_at IS NULL`,
				identity.LineID,
				identity.Peer,
			); err != nil {
				return fmt.Errorf("soft-delete message thread: %w", err)
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit message thread update: %w", err)
	}
	return nil
}

func updateUserMessageThreadState(
	ctx context.Context,
	transaction *sql.Tx,
	userID string,
	identity MessageThreadIdentity,
	action MessageThreadAction,
) error {
	// last_read_sms_id is an ingestion watermark, not the ID of the message
	// displayed last. sms_contacts.last_sms_id follows message timestamps, which
	// can be out of order when a modem imports historical messages.
	if action == MessageThreadMarkRead {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO modemdeck_user_message_thread_state (
				user_id, line_id, peer, last_read_sms_id, marked_unread,
				is_favorite, updated_at
			)
			SELECT
				?,
				thread.line_id,
				thread.peer,
				COALESCE((
					SELECT MAX(message.id)
					FROM sms AS message
					WHERE message.line_id = thread.line_id
						AND message.peer = thread.peer
				), 0),
				0,
				0,
				CURRENT_TIMESTAMP
			FROM sms_contacts AS thread
			WHERE thread.line_id = ? AND thread.peer = ?
			ON CONFLICT(user_id, line_id, peer) DO UPDATE SET
				last_read_sms_id = MAX(
					modemdeck_user_message_thread_state.last_read_sms_id,
					excluded.last_read_sms_id
				),
				marked_unread = 0,
				updated_at = CURRENT_TIMESTAMP
		`, userID, identity.LineID, identity.Peer); err != nil {
			return fmt.Errorf("mark user message thread read: %w", err)
		}
		return nil
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO modemdeck_user_message_thread_state (
			user_id, line_id, peer, last_read_sms_id, marked_unread,
			is_favorite, updated_at
		)
		SELECT
			?,
			thread.line_id,
			thread.peer,
			COALESCE((
				SELECT MAX(message.id)
				FROM sms AS message
				WHERE message.line_id = thread.line_id
					AND message.peer = thread.peer
			), 0),
			0,
			0,
			CURRENT_TIMESTAMP
		FROM sms_contacts AS thread
		WHERE thread.line_id = ? AND thread.peer = ?
		ON CONFLICT(user_id, line_id, peer) DO NOTHING
	`, userID, identity.LineID, identity.Peer); err != nil {
		return fmt.Errorf("initialize user message thread state: %w", err)
	}
	var statement string
	switch action {
	case MessageThreadMarkUnread:
		statement = `UPDATE modemdeck_user_message_thread_state
			SET marked_unread = 1, updated_at = CURRENT_TIMESTAMP
			WHERE user_id = ? AND line_id = ? AND peer = ?`
	case MessageThreadFavorite:
		statement = `UPDATE modemdeck_user_message_thread_state
			SET is_favorite = 1, updated_at = CURRENT_TIMESTAMP
			WHERE user_id = ? AND line_id = ? AND peer = ?`
	case MessageThreadUnfavorite:
		statement = `UPDATE modemdeck_user_message_thread_state
			SET is_favorite = 0, updated_at = CURRENT_TIMESTAMP
			WHERE user_id = ? AND line_id = ? AND peer = ?`
	default:
		return fmt.Errorf("update user message thread state: unsupported action %q", action)
	}
	if _, err := transaction.ExecContext(
		ctx,
		statement,
		userID,
		identity.LineID,
		identity.Peer,
	); err != nil {
		return fmt.Errorf("update user message thread state: %w", err)
	}
	return nil
}

func normalizeMessageThreadIdentities(
	identities []MessageThreadIdentity,
) ([]MessageThreadIdentity, error) {
	if len(identities) == 0 || len(identities) > 100 {
		return nil, fmt.Errorf("update message threads: between 1 and 100 threads are required")
	}
	result := make([]MessageThreadIdentity, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		identity.LineID = strings.TrimSpace(identity.LineID)
		identity.Peer = strings.TrimSpace(identity.Peer)
		if identity.LineID == "" || identity.Peer == "" {
			return nil, fmt.Errorf("update message threads: line ID and peer are required")
		}
		key := identity.LineID + "\x00" + identity.Peer
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, identity)
	}
	return result, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
