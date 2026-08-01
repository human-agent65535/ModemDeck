package communication

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestSnapshotTransitionLogsUsefulStateWithoutPrivatePayloads(t *testing.T) {
	buffer := diagnostics.NewLogBuffer(32)
	service := &Service{}
	service.SetLogger(slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil))).With(
		"component", "communications",
	))
	previousLine := store.LineSummary{
		ID:                     "line-1",
		EndpointID:             "endpoint-1",
		ICCID:                  "8944012345678901234",
		State:                  "registered",
		RegistrationStateKnown: true,
		RegistrationState:      "home",
		ServingRadio: &store.ServingRadio{
			AccessTechnology: "lte",
			DuplexMode:       "fdd",
			Band:             "B1",
		},
	}
	currentLine := previousLine
	service.logSnapshotTransitions(
		Status{Connected: true, BootEpoch: "boot-1", Lines: []store.LineSummary{previousLine}},
		agentclient.Snapshot{Calls: []agentclient.Call{{
			ID: "call-1", LineID: "endpoint-1", Number: "+818012345678", State: "ringing",
		}}},
		Status{Connected: true, BootEpoch: "boot-1", Lines: []store.LineSummary{currentLine}},
		agentclient.Snapshot{ObservedAt: time.Now(), Calls: []agentclient.Call{{
			ID: "call-1", LineID: "endpoint-1", Number: "+818012345678", State: "active", Bearer: "volte",
		}}},
		[]store.Message{{ID: 42, LineID: "line-1", Peer: "+818099999999", Content: "secret body"}},
		[]string{"report-1", "report-1"},
	)

	window := buffer.Snapshot(0)
	joined := fmt.Sprint(window.Entries)
	for _, message := range []string{
		"call state changed",
		"SMS received",
		"SMS delivery reports processed",
		"volte",
	} {
		if !strings.Contains(joined, message) {
			t.Fatalf("logs do not contain %q: %s", message, joined)
		}
	}
	for _, private := range []string{
		"8944012345678901234",
		"+818012345678",
		"+818099999999",
		"secret body",
	} {
		if strings.Contains(joined, private) {
			t.Fatalf("logs exposed %q: %s", private, joined)
		}
	}
	for _, snapshotFact := range []string{"registration", "roaming", "technology", "band"} {
		if strings.Contains(joined, snapshotFact) {
			t.Fatalf("logs repeated snapshot fact %q: %s", snapshotFact, joined)
		}
	}
}

func TestSnapshotTransitionLogsDeliveryReportOnlyOnce(t *testing.T) {
	buffer := diagnostics.NewLogBuffer(8)
	service := &Service{}
	service.SetLogger(slog.New(buffer.Handler(slog.NewJSONHandler(io.Discard, nil))))
	status := Status{Connected: true, BootEpoch: "boot-1"}
	for range 2 {
		service.logSnapshotTransitions(
			status,
			agentclient.Snapshot{},
			status,
			agentclient.Snapshot{},
			nil,
			[]string{"report-1"},
		)
	}
	window := buffer.Snapshot(0)
	if len(window.Entries) != 1 || window.Entries[0].Message != "SMS delivery reports processed" {
		t.Fatalf("entries = %+v", window.Entries)
	}
}
