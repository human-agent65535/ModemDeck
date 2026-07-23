package database

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type tableSchema struct {
	name       string
	create     string
	identity   []string
	addColumns map[string]string
}

var compatibleTables = []tableSchema{
	{
		name: "modemdeck_admin_credentials",
		create: `CREATE TABLE IF NOT EXISTS modemdeck_admin_credentials (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			password_hash TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		identity: []string{"singleton"},
		addColumns: map[string]string{
			"password_hash": "TEXT NOT NULL DEFAULT ''",
			"updated_at":    "DATETIME",
		},
	},
	{
		name: "modemdeck_auth_sessions",
		create: `CREATE TABLE IF NOT EXISTS modemdeck_auth_sessions (
			session_token_digest BLOB PRIMARY KEY CHECK (length(session_token_digest) = 32),
			csrf_token_digest BLOB NOT NULL CHECK (length(csrf_token_digest) = 32),
			created_at_unix INTEGER NOT NULL,
			expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix >= created_at_unix)
		)`,
		identity: []string{"session_token_digest"},
		addColumns: map[string]string{
			"csrf_token_digest": "BLOB",
			"created_at_unix":   "INTEGER NOT NULL DEFAULT 0",
			"expires_at_unix":   "INTEGER NOT NULL DEFAULT 0",
		},
	},
	{
		name: "contacts",
		create: `CREATE TABLE IF NOT EXISTS contacts (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		identity: []string{"id"},
		addColumns: map[string]string{
			"display_name": "TEXT NOT NULL DEFAULT ''",
			"notes":        "TEXT NOT NULL DEFAULT ''",
			"revision":     "INTEGER NOT NULL DEFAULT 1",
			"created_at":   "DATETIME",
			"updated_at":   "DATETIME",
		},
	},
	{
		name: "contact_phones",
		create: `CREATE TABLE IF NOT EXISTS contact_phones (
			id TEXT PRIMARY KEY,
			contact_id TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			original_number TEXT NOT NULL DEFAULT '',
			canonical_e164 TEXT NOT NULL DEFAULT '',
			is_primary NUMERIC NOT NULL DEFAULT 0,
			FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE ON UPDATE CASCADE
		)`,
		identity: []string{"id", "contact_id"},
		addColumns: map[string]string{
			"label":           "TEXT NOT NULL DEFAULT ''",
			"original_number": "TEXT NOT NULL DEFAULT ''",
			"canonical_e164":  "TEXT NOT NULL DEFAULT ''",
			"is_primary":      "NUMERIC NOT NULL DEFAULT 0",
		},
	},
	{
		name: "sms",
		create: `CREATE TABLE IF NOT EXISTS sms (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			imsi TEXT NOT NULL DEFAULT '',
			iccid TEXT NOT NULL DEFAULT '',
			peer TEXT NOT NULL DEFAULT '',
			local_phone TEXT NOT NULL DEFAULT '',
			sender TEXT NOT NULL DEFAULT '',
			recipient TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			type INTEGER NOT NULL DEFAULT 0,
			status INTEGER NOT NULL DEFAULT 0,
			timestamp DATETIME,
			created_at DATETIME
		)`,
		identity: []string{"id"},
		addColumns: map[string]string{
			"imsi":        "TEXT NOT NULL DEFAULT ''",
			"iccid":       "TEXT NOT NULL DEFAULT ''",
			"peer":        "TEXT NOT NULL DEFAULT ''",
			"local_phone": "TEXT NOT NULL DEFAULT ''",
			"sender":      "TEXT NOT NULL DEFAULT ''",
			"recipient":   "TEXT NOT NULL DEFAULT ''",
			"content":     "TEXT NOT NULL DEFAULT ''",
			"type":        "INTEGER NOT NULL DEFAULT 0",
			"status":      "INTEGER NOT NULL DEFAULT 0",
			"timestamp":   "DATETIME",
			"created_at":  "DATETIME",
		},
	},
	{
		name: "sms_contacts",
		create: `CREATE TABLE IF NOT EXISTS sms_contacts (
			imsi TEXT NOT NULL,
			iccid TEXT NOT NULL DEFAULT '',
			peer TEXT NOT NULL,
			last_sms_id INTEGER NOT NULL DEFAULT 0,
			last_timestamp DATETIME,
			last_content TEXT NOT NULL DEFAULT '',
			last_type INTEGER NOT NULL DEFAULT 0,
			unread_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			PRIMARY KEY (imsi, peer)
		)`,
		identity: []string{"imsi", "peer"},
		addColumns: map[string]string{
			"iccid":          "TEXT NOT NULL DEFAULT ''",
			"last_sms_id":    "INTEGER NOT NULL DEFAULT 0",
			"last_timestamp": "DATETIME",
			"last_content":   "TEXT NOT NULL DEFAULT ''",
			"last_type":      "INTEGER NOT NULL DEFAULT 0",
			"unread_count":   "INTEGER NOT NULL DEFAULT 0",
			"created_at":     "DATETIME",
			"updated_at":     "DATETIME",
		},
	},
	{
		name: "call_history",
		create: `CREATE TABLE IF NOT EXISTS call_history (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL DEFAULT '',
			device_id TEXT NOT NULL DEFAULT '',
			direction TEXT NOT NULL DEFAULT '',
			remote_number TEXT NOT NULL DEFAULT '',
			endpoint_id TEXT NOT NULL DEFAULT '',
			endpoint_call_id TEXT NOT NULL DEFAULT '',
			phase TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME,
			active_at DATETIME,
			ended_at DATETIME,
			end_reason TEXT NOT NULL DEFAULT '',
			failure_code TEXT NOT NULL DEFAULT ''
		)`,
		identity: []string{"id"},
		addColumns: map[string]string{
			"request_id":       "TEXT NOT NULL DEFAULT ''",
			"device_id":        "TEXT NOT NULL DEFAULT ''",
			"direction":        "TEXT NOT NULL DEFAULT ''",
			"remote_number":    "TEXT NOT NULL DEFAULT ''",
			"endpoint_id":      "TEXT NOT NULL DEFAULT ''",
			"endpoint_call_id": "TEXT NOT NULL DEFAULT ''",
			"phase":            "TEXT NOT NULL DEFAULT ''",
			"revision":         "INTEGER NOT NULL DEFAULT 1",
			"created_at":       "DATETIME",
			"updated_at":       "DATETIME",
			"active_at":        "DATETIME",
			"ended_at":         "DATETIME",
			"end_reason":       "TEXT NOT NULL DEFAULT ''",
			"failure_code":     "TEXT NOT NULL DEFAULT ''",
		},
	},
	{
		name: "devices",
		create: `CREATE TABLE IF NOT EXISTS devices (
			imei TEXT PRIMARY KEY,
			alias TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			firmware TEXT NOT NULL DEFAULT '',
			port TEXT NOT NULL DEFAULT '',
			public_ip TEXT NOT NULL DEFAULT '',
			private_ip TEXT NOT NULL DEFAULT '',
			public_ipv6 TEXT NOT NULL DEFAULT '',
			private_ipv6 TEXT NOT NULL DEFAULT '',
			iccid TEXT,
			sim_inserted NUMERIC NOT NULL DEFAULT 0,
			signal_db_m INTEGER NOT NULL DEFAULT 0,
			signal_rsrq INTEGER NOT NULL DEFAULT 0,
			signal_rsrp INTEGER NOT NULL DEFAULT 0,
			last_seen DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		identity: []string{"imei"},
		addColumns: map[string]string{
			"alias":        "TEXT NOT NULL DEFAULT ''",
			"model":        "TEXT NOT NULL DEFAULT ''",
			"firmware":     "TEXT NOT NULL DEFAULT ''",
			"port":         "TEXT NOT NULL DEFAULT ''",
			"public_ip":    "TEXT NOT NULL DEFAULT ''",
			"private_ip":   "TEXT NOT NULL DEFAULT ''",
			"public_ipv6":  "TEXT NOT NULL DEFAULT ''",
			"private_ipv6": "TEXT NOT NULL DEFAULT ''",
			"iccid":        "TEXT",
			"sim_inserted": "NUMERIC NOT NULL DEFAULT 0",
			"signal_db_m":  "INTEGER NOT NULL DEFAULT 0",
			"signal_rsrq":  "INTEGER NOT NULL DEFAULT 0",
			"signal_rsrp":  "INTEGER NOT NULL DEFAULT 0",
			"last_seen":    "DATETIME",
			"created_at":   "DATETIME",
			"updated_at":   "DATETIME",
		},
	},
	{
		name: "sim_cards",
		create: `CREATE TABLE IF NOT EXISTS sim_cards (
			iccid TEXT PRIMARY KEY,
			imsi TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			current_imei TEXT,
			reg_status INTEGER NOT NULL DEFAULT 0,
			reg_status_text TEXT NOT NULL DEFAULT '',
			lac TEXT NOT NULL DEFAULT '',
			cell_id TEXT NOT NULL DEFAULT '',
			apn TEXT NOT NULL DEFAULT '',
			ims_status INTEGER NOT NULL DEFAULT 0,
			last_seen DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		identity: []string{"iccid"},
		addColumns: map[string]string{
			"imsi":            "TEXT NOT NULL DEFAULT ''",
			"operator":        "TEXT NOT NULL DEFAULT ''",
			"current_imei":    "TEXT",
			"reg_status":      "INTEGER NOT NULL DEFAULT 0",
			"reg_status_text": "TEXT NOT NULL DEFAULT ''",
			"lac":             "TEXT NOT NULL DEFAULT ''",
			"cell_id":         "TEXT NOT NULL DEFAULT ''",
			"apn":             "TEXT NOT NULL DEFAULT ''",
			"ims_status":      "INTEGER NOT NULL DEFAULT 0",
			"last_seen":       "DATETIME",
			"created_at":      "DATETIME",
			"updated_at":      "DATETIME",
		},
	},
	{
		name: "sim_subscriptions",
		create: `CREATE TABLE IF NOT EXISTS sim_subscriptions (
			imsi TEXT PRIMARY KEY,
			current_iccid TEXT NOT NULL DEFAULT '',
			phone_number TEXT NOT NULL DEFAULT '',
			modem_phone_number TEXT NOT NULL DEFAULT '',
			vowifi_phone_number TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			last_seen DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		identity: []string{"imsi"},
		addColumns: map[string]string{
			"current_iccid":       "TEXT NOT NULL DEFAULT ''",
			"phone_number":        "TEXT NOT NULL DEFAULT ''",
			"modem_phone_number":  "TEXT NOT NULL DEFAULT ''",
			"vowifi_phone_number": "TEXT NOT NULL DEFAULT ''",
			"operator":            "TEXT NOT NULL DEFAULT ''",
			"last_seen":           "DATETIME",
			"created_at":          "DATETIME",
			"updated_at":          "DATETIME",
		},
	},
}

var compatibleIndexes = []string{
	"CREATE INDEX IF NOT EXISTS idx_modemdeck_auth_sessions_expiry ON modemdeck_auth_sessions(expires_at_unix)",
	"CREATE INDEX IF NOT EXISTS idx_contacts_display_name ON contacts(display_name)",
	"CREATE INDEX IF NOT EXISTS idx_contact_phones_contact_id ON contact_phones(contact_id)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_contact_phones_canonical_e164 ON contact_phones(canonical_e164)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_contact_phones_primary_per_contact ON contact_phones(contact_id) WHERE is_primary = 1",
	"CREATE INDEX IF NOT EXISTS idx_sms_iccid_timestamp ON sms(iccid, timestamp DESC)",
	"CREATE INDEX IF NOT EXISTS idx_sms_iccid_peer_timestamp ON sms(iccid, peer, timestamp DESC)",
	"CREATE INDEX IF NOT EXISTS idx_sms_imsi_peer_timestamp ON sms(imsi, peer, timestamp DESC)",
	"CREATE INDEX IF NOT EXISTS idx_sms_contacts_iccid_timestamp ON sms_contacts(iccid, last_timestamp DESC)",
	"CREATE INDEX IF NOT EXISTS idx_sms_contacts_timestamp ON sms_contacts(last_timestamp DESC)",
	"CREATE INDEX IF NOT EXISTS idx_call_history_ended_at ON call_history(ended_at DESC)",
	"CREATE INDEX IF NOT EXISTS idx_call_history_device_ended_at ON call_history(device_id, ended_at DESC)",
	"CREATE INDEX IF NOT EXISTS idx_devices_iccid ON devices(iccid)",
	"CREATE INDEX IF NOT EXISTS idx_sim_cards_imsi ON sim_cards(imsi)",
	"CREATE INDEX IF NOT EXISTS idx_sim_subscriptions_current_iccid ON sim_subscriptions(current_iccid)",
}

func MigrateSchema(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return fmt.Errorf("migrate schema: nil database")
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()

	for _, table := range compatibleTables {
		if _, err := transaction.ExecContext(ctx, table.create); err != nil {
			return fmt.Errorf("create compatible table %s: %w", table.name, err)
		}
		if err := ensureCompatibleColumns(ctx, transaction, table); err != nil {
			return err
		}
	}
	if err := backfillCompatibilityData(ctx, transaction); err != nil {
		return err
	}
	for _, statement := range compatibleIndexes {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create compatible index with %q: %w", statement, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	committed = true
	return nil
}

func ensureCompatibleColumns(ctx context.Context, transaction *sql.Tx, table tableSchema) error {
	columns, err := tableColumns(ctx, transaction, table.name)
	if err != nil {
		return err
	}
	for _, identity := range table.identity {
		if _, exists := columns[identity]; !exists {
			return fmt.Errorf("table %s is incompatible: required identity column %s is missing", table.name, identity)
		}
	}

	names := make([]string, 0, len(table.addColumns))
	for name := range table.addColumns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, exists := columns[name]; exists {
			continue
		}
		statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoteIdentifier(table.name), quoteIdentifier(name), table.addColumns[name])
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add compatible column %s.%s: %w", table.name, name, err)
		}
	}
	return nil
}

func tableColumns(ctx context.Context, transaction *sql.Tx, table string) (map[string]struct{}, error) {
	rows, err := transaction.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			sequence     int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&sequence, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan table metadata for %s: %w", table, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read table metadata for %s: %w", table, err)
	}
	return columns, nil
}

func backfillCompatibilityData(ctx context.Context, transaction *sql.Tx) error {
	statements := []string{
		`UPDATE sms
		 SET peer = TRIM(CASE WHEN type = 2 AND COALESCE(recipient, '') <> '' THEN recipient ELSE sender END)
		 WHERE COALESCE(peer, '') = ''`,
		`UPDATE sms
		 SET iccid = COALESCE(
			(SELECT sim_cards.iccid FROM sim_cards WHERE sim_cards.imsi = sms.imsi AND COALESCE(sim_cards.iccid, '') <> '' LIMIT 1),
			CASE WHEN COALESCE(sms.imsi, '') <> '' THEN 'imsi:' || sms.imsi ELSE '' END
		 )
		 WHERE COALESCE(iccid, '') = ''`,
		`UPDATE sms_contacts
		 SET iccid = COALESCE(
			(SELECT sim_cards.iccid FROM sim_cards WHERE sim_cards.imsi = sms_contacts.imsi AND COALESCE(sim_cards.iccid, '') <> '' LIMIT 1),
			CASE WHEN COALESCE(sms_contacts.imsi, '') <> '' THEN 'imsi:' || sms_contacts.imsi ELSE '' END
		 )
		 WHERE COALESCE(iccid, '') = ''`,
		`INSERT INTO sms_contacts (
			imsi, iccid, peer, last_sms_id, last_timestamp, last_content,
			last_type, unread_count, created_at, updated_at
		 )
		 SELECT latest.imsi, latest.iccid, latest.peer, latest.id, latest.timestamp,
			latest.content, latest.type,
			(SELECT COUNT(*) FROM sms unread
			 WHERE unread.imsi = latest.imsi AND unread.peer = latest.peer
			 AND unread.type = 1 AND unread.status = 0),
			latest.created_at, latest.timestamp
		 FROM sms latest
		 WHERE COALESCE(latest.peer, '') <> ''
		 AND NOT EXISTS (
			SELECT 1 FROM sms newer
			WHERE newer.imsi = latest.imsi AND newer.peer = latest.peer
			AND (newer.timestamp > latest.timestamp OR
				(newer.timestamp = latest.timestamp AND newer.id > latest.id))
		 )
		 ON CONFLICT(imsi, peer) DO NOTHING`,
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("backfill compatible data: %w", err)
		}
	}
	return nil
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
