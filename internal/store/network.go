package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
)

var (
	ErrProxyInstanceNotFound         = errors.New("proxy instance not found")
	ErrProxyInstanceRevisionConflict = errors.New("proxy instance revision conflict")
)

func (s *Store) ProxyInstances(ctx context.Context) ([]ProxyInstanceRecord, error) {
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT id, name, line_id, enabled, mode, listen_address, listen_port,
		        auth_enabled, username, password_nonce, password_ciphertext,
		        revision, applied_revision, desired_deleted, created_at, updated_at
		 FROM modemdeck_proxy_instances
		 ORDER BY lower(name), id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list proxy instances: %w", err)
	}
	defer rows.Close()

	instances := make([]ProxyInstanceRecord, 0)
	for rows.Next() {
		instance, err := scanProxyInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("scan proxy instance: %w", err)
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read proxy instances: %w", err)
	}
	return instances, nil
}

func (s *Store) ProxyInstance(ctx context.Context, id string) (ProxyInstanceRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ProxyInstanceRecord{}, ErrProxyInstanceNotFound
	}
	instance, err := scanProxyInstance(s.database.QueryRowContext(
		ctx,
		`SELECT id, name, line_id, enabled, mode, listen_address, listen_port,
		        auth_enabled, username, password_nonce, password_ciphertext,
		        revision, applied_revision, desired_deleted, created_at, updated_at
		 FROM modemdeck_proxy_instances
		 WHERE id = ?`,
		id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ProxyInstanceRecord{}, ErrProxyInstanceNotFound
	}
	if err != nil {
		return ProxyInstanceRecord{}, fmt.Errorf("load proxy instance: %w", err)
	}
	return instance, nil
}

func (s *Store) CreateProxyInstance(
	ctx context.Context,
	instance ProxyInstanceRecord,
) (ProxyInstanceRecord, error) {
	if strings.TrimSpace(instance.ID) == "" {
		return ProxyInstanceRecord{}, errors.New("create proxy instance: ID is required")
	}
	_, err := s.database.ExecContext(
		ctx,
		`INSERT INTO modemdeck_proxy_instances (
				id, name, line_id, enabled, mode, listen_address, listen_port,
				auth_enabled, username, password_nonce, password_ciphertext, revision,
				applied_revision, desired_deleted, created_at, updated_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0, 0,
			           CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		instance.ID,
		instance.Name,
		instance.LineID,
		instance.Enabled,
		instance.Mode,
		instance.ListenAddress,
		instance.ListenPort,
		instance.AuthEnabled,
		instance.Username,
		nonNilBlob(instance.PasswordNonce),
		nonNilBlob(instance.PasswordCiphertext),
	)
	if err != nil {
		return ProxyInstanceRecord{}, fmt.Errorf("create proxy instance: %w", err)
	}
	return s.ProxyInstance(ctx, instance.ID)
}

func (s *Store) UpdateProxyInstance(
	ctx context.Context,
	instance ProxyInstanceRecord,
	expectedRevision int64,
) (ProxyInstanceRecord, error) {
	id := strings.TrimSpace(instance.ID)
	if id == "" || expectedRevision <= 0 {
		return ProxyInstanceRecord{}, errors.New(
			"update proxy instance: ID and positive revision are required",
		)
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_proxy_instances
		 SET name = ?,
		     line_id = ?,
		     enabled = ?,
		     mode = ?,
		     listen_address = ?,
		     listen_port = ?,
		     auth_enabled = ?,
		     username = ?,
		     password_nonce = ?,
		     password_ciphertext = ?,
		     revision = revision + 1,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND revision = ?`,
		instance.Name,
		instance.LineID,
		instance.Enabled,
		instance.Mode,
		instance.ListenAddress,
		instance.ListenPort,
		instance.AuthEnabled,
		instance.Username,
		nonNilBlob(instance.PasswordNonce),
		nonNilBlob(instance.PasswordCiphertext),
		id,
		expectedRevision,
	)
	if err != nil {
		return ProxyInstanceRecord{}, fmt.Errorf("update proxy instance: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ProxyInstanceRecord{}, fmt.Errorf("inspect proxy instance update: %w", err)
	}
	if affected == 0 {
		if _, err := s.ProxyInstance(ctx, id); errors.Is(err, ErrProxyInstanceNotFound) {
			return ProxyInstanceRecord{}, ErrProxyInstanceNotFound
		} else if err != nil {
			return ProxyInstanceRecord{}, err
		}
		return ProxyInstanceRecord{}, ErrProxyInstanceRevisionConflict
	}
	return s.ProxyInstance(ctx, id)
}

func (s *Store) DeleteProxyInstance(ctx context.Context, id string, expectedRevision int64) error {
	id = strings.TrimSpace(id)
	if id == "" || expectedRevision <= 0 {
		return errors.New("delete proxy instance: ID and positive revision are required")
	}
	result, err := s.database.ExecContext(
		ctx,
		`UPDATE modemdeck_proxy_instances
		 SET desired_deleted = 1,
		     revision = revision + 1,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND revision = ? AND desired_deleted = 0`,
		id,
		expectedRevision,
	)
	if err != nil {
		return fmt.Errorf("delete proxy instance: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect proxy instance deletion: %w", err)
	}
	if affected != 0 {
		return nil
	}
	if _, err := s.ProxyInstance(ctx, id); errors.Is(err, ErrProxyInstanceNotFound) {
		return ErrProxyInstanceNotFound
	} else if err != nil {
		return err
	}
	return ErrProxyInstanceRevisionConflict
}

func (s *Store) FinalizeProxyApply(
	ctx context.Context,
	tokens []ProxyApplyToken,
) (bool, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return true, fmt.Errorf("begin proxy apply finalization: %w", err)
	}
	defer transaction.Rollback()

	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		id := strings.TrimSpace(token.ID)
		if id == "" || token.Revision <= 0 {
			return true, errors.New("finalize proxy apply: token is invalid")
		}
		if _, exists := seen[id]; exists {
			return true, errors.New("finalize proxy apply: duplicate proxy ID")
		}
		seen[id] = struct{}{}
		if token.DesiredDeleted {
			if _, err := transaction.ExecContext(
				ctx,
				`DELETE FROM modemdeck_proxy_instances
				 WHERE id = ? AND revision = ? AND desired_deleted = 1`,
				id,
				token.Revision,
			); err != nil {
				return true, fmt.Errorf("finalize proxy deletion: %w", err)
			}
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE modemdeck_proxy_instances
			 SET applied_revision = revision,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND revision = ? AND desired_deleted = 0`,
			id,
			token.Revision,
		); err != nil {
			return true, fmt.Errorf("finalize proxy revision: %w", err)
		}
	}

	var pendingValue int64
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM modemdeck_proxy_instances
			WHERE desired_deleted = 1 OR applied_revision <> revision
		)`,
	).Scan(&pendingValue); err != nil {
		return true, fmt.Errorf("inspect pending proxy state: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return true, fmt.Errorf("commit proxy apply finalization: %w", err)
	}
	return pendingValue != 0, nil
}

func (s *Store) ApplyNetworkCounterSamples(
	ctx context.Context,
	samples []NetworkCounterSample,
) error {
	return s.applyNetworkCounterSamples(ctx, samples, nil)
}

func (s *Store) ApplyNetworkCounterSnapshot(
	ctx context.Context,
	samples []NetworkCounterSample,
	activeLineIDs []string,
) error {
	active := make(map[string]struct{}, len(activeLineIDs))
	for _, lineID := range activeLineIDs {
		lineID = strings.TrimSpace(lineID)
		if lineID == "" {
			return errors.New("apply network counters: active line ID is invalid")
		}
		if _, exists := active[lineID]; exists {
			return errors.New("apply network counters: duplicate active line ID")
		}
		active[lineID] = struct{}{}
	}
	if err := validateActiveLineSamples(samples, active); err != nil {
		return err
	}
	return s.applyNetworkCounterSamples(ctx, samples, active)
}

func (s *Store) applyNetworkCounterSamples(
	ctx context.Context,
	samples []NetworkCounterSample,
	activeLineIDs map[string]struct{},
) error {
	if len(samples) == 0 {
		if activeLineIDs == nil {
			return nil
		}
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin network counter update: %w", err)
	}
	defer transaction.Rollback()

	if activeLineIDs != nil {
		if err := resetInactiveLineCheckpoints(ctx, transaction, activeLineIDs); err != nil {
			return err
		}
	}

	seen := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		if err := validateNetworkCounterSample(sample); err != nil {
			return err
		}
		endpointScopeID := networkCounterEndpointScopeID(sample)
		key := string(sample.ScopeKind) + "\x00" + endpointScopeID
		if _, exists := seen[key]; exists {
			return errors.New("apply network counters: duplicate endpoint scope")
		}
		seen[key] = struct{}{}

		var (
			previousEpoch      string
			previousRX         int64
			previousTX         int64
			previousObservedAt sql.NullString
		)
		err := transaction.QueryRowContext(
			ctx,
			`SELECT epoch, rx_bytes, tx_bytes, observed_at
			 FROM modemdeck_network_counter_checkpoints
			 WHERE scope_kind = ? AND endpoint_scope_id = ?`,
			sample.ScopeKind,
			endpointScopeID,
		).Scan(&previousEpoch, &previousRX, &previousTX, &previousObservedAt)

		previousTime, previousTimeKnown := parseDatabaseTime(stringValue(previousObservedAt))
		var deltaRX, deltaTX uint64
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("load network counter checkpoint: %w", err)
		case previousTimeKnown && !sample.ObservedAt.UTC().After(previousTime.UTC()):
			continue
		case previousTimeKnown && previousEpoch == sample.Epoch:
			if sample.RXBytes >= uint64(previousRX) {
				deltaRX = sample.RXBytes - uint64(previousRX)
			}
			if sample.TXBytes >= uint64(previousTX) {
				deltaTX = sample.TXBytes - uint64(previousTX)
			}
		}

		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_network_counter_checkpoints (
				scope_kind, scope_id, endpoint_scope_id, epoch, rx_bytes, tx_bytes, observed_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(scope_kind, endpoint_scope_id) DO UPDATE SET
				scope_id = excluded.scope_id,
				epoch = excluded.epoch,
				rx_bytes = excluded.rx_bytes,
				tx_bytes = excluded.tx_bytes,
				observed_at = excluded.observed_at`,
			sample.ScopeKind,
			sample.ScopeID,
			endpointScopeID,
			sample.Epoch,
			int64(sample.RXBytes),
			int64(sample.TXBytes),
			sample.ObservedAt.UTC(),
		); err != nil {
			return fmt.Errorf("save network counter checkpoint: %w", err)
		}
		if deltaRX == 0 && deltaTX == 0 {
			continue
		}
		for _, delta := range splitNetworkUsageDelta(
			previousTime,
			sample.ObservedAt,
			deltaRX,
			deltaTX,
			sample.Location,
		) {
			if _, err := transaction.ExecContext(
				ctx,
				`INSERT INTO modemdeck_network_daily_usage (
						day, scope_kind, scope_id, rx_bytes, tx_bytes, updated_at
					 ) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
					 ON CONFLICT(day, scope_kind, scope_id) DO UPDATE SET
						rx_bytes = rx_bytes + excluded.rx_bytes,
						tx_bytes = tx_bytes + excluded.tx_bytes,
						updated_at = CURRENT_TIMESTAMP`,
				delta.Day,
				sample.ScopeKind,
				sample.ScopeID,
				int64(delta.RXBytes),
				int64(delta.TXBytes),
			); err != nil {
				return fmt.Errorf("aggregate network usage: %w", err)
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit network counter update: %w", err)
	}
	return nil
}

func (s *Store) NetworkUsage(
	ctx context.Context,
	firstDay, lastDay string,
) ([]NetworkUsage, error) {
	firstDay = strings.TrimSpace(firstDay)
	lastDay = strings.TrimSpace(lastDay)
	if _, err := time.Parse(time.DateOnly, firstDay); err != nil {
		return nil, errors.New("load network usage: first day is invalid")
	}
	if _, err := time.Parse(time.DateOnly, lastDay); err != nil || firstDay > lastDay {
		return nil, errors.New("load network usage: last day is invalid")
	}
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT scope_kind, scope_id, SUM(rx_bytes), SUM(tx_bytes)
		 FROM modemdeck_network_daily_usage
		 WHERE day BETWEEN ? AND ?
		 GROUP BY scope_kind, scope_id
		 ORDER BY scope_kind, scope_id`,
		firstDay,
		lastDay,
	)
	if err != nil {
		return nil, fmt.Errorf("load network usage: %w", err)
	}
	defer rows.Close()

	usage := make([]NetworkUsage, 0)
	for rows.Next() {
		var (
			item    NetworkUsage
			rxBytes int64
			txBytes int64
		)
		if err := rows.Scan(&item.ScopeKind, &item.ScopeID, &rxBytes, &txBytes); err != nil {
			return nil, fmt.Errorf("scan network usage: %w", err)
		}
		item.RXBytes = uint64(rxBytes)
		item.TXBytes = uint64(txBytes)
		usage = append(usage, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read network usage: %w", err)
	}
	return usage, nil
}

type proxyInstanceScanner interface {
	Scan(...any) error
}

func scanProxyInstance(scanner proxyInstanceScanner) (ProxyInstanceRecord, error) {
	var (
		instance                            ProxyInstanceRecord
		enabled, authEnabled, desiredDelete sql.NullInt64
		listenPort, revision, appliedRev    sql.NullInt64
		createdAt, updatedAt                sql.NullString
		passwordNonce, passwordCipher       []byte
	)
	if err := scanner.Scan(
		&instance.ID,
		&instance.Name,
		&instance.LineID,
		&enabled,
		&instance.Mode,
		&instance.ListenAddress,
		&listenPort,
		&authEnabled,
		&instance.Username,
		&passwordNonce,
		&passwordCipher,
		&revision,
		&appliedRev,
		&desiredDelete,
		&createdAt,
		&updatedAt,
	); err != nil {
		return ProxyInstanceRecord{}, err
	}
	instance.Enabled = boolValue(enabled)
	instance.ListenPort = uint16(intValue(listenPort))
	instance.AuthEnabled = boolValue(authEnabled)
	instance.PasswordNonce = append([]byte(nil), passwordNonce...)
	instance.PasswordCiphertext = append([]byte(nil), passwordCipher...)
	instance.Revision = intValue(revision)
	instance.AppliedRevision = intValue(appliedRev)
	instance.DesiredDeleted = boolValue(desiredDelete)
	instance.CreatedAt = stringValue(createdAt)
	instance.UpdatedAt = stringValue(updatedAt)
	return instance, nil
}

func resetInactiveLineCheckpoints(
	ctx context.Context,
	transaction *sql.Tx,
	activeLineIDs map[string]struct{},
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT scope_id, endpoint_scope_id
		 FROM modemdeck_network_counter_checkpoints
		 WHERE scope_kind = ?`,
		NetworkScopeLine,
	)
	if err != nil {
		return fmt.Errorf("list line counter checkpoints: %w", err)
	}
	var inactive []string
	for rows.Next() {
		var lineID, endpointScopeID string
		if err := rows.Scan(&lineID, &endpointScopeID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan line counter checkpoint: %w", err)
		}
		if _, active := activeLineIDs[lineID]; !active {
			inactive = append(inactive, endpointScopeID)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read line counter checkpoints: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close line counter checkpoints: %w", err)
	}
	for _, endpointScopeID := range inactive {
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM modemdeck_network_counter_checkpoints
			 WHERE scope_kind = ? AND endpoint_scope_id = ?`,
			NetworkScopeLine,
			endpointScopeID,
		); err != nil {
			return fmt.Errorf("reset inactive line counter checkpoint: %w", err)
		}
	}
	return nil
}

type networkUsageDelta struct {
	Day     string
	RXBytes uint64
	TXBytes uint64
}

func splitNetworkUsageDelta(
	previousAt, observedAt time.Time,
	rxBytes, txBytes uint64,
	location *time.Location,
) []networkUsageDelta {
	if !observedAt.After(previousAt) || (rxBytes == 0 && txBytes == 0) {
		return nil
	}
	if location == nil {
		location = time.Local
	}
	previousAt = previousAt.In(location)
	observedAt = observedAt.In(location)
	totalDuration := observedAt.Sub(previousAt)
	if totalDuration <= 0 {
		return nil
	}

	var (
		result      []networkUsageDelta
		cursor      = previousAt
		allocatedRX uint64
		allocatedTX uint64
	)
	for cursor.Before(observedAt) {
		nextDay := time.Date(
			cursor.Year(),
			cursor.Month(),
			cursor.Day()+1,
			0,
			0,
			0,
			0,
			location,
		)
		segmentEnd := nextDay
		if observedAt.Before(segmentEnd) {
			segmentEnd = observedAt
		}
		segmentDuration := segmentEnd.Sub(cursor)
		last := segmentEnd.Equal(observedAt)
		segmentRX := rxBytes - allocatedRX
		segmentTX := txBytes - allocatedTX
		if !last {
			segmentRX = proportionalUint64(rxBytes, segmentDuration, totalDuration)
			segmentTX = proportionalUint64(txBytes, segmentDuration, totalDuration)
			allocatedRX += segmentRX
			allocatedTX += segmentTX
		}
		result = append(result, networkUsageDelta{
			Day:     cursor.Format(time.DateOnly),
			RXBytes: segmentRX,
			TXBytes: segmentTX,
		})
		cursor = segmentEnd
	}
	return result
}

func proportionalUint64(value uint64, numerator, denominator time.Duration) uint64 {
	if value == 0 || numerator <= 0 || denominator <= 0 {
		return 0
	}
	product := new(big.Int).SetUint64(value)
	product.Mul(product, big.NewInt(int64(numerator)))
	product.Quo(product, big.NewInt(int64(denominator)))
	return product.Uint64()
}

func validateNetworkCounterSample(sample NetworkCounterSample) error {
	if sample.ScopeKind != NetworkScopeLine && sample.ScopeKind != NetworkScopeProxy {
		return errors.New("apply network counters: scope kind is invalid")
	}
	if strings.TrimSpace(sample.ScopeID) == "" ||
		networkCounterEndpointScopeID(sample) == "" ||
		strings.TrimSpace(sample.Epoch) == "" ||
		sample.ObservedAt.IsZero() {
		return errors.New("apply network counters: sample is incomplete")
	}
	if sample.RXBytes > math.MaxInt64 || sample.TXBytes > math.MaxInt64 {
		return errors.New("apply network counters: counter exceeds SQLite integer range")
	}
	return nil
}

func networkCounterEndpointScopeID(sample NetworkCounterSample) string {
	endpointScopeID := strings.TrimSpace(sample.EndpointScopeID)
	if endpointScopeID == "" && sample.ScopeKind == NetworkScopeProxy {
		return strings.TrimSpace(sample.ScopeID)
	}
	return endpointScopeID
}

func validateActiveLineSamples(
	samples []NetworkCounterSample,
	activeLineIDs map[string]struct{},
) error {
	sampled := make(map[string]struct{}, len(activeLineIDs))
	for _, sample := range samples {
		if sample.ScopeKind != NetworkScopeLine {
			continue
		}
		lineID := strings.TrimSpace(sample.ScopeID)
		if _, active := activeLineIDs[lineID]; !active {
			return errors.New("apply network counters: line sample is not active")
		}
		if _, exists := sampled[lineID]; exists {
			return errors.New("apply network counters: duplicate active line sample")
		}
		sampled[lineID] = struct{}{}
	}
	if len(sampled) != len(activeLineIDs) {
		return errors.New("apply network counters: active line sample is missing")
	}
	return nil
}

func nonNilBlob(value []byte) []byte {
	if value == nil {
		return []byte{}
	}
	return value
}
