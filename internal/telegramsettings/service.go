package telegramsettings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegram"
)

const (
	maxDisplayNameRunes = 100
	maxUnitIDLength     = 80
)

type Repository interface {
	TelegramUnits(context.Context) ([]store.TelegramUnitRecord, error)
	TelegramUnit(context.Context, string) (store.TelegramUnitRecord, error)
	CreateTelegramUnit(context.Context, store.TelegramUnitRecord) (store.TelegramUnitRecord, error)
	UpdateTelegramUnit(context.Context, store.TelegramUnitRecord, int64) (store.TelegramUnitRecord, error)
	DeleteTelegramUnit(context.Context, string, int64) error
}

type SecretBox interface {
	Seal([]byte, []byte) ([]byte, []byte, error)
	Open([]byte, []byte, []byte) ([]byte, error)
}

type Unit struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	Enabled         bool     `json:"enabled"`
	ChatID          string   `json:"chat_id"`
	AdminID         string   `json:"admin_id"`
	LineScopes      []string `json:"line_scopes"`
	IncomingSMS     bool     `json:"incoming_sms"`
	MissedCalls     bool     `json:"missed_calls"`
	TokenConfigured bool     `json:"token_configured"`
	TokenHint       string   `json:"token_hint"`
	BotUsername     string   `json:"bot_username"`
	VerifiedAt      string   `json:"verified_at"`
	LastErrorClass  string   `json:"last_error_class"`
	Revision        int64    `json:"revision"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type CreateInput struct {
	DisplayName string
	Enabled     bool
	BotToken    string
	ChatID      string
	AdminID     string
	LineScopes  []string
	IncomingSMS bool
	MissedCalls bool
}

type UpdateInput struct {
	Revision    int64
	DisplayName string
	Enabled     bool
	BotToken    *string
	ChatID      string
	AdminID     string
	LineScopes  []string
	IncomingSMS bool
	MissedCalls bool
}

type Service struct {
	repository Repository
	secrets    SecretBox
	random     io.Reader
	changes    chan struct{}
}

func New(repository Repository, secrets SecretBox) (*Service, error) {
	if repository == nil {
		return nil, operationError(CodeInvalidArgument, "", "Telegram settings repository is required", nil)
	}
	if secrets == nil {
		return nil, operationError(CodeInvalidArgument, "", "Telegram settings encryption is required", nil)
	}
	return &Service{
		repository: repository,
		secrets:    secrets,
		random:     rand.Reader,
		changes:    make(chan struct{}, 1),
	}, nil
}

// Changes emits a coalesced signal after a settings mutation commits. Runtime
// consumers must reload the authoritative collection instead of inferring the
// change from the request that triggered it.
func (s *Service) Changes() <-chan struct{} {
	return s.changes
}

func (s *Service) List(ctx context.Context) ([]Unit, error) {
	records, err := s.repository.TelegramUnits(ctx)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	units := make([]Unit, 0, len(records))
	for _, record := range records {
		units = append(units, publicUnit(record))
	}
	return units, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Unit, error) {
	id, err := s.newID()
	if err != nil {
		return Unit{}, operationError(CodeInternal, "", "Telegram unit ID could not be generated", err)
	}
	record, err := s.buildRecord(id, store.TelegramUnitRecord{}, inputFields{
		DisplayName: input.DisplayName,
		Enabled:     input.Enabled,
		BotToken:    &input.BotToken,
		ChatID:      input.ChatID,
		AdminID:     input.AdminID,
		LineScopes:  input.LineScopes,
		IncomingSMS: input.IncomingSMS,
		MissedCalls: input.MissedCalls,
	})
	if err != nil {
		return Unit{}, err
	}
	if err := s.ensureUniqueBotID(ctx, id, record.BotID); err != nil {
		return Unit{}, err
	}
	created, err := s.repository.CreateTelegramUnit(ctx, record)
	if err != nil {
		return Unit{}, classifyStoreError(err)
	}
	s.notifyChange()
	return publicUnit(created), nil
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Unit, error) {
	id = strings.TrimSpace(id)
	if !validUnitID(id) {
		return Unit{}, operationError(CodeInvalidArgument, "id", "Telegram unit ID is invalid", nil)
	}
	if input.Revision <= 0 {
		return Unit{}, operationError(CodeInvalidArgument, "revision", "A positive revision is required", nil)
	}
	current, err := s.repository.TelegramUnit(ctx, id)
	if err != nil {
		return Unit{}, classifyStoreError(err)
	}
	if current.Revision != input.Revision {
		return Unit{}, operationError(
			CodeConflict,
			"revision",
			"Telegram unit was changed by another request",
			store.ErrTelegramUnitRevisionConflict,
		)
	}
	record, err := s.buildRecord(id, current, inputFields{
		DisplayName: input.DisplayName,
		Enabled:     input.Enabled,
		BotToken:    input.BotToken,
		ChatID:      input.ChatID,
		AdminID:     input.AdminID,
		LineScopes:  input.LineScopes,
		IncomingSMS: input.IncomingSMS,
		MissedCalls: input.MissedCalls,
	})
	if err != nil {
		return Unit{}, err
	}
	if err := s.ensureUniqueBotID(ctx, id, record.BotID); err != nil {
		return Unit{}, err
	}
	updated, err := s.repository.UpdateTelegramUnit(ctx, record, input.Revision)
	if err != nil {
		return Unit{}, classifyStoreError(err)
	}
	s.notifyChange()
	return publicUnit(updated), nil
}

func (s *Service) Delete(ctx context.Context, id string, revision int64) error {
	id = strings.TrimSpace(id)
	if !validUnitID(id) {
		return operationError(CodeInvalidArgument, "id", "Telegram unit ID is invalid", nil)
	}
	if revision <= 0 {
		return operationError(CodeInvalidArgument, "revision", "A positive revision is required", nil)
	}
	if err := s.repository.DeleteTelegramUnit(ctx, id, revision); err != nil {
		return classifyStoreError(err)
	}
	s.notifyChange()
	return nil
}

func (s *Service) RuntimeConfig(ctx context.Context, id string) (telegram.Config, error) {
	record, err := s.repository.TelegramUnit(ctx, strings.TrimSpace(id))
	if err != nil {
		return telegram.Config{}, classifyStoreError(err)
	}
	token, err := s.openToken(record)
	if err != nil {
		return telegram.Config{}, err
	}
	config := telegram.Config{
		Enabled:    record.Enabled,
		BotToken:   token,
		ChatID:     record.ChatID,
		AdminID:    record.AdminID,
		LineScopes: append([]string(nil), record.LineScopes...),
		Notifications: telegram.NotificationConfig{
			IncomingSMS: record.IncomingSMS,
			MissedCalls: record.MissedCalls,
		},
	}
	if err := config.Validate(); err != nil {
		return telegram.Config{}, operationError(
			CodeInvalidArgument,
			"",
			"Stored Telegram settings are invalid",
			err,
		)
	}
	return config, nil
}

type inputFields struct {
	DisplayName string
	Enabled     bool
	BotToken    *string
	ChatID      string
	AdminID     string
	LineScopes  []string
	IncomingSMS bool
	MissedCalls bool
}

func (s *Service) buildRecord(
	id string,
	current store.TelegramUnitRecord,
	input inputFields,
) (store.TelegramUnitRecord, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > maxDisplayNameRunes {
		return store.TelegramUnitRecord{}, operationError(
			CodeInvalidArgument,
			"display_name",
			"Display name must contain 1 to 100 characters",
			nil,
		)
	}
	chatID, err := parseIdentifier(input.ChatID, false, input.Enabled)
	if err != nil {
		return store.TelegramUnitRecord{}, operationError(CodeInvalidArgument, "chat_id", err.Error(), err)
	}
	adminID, err := parseIdentifier(input.AdminID, true, input.Enabled)
	if err != nil {
		return store.TelegramUnitRecord{}, operationError(CodeInvalidArgument, "admin_id", err.Error(), err)
	}
	lineScopes, err := normalizeLineScopes(input.LineScopes)
	if err != nil {
		return store.TelegramUnitRecord{}, operationError(CodeInvalidArgument, "line_scopes", err.Error(), err)
	}

	record := current
	record.ID = id
	record.DisplayName = displayName
	record.Enabled = input.Enabled
	record.ChatID = chatID
	record.AdminID = adminID
	record.LineScopes = lineScopes
	record.IncomingSMS = input.IncomingSMS
	record.MissedCalls = input.MissedCalls

	tokenChanged := input.BotToken != nil
	if tokenChanged {
		token := strings.TrimSpace(*input.BotToken)
		if token == "" {
			record.BotID = 0
			record.BotTokenNonce = nil
			record.BotTokenCiphertext = nil
			record.TokenHint = ""
		} else {
			client, err := telegram.NewClient(token, telegram.ClientOptions{})
			if err != nil || client == nil {
				return store.TelegramUnitRecord{}, operationError(
					CodeInvalidArgument,
					"bot_token",
					"Bot token has invalid format",
					err,
				)
			}
			botID, err := tokenBotID(token)
			if err != nil {
				return store.TelegramUnitRecord{}, operationError(
					CodeInvalidArgument,
					"bot_token",
					"Bot token has invalid identity",
					err,
				)
			}
			nonce, ciphertext, err := s.secrets.Seal([]byte(token), tokenAssociatedData(id))
			if err != nil {
				return store.TelegramUnitRecord{}, operationError(
					CodeUnavailable,
					"bot_token",
					"Bot token could not be encrypted",
					err,
				)
			}
			record.BotID = botID
			record.BotTokenNonce = nonce
			record.BotTokenCiphertext = ciphertext
			record.TokenHint = strconv.FormatInt(botID, 10)
		}
		record.BotUsername = ""
		record.VerifiedAt = ""
		record.LastErrorClass = ""
		record.NextUpdateOffset = 0
	}

	tokenConfigured := len(record.BotTokenNonce) > 0 && len(record.BotTokenCiphertext) > 0
	config := telegram.Config{
		Enabled:    input.Enabled,
		ChatID:     chatID,
		AdminID:    adminID,
		LineScopes: lineScopes,
	}
	if tokenConfigured {
		token, err := s.openToken(record)
		if err != nil {
			return store.TelegramUnitRecord{}, err
		}
		config.BotToken = token
	}
	if err := config.Validate(); err != nil {
		field := ""
		var configError *telegram.ConfigError
		if errors.As(err, &configError) {
			field = configError.Field
		}
		return store.TelegramUnitRecord{}, operationError(
			CodeInvalidArgument,
			field,
			"Telegram settings are incomplete or invalid",
			err,
		)
	}
	return record, nil
}

func (s *Service) openToken(record store.TelegramUnitRecord) (string, error) {
	if len(record.BotTokenNonce) == 0 && len(record.BotTokenCiphertext) == 0 {
		return "", nil
	}
	plaintext, err := s.secrets.Open(
		record.BotTokenNonce,
		record.BotTokenCiphertext,
		tokenAssociatedData(record.ID),
	)
	if err != nil {
		return "", operationError(
			CodeUnavailable,
			"bot_token",
			"Stored Bot token could not be decrypted",
			err,
		)
	}
	return string(plaintext), nil
}

func publicUnit(record store.TelegramUnitRecord) Unit {
	lineScopes := append([]string(nil), record.LineScopes...)
	if lineScopes == nil {
		lineScopes = []string{}
	}
	return Unit{
		ID:              record.ID,
		DisplayName:     record.DisplayName,
		Enabled:         record.Enabled,
		ChatID:          formatIdentifier(record.ChatID),
		AdminID:         formatIdentifier(record.AdminID),
		LineScopes:      lineScopes,
		IncomingSMS:     record.IncomingSMS,
		MissedCalls:     record.MissedCalls,
		TokenConfigured: len(record.BotTokenNonce) > 0 && len(record.BotTokenCiphertext) > 0,
		TokenHint:       record.TokenHint,
		BotUsername:     record.BotUsername,
		VerifiedAt:      record.VerifiedAt,
		LastErrorClass:  record.LastErrorClass,
		Revision:        record.Revision,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
}

func (s *Service) ensureUniqueBotID(ctx context.Context, unitID string, botID int64) error {
	if botID == 0 {
		return nil
	}
	units, err := s.repository.TelegramUnits(ctx)
	if err != nil {
		return classifyStoreError(err)
	}
	for _, unit := range units {
		if unit.ID != unitID && unit.BotID == botID {
			return operationError(
				CodeConflict,
				"bot_token",
				"This Telegram Bot is already configured",
				nil,
			)
		}
	}
	return nil
}

func parseIdentifier(value string, positive, required bool) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return 0, errors.New("value is required")
		}
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed == 0 || (positive && parsed < 0) {
		if positive {
			return 0, errors.New("value must be a positive integer")
		}
		return 0, errors.New("value must be a non-zero integer")
	}
	return parsed, nil
}

func formatIdentifier(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func normalizeLineScopes(values []string) ([]string, error) {
	config := telegram.Config{LineScopes: append([]string(nil), values...)}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, exists := seen[value]; exists {
			return nil, errors.New("line scopes contain a duplicate")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func tokenBotID(token string) (int64, error) {
	prefix, _, found := strings.Cut(token, ":")
	if !found {
		return 0, errors.New("token has no bot identity")
	}
	botID, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || botID <= 0 {
		return 0, errors.New("token bot identity is invalid")
	}
	return botID, nil
}

func tokenAssociatedData(id string) []byte {
	return []byte("modemdeck:telegram:" + id + ":bot-token:v1")
}

func (s *Service) newID() (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(s.random, value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return "telegram_" + hex.EncodeToString(value[:]), nil
}

func validUnitID(id string) bool {
	if id == "" || len(id) > maxUnitIDLength {
		return false
	}
	for _, character := range id {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) notifyChange() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}
