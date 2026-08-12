package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

func TestApplePushOutboxAllocatesScopedDeliveryAndPersistsRetry(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 12, 8, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-apple-outbox",
		EquipmentIdentifier: "990000000000399",
		ICCID:               "8901000000000000399",
		IMSI:                "440500000000399",
	}
	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "apple-outbox",
		Revision:   "apple-outbox-line",
		ObservedAt: now,
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatal(err)
	}
	lineID := result.LineIDsByEndpoint[line.ID]
	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:          "apple-outbox-member",
		PasswordHash:      "fixture-hash",
		IOSPairingEnabled: true,
		LineIDs:           []string{lineID},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := mobilepairing.TokenDigest{3, 9, 9}
	if _, err := repository.CreateIOSPairingCredential(ctx, member.ID, digest); err != nil {
		t.Fatal(err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		digest,
		mobilepairing.DeviceInfo{Name: "Example iPhone"},
	); err != nil || !confirmed {
		t.Fatalf("ConfirmIOSPairingCredential() = %t, %v", confirmed, err)
	}
	if err := repository.UpdateIOSPushRegistration(ctx, digest, mobilepairing.PushRegistration{
		APNSToken:   strings.Repeat("ab", 32),
		VoIPToken:   strings.Repeat("cd", 32),
		Environment: "production",
		BundleID:    "com.example.modemdeck",
	}); err != nil {
		t.Fatal(err)
	}

	message := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "endpoint-message-399",
		Number:            "+12025550106",
		Text:              "Example",
		Direction:         "incoming",
		State:             "received",
		Timestamp:         now,
		ObservedAt:        now,
	}
	stored, created, err := repository.UpsertHardwareMessage(ctx, message)
	if err != nil || !created {
		t.Fatalf("UpsertHardwareMessage() = %+v, %t, %v", stored, created, err)
	}
	if _, replayCreated, err := repository.UpsertHardwareMessage(ctx, message); err != nil || replayCreated {
		t.Fatalf("replay created = %t, error = %v", replayCreated, err)
	}

	deliveries, err := repository.PendingApplePushDeliveries(ctx, now.Add(time.Second), 10)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("PendingApplePushDeliveries() = %+v, %v", deliveries, err)
	}
	delivery := deliveries[0]
	if delivery.EventKey != "sms:"+delivery.ResourceID ||
		delivery.ResourceID != fmt.Sprint(stored.ID) ||
		delivery.TokenKind != IOSPushTokenAPNS || !delivery.Eligible || !delivery.ResourceLive {
		t.Fatalf("delivery = %+v", delivery)
	}
	claimed, err := repository.ClaimApplePushDelivery(
		ctx,
		delivery.EventKey,
		delivery.CredentialID,
		delivery.TokenKind,
		"attempt-1",
	)
	if err != nil || !claimed {
		t.Fatalf("ClaimApplePushDelivery() = %t, %v", claimed, err)
	}
	nextAttempt := now.Add(time.Minute)
	if err := repository.FinishApplePushDelivery(
		ctx,
		delivery.EventKey,
		delivery.CredentialID,
		delivery.TokenKind,
		"attempt-1",
		NotificationPending,
		"apns_shutdown",
		nextAttempt,
	); err != nil {
		t.Fatal(err)
	}
	if pending, err := repository.PendingApplePushDeliveries(
		ctx,
		now.Add(30*time.Second),
		10,
	); err != nil || len(pending) != 0 {
		t.Fatalf("early pending = %+v, %v", pending, err)
	}
	deliveries, err = repository.PendingApplePushDeliveries(
		ctx,
		nextAttempt.Add(time.Second),
		10,
	)
	if err != nil || len(deliveries) != 1 || deliveries[0].AttemptCount != 1 {
		t.Fatalf("due retry = %+v, %v", deliveries, err)
	}
}

func TestIncomingCallPushOutboxFollowsPolicyAndPersistedCallState(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	line := policyTestLine()
	base, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "call-push",
		Revision:   "call-push-line",
		ObservedAt: now,
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatal(err)
	}
	lineID := base.LineIDsByEndpoint[line.ID]
	member, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:          "call-push-member",
		PasswordHash:      "fixture-hash",
		IOSPairingEnabled: true,
		LineIDs:           []string{lineID},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := mobilepairing.TokenDigest{4, 0, 1}
	if _, err := repository.CreateIOSPairingCredential(ctx, member.ID, digest); err != nil {
		t.Fatal(err)
	}
	if confirmed, err := repository.ConfirmIOSPairingCredential(
		ctx,
		digest,
		mobilepairing.DeviceInfo{Name: "Example iPad"},
	); err != nil || !confirmed {
		t.Fatalf("ConfirmIOSPairingCredential() = %t, %v", confirmed, err)
	}
	if err := repository.UpdateIOSPushRegistration(ctx, digest, mobilepairing.PushRegistration{
		APNSToken:   strings.Repeat("ef", 32),
		VoIPToken:   strings.Repeat("01", 32),
		Environment: "production",
		BundleID:    "com.example.modemdeck",
	}); err != nil {
		t.Fatal(err)
	}

	call := HardwareCall{
		AppID:          "call-push-1",
		LineID:         lineID,
		EndpointLineID: line.ID,
		EndpointCallID: "endpoint-call-push-1",
		Number:         "+12025550107",
		Direction:      "incoming",
		Phase:          "ringing",
		ObservedAt:     now.Add(time.Second),
	}
	snapshot := HardwareSnapshot{
		BootEpoch:  "call-push",
		Revision:   "call-push-ringing",
		ObservedAt: call.ObservedAt,
		Lines:      []HardwareLine{line},
		Calls:      []HardwareCall{call},
	}
	if _, err := repository.ApplyHardwareSnapshotWithResult(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ApplyHardwareSnapshotWithResult(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	deliveries, err := repository.PendingApplePushDeliveries(ctx, now.Add(2*time.Second), 10)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("ringing deliveries = %+v, %v", deliveries, err)
	}
	if deliveries[0].EventType != NotificationIncomingCall ||
		deliveries[0].TokenKind != IOSPushTokenVoIP ||
		deliveries[0].ResourceID != call.AppID ||
		!deliveries[0].Eligible || !deliveries[0].ResourceLive {
		t.Fatalf("ringing delivery = %+v", deliveries[0])
	}

	call.Phase = "ended"
	call.ObservedAt = now.Add(3 * time.Second)
	snapshot.Revision = "call-push-ended"
	snapshot.ObservedAt = call.ObservedAt
	snapshot.Calls = []HardwareCall{call}
	if _, err := repository.ApplyHardwareSnapshotWithResult(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	deliveries, err = repository.PendingApplePushDeliveries(ctx, now.Add(4*time.Second), 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].ResourceLive {
		t.Fatalf("terminal delivery = %+v, %v", deliveries, err)
	}

	policy, err := repository.LineCallPolicy(ctx, lineID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		lineID,
		LineCallPolicyDND,
		policy.Revision,
	); err != nil {
		t.Fatal(err)
	}
	call.AppID = "call-push-dnd"
	call.EndpointCallID = "endpoint-call-push-dnd"
	call.Phase = "ringing"
	call.ObservedAt = now.Add(5 * time.Second)
	snapshot.Revision = "call-push-dnd"
	snapshot.ObservedAt = call.ObservedAt
	snapshot.Calls = []HardwareCall{call}
	if _, err := repository.ApplyHardwareSnapshotWithResult(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repository.database.QueryRow(
		`SELECT COUNT(*) FROM modemdeck_apple_push_deliveries`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Apple call deliveries = %d, want only the receive-policy call", count)
	}
}

func TestNotificationOutboxAllocatesScopedDeliveryOnce(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-notification-1",
		EquipmentIdentifier: "990000000000301",
		ICCID:               "8901000000000000301",
		IMSI:                "440500000000301",
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "notification-1",
		Revision:   "notification-line-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	stableLineID := stableLineIDForICCID(t, repository, line.ICCID)
	for _, unit := range []TelegramUnitRecord{
		{
			ID:                 "matching",
			DisplayName:        "Matching",
			Enabled:            true,
			BotID:              100001,
			BotTokenNonce:      []byte("nonce"),
			BotTokenCiphertext: []byte("ciphertext"),
			ChatID:             1,
			AdminID:            2,
			LineScopes:         []string{stableLineID},
			IncomingSMS:        true,
			MissedCalls:        true,
		},
		{
			ID:                 "other-line",
			DisplayName:        "Other line",
			Enabled:            true,
			BotID:              100002,
			BotTokenNonce:      []byte("nonce"),
			BotTokenCiphertext: []byte("ciphertext"),
			ChatID:             3,
			AdminID:            4,
			LineScopes:         []string{"line-2"},
			IncomingSMS:        true,
			MissedCalls:        true,
		},
		{
			ID:                 "disabled",
			DisplayName:        "Disabled",
			Enabled:            false,
			BotID:              100003,
			BotTokenNonce:      []byte("nonce"),
			BotTokenCiphertext: []byte("ciphertext"),
			ChatID:             5,
			AdminID:            6,
			IncomingSMS:        true,
			MissedCalls:        true,
		},
	} {
		if _, err := repository.CreateTelegramUnit(ctx, unit); err != nil {
			t.Fatalf("CreateTelegramUnit(%s) error = %v", unit.ID, err)
		}
	}

	message := HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "message-1",
		Number:            "+818012345678",
		Text:              "fixture",
		Direction:         "incoming",
		State:             "received",
		Timestamp:         observed,
		ObservedAt:        observed,
	}
	if _, created, err := repository.UpsertHardwareMessage(ctx, message); err != nil || !created {
		t.Fatalf("UpsertHardwareMessage() created = %v, error = %v", created, err)
	}
	if _, created, err := repository.UpsertHardwareMessage(ctx, message); err != nil || created {
		t.Fatalf("replay UpsertHardwareMessage() created = %v, error = %v", created, err)
	}

	deliveries, err := repository.PendingTelegramNotificationDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("PendingTelegramNotificationDeliveries() error = %v", err)
	}
	if len(deliveries) != 1 ||
		deliveries[0].UnitID != "matching" ||
		deliveries[0].EventType != NotificationIncomingSMS ||
		deliveries[0].LineID != stableLineID {
		t.Fatalf("deliveries = %+v", deliveries)
	}

	claimed, err := repository.ClaimTelegramNotificationDelivery(
		ctx,
		deliveries[0].EventKey,
		deliveries[0].UnitID,
		"attempt-1",
	)
	if err != nil || !claimed {
		t.Fatalf("ClaimTelegramNotificationDelivery() claimed = %v, error = %v", claimed, err)
	}
	if err := repository.FinishTelegramNotificationDelivery(
		ctx,
		deliveries[0].EventKey,
		deliveries[0].UnitID,
		"attempt-1",
		NotificationSent,
		"",
	); err != nil {
		t.Fatalf("FinishTelegramNotificationDelivery() error = %v", err)
	}
	deliveries, err = repository.PendingTelegramNotificationDeliveries(ctx, 10)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("pending after finish = %+v, error = %v", deliveries, err)
	}
}

func TestNotificationOutboxFollowsAssignedUserAccess(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-notification-user",
		EquipmentIdentifier: "990000000000303",
		ICCID:               "8901000000000000303",
		IMSI:                "440500000000303",
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "notification-user",
		Revision:   "notification-user-line",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	stableLineID := stableLineIDForICCID(t, repository, line.ICCID)
	assigned, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "assigned",
		PasswordHash: "assigned-hash",
		LineIDs:      []string{stableLineID},
	})
	if err != nil {
		t.Fatalf("CreateMember(assigned) error = %v", err)
	}
	unassigned, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "unassigned",
		PasswordHash: "unassigned-hash",
	})
	if err != nil {
		t.Fatalf("CreateMember(unassigned) error = %v", err)
	}
	for _, unit := range []TelegramUnitRecord{
		{
			ID:                 "assigned-user",
			DisplayName:        "Assigned user",
			Enabled:            true,
			BotID:              100004,
			BotTokenNonce:      []byte("nonce"),
			BotTokenCiphertext: []byte("ciphertext"),
			ChatID:             7,
			AdminID:            8,
			ScopeSource:        "user",
			AssignedUserID:     assigned.ID,
			IncomingSMS:        true,
		},
		{
			ID:                 "unassigned-user",
			DisplayName:        "Unassigned user",
			Enabled:            true,
			BotID:              100005,
			BotTokenNonce:      []byte("nonce"),
			BotTokenCiphertext: []byte("ciphertext"),
			ChatID:             9,
			AdminID:            10,
			ScopeSource:        "user",
			AssignedUserID:     unassigned.ID,
			IncomingSMS:        true,
		},
		{
			ID:                 "selected-other-line",
			DisplayName:        "Selected other line",
			Enabled:            true,
			BotID:              100006,
			BotTokenNonce:      []byte("nonce"),
			BotTokenCiphertext: []byte("ciphertext"),
			ChatID:             11,
			AdminID:            12,
			ScopeSource:        "user",
			AssignedUserID:     assigned.ID,
			LineScopes:         []string{"line-not-assigned"},
			IncomingSMS:        true,
		},
	} {
		if _, err := repository.CreateTelegramUnit(ctx, unit); err != nil {
			t.Fatalf("CreateTelegramUnit(%s) error = %v", unit.ID, err)
		}
	}

	if _, _, err := repository.UpsertHardwareMessage(ctx, HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "message-user-1",
		Number:            "+818012345678",
		Text:              "assigned",
		Direction:         "incoming",
		State:             "received",
		Timestamp:         observed,
		ObservedAt:        observed,
	}); err != nil {
		t.Fatalf("UpsertHardwareMessage(assigned) error = %v", err)
	}
	deliveries, err := repository.PendingTelegramNotificationDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("PendingTelegramNotificationDeliveries() error = %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].UnitID != "assigned-user" {
		t.Fatalf("user-scoped deliveries = %+v, want assigned-user only", deliveries)
	}
	if claimed, err := repository.ClaimTelegramNotificationDelivery(
		ctx,
		deliveries[0].EventKey,
		deliveries[0].UnitID,
		"assigned-attempt",
	); err != nil || !claimed {
		t.Fatalf("claim assigned delivery = %v, error = %v", claimed, err)
	}
	if err := repository.FinishTelegramNotificationDelivery(
		ctx,
		deliveries[0].EventKey,
		deliveries[0].UnitID,
		"assigned-attempt",
		NotificationSent,
		"",
	); err != nil {
		t.Fatalf("finish assigned delivery: %v", err)
	}

	assigned, err = repository.UpdateMember(ctx, assigned.ID, UpdateMemberInput{
		Username: assigned.Username,
		Enabled:  false,
		LineIDs:  assigned.LineIDs,
		Revision: assigned.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateMember(disable assigned) error = %v", err)
	}
	if _, _, err := repository.UpsertHardwareMessage(ctx, HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "message-user-2",
		Number:            "+818012345678",
		Text:              "disabled",
		Direction:         "incoming",
		State:             "received",
		Timestamp:         observed.Add(time.Minute),
		ObservedAt:        observed.Add(time.Minute),
	}); err != nil {
		t.Fatalf("UpsertHardwareMessage(disabled) error = %v", err)
	}
	deliveries, err = repository.PendingTelegramNotificationDeliveries(ctx, 10)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("disabled user deliveries = %+v, error = %v", deliveries, err)
	}
}

func TestNotificationOutboxMarksInterruptedDeliveryIndeterminate(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 23, 12, 30, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-notification-2",
		EquipmentIdentifier: "990000000000302",
		ICCID:               "8901000000000000302",
		IMSI:                "440500000000302",
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "notification-2",
		Revision:   "notification-line-2",
		ObservedAt: now,
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	if _, err := repository.CreateTelegramUnit(ctx, TelegramUnitRecord{
		ID:                 "unit-1",
		DisplayName:        "Unit",
		Enabled:            true,
		BotID:              100001,
		BotTokenNonce:      []byte("nonce"),
		BotTokenCiphertext: []byte("ciphertext"),
		ChatID:             1,
		AdminID:            2,
		IncomingSMS:        true,
	}); err != nil {
		t.Fatalf("CreateTelegramUnit() error = %v", err)
	}
	if _, _, err := repository.UpsertHardwareMessage(ctx, HardwareMessage{
		LineID:            line.ID,
		EndpointMessageID: "message-1",
		Number:            "+818012345678",
		Text:              "fixture",
		Direction:         "incoming",
		State:             "received",
		Timestamp:         now,
		ObservedAt:        now,
	}); err != nil {
		t.Fatalf("UpsertHardwareMessage() error = %v", err)
	}
	deliveries, err := repository.PendingTelegramNotificationDeliveries(ctx, 10)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("pending = %+v, error = %v", deliveries, err)
	}
	claimed, err := repository.ClaimTelegramNotificationDelivery(
		ctx,
		deliveries[0].EventKey,
		deliveries[0].UnitID,
		"attempt-1",
	)
	if err != nil || !claimed {
		t.Fatalf("claim = %v, error = %v", claimed, err)
	}
	if err := repository.MarkSendingTelegramNotificationsIndeterminate(ctx); err != nil {
		t.Fatalf("MarkSendingTelegramNotificationsIndeterminate() error = %v", err)
	}
	var status, errorClass string
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT status, last_error_class
		 FROM modemdeck_notification_deliveries
		 WHERE event_key = ? AND unit_id = ?`,
		deliveries[0].EventKey,
		deliveries[0].UnitID,
	).Scan(&status, &errorClass); err != nil {
		t.Fatalf("query delivery status: %v", err)
	}
	if status != NotificationIndeterminate || errorClass != "process_interrupted" {
		t.Fatalf("status = %q, error class = %q", status, errorClass)
	}
}
