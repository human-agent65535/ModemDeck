package communication

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type postCommitLinesFailureRepository struct {
	*fakeRepository
	lineReads int
}

func (repository *postCommitLinesFailureRepository) Lines(
	ctx context.Context,
) ([]store.LineSummary, error) {
	repository.lineReads++
	if repository.lineReads > 1 {
		return nil, errors.New("persisted lines unavailable")
	}
	return repository.fakeRepository.Lines(ctx)
}

type orderedMessagePublisher struct {
	order *[]string
}

func (publisher orderedMessagePublisher) Publish(event messageevents.IncomingSMS) {
	*publisher.order = append(*publisher.order, "notification:"+event.MessageID)
}

type orderedRuntimePublisher struct {
	order *[]string
}

func (publisher orderedRuntimePublisher) Publish(
	event runtimeevents.Event,
) (runtimeevents.Event, bool) {
	for _, resource := range event.Resources {
		*publisher.order = append(*publisher.order, "runtime:"+string(resource))
	}
	return event, true
}

func TestRefreshPublishesMessageInvalidationBeforePostCommitWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 24, 7, 30, 0, 0, time.UTC)
	repository := &postCommitLinesFailureRepository{fakeRepository: &fakeRepository{
		snapshotResult: store.HardwareSnapshotResult{
			CreatedIncomingMessages: []store.Message{{
				ID:        42,
				LineID:    "line-1",
				Peer:      "+818012345678",
				Content:   "hello",
				Timestamp: observedAt.Format(time.RFC3339),
			}},
		},
	}}
	order := []string{}
	service, err := New(
		connectedAgent(observedAt),
		repository,
		orderedMessagePublisher{order: &order},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := service.SetRuntimeEventPublisher(orderedRuntimePublisher{order: &order}); err != nil {
		t.Fatalf("SetRuntimeEventPublisher() error = %v", err)
	}

	if _, err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want post-commit line read failure")
	}
	if len(order) < 2 || order[0] != "runtime:messages" || order[1] != "notification:42" {
		t.Fatalf("publish order = %v, want message invalidation then notification first", order)
	}
}
