package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

var errSessionManagementUnavailable = errors.New("session management repository is unavailable")

const (
	MinimumPasswordRunes   = 8
	MaximumPasswordBytes   = 1024
	MaximumUsernameRunes   = 64
	maximumUserAgentBytes  = 512
	maximumAccessIPBytes   = 64
	maximumAccessHostBytes = 255
	sessionMetadataRefresh = 5 * time.Minute
)

type AdminCredentials struct {
	Username     string
	PasswordHash string
}

type AdminStatus struct {
	Configured bool
	Username   string
}

type SessionClient struct {
	UserAgent  string
	AccessIP   string
	AccessHost string
}

type sessionClientContextKey struct{}

func ContextWithSessionClient(ctx context.Context, client SessionClient) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	client.UserAgent = normalizedSessionClientValue(client.UserAgent, maximumUserAgentBytes)
	client.AccessIP = normalizedSessionClientValue(client.AccessIP, maximumAccessIPBytes)
	client.AccessHost = normalizedSessionClientValue(client.AccessHost, maximumAccessHostBytes)
	return context.WithValue(ctx, sessionClientContextKey{}, client)
}

func normalizedSessionClientValue(value string, maximumBytes int) string {
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value))
	if len(value) <= maximumBytes {
		return value
	}
	value = value[:maximumBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func sessionClientFromContext(ctx context.Context) SessionClient {
	client, _ := ctx.Value(sessionClientContextKey{}).(SessionClient)
	return client
}

// SessionRecord is the complete session representation exposed to the
// persistence layer. It deliberately contains digests rather than either
// plaintext token.
type SessionRecord struct {
	SessionTokenDigest SessionTokenDigest
	CSRFTokenDigest    CSRFTokenDigest
	CreatedAt          time.Time
	LastSeenAt         time.Time
	UserAgent          string
	AccessIP           string
	AccessHost         string
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
	Principal    *Principal
}

type WebSession struct {
	ID         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	AccessIP   string
	AccessHost string
	Current    bool
}

// Authentication is proof of a current session. The CSRF digest stays private;
// callers validate a supplied token through VerifyCSRF.
type Authentication struct {
	CreatedAt     time.Time
	principal     Principal
	csrfDigest    CSRFTokenDigest
	authenticated bool
}

func (a Authentication) Principal() (Principal, bool) {
	if !a.authenticated || a.principal.UserID == "" {
		return Principal{}, false
	}
	return a.principal.Copy(), true
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

	if repository, ok := s.repository.(MultiUserRepository); ok {
		if principal, authenticated := PrincipalFromContext(ctx); authenticated {
			return s.changeUserPassword(
				ctx,
				repository,
				principal.UserID,
				currentPassword,
				newPassword,
			)
		}
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

func (s *Service) changeUserPassword(
	ctx context.Context,
	repository MultiUserRepository,
	userID, currentPassword, newPassword string,
) error {
	const op = "change password"

	credentials, found, err := repository.UserCredentialsByID(ctx, userID)
	if err != nil {
		return repositoryError(op, err)
	}
	if !found || !credentials.Enabled {
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
	replaced, err := repository.ReplaceUserPasswordHashIfCurrentAndRevokeSessions(
		ctx,
		userID,
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

	if repository, ok := s.repository.(MultiUserRepository); ok {
		return s.loginUser(ctx, repository, normalizedUsername, password)
	}

	credentials, configured, err := s.repository.AdminCredentials(ctx)
	if err != nil {
		return LoginResult{}, repositoryError(op, err)
	}
	usableCredentials := configured && subtle.ConstantTimeCompare(
		[]byte(normalizedUsername),
		[]byte(credentials.Username),
	) == 1

	matches, err := verifyLoginPassword(
		password,
		credentials.PasswordHash,
		usableCredentials,
	)
	if err != nil {
		return LoginResult{}, err
	}
	if !usableCredentials || !matches {
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
	client := sessionClientFromContext(ctx)
	record := SessionRecord{
		SessionTokenDigest: sessionDigest,
		CSRFTokenDigest:    csrfDigest,
		CreatedAt:          createdAt,
		LastSeenAt:         createdAt,
		UserAgent:          client.UserAgent,
		AccessIP:           client.AccessIP,
		AccessHost:         client.AccessHost,
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
	}, nil
}

func (s *Service) loginUser(
	ctx context.Context,
	repository MultiUserRepository,
	username, password string,
) (LoginResult, error) {
	const op = "login"

	credentials, found, err := repository.UserCredentialsByUsername(ctx, username)
	if err != nil {
		return LoginResult{}, repositoryError(op, err)
	}
	usableCredentials := found && credentials.Enabled
	matches, err := verifyLoginPassword(
		password,
		credentials.PasswordHash,
		usableCredentials,
	)
	if err != nil {
		return LoginResult{}, err
	}
	if !usableCredentials || !matches {
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
	client := sessionClientFromContext(ctx)
	record := UserSessionRecord{
		UserID:             credentials.ID,
		SessionTokenDigest: sessionDigest,
		CSRFTokenDigest:    csrfDigest,
		CreatedAt:          createdAt,
		LastSeenAt:         createdAt,
		UserAgent:          client.UserAgent,
		AccessIP:           client.AccessIP,
		AccessHost:         client.AccessHost,
	}
	created, err := repository.CreateUserSessionIfPasswordHash(
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
	_, principal, found, err := repository.UserSessionByTokenDigest(ctx, sessionDigest)
	if err != nil {
		return LoginResult{}, repositoryError(op, err)
	}
	if !found {
		return LoginResult{}, newError(op, CodeInvalidSessionRecord, nil)
	}
	return LoginResult{
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		Principal:    &principal,
	}, nil
}

func verifyLoginPassword(password, encodedHash string, usableCredentials bool) (bool, error) {
	if !usableCredentials {
		encodedHash = dummyPasswordHash
	}
	matches, err := VerifyPassword(password, encodedHash)
	if err != nil {
		return false, err
	}
	return usableCredentials && matches, nil
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

	if utf8.RuneCountInString(password) < MinimumPasswordRunes {
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

func ValidateNewPassword(password string) error {
	return validateNewPassword(password)
}

func (s *Service) Authenticate(ctx context.Context, token SessionToken) (Authentication, error) {
	const op = "authenticate"

	digest, err := sessionTokenDigest(token)
	if err != nil {
		return Authentication{}, newError(op, CodeInvalidSessionToken, nil)
	}

	if repository, ok := s.repository.(MultiUserRepository); ok {
		record, principal, found, err := repository.UserSessionByTokenDigest(ctx, digest)
		if err != nil {
			return Authentication{}, repositoryError(op, err)
		}
		if !found {
			return Authentication{}, newError(op, CodeUnauthenticated, nil)
		}
		authentication, err := s.authenticationFromUserSession(record, principal)
		if err != nil {
			return Authentication{}, err
		}
		s.refreshUserSessionMetadata(ctx, repository, record)
		return authentication, nil
	}

	record, found, err := s.repository.SessionByTokenDigest(ctx, digest)
	if err != nil {
		return Authentication{}, repositoryError(op, err)
	}
	if !found {
		return Authentication{}, newError(op, CodeUnauthenticated, nil)
	}

	createdAt := record.CreatedAt.UTC()
	if createdAt.IsZero() {
		return Authentication{}, newError(op, CodeInvalidSessionRecord, nil)
	}
	s.refreshSessionMetadata(ctx, record)

	return Authentication{
		CreatedAt:     createdAt,
		csrfDigest:    record.CSRFTokenDigest,
		authenticated: true,
	}, nil
}

func (s *Service) authenticationFromUserSession(
	record UserSessionRecord,
	principal Principal,
) (Authentication, error) {
	const op = "authenticate"

	createdAt := record.CreatedAt.UTC()
	if record.UserID == "" ||
		principal.UserID != record.UserID ||
		createdAt.IsZero() {
		return Authentication{}, newError(op, CodeInvalidSessionRecord, nil)
	}
	return Authentication{
		CreatedAt:     createdAt,
		principal:     principal.Copy(),
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

func (s *Service) WebSessions(
	ctx context.Context,
	currentToken SessionToken,
) ([]WebSession, error) {
	const op = "list web sessions"

	repository, digest, current, err := s.currentUserSession(ctx, currentToken)
	if err != nil {
		return nil, err
	}
	records, err := repository.UserSessions(ctx, current.UserID)
	if err != nil {
		return nil, repositoryError(op, err)
	}
	result := make([]WebSession, 0, len(records))
	for _, record := range records {
		result = append(result, WebSession{
			ID:         webSessionID(record.SessionTokenDigest),
			CreatedAt:  record.CreatedAt.UTC(),
			LastSeenAt: record.LastSeenAt.UTC(),
			UserAgent:  record.UserAgent,
			AccessIP:   record.AccessIP,
			AccessHost: record.AccessHost,
			Current:    record.SessionTokenDigest == digest,
		})
	}
	return result, nil
}

func (s *Service) refreshSessionMetadata(ctx context.Context, record SessionRecord) {
	client := sessionClientFromContext(ctx)
	client = mergeSessionClient(
		client,
		record.UserAgent,
		record.AccessIP,
		record.AccessHost,
	)
	now := s.now().UTC()
	if !sessionMetadataNeedsRefresh(record.LastSeenAt, record.UserAgent, record.AccessIP, record.AccessHost, client, now) {
		return
	}
	repository, ok := s.repository.(SessionMetadataRepository)
	if !ok {
		return
	}
	_ = repository.UpdateSessionMetadata(ctx, record.SessionTokenDigest, client, now)
}

func (s *Service) refreshUserSessionMetadata(
	ctx context.Context,
	repository MultiUserRepository,
	record UserSessionRecord,
) {
	client := sessionClientFromContext(ctx)
	client = mergeSessionClient(
		client,
		record.UserAgent,
		record.AccessIP,
		record.AccessHost,
	)
	now := s.now().UTC()
	if !sessionMetadataNeedsRefresh(record.LastSeenAt, record.UserAgent, record.AccessIP, record.AccessHost, client, now) {
		return
	}
	metadataRepository, ok := repository.(UserSessionMetadataRepository)
	if !ok {
		return
	}
	_ = metadataRepository.UpdateUserSessionMetadata(
		ctx,
		record.UserID,
		record.SessionTokenDigest,
		client,
		now,
	)
}

func mergeSessionClient(
	client SessionClient,
	userAgent, accessIP, accessHost string,
) SessionClient {
	if client.UserAgent == "" {
		client.UserAgent = userAgent
	}
	if client.AccessIP == "" {
		client.AccessIP = accessIP
	}
	if client.AccessHost == "" {
		client.AccessHost = accessHost
	}
	return client
}

func sessionMetadataNeedsRefresh(
	lastSeenAt time.Time,
	userAgent, accessIP, accessHost string,
	client SessionClient,
	now time.Time,
) bool {
	if client.UserAgent == "" && client.AccessIP == "" && client.AccessHost == "" {
		return false
	}
	return userAgent != client.UserAgent ||
		accessIP != client.AccessIP ||
		accessHost != client.AccessHost ||
		lastSeenAt.IsZero() ||
		now.Sub(lastSeenAt) >= sessionMetadataRefresh
}

func (s *Service) RevokeWebSession(
	ctx context.Context,
	currentToken SessionToken,
	sessionID string,
) (SessionTokenDigest, error) {
	const op = "revoke web session"

	repository, currentDigest, current, err := s.currentUserSession(ctx, currentToken)
	if err != nil {
		return SessionTokenDigest{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionTokenDigest{}, newError(op, CodeSessionNotFound, nil)
	}
	records, err := repository.UserSessions(ctx, current.UserID)
	if err != nil {
		return SessionTokenDigest{}, repositoryError(op, err)
	}
	for _, record := range records {
		if webSessionID(record.SessionTokenDigest) != sessionID {
			continue
		}
		if record.SessionTokenDigest == currentDigest {
			return SessionTokenDigest{}, newError(op, CodeCurrentSession, nil)
		}
		deleted, err := repository.DeleteUserSession(
			ctx,
			current.UserID,
			record.SessionTokenDigest,
		)
		if err != nil {
			return SessionTokenDigest{}, repositoryError(op, err)
		}
		if !deleted {
			return SessionTokenDigest{}, newError(op, CodeSessionNotFound, nil)
		}
		return record.SessionTokenDigest, nil
	}
	return SessionTokenDigest{}, newError(op, CodeSessionNotFound, nil)
}

func (s *Service) RevokeOtherWebSessions(
	ctx context.Context,
	currentToken SessionToken,
) ([]SessionTokenDigest, error) {
	const op = "revoke other web sessions"

	repository, currentDigest, current, err := s.currentUserSession(ctx, currentToken)
	if err != nil {
		return nil, err
	}
	digests, err := repository.DeleteOtherUserSessions(
		ctx,
		current.UserID,
		currentDigest,
	)
	if err != nil {
		return nil, repositoryError(op, err)
	}
	return digests, nil
}

func (s *Service) currentUserSession(
	ctx context.Context,
	token SessionToken,
) (SessionManagementRepository, SessionTokenDigest, UserSessionRecord, error) {
	const op = "read current web session"

	repository, ok := s.repository.(SessionManagementRepository)
	if !ok {
		return nil, SessionTokenDigest{}, UserSessionRecord{}, repositoryError(
			op,
			errSessionManagementUnavailable,
		)
	}
	digest, err := sessionTokenDigest(token)
	if err != nil {
		return nil, SessionTokenDigest{}, UserSessionRecord{}, newError(
			op,
			CodeInvalidSessionToken,
			nil,
		)
	}
	record, _, found, err := repository.UserSessionByTokenDigest(ctx, digest)
	if err != nil {
		return nil, SessionTokenDigest{}, UserSessionRecord{}, repositoryError(op, err)
	}
	if !found {
		return nil, SessionTokenDigest{}, UserSessionRecord{}, newError(
			op,
			CodeUnauthenticated,
			nil,
		)
	}
	return repository, digest, record, nil
}

func webSessionID(digest SessionTokenDigest) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("modemdeck-web-session-id\x00"))
	_, _ = hash.Write(digest[:])
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil)[:16])
}
