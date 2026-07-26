package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SessionLifetime      = 24 * time.Hour
	MinimumPasswordBytes = 12
	MaximumPasswordBytes = 1024
	MaximumUsernameRunes = 64
)

type AdminCredentials struct {
	Username     string
	PasswordHash string
}

type AdminStatus struct {
	Configured bool
	Username   string
}

// SessionRecord is the complete session representation exposed to the
// persistence layer. It deliberately contains digests rather than either
// plaintext token.
type SessionRecord struct {
	SessionTokenDigest SessionTokenDigest
	CSRFTokenDigest    CSRFTokenDigest
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

// Repository owns persistence and transaction boundaries for the single admin.
type Repository interface {
	AdminCredentials(ctx context.Context) (credentials AdminCredentials, configured bool, err error)

	CreateAdminIfAbsent(
		ctx context.Context,
		credentials AdminCredentials,
	) (created bool, err error)

	// ReplaceAdminPasswordHashIfCurrentAndRevokeSessions must atomically replace
	// the hash and delete every session only when the persisted hash still
	// matches expectedPasswordHash.
	ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
		ctx context.Context,
		expectedPasswordHash, replacementPasswordHash string,
	) (replaced bool, err error)

	// CreateSessionIfPasswordHash must atomically compare the current admin
	// hash with expectedPasswordHash and insert session only when they match.
	// It returns false without inserting when the hash changed.
	CreateSessionIfPasswordHash(
		ctx context.Context,
		expectedPasswordHash string,
		session SessionRecord,
	) (created bool, err error)

	SessionByTokenDigest(
		ctx context.Context,
		digest SessionTokenDigest,
	) (session SessionRecord, found bool, err error)

	// DeleteSessionByTokenDigest is idempotent when the digest is absent.
	DeleteSessionByTokenDigest(ctx context.Context, digest SessionTokenDigest) error
}

type LoginResult struct {
	SessionToken SessionToken
	CSRFToken    CSRFToken
	ExpiresAt    time.Time
}

// Authentication is proof of a current session. The CSRF digest stays private;
// callers validate a supplied token through VerifyCSRF.
type Authentication struct {
	CreatedAt     time.Time
	ExpiresAt     time.Time
	csrfDigest    CSRFTokenDigest
	authenticated bool
}

func (a Authentication) VerifyCSRF(token CSRFToken) error {
	const op = "verify CSRF token"

	if !a.authenticated {
		return newError(op, CodeUnauthenticated, nil)
	}

	digest, err := csrfTokenDigest(token)
	if err != nil || !equalCSRFDigests(digest, a.csrfDigest) {
		return newError(op, CodeInvalidCSRFToken, nil)
	}
	return nil
}

type Service struct {
	repository Repository
	random     io.Reader
	now        func() time.Time
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, newError("create auth service", CodeRepositoryRequired, nil)
	}
	return &Service{
		repository: repository,
		random:     rand.Reader,
		now:        time.Now,
	}, nil
}

func (s *Service) Status(ctx context.Context) (AdminStatus, error) {
	const op = "read admin status"

	credentials, configured, err := s.repository.AdminCredentials(ctx)
	if err != nil {
		return AdminStatus{}, repositoryError(op, err)
	}
	return AdminStatus{
		Configured: configured,
		Username:   credentials.Username,
	}, nil
}

func (s *Service) Setup(ctx context.Context, username, password string) error {
	const op = "set up admin"

	if _, configured, err := s.repository.AdminCredentials(ctx); err != nil {
		return repositoryError(op, err)
	} else if configured {
		return newError(op, CodeAlreadyConfigured, nil)
	}
	normalizedUsername, err := normalizeUsername(username)
	if err != nil {
		return err
	}
	if err := validateNewPassword(password); err != nil {
		return err
	}
	passwordHash, err := hashPassword(password, s.random)
	if err != nil {
		return err
	}
	created, err := s.repository.CreateAdminIfAbsent(ctx, AdminCredentials{
		Username:     normalizedUsername,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return repositoryError(op, err)
	}
	if !created {
		return newError(op, CodeAlreadyConfigured, nil)
	}
	return nil
}

// ChangePassword verifies the current password, replaces it with a new
// Argon2id hash, and revokes every active session in one conditional
// transaction.
func (s *Service) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	const op = "change password"

	if currentPassword == "" || len(currentPassword) > MaximumPasswordBytes {
		return newError(op, CodeInvalidCredentials, nil)
	}
	if err := validateNewPassword(newPassword); err != nil {
		return err
	}
	if currentPassword == newPassword {
		return newError(op, CodePasswordUnchanged, nil)
	}

	credentials, configured, err := s.repository.AdminCredentials(ctx)
	if err != nil {
		return repositoryError(op, err)
	}
	if !configured {
		return newError(op, CodeInvalidCredentials, nil)
	}
	matches, err := VerifyPassword(currentPassword, credentials.PasswordHash)
	if err != nil {
		return err
	}
	if !matches {
		return newError(op, CodeInvalidCredentials, nil)
	}

	replacement, err := hashPassword(newPassword, s.random)
	if err != nil {
		return err
	}
	replaced, err := s.repository.ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
		ctx,
		credentials.PasswordHash,
		replacement,
	)
	if err != nil {
		return repositoryError(op, err)
	}
	if !replaced {
		return newError(op, CodeInvalidCredentials, nil)
	}
	return nil
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	const op = "login"

	normalizedUsername := strings.TrimSpace(username)
	if normalizedUsername == "" ||
		utf8.RuneCountInString(normalizedUsername) > MaximumUsernameRunes ||
		strings.ContainsAny(normalizedUsername, "\x00\r\n") ||
		password == "" ||
		len(password) > MaximumPasswordBytes {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}

	credentials, configured, err := s.repository.AdminCredentials(ctx)
	if err != nil {
		return LoginResult{}, repositoryError(op, err)
	}
	if !configured {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}
	if subtle.ConstantTimeCompare(
		[]byte(normalizedUsername),
		[]byte(credentials.Username),
	) != 1 {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}

	matches, err := VerifyPassword(password, credentials.PasswordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if !matches {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}

	sessionTokenValue, err := newOpaqueToken(s.random)
	if err != nil {
		return LoginResult{}, newError(op, CodeRandomSource, err)
	}
	csrfTokenValue, err := newOpaqueToken(s.random)
	if err != nil {
		return LoginResult{}, newError(op, CodeRandomSource, err)
	}

	sessionToken := SessionToken(sessionTokenValue)
	sessionDigest, err := sessionTokenDigest(sessionToken)
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken := CSRFToken(csrfTokenValue)
	csrfDigest, err := csrfTokenDigest(csrfToken)
	if err != nil {
		return LoginResult{}, err
	}

	createdAt := s.now().UTC()
	expiresAt := createdAt.Add(SessionLifetime)
	record := SessionRecord{
		SessionTokenDigest: sessionDigest,
		CSRFTokenDigest:    csrfDigest,
		CreatedAt:          createdAt,
		ExpiresAt:          expiresAt,
	}
	created, err := s.repository.CreateSessionIfPasswordHash(
		ctx,
		credentials.PasswordHash,
		record,
	)
	if err != nil {
		return LoginResult{}, repositoryError(op, err)
	}
	if !created {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}

	return LoginResult{
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    expiresAt,
	}, nil
}

func normalizeUsername(username string) (string, error) {
	const op = "validate username"

	username = strings.TrimSpace(username)
	if username == "" ||
		utf8.RuneCountInString(username) > MaximumUsernameRunes ||
		strings.ContainsAny(username, "\x00\r\n") {
		return "", newError(op, CodeUsernameInvalid, nil)
	}
	return username, nil
}

func validateNewPassword(password string) error {
	const op = "validate new password"

	if len(password) < MinimumPasswordBytes {
		return newError(op, CodePasswordTooShort, nil)
	}
	if len(password) > MaximumPasswordBytes {
		return newError(op, CodePasswordTooLong, nil)
	}
	if strings.ContainsRune(password, '\x00') {
		return newError(op, CodePasswordInvalid, nil)
	}
	return nil
}

func (s *Service) Authenticate(ctx context.Context, token SessionToken) (Authentication, error) {
	const op = "authenticate"

	digest, err := sessionTokenDigest(token)
	if err != nil {
		return Authentication{}, newError(op, CodeInvalidSessionToken, nil)
	}

	record, found, err := s.repository.SessionByTokenDigest(ctx, digest)
	if err != nil {
		return Authentication{}, repositoryError(op, err)
	}
	if !found {
		return Authentication{}, newError(op, CodeUnauthenticated, nil)
	}

	createdAt := record.CreatedAt.UTC()
	expiresAt := record.ExpiresAt.UTC()
	if createdAt.IsZero() || expiresAt.IsZero() || expiresAt.Before(createdAt) {
		return Authentication{}, newError(op, CodeInvalidSessionRecord, nil)
	}

	absoluteExpiry := createdAt.Add(SessionLifetime)
	if expiresAt.After(absoluteExpiry) {
		expiresAt = absoluteExpiry
	}
	if !s.now().UTC().Before(expiresAt) {
		return Authentication{}, newError(op, CodeSessionExpired, nil)
	}

	return Authentication{
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
		csrfDigest:    record.CSRFTokenDigest,
		authenticated: true,
	}, nil
}

func (s *Service) Logout(ctx context.Context, token SessionToken) error {
	const op = "logout"

	digest, err := sessionTokenDigest(token)
	if err != nil {
		return newError(op, CodeInvalidSessionToken, nil)
	}
	if err := s.repository.DeleteSessionByTokenDigest(ctx, digest); err != nil {
		return repositoryError(op, err)
	}
	return nil
}
