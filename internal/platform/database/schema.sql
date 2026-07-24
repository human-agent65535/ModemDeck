-- ModemDeck current database schema.

-- Run only against an empty SQLite database.

CREATE TABLE modemdeck_admin_credentials (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			password_hash TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_auth_sessions (
			session_token_digest BLOB PRIMARY KEY CHECK (length(session_token_digest) = 32),
			csrf_token_digest BLOB NOT NULL CHECK (length(csrf_token_digest) = 32),
			created_at_unix INTEGER NOT NULL,
			expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix >= created_at_unix)
		);

CREATE TABLE contacts (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			preferred_device_imei TEXT NOT NULL DEFAULT '',
			is_favorite NUMERIC NOT NULL DEFAULT 0,
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE contact_phones (
			id TEXT PRIMARY KEY,
			contact_id TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			original_number TEXT NOT NULL DEFAULT '',
			canonical_e164 TEXT NOT NULL DEFAULT '',
			is_primary NUMERIC NOT NULL DEFAULT 0,
			FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE ON UPDATE CASCADE
		);

CREATE TABLE sms (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				request_id TEXT NOT NULL DEFAULT '',
				line_id TEXT NOT NULL DEFAULT '',
				endpoint_message_id TEXT NOT NULL DEFAULT '',
				imsi TEXT NOT NULL DEFAULT '',
				iccid TEXT NOT NULL DEFAULT '',
				peer TEXT NOT NULL DEFAULT '',
			local_phone TEXT NOT NULL DEFAULT '',
			sender TEXT NOT NULL DEFAULT '',
			recipient TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
				type INTEGER NOT NULL DEFAULT 0,
				status INTEGER NOT NULL DEFAULT 0,
				state TEXT NOT NULL DEFAULT '',
				failure_code TEXT NOT NULL DEFAULT '',
				revision INTEGER NOT NULL DEFAULT 1,
				timestamp DATETIME,
				created_at DATETIME
			);

CREATE TABLE sms_contacts (
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
		);

CREATE TABLE call_history (
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
			failure_code TEXT NOT NULL DEFAULT '',
			bearer TEXT NOT NULL DEFAULT '',
			state_reason TEXT NOT NULL DEFAULT '',
			state_reason_code INTEGER NOT NULL DEFAULT 0,
			multiparty NUMERIC NOT NULL DEFAULT 0,
			audio_port TEXT NOT NULL DEFAULT '',
			audio_encoding TEXT NOT NULL DEFAULT '',
			audio_resolution TEXT NOT NULL DEFAULT '',
			audio_rate INTEGER NOT NULL DEFAULT 0,
			media_available NUMERIC NOT NULL DEFAULT 0
			);

CREATE TABLE modemdeck_call_settings (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			receive_calls NUMERIC NOT NULL DEFAULT 1,
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_line_settings (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			default_device_imei TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_line_call_policies (
			line_id TEXT PRIMARY KEY,
			policy TEXT NOT NULL DEFAULT 'follow_global'
				CHECK (policy IN ('follow_global', 'receive', 'do_not_disturb')),
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_incoming_call_actions (
			call_id TEXT PRIMARY KEY,
			line_id TEXT NOT NULL,
			endpoint_call_id TEXT NOT NULL,
			effective_policy TEXT NOT NULL
				CHECK (effective_policy IN ('receive', 'do_not_disturb')),
			global_revision INTEGER NOT NULL,
			line_revision INTEGER NOT NULL,
			request_id TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'pending'
				CHECK (status IN ('pending', 'sending', 'succeeded', 'failed', 'indeterminate', 'skipped')),
			error_code TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (call_id) REFERENCES call_history(id) ON DELETE CASCADE ON UPDATE CASCADE
		);

CREATE TABLE modemdeck_recording_settings (
				singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
				default_enabled NUMERIC NOT NULL DEFAULT 0,
				revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

CREATE TABLE modemdeck_call_recording_requests (
				request_id TEXT PRIMARY KEY,
				enabled NUMERIC NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

CREATE TABLE modemdeck_call_recording_state (
				call_id TEXT PRIMARY KEY,
				preference TEXT NOT NULL DEFAULT 'default',
				request_enabled NUMERIC,
				enabled NUMERIC NOT NULL DEFAULT 0,
				generation INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL DEFAULT 'off',
				active_segment_id TEXT NOT NULL DEFAULT '',
				last_error_code TEXT NOT NULL DEFAULT '',
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (call_id) REFERENCES call_history(id) ON DELETE CASCADE ON UPDATE CASCADE
			);

CREATE TABLE modemdeck_call_recordings (
				id TEXT PRIMARY KEY,
				call_id TEXT NOT NULL,
				segment_index INTEGER NOT NULL CHECK (segment_index > 0),
				status TEXT NOT NULL,
				started_at DATETIME,
				ended_at DATETIME,
				duration_ms INTEGER NOT NULL DEFAULT 0,
				size_bytes INTEGER NOT NULL DEFAULT 0,
				relative_path TEXT NOT NULL DEFAULT '',
				failure_code TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE (call_id, segment_index),
				FOREIGN KEY (call_id) REFERENCES call_history(id) ON DELETE CASCADE ON UPDATE CASCADE
			);

CREATE TABLE devices (
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
				signal_quality INTEGER,
				signal_db_m INTEGER NOT NULL DEFAULT 0,
			signal_rsrq INTEGER NOT NULL DEFAULT 0,
			signal_rsrp INTEGER NOT NULL DEFAULT 0,
			last_seen DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		);

CREATE TABLE sim_cards (
			iccid TEXT PRIMARY KEY,
			imsi TEXT NOT NULL DEFAULT '',
			line_label TEXT NOT NULL DEFAULT '',
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
		);

CREATE TABLE sim_subscriptions (
			imsi TEXT PRIMARY KEY,
			current_iccid TEXT NOT NULL DEFAULT '',
			phone_number TEXT NOT NULL DEFAULT '',
			modem_phone_number TEXT NOT NULL DEFAULT '',
			vowifi_phone_number TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			last_seen DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		);

CREATE TABLE modemdeck_telegram_units (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL DEFAULT '',
			enabled NUMERIC NOT NULL DEFAULT 0,
			bot_id INTEGER NOT NULL DEFAULT 0,
			bot_token_nonce BLOB NOT NULL DEFAULT X'',
			bot_token_ciphertext BLOB NOT NULL DEFAULT X'',
			token_hint TEXT NOT NULL DEFAULT '',
			chat_id INTEGER NOT NULL DEFAULT 0,
			admin_id INTEGER NOT NULL DEFAULT 0,
			incoming_sms NUMERIC NOT NULL DEFAULT 1,
			missed_calls NUMERIC NOT NULL DEFAULT 1,
			revision INTEGER NOT NULL DEFAULT 1,
			bot_username TEXT NOT NULL DEFAULT '',
			verified_at DATETIME,
			last_error_class TEXT NOT NULL DEFAULT '',
			next_update_offset INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_telegram_line_scopes (
			unit_id TEXT NOT NULL,
			line_id TEXT NOT NULL,
			PRIMARY KEY (unit_id, line_id),
			FOREIGN KEY (unit_id) REFERENCES modemdeck_telegram_units(id) ON DELETE CASCADE ON UPDATE CASCADE
		);

CREATE TABLE modemdeck_telegram_reply_bindings (
			bot_id INTEGER NOT NULL,
			chat_id INTEGER NOT NULL,
			message_id INTEGER NOT NULL,
			line_id TEXT NOT NULL,
			phone_number TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (bot_id, chat_id, message_id)
		);

CREATE TABLE modemdeck_proxy_instances (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			line_id TEXT NOT NULL,
			enabled NUMERIC NOT NULL DEFAULT 0,
			mode TEXT NOT NULL CHECK (mode IN ('socks5', 'http')),
			listen_address TEXT NOT NULL,
			listen_port INTEGER NOT NULL CHECK (listen_port BETWEEN 1024 AND 65535),
			auth_enabled NUMERIC NOT NULL DEFAULT 0,
			username TEXT NOT NULL DEFAULT '',
			password_nonce BLOB NOT NULL DEFAULT X'',
			password_ciphertext BLOB NOT NULL DEFAULT X'',
			revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
			applied_revision INTEGER NOT NULL DEFAULT 0
				CHECK (applied_revision >= 0 AND applied_revision <= revision),
			desired_deleted NUMERIC NOT NULL DEFAULT 0
				CHECK (desired_deleted IN (0, 1)),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_network_counter_checkpoints (
			scope_kind TEXT NOT NULL CHECK (scope_kind IN ('line', 'proxy')),
			scope_id TEXT NOT NULL,
			epoch TEXT NOT NULL,
			rx_bytes INTEGER NOT NULL CHECK (rx_bytes >= 0),
			tx_bytes INTEGER NOT NULL CHECK (tx_bytes >= 0),
			observed_at DATETIME NOT NULL,
			PRIMARY KEY (scope_kind, scope_id)
		);

CREATE TABLE modemdeck_network_daily_usage (
			day TEXT NOT NULL,
			scope_kind TEXT NOT NULL CHECK (scope_kind IN ('line', 'proxy')),
			scope_id TEXT NOT NULL,
			rx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (rx_bytes >= 0),
			tx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (tx_bytes >= 0),
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (day, scope_kind, scope_id)
		);

CREATE TABLE modemdeck_hardware_sync (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			boot_epoch TEXT NOT NULL DEFAULT '',
			snapshot_revision TEXT NOT NULL DEFAULT '',
			sequence INTEGER NOT NULL DEFAULT 0,
			observed_at DATETIME,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_hardware_commands (
			request_id TEXT PRIMARY KEY,
			operation TEXT NOT NULL,
			payload_digest BLOB NOT NULL,
			status TEXT NOT NULL,
			resource_id TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_notification_events (
			event_key TEXT PRIMARY KEY,
			event_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			line_id TEXT NOT NULL,
			peer TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			occurred_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

CREATE TABLE modemdeck_notification_deliveries (
			event_key TEXT NOT NULL,
			unit_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempt_token TEXT NOT NULL DEFAULT '',
			last_error_class TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (event_key, unit_id),
			FOREIGN KEY (event_key) REFERENCES modemdeck_notification_events(event_key) ON DELETE CASCADE ON UPDATE CASCADE,
			FOREIGN KEY (unit_id) REFERENCES modemdeck_telegram_units(id) ON DELETE CASCADE ON UPDATE CASCADE
		);

CREATE INDEX idx_modemdeck_auth_sessions_expiry ON modemdeck_auth_sessions(expires_at_unix);

CREATE INDEX idx_contacts_display_name ON contacts(display_name);

CREATE INDEX idx_contacts_preferred_device ON contacts(preferred_device_imei);

CREATE INDEX idx_contact_phones_contact_id ON contact_phones(contact_id);

CREATE UNIQUE INDEX ux_contact_phones_canonical_e164 ON contact_phones(canonical_e164);

CREATE UNIQUE INDEX ux_contact_phones_primary_per_contact ON contact_phones(contact_id) WHERE is_primary = 1;

CREATE INDEX idx_sms_iccid_timestamp ON sms(iccid, timestamp DESC);

CREATE INDEX idx_sms_iccid_peer_timestamp ON sms(iccid, peer, timestamp DESC);

CREATE INDEX idx_sms_imsi_peer_timestamp ON sms(imsi, peer, timestamp DESC);

CREATE UNIQUE INDEX ux_sms_request_id ON sms(request_id) WHERE request_id <> '';

CREATE UNIQUE INDEX ux_sms_endpoint_message ON sms(line_id, endpoint_message_id) WHERE line_id <> '' AND endpoint_message_id <> '';

CREATE INDEX idx_sms_contacts_iccid_timestamp ON sms_contacts(iccid, last_timestamp DESC);

CREATE INDEX idx_sms_contacts_timestamp ON sms_contacts(last_timestamp DESC);

CREATE INDEX idx_call_history_ended_at ON call_history(ended_at DESC);

CREATE INDEX idx_call_history_device_ended_at ON call_history(device_id, ended_at DESC);

CREATE UNIQUE INDEX ux_call_history_request_id ON call_history(request_id) WHERE request_id <> '';

CREATE INDEX idx_modemdeck_incoming_call_actions_status ON modemdeck_incoming_call_actions(status, created_at);

CREATE INDEX idx_modemdeck_call_recording_state_enabled ON modemdeck_call_recording_state(enabled, generation);

CREATE INDEX idx_modemdeck_call_recording_requests_created ON modemdeck_call_recording_requests(created_at, request_id);

CREATE INDEX idx_modemdeck_call_recordings_call ON modemdeck_call_recordings(call_id, segment_index);

CREATE INDEX idx_modemdeck_call_recordings_status ON modemdeck_call_recordings(status, updated_at);

CREATE INDEX idx_devices_iccid ON devices(iccid);

CREATE INDEX idx_sim_cards_imsi ON sim_cards(imsi);

CREATE INDEX idx_sim_subscriptions_current_iccid ON sim_subscriptions(current_iccid);

CREATE UNIQUE INDEX ux_modemdeck_telegram_units_bot_id ON modemdeck_telegram_units(bot_id) WHERE bot_id > 0;

CREATE INDEX idx_modemdeck_telegram_reply_bindings_created_at ON modemdeck_telegram_reply_bindings(created_at);

CREATE INDEX idx_modemdeck_proxy_instances_line ON modemdeck_proxy_instances(line_id, enabled);

CREATE INDEX idx_modemdeck_network_daily_usage_scope ON modemdeck_network_daily_usage(scope_kind, scope_id, day);

CREATE INDEX idx_modemdeck_hardware_commands_status_updated ON modemdeck_hardware_commands(status, updated_at);

CREATE INDEX idx_modemdeck_notification_deliveries_status ON modemdeck_notification_deliveries(status, created_at);

INSERT INTO modemdeck_call_settings (
	singleton, receive_calls, revision, updated_at
) VALUES (1, 1, 1, CURRENT_TIMESTAMP);

INSERT INTO modemdeck_line_settings (
	singleton, default_device_imei, revision, updated_at
) VALUES (1, '', 1, CURRENT_TIMESTAMP);

INSERT INTO modemdeck_recording_settings (
	singleton, default_enabled, revision, updated_at
) VALUES (1, 0, 1, CURRENT_TIMESTAMP);
