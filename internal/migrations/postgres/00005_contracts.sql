-- Porto M3 contracts schema (PostgreSQL): occupancy settings, number sequences, inquiries, quotes with their options,
-- contracts (mooring and transit) and contract lines (spec 06, 13; m3-plan §5.1). quote_option_placements is in
-- 00006_occupancy (it references allocations). The SQLite twin is internal/migrations/sqlite/00005_contracts.sql:
-- keep table, column, index and constraint names identical.

-- +goose Up
CREATE TABLE occupancy_settings (
    marina_id              UUID PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    safety_clearance_cm    BIGINT NOT NULL DEFAULT 30 CHECK (safety_clearance_cm BETWEEN 0 AND 300),
    waste_threshold_bp     INTEGER NOT NULL DEFAULT 6000 CHECK (waste_threshold_bp BETWEEN 0 AND 10000),
    draft_hold_days        INTEGER NOT NULL DEFAULT 7 CHECK (draft_hold_days BETWEEN 1 AND 60),
    desk_hold_hours        INTEGER NOT NULL DEFAULT 24 CHECK (desk_hold_hours BETWEEN 1 AND 168),
    quote_validity_days    INTEGER NOT NULL DEFAULT 7 CHECK (quote_validity_days BETWEEN 1 AND 120),
    max_segments           INTEGER NOT NULL DEFAULT 1 CHECK (max_segments BETWEEN 0 AND 2),
    absent_days_threshold  INTEGER NOT NULL DEFAULT 7 CHECK (absent_days_threshold BETWEEN 1 AND 365),
    arrival_grace_minutes  INTEGER NOT NULL DEFAULT 120 CHECK (arrival_grace_minutes BETWEEN 0 AND 1440),
    default_arrival_time   TEXT NOT NULL DEFAULT '14:00' CHECK (char_length(default_arrival_time) = 5),
    default_departure_time TEXT NOT NULL DEFAULT '11:00' CHECK (char_length(default_departure_time) = 5),
    reprice_on_move        BOOLEAN NOT NULL DEFAULT false,
    release_policy         TEXT NOT NULL DEFAULT 'with_consent' CHECK (release_policy IN ('never','with_consent')),
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL
);

CREATE TABLE number_sequences (
    marina_id    UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    series       TEXT NOT NULL CHECK (series IN ('mooring','transit','quote','storage','charter')),
    year         INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    next_number  BIGINT NOT NULL DEFAULT 1 CHECK (next_number >= 1),
    PRIMARY KEY (marina_id, series, year)
);

CREATE TABLE inquiries (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    channel             TEXT NOT NULL DEFAULT 'desk' CHECK (channel IN ('desk','online','api','external')),
    source              TEXT NOT NULL CHECK (source IN ('phone','email','desk','public','channel')),
    purpose             TEXT NOT NULL CHECK (purpose IN ('transit','contract')),
    term                TEXT CHECK (term IS NULL OR term IN ('annual','seasonal','monthly','period')),
    vessel_id           UUID,
    customer_id         UUID,
    loa_cm              BIGINT NOT NULL CHECK (loa_cm > 0),
    beam_cm             BIGINT NOT NULL CHECK (beam_cm > 0),
    draft_cm            BIGINT CHECK (draft_cm IS NULL OR draft_cm > 0),
    arrival             DATE NOT NULL,
    departure           DATE NOT NULL,
    outcome             TEXT NOT NULL DEFAULT 'open' CHECK (outcome IN ('open','quoted','booked','declined_no_space','declined_size','declined_other','lost')),
    quote_id            UUID,                     -- FK added after quotes; restored late (internal/backup)
    contract_id         UUID,                     -- FK added after contracts; restored late (internal/backup)
    notes               TEXT NOT NULL DEFAULT '',
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    CHECK (departure > arrival)
);
CREATE INDEX inquiries_marina_outcome_idx ON inquiries (marina_id, outcome, arrival);

CREATE TABLE quotes (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number              BIGINT NOT NULL CHECK (number >= 1),
    year                INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','sent','accepted','declined','expired')),
    kind                TEXT NOT NULL CHECK (kind IN ('mooring','transit')),
    term                TEXT CHECK (term IS NULL OR term IN ('annual','seasonal','monthly','period')),
    customer_id         UUID NOT NULL,
    vessel_id           UUID NOT NULL,
    starts_on           DATE NOT NULL,
    ends_on             DATE NOT NULL,            -- exclusive (departure)
    valid_until         DATE NOT NULL,
    expires_at          TIMESTAMPTZ NOT NULL,       -- end of valid_until in the marina time zone
    channel             TEXT NOT NULL DEFAULT 'desk' CHECK (channel IN ('desk','online','api','external')),
    policy              JSONB NOT NULL DEFAULT '{}',
    notes               TEXT NOT NULL DEFAULT '',
    inquiry_id          UUID,
    accepted_option_id  UUID,                     -- quote_options.id (no FK: options cascade from the quote); restored late
    contract_id         UUID,                     -- FK added after contracts; restored late (internal/backup)
    sent_at             TIMESTAMPTZ,
    decided_at          TIMESTAMPTZ,
    decline_reason      TEXT NOT NULL DEFAULT '',
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (inquiry_id, marina_id) REFERENCES inquiries (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK ((kind = 'mooring') = (term IS NOT NULL)),
    CHECK (ends_on > starts_on)
);
CREATE UNIQUE INDEX quotes_number_uidx ON quotes (marina_id, year, number);
CREATE INDEX quotes_marina_status_idx ON quotes (marina_id, status, expires_at);
CREATE INDEX quotes_marina_customer_idx ON quotes (marina_id, customer_id);

CREATE TABLE quote_options (
    id              UUID PRIMARY KEY,
    marina_id       UUID NOT NULL,
    quote_id        UUID NOT NULL,
    seq             INTEGER NOT NULL CHECK (seq BETWEEN 1 AND 3),
    label           TEXT NOT NULL DEFAULT '' CHECK (char_length(label) <= 120),
    hold            BOOLEAN NOT NULL DEFAULT false,
    price_list_id   UUID,
    price_snapshot  JSONB,                         -- pricing.Snapshot JSON
    engine_version  INTEGER NOT NULL DEFAULT 0,
    total_cents     BIGINT NOT NULL DEFAULT 0,
    deposit_cents   BIGINT NOT NULL DEFAULT 0,
    fit_override    JSONB,                         -- {user_id, reason, codes[]}
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (quote_id, marina_id) REFERENCES quotes (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id)
);
CREATE UNIQUE INDEX quote_options_seq_uidx ON quote_options (quote_id, seq);
CREATE INDEX quote_options_marina_idx ON quote_options (marina_id, quote_id);

CREATE TABLE contracts (
    id                       UUID PRIMARY KEY,
    marina_id                UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                     TEXT NOT NULL CHECK (kind IN ('mooring','transit','storage','charter')),
    term                     TEXT CHECK (term IS NULL OR term IN ('annual','seasonal','monthly','period')),
    number                   BIGINT NOT NULL CHECK (number >= 1),
    year                     INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    status                   TEXT NOT NULL CHECK (status IN ('draft','awaiting_signature','active','expired','renewed',
                               'terminated','cancelled','suspended','held','pending_payment','confirmed','arrived',
                               'departed','no_show','awaiting_approval')),
    customer_id              UUID,
    billing_customer_id      UUID,
    vessel_id                UUID NOT NULL,
    starts_on                DATE NOT NULL,
    ends_on                  DATE NOT NULL,       -- exclusive (departure / end date)
    channel                  TEXT NOT NULL DEFAULT 'desk' CHECK (channel IN ('desk','online','api','external')),
    quote_id                 UUID,
    price_list_id            UUID,
    price_snapshot           JSONB,                -- pricing.Snapshot JSON, never recomputed (INV-20)
    engine_version  INTEGER NOT NULL DEFAULT 0,
    total_cents              BIGINT NOT NULL DEFAULT 0,
    deposit_cents            BIGINT NOT NULL DEFAULT 0,
    policy                   JSONB NOT NULL DEFAULT '{}',   -- m3-plan §4.7
    expected_arrival_time    TEXT CHECK (expected_arrival_time IS NULL OR char_length(expected_arrival_time) = 5),
    expected_departure_time  TEXT CHECK (expected_departure_time IS NULL OR char_length(expected_departure_time) = 5),
    notes                    TEXT NOT NULL DEFAULT '',
    hold_lost                BOOLEAN NOT NULL DEFAULT false,
    activated_at             TIMESTAMPTZ,
    signed_on_paper          BOOLEAN NOT NULL DEFAULT false,
    confirmed_at             TIMESTAMPTZ,
    confirmed_by_user_id     UUID REFERENCES users (id) ON DELETE SET NULL,
    confirmation_source      TEXT CHECK (confirmation_source IS NULL OR confirmation_source IN ('manual','provider')),
    confirmation_ref         TEXT,                -- free text, never logged
    pay_at_departure         BOOLEAN NOT NULL DEFAULT false,
    cancelled_at             TIMESTAMPTZ,
    cancel_reason            TEXT NOT NULL DEFAULT '',
    terminated_on            DATE,
    terminate_reason         TEXT NOT NULL DEFAULT '',
    created_by_user_id       UUID REFERENCES users (id) ON DELETE SET NULL,
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ NOT NULL,
    updated_at               TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (billing_customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (quote_id, marina_id) REFERENCES quotes (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id),
    CHECK (ends_on > starts_on),
    CONSTRAINT contracts_kind_status_check CHECK (
        (kind = 'transit' AND status IN ('held','pending_payment','awaiting_approval','confirmed','arrived','departed',
                                         'cancelled','expired','no_show'))
        OR (kind <> 'transit' AND status IN ('draft','awaiting_signature','active','expired','renewed','terminated',
                                             'cancelled','suspended'))),
    CONSTRAINT contracts_mooring_term_check CHECK (kind <> 'mooring' OR term IS NOT NULL),
    CONSTRAINT contracts_customer_check CHECK (status NOT IN ('confirmed','arrived','departed','no_show') OR customer_id IS NOT NULL)
);
CREATE UNIQUE INDEX contracts_number_uidx ON contracts (marina_id, kind, year, number);
CREATE INDEX contracts_marina_kind_status_idx ON contracts (marina_id, kind, status, starts_on);
CREATE INDEX contracts_marina_vessel_idx ON contracts (marina_id, vessel_id);
CREATE INDEX contracts_marina_customer_idx ON contracts (marina_id, customer_id);

CREATE TABLE contract_lines (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL,
    contract_id    UUID NOT NULL,
    seq            INTEGER NOT NULL CHECK (seq >= 1),
    line_kind      TEXT NOT NULL CHECK (line_kind IN ('resource','service')),
    item_id        UUID,
    resource_type  TEXT CHECK (resource_type IS NULL OR resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id    UUID,
    qty_milli      BIGINT NOT NULL DEFAULT 1000,
    amount_cents   BIGINT NOT NULL DEFAULT 0,  -- informative share of the snapshot
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CHECK ((line_kind = 'resource') = (resource_type IS NOT NULL))
);
CREATE UNIQUE INDEX contract_lines_seq_uidx ON contract_lines (contract_id, seq);
CREATE INDEX contract_lines_marina_idx ON contract_lines (marina_id, contract_id);

-- Forward references (SQLite accepts them inline; PostgreSQL needs the target table first).
ALTER TABLE inquiries ADD CONSTRAINT inquiries_quote_fk FOREIGN KEY (quote_id, marina_id)
    REFERENCES quotes (id, marina_id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE inquiries ADD CONSTRAINT inquiries_contract_fk FOREIGN KEY (contract_id, marina_id)
    REFERENCES contracts (id, marina_id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE quotes ADD CONSTRAINT quotes_contract_fk FOREIGN KEY (contract_id, marina_id)
    REFERENCES contracts (id, marina_id) DEFERRABLE INITIALLY DEFERRED;

-- +goose Down
ALTER TABLE quotes DROP CONSTRAINT quotes_contract_fk;
ALTER TABLE inquiries DROP CONSTRAINT inquiries_contract_fk;
ALTER TABLE inquiries DROP CONSTRAINT inquiries_quote_fk;
DROP TABLE contract_lines;
DROP TABLE contracts;
DROP TABLE quote_options;
DROP TABLE quotes;
DROP TABLE inquiries;
DROP TABLE number_sequences;
DROP TABLE occupancy_settings;
