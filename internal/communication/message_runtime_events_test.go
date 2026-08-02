package communication

import (
	"context"
	"errors"
	"strconv"
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
	order   *[]string
	runtime *runtimeevents.Hub
}

func (publisher orderedMessagePublisher) Publish(event messageevents.IncomingSMS) {
	*publisher.order = append(
		*publisher.order,
		"watermark:"+strconv.FormatUint(publisher.runtime.Current().DataRevision, 10),
	)
	*publisher.order = append(*publisher.order, "notification:"+event.MessageID)
}

func TestRefreshPublishesMessageNotificationBeforePostCommitWork(t *testing.T) {
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
	runtime := runtimeevents.NewHub()
	service, err := New(
		connectedAgent(observedAt),
		repository,
		orderedMessagePublisher{order: &order, runtime: runtime},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := service.SetRuntimeEventPublisher(runtime); err != nil {
		t.Fatalf("SetRuntimeEventPublisher() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want post-commit line read failure")
	}
	if len(order) != 2 || order[0] != "watermark:1" || order[1] != "notification:42" {
		t.Fatalf("publish order = %v, want durable watermark before message notification", order)
	}
}
