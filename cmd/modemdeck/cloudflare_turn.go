package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/cloudflareturn"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

const maximumCloudflareTURNTokenFileBytes = 4097

func cloudflareTURNProvider() (rtcconfig.Provider, error) {
	keyID := strings.TrimSpace(
		os.Getenv("MODEMDECK_CLOUDFLARE_TURN_KEY_ID"),
	)
	tokenFile := strings.TrimSpace(
		os.Getenv("MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE"),
	)
	if keyID == "" && tokenFile == "" {
		return nil, nil
	}
	if keyID == "" || tokenFile == "" {
		return nil, errors.New(
			"Cloudflare TURN key ID and token file must be configured together",
		)
	}
	token, err := readCloudflareTURNToken(tokenFile)
	if err != nil {
		return nil, err
	}
	client, err := cloudflareturn.New(cloudflareturn.Options{
		KeyID:    keyID,
		APIToken: token,
	})
	if err != nil {
		return nil, fmt.Errorf("configure Cloudflare TURN client: %w", err)
	}
	return client, nil
}

func readCloudflareTURNToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect Cloudflare TURN token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Cloudflare TURN token file must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Cloudflare TURN token file: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(
		file,
		maximumCloudflareTURNTokenFileBytes+1,
	))
	if err != nil {
		return "", fmt.Errorf("read Cloudflare TURN token file: %w", err)
	}
	if len(content) > maximumCloudflareTURNTokenFileBytes {
		return "", errors.New("Cloudflare TURN token file is too large")
	}
	token := strings.TrimSpace(string(content))
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", errors.New(
			"Cloudflare TURN token file must contain one token",
		)
	}
	return token, nil
}
