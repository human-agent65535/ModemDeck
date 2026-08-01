package communication

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
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

type controlLeaseEventAgent struct {
	*fakeAgent
	started       chan struct{}
	disconnect    chan struct{}
	released      chan struct{}
	renewStarted  chan struct{}
	renewContinue chan struct{}
	startOnce     sync.Once
	renewOnce     sync.Once
	releaseOnce   sync.Once
	watchCalls    atomic.Int32
	renewals      atomic.Int32
	releaseCalls  atomic.Int32
	releaseErr    error
}

func (agent *controlLeaseEventAgent) WatchChanges(
	ctx context.Context,
	notify func(),
) error {
	if agent.watchCalls.Add(1) != 1 {
		<-ctx.Done()
		return nil
	}
	agent.startOnce.Do(func() { close(agent.started) })
	notify()
	select {
	case <-ctx.Done():
		return nil
	case <-agent.disconnect:
		return errors.New("fixture agent event stream disconnected")
	}
}

func (agent *controlLeaseEventAgent) RenewControlLease(
	ctx context.Context,
) (agentclient.ControlLeaseStatus, error) {
	agent.renewals.Add(1)
	if agent.renewStarted != nil {
		agent.renewOnce.Do(func() { close(agent.renewStarted) })
	}
	if agent.renewContinue != nil {
		select {
		case <-agent.renewContinue:
		case <-ctx.Done():
			return agentclient.ControlLeaseStatus{}, ctx.Err()
		}
	}
	return agentclient.ControlLeaseStatus{
		ControllerID: "fixture",
		ExpiresAt:    time.Now().Add(5 * time.Second),
	}, nil
}

func (agent *controlLeaseEventAgent) ReleaseControlLease(context.Context) error {
	agent.releaseCalls.Add(1)
	if agent.released != nil {
		agent.releaseOnce.Do(func() { close(agent.released) })
	}
	return agent.releaseErr
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

func TestRunCoalescesAgentEventBurstsIntoOneRefresh(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.health.Provider.Capabilities.Events = true
	agent := &eventTestAgent{
		fakeAgent: baseAgent,
		changes:   make(chan struct{}, 16),
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
	for range 16 {
		agent.changes <- struct{}{}
	}
	waitForAppliedSnapshots(t, repository, before+1)
	quietWindow := time.NewTimer(2 * agentEventCoalesceDelay)
	defer quietWindow.Stop()
	<-quietWindow.C
	if got := repository.applied.Load(); got != before+1 {
		t.Fatalf("applied snapshots after one event burst = %d, want %d", got, before+1)
	}
}

func TestRunReleasesControlLeaseWhenAgentEventStreamDisconnects(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.health.Provider.Capabilities.Events = true
	baseAgent.health.Provider.Capabilities.ControlLease = true
	agent := &controlLeaseEventAgent{
		fakeAgent:  baseAgent,
		started:    make(chan struct{}),
		disconnect: make(chan struct{}),
		released:   make(chan struct{}),
	}
	service, err := New(agent, &fakeRepository{}, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx, time.Hour, func(err error) {
			t.Logf("Run() report = %v", err)
		})
	}()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case <-agent.started:
	case <-time.After(time.Second):
		t.Fatal("event watcher did not start")
	}
	waitForAgentEventsHealthy(t, service)
	if err := service.ensureAgentControlLease(context.Background()); err != nil {
		t.Fatalf("ensureAgentControlLease() error = %v", err)
	}
	waitForCount(t, &agent.renewals, 1, "control lease renewals")
	close(agent.disconnect)
	select {
	case <-agent.released:
	case <-time.After(time.Second):
		t.Fatal("control lease was not released after agent event disconnect")
	}
	if got := agent.releaseCalls.Load(); got != 1 {
		t.Fatalf("control lease releases = %d, want 1", got)
	}
}

func TestFailedControlReleaseIsNotRenewed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.health.Provider.Capabilities.ControlLease = true
	agent := &controlLeaseEventAgent{
		fakeAgent:  baseAgent,
		releaseErr: errors.New("fixture release failed"),
	}
	service, err := New(agent, &fakeRepository{}, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if err := service.ensureAgentControlLease(context.Background()); err != nil {
		t.Fatalf("ensureAgentControlLease() error = %v", err)
	}
	if err := service.releaseAgentControlLease(context.Background()); err == nil {
		t.Fatal("releaseAgentControlLease() succeeded")
	}
	if err := service.renewAgentControlLease(context.Background()); err != nil {
		t.Fatalf("renewAgentControlLease() error after release = %v", err)
	}
	if got := agent.renewals.Load(); got != 1 {
		t.Fatalf("control lease renewals = %d, want 1", got)
	}
}

func TestControlReleaseWaitsForInFlightRenewal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.health.Provider.Capabilities.ControlLease = true
	agent := &controlLeaseEventAgent{
		fakeAgent:     baseAgent,
		renewStarted:  make(chan struct{}),
		renewContinue: make(chan struct{}),
	}
	service, err := New(agent, &fakeRepository{}, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	renewed := make(chan error, 1)
	go func() {
		renewed <- service.ensureAgentControlLease(context.Background())
	}()
	select {
	case <-agent.renewStarted:
	case <-time.After(time.Second):
		t.Fatal("control lease renewal did not start")
	}

	released := make(chan error, 1)
	go func() {
		released <- service.releaseAgentControlLease(context.Background())
	}()
	close(agent.renewContinue)
	if err := <-renewed; err != nil {
		t.Fatalf("ensureAgentControlLease() error = %v", err)
	}
	if err := <-released; err != nil {
		t.Fatalf("releaseAgentControlLease() error = %v", err)
	}
	if err := service.renewAgentControlLease(context.Background()); err != nil {
		t.Fatalf("renewAgentControlLease() error after release = %v", err)
	}
	if got := agent.renewals.Load(); got != 1 {
		t.Fatalf("control lease renewals = %d, want 1", got)
	}
	if got := agent.releaseCalls.Load(); got != 1 {
		t.Fatalf("control lease releases = %d, want 1", got)
	}
	service.controlMu.RLock()
	wanted := service.controlLeaseWanted
	active := service.controlLeaseActive
	service.controlMu.RUnlock()
	if wanted || active {
		t.Fatalf("control lease state after release = wanted:%t active:%t", wanted, active)
	}
}

func TestStartCallRenewsControlLeaseWithoutAgentEvents(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.health.Provider.Capabilities.ControlLease = true
	baseAgent.startResult = agentclient.CommandReceipt{
		RequestID:  "request-call-lease",
		ResourceID: "call-endpoint-1",
	}
	startedSnapshot := baseAgent.snapshot
	startedSnapshot.ObservedAt = now.Add(time.Second)
	startedSnapshot.Calls = []agentclient.Call{{
		ID:        "call-endpoint-1",
		LineID:    "line-1",
		Number:    "+818012345678",
		Direction: "outgoing",
		State:     "dialing",
		StateCode: 1,
	}}
	baseAgent.snapshotAfterStart = &startedSnapshot
	agent := &controlLeaseEventAgent{fakeAgent: baseAgent}
	repository := &fakeRepository{
		snapshotResult: store.HardwareSnapshotResult{
			LineIDsByEndpoint: map[string]string{"line-1": "line-stable"},
		},
	}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := service.StartCall(context.Background(), StartCallInput{
		RequestID: "request-call-lease",
		LineID:    "line-stable",
		Number:    "+818012345678",
	}); err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if got := agent.renewals.Load(); got != 1 {
		t.Fatalf("control lease renewals = %d, want 1", got)
	}
}

func waitForAgentEventsHealthy(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		service.controlMu.RLock()
		healthy := service.agentEventsHealthy
		service.controlMu.RUnlock()
		if healthy {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("agent event stream did not become healthy")
		}
	}
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

func waitForCount(
	t *testing.T,
	value *atomic.Int32,
	expected int32,
	label string,
) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for value.Load() < expected {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("%s = %d, want at least %d", label, value.Load(), expected)
		}
	}
}

var _ Agent = (*eventTestAgent)(nil)
var _ AgentChangeSource = (*eventTestAgent)(nil)
var _ Agent = (*controlLeaseEventAgent)(nil)
var _ AgentChangeSource = (*controlLeaseEventAgent)(nil)
var _ AgentControlLease = (*controlLeaseEventAgent)(nil)
var _ Repository = (*eventCountingRepository)(nil)
