package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

func TestAuthStorePasswordAndSessionLifecycle(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	if passwordHash, configured, err := repository.AdminPasswordHash(ctx); err != nil || configured || passwordHash != "" {
		t.Fatalf("initial AdminPasswordHash() = %q, %v, %v", passwordHash, configured, err)
	}
	if err := repository.ReplaceAdminPasswordHashAndRevokeSessions(ctx, "hash-1"); err != nil {
		t.Fatalf("ReplaceAdminPasswordHashAndRevokeSessions() error = %v", err)
	}
	if passwordHash, configured, err := repository.AdminPasswordHash(ctx); err != nil || !configured || passwordHash != "hash-1" {
		t.Fatalf("AdminPasswordHash() = %q, %v, %v", passwordHash, configured, err)
	}

	createdAt := time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC)
	session := auth.SessionRecord{
		SessionTokenDigest: auth.SessionTokenDigest{1, 2, 3},
		CSRFTokenDigest:    auth.CSRFTokenDigest{4, 5, 6},
		CreatedAt:          createdAt,
		ExpiresAt:          createdAt.Add(auth.SessionLifetime),
	}
	created, err := repository.CreateSessionIfPasswordHash(ctx, "wrong-hash", session)
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
	if stored.CSRFTokenDigest != session.CSRFTokenDigest || !stored.CreatedAt.Equal(session.CreatedAt) || !stored.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("stored session = %+v, want %+v", stored, session)
	}

	if err := repository.ReplaceAdminPasswordHashAndRevokeSessions(ctx, "hash-2"); err != nil {
		t.Fatalf("replace password and revoke: %v", err)
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

func TestAuthStoreBoundsAndExpiresSessions(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	if err := repository.ReplaceAdminPasswordHashAndRevokeSessions(ctx, "hash"); err != nil {
		t.Fatalf("configure password: %v", err)
	}
	base := time.Date(2026, 7, 23, 2, 0, 0, 0, time.UTC)
	var first auth.SessionTokenDigest
	for index := 0; index < maxAdminSessions+3; index++ {
		digest := auth.SessionTokenDigest{byte(index + 1)}
		if index == 0 {
			first = digest
		}
		record := auth.SessionRecord{
			SessionTokenDigest: digest,
			CSRFTokenDigest:    auth.CSRFTokenDigest{byte(index + 101)},
			CreatedAt:          base.Add(time.Duration(index) * time.Second),
			ExpiresAt:          base.Add(auth.SessionLifetime),
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
	if count != maxAdminSessions {
		t.Fatalf("session count = %d, want %d", count, maxAdminSessions)
	}
	if _, found, err := repository.SessionByTokenDigest(ctx, first); err != nil || found {
		t.Fatalf("oldest session found = %v, err = %v", found, err)
	}

	latest := auth.SessionRecord{
		SessionTokenDigest: auth.SessionTokenDigest{250},
		CSRFTokenDigest:    auth.CSRFTokenDigest{251},
		CreatedAt:          base.Add(auth.SessionLifetime + time.Second),
		ExpiresAt:          base.Add(2 * auth.SessionLifetime),
	}
	if created, err := repository.CreateSessionIfPasswordHash(ctx, "hash", latest); err != nil || !created {
		t.Fatalf("create post-expiry session = %v, %v", created, err)
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM modemdeck_auth_sessions").Scan(&count); err != nil {
		t.Fatalf("count sessions after expiry: %v", err)
	}
	if count != 1 {
		t.Fatalf("session count after expiry = %d, want 1", count)
	}
}
