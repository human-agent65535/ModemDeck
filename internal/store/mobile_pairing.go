package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

var (
	ErrIOSPairingNotAllowed         = errors.New("iOS pairing is not allowed")
	errIOSPairingCredentialNotFound = errors.New("iOS pairing credential not found")
)

type IOSPairingStatus struct {
	Allowed             bool                     `json:"allowed"`
	HasCredential       bool                     `json:"has_credential"`
	CredentialCreatedAt string                   `json:"credential_created_at,omitempty"`
	Paired              bool                     `json:"paired"`
	PairedAt            string                   `json:"paired_at,omitempty"`
	Device              mobilepairing.DeviceInfo `json:"device,omitempty"`
	LastSeenAt          string                   `json:"last_seen_at,omitempty"`
}

func (s *Store) UpdateIOSPushRegistration(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
	registration mobilepairing.PushRegistration,
) error {
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET apns_token = ?,
			 voip_token = ?,
			 push_environment = ?,
			 push_bundle_id = ?,
			 push_updated_at = CURRENT_TIMESTAMP,
			 updated_at = CURRENT_TIMESTAMP
		 WHERE token_digest = ?
			AND activated_at IS NOT NULL
			AND EXISTS (
				SELECT 1
				FROM modemdeck_users AS user
				WHERE user.id = modemdeck_ios_pairing_credentials.user_id
					AND user.enabled = 1
					AND user.ios_pairing_enabled = 1
			)`,
		registration.APNSToken,
		registration.VoIPToken,
		registration.Environment,
		registration.BundleID,
		digest[:],
	)
	if err != nil {
		return fmt.Errorf("store iOS push registration: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read iOS push registration result: %w", err)
	}
	if updated != 1 {
		return errIOSPairingCredentialNotFound
	}
	return nil
}

func (s *Store) ClearIOSPushRegistration(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) error {
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET apns_token = '',
			 voip_token = '',
			 push_bundle_id = '',
			 push_updated_at = CURRENT_TIMESTAMP,
			 updated_at = CURRENT_TIMESTAMP
		 WHERE token_digest = ?`,
		digest[:],
	)
	if err != nil {
		return fmt.Errorf("clear iOS push registration: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read cleared iOS push registration result: %w", err)
	}
	if updated != 1 {
		return errIOSPairingCredentialNotFound
	}
	return nil
}

func (s *Store) IOSPushTargetsForLine(
	ctx context.Context,
	lineID string,
	kind IOSPushTokenKind,
) ([]IOSPushTarget, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || (kind != IOSPushTokenAPNS && kind != IOSPushTokenVoIP) {
		return nil, fmt.Errorf("query iOS push targets: invalid target scope")
	}
	tokenColumn := "credential.apns_token"
	if kind == IOSPushTokenVoIP {
		tokenColumn = "credential.voip_token"
	}
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT credential.user_id, `+tokenColumn+`,
			credential.push_environment, credential.push_bundle_id
		 FROM modemdeck_ios_pairing_credentials AS credential
		 JOIN modemdeck_users AS user
			ON user.id = credential.user_id
				AND user.enabled = 1
				AND user.ios_pairing_enabled = 1
		 JOIN modemdeck_user_lines AS access
			ON access.user_id = user.id
		 WHERE access.line_id = ?
			AND credential.activated_at IS NOT NULL
			AND `+tokenColumn+` <> ''
		 ORDER BY credential.user_id`,
		lineID,
	)
	if err != nil {
		return nil, fmt.Errorf("query iOS push targets: %w", err)
	}
	defer rows.Close()
	targets := make([]IOSPushTarget, 0)
	for rows.Next() {
		var target IOSPushTarget
		if err := rows.Scan(
			&target.UserID,
			&target.Token,
			&target.Environment,
			&target.BundleID,
		); err != nil {
			return nil, fmt.Errorf("scan iOS push target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate iOS push targets: %w", err)
	}
	return targets, nil
}

func (s *Store) IOSPushTargetForUser(
	ctx context.Context,
	userID string,
	kind IOSPushTokenKind,
) (IOSPushTarget, bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || (kind != IOSPushTokenAPNS && kind != IOSPushTokenVoIP) {
		return IOSPushTarget{}, false, fmt.Errorf("query iOS push target: invalid target scope")
	}
	tokenColumn := "credential.apns_token"
	if kind == IOSPushTokenVoIP {
		tokenColumn = "credential.voip_token"
	}
	var target IOSPushTarget
	err := s.database.QueryRowContext(
		ctx,
		`SELECT credential.user_id, `+tokenColumn+`,
			credential.push_environment, credential.push_bundle_id
		 FROM modemdeck_ios_pairing_credentials AS credential
		 JOIN modemdeck_users AS user
			ON user.id = credential.user_id
				AND user.enabled = 1
				AND user.ios_pairing_enabled = 1
		 WHERE credential.user_id = ?
			AND credential.activated_at IS NOT NULL
			AND `+tokenColumn+` <> ''`,
		userID,
	).Scan(
		&target.UserID,
		&target.Token,
		&target.Environment,
		&target.BundleID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPushTarget{}, false, nil
	}
	if err != nil {
		return IOSPushTarget{}, false, fmt.Errorf("query iOS push target: %w", err)
	}
	return target, true, nil
}

func (s *Store) ClearIOSPushToken(
	ctx context.Context,
	userID string,
	kind IOSPushTokenKind,
	token string,
) error {
	userID = strings.TrimSpace(userID)
	token = strings.TrimSpace(token)
	if userID == "" || token == "" || (kind != IOSPushTokenAPNS && kind != IOSPushTokenVoIP) {
		return errors.New("clear iOS push token: invalid token identity")
	}
	tokenColumn := "apns_token"
	if kind == IOSPushTokenVoIP {
		tokenColumn = "voip_token"
	}
	_, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET `+tokenColumn+` = '',
			 push_updated_at = CURRENT_TIMESTAMP,
			 updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ? AND `+tokenColumn+` = ?`,
		userID,
		token,
	)
	if err != nil {
		return fmt.Errorf("clear iOS push token: %w", err)
	}
	return nil
}

func (s *Store) IOSPairingPrincipalByTokenDigest(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) (auth.Principal, bool, error) {
	var (
		principal auth.Principal
		role      string
		pairing   int64
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT
			user.id,
			user.username,
			user.role,
			user.ios_pairing_enabled,
			COALESCE(profile.contact_id, '')
		 FROM modemdeck_ios_pairing_credentials AS credential
		 JOIN modemdeck_users AS user
			ON user.id = credential.user_id
				AND user.enabled = 1
				AND user.ios_pairing_enabled = 1
		 LEFT JOIN modemdeck_user_profile_contacts AS profile
			ON profile.user_id = user.id
		 WHERE credential.token_digest = ?`,
		digest[:],
	).Scan(
		&principal.UserID,
		&principal.Username,
		&role,
		&pairing,
		&principal.ProfileContactID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, false, nil
	}
	if err != nil {
		return auth.Principal{}, false, fmt.Errorf(
			"query iOS pairing principal: %w",
			err,
		)
	}
	principal.Role = auth.Role(role)
	principal.IOSPairingEnabled = pairing != 0

	rows, err := s.database.QueryContext(
		ctx,
		`SELECT line_id
		 FROM modemdeck_user_lines
		 WHERE user_id = ?
		 ORDER BY line_id`,
		principal.UserID,
	)
	if err != nil {
		return auth.Principal{}, false, fmt.Errorf(
			"query iOS pairing line access: %w",
			err,
		)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		if err := rows.Scan(&lineID); err != nil {
			return auth.Principal{}, false, fmt.Errorf(
				"scan iOS pairing line access: %w",
				err,
			)
		}
		principal.AllowedLineIDs = append(
			principal.AllowedLineIDs,
			lineID,
		)
	}
	if err := rows.Err(); err != nil {
		return auth.Principal{}, false, fmt.Errorf(
			"iterate iOS pairing line access: %w",
			err,
		)
	}
	return principal, true, nil
}

func (s *Store) ConfirmIOSPairingCredential(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
	device mobilepairing.DeviceInfo,
) (bool, error) {
	hasDevice := device != (mobilepairing.DeviceInfo{})
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET activated_at = CURRENT_TIMESTAMP,
			device_name = CASE WHEN ? <> '' THEN ? ELSE device_name END,
			device_model = CASE WHEN ? <> '' THEN ? ELSE device_model END,
			device_model_identifier = CASE WHEN ? <> '' THEN ? ELSE device_model_identifier END,
			os_name = CASE WHEN ? <> '' THEN ? ELSE os_name END,
			os_version = CASE WHEN ? <> '' THEN ? ELSE os_version END,
			app_version = CASE WHEN ? <> '' THEN ? ELSE app_version END,
			app_build = CASE WHEN ? <> '' THEN ? ELSE app_build END,
			last_seen_at = CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE last_seen_at END,
			updated_at = CURRENT_TIMESTAMP
		 WHERE token_digest = ?
			AND activated_at IS NULL
			AND EXISTS (
				SELECT 1
				FROM modemdeck_users AS user
				WHERE user.id = modemdeck_ios_pairing_credentials.user_id
					AND user.enabled = 1
					AND user.ios_pairing_enabled = 1
			)`,
		device.Name, device.Name,
		device.Model, device.Model,
		device.ModelIdentifier, device.ModelIdentifier,
		device.OSName, device.OSName,
		device.OSVersion, device.OSVersion,
		device.AppVersion, device.AppVersion,
		device.AppBuild, device.AppBuild,
		hasDevice,
		digest[:],
	)
	if err != nil {
		return false, fmt.Errorf("confirm iOS pairing credential: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf(
			"read iOS pairing confirmation result: %w",
			err,
		)
	}
	if updated > 0 || !hasDevice {
		return updated > 0, nil
	}
	_, err = s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET device_name = CASE WHEN ? <> '' THEN ? ELSE device_name END,
			device_model = CASE WHEN ? <> '' THEN ? ELSE device_model END,
			device_model_identifier = CASE WHEN ? <> '' THEN ? ELSE device_model_identifier END,
			os_name = CASE WHEN ? <> '' THEN ? ELSE os_name END,
			os_version = CASE WHEN ? <> '' THEN ? ELSE os_version END,
			app_version = CASE WHEN ? <> '' THEN ? ELSE app_version END,
			app_build = CASE WHEN ? <> '' THEN ? ELSE app_build END,
			last_seen_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		 WHERE token_digest = ?
			AND activated_at IS NOT NULL
			AND EXISTS (
				SELECT 1
				FROM modemdeck_users AS user
				WHERE user.id = modemdeck_ios_pairing_credentials.user_id
					AND user.enabled = 1
					AND user.ios_pairing_enabled = 1
			)`,
		device.Name, device.Name,
		device.Model, device.Model,
		device.ModelIdentifier, device.ModelIdentifier,
		device.OSName, device.OSName,
		device.OSVersion, device.OSVersion,
		device.AppVersion, device.AppVersion,
		device.AppBuild, device.AppBuild,
		digest[:],
	)
	if err != nil {
		return false, fmt.Errorf("refresh iOS pairing device: %w", err)
	}
	return false, nil
}

func (s *Store) IOSPairingStatus(
	ctx context.Context,
	userID string,
) (IOSPairingStatus, error) {
	var (
		status     IOSPairingStatus
		enabled    int64
		pairing    int64
		createdAt  sql.NullString
		pairedAt   sql.NullString
		lastSeenAt sql.NullString
	)
	err := s.database.QueryRowContext(
		ctx,
		`SELECT
			user.enabled,
			user.ios_pairing_enabled,
			credential.created_at,
			credential.activated_at,
			COALESCE(credential.device_name, ''),
			COALESCE(credential.device_model, ''),
			COALESCE(credential.device_model_identifier, ''),
			COALESCE(credential.os_name, ''),
			COALESCE(credential.os_version, ''),
			COALESCE(credential.app_version, ''),
			COALESCE(credential.app_build, ''),
			credential.last_seen_at
		 FROM modemdeck_users AS user
		 LEFT JOIN modemdeck_ios_pairing_credentials AS credential
			ON credential.user_id = user.id
		 WHERE user.id = ?`,
		userID,
	).Scan(
		&enabled,
		&pairing,
		&createdAt,
		&pairedAt,
		&status.Device.Name,
		&status.Device.Model,
		&status.Device.ModelIdentifier,
		&status.Device.OSName,
		&status.Device.OSVersion,
		&status.Device.AppVersion,
		&status.Device.AppBuild,
		&lastSeenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPairingStatus{}, ErrUserNotFound
	}
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("query iOS pairing status: %w", err)
	}
	status.Allowed = enabled != 0 && pairing != 0
	status.HasCredential = createdAt.Valid
	status.CredentialCreatedAt = iosPairingTimestamp(stringValue(createdAt))
	status.Paired = pairedAt.Valid
	status.PairedAt = iosPairingTimestamp(stringValue(pairedAt))
	status.LastSeenAt = iosPairingTimestamp(stringValue(lastSeenAt))
	return status, nil
}

func (s *Store) RotateIOSPairingCredential(
	ctx context.Context,
	userID string,
	digest mobilepairing.TokenDigest,
) (IOSPairingStatus, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("begin iOS pairing credential update: %w", err)
	}
	defer transaction.Rollback()

	var enabled, pairing int64
	err = transaction.QueryRowContext(
		ctx,
		`SELECT user.enabled, user.ios_pairing_enabled
		 FROM modemdeck_users AS user
		 WHERE user.id = ?`,
		userID,
	).Scan(&enabled, &pairing)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPairingStatus{}, ErrUserNotFound
	}
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("read user before iOS pairing: %w", err)
	}
	if enabled == 0 || pairing == 0 {
		return IOSPairingStatus{}, ErrIOSPairingNotAllowed
	}
	var createdAt string
	if err := transaction.QueryRowContext(
		ctx,
		`INSERT INTO modemdeck_ios_pairing_credentials (
			user_id, token_digest, created_at, updated_at
		 ) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id) DO UPDATE SET
			token_digest = excluded.token_digest,
			activated_at = NULL,
			device_name = '',
			device_model = '',
			device_model_identifier = '',
			os_name = '',
			os_version = '',
			app_version = '',
			app_build = '',
			apns_token = '',
			voip_token = '',
			push_environment = 'development',
			push_bundle_id = '',
			push_updated_at = NULL,
			last_seen_at = NULL,
			created_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		 RETURNING created_at`,
		userID,
		digest[:],
	).Scan(&createdAt); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("store iOS pairing credential: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("commit iOS pairing credential update: %w", err)
	}
	return IOSPairingStatus{
		Allowed:             true,
		HasCredential:       true,
		CredentialCreatedAt: iosPairingTimestamp(createdAt),
	}, nil
}

func (s *Store) RevokeIOSPairingCredential(
	ctx context.Context,
	userID string,
) error {
	_, _, err := s.RevokeIOSPairingCredentialWithDigest(ctx, userID)
	return err
}

func (s *Store) RevokeIOSPairingCredentialWithDigest(
	ctx context.Context,
	userID string,
) (mobilepairing.TokenDigest, bool, error) {
	var value []byte
	err := s.database.QueryRowContext(
		ctx,
		`DELETE FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ?
		 RETURNING token_digest`,
		userID,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return mobilepairing.TokenDigest{}, false, nil
	}
	if err != nil {
		return mobilepairing.TokenDigest{}, false, fmt.Errorf(
			"revoke iOS pairing credential: %w",
			err,
		)
	}
	if len(value) != len(mobilepairing.TokenDigest{}) {
		return mobilepairing.TokenDigest{}, false, fmt.Errorf(
			"revoke iOS pairing credential: invalid digest length %d",
			len(value),
		)
	}
	var digest mobilepairing.TokenDigest
	copy(digest[:], value)
	return digest, true, nil
}

func iosPairingTimestamp(value string) string {
	parsed, ok := parseDatabaseTime(value)
	if !ok {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}
