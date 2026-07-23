package telegram

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

const maxServerRetryAfter = 24 * time.Hour

type UpdateHandler interface {
	HandleUpdate(context.Context, Update) error
}

type Checkpoint interface {
	// A Checkpoint instance must be durably scoped to exactly one Bot API
	// identity. Advance must reject regressions.
	NextOffset(context.Context) (int64, error)
	Advance(context.Context, int64) error
}

type PollOptions struct {
	LongPollTimeout        time.Duration
	BatchSize              int
	MinFailureBackoff      time.Duration
	MaxFailureBackoff      time.Duration
	MaxConsecutiveFailures int
	MinimumEmptyInterval   time.Duration
}

type Poller struct {
	bot        BotAPI
	handler    UpdateHandler
	checkpoint Checkpoint
	observer   Observer
	options    PollOptions
	sleep      func(context.Context, time.Duration) error
	now        func() time.Time
}

type PollingError struct {
	Failures int
	Err      error
}

func (e *PollingError) Error() string {
	return fmt.Sprintf("telegram polling stopped after %d consecutive failures", e.Failures)
}

func (e *PollingError) Unwrap() error {
	return e.Err
}

func NewPoller(
	bot BotAPI,
	handler UpdateHandler,
	checkpoint Checkpoint,
	observer Observer,
	options PollOptions,
) (*Poller, error) {
	if bot == nil {
		return nil, &ConfigError{Field: "poller.bot", Reason: "dependency is required"}
	}
	if handler == nil {
		return nil, &ConfigError{Field: "poller.handler", Reason: "dependency is required"}
	}
	if checkpoint == nil {
		return nil, &ConfigError{Field: "poller.checkpoint", Reason: "dependency is required"}
	}

	options = defaultPollOptions(options)
	if err := validatePollOptions(options); err != nil {
		return nil, err
	}
	return &Poller{
		bot:        bot,
		handler:    handler,
		checkpoint: checkpoint,
		observer:   observer,
		options:    options,
		sleep:      sleepContext,
		now:        time.Now,
	}, nil
}

func (p *Poller) Run(ctx context.Context) error {
	nextOffset, err := p.checkpoint.NextOffset(ctx)
	if err != nil {
		return &OperationError{Operation: "load_telegram_offset", Kind: errorClass(err), Err: err}
	}
	if nextOffset < 0 {
		err := errors.New("checkpoint returned a negative offset")
		return &OperationError{Operation: "load_telegram_offset", Kind: "invalid_checkpoint", Err: err}
	}

	consecutiveFailures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := p.now()
		updates, err := p.bot.GetUpdates(ctx, GetUpdatesRequest{
			Offset:  nextOffset,
			Limit:   p.options.BatchSize,
			Timeout: p.options.LongPollTimeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			consecutiveFailures++
			p.observe(ctx, Event{
				Kind:       EventPollingFailed,
				Operation:  "get_updates",
				ErrorClass: errorClass(err),
			})
			if consecutiveFailures >= p.options.MaxConsecutiveFailures {
				return &PollingError{Failures: consecutiveFailures, Err: err}
			}
			if err := p.sleep(ctx, p.failureDelay(err, consecutiveFailures)); err != nil {
				return err
			}
			continue
		}
		consecutiveFailures = 0

		sort.Slice(updates, func(i, j int) bool {
			return updates[i].UpdateID < updates[j].UpdateID
		})
		advanced := false
		for _, update := range updates {
			if update.UpdateID < nextOffset {
				continue
			}
			if update.UpdateID == math.MaxInt64 {
				err := errors.New("update ID cannot be advanced")
				return &OperationError{Operation: "advance_telegram_offset", Kind: "offset_overflow", Err: err}
			}

			if err := p.handler.HandleUpdate(ctx, update); err != nil {
				// Handler errors are consumed within this run. A process crash
				// before durable Advance can replay an update, so SMS and call
				// adapters must honor their RequestID idempotently.
				p.observe(ctx, Event{
					Kind:       EventOperationFailed,
					Operation:  "handle_update",
					ErrorClass: errorClass(err),
					UpdateID:   update.UpdateID,
				})
			}

			candidate := update.UpdateID + 1
			if err := p.checkpoint.Advance(ctx, candidate); err != nil {
				return &OperationError{Operation: "advance_telegram_offset", Kind: errorClass(err), Err: err}
			}
			nextOffset = candidate
			advanced = true
			p.observe(ctx, Event{Kind: EventUpdateHandled, Operation: "handle_update", UpdateID: update.UpdateID})
		}

		if !advanced {
			elapsed := p.now().Sub(started)
			if remaining := p.options.MinimumEmptyInterval - elapsed; remaining > 0 {
				if err := p.sleep(ctx, remaining); err != nil {
					return err
				}
			}
		}
	}
}

func (p *Poller) failureDelay(err error, failureCount int) time.Duration {
	delay := p.options.MinFailureBackoff
	for step := 1; step < failureCount && delay < p.options.MaxFailureBackoff; step++ {
		if delay > p.options.MaxFailureBackoff/2 {
			delay = p.options.MaxFailureBackoff
			break
		}
		delay *= 2
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > delay {
		delay = min(apiErr.RetryAfter, maxServerRetryAfter)
	}
	return delay
}

func (p *Poller) observe(ctx context.Context, event Event) {
	if p.observer != nil {
		p.observer.Observe(ctx, event)
	}
}

func defaultPollOptions(options PollOptions) PollOptions {
	if options.LongPollTimeout == 0 {
		options.LongPollTimeout = 30 * time.Second
	}
	if options.BatchSize == 0 {
		options.BatchSize = 50
	}
	if options.MinFailureBackoff == 0 {
		options.MinFailureBackoff = time.Second
	}
	if options.MaxFailureBackoff == 0 {
		options.MaxFailureBackoff = 30 * time.Second
	}
	if options.MaxConsecutiveFailures == 0 {
		options.MaxConsecutiveFailures = 5
	}
	if options.MinimumEmptyInterval == 0 {
		options.MinimumEmptyInterval = 250 * time.Millisecond
	}
	return options
}

func validatePollOptions(options PollOptions) error {
	switch {
	case options.LongPollTimeout < time.Second || options.LongPollTimeout > maxTelegramLongPoll:
		return &ConfigError{Field: "poller.long_poll_timeout", Reason: "must be between 1s and 50s"}
	case options.BatchSize < 1 || options.BatchSize > maxTelegramUpdatesPerCall:
		return &ConfigError{Field: "poller.batch_size", Reason: "must be between 1 and 100"}
	case options.MinFailureBackoff < 10*time.Millisecond:
		return &ConfigError{Field: "poller.min_failure_backoff", Reason: "must be at least 10ms"}
	case options.MaxFailureBackoff < options.MinFailureBackoff || options.MaxFailureBackoff > 10*time.Minute:
		return &ConfigError{Field: "poller.max_failure_backoff", Reason: "must be between min_failure_backoff and 10m"}
	case options.MaxConsecutiveFailures < 1 || options.MaxConsecutiveFailures > 100:
		return &ConfigError{Field: "poller.max_consecutive_failures", Reason: "must be between 1 and 100"}
	case options.MinimumEmptyInterval < 10*time.Millisecond || options.MinimumEmptyInterval > time.Minute:
		return &ConfigError{Field: "poller.minimum_empty_interval", Reason: "must be between 10ms and 1m"}
	default:
		return nil
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// MemoryCheckpoint is suitable for tests and ephemeral single-process use. A
// production poller needs a durable per-bot Checkpoint implementation.
type MemoryCheckpoint struct {
	mu         sync.Mutex
	nextOffset int64
}

func NewMemoryCheckpoint(nextOffset int64) (*MemoryCheckpoint, error) {
	if nextOffset < 0 {
		return nil, fmt.Errorf("telegram checkpoint offset must not be negative")
	}
	return &MemoryCheckpoint{nextOffset: nextOffset}, nil
}

func (c *MemoryCheckpoint) NextOffset(context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nextOffset, nil
}

func (c *MemoryCheckpoint) Advance(_ context.Context, nextOffset int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if nextOffset < c.nextOffset {
		return fmt.Errorf("telegram checkpoint cannot move backwards")
	}
	c.nextOffset = nextOffset
	return nil
}
