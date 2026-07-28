package communication

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestRefreshPersistsPhysicalInventoryWhenSIMAttachmentIsDuplicated(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 28, 5, 0, 0, 0, time.UTC)
	snapshot := duplicateSubscriptionSnapshot()
	snapshot.Revision = "snapshot-duplicate-subscription"
	snapshot.ObservedAt = observedAt
	snapshot.Lines[0].State = "searching"
	snapshot.Lines[0].RegistrationState = "searching"
	snapshot.Lines[1].State = "registered"
	snapshot.Lines[1].RegistrationState = "home"

	agent := connectedAgent(observedAt)
	agent.snapshot = snapshot
	repository := &fakeRepository{
		snapshotResult: store.HardwareSnapshotResult{
			LineIDsByEndpoint: map[string]string{"new-endpoint": "stable-line"},
		},
	}
	service, err := New(agent, repository, messageevents.NewBuffer(4))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !status.Connected ||
		len(status.Lines) != 1 ||
		status.Lines[0].ID != "stable-line" ||
		status.Lines[0].EndpointID != "new-endpoint" {
		t.Fatalf("status = %+v, want only the registered service line", status)
	}
	if len(repository.snapshot.Lines) != 2 {
		t.Fatalf("persisted hardware lines = %d, want both physical modems", len(repository.snapshot.Lines))
	}
	if repository.snapshot.Lines[0].ICCID != "" ||
		repository.snapshot.Lines[0].IMSI != "" ||
		repository.snapshot.Lines[1].ICCID == "" ||
		repository.snapshot.Lines[1].IMSI == "" {
		t.Fatalf("persisted hardware attachment quarantine = %+v", repository.snapshot.Lines)
	}
}

func TestQuarantineDuplicateSubscriptionAttachmentsKeepsRegisteredEndpoint(t *testing.T) {
	t.Parallel()

	snapshot := duplicateSubscriptionSnapshot()
	snapshot.Lines[0].State = "searching"
	snapshot.Lines[0].RegistrationState = "searching"
	snapshot.Lines[1].State = "registered"
	snapshot.Lines[1].RegistrationState = "home"

	sanitized, quarantined := quarantineDuplicateSubscriptionAttachments(snapshot)

	if !slices.Equal(quarantined, []string{"old-endpoint"}) {
		t.Fatalf("quarantined endpoints = %v, want old-endpoint", quarantined)
	}
	assertQuarantinedSubscription(t, sanitized.Lines[0])
	if sanitized.Lines[1].SIMIdentifier != snapshot.Lines[1].SIMIdentifier ||
		sanitized.Lines[1].IMSI != snapshot.Lines[1].IMSI ||
		!sanitized.Lines[1].Capabilities.SendMessage {
		t.Fatalf("registered endpoint was altered: %+v", sanitized.Lines[1])
	}
	if len(sanitized.Calls) != 1 || sanitized.Calls[0].LineID != "new-endpoint" {
		t.Fatalf("calls = %+v, want only registered endpoint call", sanitized.Calls)
	}
	if len(sanitized.Messages) != 1 || sanitized.Messages[0].LineID != "new-endpoint" {
		t.Fatalf("messages = %+v, want only registered endpoint message", sanitized.Messages)
	}
	if snapshot.Lines[0].SIMIdentifier == "" ||
		len(snapshot.Calls) != 2 ||
		len(snapshot.Messages) != 2 {
		t.Fatal("input snapshot was mutated")
	}
}

func TestQuarantineDuplicateSubscriptionAttachmentsKeepsOnlySIMPresentEndpoint(t *testing.T) {
	t.Parallel()

	snapshot := duplicateSubscriptionSnapshot()
	snapshot.Lines[0].State = "searching"
	snapshot.Lines[0].SIMPresent = false
	snapshot.Lines[1].State = "initializing"
	snapshot.Lines[1].SIMPresent = true

	sanitized, quarantined := quarantineDuplicateSubscriptionAttachments(snapshot)

	if !slices.Equal(quarantined, []string{"old-endpoint"}) {
		t.Fatalf("quarantined endpoints = %v, want old-endpoint", quarantined)
	}
	assertQuarantinedSubscription(t, sanitized.Lines[0])
	if !sanitized.Lines[1].SIMPresent || sanitized.Lines[1].SIMIdentifier == "" {
		t.Fatalf("SIM-present endpoint was altered: %+v", sanitized.Lines[1])
	}
}

func TestQuarantineDuplicateSubscriptionAttachmentsQuarantinesAmbiguousEndpoints(t *testing.T) {
	t.Parallel()

	snapshot := duplicateSubscriptionSnapshot()
	for index := range snapshot.Lines {
		snapshot.Lines[index].State = "registered"
		snapshot.Lines[index].RegistrationState = "home"
	}

	sanitized, quarantined := quarantineDuplicateSubscriptionAttachments(snapshot)

	if !slices.Equal(quarantined, []string{"new-endpoint", "old-endpoint"}) {
		t.Fatalf("quarantined endpoints = %v, want both endpoints", quarantined)
	}
	for _, line := range sanitized.Lines {
		assertQuarantinedSubscription(t, line)
	}
	if len(sanitized.Calls) != 0 || len(sanitized.Messages) != 0 {
		t.Fatalf(
			"ambiguous subscription activity survived: calls=%+v messages=%+v",
			sanitized.Calls,
			sanitized.Messages,
		)
	}
}

func TestQuarantineDuplicateSubscriptionAttachmentsLeavesDistinctSIMsAlone(t *testing.T) {
	t.Parallel()

	snapshot := duplicateSubscriptionSnapshot()
	snapshot.Lines[1].SIMIdentifier = "8986010000000000002"
	snapshot.Lines[1].IMSI = "460010000000002"
	snapshot.Lines[1].OwnNumbers = []string{"+8613800000002"}

	sanitized, quarantined := quarantineDuplicateSubscriptionAttachments(snapshot)

	if len(quarantined) != 0 {
		t.Fatalf("quarantined endpoints = %v, want none", quarantined)
	}
	if len(sanitized.Calls) != 2 || len(sanitized.Messages) != 2 {
		t.Fatalf("distinct snapshot activity changed: %+v", sanitized)
	}
	for index := range snapshot.Lines {
		if sanitized.Lines[index].SIMIdentifier != snapshot.Lines[index].SIMIdentifier {
			t.Fatalf("line %d changed: %+v", index, sanitized.Lines[index])
		}
	}
}

func duplicateSubscriptionSnapshot() agentclient.Snapshot {
	line := agentclient.Line{
		SIMPresent:         true,
		SIMPath:            "/org/freedesktop/ModemManager1/SIM/1",
		SIMIdentifier:      "8986010000000000001",
		IMSI:               "460010000000001",
		OwnNumbers:         []string{"+8613800000001"},
		HomeOperatorCode:   "46001",
		HomeOperatorName:   "China Unicom",
		OperatorIdentifier: "46001",
		OperatorName:       "China Unicom",
		CallIDs:            []string{"call-1"},
		MessageIDs:         []string{"message-1"},
		EmergencyNumbers:   []string{"112"},
		RegistrationState:  "searching",
		Capabilities: agentclient.LineCapabilities{
			ModemInterface:     true,
			SIMInterface:       true,
			VoiceInterface:     true,
			MessagingInterface: true,
			Dial:               true,
			SendMessage:        true,
		},
	}
	oldLine := line
	oldLine.ID = "old-endpoint"
	oldLine.EquipmentIdentifier = "860000000000001"
	newLine := line
	newLine.ID = "new-endpoint"
	newLine.EquipmentIdentifier = "860000000000002"
	return agentclient.Snapshot{
		Lines: []agentclient.Line{oldLine, newLine},
		Calls: []agentclient.Call{
			{ID: "call-old", LineID: oldLine.ID},
			{ID: "call-new", LineID: newLine.ID},
		},
		Messages: []agentclient.Message{
			{ID: "message-old", LineID: oldLine.ID},
			{ID: "message-new", LineID: newLine.ID},
		},
	}
}

func assertQuarantinedSubscription(t *testing.T, line agentclient.Line) {
	t.Helper()
	if line.SIMPresent ||
		line.SIMIdentifier != "" ||
		line.IMSI != "" ||
		len(line.OwnNumbers) != 0 ||
		line.RegistrationStateKnown ||
		line.RegistrationState != "" ||
		line.Capabilities.SIMInterface ||
		line.Capabilities.VoiceInterface ||
		line.Capabilities.MessagingInterface ||
		line.Capabilities.SendMessage {
		t.Fatalf("line was not quarantined: %+v", line)
	}
	if !line.Capabilities.ModemInterface {
		t.Fatalf("quarantine removed physical modem capability: %+v", line)
	}
}
