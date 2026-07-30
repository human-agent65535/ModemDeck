package store

import (
	"context"
	"testing"
	"time"
)

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
