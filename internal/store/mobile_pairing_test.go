package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

func TestIOSPairingPermissionAndRevocationLifecycle(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "owner",
		PasswordHash: "owner-hash",
	})
	if err != nil || !created {
		t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
	}
	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "member",
		PasswordHash: "member-hash",
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	status, err := repository.IOSPairingStatus(ctx, member.ID)
	if err != nil {
		t.Fatalf("IOSPairingStatus() error = %v", err)
	}
	if status.Allowed || status.HasCredential {
		t.Fatalf("initial pairing status = %+v", status)
	}
	digest := mobilepairing.TokenDigest{1, 2, 3}
	if _, err := repository.RotateIOSPairingCredential(
		ctx,
		member.ID,
		digest,
	); !errors.Is(err, ErrIOSPairingNotAllowed) {
		t.Fatalf("disallowed pairing error = %v", err)
	}

	member, err = repository.UpdateMember(ctx, member.ID, UpdateMemberInput{
		Username:          member.Username,
		Enabled:           true,
		IOSPairingEnabled: true,
		LineIDs:           member.LineIDs,
		Revision:          member.Revision,
	})
	if err != nil {
		t.Fatalf("enable iOS pairing: %v", err)
	}
	status, err = repository.RotateIOSPairingCredential(ctx, member.ID, digest)
	if err != nil {
		t.Fatalf("RotateIOSPairingCredential() error = %v", err)
	}
	if !status.Allowed ||
		!status.HasCredential ||
		status.CredentialCreatedAt == "" {
		t.Fatalf("paired status = %+v", status)
	}
	if _, err := time.Parse(time.RFC3339, status.CredentialCreatedAt); err != nil {
		t.Fatalf(
			"credential timestamp = %q, want RFC3339: %v",
			status.CredentialCreatedAt,
			err,
		)
	}
	member, err = repository.User(ctx, member.ID)
	if err != nil {
		t.Fatalf("User() after pairing error = %v", err)
	}
	if !member.IOSPairingHasCredential ||
		member.IOSPairingCredentialCreatedAt != status.CredentialCreatedAt {
		t.Fatalf("user pairing status = %+v, want credential at %q", member, status.CredentialCreatedAt)
	}

	status, err = repository.IOSPairingStatus(ctx, member.ID)
	if err != nil || !status.HasCredential {
		t.Fatalf("persisted credential = %+v, %v", status, err)
	}

	if _, err := repository.RotateIOSPairingCredential(
		ctx,
		member.ID,
		mobilepairing.TokenDigest{4, 5, 6},
	); err != nil {
		t.Fatalf("rotate credential again: %v", err)
	}
	member, err = repository.UpdateMember(ctx, member.ID, UpdateMemberInput{
		Username:          member.Username,
		Enabled:           true,
		IOSPairingEnabled: false,
		LineIDs:           member.LineIDs,
		Revision:          member.Revision,
	})
	if err != nil {
		t.Fatalf("disable iOS pairing: %v", err)
	}
	status, err = repository.IOSPairingStatus(ctx, member.ID)
	if err != nil || status.Allowed || status.HasCredential {
		t.Fatalf("disabled pairing status = %+v, %v", status, err)
	}
	member, err = repository.User(ctx, member.ID)
	if err != nil {
		t.Fatalf("User() after disabling pairing error = %v", err)
	}
	if member.IOSPairingHasCredential ||
		member.IOSPairingCredentialCreatedAt != "" {
		t.Fatalf("disabled user pairing status = %+v", member)
	}
}

func TestIOSPairingPrincipalLookupFollowsCredentialAndPermission(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "owner",
		PasswordHash: "owner-hash",
	})
	if err != nil || !created {
		t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
	}
	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:          "member",
		PasswordHash:      "member-hash",
		IOSPairingEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	digest := mobilepairing.TokenDigest{1, 2, 3}
	if _, err := repository.RotateIOSPairingCredential(
		ctx,
		member.ID,
		digest,
	); err != nil {
		t.Fatalf("RotateIOSPairingCredential() error = %v", err)
	}
	principal, found, err := repository.IOSPairingPrincipalByTokenDigest(
		ctx,
		digest,
	)
	if err != nil || !found {
		t.Fatalf(
			"IOSPairingPrincipalByTokenDigest() = %+v, %t, %v",
			principal,
			found,
			err,
		)
	}
	if principal.UserID != member.ID ||
		principal.Username != member.Username ||
		principal.Role != auth.RoleMember ||
		!principal.IOSPairingEnabled {
		t.Fatalf("principal = %+v", principal)
	}

	if err := repository.RevokeIOSPairingCredential(ctx, member.ID); err != nil {
		t.Fatalf("RevokeIOSPairingCredential() error = %v", err)
	}
	if _, found, err := repository.IOSPairingPrincipalByTokenDigest(
		ctx,
		digest,
	); err != nil || found {
		t.Fatalf("revoked credential found = %t, error = %v", found, err)
	}
}

func TestPasswordChangesRevokeIOSPairingCredentials(t *testing.T) {
	t.Run("administrator reset", func(t *testing.T) {
		repository, _ := newContactTestStore(t)
		ctx := context.Background()
		if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
			Username:     "owner",
			PasswordHash: "owner-hash",
		}); err != nil || !created {
			t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
		}
		member, err := repository.CreateMember(ctx, CreateMemberInput{
			Username:          "member",
			PasswordHash:      "member-hash",
			IOSPairingEnabled: true,
		})
		if err != nil {
			t.Fatalf("CreateMember() error = %v", err)
		}
		if _, err := repository.RotateIOSPairingCredential(
			ctx,
			member.ID,
			mobilepairing.TokenDigest{1},
		); err != nil {
			t.Fatalf("RotateIOSPairingCredential() error = %v", err)
		}
		if err := repository.SetMemberPassword(
			ctx,
			member.ID,
			"replacement-hash",
		); err != nil {
			t.Fatalf("SetMemberPassword() error = %v", err)
		}
		status, err := repository.IOSPairingStatus(ctx, member.ID)
		if err != nil || status.HasCredential {
			t.Fatalf("pairing after administrator reset = %+v, %v", status, err)
		}
	})

	t.Run("profile update with password", func(t *testing.T) {
		repository, _ := newContactTestStore(t)
		ctx := context.Background()
		if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
			Username:     "owner",
			PasswordHash: "owner-hash",
		}); err != nil || !created {
			t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
		}
		member, err := repository.CreateMember(ctx, CreateMemberInput{
			Username:          "member",
			PasswordHash:      "member-hash",
			IOSPairingEnabled: true,
		})
		if err != nil {
			t.Fatalf("CreateMember() error = %v", err)
		}
		if _, err := repository.RotateIOSPairingCredential(
			ctx,
			member.ID,
			mobilepairing.TokenDigest{2},
		); err != nil {
			t.Fatalf("RotateIOSPairingCredential() error = %v", err)
		}
		if _, err := repository.UpdateMember(ctx, member.ID, UpdateMemberInput{
			Username:          member.Username,
			PasswordHash:      "replacement-hash",
			Enabled:           true,
			IOSPairingEnabled: true,
			LineIDs:           member.LineIDs,
			Revision:          member.Revision,
		}); err != nil {
			t.Fatalf("UpdateMember() error = %v", err)
		}
		status, err := repository.IOSPairingStatus(ctx, member.ID)
		if err != nil || status.HasCredential {
			t.Fatalf("pairing after profile password update = %+v, %v", status, err)
		}
	})

	t.Run("self-service change", func(t *testing.T) {
		repository, _ := newContactTestStore(t)
		ctx := context.Background()
		if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
			Username:     "owner",
			PasswordHash: "owner-hash",
		}); err != nil || !created {
			t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
		}
		member, err := repository.CreateMember(ctx, CreateMemberInput{
			Username:          "member",
			PasswordHash:      "member-hash",
			IOSPairingEnabled: true,
		})
		if err != nil {
			t.Fatalf("CreateMember() error = %v", err)
		}
		if _, err := repository.RotateIOSPairingCredential(
			ctx,
			member.ID,
			mobilepairing.TokenDigest{3},
		); err != nil {
			t.Fatalf("RotateIOSPairingCredential() error = %v", err)
		}
		replaced, err := repository.ReplaceUserPasswordHashIfCurrentAndRevokeSessions(
			ctx,
			member.ID,
			"member-hash",
			"replacement-hash",
		)
		if err != nil || !replaced {
			t.Fatalf(
				"ReplaceUserPasswordHashIfCurrentAndRevokeSessions() = %t, %v",
				replaced,
				err,
			)
		}
		status, err := repository.IOSPairingStatus(ctx, member.ID)
		if err != nil || status.HasCredential {
			t.Fatalf("pairing after self-service change = %+v, %v", status, err)
		}
	})
}
