package telegram

import (
	"context"
	"sync"
)

const testBotToken = "123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcd"
const testBotID = int64(123456789)

type botStub struct {
	getMe       func(context.Context) (BotUser, error)
	setCommands func(context.Context, []BotCommand) error
	send        func(context.Context, SendMessageRequest) (Message, error)
	answer      func(context.Context, AnswerCallbackQueryRequest) error
	editMarkup  func(context.Context, EditMessageReplyMarkupRequest) error
	updates     func(context.Context, GetUpdatesRequest) ([]Update, error)
}

func (b botStub) GetMe(ctx context.Context) (BotUser, error) {
	if b.getMe == nil {
		return BotUser{ID: testBotID, IsBot: true, Username: "modemdeck_test_bot"}, nil
	}
	return b.getMe(ctx)
}

func (b botStub) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	if b.setCommands == nil {
		return nil
	}
	return b.setCommands(ctx, commands)
}

func (b botStub) SendMessage(ctx context.Context, request SendMessageRequest) (Message, error) {
	if b.send == nil {
		return Message{MessageID: 1, Chat: Chat{ID: request.ChatID}}, nil
	}
	return b.send(ctx, request)
}

func (b botStub) AnswerCallbackQuery(ctx context.Context, request AnswerCallbackQueryRequest) error {
	if b.answer == nil {
		return nil
	}
	return b.answer(ctx, request)
}

func (b botStub) EditMessageReplyMarkup(ctx context.Context, request EditMessageReplyMarkupRequest) error {
	if b.editMarkup == nil {
		return nil
	}
	return b.editMarkup(ctx, request)
}

func (b botStub) GetUpdates(ctx context.Context, request GetUpdatesRequest) ([]Update, error) {
	if b.updates == nil {
		return nil, nil
	}
	return b.updates(ctx, request)
}

type lineQuerierFunc func(context.Context) ([]Line, error)

func (f lineQuerierFunc) Lines(ctx context.Context) ([]Line, error) {
	return f(ctx)
}

type smsQuerierFunc func(context.Context, SMSQuery) ([]SMS, error)

func (f smsQuerierFunc) RecentSMS(ctx context.Context, query SMSQuery) ([]SMS, error) {
	return f(ctx, query)
}

type smsSenderFunc func(context.Context, SMSRequest) error

func (f smsSenderFunc) SendSMS(ctx context.Context, request SMSRequest) error {
	return f(ctx, request)
}

type dialerFunc func(context.Context, CallRequest) error

func (f dialerFunc) Dial(ctx context.Context, request CallRequest) error {
	return f(ctx, request)
}

type messageReadMarkerFunc func(context.Context, string, string) error

func (f messageReadMarkerFunc) MarkMessageThreadRead(ctx context.Context, lineID, peer string) error {
	return f(ctx, lineID, peer)
}

type replyStoreStub struct {
	bind    func(context.Context, int64, int64, int64, ReplyBinding) error
	resolve func(context.Context, int64, int64, int64) (ReplyBinding, error)
}

func (s replyStoreStub) Bind(ctx context.Context, botID, chatID, messageID int64, binding ReplyBinding) error {
	if s.bind == nil {
		return nil
	}
	return s.bind(ctx, botID, chatID, messageID, binding)
}

func (s replyStoreStub) Resolve(ctx context.Context, botID, chatID, messageID int64) (ReplyBinding, error) {
	if s.resolve == nil {
		return ReplyBinding{}, &ReplyBindingNotFoundError{}
	}
	return s.resolve(ctx, botID, chatID, messageID)
}

type eventRecorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *eventRecorder) Observe(_ context.Context, event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *eventRecorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}
