package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/modemidentity"
	"github.com/human-agent65535/modemdeck/internal/phone"
)

var (
	ErrCallNotFound          = errors.New("call not found")
	ErrCallActive            = errors.New("call is still active")
	ErrMessageNotFound       = errors.New("message not found")
	ErrMessageThreadNotFound = errors.New("message thread not found")
	ErrSnapshotInvalid       = errors.New("hardware snapshot is invalid")
)

const (
	modemManagerEndpointID         = "modemmanager"
	defaultHardwareCallFailureCode = ""
)

func (s *Store) ApplyHardwareSnapshot(ctx context.Context, snapshot HardwareSnapshot) error {
	_, err := s.ApplyHardwareSnapshotWithResult(ctx, snapshot)
	return err
}

func (s *Store) ApplyHardwareSnapshotWithResult(
	ctx context.Context,
	snapshot HardwareSnapshot,
) (HardwareSnapshotResult, error) {
	snapshot.BootEpoch = strings.TrimSpace(snapshot.BootEpoch)
	snapshot.Revision = strings.TrimSpace(snapshot.Revision)
	if snapshot.BootEpoch == "" || snapshot.Revision == "" || snapshot.ObservedAt.IsZero() {
		return HardwareSnapshotResult{}, ErrSnapshotInvalid
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return HardwareSnapshotResult{}, fmt.Errorf("begin hardware snapshot: %w", err)
	}
	defer transaction.Rollback()
	sequence, duplicate, err := allocateSnapshotSequence(ctx, transaction, snapshot)
	if err != nil {
		return HardwareSnapshotResult{}, err
	}
	lineIDsByEndpoint := make(map[string]string, len(snapshot.Lines))
	endpointsByLineID := make(map[string]string, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		lineID, err := upsertHardwareLine(ctx, transaction, line, snapshot.ObservedAt)
		if err != nil {
			return HardwareSnapshotResult{}, err
		}
		endpointID := strings.TrimSpace(line.ID)
		if endpointID != "" && lineID != "" {
			if existingEndpoint := endpointsByLineID[lineID]; existingEndpoint != "" &&
				existingEndpoint != endpointID {
				return HardwareSnapshotResult{}, fmt.Errorf(
					"%w: stable line %q is attached to endpoints %q and %q",
					ErrSnapshotInvalid,
					lineID,
					existingEndpoint,
					endpointID,
				)
			}
			endpointsByLineID[lineID] = endpointID
			lineIDsByEndpoint[endpointID] = lineID
		}
	}
	if err := rewriteSnapshotLineIdentities(
		ctx,
		transaction,
		&snapshot,
		lineIDsByEndpoint,
	); err != nil {
		return HardwareSnapshotResult{}, err
	}
	handledReports := make([]string, 0, len(snapshot.DeliveryReports))
	if duplicate {
		for _, report := range snapshot.DeliveryReports {
			handled, err := applyHardwareMessageDeliveryReport(
				ctx,
				transaction,
				report,
				sequence,
			)
			if err != nil {
				return HardwareSnapshotResult{}, err
			}
			if handled {
				handledReports = append(handledReports, report.EndpointReportID)
			}
		}
		if err := closeMissingCalls(ctx, transaction, snapshot, sequence); err != nil {
			return HardwareSnapshotResult{}, err
		}
		if err := transaction.Commit(); err != nil {
			return HardwareSnapshotResult{}, fmt.Errorf(
				"commit duplicate hardware snapshot reconciliation: %w",
				err,
			)
		}
		return HardwareSnapshotResult{
			CreatedIncomingMessages:  []Message{},
			HandledDeliveryReportIDs: handledReports,
			LineIDsByEndpoint:        lineIDsByEndpoint,
		}, nil
	}

	createdIncoming := make([]Message, 0)
	for _, message := range snapshot.Messages {
		message.Revision = sequence
		stored, created, err := upsertHardwareMessage(ctx, transaction, message)
		if err != nil {
			return HardwareSnapshotResult{}, err
		}
		if created && stored.Direction == "incoming" && stored.State == "received" {
			createdIncoming = append(createdIncoming, stored)
		}
	}
	for _, report := range snapshot.DeliveryReports {
		handled, err := applyHardwareMessageDeliveryReport(
			ctx,
			transaction,
			report,
			sequence,
		)
		if err != nil {
			return HardwareSnapshotResult{}, err
		}
		if handled {
			handledReports = append(handledReports, report.EndpointReportID)
		}
	}
	for _, call := range snapshot.Calls {
		call.Revision = sequence
		if _, err := upsertHardwareCall(ctx, transaction, call); err != nil {
			return HardwareSnapshotResult{}, err
		}
	}
	if err := closeMissingCalls(ctx, transaction, snapshot, sequence); err != nil {
		return HardwareSnapshotResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return HardwareSnapshotResult{}, fmt.Errorf("commit hardware snapshot: %w", err)
	}
	return HardwareSnapshotResult{
		CreatedIncomingMessages:  createdIncoming,
		HandledDeliveryReportIDs: handledReports,
		LineIDsByEndpoint:        lineIDsByEndpoint,
	}, nil
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
	message.LineID, message.EndpointLineID, err = resolveStoredHardwareLine(
		ctx,
		transaction,
		message.LineID,
		message.EndpointLineID,
		message.ICCID,
		message.IMSI,
		message.LocalPhone,
		message.HomeCountryISO,
	)
	if err != nil {
		return Message{}, false, fmt.Errorf("resolve message line: %w", err)
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
	call.LineID, call.EndpointLineID, err = resolveStoredHardwareLine(
		ctx,
		transaction,
		call.LineID,
		call.EndpointLineID,
		call.LineICCID,
		call.LineIMSI,
		call.LocalPhone,
		call.HomeCountryISO,
	)
	if err != nil {
		return Call{}, fmt.Errorf("resolve call line: %w", err)
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
		`SELECT id, line_id, endpoint_line_id, endpoint_call_id,
			remote_number, direction, phase, bearer, revision
		 FROM call_history
		 WHERE id = ?`,
		appID,
	).Scan(
		&target.AppID,
		&target.LineID,
		&target.EndpointLineID,
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
		`SELECT id, request_id, line_id, endpoint_line_id, local_phone, line_imsi, line_iccid,
			direction, remote_number, reported_remote_number,
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

func (s *Store) MarkMessageThreadRead(
	ctx context.Context,
	identity MessageThreadIdentity,
) error {
	return s.UpdateMessageThreads(
		ctx,
		[]MessageThreadIdentity{identity},
		MessageThreadMarkRead,
	)
}

func (s *Store) MarkMessageThreadReadByLine(ctx context.Context, lineID, peer string) error {
	return s.MarkMessageThreadRead(ctx, MessageThreadIdentity{
		LineID: lineID,
		Peer:   peer,
	})
}

func upsertHardwareLine(
	ctx context.Context,
	transaction *sql.Tx,
	line HardwareLine,
	observedAt time.Time,
) (string, error) {
	reportedPhoneNumber := strings.TrimSpace(line.ReportedPhoneNumber)
	if reportedPhoneNumber == "" {
		reportedPhoneNumber = strings.TrimSpace(line.PhoneNumber)
	}
	endpointID := strings.TrimSpace(line.ID)
	if endpointID == "" {
		return "", nil
	}
	imei := hardwareLineIMEI(line)
	if imei == "" {
		return "", nil
	}
	observedRegion := phone.CanonicalRegion(line.HomeCountryISO)
	identityLineID, identityErr := resolveStableLineIdentity(
		ctx,
		transaction,
		line.ICCID,
		line.IMSI,
		"",
		"",
	)
	if identityErr != nil && !errors.Is(identityErr, ErrLineNotFound) {
		return "", identityErr
	}
	homeRegion := observedRegion
	if identityErr == nil {
		storedRegion, err := stableLineHomeCountry(ctx, transaction, identityLineID)
		if err != nil {
			return "", err
		}
		if storedRegion != "" {
			homeRegion = storedRegion
		}
	}
	line.PhoneNumber = phone.NetworkSubscriberE164(reportedPhoneNumber, homeRegion)
	line.HomeCountryISO = homeRegion
	provisionalLineIDs, err := resolveLegacyEndpointLines(ctx, transaction, endpointID, imei)
	if err != nil {
		return "", err
	}
	lineID, err := resolveOrCreateStableLine(
		ctx,
		transaction,
		line.ICCID,
		line.IMSI,
		line.PhoneNumber,
		line.HomeCountryISO,
		observedAt,
	)
	if err != nil && !errors.Is(err, ErrLineNotFound) {
		return "", err
	}
	if errors.Is(err, ErrLineNotFound) {
		lineID = ""
	}
	provisionalAliases := uniqueStableLineAliases(lineID, provisionalLineIDs...)
	if lineID != "" && len(provisionalAliases) > 0 {
		if err := mergeStableLines(
			ctx,
			transaction,
			lineID,
			provisionalAliases,
		); err != nil {
			return "", err
		}
	}
	if lineID != "" && len(provisionalLineIDs) > 0 {
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM modemdeck_legacy_endpoint_lines
			 WHERE endpoint_id IN (?, ?)`,
			endpointID,
			imei,
		); err != nil {
			return "", fmt.Errorf("release provisional stable line endpoint: %w", err)
		}
	}
	if lineID != "" {
		homeRegion, established, err := establishStableLineHomeCountry(
			ctx,
			transaction,
			lineID,
			observedRegion,
			line.PhoneNumber,
		)
		if err != nil {
			return "", err
		}
		line.HomeCountryISO = homeRegion
		if established {
			if err := canonicalizeLinePhoneIdentities(
				ctx,
				transaction,
				lineID,
				homeRegion,
			); err != nil {
				return "", err
			}
		}
		if err := ensureLineCallPolicy(ctx, transaction, lineID); err != nil {
			return "", err
		}
	}
	currentICCID := strings.TrimSpace(line.ICCID)
	var signal any
	if line.SignalKnown {
		signal = int64(line.SignalQuality)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sim_cards
		 SET current_imei = '', updated_at = CURRENT_TIMESTAMP
		 WHERE current_imei IN (
			SELECT imei FROM devices WHERE endpoint_id = ? AND imei <> ?
		 );
		 UPDATE devices
		 SET endpoint_id = '', iccid = NULL, sim_inserted = 0,
			updated_at = CURRENT_TIMESTAMP
		 WHERE endpoint_id = ? AND imei <> ?`,
		endpointID,
		imei,
		endpointID,
		imei,
	); err != nil {
		return "", fmt.Errorf("release previous hardware endpoint: %w", err)
	}
	if err := reconcileCurrentSIMAttachment(
		ctx,
		transaction,
		lineID,
		imei,
		currentICCID,
	); err != nil {
		return "", err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO devices (
			imei, endpoint_id, model, firmware, port, iccid, sim_inserted, signal_quality,
			signal_db_m, signal_rsrq, signal_rsrp, last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(imei) DO UPDATE SET
			endpoint_id = excluded.endpoint_id,
			model = excluded.model,
			firmware = excluded.firmware,
			port = excluded.port,
			iccid = excluded.iccid,
			sim_inserted = excluded.sim_inserted,
			signal_quality = excluded.signal_quality,
			signal_db_m = excluded.signal_db_m,
			signal_rsrq = excluded.signal_rsrq,
			signal_rsrp = excluded.signal_rsrp,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		imei,
		endpointID,
		modemidentity.DisplayModel(line.Model, line.Firmware, line.HardwareRevision),
		strings.TrimSpace(line.Firmware),
		strings.TrimSpace(line.PrimaryPort),
		currentICCID,
		currentICCID != "",
		signal,
		signalMetricValue(line.SignalDBM),
		signalMetricValue(line.SignalRSRQ),
		signalMetricValue(line.SignalRSRP),
		databaseTime(observedAt),
		databaseTime(observedAt),
		databaseTime(observedAt),
	); err != nil {
		return "", fmt.Errorf("upsert hardware line device: %w", err)
	}
	if currentICCID == "" {
		return lineID, nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sim_cards (
			iccid, line_id, imsi, operator, current_imei, reg_status_text,
			last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(iccid) DO UPDATE SET
			line_id = excluded.line_id,
			imsi = excluded.imsi,
			operator = excluded.operator,
			current_imei = excluded.current_imei,
			reg_status_text = excluded.reg_status_text,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		currentICCID,
		lineID,
		strings.TrimSpace(line.IMSI),
		strings.TrimSpace(line.Operator),
		imei,
		strings.TrimSpace(line.State),
		databaseTime(observedAt),
		databaseTime(observedAt),
		databaseTime(observedAt),
	); err != nil {
		return "", fmt.Errorf("upsert hardware line SIM: %w", err)
	}
	if strings.TrimSpace(line.IMSI) == "" {
		if err := initializeDefaultLine(ctx, transaction, lineID); err != nil {
			return "", err
		}
		return lineID, nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sim_subscriptions (
			imsi, line_id, current_iccid, phone_number, modem_phone_number, operator,
			home_operator_code, home_country_iso,
			last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(imsi) DO UPDATE SET
			line_id = excluded.line_id,
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
			home_operator_code = CASE
				WHEN excluded.home_operator_code <> '' THEN excluded.home_operator_code
				ELSE sim_subscriptions.home_operator_code
			END,
			home_country_iso = CASE
				WHEN excluded.home_country_iso <> '' THEN excluded.home_country_iso
				ELSE sim_subscriptions.home_country_iso
			END,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		strings.TrimSpace(line.IMSI),
		lineID,
		currentICCID,
		strings.TrimSpace(line.PhoneNumber),
		reportedPhoneNumber,
		strings.TrimSpace(line.Operator),
		strings.TrimSpace(line.HomeOperatorCode),
		strings.ToUpper(strings.TrimSpace(line.HomeCountryISO)),
		databaseTime(observedAt),
		databaseTime(observedAt),
		databaseTime(observedAt),
	); err != nil {
		return "", fmt.Errorf("upsert hardware line subscription: %w", err)
	}
	if err := initializeDefaultLine(ctx, transaction, lineID); err != nil {
		return "", err
	}
	return lineID, nil
}

func hardwareLineIMEI(line HardwareLine) string {
	if imei := strings.TrimSpace(line.EquipmentIdentifier); imei != "" {
		return imei
	}
	return strings.TrimSpace(line.DeviceIdentifier)
}

func reconcileCurrentSIMAttachment(
	ctx context.Context,
	transaction *sql.Tx,
	lineID, imei, currentICCID string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sim_cards
		 SET current_imei = '', updated_at = CURRENT_TIMESTAMP
		 WHERE current_imei = ? AND iccid <> ?`,
		imei,
		currentICCID,
	); err != nil {
		return fmt.Errorf("detach replaced SIM from current modem: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE devices
		 SET iccid = NULL, sim_inserted = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE imei <> ? AND (
			(? <> '' AND COALESCE(iccid, '') = ?) OR
			imei IN (
				SELECT current_imei
				FROM sim_cards
				WHERE line_id = ? AND COALESCE(current_imei, '') <> ''
			)
		 )`,
		imei,
		currentICCID,
		currentICCID,
		lineID,
	); err != nil {
		return fmt.Errorf("detach stable line from previous modem: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sim_cards
		 SET current_imei = '', updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ? AND (
			iccid <> ? OR COALESCE(current_imei, '') <> ?
		 )`,
		lineID,
		currentICCID,
		imei,
	); err != nil {
		return fmt.Errorf("detach previous SIM attachment for stable line: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE devices
		 SET iccid = NULL, sim_inserted = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE ? <> '' AND imei <> ? AND COALESCE(iccid, '') = ?`,
		currentICCID,
		imei,
		currentICCID,
	); err != nil {
		return fmt.Errorf("detach duplicate SIM device attachment: %w", err)
	}
	return nil
}

func initializeDefaultLine(ctx context.Context, transaction *sql.Tx, lineID string) error {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_line_settings
		 SET default_line_id = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND default_line_id = ''`,
		lineID,
	); err != nil {
		return fmt.Errorf("initialize default line: %w", err)
	}
	return nil
}

func rewriteSnapshotLineIdentities(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot *HardwareSnapshot,
	lineIDsByEndpoint map[string]string,
) error {
	homeCountries := make(map[string]string, len(lineIDsByEndpoint))
	lineHomeCountry := func(lineID, observed string) (string, error) {
		if region, exists := homeCountries[lineID]; exists {
			if region != "" {
				return region, nil
			}
			return phone.CanonicalRegion(observed), nil
		}
		region, err := stableLineHomeCountry(ctx, transaction, lineID)
		if err != nil {
			return "", err
		}
		homeCountries[lineID] = region
		if region != "" {
			return region, nil
		}
		return phone.CanonicalRegion(observed), nil
	}
	for index := range snapshot.Messages {
		message := &snapshot.Messages[index]
		endpointLineID := strings.TrimSpace(message.EndpointLineID)
		if endpointLineID == "" {
			endpointLineID = strings.TrimSpace(message.LineID)
		}
		lineID := lineIDsByEndpoint[endpointLineID]
		if lineID == "" {
			var err error
			lineID, endpointLineID, err = resolveStoredHardwareLine(
				ctx,
				transaction,
				message.LineID,
				endpointLineID,
				message.ICCID,
				message.IMSI,
				message.LocalPhone,
				message.HomeCountryISO,
			)
			if err != nil {
				return fmt.Errorf("resolve snapshot message line: %w", err)
			}
		}
		message.LineID = lineID
		message.EndpointLineID = endpointLineID
		homeCountry, err := lineHomeCountry(lineID, message.HomeCountryISO)
		if err != nil {
			return fmt.Errorf("resolve snapshot message home country: %w", err)
		}
		message.HomeCountryISO = homeCountry
	}
	for index := range snapshot.DeliveryReports {
		report := &snapshot.DeliveryReports[index]
		endpointLineID := strings.TrimSpace(report.EndpointLineID)
		if endpointLineID == "" {
			endpointLineID = strings.TrimSpace(report.LineID)
		}
		lineID := lineIDsByEndpoint[endpointLineID]
		if lineID == "" {
			return fmt.Errorf(
				"%w: delivery report line %q is unavailable",
				ErrSnapshotInvalid,
				endpointLineID,
			)
		}
		report.LineID = lineID
		report.EndpointLineID = endpointLineID
		homeCountry, err := lineHomeCountry(lineID, report.HomeCountryISO)
		if err != nil {
			return fmt.Errorf("resolve delivery report home country: %w", err)
		}
		report.HomeCountryISO = homeCountry
	}
	for index := range snapshot.Calls {
		call := &snapshot.Calls[index]
		endpointLineID := strings.TrimSpace(call.EndpointLineID)
		if endpointLineID == "" {
			endpointLineID = strings.TrimSpace(call.LineID)
		}
		lineID := lineIDsByEndpoint[endpointLineID]
		if lineID == "" {
			var err error
			lineID, endpointLineID, err = resolveStoredHardwareLine(
				ctx,
				transaction,
				call.LineID,
				endpointLineID,
				call.LineICCID,
				call.LineIMSI,
				call.LocalPhone,
				call.HomeCountryISO,
			)
			if err != nil {
				return fmt.Errorf("resolve snapshot call line: %w", err)
			}
		}
		call.LineID = lineID
		call.EndpointLineID = endpointLineID
		homeCountry, err := lineHomeCountry(lineID, call.HomeCountryISO)
		if err != nil {
			return fmt.Errorf("resolve snapshot call home country: %w", err)
		}
		call.HomeCountryISO = homeCountry
	}
	return nil
}

func upsertHardwareMessage(
	ctx context.Context,
	transaction *sql.Tx,
	message HardwareMessage,
) (Message, bool, error) {
	message.LineID = strings.TrimSpace(message.LineID)
	message.EndpointLineID = strings.TrimSpace(message.EndpointLineID)
	message.EndpointMessageID = strings.TrimSpace(message.EndpointMessageID)
	message.RequestID = strings.TrimSpace(message.RequestID)
	reportedNumber := strings.TrimSpace(message.ReportedNumber)
	if reportedNumber == "" {
		reportedNumber = strings.TrimSpace(message.Number)
	}
	message.Number = phone.CanonicalNetworkAddress(reportedNumber, message.HomeCountryISO)
	message.LocalPhone = phone.CanonicalNetworkAddress(
		message.LocalPhone,
		message.HomeCountryISO,
	)
	message.Text = strings.TrimSpace(message.Text)
	message.Direction = strings.ToLower(strings.TrimSpace(message.Direction))
	message.State = strings.ToLower(strings.TrimSpace(message.State))
	if message.Direction == "incoming" {
		message.DeliveryStatus = MessageDeliveryUnknown
		message.DeliveryReportRequested = false
		message.DeliveryReportTrackable = false
	} else if message.DeliveryStatus == MessageDeliveryUnknown {
		message.DeliveryStatus = MessageDeliverySubmitted
	}
	if message.LineID == "" || message.EndpointLineID == "" ||
		message.Number == "" || message.Text == "" {
		return Message{}, false, fmt.Errorf("%w: message identity or content is empty", ErrSnapshotInvalid)
	}
	if message.Direction != "incoming" && message.Direction != "outgoing" {
		return Message{}, false, fmt.Errorf("%w: invalid message direction", ErrSnapshotInvalid)
	}
	switch message.DeliveryStatus {
	case MessageDeliveryUnknown,
		MessageDeliverySubmitted,
		MessageDeliveryDelivered,
		MessageDeliveryFailed:
	default:
		return Message{}, false, fmt.Errorf("%w: invalid message delivery status", ErrSnapshotInvalid)
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
			"SELECT id FROM sms WHERE endpoint_line_id = ? AND endpoint_message_id = ?",
			message.EndpointLineID,
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
		var messageReference any
		if message.MessageReferenceKnown {
			messageReference = int64(message.MessageReference)
		}
		result, err := transaction.ExecContext(
			ctx,
			`INSERT INTO sms (
				request_id, line_id, endpoint_line_id, endpoint_message_id, imsi, iccid, peer,
				reported_peer, local_phone, sender, recipient, content, type, status, state,
				delivery_status, message_reference, delivery_report_requested,
				delivery_report_trackable, delivery_report_code,
				failure_code, revision, timestamp, created_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, '', ?, ?, ?)`,
			message.RequestID,
			message.LineID,
			message.EndpointLineID,
			message.EndpointMessageID,
			strings.TrimSpace(message.IMSI),
			strings.TrimSpace(message.ICCID),
			message.Number,
			reportedNumber,
			strings.TrimSpace(message.LocalPhone),
			sender,
			recipient,
			message.Text,
			messageType,
			message.StateCode,
			message.State,
			string(message.DeliveryStatus),
			messageReference,
			message.DeliveryReportRequested,
			message.DeliveryReportTrackable,
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
		existing, err := messageByID(ctx, transaction, existingID)
		if err != nil {
			return Message{}, false, err
		}
		nextPeer := preferGlobalPhoneIdentity(existing.Peer, message.Number)
		nextLocal := preferGlobalPhoneIdentity(
			existing.LocalPhone,
			strings.TrimSpace(message.LocalPhone),
		)
		nextReportedPeer := existing.ReportedPeer
		if nextReportedPeer == "" {
			nextReportedPeer = reportedNumber
		}
		nextSender, nextRecipient := existing.Sender, existing.Recipient
		switch existing.Type {
		case 1:
			nextSender, nextRecipient = nextPeer, nextLocal
		case 2:
			nextSender, nextRecipient = nextLocal, nextPeer
		}
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE sms SET
				request_id = CASE WHEN request_id = '' THEN ? ELSE request_id END,
				endpoint_message_id = CASE WHEN endpoint_message_id = '' THEN ? ELSE endpoint_message_id END,
				peer = ?,
				reported_peer = ?,
				local_phone = ?,
				sender = ?,
				recipient = ?,
				status = CASE WHEN revision <= ? THEN ? ELSE status END,
				state = CASE WHEN revision <= ? THEN ? ELSE state END,
				delivery_status = CASE
					WHEN delivery_status = '' AND ? <> '' THEN ?
					ELSE delivery_status
				END,
				message_reference = CASE WHEN ? THEN ? ELSE message_reference END,
				delivery_report_requested = CASE
					WHEN ? THEN 1 ELSE delivery_report_requested
				END,
				delivery_report_trackable = CASE
					WHEN ? THEN 1 ELSE delivery_report_trackable
				END,
				revision = CASE WHEN revision < ? THEN ? ELSE revision END
			 WHERE id = ?`,
			message.RequestID,
			message.EndpointMessageID,
			nextPeer,
			nextReportedPeer,
			nextLocal,
			nextSender,
			nextRecipient,
			message.Revision,
			message.StateCode,
			message.Revision,
			message.State,
			string(message.DeliveryStatus),
			string(message.DeliveryStatus),
			message.MessageReferenceKnown,
			int64(message.MessageReference),
			message.DeliveryReportRequested,
			message.DeliveryReportTrackable,
			message.Revision,
			message.Revision,
			existingID,
		); err != nil {
			return Message{}, false, fmt.Errorf("update hardware message: %w", err)
		}
		if nextPeer != existing.Peer {
			if err := upgradeLineMessagePeerIdentity(
				ctx,
				transaction,
				existing.LineID,
				existing.Peer,
				nextPeer,
			); err != nil {
				return Message{}, false, err
			}
			if err := mergeMessageThreadPeer(
				ctx,
				transaction,
				existing.LineID,
				existing.Peer,
				nextPeer,
			); err != nil {
				return Message{}, false, err
			}
		}
	}
	stored, err := messageByID(ctx, transaction, existingID)
	return stored, created, err
}

func applyHardwareMessageDeliveryReport(
	ctx context.Context,
	transaction *sql.Tx,
	report HardwareMessageDeliveryReport,
	revision int64,
) (bool, error) {
	report.EndpointReportID = strings.TrimSpace(report.EndpointReportID)
	report.LineID = strings.TrimSpace(report.LineID)
	if report.EndpointReportID == "" || report.LineID == "" {
		return false, fmt.Errorf("%w: invalid delivery report identity", ErrSnapshotInvalid)
	}
	if !report.MessageReferenceKnown || !report.DeliveryStateKnown {
		return false, nil
	}
	if report.Timestamp.IsZero() {
		report.Timestamp = report.ObservedAt
	}
	if report.Timestamp.IsZero() {
		return false, fmt.Errorf("%w: delivery report timestamp is empty", ErrSnapshotInvalid)
	}
	number := phone.CanonicalNetworkAddress(report.Number, report.HomeCountryISO)
	reportTime := databaseTime(report.Timestamp)
	var (
		messageID      int64
		deliveryStatus sql.NullString
		trackable      sql.NullInt64
	)
	err := transaction.QueryRowContext(
		ctx,
		`SELECT id, delivery_status, delivery_report_trackable
		 FROM sms
		 WHERE line_id = ?
			AND type = 2
			AND delivery_report_requested = 1
			AND message_reference = ?
			AND (? = '' OR peer = ?)
			AND datetime(timestamp) <= datetime(?, '+5 minutes')
			AND datetime(timestamp) >= datetime(?, '-30 days')
		 ORDER BY datetime(timestamp) DESC, id DESC
		 LIMIT 1`,
		report.LineID,
		int64(report.MessageReference),
		number,
		number,
		reportTime,
		reportTime,
	).Scan(&messageID, &deliveryStatus, &trackable)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("match SMS delivery report: %w", err)
	}

	current := MessageDeliveryStatus(stringValue(deliveryStatus))
	next := current
	switch {
	case report.DeliveryState == 0x00 &&
		current == MessageDeliverySubmitted &&
		boolValue(trackable):
		next = MessageDeliveryDelivered
	case report.DeliveryState >= 0x40 &&
		report.DeliveryState <= 0x7f &&
		current == MessageDeliverySubmitted:
		next = MessageDeliveryFailed
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sms
		 SET delivery_status = ?,
			delivery_report_code = ?,
			revision = CASE WHEN revision < ? THEN ? ELSE revision END
		 WHERE id = ?`,
		string(next),
		int64(report.DeliveryState),
		revision,
		revision,
		messageID,
	); err != nil {
		return false, fmt.Errorf("apply SMS delivery report: %w", err)
	}
	return true, nil
}

func updateMessageThread(
	ctx context.Context,
	transaction *sql.Tx,
	messageID int64,
	message HardwareMessage,
) error {
	if strings.TrimSpace(message.LineID) == "" {
		return fmt.Errorf("%w: message stable line is empty", ErrSnapshotInvalid)
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
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(line_id, peer) DO UPDATE SET
			imsi = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.imsi ELSE sms_contacts.imsi
			END,
			iccid = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.iccid ELSE sms_contacts.iccid
			END,
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
		strings.TrimSpace(message.LineID),
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

func mergeMessageThreadPeer(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	previousPeer,
	canonicalPeer string,
) error {
	lineID = strings.TrimSpace(lineID)
	previousPeer = strings.TrimSpace(previousPeer)
	canonicalPeer = strings.TrimSpace(canonicalPeer)
	if lineID == "" || previousPeer == "" || canonicalPeer == "" ||
		previousPeer == canonicalPeer {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp,
			last_content, last_type, unread_count, marked_unread, is_favorite,
			created_at, updated_at
		 )
		 SELECT line_id, imsi, iccid, ?, last_sms_id, last_timestamp,
			last_content, last_type, unread_count, marked_unread, is_favorite,
			created_at, updated_at
		 FROM sms_contacts
		 WHERE line_id = ? AND peer = ?
		 ON CONFLICT(line_id, peer) DO UPDATE SET
			imsi = CASE
				WHEN COALESCE(excluded.last_timestamp, '') >=
					COALESCE(sms_contacts.last_timestamp, '')
				THEN excluded.imsi ELSE sms_contacts.imsi
			END,
			iccid = CASE
				WHEN COALESCE(excluded.last_timestamp, '') >=
					COALESCE(sms_contacts.last_timestamp, '')
				THEN excluded.iccid ELSE sms_contacts.iccid
			END,
			last_sms_id = CASE
				WHEN COALESCE(excluded.last_timestamp, '') >=
					COALESCE(sms_contacts.last_timestamp, '')
				THEN excluded.last_sms_id ELSE sms_contacts.last_sms_id
			END,
			last_timestamp = CASE
				WHEN COALESCE(excluded.last_timestamp, '') >=
					COALESCE(sms_contacts.last_timestamp, '')
				THEN excluded.last_timestamp ELSE sms_contacts.last_timestamp
			END,
			last_content = CASE
				WHEN COALESCE(excluded.last_timestamp, '') >=
					COALESCE(sms_contacts.last_timestamp, '')
				THEN excluded.last_content ELSE sms_contacts.last_content
			END,
			last_type = CASE
				WHEN COALESCE(excluded.last_timestamp, '') >=
					COALESCE(sms_contacts.last_timestamp, '')
				THEN excluded.last_type ELSE sms_contacts.last_type
			END,
			unread_count = sms_contacts.unread_count + excluded.unread_count,
			marked_unread = MAX(sms_contacts.marked_unread, excluded.marked_unread),
			is_favorite = MAX(sms_contacts.is_favorite, excluded.is_favorite),
			created_at = CASE
				WHEN sms_contacts.created_at IS NULL THEN excluded.created_at
				WHEN excluded.created_at IS NULL THEN sms_contacts.created_at
				WHEN excluded.created_at < sms_contacts.created_at THEN excluded.created_at
				ELSE sms_contacts.created_at
			END,
			updated_at = CASE
				WHEN sms_contacts.updated_at IS NULL THEN excluded.updated_at
				WHEN excluded.updated_at IS NULL THEN sms_contacts.updated_at
				WHEN excluded.updated_at > sms_contacts.updated_at THEN excluded.updated_at
				ELSE sms_contacts.updated_at
			END`,
		canonicalPeer,
		lineID,
		previousPeer,
	); err != nil {
		return fmt.Errorf("merge message thread phone identity: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM sms_contacts
		 WHERE line_id = ? AND peer = ?`,
		lineID,
		previousPeer,
	); err != nil {
		return fmt.Errorf("remove superseded message thread phone identity: %w", err)
	}
	return nil
}

func upgradeLineMessagePeerIdentity(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	previousPeer,
	canonicalPeer string,
) error {
	lineID = strings.TrimSpace(lineID)
	previousPeer = strings.TrimSpace(previousPeer)
	canonicalPeer = strings.TrimSpace(canonicalPeer)
	if lineID == "" ||
		previousPeer == "" ||
		!strings.HasPrefix(canonicalPeer, "+") ||
		strings.HasPrefix(previousPeer, "+") {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sms
		 SET peer = ?,
			sender = CASE WHEN type = 1 THEN ? ELSE sender END,
			recipient = CASE WHEN type = 2 THEN ? ELSE recipient END
		 WHERE line_id = ? AND peer = ?`,
		canonicalPeer,
		canonicalPeer,
		canonicalPeer,
		lineID,
		previousPeer,
	); err != nil {
		return fmt.Errorf("upgrade line message peer identity: %w", err)
	}
	return nil
}

func preferGlobalPhoneIdentity(existing, observed string) string {
	existing = strings.TrimSpace(existing)
	observed = strings.TrimSpace(observed)
	switch {
	case existing == "":
		return observed
	case strings.HasPrefix(observed, "+") && !strings.HasPrefix(existing, "+"):
		return observed
	default:
		return existing
	}
}

func messageByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64) (Message, error) {
	var (
		message                                                             Message
		requestID, lineID, endpointLineID, endpointID, imsi, iccid, peer    sql.NullString
		reportedPeer, local, sender, recipient, content, state, failureCode sql.NullString
		deliveryStatus                                                      sql.NullString
		timestamp, createdAt                                                sql.NullString
		messageType, status, messageReference, reportRequested              sql.NullInt64
		reportTrackable, reportCode, revision                               sql.NullInt64
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT id, request_id, line_id, endpoint_line_id, endpoint_message_id, imsi, iccid, peer,
			reported_peer, local_phone, sender, recipient, content, type, status, state,
			delivery_status, message_reference, delivery_report_requested,
			delivery_report_trackable, delivery_report_code,
			failure_code, revision, timestamp, created_at
		 FROM sms WHERE id = ?`,
		id,
	).Scan(
		&message.ID,
		&requestID,
		&lineID,
		&endpointLineID,
		&endpointID,
		&imsi,
		&iccid,
		&peer,
		&reportedPeer,
		&local,
		&sender,
		&recipient,
		&content,
		&messageType,
		&status,
		&state,
		&deliveryStatus,
		&messageReference,
		&reportRequested,
		&reportTrackable,
		&reportCode,
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
	if message.Type == 1 {
		message.Direction = "incoming"
	} else if message.Type == 2 {
		message.Direction = "outgoing"
	}
	return message, nil
}

func signalMetricValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func upsertHardwareCall(ctx context.Context, transaction *sql.Tx, call HardwareCall) (Call, error) {
	call.AppID = strings.TrimSpace(call.AppID)
	call.LineID = strings.TrimSpace(call.LineID)
	call.EndpointLineID = strings.TrimSpace(call.EndpointLineID)
	call.EndpointCallID = strings.TrimSpace(call.EndpointCallID)
	reportedNumber := strings.TrimSpace(call.ReportedNumber)
	if reportedNumber == "" {
		reportedNumber = strings.TrimSpace(call.Number)
	}
	call.Number = phone.CanonicalNetworkAddress(reportedNumber, call.HomeCountryISO)
	call.LocalPhone = phone.CanonicalNetworkAddress(call.LocalPhone, call.HomeCountryISO)
	call.Direction = strings.ToLower(strings.TrimSpace(call.Direction))
	call.Phase = strings.ToLower(strings.TrimSpace(call.Phase))
	call.Bearer = strings.TrimSpace(call.Bearer)
	call.StateReason = strings.TrimSpace(call.StateReason)
	call.AudioPort = strings.TrimSpace(call.AudioPort)
	call.AudioEncoding = strings.TrimSpace(call.AudioEncoding)
	call.AudioResolution = strings.TrimSpace(call.AudioResolution)
	if call.AppID == "" || call.LineID == "" || call.EndpointLineID == "" ||
		call.EndpointCallID == "" ||
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
			id, request_id, line_id, endpoint_line_id, local_phone, line_imsi, line_iccid,
			direction, remote_number, reported_remote_number, endpoint_id,
			endpoint_call_id, phase,
			revision, created_at, updated_at, active_at, ended_at, end_reason,
			failure_code, bearer, state_reason, state_reason_code, multiparty,
			audio_port, audio_encoding, audio_resolution, audio_rate,
			media_available
		 ) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?
		 )
		 ON CONFLICT(id) DO UPDATE SET
			local_phone = CASE
				WHEN excluded.local_phone <> '' THEN excluded.local_phone
				ELSE call_history.local_phone
			END,
			line_imsi = CASE
				WHEN excluded.line_imsi <> '' THEN excluded.line_imsi
				ELSE call_history.line_imsi
			END,
			line_iccid = CASE
				WHEN excluded.line_iccid <> '' THEN excluded.line_iccid
				ELSE call_history.line_iccid
			END,
			remote_number = CASE
				WHEN call_history.remote_number = '' AND excluded.remote_number <> ''
					THEN excluded.remote_number
				WHEN excluded.remote_number LIKE '+%'
					AND call_history.remote_number NOT LIKE '+%'
					THEN excluded.remote_number
				ELSE call_history.remote_number
			END,
			reported_remote_number = CASE
				WHEN call_history.reported_remote_number = ''
					AND excluded.reported_remote_number <> ''
					THEN excluded.reported_remote_number
				ELSE call_history.reported_remote_number
			END,
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
		call.EndpointLineID,
		strings.TrimSpace(call.LocalPhone),
		strings.TrimSpace(call.LineIMSI),
		strings.TrimSpace(call.LineICCID),
		call.Direction,
		call.Number,
		reportedNumber,
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
			stored.LineID,
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
		`SELECT id, line_id, endpoint_line_id, remote_number, created_at, direction,
			active_at, end_reason, failure_code
		 FROM call_history
		 WHERE endpoint_id = ? AND phase NOT IN ('ended', 'failed')`,
		modemManagerEndpointID,
	)
	if err != nil {
		return fmt.Errorf("query open hardware calls: %w", err)
	}
	type missingCall struct {
		id, lineID, endpointLineID, peer, createdAt, direction string
		missed                                                 bool
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
			&call.endpointLineID,
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
					revision = CASE
						WHEN revision < ? THEN ?
						ELSE revision + 1
					END,
					audio_port = '',
					audio_encoding = '',
					audio_resolution = '',
					audio_rate = 0,
					media_available = 0
				 WHERE id = ? AND line_id = ? AND endpoint_line_id = ? AND endpoint_id = ?
					AND phase NOT IN ('ended', 'failed')`,
			databaseTime(snapshot.ObservedAt),
			databaseTime(snapshot.ObservedAt),
			sequence,
			sequence,
			call.id,
			call.lineID,
			call.endpointLineID,
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
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_hardware_sync
			 SET observed_at = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE singleton = 1`,
			databaseTime(snapshot.ObservedAt),
		); err != nil {
			return 0, false, fmt.Errorf("advance duplicate hardware snapshot observation: %w", err)
		}
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
		`SELECT id, request_id, line_id, endpoint_line_id, local_phone, line_imsi, line_iccid,
			direction, remote_number, reported_remote_number,
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
		call                                                                      Call
		requestID, lineID, endpointLineID, localPhone, lineIMSI, lineICCID        sql.NullString
		direction, remoteNumber, reportedRemoteNumber, endpointID, endpointCallID sql.NullString
		phase, createdAt, updatedAt, activeAt, endedAt, endReason                 sql.NullString
		failure, bearer, stateReason, audioPort, audioEncoding, audioResolution   sql.NullString
		revision, stateReasonCode, multiparty, audioRate, mediaAvailable          sql.NullInt64
	)
	if err := scanner.Scan(
		&call.ID,
		&requestID,
		&lineID,
		&endpointLineID,
		&localPhone,
		&lineIMSI,
		&lineICCID,
		&direction,
		&remoteNumber,
		&reportedRemoteNumber,
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
	call.LineID = stringValue(lineID)
	call.EndpointLineID = stringValue(endpointLineID)
	call.LocalPhone = stringValue(localPhone)
	call.LineIMSI = stringValue(lineIMSI)
	call.LineICCID = stringValue(lineICCID)
	call.Direction = stringValue(direction)
	call.RemoteNumber = stringValue(remoteNumber)
	call.ReportedRemoteNumber = stringValue(reportedRemoteNumber)
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
