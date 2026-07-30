package store

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

func TestInitialAdministratorReceivesExistingLinesAndCanRemoveThem(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	insertTestLine(t, database, "line_alpha", "+819011111111")
	insertTestLine(t, database, "line_beta", "+819022222222")

	created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "owner",
		PasswordHash: "owner-hash",
	})
	if err != nil || !created {
		t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
	}
	admin, err := repository.User(ctx, auth.InitialAdminUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LineIDs) != 2 ||
		!containsString(admin.LineIDs, "line_alpha") ||
		!containsString(admin.LineIDs, "line_beta") {
		t.Fatalf("initial administrator lines = %v, want both existing lines", admin.LineIDs)
	}

	admin, err = repository.UpdateMember(ctx, admin.ID, UpdateMemberInput{
		Username: admin.Username,
		Enabled:  true,
		LineIDs:  []string{"line_alpha"},
		Revision: admin.Revision,
	})
	if err != nil {
		t.Fatalf("remove administrator line: %v", err)
	}
	if len(admin.LineIDs) != 1 || admin.LineIDs[0] != "line_alpha" {
		t.Fatalf("updated administrator lines = %v, want line_alpha", admin.LineIDs)
	}

	adminContext := auth.ContextWithPrincipal(ctx, auth.Principal{
		UserID:         admin.ID,
		Username:       admin.Username,
		Role:           auth.RoleAdmin,
		AllowedLineIDs: append([]string(nil), admin.LineIDs...),
	})
	if _, err := repository.CreateContact(adminContext, ContactInput{
		DisplayName:     "Unassigned line",
		PreferredLineID: "line_beta",
		Phones: []ContactPhoneInput{{
			Number:  "+81 90 0000 0001",
			Primary: true,
		}},
	}); err == nil {
		t.Fatal("administrator created a contact with an unassigned preferred line")
	}
}

func TestMemberLineAssignmentPreservesPersonalDefaultUntilRemoved(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	insertTestLine(t, database, "line_alpha", "+819011111111")
	insertTestLine(t, database, "line_beta", "+819022222222")
	insertTestLine(t, database, "line_gamma", "+819033333333")

	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "member",
		PasswordHash: "member-hash",
		LineIDs:      []string{"line_alpha", "line_beta"},
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	memberContext := memberTestContext(member)
	settings, err := repository.LineSettings(memberContext)
	if err != nil {
		t.Fatalf("LineSettings() error = %v", err)
	}
	if _, err := repository.UpdateLineSettings(
		memberContext,
		"line_beta",
		settings.Revision,
	); err != nil {
		t.Fatalf("UpdateLineSettings() error = %v", err)
	}

	member, err = repository.UpdateMember(ctx, member.ID, UpdateMemberInput{
		Username: member.Username,
		Enabled:  true,
		LineIDs:  []string{"line_beta", "line_gamma"},
		Revision: member.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateMember(preserve default) error = %v", err)
	}
	memberContext = memberTestContext(member)
	settings, err = repository.LineSettings(memberContext)
	if err != nil {
		t.Fatalf("LineSettings(after assignment) error = %v", err)
	}
	if settings.DefaultLineID != "line_beta" {
		t.Fatalf("default line = %q, want preserved line_beta", settings.DefaultLineID)
	}

	member, err = repository.UpdateMember(ctx, member.ID, UpdateMemberInput{
		Username: member.Username,
		Enabled:  true,
		LineIDs:  []string{"line_gamma", "line_alpha"},
		Revision: member.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateMember(remove default) error = %v", err)
	}
	memberContext = memberTestContext(member)
	settings, err = repository.LineSettings(memberContext)
	if err != nil {
		t.Fatalf("LineSettings(after default removal) error = %v", err)
	}
	if settings.DefaultLineID != "line_gamma" {
		t.Fatalf("default line = %q, want fallback line_gamma", settings.DefaultLineID)
	}
}

func TestNewStableLineIsAssignedToAdministratorOnlyOnce(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "owner",
		PasswordHash: "owner-hash",
	})
	if err != nil || !created {
		t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
	}

	resolve := func(number string) string {
		t.Helper()
		transaction, txErr := database.BeginTx(ctx, nil)
		if txErr != nil {
			t.Fatal(txErr)
		}
		lineID, resolveErr := resolveOrCreateStableLine(
			ctx,
			transaction,
			"",
			"",
			number,
			"JP",
			time.Now().UTC(),
		)
		if resolveErr != nil {
			_ = transaction.Rollback()
			t.Fatal(resolveErr)
		}
		if commitErr := transaction.Commit(); commitErr != nil {
			t.Fatal(commitErr)
		}
		return lineID
	}

	firstLineID := resolve("+81 90 1111 1111")
	admin, err := repository.User(ctx, auth.InitialAdminUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LineIDs) != 1 || admin.LineIDs[0] != firstLineID {
		t.Fatalf("new line assignment = %v, want %s", admin.LineIDs, firstLineID)
	}
	admin, err = repository.UpdateMember(ctx, admin.ID, UpdateMemberInput{
		Username: admin.Username,
		Enabled:  true,
		LineIDs:  nil,
		Revision: admin.Revision,
	})
	if err != nil {
		t.Fatalf("remove new line assignment: %v", err)
	}

	if resolved := resolve("+81 90 1111 1111"); resolved != firstLineID {
		t.Fatalf("rediscovered line = %q, want %q", resolved, firstLineID)
	}
	admin, err = repository.User(ctx, auth.InitialAdminUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LineIDs) != 0 {
		t.Fatalf("rediscovery restored removed assignment: %v", admin.LineIDs)
	}

	secondLineID := resolve("+81 90 2222 2222")
	admin, err = repository.User(ctx, auth.InitialAdminUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LineIDs) != 1 || admin.LineIDs[0] != secondLineID {
		t.Fatalf("second new line assignment = %v, want %s", admin.LineIDs, secondLineID)
	}
}
