package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"time"
)

const SessionLifetime = 24 * time.Hour

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
	AdminPasswordHash(ctx context.Context) (passwordHash string, configured bool, err error)

	// ReplaceAdminPasswordHashAndRevokeSessions must atomically store the hash
	// and delete every existing session.
	ReplaceAdminPasswordHashAndRevokeSessions(ctx context.Context, passwordHash string) error

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

// EnsureAdmin synchronizes the authoritative password-file value into
// persistence. A changed or invalid persisted hash is replaced and all prior
// sessions are revoked by one repository transaction.
func (s *Service) EnsureAdmin(ctx context.Context, passwordFileValue string) error {
	const op = "ensure admin"

	if passwordFileValue == "" {
		return newError(op, CodePasswordRequired, nil)
	}

	currentHash, configured, err := s.repository.AdminPasswordHash(ctx)
	if err != nil {
		return repositoryError(op, err)
	}
	if configured {
		matches, verifyErr := VerifyPassword(passwordFileValue, currentHash)
		if verifyErr == nil && matches {
			return nil
		}
		if verifyErr != nil &&
			!errors.Is(verifyErr, ErrInvalidPasswordHash) &&
			!errors.Is(verifyErr, ErrPasswordHashTooExpensive) {
			return verifyErr
		}
	}

	replacement, err := hashPassword(passwordFileValue, s.random)
	if err != nil {
		return err
	}
	if err := s.repository.ReplaceAdminPasswordHashAndRevokeSessions(ctx, replacement); err != nil {
		return repositoryError(op, err)
	}
	return nil
}

func (s *Service) Login(ctx context.Context, password string) (LoginResult, error) {
	const op = "login"

	if password == "" {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}

	passwordHash, configured, err := s.repository.AdminPasswordHash(ctx)
	if err != nil {
		return LoginResult{}, repositoryError(op, err)
	}
	if !configured {
		return LoginResult{}, newError(op, CodeInvalidCredentials, nil)
	}

	matches, err := VerifyPassword(password, passwordHash)
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
	created, err := s.repository.CreateSessionIfPasswordHash(ctx, passwordHash, record)
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
