package communication

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/modemidentity"
	"github.com/human-agent65535/modemdeck/internal/phone"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	snapshotTimeout            = 5 * time.Second
	commandTimeout             = 20 * time.Second
	defaultSyncEvery           = 30 * time.Second
	freshSnapshotAge           = 35 * time.Second
	eventReconnectDelay        = 2 * time.Second
	agentEventCoalesceDelay    = 250 * time.Millisecond
	maxMessageRunes            = 1600
	maxRequestIDLen            = 128
	maxIncomingCallActions     = 8
	incomingCallActionTimeout  = 5 * time.Second
	deviceConfigurationTimeout = 50 * time.Second
	controlLeaseRenewInterval  = time.Second
	controlLeaseRequestTimeout = 5 * time.Second
	deviceMessageDeleteTimeout = 5 * time.Second
)

type Agent interface {
	Health(context.Context) (agentclient.Health, error)
	Snapshot(context.Context) (agentclient.Snapshot, error)
	StartCall(context.Context, agentclient.StartCallRequest) (agentclient.CommandReceipt, error)
	CallAction(context.Context, string, string, agentclient.CallActionRequest) (agentclient.CommandReceipt, error)
	SendDTMF(context.Context, string, agentclient.DTMFRequest) (agentclient.CommandReceipt, error)
	SendMessage(context.Context, agentclient.SendMessageRequest) (agentclient.CommandReceipt, error)
	DeviceConfiguration(context.Context, string) (agentclient.DeviceConfiguration, error)
	ApplyDeviceConfiguration(
		context.Context,
		string,
		agentclient.ApplyDeviceConfigurationRequest,
	) (agentclient.DeviceConfiguration, error)
}

type AgentChangeSource interface {
	WatchChanges(context.Context, func()) error
}

type AgentControlLease interface {
	RenewControlLease(context.Context) (agentclient.ControlLeaseStatus, error)
	ReleaseControlLease(context.Context) error
}

type AgentMessageDeleter interface {
	DeleteMessage(context.Context, string) error
}

type Repository interface {
	ApplyHardwareSnapshotWithResult(
		context.Context,
		store.HardwareSnapshot,
	) (store.HardwareSnapshotResult, error)
	Lines(context.Context) ([]store.LineSummary, error)
	UpsertHardwareMessage(context.Context, store.HardwareMessage) (store.Message, bool, error)
	UpsertHardwareCall(context.Context, store.HardwareCall) (store.Call, error)
	BeginHardwareCommand(context.Context, string, string, []byte) (store.HardwareCommand, bool, error)
	FinishHardwareCommand(context.Context, string, string, string, string) error
	MessageByRequestID(context.Context, string) (store.Message, error)
	CallByRequestID(context.Context, string) (store.Call, error)
	CallByID(context.Context, string) (store.Call, error)
	CallControlTarget(context.Context, string) (store.CallControlTarget, error)
	ActiveCalls(context.Context) ([]store.Call, error)
	GlobalCallSettings(context.Context) (store.GlobalCallSettings, error)
	UpdateGlobalCallSettings(context.Context, bool, int64) (store.GlobalCallSettings, error)
	LineCallPolicy(context.Context, string) (store.LineCallPolicy, error)
	UpdateLineCallPolicy(
		context.Context,
		string,
		store.LineCallPolicyValue,
		int64,
	) (store.LineCallPolicy, error)
	MessageDeliveryPolicy(context.Context, string) (store.MessageDeliveryPolicy, error)
	UpdateMessageDeliveryPolicy(
		context.Context,
		string,
		bool,
		int64,
	) (store.MessageDeliveryPolicy, error)
	MarkMessageDeliveryReportsUnsupported(
		context.Context,
		string,
		int64,
	) (store.MessageDeliveryPolicy, error)
	EffectiveCallPolicy(context.Context, string) (store.EffectiveCallPolicy, error)
	CallPolicyConfiguration(context.Context, string) (store.CallPolicyConfiguration, error)
	ClaimIncomingCallActions(context.Context, int) ([]store.IncomingCallAction, error)
	FinishIncomingCallAction(context.Context, string, string, string) error
	LatestIncomingCallAction(context.Context, string) (*store.IncomingCallAction, error)
}

type CallLifecycleObserver interface {
	ReconcileAuthoritativeCalls(context.Context, []store.Call) error
}

type Status struct {
	Connected      bool
	BootEpoch      string
	Revision       string
	ObservedAt     time.Time
	LastError      string
	AgentVersion   string
	ProviderName   string
	RuntimeVersion string
	Capabilities   agentclient.Capabilities
	Lines          []store.LineSummary
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

type runtimeProjection struct {
	linesDigest string
	callsDigest string
}

type Service struct {
	agent      Agent
	repository Repository
	events     messageevents.Publisher
	runtime    runtimeevents.Publisher
	random     io.Reader
	now        func() time.Time

	mu                sync.RWMutex
	status            Status
	lastSnapshot      agentclient.Snapshot
	refreshMu         sync.Mutex
	runtimeProjection runtimeProjection

	lifecycleMu       sync.RWMutex
	lifecycleObserver CallLifecycleObserver

	controlMu             sync.RWMutex
	controlLeaseSupported bool
	agentEventsRequired   bool
	agentEventsHealthy    bool
	controlLeaseWanted    bool
	controlLeaseActive    bool

	messageCleanupMu      sync.Mutex
	messageCleanupPending map[string]struct{}
	messageCleanupRunning bool
}

func New(
	agent Agent,
	repository Repository,
	events messageevents.Publisher,
) (*Service, error) {
	if agent == nil {
		return nil, operationError(CodeInvalidArgument, "create communication service", "host agent is required", nil)
	}
	if repository == nil {
		return nil, operationError(CodeInvalidArgument, "create communication service", "repository is required", nil)
	}
	if events == nil {
		return nil, operationError(CodeInvalidArgument, "create communication service", "message event publisher is required", nil)
	}
	return &Service{
		agent:                 agent,
		repository:            repository,
		events:                events,
		random:                rand.Reader,
		now:                   time.Now,
		messageCleanupPending: make(map[string]struct{}),
	}, nil
}

func (s *Service) SetRuntimeEventPublisher(events runtimeevents.Publisher) error {
	if events == nil {
		return operationError(
			CodeInvalidArgument,
			"configure runtime events",
			"runtime event publisher is required",
			nil,
		)
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.runtime != nil {
		return operationError(
			CodeConflict,
			"configure runtime events",
			"runtime event publisher is already configured",
			nil,
		)
	}
	s.runtime = events
	return nil
}

func (s *Service) Refresh(ctx context.Context) (Status, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

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
	if strings.TrimSpace(health.Provider.BootEpoch) == "" {
		return s.recordRefreshFailure("read host agent health", errors.New("agent boot_epoch is missing"))
	}
	snapshot, err := s.agent.Snapshot(refreshContext)
	if err != nil {
		return s.recordRefreshFailure("read host agent snapshot", err)
	}
	if snapshot.ObservedAt.IsZero() {
		return s.recordRefreshFailure("read host agent snapshot", errors.New("snapshot observed_at is missing"))
	}
	snapshot, _ = quarantineDuplicateSubscriptionAttachments(snapshot)
	stableLines, err := s.repository.Lines(refreshContext)
	if err != nil {
		return s.recordRefreshFailure("read stable line identities", err)
	}
	snapshot = bindSnapshotHomeCountries(snapshot, stableLines)

	hardwareSnapshot, lines := projectSnapshot(snapshot, health.Provider.BootEpoch)
	snapshotResult, err := s.repository.ApplyHardwareSnapshotWithResult(
		refreshContext,
		hardwareSnapshot,
	)
	if err != nil {
		return s.recordRefreshFailure("persist host agent snapshot", err)
	}
	s.enqueueDeviceMessageCleanup(
		hardwareSnapshot.Messages,
		snapshotResult.HandledDeliveryReportIDs,
	)
	stableLines, err = s.repository.Lines(refreshContext)
	if err != nil {
		return s.recordRefreshFailure("read persisted line identities", err)
	}
	lines = bindProjectedLines(lines, snapshotResult.LineIDsByEndpoint, stableLines)
	s.publishIncomingMessages(snapshotResult.CreatedIncomingMessages, snapshot.ObservedAt)
	activeCalls, err := s.repository.ActiveCalls(refreshContext)
	if err != nil {
		return s.recordRefreshFailure("read authoritative active calls", err)
	}
	status := Status{
		Connected:      true,
		BootEpoch:      health.Provider.BootEpoch,
		Revision:       snapshot.Revision,
		ObservedAt:     snapshot.ObservedAt.UTC(),
		AgentVersion:   health.AgentVersion,
		ProviderName:   health.Provider.Name,
		RuntimeVersion: health.Provider.RuntimeVersion,
		Capabilities:   health.Provider.Capabilities,
		Lines:          lines,
	}
	s.mu.Lock()
	s.status = cloneStatus(status)
	s.lastSnapshot = snapshot
	s.mu.Unlock()
	s.updateAgentControlCapabilities(status.Capabilities)
	s.publishRuntimeSnapshot(status.BootEpoch, snapshot, lines)
	if err := s.reconcileAuthoritativeCalls(refreshContext, activeCalls); err != nil {
		return cloneStatus(status), operationError(
			CodeInternal,
			"reconcile call lifecycle",
			"call resources could not be reconciled",
			err,
		)
	}
	if err := s.processIncomingCallActions(ctx, snapshot); err != nil {
		return cloneStatus(status), operationError(
			CodeInternal,
			"apply incoming call policy",
			"incoming call policy outcome could not be recorded",
			err,
		)
	}
	return cloneStatus(status), nil
}

func (s *Service) enqueueDeviceMessageCleanup(
	messages []store.HardwareMessage,
	deliveryReportIDs []string,
) {
	deleter, available := s.agent.(AgentMessageDeleter)
	if !available {
		return
	}
	s.messageCleanupMu.Lock()
	for _, message := range messages {
		state := strings.ToLower(strings.TrimSpace(message.State))
		if state != "received" && state != "sent" {
			continue
		}
		if messageID := strings.TrimSpace(message.EndpointMessageID); messageID != "" {
			s.messageCleanupPending[messageID] = struct{}{}
		}
	}
	for _, reportID := range deliveryReportIDs {
		if reportID = strings.TrimSpace(reportID); reportID != "" {
			s.messageCleanupPending[reportID] = struct{}{}
		}
	}
	if s.messageCleanupRunning || len(s.messageCleanupPending) == 0 {
		s.messageCleanupMu.Unlock()
		return
	}
	s.messageCleanupRunning = true
	s.messageCleanupMu.Unlock()
	go s.drainDeviceMessageCleanup(deleter)
}

func (s *Service) drainDeviceMessageCleanup(deleter AgentMessageDeleter) {
	for {
		s.messageCleanupMu.Lock()
		messageID := ""
		for pendingID := range s.messageCleanupPending {
			messageID = pendingID
			break
		}
		if messageID == "" {
			s.messageCleanupRunning = false
			s.messageCleanupMu.Unlock()
			return
		}
		s.messageCleanupMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), deviceMessageDeleteTimeout)
		err := deleter.DeleteMessage(ctx, messageID)
		cancel()

		s.messageCleanupMu.Lock()
		delete(s.messageCleanupPending, messageID)
		if err != nil {
			clear(s.messageCleanupPending)
			s.messageCleanupRunning = false
			s.messageCleanupMu.Unlock()
			slog.Warn(
				"persisted SMS could not be removed from the modem",
				"component", "communication",
				"message_id", messageID,
				"error", err,
			)
			return
		}
		s.messageCleanupMu.Unlock()
	}
}

func (s *Service) publishRuntimeSnapshot(
	bootEpoch string,
	snapshot agentclient.Snapshot,
	lines []store.LineSummary,
) {
	if s.runtime == nil {
		return
	}
	previous := s.runtimeProjection
	current := previous
	if digest, ok := runtimeProjectionDigest(struct {
		BootEpoch string              `json:"boot_epoch"`
		Lines     []store.LineSummary `json:"lines"`
	}{
		BootEpoch: strings.TrimSpace(bootEpoch),
		Lines:     canonicalRuntimeLines(lines),
	}); ok {
		current.linesDigest = digest
		if digest != previous.linesDigest {
			s.runtime.Publish(runtimeevents.Event{
				Resources:  []runtimeevents.Resource{runtimeevents.ResourceLines},
				ObservedAt: snapshot.ObservedAt,
			})
		}
	}
	if digest, ok := runtimeProjectionDigest(struct {
		BootEpoch string             `json:"boot_epoch"`
		Calls     []agentclient.Call `json:"calls"`
	}{
		BootEpoch: strings.TrimSpace(bootEpoch),
		Calls:     canonicalRuntimeCalls(snapshot.Calls),
	}); ok {
		current.callsDigest = digest
		if digest != previous.callsDigest {
			s.runtime.Publish(runtimeevents.Event{
				Resources:  []runtimeevents.Resource{runtimeevents.ResourceCalls},
				ObservedAt: snapshot.ObservedAt,
			})
		}
	}
	s.runtimeProjection = current
}

func runtimeProjectionDigest(payload any) (string, bool) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), true
}

func canonicalRuntimeLines(lines []store.LineSummary) []store.LineSummary {
	canonical := append([]store.LineSummary(nil), lines...)
	for index := range canonical {
		canonical[index].Ports = append([]store.HardwarePort(nil), canonical[index].Ports...)
		slices.SortFunc(canonical[index].Ports, func(left, right store.HardwarePort) int {
			if comparison := strings.Compare(left.Name, right.Name); comparison != 0 {
				return comparison
			}
			if comparison := strings.Compare(left.Type, right.Type); comparison != 0 {
				return comparison
			}
			return cmp.Compare(left.TypeCode, right.TypeCode)
		})
	}
	slices.SortFunc(canonical, func(left, right store.LineSummary) int {
		return strings.Compare(left.ID, right.ID)
	})
	return canonical
}

func canonicalRuntimeCalls(calls []agentclient.Call) []agentclient.Call {
	canonical := append([]agentclient.Call(nil), calls...)
	slices.SortFunc(canonical, func(left, right agentclient.Call) int {
		if comparison := strings.Compare(left.LineID, right.LineID); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ID, right.ID)
	})
	return canonical
}

func (s *Service) publishIncomingMessages(messages []store.Message, observedAt time.Time) {
	for _, message := range messages {
		messageID := strconv.FormatInt(message.ID, 10)
		s.events.Publish(messageevents.IncomingSMS{
			EventKey:  "sms:" + messageID,
			MessageID: messageID,
			ThreadKey: store.MessageThreadKey(
				message.LineID,
				message.Peer,
			),
			LineID:     message.LineID,
			ICCID:      message.ICCID,
			Peer:       message.Peer,
			Content:    message.Content,
			Timestamp:  message.Timestamp,
			ObservedAt: observedAt,
		})
	}
}

func (s *Service) SetCallLifecycleObserver(observer CallLifecycleObserver) error {
	if observer == nil {
		return operationError(
			CodeInvalidArgument,
			"configure call lifecycle observer",
			"call lifecycle observer is required",
			nil,
		)
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.lifecycleObserver != nil {
		return operationError(
			CodeConflict,
			"configure call lifecycle observer",
			"call lifecycle observer is already configured",
			nil,
		)
	}
	s.lifecycleObserver = observer
	return nil
}

func (s *Service) reconcileAuthoritativeCalls(
	ctx context.Context,
	activeCalls []store.Call,
) error {
	s.lifecycleMu.RLock()
	observer := s.lifecycleObserver
	s.lifecycleMu.RUnlock()
	if observer == nil {
		return nil
	}
	return observer.ReconcileAuthoritativeCalls(ctx, append([]store.Call(nil), activeCalls...))
}

func (s *Service) GlobalCallSettings(ctx context.Context) (store.GlobalCallSettings, error) {
	settings, err := s.repository.GlobalCallSettings(normalizeContext(ctx))
	if err != nil {
		return store.GlobalCallSettings{}, operationError(
			CodeInternal,
			"read global call settings",
			"global call settings could not be read",
			err,
		)
	}
	return settings, nil
}

func (s *Service) UpdateGlobalCallSettings(
	ctx context.Context,
	receiveCalls bool,
	expectedRevision int64,
) (store.GlobalCallSettings, error) {
	settings, err := s.repository.UpdateGlobalCallSettings(
		normalizeContext(ctx),
		receiveCalls,
		expectedRevision,
	)
	if errors.Is(err, store.ErrRevisionConflict) {
		return store.GlobalCallSettings{}, operationError(
			CodeConflict,
			"update global call settings",
			"call settings changed; read the latest revision before updating",
			err,
		)
	}
	if err != nil {
		return store.GlobalCallSettings{}, operationError(
			CodeInternal,
			"update global call settings",
			"global call settings could not be updated",
			err,
		)
	}
	return settings, nil
}

func (s *Service) LineCallPolicy(
	ctx context.Context,
	lineID string,
) (store.LineCallPolicy, error) {
	policy, err := s.repository.LineCallPolicy(normalizeContext(ctx), strings.TrimSpace(lineID))
	if errors.Is(err, store.ErrInvalidCallPolicy) {
		return store.LineCallPolicy{}, operationError(
			CodeInvalidArgument,
			"read line call policy",
			"line_id is required",
			err,
		)
	}
	if err != nil {
		return store.LineCallPolicy{}, operationError(
			CodeInternal,
			"read line call policy",
			"line call policy could not be read",
			err,
		)
	}
	return policy, nil
}

func (s *Service) UpdateLineCallPolicy(
	ctx context.Context,
	lineID string,
	policy store.LineCallPolicyValue,
	expectedRevision int64,
) (store.LineCallPolicy, error) {
	updated, err := s.repository.UpdateLineCallPolicy(
		normalizeContext(ctx),
		strings.TrimSpace(lineID),
		policy,
		expectedRevision,
	)
	switch {
	case errors.Is(err, store.ErrInvalidCallPolicy):
		return store.LineCallPolicy{}, operationError(
			CodeInvalidArgument,
			"update line call policy",
			"policy must be follow_global, receive, or do_not_disturb",
			err,
		)
	case errors.Is(err, store.ErrRevisionConflict):
		return store.LineCallPolicy{}, operationError(
			CodeConflict,
			"update line call policy",
			"line call policy changed; read the latest revision before updating",
			err,
		)
	case err != nil:
		return store.LineCallPolicy{}, operationError(
			CodeInternal,
			"update line call policy",
			"line call policy could not be updated",
			err,
		)
	default:
		return updated, nil
	}
}

func (s *Service) MessageDeliveryPolicy(
	ctx context.Context,
	lineID string,
) (store.MessageDeliveryPolicy, error) {
	policy, err := s.repository.MessageDeliveryPolicy(
		normalizeContext(ctx),
		strings.TrimSpace(lineID),
	)
	if errors.Is(err, store.ErrInvalidMessagePolicy) {
		return store.MessageDeliveryPolicy{}, operationError(
			CodeInvalidArgument,
			"read message delivery policy",
			"line_id is invalid",
			err,
		)
	}
	if err != nil {
		return store.MessageDeliveryPolicy{}, operationError(
			CodeInternal,
			"read message delivery policy",
			"message delivery policy could not be read",
			err,
		)
	}
	return policy, nil
}

func (s *Service) UpdateMessageDeliveryPolicy(
	ctx context.Context,
	lineID string,
	enabled bool,
	expectedRevision int64,
) (store.MessageDeliveryPolicy, error) {
	policy, err := s.repository.UpdateMessageDeliveryPolicy(
		normalizeContext(ctx),
		strings.TrimSpace(lineID),
		enabled,
		expectedRevision,
	)
	switch {
	case errors.Is(err, store.ErrInvalidMessagePolicy):
		return store.MessageDeliveryPolicy{}, operationError(
			CodeInvalidArgument,
			"update message delivery policy",
			"line_id is invalid",
			err,
		)
	case errors.Is(err, store.ErrRevisionConflict):
		return store.MessageDeliveryPolicy{}, operationError(
			CodeConflict,
			"update message delivery policy",
			"message delivery policy changed; read the latest revision before updating",
			err,
		)
	case err != nil:
		return store.MessageDeliveryPolicy{}, operationError(
			CodeInternal,
			"update message delivery policy",
			"message delivery policy could not be updated",
			err,
		)
	default:
		return policy, nil
	}
}

func (s *Service) EffectiveCallPolicy(
	ctx context.Context,
	lineID string,
) (store.EffectiveCallPolicy, error) {
	effective, err := s.repository.EffectiveCallPolicy(
		normalizeContext(ctx),
		strings.TrimSpace(lineID),
	)
	if err != nil {
		return store.EffectiveCallPolicy{}, operationError(
			CodeInternal,
			"read effective call policy",
			"effective call policy could not be read",
			err,
		)
	}
	return effective, nil
}

func (s *Service) CallPolicyConfiguration(
	ctx context.Context,
	lineID string,
) (store.CallPolicyConfiguration, error) {
	configuration, err := s.repository.CallPolicyConfiguration(
		normalizeContext(ctx),
		strings.TrimSpace(lineID),
	)
	if err != nil {
		return store.CallPolicyConfiguration{}, operationError(
			CodeInternal,
			"read call policy configuration",
			"call policy configuration could not be read",
			err,
		)
	}
	return configuration, nil
}

func (s *Service) LatestIncomingCallAction(
	ctx context.Context,
	lineID string,
) (*store.IncomingCallAction, error) {
	action, err := s.repository.LatestIncomingCallAction(
		normalizeContext(ctx),
		strings.TrimSpace(lineID),
	)
	if err != nil {
		return nil, operationError(
			CodeInternal,
			"read incoming call policy outcome",
			"incoming call policy outcome could not be read",
			err,
		)
	}
	return action, nil
}

func (s *Service) processIncomingCallActions(
	ctx context.Context,
	snapshot agentclient.Snapshot,
) error {
	claimContext, claimCancel := durableContext(ctx)
	actions, err := s.repository.ClaimIncomingCallActions(
		claimContext,
		maxIncomingCallActions,
	)
	claimCancel()
	if err != nil {
		return err
	}
	for _, action := range actions {
		status, errorCode := s.executeIncomingCallAction(ctx, snapshot, action)
		finishContext, finishCancel := durableContext(ctx)
		err := s.repository.FinishIncomingCallAction(
			finishContext,
			action.CallID,
			status,
			errorCode,
		)
		finishCancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) executeIncomingCallAction(
	ctx context.Context,
	snapshot agentclient.Snapshot,
	action store.IncomingCallAction,
) (string, string) {
	if action.EffectivePolicy != store.EffectiveCallPolicyDND {
		return store.IncomingCallActionSkipped, "policy_not_dnd"
	}
	call, found := currentIncomingRingingCall(
		snapshot,
		action.EndpointLineID,
		action.EndpointCallID,
	)
	if !found {
		return store.IncomingCallActionSkipped, "call_no_longer_ringing"
	}
	if !lineCanRejectCall(snapshot, call.LineID) {
		return store.IncomingCallActionSkipped, "reject_not_supported"
	}

	commandContext, cancel := context.WithTimeout(
		context.WithoutCancel(normalizeContext(ctx)),
		incomingCallActionTimeout,
	)
	defer cancel()
	receipt, err := s.agent.CallAction(
		commandContext,
		action.EndpointCallID,
		"reject",
		agentclient.CallActionRequest{RequestID: action.RequestID},
	)
	if err != nil {
		var operationError *agentclient.OperationError
		if errors.As(err, &operationError) {
			code := strings.TrimSpace(operationError.Code)
			if code == "" {
				code = "agent_rejected"
			} else {
				code = "agent_" + code
			}
			return store.IncomingCallActionFailed, boundedActionErrorCode(code)
		}
		return store.IncomingCallActionIndeterminate, "transport_indeterminate"
	}
	if err := validateReceipt(receipt, action.RequestID); err != nil {
		return store.IncomingCallActionIndeterminate, "invalid_receipt"
	}
	return store.IncomingCallActionSucceeded, ""
}

func currentIncomingRingingCall(
	snapshot agentclient.Snapshot,
	lineID string,
	endpointCallID string,
) (agentclient.Call, bool) {
	for _, call := range snapshot.Calls {
		if call.LineID != lineID || call.ID != endpointCallID {
			continue
		}
		if normalizeCallDirection(call.Direction, call.State) != "incoming" {
			return agentclient.Call{}, false
		}
		if callPhase(call.State) != "ringing" {
			return agentclient.Call{}, false
		}
		return call, true
	}
	return agentclient.Call{}, false
}

func lineCanRejectCall(snapshot agentclient.Snapshot, lineID string) bool {
	for _, line := range snapshot.Lines {
		if line.ID == lineID {
			return line.Capabilities.RejectCall
		}
	}
	return false
}

func boundedActionErrorCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 128 {
		return value
	}
	return value[:128]
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
	timer := time.NewTimer(every)
	defer timer.Stop()
	controlLeaseTicker := time.NewTicker(controlLeaseRenewInterval)
	defer controlLeaseTicker.Stop()
	changeEvents := make(chan struct{}, 1)
	var changeTimer *time.Timer
	var changeTimerC <-chan time.Time
	watchFailures := make(chan error, 1)
	watchStates := make(chan bool)
	var watchCancel context.CancelFunc
	defer func() {
		if changeTimer != nil {
			changeTimer.Stop()
		}
		if watchCancel != nil {
			watchCancel()
		}
	}()
	lastRefreshError := ""
	lastWatchError := ""
	lastControlLeaseError := ""
	watchStarted := false

	reportDistinct := func(err error, previous *string) {
		if err == nil {
			*previous = ""
			return
		}
		if report != nil && err.Error() != *previous {
			report(err)
		}
		*previous = err.Error()
	}
	refresh := func() (Status, bool) {
		status, err := s.Refresh(ctx)
		reportDistinct(err, &lastRefreshError)
		return status, err == nil
	}
	startWatcher := func(status Status) {
		if watchStarted || !status.Capabilities.Events {
			return
		}
		source, ok := s.agent.(AgentChangeSource)
		if !ok {
			return
		}
		watchStarted = true
		watchContext, cancel := context.WithCancel(ctx)
		watchCancel = cancel
		go watchAgentChanges(
			watchContext,
			source,
			changeEvents,
			watchFailures,
			watchStates,
		)
	}

	if status, ok := refresh(); ok {
		startWatcher(status)
		if !status.Capabilities.Events {
			reportDistinct(s.renewAgentControlLease(ctx), &lastControlLeaseError)
		}
	}
	for {
		select {
		case <-ctx.Done():
			releaseContext, cancel := context.WithTimeout(
				context.Background(),
				controlLeaseRequestTimeout,
			)
			reportDistinct(
				s.releaseAgentControlLease(releaseContext),
				&lastControlLeaseError,
			)
			cancel()
			return
		case healthy := <-watchStates:
			s.setAgentEventsHealthy(healthy)
			if healthy {
				reportDistinct(
					s.renewAgentControlLease(ctx),
					&lastControlLeaseError,
				)
				continue
			}
			releaseContext, cancel := context.WithTimeout(
				context.Background(),
				controlLeaseRequestTimeout,
			)
			reportDistinct(
				s.releaseAgentControlLease(releaseContext),
				&lastControlLeaseError,
			)
			cancel()
		case <-changeEvents:
			if changeTimerC == nil {
				if changeTimer == nil {
					changeTimer = time.NewTimer(agentEventCoalesceDelay)
				} else {
					resetTimer(changeTimer, agentEventCoalesceDelay)
				}
				changeTimerC = changeTimer.C
			}
		case <-changeTimerC:
			changeTimerC = nil
			drainChangeEvents(changeEvents)
			lastWatchError = ""
			if status, ok := refresh(); ok {
				startWatcher(status)
			}
			resetTimer(timer, every)
		case err := <-watchFailures:
			reportDistinct(err, &lastWatchError)
		case <-timer.C:
			if changeTimerC != nil {
				changeTimer.Stop()
				changeTimerC = nil
			}
			drainChangeEvents(changeEvents)
			if status, ok := refresh(); ok {
				startWatcher(status)
			}
			timer.Reset(every)
		case <-controlLeaseTicker.C:
			if s.agentControlLeaseRenewable() {
				reportDistinct(
					s.renewAgentControlLease(ctx),
					&lastControlLeaseError,
				)
			}
		}
	}
}

func drainChangeEvents(events <-chan struct{}) {
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}

func watchAgentChanges(
	ctx context.Context,
	source AgentChangeSource,
	changes chan<- struct{},
	failures chan<- error,
	states chan<- bool,
) {
	for {
		connected := false
		notify := func() {
			if !connected {
				select {
				case states <- true:
					connected = true
				case <-ctx.Done():
					return
				}
			}
			select {
			case changes <- struct{}{}:
			default:
			}
		}
		err := source.WatchChanges(ctx, notify)
		if ctx.Err() != nil {
			return
		}
		if connected {
			select {
			case states <- false:
			case <-ctx.Done():
				return
			}
		}
		if err == nil {
			err = errors.New("host agent event stream ended")
		}
		select {
		case failures <- fmt.Errorf("watch host agent changes: %w", err):
		default:
		}
		timer := time.NewTimer(eventReconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) updateAgentControlCapabilities(
	capabilities agentclient.Capabilities,
) {
	_, available := s.agent.(AgentControlLease)
	s.controlMu.Lock()
	s.controlLeaseSupported = capabilities.ControlLease && available
	s.agentEventsRequired = capabilities.Events
	if !capabilities.Events {
		s.agentEventsHealthy = false
	}
	if !s.controlLeaseSupported {
		s.controlLeaseWanted = false
		s.controlLeaseActive = false
	}
	s.controlMu.Unlock()
}

func (s *Service) setAgentEventsHealthy(healthy bool) {
	s.controlMu.Lock()
	s.agentEventsHealthy = healthy
	s.controlMu.Unlock()
}

func (s *Service) agentControlLeaseRenewable() bool {
	s.controlMu.RLock()
	defer s.controlMu.RUnlock()
	return s.controlLeaseSupported &&
		s.controlLeaseWanted &&
		(!s.agentEventsRequired || s.agentEventsHealthy)
}

func (s *Service) ensureAgentControlLease(ctx context.Context) error {
	lease, available := s.agent.(AgentControlLease)
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	supported := s.controlLeaseSupported
	eventsRequired := s.agentEventsRequired
	eventsHealthy := s.agentEventsHealthy
	if !supported || !available {
		return nil
	}
	if eventsRequired && !eventsHealthy {
		return operationError(
			CodeUnavailable,
			"acquire host agent control",
			"host agent event stream is not healthy",
			nil,
		)
	}
	s.controlLeaseWanted = true
	if err := s.renewAgentControlLeaseLocked(ctx, lease); err != nil {
		s.controlLeaseWanted = false
		s.controlLeaseActive = false
		return operationError(
			CodeUnavailable,
			"acquire host agent control",
			"host agent control lease could not be renewed",
			err,
		)
	}
	return nil
}

func (s *Service) renewAgentControlLease(ctx context.Context) error {
	lease, ok := s.agent.(AgentControlLease)
	if !ok {
		return nil
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	return s.renewAgentControlLeaseLocked(ctx, lease)
}

func (s *Service) renewAgentControlLeaseLocked(
	ctx context.Context,
	lease AgentControlLease,
) error {
	if !s.controlLeaseSupported ||
		!s.controlLeaseWanted ||
		(s.agentEventsRequired && !s.agentEventsHealthy) {
		return nil
	}
	renewContext, cancel := context.WithTimeout(
		normalizeContext(ctx),
		controlLeaseRequestTimeout,
	)
	_, err := lease.RenewControlLease(renewContext)
	cancel()
	if err == nil {
		s.controlLeaseActive = true
	}
	return err
}

func (s *Service) releaseAgentControlLease(ctx context.Context) error {
	lease, ok := s.agent.(AgentControlLease)
	if !ok {
		return nil
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	active := s.controlLeaseActive
	s.controlLeaseWanted = false
	s.controlLeaseActive = false
	if !active {
		return nil
	}
	return lease.ReleaseControlLease(normalizeContext(ctx))
}

func (s *Service) terminateEndpointCall(
	ctx context.Context,
	endpointCallID string,
	reason string,
	cause error,
) error {
	endpointCallID = strings.TrimSpace(endpointCallID)
	if endpointCallID == "" {
		return cause
	}
	requestID := stableInstanceID("end-call", reason, endpointCallID)
	commandContext, cancel := context.WithTimeout(
		context.WithoutCancel(normalizeContext(ctx)),
		commandTimeout,
	)
	receipt, commandErr := s.agent.CallAction(
		commandContext,
		endpointCallID,
		"hangup",
		agentclient.CallActionRequest{RequestID: requestID},
	)
	cancel()
	if commandErr == nil {
		if receiptErr := validateReceipt(receipt, requestID); receiptErr != nil {
			commandErr = receiptErr
		} else if receipt.ResourceID != endpointCallID {
			commandErr = errors.New("call termination receipt resource does not match")
		}
	}
	if commandErr == nil {
		return cause
	}
	return errors.Join(
		cause,
		fmt.Errorf("terminate accepted call %s: %w", endpointCallID, commandErr),
	)
}

func (s *Service) recordIndeterminateAcceptedCall(
	ctx context.Context,
	command store.HardwareCommand,
	endpointCallID string,
	cause error,
) error {
	cause = s.terminateEndpointCall(
		ctx,
		endpointCallID,
		"indeterminate-call",
		cause,
	)
	finishContext, cancel := durableContext(ctx)
	finishErr := s.finishIndeterminateCommand(finishContext, command, cause)
	cancel()
	if finishErr != nil {
		return errors.Join(cause, finishErr)
	}
	return cause
}

func (s *Service) EndCall(ctx context.Context, callID string) error {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return operationError(
			CodeInvalidArgument,
			"end call",
			"call_id is required",
			nil,
		)
	}
	target, err := s.repository.CallControlTarget(normalizeContext(ctx), callID)
	if errors.Is(err, store.ErrCallNotFound) {
		return nil
	}
	if err != nil {
		return operationError(CodeInternal, "end call", "cannot load call", err)
	}
	if target.Phase == "ended" || target.Phase == "failed" {
		return nil
	}
	_, err = s.CallAction(ctx, CallActionInput{
		RequestID: stableInstanceID("end-call", "browser-lease", callID),
		CallID:    callID,
		Action:    "hangup",
	})
	return err
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
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
	address, err := phone.ParseDestination(input.Number, line.HomeCountryISO)
	if err != nil {
		return store.Message{}, operationError(CodeInvalidArgument, operation, "recipient is not valid for the selected line", err)
	}
	number := address.Dial
	text := strings.TrimSpace(input.Text)
	if text == "" || utf8.RuneCountInString(text) > maxMessageRunes {
		return store.Message{}, operationError(CodeInvalidArgument, operation, "message must contain 1 to 1600 characters", nil)
	}
	requestID, err := s.requestID(input.RequestID)
	if err != nil {
		return store.Message{}, operationError(CodeInvalidArgument, operation, "request id is invalid", err)
	}
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"send_message",
		commandDigest("send_message", line.ID, number, text),
	)
	if err != nil {
		return store.Message{}, err
	}
	if replay {
		message, err := s.repository.MessageByRequestID(ctx, requestID)
		if err != nil {
			return store.Message{}, operationError(
				CodeInternal,
				operation,
				"completed message command has no stored result",
				err,
			)
		}
		return message, nil
	}
	deliveryPolicy, err := s.MessageDeliveryPolicy(ctx, line.ID)
	if err != nil {
		return store.Message{}, s.failLocalCommand(
			ctx,
			command,
			operation,
			"message delivery policy could not be loaded",
			err,
		)
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	receipt, err := s.agent.SendMessage(commandContext, agentclient.SendMessageRequest{
		RequestID:               requestID,
		LineID:                  line.EndpointID,
		Number:                  number,
		Text:                    text,
		DeliveryReportRequested: deliveryPolicy.DeliveryReportsEnabled,
	})
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return store.Message{}, finishErr
		}
		return store.Message{}, translateAgentError(operation, err)
	}
	if err := validateReceipt(receipt, requestID); err != nil {
		if finishErr := s.finishIndeterminateCommand(ctx, command, err); finishErr != nil {
			return store.Message{}, finishErr
		}
		return store.Message{}, operationError(CodeUnavailable, operation, "host agent returned an invalid command receipt", err)
	}
	outcomeContext, outcomeCancel := durableContext(ctx)
	defer outcomeCancel()
	deliveryReportRequested := deliveryPolicy.DeliveryReportsEnabled &&
		!receipt.DeliveryReportUnsupported
	if receipt.DeliveryReportUnsupported {
		if _, policyErr := s.repository.MarkMessageDeliveryReportsUnsupported(
			outcomeContext,
			line.ID,
			deliveryPolicy.Revision,
		); policyErr != nil && !errors.Is(policyErr, store.ErrRevisionConflict) {
			slog.Warn(
				"delivery-report rejection could not be persisted",
				"component", "communication",
				"line_id", line.ID,
				"error", policyErr,
			)
		}
	}
	stored, _, err := s.repository.UpsertHardwareMessage(outcomeContext, store.HardwareMessage{
		RequestID:               requestID,
		LineID:                  line.ID,
		EndpointLineID:          line.EndpointID,
		EndpointMessageID:       receipt.ResourceID,
		IMSI:                    line.IMSI,
		ICCID:                   line.ICCID,
		LocalPhone:              line.PhoneNumber,
		HomeCountryISO:          line.HomeCountryISO,
		Number:                  number,
		ReportedNumber:          input.Number,
		Text:                    text,
		Direction:               "outgoing",
		State:                   "unknown",
		DeliveryStatus:          store.MessageDeliverySubmitted,
		DeliveryReportRequested: deliveryReportRequested,
		DeliveryReportTrackable: deliveryReportRequested && singlePartSMS(text),
		Timestamp:               s.now().UTC(),
		ObservedAt:              s.now().UTC(),
	})
	if err != nil {
		return store.Message{}, operationError(
			CodeInternal,
			operation,
			"message was accepted by the modem but could not be recorded",
			err,
		)
	}
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		requestID,
		store.HardwareCommandCompleted,
		receipt.ResourceID,
		"",
	); err != nil {
		return store.Message{}, operationError(CodeInternal, operation, "message command result could not be finalized", err)
	}
	_, _ = s.Refresh(ctx)
	if refreshed, err := s.repository.MessageByRequestID(outcomeContext, requestID); err == nil {
		return refreshed, nil
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
	address, err := phone.ParseDestination(input.Number, line.HomeCountryISO)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "destination is not dialable", err)
	}
	number := address.Dial
	requestID, err := s.requestID(input.RequestID)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "request id is invalid", err)
	}
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"start_call",
		commandDigest("start_call", line.ID, number),
	)
	if err != nil {
		return store.Call{}, err
	}
	if replay {
		call, err := s.repository.CallByID(ctx, command.ResourceID)
		if err != nil {
			return store.Call{}, operationError(
				CodeInternal,
				operation,
				"completed call command has no stored result",
				err,
			)
		}
		return call, nil
	}
	if err := s.ensureAgentControlLease(ctx); err != nil {
		return store.Call{}, s.failLocalCommand(
			ctx,
			command,
			operation,
			"host agent control is unavailable",
			err,
		)
	}
	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	receipt, err := s.agent.StartCall(commandContext, agentclient.StartCallRequest{
		RequestID: requestID,
		LineID:    line.EndpointID,
		Number:    number,
	})
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return store.Call{}, finishErr
		}
		return store.Call{}, translateAgentError(operation, err)
	}
	if err := validateReceipt(receipt, requestID); err != nil {
		err = s.recordIndeterminateAcceptedCall(
			ctx,
			command,
			validEndpointResourceID(receipt.ResourceID),
			err,
		)
		return store.Call{}, operationError(CodeUnavailable, operation, "host agent returned an invalid command receipt", err)
	}
	appID := stableInstanceID("call", line.EndpointID, receipt.ResourceID)
	outcomeContext, outcomeCancel := durableContext(ctx)
	defer outcomeCancel()
	snapshot, err := s.agent.Snapshot(outcomeContext)
	if err != nil {
		err = s.recordIndeterminateAcceptedCall(
			ctx,
			command,
			receipt.ResourceID,
			err,
		)
		return store.Call{}, operationError(
			CodeUnavailable,
			operation,
			"host agent accepted the call but its state could not be observed",
			err,
		)
	}
	var observed agentclient.Call
	found := false
	for _, candidate := range snapshot.Calls {
		if candidate.ID == receipt.ResourceID &&
			candidate.LineID == line.EndpointID {
			observed = candidate
			found = true
			break
		}
	}
	phase := callPhase(observed.State)
	if !found || phase == "unknown" || phase == "ended" || phase == "failed" {
		stateErr := errors.New("accepted call is missing from the authoritative host-agent snapshot")
		if found {
			stateErr = fmt.Errorf("accepted call has non-active state %q", observed.State)
		}
		stateErr = s.recordIndeterminateAcceptedCall(
			ctx,
			command,
			receipt.ResourceID,
			stateErr,
		)
		return store.Call{}, operationError(
			CodeUnavailable,
			operation,
			"host agent accepted the call but did not report an active call state",
			stateErr,
		)
	}
	if snapshot.ObservedAt.IsZero() {
		snapshot.ObservedAt = s.now().UTC()
	}
	projected := projectCall(
		observed,
		line,
		requestID,
		snapshot.ObservedAt,
		0,
	)
	projected.AppID = appID
	projected.LineID = line.ID
	projected.EndpointLineID = line.EndpointID
	if strings.TrimSpace(projected.Number) == "" {
		projected.Number = number
	}
	stored, err := s.repository.UpsertHardwareCall(outcomeContext, projected)
	if err != nil {
		err = s.terminateEndpointCall(
			ctx,
			receipt.ResourceID,
			"persist-call",
			err,
		)
		return store.Call{}, operationError(
			CodeInternal,
			operation,
			"call was accepted by the modem but could not be recorded",
			err,
		)
	}
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		requestID,
		store.HardwareCommandCompleted,
		appID,
		"",
	); err != nil {
		err = s.terminateEndpointCall(
			ctx,
			receipt.ResourceID,
			"finalize-call",
			err,
		)
		return store.Call{}, operationError(CodeInternal, operation, "call command result could not be finalized", err)
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
	requestID, err := s.requestID(input.RequestID)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "request id is invalid", err)
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	digits := strings.ToUpper(strings.TrimSpace(input.Digits))
	switch action {
	case "answer", "reject", "hangup":
	case "dtmf":
		if !validDTMF(digits) {
			return store.Call{}, operationError(CodeInvalidArgument, operation, "DTMF digits are invalid", nil)
		}
	default:
		return store.Call{}, operationError(CodeInvalidArgument, operation, "call action is invalid", nil)
	}
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"call_"+action,
		commandDigest("call_"+action, target.AppID, target.EndpointCallID, digits),
	)
	if err != nil {
		return store.Call{}, err
	}
	if replay {
		call, err := s.repository.CallByID(ctx, command.ResourceID)
		if err != nil {
			return store.Call{}, operationError(
				CodeInternal,
				operation,
				"completed call command has no stored result",
				err,
			)
		}
		return call, nil
	}
	if target.Phase == "ended" || target.Phase == "failed" {
		return store.Call{}, s.failLocalCommand(ctx, command, operation, "call has already ended", nil)
	}
	if action == "answer" || action == "dtmf" {
		if err := s.ensureAgentControlLease(ctx); err != nil {
			return store.Call{}, s.failLocalCommand(
				ctx,
				command,
				operation,
				"host agent control is unavailable",
				err,
			)
		}
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	var receipt agentclient.CommandReceipt
	switch action {
	case "answer", "reject", "hangup":
		receipt, err = s.agent.CallAction(
			commandContext,
			target.EndpointCallID,
			action,
			agentclient.CallActionRequest{RequestID: requestID},
		)
	case "dtmf":
		receipt, err = s.agent.SendDTMF(commandContext, target.EndpointCallID, agentclient.DTMFRequest{
			RequestID: requestID,
			Digits:    digits,
		})
	}
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return store.Call{}, finishErr
		}
		return store.Call{}, translateAgentError(operation, err)
	}
	if err := validateReceipt(receipt, requestID); err != nil ||
		receipt.ResourceID != target.EndpointCallID {
		if err == nil {
			err = errors.New("receipt resource does not match the controlled call")
		}
		if finishErr := s.finishIndeterminateCommand(ctx, command, err); finishErr != nil {
			return store.Call{}, finishErr
		}
		return store.Call{}, operationError(CodeUnavailable, operation, "host agent returned an invalid command receipt", err)
	}
	outcomeContext, outcomeCancel := durableContext(ctx)
	defer outcomeCancel()
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		requestID,
		store.HardwareCommandCompleted,
		target.AppID,
		"",
	); err != nil {
		return store.Call{}, operationError(CodeInternal, operation, "call command result could not be finalized", err)
	}
	call, err := s.repository.CallByID(outcomeContext, target.AppID)
	if err != nil {
		return store.Call{}, operationError(CodeInternal, operation, "call state could not be loaded", err)
	}
	return call, nil
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
	changed := s.status.Connected || s.status.LastError != cause.Error()
	s.status.Connected = false
	s.status.LastError = cause.Error()
	if changed {
		// The failure event invalidates both live projections. Clearing their
		// digests makes the first successful observation publish the matching
		// recovery event even when the modem data itself is unchanged.
		s.runtimeProjection = runtimeProjection{}
	}
	status := cloneStatus(s.status)
	s.mu.Unlock()
	if changed && s.runtime != nil {
		s.runtime.Publish(runtimeevents.Event{
			Resources:  []runtimeevents.Resource{runtimeevents.ResourceLines, runtimeevents.ResourceCalls},
			ObservedAt: s.now().UTC(),
		})
	}
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

func (s *Service) beginCommand(
	ctx context.Context,
	requestID, operation string,
	digest []byte,
) (store.HardwareCommand, bool, error) {
	command, created, err := s.repository.BeginHardwareCommand(ctx, requestID, operation, digest)
	if errors.Is(err, store.ErrHardwareCommandConflict) {
		return store.HardwareCommand{}, false, operationError(
			CodeConflict,
			operation,
			"request id was already used for a different command",
			err,
		)
	}
	if err != nil {
		return store.HardwareCommand{}, false, operationError(
			CodeInternal,
			operation,
			"command request could not be recorded",
			err,
		)
	}
	if created {
		return command, false, nil
	}
	switch command.Status {
	case store.HardwareCommandCompleted:
		return command, true, nil
	case store.HardwareCommandPending:
		return store.HardwareCommand{}, false, operationError(
			CodeConflict,
			operation,
			"the same command is already in progress",
			nil,
		)
	case store.HardwareCommandIndeterminate:
		return store.HardwareCommand{}, false, operationError(
			CodeConflict,
			operation,
			"the previous command outcome is unknown; inspect live state before using a new request id",
			nil,
		)
	default:
		return store.HardwareCommand{}, false, operationError(
			CodeConflict,
			operation,
			"the same request id already completed with a failure",
			nil,
		)
	}
}

func (s *Service) finishFailedCommand(
	ctx context.Context,
	command store.HardwareCommand,
	cause error,
) error {
	status := store.HardwareCommandIndeterminate
	errorCode := "transport"
	var agentError *agentclient.OperationError
	if errors.As(cause, &agentError) {
		status = store.HardwareCommandFailed
		errorCode = agentError.Code
	}
	outcomeContext, cancel := durableContext(ctx)
	defer cancel()
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		command.RequestID,
		status,
		"",
		errorCode,
	); err != nil {
		return operationError(
			CodeInternal,
			command.Operation,
			"hardware command outcome could not be recorded",
			err,
		)
	}
	return nil
}

func (s *Service) finishIndeterminateCommand(
	ctx context.Context,
	command store.HardwareCommand,
	cause error,
) error {
	outcomeContext, cancel := durableContext(ctx)
	defer cancel()
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		command.RequestID,
		store.HardwareCommandIndeterminate,
		"",
		"protocol",
	); err != nil {
		return operationError(
			CodeInternal,
			command.Operation,
			"hardware command outcome could not be recorded",
			errors.Join(cause, err),
		)
	}
	return nil
}

func (s *Service) failLocalCommand(
	ctx context.Context,
	command store.HardwareCommand,
	operation, message string,
	cause error,
) error {
	code := CodeConflict
	errorCode := string(CodeConflict)
	if cause != nil {
		code = CodeInternal
		errorCode = string(CodeInternal)
	}
	outcomeContext, cancel := durableContext(ctx)
	defer cancel()
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		command.RequestID,
		store.HardwareCommandFailed,
		"",
		errorCode,
	); err != nil {
		return operationError(
			CodeInternal,
			operation,
			"hardware command failure could not be recorded",
			errors.Join(cause, err),
		)
	}
	return operationError(code, operation, message, cause)
}

func commandDigest(values ...string) []byte {
	hash := sha256.New()
	for _, value := range values {
		_, _ = io.WriteString(hash, value)
		_, _ = hash.Write([]byte{0})
	}
	return hash.Sum(nil)
}

func validateReceipt(receipt agentclient.CommandReceipt, requestID string) error {
	if receipt.RequestID != requestID {
		return errors.New("receipt request id does not match")
	}
	if validEndpointResourceID(receipt.ResourceID) == "" {
		return errors.New("receipt resource id is invalid")
	}
	return nil
}

func validEndpointResourceID(resourceID string) string {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" || len(resourceID) > 256 {
		return ""
	}
	for _, character := range resourceID {
		if character < 0x20 || character == 0x7f {
			return ""
		}
	}
	return resourceID
}

func singlePartSMS(text string) bool {
	const (
		gsmBasic     = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
		gsmExtension = "\f^{}\\[~]|€"
	)
	septets := 0
	gsm := true
	for _, character := range text {
		switch {
		case strings.ContainsRune(gsmBasic, character):
			septets++
		case strings.ContainsRune(gsmExtension, character):
			septets += 2
		default:
			gsm = false
		}
	}
	if gsm {
		return septets <= 160
	}
	return len(utf16.Encode([]rune(text))) <= 70
}

func durableContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(normalizeContext(ctx)), snapshotTimeout)
}

func projectSnapshot(
	snapshot agentclient.Snapshot,
	bootEpoch string,
) (store.HardwareSnapshot, []store.LineSummary) {
	lines := make([]store.LineSummary, 0, len(snapshot.Lines))
	hardwareLines := make([]store.HardwareLine, 0, len(snapshot.Lines))
	lineIndex := make(map[string]store.LineSummary, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		projected := projectLine(line)
		reportedPhoneNumber := firstString(line.OwnNumbers)
		lines = append(lines, projected)
		lineIndex[line.ID] = projected
		accessTechnologies := knownAccessTechnologies(line)
		signal := projectedSignalQuality(line)
		signalQuality := uint32(0)
		if signal != nil {
			signalQuality = *signal
		}
		signalMetricsFresh := line.SignalMetricsRecent
		hardwareLines = append(hardwareLines, store.HardwareLine{
			ID:                  line.ID,
			Manufacturer:        line.Manufacturer,
			Model:               modemidentity.DisplayModel(line.Model, line.Revision, line.HardwareRevision),
			Firmware:            line.Revision,
			HardwareRevision:    line.HardwareRevision,
			DeviceIdentifier:    line.DeviceIdentifier,
			EquipmentIdentifier: line.EquipmentIdentifier,
			PhysicalDevice:      line.PhysicalDevice,
			PrimaryPort:         line.PrimaryPort,
			Ports:               projectHardwarePorts(line.Ports),
			AccessTechnologies:  accessTechnologies,
			State:               line.State,
			SignalKnown:         signal != nil,
			SignalQuality:       signalQuality,
			SignalDBM:           freshRoundedSignal(signalMetricsFresh, line.SignalDBM),
			SignalRSRQ:          freshRoundedSignal(signalMetricsFresh, line.SignalRSRQ),
			SignalRSRP:          freshRoundedSignal(signalMetricsFresh, line.SignalRSRP),
			SignalSNR:           freshSignal(signalMetricsFresh, line.SignalSNR),
			PhoneNumber:         projected.PhoneNumber,
			ReportedPhoneNumber: reportedPhoneNumber,
			ICCID:               line.SIMIdentifier,
			IMSI:                line.IMSI,
			Operator:            projected.Operator,
			HomeOperatorCode:    projected.HomeOperatorCode,
			HomeOperatorName:    projected.HomeOperatorName,
			HomeCountryISO:      projected.HomeCountryISO,
			Capabilities:        projected.Capabilities,
		})
	}

	calls := make([]store.HardwareCall, 0, len(snapshot.Calls))
	for _, call := range snapshot.Calls {
		calls = append(calls, projectCall(
			call,
			lineIndex[call.LineID],
			"",
			snapshot.ObservedAt,
			0,
		))
	}
	messages := make([]store.HardwareMessage, 0, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		line, found := lineIndex[message.LineID]
		if !found || strings.TrimSpace(message.Number) == "" || strings.TrimSpace(message.Text) == "" {
			continue
		}
		if normalizeMessageDirection(message.Direction) == "incoming" &&
			strings.ToLower(strings.TrimSpace(message.State)) != "received" {
			continue
		}
		messages = append(messages, projectMessage(
			message,
			line,
			"",
			message.Text,
			snapshot.ObservedAt,
			0,
		))
	}
	deliveryReports := make(
		[]store.HardwareMessageDeliveryReport,
		0,
		len(snapshot.DeliveryReports),
	)
	for _, report := range snapshot.DeliveryReports {
		line, found := lineIndex[report.LineID]
		if !found {
			continue
		}
		timestamp, parsed := parseModemManagerTimestamp(report.Timestamp)
		if !parsed {
			timestamp = snapshot.ObservedAt
		}
		deliveryReports = append(deliveryReports, store.HardwareMessageDeliveryReport{
			EndpointReportID:      report.ID,
			LineID:                line.ID,
			EndpointLineID:        line.EndpointID,
			HomeCountryISO:        line.HomeCountryISO,
			Number:                report.Number,
			MessageReference:      report.MessageReference,
			MessageReferenceKnown: report.MessageReferenceKnown,
			DeliveryState:         report.DeliveryState,
			DeliveryStateKnown:    report.DeliveryStateKnown,
			Timestamp:             timestamp,
			ObservedAt:            snapshot.ObservedAt,
		})
	}
	return store.HardwareSnapshot{
		BootEpoch:       bootEpoch,
		Revision:        snapshot.Revision,
		ObservedAt:      snapshot.ObservedAt.UTC(),
		Lines:           hardwareLines,
		Calls:           calls,
		Messages:        messages,
		DeliveryReports: deliveryReports,
	}, lines
}

func roundedSignal(value *float64) *int64 {
	if value == nil {
		return nil
	}
	rounded := int64(math.Round(*value))
	return &rounded
}

func freshRoundedSignal(fresh bool, value *float64) *int64 {
	if !fresh {
		return nil
	}
	return roundedSignal(value)
}

func freshSignal(fresh bool, value *float64) *float64 {
	if !fresh {
		return nil
	}
	return cloneFloat64(value)
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func knownAccessTechnologies(line agentclient.Line) *uint32 {
	if !line.AccessTechnologiesKnown || line.AccessTechnologies == 0 {
		return nil
	}
	value := line.AccessTechnologies
	return &value
}

func projectHardwarePorts(ports []agentclient.ModemPort) []store.HardwarePort {
	if len(ports) == 0 {
		return nil
	}
	projected := make([]store.HardwarePort, 0, len(ports))
	for _, port := range ports {
		projected = append(projected, store.HardwarePort{
			Name:     port.Name,
			Type:     port.Type,
			TypeCode: port.TypeCode,
		})
	}
	return projected
}

func projectLine(line agentclient.Line) store.LineSummary {
	signal := projectedSignalQuality(line)
	homeOperatorCode := firstNonEmpty(line.HomeOperatorCode, line.OperatorIdentifier)
	homeOperatorName := firstNonEmpty(line.HomeOperatorName, line.OperatorName)
	phoneNumber := phone.NetworkSubscriberE164(
		firstString(line.OwnNumbers),
		line.HomeCountryISO,
	)
	return store.LineSummary{
		ID:                       line.ID,
		EndpointID:               line.ID,
		ICCID:                    line.SIMIdentifier,
		IMSI:                     line.IMSI,
		PhoneNumber:              phoneNumber,
		Operator:                 firstNonEmpty(homeOperatorName, homeOperatorCode),
		HomeOperatorCode:         homeOperatorCode,
		HomeOperatorName:         homeOperatorName,
		HomeCountryISO:           line.HomeCountryISO,
		ServingOperatorCode:      line.ServingOperatorCode,
		ServingOperatorName:      line.ServingOperatorName,
		ServingCountryISO:        line.ServingCountryISO,
		RegistrationStateKnown:   line.RegistrationStateKnown,
		RegistrationStateCode:    line.RegistrationStateCode,
		RegistrationState:        line.RegistrationState,
		Roaming:                  line.Roaming,
		EmergencyOnly:            line.EmergencyOnly,
		DeviceIMEI:               firstNonEmpty(line.EquipmentIdentifier, line.DeviceIdentifier),
		DeviceName:               "",
		Model:                    modemidentity.DisplayModel(line.Model, line.Revision, line.HardwareRevision),
		Firmware:                 line.Revision,
		HardwareRevision:         line.HardwareRevision,
		PrimaryPort:              line.PrimaryPort,
		Ports:                    projectHardwarePorts(line.Ports),
		AccessTechnologies:       knownAccessTechnologies(line),
		State:                    line.State,
		FailureReason:            line.FailureReason,
		FailureReasonCode:        line.FailureReasonCode,
		RadioDesiredEnabled:      line.RadioDesiredEnabled,
		RadioDesiredEnabledKnown: line.RadioDesiredEnabledKnown,
		Signal:                   signal,
		SignalSNR:                freshSignal(line.SignalMetricsRecent, line.SignalSNR),
		Capabilities: store.LineCapabilities{
			Modem:       line.Capabilities.ModemInterface,
			SIM:         line.Capabilities.SIMInterface,
			Voice:       line.Capabilities.VoiceInterface,
			Messaging:   line.Capabilities.MessagingInterface,
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

func projectedSignalQuality(line agentclient.Line) *uint32 {
	if line.SignalQualityKnown && line.SignalQualityRecent {
		value := min(line.SignalQuality, uint32(100))
		return &value
	}
	if !line.SignalMetricsRecent {
		return nil
	}
	switch {
	case line.SignalRSRP != nil:
		value := normalizedSignalQuality(*line.SignalRSRP, -140, -80)
		return &value
	case line.SignalDBM != nil:
		value := normalizedSignalQuality(*line.SignalDBM, -110, -50)
		return &value
	default:
		return nil
	}
}

func normalizedSignalQuality(value, minimum, maximum float64) uint32 {
	if value <= minimum {
		return 0
	}
	if value >= maximum {
		return 100
	}
	return uint32(math.Round((value - minimum) * 100 / (maximum - minimum)))
}

func projectMessage(
	message agentclient.Message,
	line store.LineSummary,
	requestID string,
	text string,
	observedAt time.Time,
	revision int64,
) store.HardwareMessage {
	timestamp, parsed := parseModemManagerTimestamp(message.Timestamp)
	if !parsed {
		timestamp = observedAt
	}
	return store.HardwareMessage{
		RequestID:         requestID,
		LineID:            line.ID,
		EndpointLineID:    line.EndpointID,
		EndpointMessageID: message.ID,
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		LocalPhone:        line.PhoneNumber,
		HomeCountryISO:    line.HomeCountryISO,
		Number:            phone.CanonicalNetworkAddress(message.Number, line.HomeCountryISO),
		ReportedNumber:    message.Number,
		Text:              text,
		Direction:         normalizeMessageDirection(message.Direction),
		State:             message.State,
		StateCode:         int64(message.StateCode),
		DeliveryStatus: func() store.MessageDeliveryStatus {
			if normalizeMessageDirection(message.Direction) == "outgoing" {
				return store.MessageDeliverySubmitted
			}
			return store.MessageDeliveryUnknown
		}(),
		MessageReference:      message.MessageReference,
		MessageReferenceKnown: message.MessageReferenceKnown,
		Revision:              revision,
		Timestamp:             timestamp,
		ObservedAt:            observedAt,
	}
}

func projectCall(
	call agentclient.Call,
	line store.LineSummary,
	requestID string,
	observedAt time.Time,
	revision int64,
) store.HardwareCall {
	projected := store.HardwareCall{
		AppID:           stableInstanceID("call", call.LineID, call.ID),
		RequestID:       requestID,
		LineID:          call.LineID,
		EndpointLineID:  call.LineID,
		LocalPhone:      line.PhoneNumber,
		LineIMSI:        line.IMSI,
		LineICCID:       line.ICCID,
		HomeCountryISO:  line.HomeCountryISO,
		EndpointCallID:  call.ID,
		Number:          phone.CanonicalNetworkAddress(call.Number, line.HomeCountryISO),
		ReportedNumber:  call.Number,
		Direction:       normalizeCallDirection(call.Direction, call.State),
		Phase:           callPhase(call.State),
		Bearer:          call.Bearer,
		StateReason:     call.StateReason,
		StateReasonCode: int64(call.StateReasonCode),
		Multiparty:      call.Multiparty,
		AudioPort:       call.AudioPort,
		MediaAvailable:  applicationMediaAvailable(call),
		Revision:        revision,
		ObservedAt:      observedAt,
	}
	if call.AudioFormat != nil {
		projected.AudioEncoding = call.AudioFormat.Encoding
		projected.AudioResolution = call.AudioFormat.Resolution
		projected.AudioRate = call.AudioFormat.Rate
	}
	return projected
}

func applicationMediaAvailable(call agentclient.Call) bool {
	if !call.MediaAvailable ||
		!call.MediaConfigured ||
		callPhase(call.State) != "active" ||
		strings.TrimSpace(call.AudioPort) == "" ||
		call.AudioFormat == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(call.AudioFormat.Encoding), "pcm") ||
		!strings.EqualFold(strings.TrimSpace(call.AudioFormat.Resolution), "s16le") {
		return false
	}
	return call.AudioFormat.Rate == 8000 || call.AudioFormat.Rate == 16000
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
		if selector != line.ID {
			continue
		}
		if strings.TrimSpace(line.EndpointID) == "" {
			return store.LineSummary{}, operationError(
				CodeConflict,
				"select line",
				"selected line is not attached to a modem endpoint",
				nil,
			)
		}
		if !supported(line.Capabilities) {
			return store.LineSummary{}, operationError(CodeNotSupported, "select line", "selected line does not support this operation", nil)
		}
		return line, nil
	}
	return store.LineSummary{}, operationError(CodeNotFound, "select line", "selected line is not attached", nil)
}

func bindProjectedLines(
	lines []store.LineSummary,
	lineIDsByEndpoint map[string]string,
	stableLines []store.LineSummary,
) []store.LineSummary {
	homeCountriesByLineID := make(map[string]string, len(stableLines))
	for _, line := range stableLines {
		lineID := strings.TrimSpace(line.ID)
		homeCountryISO := phone.CanonicalRegion(line.HomeCountryISO)
		if lineID != "" && homeCountryISO != "" {
			homeCountriesByLineID[lineID] = homeCountryISO
		}
	}
	bound := make([]store.LineSummary, 0, len(lines))
	for _, line := range lines {
		endpointID := strings.TrimSpace(line.EndpointID)
		if endpointID == "" {
			endpointID = strings.TrimSpace(line.ID)
		}
		lineID := strings.TrimSpace(lineIDsByEndpoint[endpointID])
		if lineID == "" {
			continue
		}
		line.ID = lineID
		line.EndpointID = endpointID
		if homeCountryISO := homeCountriesByLineID[lineID]; homeCountryISO != "" {
			line.HomeCountryISO = homeCountryISO
		}
		bound = append(bound, line)
	}
	return bound
}

func bindSnapshotHomeCountries(
	snapshot agentclient.Snapshot,
	stableLines []store.LineSummary,
) agentclient.Snapshot {
	lookup := newStableHomeCountryLookup(stableLines)
	snapshot.Lines = append([]agentclient.Line(nil), snapshot.Lines...)
	for index := range snapshot.Lines {
		line := &snapshot.Lines[index]
		homeCountryISO := lookup.resolve(*line)
		if homeCountryISO != "" {
			line.HomeCountryISO = homeCountryISO
		}
	}
	return snapshot
}

type stableHomeCountryLookup struct {
	byICCID map[string]string
	byIMSI  map[string]string
}

func newStableHomeCountryLookup(lines []store.LineSummary) stableHomeCountryLookup {
	lookup := stableHomeCountryLookup{
		byICCID: make(map[string]string, len(lines)),
		byIMSI:  make(map[string]string, len(lines)),
	}
	for _, line := range lines {
		homeCountryISO := phone.CanonicalRegion(line.HomeCountryISO)
		if homeCountryISO == "" {
			continue
		}
		indexStableHomeCountry(lookup.byICCID, line.ICCID, homeCountryISO)
		indexStableHomeCountry(lookup.byIMSI, line.IMSI, homeCountryISO)
	}
	return lookup
}

func (lookup stableHomeCountryLookup) resolve(line agentclient.Line) string {
	iccid := strings.TrimSpace(line.SIMIdentifier)
	if homeCountryISO := lookup.byICCID[iccid]; homeCountryISO != "" {
		return homeCountryISO
	}
	imsi := strings.TrimSpace(line.IMSI)
	if homeCountryISO := lookup.byIMSI[imsi]; homeCountryISO != "" {
		return homeCountryISO
	}
	return ""
}

func indexStableHomeCountry(index map[string]string, key, homeCountryISO string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if existing, found := index[key]; found && existing != homeCountryISO {
		index[key] = ""
		return
	}
	index[key] = homeCountryISO
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
