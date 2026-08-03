package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

func TestAuthStorePasswordAndSessionLifecycle(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	if credentials, configured, err := repository.AdminCredentials(ctx); err != nil || configured || credentials != (auth.AdminCredentials{}) {
		t.Fatalf("initial AdminCredentials() = %+v, %v, %v", credentials, configured, err)
	}
	created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "owner",
		PasswordHash: "hash-1",
	})
	if err != nil || !created {
		t.Fatalf("CreateAdminIfAbsent() = %v, %v", created, err)
	}
	created, err = repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "other",
		PasswordHash: "hash-other",
	})
	if err != nil || created {
		t.Fatalf("second CreateAdminIfAbsent() = %v, %v", created, err)
	}
	if credentials, configured, err := repository.AdminCredentials(ctx); err != nil ||
		!configured ||
		credentials != (auth.AdminCredentials{Username: "owner", PasswordHash: "hash-1"}) {
		t.Fatalf("AdminCredentials() = %+v, %v, %v", credentials, configured, err)
	}

	createdAt := time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC)
	session := auth.SessionRecord{
		SessionTokenDigest: auth.SessionTokenDigest{1, 2, 3},
		CSRFTokenDigest:    auth.CSRFTokenDigest{4, 5, 6},
		CreatedAt:          createdAt,
		LastSeenAt:         createdAt,
		UserAgent:          "Test Browser",
		AccessIP:           "192.0.2.7",
		AccessHost:         "call.example.test",
	}
	created, err = repository.CreateSessionIfPasswordHash(ctx, "wrong-hash", session)
	if err != nil || created {
		t.Fatalf("CreateSessionIfPasswordHash(wrong) = %v, %v", created, err)
	}
	created, err = repository.CreateSessionIfPasswordHash(ctx, "hash-1", session)
	if err != nil || !created {
		t.Fatalf("CreateSessionIfPasswordHash() = %v, %v", created, err)
	}
	stored, found, err := repository.SessionByTokenDigest(ctx, session.SessionTokenDigest)
	if err != nil || !found {
		t.Fatalf("SessionByTokenDigest() = %+v, %v, %v", stored, found, err)
	}
	if stored.CSRFTokenDigest != session.CSRFTokenDigest ||
		!stored.CreatedAt.Equal(session.CreatedAt) ||
		!stored.LastSeenAt.Equal(session.LastSeenAt) ||
		stored.UserAgent != session.UserAgent ||
		stored.AccessIP != session.AccessIP ||
		stored.AccessHost != session.AccessHost {
		t.Fatalf("stored session = %+v, want %+v", stored, session)
	}
	updatedAt := createdAt.Add(6 * time.Minute)
	if err := repository.UpdateSessionMetadata(ctx, session.SessionTokenDigest, auth.SessionClient{
		UserAgent:  "Updated Browser",
		AccessIP:   "198.51.100.7",
		AccessHost: "updated.example.test",
	}, updatedAt); err != nil {
		t.Fatalf("UpdateSessionMetadata() error = %v", err)
	}
	stored, found, err = repository.SessionByTokenDigest(ctx, session.SessionTokenDigest)
	if err != nil || !found ||
		!stored.LastSeenAt.Equal(updatedAt) ||
		stored.UserAgent != "Updated Browser" ||
		stored.AccessIP != "198.51.100.7" ||
		stored.AccessHost != "updated.example.test" {
		t.Fatalf("updated session = %+v, found %t, err %v", stored, found, err)
	}

	if replaced, err := repository.ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
		ctx,
		"wrong-hash",
		"hash-2",
	); err != nil || replaced {
		t.Fatalf("conditional replacement with stale hash = %v, %v", replaced, err)
	}
	if _, found, err := repository.SessionByTokenDigest(ctx, session.SessionTokenDigest); err != nil || !found {
		t.Fatalf("stale replacement revoked session; found = %v, err = %v", found, err)
	}
	if replaced, err := repository.ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
		ctx,
		"hash-1",
		"hash-2",
	); err != nil || !replaced {
		t.Fatalf("conditional replacement = %v, %v", replaced, err)
	}
	if _, found, err := repository.SessionByTokenDigest(ctx, session.SessionTokenDigest); err != nil || found {
		t.Fatalf("revoked SessionByTokenDigest() found = %v, err = %v", found, err)
	}
	if err := repository.DeleteSessionByTokenDigest(ctx, session.SessionTokenDigest); err != nil {
		t.Fatalf("idempotent DeleteSessionByTokenDigest() error = %v", err)
	}
}

func TestAuthStoreRejectsInvalidSession(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	_, err := repository.CreateSessionIfPasswordHash(context.Background(), "hash", auth.SessionRecord{})
	if !errors.Is(err, ErrInvalidAuthSession) {
		t.Fatalf("error = %v, want ErrInvalidAuthSession", err)
	}
}

func TestAuthStoreRotatesSessions(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "admin",
		PasswordHash: "hash",
	}); err != nil || !created {
		t.Fatalf("configure administrator = %v, %v", created, err)
	}
	base := time.Date(2026, 7, 23, 2, 0, 0, 0, time.UTC)
	var first auth.SessionTokenDigest
	for index := 0; index < maxSessionsPerUser+3; index++ {
		digest := auth.SessionTokenDigest{byte(index + 1)}
		if index == 0 {
			first = digest
		}
		record := auth.SessionRecord{
			SessionTokenDigest: digest,
			CSRFTokenDigest:    auth.CSRFTokenDigest{byte(index + 101)},
			CreatedAt:          base.Add(time.Duration(index) * time.Second),
		}
		created, err := repository.CreateSessionIfPasswordHash(ctx, "hash", record)
		if err != nil || !created {
			t.Fatalf("create session %d = %v, %v", index, created, err)
		}
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM modemdeck_auth_sessions").Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != maxSessionsPerUser {
		t.Fatalf("session count = %d, want %d", count, maxSessionsPerUser)
	}
	if _, found, err := repository.SessionByTokenDigest(ctx, first); err != nil || found {
		t.Fatalf("oldest session found = %v, err = %v", found, err)
	}
}

func TestAuthStoreRotatesUserSessionsPerUser(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "admin",
		PasswordHash: "admin-hash",
	}); err != nil || !created {
		t.Fatalf("configure administrator = %v, %v", created, err)
	}

	type userLogin struct {
		id           string
		passwordHash string
		digestPrefix byte
	}
	users := make([]userLogin, 0, 2)
	for index, username := range []string{"member-a", "member-b"} {
		passwordHash := username + "-hash"
		member, err := repository.CreateMember(ctx, CreateMemberInput{
			Username:     username,
			PasswordHash: passwordHash,
		})
		if err != nil {
			t.Fatalf("CreateMember(%q) error = %v", username, err)
		}
		users = append(users, userLogin{
			id:           member.ID,
			passwordHash: passwordHash,
			digestPrefix: byte(index + 1),
		})
	}

	base := time.Date(2026, 7, 23, 2, 0, 0, 0, time.UTC)
	for _, user := range users {
		var oldest auth.SessionTokenDigest
		for index := 0; index < maxSessionsPerUser+2; index++ {
			digest := auth.SessionTokenDigest{user.digestPrefix, byte(index + 1)}
			if index == 0 {
				oldest = digest
			}
			record := auth.UserSessionRecord{
				UserID:             user.id,
				SessionTokenDigest: digest,
				CSRFTokenDigest:    auth.CSRFTokenDigest{user.digestPrefix, byte(index + 101)},
				CreatedAt:          base.Add(time.Duration(index) * time.Second),
			}
			created, err := repository.CreateUserSessionIfPasswordHash(
				ctx,
				user.passwordHash,
				record,
			)
			if err != nil || !created {
				t.Fatalf("create %s session %d = %v, %v", user.id, index, created, err)
			}
		}

		var count int
		if err := database.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM modemdeck_auth_sessions WHERE user_id = ?",
			user.id,
		).Scan(&count); err != nil {
			t.Fatalf("count %s sessions: %v", user.id, err)
		}
		if count != maxSessionsPerUser {
			t.Fatalf("%s session count = %d, want %d", user.id, count, maxSessionsPerUser)
		}
		if _, _, found, err := repository.UserSessionByTokenDigest(
			ctx,
			oldest,
		); err != nil || found {
			t.Fatalf("%s oldest session found = %v, err = %v", user.id, found, err)
		}
	}
}

func TestAuthStoreRevokesSelectedAndOtherUserSessions(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	if created, err := repository.CreateAdminIfAbsent(ctx, auth.AdminCredentials{
		Username:     "admin",
		PasswordHash: "admin-hash",
	}); err != nil || !created {
		t.Fatalf("configure administrator = %v, %v", created, err)
	}
	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "member",
		PasswordHash: "member-hash",
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	createdAt := time.Date(2026, 8, 2, 4, 0, 0, 0, time.UTC)
	digests := []auth.SessionTokenDigest{{1}, {2}, {3}}
	for index, digest := range digests {
		created, err := repository.CreateUserSessionIfPasswordHash(
			ctx,
			"member-hash",
			auth.UserSessionRecord{
				UserID:             member.ID,
				SessionTokenDigest: digest,
				CSRFTokenDigest:    auth.CSRFTokenDigest{byte(index + 11)},
				CreatedAt:          createdAt.Add(time.Duration(index) * time.Minute),
				UserAgent:          fmt.Sprintf("Browser %d", index+1),
				AccessHost:         fmt.Sprintf("host-%d.example", index+1),
			},
		)
		if err != nil || !created {
			t.Fatalf("create session %d = %v, %v", index, created, err)
		}
	}

	sessions, err := repository.UserSessions(ctx, member.ID)
	if err != nil {
		t.Fatalf("UserSessions() error = %v", err)
	}
	if len(sessions) != 3 ||
		sessions[0].SessionTokenDigest != digests[2] ||
		sessions[0].UserAgent != "Browser 3" ||
		sessions[0].AccessHost != "host-3.example" {
		t.Fatalf("UserSessions() = %+v", sessions)
	}
	deleted, err := repository.DeleteUserSession(ctx, member.ID, digests[1])
	if err != nil || !deleted {
		t.Fatalf("DeleteUserSession() = %v, %v", deleted, err)
	}
	revoked, err := repository.DeleteOtherUserSessions(ctx, member.ID, digests[2])
	if err != nil {
		t.Fatalf("DeleteOtherUserSessions() error = %v", err)
	}
	if len(revoked) != 1 || revoked[0] != digests[0] {
		t.Fatalf("revoked digests = %x, want %x", revoked, digests[0])
	}
	sessions, err = repository.UserSessions(ctx, member.ID)
	if err != nil || len(sessions) != 1 || sessions[0].SessionTokenDigest != digests[2] {
		t.Fatalf("remaining sessions = %+v, %v", sessions, err)
	}
}
