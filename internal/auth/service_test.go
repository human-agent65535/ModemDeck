package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestNewServiceRequiresRepository(t *testing.T) {
	service, err := NewService(nil)
	if !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("NewService(nil) error = %v, want ErrRepositoryRequired", err)
	}
	if service != nil {
		t.Fatalf("NewService(nil) service = %#v, want nil", service)
	}
}

func TestEnsureAdminCreatesOrRotatesPasswordAndRevokesSessions(t *testing.T) {
	ctx := context.Background()
	oldHash := mustPasswordHash(t, "old password", 0x10)
	existingDigest := mustSessionDigest(t, 0x20)

	tests := []struct {
		name           string
		configured     bool
		currentHash    string
		password       string
		wantReplace    bool
		wantOldSession bool
	}{
		{
			name:        "initial password",
			password:    "initial password",
			wantReplace: true,
		},
		{
			name:           "matching password",
			configured:     true,
			currentHash:    oldHash,
			password:       "old password",
			wantOldSession: true,
		},
		{
			name:        "changed password",
			configured:  true,
			currentHash: oldHash,
			password:    "new password",
			wantReplace: true,
		},
		{
			name:        "malformed persisted hash",
			configured:  true,
			currentHash: "malformed",
			password:    "recovered password",
			wantReplace: true,
		},
		{
			name:        "excessive persisted hash",
			configured:  true,
			currentHash: excessivePasswordHash(),
			password:    "recovered password",
			wantReplace: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository()
			repository.configured = test.configured
			repository.passwordHash = test.currentHash
			repository.sessions[existingDigest] = SessionRecord{SessionTokenDigest: existingDigest}

			service := mustService(t, repository)
			service.random = bytes.NewReader(bytes.Repeat([]byte{0x31}, PasswordHashSaltBytes))

			if err := service.EnsureAdmin(ctx, test.password); err != nil {
				t.Fatalf("EnsureAdmin() error = %v", err)
			}
			if repository.replaceCalls != boolInt(test.wantReplace) {
				t.Fatalf("replace calls = %d, want %d", repository.replaceCalls, boolInt(test.wantReplace))
			}
			if _, found := repository.sessions[existingDigest]; found != test.wantOldSession {
				t.Fatalf("old session found = %t, want %t", found, test.wantOldSession)
			}

			if test.wantReplace {
				matches, err := VerifyPassword(test.password, repository.passwordHash)
				if err != nil {
					t.Fatalf("VerifyPassword(replacement) error = %v", err)
				}
				if !matches {
					t.Fatal("replacement password does not match")
				}
			} else if repository.passwordHash != test.currentHash {
				t.Fatal("matching hash was unexpectedly changed")
			}
		})
	}
}

func TestEnsureAdminFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("empty password", func(t *testing.T) {
		repository := newMemoryRepository()
		service := mustService(t, repository)

		if err := service.EnsureAdmin(ctx, ""); !errors.Is(err, ErrPasswordRequired) {
			t.Fatalf("EnsureAdmin() error = %v, want ErrPasswordRequired", err)
		}
		if repository.adminReadCalls != 0 {
			t.Fatalf("admin read calls = %d, want 0", repository.adminReadCalls)
		}
	})

	t.Run("repository read", func(t *testing.T) {
		cause := errors.New("read failed")
		repository := newMemoryRepository()
		repository.adminReadErr = cause
		service := mustService(t, repository)

		err := service.EnsureAdmin(ctx, "password")
		assertErrorAndCause(t, err, ErrRepository, cause)
	})

	t.Run("random source", func(t *testing.T) {
		cause := errors.New("random failed")
		repository := newMemoryRepository()
		service := mustService(t, repository)
		service.random = failingReader{err: cause}

		err := service.EnsureAdmin(ctx, "password")
		assertErrorAndCause(t, err, ErrRandomSource, cause)
		if repository.replaceCalls != 0 {
			t.Fatalf("replace calls = %d, want 0", repository.replaceCalls)
		}
	})

	t.Run("repository replace", func(t *testing.T) {
		cause := errors.New("replace failed")
		repository := newMemoryRepository()
		repository.replaceErr = cause
		service := mustService(t, repository)
		service.random = bytes.NewReader(bytes.Repeat([]byte{0x40}, PasswordHashSaltBytes))

		err := service.EnsureAdmin(ctx, "password")
		assertErrorAndCause(t, err, ErrRepository, cause)
	})
}

func TestLoginCreatesIndependentTokensAndDigestOnlyRecord(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 9, 30, 0, 0, time.FixedZone("JST", 9*60*60))
	passwordHash := mustPasswordHash(t, "password", 0x41)
	repository := newMemoryRepository()
	repository.configured = true
	repository.passwordHash = passwordHash

	service := mustService(t, repository)
	service.now = func() time.Time { return now }
	service.random = bytes.NewReader(append(
		bytes.Repeat([]byte{0x51}, TokenBytes),
		bytes.Repeat([]byte{0x62}, TokenBytes)...,
	))

	result, err := service.Login(ctx, "password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.SessionToken == "" || result.CSRFToken == "" {
		t.Fatal("Login() returned an empty token")
	}
	if string(result.SessionToken) == string(result.CSRFToken) {
		t.Fatal("session and CSRF tokens are not independent")
	}
	if !validOpaqueToken(string(result.SessionToken)) || !validOpaqueToken(string(result.CSRFToken)) {
		t.Fatal("Login() tokens are not canonical 32-byte values")
	}

	wantCreatedAt := now.UTC()
	wantExpiresAt := wantCreatedAt.Add(SessionLifetime)
	if !result.ExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("ExpiresAt = %s, want %s", result.ExpiresAt, wantExpiresAt)
	}
	if repository.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", repository.createCalls)
	}
	if repository.createExpectedHash != passwordHash {
		t.Fatal("CreateSessionIfPasswordHash did not receive the verified hash")
	}

	sessionDigest := SessionTokenDigest(sha256.Sum256([]byte(result.SessionToken)))
	record, found := repository.sessions[sessionDigest]
	if !found {
		t.Fatal("repository did not receive the session token digest")
	}
	if record.SessionTokenDigest != sessionDigest {
		t.Fatalf("stored session digest = %x, want %x", record.SessionTokenDigest, sessionDigest)
	}
	wantCSRFDigest := CSRFTokenDigest(sha256.Sum256([]byte(result.CSRFToken)))
	if record.CSRFTokenDigest != wantCSRFDigest {
		t.Fatalf("stored CSRF digest = %x, want %x", record.CSRFTokenDigest, wantCSRFDigest)
	}
	if !record.CreatedAt.Equal(wantCreatedAt) || !record.ExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("stored times = %s..%s, want %s..%s", record.CreatedAt, record.ExpiresAt, wantCreatedAt, wantExpiresAt)
	}
}

func TestSessionRecordCannotCarryPlaintextTokens(t *testing.T) {
	recordType := reflect.TypeFor[SessionRecord]()
	for index := range recordType.NumField() {
		field := recordType.Field(index)
		if field.Type.Kind() == reflect.String {
			t.Fatalf("SessionRecord field %s has string type and could persist a plaintext token", field.Name)
		}
	}
}

func TestLoginFailures(t *testing.T) {
	ctx := context.Background()
	passwordHash := mustPasswordHash(t, "password", 0x71)
	storageFailure := errors.New("storage failed")
	randomFailure := errors.New("random failed")

	tests := []struct {
		name        string
		password    string
		configure   func(*memoryRepository)
		random      io.Reader
		want        error
		wantCause   error
		wantCreates int
	}{
		{
			name:     "empty password",
			password: "",
			want:     ErrInvalidCredentials,
		},
		{
			name:     "admin not configured",
			password: "password",
			want:     ErrInvalidCredentials,
		},
		{
			name:     "wrong password",
			password: "wrong",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.passwordHash = passwordHash
			},
			want: ErrInvalidCredentials,
		},
		{
			name:     "malformed hash",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.passwordHash = "malformed"
			},
			want: ErrInvalidPasswordHash,
		},
		{
			name:     "admin read failure",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.adminReadErr = storageFailure
			},
			want:      ErrRepository,
			wantCause: storageFailure,
		},
		{
			name:     "session random failure",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.passwordHash = passwordHash
			},
			random:    failingReader{err: randomFailure},
			want:      ErrRandomSource,
			wantCause: randomFailure,
		},
		{
			name:     "CSRF random failure",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.passwordHash = passwordHash
			},
			random: io.MultiReader(
				bytes.NewReader(bytes.Repeat([]byte{0x81}, TokenBytes)),
				failingReader{err: randomFailure},
			),
			want:      ErrRandomSource,
			wantCause: randomFailure,
		},
		{
			name:     "password changed during login",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.passwordHash = passwordHash
				repository.rejectCreate = true
			},
			random:      bytes.NewReader(bytes.Repeat([]byte{0x91}, 2*TokenBytes)),
			want:        ErrInvalidCredentials,
			wantCreates: 1,
		},
		{
			name:     "session create failure",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.passwordHash = passwordHash
				repository.createErr = storageFailure
			},
			random:      bytes.NewReader(bytes.Repeat([]byte{0xa1}, 2*TokenBytes)),
			want:        ErrRepository,
			wantCause:   storageFailure,
			wantCreates: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository()
			if test.configure != nil {
				test.configure(repository)
			}
			service := mustService(t, repository)
			if test.random != nil {
				service.random = test.random
			}

			result, err := service.Login(ctx, test.password)
			if !errors.Is(err, test.want) {
				t.Fatalf("Login() error = %v, want %v", err, test.want)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("Login() error = %v, want cause %v", err, test.wantCause)
			}
			if result != (LoginResult{}) {
				t.Fatalf("Login() result = %#v, want zero value", result)
			}
			if repository.createCalls != test.wantCreates {
				t.Fatalf("create calls = %d, want %d", repository.createCalls, test.wantCreates)
			}
			if len(repository.sessions) != 0 {
				t.Fatalf("sessions = %d, want 0", len(repository.sessions))
			}
		})
	}
}

func TestAuthenticateAndVerifyCSRF(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	sessionToken, sessionDigest := mustSessionToken(t, 0xb1)
	csrfToken, csrfDigest := mustCSRFToken(t, 0xc1)
	repository := newMemoryRepository()
	repository.sessions[sessionDigest] = SessionRecord{
		SessionTokenDigest: sessionDigest,
		CSRFTokenDigest:    csrfDigest,
		CreatedAt:          createdAt,
		ExpiresAt:          createdAt.Add(SessionLifetime),
	}
	service := mustService(t, repository)
	service.now = func() time.Time { return createdAt.Add(2 * time.Hour) }

	authentication, err := service.Authenticate(ctx, sessionToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if repository.lastLookupDigest != sessionDigest {
		t.Fatalf("lookup digest = %x, want %x", repository.lastLookupDigest, sessionDigest)
	}
	if !authentication.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s, want %s", authentication.CreatedAt, createdAt)
	}
	if !authentication.ExpiresAt.Equal(createdAt.Add(SessionLifetime)) {
		t.Fatalf("ExpiresAt = %s, want %s", authentication.ExpiresAt, createdAt.Add(SessionLifetime))
	}
	if err := authentication.VerifyCSRF(csrfToken); err != nil {
		t.Fatalf("VerifyCSRF(correct) error = %v", err)
	}

	wrongCSRF, _ := mustCSRFToken(t, 0xd1)
	if err := authentication.VerifyCSRF(wrongCSRF); !errors.Is(err, ErrInvalidCSRFToken) {
		t.Fatalf("VerifyCSRF(wrong) error = %v, want ErrInvalidCSRFToken", err)
	}
	if err := authentication.VerifyCSRF("malformed"); !errors.Is(err, ErrInvalidCSRFToken) {
		t.Fatalf("VerifyCSRF(malformed) error = %v, want ErrInvalidCSRFToken", err)
	}
	if err := (Authentication{}).VerifyCSRF(csrfToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("zero Authentication.VerifyCSRF() error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateEnforcesAbsoluteExpiry(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	sessionToken, sessionDigest := mustSessionToken(t, 0xe1)
	_, csrfDigest := mustCSRFToken(t, 0xe2)

	tests := []struct {
		name        string
		record      SessionRecord
		now         time.Time
		want        error
		wantExpires time.Time
	}{
		{
			name: "valid",
			record: SessionRecord{
				CreatedAt: createdAt,
				ExpiresAt: createdAt.Add(12 * time.Hour),
			},
			now:         createdAt.Add(time.Hour),
			wantExpires: createdAt.Add(12 * time.Hour),
		},
		{
			name: "repository expiry capped at 24 hours",
			record: SessionRecord{
				CreatedAt: createdAt,
				ExpiresAt: createdAt.Add(72 * time.Hour),
			},
			now:         createdAt.Add(23 * time.Hour),
			wantExpires: createdAt.Add(SessionLifetime),
		},
		{
			name: "expired at exact boundary",
			record: SessionRecord{
				CreatedAt: createdAt,
				ExpiresAt: createdAt.Add(SessionLifetime),
			},
			now:  createdAt.Add(SessionLifetime),
			want: ErrSessionExpired,
		},
		{
			name: "extended record still expires at 24 hours",
			record: SessionRecord{
				CreatedAt: createdAt,
				ExpiresAt: createdAt.Add(72 * time.Hour),
			},
			now:  createdAt.Add(25 * time.Hour),
			want: ErrSessionExpired,
		},
		{
			name: "zero creation",
			record: SessionRecord{
				ExpiresAt: createdAt.Add(time.Hour),
			},
			now:  createdAt,
			want: ErrInvalidSessionRecord,
		},
		{
			name: "expiry before creation",
			record: SessionRecord{
				CreatedAt: createdAt,
				ExpiresAt: createdAt.Add(-time.Second),
			},
			now:  createdAt,
			want: ErrInvalidSessionRecord,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository()
			test.record.SessionTokenDigest = sessionDigest
			test.record.CSRFTokenDigest = csrfDigest
			repository.sessions[sessionDigest] = test.record
			service := mustService(t, repository)
			service.now = func() time.Time { return test.now }

			authentication, err := service.Authenticate(ctx, sessionToken)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("Authenticate() error = %v, want %v", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if !authentication.ExpiresAt.Equal(test.wantExpires) {
				t.Fatalf("ExpiresAt = %s, want %s", authentication.ExpiresAt, test.wantExpires)
			}
		})
	}
}

func TestAuthenticateFailures(t *testing.T) {
	ctx := context.Background()
	validToken, validDigest := mustSessionToken(t, 0xf1)

	t.Run("malformed token avoids repository", func(t *testing.T) {
		repository := newMemoryRepository()
		service := mustService(t, repository)

		_, err := service.Authenticate(ctx, "malformed")
		if !errors.Is(err, ErrInvalidSessionToken) {
			t.Fatalf("Authenticate() error = %v, want ErrInvalidSessionToken", err)
		}
		if repository.sessionReadCalls != 0 {
			t.Fatalf("session read calls = %d, want 0", repository.sessionReadCalls)
		}
	})

	t.Run("unknown digest", func(t *testing.T) {
		repository := newMemoryRepository()
		service := mustService(t, repository)

		_, err := service.Authenticate(ctx, validToken)
		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
		}
		if repository.lastLookupDigest != validDigest {
			t.Fatalf("lookup digest = %x, want %x", repository.lastLookupDigest, validDigest)
		}
	})

	t.Run("repository failure", func(t *testing.T) {
		cause := errors.New("lookup failed")
		repository := newMemoryRepository()
		repository.sessionReadErr = cause
		service := mustService(t, repository)

		_, err := service.Authenticate(ctx, validToken)
		assertErrorAndCause(t, err, ErrRepository, cause)
	})
}

func TestLogoutUsesOnlyDigestAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	token, digest := mustSessionToken(t, 0x33)
	repository := newMemoryRepository()
	repository.sessions[digest] = SessionRecord{SessionTokenDigest: digest}
	service := mustService(t, repository)

	if err := service.Logout(ctx, token); err != nil {
		t.Fatalf("Logout(existing) error = %v", err)
	}
	if repository.lastDeleteDigest != digest {
		t.Fatalf("delete digest = %x, want %x", repository.lastDeleteDigest, digest)
	}
	if _, found := repository.sessions[digest]; found {
		t.Fatal("Logout(existing) did not delete session")
	}
	if err := service.Logout(ctx, token); err != nil {
		t.Fatalf("Logout(missing) error = %v", err)
	}
}

func TestLogoutFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("malformed token", func(t *testing.T) {
		repository := newMemoryRepository()
		service := mustService(t, repository)

		if err := service.Logout(ctx, "malformed"); !errors.Is(err, ErrInvalidSessionToken) {
			t.Fatalf("Logout() error = %v, want ErrInvalidSessionToken", err)
		}
		if repository.deleteCalls != 0 {
			t.Fatalf("delete calls = %d, want 0", repository.deleteCalls)
		}
	})

	t.Run("repository failure", func(t *testing.T) {
		cause := errors.New("delete failed")
		token, _ := mustSessionToken(t, 0x44)
		repository := newMemoryRepository()
		repository.deleteErr = cause
		service := mustService(t, repository)

		err := service.Logout(ctx, token)
		assertErrorAndCause(t, err, ErrRepository, cause)
	})
}

func TestErrorSupportsCodeMatchingAndConcreteInspection(t *testing.T) {
	cause := errors.New("database offline")
	err := repositoryError("test operation", cause)

	if !errors.Is(err, ErrRepository) {
		t.Fatalf("errors.Is(%v, ErrRepository) = false", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, cause) = false", err)
	}

	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("errors.As(%T) = false", err)
	}
	if typed.Code != CodeRepository || typed.Op != "test operation" {
		t.Fatalf("typed error = %#v", typed)
	}
}

type memoryRepository struct {
	configured   bool
	passwordHash string
	sessions     map[SessionTokenDigest]SessionRecord

	adminReadErr   error
	replaceErr     error
	createErr      error
	sessionReadErr error
	deleteErr      error
	rejectCreate   bool

	adminReadCalls     int
	replaceCalls       int
	createCalls        int
	sessionReadCalls   int
	deleteCalls        int
	createExpectedHash string
	lastLookupDigest   SessionTokenDigest
	lastDeleteDigest   SessionTokenDigest
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		sessions: make(map[SessionTokenDigest]SessionRecord),
	}
}

func (r *memoryRepository) AdminPasswordHash(context.Context) (string, bool, error) {
	r.adminReadCalls++
	if r.adminReadErr != nil {
		return "", false, r.adminReadErr
	}
	return r.passwordHash, r.configured, nil
}

func (r *memoryRepository) ReplaceAdminPasswordHashAndRevokeSessions(_ context.Context, passwordHash string) error {
	r.replaceCalls++
	if r.replaceErr != nil {
		return r.replaceErr
	}
	r.passwordHash = passwordHash
	r.configured = true
	clear(r.sessions)
	return nil
}

func (r *memoryRepository) CreateSessionIfPasswordHash(
	_ context.Context,
	expectedPasswordHash string,
	session SessionRecord,
) (bool, error) {
	r.createCalls++
	r.createExpectedHash = expectedPasswordHash
	if r.createErr != nil {
		return false, r.createErr
	}
	if r.rejectCreate || !r.configured || r.passwordHash != expectedPasswordHash {
		return false, nil
	}
	r.sessions[session.SessionTokenDigest] = session
	return true, nil
}

func (r *memoryRepository) SessionByTokenDigest(
	_ context.Context,
	digest SessionTokenDigest,
) (SessionRecord, bool, error) {
	r.sessionReadCalls++
	r.lastLookupDigest = digest
	if r.sessionReadErr != nil {
		return SessionRecord{}, false, r.sessionReadErr
	}
	session, found := r.sessions[digest]
	return session, found, nil
}

func (r *memoryRepository) DeleteSessionByTokenDigest(_ context.Context, digest SessionTokenDigest) error {
	r.deleteCalls++
	r.lastDeleteDigest = digest
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.sessions, digest)
	return nil
}

func mustService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func mustPasswordHash(t *testing.T, password string, saltByte byte) string {
	t.Helper()
	hash, err := hashPassword(
		password,
		bytes.NewReader(bytes.Repeat([]byte{saltByte}, PasswordHashSaltBytes)),
	)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	return hash
}

func mustSessionToken(t *testing.T, value byte) (SessionToken, SessionTokenDigest) {
	t.Helper()
	tokenValue, err := newOpaqueToken(bytes.NewReader(bytes.Repeat([]byte{value}, TokenBytes)))
	if err != nil {
		t.Fatalf("newOpaqueToken() error = %v", err)
	}
	token := SessionToken(tokenValue)
	digest, err := sessionTokenDigest(token)
	if err != nil {
		t.Fatalf("sessionTokenDigest() error = %v", err)
	}
	return token, digest
}

func mustSessionDigest(t *testing.T, value byte) SessionTokenDigest {
	t.Helper()
	_, digest := mustSessionToken(t, value)
	return digest
}

func mustCSRFToken(t *testing.T, value byte) (CSRFToken, CSRFTokenDigest) {
	t.Helper()
	tokenValue, err := newOpaqueToken(bytes.NewReader(bytes.Repeat([]byte{value}, TokenBytes)))
	if err != nil {
		t.Fatalf("newOpaqueToken() error = %v", err)
	}
	token := CSRFToken(tokenValue)
	digest, err := csrfTokenDigest(token)
	if err != nil {
		t.Fatalf("csrfTokenDigest() error = %v", err)
	}
	return token, digest
}

func excessivePasswordHash() string {
	salt := rawBase64.EncodeToString(bytes.Repeat([]byte{0x55}, PasswordHashSaltBytes))
	key := rawBase64.EncodeToString(bytes.Repeat([]byte{0x66}, PasswordHashKeyBytes))
	return "$argon2id$v=19$m=262144,t=11,p=4$" + salt + "$" + key
}

func assertErrorAndCause(t *testing.T, err, kind, cause error) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Fatalf("error = %v, want kind %v", err, kind)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause %v", err, cause)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
