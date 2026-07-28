package telegramruntime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegram"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
)

func TestMergePersistedLineMetadataUsesStableLineID(t *testing.T) {
	t.Parallel()

	live := []store.LineSummary{{
		ID:         "line-phone",
		ICCID:      "iccid-new",
		DeviceIMEI: "imei-new",
	}}
	persisted := []store.LineSummary{
		{
			ID:          "line-phone",
			ICCID:       "iccid-old",
			LineLabel:   "Main",
			LineColor:   store.LineColorAmber,
			PhoneNumber: "+819012345678",
		},
		{
			ID:        "line-other",
			ICCID:     "iccid-new",
			LineLabel: "Wrong ICCID match",
			LineColor: store.LineColorRed,
		},
	}

	merged := mergePersistedLineMetadata(live, persisted)
	if len(merged) != 1 {
		t.Fatalf("merged lines = %+v", merged)
	}
	if merged[0].ID != "line-phone" ||
		merged[0].LineLabel != "Main" ||
		merged[0].LineColor != store.LineColorAmber ||
		merged[0].PhoneNumber != "+819012345678" {
		t.Fatalf("merged line = %+v", merged[0])
	}
}

func TestAdaptersRecentSMSRequestsChronologicalWindow(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{
		messages: []store.Message{{
			EndpointMessageID: "sms-1",
			LineID:            "line-1",
			Direction:         "incoming",
			Peer:              "+818012345678",
			Content:           "message",
			Timestamp:         "2026-07-24T08:30:00Z",
		}},
	}
	result, err := (adapters{repository: repository}).RecentSMS(
		context.Background(),
		telegram.SMSQuery{
			LineIDs:       []string{"line-1"},
			Limit:         5,
			Chronological: true,
		},
	)
	if err != nil {
		t.Fatalf("RecentSMS() error = %v", err)
	}
	if len(result) != 1 ||
		result[0].LineID != "line-1" ||
		result[0].Body != "message" ||
		result[0].ReceivedAt.IsZero() {
		t.Fatalf("RecentSMS() = %+v", result)
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(repository.messageQueries) != 1 {
		t.Fatalf("message queries = %+v", repository.messageQueries)
	}
	query := repository.messageQueries[0]
	if len(query.LineIDs) != 1 ||
		query.LineIDs[0] != "line-1" ||
		query.Limit != 5 ||
		!query.Chronological {
		t.Fatalf("message query = %+v", query)
	}
}

func TestAdaptersPublishMessageInvalidationAfterMarkingThreadRead(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	events := runtimeevents.NewBuffer(4)
	_, updates, cancel := events.SubscribeCurrent()
	defer cancel()

	err := (adapters{
		repository:    repository,
		runtimeEvents: events,
	}).MarkMessageThreadRead(context.Background(), "line-1", "+818012345678")
	if err != nil {
		t.Fatalf("MarkMessageThreadRead() error = %v", err)
	}

	repository.mu.Lock()
	markedLine := repository.markedLine
	markedPeer := repository.markedPeer
	repository.mu.Unlock()
	if markedLine != "line-1" || markedPeer != "+818012345678" {
		t.Fatalf("marked thread = %q %q", markedLine, markedPeer)
	}

	select {
	case event := <-updates:
		if len(event.Resources) != 1 ||
			event.Resources[0] != runtimeevents.ResourceMessages {
			t.Fatalf("runtime event = %+v", event)
		}
	default:
		t.Fatal("message invalidation event was not published")
	}
}

func TestAdaptersDoNotPublishMessageInvalidationWhenMarkReadFails(t *testing.T) {
	t.Parallel()

	markErr := errors.New("database unavailable")
	repository := &fakeRepository{markReadError: markErr}
	events := runtimeevents.NewBuffer(4)
	_, updates, cancel := events.SubscribeCurrent()
	defer cancel()

	err := (adapters{
		repository:    repository,
		runtimeEvents: events,
	}).MarkMessageThreadRead(context.Background(), "line-1", "+818012345678")
	if !errors.Is(err, markErr) {
		t.Fatalf("MarkMessageThreadRead() error = %v, want %v", err, markErr)
	}
	select {
	case event := <-updates:
		t.Fatalf("unexpected runtime event = %+v", event)
	default:
	}
}

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
	commands, messages := bot.snapshots()
	if len(commands) != 1 || len(commands[0]) != 5 ||
		commands[0][0].Command != "list" || commands[0][4].Command != "help" {
		t.Fatalf("registered commands = %+v", commands)
	}
	if len(messages) != 1 ||
		!strings.Contains(messages[0].Text, "主线路 · +818000000001") ||
		strings.Contains(messages[0].Text, "line-1") {
		t.Fatalf("notification text = %q", messages[0].Text)
	}
}

func TestManagerRegistersBotCommandsOnStartupAndReload(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		changes: make(chan struct{}, 1),
		units: []telegramsettings.Unit{{
			ID:      "unit-1",
			Enabled: true,
		}},
		config: telegram.Config{
			Enabled:  true,
			BotToken: "100001:abcdefghijklmnopqrstuvwxyz",
			ChatID:   10,
			AdminID:  20,
		},
	}
	repository := &fakeRepository{}
	bot := &fakeBot{configured: make(chan struct{}, 2)}
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
	waitForBotConfiguration(t, bot.configured)
	settings.changes <- struct{}{}
	waitForBotConfiguration(t, bot.configured)
	cancel()
	select {
	case err := <-runResult:
		if err != context.Canceled {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop")
	}

	commands, _ := bot.snapshots()
	if len(commands) != 2 {
		t.Fatalf("command registrations = %d, want 2", len(commands))
	}
	for _, registration := range commands {
		if len(registration) != 5 ||
			registration[0].Command != "list" ||
			registration[1].Command != "sms" ||
			registration[2].Command != "call" ||
			registration[2].Description != "查看最近通话" ||
			registration[3].Command != "reply" ||
			registration[4].Command != "help" {
			t.Fatalf("registered commands = %+v", registration)
		}
	}
}

func waitForBotConfiguration(t *testing.T, configured <-chan struct{}) {
	t.Helper()
	select {
	case <-configured:
	case <-time.After(time.Second):
		t.Fatal("bot command menu was not configured")
	}
}

func TestAdaptersExposeHumanLineMetadataAndRecentCalls(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{
		calls: []store.Call{{
			ID:             "call-1",
			LineID:         "line-1",
			EndpointLineID: "legacy-modem-id",
			LocalPhone:     "+81 80-0000-0001",
			LineIMSI:       "legacy-imsi",
			LineICCID:      "legacy-iccid",
			Direction:      "incoming",
			RemoteNumber:   "+818012345678",
			ContactName:    "Aiko Tanaka",
			EndedAt:        "2026-07-24T08:30:00Z",
			Missed:         true,
		}},
		recordings: []store.RecordingEntry{{
			Call:     store.RecordingCall{ID: "call-1"},
			Playable: true,
		}},
	}
	adapter := adapters{
		communications: fakeCommunications{},
		repository:     repository,
	}

	lines, err := adapter.Lines(context.Background())
	if err != nil {
		t.Fatalf("Lines() error = %v", err)
	}
	if len(lines) != 1 ||
		lines[0].ID != "line-1" ||
		lines[0].Label != "主线路" ||
		lines[0].PhoneNumber != "+818000000001" {
		t.Fatalf("Lines() = %+v", lines)
	}

	calls, err := adapter.RecentCalls(context.Background(), telegram.CallQuery{
		LineIDs: []string{"line-1"},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("RecentCalls() error = %v", err)
	}
	if len(calls) != 1 ||
		calls[0].LineID != "line-1" ||
		calls[0].ContactName != "Aiko Tanaka" ||
		!calls[0].Missed ||
		!calls[0].HasRecording ||
		calls[0].OccurredAt.Format(time.RFC3339) != "2026-07-24T08:30:00Z" {
		t.Fatalf("RecentCalls() = %+v", calls)
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
			ID:          "line-1",
			ICCID:       "iccid-1",
			Model:       "QDC507",
			PhoneNumber: "+818000000001",
			State:       "registered",
		}},
	}, nil
}

func (fakeCommunications) SendMessage(
	context.Context,
	communication.SendMessageInput,
) (store.Message, error) {
	return store.Message{}, nil
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
	calls       []store.Call
	recordings  []store.RecordingEntry
	messages    []store.Message

	messageQueries []store.MessageQuery
	markedLine     string
	markedPeer     string
	markReadError  error
}

func (r *fakeRepository) Lines(context.Context) ([]store.LineSummary, error) {
	return []store.LineSummary{{
		ID:          "line-1",
		ICCID:       "iccid-1",
		LineLabel:   "主线路",
		PhoneNumber: "+818000000001",
	}}, nil
}

func (r *fakeRepository) Messages(_ context.Context, query store.MessageQuery) ([]store.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messageQueries = append(r.messageQueries, query)
	return append([]store.Message(nil), r.messages...), nil
}

func (r *fakeRepository) Calls(context.Context, store.CallQuery) ([]store.Call, error) {
	return append([]store.Call(nil), r.calls...), nil
}

func (r *fakeRepository) RecordingEntries(
	context.Context,
	store.RecordingQuery,
) ([]store.RecordingEntry, error) {
	return append([]store.RecordingEntry(nil), r.recordings...), nil
}

func (r *fakeRepository) MarkMessageThreadReadByLine(
	_ context.Context,
	lineID, peer string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markedLine = lineID
	r.markedPeer = peer
	return r.markReadError
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

type fakeBot struct {
	mu         sync.Mutex
	commands   [][]telegram.BotCommand
	messages   []telegram.SendMessageRequest
	configured chan struct{}
}

func (*fakeBot) GetMe(context.Context) (telegram.BotUser, error) {
	return telegram.BotUser{
		ID:       100001,
		IsBot:    true,
		Username: "fixture_bot",
	}, nil
}

func (b *fakeBot) SetMyCommands(
	_ context.Context,
	commands []telegram.BotCommand,
) error {
	copied := append([]telegram.BotCommand(nil), commands...)
	b.mu.Lock()
	b.commands = append(b.commands, copied)
	configured := b.configured
	b.mu.Unlock()
	if configured != nil {
		select {
		case configured <- struct{}{}:
		default:
		}
	}
	return nil
}

func (b *fakeBot) SendMessage(
	_ context.Context,
	request telegram.SendMessageRequest,
) (telegram.Message, error) {
	b.mu.Lock()
	b.messages = append(b.messages, request)
	b.mu.Unlock()
	return telegram.Message{
		MessageID: 1,
		Chat:      telegram.Chat{ID: request.ChatID},
	}, nil
}

func (*fakeBot) AnswerCallbackQuery(
	context.Context,
	telegram.AnswerCallbackQueryRequest,
) error {
	return nil
}

func (*fakeBot) EditMessageReplyMarkup(
	context.Context,
	telegram.EditMessageReplyMarkupRequest,
) error {
	return nil
}

func (*fakeBot) GetUpdates(
	ctx context.Context,
	_ telegram.GetUpdatesRequest,
) ([]telegram.Update, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *fakeBot) snapshots() ([][]telegram.BotCommand, []telegram.SendMessageRequest) {
	b.mu.Lock()
	defer b.mu.Unlock()
	commands := make([][]telegram.BotCommand, len(b.commands))
	for index := range b.commands {
		commands[index] = append([]telegram.BotCommand(nil), b.commands[index]...)
	}
	return commands, append([]telegram.SendMessageRequest(nil), b.messages...)
}
