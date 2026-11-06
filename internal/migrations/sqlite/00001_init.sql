-- Porto M0 schema (SQLite). Platform tables, organizations and marinas (spec 06 §6.1, §6.9; m0-plan §2).
-- The PostgreSQL twin is internal/migrations/postgres/00001_init.sql: keep table, column and index names identical.

-- +goose Up
CREATE TABLE organizations (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    vat_number      TEXT,
    fiscal_code     TEXT,
    billing_address TEXT NOT NULL DEFAULT '{}',
    sdi_code        TEXT,
    pec             TEXT,
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX organizations_name_idx ON organizations (name);

CREATE TABLE marinas (
    id                    TEXT PRIMARY KEY,
    organization_id       TEXT NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    slug                  TEXT NOT NULL UNIQUE CHECK (length(slug) BETWEEN 3 AND 63 AND slug = lower(slug)),
    name                  TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    kind                  TEXT NOT NULL DEFAULT 'marina' CHECK (kind IN ('marina', 'yard', 'both')),
    timezone              TEXT NOT NULL DEFAULT 'Europe/Rome',
    currency              TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    default_locale        TEXT NOT NULL DEFAULT 'it',
    locales               TEXT NOT NULL DEFAULT '["it"]',
    contacts              TEXT NOT NULL DEFAULT '{}',
    address               TEXT NOT NULL DEFAULT '{}',
    geo                   TEXT,
    concession_ref        TEXT,
    concession_expires_on DATE,
    settings              TEXT NOT NULL DEFAULT '{}',
    settings_version      INTEGER NOT NULL DEFAULT 1,
    theme                 TEXT NOT NULL DEFAULT '{}',
    booking_enabled       INTEGER NOT NULL DEFAULT 0 CHECK (booking_enabled IN (0, 1)),
    allowed_embed_origins TEXT NOT NULL DEFAULT '[]',
    version               INTEGER NOT NULL DEFAULT 1,
    archived_at           TIMESTAMP,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, organization_id)
);
CREATE INDEX marinas_organization_id_idx ON marinas (organization_id);

CREATE TABLE users (
    id                  TEXT PRIMARY KEY,
    email               TEXT NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    password_hash       TEXT,
    name                TEXT NOT NULL,
    locale              TEXT NOT NULL DEFAULT 'it',
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    password_changed_at TIMESTAMP,
    last_login_at       TIMESTAMP,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL
);

CREATE TABLE user_totp (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    secret_enc   TEXT NOT NULL,
    confirmed_at TIMESTAMP,
    last_step    INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL
);

CREATE TABLE recovery_codes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL UNIQUE,
    used_at    TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX recovery_codes_user_id_idx ON recovery_codes (user_id);

CREATE TABLE memberships (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    organization_id    TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    marina_id          TEXT,
    role               TEXT NOT NULL CHECK (role IN ('owner', 'director', 'office', 'accounting', 'dock', 'yard_mgr', 'yard', 'rental', 'viewer')),
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    FOREIGN KEY (marina_id, organization_id) REFERENCES marinas (id, organization_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX memberships_user_scope_uidx ON memberships (user_id, organization_id, COALESCE(marina_id, ''));
CREATE INDEX memberships_marina_id_idx ON memberships (marina_id);
CREATE INDEX memberships_organization_id_idx ON memberships (organization_id);

CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    token_hash      TEXT NOT NULL UNIQUE,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    totp_verified   INTEGER NOT NULL DEFAULT 0 CHECK (totp_verified IN (0, 1)),
    expires_at      TIMESTAMP NOT NULL,
    idle_expires_at TIMESTAMP NOT NULL,
    last_seen_at    TIMESTAMP NOT NULL,
    ip              TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE password_reset_tokens (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    purpose    TEXT NOT NULL CHECK (purpose IN ('reset', 'invite')),
    expires_at TIMESTAMP NOT NULL,
    used_at    TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);
CREATE INDEX password_reset_tokens_expires_at_idx ON password_reset_tokens (expires_at);

CREATE TABLE api_keys (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name               TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    prefix             TEXT NOT NULL UNIQUE,
    key_hash           TEXT NOT NULL UNIQUE,
    scopes             TEXT NOT NULL DEFAULT '[]',
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    expires_at         TIMESTAMP,
    last_used_at       TIMESTAMP,
    revoked_at         TIMESTAMP,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX api_keys_marina_id_idx ON api_keys (marina_id);

CREATE TABLE devices (
    id              TEXT PRIMARY KEY,
    marina_id       TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    platform        TEXT NOT NULL CHECK (platform IN ('ios', 'android', 'windows', 'macos', 'linux', 'other')),
    kind            TEXT NOT NULL CHECK (kind IN ('mobile', 'desktop')),
    app_version     TEXT,
    token_prefix    TEXT NOT NULL UNIQUE,
    token_hash      TEXT NOT NULL UNIQUE,
    pairing_code_id TEXT,
    last_used_at    TIMESTAMP,
    revoked_at      TIMESTAMP,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX devices_marina_id_idx ON devices (marina_id);
CREATE INDEX devices_user_id_idx ON devices (user_id);

CREATE TABLE pairing_codes (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    user_id            TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    kind               TEXT CHECK (kind IN ('mobile', 'desktop')),
    code_hash          TEXT NOT NULL UNIQUE,
    expires_at         TIMESTAMP NOT NULL,
    used_at            TIMESTAMP,
    device_id          TEXT REFERENCES devices (id) ON DELETE SET NULL,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX pairing_codes_marina_id_idx ON pairing_codes (marina_id);
CREATE INDEX pairing_codes_expires_at_idx ON pairing_codes (expires_at);

CREATE TABLE idempotency_keys (
    id                    TEXT PRIMARY KEY,
    scope                 TEXT NOT NULL,
    key                   TEXT NOT NULL,
    request_hash          TEXT NOT NULL,
    state                 TEXT NOT NULL CHECK (state IN ('in_progress', 'completed')),
    response_status       INTEGER,
    response_content_type TEXT,
    response_body         BLOB,
    expires_at            TIMESTAMP NOT NULL,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (scope, key)
);
CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);

CREATE TABLE audit_log (
    id              TEXT PRIMARY KEY,
    organization_id TEXT,
    marina_id       TEXT,
    actor_type      TEXT NOT NULL CHECK (actor_type IN ('user', 'api_key', 'device', 'system')),
    actor_id        TEXT,
    action          TEXT NOT NULL,
    entity_type     TEXT NOT NULL,
    entity_id       TEXT,
    diff            TEXT,
    request_id      TEXT NOT NULL DEFAULT '',
    ip              TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL
);
CREATE INDEX audit_log_marina_created_idx ON audit_log (marina_id, created_at);
CREATE INDEX audit_log_org_created_idx ON audit_log (organization_id, created_at);
CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id);
-- +goose StatementBegin
CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is insert-only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is insert-only'); END;
-- +goose StatementEnd

CREATE TABLE outbox_events (
    id              TEXT PRIMARY KEY,
    organization_id TEXT,
    marina_id       TEXT,
    type            TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    payload         TEXT NOT NULL,
    occurred_at     TIMESTAMP NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP,
    locked_until    TIMESTAMP,
    last_error      TEXT,
    dispatched_at   TIMESTAMP,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX outbox_events_pending_idx ON outbox_events (id) WHERE dispatched_at IS NULL;
CREATE INDEX outbox_events_marina_created_idx ON outbox_events (marina_id, created_at);

CREATE TABLE webhook_endpoints (
    id                         TEXT PRIMARY KEY,
    marina_id                  TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    url                        TEXT NOT NULL,
    description                TEXT NOT NULL DEFAULT '',
    secret_enc                 TEXT NOT NULL,
    previous_secret_enc        TEXT,
    previous_secret_expires_at TIMESTAMP,
    events                     TEXT NOT NULL DEFAULT '[]',
    active                     INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    consecutive_failures       INTEGER NOT NULL DEFAULT 0,
    disabled_at                TIMESTAMP,
    disabled_reason            TEXT,
    version                    INTEGER NOT NULL DEFAULT 1,
    created_at                 TIMESTAMP NOT NULL,
    updated_at                 TIMESTAMP NOT NULL
);
CREATE INDEX webhook_endpoints_marina_id_idx ON webhook_endpoints (marina_id);

CREATE TABLE webhook_deliveries (
    id               TEXT PRIMARY KEY,
    marina_id        TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    endpoint_id      TEXT NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_id         TEXT NOT NULL REFERENCES outbox_events (id) ON DELETE CASCADE,
    event_type       TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts         INTEGER NOT NULL DEFAULT 0,
    last_status_code INTEGER,
    last_error       TEXT,
    response_excerpt TEXT,
    first_attempt_at TIMESTAMP,
    next_attempt_at  TIMESTAMP NOT NULL,
    locked_until     TIMESTAMP,
    delivered_at     TIMESTAMP,
    failed_at        TIMESTAMP,
    created_at       TIMESTAMP NOT NULL,
    updated_at       TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX webhook_deliveries_endpoint_event_uidx ON webhook_deliveries (endpoint_id, event_id);
CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_endpoint_created_idx ON webhook_deliveries (marina_id, endpoint_id, created_at);

CREATE TABLE message_log (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT REFERENCES marinas (id) ON DELETE CASCADE,
    user_id             TEXT REFERENCES users (id) ON DELETE SET NULL,
    channel             TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email')),
    template            TEXT NOT NULL,
    locale              TEXT NOT NULL DEFAULT 'it',
    recipient_hash      TEXT NOT NULL,
    recipient_enc       TEXT NOT NULL,
    payload_enc         TEXT NOT NULL,
    dedupe_key          TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'cancelled')),
    attempts            INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     TIMESTAMP,
    locked_until        TIMESTAMP,
    last_error          TEXT,
    provider_message_id TEXT,
    sent_at             TIMESTAMP,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX message_log_dedupe_uidx ON message_log (COALESCE(marina_id, ''), dedupe_key);
CREATE INDEX message_log_due_idx ON message_log (next_attempt_at) WHERE status = 'queued';
CREATE INDEX message_log_marina_created_idx ON message_log (marina_id, created_at);

-- +goose Down
DROP TABLE message_log;
DROP TABLE webhook_deliveries;
DROP TABLE webhook_endpoints;
DROP TABLE outbox_events;
DROP TRIGGER audit_log_no_delete;
DROP TRIGGER audit_log_no_update;
DROP TABLE audit_log;
DROP TABLE idempotency_keys;
DROP TABLE pairing_codes;
DROP TABLE devices;
DROP TABLE api_keys;
DROP TABLE password_reset_tokens;
DROP TABLE sessions;
DROP TABLE memberships;
DROP TABLE recovery_codes;
DROP TABLE user_totp;
DROP TABLE users;
DROP TABLE marinas;
DROP TABLE organizations;
