package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrLineSettingsRevisionConflict = errors.New("line settings revision conflict")
	ErrLineSettingsInvalidLine      = errors.New("line settings line is invalid")
)

type LineSettingsRevisionConflictError struct {
	ExpectedRevision int64
	ActualRevision   int64
}

func (e *LineSettingsRevisionConflictError) Error() string {
	if e == nil {
		return ErrLineSettingsRevisionConflict.Error()
	}
	return fmt.Sprintf(
		"%s: expected %d, actual %d",
		ErrLineSettingsRevisionConflict,
		e.ExpectedRevision,
		e.ActualRevision,
	)
}

func (e *LineSettingsRevisionConflictError) Unwrap() error {
	return ErrLineSettingsRevisionConflict
}

func (s *Store) LineSettings(ctx context.Context) (LineSettings, error) {
	return readLineSettings(ctx, s.database)
}

func (s *Store) UpdateLineSettings(
	ctx context.Context,
	defaultLineID string,
	expectedRevision int64,
) (LineSettings, error) {
	defaultLineID = strings.TrimSpace(defaultLineID)
	if defaultLineID == "" {
		return LineSettings{}, fmt.Errorf("%w: default_line_id is required", ErrLineSettingsInvalidLine)
	}
	if expectedRevision <= 0 {
		return LineSettings{}, fmt.Errorf("%w: expected_revision must be positive", ErrLineSettingsRevisionConflict)
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return LineSettings{}, fmt.Errorf("begin update line settings: %w", err)
	}
	defer transaction.Rollback()

	if err := requireLine(ctx, transaction, defaultLineID); err != nil {
		return LineSettings{}, err
	}
	current, err := readLineSettings(ctx, transaction)
	if err != nil {
		return LineSettings{}, err
	}
	if current.Revision != expectedRevision {
		return LineSettings{}, &LineSettingsRevisionConflictError{
			ExpectedRevision: expectedRevision,
			ActualRevision:   current.Revision,
		}
	}
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_line_settings
		 SET default_line_id = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND revision = ?`,
		defaultLineID,
		expectedRevision,
	)
	if err != nil {
		return LineSettings{}, fmt.Errorf("update line settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return LineSettings{}, fmt.Errorf("read updated line settings count: %w", err)
	}
	if affected != 1 {
		actual, readErr := readLineSettings(ctx, transaction)
		if readErr != nil {
			return LineSettings{}, readErr
		}
		return LineSettings{}, &LineSettingsRevisionConflictError{
			ExpectedRevision: expectedRevision,
			ActualRevision:   actual.Revision,
		}
	}
	updated, err := readLineSettings(ctx, transaction)
	if err != nil {
		return LineSettings{}, err
	}
	if err := transaction.Commit(); err != nil {
		return LineSettings{}, fmt.Errorf("commit line settings: %w", err)
	}
	return updated, nil
}

type lineSettingsQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readLineSettings(ctx context.Context, queryer lineSettingsQueryer) (LineSettings, error) {
	var (
		settings  LineSettings
		lineID    sql.NullString
		updatedAt sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT default_line_id, revision, updated_at
		 FROM modemdeck_line_settings
		 WHERE singleton = 1`,
	).Scan(&lineID, &settings.Revision, &updatedAt)
	if err != nil {
		return LineSettings{}, fmt.Errorf("read line settings: %w", err)
	}
	settings.DefaultLineID = stringValue(lineID)
	settings.UpdatedAt = stringValue(updatedAt)
	return settings, nil
}

func requireLine(ctx context.Context, queryer lineSettingsQueryer, lineID string) error {
	var exists int
	err := queryer.QueryRowContext(
		ctx,
		"SELECT 1 FROM modemdeck_lines WHERE line_id = ?",
		strings.TrimSpace(lineID),
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrLineSettingsInvalidLine, lineID)
	}
	if err != nil {
		return fmt.Errorf("validate line settings line: %w", err)
	}
	return nil
}
