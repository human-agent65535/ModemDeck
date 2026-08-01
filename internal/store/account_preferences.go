package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

type AccountPreferencesInput struct {
	ProfileContactID         string
	Language                 SystemLanguage
	DefaultLineID            string
	ExpectedLanguageRevision int64
	ExpectedLineRevision     int64
}

type AccountPreferences struct {
	ProfileContactID string         `json:"profile_contact_id"`
	SystemSettings   SystemSettings `json:"system_settings"`
	LineSettings     LineSettings   `json:"line_settings"`
}

// UpdateAccountPreferences commits the current user's identity and direct
// preferences as one transaction. The separate revision counters are retained
// for compatibility with the focused settings endpoints.
func (s *Store) UpdateAccountPreferences(
	ctx context.Context,
	input AccountPreferencesInput,
) (AccountPreferences, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return AccountPreferences{}, ErrUserNotFound
	}
	input.ProfileContactID = strings.TrimSpace(input.ProfileContactID)
	input.DefaultLineID = strings.TrimSpace(input.DefaultLineID)
	if !validSystemLanguage(input.Language) {
		return AccountPreferences{}, fmt.Errorf(
			"%w: %q",
			ErrSystemSettingsLanguageInvalid,
			input.Language,
		)
	}
	if input.ExpectedLanguageRevision <= 0 {
		return AccountPreferences{}, fmt.Errorf(
			"%w: expected language revision must be positive",
			ErrSystemSettingsRevisionConflict,
		)
	}
	if input.ExpectedLineRevision <= 0 {
		return AccountPreferences{}, fmt.Errorf(
			"%w: expected line revision must be positive",
			ErrLineSettingsRevisionConflict,
		)
	}
	if input.DefaultLineID == "" {
		if len(principal.AllowedLineIDs) > 0 {
			return AccountPreferences{}, fmt.Errorf(
				"%w: default_line_id is required",
				ErrLineSettingsInvalidLine,
			)
		}
	} else if !principal.CanAccessLine(input.DefaultLineID) {
		return AccountPreferences{}, fmt.Errorf(
			"%w: default line is not assigned",
			ErrLineSettingsInvalidLine,
		)
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return AccountPreferences{}, fmt.Errorf("begin account preferences update: %w", err)
	}
	defer transaction.Rollback()

	if input.DefaultLineID != "" {
		if err := requireLine(ctx, transaction, input.DefaultLineID); err != nil {
			return AccountPreferences{}, err
		}
	}
	if input.ProfileContactID != "" {
		var ownerUserID string
		err := transaction.QueryRowContext(
			ctx,
			"SELECT owner_user_id FROM contacts WHERE id = ?",
			input.ProfileContactID,
		).Scan(&ownerUserID)
		if errors.Is(err, sql.ErrNoRows) || ownerUserID != principal.UserID {
			return AccountPreferences{}, ErrContactNotFound
		}
		if err != nil {
			return AccountPreferences{}, fmt.Errorf("query profile contact: %w", err)
		}
	}

	currentSystem, err := readUserSystemSettings(ctx, transaction, principal.UserID)
	if err != nil {
		return AccountPreferences{}, err
	}
	if currentSystem.Revision != input.ExpectedLanguageRevision {
		return AccountPreferences{}, &SystemSettingsRevisionConflictError{
			ExpectedRevision: input.ExpectedLanguageRevision,
			ActualRevision:   currentSystem.Revision,
		}
	}
	currentLine, err := readUserLineSettings(ctx, transaction, principal.UserID)
	if err != nil {
		return AccountPreferences{}, err
	}
	if currentLine.Revision != input.ExpectedLineRevision {
		return AccountPreferences{}, &LineSettingsRevisionConflictError{
			ExpectedRevision: input.ExpectedLineRevision,
			ActualRevision:   currentLine.Revision,
		}
	}

	result, err := transaction.ExecContext(ctx, `
		UPDATE modemdeck_user_preferences
		SET language = ?, language_revision = language_revision + 1,
			default_line_id = ?, revision = revision + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND language_revision = ? AND revision = ?
	`,
		input.Language,
		input.DefaultLineID,
		principal.UserID,
		input.ExpectedLanguageRevision,
		input.ExpectedLineRevision,
	)
	if err != nil {
		return AccountPreferences{}, fmt.Errorf("update account preferences: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return AccountPreferences{}, fmt.Errorf("read account preferences update count: %w", err)
	}
	if affected != 1 {
		actualSystem, readErr := readUserSystemSettings(ctx, transaction, principal.UserID)
		if readErr != nil {
			return AccountPreferences{}, readErr
		}
		if actualSystem.Revision != input.ExpectedLanguageRevision {
			return AccountPreferences{}, &SystemSettingsRevisionConflictError{
				ExpectedRevision: input.ExpectedLanguageRevision,
				ActualRevision:   actualSystem.Revision,
			}
		}
		actualLine, readErr := readUserLineSettings(ctx, transaction, principal.UserID)
		if readErr != nil {
			return AccountPreferences{}, readErr
		}
		return AccountPreferences{}, &LineSettingsRevisionConflictError{
			ExpectedRevision: input.ExpectedLineRevision,
			ActualRevision:   actualLine.Revision,
		}
	}

	if input.ProfileContactID == "" {
		if _, err := transaction.ExecContext(
			ctx,
			"DELETE FROM modemdeck_user_profile_contacts WHERE user_id = ?",
			principal.UserID,
		); err != nil {
			return AccountPreferences{}, fmt.Errorf("clear profile contact: %w", err)
		}
	} else if _, err := transaction.ExecContext(ctx, `
		INSERT INTO modemdeck_user_profile_contacts (
			user_id, contact_id, updated_at
		) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			contact_id = excluded.contact_id,
			updated_at = CURRENT_TIMESTAMP
	`, principal.UserID, input.ProfileContactID); err != nil {
		return AccountPreferences{}, fmt.Errorf("set profile contact: %w", err)
	}

	updatedSystem, err := readUserSystemSettings(ctx, transaction, principal.UserID)
	if err != nil {
		return AccountPreferences{}, err
	}
	updatedLine, err := readUserLineSettings(ctx, transaction, principal.UserID)
	if err != nil {
		return AccountPreferences{}, err
	}
	if err := transaction.Commit(); err != nil {
		return AccountPreferences{}, fmt.Errorf("commit account preferences: %w", err)
	}
	return AccountPreferences{
		ProfileContactID: input.ProfileContactID,
		SystemSettings:   updatedSystem,
		LineSettings:     updatedLine,
	}, nil
}
