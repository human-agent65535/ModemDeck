package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestStableLineMergesProvisionalSIMWhenPhoneArrives(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC)
	const (
		phone = "+819012345678"
		peer  = "+818012345678"
	)
	historical := HardwareLine{
		ID:                  "endpoint-historical",
		EquipmentIdentifier: "imei-historical",
		PhoneNumber:         phone,
		ICCID:               "iccid-historical",
		IMSI:                "imsi-historical",
	}
	firstResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-historical",
		Revision:   "snapshot-historical",
		ObservedAt: observed,
		Lines:      []HardwareLine{historical},
		Messages: []HardwareMessage{{
			LineID:            historical.ID,
			EndpointMessageID: "message-historical",
			IMSI:              historical.IMSI,
			ICCID:             historical.ICCID,
			LocalPhone:        historical.PhoneNumber,
			Number:            peer,
			Text:              "historical",
			Direction:         "incoming",
			State:             "received",
			StateCode:         3,
			Timestamp:         observed,
			ObservedAt:        observed,
		}},
	})
	if err != nil {
		t.Fatalf("apply historical snapshot: %v", err)
	}
	stableLineID := firstResult.LineIDsByEndpoint[historical.ID]
	if stableLineID == "" {
		t.Fatal("historical stable line ID is empty")
	}
	color := LineColorBlue
	if _, err := repository.UpdateLineLabel(ctx, stableLineID, "Main", &color); err != nil {
		t.Fatalf("UpdateLineLabel() error = %v", err)
	}
	policy, err := repository.LineCallPolicy(ctx, stableLineID)
	if err != nil {
		t.Fatalf("LineCallPolicy() error = %v", err)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		stableLineID,
		LineCallPolicyDND,
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_telegram_units (id) VALUES ('unit-stable-line');
		 INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 VALUES ('unit-stable-line', ?)`,
		stableLineID,
	); err != nil {
		t.Fatalf("seed Telegram line scope: %v", err)
	}

	provisional := HardwareLine{
		ID:                  "endpoint-provisional",
		EquipmentIdentifier: "imei-provisional",
		ICCID:               "iccid-provisional",
		IMSI:                "imsi-provisional",
	}
	provisionalResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-provisional",
		Revision:   "snapshot-provisional",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{provisional},
		Messages: []HardwareMessage{{
			LineID:            provisional.ID,
			EndpointMessageID: "message-provisional",
			IMSI:              provisional.IMSI,
			ICCID:             provisional.ICCID,
			Number:            peer,
			Text:              "provisional",
			Direction:         "incoming",
			State:             "received",
			StateCode:         3,
			Timestamp:         observed.Add(time.Minute),
			ObservedAt:        observed.Add(time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("apply provisional snapshot: %v", err)
	}
	provisionalLineID := provisionalResult.LineIDsByEndpoint[provisional.ID]
	if provisionalLineID == "" || provisionalLineID == stableLineID {
		t.Fatalf(
			"provisional stable line ID = %q, historical = %q",
			provisionalLineID,
			stableLineID,
		)
	}

	provisional.PhoneNumber = phone
	mergedResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-provisional",
		Revision:   "snapshot-phone-arrived",
		ObservedAt: observed.Add(2 * time.Minute),
		Lines:      []HardwareLine{provisional},
	})
	if err != nil {
		t.Fatalf("apply phone identity snapshot: %v", err)
	}
	if got := mergedResult.LineIDsByEndpoint[provisional.ID]; got != stableLineID {
		t.Fatalf("merged stable line ID = %q, want %q", got, stableLineID)
	}

	var aliasCount int
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM modemdeck_lines WHERE line_id = ?",
		provisionalLineID,
	).Scan(&aliasCount); err != nil {
		t.Fatalf("count provisional stable line: %v", err)
	}
	if aliasCount != 0 {
		t.Fatalf("provisional stable line rows = %d, want 0", aliasCount)
	}
	var label, storedColor string
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT line_label, line_color FROM modemdeck_lines WHERE line_id = ?`,
		stableLineID,
	).Scan(&label, &storedColor); err != nil {
		t.Fatalf("read merged line metadata: %v", err)
	}
	if label != "Main" || storedColor != string(color) {
		t.Fatalf("merged line metadata = (%q, %q)", label, storedColor)
	}
	mergedPolicy, err := repository.LineCallPolicy(ctx, stableLineID)
	if err != nil {
		t.Fatalf("merged LineCallPolicy() error = %v", err)
	}
	if mergedPolicy.Policy != LineCallPolicyDND {
		t.Fatalf("merged line call policy = %q, want %q", mergedPolicy.Policy, LineCallPolicyDND)
	}
	var canonicalScopes, provisionalScopes int
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM modemdeck_telegram_line_scopes WHERE line_id = ?),
			(SELECT COUNT(*) FROM modemdeck_telegram_line_scopes WHERE line_id = ?)`,
		stableLineID,
		provisionalLineID,
	).Scan(&canonicalScopes, &provisionalScopes); err != nil {
		t.Fatalf("read merged Telegram scopes: %v", err)
	}
	if canonicalScopes != 1 || provisionalScopes != 0 {
		t.Fatalf(
			"merged Telegram scopes = canonical %d, provisional %d",
			canonicalScopes,
			provisionalScopes,
		)
	}
	messages, err := repository.Messages(ctx, MessageQuery{LineID: stableLineID, Peer: peer})
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("merged line messages = %+v, want both histories", messages)
	}
	assertResolvedLineEndpoint(t, repository, stableLineID, provisional.ID)
	assertSIMAttachment(t, repository, historical.ICCID, "")
	assertSIMAttachment(t, repository, provisional.ICCID, provisional.EquipmentIdentifier)
	assertDeviceSIMAttachment(t, repository, historical.EquipmentIdentifier, "", false)
	assertDeviceSIMAttachment(
		t,
		repository,
		provisional.EquipmentIdentifier,
		provisional.ICCID,
		true,
	)
	assertActiveLineAttachmentCount(t, repository, stableLineID, 1)
}

func TestStableLineMergesEquivalentInternationalNumberRepresentations(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 28, 8, 30, 0, 0, time.UTC)
	const (
		historicalLineID = "line_historical_plus"
		currentLineID    = "line_current_bare"
		phoneWithPrefix  = "+8613800000001"
		phoneWithoutPlus = "8618636812882"
		iccid            = "8986012345678900001"
		imsi             = "460010000000001"
		peer             = "+818000000001"
	)
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_lines (
			line_id, phone_number, line_label, line_color, created_at, updated_at
		 ) VALUES
			(?, ?, '', '', ?, ?),
			(?, ?, 'Bac', 'teal', ?, ?)`,
		historicalLineID,
		phoneWithPrefix,
		databaseTime(observed.Add(-time.Hour)),
		databaseTime(observed.Add(-time.Hour)),
		currentLineID,
		phoneWithoutPlus,
		databaseTime(observed),
		databaseTime(observed),
	); err != nil {
		t.Fatalf("seed equivalent phone lines: %v", err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO sim_cards (
			iccid, line_id, imsi, current_imei, last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, 'imei-current', ?, ?, ?)`,
		iccid,
		currentLineID,
		imsi,
		databaseTime(observed),
		databaseTime(observed),
		databaseTime(observed),
	); err != nil {
		t.Fatalf("seed current SIM card: %v", err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO sim_subscriptions (
			imsi, line_id, current_iccid, phone_number, modem_phone_number,
			last_seen, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		imsi,
		currentLineID,
		iccid,
		phoneWithoutPlus,
		phoneWithoutPlus,
		databaseTime(observed),
		databaseTime(observed),
		databaseTime(observed),
	); err != nil {
		t.Fatalf("seed current SIM subscription: %v", err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO sms (
			line_id, iccid, peer, content, type, timestamp, created_at
		 ) VALUES (?, 'iccid-historical', ?, 'historical message', 1, ?, ?)`,
		historicalLineID,
		peer,
		databaseTime(observed.Add(-time.Hour)),
		databaseTime(observed.Add(-time.Hour)),
	); err != nil {
		t.Fatalf("seed historical message: %v", err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO call_history (
			id, line_id, direction, remote_number, phase, created_at, ended_at
		 ) VALUES ('call-historical-plus', ?, 'outgoing', ?, 'ended', ?, ?)`,
		historicalLineID,
		peer,
		databaseTime(observed.Add(-time.Hour)),
		databaseTime(observed.Add(-time.Hour)),
	); err != nil {
		t.Fatalf("seed historical call: %v", err)
	}

	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-equivalent-phone",
		Revision:   "snapshot-equivalent-phone",
		ObservedAt: observed.Add(time.Minute),
		Lines: []HardwareLine{{
			ID:                  "endpoint-current",
			EquipmentIdentifier: "imei-current",
			PhoneNumber:         phoneWithoutPlus,
			ICCID:               iccid,
			IMSI:                imsi,
		}},
	})
	if err != nil {
		t.Fatalf("apply equivalent phone snapshot: %v", err)
	}
	if got := result.LineIDsByEndpoint["endpoint-current"]; got != currentLineID {
		t.Fatalf("resolved stable line = %q, want %q", got, currentLineID)
	}

	var (
		lineCount, historicalMessages, historicalCalls int
		phone, label, color                            string
	)
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM modemdeck_lines
				WHERE line_id IN (?, ?)),
			(SELECT phone_number FROM modemdeck_lines WHERE line_id = ?),
			(SELECT line_label FROM modemdeck_lines WHERE line_id = ?),
			(SELECT line_color FROM modemdeck_lines WHERE line_id = ?),
			(SELECT COUNT(*) FROM sms
				WHERE line_id = ? AND content = 'historical message'),
			(SELECT COUNT(*) FROM call_history
				WHERE line_id = ? AND id = 'call-historical-plus')`,
		historicalLineID,
		currentLineID,
		currentLineID,
		currentLineID,
		currentLineID,
		currentLineID,
		currentLineID,
	).Scan(
		&lineCount,
		&phone,
		&label,
		&color,
		&historicalMessages,
		&historicalCalls,
	); err != nil {
		t.Fatalf("read merged equivalent phone line: %v", err)
	}
	if lineCount != 1 ||
		phone != phoneWithoutPlus ||
		label != "Bac" ||
		color != "teal" ||
		historicalMessages != 1 ||
		historicalCalls != 1 {
		t.Fatalf(
			"merged line = count %d, phone %q, label %q, color %q, messages %d, calls %d",
			lineCount,
			phone,
			label,
			color,
			historicalMessages,
			historicalCalls,
		)
	}
	assertResolvedLineEndpoint(t, repository, currentLineID, "endpoint-current")
}

func TestStableLinePhoneIdentityTreatsInternationalPrefixesAsOne(t *testing.T) {
	t.Parallel()

	const expected = "+8613800000001"
	for _, number := range []string{
		"+8613800000001",
		"8618636812882",
		"008618636812882",
	} {
		if got := stableLinePhoneIdentity(number); got != expected {
			t.Fatalf("identity for %q = %q, want %q", number, got, expected)
		}
	}
}

func TestLegacyEndpointProvisionalLineMergesOnFirstIdentitySnapshot(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 9, 15, 0, 0, time.UTC)
	const endpointID = "line_legacy_endpoint"
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_lines (
			line_id, line_label, line_color, created_at, updated_at
		 ) VALUES (?, 'Migrated', 'amber', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
		 INSERT INTO modemdeck_telegram_units (id) VALUES ('unit-legacy-endpoint');
		 INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 VALUES ('unit-legacy-endpoint', ?);
		 INSERT INTO modemdeck_line_call_policies (line_id, policy, revision)
		 VALUES (?, 'do_not_disturb', 4);
		 UPDATE modemdeck_line_settings
		 SET default_line_id = ?, revision = 2
		 WHERE singleton = 1`,
		endpointID,
		endpointID,
		endpointID,
		endpointID,
	); err != nil {
		t.Fatalf("seed migrated endpoint-only line: %v", err)
	}

	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-legacy-endpoint",
		Revision:   "snapshot-first-identity",
		ObservedAt: observed,
		Lines: []HardwareLine{{
			ID:                  endpointID,
			EquipmentIdentifier: "imei-legacy-endpoint",
			PhoneNumber:         "+819066666666",
			ICCID:               "iccid-legacy-endpoint",
			IMSI:                "imsi-legacy-endpoint",
		}},
	})
	if err != nil {
		t.Fatalf("apply first identity snapshot: %v", err)
	}
	stableLineID := result.LineIDsByEndpoint[endpointID]
	if stableLineID == "" || stableLineID == endpointID {
		t.Fatalf("resolved stable line ID = %q, provisional = %q", stableLineID, endpointID)
	}
	var provisionalCount int
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM modemdeck_lines WHERE line_id = ?`,
		endpointID,
	).Scan(&provisionalCount); err != nil {
		t.Fatalf("count legacy provisional line: %v", err)
	}
	if provisionalCount != 0 {
		t.Fatalf("legacy provisional line rows = %d, want 0", provisionalCount)
	}
	var phone, label, color string
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT phone_number, line_label, line_color
		 FROM modemdeck_lines WHERE line_id = ?`,
		stableLineID,
	).Scan(&phone, &label, &color); err != nil {
		t.Fatalf("read claimed stable line: %v", err)
	}
	if phone != "+819066666666" || label != "Migrated" || color != "amber" {
		t.Fatalf("claimed stable line metadata = (%q, %q, %q)", phone, label, color)
	}
	var scopeLineID, defaultLineID, policy string
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT
			(SELECT line_id FROM modemdeck_telegram_line_scopes
				WHERE unit_id = 'unit-legacy-endpoint'),
			(SELECT default_line_id FROM modemdeck_line_settings WHERE singleton = 1),
			(SELECT policy FROM modemdeck_line_call_policies WHERE line_id = ?)`,
		stableLineID,
	).Scan(&scopeLineID, &defaultLineID, &policy); err != nil {
		t.Fatalf("read claimed stable line references: %v", err)
	}
	if scopeLineID != stableLineID || defaultLineID != stableLineID ||
		policy != string(LineCallPolicyDND) {
		t.Fatalf(
			"claimed stable refs = scope %q, default %q, policy %q",
			scopeLineID,
			defaultLineID,
			policy,
		)
	}
	assertResolvedLineEndpoint(t, repository, stableLineID, endpointID)
}

func TestStableLineMovesCurrentSIMWhenReportedPhoneChanges(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 9, 30, 0, 0, time.UTC)
	const peer = "+818012345678"
	line := HardwareLine{
		ID:                  "endpoint-phone-change",
		EquipmentIdentifier: "imei-phone-change",
		PhoneNumber:         "+819011111111",
		ICCID:               "iccid-phone-change",
		IMSI:                "imsi-phone-change",
	}
	firstResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-phone-change",
		Revision:   "snapshot-old-phone",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
		Messages: []HardwareMessage{{
			LineID:            line.ID,
			EndpointMessageID: "message-old-phone",
			IMSI:              line.IMSI,
			ICCID:             line.ICCID,
			LocalPhone:        line.PhoneNumber,
			Number:            peer,
			Text:              "old phone history",
			Direction:         "incoming",
			State:             "received",
			StateCode:         3,
			Timestamp:         observed,
			ObservedAt:        observed,
		}},
	})
	if err != nil {
		t.Fatalf("apply old phone snapshot: %v", err)
	}
	oldLineID := firstResult.LineIDsByEndpoint[line.ID]
	color := LineColorIndigo
	if _, err := repository.UpdateLineLabel(ctx, oldLineID, "Old phone", &color); err != nil {
		t.Fatalf("UpdateLineLabel() error = %v", err)
	}
	policy, err := repository.LineCallPolicy(ctx, oldLineID)
	if err != nil {
		t.Fatalf("LineCallPolicy() error = %v", err)
	}
	if _, err := repository.UpdateLineCallPolicy(
		ctx,
		oldLineID,
		LineCallPolicyDND,
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateLineCallPolicy() error = %v", err)
	}
	if _, err := repository.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_telegram_units (id) VALUES ('unit-phone-change');
		 INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 VALUES ('unit-phone-change', ?)`,
		oldLineID,
	); err != nil {
		t.Fatalf("seed old phone Telegram scope: %v", err)
	}

	line.PhoneNumber = "+819022222222"
	secondResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-phone-change",
		Revision:   "snapshot-new-phone",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{line},
		Messages: []HardwareMessage{{
			LineID:            line.ID,
			EndpointMessageID: "message-new-phone",
			IMSI:              line.IMSI,
			ICCID:             line.ICCID,
			LocalPhone:        line.PhoneNumber,
			Number:            peer,
			Text:              "new phone history",
			Direction:         "incoming",
			State:             "received",
			StateCode:         3,
			Timestamp:         observed.Add(time.Minute),
			ObservedAt:        observed.Add(time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("apply new phone snapshot: %v", err)
	}
	newLineID := secondResult.LineIDsByEndpoint[line.ID]
	if newLineID == "" || newLineID == oldLineID {
		t.Fatalf("new phone stable line ID = %q, old = %q", newLineID, oldLineID)
	}

	var (
		oldPhone, oldLabel, oldColor  string
		newPhone, newLabel, newColor  string
		simLineID, subscriptionLineID string
	)
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT phone_number, line_label, line_color
		 FROM modemdeck_lines WHERE line_id = ?`,
		oldLineID,
	).Scan(&oldPhone, &oldLabel, &oldColor); err != nil {
		t.Fatalf("read old phone line: %v", err)
	}
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT phone_number, line_label, line_color
		 FROM modemdeck_lines WHERE line_id = ?`,
		newLineID,
	).Scan(&newPhone, &newLabel, &newColor); err != nil {
		t.Fatalf("read new phone line: %v", err)
	}
	if oldPhone != "+819011111111" || oldLabel != "Old phone" || oldColor != string(color) {
		t.Fatalf("old phone line changed to (%q, %q, %q)", oldPhone, oldLabel, oldColor)
	}
	if newPhone != "+819022222222" || newLabel != "" || newColor != "" {
		t.Fatalf("new phone line metadata = (%q, %q, %q)", newPhone, newLabel, newColor)
	}
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT
			(SELECT line_id FROM sim_cards WHERE iccid = ?),
			(SELECT line_id FROM sim_subscriptions WHERE imsi = ?)`,
		line.ICCID,
		line.IMSI,
	).Scan(&simLineID, &subscriptionLineID); err != nil {
		t.Fatalf("read reassigned SIM identities: %v", err)
	}
	if simLineID != newLineID || subscriptionLineID != newLineID {
		t.Fatalf(
			"reassigned SIM lines = card %q, subscription %q; want %q",
			simLineID,
			subscriptionLineID,
			newLineID,
		)
	}
	oldMessages, err := repository.Messages(ctx, MessageQuery{LineID: oldLineID, Peer: peer})
	if err != nil {
		t.Fatalf("old line Messages() error = %v", err)
	}
	newMessages, err := repository.Messages(ctx, MessageQuery{LineID: newLineID, Peer: peer})
	if err != nil {
		t.Fatalf("new line Messages() error = %v", err)
	}
	if len(oldMessages) != 1 || oldMessages[0].Content != "old phone history" {
		t.Fatalf("old line messages = %+v", oldMessages)
	}
	if len(newMessages) != 1 || newMessages[0].Content != "new phone history" {
		t.Fatalf("new line messages = %+v", newMessages)
	}
	var oldScopes, newScopes int
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM modemdeck_telegram_line_scopes WHERE line_id = ?),
			(SELECT COUNT(*) FROM modemdeck_telegram_line_scopes WHERE line_id = ?)`,
		oldLineID,
		newLineID,
	).Scan(&oldScopes, &newScopes); err != nil {
		t.Fatalf("read phone-change Telegram scopes: %v", err)
	}
	if oldScopes != 1 || newScopes != 0 {
		t.Fatalf("phone-change Telegram scopes = old %d, new %d", oldScopes, newScopes)
	}
	oldPolicy, err := repository.LineCallPolicy(ctx, oldLineID)
	if err != nil {
		t.Fatalf("old LineCallPolicy() error = %v", err)
	}
	newPolicy, err := repository.LineCallPolicy(ctx, newLineID)
	if err != nil {
		t.Fatalf("new LineCallPolicy() error = %v", err)
	}
	if oldPolicy.Policy != LineCallPolicyDND ||
		newPolicy.Policy != LineCallPolicyFollowGlobal {
		t.Fatalf(
			"phone-change policies = old %q, new %q",
			oldPolicy.Policy,
			newPolicy.Policy,
		)
	}
	if _, err := repository.ResolveLineEndpoint(ctx, oldLineID); !errors.Is(err, ErrLineNotFound) {
		t.Fatalf("ResolveLineEndpoint(old phone) error = %v, want ErrLineNotFound", err)
	}
	assertResolvedLineEndpoint(t, repository, newLineID, line.ID)
	assertActiveLineAttachmentCount(t, repository, oldLineID, 0)
	assertActiveLineAttachmentCount(t, repository, newLineID, 1)
}

func TestProvisionalStableLineKeepsIDWhenFirstPhoneArrives(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 9, 45, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-first-phone",
		EquipmentIdentifier: "imei-first-phone",
		ICCID:               "iccid-first-phone",
		IMSI:                "imsi-first-phone",
	}
	firstResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-first-phone",
		Revision:   "snapshot-without-phone",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatalf("apply provisional snapshot: %v", err)
	}
	provisionalLineID := firstResult.LineIDsByEndpoint[line.ID]
	if provisionalLineID == "" {
		t.Fatal("provisional stable line ID is empty")
	}
	if _, err := repository.UpdateLineLabel(ctx, provisionalLineID, "First phone", nil); err != nil {
		t.Fatalf("UpdateLineLabel() error = %v", err)
	}

	line.PhoneNumber = "+819033333333"
	secondResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-first-phone",
		Revision:   "snapshot-with-first-phone",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatalf("apply first phone snapshot: %v", err)
	}
	if got := secondResult.LineIDsByEndpoint[line.ID]; got != provisionalLineID {
		t.Fatalf("first phone stable line ID = %q, want %q", got, provisionalLineID)
	}
	var phone, label string
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT phone_number, line_label FROM modemdeck_lines WHERE line_id = ?`,
		provisionalLineID,
	).Scan(&phone, &label); err != nil {
		t.Fatalf("read first phone line: %v", err)
	}
	if phone != "+819033333333" || label != "First phone" {
		t.Fatalf("first phone line = phone %q, label %q", phone, label)
	}
	assertResolvedLineEndpoint(t, repository, provisionalLineID, line.ID)
}

func TestHardwareLineReplacementDetachesPreviousSIMFromSameModem(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	original := HardwareLine{
		ID:                  "endpoint-same-modem",
		EquipmentIdentifier: "imei-same-modem",
		PhoneNumber:         "+819011111111",
		ICCID:               "iccid-original",
		IMSI:                "imsi-original",
	}
	originalResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-same-modem",
		Revision:   "snapshot-original-sim",
		ObservedAt: observed,
		Lines:      []HardwareLine{original},
	})
	if err != nil {
		t.Fatalf("apply original SIM snapshot: %v", err)
	}
	originalLineID := originalResult.LineIDsByEndpoint[original.ID]

	replacement := original
	replacement.PhoneNumber = "+819022222222"
	replacement.ICCID = "iccid-replacement"
	replacement.IMSI = "imsi-replacement"
	replacementResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-same-modem",
		Revision:   "snapshot-replacement-sim",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{replacement},
	})
	if err != nil {
		t.Fatalf("apply replacement SIM snapshot: %v", err)
	}
	replacementLineID := replacementResult.LineIDsByEndpoint[replacement.ID]
	if replacementLineID == "" || replacementLineID == originalLineID {
		t.Fatalf(
			"replacement stable line ID = %q, original = %q",
			replacementLineID,
			originalLineID,
		)
	}
	if _, err := repository.ResolveLineEndpoint(ctx, originalLineID); !errors.Is(err, ErrLineNotFound) {
		t.Fatalf("ResolveLineEndpoint(original) error = %v, want ErrLineNotFound", err)
	}
	assertResolvedLineEndpoint(t, repository, replacementLineID, replacement.ID)
	assertSIMAttachment(t, repository, original.ICCID, "")
	assertSIMAttachment(t, repository, replacement.ICCID, replacement.EquipmentIdentifier)
	assertDeviceSIMAttachment(
		t,
		repository,
		replacement.EquipmentIdentifier,
		replacement.ICCID,
		true,
	)
}

func TestHardwareSIMMoveDetachesPreviousModem(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 11, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-old-modem",
		EquipmentIdentifier: "imei-old-modem",
		PhoneNumber:         "+819033333333",
		ICCID:               "iccid-moving",
		IMSI:                "imsi-moving",
	}
	firstResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-old-modem",
		Revision:   "snapshot-old-modem",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatalf("apply old modem snapshot: %v", err)
	}
	stableLineID := firstResult.LineIDsByEndpoint[line.ID]

	oldEndpointID := line.ID
	oldIMEI := line.EquipmentIdentifier
	line.ID = "endpoint-new-modem"
	line.EquipmentIdentifier = "imei-new-modem"
	secondResult, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-new-modem",
		Revision:   "snapshot-new-modem",
		ObservedAt: observed.Add(time.Minute),
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatalf("apply new modem snapshot: %v", err)
	}
	if got := secondResult.LineIDsByEndpoint[line.ID]; got != stableLineID {
		t.Fatalf("moved SIM stable line ID = %q, want %q", got, stableLineID)
	}
	assertResolvedLineEndpoint(t, repository, stableLineID, line.ID)
	assertSIMAttachment(t, repository, line.ICCID, line.EquipmentIdentifier)
	assertDeviceSIMAttachment(t, repository, oldIMEI, "", false)
	assertDeviceSIMAttachment(t, repository, line.EquipmentIdentifier, line.ICCID, true)
	assertActiveLineAttachmentCount(t, repository, stableLineID, 1)

	var retainedEndpoint string
	if err := repository.database.QueryRowContext(
		ctx,
		"SELECT endpoint_id FROM devices WHERE imei = ?",
		oldIMEI,
	).Scan(&retainedEndpoint); err != nil {
		t.Fatalf("read detached modem endpoint: %v", err)
	}
	if retainedEndpoint != oldEndpointID {
		t.Fatalf("detached modem endpoint = %q, want %q", retainedEndpoint, oldEndpointID)
	}
	devices, err := repository.Devices(ctx)
	if err != nil {
		t.Fatalf("list devices after SIM move: %v", err)
	}
	presentByIMEI := make(map[string]bool, len(devices))
	for _, device := range devices {
		presentByIMEI[device.IMEI] = device.Present
	}
	if presentByIMEI[oldIMEI] {
		t.Fatalf("old modem %q is still marked present after replacement", oldIMEI)
	}
	if !presentByIMEI[line.EquipmentIdentifier] {
		t.Fatalf("replacement modem %q is not marked present", line.EquipmentIdentifier)
	}
}

func TestHardwareSnapshotWithoutSIMDetachesPreviousAttachment(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	line := HardwareLine{
		ID:                  "endpoint-no-sim",
		EquipmentIdentifier: "imei-no-sim",
		PhoneNumber:         "+819044444444",
		ICCID:               "iccid-removed",
		IMSI:                "imsi-removed",
	}
	result, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-no-sim",
		Revision:   "snapshot-with-sim",
		ObservedAt: observed,
		Lines:      []HardwareLine{line},
	})
	if err != nil {
		t.Fatalf("apply attached SIM snapshot: %v", err)
	}
	stableLineID := result.LineIDsByEndpoint[line.ID]

	if _, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-no-sim",
		Revision:   "snapshot-without-sim",
		ObservedAt: observed.Add(time.Minute),
		Lines: []HardwareLine{{
			ID:                  line.ID,
			EquipmentIdentifier: line.EquipmentIdentifier,
		}},
	}); err != nil {
		t.Fatalf("apply no-SIM snapshot: %v", err)
	}
	if _, err := repository.ResolveLineEndpoint(ctx, stableLineID); !errors.Is(err, ErrLineNotFound) {
		t.Fatalf("ResolveLineEndpoint(detached) error = %v, want ErrLineNotFound", err)
	}
	assertSIMAttachment(t, repository, line.ICCID, "")
	assertDeviceSIMAttachment(t, repository, line.EquipmentIdentifier, "", false)
	assertActiveLineAttachmentCount(t, repository, stableLineID, 0)
}

func TestHardwareSnapshotRejectsStableLineOnMultipleEndpoints(t *testing.T) {
	t.Parallel()

	repository := newHardwareTestStore(t)
	ctx := context.Background()
	observed := time.Date(2026, time.July, 27, 13, 0, 0, 0, time.UTC)
	_, err := repository.ApplyHardwareSnapshotWithResult(ctx, HardwareSnapshot{
		BootEpoch:  "boot-conflict",
		Revision:   "snapshot-conflict",
		ObservedAt: observed,
		Lines: []HardwareLine{
			{
				ID:                  "endpoint-conflict-a",
				EquipmentIdentifier: "imei-conflict-a",
				PhoneNumber:         "+819055555555",
				ICCID:               "iccid-conflict-a",
				IMSI:                "imsi-conflict-a",
			},
			{
				ID:                  "endpoint-conflict-b",
				EquipmentIdentifier: "imei-conflict-b",
				PhoneNumber:         "+81 90-5555-5555",
				ICCID:               "iccid-conflict-b",
				IMSI:                "imsi-conflict-b",
			},
		},
	})
	if !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("ApplyHardwareSnapshotWithResult() error = %v, want ErrSnapshotInvalid", err)
	}
	var lines, devices, simCards int
	if err := repository.database.QueryRowContext(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM modemdeck_lines),
			(SELECT COUNT(*) FROM devices),
			(SELECT COUNT(*) FROM sim_cards)`,
	).Scan(&lines, &devices, &simCards); err != nil {
		t.Fatalf("count rolled-back snapshot rows: %v", err)
	}
	if lines != 0 || devices != 0 || simCards != 0 {
		t.Fatalf(
			"rolled-back conflict rows = lines %d, devices %d, SIMs %d",
			lines,
			devices,
			simCards,
		)
	}
}

func assertResolvedLineEndpoint(
	t *testing.T,
	repository *Store,
	lineID,
	expectedEndpointID string,
) {
	t.Helper()
	endpointID, err := repository.ResolveLineEndpoint(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ResolveLineEndpoint(%q) error = %v", lineID, err)
	}
	if endpointID != expectedEndpointID {
		t.Fatalf(
			"ResolveLineEndpoint(%q) = %q, want %q",
			lineID,
			endpointID,
			expectedEndpointID,
		)
	}
}

func assertSIMAttachment(
	t *testing.T,
	repository *Store,
	iccid,
	expectedIMEI string,
) {
	t.Helper()
	var currentIMEI sql.NullString
	if err := repository.database.QueryRowContext(
		context.Background(),
		"SELECT current_imei FROM sim_cards WHERE iccid = ?",
		iccid,
	).Scan(&currentIMEI); err != nil {
		t.Fatalf("read SIM attachment for %q: %v", iccid, err)
	}
	if got := stringValue(currentIMEI); got != expectedIMEI {
		t.Fatalf("SIM %q current IMEI = %q, want %q", iccid, got, expectedIMEI)
	}
}

func assertDeviceSIMAttachment(
	t *testing.T,
	repository *Store,
	imei,
	expectedICCID string,
	expectedInserted bool,
) {
	t.Helper()
	var (
		iccid       sql.NullString
		simInserted bool
	)
	if err := repository.database.QueryRowContext(
		context.Background(),
		"SELECT iccid, sim_inserted FROM devices WHERE imei = ?",
		imei,
	).Scan(&iccid, &simInserted); err != nil {
		t.Fatalf("read device SIM attachment for %q: %v", imei, err)
	}
	if got := stringValue(iccid); got != expectedICCID || simInserted != expectedInserted {
		t.Fatalf(
			"device %q SIM attachment = ICCID %q, inserted %v; want %q, %v",
			imei,
			got,
			simInserted,
			expectedICCID,
			expectedInserted,
		)
	}
}

func assertActiveLineAttachmentCount(
	t *testing.T,
	repository *Store,
	lineID string,
	expected int,
) {
	t.Helper()
	var count int
	if err := repository.database.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM sim_cards
		 WHERE line_id = ? AND COALESCE(current_imei, '') <> ''`,
		lineID,
	).Scan(&count); err != nil {
		t.Fatalf("count active attachments for %q: %v", lineID, err)
	}
	if count != expected {
		t.Fatalf("active attachments for %q = %d, want %d", lineID, count, expected)
	}
}
