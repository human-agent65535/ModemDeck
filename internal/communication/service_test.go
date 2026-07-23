package communication

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeAgent struct {
	health          agentclient.Health
	healthError     error
	snapshot        agentclient.Snapshot
	snapshotError   error
	startResult     agentclient.Call
	startError      error
	startRequests   []agentclient.StartCallRequest
	actionResult    agentclient.Call
	actionError     error
	actionCallID    string
	actionName      string
	actionRequest   agentclient.CallActionRequest
	dtmfResult      agentclient.Call
	dtmfError       error
	dtmfCallID      string
	dtmfRequest     agentclient.DTMFRequest
	messageResult   agentclient.Message
	messageError    error
	messageRequests []agentclient.SendMessageRequest
}

func (agent *fakeAgent) Health(context.Context) (agentclient.Health, error) {
	return agent.health, agent.healthError
}

func (agent *fakeAgent) Snapshot(context.Context) (agentclient.Snapshot, error) {
	return agent.snapshot, agent.snapshotError
}

func (agent *fakeAgent) StartCall(_ context.Context, request agentclient.StartCallRequest) (agentclient.Call, error) {
	agent.startRequests = append(agent.startRequests, request)
	return agent.startResult, agent.startError
}

func (agent *fakeAgent) CallAction(
	_ context.Context,
	callID string,
	action string,
	request agentclient.CallActionRequest,
) (agentclient.Call, error) {
	agent.actionCallID = callID
	agent.actionName = action
	agent.actionRequest = request
	return agent.actionResult, agent.actionError
}

func (agent *fakeAgent) SendDTMF(
	_ context.Context,
	callID string,
	request agentclient.DTMFRequest,
) (agentclient.Call, error) {
	agent.dtmfCallID = callID
	agent.dtmfRequest = request
	return agent.dtmfResult, agent.dtmfError
}

func (agent *fakeAgent) SendMessage(
	_ context.Context,
	request agentclient.SendMessageRequest,
) (agentclient.Message, error) {
	agent.messageRequests = append(agent.messageRequests, request)
	return agent.messageResult, agent.messageError
}

type fakeRepository struct {
	snapshot         store.HardwareSnapshot
	snapshotError    error
	message          store.Message
	messageInput     store.HardwareMessage
	messageError     error
	call             store.Call
	callInput        store.HardwareCall
	callError        error
	target           store.CallControlTarget
	targetError      error
	activeCalls      []store.Call
	activeCallsError error
}

func (repository *fakeRepository) ApplyHardwareSnapshot(_ context.Context, snapshot store.HardwareSnapshot) error {
	repository.snapshot = snapshot
	return repository.snapshotError
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

func TestServiceRequiresExplicitCapableLine(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.startResult = agentclient.Call{
		ID:        "boot-1:/call/1",
		State:     "dialing",
		StateCode: 2,
	}
	repository := &fakeRepository{}
	service, err := New(agent, repository)
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
		Number:    "+81 (80) 1234-5678",
	})
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if len(agent.startRequests) != 1 {
		t.Fatalf("agent start request count = %d, want 1", len(agent.startRequests))
	}
	request := agent.startRequests[0]
	if request.LineID != "line-1" || request.Number != "+818012345678" ||
		request.RequestID != "request-call-1" {
		t.Fatalf("agent start request = %+v", request)
	}
	if call.DeviceID != "line-1" || call.RemoteNumber != "+818012345678" ||
		call.Direction != "outgoing" || call.Phase != "dialing" {
		t.Fatalf("call = %+v", call)
	}
}

func TestCallActionPreservesStoredIdentityAndAdvancesRevision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 12, 30, 0, 0, time.UTC)
	agent := connectedAgent(now)
	agent.actionResult = agentclient.Call{State: "terminated"}
	repository := &fakeRepository{target: store.CallControlTarget{
		AppID:          "call-app-1",
		LineID:         "line-1",
		EndpointCallID: "boot-1:/call/4",
		Number:         "+818012345678",
		Direction:      "incoming",
		Phase:          "active",
		Bearer:         "volte",
		Revision:       7,
	}}
	service, err := New(agent, repository)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }

	if _, err := service.CallAction(context.Background(), CallActionInput{
		RequestID: "request-hangup-1",
		CallID:    "call-app-1",
		Action:    "hangup",
	}); err != nil {
		t.Fatalf("CallAction() error = %v", err)
	}
	if agent.actionCallID != repository.target.EndpointCallID || agent.actionName != "hangup" ||
		agent.actionRequest.RequestID != "request-hangup-1" {
		t.Fatalf("agent action = %q %q %+v", agent.actionCallID, agent.actionName, agent.actionRequest)
	}
	projected := repository.callInput
	if projected.AppID != repository.target.AppID ||
		projected.LineID != repository.target.LineID ||
		projected.EndpointCallID != repository.target.EndpointCallID ||
		projected.Number != repository.target.Number ||
		projected.Direction != repository.target.Direction ||
		projected.Bearer != repository.target.Bearer ||
		projected.Phase != "ended" ||
		projected.Revision != repository.target.Revision+1 {
		t.Fatalf("projected call = %+v", projected)
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
	service, err := New(agent, repository)
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
	service, err := New(agent, repository)
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

func connectedAgent(observedAt time.Time) *fakeAgent {
	return &fakeAgent{
		health: agentclient.Health{
			Status:       "ok",
			APIVersion:   agentclient.APIVersion,
			AgentVersion: "test",
			Provider: agentclient.ProviderHealth{
				Name:      "org.freedesktop.ModemManager1",
				Available: true,
			},
		},
		snapshot: agentclient.Snapshot{
			Revision:   3,
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
