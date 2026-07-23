package telegramruntime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegram"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
)

func TestManagerVerifiesBotAndDispatchesDurableNotification(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		changes: make(chan struct{}, 1),
		units: []telegramsettings.Unit{{
			ID:      "unit-1",
			Enabled: true,
		}},
		config: telegram.Config{
			Enabled:    true,
			BotToken:   "100001:abcdefghijklmnopqrstuvwxyz",
			ChatID:     10,
			AdminID:    20,
			LineScopes: []string{"line-1"},
			Notifications: telegram.NotificationConfig{
				IncomingSMS: true,
			},
		},
	}
	repository := &fakeRepository{
		offset: 0,
		delivery: store.TelegramNotificationDelivery{
			EventKey:   "sms:1",
			UnitID:     "unit-1",
			EventType:  store.NotificationIncomingSMS,
			ResourceID: "message-1",
			LineID:     "line-1",
			Peer:       "+818012345678",
			Body:       "fixture",
			OccurredAt: time.Date(2026, time.July, 23, 13, 0, 0, 0, time.UTC),
		},
		deliveryStatus: store.NotificationPending,
		sent:           make(chan struct{}),
	}
	bot := &fakeBot{}
	manager, err := New(
		settings,
		fakeCommunications{},
		repository,
		Options{
			BotFactory: func(string) (telegram.BotAPI, error) {
				return bot, nil
			},
			PollOptions: telegram.PollOptions{
				LongPollTimeout:        time.Second,
				BatchSize:              1,
				MinFailureBackoff:      10 * time.Millisecond,
				MaxFailureBackoff:      10 * time.Millisecond,
				MaxConsecutiveFailures: 1,
				MinimumEmptyInterval:   10 * time.Millisecond,
			},
			NotificationEvery: 5 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() {
		runResult <- manager.Run(ctx)
	}()
	select {
	case <-repository.sent:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("notification was not dispatched")
	}
	cancel()
	select {
	case err := <-runResult:
		if err != context.Canceled {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop")
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.deliveryStatus != store.NotificationSent {
		t.Fatalf("delivery status = %q", repository.deliveryStatus)
	}
	if repository.botUsername != "fixture_bot" || repository.verifiedAt == "" {
		t.Fatalf("runtime status username = %q, verified = %q", repository.botUsername, repository.verifiedAt)
	}
	if repository.bound.LineID != "line-1" || repository.bound.Number != "+818012345678" {
		t.Fatalf("reply binding = %+v", repository.bound)
	}
}

type fakeSettings struct {
	changes chan struct{}
	units   []telegramsettings.Unit
	config  telegram.Config
}

func (s *fakeSettings) List(context.Context) ([]telegramsettings.Unit, error) {
	return append([]telegramsettings.Unit(nil), s.units...), nil
}

func (s *fakeSettings) RuntimeConfig(context.Context, string) (telegram.Config, error) {
	return s.config, nil
}

func (s *fakeSettings) Changes() <-chan struct{} {
	return s.changes
}

type fakeCommunications struct{}

func (fakeCommunications) Status(context.Context) (communication.Status, error) {
	return communication.Status{
		Connected: true,
		Lines: []store.LineSummary{{
			ID:    "line-1",
			State: "registered",
		}},
	}, nil
}

func (fakeCommunications) SendMessage(
	context.Context,
	communication.SendMessageInput,
) (store.Message, error) {
	return store.Message{}, nil
}

func (fakeCommunications) StartCall(
	context.Context,
	communication.StartCallInput,
) (store.Call, error) {
	return store.Call{}, nil
}

type fakeRepository struct {
	mu sync.Mutex

	offset         int64
	delivery       store.TelegramNotificationDelivery
	deliveryStatus string
	attemptToken   string
	sent           chan struct{}

	botUsername string
	verifiedAt  string
	lastError   string
	bound       store.TelegramReplyBinding
}

func (r *fakeRepository) Messages(context.Context, store.MessageQuery) ([]store.Message, error) {
	return []store.Message{}, nil
}

func (r *fakeRepository) TelegramNextOffset(context.Context, string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.offset, nil
}

func (r *fakeRepository) AdvanceTelegramOffset(_ context.Context, _ string, next int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offset = next
	return nil
}

func (r *fakeRepository) BindTelegramReply(
	_ context.Context,
	_, _, _ int64,
	binding store.TelegramReplyBinding,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bound = binding
	return nil
}

func (r *fakeRepository) ResolveTelegramReply(
	context.Context,
	int64,
	int64,
	int64,
) (store.TelegramReplyBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bound, nil
}

func (r *fakeRepository) UpdateTelegramRuntimeStatus(
	_ context.Context,
	_, botUsername, verifiedAt, lastError string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if botUsername != "" {
		r.botUsername = botUsername
	}
	if verifiedAt != "" {
		r.verifiedAt = verifiedAt
	}
	r.lastError = lastError
	return nil
}

func (r *fakeRepository) PendingTelegramNotificationDeliveries(
	context.Context,
	int,
) ([]store.TelegramNotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deliveryStatus != store.NotificationPending {
		return []store.TelegramNotificationDelivery{}, nil
	}
	return []store.TelegramNotificationDelivery{r.delivery}, nil
}

func (r *fakeRepository) MarkSendingTelegramNotificationsIndeterminate(context.Context) error {
	return nil
}

func (r *fakeRepository) ClaimTelegramNotificationDelivery(
	_ context.Context,
	_, _, attemptToken string,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deliveryStatus != store.NotificationPending {
		return false, nil
	}
	r.deliveryStatus = store.NotificationSending
	r.attemptToken = attemptToken
	return true, nil
}

func (r *fakeRepository) FinishTelegramNotificationDelivery(
	_ context.Context,
	_, _, attemptToken, status, _ string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.attemptToken == attemptToken {
		r.deliveryStatus = status
		close(r.sent)
	}
	return nil
}

type fakeBot struct{}

func (*fakeBot) GetMe(context.Context) (telegram.BotUser, error) {
	return telegram.BotUser{
		ID:       100001,
		IsBot:    true,
		Username: "fixture_bot",
	}, nil
}

func (*fakeBot) SendMessage(
	_ context.Context,
	request telegram.SendMessageRequest,
) (telegram.Message, error) {
	return telegram.Message{
		MessageID: 1,
		Chat:      telegram.Chat{ID: request.ChatID},
	}, nil
}

func (*fakeBot) GetUpdates(
	ctx context.Context,
	_ telegram.GetUpdatesRequest,
) ([]telegram.Update, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
