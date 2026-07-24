package communication

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type lineServiceTestAgent struct {
	*fakeAgent
	simStatus       agentclient.SIMStatus
	simRequests     []agentclient.SIMCommandRequest
	profiles        []agentclient.ConnectionProfile
	profileRequests []agentclient.SaveConnectionProfileRequest
	deleteRequests  []agentclient.DeleteConnectionProfileRequest
	ussdStatus      agentclient.USSDStatus
	ussdRequests    []agentclient.USSDRequest
}

func (agent *lineServiceTestAgent) SIMStatus(
	context.Context,
	string,
) (agentclient.SIMStatus, error) {
	return agent.simStatus, nil
}

func (agent *lineServiceTestAgent) SIMCommand(
	_ context.Context,
	lineID string,
	request agentclient.SIMCommandRequest,
) (agentclient.CommandReceipt, error) {
	agent.simRequests = append(agent.simRequests, request)
	return agentclient.CommandReceipt{
		RequestID:  request.RequestID,
		ResourceID: lineID,
	}, nil
}

func (agent *lineServiceTestAgent) ConnectionProfiles(
	context.Context,
	string,
) ([]agentclient.ConnectionProfile, error) {
	return append([]agentclient.ConnectionProfile(nil), agent.profiles...), nil
}

func (agent *lineServiceTestAgent) SaveConnectionProfile(
	_ context.Context,
	_ string,
	request agentclient.SaveConnectionProfileRequest,
) (agentclient.ConnectionProfile, error) {
	agent.profileRequests = append(agent.profileRequests, request)
	profile := agentclient.ConnectionProfile{
		ProfileID:   7,
		ProfileName: request.ProfileName,
		APN:         request.APN,
		IPFamily:    request.IPFamily,
	}
	agent.profiles = append(agent.profiles, profile)
	return profile, nil
}

func (agent *lineServiceTestAgent) DeleteConnectionProfile(
	_ context.Context,
	lineID string,
	request agentclient.DeleteConnectionProfileRequest,
) (agentclient.CommandReceipt, error) {
	agent.deleteRequests = append(agent.deleteRequests, request)
	return agentclient.CommandReceipt{
		RequestID:  request.RequestID,
		ResourceID: lineID,
	}, nil
}

func (agent *lineServiceTestAgent) USSDStatus(
	context.Context,
	string,
) (agentclient.USSDStatus, error) {
	return agent.ussdStatus, nil
}

func (agent *lineServiceTestAgent) USSDCommand(
	_ context.Context,
	_ string,
	request agentclient.USSDRequest,
) (agentclient.USSDResponse, error) {
	agent.ussdRequests = append(agent.ussdRequests, request)
	return agentclient.USSDResponse{Response: "balance"}, nil
}

func TestLineServicesUseLiveLineAndDeduplicateSensitiveCommands(t *testing.T) {
	observedAt := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	agent := &lineServiceTestAgent{
		fakeAgent: &fakeAgent{},
		simStatus: agentclient.SIMStatus{
			LineID:        "line-1",
			Present:       true,
			SIMType:       agentclient.SIMTypeESIM,
			ESIMStatus:    agentclient.ESIMStatusWithProfiles,
			EIDMasked:     "****5678",
			SIMSlots:      []agentclient.SIMSlot{},
			UnlockRetries: map[string]uint32{"sim-pin": 3},
			ObservedAt:    observedAt,
		},
		ussdStatus: agentclient.USSDStatus{
			LineID:     "line-1",
			State:      "idle",
			ObservedAt: observedAt,
		},
	}
	repository := &fakeRepository{}
	service, err := New(agent, repository, messageevents.NewBuffer(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return observedAt }
	service.status = Status{
		Connected:  true,
		ObservedAt: observedAt,
		Lines:      []store.LineSummary{{ID: "line-1"}},
	}

	status, err := service.SIMStatus(context.Background(), "line-1")
	if err != nil || status.UnlockRetries["sim-pin"] != 3 ||
		status.EIDMasked != "****5678" {
		t.Fatalf("SIMStatus() = %+v, %v", status, err)
	}
	if repository.snapshot.ObservedAt != (time.Time{}) ||
		repository.messageInput.LineID != "" ||
		repository.callInput.LineID != "" ||
		repository.commands != nil {
		t.Fatalf("read-only SIM facts were persisted: %+v", repository)
	}
	firstReceipt, err := service.SIMCommand(
		context.Background(),
		"line-1",
		agentclient.SIMCommandRequest{
			RequestID: "sim-request-1",
			Operation: agentclient.SIMSendPIN,
			PIN:       "1234",
		},
	)
	if err != nil || firstReceipt.ResourceID != "line-1" {
		t.Fatalf("SIMCommand() = %+v, %v", firstReceipt, err)
	}
	replayedReceipt, err := service.SIMCommand(
		context.Background(),
		"line-1",
		agentclient.SIMCommandRequest{
			RequestID: "sim-request-1",
			Operation: agentclient.SIMSendPIN,
			PIN:       "9999",
		},
	)
	if err != nil || replayedReceipt.ResourceID != "line-1" || len(agent.simRequests) != 1 {
		t.Fatalf(
			"replayed SIMCommand() = %+v, %v; requests = %+v",
			replayedReceipt,
			err,
			agent.simRequests,
		)
	}

	profile, err := service.SaveConnectionProfile(
		context.Background(),
		"line-1",
		agentclient.SaveConnectionProfileRequest{
			RequestID:   "profile-request-1",
			ProfileName: "data",
			APN:         "internet",
			IPFamily:    "ipv4v6",
			Password:    "first-secret",
		},
	)
	if err != nil || profile.ProfileID != 7 {
		t.Fatalf("SaveConnectionProfile() = %+v, %v", profile, err)
	}
	replayedProfile, err := service.SaveConnectionProfile(
		context.Background(),
		"line-1",
		agentclient.SaveConnectionProfileRequest{
			RequestID:   "profile-request-1",
			ProfileName: "data",
			APN:         "internet",
			IPFamily:    "ipv4v6",
			Password:    "different-secret",
		},
	)
	if err != nil || replayedProfile.ProfileID != 7 || len(agent.profileRequests) != 1 {
		t.Fatalf(
			"replayed SaveConnectionProfile() = %+v, %v; requests = %+v",
			replayedProfile,
			err,
			agent.profileRequests,
		)
	}

	result, err := service.USSDCommand(
		context.Background(),
		"line-1",
		agentclient.USSDRequest{
			RequestID: "ussd-request-1",
			Action:    agentclient.USSDInitiate,
			Command:   "*123#",
		},
	)
	if err != nil || result.Response != "balance" {
		t.Fatalf("USSDCommand() = %+v, %v", result, err)
	}
	result, err = service.USSDCommand(
		context.Background(),
		"line-1",
		agentclient.USSDRequest{
			RequestID: "ussd-request-1",
			Action:    agentclient.USSDInitiate,
			Command:   "*999#",
		},
	)
	if err != nil || result.Response != "" || len(agent.ussdRequests) != 1 {
		t.Fatalf("replayed USSDCommand() = %+v, %v; requests = %+v", result, err, agent.ussdRequests)
	}
}
