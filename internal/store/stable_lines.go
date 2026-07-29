package store

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

type stableLineQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type stableLineIdentityMatches struct {
	ICCID        string
	IMSI         string
	Phone        string
	PhoneAliases []string
}

func resolveOrCreateStableLine(
	ctx context.Context,
	transaction *sql.Tx,
	iccid, imsi, number, region string,
	observedAt time.Time,
) (string, error) {
	iccid = strings.TrimSpace(iccid)
	imsi = strings.TrimSpace(imsi)
	number = normalizeStableLinePhone(number, region)

	matches, err := resolveStableLineIdentityMatches(ctx, transaction, iccid, imsi, number)
	if err != nil {
		return "", err
	}
	linePhones, err := stableLinePhones(
		ctx,
		transaction,
		matches.ICCID,
		matches.IMSI,
		matches.Phone,
	)
	if err != nil {
		return "", err
	}
	phoneAliases, err := stableLinePhones(
		ctx,
		transaction,
		matches.PhoneAliases...,
	)
	if err != nil {
		return "", err
	}
	for lineID, phone := range phoneAliases {
		linePhones[lineID] = phone
	}
	lineID := chooseStableLineCanonical(matches, linePhones, number)
	if lineID == "" {
		if iccid == "" && imsi == "" && number == "" {
			return "", ErrLineNotFound
		}
		lineID, err = newStoreLineID()
		if err != nil {
			return "", err
		}
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_lines (
				line_id, phone_number, created_at, updated_at
			 ) VALUES (?, ?, ?, ?)`,
			lineID,
			number,
			databaseTime(observedAt),
			databaseTime(observedAt),
		); err != nil {
			return "", fmt.Errorf("create stable line: %w", err)
		}
	}
	canonicalPhone := linePhones[lineID]
	if canonicalPhone == "" {
		canonicalPhone = number
	}
	matchedLineIDs := []string{matches.ICCID, matches.IMSI, matches.Phone}
	matchedLineIDs = append(matchedLineIDs, matches.PhoneAliases...)
	aliases := mergeableStableLineAliases(
		lineID,
		canonicalPhone,
		linePhones,
		matchedLineIDs...,
	)
	if err := mergeStableLines(ctx, transaction, lineID, aliases); err != nil {
		return "", err
	}
	if number != "" {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_lines
			 SET phone_number = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE line_id = ?`,
			number,
			lineID,
		); err != nil {
			return "", fmt.Errorf("refresh stable line phone number: %w", err)
		}
	}
	return lineID, nil
}

func stableLinePhones(
	ctx context.Context,
	queryer stableLineQueryer,
	lineIDs ...string,
) (map[string]string, error) {
	result := make(map[string]string)
	for _, lineID := range lineIDs {
		lineID = strings.TrimSpace(lineID)
		if lineID == "" {
			continue
		}
		if _, exists := result[lineID]; exists {
			continue
		}
		var number string
		err := queryer.QueryRowContext(
			ctx,
			`SELECT phone_number FROM modemdeck_lines WHERE line_id = ?`,
			lineID,
		).Scan(&number)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read stable line phone identity: %w", err)
		}
		result[lineID] = storedStableLinePhone(number)
	}
	return result, nil
}

func chooseStableLineCanonical(
	matches stableLineIdentityMatches,
	linePhones map[string]string,
	number string,
) string {
	number = storedStableLinePhone(number)
	if number != "" {
		if matches.Phone != "" {
			return matches.Phone
		}
		if len(matches.PhoneAliases) > 0 {
			return matches.PhoneAliases[0]
		}
		for _, lineID := range []string{matches.ICCID, matches.IMSI} {
			if lineID != "" && linePhones[lineID] == "" {
				return lineID
			}
		}
		return ""
	}
	for _, lineID := range []string{matches.ICCID, matches.IMSI} {
		if lineID != "" && linePhones[lineID] != "" {
			return lineID
		}
	}
	return firstNonEmpty(matches.ICCID, matches.IMSI)
}

func mergeableStableLineAliases(
	canonical,
	canonicalPhone string,
	linePhones map[string]string,
	values ...string,
) []string {
	candidates := uniqueStableLineAliases(canonical, values...)
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidatePhone := linePhones[candidate]
		if candidatePhone == "" ||
			(canonicalPhone != "" && stableLinePhonesMatch(candidatePhone, canonicalPhone)) {
			result = append(result, candidate)
		}
	}
	return result
}

func resolveStableLineIdentityMatches(
	ctx context.Context,
	queryer stableLineQueryer,
	iccid, imsi, number string,
) (stableLineIdentityMatches, error) {
	number = storedStableLinePhone(number)
	var matches stableLineIdentityMatches
	queries := []struct {
		destination *string
		statement   string
		value       string
	}{
		{
			destination: &matches.ICCID,
			statement:   `SELECT line_id FROM sim_cards WHERE iccid = ?`,
			value:       strings.TrimSpace(iccid),
		},
		{
			destination: &matches.IMSI,
			statement:   `SELECT line_id FROM sim_subscriptions WHERE imsi = ?`,
			value:       strings.TrimSpace(imsi),
		},
		{
			destination: &matches.Phone,
			statement:   `SELECT line_id FROM modemdeck_lines WHERE phone_number = ?`,
			value:       number,
		},
	}
	for _, query := range queries {
		if query.value == "" {
			continue
		}
		err := queryer.QueryRowContext(ctx, query.statement, query.value).Scan(query.destination)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return stableLineIdentityMatches{}, fmt.Errorf("resolve stable line identity: %w", err)
		}
		*query.destination = strings.TrimSpace(*query.destination)
	}
	phoneIdentity := stableLinePhoneIdentity(number)
	if phoneIdentity == "" {
		return matches, nil
	}
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT line_id, phone_number
		 FROM modemdeck_lines
		 WHERE phone_number <> ''
		 ORDER BY line_id`,
	)
	if err != nil {
		return stableLineIdentityMatches{}, fmt.Errorf("query stable line phone aliases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID, storedNumber string
		if err := rows.Scan(&lineID, &storedNumber); err != nil {
			return stableLineIdentityMatches{}, fmt.Errorf("scan stable line phone alias: %w", err)
		}
		lineID = strings.TrimSpace(lineID)
		if lineID == "" || lineID == matches.Phone ||
			stableLinePhoneIdentity(storedNumber) != phoneIdentity {
			continue
		}
		matches.PhoneAliases = append(matches.PhoneAliases, lineID)
	}
	if err := rows.Err(); err != nil {
		return stableLineIdentityMatches{}, fmt.Errorf("read stable line phone aliases: %w", err)
	}
	return matches, nil
}

func resolveStableLineIdentity(
	ctx context.Context,
	queryer stableLineQueryer,
	iccid, imsi, number, region string,
) (string, error) {
	number = normalizeStableLinePhone(number, region)
	matches, err := resolveStableLineIdentityMatches(ctx, queryer, iccid, imsi, number)
	if err != nil {
		return "", err
	}
	if lineID := firstNonEmpty(
		matches.Phone,
		firstNonEmpty(matches.PhoneAliases...),
		matches.ICCID,
		matches.IMSI,
	); lineID != "" {
		return lineID, nil
	}
	return "", ErrLineNotFound
}

func uniqueStableLineAliases(canonical string, values ...string) []string {
	seen := map[string]struct{}{strings.TrimSpace(canonical): {}}
	aliases := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		aliases = append(aliases, value)
	}
	sort.Strings(aliases)
	return aliases
}

func mergeStableLines(
	ctx context.Context,
	transaction *sql.Tx,
	canonical string,
	aliases []string,
) error {
	canonical = strings.TrimSpace(canonical)
	if canonical == "" || len(aliases) == 0 {
		return nil
	}
	for _, alias := range aliases {
		if err := mergeStableLineCallPolicy(ctx, transaction, canonical, alias); err != nil {
			return err
		}
		if err := mergeStableLineNetworkPolicy(ctx, transaction, canonical, alias); err != nil {
			return err
		}
		if err := mergeStableLineTelegramScope(ctx, transaction, canonical, alias); err != nil {
			return err
		}
		if err := mergeStableLineMessageThreads(ctx, transaction, canonical, alias); err != nil {
			return err
		}
		if err := mergeStableLineNetworkUsage(ctx, transaction, canonical, alias); err != nil {
			return err
		}
		if err := rebindStableLineReferences(ctx, transaction, canonical, alias); err != nil {
			return err
		}
	}
	if err := mergeStableLineMetadata(ctx, transaction, canonical, aliases); err != nil {
		return err
	}
	for _, alias := range aliases {
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM modemdeck_lines WHERE line_id = ?`,
			alias,
		); err != nil {
			return fmt.Errorf("delete merged stable line %q: %w", alias, err)
		}
	}
	return nil
}

func mergeStableLineCallPolicy(
	ctx context.Context,
	transaction *sql.Tx,
	canonical, alias string,
) error {
	var policy, updatedAt string
	var revision int64
	err := transaction.QueryRowContext(
		ctx,
		`SELECT policy, revision, updated_at
			 FROM modemdeck_line_call_policies
			 WHERE line_id IN (?, ?)
			 ORDER BY
				CASE WHEN revision > 1 THEN 0 ELSE 1 END,
				updated_at DESC,
				revision DESC,
				CASE WHEN line_id = ? THEN 0 ELSE 1 END
			 LIMIT 1`,
		canonical,
		alias,
		canonical,
	).Scan(&policy, &revision, &updatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("select merged line call policy: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM modemdeck_line_call_policies WHERE line_id = ?`,
		alias,
	); err != nil {
		return fmt.Errorf("delete merged line call policy: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_line_call_policies (
			line_id, policy, revision, updated_at
		 ) VALUES (?, ?, ?, ?)
		 ON CONFLICT(line_id) DO UPDATE SET
			policy = excluded.policy,
			revision = excluded.revision,
			updated_at = excluded.updated_at`,
		canonical,
		policy,
		revision,
		updatedAt,
	); err != nil {
		return fmt.Errorf("save merged line call policy: %w", err)
	}
	return nil
}

func mergeStableLineNetworkPolicy(
	ctx context.Context,
	transaction *sql.Tx,
	canonical, alias string,
) error {
	var policy NetworkSelectionPolicyRecord
	var configured int64
	err := transaction.QueryRowContext(
		ctx,
		`SELECT line_id, mode, operator_code, configured, revision, applied_revision,
			applied_boot_epoch, COALESCE(applied_at, ''), last_error, created_at, updated_at
			 FROM modemdeck_network_selection_policies
			 WHERE line_id IN (?, ?)
			 ORDER BY
				configured DESC,
				updated_at DESC,
				revision DESC,
				CASE WHEN line_id = ? THEN 0 ELSE 1 END
			 LIMIT 1`,
		canonical,
		alias,
		canonical,
	).Scan(
		&policy.LineID,
		&policy.Mode,
		&policy.OperatorCode,
		&configured,
		&policy.Revision,
		&policy.AppliedRevision,
		&policy.AppliedBootEpoch,
		&policy.AppliedAt,
		&policy.LastError,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("select merged network selection policy: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM modemdeck_network_selection_policies WHERE line_id = ?`,
		alias,
	); err != nil {
		return fmt.Errorf("delete merged network selection policy: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_network_selection_policies (
			line_id, mode, operator_code, configured, revision, applied_revision,
			applied_boot_epoch, applied_at, last_error, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
		 ON CONFLICT(line_id) DO UPDATE SET
			mode = excluded.mode,
			operator_code = excluded.operator_code,
			configured = excluded.configured,
			revision = excluded.revision,
			applied_revision = excluded.applied_revision,
			applied_boot_epoch = excluded.applied_boot_epoch,
			applied_at = excluded.applied_at,
			last_error = excluded.last_error,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at`,
		canonical,
		policy.Mode,
		policy.OperatorCode,
		configured,
		policy.Revision,
		policy.AppliedRevision,
		policy.AppliedBootEpoch,
		policy.AppliedAt,
		policy.LastError,
		policy.CreatedAt,
		policy.UpdatedAt,
	); err != nil {
		return fmt.Errorf("save merged network selection policy: %w", err)
	}
	return nil
}

func mergeStableLineTelegramScope(
	ctx context.Context,
	transaction *sql.Tx,
	canonical, alias string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO modemdeck_telegram_line_scopes (unit_id, line_id)
		 SELECT unit_id, ? FROM modemdeck_telegram_line_scopes WHERE line_id = ?`,
		canonical,
		alias,
	); err != nil {
		return fmt.Errorf("merge Telegram line scope: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM modemdeck_telegram_line_scopes WHERE line_id = ?`,
		alias,
	); err != nil {
		return fmt.Errorf("delete merged Telegram line scope: %w", err)
	}
	return nil
}

func mergeStableLineMessageThreads(
	ctx context.Context,
	transaction *sql.Tx,
	canonical, alias string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO sms_contacts (
			line_id, imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count, marked_unread, is_favorite, created_at, updated_at
		 )
		 SELECT ?, imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count, marked_unread, is_favorite, created_at, updated_at
		 FROM sms_contacts
		 WHERE line_id = ?
		 ON CONFLICT(line_id, peer) DO UPDATE SET
			imsi = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.imsi ELSE sms_contacts.imsi
			END,
			iccid = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.iccid ELSE sms_contacts.iccid
			END,
			last_sms_id = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_sms_id ELSE sms_contacts.last_sms_id
			END,
			last_timestamp = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_timestamp ELSE sms_contacts.last_timestamp
			END,
			last_content = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_content ELSE sms_contacts.last_content
			END,
			last_type = CASE
				WHEN COALESCE(sms_contacts.last_timestamp, '') <= excluded.last_timestamp
				THEN excluded.last_type ELSE sms_contacts.last_type
			END,
			unread_count = sms_contacts.unread_count + excluded.unread_count,
			marked_unread = MAX(sms_contacts.marked_unread, excluded.marked_unread),
			is_favorite = MAX(sms_contacts.is_favorite, excluded.is_favorite),
			created_at = MIN(sms_contacts.created_at, excluded.created_at),
			updated_at = MAX(sms_contacts.updated_at, excluded.updated_at)`,
		canonical,
		alias,
	); err != nil {
		return fmt.Errorf("merge stable message thread: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM sms_contacts WHERE line_id = ?`,
		alias,
	); err != nil {
		return fmt.Errorf("delete merged stable message thread: %w", err)
	}
	return nil
}

func mergeStableLineNetworkUsage(
	ctx context.Context,
	transaction *sql.Tx,
	canonical, alias string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_network_daily_usage (
			day, scope_kind, scope_id, rx_bytes, tx_bytes, updated_at
		 )
		 SELECT day, scope_kind, ?, rx_bytes, tx_bytes, updated_at
		 FROM modemdeck_network_daily_usage
		 WHERE scope_kind = 'line' AND scope_id = ?
		 ON CONFLICT(day, scope_kind, scope_id) DO UPDATE SET
			rx_bytes = modemdeck_network_daily_usage.rx_bytes + excluded.rx_bytes,
			tx_bytes = modemdeck_network_daily_usage.tx_bytes + excluded.tx_bytes,
			updated_at = MAX(modemdeck_network_daily_usage.updated_at, excluded.updated_at)`,
		canonical,
		alias,
	); err != nil {
		return fmt.Errorf("merge stable line network usage: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM modemdeck_network_daily_usage
		 WHERE scope_kind = 'line' AND scope_id = ?`,
		alias,
	); err != nil {
		return fmt.Errorf("delete merged stable line network usage: %w", err)
	}
	return nil
}

func rebindStableLineReferences(
	ctx context.Context,
	transaction *sql.Tx,
	canonical, alias string,
) error {
	statements := []string{
		`UPDATE contacts SET preferred_line_id = ? WHERE preferred_line_id = ?`,
		`UPDATE sms SET line_id = ? WHERE line_id = ?`,
		`UPDATE call_history SET line_id = ? WHERE line_id = ?`,
		`UPDATE modemdeck_line_settings SET default_line_id = ? WHERE default_line_id = ?`,
		`UPDATE modemdeck_incoming_call_actions SET line_id = ? WHERE line_id = ?`,
		`UPDATE sim_cards SET line_id = ? WHERE line_id = ?`,
		`UPDATE sim_subscriptions SET line_id = ? WHERE line_id = ?`,
		`UPDATE modemdeck_telegram_reply_bindings SET line_id = ? WHERE line_id = ?`,
		`UPDATE modemdeck_proxy_instances SET line_id = ? WHERE line_id = ?`,
		`UPDATE modemdeck_network_counter_checkpoints
		 SET scope_id = ? WHERE scope_kind = 'line' AND scope_id = ?`,
		`UPDATE modemdeck_notification_events SET line_id = ? WHERE line_id = ?`,
		`UPDATE modemdeck_legacy_endpoint_lines SET line_id = ? WHERE line_id = ?`,
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement, canonical, alias); err != nil {
			return fmt.Errorf("rebind merged stable line references: %w", err)
		}
	}
	return nil
}

func mergeStableLineMetadata(
	ctx context.Context,
	transaction *sql.Tx,
	canonical string,
	aliases []string,
) error {
	lineIDs := append([]string{canonical}, aliases...)
	arguments := make([]any, 0, len(lineIDs))
	for _, lineID := range lineIDs {
		arguments = append(arguments, lineID)
	}
	value := func(column string) (string, error) {
		queryArguments := append([]any{}, arguments...)
		queryArguments = append(queryArguments, canonical)
		var result string
		err := transaction.QueryRowContext(
			ctx,
			`SELECT `+column+`
			 FROM modemdeck_lines
			 WHERE line_id IN (`+placeholders(len(lineIDs))+`)
				AND `+column+` <> ''
			 ORDER BY CASE WHEN line_id = ? THEN 0 ELSE 1 END,
				updated_at DESC, line_id
			 LIMIT 1`,
			queryArguments...,
		).Scan(&result)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("select merged line %s: %w", column, err)
		}
		return result, nil
	}
	phoneNumber, err := value("phone_number")
	if err != nil {
		return err
	}
	homeCountryISO, err := value("home_country_iso")
	if err != nil {
		return err
	}
	lineLabel, err := value("line_label")
	if err != nil {
		return err
	}
	lineColor, err := value("line_color")
	if err != nil {
		return err
	}
	var createdAt string
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT MIN(created_at) FROM modemdeck_lines
		 WHERE line_id IN (`+placeholders(len(lineIDs))+`)`,
		arguments...,
	).Scan(&createdAt); err != nil {
		return fmt.Errorf("select merged line creation time: %w", err)
	}
	aliasArguments := make([]any, 0, len(aliases))
	for _, alias := range aliases {
		aliasArguments = append(aliasArguments, alias)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_lines SET phone_number = ''
		 WHERE line_id IN (`+placeholders(len(aliases))+`)`,
		aliasArguments...,
	); err != nil {
		return fmt.Errorf("release merged line phone number: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_lines
		 SET phone_number = ?, home_country_iso = ?, line_label = ?, line_color = ?,
			created_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ?`,
		phoneNumber,
		homeCountryISO,
		lineLabel,
		lineColor,
		createdAt,
		canonical,
	); err != nil {
		return fmt.Errorf("update merged line metadata: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func resolveLegacyEndpointLines(
	ctx context.Context,
	transaction *sql.Tx,
	endpointID, imei string,
) ([]string, error) {
	endpointID = strings.TrimSpace(endpointID)
	imei = strings.TrimSpace(imei)
	result := make([]string, 0, 2)
	if strings.HasPrefix(endpointID, "line_") && len(endpointID) > len("line_") {
		var lineID string
		err := transaction.QueryRowContext(
			ctx,
			`SELECT line_id
			 FROM modemdeck_lines
			 WHERE line_id = ? AND phone_number = ''`,
			endpointID,
		).Scan(&lineID)
		if err == nil {
			result = append(result, strings.TrimSpace(lineID))
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("resolve provisional stable endpoint line: %w", err)
		}
	}
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT mapping.line_id
		 FROM modemdeck_legacy_endpoint_lines mapping
		 JOIN modemdeck_lines lines ON lines.line_id = mapping.line_id
		 WHERE mapping.endpoint_id IN (?, ?) AND lines.phone_number = ''
		 ORDER BY mapping.endpoint_id`,
		endpointID,
		imei,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve legacy stable endpoint line: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		if err := rows.Scan(&lineID); err != nil {
			return nil, fmt.Errorf("scan legacy stable endpoint line: %w", err)
		}
		result = append(result, strings.TrimSpace(lineID))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy stable endpoint lines: %w", err)
	}
	return uniqueStableLineAliases("", result...), nil
}

func resolveStoredHardwareLine(
	ctx context.Context,
	transaction *sql.Tx,
	lineID, endpointLineID, iccid, imsi, number, region string,
) (string, string, error) {
	lineID = strings.TrimSpace(lineID)
	endpointLineID = strings.TrimSpace(endpointLineID)
	if lineID != "" {
		var exists int
		err := transaction.QueryRowContext(
			ctx,
			`SELECT 1 FROM modemdeck_lines WHERE line_id = ?`,
			lineID,
		).Scan(&exists)
		if err == nil {
			return lineID, endpointLineID, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("validate stable hardware line: %w", err)
		}
		if endpointLineID == "" {
			endpointLineID = lineID
		}
	}
	resolved, err := resolveStableLineIdentity(ctx, transaction, iccid, imsi, number, region)
	if err == nil {
		return resolved, endpointLineID, nil
	}
	if !errors.Is(err, ErrLineNotFound) {
		return "", "", err
	}
	if endpointLineID != "" {
		err := transaction.QueryRowContext(
			ctx,
			`SELECT sim_cards.line_id
			 FROM devices
			 JOIN sim_cards ON sim_cards.iccid = devices.iccid
			 WHERE devices.endpoint_id = ?
			 ORDER BY devices.last_seen DESC, devices.imei
			 LIMIT 1`,
			endpointLineID,
		).Scan(&resolved)
		if err == nil {
			return resolved, endpointLineID, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("resolve hardware endpoint line: %w", err)
		}
	}
	return "", "", ErrLineNotFound
}

func (s *Store) ResolveLineEndpoint(ctx context.Context, lineID string) (string, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return "", ErrLineNotFound
	}
	var endpointID string
	err := s.database.QueryRowContext(
		ctx,
		`SELECT devices.endpoint_id
		 FROM sim_cards
		 JOIN devices ON devices.imei = sim_cards.current_imei
		 WHERE sim_cards.line_id = ? AND devices.endpoint_id <> ''
		 ORDER BY sim_cards.last_seen DESC, devices.last_seen DESC, devices.imei
		 LIMIT 1`,
		lineID,
	).Scan(&endpointID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrLineNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve line endpoint: %w", err)
	}
	endpointID = strings.TrimSpace(endpointID)
	if endpointID == "" {
		return "", ErrLineNotFound
	}
	return endpointID, nil
}

func normalizeStableLinePhone(value, region string) string {
	return phone.NetworkSubscriberE164(strings.TrimSpace(value), region)
}

func storedStableLinePhone(value string) string {
	return phone.NetworkSubscriberE164(strings.TrimSpace(value), "")
}

func stableLinePhonesMatch(left, right string) bool {
	leftIdentity := stableLinePhoneIdentity(left)
	return leftIdentity != "" && leftIdentity == stableLinePhoneIdentity(right)
}

func stableLinePhoneIdentity(value string) string {
	return storedStableLinePhone(value)
}

func newStoreLineID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate stable line ID: %w", err)
	}
	return "line_" + hex.EncodeToString(random[:]), nil
}
