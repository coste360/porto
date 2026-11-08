-- Porto M8 online booking schema (PostgreSQL) (spec 23, 10.8, 13.5, 27.5; m8-plan §13, ADR 0013). The twin is
-- internal/migrations/sqlite/00016_online_booking.sql: keep table, column, index and constraint names identical.
--
-- online_booking_settings: one row per marina (rules, payment, cancellation tiers, approval, texts, theme); missing
--     row = defaults of onlineconf.Defaults().
-- booking_texts: published versions of terms, privacy and marketing texts; insert-only (published texts are
--     immutable, trigger below).
-- online_bookings: the online side of a transit created by the public page or a channel (reference, token nonce,
--     domain idempotency, encrypted contact, approval state). The contract keeps the state (INV-30).
-- online_quota_usage / online_quota_claims: online quotas as DB counters (INV-12): used = |claims| and used <= cap
--     per (group, night); online_quota_usage_limit_check makes the conditional UPDATE the only way to claim.
-- customer_portal_links / customer_portal_sessions: magic-link login of the customer area (token hashes only).
-- channel_connections / channel_reservations / channel_events: BookingChannel extensions (INV-89: a reservation
--     and an event are applied once, by unique keys).
-- ALTERs: berth_groups online quotas, dynamic_areas.online_sellable, documents verification, payment_intents
--     manual capture (status stays processing while authorised, m8-plan §16 D6), consents.text_sha256.
-- CHECK changes: consents.kind gains 'terms', contracts.confirmation_source gains 'channel' (constraints dropped and
--     re-created with the same names; SQLite edits the table sql in place).

-- +goose Up
ALTER TABLE contracts DROP CONSTRAINT contracts_confirmation_source_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_confirmation_source_check
    CHECK (confirmation_source IS NULL OR confirmation_source IN ('manual','provider','channel'));
ALTER TABLE consents DROP CONSTRAINT consents_kind_check;
ALTER TABLE consents ADD CONSTRAINT consents_kind_check CHECK (kind IN ('terms','privacy','marketing','third_party'));

ALTER TABLE consents ADD COLUMN text_sha256 TEXT;            -- SHA-256 of the accepted booking text (online consents)

ALTER TABLE berth_groups ADD COLUMN online_quota_kind TEXT NOT NULL DEFAULT 'none'
    CHECK (online_quota_kind IN ('none','count','percent'));
ALTER TABLE berth_groups ADD COLUMN online_quota_value BIGINT NOT NULL DEFAULT 0 CHECK (online_quota_value >= 0);

ALTER TABLE dynamic_areas ADD COLUMN online_sellable BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE documents ADD COLUMN verified_at TIMESTAMPTZ;
ALTER TABLE documents ADD COLUMN verified_by_user_id UUID REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE payment_intents ADD COLUMN capture_method TEXT NOT NULL DEFAULT 'automatic'
    CHECK (capture_method IN ('automatic','manual'));
ALTER TABLE payment_intents ADD COLUMN authorized_at TIMESTAMPTZ;
ALTER TABLE payment_intents ADD COLUMN captured_at TIMESTAMPTZ;
ALTER TABLE payment_intents ADD COLUMN voided_at TIMESTAMPTZ;

CREATE INDEX contracts_marina_channel_status_idx ON contracts (marina_id, channel, status);

CREATE TABLE online_booking_settings (
    marina_id               UUID PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    min_nights              INTEGER NOT NULL DEFAULT 1 CHECK (min_nights >= 1),
    max_nights              INTEGER NOT NULL DEFAULT 30 CHECK (max_nights BETWEEN 1 AND 366),
    min_advance_hours       INTEGER NOT NULL DEFAULT 2 CHECK (min_advance_hours >= 0),
    max_advance_days        INTEGER NOT NULL DEFAULT 365 CHECK (max_advance_days BETWEEN 1 AND 1095),
    arrival_from            TEXT NOT NULL DEFAULT '12:00' CHECK (char_length(arrival_from) = 5),
    arrival_to              TEXT NOT NULL DEFAULT '20:00' CHECK (char_length(arrival_to) = 5),
    closed_weekdays         JSONB NOT NULL DEFAULT '[]',          -- [0..6], 0 = Sunday (time.Weekday)
    payment_mode            TEXT NOT NULL DEFAULT 'full' CHECK (payment_mode IN ('deposit','full')),
    deposit_bp              INTEGER NOT NULL DEFAULT 3000 CHECK (deposit_bp BETWEEN 1 AND 10000),
    cancellation_tiers      JSONB NOT NULL DEFAULT '[]',          -- [{min_days_before, refund_bp}]
    refund_as               TEXT NOT NULL DEFAULT 'refund' CHECK (refund_as IN ('refund','credit')),
    approval                JSONB NOT NULL DEFAULT '{}',          -- {loa_over_cm, group_ids, always, hold_hours}
    required_fields         JSONB NOT NULL DEFAULT '{}',          -- {documents, people_on_board, phone, documents_later}
    reveal_berth            TEXT NOT NULL DEFAULT 'after_confirmation' CHECK (reveal_berth IN ('never','after_confirmation')),
    late_payment_recovery   BOOLEAN NOT NULL DEFAULT true,
    allow_without_provider  BOOLEAN NOT NULL DEFAULT false,
    max_live_holds          INTEGER NOT NULL DEFAULT 50 CHECK (max_live_holds BETWEEN 1 AND 10000),
    arrival_info            JSONB NOT NULL DEFAULT '{}',          -- {vhf_channel, phone, rules_url, text: I18nText}
    theme_preset            TEXT NOT NULL DEFAULT 'default' CHECK (char_length(theme_preset) BETWEEN 1 AND 32),
    hide_header_in_embed    BOOLEAN NOT NULL DEFAULT false,
    show_feet               BOOLEAN NOT NULL DEFAULT true,
    version                 INTEGER NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ NOT NULL,
    updated_at              TIMESTAMPTZ NOT NULL,
    CHECK (min_nights <= max_nights)
);

CREATE TABLE booking_texts (
    id                    UUID PRIMARY KEY,
    marina_id             UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                  TEXT NOT NULL CHECK (kind IN ('terms','privacy','marketing')),
    version               INTEGER NOT NULL CHECK (version >= 1),
    texts                 JSONB NOT NULL,                         -- I18nText (it required)
    sha256                TEXT NOT NULL CHECK (char_length(sha256) = 64),
    published_at          TIMESTAMPTZ NOT NULL,
    published_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at            TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX booking_texts_version_uidx ON booking_texts (marina_id, kind, version);
-- +goose StatementBegin
CREATE FUNCTION porto_booking_texts_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.kind <> OLD.kind OR NEW.version <> OLD.version OR NEW.texts <> OLD.texts OR NEW.sha256 <> OLD.sha256 THEN
        RAISE EXCEPTION 'booking_text_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER booking_texts_immutable BEFORE UPDATE ON booking_texts
    FOR EACH ROW EXECUTE FUNCTION porto_booking_texts_immutable();

CREATE TABLE online_bookings (
    id                    UUID PRIMARY KEY,
    marina_id             UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    contract_id           UUID NOT NULL,
    channel               TEXT NOT NULL DEFAULT 'online' CHECK (channel IN ('online','external')),
    reference             TEXT NOT NULL CHECK (char_length(reference) = 8),   -- Crockford base32
    token_nonce           TEXT NOT NULL,                         -- booking token = HMAC(nonce); the token is never stored
    idempotency_hash      TEXT,                                  -- SHA-256 of the Idempotency-Key (public creation)
    body_sha256           TEXT,                                  -- SHA-256 of the canonical request body
    contact_enc           TEXT,                                  -- KeyRing, AAD booking-contact:<id>
    people_on_board       INTEGER CHECK (people_on_board IS NULL OR people_on_board BETWEEN 1 AND 500),
    approval_reason       TEXT CHECK (approval_reason IS NULL OR approval_reason IN ('loa','group','always','no_provider')),
    approval_deadline     TIMESTAMPTZ,
    approved_at           TIMESTAMPTZ,
    approved_by_user_id   UUID REFERENCES users (id) ON DELETE SET NULL,
    rejected_at           TIMESTAMPTZ,
    reject_reason         TEXT,
    embed_parent_origin   TEXT,
    locale                TEXT NOT NULL DEFAULT 'it',
    inquiry_id            UUID,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (inquiry_id, marina_id) REFERENCES inquiries (id, marina_id)
);
CREATE UNIQUE INDEX online_bookings_contract_uidx ON online_bookings (contract_id);
CREATE UNIQUE INDEX online_bookings_reference_uidx ON online_bookings (marina_id, reference);
CREATE UNIQUE INDEX online_bookings_idempotency_uidx ON online_bookings (marina_id, idempotency_hash)
    WHERE idempotency_hash IS NOT NULL;
CREATE INDEX online_bookings_created_idx ON online_bookings (marina_id, created_at);
CREATE INDEX online_bookings_deadline_idx ON online_bookings (approval_deadline) WHERE approval_deadline IS NOT NULL;

-- Documents uploaded through a booking (token surfaces list and count only these, never the older documents of
-- a customer the booking was linked to by email).
CREATE TABLE online_booking_documents (
    id                UUID PRIMARY KEY,
    marina_id         UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    online_booking_id UUID NOT NULL,
    document_id       UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    UNIQUE (online_booking_id, document_id),
    FOREIGN KEY (online_booking_id, marina_id) REFERENCES online_bookings (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (document_id, marina_id) REFERENCES documents (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX online_booking_documents_marina_idx ON online_booking_documents (marina_id, document_id);

CREATE TABLE online_quota_usage (
    marina_id   UUID NOT NULL,
    group_id    UUID NOT NULL,
    night       DATE NOT NULL,
    cap         INTEGER NOT NULL CHECK (cap >= 0),
    used        INTEGER NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (group_id, night),
    FOREIGN KEY (group_id, marina_id) REFERENCES berth_groups (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT online_quota_usage_limit_check CHECK (used >= 0 AND used <= cap)
);
CREATE INDEX online_quota_usage_night_idx ON online_quota_usage (marina_id, night);

CREATE TABLE online_quota_claims (
    marina_id    UUID NOT NULL,
    contract_id  UUID NOT NULL,
    group_id     UUID NOT NULL,
    night        DATE NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (contract_id, group_id, night),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (group_id, marina_id) REFERENCES berth_groups (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX online_quota_claims_group_idx ON online_quota_claims (marina_id, group_id, night);

CREATE TABLE customer_portal_links (
    id           UUID PRIMARY KEY,
    marina_id    UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id  UUID NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('login','booking')),
    contract_id  UUID,                                           -- kind booking: the transit of the confirmation email
    token_hash   TEXT NOT NULL CHECK (char_length(token_hash) = 64),  -- SHA-256 hex; the token travels in the URL fragment
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id) ON DELETE CASCADE,
    CHECK (kind <> 'booking' OR contract_id IS NOT NULL)
);
CREATE UNIQUE INDEX customer_portal_links_token_uidx ON customer_portal_links (token_hash);
CREATE INDEX customer_portal_links_customer_idx ON customer_portal_links (marina_id, customer_id, created_at);
CREATE INDEX customer_portal_links_expires_idx ON customer_portal_links (expires_at);

CREATE TABLE customer_portal_sessions (
    id            UUID PRIMARY KEY,
    marina_id     UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id   UUID NOT NULL,
    token_hash    TEXT NOT NULL CHECK (char_length(token_hash) = 64),
    last_seen_at  TIMESTAMPTZ NOT NULL,                            -- idle expiry = last_seen_at + 8 h
    expires_at    TIMESTAMPTZ NOT NULL,                            -- absolute expiry (7 days)
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX customer_portal_sessions_token_uidx ON customer_portal_sessions (token_hash);
CREATE INDEX customer_portal_sessions_customer_idx ON customer_portal_sessions (marina_id, customer_id);
CREATE INDEX customer_portal_sessions_expires_idx ON customer_portal_sessions (expires_at);

CREATE TABLE channel_connections (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    channel_code        TEXT NOT NULL CHECK (char_length(channel_code) BETWEEN 1 AND 32),
    enabled             BOOLEAN NOT NULL DEFAULT false,
    credentials_enc     TEXT,                                    -- KeyRing, AAD channel:<marina>:<code>
    webhook_secret_enc  TEXT,                                    -- KeyRing, AAD channel:<marina>:<code>
    category_map        JSONB NOT NULL DEFAULT '{}',              -- {external code: Porto category code}
    push_enabled        BOOLEAN NOT NULL DEFAULT false,
    push_dirty          BOOLEAN NOT NULL DEFAULT false,
    pull_cursor         TEXT NOT NULL DEFAULT '',
    last_push_at        TIMESTAMPTZ,
    last_full_push_at   TIMESTAMPTZ,
    last_pull_at        TIMESTAMPTZ,
    last_error          TEXT NOT NULL DEFAULT '',                -- error code only, never a payload
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX channel_connections_uidx ON channel_connections (marina_id, channel_code);

CREATE TABLE channel_reservations (
    id              UUID PRIMARY KEY,
    marina_id       UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    channel_code    TEXT NOT NULL,
    external_ref    TEXT NOT NULL CHECK (char_length(external_ref) BETWEEN 1 AND 128),
    contract_id     UUID,
    status          TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received','booked','rejected','cancelled')),
    reject_reason   TEXT CHECK (reject_reason IS NULL OR reject_reason IN ('no_space','rules','unmapped_category',
                        'modify_unsupported','invalid','not_found')),
    payload_sha256  TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id)
);
CREATE UNIQUE INDEX channel_reservations_uidx ON channel_reservations (marina_id, channel_code, external_ref);
CREATE INDEX channel_reservations_created_idx ON channel_reservations (marina_id, created_at);

CREATE TABLE channel_events (
    id              UUID PRIMARY KEY,
    marina_id       UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    channel_code    TEXT NOT NULL,
    event_id        TEXT NOT NULL CHECK (char_length(event_id) BETWEEN 1 AND 128),
    reservation_id  UUID,                                        -- channel_reservations.id (plain column)
    status          TEXT NOT NULL CHECK (status IN ('applied','ignored')),
    received_at     TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX channel_events_uidx ON channel_events (marina_id, channel_code, event_id);
CREATE INDEX channel_events_received_idx ON channel_events (marina_id, received_at);

-- +goose Down
UPDATE contracts SET confirmation_source = 'provider' WHERE confirmation_source = 'channel';
DELETE FROM consents WHERE kind = 'terms';
ALTER TABLE contracts DROP CONSTRAINT contracts_confirmation_source_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_confirmation_source_check
    CHECK (confirmation_source IS NULL OR confirmation_source IN ('manual','provider'));
ALTER TABLE consents DROP CONSTRAINT consents_kind_check;
ALTER TABLE consents ADD CONSTRAINT consents_kind_check CHECK (kind IN ('privacy','marketing','third_party'));

DROP TABLE channel_events;
DROP TABLE channel_reservations;
DROP TABLE channel_connections;
DROP TABLE customer_portal_sessions;
DROP TABLE customer_portal_links;
DROP TABLE online_quota_claims;
DROP TABLE online_quota_usage;
DROP TABLE online_booking_documents;
DROP TABLE online_bookings;
DROP TRIGGER booking_texts_immutable ON booking_texts;
DROP FUNCTION porto_booking_texts_immutable();
DROP TABLE booking_texts;
DROP TABLE online_booking_settings;
DROP INDEX contracts_marina_channel_status_idx;
ALTER TABLE payment_intents DROP COLUMN voided_at;
ALTER TABLE payment_intents DROP COLUMN captured_at;
ALTER TABLE payment_intents DROP COLUMN authorized_at;
ALTER TABLE payment_intents DROP COLUMN capture_method;
ALTER TABLE documents DROP COLUMN verified_by_user_id;
ALTER TABLE documents DROP COLUMN verified_at;
ALTER TABLE dynamic_areas DROP COLUMN online_sellable;
ALTER TABLE berth_groups DROP COLUMN online_quota_value;
ALTER TABLE berth_groups DROP COLUMN online_quota_kind;
ALTER TABLE consents DROP COLUMN text_sha256;
