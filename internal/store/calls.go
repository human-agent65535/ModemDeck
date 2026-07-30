package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
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

func (s *Store) CallLineID(ctx context.Context, callID string) (string, error) {
	var lineID string
	err := s.database.QueryRowContext(
		ctx,
		"SELECT line_id FROM call_history WHERE id = ?",
		strings.TrimSpace(callID),
	).Scan(&lineID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCallNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query call line: %w", err)
	}
	return lineID, nil
}

func (s *Store) Calls(ctx context.Context, query CallQuery) ([]Call, error) {
	kind, err := ParseCallKind(string(query.Kind))
	if err != nil {
		return nil, err
	}
	limit := queryLimit(query.Limit, query.Lookahead)
	contactOwner := contactOwnerSQL(ctx, "contacts")
	stateJoin := ""
	readExpression := "ch.read_at"
	favoriteExpression := "ch.is_favorite"
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		userID := strings.ReplaceAll(principal.UserID, "'", "''")
		stateJoin = ` LEFT JOIN modemdeck_user_call_state AS user_state
			ON user_state.user_id = '` + userID + `'
			AND user_state.call_id = ch.id`
		readExpression = `CASE
			WHEN COALESCE(user_state.is_read, 0) = 1 THEN ch.updated_at
			ELSE NULL
		END`
		favoriteExpression = "COALESCE(user_state.is_favorite, 0)"
	}
	statement := fmt.Sprintf(`SELECT
		ch.id, ch.request_id, ch.line_id, ch.endpoint_line_id, ch.local_phone, ch.line_imsi,
		ch.line_iccid, ch.direction, ch.remote_number, ch.reported_remote_number,
		%s, %s,
			ch.endpoint_id, ch.endpoint_call_id, ch.phase, ch.revision,
			ch.created_at, ch.updated_at, ch.active_at, ch.ended_at,
			%s, %s, ch.end_reason, ch.failure_code, ch.bearer, ch.state_reason,
			ch.state_reason_code, ch.multiparty, ch.audio_port,
			ch.audio_encoding, ch.audio_resolution, ch.audio_rate,
			ch.media_available, COALESCE(CAST(ch.ended_at AS TEXT), '')
		FROM call_history ch%s`,
		fmt.Sprintf(contactIDForNumberSQL, "ch.remote_number", contactOwner),
		fmt.Sprintf(contactNameForNumberSQL, "ch.remote_number", contactOwner),
		readExpression,
		favoriteExpression,
		stateJoin,
	)
	conditions := make([]string, 0, 3)
	arguments := make([]any, 0, 4)
	if condition, values, scoped := principalLineScope(ctx, "ch.line_id"); scoped {
		conditions = append(conditions, condition)
		arguments = append(arguments, values...)
	}
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
			AND COALESCE(ch.failure_code, '') <> 'rejected'
			AND (
				ch.phase IN ('ended', 'failed') OR
				COALESCE(ch.ended_at, '') <> ''
			)`)
	}
	if strings.TrimSpace(query.Search) != "" {
		pattern := searchPattern(query.Search)
		conditions = append(conditions, `(
			LOWER(COALESCE(ch.remote_number, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(ch.local_phone, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(ch.line_id, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(ch.endpoint_line_id, '')) LIKE ? ESCAPE '\' OR
			EXISTS (
				SELECT 1 FROM contact_phones
				JOIN contacts ON contacts.id = contact_phones.contact_id
				WHERE contact_phones.canonical_e164 = ch.remote_number
				`+contactOwnerSQL(ctx, "contacts")+`
				AND LOWER(COALESCE(contacts.display_name, '')) LIKE ? ESCAPE '\'
			)
		)`)
		arguments = append(arguments, pattern, pattern, pattern, pattern, pattern)
	}
	if query.After != nil {
		conditions = append(conditions, `(
			COALESCE(ch.ended_at, '') < ? OR
			(COALESCE(ch.ended_at, '') = ? AND ch.id < ?)
		)`)
		arguments = append(
			arguments,
			query.After.EndedAt,
			query.After.EndedAt,
			query.After.ID,
		)
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY COALESCE(ch.ended_at, '') DESC, ch.id DESC LIMIT ?"
	arguments = append(arguments, limit)

	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query calls: %w", err)
	}
	defer rows.Close()

	calls := make([]Call, 0)
	for rows.Next() {
		var (
			call                                                               Call
			requestID, lineID, endpointLineID, localPhone, lineIMSI, lineICCID sql.NullString
			direction, remoteNumber, reportedRemoteNumber                      sql.NullString
			contactID, contactName, endpointID, endpointCallID, phase          sql.NullString
			revision, favorite                                                 sql.NullInt64
			createdAt, updatedAt, activeAt, endedAt, readAt, endReason         sql.NullString
			failureCode, bearer                                                sql.NullString
			stateReason, audioPort, audioEncoding, audioResolution             sql.NullString
			sortEndedAt                                                        sql.NullString
			stateReasonCode, multiparty, audioRate, mediaAvailable             sql.NullInt64
		)
		if err := rows.Scan(
			&call.ID, &requestID, &lineID, &endpointLineID, &localPhone, &lineIMSI, &lineICCID,
			&direction, &remoteNumber, &reportedRemoteNumber,
			&contactID, &contactName, &endpointID, &endpointCallID, &phase,
			&revision, &createdAt, &updatedAt, &activeAt, &endedAt, &readAt, &favorite,
			&endReason, &failureCode, &bearer, &stateReason, &stateReasonCode,
			&multiparty, &audioPort, &audioEncoding, &audioResolution,
			&audioRate, &mediaAvailable, &sortEndedAt,
		); err != nil {
			return nil, fmt.Errorf("scan call: %w", err)
		}
		call.RequestID = stringValue(requestID)
		call.LineID = stringValue(lineID)
		call.EndpointLineID = stringValue(endpointLineID)
		call.LocalPhone = stringValue(localPhone)
		call.LineIMSI = stringValue(lineIMSI)
		call.LineICCID = stringValue(lineICCID)
		call.Direction = stringValue(direction)
		call.RemoteNumber = stringValue(remoteNumber)
		call.ReportedRemoteNumber = stringValue(reportedRemoteNumber)
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
		call.Missed = (call.Phase == "ended" || call.Phase == "failed" || call.EndedAt != "") &&
			call.Direction == string(CallKindIncoming) && call.ActiveAt == nil &&
			call.EndReason != "rejected" && call.FailureCode != "rejected"
		call.Read = readAt.Valid && strings.TrimSpace(readAt.String) != ""
		call.Favorite = boolValue(favorite)
		call.SortEndedAt = stringValue(sortEndedAt)
		calls = append(calls, call)
	}
	return calls, rowsError("read calls", rows.Err())
}

func (s *Store) SetCallFavoritesByIDs(
	ctx context.Context,
	callIDs []string,
	favorite bool,
) error {
	normalized := make([]string, 0, len(callIDs))
	seen := make(map[string]struct{}, len(callIDs))
	for _, value := range callIDs {
		callID := strings.TrimSpace(value)
		if callID == "" {
			continue
		}
		if _, exists := seen[callID]; exists {
			continue
		}
		seen[callID] = struct{}{}
		normalized = append(normalized, callID)
	}
	if len(normalized) == 0 {
		return nil
	}
	if len(normalized) > 100 {
		return fmt.Errorf("update call favorite state: too many call IDs")
	}
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		return s.setUserCallFavorites(ctx, principal, normalized, favorite)
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin call favorite update: %w", err)
	}
	defer transaction.Rollback()

	arguments := make([]any, 0, len(normalized)+1)
	arguments = append(arguments, favorite)
	for _, callID := range normalized {
		arguments = append(arguments, callID)
	}
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE call_history
		 SET is_favorite = ?
		 WHERE id IN (`+placeholders(len(normalized))+`)`,
		arguments...,
	)
	if err != nil {
		return fmt.Errorf("update call favorite state by ID: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read call favorite update result: %w", err)
	}
	if affected != int64(len(normalized)) {
		return ErrCallNotFound
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit call favorite update: %w", err)
	}
	return nil
}

func (s *Store) setUserCallFavorites(
	ctx context.Context,
	principal auth.Principal,
	callIDs []string,
	favorite bool,
) error {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin user call favorite update: %w", err)
	}
	defer transaction.Rollback()
	for _, callID := range callIDs {
		var lineID string
		if err := transaction.QueryRowContext(
			ctx,
			"SELECT line_id FROM call_history WHERE id = ?",
			callID,
		).Scan(&lineID); errors.Is(err, sql.ErrNoRows) {
			return ErrCallNotFound
		} else if err != nil {
			return fmt.Errorf("query favorite call line: %w", err)
		}
		if !principal.CanAccessLine(lineID) {
			return ErrCallNotFound
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO modemdeck_user_call_state (
				user_id, call_id, is_read, is_favorite, updated_at
			) VALUES (?, ?, 0, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(user_id, call_id) DO UPDATE SET
				is_favorite = excluded.is_favorite,
				updated_at = CURRENT_TIMESTAMP
		`, principal.UserID, callID, favorite); err != nil {
			return fmt.Errorf("update user call favorite: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit user call favorite update: %w", err)
	}
	return nil
}

func (s *Store) MarkMissedCallsRead(ctx context.Context) error {
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		condition, arguments, _ := principalLineScope(ctx, "call_history.line_id")
		values := []any{principal.UserID}
		values = append(values, arguments...)
		if _, err := s.database.ExecContext(ctx, `
			INSERT INTO modemdeck_user_call_state (
				user_id, call_id, is_read, is_favorite, updated_at
			)
			SELECT ?, id, 1, 0, CURRENT_TIMESTAMP
			FROM call_history
			WHERE `+condition+`
				AND direction = 'incoming'
				AND active_at IS NULL
				AND COALESCE(end_reason, '') <> 'rejected'
				AND COALESCE(failure_code, '') <> 'rejected'
				AND (
					phase IN ('ended', 'failed') OR
					COALESCE(ended_at, '') <> ''
				)
			ON CONFLICT(user_id, call_id) DO UPDATE SET
				is_read = 1,
				updated_at = CURRENT_TIMESTAMP
		`, values...); err != nil {
			return fmt.Errorf("mark user missed calls read: %w", err)
		}
		return nil
	}
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE call_history
		 SET read_at = COALESCE(read_at, CURRENT_TIMESTAMP)
		 WHERE read_at IS NULL
			AND direction = 'incoming'
			AND active_at IS NULL
			AND COALESCE(end_reason, '') <> 'rejected'
			AND COALESCE(failure_code, '') <> 'rejected'
			AND (
				phase IN ('ended', 'failed') OR
				COALESCE(ended_at, '') <> ''
			)`,
	); err != nil {
		return fmt.Errorf("mark missed calls read: %w", err)
	}
	return nil
}

func (s *Store) MarkMissedCallsReadByIDs(ctx context.Context, callIDs []string) error {
	return s.SetMissedCallsReadByIDs(ctx, callIDs, true)
}

func (s *Store) MarkMissedCallsUnreadByIDs(ctx context.Context, callIDs []string) error {
	return s.SetMissedCallsReadByIDs(ctx, callIDs, false)
}

func (s *Store) SetMissedCallsReadByIDs(
	ctx context.Context,
	callIDs []string,
	read bool,
) error {
	normalized := make([]string, 0, len(callIDs))
	seen := make(map[string]struct{}, len(callIDs))
	for _, value := range callIDs {
		callID := strings.TrimSpace(value)
		if callID == "" {
			continue
		}
		if _, exists := seen[callID]; exists {
			continue
		}
		seen[callID] = struct{}{}
		normalized = append(normalized, callID)
	}
	if len(normalized) == 0 {
		return nil
	}
	if len(normalized) > 100 {
		return fmt.Errorf("update missed call read state: too many call IDs")
	}
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		transaction, err := s.database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin user missed call state update: %w", err)
		}
		defer transaction.Rollback()
		for _, callID := range normalized {
			var lineID string
			err := transaction.QueryRowContext(
				ctx,
				`SELECT line_id FROM call_history
				 WHERE id = ?
					AND direction = 'incoming'
					AND active_at IS NULL`,
				callID,
			).Scan(&lineID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCallNotFound
			}
			if err != nil {
				return fmt.Errorf("query missed call line: %w", err)
			}
			if !principal.CanAccessLine(lineID) {
				return ErrCallNotFound
			}
			if _, err := transaction.ExecContext(ctx, `
				INSERT INTO modemdeck_user_call_state (
					user_id, call_id, is_read, is_favorite, updated_at
				) VALUES (?, ?, ?, 0, CURRENT_TIMESTAMP)
				ON CONFLICT(user_id, call_id) DO UPDATE SET
					is_read = excluded.is_read,
					updated_at = CURRENT_TIMESTAMP
			`, principal.UserID, callID, read); err != nil {
				return fmt.Errorf("update user missed call state: %w", err)
			}
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit user missed call state: %w", err)
		}
		return nil
	}
	arguments := make([]any, len(normalized))
	for index, callID := range normalized {
		arguments[index] = callID
	}
	readValue := "NULL"
	if read {
		readValue = "COALESCE(read_at, CURRENT_TIMESTAMP)"
	}
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE call_history
		 SET read_at = `+readValue+`
		 WHERE direction = 'incoming'
			AND active_at IS NULL
			AND COALESCE(end_reason, '') <> 'rejected'
			AND COALESCE(failure_code, '') <> 'rejected'
			AND (
				phase IN ('ended', 'failed') OR
				COALESCE(ended_at, '') <> ''
			)
			AND id IN (`+placeholders(len(normalized))+`)`,
		arguments...,
	); err != nil {
		return fmt.Errorf("update missed call read state by ID: %w", err)
	}
	return nil
}

func (s *Store) DeleteCall(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrCallNotFound
	}
	result, err := s.database.ExecContext(
		ctx,
		`DELETE FROM call_history
		 WHERE id = ?
			AND (
				phase IN ('ended', 'failed') OR
				COALESCE(ended_at, '') <> ''
			)`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted call count: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var exists int
	if err := s.database.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM call_history WHERE id = ?)`,
		id,
	).Scan(&exists); err != nil {
		return fmt.Errorf("inspect call deletion conflict: %w", err)
	}
	if exists == 0 {
		return ErrCallNotFound
	}
	return ErrCallActive
}
