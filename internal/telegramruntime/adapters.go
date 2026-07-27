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
}

type Repository interface {
	Lines(context.Context) ([]store.LineSummary, error)
	Messages(context.Context, store.MessageQuery) ([]store.Message, error)
	Calls(context.Context, store.CallQuery) ([]store.Call, error)
	RecordingEntries(context.Context, store.RecordingQuery) ([]store.RecordingEntry, error)
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
	lines, err := a.lineSummaries(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]telegram.Line, 0, len(lines))
	for _, line := range lines {
		capabilitiesKnown := line.Capabilities.Modem ||
			line.Capabilities.SIM ||
			line.Capabilities.Messaging ||
			line.Capabilities.Voice ||
			line.Capabilities.SendMessage ||
			line.Capabilities.Dial
		result = append(result, telegram.Line{
			ID:                line.ID,
			Label:             lineLabel(line),
			PhoneNumber:       line.PhoneNumber,
			Operator:          currentOperator(line),
			RegistrationKnown: line.RegistrationStateKnown,
			RegistrationState: line.RegistrationState,
			Roaming:           line.Roaming,
			State:             line.State,
			Signal:            cloneSignal(line.Signal),
			CapabilitiesKnown: capabilitiesKnown,
			SMSAvailable:      line.Capabilities.SendMessage,
			CallAvailable:     line.Capabilities.Dial,
			Available:         lineAvailable(line.State),
		})
	}
	return result, nil
}

func (a adapters) lineSummaries(ctx context.Context) ([]store.LineSummary, error) {
	status, err := a.communications.Status(ctx)
	if err != nil {
		return nil, err
	}
	if persisted, persistedErr := a.repository.Lines(ctx); persistedErr == nil {
		status.Lines = mergePersistedLineMetadata(status.Lines, persisted)
	}
	return status.Lines, nil
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

func (a adapters) RecentCalls(ctx context.Context, query telegram.CallQuery) ([]telegram.Call, error) {
	allowed := make(map[string]struct{}, len(query.LineIDs))
	for _, lineID := range query.LineIDs {
		allowed[lineID] = struct{}{}
	}
	calls, err := a.repository.Calls(ctx, store.CallQuery{
		Kind:  store.CallKindAll,
		Limit: store.MaxQueryLimit,
	})
	if err != nil {
		return nil, err
	}
	recorded := make(map[string]struct{})
	if entries, recordingErr := a.repository.RecordingEntries(
		ctx,
		store.RecordingQuery{Limit: store.MaxQueryLimit},
	); recordingErr == nil {
		for _, entry := range entries {
			if entry.Playable {
				recorded[entry.Call.ID] = struct{}{}
			}
		}
	}
	limit := query.Limit
	if limit <= 0 || limit > telegram.MaxCallQueryLimit {
		limit = telegram.DefaultCallQueryLimit
	}
	result := make([]telegram.Call, 0, limit)
	for _, call := range calls {
		lineID := strings.TrimSpace(call.LineID)
		if _, ok := allowed[lineID]; !ok {
			continue
		}
		occurredAt := parseDatabaseTime(call.EndedAt)
		if occurredAt.IsZero() {
			occurredAt = parseDatabaseTime(call.StartedAt)
		}
		_, hasRecording := recorded[call.ID]
		result = append(result, telegram.Call{
			ID:           call.ID,
			LineID:       lineID,
			Direction:    call.Direction,
			Peer:         call.RemoteNumber,
			ContactName:  call.ContactName,
			OccurredAt:   occurredAt,
			Missed:       call.Missed,
			HasRecording: hasRecording,
		})
		if len(result) == limit {
			break
		}
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
		line.LineLabel,
		line.DeviceAlias,
		line.Model,
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
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

func mergePersistedLineMetadata(
	liveLines []store.LineSummary,
	persistedLines []store.LineSummary,
) []store.LineSummary {
	byID := make(map[string]store.LineSummary, len(persistedLines))
	aliasesByIMEI := make(map[string]string, len(persistedLines))
	for _, line := range persistedLines {
		if lineID := strings.TrimSpace(line.ID); lineID != "" {
			byID[lineID] = line
		}
		if imei := strings.TrimSpace(line.DeviceIMEI); imei != "" {
			aliasesByIMEI[imei] = strings.TrimSpace(line.DeviceAlias)
		}
	}
	merged := make([]store.LineSummary, len(liveLines))
	for index, live := range liveLines {
		line := live
		if line.DeviceAlias == "" {
			line.DeviceAlias = aliasesByIMEI[strings.TrimSpace(line.DeviceIMEI)]
		}
		persisted, found := byID[strings.TrimSpace(line.ID)]
		if found {
			line.LineLabel = persisted.LineLabel
			line.LineColor = persisted.LineColor
			if line.PhoneNumber == "" {
				line.PhoneNumber = persisted.PhoneNumber
			}
			if line.HomeOperatorName == "" {
				line.HomeOperatorName = persisted.HomeOperatorName
			}
			if line.Operator == "" {
				line.Operator = persisted.Operator
			}
		}
		merged[index] = line
	}
	return merged
}

func currentOperator(line store.LineSummary) string {
	state := strings.ToLower(strings.TrimSpace(line.RegistrationState))
	if line.RegistrationStateKnown &&
		(state == "registered" || state == "home" || state == "registered-home" ||
			state == "roaming" || state == "registered-roaming") {
		for _, value := range []string{line.ServingOperatorName, line.ServingOperatorCode} {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	for _, value := range []string{
		line.HomeOperatorName,
		line.Operator,
		line.HomeOperatorCode,
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func cloneSignal(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
