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
	ErrTelegramUnitNotFound         = errors.New("telegram unit not found")
	ErrTelegramUnitRevisionConflict = errors.New("telegram unit revision conflict")
	ErrTelegramCheckpointRegression = errors.New("telegram checkpoint cannot move backwards")
	ErrTelegramReplyBindingNotFound = errors.New("telegram reply binding not found")
)

type TelegramUnitRecord struct {
	ID                  string
	DisplayName         string
	Enabled             bool
	BotID               int64
	BotTokenNonce       []byte
	BotTokenCiphertext  []byte
	TokenHint           string
	ChatID              int64
	AdminID             int64
	ScopeSource         string
	AssignedUserID      string
	AssignedUsername    string
	AssignedUserRole    string
	AssignedUserEnabled bool
	AllAssignedLines    bool
	ManualAllLines      bool
	LineScopes          []string
	IncomingSMS         bool
	MissedCalls         bool
	Revision            int64
	BotUsername         string
	VerifiedAt          string
	LastErrorClass      string
	NextUpdateOffset    int64
	CreatedAt           string
	UpdatedAt           string
}

type TelegramReplyBinding struct {
	LineID string
	Number string
}

func (s *Store) TelegramUnits(ctx context.Context) ([]TelegramUnitRecord, error) {
	rows, err := s.database.QueryContext(ctx, telegramUnitSelect+`
		ORDER BY LOWER(display_name) ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query telegram units: %w", err)
	}
	units := make([]TelegramUnitRecord, 0)
	for rows.Next() {
		unit, err := scanTelegramUnit(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		units = append(units, unit)
	}
	rowsErr := rows.Err()
	if closeErr := rows.Close(); rowsErr == nil {
		rowsErr = closeErr
	}
	if rowsErr != nil {
		return nil, fmt.Errorf("read telegram units: %w", rowsErr)
	}
	if err := loadTelegramLineScopes(ctx, s.database, units); err != nil {
		return nil, err
	}
	for index := range units {
		if err := resolveTelegramUnitScope(ctx, s.database, &units[index]); err != nil {
			return nil, err
		}
	}
	return units, nil
}

func (s *Store) TelegramUnit(ctx context.Context, id string) (TelegramUnitRecord, error) {
	return telegramUnitByID(ctx, s.database, strings.TrimSpace(id))
}

func (s *Store) CreateTelegramUnit(ctx context.Context, unit TelegramUnitRecord) (TelegramUnitRecord, error) {
	unit.ID = strings.TrimSpace(unit.ID)
	if unit.ID == "" {
		return TelegramUnitRecord{}, fmt.Errorf("create telegram unit: id is required")
	}
	unit.BotTokenNonce = nonNilBytes(unit.BotTokenNonce)
	unit.BotTokenCiphertext = nonNilBytes(unit.BotTokenCiphertext)
	normalizeTelegramUnitScope(&unit)
	unit.Revision = 1
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("begin create telegram unit: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_telegram_units (
			id, display_name, enabled, bot_id, bot_token_nonce,
			bot_token_ciphertext, token_hint, chat_id, admin_id,
			scope_source, assigned_user_id, manual_all_lines, incoming_sms,
			missed_calls, revision, bot_username, verified_at, last_error_class,
			next_update_offset, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, NULLIF(?, ''), ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		unit.ID,
		strings.TrimSpace(unit.DisplayName),
		unit.Enabled,
		unit.BotID,
		unit.BotTokenNonce,
		unit.BotTokenCiphertext,
		strings.TrimSpace(unit.TokenHint),
		unit.ChatID,
		unit.AdminID,
		unit.ScopeSource,
		unit.AssignedUserID,
		unit.ManualAllLines,
		unit.IncomingSMS,
		unit.MissedCalls,
		strings.TrimSpace(unit.BotUsername),
		strings.TrimSpace(unit.VerifiedAt),
		strings.TrimSpace(unit.LastErrorClass),
		unit.NextUpdateOffset,
	); err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("insert telegram unit: %w", err)
	}
	if err := replaceTelegramLineScopes(ctx, transaction, unit.ID, unit.LineScopes); err != nil {
		return TelegramUnitRecord{}, err
	}
	created, err := telegramUnitByID(ctx, transaction, unit.ID)
	if err != nil {
		return TelegramUnitRecord{}, err
	}
	if err := transaction.Commit(); err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("commit create telegram unit: %w", err)
	}
	return created, nil
}

func (s *Store) UpdateTelegramUnit(
	ctx context.Context,
	unit TelegramUnitRecord,
	expectedRevision int64,
) (TelegramUnitRecord, error) {
	unit.ID = strings.TrimSpace(unit.ID)
	if unit.ID == "" || expectedRevision <= 0 {
		return TelegramUnitRecord{}, fmt.Errorf("update telegram unit: id and positive revision are required")
	}
	unit.BotTokenNonce = nonNilBytes(unit.BotTokenNonce)
	unit.BotTokenCiphertext = nonNilBytes(unit.BotTokenCiphertext)
	normalizeTelegramUnitScope(&unit)
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("begin update telegram unit: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_telegram_units SET
			display_name = ?,
			enabled = ?,
			bot_id = ?,
			bot_token_nonce = ?,
			bot_token_ciphertext = ?,
			token_hint = ?,
			chat_id = ?,
			admin_id = ?,
			scope_source = ?,
			assigned_user_id = ?,
			manual_all_lines = ?,
			incoming_sms = ?,
			missed_calls = ?,
			revision = revision + 1,
			bot_username = ?,
			verified_at = NULLIF(?, ''),
			last_error_class = ?,
			next_update_offset = ?,
			updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND revision = ?`,
		strings.TrimSpace(unit.DisplayName),
		unit.Enabled,
		unit.BotID,
		unit.BotTokenNonce,
		unit.BotTokenCiphertext,
		strings.TrimSpace(unit.TokenHint),
		unit.ChatID,
		unit.AdminID,
		unit.ScopeSource,
		unit.AssignedUserID,
		unit.ManualAllLines,
		unit.IncomingSMS,
		unit.MissedCalls,
		strings.TrimSpace(unit.BotUsername),
		strings.TrimSpace(unit.VerifiedAt),
		strings.TrimSpace(unit.LastErrorClass),
		unit.NextUpdateOffset,
		unit.ID,
		expectedRevision,
	)
	if err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("update telegram unit: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("read updated telegram unit count: %w", err)
	}
	if affected != 1 {
		var actual int64
		err := transaction.QueryRowContext(
			ctx,
			"SELECT revision FROM modemdeck_telegram_units WHERE id = ?",
			unit.ID,
		).Scan(&actual)
		if errors.Is(err, sql.ErrNoRows) {
			return TelegramUnitRecord{}, ErrTelegramUnitNotFound
		}
		if err != nil {
			return TelegramUnitRecord{}, fmt.Errorf("inspect telegram unit revision: %w", err)
		}
		return TelegramUnitRecord{}, fmt.Errorf(
			"%w: expected %d, actual %d",
			ErrTelegramUnitRevisionConflict,
			expectedRevision,
			actual,
		)
	}
	if err := replaceTelegramLineScopes(ctx, transaction, unit.ID, unit.LineScopes); err != nil {
		return TelegramUnitRecord{}, err
	}
	updated, err := telegramUnitByID(ctx, transaction, unit.ID)
	if err != nil {
		return TelegramUnitRecord{}, err
	}
	if err := transaction.Commit(); err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("commit update telegram unit: %w", err)
	}
	return updated, nil
}

func normalizeTelegramUnitScope(unit *TelegramUnitRecord) {
	if unit.ScopeSource == "" {
		unit.ScopeSource = "manual"
		if len(unit.LineScopes) == 0 {
			unit.ManualAllLines = true
		}
	}
	if unit.ScopeSource == "user" {
		unit.ManualAllLines = false
	}
}

func (s *Store) DeleteTelegramUnit(ctx context.Context, id string, expectedRevision int64) error {
	id = strings.TrimSpace(id)
	if id == "" || expectedRevision <= 0 {
		return fmt.Errorf("delete telegram unit: id and positive revision are required")
	}
	result, err := s.database.ExecContext(
		ctx,
		"DELETE FROM modemdeck_telegram_units WHERE id = ? AND revision = ?",
		id,
		expectedRevision,
	)
	if err != nil {
		return fmt.Errorf("delete telegram unit: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted telegram unit count: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var actual int64
	err = s.database.QueryRowContext(
		ctx,
		"SELECT revision FROM modemdeck_telegram_units WHERE id = ?",
		id,
	).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTelegramUnitNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect telegram unit revision: %w", err)
	}
	return fmt.Errorf(
		"%w: expected %d, actual %d",
		ErrTelegramUnitRevisionConflict,
		expectedRevision,
		actual,
	)
}

func (s *Store) UpdateTelegramRuntimeStatus(
	ctx context.Context,
	id, botUsername, verifiedAt, lastErrorClass string,
) error {
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_telegram_units
		 SET bot_username = CASE WHEN ? <> '' THEN ? ELSE bot_username END,
			verified_at = CASE WHEN ? <> '' THEN ? ELSE verified_at END,
			last_error_class = ?,
			updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		strings.TrimSpace(botUsername),
		strings.TrimSpace(botUsername),
		strings.TrimSpace(verifiedAt),
		strings.TrimSpace(verifiedAt),
		strings.TrimSpace(lastErrorClass),
		strings.TrimSpace(id),
	)
	if err != nil {
		return fmt.Errorf("update telegram runtime status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read telegram runtime status count: %w", err)
	}
	if affected != 1 {
		return ErrTelegramUnitNotFound
	}
	return nil
}

func (s *Store) TelegramNextOffset(ctx context.Context, id string) (int64, error) {
	var offset int64
	err := s.database.QueryRowContext(
		ctx,
		"SELECT next_update_offset FROM modemdeck_telegram_units WHERE id = ?",
		strings.TrimSpace(id),
	).Scan(&offset)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrTelegramUnitNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("query telegram checkpoint: %w", err)
	}
	if offset < 0 {
		return 0, fmt.Errorf("query telegram checkpoint: stored offset is negative")
	}
	return offset, nil
}

func (s *Store) AdvanceTelegramOffset(ctx context.Context, id string, next int64) error {
	if next < 0 {
		return ErrTelegramCheckpointRegression
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_telegram_units
		 SET next_update_offset = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND next_update_offset <= ?`,
		next,
		strings.TrimSpace(id),
		next,
	)
	if err != nil {
		return fmt.Errorf("advance telegram checkpoint: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read telegram checkpoint update count: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var current int64
	err = s.database.QueryRowContext(
		ctx,
		"SELECT next_update_offset FROM modemdeck_telegram_units WHERE id = ?",
		strings.TrimSpace(id),
	).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTelegramUnitNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect telegram checkpoint: %w", err)
	}
	return fmt.Errorf("%w: current %d, requested %d", ErrTelegramCheckpointRegression, current, next)
}

func (s *Store) BindTelegramReply(
	ctx context.Context,
	botID, chatID, messageID int64,
	binding TelegramReplyBinding,
) error {
	if botID <= 0 || chatID == 0 || messageID <= 0 ||
		strings.TrimSpace(binding.LineID) == "" || strings.TrimSpace(binding.Number) == "" {
		return fmt.Errorf("bind telegram reply: identity and target are required")
	}
	if _, err := s.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_telegram_reply_bindings (
			bot_id, chat_id, message_id, line_id, phone_number, created_at
		 ) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(bot_id, chat_id, message_id) DO UPDATE SET
			line_id = excluded.line_id,
			phone_number = excluded.phone_number,
			created_at = excluded.created_at`,
		botID,
		chatID,
		messageID,
		strings.TrimSpace(binding.LineID),
		strings.TrimSpace(binding.Number),
	); err != nil {
		return fmt.Errorf("bind telegram reply: %w", err)
	}
	return nil
}

func (s *Store) ResolveTelegramReply(
	ctx context.Context,
	botID, chatID, messageID int64,
) (TelegramReplyBinding, error) {
	var binding TelegramReplyBinding
	err := s.database.QueryRowContext(
		ctx,
		`SELECT line_id, phone_number
		 FROM modemdeck_telegram_reply_bindings
		 WHERE bot_id = ? AND chat_id = ? AND message_id = ?`,
		botID,
		chatID,
		messageID,
	).Scan(&binding.LineID, &binding.Number)
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramReplyBinding{}, ErrTelegramReplyBindingNotFound
	}
	if err != nil {
		return TelegramReplyBinding{}, fmt.Errorf("resolve telegram reply: %w", err)
	}
	return binding, nil
}

func (s *Store) PurgeTelegramReplyBindings(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return fmt.Errorf("purge telegram reply bindings: cutoff is required")
	}
	if _, err := s.database.ExecContext(
		ctx,
		"DELETE FROM modemdeck_telegram_reply_bindings WHERE created_at < ?",
		databaseTime(before),
	); err != nil {
		return fmt.Errorf("purge telegram reply bindings: %w", err)
	}
	return nil
}

const telegramUnitSelect = `SELECT
	id, display_name, enabled, bot_id, bot_token_nonce, bot_token_ciphertext,
	token_hint, chat_id, admin_id, scope_source, assigned_user_id,
	manual_all_lines, incoming_sms, missed_calls, revision,
	bot_username, verified_at, last_error_class, next_update_offset,
	created_at, updated_at
	FROM modemdeck_telegram_units `

type telegramUnitScanner interface {
	Scan(...any) error
}

func scanTelegramUnit(scanner telegramUnitScanner) (TelegramUnitRecord, error) {
	var (
		unit                                              TelegramUnitRecord
		enabled, manualAllLines, incomingSMS, missedCalls sql.NullInt64
		botID, chatID, adminID, revision, offset          sql.NullInt64
		displayName, tokenHint, botUsername               sql.NullString
		verifiedAt, lastError, createdAt, updatedAt       sql.NullString
		nonce, ciphertext                                 []byte
	)
	if err := scanner.Scan(
		&unit.ID,
		&displayName,
		&enabled,
		&botID,
		&nonce,
		&ciphertext,
		&tokenHint,
		&chatID,
		&adminID,
		&unit.ScopeSource,
		&unit.AssignedUserID,
		&manualAllLines,
		&incomingSMS,
		&missedCalls,
		&revision,
		&botUsername,
		&verifiedAt,
		&lastError,
		&offset,
		&createdAt,
		&updatedAt,
	); err != nil {
		return TelegramUnitRecord{}, err
	}
	unit.DisplayName = stringValue(displayName)
	unit.Enabled = boolValue(enabled)
	unit.BotID = intValue(botID)
	unit.BotTokenNonce = append([]byte(nil), nonce...)
	unit.BotTokenCiphertext = append([]byte(nil), ciphertext...)
	unit.TokenHint = stringValue(tokenHint)
	unit.ChatID = intValue(chatID)
	unit.AdminID = intValue(adminID)
	unit.ManualAllLines = boolValue(manualAllLines)
	unit.IncomingSMS = boolValue(incomingSMS)
	unit.MissedCalls = boolValue(missedCalls)
	unit.Revision = intValue(revision)
	unit.BotUsername = stringValue(botUsername)
	unit.VerifiedAt = stringValue(verifiedAt)
	unit.LastErrorClass = stringValue(lastError)
	unit.NextUpdateOffset = intValue(offset)
	unit.CreatedAt = stringValue(createdAt)
	unit.UpdatedAt = stringValue(updatedAt)
	return unit, nil
}

type telegramUnitQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func telegramUnitByID(
	ctx context.Context,
	queryer telegramUnitQueryer,
	id string,
) (TelegramUnitRecord, error) {
	if id == "" {
		return TelegramUnitRecord{}, ErrTelegramUnitNotFound
	}
	unit, err := scanTelegramUnit(queryer.QueryRowContext(
		ctx,
		telegramUnitSelect+"WHERE id = ?",
		id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramUnitRecord{}, ErrTelegramUnitNotFound
	}
	if err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("query telegram unit: %w", err)
	}
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT line_id FROM modemdeck_telegram_line_scopes
		 WHERE unit_id = ? ORDER BY line_id ASC`,
		unit.ID,
	)
	if err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("query telegram line scopes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		if err := rows.Scan(&lineID); err != nil {
			return TelegramUnitRecord{}, fmt.Errorf("scan telegram line scope: %w", err)
		}
		unit.LineScopes = append(unit.LineScopes, lineID)
	}
	if err := rows.Err(); err != nil {
		return TelegramUnitRecord{}, fmt.Errorf("read telegram line scopes: %w", err)
	}
	if err := resolveTelegramUnitScope(ctx, queryer, &unit); err != nil {
		return TelegramUnitRecord{}, err
	}
	return unit, nil
}

func resolveTelegramUnitScope(
	ctx context.Context,
	queryer telegramUnitQueryer,
	unit *TelegramUnitRecord,
) error {
	if unit == nil || unit.ScopeSource != "user" {
		return nil
	}
	configuredLineScopes := append([]string(nil), unit.LineScopes...)
	unit.AllAssignedLines = len(configuredLineScopes) == 0
	unit.LineScopes = nil
	var enabled int64
	err := queryer.QueryRowContext(
		ctx,
		`SELECT username, role, enabled
		 FROM modemdeck_users
		 WHERE id = ?`,
		unit.AssignedUserID,
	).Scan(&unit.AssignedUsername, &unit.AssignedUserRole, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		unit.AssignedUserEnabled = false
		return nil
	}
	if err != nil {
		return fmt.Errorf("query assigned Telegram user: %w", err)
	}
	unit.AssignedUserEnabled = enabled != 0
	statement := `SELECT line_id FROM modemdeck_user_lines
		WHERE user_id = ? ORDER BY line_id`
	arguments := []any{unit.AssignedUserID}
	rows, err := queryer.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return fmt.Errorf("query assigned Telegram user lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		if err := rows.Scan(&lineID); err != nil {
			return fmt.Errorf("scan assigned Telegram user line: %w", err)
		}
		if !unit.AllAssignedLines && !containsString(configuredLineScopes, lineID) {
			continue
		}
		unit.LineScopes = append(unit.LineScopes, lineID)
	}
	return rows.Err()
}

type telegramScopeQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadTelegramLineScopes(
	ctx context.Context,
	queryer telegramScopeQueryer,
	units []TelegramUnitRecord,
) error {
	if len(units) == 0 {
		return nil
	}
	index := make(map[string]int, len(units))
	arguments := make([]any, 0, len(units))
	for position := range units {
		index[units[position].ID] = position
		arguments = append(arguments, units[position].ID)
	}
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT unit_id, line_id
		 FROM modemdeck_telegram_line_scopes
		 WHERE unit_id IN (`+placeholders(len(arguments))+`)
		 ORDER BY unit_id ASC, line_id ASC`,
		arguments...,
	)
	if err != nil {
		return fmt.Errorf("query telegram line scopes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var unitID, lineID string
		if err := rows.Scan(&unitID, &lineID); err != nil {
			return fmt.Errorf("scan telegram line scope: %w", err)
		}
		if position, ok := index[unitID]; ok {
			units[position].LineScopes = append(units[position].LineScopes, lineID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read telegram line scopes: %w", err)
	}
	return nil
}

func replaceTelegramLineScopes(
	ctx context.Context,
	transaction *sql.Tx,
	unitID string,
	lineIDs []string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		"DELETE FROM modemdeck_telegram_line_scopes WHERE unit_id = ?",
		unitID,
	); err != nil {
		return fmt.Errorf("replace telegram line scopes: %w", err)
	}
	for _, lineID := range lineIDs {
		lineID = strings.TrimSpace(lineID)
		if lineID == "" {
			return fmt.Errorf("replace telegram line scopes: line id is required")
		}
		if _, err := transaction.ExecContext(
			ctx,
			"INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id) VALUES (?, ?)",
			unitID,
			lineID,
		); err != nil {
			return fmt.Errorf("insert telegram line scope: %w", err)
		}
	}
	return nil
}

func nonNilBytes(value []byte) []byte {
	if value == nil {
		return []byte{}
	}
	return value
}
