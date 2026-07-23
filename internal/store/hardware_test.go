package store

import (
	"context"
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
		Revision:   1,
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
		Revision:   2,
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

func newHardwareTestStore(t *testing.T) *Store {
	t.Helper()
	directory := t.TempDir()
	database, _, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(directory, "modemdeck.db"),
		LegacyPath: filepath.Join(directory, "vohive.db"),
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
