package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCallPolicyDefaultsOverridesAndOptimisticLock(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()
	line := policyTestLine()
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-policy",
		Revision:   "snapshot-policy-1",
		ObservedAt: time.Date(2026, time.July, 23, 2, 0, 0, 0, time.UTC),
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	stableLineID := stableLineIDForICCID(t, repository, line.ICCID)

	global, err := repository.GlobalCallSettings(ctx)
	if err != nil {
		t.Fatalf("GlobalCallSettings() error = %v", err)
	}
	if !global.ReceiveCalls || global.Revision != 1 {
		t.Fatalf("global defaults = %+v, want receive_calls=true revision=1", global)
	}
	linePolicy, err := repository.LineCallPolicy(ctx, stableLineID)
	if err != nil {
		t.Fatalf("LineCallPolicy() error = %v", err)
	}
	if linePolicy.Policy != LineCallPolicyFollowGlobal || linePolicy.Revision != 1 {
		t.Fatalf("line defaults = %+v", linePolicy)
	}

	global, err = repository.UpdateGlobalCallSettings(ctx, false, global.Revision)
	if err != nil {
		t.Fatalf("UpdateGlobalCallSettings() error = %v", err)
	}
	effective, err := repository.EffectiveCallPolicy(ctx, stableLineID)
	if err != nil {
		t.Fatalf("EffectiveCallPolicy() error = %v", err)
	}
	if effective.Policy != EffectiveCallPolicyDND ||
		effective.GlobalRevision != global.Revision ||
		effective.LineRevision != linePolicy.Revision {
		t.Fatalf("effective global DND = %+v", effective)
	}
	configuration, err := repository.CallPolicyConfiguration(ctx, stableLineID)
	if err != nil {
		t.Fatalf("CallPolicyConfiguration() error = %v", err)
	}
	if configuration.Global.Revision != global.Revision ||
		configuration.Line.Revision != linePolicy.Revision ||
		configuration.Effective.Policy != EffectiveCallPolicyDND ||
		configuration.Effective.GlobalRevision != configuration.Global.Revision ||
		configuration.Effective.LineRevision != configuration.Line.Revision {
		t.Fatalf("call policy configuration is inconsistent: %+v", configuration)
	}

	linePolicy, err = repository.UpdateLineCallPolicy(
		ctx,
		stableLineID,
		LineCallPolicyReceive,
		linePolicy.Revision,
	)
	if err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	effective, err = repository.EffectiveCallPolicy(ctx, stableLineID)
	if err != nil {
		t.Fatalf("EffectiveCallPolicy() error = %v", err)
	}
	if effective.Policy != EffectiveCallPolicyReceive {
		t.Fatalf("line receive override effective policy = %+v", effective)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		stableLineID,
		LineCallPolicyDND,
		linePolicy.Revision-1,
	); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale line update error = %v, want ErrRevisionConflict", err)
	}
	if _, err := repository.UpdateGlobalCallSettings(
		ctx,
		true,
		global.Revision-1,
	); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale global update error = %v, want ErrRevisionConflict", err)
	}
}

func TestDNDEnqueuesEachNewRingingIncomingCallOnlyOnce(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()
	line := policyTestLine()
	observed := time.Date(2026, time.July, 23, 3, 0, 0, 0, time.UTC)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-dnd",
		Revision:   "snapshot-dnd-0",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	stableLineID := stableLineIDForICCID(t, repository, line.ICCID)
	linePolicy, err := repository.LineCallPolicy(ctx, stableLineID)
	if err != nil {
		t.Fatalf("LineCallPolicy() error = %v", err)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		stableLineID,
		LineCallPolicyDND,
		linePolicy.Revision,
	); err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	call := HardwareCall{
		AppID:          "call-dnd-1",
		LineID:         line.ID,
		EndpointCallID: "endpoint-dnd-1",
		Number:         "+818000000001",
		Direction:      "incoming",
		Phase:          "ringing",
		ObservedAt:     observed.Add(time.Second),
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-dnd",
		Revision:   "snapshot-dnd-1",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("ringing ApplyHardwareSnapshot() error = %v", err)
	}
	claimed, err := repository.ClaimIncomingCallActions(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimIncomingCallActions() error = %v", err)
	}
	if len(claimed) != 1 ||
		claimed[0].CallID != call.AppID ||
		claimed[0].LineID != stableLineID ||
		claimed[0].EndpointLineID != line.ID ||
		claimed[0].EffectivePolicy != EffectiveCallPolicyDND ||
		claimed[0].Status != IncomingCallActionSending {
		t.Fatalf("claimed actions = %+v", claimed)
	}
	if again, err := repository.ClaimIncomingCallActions(ctx, 10); err != nil || len(again) != 0 {
		t.Fatalf("second claim = %+v, error = %v", again, err)
	}
	if err := repository.FinishIncomingCallAction(
		ctx,
		call.AppID,
		IncomingCallActionFailed,
		"agent_not_supported",
	); err != nil {
		t.Fatalf("FinishIncomingCallAction() error = %v", err)
	}
	latest, err := repository.LatestIncomingCallAction(ctx, stableLineID)
	if err != nil {
		t.Fatalf("LatestIncomingCallAction() error = %v", err)
	}
	if latest == nil || latest.Status != IncomingCallActionFailed ||
		latest.ErrorCode != "agent_not_supported" {
		t.Fatalf("latest action = %+v", latest)
	}

	call.ObservedAt = observed.Add(2 * time.Second)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-dnd",
		Revision:   "snapshot-dnd-2",
		ObservedAt: observed.Add(2 * time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("replayed call ApplyHardwareSnapshot() error = %v", err)
	}
	if replayed, err := repository.ClaimIncomingCallActions(ctx, 10); err != nil || len(replayed) != 0 {
		t.Fatalf("replayed call actions = %+v, error = %v", replayed, err)
	}

	second := call
	second.AppID = "call-dnd-2"
	second.EndpointCallID = "endpoint-dnd-2"
	second.ObservedAt = observed.Add(3 * time.Second)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-dnd",
		Revision:   "snapshot-dnd-3",
		ObservedAt: observed.Add(3 * time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call, second},
	}); err != nil {
		t.Fatalf("new call ApplyHardwareSnapshot() error = %v", err)
	}
	if next, err := repository.ClaimIncomingCallActions(ctx, 10); err != nil ||
		len(next) != 1 || next[0].CallID != second.AppID {
		t.Fatalf("new call actions = %+v, error = %v", next, err)
	}
}

func TestInterruptedDNDSubmissionBecomesIndeterminateWithoutRetry(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()
	line := policyTestLine()
	observed := time.Date(2026, time.July, 23, 4, 0, 0, 0, time.UTC)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-interrupted",
		Revision:   "snapshot-interrupted-0",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	stableLineID := stableLineIDForICCID(t, repository, line.ICCID)
	policy, _ := repository.LineCallPolicy(ctx, stableLineID)
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		stableLineID,
		LineCallPolicyDND,
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	call := HardwareCall{
		AppID:          "call-interrupted",
		LineID:         line.ID,
		EndpointCallID: "endpoint-interrupted",
		Number:         "+818000000002",
		Direction:      "incoming",
		Phase:          "ringing",
		ObservedAt:     observed.Add(time.Second),
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-interrupted",
		Revision:   "snapshot-interrupted-1",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("ringing ApplyHardwareSnapshot() error = %v", err)
	}
	if actions, err := repository.ClaimIncomingCallActions(ctx, 10); err != nil || len(actions) != 1 {
		t.Fatalf("claimed actions = %+v, error = %v", actions, err)
	}
	if err := repository.RecoverInterruptedCommunicationOperations(ctx); err != nil {
		t.Fatalf("RecoverInterruptedCommunicationOperations() error = %v", err)
	}
	action, err := repository.IncomingCallAction(ctx, call.AppID)
	if err != nil {
		t.Fatalf("IncomingCallAction() error = %v", err)
	}
	if action.Status != IncomingCallActionIndeterminate ||
		action.ErrorCode != "process_interrupted" {
		t.Fatalf("interrupted action = %+v", action)
	}
	if actions, err := repository.ClaimIncomingCallActions(ctx, 10); err != nil || len(actions) != 0 {
		t.Fatalf("post-migration actions = %+v, error = %v", actions, err)
	}
}

func policyTestLine() HardwareLine {
	return HardwareLine{
		ID:                  "line-policy-1",
		Model:               "Fixture modem",
		Firmware:            "fixture-fw",
		EquipmentIdentifier: "990000000000101",
		ICCID:               "8901000000000000101",
		IMSI:                "440500000000101",
	}
}
