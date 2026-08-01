package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

const callLeaseHolderScopePrefix = "session-"

const developmentCallLeaseHolderID = "session-development"

var errCallLeaseSessionUnavailable = errors.New(
	"authenticated call lease session is unavailable",
)

type callLeaseSessionScope [sha256.Size]byte

type callLeaseSessionScopeContextKey struct{}

func contextWithCallLeaseSession(
	ctx context.Context,
	token auth.SessionToken,
) context.Context {
	scope := callLeaseSessionScope(sha256.Sum256([]byte(token)))
	return context.WithValue(ctx, callLeaseSessionScopeContextKey{}, scope)
}

func contextWithCallLeaseMobileCredential(
	ctx context.Context,
	digest mobilepairing.TokenDigest,
) context.Context {
	return context.WithValue(
		ctx,
		callLeaseSessionScopeContextKey{},
		callLeaseSessionScope(digest),
	)
}

func (api *API) callLeaseHolder(
	ctx context.Context,
) (string, error) {
	scope, ok := ctx.Value(callLeaseSessionScopeContextKey{}).(callLeaseSessionScope)
	if !ok {
		if api.authenticator == nil {
			return developmentCallLeaseHolderID, nil
		}
		return "", errCallLeaseSessionUnavailable
	}
	return callLeaseHolderForScope(scope), nil
}

func (api *API) callLeaseOwner(ctx context.Context) (calllease.Owner, error) {
	holderID, err := api.callLeaseHolder(ctx)
	if err != nil {
		return calllease.Owner{}, err
	}
	subjectID := holderID
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		subjectID = principal.UserID
	}
	return calllease.Owner{HolderID: holderID, SubjectID: subjectID}, nil
}

func callLeaseHolderForSessionToken(token auth.SessionToken) string {
	scope := callLeaseSessionScope(sha256.Sum256([]byte(token)))
	return callLeaseHolderForScope(scope)
}

func callLeaseHolderForSessionDigest(digest auth.SessionTokenDigest) string {
	return callLeaseHolderForScope(callLeaseSessionScope(digest))
}

func callLeaseHolderForMobileCredential(
	digest mobilepairing.TokenDigest,
) string {
	return callLeaseHolderForScope(callLeaseSessionScope(digest))
}

func callLeaseHolderForScope(scope callLeaseSessionScope) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte("modemdeck-call-holder\x00"))
	_, _ = digest.Write(scope[:])
	return callLeaseHolderScopePrefix +
		base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
}
