package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func ParseCallKind(value string) (CallKind, error) {
	switch CallKind(strings.ToLower(strings.TrimSpace(value))) {
	case "", CallKindAll:
		return CallKindAll, nil
	case CallKindIncoming:
		return CallKindIncoming, nil
	case CallKindOutgoing:
		return CallKindOutgoing, nil
	case CallKindMissed:
		return CallKindMissed, nil
	default:
		return "", ErrInvalidCallKind
	}
}

func (s *Store) Calls(ctx context.Context, query CallQuery) ([]Call, error) {
	kind, err := ParseCallKind(string(query.Kind))
	if err != nil {
		return nil, err
	}
	limit := boundedLimit(query.Limit)
	statement := fmt.Sprintf(`SELECT
		ch.id, ch.request_id, ch.device_id, ch.direction, ch.remote_number,
		%s, %s,
		ch.endpoint_id, ch.endpoint_call_id, ch.phase, ch.revision,
			ch.created_at, ch.updated_at, ch.active_at, ch.ended_at,
			ch.end_reason, ch.failure_code, ch.bearer, ch.state_reason,
			ch.state_reason_code, ch.multiparty, ch.audio_port,
			ch.audio_encoding, ch.audio_resolution, ch.audio_rate,
			ch.media_available
		FROM call_history ch`,
		fmt.Sprintf(contactIDForNumberSQL, "ch.remote_number", "ch.remote_number"),
		fmt.Sprintf(contactNameForNumberSQL, "ch.remote_number", "ch.remote_number"),
	)
	conditions := make([]string, 0, 2)
	arguments := make([]any, 0, 4)
	switch kind {
	case CallKindIncoming:
		conditions = append(conditions, "ch.direction = ?")
		arguments = append(arguments, string(CallKindIncoming))
	case CallKindOutgoing:
		conditions = append(conditions, "ch.direction = ?")
		arguments = append(arguments, string(CallKindOutgoing))
	case CallKindMissed:
		conditions = append(conditions, `ch.direction = 'incoming' AND ch.active_at IS NULL
			AND COALESCE(ch.end_reason, '') <> 'rejected'
			AND COALESCE(ch.failure_code, '') <> 'rejected'`)
	}
	if strings.TrimSpace(query.Search) != "" {
		pattern := searchPattern(query.Search)
		conditions = append(conditions, `(
			LOWER(COALESCE(ch.remote_number, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(ch.device_id, '')) LIKE ? ESCAPE '\' OR
			EXISTS (
				SELECT 1 FROM contact_phones
				JOIN contacts ON contacts.id = contact_phones.contact_id
				WHERE (contact_phones.canonical_e164 = ch.remote_number OR contact_phones.original_number = ch.remote_number)
				AND LOWER(COALESCE(contacts.display_name, '')) LIKE ? ESCAPE '\'
			)
		)`)
		arguments = append(arguments, pattern, pattern, pattern)
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY ch.ended_at DESC, ch.id DESC LIMIT ?"
	arguments = append(arguments, limit)

	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query calls: %w", err)
	}
	defer rows.Close()

	calls := make([]Call, 0)
	for rows.Next() {
		var (
			call                                                                    Call
			requestID, deviceID, direction, remoteNumber, contactID, contactName    sql.NullString
			endpointID, endpointCallID, phase                                       sql.NullString
			revision                                                                sql.NullInt64
			createdAt, updatedAt, activeAt, endedAt, endReason, failureCode, bearer sql.NullString
			stateReason, audioPort, audioEncoding, audioResolution                  sql.NullString
			stateReasonCode, multiparty, audioRate, mediaAvailable                  sql.NullInt64
		)
		if err := rows.Scan(
			&call.ID, &requestID, &deviceID, &direction, &remoteNumber,
			&contactID, &contactName, &endpointID, &endpointCallID, &phase,
			&revision, &createdAt, &updatedAt, &activeAt, &endedAt,
			&endReason, &failureCode, &bearer, &stateReason, &stateReasonCode,
			&multiparty, &audioPort, &audioEncoding, &audioResolution,
			&audioRate, &mediaAvailable,
		); err != nil {
			return nil, fmt.Errorf("scan call: %w", err)
		}
		call.RequestID = stringValue(requestID)
		call.DeviceID = stringValue(deviceID)
		call.Direction = stringValue(direction)
		call.RemoteNumber = stringValue(remoteNumber)
		call.ContactID = stringValue(contactID)
		call.ContactName = stringValue(contactName)
		call.EndpointID = stringValue(endpointID)
		call.EndpointCallID = stringValue(endpointCallID)
		call.Phase = stringValue(phase)
		call.Revision = intValue(revision)
		call.CreatedAt = stringValue(createdAt)
		call.StartedAt = call.CreatedAt
		call.UpdatedAt = stringValue(updatedAt)
		call.ActiveAt = pointerValue(activeAt)
		call.EndedAt = stringValue(endedAt)
		call.EndReason = stringValue(endReason)
		call.FailureCode = stringValue(failureCode)
		call.Bearer = stringValue(bearer)
		call.StateReason = stringValue(stateReason)
		call.StateReasonCode = intValue(stateReasonCode)
		call.Multiparty = boolValue(multiparty)
		call.AudioPort = stringValue(audioPort)
		call.AudioEncoding = stringValue(audioEncoding)
		call.AudioResolution = stringValue(audioResolution)
		if audioRate.Valid && audioRate.Int64 > 0 {
			call.AudioRate = uint32(audioRate.Int64)
		}
		call.MediaAvailable = boolValue(mediaAvailable)
		if call.ActiveAt != nil {
			call.DurationSeconds = durationSeconds(*call.ActiveAt, call.EndedAt)
		}
		call.Missed = call.Direction == string(CallKindIncoming) && call.ActiveAt == nil &&
			call.EndReason != "rejected" && call.FailureCode != "rejected"
		calls = append(calls, call)
	}
	return calls, rowsError("read calls", rows.Err())
}
