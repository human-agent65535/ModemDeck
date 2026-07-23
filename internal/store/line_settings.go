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
	ErrLineSettingsInvalidDevice    = errors.New("line settings device is invalid")
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
	defaultDeviceIMEI string,
	expectedRevision int64,
) (LineSettings, error) {
	defaultDeviceIMEI = strings.TrimSpace(defaultDeviceIMEI)
	if defaultDeviceIMEI == "" {
		return LineSettings{}, fmt.Errorf("%w: default_device_imei is required", ErrLineSettingsInvalidDevice)
	}
	if expectedRevision <= 0 {
		return LineSettings{}, fmt.Errorf("%w: expected_revision must be positive", ErrLineSettingsRevisionConflict)
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return LineSettings{}, fmt.Errorf("begin update line settings: %w", err)
	}
	defer transaction.Rollback()

	if err := requireDevice(ctx, transaction, defaultDeviceIMEI); err != nil {
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
		 SET default_device_imei = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE singleton = 1 AND revision = ?`,
		defaultDeviceIMEI,
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
		settings   LineSettings
		deviceIMEI sql.NullString
		updatedAt  sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT default_device_imei, revision, updated_at
		 FROM modemdeck_line_settings
		 WHERE singleton = 1`,
	).Scan(&deviceIMEI, &settings.Revision, &updatedAt)
	if err != nil {
		return LineSettings{}, fmt.Errorf("read line settings: %w", err)
	}
	settings.DefaultDeviceIMEI = stringValue(deviceIMEI)
	settings.UpdatedAt = stringValue(updatedAt)
	return settings, nil
}

func requireDevice(ctx context.Context, queryer lineSettingsQueryer, imei string) error {
	var exists int
	err := queryer.QueryRowContext(
		ctx,
		"SELECT 1 FROM devices WHERE imei = ?",
		strings.TrimSpace(imei),
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrLineSettingsInvalidDevice, imei)
	}
	if err != nil {
		return fmt.Errorf("validate line settings device: %w", err)
	}
	return nil
}
