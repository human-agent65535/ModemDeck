package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/human-agent65535/modemdeck/internal/auth"
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
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		return readUserSystemSettings(ctx, s.database, principal.UserID)
	}
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
	if principal, scoped := auth.PrincipalFromContext(ctx); scoped {
		return s.updateUserSystemSettings(
			ctx,
			principal.UserID,
			language,
			expectedRevision,
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

func (s *Store) updateUserSystemSettings(
	ctx context.Context,
	userID string,
	language SystemLanguage,
	expectedRevision int64,
) (SystemSettings, error) {
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_user_preferences
		 SET language = ?, language_revision = language_revision + 1,
			updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ? AND language_revision = ?`,
		language,
		userID,
		expectedRevision,
	)
	if err != nil {
		return SystemSettings{}, fmt.Errorf("update user system settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return SystemSettings{}, fmt.Errorf("read updated user system settings count: %w", err)
	}
	if affected != 1 {
		actual, readErr := readUserSystemSettings(ctx, s.database, userID)
		if readErr != nil {
			return SystemSettings{}, readErr
		}
		return SystemSettings{}, &SystemSettingsRevisionConflictError{
			ExpectedRevision: expectedRevision,
			ActualRevision:   actual.Revision,
		}
	}
	return readUserSystemSettings(ctx, s.database, userID)
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

func readUserSystemSettings(
	ctx context.Context,
	queryer systemSettingsQueryer,
	userID string,
) (SystemSettings, error) {
	var (
		settings  SystemSettings
		language  string
		updatedAt sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT language, language_revision, updated_at
		 FROM modemdeck_user_preferences
		 WHERE user_id = ?`,
		userID,
	).Scan(&language, &settings.Revision, &updatedAt)
	if err != nil {
		return SystemSettings{}, fmt.Errorf("read user system settings: %w", err)
	}
	settings.Language = SystemLanguage(language)
	settings.UpdatedAt = stringValue(updatedAt)
	return settings, nil
}

func validSystemLanguage(language SystemLanguage) bool {
	return language == SystemLanguageAuto ||
		language == SystemLanguageZhCN ||
		language == SystemLanguageZhTW ||
		language == SystemLanguageEnUS ||
		language == SystemLanguageJaJP ||
		language == SystemLanguageViVN ||
		language == SystemLanguageEsES ||
		language == SystemLanguageDeDE ||
		language == SystemLanguageFrFR ||
		language == SystemLanguagePtBR
}
