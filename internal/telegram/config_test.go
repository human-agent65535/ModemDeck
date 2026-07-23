package telegram

import (
	"errors"
	"fmt"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
		field   string
	}{
		{
			name:   "disabled empty configuration",
			config: Config{},
		},
		{
			name: "enabled valid configuration",
			config: Config{
				Enabled:    true,
				BotToken:   testBotToken,
				ChatID:     -100123456,
				AdminID:    42,
				LineScopes: []string{"line-a", "line_b"},
			},
		},
		{
			name: "disabled retains incomplete credentials",
			config: Config{
				BotToken: "retained while disabled",
				ChatID:   -100,
			},
		},
		{
			name: "token required",
			config: Config{
				Enabled: true,
				ChatID:  1,
				AdminID: 2,
			},
			wantErr: true,
			field:   "bot_token",
		},
		{
			name: "token format",
			config: Config{
				Enabled:  true,
				BotToken: "not-a-token",
				ChatID:   1,
				AdminID:  2,
			},
			wantErr: true,
			field:   "bot_token",
		},
		{
			name: "token bot ID zero",
			config: Config{
				Enabled:  true,
				BotToken: "0:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcd",
				ChatID:   1,
				AdminID:  2,
			},
			wantErr: true,
			field:   "bot_token",
		},
		{
			name: "token bot ID overflow",
			config: Config{
				Enabled:  true,
				BotToken: "999999999999999999999:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcd",
				ChatID:   1,
				AdminID:  2,
			},
			wantErr: true,
			field:   "bot_token",
		},
		{
			name: "chat ID required",
			config: Config{
				Enabled:  true,
				BotToken: testBotToken,
				AdminID:  2,
			},
			wantErr: true,
			field:   "chat_id",
		},
		{
			name: "admin positive",
			config: Config{
				Enabled:  true,
				BotToken: testBotToken,
				ChatID:   -100,
				AdminID:  -1,
			},
			wantErr: true,
			field:   "admin_id",
		},
		{
			name: "duplicate line",
			config: Config{
				LineScopes: []string{"line-a", "line-a"},
			},
			wantErr: true,
			field:   "line_scopes",
		},
		{
			name: "invalid line whitespace",
			config: Config{
				LineScopes: []string{" line-a"},
			},
			wantErr: true,
			field:   "line_scopes",
		},
		{
			name: "invalid line character",
			config: Config{
				LineScopes: []string{"line/a"},
			},
			wantErr: true,
			field:   "line_scopes",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.config.Validate()
			if test.wantErr {
				if err == nil {
					t.Fatal("Validate() returned nil")
				}
				var configErr *ConfigError
				if !errors.As(err, &configErr) {
					t.Fatalf("error type = %T, want *ConfigError", err)
				}
				if configErr.Field != test.field {
					t.Fatalf("field = %q, want %q", configErr.Field, test.field)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestConfigAllowsLine(t *testing.T) {
	t.Parallel()

	if !(Config{}).AllowsLine("anything") {
		t.Fatal("empty scope must allow all lines")
	}
	config := Config{LineScopes: []string{"line-a", "line-b"}}
	if !config.AllowsLine("line-b") {
		t.Fatal("configured line was rejected")
	}
	if config.AllowsLine("line-c") {
		t.Fatal("unconfigured line was allowed")
	}
}

func TestConfigLineScopesHaveNoProductCountLimit(t *testing.T) {
	t.Parallel()

	const count = 300
	scopes := make([]string, 0, count)
	for index := 0; index < count; index++ {
		scopes = append(scopes, fmt.Sprintf("line-%03d", index))
	}
	config := Config{LineScopes: scopes}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() with %d line scopes error = %v", count, err)
	}
	if !config.AllowsLine(scopes[count-1]) {
		t.Fatalf("AllowsLine() rejected line %q", scopes[count-1])
	}
}

func TestValidateConfigsRejectsDuplicateEnabledBot(t *testing.T) {
	t.Parallel()

	first := Config{Enabled: true, BotToken: testBotToken, ChatID: 1, AdminID: 2}
	second := first
	second.BotToken = "123456789:ZYXWVUTSRQPONMLKJIHGFEDCBA_abcd"
	if err := ValidateConfigs([]Config{first, second}); err == nil {
		t.Fatal("ValidateConfigs() accepted duplicate enabled bot identity")
	}

	second.Enabled = false
	if err := ValidateConfigs([]Config{first, second}); err != nil {
		t.Fatalf("disabled duplicate error = %v", err)
	}

	invalid := Config{Enabled: true}
	var configErr *ConfigError
	if err := ValidateConfigs([]Config{first, invalid}); !errors.As(err, &configErr) ||
		configErr.Field != "units[1]" {
		t.Fatalf("nested validation error = %v", err)
	}
}
