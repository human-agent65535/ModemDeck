package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
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
	settings, err := readLineSettings(ctx, s.database)
	if err != nil {
		return LineSettings{}, err
	}
	principal, scoped := auth.PrincipalFromContext(ctx)
	if !scoped {
		return settings, nil
	}
	settings, err = readUserLineSettings(ctx, s.database, principal.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		settings = LineSettings{Revision: 1}
	} else if err != nil {
		return LineSettings{}, err
	}
	if !principal.CanAccessLine(settings.DefaultLineID) {
		settings.DefaultLineID = ""
		for _, lineID := range principal.AllowedLineIDs {
			if strings.TrimSpace(lineID) != "" {
				settings.DefaultLineID = lineID
				break
			}
		}
	}
	return settings, nil
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
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		if !principal.CanAccessLine(defaultLineID) {
			return LineSettings{}, fmt.Errorf(
				"%w: default line is not assigned",
				ErrLineSettingsInvalidLine,
			)
		}
		return s.updateUserLineSettings(
			ctx,
			principal.UserID,
			defaultLineID,
			expectedRevision,
		)
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

func (s *Store) updateUserLineSettings(
	ctx context.Context,
	userID, defaultLineID string,
	expectedRevision int64,
) (LineSettings, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return LineSettings{}, fmt.Errorf("begin user line settings update: %w", err)
	}
	defer transaction.Rollback()
	if err := requireLine(ctx, transaction, defaultLineID); err != nil {
		return LineSettings{}, err
	}
	current, err := readUserLineSettings(ctx, transaction, userID)
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
		`UPDATE modemdeck_user_preferences
		 SET default_line_id = ?, revision = revision + 1,
			updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ? AND revision = ?`,
		defaultLineID,
		userID,
		expectedRevision,
	)
	if err != nil {
		return LineSettings{}, fmt.Errorf("update user line settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return LineSettings{}, fmt.Errorf("read updated user line settings count: %w", err)
	}
	if affected != 1 {
		actual, readErr := readUserLineSettings(ctx, transaction, userID)
		if readErr != nil {
			return LineSettings{}, readErr
		}
		return LineSettings{}, &LineSettingsRevisionConflictError{
			ExpectedRevision: expectedRevision,
			ActualRevision:   actual.Revision,
		}
	}
	updated, err := readUserLineSettings(ctx, transaction, userID)
	if err != nil {
		return LineSettings{}, err
	}
	if err := transaction.Commit(); err != nil {
		return LineSettings{}, fmt.Errorf("commit user line settings: %w", err)
	}
	return updated, nil
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

func readUserLineSettings(
	ctx context.Context,
	queryer lineSettingsQueryer,
	userID string,
) (LineSettings, error) {
	var settings LineSettings
	err := queryer.QueryRowContext(
		ctx,
		`SELECT default_line_id, revision, updated_at
		 FROM modemdeck_user_preferences
		 WHERE user_id = ?`,
		userID,
	).Scan(
		&settings.DefaultLineID,
		&settings.Revision,
		&settings.UpdatedAt,
	)
	if err != nil {
		return LineSettings{}, err
	}
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
