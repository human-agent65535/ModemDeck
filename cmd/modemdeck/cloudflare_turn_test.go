package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloudflareTURNProviderDisabledWithoutConfiguration(t *testing.T) {
	t.Setenv("MODEMDECK_CLOUDFLARE_TURN_KEY_ID", "")
	t.Setenv("MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE", "")

	provider, err := cloudflareTURNProvider()
	if err != nil || provider != nil {
		t.Fatalf("cloudflareTURNProvider() = %T, %v", provider, err)
	}
}

func TestCloudflareTURNProviderLoadsTokenFile(t *testing.T) {
	directory := t.TempDir()
	tokenFile := filepath.Join(directory, "turn-token")
	if err := os.WriteFile(tokenFile, []byte("test-turn-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(
		"MODEMDECK_CLOUDFLARE_TURN_KEY_ID",
		"0123456789abcdef0123456789abcdef",
	)
	t.Setenv("MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE", tokenFile)

	provider, err := cloudflareTURNProvider()
	if err != nil || provider == nil {
		t.Fatalf("cloudflareTURNProvider() = %T, %v", provider, err)
	}
}

func TestCloudflareTURNProviderRejectsIncompleteConfiguration(t *testing.T) {
	t.Setenv(
		"MODEMDECK_CLOUDFLARE_TURN_KEY_ID",
		"0123456789abcdef0123456789abcdef",
	)
	t.Setenv("MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE", "")

	if _, err := cloudflareTURNProvider(); err == nil {
		t.Fatal("cloudflareTURNProvider() error = nil")
	}
}
