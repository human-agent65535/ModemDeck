package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAdminConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "admin-password")
	if err := os.WriteFile(path, []byte("a sufficiently long password\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	config, err := loadAdminConfig(" admin ", path, true)
	if err != nil {
		t.Fatalf("loadAdminConfig() error = %v", err)
	}
	if config.Username != "admin" || config.Password != "a sufficiently long password" || !config.SecureCookies {
		t.Fatalf("config = %+v", config)
	}
}

func TestLoadAdminConfigRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	shortPath := filepath.Join(t.TempDir(), "short")
	if err := os.WriteFile(shortPath, []byte("too-short\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	tests := []struct {
		name         string
		username     string
		passwordFile string
		want         error
	}{
		{name: "empty username", username: " ", passwordFile: shortPath, want: ErrAdminUsernameInvalid},
		{name: "missing file path", username: "admin", passwordFile: "", want: ErrAdminPasswordFileRequired},
		{name: "short password", username: "admin", passwordFile: shortPath, want: ErrAdminPasswordTooShort},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadAdminConfig(test.username, test.passwordFile, false)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnvironmentBool(t *testing.T) {
	t.Setenv("MODEMDECK_TEST_BOOL", "yes")
	value, err := environmentBool("MODEMDECK_TEST_BOOL", false)
	if err != nil || !value {
		t.Fatalf("environmentBool() = %v, %v", value, err)
	}
	t.Setenv("MODEMDECK_TEST_BOOL", "invalid")
	if _, err := environmentBool("MODEMDECK_TEST_BOOL", false); err == nil {
		t.Fatal("environmentBool() error = nil, want invalid value error")
	}
}
