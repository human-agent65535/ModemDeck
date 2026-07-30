package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
)

const callLeaseHolderScopePrefix = "session-"

var errCallLeaseSessionUnavailable = errors.New(
	"authenticated call lease session is unavailable",
)

type callLeaseSessionScope [sha256.Size]byte

type callLeaseSessionScopeContextKey struct{}

type callLeaseHolder struct {
	ClientID string
	LeaseID  string
}

func contextWithCallLeaseSession(
	ctx context.Context,
	token auth.SessionToken,
) context.Context {
	scope := callLeaseSessionScope(sha256.Sum256([]byte(token)))
	return context.WithValue(ctx, callLeaseSessionScopeContextKey{}, scope)
}

func (api *API) callLeaseHolder(
	ctx context.Context,
	clientID string,
) (callLeaseHolder, error) {
	clientID, err := calllease.NormalizeHolderID(clientID)
	if err != nil {
		return callLeaseHolder{}, err
	}
	if api.authenticator == nil {
		return callLeaseHolder{ClientID: clientID, LeaseID: clientID}, nil
	}

	scope, ok := ctx.Value(callLeaseSessionScopeContextKey{}).(callLeaseSessionScope)
	if !ok {
		return callLeaseHolder{}, errCallLeaseSessionUnavailable
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("modemdeck-call-lease-holder\x00"))
	_, _ = digest.Write(scope[:])
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(clientID))
	leaseID := callLeaseHolderScopePrefix +
		base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
	return callLeaseHolder{ClientID: clientID, LeaseID: leaseID}, nil
}
