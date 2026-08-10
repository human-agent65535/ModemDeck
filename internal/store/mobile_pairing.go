package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

var (
	ErrIOSPairingNotAllowed         = errors.New("iOS pairing is not allowed")
	ErrIOSPairingDeviceLimit        = errors.New("iOS pairing device limit reached")
	ErrIOSPairingCredentialNotFound = errors.New("iOS pairing credential not found")
)

const MaxIOSPairingDevices = 3

type IOSPairingDevice struct {
	ID                  string                   `json:"id"`
	CredentialCreatedAt string                   `json:"credential_created_at"`
	PairedAt            string                   `json:"paired_at"`
	Device              mobilepairing.DeviceInfo `json:"device,omitempty"`
	LastSeenAt          string                   `json:"last_seen_at,omitempty"`
}

type IOSPairingPendingCredential struct {
	ID                  string `json:"id"`
	CredentialCreatedAt string `json:"credential_created_at"`
}

type IOSPairingStatus struct {
	Allowed             bool                         `json:"allowed"`
	HasCredential       bool                         `json:"has_credential"`
	CredentialCreatedAt string                       `json:"credential_created_at,omitempty"`
	Paired              bool                         `json:"paired"`
	PairedAt            string                       `json:"paired_at,omitempty"`
	Device              mobilepairing.DeviceInfo     `json:"device,omitempty"`
	LastSeenAt          string                       `json:"last_seen_at,omitempty"`
	Devices             []IOSPairingDevice           `json:"devices"`
	Pending             *IOSPairingPendingCredential `json:"pending,omitempty"`
	DeviceLimit         int                          `json:"device_limit"`
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
		return ErrIOSPairingCredentialNotFound
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
		return ErrIOSPairingCredentialNotFound
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
		`SELECT credential.id, credential.user_id, `+tokenColumn+`,
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
		 ORDER BY credential.user_id, credential.id`,
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
			&target.CredentialID,
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

func (s *Store) IOSPushTargetForCredential(
	ctx context.Context,
	userID, credentialID string,
	kind IOSPushTokenKind,
) (IOSPushTarget, bool, error) {
	userID = strings.TrimSpace(userID)
	credentialID = strings.TrimSpace(credentialID)
	if userID == "" || credentialID == "" ||
		(kind != IOSPushTokenAPNS && kind != IOSPushTokenVoIP) {
		return IOSPushTarget{}, false, fmt.Errorf("query iOS push target: invalid target scope")
	}
	tokenColumn := "credential.apns_token"
	if kind == IOSPushTokenVoIP {
		tokenColumn = "credential.voip_token"
	}
	var target IOSPushTarget
	err := s.database.QueryRowContext(
		ctx,
		`SELECT credential.id, credential.user_id, `+tokenColumn+`,
			credential.push_environment, credential.push_bundle_id
		 FROM modemdeck_ios_pairing_credentials AS credential
		 JOIN modemdeck_users AS user
			ON user.id = credential.user_id
				AND user.enabled = 1
				AND user.ios_pairing_enabled = 1
		 WHERE credential.user_id = ?
			AND credential.id = ?
			AND credential.activated_at IS NOT NULL
			AND `+tokenColumn+` <> ''`,
		userID,
		credentialID,
	).Scan(
		&target.CredentialID,
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
	userID = strings.TrimSpace(userID)
	var enabled, pairing int64
	err := s.database.QueryRowContext(
		ctx,
		`SELECT enabled, ios_pairing_enabled
		 FROM modemdeck_users
		 WHERE id = ?`,
		userID,
	).Scan(&enabled, &pairing)
	if errors.Is(err, sql.ErrNoRows) {
		return IOSPairingStatus{}, ErrUserNotFound
	}
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("query iOS pairing status: %w", err)
	}
	status := IOSPairingStatus{
		Allowed:     enabled != 0 && pairing != 0,
		Devices:     []IOSPairingDevice{},
		DeviceLimit: MaxIOSPairingDevices,
	}
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT
			id,
			created_at,
			activated_at,
			device_name,
			device_model,
			device_model_identifier,
			os_name,
			os_version,
			app_version,
			app_build,
			last_seen_at
		 FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ?
		 ORDER BY activated_at IS NULL DESC,
			COALESCE(last_seen_at, activated_at, created_at) DESC,
			created_at DESC,
			id`,
		userID,
	)
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("query iOS pairing credentials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			device      IOSPairingDevice
			createdAt   string
			activatedAt sql.NullString
			lastSeenAt  sql.NullString
		)
		if err := rows.Scan(
			&device.ID,
			&createdAt,
			&activatedAt,
			&device.Device.Name,
			&device.Device.Model,
			&device.Device.ModelIdentifier,
			&device.Device.OSName,
			&device.Device.OSVersion,
			&device.Device.AppVersion,
			&device.Device.AppBuild,
			&lastSeenAt,
		); err != nil {
			return IOSPairingStatus{}, fmt.Errorf("scan iOS pairing credential: %w", err)
		}
		device.CredentialCreatedAt = iosPairingTimestamp(createdAt)
		if !activatedAt.Valid {
			status.Pending = &IOSPairingPendingCredential{
				ID:                  device.ID,
				CredentialCreatedAt: device.CredentialCreatedAt,
			}
			continue
		}
		device.PairedAt = iosPairingTimestamp(stringValue(activatedAt))
		device.LastSeenAt = iosPairingTimestamp(stringValue(lastSeenAt))
		status.Devices = append(status.Devices, device)
	}
	if err := rows.Err(); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("iterate iOS pairing credentials: %w", err)
	}
	status.HasCredential = status.Pending != nil || len(status.Devices) > 0
	if status.Pending != nil {
		status.CredentialCreatedAt = status.Pending.CredentialCreatedAt
	}
	if len(status.Devices) > 0 {
		primary := status.Devices[0]
		status.Paired = true
		status.PairedAt = primary.PairedAt
		status.Device = primary.Device
		status.LastSeenAt = primary.LastSeenAt
		if status.CredentialCreatedAt == "" {
			status.CredentialCreatedAt = primary.CredentialCreatedAt
		}
	}
	return status, nil
}

func (s *Store) CreateIOSPairingCredential(
	ctx context.Context,
	userID string,
	digest mobilepairing.TokenDigest,
) (IOSPairingStatus, error) {
	userID = strings.TrimSpace(userID)
	credentialID, err := newIOSPairingCredentialID()
	if err != nil {
		return IOSPairingStatus{}, fmt.Errorf("generate iOS pairing credential id: %w", err)
	}
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
	var activeCount int
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ? AND activated_at IS NOT NULL`,
		userID,
	).Scan(&activeCount); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("count paired iOS devices: %w", err)
	}
	if activeCount >= MaxIOSPairingDevices {
		return IOSPairingStatus{}, ErrIOSPairingDeviceLimit
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ? AND activated_at IS NULL`,
		userID,
	); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("replace pending iOS pairing credential: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_ios_pairing_credentials (
			id, user_id, token_digest, created_at, updated_at
		 ) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		credentialID,
		userID,
		digest[:],
	); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("store iOS pairing credential: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return IOSPairingStatus{}, fmt.Errorf("commit iOS pairing credential update: %w", err)
	}
	return s.IOSPairingStatus(ctx, userID)
}

func (s *Store) IOSPairingCredentialIDByTokenDigest(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) (string, bool, error) {
	var credentialID string
	err := s.database.QueryRowContext(
		ctx,
		`SELECT id
		 FROM modemdeck_ios_pairing_credentials
		 WHERE token_digest = ?`,
		digest[:],
	).Scan(&credentialID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query iOS pairing credential id: %w", err)
	}
	return credentialID, true, nil
}

func (s *Store) RevokeIOSPairingCredentialWithDigest(
	ctx context.Context,
	userID, credentialID string,
) (mobilepairing.TokenDigest, bool, error) {
	userID = strings.TrimSpace(userID)
	credentialID = strings.TrimSpace(credentialID)
	if userID == "" || credentialID == "" {
		return mobilepairing.TokenDigest{}, false, ErrIOSPairingCredentialNotFound
	}
	var value []byte
	err := s.database.QueryRowContext(
		ctx,
		`DELETE FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ? AND id = ?
		 RETURNING token_digest`,
		userID,
		credentialID,
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

func (s *Store) RevokeIOSPairingCredentialByTokenDigest(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) (bool, error) {
	result, err := s.database.ExecContext(
		ctx,
		`DELETE FROM modemdeck_ios_pairing_credentials
		 WHERE token_digest = ?`,
		digest[:],
	)
	if err != nil {
		return false, fmt.Errorf("revoke current iOS pairing credential: %w", err)
	}
	revoked, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read current iOS pairing revocation: %w", err)
	}
	return revoked == 1, nil
}

func (s *Store) RevokeAllIOSPairingCredentials(
	ctx context.Context,
	userID string,
) ([]mobilepairing.TokenDigest, error) {
	userID = strings.TrimSpace(userID)
	rows, err := s.database.QueryContext(
		ctx,
		`DELETE FROM modemdeck_ios_pairing_credentials
		 WHERE user_id = ?
		 RETURNING token_digest`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("revoke all iOS pairing credentials: %w", err)
	}
	defer rows.Close()
	digests := make([]mobilepairing.TokenDigest, 0)
	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan revoked iOS pairing credential: %w", err)
		}
		if len(value) != len(mobilepairing.TokenDigest{}) {
			return nil, fmt.Errorf(
				"revoke all iOS pairing credentials: invalid digest length %d",
				len(value),
			)
		}
		var digest mobilepairing.TokenDigest
		copy(digest[:], value)
		digests = append(digests, digest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revoked iOS pairing credentials: %w", err)
	}
	return digests, nil
}

func newIOSPairingCredentialID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "ios-" + base64.RawURLEncoding.EncodeToString(random[:]), nil
}

func iosPairingTimestamp(value string) string {
	parsed, ok := parseDatabaseTime(value)
	if !ok {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}
