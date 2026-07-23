package telegramruntime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegram"
)

type CommunicationService interface {
	Status(context.Context) (communication.Status, error)
	SendMessage(context.Context, communication.SendMessageInput) (store.Message, error)
	StartCall(context.Context, communication.StartCallInput) (store.Call, error)
}

type Repository interface {
	Messages(context.Context, store.MessageQuery) ([]store.Message, error)
	MarkMessageThreadReadByLine(context.Context, string, string) error
	TelegramNextOffset(context.Context, string) (int64, error)
	AdvanceTelegramOffset(context.Context, string, int64) error
	BindTelegramReply(context.Context, int64, int64, int64, store.TelegramReplyBinding) error
	ResolveTelegramReply(context.Context, int64, int64, int64) (store.TelegramReplyBinding, error)
	UpdateTelegramRuntimeStatus(context.Context, string, string, string, string) error
	PendingTelegramNotificationDeliveries(context.Context, int) ([]store.TelegramNotificationDelivery, error)
	MarkSendingTelegramNotificationsIndeterminate(context.Context) error
	ClaimTelegramNotificationDelivery(context.Context, string, string, string) (bool, error)
	FinishTelegramNotificationDelivery(context.Context, string, string, string, string, string) error
}

type adapters struct {
	communications CommunicationService
	repository     Repository
}

func (a adapters) Lines(ctx context.Context) ([]telegram.Line, error) {
	status, err := a.communications.Status(ctx)
	if err != nil {
		return nil, err
	}
	lines := make([]telegram.Line, 0, len(status.Lines))
	for _, line := range status.Lines {
		lines = append(lines, telegram.Line{
			ID:          line.ID,
			Label:       lineLabel(line),
			PhoneNumber: line.PhoneNumber,
			Available:   lineAvailable(line.State),
		})
	}
	return lines, nil
}

func (a adapters) RecentSMS(ctx context.Context, query telegram.SMSQuery) ([]telegram.SMS, error) {
	messages, err := a.repository.Messages(ctx, store.MessageQuery{
		LineIDs: query.LineIDs,
		Limit:   query.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]telegram.SMS, 0, len(messages))
	for _, message := range messages {
		result = append(result, telegram.SMS{
			ID:         message.EndpointMessageID,
			LineID:     message.LineID,
			Direction:  message.Direction,
			Peer:       message.Peer,
			Body:       message.Content,
			ReceivedAt: parseDatabaseTime(message.Timestamp),
		})
	}
	return result, nil
}

func (a adapters) SendSMS(ctx context.Context, request telegram.SMSRequest) error {
	_, err := a.communications.SendMessage(ctx, communication.SendMessageInput{
		RequestID: request.RequestID,
		LineID:    request.LineID,
		Number:    request.To,
		Text:      request.Body,
	})
	return err
}

func (a adapters) Dial(ctx context.Context, request telegram.CallRequest) error {
	_, err := a.communications.StartCall(ctx, communication.StartCallInput{
		RequestID: request.RequestID,
		LineID:    request.LineID,
		Number:    request.To,
	})
	return err
}

func (a adapters) MarkMessageThreadRead(ctx context.Context, lineID, peer string) error {
	return a.repository.MarkMessageThreadReadByLine(ctx, lineID, peer)
}

func (a adapters) Bind(
	ctx context.Context,
	botID, chatID, messageID int64,
	binding telegram.ReplyBinding,
) error {
	return a.repository.BindTelegramReply(ctx, botID, chatID, messageID, store.TelegramReplyBinding{
		LineID: binding.LineID,
		Number: binding.Number,
	})
}

func (a adapters) Resolve(
	ctx context.Context,
	botID, chatID, messageID int64,
) (telegram.ReplyBinding, error) {
	binding, err := a.repository.ResolveTelegramReply(ctx, botID, chatID, messageID)
	if errors.Is(err, store.ErrTelegramReplyBindingNotFound) {
		return telegram.ReplyBinding{}, &telegram.ReplyBindingNotFoundError{}
	}
	if err != nil {
		return telegram.ReplyBinding{}, err
	}
	return telegram.ReplyBinding{LineID: binding.LineID, Number: binding.Number}, nil
}

type checkpoint struct {
	unitID     string
	repository Repository
}

func (c checkpoint) NextOffset(ctx context.Context) (int64, error) {
	return c.repository.TelegramNextOffset(ctx, c.unitID)
}

func (c checkpoint) Advance(ctx context.Context, next int64) error {
	return c.repository.AdvanceTelegramOffset(ctx, c.unitID, next)
}

func lineLabel(line store.LineSummary) string {
	for _, value := range []string{
		line.DeviceAlias,
		line.PhoneNumber,
		line.Model,
		line.Operator,
		line.ID,
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return line.ID
}

func lineAvailable(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "registered", "connected":
		return true
	default:
		return false
	}
}

func parseDatabaseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
