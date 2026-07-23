package telegram

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	maxBotTokenLength = 256
	maxLineScopes     = 256
	maxLineIDLength   = 128
)

// Config is one independently scoped Telegram bot. An empty LineScopes slice
// grants access to every line; otherwise only exact line IDs are visible and
// actionable.
type Config struct {
	Enabled       bool
	BotToken      string
	ChatID        int64
	AdminID       int64
	LineScopes    []string
	Notifications NotificationConfig
}

type NotificationConfig struct {
	IncomingSMS bool
	MissedCalls bool
}

type ConfigError struct {
	Field  string
	Reason string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("telegram config %s: %s", e.Field, e.Reason)
}

func (c Config) Validate() error {
	if len(c.LineScopes) > maxLineScopes {
		return &ConfigError{Field: "line_scopes", Reason: "too many entries"}
	}

	seen := make(map[string]struct{}, len(c.LineScopes))
	for _, lineID := range c.LineScopes {
		if err := validateLineID(lineID); err != nil {
			return &ConfigError{Field: "line_scopes", Reason: err.Error()}
		}
		if _, exists := seen[lineID]; exists {
			return &ConfigError{Field: "line_scopes", Reason: "duplicate line ID"}
		}
		seen[lineID] = struct{}{}
	}

	// Disabled units retain their settings so they can be re-enabled without
	// re-entering credentials.
	if !c.Enabled {
		return nil
	}
	if err := validateBotToken(c.BotToken); err != nil {
		return &ConfigError{Field: "bot_token", Reason: err.Error()}
	}
	if c.ChatID == 0 {
		return &ConfigError{Field: "chat_id", Reason: "must be non-zero"}
	}
	if c.AdminID <= 0 {
		return &ConfigError{Field: "admin_id", Reason: "must be positive"}
	}
	return nil
}

// ValidateConfigs rejects duplicate enabled bot identities. Two long pollers
// consuming getUpdates for the same bot would race and lose updates.
func ValidateConfigs(configs []Config) error {
	enabledBots := make(map[int64]struct{}, len(configs))
	for index, config := range configs {
		if err := config.Validate(); err != nil {
			return &ConfigError{
				Field:  fmt.Sprintf("units[%d]", index),
				Reason: err.Error(),
			}
		}
		if !config.Enabled {
			continue
		}
		botID := botIDFromToken(config.BotToken)
		if _, exists := enabledBots[botID]; exists {
			return &ConfigError{
				Field:  fmt.Sprintf("units[%d].bot_token", index),
				Reason: "duplicates another enabled bot identity",
			}
		}
		enabledBots[botID] = struct{}{}
	}
	return nil
}

func (c Config) AllowsLine(lineID string) bool {
	if len(c.LineScopes) == 0 {
		return true
	}
	for _, allowed := range c.LineScopes {
		if allowed == lineID {
			return true
		}
	}
	return false
}

func validateBotToken(token string) error {
	if token == "" {
		return fmt.Errorf("is required")
	}
	if token != strings.TrimSpace(token) {
		return fmt.Errorf("must not contain surrounding whitespace")
	}
	if len(token) > maxBotTokenLength {
		return fmt.Errorf("is too long")
	}

	prefix, suffix, found := strings.Cut(token, ":")
	if !found || prefix == "" || len(suffix) < 20 {
		return fmt.Errorf("has invalid format")
	}
	for _, ch := range prefix {
		if ch < '0' || ch > '9' {
			return fmt.Errorf("has invalid format")
		}
	}
	botID, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || botID <= 0 {
		return fmt.Errorf("has invalid bot ID")
	}
	for _, ch := range suffix {
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '_' || ch == '-' {
			continue
		}
		return fmt.Errorf("has invalid format")
	}
	return nil
}

func botIDFromToken(token string) int64 {
	prefix, _, _ := strings.Cut(token, ":")
	botID, _ := strconv.ParseInt(prefix, 10, 64)
	return botID
}

func validateLineID(lineID string) error {
	if lineID == "" {
		return fmt.Errorf("line ID is empty")
	}
	if len(lineID) > maxLineIDLength {
		return fmt.Errorf("line ID is too long")
	}
	if lineID != strings.TrimSpace(lineID) {
		return fmt.Errorf("line ID has surrounding whitespace")
	}
	for _, ch := range lineID {
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '_' || ch == '-' || ch == '.' || ch == ':' {
			continue
		}
		return fmt.Errorf("line ID contains unsupported characters")
	}
	return nil
}
