package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	defaultAdminUsername = "admin"
	minimumPasswordBytes = 12
	maximumPasswordBytes = 1024
	maximumUsernameRunes = 64
)

var (
	ErrAdminPasswordFileRequired = errors.New("admin password file is required")
	ErrAdminPasswordTooShort     = errors.New("admin password is too short")
	ErrAdminPasswordTooLong      = errors.New("admin password is too long")
	ErrAdminPasswordInvalid      = errors.New("admin password is invalid")
	ErrAdminUsernameInvalid      = errors.New("admin username is invalid")
)

type adminConfig struct {
	Username      string
	Password      string
	SecureCookies bool
}

func loadAdminConfig(username, passwordFile string, secureCookies bool) (adminConfig, error) {
	username = strings.TrimSpace(username)
	if username == "" || utf8.RuneCountInString(username) > maximumUsernameRunes || strings.ContainsAny(username, "\x00\r\n") {
		return adminConfig{}, ErrAdminUsernameInvalid
	}
	passwordFile = strings.TrimSpace(passwordFile)
	if passwordFile == "" {
		return adminConfig{}, ErrAdminPasswordFileRequired
	}
	file, err := os.Open(passwordFile)
	if err != nil {
		return adminConfig{}, fmt.Errorf("open admin password file: %w", err)
	}
	defer file.Close()

	contents, err := io.ReadAll(io.LimitReader(file, maximumPasswordBytes+2))
	if err != nil {
		return adminConfig{}, fmt.Errorf("read admin password file: %w", err)
	}
	password := strings.TrimRight(string(contents), "\r\n")
	if len(password) < minimumPasswordBytes {
		return adminConfig{}, ErrAdminPasswordTooShort
	}
	if len(password) > maximumPasswordBytes {
		return adminConfig{}, ErrAdminPasswordTooLong
	}
	if strings.ContainsRune(password, '\x00') {
		return adminConfig{}, fmt.Errorf("%w: contains NUL", ErrAdminPasswordInvalid)
	}
	return adminConfig{Username: username, Password: password, SecureCookies: secureCookies}, nil
}

func environmentBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}
