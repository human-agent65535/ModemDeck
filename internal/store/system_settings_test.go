package store

import (
	"context"
	"errors"
	"testing"
)

func TestSystemSettingsPersistLanguageAndRejectStaleRevision(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()

	initial, err := repository.SystemSettings(ctx)
	if err != nil {
		t.Fatalf("SystemSettings() error = %v", err)
	}
	if initial.Language != SystemLanguageAuto || initial.Revision != 1 {
		t.Fatalf("initial settings = %+v", initial)
	}

	updated, err := repository.UpdateSystemSettings(
		ctx,
		SystemLanguageEnUS,
		initial.Revision,
	)
	if err != nil {
		t.Fatalf("UpdateSystemSettings() error = %v", err)
	}
	if updated.Language != SystemLanguageEnUS || updated.Revision != 2 {
		t.Fatalf("updated settings = %+v", updated)
	}

	if _, err := repository.UpdateSystemSettings(
		ctx,
		SystemLanguageZhCN,
		initial.Revision,
	); !errors.Is(err, ErrSystemSettingsRevisionConflict) {
		t.Fatalf("stale UpdateSystemSettings() error = %v", err)
	}
	if _, err := repository.UpdateSystemSettings(
		ctx,
		SystemLanguage("ja-JP"),
		updated.Revision,
	); !errors.Is(err, ErrSystemSettingsLanguageInvalid) {
		t.Fatalf("invalid UpdateSystemSettings() error = %v", err)
	}
}
