package telegram

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Dependencies struct {
	Bot       BotAPI
	Lines     LineQuerier
	SMS       SMSQuerier
	Calls     CallQuerier
	SMSSender SMSSender
	Replies   ReplyBindingStore
	Read      MessageReadMarker
	Observer  Observer
}

type Service struct {
	config   Config
	botID    int64
	bot      BotAPI
	lines    LineQuerier
	sms      SMSQuerier
	calls    CallQuerier
	sender   SMSSender
	replies  ReplyBindingStore
	read     MessageReadMarker
	observer Observer

	identityMu  sync.RWMutex
	botUsername string
}

func NewService(config Config, dependencies Dependencies) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	config.LineScopes = append([]string(nil), config.LineScopes...)

	if config.Enabled {
		switch {
		case dependencies.Bot == nil:
			return nil, &ConfigError{Field: "bot", Reason: "dependency is required"}
		case dependencies.Lines == nil:
			return nil, &ConfigError{Field: "lines", Reason: "dependency is required"}
		case dependencies.SMS == nil:
			return nil, &ConfigError{Field: "sms", Reason: "dependency is required"}
		case dependencies.Calls == nil:
			return nil, &ConfigError{Field: "calls", Reason: "dependency is required"}
		case dependencies.SMSSender == nil:
			return nil, &ConfigError{Field: "sms_sender", Reason: "dependency is required"}
		case dependencies.Replies == nil:
			return nil, &ConfigError{Field: "replies", Reason: "dependency is required"}
		case dependencies.Read == nil:
			return nil, &ConfigError{Field: "read_marker", Reason: "dependency is required"}
		}
	}

	return &Service{
		config:   config,
		botID:    botIDFromToken(config.BotToken),
		bot:      dependencies.Bot,
		lines:    dependencies.Lines,
		sms:      dependencies.SMS,
		calls:    dependencies.Calls,
		sender:   dependencies.SMSSender,
		replies:  dependencies.Replies,
		read:     dependencies.Read,
		observer: dependencies.Observer,
	}, nil
}

func (s *Service) VerifyBot(ctx context.Context) (BotUser, error) {
	if !s.config.Enabled {
		return BotUser{}, nil
	}
	user, err := s.bot.GetMe(ctx)
	if err != nil {
		return BotUser{}, &OperationError{Operation: "verify_bot", Kind: errorClass(err), Err: err}
	}
	if user.ID != s.botID || !user.IsBot || strings.TrimSpace(user.Username) == "" {
		err := &ProtocolError{Method: "getMe", Reason: "result does not match the configured bot identity"}
		return BotUser{}, &OperationError{Operation: "verify_bot", Kind: errorClass(err), Err: err}
	}
	s.identityMu.Lock()
	s.botUsername = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(user.Username)), "@")
	s.identityMu.Unlock()
	return user, nil
}

// InitializeBot verifies the configured identity and writes the canonical
// command menu. Repeating it is safe because setMyCommands replaces that menu.
func (s *Service) InitializeBot(ctx context.Context) (BotUser, error) {
	user, err := s.VerifyBot(ctx)
	if err != nil || !s.config.Enabled {
		return user, err
	}
	if err := s.bot.SetMyCommands(ctx, botCommands()); err != nil {
		return BotUser{}, &OperationError{
			Operation: "configure_bot_commands",
			Kind:      errorClass(err),
			Err:       err,
		}
	}
	return user, nil
}

// Run is the standard service entrypoint: it initializes the bot before
// accepting commands, then starts bounded long polling with a durable
// per-bot checkpoint.
func (s *Service) Run(ctx context.Context, checkpoint Checkpoint, options PollOptions) error {
	if !s.config.Enabled {
		return nil
	}
	if _, err := s.InitializeBot(ctx); err != nil {
		return err
	}
	poller, err := NewPoller(s.bot, s, checkpoint, s.observer, options)
	if err != nil {
		return err
	}
	return poller.Run(ctx)
}

// HandleUpdate enforces both configured identities before parsing commands. It
// intentionally returns no information to unauthorized chats or users.
func (s *Service) HandleUpdate(ctx context.Context, update Update) error {
	if !s.config.Enabled {
		return nil
	}
	if update.CallbackQuery != nil {
		return s.handleCallbackQuery(ctx, update.UpdateID, update.CallbackQuery)
	}
	if update.Message == nil {
		return nil
	}
	message := update.Message
	if message.Chat.ID != s.config.ChatID || message.From == nil || message.From.ID != s.config.AdminID {
		s.observe(ctx, Event{Kind: EventUnauthorizedUpdate, Operation: "authorize", UpdateID: update.UpdateID})
		return nil
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return nil
	}
	if !strings.HasPrefix(text, "/") && message.ReplyToMessage != nil {
		return s.handleDirectReply(ctx, update.UpdateID, message)
	}

	command, err := ParseCommand(text)
	if err != nil {
		s.observe(ctx, Event{Kind: EventInvalidCommand, Operation: "parse_command", UpdateID: update.UpdateID})
		return s.sendText(ctx, update.UpdateID, message.MessageID, "命令格式错误。使用 /help 查看可用命令。")
	}
	if command.TargetBot != "" && !s.isCommandTarget(command.TargetBot) {
		return nil
	}

	switch command.Kind {
	case CommandHelp:
		return s.sendText(ctx, update.UpdateID, message.MessageID, helpText())
	case CommandList:
		return s.handleLines(ctx, update.UpdateID, message.MessageID)
	case CommandSMS:
		return s.handleSMSQuery(ctx, update.UpdateID, message.MessageID, command)
	case CommandCall:
		return s.handleCall(ctx, update.UpdateID, message.MessageID, command)
	case CommandReply:
		return s.handleReplyCommand(ctx, update.UpdateID, message.MessageID, command)
	default:
		s.observe(ctx, Event{Kind: EventInvalidCommand, Operation: "dispatch_command", UpdateID: update.UpdateID})
		return s.sendText(ctx, update.UpdateID, message.MessageID, "未知命令。使用 /help 查看可用命令。")
	}
}

const markReadCallbackData = "sms:mark-read"

func (s *Service) handleCallbackQuery(
	ctx context.Context,
	updateID int64,
	query *CallbackQuery,
) error {
	if query == nil || query.Message == nil || query.Data != markReadCallbackData {
		return nil
	}
	message := query.Message
	if message.Chat.ID != s.config.ChatID || query.From.ID != s.config.AdminID {
		s.observe(ctx, Event{Kind: EventUnauthorizedUpdate, Operation: "authorize_callback", UpdateID: updateID})
		return nil
	}
	binding, err := s.replies.Resolve(ctx, s.botID, s.config.ChatID, message.MessageID)
	if err != nil {
		var notFoundErr *ReplyBindingNotFoundError
		if errors.As(err, &notFoundErr) {
			s.answerCallback(ctx, query.ID, "消息已过期")
			return nil
		}
		return &OperationError{Operation: "resolve_read_binding", Kind: errorClass(err), Err: err}
	}
	if err := s.read.MarkMessageThreadRead(ctx, binding.LineID, binding.Number); err != nil {
		s.answerCallback(ctx, query.ID, "标记失败，请重试")
		s.observe(ctx, Event{Kind: EventOperationFailed, Operation: "mark_message_read", ErrorClass: errorClass(err), UpdateID: updateID})
		return &OperationError{Operation: "mark_message_read", Kind: errorClass(err), Err: err}
	}
	s.answerCallback(ctx, query.ID, "已标记已读")
	if err := s.bot.EditMessageReplyMarkup(ctx, EditMessageReplyMarkupRequest{
		ChatID:    message.Chat.ID,
		MessageID: message.MessageID,
		ReplyMarkup: InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{},
		},
	}); err != nil {
		s.observe(ctx, Event{Kind: EventOperationFailed, Operation: "clear_read_action", ErrorClass: errorClass(err), UpdateID: updateID})
	}
	return nil
}

func (s *Service) answerCallback(ctx context.Context, callbackQueryID, text string) {
	if err := s.bot.AnswerCallbackQuery(ctx, AnswerCallbackQueryRequest{
		CallbackQueryID: callbackQueryID,
		Text:            text,
	}); err != nil {
		s.observe(ctx, Event{Kind: EventOperationFailed, Operation: "answer_callback", ErrorClass: errorClass(err)})
	}
}

func (s *Service) isCommandTarget(target string) bool {
	s.identityMu.RLock()
	username := s.botUsername
	s.identityMu.RUnlock()
	return username != "" && strings.EqualFold(strings.TrimPrefix(target, "@"), username)
}

func (s *Service) NotifyIncomingSMS(ctx context.Context, incoming IncomingSMS) error {
	if !s.config.Enabled || !s.config.Notifications.IncomingSMS || !s.config.AllowsLine(incoming.LineID) {
		return nil
	}
	if err := validateLineID(incoming.LineID); err != nil {
		return &OperationError{Operation: "notify_incoming_sms", Kind: "invalid_line", Err: err}
	}
	if strings.TrimSpace(incoming.Body) == "" {
		return &OperationError{Operation: "notify_incoming_sms", Kind: "invalid_body", Err: errors.New("SMS body is empty")}
	}

	displayPeer, _, replyable := notificationPeer(incoming.From)
	lineIdentity := s.notificationLineIdentity(ctx, incoming.LineID, incoming.LineLabel)
	text := formatIncomingSMS(incoming, displayPeer, replyable, lineIdentity)
	peer := strings.TrimSpace(incoming.From)
	request := SendMessageRequest{ChatID: s.config.ChatID, Text: text}
	if peer != "" {
		request.ReplyMarkup = &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{{
				{Text: "标记已读", CallbackData: markReadCallbackData},
			}},
		}
	}
	sent, err := s.bot.SendMessage(ctx, request)
	if err != nil {
		s.observe(ctx, Event{Kind: EventOperationFailed, Operation: "notify_incoming_sms", ErrorClass: errorClass(err)})
		return &OperationError{Operation: "notify_incoming_sms", Kind: errorClass(err), Err: err}
	}
	if peer != "" {
		if err := s.replies.Bind(ctx, s.botID, s.config.ChatID, sent.MessageID, ReplyBinding{
			LineID: incoming.LineID,
			Number: peer,
		}); err != nil {
			s.observe(ctx, Event{Kind: EventOperationFailed, Operation: "bind_sms_reply", ErrorClass: errorClass(err)})
			return &OperationError{Operation: "bind_sms_reply", Kind: errorClass(err), Err: err}
		}
	}
	return nil
}

func (s *Service) NotifyMissedCall(ctx context.Context, missed MissedCall) error {
	if !s.config.Enabled || !s.config.Notifications.MissedCalls || !s.config.AllowsLine(missed.LineID) {
		return nil
	}
	if err := validateLineID(missed.LineID); err != nil {
		return &OperationError{Operation: "notify_missed_call", Kind: "invalid_line", Err: err}
	}
	displayPeer, _, _ := notificationPeer(missed.From)
	lineIdentity := s.notificationLineIdentity(ctx, missed.LineID, missed.LineLabel)
	if _, err := s.bot.SendMessage(ctx, SendMessageRequest{
		ChatID: s.config.ChatID,
		Text:   formatMissedCall(missed, displayPeer, lineIdentity),
	}); err != nil {
		s.observe(ctx, Event{Kind: EventOperationFailed, Operation: "notify_missed_call", ErrorClass: errorClass(err)})
		return &OperationError{Operation: "notify_missed_call", Kind: errorClass(err), Err: err}
	}
	return nil
}

func (s *Service) notificationLineIdentity(
	ctx context.Context,
	lineID, fallbackLabel string,
) string {
	label := safeNotificationLineLabel(fallbackLabel, lineID)
	phoneNumber := ""
	if lines, err := s.lines.Lines(ctx); err == nil {
		for _, line := range lines {
			if line.ID != lineID {
				continue
			}
			if current := safeNotificationLineLabel(line.Label, lineID); current != "" {
				label = current
			}
			phoneNumber = strings.TrimSpace(line.PhoneNumber)
			break
		}
	}
	if label == "" || label == phoneNumber {
		label = "线路"
	}
	if phoneNumber != "" {
		return label + " · " + phoneNumber
	}
	return label
}

func safeNotificationLineLabel(value, lineID string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == strings.TrimSpace(lineID) {
		return ""
	}
	return value
}

func (s *Service) handleLines(ctx context.Context, updateID, replyTo int64) error {
	lines, err := s.accessibleLines(ctx)
	if err != nil {
		return s.reportOperationFailure(ctx, updateID, replyTo, "list_lines", err)
	}
	return s.sendText(ctx, updateID, replyTo, formatLines(lines))
}

func (s *Service) handleSMSQuery(ctx context.Context, updateID, replyTo int64, command Command) error {
	lines, err := s.accessibleLines(ctx)
	if err != nil {
		return s.reportOperationFailure(ctx, updateID, replyTo, "query_sms_lines", err)
	}
	queryLineIDs := make([]string, 0, len(lines))
	if command.LineID != "" {
		if !containsLine(lines, command.LineID) {
			return s.sendText(ctx, updateID, replyTo, "线路不存在或不在此 Bot 的授权范围内。")
		}
		queryLineIDs = append(queryLineIDs, command.LineID)
	} else {
		for _, line := range lines {
			queryLineIDs = append(queryLineIDs, line.ID)
		}
	}
	if len(queryLineIDs) == 0 {
		return s.sendText(ctx, updateID, replyTo, "没有可用线路。")
	}

	messages, err := s.sms.RecentSMS(ctx, SMSQuery{
		LineIDs:       queryLineIDs,
		Limit:         command.Limit,
		Chronological: true,
	})
	if err != nil {
		return s.reportOperationFailure(ctx, updateID, replyTo, "query_sms", err)
	}
	messages = filterSMS(messages, queryLineIDs)
	return s.sendText(ctx, updateID, replyTo, formatSMS(messages, lines))
}

func (s *Service) handleCall(ctx context.Context, updateID, replyTo int64, command Command) error {
	lines, err := s.accessibleLines(ctx)
	if err != nil {
		return s.reportOperationFailure(ctx, updateID, replyTo, "query_call_lines", err)
	}
	lineIDs := make([]string, 0, len(lines))
	for _, line := range lines {
		lineIDs = append(lineIDs, line.ID)
	}
	if len(lineIDs) == 0 {
		return s.sendText(ctx, updateID, replyTo, "没有可用线路。")
	}
	calls, err := s.calls.RecentCalls(ctx, CallQuery{
		LineIDs: lineIDs,
		Limit:   command.Limit,
	})
	if err != nil {
		return s.reportOperationFailure(ctx, updateID, replyTo, "query_calls", err)
	}
	return s.sendText(ctx, updateID, replyTo, formatCalls(filterCalls(calls, lineIDs), lines))
}

func (s *Service) handleReplyCommand(ctx context.Context, updateID, replyTo int64, command Command) error {
	return s.sendSMS(ctx, updateID, replyTo, command.LineID, command.Number, command.Body)
}

func (s *Service) handleDirectReply(ctx context.Context, updateID int64, message *Message) error {
	body := strings.TrimSpace(message.Text)
	if utf8.RuneCountInString(body) > MaxSMSBodyRunes {
		return s.sendText(ctx, updateID, message.MessageID, "短信内容过长，最多 1600 个字符。")
	}
	binding, err := s.replies.Resolve(ctx, s.botID, s.config.ChatID, message.ReplyToMessage.MessageID)
	if err != nil {
		var notFoundErr *ReplyBindingNotFoundError
		if errors.As(err, &notFoundErr) {
			return s.sendText(ctx, updateID, message.MessageID, "这条消息没有可用的短信回复目标。")
		}
		return s.reportOperationFailure(ctx, updateID, message.MessageID, "resolve_sms_reply", err)
	}
	number, err := NormalizePhone(binding.Number)
	if err != nil {
		return s.reportOperationFailure(ctx, updateID, message.MessageID, "resolve_sms_number", err)
	}
	return s.sendSMS(ctx, updateID, message.MessageID, binding.LineID, number, body)
}

func (s *Service) sendSMS(ctx context.Context, updateID, replyTo int64, lineID, number, body string) error {
	if _, err := s.resolveLine(ctx, lineID); err != nil {
		var unavailableErr *lineUnavailableError
		if errors.As(err, &unavailableErr) {
			return s.sendText(ctx, updateID, replyTo, "线路不存在、不可用或不在此 Bot 的授权范围内。")
		}
		return s.reportOperationFailure(ctx, updateID, replyTo, "resolve_sms_line", err)
	}
	if utf8.RuneCountInString(body) > MaxSMSBodyRunes {
		return s.sendText(ctx, updateID, replyTo, "短信内容过长，最多 1600 个字符。")
	}
	if err := s.sender.SendSMS(ctx, SMSRequest{
		RequestID: requestID(s.botID, updateID, "sms"),
		LineID:    lineID,
		To:        number,
		Body:      body,
	}); err != nil {
		return s.reportOperationFailure(ctx, updateID, replyTo, "send_sms", err)
	}
	return s.sendText(ctx, updateID, replyTo, "短信已提交发送："+number)
}

type lineUnavailableError struct{}

func (*lineUnavailableError) Error() string {
	return "line unavailable"
}

func (s *Service) resolveLine(ctx context.Context, lineID string) (Line, error) {
	if !s.config.AllowsLine(lineID) {
		return Line{}, &lineUnavailableError{}
	}
	lines, err := s.lines.Lines(ctx)
	if err != nil {
		return Line{}, err
	}
	for _, line := range lines {
		if line.ID == lineID && line.Available {
			return line, nil
		}
	}
	return Line{}, &lineUnavailableError{}
}

func (s *Service) accessibleLines(ctx context.Context) ([]Line, error) {
	lines, err := s.lines.Lines(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]Line, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		if validateLineID(line.ID) != nil || !s.config.AllowsLine(line.ID) {
			continue
		}
		if _, exists := seen[line.ID]; exists {
			continue
		}
		seen[line.ID] = struct{}{}
		filtered = append(filtered, line)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Label == filtered[j].Label {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].Label < filtered[j].Label
	})
	return filtered, nil
}

func (s *Service) reportOperationFailure(
	ctx context.Context,
	updateID int64,
	replyTo int64,
	operation string,
	err error,
) error {
	s.observe(ctx, Event{
		Kind:       EventOperationFailed,
		Operation:  operation,
		ErrorClass: errorClass(err),
		UpdateID:   updateID,
	})
	if sendErr := s.sendText(ctx, updateID, replyTo, "操作失败，请稍后重试。"); sendErr != nil {
		return sendErr
	}
	return nil
}

func (s *Service) sendText(ctx context.Context, updateID, replyTo int64, text string) error {
	text = truncateRunes(text, MaxTelegramMessageRunes)
	if _, err := s.bot.SendMessage(ctx, SendMessageRequest{
		ChatID:           s.config.ChatID,
		Text:             text,
		ReplyToMessageID: replyTo,
	}); err != nil {
		s.observe(ctx, Event{
			Kind:       EventOperationFailed,
			Operation:  "send_response",
			ErrorClass: errorClass(err),
			UpdateID:   updateID,
		})
		return &OperationError{Operation: "send_response", Kind: errorClass(err), Err: err}
	}
	return nil
}

func (s *Service) observe(ctx context.Context, event Event) {
	if s.observer != nil {
		s.observer.Observe(ctx, event)
	}
}

func containsLine(lines []Line, lineID string) bool {
	for _, line := range lines {
		if line.ID == lineID {
			return true
		}
	}
	return false
}

func filterSMS(messages []SMS, allowedLineIDs []string) []SMS {
	allowed := make(map[string]struct{}, len(allowedLineIDs))
	for _, lineID := range allowedLineIDs {
		allowed[lineID] = struct{}{}
	}
	filtered := make([]SMS, 0, len(messages))
	for _, message := range messages {
		if _, ok := allowed[message.LineID]; ok {
			filtered = append(filtered, message)
		}
	}
	return filtered
}

func filterCalls(calls []Call, allowedLineIDs []string) []Call {
	allowed := make(map[string]struct{}, len(allowedLineIDs))
	for _, lineID := range allowedLineIDs {
		allowed[lineID] = struct{}{}
	}
	filtered := make([]Call, 0, len(calls))
	for _, call := range calls {
		if _, ok := allowed[call.LineID]; ok {
			filtered = append(filtered, call)
		}
	}
	return filtered
}

func requestID(botID, updateID int64, operation string) string {
	return fmt.Sprintf("telegram:%d:%d:%s", botID, updateID, operation)
}

func helpText() string {
	return strings.Join([]string{
		"ModemDeck Telegram 命令",
		"/list - 查看线路状态",
		"/sms [线路ID] [1-20] - 查看最近短信",
		"/call [1-20] - 查看最近通话",
		"/reply <线路ID> <国际号码> <内容> - 回复短信",
		"/help - 查看帮助",
		"",
		"也可以直接回复一条“新短信”通知。",
	}, "\n")
}

func botCommands() []BotCommand {
	return []BotCommand{
		{Command: "list", Description: "查看线路状态"},
		{Command: "sms", Description: "查看最近短信"},
		{Command: "call", Description: "查看最近通话"},
		{Command: "reply", Description: "回复短信"},
		{Command: "help", Description: "查看帮助"},
	}
}

func formatLines(lines []Line) string {
	if len(lines) == 0 {
		return "没有授权线路。"
	}
	var builder strings.Builder
	builder.WriteString("线路\n")
	for index, line := range lines {
		if index > 0 {
			builder.WriteByte('\n')
		}
		fmt.Fprintf(&builder, "%s · %s\n", lineDisplayName(line, index), linePhoneNumber(line))
		details := []string{lineRegistrationLabel(line)}
		if operator := lineOperatorLabel(line.Operator); operator != "" {
			details = append(details, operator)
		}
		if line.Signal == nil {
			details = append(details, "信号未知")
		} else {
			details = append(details, fmt.Sprintf("信号 %d%%", *line.Signal))
		}
		builder.WriteString(strings.Join(details, " · "))
		builder.WriteByte('\n')
		builder.WriteString(lineCapabilityLabel(line))
	}
	return truncateRunes(strings.TrimSpace(builder.String()), MaxTelegramMessageRunes)
}

func formatCalls(calls []Call, lines []Line) string {
	if len(calls) == 0 {
		return "没有通话记录。"
	}
	lineNames := make(map[string]string, len(lines))
	for index, line := range lines {
		lineNames[line.ID] = lineDisplayName(line, index)
	}
	var builder strings.Builder
	builder.WriteString("最近通话\n")
	for _, call := range calls {
		direction := "通话"
		switch {
		case call.Missed:
			direction = "未接"
		case strings.EqualFold(call.Direction, "incoming"):
			direction = "呼入"
		case strings.EqualFold(call.Direction, "outgoing"):
			direction = "呼出"
		}
		peer := singleLine(call.ContactName)
		if peer == "" {
			peer = singleLine(call.Peer)
		}
		if peer == "" {
			peer = "未知号码"
		}
		lineName := lineNames[call.LineID]
		if lineName == "" {
			lineName = "线路"
		}
		details := []string{lineName}
		if !call.OccurredAt.IsZero() {
			details = append(details, call.OccurredAt.UTC().Format("2006-01-02 15:04 UTC"))
		}
		if call.HasRecording {
			details = append(details, "有录音")
		}
		fmt.Fprintf(&builder, "\n%s · %s\n%s\n", direction, peer, strings.Join(details, " · "))
	}
	return truncateRunes(strings.TrimSpace(builder.String()), MaxTelegramMessageRunes)
}

func lineDisplayName(line Line, index int) string {
	label := singleLine(line.Label)
	if label == "" || label == strings.TrimSpace(line.ID) {
		return fmt.Sprintf("未命名线路 %d", index+1)
	}
	return label
}

func linePhoneNumber(line Line) string {
	number := singleLine(line.PhoneNumber)
	if number == "" {
		return "号码未读取"
	}
	return number
}

func lineDisplayIdentity(line Line, index int) string {
	name := lineDisplayName(line, index)
	number := singleLine(line.PhoneNumber)
	if number == "" || number == name {
		return name
	}
	return name + " · " + number
}

func lineRegistrationLabel(line Line) string {
	if line.RegistrationKnown {
		switch strings.ToLower(strings.TrimSpace(line.RegistrationState)) {
		case "registered", "home", "registered-home":
			return "已驻网"
		case "roaming", "registered-roaming":
			return "漫游"
		case "searching":
			return "搜网中"
		case "denied":
			return "驻网被拒绝"
		case "idle", "not-registered":
			return "未驻网"
		}
	}
	if line.Roaming {
		return "漫游"
	}
	if line.Available {
		return "在线"
	}
	return "状态未知"
}

func lineOperatorLabel(operator string) string {
	operator = singleLine(operator)
	if operator == "" || isOperatorCode(operator) {
		return ""
	}
	return operator
}

func isOperatorCode(value string) bool {
	if len(value) != 5 && len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func lineCapabilityLabel(line Line) string {
	if !line.CapabilitiesKnown {
		return "能力未知"
	}
	sms := "短信不可用"
	if line.SMSAvailable {
		sms = "短信可用"
	}
	call := "呼叫控制不可用"
	if line.CallAvailable {
		call = "呼叫控制可用"
	}
	return sms + " · " + call
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

const smsMessageDivider = "────────"

func formatSMS(messages []SMS, lines []Line) string {
	if len(messages) == 0 {
		return "没有短信。"
	}
	lineIdentities := make(map[string]string, len(lines))
	for index, line := range lines {
		lineIdentities[line.ID] = lineDisplayIdentity(line, index)
	}
	var builder strings.Builder
	builder.WriteString("最近短信\n\n")
	for index, message := range messages {
		if index > 0 {
			builder.WriteString("\n")
			builder.WriteString(smsMessageDivider)
			builder.WriteString("\n\n")
		}
		direction := "收到"
		if strings.EqualFold(message.Direction, "outgoing") {
			direction = "发出"
		}
		lineIdentity := lineIdentities[message.LineID]
		if lineIdentity == "" {
			lineIdentity = "线路"
		}
		fmt.Fprintf(&builder, "%s · %s\n", direction, lineIdentity)
		peer := singleLine(message.Peer)
		if peer == "" {
			peer = "未知号码"
		}
		fmt.Fprintf(&builder, "对方：%s", peer)
		if !message.ReceivedAt.IsZero() {
			fmt.Fprintf(&builder, "\n时间：%s", message.ReceivedAt.Format(time.RFC3339))
		}
		builder.WriteByte('\n')
		builder.WriteString(truncateRunes(strings.TrimSpace(message.Body), 320))
	}
	return truncateRunes(strings.TrimSpace(builder.String()), MaxTelegramMessageRunes)
}

func formatIncomingSMS(
	incoming IncomingSMS,
	peer string,
	replyable bool,
	lineIdentity string,
) string {
	var builder strings.Builder
	builder.WriteString("新短信\n")
	fmt.Fprintf(&builder, "线路：%s\n来自：%s", lineIdentity, peer)
	if !incoming.ReceivedAt.IsZero() {
		fmt.Fprintf(&builder, "\n时间：%s", incoming.ReceivedAt.Format(time.RFC3339))
	}
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimSpace(incoming.Body))
	if replyable {
		builder.WriteString("\n\n直接回复此消息即可发送短信。")
	} else {
		builder.WriteString("\n\n发送方不是可验证的国际号码，不能直接回复。")
	}
	return truncateRunes(builder.String(), MaxTelegramMessageRunes)
}

func formatMissedCall(missed MissedCall, number, lineIdentity string) string {
	text := fmt.Sprintf("未接来电\n线路：%s\n号码：%s", lineIdentity, number)
	if !missed.CalledAt.IsZero() {
		text += "\n时间：" + missed.CalledAt.Format(time.RFC3339)
	}
	return truncateRunes(text, MaxTelegramMessageRunes)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
