package communication

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
)

type callMediaTestAgent struct {
	*fakeAgent
	callIDs  []string
	requests []agentclient.CallActionRequest
	result   agentclient.CallMediaActivation
	err      error
}

func (agent *callMediaTestAgent) ActivateCallMedia(
	_ context.Context,
	callID string,
	request agentclient.CallActionRequest,
) (agentclient.CallMediaActivation, error) {
	agent.callIDs = append(agent.callIDs, callID)
	agent.requests = append(agent.requests, request)
	return agent.result, agent.err
}

func TestRefreshActivatesCallMediaOnceAfterConnectedState(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	baseAgent := connectedAgent(now)
	baseAgent.snapshot.Lines[0].VoiceVerification = &agentclient.VoiceRuntimeVerification{
		USBConfiguration: "enabled",
		MediaRouting:     "call_required",
	}
	baseAgent.snapshot.Calls = []agentclient.Call{{
		ID:          "call-endpoint-1",
		LineID:      "line-1",
		Number:      "+818012345678",
		Direction:   "outgoing",
		State:       "active",
		StateCode:   4,
		AudioFormat: nil,
	}}
	agent := &callMediaTestAgent{
		fakeAgent: baseAgent,
		result: agentclient.CallMediaActivation{
			CallID:          "call-endpoint-1",
			MediaRouting:    "enabled",
			MediaAvailable:  true,
			MediaConfigured: true,
			AudioPort:       "quectel-uac:/sys/devices/usb1/1-2",
		},
	}
	service, err := New(agent, &fakeRepository{}, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(agent.callIDs) != 1 ||
		agent.callIDs[0] != "call-endpoint-1" ||
		len(agent.requests) != 1 ||
		agent.requests[0].RequestID == "" {
		t.Fatalf("media activation calls = %+v, requests = %+v", agent.callIDs, agent.requests)
	}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("repeated Refresh() error = %v", err)
	}
	if len(agent.callIDs) != 1 {
		t.Fatalf("media activation call count = %d, want 1", len(agent.callIDs))
	}
}

var _ AgentCallMediaActivator = (*callMediaTestAgent)(nil)
