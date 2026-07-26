package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrSystemSettingsRevisionConflict = errors.New("system settings revision conflict")
	ErrSystemSettingsLanguageInvalid  = errors.New("system settings language is invalid")
)

type SystemSettingsRevisionConflictError struct {
	ExpectedRevision int64
	ActualRevision   int64
}

func (e *SystemSettingsRevisionConflictError) Error() string {
	if e == nil {
		return ErrSystemSettingsRevisionConflict.Error()
	}
	return fmt.Sprintf(
		"%s: expected %d, actual %d",
		ErrSystemSettingsRevisionConflict,
		e.ExpectedRevision,
		e.ActualRevision,
	)
}

func (e *SystemSettingsRevisionConflictError) Unwrap() error {
	return ErrSystemSettingsRevisionConflict
}

func (s *Store) SystemSettings(ctx context.Context) (SystemSettings, error) {
	return readSystemSettings(ctx, s.database)
}

func (s *Store) UpdateSystemSettings(
	ctx context.Context,
	language SystemLanguage,
	expectedRevision int64,
) (SystemSettings, error) {
	if !validSystemLanguage(language) {
		return SystemSettings{}, fmt.Errorf(
			"%w: %q",
			ErrSystemSettingsLanguageInvalid,
			language,
		)
	}
	if expectedRevision <= 0 {
		return SystemSettings{}, fmt.Errorf(
			"%w: expected_revision must be positive",
			ErrSystemSettingsRevisionConflict,
		)
	}

	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_system_settings
		 SET language = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND revision = ?`,
		language,
		expectedRevision,
	)
	if err != nil {
		return SystemSettings{}, fmt.Errorf("update system settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return SystemSettings{}, fmt.Errorf("read updated system settings count: %w", err)
	}
	if affected != 1 {
		actual, readErr := readSystemSettings(ctx, s.database)
		if readErr != nil {
			return SystemSettings{}, readErr
		}
		return SystemSettings{}, &SystemSettingsRevisionConflictError{
			ExpectedRevision: expectedRevision,
			ActualRevision:   actual.Revision,
		}
	}
	return readSystemSettings(ctx, s.database)
}

type systemSettingsQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readSystemSettings(
	ctx context.Context,
	queryer systemSettingsQueryer,
) (SystemSettings, error) {
	var (
		settings  SystemSettings
		language  string
		updatedAt sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT language, revision, updated_at
		 FROM modemdeck_system_settings
		 WHERE singleton = 1`,
	).Scan(&language, &settings.Revision, &updatedAt)
	if err != nil {
		return SystemSettings{}, fmt.Errorf("read system settings: %w", err)
	}
	settings.Language = SystemLanguage(language)
	settings.UpdatedAt = stringValue(updatedAt)
	return settings, nil
}

func validSystemLanguage(language SystemLanguage) bool {
	return language == SystemLanguageAuto ||
		language == SystemLanguageZhCN ||
		language == SystemLanguageEnUS
}
