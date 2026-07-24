package telegram

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewServiceRequiresEnabledDependencies(t *testing.T) {
	t.Parallel()

	if _, err := NewService(Config{}, Dependencies{}); err != nil {
		t.Fatalf("disabled NewService() error = %v", err)
	}

	config := validServiceConfig()
	dependencies := completeDependencies()
	tests := []struct {
		name   string
		mutate func(*Dependencies)
		field  string
	}{
		{name: "bot", mutate: func(d *Dependencies) { d.Bot = nil }, field: "bot"},
		{name: "lines", mutate: func(d *Dependencies) { d.Lines = nil }, field: "lines"},
		{name: "sms", mutate: func(d *Dependencies) { d.SMS = nil }, field: "sms"},
		{name: "sender", mutate: func(d *Dependencies) { d.SMSSender = nil }, field: "sms_sender"},
		{name: "dialer", mutate: func(d *Dependencies) { d.Dialer = nil }, field: "dialer"},
		{name: "replies", mutate: func(d *Dependencies) { d.Replies = nil }, field: "replies"},
		{name: "read marker", mutate: func(d *Dependencies) { d.Read = nil }, field: "read_marker"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			copy := dependencies
			test.mutate(&copy)
			_, err := NewService(config, copy)
			var configErr *ConfigError
			if !errors.As(err, &configErr) || configErr.Field != test.field {
				t.Fatalf("error = %v, want ConfigError field %q", err, test.field)
			}
		})
	}
}

func TestServiceVerifyBot(t *testing.T) {
	t.Parallel()

	service := mustService(t, validServiceConfig(), completeDependencies())
	user, err := service.VerifyBot(context.Background())
	if err != nil {
		t.Fatalf("VerifyBot() error = %v", err)
	}
	if !user.IsBot || user.ID != testBotID {
		t.Fatalf("VerifyBot() = %#v", user)
	}

	dependencies := completeDependencies()
	dependencies.Bot = botStub{getMe: func(context.Context) (BotUser, error) {
		return BotUser{}, &APIError{Code: 401, Description: "Unauthorized"}
	}}
	service = mustService(t, validServiceConfig(), dependencies)
	_, err = service.VerifyBot(context.Background())
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Operation != "verify_bot" || operationErr.Kind != "telegram_api" {
		t.Fatalf("VerifyBot() error = %#v", err)
	}
}

func TestServiceInitializeBotConfiguresCanonicalCommands(t *testing.T) {
	t.Parallel()

	var registrations [][]BotCommand
	dependencies := completeDependencies()
	dependencies.Bot = botStub{setCommands: func(_ context.Context, commands []BotCommand) error {
		registrations = append(registrations, append([]BotCommand(nil), commands...))
		return nil
	}}
	service := mustService(t, validServiceConfig(), dependencies)
	for range 2 {
		user, err := service.InitializeBot(context.Background())
		if err != nil {
			t.Fatalf("InitializeBot() error = %v", err)
		}
		if user.ID != testBotID {
			t.Fatalf("InitializeBot() user = %#v", user)
		}
	}
	want := botCommands()
	if len(registrations) != 2 ||
		!reflect.DeepEqual(registrations[0], want) ||
		!reflect.DeepEqual(registrations[1], want) {
		t.Fatalf("command registrations = %#v, want %#v twice", registrations, want)
	}

	dependencies = completeDependencies()
	dependencies.Bot = botStub{setCommands: func(context.Context, []BotCommand) error {
		return &APIError{Code: 401, Description: "Unauthorized"}
	}}
	service = mustService(t, validServiceConfig(), dependencies)
	_, err := service.InitializeBot(context.Background())
	var operationErr *OperationError
	if !errors.As(err, &operationErr) ||
		operationErr.Operation != "configure_bot_commands" ||
		operationErr.Kind != "telegram_api" {
		t.Fatalf("InitializeBot() error = %#v", err)
	}
}

func TestServiceHonorsAddressedBotUsername(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	dependencies := completeDependencies()
	dependencies.Bot = botStub{
		getMe: func(context.Context) (BotUser, error) {
			return BotUser{ID: testBotID, IsBot: true, Username: "Deck_Bot"}, nil
		},
		send: func(_ context.Context, request SendMessageRequest) (Message, error) {
			sent = append(sent, request)
			return Message{MessageID: int64(len(sent)), Chat: Chat{ID: request.ChatID}}, nil
		},
	}
	service := mustService(t, validServiceConfig(), dependencies)
	if _, err := service.VerifyBot(context.Background()); err != nil {
		t.Fatalf("VerifyBot() error = %v", err)
	}
	if err := service.HandleUpdate(context.Background(), commandUpdate(8, -100, 42, "/help@OtherBot")); err != nil {
		t.Fatalf("other target error = %v", err)
	}
	if len(sent) != 0 {
		t.Fatal("command addressed to another bot was handled")
	}
	if err := service.HandleUpdate(context.Background(), commandUpdate(9, -100, 42, "/help@deck_bot")); err != nil {
		t.Fatalf("matching target error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("matching command responses = %d", len(sent))
	}
}

func TestServiceRunVerifiesBotBeforePolling(t *testing.T) {
	t.Parallel()

	t.Run("verification failure", func(t *testing.T) {
		var polled bool
		dependencies := completeDependencies()
		dependencies.Bot = botStub{
			getMe: func(context.Context) (BotUser, error) {
				return BotUser{}, &APIError{Code: 401}
			},
			updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
				polled = true
				return nil, nil
			},
		}
		service := mustService(t, validServiceConfig(), dependencies)
		err := service.Run(context.Background(), &checkpointRecorder{}, PollOptions{})
		if err == nil || polled {
			t.Fatalf("Run() error=%v polled=%v", err, polled)
		}
	})

	t.Run("verified polling", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		dependencies := completeDependencies()
		dependencies.Bot = botStub{
			getMe: func(context.Context) (BotUser, error) {
				return BotUser{ID: testBotID, IsBot: true, Username: "deck_bot"}, nil
			},
			updates: func(context.Context, GetUpdatesRequest) ([]Update, error) {
				cancel()
				return nil, context.Canceled
			},
		}
		service := mustService(t, validServiceConfig(), dependencies)
		if err := service.Run(ctx, &checkpointRecorder{}, PollOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		service := mustService(t, Config{}, Dependencies{})
		if err := service.Run(context.Background(), nil, PollOptions{}); err != nil {
			t.Fatalf("disabled Run() error = %v", err)
		}
	})
}

func TestServiceRejectsUnauthorizedUpdatesWithoutSideEffects(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	var calls []CallRequest
	recorder := &eventRecorder{}
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: 1, Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.Dialer = dialerFunc(func(_ context.Context, request CallRequest) error {
		calls = append(calls, request)
		return nil
	})
	dependencies.Observer = recorder
	service := mustService(t, validServiceConfig(), dependencies)

	updates := []Update{
		commandUpdate(10, -999, 42, "/call line-a +818012345678"),
		commandUpdate(11, -100, 99, "/call line-a +818012345678"),
		{UpdateID: 12, Message: &Message{MessageID: 12, Chat: Chat{ID: -100}, Text: "/call line-a +818012345678"}},
	}
	for _, update := range updates {
		if err := service.HandleUpdate(context.Background(), update); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}
	if len(sent) != 0 || len(calls) != 0 {
		t.Fatalf("unauthorized side effects: sent=%d calls=%d", len(sent), len(calls))
	}
	events := recorder.snapshot()
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	for _, event := range events {
		if event.Kind != EventUnauthorizedUpdate || event.Operation != "authorize" {
			t.Fatalf("event = %#v", event)
		}
	}
}

func TestServiceLinesAndSMSRespectScopes(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	var queries []SMSQuery
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: int64(len(sent)), Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.Lines = lineQuerierFunc(func(context.Context) ([]Line, error) {
		return []Line{
			{ID: "line-b", Label: "Hidden", PhoneNumber: "+14155550123", Available: true},
			{ID: "line-a", Label: "Primary", PhoneNumber: "+818012345678", Available: true},
			{ID: "line-a", Label: "Duplicate", Available: true},
			{ID: "bad/line", Label: "Invalid", Available: true},
		}, nil
	})
	dependencies.SMS = smsQuerierFunc(func(_ context.Context, query SMSQuery) ([]SMS, error) {
		queries = append(queries, query)
		return []SMS{
			{ID: "1", LineID: "line-a", Direction: "incoming", Peer: "+818011111111", Body: "allowed body"},
			{ID: "2", LineID: "line-b", Direction: "incoming", Peer: "+14155550123", Body: "hidden body"},
		}, nil
	})
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.HandleUpdate(context.Background(), commandUpdate(20, -100, 42, "/lines")); err != nil {
		t.Fatalf("/lines error = %v", err)
	}
	if len(sent) != 1 ||
		!strings.Contains(sent[0].Text, "Primary") ||
		strings.Contains(sent[0].Text, "Hidden") ||
		strings.Contains(sent[0].Text, "bad/line") {
		t.Fatalf("/lines response = %#v", sent)
	}

	if err := service.HandleUpdate(context.Background(), commandUpdate(21, -100, 42, "/sms")); err != nil {
		t.Fatalf("/sms error = %v", err)
	}
	if len(queries) != 1 || !reflect.DeepEqual(queries[0], SMSQuery{LineIDs: []string{"line-a"}, Limit: 10}) {
		t.Fatalf("queries = %#v", queries)
	}
	if !strings.Contains(sent[1].Text, "allowed body") || strings.Contains(sent[1].Text, "hidden body") {
		t.Fatalf("/sms response = %q", sent[1].Text)
	}

	if err := service.HandleUpdate(context.Background(), commandUpdate(22, -100, 42, "/sms line-b 5")); err != nil {
		t.Fatalf("/sms line-b error = %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("out-of-scope query reached dependency: %#v", queries)
	}
	if !strings.Contains(sent[2].Text, "不在此 Bot") {
		t.Fatalf("out-of-scope response = %q", sent[2].Text)
	}
}

func TestServiceCallAndSMSCommands(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	var calls []CallRequest
	var smsRequests []SMSRequest
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: int64(len(sent)), Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.SMSSender = smsSenderFunc(func(_ context.Context, request SMSRequest) error {
		smsRequests = append(smsRequests, request)
		return nil
	})
	dependencies.Dialer = dialerFunc(func(_ context.Context, request CallRequest) error {
		calls = append(calls, request)
		return nil
	})
	dependencies.Replies = replyStoreStub{resolve: func(_ context.Context, botID, chatID, messageID int64) (ReplyBinding, error) {
		if botID != testBotID || chatID != -100 || messageID != 700 {
			t.Fatalf("Resolve(%d, %d, %d)", botID, chatID, messageID)
		}
		return ReplyBinding{LineID: "line-a", Number: "+81 80-1234-5678"}, nil
	}}
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.HandleUpdate(context.Background(), commandUpdate(30, -100, 42, "/call line-a +81-80-1234-5678")); err != nil {
		t.Fatalf("/call error = %v", err)
	}
	if !reflect.DeepEqual(calls, []CallRequest{{
		RequestID: "telegram:123456789:30:call",
		LineID:    "line-a",
		To:        "+818012345678",
	}}) {
		t.Fatalf("calls = %#v", calls)
	}

	if err := service.HandleUpdate(context.Background(), commandUpdate(31, -100, 42, "/reply line-a +81-80-1234-5678 hello there")); err != nil {
		t.Fatalf("/reply error = %v", err)
	}
	if !reflect.DeepEqual(smsRequests[0], SMSRequest{
		RequestID: "telegram:123456789:31:sms",
		LineID:    "line-a",
		To:        "+818012345678",
		Body:      "hello there",
	}) {
		t.Fatalf("explicit SMS request = %#v", smsRequests[0])
	}

	direct := commandUpdate(32, -100, 42, "  direct response  ")
	direct.Message.ReplyToMessage = &Message{MessageID: 700, Chat: Chat{ID: -100}}
	if err := service.HandleUpdate(context.Background(), direct); err != nil {
		t.Fatalf("direct reply error = %v", err)
	}
	if !reflect.DeepEqual(smsRequests[1], SMSRequest{
		RequestID: "telegram:123456789:32:sms",
		LineID:    "line-a",
		To:        "+818012345678",
		Body:      "direct response",
	}) {
		t.Fatalf("direct SMS request = %#v", smsRequests[1])
	}

	for _, response := range sent {
		if response.ChatID != -100 || response.ReplyToMessageID == 0 {
			t.Fatalf("response routing = %#v", response)
		}
	}
}

func TestServiceRejectsUnavailableActionLine(t *testing.T) {
	t.Parallel()

	var calls int
	var sends int
	dependencies := completeDependencies()
	dependencies.Lines = lineQuerierFunc(func(context.Context) ([]Line, error) {
		return []Line{{ID: "line-a", Available: false}}, nil
	})
	dependencies.Dialer = dialerFunc(func(context.Context, CallRequest) error {
		calls++
		return nil
	})
	dependencies.SMSSender = smsSenderFunc(func(context.Context, SMSRequest) error {
		sends++
		return nil
	})
	service := mustService(t, validServiceConfig(), dependencies)

	_ = service.HandleUpdate(context.Background(), commandUpdate(40, -100, 42, "/call line-a +818012345678"))
	_ = service.HandleUpdate(context.Background(), commandUpdate(41, -100, 42, "/reply line-a +818012345678 body"))
	if calls != 0 || sends != 0 {
		t.Fatalf("unavailable line caused actions: calls=%d sends=%d", calls, sends)
	}
}

func TestServiceNotificationsAndReplyBinding(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	var bindings []ReplyBinding
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: int64(500 + len(sent)), Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.Replies = replyStoreStub{bind: func(_ context.Context, botID, chatID, messageID int64, binding ReplyBinding) error {
		if botID != testBotID || chatID != -100 || messageID != 501 {
			t.Fatalf("Bind(%d, %d, %d)", botID, chatID, messageID)
		}
		bindings = append(bindings, binding)
		return nil
	}}
	service := mustService(t, validServiceConfig(), dependencies)

	err := service.NotifyIncomingSMS(context.Background(), IncomingSMS{
		MessageID:  "sms-1",
		LineID:     "line-a",
		LineLabel:  "Primary",
		From:       "+81 80-1234-5678",
		Body:       "notification body",
		ReceivedAt: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NotifyIncomingSMS() error = %v", err)
	}
	if len(sent) != 1 ||
		!strings.Contains(sent[0].Text, "notification body") ||
		!strings.Contains(sent[0].Text, "+818012345678") ||
		!strings.Contains(sent[0].Text, "线路：Primary · +818012345678") ||
		strings.Contains(sent[0].Text, "line-a") {
		t.Fatalf("incoming notification = %#v", sent)
	}
	if !reflect.DeepEqual(bindings, []ReplyBinding{{LineID: "line-a", Number: "+81 80-1234-5678"}}) {
		t.Fatalf("bindings = %#v", bindings)
	}
	if sent[0].ReplyMarkup == nil ||
		len(sent[0].ReplyMarkup.InlineKeyboard) != 1 ||
		len(sent[0].ReplyMarkup.InlineKeyboard[0]) != 1 ||
		sent[0].ReplyMarkup.InlineKeyboard[0][0].CallbackData != markReadCallbackData {
		t.Fatalf("incoming notification markup = %#v", sent[0].ReplyMarkup)
	}

	if err := service.NotifyIncomingSMS(context.Background(), IncomingSMS{
		LineID: "line-b",
		From:   "+14155550123",
		Body:   "must stay hidden",
	}); err != nil {
		t.Fatalf("out-of-scope NotifyIncomingSMS() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatal("out-of-scope notification was sent")
	}

	if err := service.NotifyMissedCall(context.Background(), MissedCall{
		CallID:    "call-1",
		LineID:    "line-a",
		LineLabel: "Primary",
		From:      "+14155550123",
		CalledAt:  time.Date(2026, 7, 23, 12, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("NotifyMissedCall() error = %v", err)
	}
	if len(sent) != 2 ||
		!strings.Contains(sent[1].Text, "未接来电") ||
		!strings.Contains(sent[1].Text, "线路：Primary · +818012345678") ||
		strings.Contains(sent[1].Text, "line-a") {
		t.Fatalf("missed-call notification = %#v", sent)
	}
}

func TestServiceNotificationsNeverExposeInternalLineID(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: int64(len(sent)), Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.Lines = lineQuerierFunc(func(context.Context) ([]Line, error) {
		return nil, errors.New("line snapshot unavailable")
	})
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.NotifyIncomingSMS(context.Background(), IncomingSMS{
		MessageID: "sms-1",
		LineID:    "line-a",
		From:      "+818012345678",
		Body:      "fallback identity",
	}); err != nil {
		t.Fatalf("NotifyIncomingSMS() error = %v", err)
	}
	if err := service.NotifyMissedCall(context.Background(), MissedCall{
		CallID: "call-1",
		LineID: "line-a",
		From:   "+818012345678",
	}); err != nil {
		t.Fatalf("NotifyMissedCall() error = %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("notifications = %d, want 2", len(sent))
	}
	for _, request := range sent {
		if !strings.Contains(request.Text, "线路：线路") ||
			strings.Contains(request.Text, "line-a") {
			t.Fatalf("notification text = %q", request.Text)
		}
	}
}

func TestServiceMarksTelegramNotificationRead(t *testing.T) {
	t.Parallel()

	var markedLine, markedPeer string
	var answered AnswerCallbackQueryRequest
	var edited EditMessageReplyMarkupRequest
	dependencies := completeDependencies()
	dependencies.Replies = replyStoreStub{
		resolve: func(_ context.Context, botID, chatID, messageID int64) (ReplyBinding, error) {
			if botID != testBotID || chatID != -100 || messageID != 501 {
				t.Fatalf("Resolve(%d, %d, %d)", botID, chatID, messageID)
			}
			return ReplyBinding{LineID: "line-a", Number: "BANK ALERT"}, nil
		},
	}
	dependencies.Read = messageReadMarkerFunc(func(_ context.Context, lineID, peer string) error {
		markedLine = lineID
		markedPeer = peer
		return nil
	})
	dependencies.Bot = botStub{
		answer: func(_ context.Context, request AnswerCallbackQueryRequest) error {
			answered = request
			return nil
		},
		editMarkup: func(_ context.Context, request EditMessageReplyMarkupRequest) error {
			edited = request
			return nil
		},
	}
	service := mustService(t, validServiceConfig(), dependencies)

	err := service.HandleUpdate(context.Background(), Update{
		UpdateID: 70,
		CallbackQuery: &CallbackQuery{
			ID:   "callback-1",
			From: BotUser{ID: 42},
			Message: &Message{
				MessageID: 501,
				Chat:      Chat{ID: -100},
			},
			Data: markReadCallbackData,
		},
	})
	if err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if markedLine != "line-a" || markedPeer != "BANK ALERT" {
		t.Fatalf("marked thread = %q %q", markedLine, markedPeer)
	}
	if answered.CallbackQueryID != "callback-1" || answered.Text != "已标记已读" {
		t.Fatalf("callback answer = %+v", answered)
	}
	if edited.ChatID != -100 || edited.MessageID != 501 ||
		len(edited.ReplyMarkup.InlineKeyboard) != 0 {
		t.Fatalf("edited markup = %+v", edited)
	}
}

func TestServiceNotificationSwitches(t *testing.T) {
	t.Parallel()

	var sent int
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent++
		return Message{MessageID: int64(sent), Chat: Chat{ID: request.ChatID}}, nil
	}}
	config := validServiceConfig()
	config.Notifications = NotificationConfig{}
	service := mustService(t, config, dependencies)

	if err := service.NotifyIncomingSMS(context.Background(), IncomingSMS{LineID: "line-a", From: "+818012345678", Body: "hidden"}); err != nil {
		t.Fatalf("NotifyIncomingSMS() error = %v", err)
	}
	if err := service.NotifyMissedCall(context.Background(), MissedCall{LineID: "line-a", From: "+818012345678"}); err != nil {
		t.Fatalf("NotifyMissedCall() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("notifications disabled but sent = %d", sent)
	}
}

func TestServiceFailureBoundaryDoesNotExposeSensitiveData(t *testing.T) {
	t.Parallel()

	const dependencySecret = "token=secret phone=+818012345678 body=private"
	var sent []SendMessageRequest
	recorder := &eventRecorder{}
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: 1, Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.Lines = lineQuerierFunc(func(context.Context) ([]Line, error) {
		return nil, errors.New(dependencySecret)
	})
	dependencies.Observer = recorder
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.HandleUpdate(context.Background(), commandUpdate(50, -100, 42, "/lines")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if len(sent) != 1 || strings.Contains(sent[0].Text, dependencySecret) || !strings.Contains(sent[0].Text, "操作失败") {
		t.Fatalf("response = %#v", sent)
	}
	events := recorder.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if serialized := fmt.Sprintf("%+v", events[0]); strings.Contains(serialized, "secret") ||
		strings.Contains(serialized, "+818012345678") ||
		strings.Contains(serialized, "private") {
		t.Fatalf("event leaks sensitive data: %s", serialized)
	}
}

func TestServiceHelpExcludesTelegramVoiceCall(t *testing.T) {
	t.Parallel()

	var responseText string
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		responseText = request.Text
		return Message{MessageID: 1, Chat: Chat{ID: request.ChatID}}, nil
	}}
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.HandleUpdate(context.Background(), commandUpdate(60, -100, 42, "/help")); err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !strings.Contains(responseText, "/call") || !strings.Contains(responseText, "不会发起 Telegram 语音通话") {
		t.Fatalf("help text = %q", responseText)
	}
}

func TestServiceHandlesInvalidAndUnboundReplies(t *testing.T) {
	t.Parallel()

	var responses []SendMessageRequest
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		responses = append(responses, request)
		return Message{MessageID: int64(len(responses)), Chat: Chat{ID: request.ChatID}}, nil
	}}
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.HandleUpdate(context.Background(), Update{}); err != nil {
		t.Fatalf("empty update error = %v", err)
	}
	if err := service.HandleUpdate(context.Background(), commandUpdate(70, -100, 42, "   ")); err != nil {
		t.Fatalf("empty text error = %v", err)
	}
	if err := service.HandleUpdate(context.Background(), commandUpdate(71, -100, 42, "/unknown")); err != nil {
		t.Fatalf("unknown command error = %v", err)
	}
	reply := commandUpdate(72, -100, 42, "reply body")
	reply.Message.ReplyToMessage = &Message{MessageID: 999}
	if err := service.HandleUpdate(context.Background(), reply); err != nil {
		t.Fatalf("unbound reply error = %v", err)
	}

	if len(responses) != 2 ||
		!strings.Contains(responses[0].Text, "/help") ||
		!strings.Contains(responses[1].Text, "没有可用") {
		t.Fatalf("responses = %#v", responses)
	}
}

func TestServiceReportsSendResponseFailureSafely(t *testing.T) {
	t.Parallel()

	const secret = "bot-secret-in-transport"
	recorder := &eventRecorder{}
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(context.Context, SendMessageRequest) (Message, error) {
		return Message{}, errors.New(secret)
	}}
	dependencies.Observer = recorder
	service := mustService(t, validServiceConfig(), dependencies)

	err := service.HandleUpdate(context.Background(), commandUpdate(80, -100, 42, "/help"))
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Operation != "send_response" {
		t.Fatalf("error = %#v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks dependency text: %v", err)
	}
	events := recorder.snapshot()
	if len(events) != 1 || events[0].Operation != "send_response" || events[0].ErrorClass != "dependency" {
		t.Fatalf("events = %#v", events)
	}
}

func TestServiceNotificationFailuresAreTypedAndSafe(t *testing.T) {
	t.Parallel()

	t.Run("invalid inputs", func(t *testing.T) {
		service := mustService(t, validServiceConfig(), completeDependencies())
		allLinesConfig := validServiceConfig()
		allLinesConfig.LineScopes = nil
		allLinesService := mustService(t, allLinesConfig, completeDependencies())
		cases := []struct {
			name string
			call func() error
			kind string
		}{
			{
				name: "incoming line",
				call: func() error {
					return allLinesService.NotifyIncomingSMS(context.Background(), IncomingSMS{
						LineID: "bad/line",
						From:   "+818012345678",
						Body:   "body",
					})
				},
				kind: "invalid_line",
			},
			{
				name: "incoming body",
				call: func() error {
					return service.NotifyIncomingSMS(context.Background(), IncomingSMS{
						LineID: "line-a",
						From:   "+818012345678",
					})
				},
				kind: "invalid_body",
			},
		}
		for _, test := range cases {
			err := test.call()
			var operationErr *OperationError
			if !errors.As(err, &operationErr) || operationErr.Kind != test.kind {
				t.Errorf("%s error = %#v", test.name, err)
			}
		}
	})

	t.Run("binding failure after notification", func(t *testing.T) {
		dependencies := completeDependencies()
		dependencies.Replies = replyStoreStub{bind: func(context.Context, int64, int64, int64, ReplyBinding) error {
			return errors.New("database secret")
		}}
		service := mustService(t, validServiceConfig(), dependencies)
		err := service.NotifyIncomingSMS(context.Background(), IncomingSMS{
			LineID: "line-a",
			From:   "+818012345678",
			Body:   "body",
		})
		var operationErr *OperationError
		if !errors.As(err, &operationErr) || operationErr.Operation != "bind_sms_reply" {
			t.Fatalf("error = %#v", err)
		}
		if strings.Contains(err.Error(), "database secret") {
			t.Fatalf("error leaks dependency: %v", err)
		}
	})

	t.Run("bot API failure", func(t *testing.T) {
		dependencies := completeDependencies()
		dependencies.Bot = botStub{send: func(context.Context, SendMessageRequest) (Message, error) {
			return Message{}, &APIError{Code: 500, Description: "remote detail"}
		}}
		service := mustService(t, validServiceConfig(), dependencies)
		err := service.NotifyMissedCall(context.Background(), MissedCall{
			LineID: "line-a",
			From:   "+818012345678",
		})
		var operationErr *OperationError
		if !errors.As(err, &operationErr) ||
			operationErr.Operation != "notify_missed_call" ||
			operationErr.Kind != "telegram_api" {
			t.Fatalf("error = %#v", err)
		}
	})
}

func TestServiceNotifiesNonE164PeersWithReadBinding(t *testing.T) {
	t.Parallel()

	var sent []SendMessageRequest
	var binds int
	dependencies := completeDependencies()
	dependencies.Bot = botStub{send: func(_ context.Context, request SendMessageRequest) (Message, error) {
		sent = append(sent, request)
		return Message{MessageID: int64(len(sent)), Chat: Chat{ID: request.ChatID}}, nil
	}}
	dependencies.Replies = replyStoreStub{bind: func(context.Context, int64, int64, int64, ReplyBinding) error {
		binds++
		return nil
	}}
	service := mustService(t, validServiceConfig(), dependencies)

	if err := service.NotifyIncomingSMS(context.Background(), IncomingSMS{
		LineID: "line-a",
		From:   "BANK\nALERT",
		Body:   "one-time notice",
	}); err != nil {
		t.Fatalf("NotifyIncomingSMS() error = %v", err)
	}
	if err := service.NotifyMissedCall(context.Background(), MissedCall{
		LineID: "line-a",
		From:   "",
	}); err != nil {
		t.Fatalf("NotifyMissedCall() error = %v", err)
	}
	if binds != 1 {
		t.Fatalf("non-E.164 notification read bindings = %d", binds)
	}
	if len(sent) != 2 ||
		!strings.Contains(sent[0].Text, "BANK ALERT") ||
		!strings.Contains(sent[0].Text, "不能直接回复") ||
		!strings.Contains(sent[1].Text, "未知号码") {
		t.Fatalf("notifications = %#v", sent)
	}
}

func TestServiceCopiesLineScopes(t *testing.T) {
	t.Parallel()

	config := validServiceConfig()
	service := mustService(t, config, completeDependencies())
	config.LineScopes[0] = "line-b"
	if !service.config.AllowsLine("line-a") || service.config.AllowsLine("line-b") {
		t.Fatalf("service config changed with caller slice: %#v", service.config.LineScopes)
	}
}

func validServiceConfig() Config {
	return Config{
		Enabled:    true,
		BotToken:   testBotToken,
		ChatID:     -100,
		AdminID:    42,
		LineScopes: []string{"line-a"},
		Notifications: NotificationConfig{
			IncomingSMS: true,
			MissedCalls: true,
		},
	}
}

func completeDependencies() Dependencies {
	return Dependencies{
		Bot: botStub{},
		Lines: lineQuerierFunc(func(context.Context) ([]Line, error) {
			return []Line{
				{ID: "line-a", Label: "Primary", PhoneNumber: "+818012345678", Available: true},
				{ID: "line-b", Label: "Secondary", PhoneNumber: "+14155550123", Available: true},
			}, nil
		}),
		SMS: smsQuerierFunc(func(context.Context, SMSQuery) ([]SMS, error) {
			return nil, nil
		}),
		SMSSender: smsSenderFunc(func(context.Context, SMSRequest) error {
			return nil
		}),
		Dialer: dialerFunc(func(context.Context, CallRequest) error {
			return nil
		}),
		Replies: replyStoreStub{},
		Read: messageReadMarkerFunc(func(context.Context, string, string) error {
			return nil
		}),
	}
}

func mustService(t *testing.T, config Config, dependencies Dependencies) *Service {
	t.Helper()
	service, err := NewService(config, dependencies)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func commandUpdate(updateID, chatID, adminID int64, text string) Update {
	return Update{
		UpdateID: updateID,
		Message: &Message{
			MessageID: updateID + 1000,
			From:      &BotUser{ID: adminID},
			Chat:      Chat{ID: chatID},
			Text:      text,
		},
	}
}
