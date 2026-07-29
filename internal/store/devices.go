package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/modemidentity"
)

var (
	ErrDeviceNotFound      = errors.New("device not found")
	ErrDeviceConflict      = errors.New("device already exists")
	ErrDevicePresent       = errors.New("device is currently present")
	ErrDeviceValidation    = errors.New("device validation failed")
	ErrLineNotFound        = errors.New("line not found")
	ErrLineValidation      = errors.New("line validation failed")
	ErrLineColorValidation = errors.New("line color validation failed")
)

const (
	maxDeviceNameLength = 100
	maxLineLabelLength  = 16
	maxLineIDLength     = 64
)

type LineColor string

const (
	LineColorTeal   LineColor = "teal"
	LineColorBlue   LineColor = "blue"
	LineColorIndigo LineColor = "indigo"
	LineColorAmber  LineColor = "amber"
	LineColorOrange LineColor = "orange"
	LineColorRed    LineColor = "red"
	LineColorGreen  LineColor = "green"
	LineColorViolet LineColor = "violet"
)

func (color LineColor) Valid() bool {
	switch color {
	case LineColorTeal,
		LineColorBlue,
		LineColorIndigo,
		LineColorAmber,
		LineColorOrange,
		LineColorRed,
		LineColorGreen,
		LineColorViolet:
		return true
	default:
		return false
	}
}

func (s *Store) CreateDevice(ctx context.Context, input DeviceInput) (Device, error) {
	imei, name, err := normalizeDeviceInput(input)
	if err != nil {
		return Device{}, err
	}
	_, err = s.database.ExecContext(
		ctx,
		`INSERT INTO devices (
			imei, name, created_at, updated_at
		 ) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		imei,
		name,
	)
	if err != nil {
		var exists int
		if lookupErr := s.database.QueryRowContext(
			ctx,
			"SELECT 1 FROM devices WHERE imei = ?",
			imei,
		).Scan(&exists); lookupErr == nil {
			return Device{}, ErrDeviceConflict
		}
		return Device{}, fmt.Errorf("create device: %w", err)
	}
	return s.device(ctx, imei)
}

func (s *Store) RenameDevice(ctx context.Context, imei, name string) (Device, error) {
	imei, _, err := normalizeDeviceInput(DeviceInput{IMEI: imei})
	if err != nil {
		return Device{}, err
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > maxDeviceNameLength {
		return Device{}, fmt.Errorf("%w: name is too long", ErrDeviceValidation)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE devices
		 SET name = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE imei = ?`,
		name,
		imei,
	)
	if err != nil {
		return Device{}, fmt.Errorf("rename device: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Device{}, fmt.Errorf("read renamed device count: %w", err)
	}
	if affected != 1 {
		return Device{}, ErrDeviceNotFound
	}
	return s.device(ctx, imei)
}

func (s *Store) DeleteDevice(ctx context.Context, imei string) error {
	imei, _, err := normalizeDeviceInput(DeviceInput{IMEI: imei})
	if err != nil {
		return err
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device deletion: %w", err)
	}
	defer transaction.Rollback()

	var present int
	err = transaction.QueryRowContext(
		ctx,
		`SELECT CASE WHEN d.last_seen IS NOT NULL AND d.last_seen = (
			SELECT observed_at FROM modemdeck_hardware_sync WHERE singleton = 1
		 ) THEN 1 ELSE 0 END
		 FROM devices d
		 WHERE d.imei = ?`,
		imei,
	).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect device deletion: %w", err)
	}
	if present != 0 {
		return ErrDevicePresent
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sim_cards
		 SET current_imei = '', updated_at = CURRENT_TIMESTAMP
		 WHERE current_imei = ?;
		 DELETE FROM devices WHERE imei = ?`,
		imei,
		imei,
	); err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit device deletion: %w", err)
	}
	return nil
}

func (s *Store) UpdateLineLabel(
	ctx context.Context,
	lineID,
	label string,
	color *LineColor,
) (LineSummary, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || len([]rune(lineID)) > maxLineIDLength {
		return LineSummary{}, fmt.Errorf("%w: line ID is invalid", ErrLineValidation)
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > maxLineLabelLength {
		return LineSummary{}, fmt.Errorf("%w: label is too long", ErrLineValidation)
	}
	var colorValue any
	if color != nil {
		normalized := LineColor(strings.TrimSpace(string(*color)))
		if !normalized.Valid() {
			return LineSummary{}, fmt.Errorf("%w: unsupported preset", ErrLineColorValidation)
		}
		colorValue = string(normalized)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_lines
		 SET line_label = ?, line_color = COALESCE(?, line_color), updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ?`,
		label,
		colorValue,
		lineID,
	)
	if err != nil {
		return LineSummary{}, fmt.Errorf("update line label: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return LineSummary{}, fmt.Errorf("read updated line count: %w", err)
	}
	if affected != 1 {
		return LineSummary{}, ErrLineNotFound
	}
	lines, err := s.Lines(ctx)
	if err != nil {
		return LineSummary{}, err
	}
	for _, line := range lines {
		if line.ID == lineID {
			return line, nil
		}
	}
	return LineSummary{}, ErrLineNotFound
}

func normalizeDeviceInput(input DeviceInput) (string, string, error) {
	imei := strings.TrimSpace(input.IMEI)
	if len(imei) < 8 || len(imei) > 64 {
		return "", "", fmt.Errorf("%w: imei length is invalid", ErrDeviceValidation)
	}
	for _, character := range imei {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == ':' || character == '-' {
			continue
		}
		return "", "", fmt.Errorf("%w: imei contains invalid characters", ErrDeviceValidation)
	}
	name := strings.TrimSpace(input.Name)
	if len([]rune(name)) > maxDeviceNameLength {
		return "", "", fmt.Errorf("%w: name is too long", ErrDeviceValidation)
	}
	return imei, name, nil
}

func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT
		d.imei, d.endpoint_id, d.name, d.model, d.firmware, d.port,
		d.public_ip, d.private_ip, d.public_ipv6, d.private_ipv6,
		d.iccid, d.sim_inserted, d.signal_quality, d.signal_db_m, d.signal_rsrq, d.signal_rsrp,
		d.last_seen,
		CASE WHEN d.last_seen IS NOT NULL AND d.last_seen = (
			SELECT observed_at FROM modemdeck_hardware_sync WHERE singleton = 1
		) THEN 1 ELSE 0 END,
		d.created_at, d.updated_at,
		s.iccid, s.line_id, s.imsi,
		COALESCE(NULLIF(ss.phone_number, ''), NULLIF(ss.modem_phone_number, ''), NULLIF(ss.vowifi_phone_number, ''), ''),
		COALESCE(NULLIF(s.operator, ''), ss.operator, ''),
		COALESCE(ss.home_operator_code, ''), COALESCE(ss.home_country_iso, ''),
		s.current_imei,
		s.reg_status, s.reg_status_text, s.lac, s.cell_id, s.apn, s.ims_status, s.last_seen
		FROM devices d
		LEFT JOIN sim_cards s ON s.iccid = d.iccid
		LEFT JOIN sim_subscriptions ss ON ss.imsi = s.imsi
		ORDER BY LOWER(COALESCE(d.name, '')) ASC, d.imei ASC`)
	if err != nil {
		return nil, fmt.Errorf("query devices: %w", err)
	}
	defer rows.Close()

	devices := make([]Device, 0)
	for rows.Next() {
		var (
			device                                                        Device
			endpointID, name, model, firmware, port                       sql.NullString
			publicIP, privateIP, publicIPv6, privateIPv6, iccid           sql.NullString
			simInserted, signalQuality, signalDBM, signalRSRQ, signalRSRP sql.NullInt64
			present                                                       sql.NullInt64
			lastSeen, createdAt, updatedAt                                sql.NullString
			simICCID, simLineID, simIMSI, phoneNumber, operator           sql.NullString
			homeOperatorCode, homeCountryISO, currentIMEI                 sql.NullString
			regStatus                                                     sql.NullInt64
			regStatusText, lac, cellID, apn                               sql.NullString
			imsStatus                                                     sql.NullInt64
			simLastSeen                                                   sql.NullString
		)
		if err := rows.Scan(
			&device.IMEI, &endpointID, &name, &model, &firmware, &port,
			&publicIP, &privateIP, &publicIPv6, &privateIPv6,
			&iccid, &simInserted, &signalQuality, &signalDBM, &signalRSRQ, &signalRSRP,
			&lastSeen, &present, &createdAt, &updatedAt,
			&simICCID, &simLineID, &simIMSI, &phoneNumber, &operator,
			&homeOperatorCode, &homeCountryISO, &currentIMEI,
			&regStatus, &regStatusText, &lac, &cellID, &apn, &imsStatus, &simLastSeen,
		); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		device.EndpointID = stringValue(endpointID)
		device.Name = stringValue(name)
		device.Firmware = stringValue(firmware)
		device.Model = modemidentity.DisplayModel(stringValue(model), device.Firmware, "")
		device.Port = stringValue(port)
		device.PublicIP = stringValue(publicIP)
		device.PrivateIP = stringValue(privateIP)
		device.PublicIPv6 = stringValue(publicIPv6)
		device.PrivateIPv6 = stringValue(privateIPv6)
		device.CurrentICCID = stringValue(iccid)
		device.SIMInserted = boolValue(simInserted)
		if signalQuality.Valid && signalQuality.Int64 >= 0 {
			value := uint32(signalQuality.Int64)
			device.SignalQuality = &value
		}
		device.SignalDBM = nullableSignalMetric(signalDBM)
		device.SignalRSRQ = nullableSignalMetric(signalRSRQ)
		device.SignalRSRP = nullableSignalMetric(signalRSRP)
		device.LastSeen = stringValue(lastSeen)
		device.Present = boolValue(present)
		device.CreatedAt = stringValue(createdAt)
		device.UpdatedAt = stringValue(updatedAt)
		if stringValue(simICCID) != "" {
			device.SIM = &SIMCard{
				ICCID:            stringValue(simICCID),
				LineID:           stringValue(simLineID),
				IMSI:             stringValue(simIMSI),
				PhoneNumber:      stringValue(phoneNumber),
				Operator:         stringValue(operator),
				HomeOperatorCode: stringValue(homeOperatorCode),
				HomeOperatorName: stringValue(operator),
				HomeCountryISO:   stringValue(homeCountryISO),
				CurrentIMEI:      stringValue(currentIMEI),
				RegStatus:        intValue(regStatus),
				RegStatusText:    stringValue(regStatusText),
				LAC:              stringValue(lac),
				CellID:           stringValue(cellID),
				APN:              stringValue(apn),
				IMSStatus:        intValue(imsStatus),
				LastSeen:         stringValue(simLastSeen),
			}
		}
		devices = append(devices, device)
	}
	return devices, rowsError("read devices", rows.Err())
}

func (s *Store) device(ctx context.Context, imei string) (Device, error) {
	devices, err := s.Devices(ctx)
	if err != nil {
		return Device{}, err
	}
	for _, device := range devices {
		if device.IMEI == imei {
			return device, nil
		}
	}
	return Device{}, ErrDeviceNotFound
}

func (s *Store) Lines(ctx context.Context) ([]LineSummary, error) {
	rows, err := s.database.QueryContext(ctx, `WITH ranked_sim_cards AS (
		SELECT sim_cards.*,
			ROW_NUMBER() OVER (
				PARTITION BY line_id
				ORDER BY
					CASE WHEN current_imei <> '' THEN 0 ELSE 1 END,
					last_seen DESC,
					updated_at DESC,
					iccid
			) AS line_rank
		FROM sim_cards
	),
	ranked_subscriptions AS (
		SELECT sim_subscriptions.*,
			ROW_NUMBER() OVER (
				PARTITION BY line_id
				ORDER BY last_seen DESC, updated_at DESC, imsi
			) AS line_rank
		FROM sim_subscriptions
	)
	SELECT
		lines.line_id,
		COALESCE(devices.endpoint_id, ''),
		COALESCE(sim.iccid, subscription.current_iccid, ''),
		lines.line_label,
		lines.line_color,
		COALESCE(sim.imsi, subscription.imsi, ''),
			lines.phone_number,
			COALESCE(NULLIF(sim.operator, ''), subscription.operator, ''),
			COALESCE(subscription.home_operator_code, ''),
			lines.home_country_iso,
			COALESCE(devices.imei, ''),
			COALESCE(devices.name, '')
	FROM modemdeck_lines lines
	LEFT JOIN ranked_sim_cards sim
		ON sim.line_id = lines.line_id AND sim.line_rank = 1
	LEFT JOIN ranked_subscriptions subscription
		ON subscription.line_id = lines.line_id AND subscription.line_rank = 1
	LEFT JOIN devices
		ON devices.imei = sim.current_imei
		AND devices.last_seen = (
			SELECT observed_at FROM modemdeck_hardware_sync WHERE singleton = 1
		)
	ORDER BY COALESCE(
		NULLIF(lines.line_label, ''),
		NULLIF(lines.phone_number, ''),
		sim.iccid,
		subscription.imsi,
		lines.line_id
	) ASC`)
	if err != nil {
		return nil, fmt.Errorf("query lines: %w", err)
	}
	defer rows.Close()

	lines := make([]LineSummary, 0)
	for rows.Next() {
		var (
			line                                            LineSummary
			lineID, endpointID, iccid, lineLabel, lineColor sql.NullString
			imsi, phone, operator, homeOperatorCode         sql.NullString
			homeCountryISO, deviceIMEI, deviceName          sql.NullString
		)
		if err := rows.Scan(
			&lineID,
			&endpointID,
			&iccid,
			&lineLabel,
			&lineColor,
			&imsi,
			&phone,
			&operator,
			&homeOperatorCode,
			&homeCountryISO,
			&deviceIMEI,
			&deviceName,
		); err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		line.ID = stringValue(lineID)
		line.EndpointID = stringValue(endpointID)
		line.ICCID = stringValue(iccid)
		line.LineLabel = stringValue(lineLabel)
		line.LineColor = LineColor(stringValue(lineColor))
		line.IMSI = stringValue(imsi)
		line.PhoneNumber = stringValue(phone)
		line.Operator = stringValue(operator)
		line.HomeOperatorCode = stringValue(homeOperatorCode)
		line.HomeOperatorName = line.Operator
		line.HomeCountryISO = stringValue(homeCountryISO)
		line.DeviceIMEI = stringValue(deviceIMEI)
		line.DeviceName = stringValue(deviceName)
		lines = append(lines, line)
	}
	return lines, rowsError("read lines", rows.Err())
}
