package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
)

func TestHardwareSnapshotIsIdempotentAndAuthoritative(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 10, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "line-boot-1",
		Model:               "Fixture modem",
		Firmware:            "fixture-1",
		EquipmentIdentifier: "990000000000001",
		PrimaryPort:         "cdc-wdm0",
		State:               "registered",
		SignalKnown:         true,
		SignalQuality:       74,
		PhoneNumber:         "+819012345678",
		ICCID:               "8901000000000000001",
		IMSI:                "440500000000001",
		Operator:            "Fixture Telecom",
	}
	call := HardwareCall{
		AppID:          "call-fixture-1",
		RequestID:      "request-call-1",
		LineID:         line.ID,
		EndpointCallID: "boot-1:/call/1",
		Number:         "+818012345678",
		Direction:      "incoming",
		Phase:          "ringing",
		Bearer:         "volte",
		Revision:       1,
		ObservedAt:     observed,
	}
	message := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "boot-1:/sms/1",
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		LocalPhone:        line.PhoneNumber,
		Number:            "+818012345678",
		Text:              "hello",
		Direction:         "incoming",
		State:             "received",
		StateCode:         3,
		Revision:          1,
		Timestamp:         observed.Add(-time.Minute),
		ObservedAt:        observed,
	}
	snapshot := HardwareSnapshot{
		BootEpoch:  "boot-1",
		Revision:   "snapshot-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
		Messages:   []HardwareMessage{message},
	}

	if err := repository.ApplyHardwareSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	if err := repository.ApplyHardwareSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("replay ApplyHardwareSnapshot() error = %v", err)
	}

	messages, err := repository.Messages(ctx, MessageQuery{ICCID: line.ICCID, Peer: message.Number})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].EndpointMessageID != message.EndpointMessageID {
		t.Fatalf("messages = %+v, want one stable endpoint message", messages)
	}
	threads, err := repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() error = %v", err)
	}
	if len(threads) != 1 || threads[0].UnreadCount != 1 {
		t.Fatalf("threads = %+v, want one unread message after replay", threads)
	}
	active, err := repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() error = %v", err)
	}
	if len(active) != 1 || active[0].Phase != "ringing" || active[0].Bearer != "volte" {
		t.Fatalf("active calls = %+v", active)
	}
	target, err := repository.CallControlTarget(ctx, call.AppID)
	if err != nil {
		t.Fatalf("CallControlTarget() error = %v", err)
	}
	if target.LineID != call.LineID || target.EndpointCallID != call.EndpointCallID ||
		target.Number != call.Number || target.Direction != call.Direction ||
		target.Bearer != call.Bearer || target.Revision < call.Revision {
		t.Fatalf("call target = %+v", target)
	}
	devices, err := repository.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices() error = %v", err)
	}
	if len(devices) != 1 || devices[0].SignalQuality == nil || *devices[0].SignalQuality != 74 {
		t.Fatalf("devices = %+v, want persisted signal quality", devices)
	}

	if err := repository.MarkMessageThreadRead(ctx, line.ICCID, message.Number); err != nil {
		t.Fatalf("MarkMessageThreadRead() error = %v", err)
	}
	threads, err = repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil || len(threads) != 1 || threads[0].UnreadCount != 0 {
		t.Fatalf("threads after read = %+v, error = %v", threads, err)
	}

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-1",
		Revision:   "snapshot-2",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("terminal ApplyHardwareSnapshot() error = %v", err)
	}
	active, err = repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() after terminal snapshot error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active calls after terminal snapshot = %+v, want none", active)
	}
	calls, err := repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() error = %v", err)
	}
	if len(calls) != 1 || calls[0].Phase != "ended" || calls[0].EndedAt == "" {
		t.Fatalf("calls = %+v, want closed call", calls)
	}
}

func TestHardwareSnapshotClosesSingleMissingCallOnRetainedLine(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-single", "990000000000201")
	retained := hardwareLifecycleTestCall("call-retained", line.ID, observed)
	missing := hardwareLifecycleTestCall("call-missing", line.ID, observed)

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-single",
		Revision:   "snapshot-single-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{retained, missing},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	retained.ObservedAt = observed.Add(time.Second)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-single",
		Revision:   "snapshot-single-2",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{retained},
	}); err != nil {
		t.Fatalf("authoritative ApplyHardwareSnapshot() error = %v", err)
	}

	active, err := repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != retained.AppID {
		t.Fatalf("active calls = %+v, want only %q", active, retained.AppID)
	}
	assertMissingHardwareCallClosed(t, repository, missing)
}

func TestHardwareSnapshotClosesCallsForMissingLineAndRetainsOtherLines(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 10, 0, 0, time.UTC)
	retainedLine := hardwareLifecycleTestLine("line-retained", "990000000000202")
	missingLine := hardwareLifecycleTestLine("line-removed", "990000000000203")
	retainedCall := hardwareLifecycleTestCall("call-on-retained-line", retainedLine.ID, observed)
	missingCall := hardwareLifecycleTestCall("call-on-removed-line", missingLine.ID, observed)

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-lines",
		Revision:   "snapshot-lines-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{retainedLine, missingLine},
		Calls:      []HardwareCall{retainedCall, missingCall},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	retainedCall.ObservedAt = observed.Add(time.Second)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-lines",
		Revision:   "snapshot-lines-2",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{retainedLine},
		Calls:      []HardwareCall{retainedCall},
	}); err != nil {
		t.Fatalf("line removal ApplyHardwareSnapshot() error = %v", err)
	}

	active, err := repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != retainedCall.AppID {
		t.Fatalf("active calls = %+v, want retained line call %q", active, retainedCall.AppID)
	}
	assertMissingHardwareCallClosed(t, repository, missingCall)
}

func TestHardwareSnapshotWithZeroLinesClosesOnlyModemManagerCallsAcrossBoots(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 20, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-old-boot", "990000000000204")
	hardwareCall := hardwareLifecycleTestCall("call-old-boot", line.ID, observed)

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-before-restart",
		Revision:   "snapshot-before-restart",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{hardwareCall},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	const externalCallID = "call-external-provider"
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO call_history (
			id, device_id, direction, remote_number, endpoint_id, endpoint_call_id,
			phase, revision, created_at, updated_at, active_at, bearer,
			state_reason, state_reason_code, audio_port, audio_encoding,
			audio_resolution, audio_rate, media_available
		 ) VALUES (?, ?, 'incoming', ?, 'sip', ?, 'active', 1, ?, ?, ?, 'sip',
			'external_active', 99, 'sip:audio', 'pcm', 's16le', 16000, 1)`,
		externalCallID,
		"external-line",
		"+15550000300",
		"external-endpoint-call",
		databaseTime(observed),
		databaseTime(observed),
		databaseTime(observed),
	); err != nil {
		t.Fatalf("insert external provider call: %v", err)
	}

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-after-restart",
		Revision:   "snapshot-after-restart",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{},
		Calls:      []HardwareCall{},
	}); err != nil {
		t.Fatalf("zero-line ApplyHardwareSnapshot() error = %v", err)
	}

	active, err := repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != externalCallID ||
		active[0].EndpointID != "sip" || !active[0].MediaAvailable {
		t.Fatalf("active calls = %+v, want only untouched external provider call", active)
	}
	assertMissingHardwareCallClosed(t, repository, hardwareCall)
}

func TestHardwareSnapshotIncomingRingingEnqueuesDNDAction(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 30, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-dnd-canonical", "990000000000205")
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-dnd-canonical",
		Revision:   "snapshot-dnd-canonical-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	policy, err := repository.LineCallPolicy(ctx, line.ID)
	if err != nil {
		t.Fatalf("LineCallPolicy() error = %v", err)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		line.ID,
		LineCallPolicyDND,
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	call := HardwareCall{
		AppID:          "call-dnd-canonical",
		LineID:         line.ID,
		EndpointCallID: "endpoint-dnd-canonical",
		Number:         "+15550000400",
		Direction:      "incoming",
		Phase:          "ringing",
		ObservedAt:     observed.Add(time.Second),
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-dnd-canonical",
		Revision:   "snapshot-dnd-canonical-2",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("ringing ApplyHardwareSnapshot() error = %v", err)
	}
	actions, err := repository.ClaimIncomingCallActions(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimIncomingCallActions() error = %v", err)
	}
	if len(actions) != 1 || actions[0].CallID != call.AppID ||
		actions[0].EndpointCallID != call.EndpointCallID ||
		actions[0].EffectivePolicy != EffectiveCallPolicyDND {
		t.Fatalf("incoming DND actions = %+v", actions)
	}
}

func TestHardwareSnapshotDoesNotPersistLineWithoutStableID(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-unidentified-line",
		Revision:   "snapshot-unidentified-line",
		ObservedAt: time.Date(2026, time.July, 23, 12, 34, 0, 0, time.UTC),
		Lines: []HardwareLine{{
			EquipmentIdentifier: "990000000000299",
			PrimaryPort:         "cdc-wdm12",
		}},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	var devices, policies int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM devices WHERE imei = ?",
		"990000000000299",
	).Scan(&devices); err != nil {
		t.Fatalf("query unidentified device: %v", err)
	}
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM modemdeck_line_call_policies WHERE line_id = ''",
	).Scan(&policies); err != nil {
		t.Fatalf("query unidentified line policy: %v", err)
	}
	if devices != 0 || policies != 0 {
		t.Fatalf("unidentified line persisted devices=%d policies=%d", devices, policies)
	}
}

func TestHardwareSnapshotCommitsIncomingAndOutgoingCallsWithPolicyAndRecordingState(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 35, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-call-transaction", "990000000000206")

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-call-transaction",
		Revision:   "snapshot-call-transaction-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("seed ApplyHardwareSnapshot() error = %v", err)
	}
	policy, err := repository.LineCallPolicy(ctx, line.ID)
	if err != nil {
		t.Fatalf("LineCallPolicy() error = %v", err)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		line.ID,
		LineCallPolicyDND,
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	recordingSettings, err := repository.RecordingSettings(ctx)
	if err != nil {
		t.Fatalf("RecordingSettings() error = %v", err)
	}
	if _, err := repository.UpdateRecordingSettings(ctx, true, recordingSettings.Revision); err != nil {
		t.Fatalf("UpdateRecordingSettings() error = %v", err)
	}
	const outgoingRequestID = "request-outgoing-recording-off"
	if err := repository.PrepareCallRecordingRequest(ctx, outgoingRequestID, false); err != nil {
		t.Fatalf("PrepareCallRecordingRequest() error = %v", err)
	}

	incoming := HardwareCall{
		AppID:           "call-incoming-ringing-transaction",
		LineID:          line.ID,
		EndpointCallID:  "/org/freedesktop/ModemManager1/Call/31",
		Number:          "+15550000431",
		Direction:       "incoming",
		Phase:           "ringing",
		Bearer:          "volte",
		StateReason:     "incoming_new",
		StateReasonCode: 2,
		ObservedAt:      observed.Add(time.Second),
	}
	outgoing := HardwareCall{
		AppID:           "call-outgoing-active-transaction",
		RequestID:       outgoingRequestID,
		LineID:          line.ID,
		EndpointCallID:  "/org/freedesktop/ModemManager1/Call/32",
		Number:          "+15550000432",
		Direction:       "outgoing",
		Phase:           "active",
		Bearer:          "vowifi",
		StateReason:     "accepted",
		StateReasonCode: 3,
		Multiparty:      true,
		AudioPort:       "hw:fixture,0",
		AudioEncoding:   "pcm",
		AudioResolution: "s16le",
		AudioRate:       16000,
		MediaAvailable:  true,
		ObservedAt:      observed.Add(time.Second),
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-call-transaction",
		Revision:   "snapshot-call-transaction-2",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{incoming, outgoing},
	}); err != nil {
		t.Fatalf("call ApplyHardwareSnapshot() error = %v", err)
	}

	assertHardwareCallRow(t, repository, incoming, defaultHardwareCallFailureCode)
	assertHardwareCallRow(t, repository, outgoing, defaultHardwareCallFailureCode)

	incomingRecording, err := repository.CallRecordingState(ctx, incoming.AppID)
	if err != nil {
		t.Fatalf("incoming CallRecordingState() error = %v", err)
	}
	if incomingRecording.Preference != RecordingPreferenceDefault ||
		!incomingRecording.Enabled ||
		incomingRecording.Generation != 1 ||
		incomingRecording.Status != RecordingStatePending {
		t.Fatalf("incoming recording state = %+v", incomingRecording)
	}
	outgoingRecording, err := repository.CallRecordingState(ctx, outgoing.AppID)
	if err != nil {
		t.Fatalf("outgoing CallRecordingState() error = %v", err)
	}
	if outgoingRecording.Preference != RecordingPreferenceOverride ||
		outgoingRecording.Enabled ||
		outgoingRecording.Generation != 0 ||
		outgoingRecording.Status != RecordingStateOff {
		t.Fatalf("outgoing recording state = %+v", outgoingRecording)
	}
	var pendingRecordingRequests int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM modemdeck_call_recording_requests WHERE request_id = ?",
		outgoingRequestID,
	).Scan(&pendingRecordingRequests); err != nil {
		t.Fatalf("query consumed recording request: %v", err)
	}
	if pendingRecordingRequests != 0 {
		t.Fatalf("pending recording requests = %d, want 0", pendingRecordingRequests)
	}

	actions, err := repository.ClaimIncomingCallActions(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimIncomingCallActions() error = %v", err)
	}
	if len(actions) != 1 ||
		actions[0].CallID != incoming.AppID ||
		actions[0].EndpointCallID != incoming.EndpointCallID ||
		actions[0].EffectivePolicy != EffectiveCallPolicyDND {
		t.Fatalf("incoming DND actions = %+v", actions)
	}
}

func TestHardwareMessageRejectsStateRegression(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC)
	base := HardwareMessage{
		LineID:            "line-boot-1",
		EndpointMessageID: "boot-1:/sms/2",
		IMSI:              "440500000000001",
		ICCID:             "8901000000000000001",
		LocalPhone:        "+819012345678",
		Number:            "+818012345678",
		Text:              "outgoing",
		Direction:         "outgoing",
		State:             "sent",
		StateCode:         5,
		Revision:          5,
		Timestamp:         now,
		ObservedAt:        now,
	}
	if _, created, err := repository.UpsertHardwareMessage(ctx, base); err != nil || !created {
		t.Fatalf("initial UpsertHardwareMessage() created = %v, error = %v", created, err)
	}
	stale := base
	stale.State = "sending"
	stale.StateCode = 3
	stale.Revision = 4
	stale.ObservedAt = now.Add(time.Second)
	if _, created, err := repository.UpsertHardwareMessage(ctx, stale); err != nil || created {
		t.Fatalf("stale UpsertHardwareMessage() created = %v, error = %v", created, err)
	}
	messages, err := repository.Messages(ctx, MessageQuery{ICCID: base.ICCID, Peer: base.Number})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].State != "sent" ||
		messages[0].Status != base.StateCode || messages[0].Revision != base.Revision {
		t.Fatalf("messages after stale update = %+v", messages)
	}
}

func hardwareLifecycleTestLine(id, equipmentID string) HardwareLine {
	return HardwareLine{
		ID:                  id,
		Model:               "Lifecycle fixture modem",
		Firmware:            "lifecycle-fixture-1",
		EquipmentIdentifier: equipmentID,
		PrimaryPort:         "cdc-wdm0",
		State:               "registered",
		ICCID:               "8901000000000" + equipmentID[len(equipmentID)-6:],
		IMSI:                "440500000" + equipmentID[len(equipmentID)-6:],
	}
}

func hardwareLifecycleTestCall(id, lineID string, observedAt time.Time) HardwareCall {
	return HardwareCall{
		AppID:           id,
		LineID:          lineID,
		EndpointCallID:  "endpoint-" + id,
		Number:          "+15550000100",
		Direction:       "incoming",
		Phase:           "active",
		Bearer:          "volte",
		StateReason:     "accepted",
		StateReasonCode: 3,
		Multiparty:      true,
		AudioPort:       "hw:fixture,0",
		AudioEncoding:   "pcm",
		AudioResolution: "s16le",
		AudioRate:       16000,
		MediaAvailable:  true,
		ObservedAt:      observedAt,
	}
}

func assertMissingHardwareCallClosed(t *testing.T, repository *Store, expected HardwareCall) {
	t.Helper()
	calls, err := repository.Calls(context.Background(), CallQuery{Limit: 100})
	if err != nil {
		t.Fatalf("Calls() error = %v", err)
	}
	for _, call := range calls {
		if call.ID != expected.AppID {
			continue
		}
		if call.Phase != "ended" || call.EndedAt == "" ||
			call.EndReason != "not_present_in_snapshot" {
			t.Fatalf("closed call lifecycle = %+v", call)
		}
		if call.MediaAvailable || call.AudioPort != "" || call.AudioEncoding != "" ||
			call.AudioResolution != "" || call.AudioRate != 0 {
			t.Fatalf("closed call retained current media state: %+v", call)
		}
		if call.Bearer != expected.Bearer ||
			call.StateReason != expected.StateReason ||
			call.StateReasonCode != expected.StateReasonCode ||
			call.Multiparty != expected.Multiparty {
			t.Fatalf("closed call lost diagnostic history: %+v", call)
		}
		return
	}
	t.Fatalf("call %q not found in history", expected.AppID)
}

func assertHardwareCallRow(
	t *testing.T,
	repository *Store,
	expected HardwareCall,
	expectedFailureCode string,
) {
	t.Helper()
	var (
		id, requestID, lineID, direction, remoteNumber, endpointID, endpointCallID string
		phase, createdAt, updatedAt, endReason, failureCode, bearer, stateReason   string
		audioPort, audioEncoding, audioResolution                                  string
		revision, stateReasonCode, multiparty, audioRate, mediaAvailable           int64
		activeAt, endedAt                                                          sql.NullString
	)
	if err := repository.database.QueryRowContext(
		context.Background(),
		`SELECT id, request_id, device_id, direction, remote_number, endpoint_id,
			endpoint_call_id, phase, revision, created_at, updated_at, active_at,
			ended_at, end_reason, failure_code, bearer, state_reason,
			state_reason_code, multiparty, audio_port, audio_encoding,
			audio_resolution, audio_rate, media_available
		 FROM call_history WHERE id = ?`,
		expected.AppID,
	).Scan(
		&id,
		&requestID,
		&lineID,
		&direction,
		&remoteNumber,
		&endpointID,
		&endpointCallID,
		&phase,
		&revision,
		&createdAt,
		&updatedAt,
		&activeAt,
		&endedAt,
		&endReason,
		&failureCode,
		&bearer,
		&stateReason,
		&stateReasonCode,
		&multiparty,
		&audioPort,
		&audioEncoding,
		&audioResolution,
		&audioRate,
		&mediaAvailable,
	); err != nil {
		t.Fatalf("query call_history row %q: %v", expected.AppID, err)
	}
	if id != expected.AppID ||
		requestID != expected.RequestID ||
		lineID != expected.LineID ||
		direction != expected.Direction ||
		remoteNumber != expected.Number ||
		endpointID != modemManagerEndpointID ||
		endpointCallID != expected.EndpointCallID ||
		phase != expected.Phase ||
		failureCode != expectedFailureCode ||
		bearer != expected.Bearer ||
		stateReason != expected.StateReason ||
		stateReasonCode != int64(expected.StateReasonCode) ||
		(multiparty != 0) != expected.Multiparty ||
		audioPort != expected.AudioPort ||
		audioEncoding != expected.AudioEncoding ||
		audioResolution != expected.AudioResolution ||
		audioRate != int64(expected.AudioRate) ||
		(mediaAvailable != 0) != expected.MediaAvailable {
		t.Fatalf("call_history row mismatch for %q", expected.AppID)
	}
	if revision <= 0 || createdAt == "" || updatedAt == "" {
		t.Fatalf(
			"call_history lifecycle metadata for %q: revision=%d created_at=%q updated_at=%q",
			expected.AppID,
			revision,
			createdAt,
			updatedAt,
		)
	}
	if activeAt.Valid != (expected.Phase == "active") ||
		(activeAt.Valid && activeAt.String == "") ||
		endedAt.Valid != (expected.Phase == "ended" || expected.Phase == "failed") ||
		(endedAt.Valid && endedAt.String == "") {
		t.Fatalf(
			"call_history lifecycle timestamps for %q: active_at=%+v ended_at=%+v",
			expected.AppID,
			activeAt,
			endedAt,
		)
	}
	if expected.Phase != "ended" && expected.Phase != "failed" && endReason != "" {
		t.Fatalf("call_history end_reason for live call %q = %q", expected.AppID, endReason)
	}
}

func newHardwareTestStore(t *testing.T) *Store {
	t.Helper()
	directory := t.TempDir()
	database, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(directory, "modemdeck.db"),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return repository
}
