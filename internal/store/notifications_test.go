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
			LineScopes:         []string{"line-1"},
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

	observed := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	message := HardwareMessage{
		LineID:            "line-1",
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
		deliveries[0].LineID != message.LineID {
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

func TestNotificationOutboxMarksInterruptedDeliveryIndeterminate(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
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
	now := time.Date(2026, time.July, 23, 12, 30, 0, 0, time.UTC)
	if _, _, err := repository.UpsertHardwareMessage(ctx, HardwareMessage{
		LineID:            "line-1",
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
