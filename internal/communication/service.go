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
	maxMessageRunes            = 1600
	maxRequestIDLen            = 128
	maxIncomingCallActions     = 8
	incomingCallActionTimeout  = 5 * time.Second
	deviceConfigurationTimeout = 50 * time.Second
	controlLeaseRenewInterval  = time.Second
	controlLeaseRequestTimeout = 5 * time.Second
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

type AgentCallMediaActivator interface {
	ActivateCallMedia(
		context.Context,
		string,
		agentclient.CallActionRequest,
	) (agentclient.CallMediaActivation, error)
}

type Repository interface {
	ApplyHardwareSnapshotWithResult(
		context.Context,
		store.HardwareSnapshot,
	) (store.HardwareSnapshotResult, error)
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

type Service struct {
	agent      Agent
	repository Repository
	events     messageevents.Publisher
	runtime    runtimeevents.Publisher
	random     io.Reader
	now        func() time.Time

	mu           sync.RWMutex
	status       Status
	lastSnapshot agentclient.Snapshot
	refreshMu    sync.Mutex

	lifecycleMu       sync.RWMutex
	lifecycleObserver CallLifecycleObserver

	mediaActivationCalls map[string]struct{}

	controlMu             sync.RWMutex
	controlLeaseSupported bool
	agentEventsRequired   bool
	agentEventsHealthy    bool
	controlLeaseActive    bool
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
		agent:                agent,
		repository:           repository,
		events:               events,
		random:               rand.Reader,
		now:                  time.Now,
		mediaActivationCalls: make(map[string]struct{}),
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

	hardwareSnapshot, lines := projectSnapshot(snapshot, health.Provider.BootEpoch)
	snapshotResult, err := s.repository.ApplyHardwareSnapshotWithResult(
		refreshContext,
		hardwareSnapshot,
	)
	if err != nil {
		return s.recordRefreshFailure("persist host agent snapshot", err)
	}
	lines = bindProjectedLines(lines, snapshotResult.LineIDsByEndpoint)
	s.publishIncomingMessages(snapshotResult.CreatedIncomingMessages)
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
	s.publishRuntimeSnapshot(snapshot, lines)
	s.activatePendingCallMedia(ctx, snapshot)
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

func (s *Service) activatePendingCallMedia(
	ctx context.Context,
	snapshot agentclient.Snapshot,
) {
	activator, ok := s.agent.(AgentCallMediaActivator)
	if !ok {
		return
	}
	currentCalls := make(map[string]struct{}, len(snapshot.Calls))
	for _, call := range snapshot.Calls {
		if call.StateCode != 7 {
			currentCalls[call.ID] = struct{}{}
		}
	}
	for callID := range s.mediaActivationCalls {
		if _, found := currentCalls[callID]; !found {
			delete(s.mediaActivationCalls, callID)
		}
	}
	lines := make(map[string]agentclient.Line, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		lines[line.ID] = line
	}
	for _, call := range snapshot.Calls {
		if call.StateCode != 4 || call.MediaAvailable {
			continue
		}
		if _, attempted := s.mediaActivationCalls[call.ID]; attempted {
			continue
		}
		line, found := lines[call.LineID]
		if !found ||
			line.VoiceVerification == nil ||
			line.VoiceVerification.MediaRouting != "call_required" {
			continue
		}
		if err := s.ensureAgentControlLease(ctx); err != nil {
			slog.Warn(
				"call media activation deferred until agent control is healthy",
				"component", "communications",
				"call_id", call.ID,
				"line_id", call.LineID,
				"error", err,
			)
			continue
		}
		commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
		activation, err := activator.ActivateCallMedia(
			commandContext,
			call.ID,
			agentclient.CallActionRequest{
				RequestID: stableInstanceID("media", call.LineID, call.ID),
			},
		)
		cancel()
		if err != nil {
			slog.Warn(
				"call media activation failed",
				"component", "communications",
				"call_id", call.ID,
				"line_id", call.LineID,
				"error", err,
			)
			continue
		}
		s.mediaActivationCalls[call.ID] = struct{}{}
		logArgs := []any{
			"component", "communications",
			"call_id", call.ID,
			"line_id", call.LineID,
			"routing", activation.MediaRouting,
			"media_available", activation.MediaAvailable,
			"media_configured", activation.MediaConfigured,
		}
		if activation.MediaAvailable {
			slog.Info("call media activation observed", logArgs...)
		} else {
			logArgs = append(logArgs, "reason", activation.Reason)
			slog.Warn("call media activation unavailable", logArgs...)
		}
	}
}

func (s *Service) publishRuntimeSnapshot(
	snapshot agentclient.Snapshot,
	lines []store.LineSummary,
) {
	if s.runtime == nil {
		return
	}
	if key, ok := runtimePayloadKey("communications:lines:", canonicalRuntimeLines(lines)); ok {
		s.runtime.Publish(runtimeevents.Event{
			EventKey:   key,
			Resources:  []runtimeevents.Resource{runtimeevents.ResourceLines},
			ObservedAt: snapshot.ObservedAt,
		})
	}
	if key, ok := runtimePayloadKey("communications:calls:", canonicalRuntimeCalls(snapshot.Calls)); ok {
		s.runtime.Publish(runtimeevents.Event{
			EventKey:   key,
			Resources:  []runtimeevents.Resource{runtimeevents.ResourceCalls},
			ObservedAt: snapshot.ObservedAt,
		})
	}
}

func runtimePayloadKey(prefix string, payload any) (string, bool) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(encoded)
	return prefix + hex.EncodeToString(digest[:]), true
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

func (s *Service) publishIncomingMessages(messages []store.Message) {
	for _, message := range messages {
		messageID := strconv.FormatInt(message.ID, 10)
		s.events.Publish(messageevents.IncomingSMS{
			EventKey:  "sms:" + messageID,
			MessageID: messageID,
			ThreadKey: store.MessageThreadKey(
				message.LineID,
				message.Peer,
			),
			LineID:    message.LineID,
			ICCID:     message.ICCID,
			Peer:      message.Peer,
			Content:   message.Content,
			Timestamp: message.Timestamp,
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
	watchFailures := make(chan error, 1)
	watchStates := make(chan bool)
	var watchCancel context.CancelFunc
	defer func() {
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
			lastWatchError = ""
			if status, ok := refresh(); ok {
				startWatcher(status)
			}
			resetTimer(timer, every)
		case err := <-watchFailures:
			reportDistinct(err, &lastWatchError)
		case <-timer.C:
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
		(!s.agentEventsRequired || s.agentEventsHealthy)
}

func (s *Service) ensureAgentControlLease(ctx context.Context) error {
	if !s.agentControlLeaseRenewable() {
		s.controlMu.RLock()
		supported := s.controlLeaseSupported
		eventsRequired := s.agentEventsRequired
		eventsHealthy := s.agentEventsHealthy
		s.controlMu.RUnlock()
		if supported && eventsRequired && !eventsHealthy {
			return operationError(
				CodeUnavailable,
				"acquire host agent control",
				"host agent event stream is not healthy",
				nil,
			)
		}
		return nil
	}
	if err := s.renewAgentControlLease(ctx); err != nil {
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
	if !ok || !s.agentControlLeaseRenewable() {
		return nil
	}
	renewContext, cancel := context.WithTimeout(
		normalizeContext(ctx),
		controlLeaseRequestTimeout,
	)
	_, err := lease.RenewControlLease(renewContext)
	cancel()
	if err == nil {
		s.controlMu.Lock()
		s.controlLeaseActive = true
		s.controlMu.Unlock()
	}
	return err
}

func (s *Service) releaseAgentControlLease(ctx context.Context) error {
	lease, ok := s.agent.(AgentControlLease)
	if !ok {
		return nil
	}
	s.controlMu.Lock()
	active := s.controlLeaseActive
	s.controlLeaseActive = false
	s.controlMu.Unlock()
	if !active {
		return nil
	}
	return lease.ReleaseControlLease(normalizeContext(ctx))
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

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	receipt, err := s.agent.SendMessage(commandContext, agentclient.SendMessageRequest{
		RequestID: requestID,
		LineID:    line.EndpointID,
		Number:    number,
		Text:      text,
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
	stored, _, err := s.repository.UpsertHardwareMessage(outcomeContext, store.HardwareMessage{
		RequestID:         requestID,
		LineID:            line.ID,
		EndpointLineID:    line.EndpointID,
		EndpointMessageID: receipt.ResourceID,
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		LocalPhone:        line.PhoneNumber,
		Number:            number,
		Text:              text,
		Direction:         "outgoing",
		State:             "unknown",
		Timestamp:         s.now().UTC(),
		ObservedAt:        s.now().UTC(),
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
	_, number, err := phone.NormalizeDialTarget(input.Number)
	if err != nil {
		return store.Call{}, operationError(CodeInvalidArgument, operation, "destination is not dialable", err)
	}
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
	active, err := s.repository.ActiveCalls(ctx)
	if err != nil {
		return store.Call{}, s.failLocalCommand(ctx, command, operation, "cannot inspect active calls", err)
	}
	for _, call := range active {
		if call.LineID == line.ID {
			return store.Call{}, s.failLocalCommand(
				ctx,
				command,
				operation,
				"this line already has an active call",
				nil,
			)
		}
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
		if finishErr := s.finishIndeterminateCommand(ctx, command, err); finishErr != nil {
			return store.Call{}, finishErr
		}
		return store.Call{}, operationError(CodeUnavailable, operation, "host agent returned an invalid command receipt", err)
	}
	appID := stableInstanceID("call", line.EndpointID, receipt.ResourceID)
	outcomeContext, outcomeCancel := durableContext(ctx)
	defer outcomeCancel()
	stored, err := s.repository.UpsertHardwareCall(outcomeContext, store.HardwareCall{
		AppID:          appID,
		RequestID:      requestID,
		LineID:         line.ID,
		EndpointLineID: line.EndpointID,
		LocalPhone:     line.PhoneNumber,
		LineIMSI:       line.IMSI,
		LineICCID:      line.ICCID,
		EndpointCallID: receipt.ResourceID,
		Number:         number,
		Direction:      "outgoing",
		Phase:          "dialing",
		ObservedAt:     s.now().UTC(),
	})
	if err != nil {
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
	resourceID := strings.TrimSpace(receipt.ResourceID)
	if resourceID == "" || len(resourceID) > 256 {
		return errors.New("receipt resource id is invalid")
	}
	for _, character := range resourceID {
		if character < 0x20 || character == 0x7f {
			return errors.New("receipt resource id contains a control character")
		}
	}
	return nil
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
			PhoneNumber:         firstString(line.OwnNumbers),
			ICCID:               line.SIMIdentifier,
			IMSI:                line.IMSI,
			Operator:            projected.Operator,
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
	return store.HardwareSnapshot{
		BootEpoch:  bootEpoch,
		Revision:   snapshot.Revision,
		ObservedAt: snapshot.ObservedAt.UTC(),
		Lines:      hardwareLines,
		Calls:      calls,
		Messages:   messages,
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
	return store.LineSummary{
		ID:                       line.ID,
		EndpointID:               line.ID,
		ICCID:                    line.SIMIdentifier,
		IMSI:                     line.IMSI,
		PhoneNumber:              firstString(line.OwnNumbers),
		Operator:                 firstNonEmpty(homeOperatorName, homeOperatorCode),
		HomeOperatorCode:         homeOperatorCode,
		HomeOperatorName:         homeOperatorName,
		ServingOperatorCode:      line.ServingOperatorCode,
		ServingOperatorName:      line.ServingOperatorName,
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
		EndpointCallID:  call.ID,
		Number:          call.Number,
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
) []store.LineSummary {
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
		bound = append(bound, line)
	}
	return bound
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
