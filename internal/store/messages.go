package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const contactIDForNumberSQL = `COALESCE((
	SELECT contacts.id
	FROM contact_phones
	JOIN contacts ON contacts.id = contact_phones.contact_id
	WHERE contact_phones.canonical_e164 = %s
	GROUP BY contact_phones.canonical_e164
	HAVING COUNT(DISTINCT contacts.id) = 1
	ORDER BY contact_phones.is_primary DESC, contacts.id ASC
	LIMIT 1
), '')`

const contactNameForNumberSQL = `COALESCE((
	SELECT contacts.display_name
	FROM contact_phones
	JOIN contacts ON contacts.id = contact_phones.contact_id
	WHERE contact_phones.canonical_e164 = %s
	GROUP BY contact_phones.canonical_e164
	HAVING COUNT(DISTINCT contacts.id) = 1
	ORDER BY contact_phones.is_primary DESC, contacts.id ASC
	LIMIT 1
), '')`

func (s *Store) MessageThreads(ctx context.Context, query ThreadQuery) ([]MessageThread, error) {
	limit := boundedLimit(query.Limit)
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
			` + fmt.Sprintf(contactIDForNumberSQL, "sc.peer") + `,
			` + fmt.Sprintf(contactNameForNumberSQL, "sc.peer") + `,
			sc.last_sms_id,
			sc.last_timestamp,
			sc.last_content,
			sc.last_type,
			sc.unread_count,
			sc.marked_unread,
			sc.is_favorite
		FROM sms_contacts sc`
	arguments := []any{}
	if strings.TrimSpace(query.Search) != "" {
		pattern := searchPattern(query.Search)
		statement += ` WHERE (
			LOWER(COALESCE(sc.peer, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(sc.last_content, '')) LIKE ? ESCAPE '\' OR
			EXISTS (
				SELECT 1 FROM contact_phones
				JOIN contacts ON contacts.id = contact_phones.contact_id
				WHERE contact_phones.canonical_e164 = sc.peer
				AND LOWER(COALESCE(contacts.display_name, '')) LIKE ? ESCAPE '\'
			)
		)`
		arguments = append(arguments, pattern, pattern, pattern)
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
		content, type, status, state, failure_code, revision, timestamp, created_at
		FROM sms`
	conditions := []string{"deleted_at IS NULL"}
	arguments := make([]any, 0, 5)
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
			failureCode                                                      sql.NullString
			messageType, status, revision                                    sql.NullInt64
			timestamp, createdAt                                             sql.NullString
		)
		if err := rows.Scan(
			&message.ID, &requestID, &lineID, &endpointLineID, &endpointID,
			&imsi, &iccid, &peer, &reportedPeer, &local, &sender, &recipient,
			&content, &messageType, &status, &state, &failureCode, &revision,
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
