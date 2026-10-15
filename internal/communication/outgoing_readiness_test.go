package communication

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type outgoingDispatchRepository struct {
	*fakeRepository
	commandCreated chan struct{}
}

func (r *outgoingDispatchRepository) BeginHardwareCommand(ctx context.Context, requestID, operation string, digest []byte) (store.HardwareCommand, bool, error) {
	command, created, err := r.fakeRepository.BeginHardwareCommand(ctx, requestID, operation, digest)
	close(r.commandCreated)
	return command, created, err
}

// Readiness at the HTTP boundary is insufficient: the hardware coordinator
// may be occupied by an earlier snapshot while the prepared transport closes.
func TestStartCallRechecksPreparedAudioAtHardwareDispatch(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "transport_lost"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)
			agent := connectedAgent(now)
			repository := &outgoingDispatchRepository{
				fakeRepository: &fakeRepository{},
				commandCreated: make(chan struct{}),
			}
			service, err := New(agent, repository, messageevents.NewBuffer(8))
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			service.status = Status{
				Connected:  true,
				ObservedAt: now,
				Lines:      []store.LineSummary{projectLine(agent.snapshot.Lines[0])},
			}
			var ready atomic.Bool
			ready.Store(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service.coordinatorMu.Lock()
			result := make(chan error, 1)
			go func() {
				_, err := service.StartCall(ctx, StartCallInput{
					RequestID:   "outgoing-dispatch-readiness",
					LineID:      "line-1",
					Number:      "+818012345678",
					ReadyToDial: ready.Load,
				})
				result <- err
			}()
			select {
			case <-repository.commandCreated:
			case err := <-result:
				service.coordinatorMu.Unlock()
				t.Fatalf("call failed before reaching dispatch boundary: %v", err)
			case <-time.After(2 * time.Second):
				service.coordinatorMu.Unlock()
				t.Fatal("call did not reach dispatch boundary")
			}
			if cancelled {
				cancel()
			} else {
				ready.Store(false)
			}
			service.coordinatorMu.Unlock()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("call dispatched after readiness was lost")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled dispatch did not finish")
			}
			if len(agent.startRequests) != 0 {
				t.Fatalf("hardware received %d dial commands without prepared audio", len(agent.startRequests))
			}
			if command := repository.commands["outgoing-dispatch-readiness"]; command.Status != store.HardwareCommandFailed {
				t.Fatalf("non-dispatched command was left pending: %+v", command)
			}
		})
	}
}
