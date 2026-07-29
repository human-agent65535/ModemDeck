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
	ErrDatabaseRequired = errors.New("store database is required")
	ErrInvalidCallKind  = errors.New("invalid call kind")
)

type Store struct {
	database *sql.DB
}

func New(database *sql.DB) (*Store, error) {
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	return &Store{database: database}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.database == nil {
		return ErrDatabaseRequired
	}
	return s.database.PingContext(ctx)
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return DefaultQueryLimit
	}
	if limit > MaxQueryLimit {
		return MaxQueryLimit
	}
	return limit
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func searchPattern(value string) string {
	return "%" + escapeLike(strings.ToLower(strings.TrimSpace(value))) + "%"
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func stringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func intValue(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func nullableIntValue(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableSignalMetric(value sql.NullInt64) *int64 {
	if !value.Valid || value.Int64 == 0 {
		return nil
	}
	result := value.Int64
	return &result
}

func boolValue(value sql.NullInt64) bool {
	return value.Valid && value.Int64 != 0
}

func pointerValue(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	result := value.String
	return &result
}

func rowsError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func durationSeconds(start, end string) int64 {
	startedAt, ok := parseDatabaseTime(start)
	if !ok {
		return 0
	}
	endedAt, ok := parseDatabaseTime(end)
	if !ok || endedAt.Before(startedAt) {
		return 0
	}
	return int64(endedAt.Sub(startedAt) / time.Second)
}

func parseDatabaseTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
