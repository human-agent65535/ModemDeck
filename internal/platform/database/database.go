package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Config struct {
	TargetPath string
}

const DefaultPath = "data/modemdeck.db"

func Open(ctx context.Context, config Config) (*sql.DB, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	config = normalizeConfig(config)

	if err := os.MkdirAll(filepath.Dir(config.TargetPath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	database, err := sql.Open("sqlite", config.TargetPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(0)

	closeOnError := func(openErr error) (*sql.DB, error) {
		if closeErr := database.Close(); closeErr != nil {
			openErr = fmt.Errorf("%w; close database: %v", openErr, closeErr)
		}
		return nil, openErr
	}
	if err := database.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping sqlite database: %w", err))
	}
	if err := configureSQLite(ctx, database); err != nil {
		return closeOnError(err)
	}
	if _, err := InitializeSchema(ctx, database); err != nil {
		return closeOnError(err)
	}
	return database, nil
}

func normalizeConfig(config Config) Config {
	config.TargetPath = strings.TrimSpace(config.TargetPath)
	if config.TargetPath == "" {
		config.TargetPath = DefaultPath
	}
	return config
}

func configureSQLite(ctx context.Context, database *sql.DB) error {
	statements := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func CloseWithTimeout(database *sql.DB, timeout time.Duration) error {
	if database == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	done := make(chan error, 1)
	go func() {
		done <- database.Close()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("close database: timeout after %s", timeout)
	}
}
