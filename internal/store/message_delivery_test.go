package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMessageDeliveryPolicyDefaultsAndClearsUnsupportedOnReenable(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()
	line := policyTestLine()
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-message-policy",
		Revision:   "snapshot-message-policy",
		ObservedAt: time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC),
		Lines:      []HardwareLine{line},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
	lineID := stableLineIDForICCID(t, repository, line.ICCID)

	policy, err := repository.MessageDeliveryPolicy(ctx, lineID)
	if err != nil {
		t.Fatalf("MessageDeliveryPolicy() error = %v", err)
	}
	if policy.DeliveryReportsEnabled ||
		policy.DeliveryReportsSupport != MessageDeliveryReportSupportUnknown ||
		policy.Revision != 1 {
		t.Fatalf("default policy = %+v", policy)
	}
	policy, err = repository.UpdateMessageDeliveryPolicy(ctx, lineID, true, policy.Revision)
	if err != nil {
		t.Fatalf("enable policy error = %v", err)
	}
	if !policy.DeliveryReportsEnabled ||
		policy.DeliveryReportsSupport != MessageDeliveryReportSupportUnknown ||
		policy.Revision != 2 {
		t.Fatalf("enabled policy = %+v", policy)
	}
	if _, err := repository.MarkMessageDeliveryReportsUnsupported(
		ctx,
		lineID,
		policy.Revision-1,
	); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale unsupported update error = %v", err)
	}
	policy, err = repository.MarkMessageDeliveryReportsUnsupported(
		ctx,
		lineID,
		policy.Revision,
	)
	if err != nil {
		t.Fatalf("mark unsupported error = %v", err)
	}
	if policy.DeliveryReportsEnabled ||
		policy.DeliveryReportsSupport != MessageDeliveryReportSupportUnsupported ||
		policy.Revision != 3 {
		t.Fatalf("unsupported policy = %+v", policy)
	}
	policy, err = repository.UpdateMessageDeliveryPolicy(ctx, lineID, true, policy.Revision)
	if err != nil {
		t.Fatalf("reenable policy error = %v", err)
	}
	if !policy.DeliveryReportsEnabled ||
		policy.DeliveryReportsSupport != MessageDeliveryReportSupportUnknown ||
		policy.Revision != 4 {
		t.Fatalf("re-enabled policy = %+v", policy)
	}
}

func TestDeliveryReportsProjectOnlyExactSuccessAndTerminalFailure(t *testing.T) {
	t.Parallel()
	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	line := policyTestLine()
	message := HardwareMessage{
		RequestID:               "request-delivery-1",
		LineID:                  line.ID,
		EndpointLineID:          line.ID,
		EndpointMessageID:       "message-submit-1",
		IMSI:                    line.IMSI,
		ICCID:                   line.ICCID,
		LocalPhone:              line.PhoneNumber,
		HomeCountryISO:          line.HomeCountryISO,
		Number:                  "+818012345678",
		Text:                    "hello",
		Direction:               "outgoing",
		State:                   "sent",
		StateCode:               5,
		DeliveryStatus:          MessageDeliverySubmitted,
		MessageReference:        0,
		MessageReferenceKnown:   true,
		DeliveryReportRequested: true,
		DeliveryReportTrackable: true,
		Timestamp:               observed,
		ObservedAt:              observed,
	}
	if err := repository.ApplyHardwareSnapshot(ctx, HardwareSnapshot{
		BootEpoch:  "boot-delivery",
		Revision:   "snapshot-delivery-1",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{message},
	}); err != nil {
		t.Fatalf("initial snapshot error = %v", err)
	}
	lineID := stableLineIDForICCID(t, repository, line.ICCID)

	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-delivery",
		Revision:   "snapshot-delivery-2",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{message},
		DeliveryReports: []HardwareMessageDeliveryReport{{
			EndpointReportID:      "report-success",
			LineID:                line.ID,
			EndpointLineID:        line.ID,
			HomeCountryISO:        line.HomeCountryISO,
			Number:                message.Number,
			MessageReference:      0,
			MessageReferenceKnown: true,
			DeliveryState:         0x00,
			DeliveryStateKnown:    true,
			Timestamp:             observed.Add(time.Minute),
			ObservedAt:            observed.Add(time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("success report snapshot error = %v", err)
	}
	if len(result.HandledDeliveryReportIDs) != 1 ||
		result.HandledDeliveryReportIDs[0] != "report-success" {
		t.Fatalf("handled reports = %#v", result.HandledDeliveryReportIDs)
	}
	messages, err := repository.Messages(ctx, MessageQuery{
		LineID:        lineID,
		Peer:          message.Number,
		Chronological: true,
	})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 1 ||
		messages[0].DeliveryStatus != MessageDeliveryDelivered ||
		messages[0].DeliveryReportCode == nil ||
		*messages[0].DeliveryReportCode != 0 {
		t.Fatalf("delivered message = %+v", messages)
	}

	failing := message
	failing.RequestID = "request-delivery-2"
	failing.EndpointMessageID = "message-submit-2"
	failing.MessageReference = 1
	failing.Timestamp = observed.Add(2 * time.Minute)
	failing.ObservedAt = failing.Timestamp
	failing.DeliveryReportTrackable = false
	result, err = repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-delivery",
		Revision:   "snapshot-delivery-3",
		ObservedAt: observed.Add(3 * time.Minute),
		Lines:      []HardwareLine{line},
		Messages:   []HardwareMessage{failing},
		DeliveryReports: []HardwareMessageDeliveryReport{{
			EndpointReportID:      "report-failure",
			LineID:                line.ID,
			EndpointLineID:        line.ID,
			HomeCountryISO:        line.HomeCountryISO,
			Number:                failing.Number,
			MessageReference:      1,
			MessageReferenceKnown: true,
			DeliveryState:         0x40,
			DeliveryStateKnown:    true,
			Timestamp:             observed.Add(3 * time.Minute),
			ObservedAt:            observed.Add(3 * time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("failure report snapshot error = %v", err)
	}
	messages, err = repository.Messages(ctx, MessageQuery{
		LineID:        lineID,
		Peer:          message.Number,
		Chronological: true,
	})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 2 ||
		messages[1].DeliveryStatus != MessageDeliveryFailed {
		t.Fatalf("messages after terminal report = %+v", messages)
	}
}
