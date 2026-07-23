package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrCallNotFound    = errors.New("call not found")
	ErrMessageNotFound = errors.New("message not found")
	ErrSnapshotInvalid = errors.New("hardware snapshot is invalid")
)

const (
	modemManagerEndpointID         = "modemmanager"
	defaultHardwareCallFailureCode = ""
)

func (s *Store) ApplyHardwareSnapshot(ctx context.Context, snapshot HardwareSnapshot) error {
	snapshot.BootEpoch = strings.TrimSpace(snapshot.BootEpoch)
	snapshot.Revision = strings.TrimSpace(snapshot.Revision)
	if snapshot.BootEpoch == "" || snapshot.Revision == "" || snapshot.ObservedAt.IsZero() {
		return ErrSnapshotInvalid
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin hardware snapshot: %w", err)
	}
	defer transaction.Rollback()
	sequence, duplicate, err := allocateSnapshotSequence(ctx, transaction, snapshot)
	if err != nil {
		return err
	}
	if duplicate {
		return nil
	}

	for _, line := range snapshot.Lines {
		if err := upsertHardwareLine(ctx, transaction, line, snapshot.ObservedAt); err != nil {
			return err
		}
	}
	for _, message := range snapshot.Messages {
		message.Revision = sequence
		if _, _, err := upsertHardwareMessage(ctx, transaction, message); err != nil {
			return err
		}
	}
	for _, call := range snapshot.Calls {
		call.Revision = sequence
		if _, err := upsertHardwareCall(ctx, transaction, call); err != nil {
			return err
		}
	}
	if err := closeMissingCalls(ctx, transaction, snapshot, sequence); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit hardware snapshot: %w", err)
	}
	return nil
}

func (s *Store) UpsertHardwareMessage(ctx context.Context, message HardwareMessage) (Message, bool, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, false, fmt.Errorf("begin message upsert: %w", err)
	}
	defer transaction.Rollback()
	if message.Revision <= 0 {
		message.Revision, err = nextHardwareSequence(ctx, transaction)
		if err != nil {
			return Message{}, false, err
		}
	}
	stored, created, err := upsertHardwareMessage(ctx, transaction, message)
	if err != nil {
		return Message{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Message{}, false, fmt.Errorf("commit message upsert: %w", err)
	}
	return stored, created, nil
}

func (s *Store) UpsertHardwareCall(ctx context.Context, call HardwareCall) (Call, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Call{}, fmt.Errorf("begin call upsert: %w", err)
	}
	defer transaction.Rollback()
	if call.Revision <= 0 {
		call.Revision, err = nextHardwareSequence(ctx, transaction)
		if err != nil {
			return Call{}, err
		}
	}
	stored, err := upsertHardwareCall(ctx, transaction, call)
	if err != nil {
		return Call{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Call{}, fmt.Errorf("commit call upsert: %w", err)
	}
	return stored, nil
}

func (s *Store) CallControlTarget(ctx context.Context, appID string) (CallControlTarget, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return CallControlTarget{}, ErrCallNotFound
	}
	var target CallControlTarget
	err := s.database.QueryRowContext(
		ctx,
		`SELECT id, device_id, endpoint_call_id, remote_number, direction, phase, bearer, revision
		 FROM call_history
		 WHERE id = ?`,
		appID,
	).Scan(
		&target.AppID,
		&target.LineID,
		&target.EndpointCallID,
		&target.Number,
		&target.Direction,
		&target.Phase,
		&target.Bearer,
		&target.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CallControlTarget{}, ErrCallNotFound
	}
	if err != nil {
		return CallControlTarget{}, fmt.Errorf("query call control target: %w", err)
	}
	return target, nil
}

func (s *Store) ActiveCalls(ctx context.Context) ([]Call, error) {
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT id, request_id, device_id, direction, remote_number,
			endpoint_id, endpoint_call_id, phase, revision, created_at, updated_at,
			active_at, ended_at, end_reason, failure_code, bearer, state_reason,
			state_reason_code, multiparty, audio_port, audio_encoding,
			audio_resolution, audio_rate, media_available
		 FROM call_history
		 WHERE phase NOT IN ('ended', 'failed') AND COALESCE(ended_at, '') = ''
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active calls: %w", err)
	}
	defer rows.Close()
	result := make([]Call, 0)
	for rows.Next() {
		call, err := scanCallRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, call)
	}
	return result, rowsError("read active calls", rows.Err())
}

func (s *Store) MarkMessageThreadRead(ctx context.Context, iccid, peer string) error {
	iccid = strings.TrimSpace(iccid)
	peer = strings.TrimSpace(peer)
	if iccid == "" || peer == "" {
		return fmt.Errorf("mark message thread read: iccid and peer are required")
	}
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE sms_contacts
		 SET unread_count = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE iccid = ? AND peer = ?`,
		iccid,
		peer,
	); err != nil {
		return fmt.Errorf("mark message thread read: %w", err)
	}
	return nil
}

func (s *Store) MarkMessageThreadReadByLine(ctx context.Context, lineID, peer string) error {
	lineID = strings.TrimSpace(lineID)
	peer = strings.TrimSpace(peer)
	if lineID == "" || peer == "" {
		return fmt.Errorf("mark message thread read by line: line ID and peer are required")
	}
	if _, err := s.database.ExecContext(
		ctx,
		`UPDATE sms_contacts
		 SET unread_count = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE peer = ? AND iccid = (
			SELECT iccid
			FROM sms
			WHERE line_id = ? AND peer = ? AND COALESCE(iccid, '') <> ''
			ORDER BY timestamp DESC, id DESC
			LIMIT 1
		 )`,
		peer,
		lineID,
		peer,
	); err != nil {
		return fmt.Errorf("mark message thread read by line: %w", err)
	}
	return nil
}

func upsertHardwareLine(ctx context.Context, transaction *sql.Tx, line HardwareLine, observedAt time.Time) error {
	line.ID = strings.TrimSpace(line.ID)
	if line.ID == "" {
		return nil
	}
	if err := ensureLineCallPolicy(ctx, transaction, line.ID); err != nil {
		return err
	}
	imei := strings.TrimSpace(line.EquipmentIdentifier)
	if imei == "" {
		imei = strings.TrimSpace(line.DeviceIdentifier)
	}
	if imei == "" {
		return nil
	}
	var signal any
	if line.SignalKnown {
		signal = int64(line.SignalQuality)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO devices (
			imei, model, firmware, port, iccid, sim_inserted, signal_quality,
			last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)
		 ON CONFLICT(imei) DO UPDATE SET
			model = excluded.model,
			firmware = excluded.firmware,
			port = excluded.port,
			iccid = excluded.iccid,
			sim_inserted = excluded.sim_inserted,
			signal_quality = excluded.signal_quality,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		imei,
		strings.TrimSpace(line.Model),
		strings.TrimSpace(line.Firmware),
		strings.TrimSpace(line.PrimaryPort),
		strings.TrimSpace(line.ICCID),
		line.ICCID != "",
		signal,
		databaseTime(observedAt),
		databaseTime(observedAt),
		databaseTime(observedAt),
	); err != nil {
		return fmt.Errorf("upsert hardware line device: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_line_settings
		 SET default_device_imei = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND default_device_imei = ''`,
		imei,
	); err != nil {
		return fmt.Errorf("initialize default line: %w", err)
	}
	if strings.TrimSpace(line.ICCID) == "" {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sim_cards (
			iccid, imsi, operator, current_imei, reg_status_text, last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(iccid) DO UPDATE SET
			imsi = excluded.imsi,
			operator = excluded.operator,
			current_imei = excluded.current_imei,
			reg_status_text = excluded.reg_status_text,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		strings.TrimSpace(line.ICCID),
		strings.TrimSpace(line.IMSI),
		strings.TrimSpace(line.Operator),
		imei,
		strings.TrimSpace(line.State),
		databaseTime(observedAt),
		databaseTime(observedAt),
		databaseTime(observedAt),
	); err != nil {
		return fmt.Errorf("upsert hardware line SIM: %w", err)
	}
	if strings.TrimSpace(line.IMSI) == "" {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sim_subscriptions (
			imsi, current_iccid, phone_number, modem_phone_number, operator,
			last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(imsi) DO UPDATE SET
			current_iccid = excluded.current_iccid,
			phone_number = CASE
				WHEN excluded.phone_number <> '' THEN excluded.phone_number
				ELSE sim_subscriptions.phone_number
			END,
			modem_phone_number = CASE
				WHEN excluded.modem_phone_number <> '' THEN excluded.modem_phone_number
				ELSE sim_subscriptions.modem_phone_number
			END,
			operator = excluded.operator,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		strings.TrimSpace(line.IMSI),
		strings.TrimSpace(line.ICCID),
		strings.TrimSpace(line.PhoneNumber),
		strings.TrimSpace(line.PhoneNumber),
		strings.TrimSpace(line.Operator),
		databaseTime(observedAt),
		databaseTime(observedAt),
		databaseTime(observedAt),
	); err != nil {
		return fmt.Errorf("upsert hardware line subscription: %w", err)
	}
	return nil
}

func upsertHardwareMessage(
	ctx context.Context,
	transaction *sql.Tx,
	message HardwareMessage,
) (Message, bool, error) {
	message.LineID = strings.TrimSpace(message.LineID)
	message.EndpointMessageID = strings.TrimSpace(message.EndpointMessageID)
	message.RequestID = strings.TrimSpace(message.RequestID)
	message.Number = strings.TrimSpace(message.Number)
	message.Text = strings.TrimSpace(message.Text)
	message.Direction = strings.ToLower(strings.TrimSpace(message.Direction))
	message.State = strings.ToLower(strings.TrimSpace(message.State))
	if message.LineID == "" || message.Number == "" || message.Text == "" {
		return Message{}, false, fmt.Errorf("%w: message identity or content is empty", ErrSnapshotInvalid)
	}
	if message.Direction != "incoming" && message.Direction != "outgoing" {
		return Message{}, false, fmt.Errorf("%w: invalid message direction", ErrSnapshotInvalid)
	}
	if message.EndpointMessageID == "" && message.RequestID == "" {
		return Message{}, false, fmt.Errorf("%w: message has no endpoint or request identity", ErrSnapshotInvalid)
	}
	if message.Timestamp.IsZero() {
		message.Timestamp = message.ObservedAt
	}
	if message.ObservedAt.IsZero() {
		message.ObservedAt = message.Timestamp
	}
	if message.Revision <= 0 {
		message.Revision = 1
	}

	var existingID int64
	if message.RequestID != "" {
		err := transaction.QueryRowContext(
			ctx,
			"SELECT id FROM sms WHERE request_id = ?",
			message.RequestID,
		).Scan(&existingID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Message{}, false, fmt.Errorf("query message request identity: %w", err)
		}
	}
	if existingID == 0 && message.EndpointMessageID != "" {
		err := transaction.QueryRowContext(
			ctx,
			"SELECT id FROM sms WHERE line_id = ? AND endpoint_message_id = ?",
			message.LineID,
			message.EndpointMessageID,
		).Scan(&existingID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Message{}, false, fmt.Errorf("query message endpoint identity: %w", err)
		}
	}
	created := existingID == 0
	if created {
		messageType := int64(1)
		sender := message.Number
		recipient := message.LocalPhone
		if message.Direction == "outgoing" {
			messageType = 2
			sender = message.LocalPhone
			recipient = message.Number
		}
		result, err := transaction.ExecContext(
			ctx,
			`INSERT INTO sms (
				request_id, line_id, endpoint_message_id, imsi, iccid, peer,
				local_phone, sender, recipient, content, type, status, state,
				failure_code, revision, timestamp, created_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
			message.RequestID,
			message.LineID,
			message.EndpointMessageID,
			strings.TrimSpace(message.IMSI),
			strings.TrimSpace(message.ICCID),
			message.Number,
			strings.TrimSpace(message.LocalPhone),
			sender,
			recipient,
			message.Text,
			messageType,
			message.StateCode,
			message.State,
			message.Revision,
			databaseTime(message.Timestamp),
			databaseTime(message.ObservedAt),
		)
		if err != nil {
			return Message{}, false, fmt.Errorf("insert hardware message: %w", err)
		}
		existingID, err = result.LastInsertId()
		if err != nil {
			return Message{}, false, fmt.Errorf("read hardware message id: %w", err)
		}
		if err := updateMessageThread(ctx, transaction, existingID, message); err != nil {
			return Message{}, false, err
		}
		if message.Direction == "incoming" {
			resourceID := message.EndpointMessageID
			if resourceID == "" {
				resourceID = fmt.Sprintf("%d", existingID)
			}
			if err := enqueueTelegramNotification(
				ctx,
				transaction,
				fmt.Sprintf("sms:%d", existingID),
				NotificationIncomingSMS,
				resourceID,
				message.LineID,
				message.Number,
				message.Text,
				message.Timestamp,
			); err != nil {
				return Message{}, false, err
			}
		}
	} else {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE sms SET
				request_id = CASE WHEN request_id = '' THEN ? ELSE request_id END,
				endpoint_message_id = CASE WHEN endpoint_message_id = '' THEN ? ELSE endpoint_message_id END,
				status = CASE WHEN revision <= ? THEN ? ELSE status END,
				state = CASE WHEN revision <= ? THEN ? ELSE state END,
				revision = CASE WHEN revision < ? THEN ? ELSE revision END
			 WHERE id = ?`,
			message.RequestID,
			message.EndpointMessageID,
			message.Revision,
			message.StateCode,
			message.Revision,
			message.State,
			message.Revision,
			message.Revision,
			existingID,
		); err != nil {
			return Message{}, false, fmt.Errorf("update hardware message: %w", err)
		}
	}
	stored, err := messageByID(ctx, transaction, existingID)
	return stored, created, err
}

func updateMessageThread(
	ctx context.Context,
	transaction *sql.Tx,
	messageID int64,
	message HardwareMessage,
) error {
	if strings.TrimSpace(message.IMSI) == "" {
		return nil
	}
	unread := 0
	if message.Direction == "incoming" {
		unread = 1
	}
	messageType := 1
	if message.Direction == "outgoing" {
		messageType = 2
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sms_contacts (
			imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(imsi, peer) DO UPDATE SET
			iccid = excluded.iccid,
			last_sms_id = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_sms_id ELSE sms_contacts.last_sms_id
			END,
			last_timestamp = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_timestamp ELSE sms_contacts.last_timestamp
			END,
			last_content = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_content ELSE sms_contacts.last_content
			END,
			last_type = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_type ELSE sms_contacts.last_type
			END,
			unread_count = sms_contacts.unread_count + excluded.unread_count,
			updated_at = excluded.updated_at`,
		strings.TrimSpace(message.IMSI),
		strings.TrimSpace(message.ICCID),
		message.Number,
		messageID,
		databaseTime(message.Timestamp),
		message.Text,
		messageType,
		unread,
		databaseTime(message.ObservedAt),
		databaseTime(message.ObservedAt),
	); err != nil {
		return fmt.Errorf("update message thread: %w", err)
	}
	return nil
}

func messageByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64) (Message, error) {
	var (
		message                                                                    Message
		requestID, lineID, endpointID, imsi, iccid, peer, local, sender, recipient sql.NullString
		content, state, failureCode, timestamp, createdAt                          sql.NullString
		messageType, status, revision                                              sql.NullInt64
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT id, request_id, line_id, endpoint_message_id, imsi, iccid, peer,
			local_phone, sender, recipient, content, type, status, state,
			failure_code, revision, timestamp, created_at
		 FROM sms WHERE id = ?`,
		id,
	).Scan(
		&message.ID,
		&requestID,
		&lineID,
		&endpointID,
		&imsi,
		&iccid,
		&peer,
		&local,
		&sender,
		&recipient,
		&content,
		&messageType,
		&status,
		&state,
		&failureCode,
		&revision,
		&timestamp,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("query message by id: %w", err)
	}
	message.RequestID = stringValue(requestID)
	message.LineID = stringValue(lineID)
	message.EndpointMessageID = stringValue(endpointID)
	message.IMSI = stringValue(imsi)
	message.ICCID = stringValue(iccid)
	message.Peer = stringValue(peer)
	message.LocalPhone = stringValue(local)
	message.Sender = stringValue(sender)
	message.Recipient = stringValue(recipient)
	message.Content = stringValue(content)
	message.Type = intValue(messageType)
	message.Status = intValue(status)
	message.State = stringValue(state)
	message.FailureCode = stringValue(failureCode)
	message.Revision = intValue(revision)
	message.Timestamp = stringValue(timestamp)
	message.CreatedAt = stringValue(createdAt)
	if message.Type == 1 {
		message.Direction = "incoming"
	} else if message.Type == 2 {
		message.Direction = "outgoing"
	}
	return message, nil
}

func upsertHardwareCall(ctx context.Context, transaction *sql.Tx, call HardwareCall) (Call, error) {
	call.AppID = strings.TrimSpace(call.AppID)
	call.LineID = strings.TrimSpace(call.LineID)
	call.EndpointCallID = strings.TrimSpace(call.EndpointCallID)
	call.Number = strings.TrimSpace(call.Number)
	call.Direction = strings.ToLower(strings.TrimSpace(call.Direction))
	call.Phase = strings.ToLower(strings.TrimSpace(call.Phase))
	call.Bearer = strings.TrimSpace(call.Bearer)
	call.StateReason = strings.TrimSpace(call.StateReason)
	call.AudioPort = strings.TrimSpace(call.AudioPort)
	call.AudioEncoding = strings.TrimSpace(call.AudioEncoding)
	call.AudioResolution = strings.TrimSpace(call.AudioResolution)
	if call.AppID == "" || call.LineID == "" || call.EndpointCallID == "" ||
		(call.Direction != "incoming" && call.Direction != "outgoing") {
		return Call{}, fmt.Errorf("%w: invalid call identity", ErrSnapshotInvalid)
	}
	if call.Revision <= 0 {
		call.Revision = 1
	}
	if call.ObservedAt.IsZero() {
		call.ObservedAt = time.Now().UTC()
	}
	newlyDiscovered := false
	var existingCallID string
	err := transaction.QueryRowContext(
		ctx,
		"SELECT id FROM call_history WHERE id = ?",
		call.AppID,
	).Scan(&existingCallID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		newlyDiscovered = true
	case err != nil:
		return Call{}, fmt.Errorf("query hardware call identity: %w", err)
	}
	observed := databaseTime(call.ObservedAt)
	activeAt := any(nil)
	endedAt := any(nil)
	endReason := ""
	if call.Phase == "active" {
		activeAt = observed
	}
	if call.Phase == "ended" || call.Phase == "failed" {
		endedAt = observed
		endReason = call.StateReason
		if endReason == "" {
			endReason = "unknown"
		}
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO call_history (
			id, request_id, device_id, direction, remote_number, endpoint_id,
			endpoint_call_id, phase, revision, created_at, updated_at, active_at,
			ended_at, end_reason, failure_code, bearer, state_reason,
			state_reason_code, multiparty, audio_port, audio_encoding,
			audio_resolution, audio_rate, media_available
		 ) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?
		 )
		 ON CONFLICT(id) DO UPDATE SET
			endpoint_call_id = excluded.endpoint_call_id,
			phase = excluded.phase,
			revision = CASE
				WHEN call_history.revision < excluded.revision THEN excluded.revision
				ELSE call_history.revision + 1
			END,
			updated_at = excluded.updated_at,
			active_at = COALESCE(call_history.active_at, excluded.active_at),
			ended_at = COALESCE(call_history.ended_at, excluded.ended_at),
			end_reason = CASE
				WHEN excluded.end_reason <> '' THEN excluded.end_reason
				ELSE call_history.end_reason
			END,
			bearer = CASE
				WHEN excluded.bearer <> '' THEN excluded.bearer
				ELSE call_history.bearer
			END,
			state_reason = CASE
				WHEN excluded.state_reason <> '' THEN excluded.state_reason
				ELSE call_history.state_reason
			END,
			state_reason_code = excluded.state_reason_code,
			multiparty = excluded.multiparty,
			audio_port = CASE
				WHEN excluded.audio_port <> '' THEN excluded.audio_port
				ELSE call_history.audio_port
			END,
			audio_encoding = CASE
				WHEN excluded.audio_encoding <> '' THEN excluded.audio_encoding
				ELSE call_history.audio_encoding
			END,
			audio_resolution = CASE
				WHEN excluded.audio_resolution <> '' THEN excluded.audio_resolution
				ELSE call_history.audio_resolution
			END,
			audio_rate = CASE
				WHEN excluded.audio_rate > 0 THEN excluded.audio_rate
				ELSE call_history.audio_rate
			END,
			media_available = excluded.media_available
		 WHERE excluded.revision >= call_history.revision`,
		call.AppID,
		call.RequestID,
		call.LineID,
		call.Direction,
		call.Number,
		modemManagerEndpointID,
		call.EndpointCallID,
		call.Phase,
		call.Revision,
		observed,
		observed,
		activeAt,
		endedAt,
		endReason,
		defaultHardwareCallFailureCode,
		call.Bearer,
		call.StateReason,
		call.StateReasonCode,
		call.Multiparty,
		call.AudioPort,
		call.AudioEncoding,
		call.AudioResolution,
		call.AudioRate,
		call.MediaAvailable,
	); err != nil {
		return Call{}, fmt.Errorf("upsert hardware call: %w", err)
	}
	if err := ensureCallRecordingState(ctx, transaction, call.AppID); err != nil {
		return Call{}, err
	}
	if newlyDiscovered && call.Direction == "incoming" && call.Phase == "ringing" {
		if err := enqueueIncomingCallAction(ctx, transaction, call); err != nil {
			return Call{}, err
		}
	}
	stored, err := callByID(ctx, transaction, call.AppID)
	if err != nil {
		return Call{}, err
	}
	if stored.Missed && (stored.Phase == "ended" || stored.Phase == "failed") {
		if err := enqueueTelegramNotification(
			ctx,
			transaction,
			"call:"+stored.ID,
			NotificationMissedCall,
			stored.ID,
			stored.DeviceID,
			stored.RemoteNumber,
			"",
			call.ObservedAt,
		); err != nil {
			return Call{}, err
		}
	}
	return stored, nil
}

func closeMissingCalls(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot HardwareSnapshot,
	sequence int64,
) error {
	activeIDs := make(map[string]struct{}, len(snapshot.Calls))
	for _, call := range snapshot.Calls {
		phase := strings.ToLower(strings.TrimSpace(call.Phase))
		if phase != "ended" && phase != "failed" && strings.TrimSpace(call.AppID) != "" {
			activeIDs[strings.TrimSpace(call.AppID)] = struct{}{}
		}
	}
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT id, device_id, remote_number, created_at, direction,
			active_at, end_reason, failure_code
		 FROM call_history
		 WHERE endpoint_id = ? AND phase NOT IN ('ended', 'failed')`,
		modemManagerEndpointID,
	)
	if err != nil {
		return fmt.Errorf("query open hardware calls: %w", err)
	}
	type missingCall struct {
		id, lineID, peer, createdAt, direction string
		missed                                 bool
	}
	missing := make([]missingCall, 0)
	for rows.Next() {
		var (
			call                             missingCall
			activeAt, endReason, failureCode sql.NullString
		)
		if err := rows.Scan(
			&call.id,
			&call.lineID,
			&call.peer,
			&call.createdAt,
			&call.direction,
			&activeAt,
			&endReason,
			&failureCode,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan open hardware call: %w", err)
		}
		if _, stillActive := activeIDs[call.id]; stillActive {
			continue
		}
		call.missed = call.direction == "incoming" &&
			!activeAt.Valid &&
			stringValue(endReason) != "rejected" &&
			stringValue(failureCode) != "rejected"
		missing = append(missing, call)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read open hardware calls: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close open hardware calls: %w", err)
	}

	for _, call := range missing {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE call_history
				 SET phase = 'ended', ended_at = ?, updated_at = ?,
					end_reason = CASE
						WHEN end_reason = '' THEN 'not_present_in_snapshot'
						ELSE end_reason
					END,
					revision = ?,
					audio_port = '',
					audio_encoding = '',
					audio_resolution = '',
					audio_rate = 0,
					media_available = 0
				 WHERE id = ? AND device_id = ? AND endpoint_id = ?
					AND phase NOT IN ('ended', 'failed')`,
			databaseTime(snapshot.ObservedAt),
			databaseTime(snapshot.ObservedAt),
			sequence,
			call.id,
			call.lineID,
			modemManagerEndpointID,
		); err != nil {
			return fmt.Errorf("close missing hardware call: %w", err)
		}
		if !call.missed {
			continue
		}
		occurredAt := snapshot.ObservedAt
		if parsed, ok := parseDatabaseTime(call.createdAt); ok {
			occurredAt = parsed.UTC()
		}
		if err := enqueueTelegramNotification(
			ctx,
			transaction,
			"call:"+call.id,
			NotificationMissedCall,
			call.id,
			call.lineID,
			call.peer,
			"",
			occurredAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func allocateSnapshotSequence(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot HardwareSnapshot,
) (int64, bool, error) {
	var (
		bootEpoch, revision string
		sequence            int64
	)
	err := transaction.QueryRowContext(
		ctx,
		`SELECT boot_epoch, snapshot_revision, sequence
		 FROM modemdeck_hardware_sync WHERE singleton = 1`,
	).Scan(&bootEpoch, &revision, &sequence)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("read hardware snapshot sequence: %w", err)
	}
	if err == nil && bootEpoch == snapshot.BootEpoch && revision == snapshot.Revision {
		return sequence, true, nil
	}
	if sequence == int64(^uint64(0)>>1) {
		return 0, false, fmt.Errorf("hardware snapshot sequence exhausted")
	}
	sequence++
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_hardware_sync (
			singleton, boot_epoch, snapshot_revision, sequence, observed_at, updated_at
		 ) VALUES (1, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(singleton) DO UPDATE SET
			boot_epoch = excluded.boot_epoch,
			snapshot_revision = excluded.snapshot_revision,
			sequence = excluded.sequence,
			observed_at = excluded.observed_at,
			updated_at = excluded.updated_at`,
		snapshot.BootEpoch,
		snapshot.Revision,
		sequence,
		databaseTime(snapshot.ObservedAt),
	); err != nil {
		return 0, false, fmt.Errorf("advance hardware snapshot sequence: %w", err)
	}
	return sequence, false, nil
}

func nextHardwareSequence(ctx context.Context, transaction *sql.Tx) (int64, error) {
	var sequence int64
	err := transaction.QueryRowContext(
		ctx,
		"SELECT sequence FROM modemdeck_hardware_sync WHERE singleton = 1",
	).Scan(&sequence)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read hardware command sequence: %w", err)
	}
	if sequence == int64(^uint64(0)>>1) {
		return 0, fmt.Errorf("hardware command sequence exhausted")
	}
	sequence++
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_hardware_sync (
			singleton, sequence, updated_at
		 ) VALUES (1, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(singleton) DO UPDATE SET
			sequence = excluded.sequence,
			updated_at = excluded.updated_at`,
		sequence,
	); err != nil {
		return 0, fmt.Errorf("advance hardware command sequence: %w", err)
	}
	return sequence, nil
}

func callByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Call, error) {
	row := queryer.QueryRowContext(
		ctx,
		`SELECT id, request_id, device_id, direction, remote_number,
			endpoint_id, endpoint_call_id, phase, revision, created_at, updated_at,
			active_at, ended_at, end_reason, failure_code, bearer, state_reason,
			state_reason_code, multiparty, audio_port, audio_encoding,
			audio_resolution, audio_rate, media_available
		 FROM call_history WHERE id = ?`,
		id,
	)
	call, err := scanCallRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Call{}, ErrCallNotFound
	}
	return call, err
}

type callScanner interface {
	Scan(...any) error
}

func scanCallRow(scanner callScanner) (Call, error) {
	var (
		call                                                                       Call
		requestID, deviceID, direction, remoteNumber, endpointID, endpointCallID   sql.NullString
		phase, createdAt, updatedAt, activeAt, endedAt, endReason, failure, bearer sql.NullString
		stateReason, audioPort, audioEncoding, audioResolution                     sql.NullString
		revision, stateReasonCode, multiparty, audioRate, mediaAvailable           sql.NullInt64
	)
	if err := scanner.Scan(
		&call.ID,
		&requestID,
		&deviceID,
		&direction,
		&remoteNumber,
		&endpointID,
		&endpointCallID,
		&phase,
		&revision,
		&createdAt,
		&updatedAt,
		&activeAt,
		&endedAt,
		&endReason,
		&failure,
		&bearer,
		&stateReason,
		&stateReasonCode,
		&multiparty,
		&audioPort,
		&audioEncoding,
		&audioResolution,
		&audioRate,
		&mediaAvailable,
	); err != nil {
		return Call{}, err
	}
	call.RequestID = stringValue(requestID)
	call.DeviceID = stringValue(deviceID)
	call.Direction = stringValue(direction)
	call.RemoteNumber = stringValue(remoteNumber)
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
	call.FailureCode = stringValue(failure)
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
	call.Missed = call.Direction == "incoming" && call.ActiveAt == nil &&
		call.EndReason != "rejected" && call.FailureCode != "rejected"
	return call, nil
}

func databaseTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
