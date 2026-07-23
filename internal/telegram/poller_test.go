package telegram

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPollerSortsUpdatesAndAdvancesMonotonically(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requests []GetUpdatesRequest
	callCount := 0
	bot := botStub{updates: func(_ context.Context, request GetUpdatesRequest) ([]Update, error) {
		requests = append(requests, request)
		callCount++
		if callCount == 1 {
			return []Update{
				{UpdateID: 7},
				{UpdateID: 5},
				{UpdateID: 6},
				{UpdateID: 5},
			}, nil
		}
		cancel()
		return nil, context.Canceled
	}}
	var handled []int64
	handler := updateHandlerFunc(func(_ context.Context, update Update) error {
		handled = append(handled, update.UpdateID)
		if update.UpdateID == 6 {
			return errors.New("ack failed after action")
		}
		return nil
	})
	checkpoint := &checkpointRecorder{next: 5}
	recorder := &eventRecorder{}
	poller := mustPoller(t, bot, handler, checkpoint, recorder, PollOptions{})

	err := poller.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(handled, []int64{5, 6, 7}) {
		t.Fatalf("handled = %#v", handled)
	}
	if !reflect.DeepEqual(checkpoint.advances, []int64{6, 7, 8}) {
		t.Fatalf("checkpoint advances = %#v", checkpoint.advances)
	}
	if len(requests) != 2 || requests[0].Offset != 5 || requests[1].Offset != 8 {
		t.Fatalf("requests = %#v", requests)
	}
	if requests[0].Timeout != 30*time.Second || requests[0].Limit != 50 {
		t.Fatalf("default request = %#v", requests[0])
	}
}

func TestPollerStopsAfterBoundedFailuresAndHonorsServerRetryAfter(t *testing.T) {
	t.Parallel()

	var calls int
	bot := botStub{updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
		calls++
		return nil, &APIError{Code: 429, RetryAfter: 25 * time.Second}
	}}
	checkpoint := &checkpointRecorder{}
	poller := mustPoller(t, bot, updateHandlerFunc(func(context.Context, Update) error {
		t.Fatal("handler called")
		return nil
	}), checkpoint, nil, PollOptions{
		MinFailureBackoff:      time.Second,
		MaxFailureBackoff:      10 * time.Second,
		MaxConsecutiveFailures: 3,
	})
	var sleeps []time.Duration
	poller.sleep = func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	}

	err := poller.Run(context.Background())
	var pollingErr *PollingError
	if !errors.As(err, &pollingErr) || pollingErr.Failures != 3 {
		t.Fatalf("Run() error = %#v, want PollingError with 3 failures", err)
	}
	if calls != 3 {
		t.Fatalf("GetUpdates calls = %d, want 3", calls)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{25 * time.Second, 25 * time.Second}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestPollerCapsUntrustedServerRetryAfter(t *testing.T) {
	t.Parallel()

	poller := mustPoller(t, botStub{}, updateHandlerFunc(func(context.Context, Update) error {
		return nil
	}), &checkpointRecorder{}, nil, PollOptions{})
	delay := poller.failureDelay(&APIError{Code: 429, RetryAfter: 72 * time.Hour}, 1)
	if delay != maxServerRetryAfter {
		t.Fatalf("failureDelay() = %s, want %s", delay, maxServerRetryAfter)
	}
}

func TestPollerUsesExponentialFailureBackoff(t *testing.T) {
	t.Parallel()

	var calls int
	bot := botStub{updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
		calls++
		return nil, errors.New("temporary")
	}}
	poller := mustPoller(t, bot, updateHandlerFunc(func(context.Context, Update) error {
		return nil
	}), &checkpointRecorder{}, nil, PollOptions{
		MinFailureBackoff:      time.Second,
		MaxFailureBackoff:      10 * time.Second,
		MaxConsecutiveFailures: 4,
	})
	var sleeps []time.Duration
	poller.sleep = func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	}

	var pollingErr *PollingError
	if err := poller.Run(context.Background()); !errors.As(err, &pollingErr) {
		t.Fatalf("Run() error = %v, want PollingError", err)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestPollerThrottlesImmediateEmptyResponses(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	bot := botStub{updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
		calls++
		if calls == 1 {
			return nil, nil
		}
		cancel()
		return nil, context.Canceled
	}}
	poller := mustPoller(t, bot, updateHandlerFunc(func(context.Context, Update) error {
		return nil
	}), &checkpointRecorder{}, nil, PollOptions{
		MinimumEmptyInterval: 300 * time.Millisecond,
	})
	fixedNow := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	poller.now = func() time.Time { return fixedNow }
	var sleeps []time.Duration
	poller.sleep = func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	}

	if err := poller.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{300 * time.Millisecond}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestPollerThrottlesResponsesContainingOnlyStaleUpdates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	bot := botStub{updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
		calls++
		if calls == 1 {
			return []Update{{UpdateID: 9}}, nil
		}
		cancel()
		return nil, context.Canceled
	}}
	poller := mustPoller(t, bot, updateHandlerFunc(func(context.Context, Update) error {
		t.Fatal("stale update was handled")
		return nil
	}), &checkpointRecorder{next: 10}, nil, PollOptions{
		MinimumEmptyInterval: 300 * time.Millisecond,
	})
	fixedNow := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	poller.now = func() time.Time { return fixedNow }
	var sleeps []time.Duration
	poller.sleep = func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	}

	if err := poller.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{300 * time.Millisecond}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestPollerStopsOnCheckpointFailureWithoutRefetch(t *testing.T) {
	t.Parallel()

	var calls int
	bot := botStub{updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
		calls++
		return []Update{{UpdateID: 10}}, nil
	}}
	var handled int
	handler := updateHandlerFunc(func(context.Context, Update) error {
		handled++
		return nil
	})
	checkpoint := &checkpointRecorder{advanceErr: errors.New("disk unavailable")}
	poller := mustPoller(t, bot, handler, checkpoint, nil, PollOptions{})

	err := poller.Run(context.Background())
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Operation != "advance_telegram_offset" {
		t.Fatalf("Run() error = %#v", err)
	}
	if calls != 1 || handled != 1 {
		t.Fatalf("calls=%d handled=%d", calls, handled)
	}
}

func TestPollerOptionValidation(t *testing.T) {
	t.Parallel()

	tests := []PollOptions{
		{LongPollTimeout: 51 * time.Second},
		{BatchSize: 101},
		{MinFailureBackoff: time.Millisecond},
		{MinFailureBackoff: time.Second, MaxFailureBackoff: 500 * time.Millisecond},
		{MaxConsecutiveFailures: 101},
		{MinimumEmptyInterval: time.Millisecond},
	}
	for _, options := range tests {
		if _, err := NewPoller(botStub{}, updateHandlerFunc(func(context.Context, Update) error {
			return nil
		}), &checkpointRecorder{}, nil, options); err == nil {
			t.Fatalf("NewPoller(%#v) returned nil error", options)
		}
	}
}

func TestMemoryCheckpointRejectsRegressionAndIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	checkpoint, err := NewMemoryCheckpoint(10)
	if err != nil {
		t.Fatalf("NewMemoryCheckpoint() error = %v", err)
	}
	if err := checkpoint.Advance(context.Background(), 12); err != nil {
		t.Fatalf("Advance(12) error = %v", err)
	}
	if err := checkpoint.Advance(context.Background(), 11); err == nil {
		t.Fatal("Advance(11) returned nil error")
	}

	var wait sync.WaitGroup
	for offset := int64(12); offset < 30; offset++ {
		offset := offset
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = checkpoint.Advance(context.Background(), offset)
			_, _ = checkpoint.NextOffset(context.Background())
		}()
	}
	wait.Wait()
	next, err := checkpoint.NextOffset(context.Background())
	if err != nil {
		t.Fatalf("NextOffset() error = %v", err)
	}
	if next < 12 || next > 29 {
		t.Fatalf("next offset = %d", next)
	}
}

func TestPollerDependencyAndCheckpointValidation(t *testing.T) {
	t.Parallel()

	handler := updateHandlerFunc(func(context.Context, Update) error { return nil })
	checkpoint := &checkpointRecorder{}
	tests := []struct {
		name       string
		bot        BotAPI
		handler    UpdateHandler
		checkpoint Checkpoint
		field      string
	}{
		{name: "bot", handler: handler, checkpoint: checkpoint, field: "poller.bot"},
		{name: "handler", bot: botStub{}, checkpoint: checkpoint, field: "poller.handler"},
		{name: "checkpoint", bot: botStub{}, handler: handler, field: "poller.checkpoint"},
	}
	for _, test := range tests {
		_, err := NewPoller(test.bot, test.handler, test.checkpoint, nil, PollOptions{})
		var configErr *ConfigError
		if !errors.As(err, &configErr) || configErr.Field != test.field {
			t.Errorf("%s error = %v", test.name, err)
		}
	}

	if _, err := NewMemoryCheckpoint(-1); err == nil {
		t.Fatal("NewMemoryCheckpoint(-1) returned nil error")
	}
}

func TestPollerRejectsInvalidAndOverflowingCheckpoints(t *testing.T) {
	t.Parallel()

	t.Run("negative initial offset", func(t *testing.T) {
		poller := mustPoller(t, botStub{}, updateHandlerFunc(func(context.Context, Update) error {
			return nil
		}), &checkpointRecorder{next: -1}, nil, PollOptions{})
		err := poller.Run(context.Background())
		var operationErr *OperationError
		if !errors.As(err, &operationErr) || operationErr.Kind != "invalid_checkpoint" {
			t.Fatalf("Run() error = %#v", err)
		}
	})

	t.Run("load failure", func(t *testing.T) {
		poller := mustPoller(t, botStub{}, updateHandlerFunc(func(context.Context, Update) error {
			return nil
		}), &checkpointRecorder{loadErr: errors.New("database unavailable")}, nil, PollOptions{})
		err := poller.Run(context.Background())
		var operationErr *OperationError
		if !errors.As(err, &operationErr) || operationErr.Operation != "load_telegram_offset" {
			t.Fatalf("Run() error = %#v", err)
		}
	})

	t.Run("update ID overflow", func(t *testing.T) {
		bot := botStub{updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
			return []Update{{UpdateID: int64(^uint64(0) >> 1)}}, nil
		}}
		poller := mustPoller(t, bot, updateHandlerFunc(func(context.Context, Update) error {
			t.Fatal("overflow update must not be handled")
			return nil
		}), &checkpointRecorder{}, nil, PollOptions{})
		err := poller.Run(context.Background())
		var operationErr *OperationError
		if !errors.As(err, &operationErr) || operationErr.Kind != "offset_overflow" {
			t.Fatalf("Run() error = %#v", err)
		}
	})
}

func TestSleepContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepContext() error = %v", err)
	}
}

func TestObserverFunc(t *testing.T) {
	t.Parallel()

	called := false
	observer := ObserverFunc(func(_ context.Context, event Event) {
		called = event.Kind == EventUpdateHandled
	})
	observer.Observe(context.Background(), Event{Kind: EventUpdateHandled})
	if !called {
		t.Fatal("ObserverFunc did not invoke function")
	}
}

type updateHandlerFunc func(context.Context, Update) error

func (f updateHandlerFunc) HandleUpdate(ctx context.Context, update Update) error {
	return f(ctx, update)
}

type checkpointRecorder struct {
	mu         sync.Mutex
	next       int64
	advances   []int64
	loadErr    error
	advanceErr error
}

func (c *checkpointRecorder) NextOffset(context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.next, c.loadErr
}

func (c *checkpointRecorder) Advance(_ context.Context, next int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.advanceErr != nil {
		return c.advanceErr
	}
	if next < c.next {
		return errors.New("regression")
	}
	c.next = next
	c.advances = append(c.advances, next)
	return nil
}

func mustPoller(
	t *testing.T,
	bot BotAPI,
	handler UpdateHandler,
	checkpoint Checkpoint,
	observer Observer,
	options PollOptions,
) *Poller {
	t.Helper()
	poller, err := NewPoller(bot, handler, checkpoint, observer, options)
	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}
	return poller
}
