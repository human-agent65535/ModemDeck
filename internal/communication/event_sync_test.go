package communication

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type eventTestAgent struct {
	*fakeAgent
	changes chan struct{}
	started chan struct{}
	once    sync.Once
}

func (agent *eventTestAgent) WatchChanges(ctx context.Context, notify func()) error {
	agent.once.Do(func() { close(agent.started) })
	notify()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-agent.changes:
			notify()
		}
	}
}

type eventCountingRepository struct {
	*fakeRepository
	applied atomic.Int32
	signal  chan struct{}
}

func (repository *eventCountingRepository) ApplyHardwareSnapshotWithResult(
	ctx context.Context,
	snapshot store.HardwareSnapshot,
) (store.HardwareSnapshotResult, error) {
	result, err := repository.fakeRepository.ApplyHardwareSnapshotWithResult(ctx, snapshot)
	repository.applied.Add(1)
	select {
	case repository.signal <- struct{}{}:
	default:
	}
	return result, err
}

func TestRunRefreshesImmediatelyFromAgentEvents(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.health.Provider.Capabilities.Events = true
	agent := &eventTestAgent{
		fakeAgent: baseAgent,
		changes:   make(chan struct{}, 1),
		started:   make(chan struct{}),
	}
	repository := &eventCountingRepository{
		fakeRepository: &fakeRepository{},
		signal:         make(chan struct{}, 8),
	}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx, time.Hour, func(err error) {
			t.Errorf("Run() report = %v", err)
		})
	}()
	defer func() {
		cancel()
		<-done
	}()

	waitForAppliedSnapshots(t, repository, 1)
	select {
	case <-agent.started:
	case <-time.After(time.Second):
		t.Fatal("event watcher did not start")
	}
	waitForAppliedSnapshots(t, repository, 2)
	before := repository.applied.Load()
	agent.changes <- struct{}{}
	waitForAppliedSnapshots(t, repository, before+1)
}

func waitForAppliedSnapshots(
	t *testing.T,
	repository *eventCountingRepository,
	expected int32,
) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for repository.applied.Load() < expected {
		select {
		case <-repository.signal:
		case <-deadline.C:
			t.Fatalf("applied snapshots = %d, want at least %d", repository.applied.Load(), expected)
		}
	}
}

var _ Agent = (*eventTestAgent)(nil)
var _ AgentChangeSource = (*eventTestAgent)(nil)
var _ Repository = (*eventCountingRepository)(nil)
