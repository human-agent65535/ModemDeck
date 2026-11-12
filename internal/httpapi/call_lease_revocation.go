package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

const callRevocationTimeout = 20 * time.Second

func (api *API) revokeCallHolder(ctx context.Context, holderID string) error {
	if api.callLeases == nil {
		return nil
	}
	callIDs, err := api.callLeases.RevokeHolder(holderID)
	preparationErr := api.cancelRevokedPreparations(ctx, func(p *outgoingPreparation) bool { return p.owner.HolderID == holderID })
	if err != nil {
		return errors.Join(fmt.Errorf("revoke call holder: %w", err), preparationErr)
	}
	return errors.Join(api.finishRevokedCalls(ctx, callIDs), preparationErr)
}

func (api *API) revokeCallSubject(ctx context.Context, subjectID string) error {
	if api.callLeases == nil {
		return nil
	}
	callIDs, err := api.callLeases.RevokeSubject(subjectID)
	preparationErr := api.cancelRevokedPreparations(ctx, func(p *outgoingPreparation) bool { return p.owner.SubjectID == subjectID })
	if err != nil {
		return errors.Join(fmt.Errorf("revoke call subject: %w", err), preparationErr)
	}
	return errors.Join(api.finishRevokedCalls(ctx, callIDs), preparationErr)
}

func (api *API) revokeCallSessionToken(
	ctx context.Context,
	token auth.SessionToken,
) error {
	if strings.TrimSpace(string(token)) == "" {
		return nil
	}
	return api.revokeCallHolder(ctx, callLeaseHolderForSessionToken(token))
}

func (api *API) finishRevokedCalls(ctx context.Context, callIDs []string) error {
	if len(callIDs) == 0 {
		// A pending reservation may still have had its liveness deadline reset.
		// Invalidate the projection for every revocation.
		api.publishLiveState()
		return nil
	}
	cleanupParent := context.WithoutCancel(normalizeRequestContext(ctx))
	var result error
	for _, callID := range callIDs {
		mediaContext, cancelMedia := context.WithTimeout(
			cleanupParent,
			callRevocationTimeout,
		)
		if api.callMedia == nil {
			result = errors.Join(result, fmt.Errorf(
				"close revoked call media %s: media service is unavailable",
				callID,
			))
		} else if err := api.callMedia.CloseCall(mediaContext, callID); err != nil {
			result = errors.Join(result, fmt.Errorf(
				"close revoked call media %s: %w",
				callID,
				err,
			))
		}
		cancelMedia()

		callContext, cancelCall := context.WithTimeout(
			cleanupParent,
			callRevocationTimeout,
		)
		if api.communications == nil {
			result = errors.Join(result, fmt.Errorf(
				"end revoked call %s: communication service is unavailable",
				callID,
			))
		} else if err := api.communications.EndCall(callContext, callID); err != nil {
			result = errors.Join(result, fmt.Errorf(
				"end revoked call %s: %w",
				callID,
				err,
			))
		}
		cancelCall()
	}
	api.publishLiveState()
	return result
}

func normalizeRequestContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// cancelRevokedPreparations stops every matching outgoing preparation. A
// preparation is always marked cancelled locally so it can never dispatch, and
// any lease or media session that could not be released is reported so the
// caller does not treat the revocation as complete.
func (api *API) cancelRevokedPreparations(ctx context.Context, matches func(*outgoingPreparation) bool) error {
	api.preparationsMu.Lock()
	selected := make([]*outgoingPreparation, 0)
	for _, p := range api.preparations {
		if matches(p) {
			selected = append(selected, p)
		}
	}
	api.preparationsMu.Unlock()
	var result error
	for _, p := range selected {
		p.cancelRequested.Store(true)
		p.dispatchMu.Lock()
		if p.dispatchCancel != nil {
			p.dispatchCancel()
		}
		p.dispatchMu.Unlock()
		p.mu.Lock()
		p.setStateLocked(preparationCancelled)
		if leases, ok := api.preparationLease(); ok {
			if _, err := leases.CancelOutgoing(p.id, p.owner.HolderID); err != nil {
				result = errors.Join(result, fmt.Errorf("cancel revoked outgoing preparation %s: %w", p.id, err))
			}
		}
		if p.session != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), callRevocationTimeout)
			if err := p.session.Close(cleanup); err != nil {
				result = errors.Join(result, fmt.Errorf("close revoked preparation media %s: %w", p.id, err))
			}
			cancel()
		}
		p.mu.Unlock()
	}
	return result
}
