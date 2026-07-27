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
	WHERE contact_phones.canonical_e164 = %s OR contact_phones.original_number = %s
	ORDER BY contact_phones.is_primary DESC, contacts.id ASC
	LIMIT 1
), '')`

const contactNameForNumberSQL = `COALESCE((
	SELECT contacts.display_name
	FROM contact_phones
	JOIN contacts ON contacts.id = contact_phones.contact_id
	WHERE contact_phones.canonical_e164 = %s OR contact_phones.original_number = %s
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
			` + fmt.Sprintf(contactIDForNumberSQL, "sc.peer", "sc.peer") + `,
			` + fmt.Sprintf(contactNameForNumberSQL, "sc.peer", "sc.peer") + `,
			sc.last_sms_id,
			sc.last_timestamp,
			sc.last_content,
			sc.last_type,
			sc.unread_count
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
				WHERE (contact_phones.canonical_e164 = sc.peer OR contact_phones.original_number = sc.peer)
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
			lastID, lastType, unread                                           sql.NullInt64
			lastTimestamp, lastContent                                         sql.NullString
		)
		if err := rows.Scan(
			&key, &imsi, &iccid, &localPhone, &lineID, &peer, &contactID, &contactName,
			&lastID, &lastTimestamp, &lastContent, &lastType, &unread,
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
		threads = append(threads, thread)
	}
	return threads, rowsError("read message threads", rows.Err())
}

func (s *Store) Messages(ctx context.Context, query MessageQuery) ([]Message, error) {
	limit := boundedLimit(query.Limit)
	statement := `SELECT id, request_id, line_id, endpoint_line_id, endpoint_message_id,
		imsi, iccid, peer, local_phone, sender, recipient,
		content, type, status, state, failure_code, revision, timestamp, created_at
		FROM sms`
	conditions := make([]string, 0, 4)
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
			local, sender, recipient, content, state, failureCode            sql.NullString
			messageType, status, revision                                    sql.NullInt64
			timestamp, createdAt                                             sql.NullString
		)
		if err := rows.Scan(
			&message.ID, &requestID, &lineID, &endpointLineID, &endpointID,
			&imsi, &iccid, &peer, &local, &sender, &recipient,
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
