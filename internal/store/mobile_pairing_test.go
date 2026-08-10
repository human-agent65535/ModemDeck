package store

import (
	"context"
	"errors"
	"strings"
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
	if _, err := repository.CreateIOSPairingCredential(
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
	status, err = repository.CreateIOSPairingCredential(ctx, member.ID, digest)
	if err != nil {
		t.Fatalf("CreateIOSPairingCredential() error = %v", err)
	}
	if !status.Allowed ||
		!status.HasCredential ||
		status.CredentialCreatedAt == "" ||
		status.Paired ||
		status.Pending == nil ||
		status.DeviceLimit != MaxIOSPairingDevices {
		t.Fatalf("pending pairing status = %+v", status)
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
		member.IOSPairingPaired ||
		member.IOSPairingCredentialCreatedAt != status.CredentialCreatedAt {
		t.Fatalf("user pairing status = %+v, want credential at %q", member, status.CredentialCreatedAt)
	}

	device := mobilepairing.DeviceInfo{
		Name:            "Test iPhone",
		Model:           "iPhone",
		ModelIdentifier: "iPhone18,2",
		OSName:          "iOS",
		OSVersion:       "26.0",
		AppVersion:      "0.1.0",
		AppBuild:        "1",
	}
	confirmed, err := repository.ConfirmIOSPairingCredential(ctx, digest, device)
	if err != nil || !confirmed {
		t.Fatalf("ConfirmIOSPairingCredential() = %t, %v", confirmed, err)
	}
	device.AppBuild = "2"
	confirmed, err = repository.ConfirmIOSPairingCredential(ctx, digest, device)
	if err != nil || confirmed {
		t.Fatalf("second confirmation = %t, %v", confirmed, err)
	}
	status, err = repository.IOSPairingStatus(ctx, member.ID)
	if err != nil ||
		!status.HasCredential ||
		!status.Paired ||
		status.PairedAt == "" ||
		status.LastSeenAt == "" ||
		status.Device != device ||
		len(status.Devices) != 1 ||
		status.Pending != nil {
		t.Fatalf("confirmed credential = %+v, %v", status, err)
	}
	if _, err := time.Parse(time.RFC3339, status.PairedAt); err != nil {
		t.Fatalf("paired timestamp = %q, want RFC3339: %v", status.PairedAt, err)
	}
	member, err = repository.User(ctx, member.ID)
	if err != nil {
		t.Fatalf("User() after confirmation error = %v", err)
	}
	if !member.IOSPairingPaired ||
		member.IOSPairingDeviceCount != 1 ||
		member.IOSPairingPending ||
		member.IOSPairingPairedAt != status.PairedAt {
		t.Fatalf("confirmed user pairing status = %+v", member)
	}

	firstCredentialID := status.Devices[0].ID
	secondDigest := mobilepairing.TokenDigest{4, 5, 6}
	status, err = repository.CreateIOSPairingCredential(
		ctx,
		member.ID,
		secondDigest,
	)
	if err != nil {
		t.Fatalf("create second credential: %v", err)
	}
	if !status.Paired || len(status.Devices) != 1 ||
		status.Devices[0].ID != firstCredentialID || status.Pending == nil {
		t.Fatalf("second pending credential status = %+v", status)
	}
	secondPendingID := status.Pending.ID
	replacementDigest := mobilepairing.TokenDigest{4, 5, 7}
	status, err = repository.CreateIOSPairingCredential(
		ctx,
		member.ID,
		replacementDigest,
	)
	if err != nil {
		t.Fatalf("replace second pending credential: %v", err)
	}
	if len(status.Devices) != 1 ||
		status.Devices[0].ID != firstCredentialID ||
		status.Pending == nil ||
		status.Pending.ID == secondPendingID {
		t.Fatalf("replaced pending credential status = %+v", status)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		secondDigest,
		mobilepairing.DeviceInfo{Name: "Replaced iPad", Model: "iPad"},
	); err != nil || confirmed {
		t.Fatalf("replaced credential confirmation = %t, %v", confirmed, err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		replacementDigest,
		mobilepairing.DeviceInfo{Name: "Test iPad", Model: "iPad"},
	); err != nil || !confirmed {
		t.Fatalf("confirm second credential = %t, %v", confirmed, err)
	}
	status, err = repository.IOSPairingStatus(ctx, member.ID)
	if err != nil || len(status.Devices) != 2 || status.Pending != nil {
		t.Fatalf("two paired devices = %+v, %v", status, err)
	}
	thirdDigest := mobilepairing.TokenDigest{7, 8, 9}
	if _, err := repository.CreateIOSPairingCredential(ctx, member.ID, thirdDigest); err != nil {
		t.Fatalf("create third credential: %v", err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		thirdDigest,
		mobilepairing.DeviceInfo{Name: "Backup iPhone", Model: "iPhone"},
	); err != nil || !confirmed {
		t.Fatalf("confirm third credential = %t, %v", confirmed, err)
	}
	if _, err := repository.CreateIOSPairingCredential(
		ctx,
		member.ID,
		mobilepairing.TokenDigest{10, 11, 12},
	); !errors.Is(err, ErrIOSPairingDeviceLimit) {
		t.Fatalf("fourth credential error = %v, want device limit", err)
	}
	status, err = repository.IOSPairingStatus(ctx, member.ID)
	if err != nil || len(status.Devices) != MaxIOSPairingDevices || status.Pending != nil {
		t.Fatalf("maximum paired device status = %+v, %v", status, err)
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
	if _, err := repository.CreateIOSPairingCredential(
		ctx,
		member.ID,
		digest,
	); err != nil {
		t.Fatalf("CreateIOSPairingCredential() error = %v", err)
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

	digests, err := repository.RevokeAllIOSPairingCredentials(ctx, member.ID)
	if err != nil || len(digests) != 1 || digests[0] != digest {
		t.Fatalf("RevokeAllIOSPairingCredentials() = %x, %v", digests, err)
	}
	if _, found, err := repository.IOSPairingPrincipalByTokenDigest(
		ctx,
		digest,
	); err != nil || found {
		t.Fatalf("revoked credential found = %t, error = %v", found, err)
	}
}

func TestIOSPairingCredentialHasNoTimeExpiry(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
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
	digest := mobilepairing.TokenDigest{1, 2, 3}
	if _, err := repository.CreateIOSPairingCredential(
		ctx,
		member.ID,
		digest,
	); err != nil {
		t.Fatalf("CreateIOSPairingCredential() error = %v", err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		digest,
		mobilepairing.DeviceInfo{},
	); err != nil || !confirmed {
		t.Fatalf("ConfirmIOSPairingCredential() = %t, %v", confirmed, err)
	}
	status, err := repository.IOSPairingStatus(ctx, member.ID)
	if err != nil || len(status.Devices) != 1 {
		t.Fatalf("IOSPairingStatus() = %+v, %v", status, err)
	}
	if _, err := database.ExecContext(
		ctx,
		`UPDATE modemdeck_ios_pairing_credentials
		 SET created_at = '2000-01-01 00:00:00',
			activated_at = '2000-01-01 00:00:00',
			updated_at = '2000-01-01 00:00:00'
		 WHERE user_id = ?`,
		member.ID,
	); err != nil {
		t.Fatalf("age pairing credential: %v", err)
	}

	principal, found, err := repository.IOSPairingPrincipalByTokenDigest(
		ctx,
		digest,
	)
	if err != nil || !found || principal.UserID != member.ID {
		t.Fatalf(
			"aged IOSPairingPrincipalByTokenDigest() = %+v, %t, %v",
			principal,
			found,
			err,
		)
	}
}

func TestIOSPushTargetsAreScopedByLineAndTokenKind(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "owner",
		PasswordHash: "owner-hash",
	}); err != nil || !created {
		t.Fatalf("CreateAdminIfAbsent() = %t, %v", created, err)
	}
	line := policyTestLine()
	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-ios-push",
		Revision:   "snapshot-ios-push",
		ObservedAt: time.Date(2026, time.August, 9, 2, 0, 0, 0, time.UTC),
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatalf("ApplyHardwareSnapshotWithResult() error = %v", err)
	}
	lineID := result.LineIDsByEndpoint[line.ID]
	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:          "push-member",
		PasswordHash:      "member-hash",
		IOSPairingEnabled: true,
		LineIDs:           []string{lineID},
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	digest := mobilepairing.TokenDigest{9, 8, 7}
	if _, err := repository.CreateIOSPairingCredential(ctx, member.ID, digest); err != nil {
		t.Fatalf("CreateIOSPairingCredential() error = %v", err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		digest,
		mobilepairing.DeviceInfo{},
	); err != nil || !confirmed {
		t.Fatalf("ConfirmIOSPairingCredential() = %t, %v", confirmed, err)
	}
	status, err := repository.IOSPairingStatus(ctx, member.ID)
	if err != nil || len(status.Devices) != 1 {
		t.Fatalf("IOSPairingStatus() = %+v, %v", status, err)
	}
	credentialID := status.Devices[0].ID
	apnsToken := strings.Repeat("ab", 32)
	voipToken := strings.Repeat("cd", 32)
	if err := repository.UpdateIOSPushRegistration(ctx, digest, mobilepairing.PushRegistration{
		APNSToken:   apnsToken,
		VoIPToken:   voipToken,
		Environment: "production",
		BundleID:    "com.example.modemdeck",
	}); err != nil {
		t.Fatalf("UpdateIOSPushRegistration() error = %v", err)
	}

	assertTarget := func(kind IOSPushTokenKind, token string) {
		t.Helper()
		targets, err := repository.IOSPushTargetsForLine(ctx, lineID, kind)
		if err != nil {
			t.Fatalf("IOSPushTargetsForLine(%q) error = %v", kind, err)
		}
		if len(targets) != 1 || targets[0] != (IOSPushTarget{
			CredentialID: credentialID,
			UserID:       member.ID,
			Token:        token,
			Environment:  "production",
			BundleID:     "com.example.modemdeck",
		}) {
			t.Fatalf("IOSPushTargetsForLine(%q) = %+v", kind, targets)
		}
		target, found, err := repository.IOSPushTargetForCredential(
			ctx,
			member.ID,
			credentialID,
			kind,
		)
		if err != nil || !found || target != targets[0] {
			t.Fatalf("IOSPushTargetForCredential(%q) = %+v, %t, %v", kind, target, found, err)
		}
	}
	assertTarget(IOSPushTokenAPNS, apnsToken)
	assertTarget(IOSPushTokenVoIP, voipToken)

	secondDigest := mobilepairing.TokenDigest{6, 5, 4}
	if _, err := repository.CreateIOSPairingCredential(
		ctx,
		member.ID,
		secondDigest,
	); err != nil {
		t.Fatalf("CreateIOSPairingCredential(second) error = %v", err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		secondDigest,
		mobilepairing.DeviceInfo{Name: "Test iPad", Model: "iPad"},
	); err != nil || !confirmed {
		t.Fatalf("ConfirmIOSPairingCredential(second) = %t, %v", confirmed, err)
	}
	secondCredentialID, found, err := repository.IOSPairingCredentialIDByTokenDigest(
		ctx,
		secondDigest,
	)
	if err != nil || !found {
		t.Fatalf("IOSPairingCredentialIDByTokenDigest(second) = %q, %t, %v", secondCredentialID, found, err)
	}
	secondAPNSToken := strings.Repeat("ef", 32)
	secondVoIPToken := strings.Repeat("12", 32)
	if err := repository.UpdateIOSPushRegistration(
		ctx,
		secondDigest,
		mobilepairing.PushRegistration{
			APNSToken:   secondAPNSToken,
			VoIPToken:   secondVoIPToken,
			Environment: "production",
			BundleID:    "com.example.modemdeck",
		},
	); err != nil {
		t.Fatalf("UpdateIOSPushRegistration(second) error = %v", err)
	}
	for _, testCase := range []struct {
		kind   IOSPushTokenKind
		tokens map[string]string
	}{
		{
			kind: IOSPushTokenAPNS,
			tokens: map[string]string{
				credentialID:       apnsToken,
				secondCredentialID: secondAPNSToken,
			},
		},
		{
			kind: IOSPushTokenVoIP,
			tokens: map[string]string{
				credentialID:       voipToken,
				secondCredentialID: secondVoIPToken,
			},
		},
	} {
		targets, err := repository.IOSPushTargetsForLine(ctx, lineID, testCase.kind)
		if err != nil || len(targets) != 2 {
			t.Fatalf("multi-device IOSPushTargetsForLine(%q) = %+v, %v", testCase.kind, targets, err)
		}
		for _, target := range targets {
			if target.UserID != member.ID ||
				target.Token != testCase.tokens[target.CredentialID] ||
				target.Environment != "production" ||
				target.BundleID != "com.example.modemdeck" {
				t.Fatalf("multi-device push target = %+v", target)
			}
		}
	}
	if revoked, err := repository.RevokeIOSPairingCredentialByTokenDigest(
		ctx,
		secondDigest,
	); err != nil || !revoked {
		t.Fatalf("RevokeIOSPairingCredentialByTokenDigest(second) = %t, %v", revoked, err)
	}
	assertTarget(IOSPushTokenAPNS, apnsToken)
	assertTarget(IOSPushTokenVoIP, voipToken)

	if err := repository.ClearIOSPushToken(ctx, member.ID, IOSPushTokenAPNS, "stale-token"); err != nil {
		t.Fatalf("ClearIOSPushToken(stale) error = %v", err)
	}
	assertTarget(IOSPushTokenAPNS, apnsToken)
	if err := repository.ClearIOSPushToken(ctx, member.ID, IOSPushTokenAPNS, apnsToken); err != nil {
		t.Fatalf("ClearIOSPushToken() error = %v", err)
	}
	if targets, err := repository.IOSPushTargetsForLine(ctx, lineID, IOSPushTokenAPNS); err != nil || len(targets) != 0 {
		t.Fatalf("cleared APNs targets = %+v, %v", targets, err)
	}
	if target, found, err := repository.IOSPushTargetForCredential(
		ctx,
		member.ID,
		credentialID,
		IOSPushTokenAPNS,
	); err != nil || found {
		t.Fatalf("cleared APNs user target = %+v, %t, %v", target, found, err)
	}
	assertTarget(IOSPushTokenVoIP, voipToken)
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
		if _, err := repository.CreateIOSPairingCredential(
			ctx,
			member.ID,
			mobilepairing.TokenDigest{1},
		); err != nil {
			t.Fatalf("CreateIOSPairingCredential() error = %v", err)
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
		if _, err := repository.CreateIOSPairingCredential(
			ctx,
			member.ID,
			mobilepairing.TokenDigest{2},
		); err != nil {
			t.Fatalf("CreateIOSPairingCredential() error = %v", err)
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
		if _, err := repository.CreateIOSPairingCredential(
			ctx,
			member.ID,
			mobilepairing.TokenDigest{3},
		); err != nil {
			t.Fatalf("CreateIOSPairingCredential() error = %v", err)
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
