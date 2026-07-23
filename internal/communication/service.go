package communication

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/phone"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	snapshotTimeout  = 5 * time.Second
	commandTimeout   = 20 * time.Second
	defaultSyncEvery = 3 * time.Second
	freshSnapshotAge = 5 * time.Second
	maxMessageRunes  = 1600
	maxRequestIDLen  = 128
)

type Agent interface {
	Health(context.Context) (agentclient.Health, error)
	Snapshot(context.Context) (agentclient.Snapshot, error)
	StartCall(context.Context, agentclient.StartCallRequest) (agentclient.Call, error)
	CallAction(context.Context, string, string, agentclient.CallActionRequest) (agentclient.Call, error)
	SendDTMF(context.Context, string, agentclient.DTMFRequest) (agentclient.Call, error)
	SendMessage(context.Context, agentclient.SendMessageRequest) (agentclient.Message, error)
}

type Repository interface {
	ApplyHardwareSnapshot(context.Context, store.HardwareSnapshot) error
	UpsertHardwareMessage(context.Context, store.HardwareMessage) (store.Message, bool, error)
	UpsertHardwareCall(context.Context, store.HardwareCall) (store.Call, error)
	CallControlTarget(context.Context, string) (store.CallControlTarget, error)
	ActiveCalls(context.Context) ([]store.Call, error)
}

type Status struct {
	Connected    bool
	Revision     uint64
	ObservedAt   time.Time
	LastError    string
	AgentVersion string
	ProviderName string
	Lines        []store.LineSummary
}

type SendMessageInput struct {
	RequestID string
	LineID    string
	Number    string
	Text      string
}

type StartCallInput struct {
	RequestID string
	LineID    string
	Number    string
}

type CallActionInput struct {
	RequestID string
	CallID    string
	Action    string
	Digits    string
}

type Service struct {
	agent      Agent
	repository Repository
	random     io.Reader
	now        func() time.Time

	mu           sync.RWMutex
	status       Status
	lastSnapshot agentclient.Snapshot
}

func New(agent Agent, repository Repository) (*Service, error) {
	if agent == nil {
		return nil, operationError(CodeInvalidArgument, "create communication service", "host agent is required", nil)
	}
	if repository == nil {
		return nil, operationError(CodeInvalidArgument, "create communication service", "repository is required", nil)
	}
	return &Service{
		agent:      agent,
		repository: repository,
		random:     rand.Reader,
		now:        time.Now,
	}, nil
}

func (s *Service) Refresh(ctx context.Context) (Status, error) {
	refreshContext, cancel := context.WithTimeout(normalizeContext(ctx), snapshotTimeout)
	defer cancel()
	health, err := s.agent.Health(refreshContext)
	if err != nil {
		return s.recordRefreshFailure("read host agent health", err)
	}
	if health.APIVersion != agentclient.APIVersion {
		return s.recordRefreshFailure(
			"check host agent version",
			fmt.Errorf("agent API %q does not match %q", health.APIVersion, agentclient.APIVersion),
		)
	}
	if !health.Provider.Available {
		return s.recordRefreshFailure("read host agent health", errors.New("ModemManager is unavailable"))
	}
	snapshot, err := s.agent.Snapshot(refreshContext)
	if err != nil {
		return s.recordRefreshFailure("read host agent snapshot", err)
	}
	if snapshot.ObservedAt.IsZero() {
		return s.recordRefreshFailure("read host agent snapshot", errors.New("snapshot observed_at is missing"))
	}

	hardwareSnapshot, lines := projectSnapshot(snapshot)
	if err := s.repository.ApplyHardwareSnapshot(refreshContext, hardwareSnapshot); err != nil {
		return s.recordRefreshFailure("persist host agent snapshot", err)
	}
	status := Status{
		Connected:    true,
		Revision:     snapshot.Revision,
		ObservedAt:   snapshot.ObservedAt.UTC(),
		AgentVersion: health.AgentVersion,
		ProviderName: health.Provider.Name,
		Lines:        lines,
	}
	s.mu.Lock()
	s.status = cloneStatus(status)
	s.lastSnapshot = snapshot
	s.mu.Unlock()
	return cloneStatus(status), nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.RLock()
	status := cloneStatus(s.status)
	s.mu.RUnlock()
	if status.Connected && s.now().UTC().Sub(status.ObservedAt) <= freshSnapshotAge {
		return status, nil
	}
	return s.Refresh(ctx)
}

func (s *Service) Run(ctx context.Context, every time.Duration, report func(error)) {
	if every <= 0 {
		every = defaultSyncEvery
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	lastReportedError := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if _, err := s.Refresh(ctx); err != nil {
				if report != nil && err.Error() != lastReportedError {
					report(err)
				}
				lastReportedError = err.Error()
			} else {
				lastReportedError = ""
			}
			timer.Reset(every)
		}
	}
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (store.Message, error) {
	const operation = "send message"
	status, err := s.Status(ctx)
	if err != nil {
		return store.Message{}, operationError(CodeUnavailable, operation, "live lines are unavailable", err)
	}
	line, err := resolveLine(status.Lines, input.LineID, func(capabilities store.LineCapabilities) bool {
		return capabilities.SendMessage
	})
	if err != nil {
		return store.Message{}, fmt.Errorf("%s: %w", operation, err)
	}
	_, number, err := phone.Normalize(input.Number)
	if err != nil {
		return store.Message{}, operationError(CodeInvalidArgument, operation, "recipient must be an international phone number", err)
	}
	text := strings.TrimSpace(input.Text)
	if text == "" || utf8.RuneCountInString(text) > maxMessageRunes {
		return store.Message{}, operationError(CodeInvalidArgument, operation, "message must contain 1 to 1600 characters", nil)
	}
	requestID, err := s.requestID(input.RequestID)
	if err != nil {
		return store.Message{}, operationError(CodeInvalidArgument, operation, "request id is invalid", err)
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	result, err := s.agent.SendMessage(commandContext, agentclient.SendMessageRequest{
		RequestID: requestID,
		LineID:    line.ID,
		Number:    number,
		Text:      text,
	})
	if err != nil {
		return store.Message{}, translateAgentError(operation, err)
	}
	result.LineID = firstNonEmpty(result.LineID, line.ID)
	result.Number = firstNonEmpty(result.Number, number)
	result.Text = firstNonEmpty(result.Text, text)
	result.Direction = firstNonEmpty(result.Direction, "outgoing")
	stored, _, err := s.repository.UpsertHardwareMessage(commandContext, projectMessage(
		result,
		line,
		requestID,
		text,
		s.now().UTC(),
		1,
	))
	if err != nil {
		return store.Message{}, operationError(
			CodeInternal,
			operation,
			"message was accepted by the modem but could not be recorded",
			err,
		)
	}
	return stored, nil
}

func (s *Service) StartCall(ctx context.Context, input StartCallInput) (store.Call, error) {
	const operation = "start call"
	status, err := s.Status(ctx)
	if err != nil {
		return store.Call{}, operationError(CodeUnavailable, operation, "live lines are unavailable", err)
	}
	line, err := resolveLine(status.Lines, input.LineID, func(capabilities store.LineCapabilities) bool {
		return capabilities.Dial
	})
	if err != nil {
		return store.Call{}, fmt.Errorf("%s: %w", operation, err)
	}
	_, number, err := phone.Normalize(input.Number)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "destination must be an international phone number", err)
	}
	active, err := s.repository.ActiveCalls(ctx)
	if err != nil {
		return store.Call{}, operationError(CodeInternal, operation, "cannot inspect active calls", err)
	}
	for _, call := range active {
		if call.DeviceID == line.ID {
			return store.Call{}, operationError(CodeConflict, operation, "this line already has an active call", nil)
		}
	}
	requestID, err := s.requestID(input.RequestID)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "request id is invalid", err)
	}
	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	result, err := s.agent.StartCall(commandContext, agentclient.StartCallRequest{
		RequestID: requestID,
		LineID:    line.ID,
		Number:    number,
	})
	if err != nil {
		return store.Call{}, translateAgentError(operation, err)
	}
	result.LineID = firstNonEmpty(result.LineID, line.ID)
	result.Number = firstNonEmpty(result.Number, number)
	result.Direction = firstNonEmpty(result.Direction, "outgoing")
	call := projectCall(result, requestID, s.now().UTC(), int64(status.Revision)+1)
	stored, err := s.repository.UpsertHardwareCall(commandContext, call)
	if err != nil {
		return store.Call{}, operationError(
			CodeInternal,
			operation,
			"call was accepted by the modem but could not be recorded",
			err,
		)
	}
	return stored, nil
}

func (s *Service) CallAction(ctx context.Context, input CallActionInput) (store.Call, error) {
	const operation = "control call"
	target, err := s.repository.CallControlTarget(ctx, input.CallID)
	if errors.Is(err, store.ErrCallNotFound) {
		return store.Call{}, operationError(CodeNotFound, operation, "call was not found", err)
	}
	if err != nil {
		return store.Call{}, operationError(CodeInternal, operation, "cannot load call", err)
	}
	if target.Phase == "ended" || target.Phase == "failed" {
		return store.Call{}, operationError(CodeConflict, operation, "call has already ended", nil)
	}
	requestID, err := s.requestID(input.RequestID)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "request id is invalid", err)
	}
	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()

	var result agentclient.Call
	switch strings.ToLower(strings.TrimSpace(input.Action)) {
	case "answer", "reject", "hangup":
		result, err = s.agent.CallAction(
			commandContext,
			target.EndpointCallID,
			strings.ToLower(strings.TrimSpace(input.Action)),
			agentclient.CallActionRequest{RequestID: requestID},
		)
	case "dtmf":
		digits := strings.TrimSpace(input.Digits)
		if !validDTMF(digits) {
			return store.Call{}, operationError(CodeInvalidArgument, operation, "DTMF digits are invalid", nil)
		}
		result, err = s.agent.SendDTMF(commandContext, target.EndpointCallID, agentclient.DTMFRequest{
			RequestID: requestID,
			Digits:    digits,
		})
	default:
		return store.Call{}, operationError(CodeInvalidArgument, operation, "call action is invalid", nil)
	}
	if err != nil {
		return store.Call{}, translateAgentError(operation, err)
	}
	result.ID = firstNonEmpty(result.ID, target.EndpointCallID)
	result.LineID = firstNonEmpty(result.LineID, target.LineID)
	result.Number = firstNonEmpty(result.Number, target.Number)
	result.Direction = firstNonEmpty(result.Direction, target.Direction)
	result.State = firstNonEmpty(result.State, target.Phase)
	result.Bearer = firstNonEmpty(result.Bearer, target.Bearer)
	projected := projectCall(result, "", s.now().UTC(), target.Revision+1)
	projected.AppID = target.AppID
	stored, err := s.repository.UpsertHardwareCall(commandContext, projected)
	if err != nil {
		return store.Call{}, operationError(CodeInternal, operation, "call state could not be recorded", err)
	}
	return stored, nil
}

func (s *Service) ActiveCalls(ctx context.Context) ([]store.Call, error) {
	if _, err := s.Status(ctx); err != nil {
		return nil, operationError(CodeUnavailable, "list active calls", "live call state is unavailable", err)
	}
	calls, err := s.repository.ActiveCalls(ctx)
	if err != nil {
		return nil, operationError(CodeInternal, "list active calls", "active calls could not be loaded", err)
	}
	return calls, nil
}

func (s *Service) recordRefreshFailure(operation string, cause error) (Status, error) {
	s.mu.Lock()
	s.status.Connected = false
	s.status.LastError = cause.Error()
	status := cloneStatus(s.status)
	s.mu.Unlock()
	return status, operationError(CodeUnavailable, operation, "live hardware state is unavailable", cause)
}

func (s *Service) requestID(supplied string) (string, error) {
	supplied = strings.TrimSpace(supplied)
	if supplied != "" {
		if len(supplied) > maxRequestIDLen {
			return "", errors.New("request id is too long")
		}
		for _, character := range supplied {
			if !(character >= 'a' && character <= 'z') &&
				!(character >= 'A' && character <= 'Z') &&
				!(character >= '0' && character <= '9') &&
				character != '-' && character != '_' {
				return "", errors.New("request id contains invalid characters")
			}
		}
		return supplied, nil
	}
	var value [16]byte
	if _, err := io.ReadFull(s.random, value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return hex.EncodeToString(value[:]), nil
}

func projectSnapshot(snapshot agentclient.Snapshot) (store.HardwareSnapshot, []store.LineSummary) {
	lines := make([]store.LineSummary, 0, len(snapshot.Lines))
	hardwareLines := make([]store.HardwareLine, 0, len(snapshot.Lines))
	lineIndex := make(map[string]store.LineSummary, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		projected := projectLine(line)
		lines = append(lines, projected)
		lineIndex[line.ID] = projected
		hardwareLines = append(hardwareLines, store.HardwareLine{
			ID:                  line.ID,
			Manufacturer:        line.Manufacturer,
			Model:               line.Model,
			Firmware:            line.Revision,
			DeviceIdentifier:    line.DeviceIdentifier,
			EquipmentIdentifier: line.EquipmentIdentifier,
			PhysicalDevice:      line.PhysicalDevice,
			PrimaryPort:         line.PrimaryPort,
			State:               line.State,
			SignalKnown:         line.SignalQualityKnown,
			SignalQuality:       line.SignalQuality,
			PhoneNumber:         firstString(line.OwnNumbers),
			ICCID:               line.SIMIdentifier,
			IMSI:                line.IMSI,
			Operator:            firstNonEmpty(line.OperatorName, line.OperatorIdentifier),
			Capabilities:        projected.Capabilities,
		})
	}

	revision := snapshot.Revision
	if revision == 0 {
		revision = 1
	}
	calls := make([]store.HardwareCall, 0, len(snapshot.Calls))
	for _, call := range snapshot.Calls {
		calls = append(calls, projectCall(call, "", snapshot.ObservedAt, int64(revision)))
	}
	messages := make([]store.HardwareMessage, 0, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		line, found := lineIndex[message.LineID]
		if !found || strings.TrimSpace(message.Number) == "" || strings.TrimSpace(message.Text) == "" {
			continue
		}
		messages = append(messages, projectMessage(
			message,
			line,
			"",
			message.Text,
			snapshot.ObservedAt,
			int64(revision),
		))
	}
	return store.HardwareSnapshot{
		Revision:   snapshot.Revision,
		ObservedAt: snapshot.ObservedAt.UTC(),
		Lines:      hardwareLines,
		Calls:      calls,
		Messages:   messages,
	}, lines
}

func projectLine(line agentclient.Line) store.LineSummary {
	var signal *uint32
	if line.SignalQualityKnown {
		value := line.SignalQuality
		signal = &value
	}
	return store.LineSummary{
		ID:          line.ID,
		ICCID:       line.SIMIdentifier,
		IMSI:        line.IMSI,
		PhoneNumber: firstString(line.OwnNumbers),
		Operator:    firstNonEmpty(line.OperatorName, line.OperatorIdentifier),
		DeviceIMEI:  firstNonEmpty(line.EquipmentIdentifier, line.DeviceIdentifier),
		DeviceAlias: line.Model,
		Model:       line.Model,
		Firmware:    line.Revision,
		State:       line.State,
		Signal:      signal,
		Capabilities: store.LineCapabilities{
			Dial:        line.Capabilities.Dial,
			AnswerCall:  line.Capabilities.AnswerCall,
			HangupCall:  line.Capabilities.HangupCall,
			RejectCall:  line.Capabilities.RejectCall,
			SendDTMF:    line.Capabilities.SendDTMF,
			SendMessage: line.Capabilities.SendMessage,
			Media:       line.Capabilities.Media,
		},
	}
}

func projectMessage(
	message agentclient.Message,
	line store.LineSummary,
	requestID string,
	text string,
	observedAt time.Time,
	revision int64,
) store.HardwareMessage {
	timestamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(message.Timestamp))
	if err != nil {
		timestamp = observedAt
	}
	return store.HardwareMessage{
		RequestID:         requestID,
		LineID:            line.ID,
		EndpointMessageID: message.ID,
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		LocalPhone:        line.PhoneNumber,
		Number:            message.Number,
		Text:              text,
		Direction:         normalizeMessageDirection(message.Direction),
		State:             message.State,
		StateCode:         int64(message.StateCode),
		Revision:          revision,
		Timestamp:         timestamp,
		ObservedAt:        observedAt,
	}
}

func projectCall(call agentclient.Call, requestID string, observedAt time.Time, revision int64) store.HardwareCall {
	return store.HardwareCall{
		AppID:          stableInstanceID("call", call.LineID, call.ID),
		RequestID:      requestID,
		LineID:         call.LineID,
		EndpointCallID: call.ID,
		Number:         call.Number,
		Direction:      normalizeCallDirection(call.Direction, call.State),
		Phase:          callPhase(call.State),
		Bearer:         call.Bearer,
		Revision:       revision,
		ObservedAt:     observedAt,
	}
}

func stableInstanceID(prefix string, lineID string, endpointID string) string {
	digest := sha256.Sum256([]byte(lineID + "\x00" + endpointID))
	return prefix + "_" + hex.EncodeToString(digest[:16])
}

func resolveLine(
	lines []store.LineSummary,
	selector string,
	supported func(store.LineCapabilities) bool,
) (store.LineSummary, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return store.LineSummary{}, operationError(CodeInvalidArgument, "select line", "line_id is required", nil)
	}
	for _, line := range lines {
		if selector != line.ID && selector != line.ICCID {
			continue
		}
		if !supported(line.Capabilities) {
			return store.LineSummary{}, operationError(CodeNotSupported, "select line", "selected line does not support this operation", nil)
		}
		return line, nil
	}
	return store.LineSummary{}, operationError(CodeNotFound, "select line", "selected line is not attached", nil)
}

func callPhase(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "dialing", "ringing-out":
		return "dialing"
	case "ringing", "ringing-in", "waiting":
		return "ringing"
	case "connecting":
		return "connecting"
	case "active", "held":
		return "active"
	case "ending":
		return "ending"
	case "terminated", "ended":
		return "ended"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

func normalizeCallDirection(direction, state string) string {
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "incoming" || direction == "outgoing" {
		return direction
	}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ringing-in", "waiting":
		return "incoming"
	default:
		return "outgoing"
	}
}

func normalizeMessageDirection(direction string) string {
	if strings.EqualFold(strings.TrimSpace(direction), "incoming") {
		return "incoming"
	}
	return "outgoing"
}

func validDTMF(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			character != '*' && character != '#' &&
			character != 'A' && character != 'B' && character != 'C' && character != 'D' {
			return false
		}
	}
	return true
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneStatus(status Status) Status {
	status.Lines = append([]store.LineSummary(nil), status.Lines...)
	return status
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
