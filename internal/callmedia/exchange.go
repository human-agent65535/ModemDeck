package callmedia

import (
	"context"
	"crypto/sha256"
	"errors"
)

// One attempt belongs to one call, owner and exact offer. A lost HTTP response
// can be recovered without another peer, codec or PCM consumer. Nothing is
// persisted; the answer expires with the media owner.
type exchangeAttempt struct {
	owner  string
	offer  [32]byte
	done   chan struct{}
	result ExchangeResult
	err    error
}

func (c *Core) Exchange(ctx context.Context, offer Offer) (ExchangeResult, error) {
	if c == nil {
		return ExchangeResult{}, ErrCoreClosed
	}
	ctx = normalizeContext(ctx)
	call, err := normalizeActiveCall(offer.Call)
	if err != nil {
		return ExchangeResult{}, err
	}
	token, err := normalizeOwnerToken(offer.OwnerToken)
	if err != nil {
		return ExchangeResult{}, err
	}
	if err := validateOfferSDP(offer.SDP); err != nil {
		return ExchangeResult{}, err
	}
	hash := sha256.Sum256([]byte(offer.SDP))
	for {
		if ctx.Err() != nil {
			return ExchangeResult{}, ErrCanceled
		}
		c.mu.Lock()
		if _, err := c.activeLifetimeLocked(call.ID); err != nil {
			c.mu.Unlock()
			return ExchangeResult{}, err
		}
		if previous := c.exchanges[call.ID]; previous != nil {
			if previous.owner != token || previous.offer != hash {
				c.mu.Unlock()
				return ExchangeResult{}, ErrCallInUse
			}
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return ExchangeResult{}, ErrCanceled
			case <-previous.done:
			}
			// An abandoned request may still have been unwinding when the retry
			// arrived. Once cleanup finishes, the live request may prepare again.
			if errors.Is(previous.err, ErrCanceled) {
				continue
			}
			c.mu.Lock()
			owner := c.owners[call.ID]
			valid := !c.closed && owner != nil && owner.ctx.Err() == nil &&
				owner.token == token && owner.session == previous.result.Session
			c.mu.Unlock()
			if previous.err != nil {
				return ExchangeResult{}, previous.err
			}
			if !valid {
				return ExchangeResult{}, ErrCallNotActive
			}
			return previous.result, nil
		}
		attempt := &exchangeAttempt{owner: token, offer: hash, done: make(chan struct{})}
		c.exchanges[call.ID] = attempt
		c.mu.Unlock()

		result, err := c.exchangeOnce(ctx, offer)
		c.mu.Lock()
		attempt.result, attempt.err = result, err
		owner := c.owners[call.ID]
		if err != nil || owner == nil || owner.session != result.Session || owner.ctx.Err() != nil {
			if c.exchanges[call.ID] == attempt {
				delete(c.exchanges, call.ID)
			}
		}
		close(attempt.done)
		c.mu.Unlock()
		return result, err
	}
}
