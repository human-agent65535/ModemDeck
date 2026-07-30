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

func TestSetupCreatesAdministratorOnce(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := mustService(t, repository)
	service.random = bytes.NewReader(bytes.Repeat([]byte{0x29}, PasswordHashSaltBytes))

	status, err := service.Status(ctx)
	if err != nil || status.Configured {
		t.Fatalf("Status() before setup = %+v, %v", status, err)
	}
	if err := service.Setup(ctx, "  owner  ", "a secure initial password"); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if repository.username != "owner" {
		t.Fatalf("username = %q, want owner", repository.username)
	}
	matches, err := VerifyPassword("a secure initial password", repository.passwordHash)
	if err != nil || !matches {
		t.Fatalf("configured password matches = %v, error = %v", matches, err)
	}
	status, err = service.Status(ctx)
	if err != nil || !status.Configured || status.Username != "owner" {
		t.Fatalf("Status() after setup = %+v, %v", status, err)
	}
	if err := service.Setup(ctx, "other", "another secure password"); !errors.Is(err, ErrAlreadyConfigured) {
		t.Fatalf("second Setup() error = %v, want ErrAlreadyConfigured", err)
	}
}

func TestSetupValidatesCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		want     error
	}{
		{name: "username required", password: "a secure initial password", want: ErrUsernameInvalid},
		{name: "username newline", username: "owner\nroot", password: "a secure initial password", want: ErrUsernameInvalid},
		{name: "password too short", username: "owner", password: "short", want: ErrPasswordTooShort},
		{name: "password invalid", username: "owner", password: "secure pass\x00word", want: ErrPasswordInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := mustService(t, newMemoryRepository())
			err := service.Setup(context.Background(), test.username, test.password)
			if !errors.Is(err, test.want) {
				t.Fatalf("Setup() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestChangePasswordReplacesHashAndRevokesSessions(t *testing.T) {
	ctx := context.Background()
	currentHash := mustPasswordHash(t, "current password", 0x10)
	existingDigest := mustSessionDigest(t, 0x20)
	repository := newMemoryRepository()
	repository.configured = true
	repository.username = "admin"
	repository.passwordHash = currentHash
	repository.sessions[existingDigest] = SessionRecord{SessionTokenDigest: existingDigest}

	service := mustService(t, repository)
	service.random = bytes.NewReader(bytes.Repeat([]byte{0x31}, PasswordHashSaltBytes))

	if err := service.ChangePassword(ctx, "current password", "a new secure password"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if repository.conditionalReplaceCalls != 1 {
		t.Fatalf("conditional replace calls = %d, want 1", repository.conditionalReplaceCalls)
	}
	if _, found := repository.sessions[existingDigest]; found {
		t.Fatal("ChangePassword() did not revoke existing sessions")
	}
	matches, err := VerifyPassword("a new secure password", repository.passwordHash)
	if err != nil || !matches {
		t.Fatalf("replacement password matches = %v, error = %v", matches, err)
	}
}

func TestChangePasswordValidationAndFailures(t *testing.T) {
	ctx := context.Background()
	currentHash := mustPasswordHash(t, "current password", 0x41)

	tests := []struct {
		name          string
		current       string
		replacement   string
		configure     func(*memoryRepository)
		want          error
		wantCondition int
	}{
		{
			name:        "current password required",
			replacement: "a new secure password",
			want:        ErrInvalidCredentials,
		},
		{
			name:        "current password incorrect",
			current:     "wrong password",
			replacement: "a new secure password",
			want:        ErrInvalidCredentials,
		},
		{
			name:        "new password too short",
			current:     "current password",
			replacement: "short",
			want:        ErrPasswordTooShort,
		},
		{
			name:        "new password too long",
			current:     "current password",
			replacement: string(bytes.Repeat([]byte{'x'}, MaximumPasswordBytes+1)),
			want:        ErrPasswordTooLong,
		},
		{
			name:        "new password contains NUL",
			current:     "current password",
			replacement: "new password\x00value",
			want:        ErrPasswordInvalid,
		},
		{
			name:        "new password unchanged",
			current:     "current password",
			replacement: "current password",
			want:        ErrPasswordUnchanged,
		},
		{
			name:        "password changed concurrently",
			current:     "current password",
			replacement: "a new secure password",
			configure: func(repository *memoryRepository) {
				repository.rejectConditionalReplace = true
			},
			want:          ErrInvalidCredentials,
			wantCondition: 1,
		},
		{
			name:        "repository replacement fails",
			current:     "current password",
			replacement: "a new secure password",
			configure: func(repository *memoryRepository) {
				repository.replaceErr = errors.New("replace failed")
			},
			want:          ErrRepository,
			wantCondition: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository()
			repository.configured = true
			repository.username = "admin"
			repository.passwordHash = currentHash
			if test.configure != nil {
				test.configure(repository)
			}
			service := mustService(t, repository)
			service.random = bytes.NewReader(bytes.Repeat([]byte{0x52}, PasswordHashSaltBytes))

			err := service.ChangePassword(ctx, test.current, test.replacement)
			if !errors.Is(err, test.want) {
				t.Fatalf("ChangePassword() error = %v, want %v", err, test.want)
			}
			if repository.conditionalReplaceCalls != test.wantCondition {
				t.Fatalf(
					"conditional replace calls = %d, want %d",
					repository.conditionalReplaceCalls,
					test.wantCondition,
				)
			}
			if repository.passwordHash != currentHash {
				t.Fatal("failed password change modified the stored hash")
			}
		})
	}
}

func TestValidateTemporaryPasswordCountsCharacters(t *testing.T) {
	t.Parallel()

	for _, password := range []string{"1234567", "密码密码密码密"} {
		if err := ValidateTemporaryPassword(password); !errors.Is(err, ErrPasswordTooShort) {
			t.Fatalf("ValidateTemporaryPassword(%q) error = %v, want too short", password, err)
		}
	}
	for _, password := range []string{"12345678", "密码密码密码密码"} {
		if err := ValidateTemporaryPassword(password); err != nil {
			t.Fatalf("ValidateTemporaryPassword(%q) error = %v", password, err)
		}
	}
	if err := ValidateTemporaryPassword("1234567\x00"); !errors.Is(err, ErrPasswordInvalid) {
		t.Fatalf("ValidateTemporaryPassword(NUL) error = %v, want invalid", err)
	}
}

func TestLoginCreatesIndependentTokensAndDigestOnlyRecord(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 9, 30, 0, 0, time.FixedZone("JST", 9*60*60))
	passwordHash := mustPasswordHash(t, "password", 0x41)
	repository := newMemoryRepository()
	repository.configured = true
	repository.username = "admin"
	repository.passwordHash = passwordHash

	service := mustService(t, repository)
	service.now = func() time.Time { return now }
	service.random = bytes.NewReader(append(
		bytes.Repeat([]byte{0x51}, TokenBytes),
		bytes.Repeat([]byte{0x62}, TokenBytes)...,
	))

	result, err := service.Login(ctx, "admin", "password")
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
				repository.username = "admin"
				repository.passwordHash = passwordHash
			},
			want: ErrInvalidCredentials,
		},
		{
			name:     "malformed hash",
			password: "password",
			configure: func(repository *memoryRepository) {
				repository.configured = true
				repository.username = "admin"
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
				repository.username = "admin"
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
				repository.username = "admin"
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
				repository.username = "admin"
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
				repository.username = "admin"
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

			result, err := service.Login(ctx, "admin", test.password)
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
	username     string
	passwordHash string
	sessions     map[SessionTokenDigest]SessionRecord

	adminReadErr             error
	createAdminErr           error
	replaceErr               error
	createErr                error
	sessionReadErr           error
	deleteErr                error
	rejectCreate             bool
	rejectConditionalReplace bool

	adminReadCalls          int
	createAdminCalls        int
	conditionalReplaceCalls int
	createCalls             int
	sessionReadCalls        int
	deleteCalls             int
	createExpectedHash      string
	lastLookupDigest        SessionTokenDigest
	lastDeleteDigest        SessionTokenDigest
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		sessions: make(map[SessionTokenDigest]SessionRecord),
	}
}

func (r *memoryRepository) AdminCredentials(
	context.Context,
) (AdminCredentials, bool, error) {
	r.adminReadCalls++
	if r.adminReadErr != nil {
		return AdminCredentials{}, false, r.adminReadErr
	}
	if !r.configured {
		return AdminCredentials{}, false, nil
	}
	return AdminCredentials{
		Username:     r.username,
		PasswordHash: r.passwordHash,
	}, true, nil
}

func (r *memoryRepository) CreateAdminIfAbsent(
	_ context.Context,
	credentials AdminCredentials,
) (bool, error) {
	r.createAdminCalls++
	if r.createAdminErr != nil {
		return false, r.createAdminErr
	}
	if r.configured {
		return false, nil
	}
	r.configured = true
	r.username = credentials.Username
	r.passwordHash = credentials.PasswordHash
	return true, nil
}

func (r *memoryRepository) ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
	_ context.Context,
	expectedPasswordHash, replacementPasswordHash string,
) (bool, error) {
	r.conditionalReplaceCalls++
	if r.replaceErr != nil {
		return false, r.replaceErr
	}
	if r.rejectConditionalReplace || r.passwordHash != expectedPasswordHash {
		return false, nil
	}
	r.passwordHash = replacementPasswordHash
	r.configured = true
	if r.username == "" {
		r.username = "admin"
	}
	clear(r.sessions)
	return true, nil
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
