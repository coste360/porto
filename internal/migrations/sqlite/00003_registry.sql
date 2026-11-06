-- Porto M1 registry schema (SQLite): customers, consents, vessels, vessel parties, documents, deadlines and
-- deadline policies (spec 06 §6.4, 11; m1-plan §3.1). The PostgreSQL twin is
-- internal/migrations/postgres/00003_registry.sql: keep table, column and index names identical.

-- +goose Up
CREATE TABLE customers (
    id               TEXT PRIMARY KEY,
    marina_id        TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind             TEXT NOT NULL CHECK (kind IN ('person','company')),
    first_name       TEXT NOT NULL DEFAULT '',
    last_name        TEXT NOT NULL DEFAULT '',
    company_name     TEXT NOT NULL DEFAULT '',
    fiscal_code      TEXT CHECK (fiscal_code IS NULL OR fiscal_code = upper(fiscal_code)),
    vat_number       TEXT,                        -- with optional ISO country prefix, upper case, no spaces
    sdi_code         TEXT,
    pec              TEXT CHECK (pec IS NULL OR pec = lower(pec)),
    billing_address  TEXT NOT NULL DEFAULT '{}',  -- Address JSON (same schema as organizations)
    nationality      TEXT CHECK (nationality IS NULL OR length(nationality) = 2),
    locale           TEXT NOT NULL DEFAULT 'it',
    emails           TEXT NOT NULL DEFAULT '[]',  -- [{label, value}] value lower case
    phones           TEXT NOT NULL DEFAULT '[]',  -- [{label, value}] value E.164 when parseable, else digits
    category         TEXT CHECK (category IS NULL OR category IN ('member','resident','affiliated','charter','yard')),
    tags             TEXT NOT NULL DEFAULT '[]',
    payment_terms    TEXT,                        -- {"days":30,"end_of_month":true}; used from M5
    iban_enc         TEXT,                        -- KeyRing, AAD "iban:<id>"
    iban_last4       TEXT,
    notes            TEXT NOT NULL DEFAULT '',
    search_text      TEXT NOT NULL DEFAULT '',    -- lower-case, accent-free: names, fiscal code, VAT, emails, phone digits
    merged_into_id   TEXT,                         -- same marina: composite FK below
    anonymized_at    TIMESTAMP,
    archived_at      TIMESTAMP,
    version          INTEGER NOT NULL DEFAULT 1,
    created_at       TIMESTAMP NOT NULL,
    updated_at       TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    -- Customers are never hard-deleted (merge archives, erasure anonymises); deferred for the restore order.
    FOREIGN KEY (merged_into_id, marina_id) REFERENCES customers (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (kind <> 'person' OR length(last_name) >= 1 OR anonymized_at IS NOT NULL),
    CHECK (kind <> 'company' OR length(company_name) >= 1 OR anonymized_at IS NOT NULL)
);
CREATE INDEX customers_marina_name_idx ON customers (marina_id, last_name, first_name);
CREATE INDEX customers_marina_company_idx ON customers (marina_id, company_name);
CREATE INDEX customers_marina_fiscal_idx ON customers (marina_id, fiscal_code);
CREATE INDEX customers_marina_vat_idx ON customers (marina_id, vat_number);

CREATE TABLE consents (                           -- versioned, insert-only by API (no update operation)
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL,
    customer_id         TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('privacy','marketing','third_party')),
    policy_version      TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),
    granted             INTEGER NOT NULL CHECK (granted IN (0,1)),
    source              TEXT NOT NULL CHECK (source IN ('desk','online','import','api')),
    recorded_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    recorded_at         TIMESTAMP NOT NULL,
    created_at          TIMESTAMP NOT NULL,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX consents_marina_customer_idx ON consents (marina_id, customer_id, kind, recorded_at);

CREATE TABLE vessels (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                  TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    registration          TEXT,                   -- matricola / numero di iscrizione
    registry_office       TEXT,
    flag                  TEXT CHECK (flag IS NULL OR length(flag) = 2),
    home_port             TEXT,
    hull_type             TEXT NOT NULL DEFAULT 'other' CHECK (hull_type IN ('monohull_motor','monohull_sail','catamaran','trimaran','rib','gozzo','other')),
    model                 TEXT,
    builder               TEXT,
    year_built            INTEGER CHECK (year_built IS NULL OR year_built BETWEEN 1800 AND 2200),
    hull_color            TEXT,
    hull_material         TEXT,
    loa_cm                INTEGER NOT NULL CHECK (loa_cm > 0),
    lwl_cm                INTEGER CHECK (lwl_cm IS NULL OR lwl_cm > 0),
    beam_cm               INTEGER NOT NULL CHECK (beam_cm > 0),
    draft_cm              INTEGER CHECK (draft_cm IS NULL OR draft_cm > 0),
    air_draft_cm          INTEGER CHECK (air_draft_cm IS NULL OR air_draft_cm > 0),
    displacement_kg       INTEGER CHECK (displacement_kg IS NULL OR displacement_kg > 0),
    measured_loa_cm       INTEGER CHECK (measured_loa_cm IS NULL OR measured_loa_cm > 0),
    measured_beam_cm      INTEGER CHECK (measured_beam_cm IS NULL OR measured_beam_cm > 0),
    measured_draft_cm     INTEGER CHECK (measured_draft_cm IS NULL OR measured_draft_cm > 0),
    measured_air_draft_cm INTEGER CHECK (measured_air_draft_cm IS NULL OR measured_air_draft_cm > 0),
    measured_at           TIMESTAMP,
    measured_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    dimensions_source     TEXT NOT NULL DEFAULT 'declared' CHECK (dimensions_source IN ('declared','measured')),
    engines               TEXT NOT NULL DEFAULT '[]',  -- [{make, model, power_kw, power_hp, serial, hours}]
    fuel                  TEXT CHECK (fuel IS NULL OR fuel IN ('petrol','diesel','electric','hybrid','none','other')),
    power_required_a      INTEGER CHECK (power_required_a IS NULL OR power_required_a IN (16,32,63,125)),
    notes                 TEXT NOT NULL DEFAULT '',
    search_text           TEXT NOT NULL DEFAULT '',     -- lower-case name + registration
    archived_at           TIMESTAMP,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    CHECK (dimensions_source = 'declared' OR (measured_loa_cm IS NOT NULL AND measured_beam_cm IS NOT NULL))
);
CREATE INDEX vessels_marina_name_idx ON vessels (marina_id, name);
CREATE INDEX vessels_marina_registration_idx ON vessels (marina_id, registration);

CREATE TABLE vessel_parties (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL,
    vessel_id     TEXT NOT NULL,
    customer_id   TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner','operator','skipper','billing','contact')),
    share_millis  INTEGER CHECK (share_millis IS NULL OR share_millis BETWEEN 1 AND 1000),
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE RESTRICT,
    CHECK ((role = 'owner') = (share_millis IS NOT NULL))
);
CREATE UNIQUE INDEX vessel_parties_uidx ON vessel_parties (vessel_id, customer_id, role);
CREATE INDEX vessel_parties_marina_customer_idx ON vessel_parties (marina_id, customer_id);

CREATE TABLE documents (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    owner_type            TEXT NOT NULL CHECK (owner_type IN ('customer','vessel')),
    customer_id           TEXT,
    vessel_id             TEXT,
    type                  TEXT NOT NULL CHECK (type IN ('identity_card','passport','driving_licence','boating_licence',
                              'navigation_licence','registration_certificate','insurance_policy','safety_certificate',
                              'power_declaration','signed_contract','proxy','photo','other')),
    number_enc            TEXT,                   -- KeyRing, AAD "document-number:<id>"
    issuer                TEXT,                   -- issuing office / insurer
    issued_on             DATE,
    expires_on            DATE,
    file_id               TEXT,
    file_name             TEXT,                   -- sanitised original name, max 120 chars
    notes                 TEXT NOT NULL DEFAULT '',
    retention_until       DATE,                   -- M3 cleanup job
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (file_id, marina_id) REFERENCES stored_files (id, marina_id),
    CHECK ((owner_type = 'customer' AND customer_id IS NOT NULL AND vessel_id IS NULL)
        OR (owner_type = 'vessel' AND vessel_id IS NOT NULL AND customer_id IS NULL)),
    CHECK (issued_on IS NULL OR expires_on IS NULL OR issued_on <= expires_on)
);
CREATE INDEX documents_marina_customer_idx ON documents (marina_id, customer_id);
CREATE INDEX documents_marina_vessel_idx ON documents (marina_id, vessel_id);
CREATE INDEX documents_marina_expires_idx ON documents (marina_id, expires_on);

CREATE TABLE deadlines (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    source_type  TEXT NOT NULL,                   -- 'document' in M1; contract, installment, right, maintenance later (validated in Go)
    source_id    TEXT NOT NULL,
    kind         TEXT NOT NULL,                   -- deadline.Kind (validated in Go, extended in later milestones)
    customer_id  TEXT,
    vessel_id    TEXT,
    due_on       DATE NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','dismissed')),
    note         TEXT NOT NULL DEFAULT '',
    notified_at  TEXT NOT NULL DEFAULT '[]',      -- M4
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX deadlines_source_uidx ON deadlines (source_type, source_id, kind);
CREATE INDEX deadlines_marina_status_due_idx ON deadlines (marina_id, status, due_on);

CREATE TABLE deadline_policies (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    lead_days   TEXT NOT NULL DEFAULT '[60,15]',  -- sorted desc, each 1..730
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX deadline_policies_marina_kind_uidx ON deadline_policies (marina_id, kind);

-- +goose Down
DROP TABLE deadline_policies;
DROP TABLE deadlines;
DROP TABLE documents;
DROP TABLE vessel_parties;
DROP TABLE vessels;
DROP TABLE consents;
DROP TABLE customers;
