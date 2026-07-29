package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/phone"
)

type stableLineEvidence struct {
	endpointID string
	iccid      string
	imsi       string
	phone      string
	label      string
	color      string
	observedAt time.Time
	ordinal    int
}

type stableLineRecord struct {
	id        string
	phone     string
	label     string
	color     string
	createdAt time.Time
	updatedAt time.Time
}

type stableEndpointBinding struct {
	lineID      string
	observedAt  time.Time
	ordinal     int
	provisional bool
}

type migrationDisjointSet struct {
	parent map[string]string
}

func newMigrationDisjointSet() *migrationDisjointSet {
	return &migrationDisjointSet{parent: make(map[string]string)}
}

func (set *migrationDisjointSet) add(value string) {
	if value == "" {
		return
	}
	if _, exists := set.parent[value]; !exists {
		set.parent[value] = value
	}
}

func (set *migrationDisjointSet) find(value string) string {
	parent, exists := set.parent[value]
	if !exists {
		return ""
	}
	if parent == value {
		return value
	}
	root := set.find(parent)
	set.parent[value] = root
	return root
}

func (set *migrationDisjointSet) union(left, right string) {
	set.add(left)
	set.add(right)
	leftRoot := set.find(left)
	rightRoot := set.find(right)
	if leftRoot == "" || rightRoot == "" || leftRoot == rightRoot {
		return
	}
	if leftRoot < rightRoot {
		set.parent[rightRoot] = leftRoot
		return
	}
	set.parent[leftRoot] = rightRoot
}

func migrateStableLineIdentity(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin stable line identity migration: %w", err)
	}
	defer transaction.Rollback()

	evidence, err := readStableLineEvidence(ctx, transaction)
	if err != nil {
		return err
	}
	lines, identityLines, endpointLines, err := buildStableLineMigration(evidence)
	if err != nil {
		return err
	}
	if err := createStableLineSchema(ctx, transaction); err != nil {
		return err
	}
	if err := insertStableLines(ctx, transaction, lines); err != nil {
		return err
	}
	if err := insertStableLineMigrationMaps(
		ctx,
		transaction,
		identityLines,
		endpointLines,
	); err != nil {
		return err
	}
	if err := bindStableLineIdentities(ctx, transaction); err != nil {
		return err
	}
	if err := normalizeStableSIMAttachments(ctx, transaction); err != nil {
		return err
	}
	if err := migrateStableLineHistory(ctx, transaction); err != nil {
		return err
	}
	if err := migrateStableLineSettings(ctx, transaction); err != nil {
		return err
	}
	if err := migrateStableLineReferences(ctx, transaction); err != nil {
		return err
	}
	if err := replaceStableLineIndexes(ctx, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit stable line identity migration: %w", err)
	}
	return nil
}

func readStableLineEvidence(
	ctx context.Context,
	transaction *sql.Tx,
) ([]stableLineEvidence, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT endpoint_id, iccid, imsi, phone_number, line_label, line_color, observed_at
		 FROM (
			SELECT
				'' AS endpoint_id,
				COALESCE(sc.iccid, '') AS iccid,
				COALESCE(sc.imsi, '') AS imsi,
				COALESCE(
					NULLIF(ss.phone_number, ''),
					NULLIF(ss.modem_phone_number, ''),
					NULLIF(ss.vowifi_phone_number, ''),
					''
				) AS phone_number,
				COALESCE(sc.line_label, '') AS line_label,
				COALESCE(sc.line_color, '') AS line_color,
				COALESCE(sc.last_seen, sc.updated_at, sc.created_at, '') AS observed_at
			FROM sim_cards sc
			LEFT JOIN sim_subscriptions ss ON ss.imsi = sc.imsi
			UNION ALL
			SELECT
				'',
				COALESCE(ss.current_iccid, ''),
				COALESCE(ss.imsi, ''),
				COALESCE(
					NULLIF(ss.phone_number, ''),
					NULLIF(ss.modem_phone_number, ''),
					NULLIF(ss.vowifi_phone_number, ''),
					''
				),
				'',
				'',
				COALESCE(ss.last_seen, ss.updated_at, ss.created_at, '')
			FROM sim_subscriptions ss
			UNION ALL
			SELECT
				'',
				COALESCE(sms_contacts.iccid, ''),
				COALESCE(sms_contacts.imsi, ''),
				'',
				'',
				'',
				COALESCE(
					sms_contacts.updated_at,
					sms_contacts.last_timestamp,
					sms_contacts.created_at,
					''
				)
			FROM sms_contacts
			UNION ALL
			SELECT
				COALESCE(sms.line_id, ''),
				COALESCE(sms.iccid, ''),
				COALESCE(sms.imsi, ''),
				COALESCE(sms.local_phone, ''),
				'',
				'',
				COALESCE(sms.timestamp, sms.created_at, '')
			FROM sms
			UNION ALL
			SELECT
				COALESCE(call_history.device_id, ''),
				COALESCE(call_history.line_iccid, ''),
				COALESCE(call_history.line_imsi, ''),
				COALESCE(call_history.local_phone, ''),
				'',
				'',
				COALESCE(
					call_history.updated_at,
					call_history.ended_at,
					call_history.created_at,
					''
				)
			FROM call_history
		 ) evidence`,
	)
	if err != nil {
		return nil, fmt.Errorf("read stable line identity evidence: %w", err)
	}
	defer rows.Close()

	evidence := make([]stableLineEvidence, 0)
	ordinal := 0
	for rows.Next() {
		var endpointID, iccid, imsi, number, label, color, observed string
		if err := rows.Scan(
			&endpointID,
			&iccid,
			&imsi,
			&number,
			&label,
			&color,
			&observed,
		); err != nil {
			return nil, fmt.Errorf("scan stable line identity evidence: %w", err)
		}
		evidence = append(evidence, stableLineEvidence{
			endpointID: strings.TrimSpace(endpointID),
			iccid:      strings.TrimSpace(iccid),
			imsi:       strings.TrimSpace(imsi),
			phone:      normalizeStableLinePhone(number),
			label:      strings.TrimSpace(label),
			color:      strings.TrimSpace(color),
			observedAt: parseMigrationTime(observed),
			ordinal:    ordinal,
		})
		ordinal++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stable line identity evidence: %w", err)
	}
	return evidence, nil
}

func buildStableLineMigration(
	evidence []stableLineEvidence,
) (
	[]stableLineRecord,
	map[string]string,
	map[string]stableEndpointBinding,
	error,
) {
	simIdentities := newMigrationDisjointSet()
	for _, item := range evidence {
		tokens := stableSIMIdentityTokens(item.iccid, item.imsi)
		for _, token := range tokens {
			simIdentities.add(token)
		}
		for index := 1; index < len(tokens); index++ {
			simIdentities.union(tokens[0], tokens[index])
		}
	}

	type phoneObservation struct {
		phone      string
		observedAt time.Time
		ordinal    int
	}
	latestPhoneBySIMRoot := make(map[string]phoneObservation)
	for _, item := range evidence {
		number := normalizeStableLinePhone(item.phone)
		if number == "" {
			continue
		}
		tokens := stableSIMIdentityTokens(item.iccid, item.imsi)
		if len(tokens) == 0 {
			continue
		}
		root := simIdentities.find(tokens[0])
		current, exists := latestPhoneBySIMRoot[root]
		if !exists || migrationValueIsNewer(
			item.observedAt,
			item.ordinal,
			current.observedAt,
			current.ordinal,
		) {
			latestPhoneBySIMRoot[root] = phoneObservation{
				phone:      number,
				observedAt: item.observedAt,
				ordinal:    item.ordinal,
			}
		}
	}

	groupForEvidence := func(item stableLineEvidence) string {
		if number := normalizeStableLinePhone(item.phone); number != "" {
			return "phone:" + number
		}
		tokens := stableSIMIdentityTokens(item.iccid, item.imsi)
		if len(tokens) > 0 {
			root := simIdentities.find(tokens[0])
			if latest := latestPhoneBySIMRoot[root]; latest.phone != "" {
				return "phone:" + latest.phone
			}
			return "identity:" + root
		}
		if endpointID := strings.TrimSpace(item.endpointID); endpointID != "" {
			return "endpoint:" + endpointID
		}
		return ""
	}

	type lineCandidate struct {
		record      stableLineRecord
		labelAt     time.Time
		labelOrder  int
		colorAt     time.Time
		colorOrder  int
		hasObserved bool
	}
	groupLineIDs := make(map[string]string)
	candidates := make(map[string]*lineCandidate)
	ensureCandidate := func(group string) (*lineCandidate, error) {
		if candidate := candidates[group]; candidate != nil {
			return candidate, nil
		}
		lineID := ""
		if endpointID := strings.TrimPrefix(group, "endpoint:"); group != endpointID &&
			validProvisionalStableLineID(endpointID) {
			lineID = endpointID
		} else {
			var err error
			lineID, err = newStableLineID()
			if err != nil {
				return nil, err
			}
		}
		record := stableLineRecord{id: lineID}
		if number := strings.TrimPrefix(group, "phone:"); group != number {
			record.phone = number
		}
		candidate := &lineCandidate{record: record}
		groupLineIDs[group] = lineID
		candidates[group] = candidate
		return candidate, nil
	}

	endpointEvidence := make(map[string][]stableLineEvidence)
	for _, item := range evidence {
		group := groupForEvidence(item)
		if group == "" {
			continue
		}
		candidate, err := ensureCandidate(group)
		if err != nil {
			return nil, nil, nil, err
		}
		if !item.observedAt.IsZero() {
			if !candidate.hasObserved || item.observedAt.Before(candidate.record.createdAt) {
				candidate.record.createdAt = item.observedAt
			}
			if !candidate.hasObserved || item.observedAt.After(candidate.record.updatedAt) {
				candidate.record.updatedAt = item.observedAt
			}
			candidate.hasObserved = true
		}
		if item.label != "" && migrationValueIsNewer(
			item.observedAt,
			item.ordinal,
			candidate.labelAt,
			candidate.labelOrder,
		) {
			candidate.record.label = item.label
			candidate.labelAt = item.observedAt
			candidate.labelOrder = item.ordinal
		}
		if item.color != "" && migrationValueIsNewer(
			item.observedAt,
			item.ordinal,
			candidate.colorAt,
			candidate.colorOrder,
		) {
			candidate.record.color = item.color
			candidate.colorAt = item.observedAt
			candidate.colorOrder = item.ordinal
		}
		if item.endpointID != "" {
			endpointEvidence[item.endpointID] = append(endpointEvidence[item.endpointID], item)
		}
	}

	identityLines := make(map[string]string)
	for group, lineID := range groupLineIDs {
		if strings.HasPrefix(group, "phone:") {
			identityLines[group] = lineID
		}
	}
	for token := range simIdentities.parent {
		root := simIdentities.find(token)
		group := "identity:" + root
		if latest := latestPhoneBySIMRoot[root]; latest.phone != "" {
			group = "phone:" + latest.phone
		}
		if lineID := groupLineIDs[group]; lineID != "" {
			identityLines[token] = lineID
		}
	}

	endpointLines := make(map[string]stableEndpointBinding, len(endpointEvidence))
	for endpointID, observations := range endpointEvidence {
		sort.SliceStable(observations, func(left, right int) bool {
			if observations[left].observedAt.Equal(observations[right].observedAt) {
				return observations[left].ordinal < observations[right].ordinal
			}
			return observations[left].observedAt.Before(observations[right].observedAt)
		})
		latest := observations[len(observations)-1]
		group := groupForEvidence(latest)
		endpointLines[endpointID] = stableEndpointBinding{
			lineID:      groupLineIDs[group],
			observedAt:  latest.observedAt,
			ordinal:     latest.ordinal,
			provisional: strings.HasPrefix(group, "endpoint:"),
		}
	}

	now := time.Now().UTC()
	lines := make([]stableLineRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.hasObserved {
			candidate.record.createdAt = now
			candidate.record.updatedAt = now
		}
		lines = append(lines, candidate.record)
	}
	sort.Slice(lines, func(left, right int) bool {
		return lines[left].id < lines[right].id
	})
	return lines, identityLines, endpointLines, nil
}

func stableLineIdentityTokens(iccid, imsi, number string) []string {
	result := make([]string, 0, 3)
	if number = normalizeStableLinePhone(number); number != "" {
		result = append(result, "phone:"+number)
	}
	result = append(result, stableSIMIdentityTokens(iccid, imsi)...)
	return result
}

func stableSIMIdentityTokens(iccid, imsi string) []string {
	result := make([]string, 0, 2)
	if iccid = strings.TrimSpace(iccid); iccid != "" {
		result = append(result, "iccid:"+iccid)
	}
	if imsi = strings.TrimSpace(imsi); imsi != "" {
		result = append(result, "imsi:"+imsi)
	}
	return result
}

func migrationValueIsNewer(
	candidateTime time.Time,
	candidateOrder int,
	currentTime time.Time,
	currentOrder int,
) bool {
	if candidateTime.Equal(currentTime) {
		return candidateOrder >= currentOrder
	}
	if currentTime.IsZero() {
		return true
	}
	if candidateTime.IsZero() {
		return false
	}
	return candidateTime.After(currentTime)
}

func createStableLineSchema(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`CREATE TABLE modemdeck_lines (
			line_id TEXT PRIMARY KEY,
			phone_number TEXT NOT NULL DEFAULT '',
			home_country_iso TEXT NOT NULL DEFAULT '',
			line_label TEXT NOT NULL DEFAULT '',
			line_color TEXT NOT NULL DEFAULT ''
				CHECK (line_color IN (
					'', 'teal', 'blue', 'indigo', 'violet',
					'green', 'amber', 'orange', 'red'
				)),
			delivery_reports_enabled NUMERIC NOT NULL DEFAULT 0,
			delivery_reports_support TEXT NOT NULL DEFAULT 'unknown'
				CHECK (delivery_reports_support IN ('unknown', 'unsupported')),
			message_policy_revision INTEGER NOT NULL DEFAULT 1
				CHECK (message_policy_revision > 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CHECK (line_id GLOB 'line_*' AND length(line_id) > 5)
			);
			CREATE TABLE modemdeck_legacy_endpoint_lines (
				endpoint_id TEXT PRIMARY KEY,
				line_id TEXT NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			ALTER TABLE contacts ADD COLUMN preferred_line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE sms ADD COLUMN endpoint_line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE call_history ADD COLUMN line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE call_history ADD COLUMN endpoint_line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE modemdeck_line_settings ADD COLUMN default_line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE modemdeck_incoming_call_actions
			ADD COLUMN endpoint_line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE devices ADD COLUMN endpoint_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE sim_cards ADD COLUMN line_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE sim_subscriptions ADD COLUMN line_id TEXT NOT NULL DEFAULT '';`,
	); err != nil {
		return fmt.Errorf("create stable line identity schema: %w", err)
	}
	return nil
}

func insertStableLines(
	ctx context.Context,
	transaction *sql.Tx,
	lines []stableLineRecord,
) error {
	for _, line := range lines {
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_lines (
				line_id, phone_number, home_country_iso,
				line_label, line_color, created_at, updated_at
			 ) VALUES (?, ?, '', ?, ?, ?, ?)`,
			line.id,
			line.phone,
			line.label,
			line.color,
			line.createdAt.UTC(),
			line.updatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert stable line identity: %w", err)
		}
	}
	return nil
}

func insertStableLineMigrationMaps(
	ctx context.Context,
	transaction *sql.Tx,
	identityLines map[string]string,
	endpointLines map[string]stableEndpointBinding,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		`CREATE TEMP TABLE modemdeck_line_identity_migration (
			identity_key TEXT PRIMARY KEY,
			line_id TEXT NOT NULL
		);
		CREATE TEMP TABLE modemdeck_line_endpoint_migration (
			endpoint_id TEXT PRIMARY KEY,
			line_id TEXT NOT NULL,
			observed_at DATETIME,
			ordinal INTEGER NOT NULL
		);`,
	); err != nil {
		return fmt.Errorf("create stable line migration maps: %w", err)
	}
	for identity, lineID := range identityLines {
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_line_identity_migration (identity_key, line_id)
			 VALUES (?, ?)`,
			identity,
			lineID,
		); err != nil {
			return fmt.Errorf("insert stable line identity map: %w", err)
		}
	}
	for endpointID, binding := range endpointLines {
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_line_endpoint_migration (
				endpoint_id, line_id, observed_at, ordinal
			 ) VALUES (?, ?, ?, ?)`,
			endpointID,
			binding.lineID,
			nullableMigrationTime(binding.observedAt),
			binding.ordinal,
		); err != nil {
			return fmt.Errorf("insert stable line endpoint map: %w", err)
		}
		if binding.provisional && !validProvisionalStableLineID(endpointID) {
			if _, err := transaction.ExecContext(
				ctx,
				`INSERT INTO modemdeck_legacy_endpoint_lines (endpoint_id, line_id)
				 VALUES (?, ?)`,
				endpointID,
				binding.lineID,
			); err != nil {
				return fmt.Errorf("persist provisional stable line endpoint map: %w", err)
			}
		}
	}
	return nil
}

func bindStableLineIdentities(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE sim_cards
		 SET line_id = COALESCE((
			SELECT line_id
			FROM modemdeck_line_identity_migration
			WHERE identity_key = 'iccid:' || sim_cards.iccid
		 ), '');
		UPDATE sim_subscriptions
		 SET line_id = COALESCE((
			SELECT line_id
			FROM modemdeck_line_identity_migration
			WHERE identity_key = 'imsi:' || sim_subscriptions.imsi
		 ), '');
		UPDATE devices
		 SET endpoint_id = COALESCE((
			SELECT endpoint.endpoint_id
			FROM sim_cards
			JOIN modemdeck_line_endpoint_migration endpoint
				ON endpoint.line_id = sim_cards.line_id
			WHERE sim_cards.current_imei = devices.imei
			ORDER BY endpoint.observed_at DESC, endpoint.ordinal DESC, endpoint.endpoint_id
			LIMIT 1
		 ), '');`,
	); err != nil {
		return fmt.Errorf("bind stable line identities: %w", err)
	}
	return nil
}

func normalizeStableSIMAttachments(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`CREATE TEMP TABLE current_line_attachment_migration AS
		 SELECT line_id, current_imei, iccid, last_seen, updated_at
		 FROM (
			SELECT
				line_id,
				current_imei,
				iccid,
				last_seen,
				updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY line_id
					ORDER BY last_seen DESC, updated_at DESC, iccid DESC
				) AS attachment_rank
			FROM sim_cards
			WHERE line_id <> '' AND COALESCE(current_imei, '') <> ''
		 ) ranked
		 WHERE attachment_rank = 1;
		 CREATE TEMP TABLE current_sim_attachment_migration AS
		 SELECT current_imei, iccid
		 FROM (
			SELECT
				current_imei,
				iccid,
				ROW_NUMBER() OVER (
					PARTITION BY current_imei
					ORDER BY last_seen DESC, updated_at DESC, iccid DESC
				) AS attachment_rank
			FROM current_line_attachment_migration
		 ) ranked
		 WHERE attachment_rank = 1;
		 CREATE TEMP TABLE current_endpoint_attachment_migration AS
		 SELECT endpoint_id, imei
		 FROM (
			SELECT
				endpoint_id,
				imei,
				ROW_NUMBER() OVER (
					PARTITION BY endpoint_id
					ORDER BY last_seen DESC, updated_at DESC, imei DESC
				) AS attachment_rank
			FROM devices
			WHERE endpoint_id <> ''
		 ) ranked
		 WHERE attachment_rank = 1;
		 UPDATE devices
		 SET endpoint_id = ''
		 WHERE endpoint_id <> ''
			AND NOT EXISTS (
				SELECT 1
				FROM current_endpoint_attachment_migration current
				WHERE current.endpoint_id = devices.endpoint_id
					AND current.imei = devices.imei
			);
		 UPDATE sim_cards
		 SET current_imei = ''
		 WHERE COALESCE(current_imei, '') <> ''
			AND NOT EXISTS (
				SELECT 1
				FROM current_sim_attachment_migration current
				WHERE current.current_imei = sim_cards.current_imei
					AND current.iccid = sim_cards.iccid
			);
		 UPDATE devices
		 SET
			iccid = (
				SELECT current.iccid
				FROM current_sim_attachment_migration current
				WHERE current.current_imei = devices.imei
			),
			sim_inserted = EXISTS (
				SELECT 1
				FROM current_sim_attachment_migration current
				WHERE current.current_imei = devices.imei
			);`,
	); err != nil {
		return fmt.Errorf("normalize current SIM attachments: %w", err)
	}
	return nil
}

func migrateStableLineHistory(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(ctx, `DROP INDEX ux_sms_endpoint_message`); err != nil {
		return fmt.Errorf("drop legacy message endpoint index: %w", err)
	}
	messageRows, err := transaction.QueryContext(
		ctx,
		`SELECT id, line_id, iccid, imsi, local_phone FROM sms`,
	)
	if err != nil {
		return fmt.Errorf("read messages for stable line migration: %w", err)
	}
	type messageBinding struct {
		id       int64
		endpoint string
		lineID   string
	}
	messageBindings := make([]messageBinding, 0)
	for messageRows.Next() {
		var id int64
		var endpointID, iccid, imsi, number string
		if err := messageRows.Scan(&id, &endpointID, &iccid, &imsi, &number); err != nil {
			_ = messageRows.Close()
			return fmt.Errorf("scan message stable line migration: %w", err)
		}
		lineID, err := lookupMigratedLine(
			ctx,
			transaction,
			iccid,
			imsi,
			number,
			endpointID,
		)
		if err != nil {
			_ = messageRows.Close()
			return err
		}
		messageBindings = append(messageBindings, messageBinding{
			id:       id,
			endpoint: strings.TrimSpace(endpointID),
			lineID:   lineID,
		})
	}
	if err := messageRows.Close(); err != nil {
		return fmt.Errorf("close messages for stable line migration: %w", err)
	}
	for _, binding := range messageBindings {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE sms SET line_id = ?, endpoint_line_id = ? WHERE id = ?`,
			binding.lineID,
			binding.endpoint,
			binding.id,
		); err != nil {
			return fmt.Errorf("migrate message stable line identity: %w", err)
		}
	}
	if err := migrateStableMessageThreads(ctx, transaction); err != nil {
		return err
	}

	callRows, err := transaction.QueryContext(
		ctx,
		`SELECT id, device_id, line_iccid, line_imsi, local_phone FROM call_history`,
	)
	if err != nil {
		return fmt.Errorf("read calls for stable line migration: %w", err)
	}
	type callBinding struct {
		id       string
		endpoint string
		lineID   string
	}
	callBindings := make([]callBinding, 0)
	for callRows.Next() {
		var id, endpointID, iccid, imsi, number string
		if err := callRows.Scan(&id, &endpointID, &iccid, &imsi, &number); err != nil {
			_ = callRows.Close()
			return fmt.Errorf("scan call stable line migration: %w", err)
		}
		lineID, err := lookupMigratedLine(
			ctx,
			transaction,
			iccid,
			imsi,
			number,
			endpointID,
		)
		if err != nil {
			_ = callRows.Close()
			return err
		}
		callBindings = append(callBindings, callBinding{
			id:       id,
			endpoint: strings.TrimSpace(endpointID),
			lineID:   lineID,
		})
	}
	if err := callRows.Close(); err != nil {
		return fmt.Errorf("close calls for stable line migration: %w", err)
	}
	for _, binding := range callBindings {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE call_history
			 SET line_id = ?, endpoint_line_id = ?
			 WHERE id = ?`,
			binding.lineID,
			binding.endpoint,
			binding.id,
		); err != nil {
			return fmt.Errorf("migrate call stable line identity: %w", err)
		}
	}
	return nil
}

func migrateStableMessageThreads(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`ALTER TABLE sms_contacts RENAME TO sms_contacts_v1;
		CREATE TABLE sms_contacts (
			line_id TEXT NOT NULL,
			imsi TEXT NOT NULL,
			iccid TEXT NOT NULL DEFAULT '',
			peer TEXT NOT NULL,
			last_sms_id INTEGER NOT NULL DEFAULT 0,
			last_timestamp DATETIME,
			last_content TEXT NOT NULL DEFAULT '',
			last_type INTEGER NOT NULL DEFAULT 0,
			unread_count INTEGER NOT NULL DEFAULT 0,
			marked_unread NUMERIC NOT NULL DEFAULT 0,
			is_favorite NUMERIC NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			PRIMARY KEY (line_id, peer)
		);
		WITH mapped_unread AS (
			SELECT
				COALESCE((
					SELECT sms.line_id
					FROM sms
					WHERE sms.imsi = legacy.imsi AND sms.peer = legacy.peer
					ORDER BY sms.timestamp DESC, sms.id DESC
					LIMIT 1
				), (
					SELECT line_id
					FROM modemdeck_line_identity_migration
					WHERE identity_key = 'imsi:' || legacy.imsi
				), (
					SELECT line_id
					FROM modemdeck_line_identity_migration
					WHERE identity_key = 'iccid:' || legacy.iccid
				), '') AS line_id,
				legacy.peer,
				legacy.unread_count,
				legacy.marked_unread,
				legacy.is_favorite
			FROM sms_contacts_v1 legacy
		),
		thread_unread AS (
			SELECT
				line_id,
				peer,
				SUM(unread_count) AS unread_count,
				MAX(marked_unread) AS marked_unread,
				MAX(is_favorite) AS is_favorite
			FROM mapped_unread
			WHERE line_id <> ''
			GROUP BY line_id, peer
		),
		ranked_messages AS (
			SELECT
				sms.*,
				ROW_NUMBER() OVER (
					PARTITION BY sms.line_id, sms.peer
					ORDER BY sms.timestamp DESC, sms.id DESC
				) AS message_rank,
				MIN(COALESCE(sms.created_at, sms.timestamp)) OVER (
					PARTITION BY sms.line_id, sms.peer
				) AS thread_created_at,
				MAX(COALESCE(sms.timestamp, sms.created_at)) OVER (
					PARTITION BY sms.line_id, sms.peer
				) AS thread_updated_at
			FROM sms
			WHERE sms.line_id <> '' AND sms.peer <> ''
		)
		INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count, marked_unread, is_favorite, created_at, updated_at
		)
		SELECT
			message.line_id,
			message.imsi,
			message.iccid,
			message.peer,
			message.id,
			message.timestamp,
			message.content,
			message.type,
			COALESCE(thread_unread.unread_count, 0),
			COALESCE(thread_unread.marked_unread, 0),
			COALESCE(thread_unread.is_favorite, 0),
			message.thread_created_at,
			message.thread_updated_at
		FROM ranked_messages message
		LEFT JOIN thread_unread
			ON thread_unread.line_id = message.line_id
			AND thread_unread.peer = message.peer
		WHERE message.message_rank = 1;
		DROP TABLE sms_contacts_v1;
		CREATE INDEX idx_sms_contacts_iccid_timestamp
			ON sms_contacts(iccid, last_timestamp DESC);
		CREATE INDEX idx_sms_contacts_timestamp
			ON sms_contacts(last_timestamp DESC);`,
	); err != nil {
		return fmt.Errorf("rebuild stable message threads: %w", err)
	}
	return nil
}

func lookupMigratedLine(
	ctx context.Context,
	transaction *sql.Tx,
	iccid, imsi, number, endpointID string,
) (string, error) {
	identities := stableLineIdentityTokens(iccid, imsi, number)
	for _, identity := range identities {
		var lineID string
		err := transaction.QueryRowContext(
			ctx,
			`SELECT line_id
			 FROM modemdeck_line_identity_migration
			 WHERE identity_key = ?`,
			identity,
		).Scan(&lineID)
		if err == nil {
			return lineID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("resolve migrated stable line identity: %w", err)
		}
	}
	endpointID = strings.TrimSpace(endpointID)
	if len(identities) == 0 && endpointID == "" {
		lineID, err := newStableLineID()
		if err != nil {
			return "", err
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_lines (
				line_id, phone_number, home_country_iso,
				line_label, line_color, created_at, updated_at
			 ) VALUES (?, '', '', '', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			lineID,
		); err != nil {
			return "", fmt.Errorf("create anonymous migrated stable line: %w", err)
		}
		return lineID, nil
	}
	var lineID string
	err := transaction.QueryRowContext(
		ctx,
		`SELECT line_id
		 FROM modemdeck_line_endpoint_migration
		 WHERE endpoint_id = ?`,
		endpointID,
	).Scan(&lineID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("stable line migration has no identity for endpoint %q", endpointID)
	}
	if err != nil {
		return "", fmt.Errorf("resolve migrated endpoint line: %w", err)
	}
	return lineID, nil
}

func migrateStableLineSettings(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE contacts
		 SET preferred_line_id = COALESCE((
			SELECT sim_cards.line_id
			FROM devices
			JOIN sim_cards ON sim_cards.iccid = devices.iccid
			WHERE devices.imei = contacts.preferred_device_imei
			LIMIT 1
		 ), (
			SELECT line_id
			FROM modemdeck_line_endpoint_migration
			WHERE endpoint_id = contacts.preferred_device_imei
		 ), '')
		 WHERE preferred_device_imei <> '';
		UPDATE modemdeck_line_settings
		 SET default_line_id = COALESCE((
			SELECT sim_cards.line_id
			FROM devices
			JOIN sim_cards ON sim_cards.iccid = devices.iccid
			WHERE devices.imei = modemdeck_line_settings.default_device_imei
			LIMIT 1
		 ), (
			SELECT line_id
			FROM modemdeck_line_endpoint_migration
			WHERE endpoint_id = modemdeck_line_settings.default_device_imei
		 ), '')
		 WHERE singleton = 1;`,
	); err != nil {
		return fmt.Errorf("migrate stable line settings: %w", err)
	}
	return nil
}

func migrateStableLineReferences(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`CREATE TEMP TABLE legacy_scoped_telegram_units AS
		 SELECT DISTINCT unit_id FROM modemdeck_telegram_line_scopes;

		CREATE TEMP TABLE migrated_line_call_policies AS
		 SELECT line_id, policy, revision, updated_at
		 FROM (
			SELECT
				map.line_id,
				policy.policy,
				policy.revision,
				policy.updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY map.line_id
					ORDER BY policy.updated_at DESC, policy.revision DESC, policy.line_id DESC
				) AS rank
			FROM modemdeck_line_call_policies policy
			JOIN modemdeck_line_endpoint_migration map
				ON map.endpoint_id = policy.line_id
		 ) ranked
		 WHERE rank = 1;
		DELETE FROM modemdeck_line_call_policies;
		INSERT INTO modemdeck_line_call_policies (line_id, policy, revision, updated_at)
		 SELECT line_id, policy, revision, updated_at FROM migrated_line_call_policies;

		CREATE TEMP TABLE migrated_telegram_line_scopes AS
		 SELECT DISTINCT
			scope.unit_id,
			map.line_id
		 FROM modemdeck_telegram_line_scopes scope
		 JOIN modemdeck_line_endpoint_migration map
			ON map.endpoint_id = scope.line_id;
		DELETE FROM modemdeck_telegram_line_scopes;
		INSERT INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 SELECT unit_id, line_id FROM migrated_telegram_line_scopes;
		UPDATE modemdeck_telegram_units
		 SET enabled = 0,
			updated_at = CURRENT_TIMESTAMP
		 WHERE id IN (SELECT unit_id FROM legacy_scoped_telegram_units)
			AND NOT EXISTS (
				SELECT 1 FROM modemdeck_telegram_line_scopes scope
				WHERE scope.unit_id = modemdeck_telegram_units.id
			);

		CREATE TEMP TABLE migrated_telegram_reply_bindings AS
		 SELECT binding.bot_id, binding.chat_id, binding.message_id,
			map.line_id, binding.phone_number, binding.created_at
		 FROM modemdeck_telegram_reply_bindings binding
		 JOIN modemdeck_line_endpoint_migration map
			ON map.endpoint_id = binding.line_id;
		DELETE FROM modemdeck_telegram_reply_bindings;
		INSERT INTO modemdeck_telegram_reply_bindings (
			bot_id, chat_id, message_id, line_id, phone_number, created_at
		 )
		 SELECT bot_id, chat_id, message_id, line_id, phone_number, created_at
		 FROM migrated_telegram_reply_bindings;

		DELETE FROM modemdeck_proxy_instances
		 WHERE NOT EXISTS (
			SELECT 1 FROM modemdeck_line_endpoint_migration map
			WHERE map.endpoint_id = modemdeck_proxy_instances.line_id
		 );
		UPDATE modemdeck_proxy_instances
		 SET line_id = (
			SELECT line_id FROM modemdeck_line_endpoint_migration
			WHERE endpoint_id = modemdeck_proxy_instances.line_id
		 );

		CREATE TEMP TABLE migrated_network_selection_policies AS
		 SELECT line_id, mode, operator_code, configured, revision, applied_revision,
			applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 FROM (
			SELECT
				map.line_id,
				policy.mode,
				policy.operator_code,
				policy.configured,
				policy.revision,
				policy.applied_revision,
				policy.applied_boot_epoch,
				policy.applied_at,
				policy.last_error,
				policy.created_at,
				policy.updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY map.line_id
					ORDER BY policy.updated_at DESC, policy.revision DESC, policy.line_id DESC
				) AS rank
			FROM modemdeck_network_selection_policies policy
			JOIN modemdeck_line_endpoint_migration map
				ON map.endpoint_id = policy.line_id
		 ) ranked
		 WHERE rank = 1;
		DELETE FROM modemdeck_network_selection_policies;
		INSERT INTO modemdeck_network_selection_policies (
			line_id, mode, operator_code, configured, revision, applied_revision,
			applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 )
		 SELECT line_id, mode, operator_code, configured, revision, applied_revision,
			applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 FROM migrated_network_selection_policies;

		UPDATE modemdeck_notification_events
		 SET line_id = COALESCE(
			CASE
				WHEN event_key LIKE 'sms:%' THEN (
					SELECT sms.line_id FROM sms
					WHERE 'sms:' || sms.id = modemdeck_notification_events.event_key
				)
				WHEN event_key LIKE 'call:%' THEN (
					SELECT call_history.line_id FROM call_history
					WHERE 'call:' || call_history.id = modemdeck_notification_events.event_key
				)
			END,
			(
				SELECT line_id FROM modemdeck_line_endpoint_migration
				WHERE endpoint_id = modemdeck_notification_events.line_id
			),
			''
		 );
		DELETE FROM modemdeck_notification_events
		 WHERE line_id = ''
			OR NOT EXISTS (
				SELECT 1 FROM modemdeck_lines line
				WHERE line.line_id = modemdeck_notification_events.line_id
			);

		UPDATE modemdeck_incoming_call_actions
		 SET endpoint_line_id = line_id,
			line_id = COALESCE((
				SELECT call_history.line_id FROM call_history
				WHERE call_history.id = modemdeck_incoming_call_actions.call_id
			), (
				SELECT line_id FROM modemdeck_line_endpoint_migration
				WHERE endpoint_id = modemdeck_incoming_call_actions.line_id
			), '');
		DELETE FROM modemdeck_incoming_call_actions
		 WHERE line_id = ''
			OR NOT EXISTS (
				SELECT 1 FROM modemdeck_lines line
				WHERE line.line_id = modemdeck_incoming_call_actions.line_id
			);`,
	); err != nil {
		return fmt.Errorf("migrate stable line references: %w", err)
	}
	if err := migrateNetworkCounterIdentity(ctx, transaction); err != nil {
		return err
	}
	return nil
}

func migrateNetworkCounterIdentity(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`ALTER TABLE modemdeck_network_counter_checkpoints
		 RENAME TO modemdeck_network_counter_checkpoints_v1;
		CREATE TABLE modemdeck_network_counter_checkpoints (
			scope_kind TEXT NOT NULL CHECK (scope_kind IN ('line', 'proxy')),
			scope_id TEXT NOT NULL,
			endpoint_scope_id TEXT NOT NULL,
			epoch TEXT NOT NULL,
			rx_bytes INTEGER NOT NULL CHECK (rx_bytes >= 0),
			tx_bytes INTEGER NOT NULL CHECK (tx_bytes >= 0),
			observed_at DATETIME NOT NULL,
			PRIMARY KEY (scope_kind, endpoint_scope_id)
		);
		INSERT INTO modemdeck_network_counter_checkpoints (
			scope_kind, scope_id, endpoint_scope_id, epoch, rx_bytes, tx_bytes, observed_at
		 )
		 SELECT
			checkpoint.scope_kind,
			CASE
				WHEN checkpoint.scope_kind = 'line'
					THEN map.line_id
				ELSE checkpoint.scope_id
			END,
			checkpoint.scope_id,
			checkpoint.epoch,
			checkpoint.rx_bytes,
			checkpoint.tx_bytes,
			checkpoint.observed_at
		 FROM modemdeck_network_counter_checkpoints_v1 checkpoint
		 LEFT JOIN modemdeck_line_endpoint_migration map
			ON checkpoint.scope_kind = 'line' AND map.endpoint_id = checkpoint.scope_id
		 WHERE checkpoint.scope_kind <> 'line' OR map.line_id IS NOT NULL;
		DROP TABLE modemdeck_network_counter_checkpoints_v1;

		CREATE TEMP TABLE migrated_network_daily_usage AS
		 SELECT
			usage.day,
			usage.scope_kind,
			CASE
				WHEN usage.scope_kind = 'line'
					THEN map.line_id
				ELSE usage.scope_id
			END AS scope_id,
			SUM(usage.rx_bytes) AS rx_bytes,
			SUM(usage.tx_bytes) AS tx_bytes,
			MAX(usage.updated_at) AS updated_at
		 FROM modemdeck_network_daily_usage usage
		 LEFT JOIN modemdeck_line_endpoint_migration map
			ON usage.scope_kind = 'line' AND map.endpoint_id = usage.scope_id
		 WHERE usage.scope_kind <> 'line' OR map.line_id IS NOT NULL
		 GROUP BY usage.day, usage.scope_kind,
			CASE
				WHEN usage.scope_kind = 'line'
					THEN map.line_id
				ELSE usage.scope_id
			END;
		DELETE FROM modemdeck_network_daily_usage;
		INSERT INTO modemdeck_network_daily_usage (
			day, scope_kind, scope_id, rx_bytes, tx_bytes, updated_at
		 )
		 SELECT day, scope_kind, scope_id, rx_bytes, tx_bytes, updated_at
		 FROM migrated_network_daily_usage;`,
	); err != nil {
		return fmt.Errorf("migrate network counter stable line identity: %w", err)
	}
	return nil
}

func replaceStableLineIndexes(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(
		ctx,
		`DROP INDEX idx_contacts_preferred_device;
		DROP INDEX idx_call_history_device_ended_at;
		CREATE INDEX idx_contacts_preferred_line ON contacts(preferred_line_id);
		CREATE UNIQUE INDEX ux_sms_endpoint_line_message
			ON sms(endpoint_line_id, endpoint_message_id)
			WHERE endpoint_line_id <> '' AND endpoint_message_id <> '';
		CREATE INDEX idx_call_history_line_ended_at
			ON call_history(line_id, ended_at DESC);
		CREATE INDEX idx_call_history_endpoint_line_ended_at
			ON call_history(endpoint_line_id, ended_at DESC);
		CREATE UNIQUE INDEX ux_modemdeck_lines_phone_number
			ON modemdeck_lines(phone_number) WHERE phone_number <> '';
		CREATE UNIQUE INDEX ux_devices_endpoint_id
			ON devices(endpoint_id) WHERE endpoint_id <> '';
		CREATE UNIQUE INDEX ux_devices_current_iccid
			ON devices(iccid) WHERE COALESCE(iccid, '') <> '';
		CREATE INDEX idx_sim_cards_line_id ON sim_cards(line_id);
		CREATE UNIQUE INDEX ux_sim_cards_current_imei
			ON sim_cards(current_imei) WHERE COALESCE(current_imei, '') <> '';
		CREATE INDEX idx_sim_subscriptions_line_id ON sim_subscriptions(line_id);`,
	); err != nil {
		return fmt.Errorf("replace stable line identity indexes: %w", err)
	}
	return nil
}

func normalizeStableLinePhone(value string) string {
	return phone.NetworkSubscriberE164(strings.TrimSpace(value), "")
}

func validProvisionalStableLineID(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "line_") && len(value) > len("line_") && len(value) <= 64
}

func parseMigrationTime(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		time.DateOnly,
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func nullableMigrationTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func newStableLineID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate stable line ID: %w", err)
	}
	return "line_" + hex.EncodeToString(random[:]), nil
}
