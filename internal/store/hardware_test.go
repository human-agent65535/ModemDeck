package store

import (
	"context"
	"database/sql"
	"errors"
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
	signalDBM := int64(-68)
	signalRSRQ := int64(-11)
	signalRSRP := int64(-94)
	line := HardwareLine{
		ID:                  "line-boot-1",
		Model:               "Fixture modem",
		Firmware:            "fixture-1",
		EquipmentIdentifier: "990000000000001",
		PrimaryPort:         "cdc-wdm0",
		State:               "registered",
		SignalKnown:         true,
		SignalQuality:       74,
		SignalDBM:           &signalDBM,
		SignalRSRQ:          &signalRSRQ,
		SignalRSRP:          &signalRSRP,
		PhoneNumber:         "+819012345678",
		ICCID:               "8901000000000000001",
		IMSI:                "440500000000001",
		Operator:            "Fixture Telecom",
	}
	call := HardwareCall{
		AppID:          "call-fixture-1",
		RequestID:      "request-call-1",
		LineID:         line.ID,
		LocalPhone:     line.PhoneNumber,
		LineIMSI:       line.IMSI,
		LineICCID:      line.ICCID,
		EndpointCallID: "boot-1:/call/1",
		Number:         "00818012345678",
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
	contact, err := repository.CreateContact(ctx, ContactInput{
		DisplayName: "Prefix Contact",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 80 1234 5678", Primary: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateContact() error = %v", err)
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
		target.Number != "+818012345678" || target.Direction != call.Direction ||
		target.Bearer != call.Bearer || target.Revision < call.Revision {
		t.Fatalf("call target = %+v", target)
	}
	var reportedNumber string
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT reported_remote_number FROM call_history WHERE id = ?",
		call.AppID,
	).Scan(&reportedNumber); err != nil {
		t.Fatalf("reported call number query error = %v", err)
	}
	if reportedNumber != call.Number {
		t.Fatalf("reported call number = %q, want %q", reportedNumber, call.Number)
	}
	devices, err := repository.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices() error = %v", err)
	}
	if len(devices) != 1 || devices[0].SignalQuality == nil || *devices[0].SignalQuality != 74 {
		t.Fatalf("devices = %+v, want persisted signal quality", devices)
	}
	if devices[0].SignalDBM == nil || *devices[0].SignalDBM != signalDBM ||
		devices[0].SignalRSRQ == nil || *devices[0].SignalRSRQ != signalRSRQ ||
		devices[0].SignalRSRP == nil || *devices[0].SignalRSRP != signalRSRP {
		t.Fatalf("devices = %+v, want persisted extended signal", devices)
	}

	if err := repository.MarkMessageThreadReadByLine(ctx, line.ID, message.Number); err != nil {
		t.Fatalf("MarkMessageThreadReadByLine() error = %v", err)
	}
	threads, err = repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil || len(threads) != 1 || threads[0].UnreadCount != 0 {
		t.Fatalf("threads after read = %+v, error = %v", threads, err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		"UPDATE sms_contacts SET unread_count = 1 WHERE iccid = ? AND peer = ?",
		line.ICCID,
		message.Number,
	); err != nil {
		t.Fatalf("restore unread fixture: %v", err)
	}
	if err := repository.MarkMessageThreadRead(ctx, MessageThreadIdentity{
		ICCID: line.ICCID,
		Peer:  message.Number,
	}); err != nil {
		t.Fatalf("MarkMessageThreadRead() error = %v", err)
	}

	terminalCall := call
	terminalCall.Phase = "ended"
	terminalCall.StateReason = "terminated"
	terminalCall.ObservedAt = observed.Add(time.Minute)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-1",
		Revision:   "snapshot-2",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{terminalCall},
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
	if len(calls) != 1 || calls[0].Phase != "ended" || calls[0].EndedAt == "" ||
		calls[0].RemoteNumber != "+818012345678" ||
		calls[0].ContactID != contact.ID ||
		calls[0].ContactName != contact.DisplayName ||
		calls[0].LocalPhone != line.PhoneNumber ||
		calls[0].LineIMSI != line.IMSI ||
		calls[0].LineICCID != line.ICCID {
		t.Fatalf("calls = %+v, want closed call", calls)
	}
}

func TestHardwareSnapshotResultReturnsOnlyNewCommittedIncomingMessages(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 24, 7, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "line-result",
		EquipmentIdentifier: "990000000000099",
		ICCID:               "8901000000000000099",
		IMSI:                "440500000000099",
	}
	incoming := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "/sms/incoming",
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		Number:            "+818012345678",
		Text:              "committed",
		Direction:         "incoming",
		State:             "received",
		Timestamp:         observed,
		ObservedAt:        observed,
	}
	outgoing := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "/sms/outgoing",
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		Number:            "+818098765432",
		Text:              "sent",
		Direction:         "outgoing",
		State:             "sent",
		Timestamp:         observed,
		ObservedAt:        observed,
	}
	snapshot := HardwareSnapshot{
		BootEpoch:  "boot-result",
		Revision:   "snapshot-result",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{incoming, outgoing},
	}

	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, snapshot)
	if err != nil {
		t.Fatalf("ApplyHardwareSnapshotWithResult() error = %v", err)
	}
	if len(result.CreatedIncomingMessages) != 1 {
		t.Fatalf("created incoming messages = %+v, want one", result.CreatedIncomingMessages)
	}
	created := result.CreatedIncomingMessages[0]
	if created.ID == 0 || created.EndpointMessageID != incoming.EndpointMessageID ||
		created.Direction != "incoming" || created.State != "received" {
		t.Fatalf("created incoming message = %+v", created)
	}

	replayed, err := repository.ApplyHardwareSnapshotWithResult(ctx, snapshot)
	if err != nil {
		t.Fatalf("replayed ApplyHardwareSnapshotWithResult() error = %v", err)
	}
	if len(replayed.CreatedIncomingMessages) != 0 {
		t.Fatalf("replayed created messages = %+v, want none", replayed.CreatedIncomingMessages)
	}
}

func TestHardwareSnapshotDoesNotDuplicateMessageAcrossProviderRestart(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 24, 6, 30, 20, 0, time.UTC)
	line := HardwareLine{
		ID:                  "line-stable",
		EquipmentIdentifier: "990000000000099",
		ICCID:               "8986012345678900099",
		IMSI:                "460011234567899",
	}
	message := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "message_stable_transport_identity",
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		Number:            "106900000000000",
		Text:              "stored while the app was offline",
		Direction:         "incoming",
		State:             "received",
		StateCode:         3,
		Timestamp:         observed.Add(-3 * time.Second),
		ObservedAt:        observed,
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  ":1.41",
		Revision:   "snapshot-before-restart",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{message},
	}); err != nil {
		t.Fatalf("first ApplyHardwareSnapshot() error = %v", err)
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  ":1.42",
		Revision:   "snapshot-after-restart",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{message},
	}); err != nil {
		t.Fatalf("restarted ApplyHardwareSnapshot() error = %v", err)
	}

	messages, err := repository.Messages(ctx, MessageQuery{
		ICCID: line.ICCID,
		Peer:  message.Number,
	})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one persisted SMS", messages)
	}
	threads, err := repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() error = %v", err)
	}
	if len(threads) != 1 || threads[0].UnreadCount != 1 {
		t.Fatalf("threads = %+v, want one unread SMS", threads)
	}
	var notifications int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM modemdeck_notification_events WHERE event_type = ?",
		NotificationIncomingSMS,
	).Scan(&notifications); err != nil {
		t.Fatalf("count incoming SMS notifications: %v", err)
	}
	if notifications != 1 {
		t.Fatalf("incoming SMS notifications = %d, want 1", notifications)
	}
}

func TestMarkMessageThreadReadByLineOnlyUsesCurrentSIM(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 10, 30, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-reused", "990000000000200")
	line.ICCID = "8901000000000000200"
	line.IMSI = "440500000000200"
	peer := "+818012345678"
	oldMessage := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "old-sim:/sms/1",
		IMSI:              line.IMSI,
		ICCID:             line.ICCID,
		Number:            peer,
		Text:              "old SIM",
		Direction:         "incoming",
		State:             "received",
		StateCode:         3,
		Revision:          1,
		Timestamp:         observed,
		ObservedAt:        observed,
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "old-sim",
		Revision:   "old-sim-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{oldMessage},
	}); err != nil {
		t.Fatalf("apply old SIM snapshot: %v", err)
	}

	line.ICCID = "8901000000000000201"
	line.IMSI = "440500000000201"
	newMessage := oldMessage
	newMessage.EndpointMessageID = "new-sim:/sms/1"
	newMessage.ICCID = line.ICCID
	newMessage.IMSI = line.IMSI
	newMessage.Text = "new SIM"
	newMessage.Timestamp = observed.Add(time.Minute)
	newMessage.ObservedAt = newMessage.Timestamp
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "new-sim",
		Revision:   "new-sim-1",
		ObservedAt: newMessage.ObservedAt,
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{newMessage},
	}); err != nil {
		t.Fatalf("apply new SIM snapshot: %v", err)
	}

	if err := repository.MarkMessageThreadReadByLine(ctx, line.ID, peer); err != nil {
		t.Fatalf("MarkMessageThreadReadByLine() error = %v", err)
	}
	var oldUnread, newUnread int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT unread_count FROM sms_contacts WHERE imsi = ? AND peer = ?",
		oldMessage.IMSI,
		peer,
	).Scan(&oldUnread); err != nil {
		t.Fatalf("read old SIM unread count: %v", err)
	}
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT unread_count FROM sms_contacts WHERE imsi = ? AND peer = ?",
		newMessage.IMSI,
		peer,
	).Scan(&newUnread); err != nil {
		t.Fatalf("read new SIM unread count: %v", err)
	}
	if oldUnread != 1 || newUnread != 0 {
		t.Fatalf("unread counts old=%d new=%d, want old=1 new=0", oldUnread, newUnread)
	}
}

func TestMarkMessageThreadReadUsesExactSIMIdentity(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	peer := "+818012345678"
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO sms_contacts (
			imsi, iccid, peer, unread_count, created_at, updated_at
		 ) VALUES
			('imsi-a', 'iccid-a', ?, 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			('imsi-b', 'iccid-b', ?, 3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		peer,
		peer,
	); err != nil {
		t.Fatalf("insert message threads: %v", err)
	}

	if err := repository.MarkMessageThreadRead(ctx, MessageThreadIdentity{
		ICCID: "iccid-a",
		Peer:  peer,
	}); err != nil {
		t.Fatalf("MarkMessageThreadRead() error = %v", err)
	}
	var firstUnread, secondUnread int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT unread_count FROM sms_contacts WHERE imsi = 'imsi-a' AND peer = ?",
		peer,
	).Scan(&firstUnread); err != nil {
		t.Fatalf("read first SIM unread count: %v", err)
	}
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT unread_count FROM sms_contacts WHERE imsi = 'imsi-b' AND peer = ?",
		peer,
	).Scan(&secondUnread); err != nil {
		t.Fatalf("read second SIM unread count: %v", err)
	}
	if firstUnread != 0 || secondUnread != 3 {
		t.Fatalf("unread counts first=%d second=%d, want first=0 second=3", firstUnread, secondUnread)
	}

	if err := repository.MarkMessageThreadRead(ctx, MessageThreadIdentity{
		ICCID: "missing-iccid",
		Peer:  peer,
	}); !errors.Is(err, ErrMessageThreadNotFound) {
		t.Fatalf("missing thread error = %v, want ErrMessageThreadNotFound", err)
	}
}

func TestMessageHistoryUsesLocalPhoneAcrossSIMIdentities(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	peer := "+818055550132"
	firstObserved := time.Date(2026, time.July, 23, 9, 0, 0, 0, time.UTC)
	fixtures := []struct {
		line    HardwareLine
		message HardwareMessage
	}{
		{
			line: HardwareLine{
				ID:          "line-before",
				PhoneNumber: "+81 90-1234-5678",
				ICCID:       "iccid-before",
				IMSI:        "imsi-before",
			},
			message: HardwareMessage{
				LineID:            "line-before",
				EndpointMessageID: "message-before",
				IMSI:              "imsi-before",
				ICCID:             "iccid-before",
				LocalPhone:        "+81 90-1234-5678",
				Number:            peer,
				Text:              "before",
				Direction:         "incoming",
				State:             "received",
				StateCode:         3,
				Revision:          1,
				Timestamp:         firstObserved,
				ObservedAt:        firstObserved,
			},
		},
		{
			line: HardwareLine{
				ID:          "line-after",
				PhoneNumber: "+819012345678",
				ICCID:       "iccid-after",
				IMSI:        "imsi-after",
			},
			message: HardwareMessage{
				LineID:            "line-after",
				EndpointMessageID: "message-after",
				IMSI:              "imsi-after",
				ICCID:             "iccid-after",
				LocalPhone:        "+819012345678",
				Number:            peer,
				Text:              "after",
				Direction:         "incoming",
				State:             "received",
				StateCode:         3,
				Revision:          1,
				Timestamp:         firstObserved.Add(time.Minute),
				ObservedAt:        firstObserved.Add(time.Minute),
			},
		},
	}
	for index, fixture := range fixtures {
		if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
			BootEpoch:  fixture.line.ID,
			Revision:   fixture.line.ID,
			ObservedAt: fixture.message.ObservedAt,
			Lines:      []HardwareLine{fixture.line},
			Messages:   []HardwareMessage{fixture.message},
		}); err != nil {
			t.Fatalf("apply fixture %d: %v", index, err)
		}
	}

	threads, err := repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() error = %v", err)
	}
	if len(threads) != 1 ||
		threads[0].Key != "phone:819012345678|"+peer ||
		threads[0].UnreadCount != 2 ||
		threads[0].LastContent != "after" {
		t.Fatalf("threads = %+v, want one phone-owned logical thread", threads)
	}

	messages, err := repository.Messages(ctx, MessageQuery{
		LocalPhone: "+81 (90) 1234-5678",
		Peer:       peer,
	})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 2 ||
		messages[0].Content != "after" ||
		messages[1].Content != "before" {
		t.Fatalf("messages = %+v, want both SIM histories in timestamp order", messages)
	}

	if err := repository.MarkMessageThreadRead(ctx, MessageThreadIdentity{
		LocalPhone: "+81 90 1234 5678",
		Peer:       peer,
	}); err != nil {
		t.Fatalf("MarkMessageThreadRead() error = %v", err)
	}
	threads, err = repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil || len(threads) != 1 || threads[0].UnreadCount != 0 {
		t.Fatalf("threads after read = %+v, error = %v", threads, err)
	}
}

func TestMarkMessageThreadReadRejectsAmbiguousSIMIdentity(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	peer := "+818012345678"
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO sms_contacts (
			imsi, iccid, peer, unread_count, created_at, updated_at
		 ) VALUES
			('imsi-a', 'shared-iccid', ?, 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			('imsi-b', 'shared-iccid', ?, 3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		peer,
		peer,
	); err != nil {
		t.Fatalf("insert ambiguous message threads: %v", err)
	}

	err := repository.MarkMessageThreadRead(ctx, MessageThreadIdentity{
		ICCID: "shared-iccid",
		Peer:  peer,
	})
	if !errors.Is(err, ErrMessageThreadIdentityInvalid) {
		t.Fatalf("MarkMessageThreadRead() error = %v, want ErrMessageThreadIdentityInvalid", err)
	}
	var unreadTotal int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT SUM(unread_count) FROM sms_contacts WHERE iccid = 'shared-iccid' AND peer = ?",
		peer,
	).Scan(&unreadTotal); err != nil {
		t.Fatalf("read ambiguous unread total: %v", err)
	}
	if unreadTotal != 5 {
		t.Fatalf("ambiguous unread total = %d, want 5", unreadTotal)
	}
}

func TestHardwareSnapshotRetainsCallAcrossTransientOmission(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-single", "990000000000201")
	call := hardwareLifecycleTestCall("call-transient", line.ID, observed)

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-single",
		Revision:   "snapshot-single-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-single",
		Revision:   "snapshot-single-2",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{},
	}); err != nil {
		t.Fatalf("authoritative ApplyHardwareSnapshot() error = %v", err)
	}

	active, err := repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != call.AppID {
		t.Fatalf("active calls after one omission = %+v, want retained call", active)
	}

	call.ObservedAt = observed.Add(2 * time.Second)
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-single",
		Revision:   "snapshot-single-3",
		ObservedAt: observed.Add(2 * time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("recovery ApplyHardwareSnapshot() error = %v", err)
	}

	active, err = repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() after recovery error = %v", err)
	}
	if len(active) != 1 || active[0].ID != call.AppID {
		t.Fatalf("active calls after recovery = %+v, want restored call", active)
	}
	for _, call := range active {
		if call.Phase != "active" || call.EndedAt != "" {
			t.Fatalf("recovered active call = %+v, want open active call", call)
		}
	}
}

func TestHardwareSnapshotClosesPersistentlyMissingCallAfterConfirmationWindow(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 5, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-persistent", "990000000000206")
	call := hardwareLifecycleTestCall("call-persistent", line.ID, observed)

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-persistent",
		Revision:   "snapshot-persistent-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}

	for index, omission := range []struct {
		revision string
		elapsed  time.Duration
	}{
		{revision: "snapshot-persistent-missing", elapsed: time.Second},
		{revision: "snapshot-persistent-missing", elapsed: 8 * time.Second},
		{revision: "snapshot-persistent-missing", elapsed: missingCallConfirmationWindow},
	} {
		if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
			BootEpoch:  "boot-persistent",
			Revision:   omission.revision,
			ObservedAt: observed.Add(omission.elapsed),
			Lines:      []HardwareLine{line},
			Calls:      []HardwareCall{},
		}); err != nil {
			t.Fatalf("missing ApplyHardwareSnapshot() %d error = %v", index+1, err)
		}

		active, err := repository.ActiveCalls(ctx)
		if err != nil {
			t.Fatalf("ActiveCalls() after omission %d error = %v", index+1, err)
		}
		if index < 2 && (len(active) != 1 || active[0].ID != call.AppID) {
			t.Fatalf("active calls after omission %d = %+v, want retained call", index+1, active)
		}
		if index == 2 && len(active) != 0 {
			t.Fatalf("active calls after confirmed omission = %+v, want none", active)
		}
	}

	assertMissingHardwareCallClosed(t, repository, call)
}

func TestHardwareSnapshotNewCallSupersedesMissingCallOnSameLine(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 7, 0, 0, time.UTC)
	line := hardwareLifecycleTestLine("line-superseded", "990000000000207")
	previous := hardwareLifecycleTestCall("call-previous", line.ID, observed)
	current := hardwareLifecycleTestCall(
		"call-current",
		line.ID,
		observed.Add(time.Second),
	)

	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-superseded",
		Revision:   "snapshot-superseded-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{previous},
	}); err != nil {
		t.Fatalf("initial ApplyHardwareSnapshot() error = %v", err)
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-superseded",
		Revision:   "snapshot-superseded-2",
		ObservedAt: observed.Add(time.Second),
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{current},
	}); err != nil {
		t.Fatalf("replacement ApplyHardwareSnapshot() error = %v", err)
	}

	active, err := repository.ActiveCalls(ctx)
	if err != nil {
		t.Fatalf("ActiveCalls() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != current.AppID {
		t.Fatalf("active calls = %+v, want only replacement %q", active, current.AppID)
	}
	assertMissingHardwareCallClosed(t, repository, previous)
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
