-- Porto M12 occupancy sensors schema (PostgreSQL) (spec 24, 06.8; m12-plan §3-§8, §15, ADR 0017). The twin is
-- internal/migrations/sqlite/00026_sensors.sql: keep table, column, index and constraint names identical.
--
-- sensor_settings: one row per marina (defaults in Go when never saved): hysteresis, silence, alert thresholds and
--     the opt-in automatic presence effect. The reconcile job locks this row first (m12-plan §8.4).
-- sensor_gateways / sensor_sources: external bridges posting decoded readings with their own key, and polled
--     ext.SensorSource configurations (config_enc: KeyRing, AAD sensor-source:<id>, never returned).
-- sensor_devices: one device on a berth, a [from_cm, to_cm) segment of a linear dynamic area or a pedestal
--     (resource validated in Go and re-checked in the transaction). Keys: key_id is the public lookup (Porto-Key),
--     key_hash the hex SHA-256 of the full key, key_verifier_enc the KeyRing-encrypted key for HMAC verification
--     (AAD sensor-key:<device_id>); the plaintext key is never stored (INV-105).
-- sensor_device_states: the hot per-device row (hysteresis and health), written only by sensor.Ingest, the
--     silence job and device reassignment.
-- sensor_readings: raw readings, idempotent on (device_id, observed_at) (INV-101), kept PORTO_SENSOR_RAW_RETENTION
--     and excluded from backup (m12-plan §7, D8). Not audited, no events.
-- sensor_state_changes: append-only history of confirmed physical changes (one outbox event each, no audit row).
-- sensor_alerts: alerts and proposals of the reconciliation; one open alert per (device, kind) (INV-104).
-- CHECK changes: maintenance_tasks.element_type and maintenance_tasks.source gain 'sensor' (maintenance proposals
--     of silent or low-battery devices, D15). PostgreSQL drops and re-adds the generated constraints; SQLite
--     edits the table sql with writable_schema as 00024/00025 do. Sensors never write allocations or occupancy (INV-100).

-- +goose Up
ALTER TABLE maintenance_tasks DROP CONSTRAINT maintenance_tasks_element_type_check;
ALTER TABLE maintenance_tasks ADD CONSTRAINT maintenance_tasks_element_type_check
    CHECK (element_type IS NULL OR element_type IN ('berth','structure','dynamic_area','land_resource','pedestal','equipment','sensor'));
ALTER TABLE maintenance_tasks DROP CONSTRAINT maintenance_tasks_source_check;
ALTER TABLE maintenance_tasks ADD CONSTRAINT maintenance_tasks_source_check
    CHECK (source IN ('plan','office','report','sensor'));

CREATE TABLE sensor_settings (
    marina_id                    UUID PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    hysteresis_readings          INTEGER NOT NULL DEFAULT 2 CHECK (hysteresis_readings BETWEEN 1 AND 10),
    hysteresis_minutes           INTEGER NOT NULL DEFAULT 5 CHECK (hysteresis_minutes BETWEEN 1 AND 60),
    offline_after_minutes        INTEGER NOT NULL DEFAULT 30 CHECK (offline_after_minutes BETWEEN 5 AND 1440),
    silent_alert_minutes         INTEGER NOT NULL DEFAULT 180 CHECK (silent_alert_minutes BETWEEN 30 AND 10080),
    battery_low_pct              INTEGER NOT NULL DEFAULT 20 CHECK (battery_low_pct BETWEEN 1 AND 90),
    unexpected_after_minutes     INTEGER NOT NULL DEFAULT 15 CHECK (unexpected_after_minutes BETWEEN 0 AND 240),
    departure_cutoff_minutes     INTEGER NOT NULL DEFAULT 720 CHECK (departure_cutoff_minutes BETWEEN 0 AND 1439),
    presence_effect_enabled      BOOLEAN NOT NULL DEFAULT false,
    presence_out_after_hours     INTEGER NOT NULL DEFAULT 6 CHECK (presence_out_after_hours BETWEEN 1 AND 168),
    release_proposal_after_days  INTEGER NOT NULL DEFAULT 3 CHECK (release_proposal_after_days BETWEEN 1 AND 60),
    release_proposal_nights      INTEGER NOT NULL DEFAULT 7 CHECK (release_proposal_nights BETWEEN 1 AND 60),
    version                      INTEGER NOT NULL DEFAULT 1,
    created_at                   TIMESTAMPTZ NOT NULL,
    updated_at                   TIMESTAMPTZ NOT NULL,
    CONSTRAINT sensor_settings_silent_check CHECK (silent_alert_minutes >= offline_after_minutes)
);

CREATE TABLE sensor_gateways (
    id                     UUID PRIMARY KEY,
    marina_id              UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                   TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name                   TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    kind                   TEXT NOT NULL CHECK (kind IN ('lorawan','nbiot','mqtt_bridge','other')),
    key_id                 TEXT UNIQUE,                          -- po_gw_<kid>, NULL when revoked
    key_hash               TEXT,                                 -- hex SHA-256 of the full key
    key_verifier_enc       TEXT,                                 -- KeyRing, AAD gateway-key:<id>
    key_issued_at          TIMESTAMPTZ,
    prev_key_id            TEXT,
    prev_key_hash          TEXT,
    prev_key_verifier_enc  TEXT,
    prev_valid_until       TIMESTAMPTZ,
    last_contact_at        TIMESTAMPTZ,
    active                 BOOLEAN NOT NULL DEFAULT true,
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    CONSTRAINT sensor_gateways_key_check CHECK ((key_id IS NULL) = (key_hash IS NULL)),
    CONSTRAINT sensor_gateways_prev_check CHECK ((prev_key_id IS NULL) = (prev_valid_until IS NULL))
);
CREATE UNIQUE INDEX sensor_gateways_code_uidx ON sensor_gateways (marina_id, code);
CREATE UNIQUE INDEX sensor_gateways_prev_key_uidx ON sensor_gateways (prev_key_id) WHERE prev_key_id IS NOT NULL;

CREATE TABLE sensor_sources (
    id               UUID PRIMARY KEY,
    marina_id        UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code             TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 64),   -- ext.SensorSource.Code()
    name             TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    config_enc       TEXT,                                                  -- KeyRing, AAD sensor-source:<id>
    poll_seconds     INTEGER NOT NULL DEFAULT 60 CHECK (poll_seconds BETWEEN 15 AND 3600),
    cursor           TEXT NOT NULL DEFAULT '' CHECK (char_length(cursor) <= 512),
    enabled          BOOLEAN NOT NULL DEFAULT true,
    last_polled_at   TIMESTAMPTZ,
    last_error_code  TEXT CHECK (last_error_code IS NULL OR last_error_code IN ('timeout','auth','bad_response','unavailable')),
    version          INTEGER NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX sensor_sources_name_uidx ON sensor_sources (marina_id, name);
CREATE INDEX sensor_sources_poll_idx ON sensor_sources (enabled, last_polled_at);

CREATE TABLE sensor_devices (
    id                     UUID PRIMARY KEY,
    marina_id              UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    external_id            TEXT NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 64),   -- protocol device_id
    code                   TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    kind                   TEXT NOT NULL CHECK (kind IN ('ultrasonic','radar','magnetometer','infrared','camera','other')),
    model                  TEXT NOT NULL DEFAULT '' CHECK (char_length(model) <= 64),
    firmware               TEXT NOT NULL DEFAULT '' CHECK (char_length(firmware) <= 64),
    resource_type          TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','pedestal')),
    resource_id            UUID NOT NULL,                       -- berth, linear dynamic area or pedestal of the marina
    from_cm                BIGINT,
    to_cm                  BIGINT,
    pedestal_socket        TEXT CHECK (pedestal_socket IS NULL OR char_length(pedestal_socket) BETWEEN 1 AND 16),
    map_id                 UUID NOT NULL,
    x_cm                   BIGINT NOT NULL,
    y_cm                   BIGINT NOT NULL,
    gateway_id             UUID,
    source_id              UUID,
    key_id                 TEXT UNIQUE,                         -- po_sns_<kid>, NULL when revoked
    key_hash               TEXT,
    key_verifier_enc       TEXT,                                -- KeyRing, AAD sensor-key:<id>
    key_issued_at          TIMESTAMPTZ,
    prev_key_id            TEXT,
    prev_key_hash          TEXT,
    prev_key_verifier_enc  TEXT,
    prev_valid_until       TIMESTAMPTZ,
    active                 BOOLEAN NOT NULL DEFAULT true,
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id),
    FOREIGN KEY (gateway_id, marina_id) REFERENCES sensor_gateways (id, marina_id),
    FOREIGN KEY (source_id, marina_id) REFERENCES sensor_sources (id, marina_id),
    CONSTRAINT sensor_devices_segment_check CHECK ((resource_type = 'dynamic_area') = (from_cm IS NOT NULL AND to_cm IS NOT NULL)),
    CONSTRAINT sensor_devices_segment_order_check CHECK (from_cm IS NULL OR to_cm IS NULL OR (from_cm >= 0 AND from_cm < to_cm)),
    CONSTRAINT sensor_devices_socket_check CHECK (pedestal_socket IS NULL OR resource_type = 'pedestal'),
    CONSTRAINT sensor_devices_binding_check CHECK (NOT (gateway_id IS NOT NULL AND source_id IS NOT NULL)),
    CONSTRAINT sensor_devices_key_check CHECK ((key_id IS NULL) = (key_hash IS NULL)),
    CONSTRAINT sensor_devices_prev_check CHECK ((prev_key_id IS NULL) = (prev_valid_until IS NULL))
);
CREATE UNIQUE INDEX sensor_devices_external_uidx ON sensor_devices (marina_id, external_id);
CREATE UNIQUE INDEX sensor_devices_code_uidx ON sensor_devices (marina_id, code);
CREATE UNIQUE INDEX sensor_devices_prev_key_uidx ON sensor_devices (prev_key_id) WHERE prev_key_id IS NOT NULL;
CREATE UNIQUE INDEX sensor_devices_resource_uidx ON sensor_devices (marina_id, resource_type, resource_id, coalesce(pedestal_socket, ''))
    WHERE active AND resource_type <> 'dynamic_area';
CREATE INDEX sensor_devices_map_idx ON sensor_devices (marina_id, map_id);
CREATE INDEX sensor_devices_resource_idx ON sensor_devices (marina_id, resource_type, resource_id);
CREATE INDEX sensor_devices_gateway_idx ON sensor_devices (gateway_id) WHERE gateway_id IS NOT NULL;
CREATE INDEX sensor_devices_source_idx ON sensor_devices (source_id) WHERE source_id IS NOT NULL;

CREATE TABLE sensor_device_states (
    device_id         UUID PRIMARY KEY,
    marina_id         UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    physical          TEXT NOT NULL DEFAULT 'unknown' CHECK (physical IN ('occupied','free','unknown')),
    physical_since    TIMESTAMPTZ,
    candidate         TEXT CHECK (candidate IS NULL OR candidate IN ('occupied','free','unknown')),
    candidate_since   TIMESTAMPTZ,
    candidate_count   INTEGER NOT NULL DEFAULT 0 CHECK (candidate_count >= 0),
    last_observed_at  TIMESTAMPTZ,
    last_contact_at   TIMESTAMPTZ,                        -- server receive time
    battery_pct       INTEGER CHECK (battery_pct IS NULL OR battery_pct BETWEEN 0 AND 100),
    rssi              INTEGER CHECK (rssi IS NULL OR rssi BETWEEN -200 AND 0),
    distance_cm       BIGINT CHECK (distance_cm IS NULL OR distance_cm BETWEEN 0 AND 10000),
    firmware          TEXT NOT NULL DEFAULT '' CHECK (char_length(firmware) <= 64),
    updated_at        TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (device_id, marina_id) REFERENCES sensor_devices (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT sensor_device_states_candidate_pair_check CHECK ((candidate IS NULL) = (candidate_since IS NULL))
);
CREATE INDEX sensor_device_states_marina_idx ON sensor_device_states (marina_id, physical);
CREATE INDEX sensor_device_states_observed_idx ON sensor_device_states (marina_id, last_observed_at);

CREATE TABLE sensor_readings (
    device_id    UUID NOT NULL,
    observed_at  TIMESTAMPTZ NOT NULL,                    -- UTC, truncated to milliseconds (idempotency key)
    marina_id    UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    received_at  TIMESTAMPTZ NOT NULL,
    state        TEXT NOT NULL CHECK (state IN ('occupied','free','unknown')),
    distance_cm  BIGINT CHECK (distance_cm IS NULL OR distance_cm BETWEEN 0 AND 10000),
    battery_pct  INTEGER CHECK (battery_pct IS NULL OR battery_pct BETWEEN 0 AND 100),
    rssi         INTEGER CHECK (rssi IS NULL OR rssi BETWEEN -200 AND 0),
    origin       TEXT NOT NULL CHECK (origin IN ('device','gateway','source')),
    PRIMARY KEY (device_id, observed_at),
    FOREIGN KEY (device_id, marina_id) REFERENCES sensor_devices (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX sensor_readings_observed_idx ON sensor_readings (observed_at);
CREATE INDEX sensor_readings_marina_idx ON sensor_readings (marina_id, device_id, observed_at);

CREATE TABLE sensor_state_changes (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    device_id      UUID NOT NULL,
    resource_type  TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','pedestal')),
    resource_id    UUID NOT NULL,                       -- the device's resource at the time of the change
    map_id         UUID NOT NULL,
    from_cm        BIGINT,
    to_cm          BIGINT,
    from_state     TEXT NOT NULL CHECK (from_state IN ('occupied','free','unknown')),
    to_state       TEXT NOT NULL CHECK (to_state IN ('occupied','free','unknown')),
    at             TIMESTAMPTZ NOT NULL,                  -- first agreeing reading (or silence/move instant)
    reason         TEXT NOT NULL CHECK (reason IN ('readings','duration','silence','device_moved')),
    created_at     TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (device_id, marina_id) REFERENCES sensor_devices (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT sensor_state_changes_states_check CHECK (from_state <> to_state)
);
CREATE INDEX sensor_state_changes_resource_idx ON sensor_state_changes (marina_id, resource_id, at);
CREATE INDEX sensor_state_changes_device_idx ON sensor_state_changes (device_id, at);

CREATE TABLE sensor_alerts (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('unexpected_occupied','transit_arrival','return_detected',
                            'release_proposal','early_return','device_silent','battery_low')),
    device_id           UUID NOT NULL,
    resource_type       TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','pedestal')),
    resource_id         UUID NOT NULL,
    map_id              UUID NOT NULL,
    condition_key       TEXT NOT NULL CHECK (char_length(condition_key) BETWEEN 1 AND 120),
    refs                JSONB NOT NULL DEFAULT '{}',    -- ids and enums only (m12-plan §8.3)
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','confirmed','dismissed','resolved')),
    opened_at           TIMESTAMPTZ NOT NULL,
    cleared_at          TIMESTAMPTZ,
    decided_at          TIMESTAMPTZ,
    decided_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    outcome_ref         TEXT,                          -- presence, release or maintenance task created on confirm
    note                TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),   -- never in events
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (device_id, marina_id) REFERENCES sensor_devices (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT sensor_alerts_decided_check CHECK ((status IN ('confirmed','dismissed')) = (decided_at IS NOT NULL))
);
CREATE UNIQUE INDEX sensor_alerts_open_uidx ON sensor_alerts (device_id, kind) WHERE status = 'open';
CREATE INDEX sensor_alerts_status_idx ON sensor_alerts (marina_id, status, opened_at);
CREATE INDEX sensor_alerts_condition_idx ON sensor_alerts (device_id, kind, condition_key);
CREATE INDEX sensor_alerts_resource_idx ON sensor_alerts (marina_id, resource_id, opened_at);

-- +goose Down
DROP TABLE sensor_alerts;
DROP TABLE sensor_state_changes;
DROP TABLE sensor_readings;
DROP TABLE sensor_device_states;
DROP TABLE sensor_devices;
DROP TABLE sensor_sources;
DROP TABLE sensor_gateways;
DROP TABLE sensor_settings;
DELETE FROM maintenance_task_files WHERE task_id IN (SELECT id FROM maintenance_tasks WHERE element_type = 'sensor' OR source = 'sensor');
DELETE FROM maintenance_tasks WHERE element_type = 'sensor' OR source = 'sensor';
ALTER TABLE maintenance_tasks DROP CONSTRAINT maintenance_tasks_element_type_check;
ALTER TABLE maintenance_tasks ADD CONSTRAINT maintenance_tasks_element_type_check
    CHECK (element_type IS NULL OR element_type IN ('berth','structure','dynamic_area','land_resource','pedestal','equipment'));
ALTER TABLE maintenance_tasks DROP CONSTRAINT maintenance_tasks_source_check;
ALTER TABLE maintenance_tasks ADD CONSTRAINT maintenance_tasks_source_check
    CHECK (source IN ('plan','office','report'));
