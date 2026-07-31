package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

var (
	ErrUserNotFound         = errors.New("user not found")
	ErrUserRevisionConflict = errors.New("user revision conflict")
	ErrUserValidation       = errors.New("user validation failed")
	ErrUsernameConflict     = errors.New("username conflict")
)

type User struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Role     auth.Role `json:"role"`
	Enabled  bool      `json:"enabled"`
	// Kept false for compatibility with clients from before the policy was removed.
	MustChangePassword            bool     `json:"must_change_password"`
	IOSPairingEnabled             bool     `json:"ios_pairing_enabled"`
	IOSPairingHasCredential       bool     `json:"ios_pairing_has_credential"`
	IOSPairingCredentialCreatedAt string   `json:"ios_pairing_credential_created_at,omitempty"`
	IOSPairingPaired              bool     `json:"ios_pairing_paired"`
	IOSPairingPairedAt            string   `json:"ios_pairing_paired_at,omitempty"`
	Revision                      int64    `json:"revision"`
	ProfileName                   string   `json:"profile_name,omitempty"`
	ProfileAvatar                 string   `json:"profile_avatar,omitempty"`
	LineIDs                       []string `json:"line_ids"`
	CreatedAt                     string   `json:"created_at"`
	UpdatedAt                     string   `json:"updated_at"`
}

type CreateMemberInput struct {
	Username          string
	PasswordHash      string
	IOSPairingEnabled bool
	LineIDs           []string
}

type UpdateMemberInput struct {
	Username          string
	PasswordHash      string
	Enabled           bool
	IOSPairingEnabled bool
	LineIDs           []string
	Revision          int64
}

type UserRevisionConflictError struct {
	UserID           string
	ExpectedRevision int64
	ActualRevision   int64
}

func (e *UserRevisionConflictError) Error() string {
	return fmt.Sprintf(
		"%s: %s expected %d, actual %d",
		ErrUserRevisionConflict,
		e.UserID,
		e.ExpectedRevision,
		e.ActualRevision,
	)
}

func (e *UserRevisionConflictError) Unwrap() error {
	return ErrUserRevisionConflict
}

func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT
			user.id,
			user.username,
			user.role,
			user.enabled,
			user.ios_pairing_enabled,
			credential.created_at,
			credential.activated_at,
			user.revision,
			COALESCE(contact.display_name, ''),
			COALESCE(contact.avatar, ''),
			user.created_at,
			user.updated_at
		FROM modemdeck_users AS user
		LEFT JOIN modemdeck_ios_pairing_credentials AS credential
			ON credential.user_id = user.id
		LEFT JOIN modemdeck_user_profile_contacts AS profile
			ON profile.user_id = user.id
		LEFT JOIN contacts AS contact
			ON contact.id = profile.contact_id
		ORDER BY user.role = 'admin' DESC, LOWER(user.username), user.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		var (
			user                User
			role                string
			enabled             int64
			iosPairing          int64
			credentialCreatedAt sql.NullString
			pairedAt            sql.NullString
		)
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&role,
			&enabled,
			&iosPairing,
			&credentialCreatedAt,
			&pairedAt,
			&user.Revision,
			&user.ProfileName,
			&user.ProfileAvatar,
			&user.CreatedAt,
			&user.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		user.Role = auth.Role(role)
		user.Enabled = enabled != 0
		user.IOSPairingEnabled = iosPairing != 0
		user.IOSPairingHasCredential = credentialCreatedAt.Valid
		user.IOSPairingCredentialCreatedAt = iosPairingTimestamp(
			stringValue(credentialCreatedAt),
		)
		user.IOSPairingPaired = pairedAt.Valid
		user.IOSPairingPairedAt = iosPairingTimestamp(stringValue(pairedAt))
		user.LineIDs = []string{}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	if err := s.loadUserLines(ctx, users); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *Store) CreateMember(ctx context.Context, input CreateMemberInput) (User, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" || input.PasswordHash == "" {
		return User{}, ErrUserValidation
	}
	lineIDs, err := normalizedUserLineIDs(input.LineIDs)
	if err != nil {
		return User{}, err
	}
	userID, err := newUserIdentifier()
	if err != nil {
		return User{}, fmt.Errorf("generate user id: %w", err)
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin member creation: %w", err)
	}
	defer transaction.Rollback()
	if err := requireUserLines(ctx, transaction, lineIDs); err != nil {
		return User{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO modemdeck_users (
			id, username, password_hash, role, enabled, ios_pairing_enabled
		) VALUES (?, ?, ?, 'member', 1, ?)
	`, userID, username, input.PasswordHash, input.IOSPairingEnabled); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUsernameConflict
		}
		return User{}, fmt.Errorf("insert member: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_user_preferences (user_id, default_line_id)
		 VALUES (?, ?)`,
		userID,
		firstString(lineIDs),
	); err != nil {
		return User{}, fmt.Errorf("insert member preferences: %w", err)
	}
	if err := replaceUserLines(ctx, transaction, userID, lineIDs); err != nil {
		return User{}, err
	}
	if err := transaction.Commit(); err != nil {
		return User{}, fmt.Errorf("commit member creation: %w", err)
	}
	return s.User(ctx, userID)
}

func (s *Store) User(ctx context.Context, userID string) (User, error) {
	users, err := s.Users(ctx)
	if err != nil {
		return User{}, err
	}
	for _, user := range users {
		if user.ID == userID {
			return user, nil
		}
	}
	return User{}, ErrUserNotFound
}

func (s *Store) UpdateMember(
	ctx context.Context,
	userID string,
	input UpdateMemberInput,
) (User, error) {
	username := strings.TrimSpace(input.Username)
	if userID == "" || username == "" || input.Revision <= 0 {
		return User{}, ErrUserValidation
	}
	lineIDs, err := normalizedUserLineIDs(input.LineIDs)
	if err != nil {
		return User{}, err
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin member update: %w", err)
	}
	defer transaction.Rollback()
	if err := requireUserLines(ctx, transaction, lineIDs); err != nil {
		return User{}, err
	}
	var (
		currentUsername      string
		role                 string
		currentDefaultLineID string
	)
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT user.username, user.role,
			COALESCE(preference.default_line_id, '')
		 FROM modemdeck_users AS user
		 LEFT JOIN modemdeck_user_preferences AS preference
			ON preference.user_id = user.id
		 WHERE user.id = ?`,
		userID,
	).Scan(
		&currentUsername,
		&role,
		&currentDefaultLineID,
	); errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	} else if err != nil {
		return User{}, fmt.Errorf("read user before update: %w", err)
	}
	defaultLineID := currentDefaultLineID
	if !containsString(lineIDs, defaultLineID) {
		defaultLineID = firstString(lineIDs)
	}
	var result sql.Result
	if auth.Role(role) == auth.RoleAdmin {
		if userID != auth.InitialAdminUserID ||
			username != currentUsername ||
			!input.Enabled ||
			input.PasswordHash != "" {
			return User{}, ErrUserValidation
		}
		result, err = transaction.ExecContext(ctx, `
			UPDATE modemdeck_users
			SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND role = 'admin' AND revision = ?
		`, userID, input.Revision)
	} else if input.PasswordHash == "" {
		result, err = transaction.ExecContext(ctx, `
			UPDATE modemdeck_users
			SET username = ?, enabled = ?, ios_pairing_enabled = ?,
				revision = revision + 1,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND role = 'member' AND revision = ?
		`,
			username,
			input.Enabled,
			input.IOSPairingEnabled,
			userID,
			input.Revision,
		)
	} else {
		result, err = transaction.ExecContext(ctx, `
			UPDATE modemdeck_users
			SET username = ?, password_hash = ?, enabled = ?,
				ios_pairing_enabled = ?, revision = revision + 1,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND role = 'member' AND revision = ?
		`,
			username,
			input.PasswordHash,
			input.Enabled,
			input.IOSPairingEnabled,
			userID,
			input.Revision,
		)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUsernameConflict
		}
		return User{}, fmt.Errorf("update member: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("read updated member count: %w", err)
	}
	if affected != 1 {
		return User{}, userWriteMiss(ctx, transaction, userID, input.Revision)
	}
	if err := replaceUserLines(ctx, transaction, userID, lineIDs); err != nil {
		return User{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE modemdeck_user_preferences
		SET default_line_id = ?, revision = revision + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ?
	`, defaultLineID, userID); err != nil {
		return User{}, fmt.Errorf("update member preferences: %w", err)
	}
	if auth.Role(role) == auth.RoleMember &&
		(!input.Enabled || input.PasswordHash != "") {
		if _, err := transaction.ExecContext(
			ctx,
			"DELETE FROM modemdeck_auth_sessions WHERE user_id = ?",
			userID,
		); err != nil {
			return User{}, fmt.Errorf("revoke member sessions after update: %w", err)
		}
	}
	if auth.Role(role) == auth.RoleMember &&
		(!input.Enabled || !input.IOSPairingEnabled || input.PasswordHash != "") {
		if _, err := transaction.ExecContext(
			ctx,
			"DELETE FROM modemdeck_ios_pairing_credentials WHERE user_id = ?",
			userID,
		); err != nil {
			return User{}, fmt.Errorf("revoke disabled iOS pairing credential: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return User{}, fmt.Errorf("commit member update: %w", err)
	}
	return s.User(ctx, userID)
}

func (s *Store) SetMemberPassword(
	ctx context.Context,
	userID, passwordHash string,
) error {
	if userID == "" || passwordHash == "" {
		return ErrUserValidation
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin member password update: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `
		UPDATE modemdeck_users
		SET password_hash = ?, revision = revision + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND role = 'member'
	`, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("set member password: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated member password count: %w", err)
	}
	if affected != 1 {
		return ErrUserNotFound
	}
	if _, err := transaction.ExecContext(
		ctx,
		"DELETE FROM modemdeck_auth_sessions WHERE user_id = ?",
		userID,
	); err != nil {
		return fmt.Errorf("revoke member sessions after password update: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		"DELETE FROM modemdeck_ios_pairing_credentials WHERE user_id = ?",
		userID,
	); err != nil {
		return fmt.Errorf("revoke iOS pairing after password update: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit member password update: %w", err)
	}
	return nil
}

func (s *Store) SetProfileContact(ctx context.Context, contactID string) error {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return ErrUserNotFound
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		_, err := s.database.ExecContext(
			ctx,
			"DELETE FROM modemdeck_user_profile_contacts WHERE user_id = ?",
			principal.UserID,
		)
		return err
	}
	var ownerUserID string
	err := s.database.QueryRowContext(
		ctx,
		"SELECT owner_user_id FROM contacts WHERE id = ?",
		contactID,
	).Scan(&ownerUserID)
	if errors.Is(err, sql.ErrNoRows) || ownerUserID != principal.UserID {
		return ErrContactNotFound
	}
	if err != nil {
		return fmt.Errorf("query profile contact: %w", err)
	}
	_, err = s.database.ExecContext(ctx, `
		INSERT INTO modemdeck_user_profile_contacts (
			user_id, contact_id, updated_at
		) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			contact_id = excluded.contact_id,
			updated_at = CURRENT_TIMESTAMP
	`, principal.UserID, contactID)
	if err != nil {
		return fmt.Errorf("set profile contact: %w", err)
	}
	return nil
}

func (s *Store) loadUserLines(ctx context.Context, users []User) error {
	if len(users) == 0 {
		return nil
	}
	index := make(map[string]int, len(users))
	for position := range users {
		index[users[position].ID] = position
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT user_id, line_id
		FROM modemdeck_user_lines
		ORDER BY user_id, line_id
	`)
	if err != nil {
		return fmt.Errorf("query user lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID, lineID string
		if err := rows.Scan(&userID, &lineID); err != nil {
			return fmt.Errorf("scan user line: %w", err)
		}
		if position, exists := index[userID]; exists {
			users[position].LineIDs = append(users[position].LineIDs, lineID)
		}
	}
	return rows.Err()
}

func normalizedUserLineIDs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		lineID := strings.TrimSpace(value)
		if lineID == "" {
			return nil, ErrUserValidation
		}
		if _, duplicate := seen[lineID]; duplicate {
			continue
		}
		seen[lineID] = struct{}{}
		result = append(result, lineID)
	}
	return result, nil
}

func requireUserLines(ctx context.Context, queryer contactQueryer, lineIDs []string) error {
	for _, lineID := range lineIDs {
		var exists bool
		if err := queryer.QueryRowContext(
			ctx,
			"SELECT EXISTS(SELECT 1 FROM modemdeck_lines WHERE line_id = ?)",
			lineID,
		).Scan(&exists); err != nil {
			return fmt.Errorf("query assigned line: %w", err)
		}
		if !exists {
			return ErrUserValidation
		}
	}
	return nil
}

func replaceUserLines(
	ctx context.Context,
	transaction *sql.Tx,
	userID string,
	lineIDs []string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		"DELETE FROM modemdeck_user_lines WHERE user_id = ?",
		userID,
	); err != nil {
		return fmt.Errorf("delete user lines: %w", err)
	}
	for _, lineID := range lineIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO modemdeck_user_lines (user_id, line_id)
			VALUES (?, ?)
		`, userID, lineID); err != nil {
			return fmt.Errorf("insert user line: %w", err)
		}
	}
	for _, lineID := range lineIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT OR IGNORE INTO modemdeck_user_message_thread_state (
				user_id, line_id, peer, last_read_sms_id,
				marked_unread, is_favorite
			)
			SELECT
				?,
				thread.line_id,
				thread.peer,
				COALESCE((
					SELECT MAX(message.id)
					FROM sms AS message
					WHERE message.line_id = thread.line_id
						AND message.peer = thread.peer
				), 0),
				0,
				0
			FROM sms_contacts AS thread
			WHERE thread.line_id = ?
		`, userID, lineID); err != nil {
			return fmt.Errorf("initialize assigned message state: %w", err)
		}
	}
	for _, lineID := range lineIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT OR IGNORE INTO modemdeck_user_call_state (
				user_id, call_id, is_read, is_favorite
			)
			SELECT ?, id, 1, 0
			FROM call_history
			WHERE line_id = ?
		`, userID, lineID); err != nil {
			return fmt.Errorf("initialize assigned call state: %w", err)
		}
	}
	return nil
}

func userWriteMiss(
	ctx context.Context,
	queryer contactQueryer,
	userID string,
	expectedRevision int64,
) error {
	var (
		role     string
		revision int64
	)
	err := queryer.QueryRowContext(
		ctx,
		"SELECT role, revision FROM modemdeck_users WHERE id = ?",
		userID,
	).Scan(&role, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("query member revision: %w", err)
	}
	return &UserRevisionConflictError{
		UserID:           userID,
		ExpectedRevision: expectedRevision,
		ActualRevision:   revision,
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func newUserIdentifier() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"user_%x%x%x%x%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}
