package store

import (
	"context"
	"testing"
	"time"
)

func TestCoalescedTerminalSnapshotPreservesAnswerAndEndTimes(t *testing.T) {
	repository := newHardwareTestStore(t)
	ctx := context.Background()
	answered := time.Date(2026, 10, 11, 6, 1, 50, 952000000, time.UTC)
	ended := answered.Add(19 * time.Second)
	snapshot := HardwareSnapshot{
		BootEpoch: "boot-call-lifecycle", Revision: "short-call", ObservedAt: ended.Add(time.Second),
		Lines: []HardwareLine{policyTestLine()},
		Calls: []HardwareCall{{AppID: "short-call", LineID: policyTestLine().ID, EndpointCallID: "endpoint-call-1",
			LineICCID: policyTestLine().ICCID, LineIMSI: policyTestLine().IMSI, Direction: "incoming", Number: "+818012345678", Phase: "ended", StateReason: "terminated",
			ConnectedAt: answered, TerminatedAt: ended, ObservedAt: ended.Add(time.Second)}},
	}
	if err := repository.ApplyHardwareSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	call, err := repository.CallByID(ctx, "short-call")
	if err != nil {
		t.Fatal(err)
	}
	if call.Missed || call.ActiveAt == nil || call.EndedAt == "" || call.DurationSeconds != 19 {
		t.Fatalf("coalesced answer was classified as missed or timed from polling: %+v", call)
	}
	if *call.ActiveAt != databaseTime(answered) || call.EndedAt != databaseTime(ended) {
		t.Fatalf("source event times changed: active=%s ended=%s", *call.ActiveAt, call.EndedAt)
	}
	// A later terminal refresh must not rewrite either durable event time.
	snapshot.Revision = "later-terminal"
	snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Second)
	snapshot.Calls[0].ObservedAt = snapshot.ObservedAt
	if err := repository.ApplyHardwareSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	call, err = repository.CallByID(ctx, "short-call")
	if err != nil || call.ActiveAt == nil || *call.ActiveAt != databaseTime(answered) || call.EndedAt == "" || call.EndedAt != databaseTime(ended) {
		t.Fatalf("later snapshot lost lifecycle: %+v %v", call, err)
	}
}
