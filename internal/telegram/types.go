package telegram

import (
	"context"
	"time"
)

const (
	MaxTelegramMessageRunes = 4096
	MaxSMSBodyRunes         = 1600
	DefaultSMSQueryLimit    = 10
	MaxSMSQueryLimit        = 20
	DefaultCallQueryLimit   = 10
	MaxCallQueryLimit       = 20
)

type BotUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type Message struct {
	MessageID      int64    `json:"message_id"`
	From           *BotUser `json:"from,omitempty"`
	Chat           Chat     `json:"chat"`
	Date           int64    `json:"date"`
	Text           string   `json:"text,omitempty"`
	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type SendMessageRequest struct {
	ChatID           int64
	Text             string
	ReplyToMessageID int64
	ReplyMarkup      *InlineKeyboardMarkup
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    BotUser  `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type AnswerCallbackQueryRequest struct {
	CallbackQueryID string
	Text            string
}

type EditMessageReplyMarkupRequest struct {
	ChatID      int64
	MessageID   int64
	ReplyMarkup InlineKeyboardMarkup
}

type GetUpdatesRequest struct {
	Offset  int64
	Limit   int
	Timeout time.Duration
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type BotAPI interface {
	GetMe(context.Context) (BotUser, error)
	SetMyCommands(context.Context, []BotCommand) error
	SendMessage(context.Context, SendMessageRequest) (Message, error)
	AnswerCallbackQuery(context.Context, AnswerCallbackQueryRequest) error
	EditMessageReplyMarkup(context.Context, EditMessageReplyMarkupRequest) error
	GetUpdates(context.Context, GetUpdatesRequest) ([]Update, error)
}

type Line struct {
	ID                string
	Label             string
	PhoneNumber       string
	Operator          string
	RegistrationKnown bool
	RegistrationState string
	Roaming           bool
	State             string
	Signal            *uint32
	CapabilitiesKnown bool
	SMSAvailable      bool
	CallAvailable     bool
	Available         bool
}

type SMS struct {
	ID         string
	LineID     string
	Direction  string
	Peer       string
	Body       string
	ReceivedAt time.Time
}

type SMSQuery struct {
	LineIDs       []string
	Limit         int
	Chronological bool
}

type SMSRequest struct {
	RequestID string
	LineID    string
	To        string
	Body      string
}

type Call struct {
	ID           string
	LineID       string
	Direction    string
	Peer         string
	ContactName  string
	OccurredAt   time.Time
	Missed       bool
	Read         bool
	HasRecording bool
}

type CallQuery struct {
	LineIDs []string
	Limit   int
}

type IncomingSMS struct {
	MessageID  string
	LineID     string
	LineLabel  string
	From       string
	Body       string
	ReceivedAt time.Time
}

type MissedCall struct {
	CallID    string
	LineID    string
	LineLabel string
	From      string
	CalledAt  time.Time
}

type ReplyBinding struct {
	LineID string
	Number string
}

type LineQuerier interface {
	Lines(context.Context) ([]Line, error)
}

type SMSQuerier interface {
	RecentSMS(context.Context, SMSQuery) ([]SMS, error)
}

type CallQuerier interface {
	RecentCalls(context.Context, CallQuery) ([]Call, error)
}

type SMSSender interface {
	// SendSMS must treat a repeated non-empty RequestID as the same operation.
	// This closes the crash window between executing a command and durably
	// advancing the Telegram update checkpoint.
	SendSMS(context.Context, SMSRequest) error
}

type ReplyBindingStore interface {
	// Keys include Bot ID because private-chat message IDs can overlap between
	// independent bots serving the same administrator. Resolve returns
	// *ReplyBindingNotFoundError when the referenced notification is unknown or
	// expired.
	Bind(context.Context, int64, int64, int64, ReplyBinding) error
	Resolve(context.Context, int64, int64, int64) (ReplyBinding, error)
}

type MessageReadMarker interface {
	MarkMessageThreadRead(context.Context, string, string) error
}

type CallReadMarker interface {
	MarkMissedCallsRead(context.Context, []string) error
}

type EventKind string

const (
	EventUnauthorizedUpdate EventKind = "unauthorized_update"
	EventInvalidCommand     EventKind = "invalid_command"
	EventOperationFailed    EventKind = "operation_failed"
	EventPollingFailed      EventKind = "polling_failed"
	EventUpdateHandled      EventKind = "update_handled"
)

// Event is deliberately safe for logs. It excludes bot tokens, chat/admin IDs,
// line IDs, phone numbers, SMS bodies, Telegram response descriptions, and raw
// dependency errors.
type Event struct {
	Kind       EventKind
	Operation  string
	ErrorClass string
	UpdateID   int64
}

type Observer interface {
	Observe(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (f ObserverFunc) Observe(ctx context.Context, event Event) {
	f(ctx, event)
}
