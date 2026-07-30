package store

import (
	"context"
	"errors"
	"testing"

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
