package communication

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestCallProjectionPreservesRingingAndLifecycleEvidence(t *testing.T) {
	at := time.Now().UTC()
	call := agentclient.Call{ID: "endpoint-call", LineID: "line", State: "ringing-out", Direction: "outgoing"}
	projected := projectCall(call, store.LineSummary{}, "", at, 1)
	if projected.Phase != "ringing-out" {
		t.Fatalf("remote ringing collapsed into dialing: %+v", projected)
	}
	call.State, call.ConnectedAt, call.TerminatedAt = "terminated", at.Add(-time.Second), at
	projected = projectCall(call, store.LineSummary{}, "", at, 2)
	if projected.Phase != "ended" || !projected.ConnectedAt.Equal(call.ConnectedAt) || !projected.TerminatedAt.Equal(at) {
		t.Fatalf("projection dropped source lifecycle: %+v", projected)
	}
}

func TestCallConnectionDiagnosticsCorrelateWithoutPhoneNumber(t *testing.T) {
	var output bytes.Buffer
	s := &Service{logger: slog.New(slog.NewJSONHandler(&output, nil))}
	snapshot := agentclient.Snapshot{Calls: []agentclient.Call{{
		ID: "endpoint-call", LineID: "line", Number: "+818012345678", State: "active",
		ReportedState: "ringing-out", StateSource: "state_changed", StateRevision: 2,
	}}}
	s.logCallObservations(agentclient.Snapshot{}, snapshot)
	log := output.String()
	if !strings.Contains(log, "endpoint-call") || !strings.Contains(log, "ringing-out") || strings.Contains(log, "+818012345678") {
		t.Fatalf("diagnostic correlation/privacy mismatch: %s", log)
	}
	s.logCallObservations(snapshot, snapshot)
	if output.String() != log {
		t.Fatal("unchanged snapshot duplicated connection logs")
	}
}
