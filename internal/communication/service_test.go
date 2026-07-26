package communication

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeAgent struct {
	health                           agentclient.Health
	healthError                      error
	snapshot                         agentclient.Snapshot
	snapshotError                    error
	startResult                      agentclient.CommandReceipt
	startError                       error
	startRequests                    []agentclient.StartCallRequest
	actionResult                     agentclient.CommandReceipt
	actionError                      error
	actionCallID                     string
	actionName                       string
	actionRequest                    agentclient.CallActionRequest
	actionRequests                   []agentclient.CallActionRequest
	dtmfResult                       agentclient.CommandReceipt
	dtmfError                        error
	dtmfCallID                       string
	dtmfRequest                      agentclient.DTMFRequest
	messageResult                    agentclient.CommandReceipt
	messageError                     error
	messageRequests                  []agentclient.SendMessageRequest
	deviceConfiguration              agentclient.DeviceConfiguration
	deviceConfigurationError         error
	applyDeviceConfigurationRequests []agentclient.ApplyDeviceConfigurationRequest
	applyDeviceConfigurationError    error
}

func (agent *fakeAgent) Health(context.Context) (agentclient.Health, error) {
	return agent.health, agent.healthError
}

func (agent *fakeAgent) Snapshot(context.Context) (agentclient.Snapshot, error) {
	return agent.snapshot, agent.snapshotError
}

func (agent *fakeAgent) StartCall(
	_ context.Context,
	request agentclient.StartCallRequest,
) (agentclient.CommandReceipt, error) {
	agent.startRequests = append(agent.startRequests, request)
	return agent.startResult, agent.startError
}

func (agent *fakeAgent) CallAction(
	_ context.Context,
	callID string,
	action string,
	request agentclient.CallActionRequest,
) (agentclient.CommandReceipt, error) {
	agent.actionCallID = callID
	agent.actionName = action
	agent.actionRequest = request
	agent.actionRequests = append(agent.actionRequests, request)
	return agent.actionResult, agent.actionError
}

func (agent *fakeAgent) SendDTMF(
	_ context.Context,
	callID string,
	request agentclient.DTMFRequest,
) (agentclient.CommandReceipt, error) {
	agent.dtmfCallID = callID
	agent.dtmfRequest = request
	return agent.dtmfResult, agent.dtmfError
}

func (agent *fakeAgent) SendMessage(
	_ context.Context,
	request agentclient.SendMessageRequest,
) (agentclient.CommandReceipt, error) {
	agent.messageRequests = append(agent.messageRequests, request)
	return agent.messageResult, agent.messageError
}

func (agent *fakeAgent) DeviceConfiguration(
	_ context.Context,
	_ string,
) (agentclient.DeviceConfiguration, error) {
	return agent.deviceConfiguration, agent.deviceConfigurationError
}

func (agent *fakeAgent) ApplyDeviceConfiguration(
	_ context.Context,
	_ string,
	request agentclient.ApplyDeviceConfigurationRequest,
) (agentclient.DeviceConfiguration, error) {
	agent.applyDeviceConfigurationRequests = append(agent.applyDeviceConfigurationRequests, request)
	return agent.deviceConfiguration, agent.applyDeviceConfigurationError
}

type fakeRepository struct {
	snapshot                    store.HardwareSnapshot
	snapshotResult              store.HardwareSnapshotResult
	snapshotError               error
	message                     store.Message
	messageInput                store.HardwareMessage
	messageError                error
	call                        store.Call
	callInput                   store.HardwareCall
	callError                   error
	target                      store.CallControlTarget
	targetError                 error
	activeCalls                 []store.Call
	activeCallsError            error
	commands                    map[string]store.HardwareCommand
	globalCallSettings          store.GlobalCallSettings
	lineCallPolicies            map[string]store.LineCallPolicy
	incomingCallActions         []store.IncomingCallAction
	finishedIncomingCallActions []store.IncomingCallAction
}

type fakeCallLifecycleObserver struct {
	snapshots [][]store.Call
	err       error
}

func (observer *fakeCallLifecycleObserver) ReconcileAuthoritativeCalls(
	_ context.Context,
	calls []store.Call,
) error {
	observer.snapshots = append(observer.snapshots, append([]store.Call(nil), calls...))
	return observer.err
}

func (repository *fakeRepository) ApplyHardwareSnapshotWithResult(
	_ context.Context,
	snapshot store.HardwareSnapshot,
) (store.HardwareSnapshotResult, error) {
	repository.snapshot = snapshot
	return repository.snapshotResult, repository.snapshotError
}

func (repository *fakeRepository) UpsertHardwareMessage(
	_ context.Context,
	message store.HardwareMessage,
) (store.Message, bool, error) {
	repository.messageInput = message
	if repository.message.ID == 0 {
		repository.message = store.Message{
			ID:                1,
			RequestID:         message.RequestID,
			LineID:            message.LineID,
			EndpointMessageID: message.EndpointMessageID,
			Peer:              message.Number,
			Content:           message.Text,
			Direction:         message.Direction,
			State:             message.State,
		}
	}
	return repository.message, true, repository.messageError
}

func (repository *fakeRepository) UpsertHardwareCall(
	_ context.Context,
	call store.HardwareCall,
) (store.Call, error) {
	repository.callInput = call
	if repository.call.ID == "" {
		repository.call = store.Call{
			ID:             call.AppID,
			RequestID:      call.RequestID,
			DeviceID:       call.LineID,
			LocalPhone:     call.LocalPhone,
			LineIMSI:       call.LineIMSI,
			LineICCID:      call.LineICCID,
			Direction:      call.Direction,
			RemoteNumber:   call.Number,
			EndpointCallID: call.EndpointCallID,
			Phase:          call.Phase,
			Revision:       call.Revision,
			Bearer:         call.Bearer,
		}
	}
	return repository.call, repository.callError
}

func (repository *fakeRepository) CallControlTarget(context.Context, string) (store.CallControlTarget, error) {
	return repository.target, repository.targetError
}

func (repository *fakeRepository) ActiveCalls(context.Context) ([]store.Call, error) {
	return repository.activeCalls, repository.activeCallsError
}

func (repository *fakeRepository) BeginHardwareCommand(
	_ context.Context,
	requestID, operation string,
	digest []byte,
) (store.HardwareCommand, bool, error) {
	if repository.commands == nil {
		repository.commands = make(map[string]store.HardwareCommand)
	}
	if existing, ok := repository.commands[requestID]; ok {
		if existing.Operation != operation || !bytes.Equal(existing.PayloadDigest, digest) {
			return store.HardwareCommand{}, false, store.ErrHardwareCommandConflict
		}
		return existing, false, nil
	}
	command := store.HardwareCommand{
		RequestID:     requestID,
		Operation:     operation,
		PayloadDigest: append([]byte(nil), digest...),
		Status:        store.HardwareCommandPending,
	}
	repository.commands[requestID] = command
	return command, true, nil
}

func (repository *fakeRepository) FinishHardwareCommand(
	_ context.Context,
	requestID, status, resourceID, errorCode string,
) error {
	command, ok := repository.commands[requestID]
	if !ok {
		return store.ErrHardwareCommandConflict
	}
	command.Status = status
	command.ResourceID = resourceID
	command.ErrorCode = errorCode
	repository.commands[requestID] = command
	return nil
}

func (repository *fakeRepository) MessageByRequestID(context.Context, string) (store.Message, error) {
	if repository.message.ID == 0 {
		return store.Message{}, store.ErrMessageNotFound
	}
	return repository.message, nil
}

func (repository *fakeRepository) CallByRequestID(context.Context, string) (store.Call, error) {
	if repository.call.ID == "" {
		return store.Call{}, store.ErrCallNotFound
	}
	return repository.call, nil
}

func (repository *fakeRepository) CallByID(_ context.Context, id string) (store.Call, error) {
	if repository.call.ID == "" || repository.call.ID != id {
		return store.Call{}, store.ErrCallNotFound
	}
	return repository.call, nil
}

func (repository *fakeRepository) GlobalCallSettings(
	context.Context,
) (store.GlobalCallSettings, error) {
	if repository.globalCallSettings.Revision == 0 {
		repository.globalCallSettings = store.GlobalCallSettings{
			ReceiveCalls: true,
			Revision:     1,
		}
	}
	return repository.globalCallSettings, nil
}

func (repository *fakeRepository) UpdateGlobalCallSettings(
	_ context.Context,
	receiveCalls bool,
	expectedRevision int64,
) (store.GlobalCallSettings, error) {
	current, _ := repository.GlobalCallSettings(context.Background())
	if current.Revision != expectedRevision {
		return store.GlobalCallSettings{}, store.ErrRevisionConflict
	}
	current.ReceiveCalls = receiveCalls
	current.Revision++
	repository.globalCallSettings = current
	return current, nil
}

func (repository *fakeRepository) LineCallPolicy(
	_ context.Context,
	lineID string,
) (store.LineCallPolicy, error) {
	if repository.lineCallPolicies == nil {
		repository.lineCallPolicies = make(map[string]store.LineCallPolicy)
	}
	policy, found := repository.lineCallPolicies[lineID]
	if !found {
		policy = store.LineCallPolicy{
			LineID:   lineID,
			Policy:   store.LineCallPolicyFollowGlobal,
			Revision: 1,
		}
		repository.lineCallPolicies[lineID] = policy
	}
	return policy, nil
}

func (repository *fakeRepository) UpdateLineCallPolicy(
	_ context.Context,
	lineID string,
	value store.LineCallPolicyValue,
	expectedRevision int64,
) (store.LineCallPolicy, error) {
	policy, _ := repository.LineCallPolicy(context.Background(), lineID)
	if policy.Revision != expectedRevision {
		return store.LineCallPolicy{}, store.ErrRevisionConflict
	}
	policy.Policy = value
	policy.Revision++
	repository.lineCallPolicies[lineID] = policy
	return policy, nil
}

func (repository *fakeRepository) EffectiveCallPolicy(
	_ context.Context,
	lineID string,
) (store.EffectiveCallPolicy, error) {
	global, _ := repository.GlobalCallSettings(context.Background())
	line, _ := repository.LineCallPolicy(context.Background(), lineID)
	effective := store.EffectiveCallPolicyReceive
	if line.Policy == store.LineCallPolicyDND ||
		(line.Policy == store.LineCallPolicyFollowGlobal && !global.ReceiveCalls) {
		effective = store.EffectiveCallPolicyDND
	}
	return store.EffectiveCallPolicy{
		LineID:         lineID,
		Policy:         effective,
		GlobalRevision: global.Revision,
		LineRevision:   line.Revision,
	}, nil
}

func (repository *fakeRepository) CallPolicyConfiguration(
	ctx context.Context,
	lineID string,
) (store.CallPolicyConfiguration, error) {
	global, _ := repository.GlobalCallSettings(ctx)
	line, _ := repository.LineCallPolicy(ctx, lineID)
	effective, _ := repository.EffectiveCallPolicy(ctx, lineID)
	return store.CallPolicyConfiguration{
		Global:    global,
		Line:      line,
		Effective: effective,
	}, nil
}

func (repository *fakeRepository) ClaimIncomingCallActions(
	_ context.Context,
	limit int,
) ([]store.IncomingCallAction, error) {
	if len(repository.incomingCallActions) > limit {
		claimed := append([]store.IncomingCallAction(nil), repository.incomingCallActions[:limit]...)
		repository.incomingCallActions = repository.incomingCallActions[limit:]
		return claimed, nil
	}
	claimed := append([]store.IncomingCallAction(nil), repository.incomingCallActions...)
	repository.incomingCallActions = nil
	return claimed, nil
}

func (repository *fakeRepository) FinishIncomingCallAction(
	_ context.Context,
	callID string,
	status string,
	errorCode string,
) error {
	repository.finishedIncomingCallActions = append(
		repository.finishedIncomingCallActions,
		store.IncomingCallAction{CallID: callID, Status: status, ErrorCode: errorCode},
	)
	return nil
}

func (repository *fakeRepository) LatestIncomingCallAction(
	_ context.Context,
	_ string,
) (*store.IncomingCallAction, error) {
	if len(repository.finishedIncomingCallActions) == 0 {
		return nil, nil
	}
	action := repository.finishedIncomingCallActions[len(repository.finishedIncomingCallActions)-1]
	return &action, nil
}

func TestServiceRequiresExplicitCapableLine(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.startResult = agentclient.CommandReceipt{
		RequestID:  "request-call-1",
		ResourceID: "call-endpoint-1",
	}
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }

	if _, err := service.StartCall(context.Background(), StartCallInput{
		Number: "+818012345678",
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("StartCall() without line error = %v, want invalid argument", err)
	}
	if len(agent.startRequests) != 0 {
		t.Fatalf("agent start requests = %+v, want none", agent.startRequests)
	}

	call, err := service.StartCall(context.Background(), StartCallInput{
		RequestID: "request-call-1",
		LineID:    "line-1",
		Number:    "090-1234-5678",
	})
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if len(agent.startRequests) != 1 {
		t.Fatalf("agent start request count = %d, want 1", len(agent.startRequests))
	}
	request := agent.startRequests[0]
	if request.LineID != "line-1" || request.Number != "09012345678" ||
		request.RequestID != "request-call-1" {
		t.Fatalf("agent start request = %+v", request)
	}
	if call.DeviceID != "line-1" || call.RemoteNumber != "09012345678" ||
		call.LocalPhone != "+819012345678" ||
		call.LineIMSI != "440500000000001" ||
		call.LineICCID != "8901000000000000001" ||
		call.Direction != "outgoing" || call.Phase != "unknown" {
		t.Fatalf("call = %+v", call)
	}
	replayed, err := service.StartCall(context.Background(), StartCallInput{
		RequestID: "request-call-1",
		LineID:    "line-1",
		Number:    "09012345678",
	})
	if err != nil {
		t.Fatalf("replayed StartCall() error = %v", err)
	}
	if replayed.ID != call.ID || len(agent.startRequests) != 1 {
		t.Fatalf("replayed call = %+v, agent requests = %d", replayed, len(agent.startRequests))
	}
}

func TestCallActionUsesReceiptWithoutInventingState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 12, 30, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.actionResult = agentclient.CommandReceipt{
		RequestID:  "request-hangup-1",
		ResourceID: "call-endpoint-4",
	}
	repository := &fakeRepository{target: store.CallControlTarget{
		AppID:          "call-app-1",
		LineID:         "line-1",
		EndpointCallID: "call-endpoint-4",
		Number:         "+818012345678",
		Direction:      "incoming",
		Phase:          "active",
		Bearer:         "volte",
		Revision:       7,
	}, call: store.Call{
		ID:             "call-app-1",
		DeviceID:       "line-1",
		EndpointCallID: "call-endpoint-4",
		RemoteNumber:   "+818012345678",
		Direction:      "incoming",
		Phase:          "active",
		Bearer:         "volte",
	}}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }

	call, err := service.CallAction(context.Background(), CallActionInput{
		RequestID: "request-hangup-1",
		CallID:    "call-app-1",
		Action:    "hangup",
	})
	if err != nil {
		t.Fatalf("CallAction() error = %v", err)
	}
	if agent.actionCallID != repository.target.EndpointCallID || agent.actionName != "hangup" ||
		agent.actionRequest.RequestID != "request-hangup-1" {
		t.Fatalf("agent action = %q %q %+v", agent.actionCallID, agent.actionName, agent.actionRequest)
	}
	if call.Phase != "active" {
		t.Fatalf("call phase = %q, want last authoritative state", call.Phase)
	}
	if repository.callInput.AppID != "" {
		t.Fatalf("call action invented a projected state: %+v", repository.callInput)
	}
}

func TestStartCallRejectsBusyLineBeforeAgentMutation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 13, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	repository := &fakeRepository{activeCalls: []store.Call{{
		ID:       "call-active",
		DeviceID: "line-1",
		Phase:    "active",
	}}}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }

	_, err = service.StartCall(context.Background(), StartCallInput{
		LineID: "line-1",
		Number: "+818012345678",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("StartCall() error = %v, want conflict", err)
	}
	if len(agent.startRequests) != 0 {
		t.Fatalf("agent start requests = %+v, want none", agent.startRequests)
	}
}

func TestRefreshDoesNotServeStaleConnectedStateAfterFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 14, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }
	status, err := service.Refresh(context.Background())
	if err != nil || !status.Connected {
		t.Fatalf("first Refresh() status = %+v, error = %v", status, err)
	}
	agent.healthError = errors.New("socket unavailable")
	status, err = service.Refresh(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second Refresh() error = %v, want unavailable", err)
	}
	if status.Connected || !strings.Contains(status.LastError, "socket unavailable") {
		t.Fatalf("second Refresh() status = %+v", status)
	}
}

func TestRefreshPublishesCommittedIncomingMessage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 7, 30, 0, 0, time.UTC)
	agent := connectedAgent(now)
	repository := &fakeRepository{snapshotResult: store.HardwareSnapshotResult{
		CreatedIncomingMessages: []store.Message{{
			ID:         42,
			LineID:     "line-1",
			LocalPhone: "+81 90 0000 0001",
			IMSI:       "440500000000001",
			ICCID:      "8901000000000000001",
			Peer:       "+818012345678",
			Content:    "hello",
			Direction:  "incoming",
			State:      "received",
			Timestamp:  now.Format(time.RFC3339),
		}},
	}}
	events := messageevents.NewBuffer(8)
	service, err := New(agent, repository, events)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	window, _, cancel := events.Subscribe(0)
	cancel()
	if len(window.Events) != 1 {
		t.Fatalf("published events = %+v, want one", window.Events)
	}
	event := window.Events[0]
	if event.EventKey != "sms:42" || event.MessageID != "42" ||
		event.ThreadKey != "phone:819000000001|+818012345678" ||
		event.LineID != "line-1" || event.Content != "hello" {
		t.Fatalf("published event = %+v", event)
	}
}

func TestRefreshDoesNotPublishWhenSnapshotCommitFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 7, 45, 0, 0, time.UTC)
	agent := connectedAgent(now)
	repository := &fakeRepository{
		snapshotResult: store.HardwareSnapshotResult{
			CreatedIncomingMessages: []store.Message{{ID: 43}},
		},
		snapshotError: errors.New("commit failed"),
	}
	events := messageevents.NewBuffer(8)
	service, err := New(agent, repository, events)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want commit failure")
	}
	window, _, cancel := events.Subscribe(0)
	cancel()
	if len(window.Events) != 0 {
		t.Fatalf("published events = %+v, want none", window.Events)
	}
}

func TestRefreshReconcilesRemoteCallRemovalImmediately(t *testing.T) {
	now := time.Date(2026, time.July, 23, 14, 30, 0, 0, time.UTC)
	agent := connectedAgent(now)
	repository := &fakeRepository{activeCalls: []store.Call{{
		ID:       "call-remote",
		DeviceID: "line-1",
		Phase:    "active",
	}}}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer := &fakeCallLifecycleObserver{}
	if err := service.SetCallLifecycleObserver(observer); err != nil {
		t.Fatalf("SetCallLifecycleObserver() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	repository.activeCalls = nil
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("removal Refresh() error = %v", err)
	}
	if len(observer.snapshots) != 2 {
		t.Fatalf("observer snapshots = %d, want 2", len(observer.snapshots))
	}
	if len(observer.snapshots[0]) != 1 || observer.snapshots[0][0].ID != "call-remote" {
		t.Fatalf("active snapshot = %+v", observer.snapshots[0])
	}
	if len(observer.snapshots[1]) != 0 {
		t.Fatalf("removal snapshot = %+v, want empty", observer.snapshots[1])
	}
}

func TestRefreshPreservesSixDiscoveredLines(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.snapshot.Lines = make([]agentclient.Line, 0, 6)
	for index := 0; index < 6; index++ {
		agent.snapshot.Lines = append(agent.snapshot.Lines, agentclient.Line{
			ID:                  fmt.Sprintf("line-%d", index),
			Model:               fmt.Sprintf("Fixture-%d", index),
			EquipmentIdentifier: fmt.Sprintf("99%013d", index),
			SIMIdentifier:       fmt.Sprintf("89%017d", index),
			IMSI:                fmt.Sprintf("44%013d", index),
			Capabilities: agentclient.LineCapabilities{
				Dial:       true,
				RejectCall: true,
			},
		})
	}
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	status, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(status.Lines) != 6 || len(repository.snapshot.Lines) != 6 {
		t.Fatalf(
			"line counts: status=%d persisted snapshot=%d, want 6",
			len(status.Lines),
			len(repository.snapshot.Lines),
		)
	}
}

func TestProjectLinePreservesDetectedInterfaces(t *testing.T) {
	t.Parallel()
	snr := 8.75
	projected := projectLine(agentclient.Line{
		ID:                      "line-voice",
		Model:                   "QDC507",
		HardwareRevision:        "fixture-hw-1",
		PrimaryPort:             "cdc-wdm0",
		AccessTechnologies:      1 << 14,
		AccessTechnologiesKnown: true,
		SignalQualityKnown:      true,
		SignalQualityRecent:     true,
		SignalQuality:           73,
		SignalSNR:               &snr,
		Ports: []agentclient.ModemPort{{
			Name:     "cdc-wdm0",
			Type:     "qmi",
			TypeCode: 6,
		}},
		Capabilities: agentclient.LineCapabilities{
			ModemInterface:     true,
			SIMInterface:       true,
			VoiceInterface:     true,
			MessagingInterface: true,
			Dial:               true,
			AnswerCall:         true,
			HangupCall:         true,
			RejectCall:         true,
			SendDTMF:           true,
			SendMessage:        true,
		},
	})
	if !projected.Capabilities.Modem ||
		!projected.Capabilities.SIM ||
		!projected.Capabilities.Voice ||
		!projected.Capabilities.Messaging ||
		!projected.Capabilities.Dial ||
		!projected.Capabilities.AnswerCall ||
		!projected.Capabilities.HangupCall ||
		!projected.Capabilities.RejectCall ||
		!projected.Capabilities.SendDTMF ||
		!projected.Capabilities.SendMessage {
		t.Fatalf("projected capabilities = %+v", projected.Capabilities)
	}
	if projected.DeviceAlias != "" || projected.Model != "QDC507" {
		t.Fatalf(
			"projected identity = alias %q, model %q; want empty alias and detected model",
			projected.DeviceAlias,
			projected.Model,
		)
	}
	if projected.HardwareRevision != "fixture-hw-1" ||
		projected.PrimaryPort != "cdc-wdm0" ||
		projected.AccessTechnologies == nil ||
		*projected.AccessTechnologies != 1<<14 ||
		projected.SignalSNR == nil ||
		*projected.SignalSNR != snr ||
		len(projected.Ports) != 1 ||
		projected.Ports[0].Type != "qmi" {
		t.Fatalf("projected hardware details = %+v", projected)
	}
}

func TestProjectSnapshotDropsStaleSignalTelemetry(t *testing.T) {
	t.Parallel()

	dbm := -68.5
	rsrp := -94.0
	rsrq := -11.0
	snr := 7.25
	hardware, lines := projectSnapshot(agentclient.Snapshot{
		ObservedAt: time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC),
		Lines: []agentclient.Line{{
			ID:                  "line-stale-signal",
			EquipmentIdentifier: "990000000000001",
			SignalQualityKnown:  true,
			SignalQuality:       76,
			SignalQualityRecent: false,
			SignalDBM:           &dbm,
			SignalRSRP:          &rsrp,
			SignalRSRQ:          &rsrq,
			SignalSNR:           &snr,
		}},
	}, "boot-stale-signal")

	if len(lines) != 1 || lines[0].Signal != nil || lines[0].SignalSNR != nil {
		t.Fatalf("stale live signal was projected: %+v", lines)
	}
	if len(hardware.Lines) != 1 {
		t.Fatalf("hardware lines = %d, want 1", len(hardware.Lines))
	}
	line := hardware.Lines[0]
	if line.SignalKnown || line.SignalDBM != nil || line.SignalRSRP != nil ||
		line.SignalRSRQ != nil || line.SignalSNR != nil {
		t.Fatalf("stale persisted signal was projected: %+v", line)
	}
}

func TestProjectLineSeparatesHomeAndServingOperators(t *testing.T) {
	t.Parallel()

	projected := projectLine(agentclient.Line{
		ID:                     "line-roaming",
		SIMIdentifier:          "89840400000000000099",
		IMSI:                   "452040000000001",
		HomeOperatorCode:       "45204",
		HomeOperatorName:       "Viettel Mobile",
		ServingOperatorCode:    "44010",
		ServingOperatorName:    "NTT DOCOMO",
		RegistrationStateKnown: true,
		RegistrationStateCode:  5,
		RegistrationState:      "roaming",
		Roaming:                true,
		EmergencyOnly:          true,
	})
	if projected.Operator != "Viettel Mobile" ||
		projected.HomeOperatorCode != "45204" ||
		projected.HomeOperatorName != "Viettel Mobile" ||
		projected.ServingOperatorCode != "44010" ||
		projected.ServingOperatorName != "NTT DOCOMO" ||
		!projected.RegistrationStateKnown ||
		projected.RegistrationStateCode != 5 ||
		projected.RegistrationState != "roaming" ||
		!projected.Roaming ||
		!projected.EmergencyOnly {
		t.Fatalf("projected roaming line = %+v", projected)
	}

	legacy := projectLine(agentclient.Line{
		OperatorIdentifier: "46001",
		OperatorName:       "China Unicom",
	})
	if legacy.Operator != "China Unicom" ||
		legacy.HomeOperatorCode != "46001" ||
		legacy.HomeOperatorName != "China Unicom" {
		t.Fatalf("legacy home operator projection = %+v", legacy)
	}
}

func TestDNDRejectsNewRingingIncomingCallOnceAndRecordsOutcome(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 16, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.snapshot.Calls = []agentclient.Call{{
		ID:        "endpoint-dnd-1",
		LineID:    "line-1",
		Number:    "+818000000001",
		Direction: "incoming",
		State:     "ringing",
	}}
	agent.actionResult = agentclient.CommandReceipt{
		RequestID:  "dnd-request-1",
		ResourceID: "endpoint-dnd-1",
	}
	repository := &fakeRepository{
		incomingCallActions: []store.IncomingCallAction{{
			CallID:          "app-call-dnd-1",
			LineID:          "line-1",
			EndpointCallID:  "endpoint-dnd-1",
			EffectivePolicy: store.EffectiveCallPolicyDND,
			RequestID:       "dnd-request-1",
			Status:          store.IncomingCallActionSending,
		}},
	}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	if len(agent.actionRequests) != 1 ||
		agent.actionName != "reject" ||
		agent.actionCallID != "endpoint-dnd-1" {
		t.Fatalf(
			"reject calls = %d, action=%q, endpoint=%q",
			len(agent.actionRequests),
			agent.actionName,
			agent.actionCallID,
		)
	}
	if len(repository.finishedIncomingCallActions) != 1 ||
		repository.finishedIncomingCallActions[0].Status != store.IncomingCallActionSucceeded {
		t.Fatalf("finished actions = %+v", repository.finishedIncomingCallActions)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	if len(agent.actionRequests) != 1 {
		t.Fatalf("reject submissions = %d, want exactly one", len(agent.actionRequests))
	}
}

func TestDNDRecordsFailureAndIndeterminateWithoutRetry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		actionError   error
		wantStatus    string
		wantErrorCode string
	}{
		{
			name: "typed agent failure",
			actionError: &agentclient.OperationError{
				Status:  http.StatusNotImplemented,
				Code:    "not_supported",
				Message: "reject unavailable",
			},
			wantStatus:    store.IncomingCallActionFailed,
			wantErrorCode: "agent_not_supported",
		},
		{
			name:          "transport outcome unknown",
			actionError:   errors.New("socket closed"),
			wantStatus:    store.IncomingCallActionIndeterminate,
			wantErrorCode: "transport_indeterminate",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.July, 23, 17, 0, 0, 0, time.UTC)
			agent := connectedAgent(now)
			agent.snapshot.Calls = []agentclient.Call{{
				ID:        "endpoint-dnd-failure",
				LineID:    "line-1",
				Direction: "incoming",
				State:     "ringing",
			}}
			agent.actionError = test.actionError
			repository := &fakeRepository{
				incomingCallActions: []store.IncomingCallAction{{
					CallID:          "app-call-dnd-failure",
					LineID:          "line-1",
					EndpointCallID:  "endpoint-dnd-failure",
					EffectivePolicy: store.EffectiveCallPolicyDND,
					RequestID:       "dnd-request-failure",
				}},
			}
			service, err := New(agent, repository, messageevents.NewBuffer(8))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := service.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if len(repository.finishedIncomingCallActions) != 1 {
				t.Fatalf("finished actions = %+v", repository.finishedIncomingCallActions)
			}
			finished := repository.finishedIncomingCallActions[0]
			if finished.Status != test.wantStatus || finished.ErrorCode != test.wantErrorCode {
				t.Fatalf("finished action = %+v", finished)
			}
			if _, err := service.Refresh(context.Background()); err != nil {
				t.Fatalf("second Refresh() error = %v", err)
			}
			if len(agent.actionRequests) != 1 {
				t.Fatalf("reject submissions = %d, want one", len(agent.actionRequests))
			}
		})
	}
}

func TestDeviceConfigurationUsesExplicitLineAndOpaqueRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 18, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.deviceConfiguration = agentclient.DeviceConfiguration{
		LineID:          "line-1",
		Revision:        "sha256:updated",
		ObservedAt:      now,
		DataConnections: []agentclient.DataConnection{},
	}
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	enabled := false
	configuration, err := service.ApplyDeviceConfiguration(
		context.Background(),
		"line-1",
		agentclient.ApplyDeviceConfigurationRequest{
			RequestID:        "device-config-1",
			ExpectedRevision: "sha256:current",
			Operation:        agentclient.DeviceConfigurationSetRadioEnabled,
			RadioEnabled:     &enabled,
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration() error = %v", err)
	}
	if configuration.Revision != "sha256:updated" ||
		len(agent.applyDeviceConfigurationRequests) != 1 {
		t.Fatalf(
			"configuration = %+v, requests = %+v",
			configuration,
			agent.applyDeviceConfigurationRequests,
		)
	}
	applied := agent.applyDeviceConfigurationRequests[0]
	if applied.RequestID != "device-config-1" ||
		applied.ExpectedRevision != "sha256:current" ||
		applied.RadioEnabled == nil ||
		*applied.RadioEnabled {
		t.Fatalf("applied request = %+v", applied)
	}
	command := repository.commands["device-config-1"]
	if command.Status != store.HardwareCommandCompleted ||
		command.ResourceID != "sha256:updated" {
		t.Fatalf("hardware command = %+v", command)
	}
}

func TestHardwareMutationSeparatesRejectedFromUnknownOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		requestID     string
		startError    error
		wantStatus    string
		wantErrorCode string
		wantError     error
	}{
		{
			name:          "modem manager rejected before applying",
			requestID:     "request-rejected",
			startError:    &agentclient.OperationError{Code: "failed_precondition", Message: "modem is not ready"},
			wantStatus:    store.HardwareCommandFailed,
			wantErrorCode: "failed_precondition",
			wantError:     ErrFailedPrecondition,
		},
		{
			name:          "transport closed after submission",
			requestID:     "request-unknown",
			startError:    errors.New("socket closed without response"),
			wantStatus:    store.HardwareCommandIndeterminate,
			wantErrorCode: "transport",
			wantError:     ErrUnavailable,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			agent := connectedAgent(time.Date(2026, time.July, 23, 18, 30, 0, 0, time.UTC))
			agent.startError = test.startError
			repository := &fakeRepository{}
			service, err := New(agent, repository, messageevents.NewBuffer(8))
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.StartCall(context.Background(), StartCallInput{
				RequestID: test.requestID,
				LineID:    "line-1",
				Number:    "+818000000001",
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("StartCall() error = %v, want %v", err, test.wantError)
			}
			command := repository.commands[test.requestID]
			if command.Status != test.wantStatus ||
				command.ErrorCode != test.wantErrorCode {
				t.Fatalf("hardware command = %+v", command)
			}
		})
	}
}

func TestProjectCallExposesMediaOnlyWhenDetectedConfiguredAndSupported(t *testing.T) {
	valid := agentclient.Call{
		ID:              "endpoint-call-1",
		LineID:          "line-1",
		State:           "active",
		AudioPort:       "hw:2,0",
		MediaAvailable:  true,
		MediaConfigured: true,
		AudioFormat: &agentclient.CallAudioFormat{
			Encoding:   "pcm",
			Resolution: "s16le",
			Rate:       8000,
		},
	}
	tests := []struct {
		name   string
		mutate func(*agentclient.Call)
		want   bool
	}{
		{name: "all gates pass", want: true},
		{
			name: "raw media unavailable",
			mutate: func(call *agentclient.Call) {
				call.MediaAvailable = false
			},
		},
		{
			name: "backend not configured",
			mutate: func(call *agentclient.Call) {
				call.MediaConfigured = false
			},
		},
		{
			name: "call not active",
			mutate: func(call *agentclient.Call) {
				call.State = "ringing_out"
			},
		},
		{
			name: "audio port missing",
			mutate: func(call *agentclient.Call) {
				call.AudioPort = ""
			},
		},
		{
			name: "format missing",
			mutate: func(call *agentclient.Call) {
				call.AudioFormat = nil
			},
		},
		{
			name: "encoding unsupported",
			mutate: func(call *agentclient.Call) {
				call.AudioFormat.Encoding = "flac"
			},
		},
		{
			name: "resolution unsupported",
			mutate: func(call *agentclient.Call) {
				call.AudioFormat.Resolution = "s24le"
			},
		},
		{
			name: "rate unsupported",
			mutate: func(call *agentclient.Call) {
				call.AudioFormat.Rate = 48000
			},
		},
		{
			name: "16khz supported",
			mutate: func(call *agentclient.Call) {
				call.AudioFormat.Rate = 16000
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := valid
			format := *valid.AudioFormat
			call.AudioFormat = &format
			if test.mutate != nil {
				test.mutate(&call)
			}
			projected := projectCall(call, store.LineSummary{}, "", time.Time{}, 0)
			if projected.MediaAvailable != test.want {
				t.Fatalf(
					"MediaAvailable = %t, want %t for %+v",
					projected.MediaAvailable,
					test.want,
					call,
				)
			}
		})
	}
}

func connectedAgent(observedAt time.Time) *fakeAgent {
	return &fakeAgent{
		health: agentclient.Health{
			Status:       "ok",
			APIVersion:   agentclient.APIVersion,
			AgentVersion: "test",
			Provider: agentclient.ProviderHealth{
				Name:           "org.freedesktop.ModemManager1",
				Available:      true,
				BootEpoch:      "boot-1",
				RuntimeVersion: "1.24.0",
			},
		},
		snapshot: agentclient.Snapshot{
			Revision:   "snapshot-3",
			ObservedAt: observedAt,
			Lines: []agentclient.Line{{
				ID:                  "line-1",
				Model:               "Fixture modem",
				EquipmentIdentifier: "990000000000001",
				SIMIdentifier:       "8901000000000000001",
				IMSI:                "440500000000001",
				OwnNumbers:          []string{"+819012345678"},
				Capabilities: agentclient.LineCapabilities{
					Dial:        true,
					AnswerCall:  true,
					HangupCall:  true,
					RejectCall:  true,
					SendDTMF:    true,
					SendMessage: true,
				},
			}},
		},
	}
}
