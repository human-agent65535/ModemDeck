package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrDeviceNotFound   = errors.New("device not found")
	ErrDeviceConflict   = errors.New("device already exists")
	ErrDeviceValidation = errors.New("device validation failed")
	ErrLineNotFound     = errors.New("line not found")
	ErrLineValidation   = errors.New("line validation failed")
)

const (
	maxDeviceAliasLength = 100
	maxLineLabelLength   = 16
	maxLineICCIDLength   = 64
)

func (s *Store) CreateDevice(ctx context.Context, input DeviceInput) (Device, error) {
	imei, alias, err := normalizeDeviceInput(input)
	if err != nil {
		return Device{}, err
	}
	_, err = s.database.ExecContext(
		ctx,
		`INSERT INTO devices (
			imei, alias, created_at, updated_at
		 ) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		imei,
		alias,
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

func (s *Store) RenameDevice(ctx context.Context, imei, alias string) (Device, error) {
	imei, _, err := normalizeDeviceInput(DeviceInput{IMEI: imei})
	if err != nil {
		return Device{}, err
	}
	alias = strings.TrimSpace(alias)
	if len([]rune(alias)) > maxDeviceAliasLength {
		return Device{}, fmt.Errorf("%w: alias is too long", ErrDeviceValidation)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE devices
		 SET alias = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE imei = ?`,
		alias,
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

func (s *Store) UpdateLineLabel(ctx context.Context, iccid, label string) (LineSummary, error) {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" || len([]rune(iccid)) > maxLineICCIDLength {
		return LineSummary{}, fmt.Errorf("%w: ICCID is invalid", ErrLineValidation)
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > maxLineLabelLength {
		return LineSummary{}, fmt.Errorf("%w: label is too long", ErrLineValidation)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE sim_cards
		 SET line_label = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE iccid = ?`,
		label,
		iccid,
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
		if line.ICCID == iccid {
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
	alias := strings.TrimSpace(input.Alias)
	if len([]rune(alias)) > maxDeviceAliasLength {
		return "", "", fmt.Errorf("%w: alias is too long", ErrDeviceValidation)
	}
	return imei, alias, nil
}

func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT
		d.imei, d.alias, d.model, d.firmware, d.port,
		d.public_ip, d.private_ip, d.public_ipv6, d.private_ipv6,
			d.iccid, d.sim_inserted, d.signal_quality, d.signal_db_m, d.signal_rsrq, d.signal_rsrp,
		d.last_seen, d.created_at, d.updated_at,
		s.iccid, s.imsi,
		COALESCE(NULLIF(ss.phone_number, ''), NULLIF(ss.modem_phone_number, ''), NULLIF(ss.vowifi_phone_number, ''), ''),
		COALESCE(NULLIF(s.operator, ''), ss.operator, ''), s.current_imei,
		s.reg_status, s.reg_status_text, s.lac, s.cell_id, s.apn, s.ims_status, s.last_seen
		FROM devices d
		LEFT JOIN sim_cards s ON s.iccid = d.iccid
		LEFT JOIN sim_subscriptions ss ON ss.imsi = s.imsi
		ORDER BY LOWER(COALESCE(d.alias, '')) ASC, d.imei ASC`)
	if err != nil {
		return nil, fmt.Errorf("query devices: %w", err)
	}
	defer rows.Close()

	devices := make([]Device, 0)
	for rows.Next() {
		var (
			device                                                        Device
			alias, model, firmware, port                                  sql.NullString
			publicIP, privateIP, publicIPv6, privateIPv6, iccid           sql.NullString
			simInserted, signalQuality, signalDBM, signalRSRQ, signalRSRP sql.NullInt64
			lastSeen, createdAt, updatedAt                                sql.NullString
			simICCID, simIMSI, phoneNumber, operator, currentIMEI         sql.NullString
			regStatus                                                     sql.NullInt64
			regStatusText, lac, cellID, apn                               sql.NullString
			imsStatus                                                     sql.NullInt64
			simLastSeen                                                   sql.NullString
		)
		if err := rows.Scan(
			&device.IMEI, &alias, &model, &firmware, &port,
			&publicIP, &privateIP, &publicIPv6, &privateIPv6,
			&iccid, &simInserted, &signalQuality, &signalDBM, &signalRSRQ, &signalRSRP,
			&lastSeen, &createdAt, &updatedAt,
			&simICCID, &simIMSI, &phoneNumber, &operator, &currentIMEI,
			&regStatus, &regStatusText, &lac, &cellID, &apn, &imsStatus, &simLastSeen,
		); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		device.Alias = stringValue(alias)
		device.Model = stringValue(model)
		device.Firmware = stringValue(firmware)
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
		device.SignalDBM = intValue(signalDBM)
		device.SignalRSRQ = intValue(signalRSRQ)
		device.SignalRSRP = intValue(signalRSRP)
		device.LastSeen = stringValue(lastSeen)
		device.CreatedAt = stringValue(createdAt)
		device.UpdatedAt = stringValue(updatedAt)
		if stringValue(simICCID) != "" {
			device.SIM = &SIMCard{
				ICCID:            stringValue(simICCID),
				IMSI:             stringValue(simIMSI),
				PhoneNumber:      stringValue(phoneNumber),
				Operator:         stringValue(operator),
				HomeOperatorName: stringValue(operator),
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
	rows, err := s.database.QueryContext(ctx, `WITH line_rows AS (
		SELECT
			s.iccid AS iccid,
			s.line_label AS line_label,
			s.imsi AS imsi,
			COALESCE(NULLIF(ss.phone_number, ''), NULLIF(ss.modem_phone_number, ''), NULLIF(ss.vowifi_phone_number, ''), '') AS phone_number,
			COALESCE(NULLIF(s.operator, ''), ss.operator, '') AS operator,
			COALESCE(NULLIF(s.current_imei, ''), (
				SELECT devices.imei FROM devices WHERE devices.iccid = s.iccid ORDER BY devices.imei LIMIT 1
			), '') AS device_imei
		FROM sim_cards s
		LEFT JOIN sim_subscriptions ss ON ss.imsi = s.imsi
		UNION ALL
		SELECT
			ss.current_iccid,
			COALESCE((SELECT sim_cards.line_label FROM sim_cards WHERE sim_cards.iccid = ss.current_iccid), ''),
			ss.imsi,
			COALESCE(NULLIF(ss.phone_number, ''), NULLIF(ss.modem_phone_number, ''), NULLIF(ss.vowifi_phone_number, ''), ''),
			ss.operator,
			COALESCE((SELECT devices.imei FROM devices WHERE devices.iccid = ss.current_iccid ORDER BY devices.imei LIMIT 1), '')
		FROM sim_subscriptions ss
		WHERE NOT EXISTS (SELECT 1 FROM sim_cards WHERE sim_cards.imsi = ss.imsi)
	)
	SELECT line_rows.iccid, line_rows.line_label, line_rows.imsi, line_rows.phone_number,
		line_rows.operator, line_rows.device_imei, COALESCE(devices.alias, '')
	FROM line_rows
	LEFT JOIN devices ON devices.imei = line_rows.device_imei
	ORDER BY COALESCE(NULLIF(line_rows.phone_number, ''), line_rows.iccid, line_rows.imsi) ASC`)
	if err != nil {
		return nil, fmt.Errorf("query lines: %w", err)
	}
	defer rows.Close()

	lines := make([]LineSummary, 0)
	for rows.Next() {
		var (
			line                                                             LineSummary
			iccid, lineLabel, imsi, phone, operator, deviceIMEI, deviceAlias sql.NullString
		)
		if err := rows.Scan(
			&iccid,
			&lineLabel,
			&imsi,
			&phone,
			&operator,
			&deviceIMEI,
			&deviceAlias,
		); err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		line.ICCID = stringValue(iccid)
		line.LineLabel = stringValue(lineLabel)
		line.IMSI = stringValue(imsi)
		line.PhoneNumber = stringValue(phone)
		line.Operator = stringValue(operator)
		line.HomeOperatorName = line.Operator
		line.DeviceIMEI = stringValue(deviceIMEI)
		line.DeviceAlias = stringValue(deviceAlias)
		lines = append(lines, line)
	}
	return lines, rowsError("read lines", rows.Err())
}
