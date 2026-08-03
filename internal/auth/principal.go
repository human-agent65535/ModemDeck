package auth

import (
	"context"
	"time"
)

const InitialAdminUserID = "user_admin"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Principal struct {
	UserID            string
	Username          string
	Role              Role
	ProfileContactID  string
	IOSPairingEnabled bool
	AllowedLineIDs    []string
}

func (p Principal) IsAdmin() bool {
	return p.Role == RoleAdmin
}

func (p Principal) CanAccessLine(lineID string) bool {
	for _, allowed := range p.AllowedLineIDs {
		if allowed == lineID {
			return true
		}
	}
	return false
}

func (p Principal) Copy() Principal {
	result := p
	result.AllowedLineIDs = append([]string(nil), p.AllowedLineIDs...)
	return result
}

type principalContextKey struct{}

func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal.Copy())
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok || principal.UserID == "" {
		return Principal{}, false
	}
	return principal.Copy(), true
}

type UserCredentials struct {
	ID           string
	Username     string
	PasswordHash string
	Role         Role
	Enabled      bool
}

type UserSessionRecord struct {
	UserID             string
	SessionTokenDigest SessionTokenDigest
	CSRFTokenDigest    CSRFTokenDigest
	CreatedAt          time.Time
	LastSeenAt         time.Time
	UserAgent          string
	AccessIP           string
	AccessHost         string
}

// SessionMetadataRepository updates the non-authoritative client facts that
// are shown in account security. They describe the most recent observation of
// a session and must never be used as an authentication binding.
type SessionMetadataRepository interface {
	UpdateSessionMetadata(
		ctx context.Context,
		digest SessionTokenDigest,
		client SessionClient,
		lastSeenAt time.Time,
	) error
}

// UserSessionMetadataRepository is the multi-user equivalent of
// SessionMetadataRepository.
type UserSessionMetadataRepository interface {
	UpdateUserSessionMetadata(
		ctx context.Context,
		userID string,
		digest SessionTokenDigest,
		client SessionClient,
		lastSeenAt time.Time,
	) error
}

type MultiUserRepository interface {
	UserCredentialsByUsername(
		ctx context.Context,
		username string,
	) (credentials UserCredentials, found bool, err error)
	UserCredentialsByID(
		ctx context.Context,
		userID string,
	) (credentials UserCredentials, found bool, err error)
	CreateUserSessionIfPasswordHash(
		ctx context.Context,
		expectedPasswordHash string,
		session UserSessionRecord,
	) (created bool, err error)
	UserSessionByTokenDigest(
		ctx context.Context,
		digest SessionTokenDigest,
	) (session UserSessionRecord, principal Principal, found bool, err error)
	ReplaceUserPasswordHashIfCurrentAndRevokeSessions(
		ctx context.Context,
		userID, expectedPasswordHash, replacementPasswordHash string,
	) (replaced bool, err error)
}

type SessionManagementRepository interface {
	MultiUserRepository
	UserSessions(ctx context.Context, userID string) ([]UserSessionRecord, error)
	DeleteUserSession(
		ctx context.Context,
		userID string,
		digest SessionTokenDigest,
	) (deleted bool, err error)
	DeleteOtherUserSessions(
		ctx context.Context,
		userID string,
		currentDigest SessionTokenDigest,
	) ([]SessionTokenDigest, error)
}
