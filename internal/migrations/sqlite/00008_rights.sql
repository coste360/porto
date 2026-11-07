-- Porto M4 rights schema (SQLite): concession and obligations, berth rights with holders, authorised vessels and
-- owner allocations, transfers, management mandates and their revenues, management fee tables and runs, millesimal
-- tables, common charge budgets, runs and allocations, holder instalments (spec 06, 14; m4-plan §4, §5.2). The twin
-- is internal/migrations/postgres/00008_rights.sql: keep table, column, index and constraint names identical.
--
-- INV-40: holder shares sum to 1000 (checked in Go in the tx of every write; each share is 1..1000 here).
-- INV-41: a millesimal table is active only at sum 1000 (Go); entries of a non-draft table are frozen (trigger).
-- INV-44: allocations of an issued common charge run and lines of an issued fee run are frozen (triggers).

-- +goose Up
CREATE TABLE concessions (
    id                TEXT PRIMARY KEY,
    marina_id         TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    authority         TEXT NOT NULL DEFAULT '',
    deed_kind         TEXT NOT NULL DEFAULT '',
    deed_number       TEXT NOT NULL DEFAULT '',
    granted_on        DATE,
    expires_on        DATE,
    annual_fee_cents  INTEGER NOT NULL DEFAULT 0 CHECK (annual_fee_cents >= 0),
    surfaces          TEXT NOT NULL DEFAULT '[]',
    prescriptions     TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','revoked')),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX concessions_marina_uidx ON concessions (marina_id);

CREATE TABLE concession_obligations (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    concession_id  TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('fee_instalment','deposit','insurance','periodic_report','other')),
    description    TEXT NOT NULL DEFAULT '',
    due_on         DATE NOT NULL,
    recurrence     TEXT NOT NULL DEFAULT 'none' CHECK (recurrence IN ('none','yearly')),
    amount_cents   INTEGER CHECK (amount_cents IS NULL OR amount_cents >= 0),
    done_on        DATE,
    version        INTEGER NOT NULL DEFAULT 1,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (concession_id, marina_id) REFERENCES concessions (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX concession_obligations_marina_idx ON concession_obligations (marina_id, due_on);

CREATE TABLE berth_rights (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    berth_id            TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('ownership_share','long_term_use','membership')),
    from_on             DATE NOT NULL,
    to_on               DATE NOT NULL,                        -- inclusive last night
    title               TEXT NOT NULL DEFAULT '{}',          -- {deed, number, notary, date}
    status              TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','transferred','ended','cancelled')),
    end_reason          TEXT NOT NULL DEFAULT '',
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (berth_id, marina_id) REFERENCES berths (id, marina_id),
    CHECK (to_on >= from_on)
);
CREATE INDEX berth_rights_berth_idx ON berth_rights (marina_id, berth_id, from_on);
CREATE INDEX berth_rights_status_idx ON berth_rights (marina_id, status, to_on);

CREATE TABLE berth_right_holders (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    right_id      TEXT NOT NULL,
    customer_id   TEXT NOT NULL,
    share_millis  INTEGER NOT NULL CHECK (share_millis BETWEEN 1 AND 1000),
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX berth_right_holders_uidx ON berth_right_holders (right_id, customer_id);
CREATE INDEX berth_right_holders_customer_idx ON berth_right_holders (marina_id, customer_id);

CREATE TABLE berth_right_vessels (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    right_id      TEXT NOT NULL,
    vessel_id     TEXT NOT NULL,
    is_primary    INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0,1)),
    fit           TEXT,                                      -- fit.Result of the last check
    fit_override  TEXT,                                      -- {user_id, reason, codes[]}
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id)
);
CREATE UNIQUE INDEX berth_right_vessels_uidx ON berth_right_vessels (right_id, vessel_id);
CREATE UNIQUE INDEX berth_right_vessels_primary_uidx ON berth_right_vessels (right_id) WHERE is_primary = 1;
CREATE INDEX berth_right_vessels_marina_idx ON berth_right_vessels (marina_id, vessel_id);

CREATE TABLE berth_right_allocations (
    allocation_id  TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    right_id       TEXT NOT NULL,
    created_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id) ON DELETE RESTRICT
);
CREATE INDEX berth_right_allocations_right_idx ON berth_right_allocations (marina_id, right_id);

CREATE TABLE right_transfers (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    right_id              TEXT NOT NULL,
    effective_on          DATE NOT NULL,
    to_holders            TEXT NOT NULL DEFAULT '[]',        -- [{customer_id, share_millis}]
    declared_price_cents  INTEGER CHECK (declared_price_cents IS NULL OR declared_price_cents >= 0),
    takeover_item_id      TEXT,
    takeover_fee_cents    INTEGER NOT NULL DEFAULT 0 CHECK (takeover_fee_cents >= 0),
    requirement_policy    TEXT NOT NULL DEFAULT 'warn' CHECK (requirement_policy IN ('block','warn')),
    requirement_issues    TEXT NOT NULL DEFAULT '[]',
    fees_policy           TEXT NOT NULL DEFAULT 'reassign' CHECK (fees_policy IN ('reassign','prorata')),
    status                TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','awaiting_signature','completed','cancelled')),
    new_right_id          TEXT,
    document_id           TEXT,                                -- generated_documents.id (00009, no FK: forward)
    signature_request_id  TEXT,                                -- signature_requests.id (00009, no FK: forward)
    completed_at          TIMESTAMP,
    cancelled_at          TIMESTAMP,
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    FOREIGN KEY (new_right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    FOREIGN KEY (takeover_item_id, marina_id) REFERENCES items (id, marina_id)
);
CREATE INDEX right_transfers_right_idx ON right_transfers (marina_id, right_id, status);

CREATE TABLE management_mandates (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    right_id              TEXT NOT NULL,
    from_on               DATE NOT NULL,
    to_on                 DATE NOT NULL,                      -- inclusive last night
    owner_share_bp        INTEGER NOT NULL CHECK (owner_share_bp BETWEEN 0 AND 10000),
    min_guaranteed_cents  INTEGER NOT NULL DEFAULT 0 CHECK (min_guaranteed_cents >= 0),
    channels              TEXT NOT NULL DEFAULT '["transit","period"]',
    settlement            TEXT NOT NULL DEFAULT 'credit' CHECK (settlement IN ('credit','transfer')),
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','ended','cancelled')),
    ended_on              DATE,
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    CHECK (to_on >= from_on)
);
CREATE INDEX management_mandates_right_idx ON management_mandates (marina_id, right_id, from_on);
CREATE INDEX management_mandates_status_idx ON management_mandates (marina_id, status, to_on);

CREATE TABLE mandate_revenues (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    mandate_id         TEXT NOT NULL,
    allocation_id      TEXT NOT NULL,
    contract_id        TEXT,
    nights             INTEGER NOT NULL DEFAULT 0 CHECK (nights >= 0),
    revenue_cents      INTEGER NOT NULL DEFAULT 0,
    owner_share_cents  INTEGER NOT NULL DEFAULT 0,
    through_on         DATE NOT NULL,
    settled_at         TIMESTAMP,                                   -- M5 compensation
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (mandate_id, marina_id) REFERENCES management_mandates (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id)
);
CREATE UNIQUE INDEX mandate_revenues_uidx ON mandate_revenues (mandate_id, allocation_id);
CREATE INDEX mandate_revenues_marina_idx ON mandate_revenues (marina_id, mandate_id);

CREATE TABLE management_fee_tables (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    year        INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    basis       TEXT NOT NULL CHECK (basis IN ('per_category','per_m2_berth','per_length_band')),
    istat_bp    INTEGER NOT NULL DEFAULT 0 CHECK (istat_bp BETWEEN -5000 AND 5000),
    status      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active')),
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE INDEX management_fee_tables_marina_idx ON management_fee_tables (marina_id, year);

CREATE TABLE management_fee_table_rows (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    table_id           TEXT NOT NULL,
    category_id        TEXT,                                   -- per_category
    from_cm            INTEGER,                                  -- per_length_band, inclusive
    to_cm              INTEGER,                                  -- per_length_band, exclusive
    amount_cents       INTEGER CHECK (amount_cents IS NULL OR amount_cents >= 0),
    rate_cents_per_m2  INTEGER CHECK (rate_cents_per_m2 IS NULL OR rate_cents_per_m2 >= 0),
    sort               INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (table_id, marina_id) REFERENCES management_fee_tables (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, marina_id) REFERENCES berth_categories (id, marina_id),
    CONSTRAINT management_fee_table_rows_amount_check CHECK (amount_cents IS NOT NULL OR rate_cents_per_m2 IS NOT NULL),
    CONSTRAINT management_fee_table_rows_band_check CHECK (from_cm IS NULL OR to_cm IS NULL OR to_cm > from_cm)
);
CREATE INDEX management_fee_table_rows_marina_idx ON management_fee_table_rows (marina_id, table_id);

CREATE TABLE management_fee_runs (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    year                INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    table_id            TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','simulated','issued','discarded')),
    plan                TEXT NOT NULL DEFAULT '{}',           -- same schema as contract policy.payment
    totals              TEXT NOT NULL DEFAULT '{}',
    simulated_at        TIMESTAMP,
    issued_at           TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (table_id, marina_id) REFERENCES management_fee_tables (id, marina_id)
);
CREATE UNIQUE INDEX management_fee_runs_issued_uidx ON management_fee_runs (marina_id, year, table_id) WHERE status = 'issued';
CREATE INDEX management_fee_runs_marina_idx ON management_fee_runs (marina_id, year, status);

CREATE TABLE management_fee_lines (
    id                TEXT PRIMARY KEY,
    marina_id         TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    run_id            TEXT NOT NULL,
    right_id          TEXT NOT NULL,
    berth_id          TEXT NOT NULL,
    category          TEXT NOT NULL DEFAULT '',
    base_cents        INTEGER NOT NULL DEFAULT 0,
    days              INTEGER NOT NULL DEFAULT 0 CHECK (days >= 0),
    amount_cents      INTEGER NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    previous_cents    INTEGER,
    holders           TEXT NOT NULL DEFAULT '[]',            -- [{customer_id, share_millis, amount_cents}]
    excluded          INTEGER NOT NULL DEFAULT 0 CHECK (excluded IN (0,1)),
    exclusion_reason  TEXT,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (run_id, marina_id) REFERENCES management_fee_runs (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    FOREIGN KEY (berth_id, marina_id) REFERENCES berths (id, marina_id)
);
CREATE UNIQUE INDEX management_fee_lines_uidx ON management_fee_lines (run_id, right_id);
CREATE INDEX management_fee_lines_marina_idx ON management_fee_lines (marina_id, run_id);

CREATE TABLE millesimal_tables (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    code          TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    status        TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    activated_at  TIMESTAMP,
    archived_at   TIMESTAMP,
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX millesimal_tables_code_uidx ON millesimal_tables (marina_id, code);

CREATE TABLE millesimal_entries (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    table_id    TEXT NOT NULL,
    berth_id    TEXT NOT NULL,
    millis      INTEGER NOT NULL CHECK (millis BETWEEN 0 AND 1000),
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (table_id, marina_id) REFERENCES millesimal_tables (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (berth_id, marina_id) REFERENCES berths (id, marina_id)
);
CREATE UNIQUE INDEX millesimal_entries_uidx ON millesimal_entries (table_id, berth_id);
CREATE INDEX millesimal_entries_marina_idx ON millesimal_entries (marina_id, table_id);

CREATE TABLE common_charge_budgets (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    year         INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    name         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','closed')),
    approved_at  TIMESTAMP,
    closed_at    TIMESTAMP,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX common_charge_budgets_year_uidx ON common_charge_budgets (marina_id, year);

CREATE TABLE common_charge_items (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    budget_id     TEXT NOT NULL,
    name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    table_id      TEXT NOT NULL,
    budget_cents  INTEGER NOT NULL DEFAULT 0,
    actual_cents  INTEGER,
    sort          INTEGER NOT NULL DEFAULT 0,
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (budget_id, marina_id) REFERENCES common_charge_budgets (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (table_id, marina_id) REFERENCES millesimal_tables (id, marina_id)
);
CREATE INDEX common_charge_items_marina_idx ON common_charge_items (marina_id, budget_id);

CREATE TABLE common_charge_runs (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    budget_id           TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('advance','balance')),
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    share_bp            INTEGER CHECK (share_bp IS NULL OR share_bp BETWEEN 0 AND 10000),
    reference_on        DATE NOT NULL,                        -- holders on this date (m4-plan §15 D17)
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','issued','discarded')),
    totals              TEXT NOT NULL DEFAULT '{}',
    plan                TEXT NOT NULL DEFAULT '{}',           -- holder instalment plan, same schema as policy.payment
    issued_at           TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (budget_id, marina_id) REFERENCES common_charge_budgets (id, marina_id),
    CONSTRAINT common_charge_runs_share_check CHECK ((kind = 'advance') = (share_bp IS NOT NULL))
);
CREATE UNIQUE INDEX common_charge_runs_seq_uidx ON common_charge_runs (budget_id, kind, seq);
CREATE UNIQUE INDEX common_charge_runs_balance_uidx ON common_charge_runs (budget_id) WHERE kind = 'balance' AND status = 'issued';
CREATE INDEX common_charge_runs_marina_idx ON common_charge_runs (marina_id, budget_id, status);

CREATE TABLE common_charge_allocations (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    run_id             TEXT NOT NULL,
    item_id            TEXT NOT NULL,
    berth_id           TEXT NOT NULL,
    right_id           TEXT,
    customer_id        TEXT,                                   -- NULL: the marina's own share (m4-plan §15 D18)
    berth_share_cents  INTEGER NOT NULL,
    amount_cents       INTEGER NOT NULL,
    created_at         TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (run_id, marina_id) REFERENCES common_charge_runs (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (item_id, marina_id) REFERENCES common_charge_items (id, marina_id),
    FOREIGN KEY (berth_id, marina_id) REFERENCES berths (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE INDEX common_charge_allocations_run_idx ON common_charge_allocations (run_id, item_id);
CREATE INDEX common_charge_allocations_customer_idx ON common_charge_allocations (marina_id, customer_id);

CREATE TABLE holder_installments (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    source_type          TEXT NOT NULL CHECK (source_type IN ('management_fee','common_advance','common_balance','transfer_fee')),
    source_id            TEXT NOT NULL,                        -- fee line, common run or transfer
    run_id               TEXT,                                 -- fee or common run (no FK: two parents)
    right_id             TEXT,
    customer_id          TEXT NOT NULL,
    seq                  INTEGER NOT NULL CHECK (seq >= 1),
    kind                 TEXT NOT NULL DEFAULT 'installment' CHECK (kind IN ('installment','credit')),
    issue_on             DATE NOT NULL,
    due_on               DATE NOT NULL,
    amount_cents         INTEGER NOT NULL,
    status               TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','issued','partially_paid','paid','cancelled')),
    issued_at            TIMESTAMP,
    overdue_notified_at  TIMESTAMP,
    charge_id            TEXT,                                 -- M5 charges (no FK in M4)
    settlement_source    TEXT CHECK (settlement_source IS NULL OR settlement_source IN ('manual','payment')),
    settlement_method    TEXT CHECK (settlement_method IS NULL OR settlement_method IN ('bank_transfer','cash','card_pos','cheque','other')),
    settlement_ref_enc   TEXT,                                -- KeyRing, AAD holder-installment-ref:<id>
    settled_on           DATE,
    settled_at           TIMESTAMP,
    settled_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    cancelled_at         TIMESTAMP,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    CHECK (issue_on <= due_on),
    CONSTRAINT holder_installments_amount_check CHECK (amount_cents >= 0 OR (source_type = 'common_balance' AND kind = 'credit')),
    CONSTRAINT holder_installments_paid_check CHECK (status <> 'paid' OR (settlement_source IS NOT NULL AND settled_at IS NOT NULL))
);
CREATE UNIQUE INDEX holder_installments_source_uidx ON holder_installments (source_type, source_id, customer_id, seq);
CREATE INDEX holder_installments_marina_status_idx ON holder_installments (marina_id, status, due_on);
CREATE INDEX holder_installments_issue_idx ON holder_installments (marina_id, issue_on) WHERE status = 'scheduled';
CREATE INDEX holder_installments_customer_idx ON holder_installments (marina_id, customer_id);

-- Frozen children (INV-41 edits, INV-44). The services check first (409 table_active / conflict); the triggers are
-- the backstop. A cascade from a deleted draft parent passes.
-- +goose StatementBegin
CREATE TRIGGER millesimal_entries_immutable_insert BEFORE INSERT ON millesimal_entries
WHEN EXISTS (SELECT 1 FROM millesimal_tables WHERE id = NEW.table_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'millesimal_table_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER millesimal_entries_immutable_update BEFORE UPDATE ON millesimal_entries
WHEN EXISTS (SELECT 1 FROM millesimal_tables WHERE id IN (OLD.table_id, NEW.table_id) AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'millesimal_table_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER millesimal_entries_immutable_delete BEFORE DELETE ON millesimal_entries
WHEN EXISTS (SELECT 1 FROM millesimal_tables WHERE id = OLD.table_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'millesimal_table_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER common_charge_allocations_immutable_insert BEFORE INSERT ON common_charge_allocations
WHEN EXISTS (SELECT 1 FROM common_charge_runs WHERE id = NEW.run_id AND status = 'issued')
BEGIN SELECT RAISE(ABORT, 'allocations_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER common_charge_allocations_immutable_update BEFORE UPDATE ON common_charge_allocations
WHEN EXISTS (SELECT 1 FROM common_charge_runs WHERE id IN (OLD.run_id, NEW.run_id) AND status = 'issued')
BEGIN SELECT RAISE(ABORT, 'allocations_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER common_charge_allocations_immutable_delete BEFORE DELETE ON common_charge_allocations
WHEN EXISTS (SELECT 1 FROM common_charge_runs WHERE id = OLD.run_id AND status = 'issued')
BEGIN SELECT RAISE(ABORT, 'allocations_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER management_fee_lines_immutable_insert BEFORE INSERT ON management_fee_lines
WHEN EXISTS (SELECT 1 FROM management_fee_runs WHERE id = NEW.run_id AND status = 'issued')
BEGIN SELECT RAISE(ABORT, 'allocations_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER management_fee_lines_immutable_update BEFORE UPDATE ON management_fee_lines
WHEN EXISTS (SELECT 1 FROM management_fee_runs WHERE id IN (OLD.run_id, NEW.run_id) AND status = 'issued')
BEGIN SELECT RAISE(ABORT, 'allocations_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER management_fee_lines_immutable_delete BEFORE DELETE ON management_fee_lines
WHEN EXISTS (SELECT 1 FROM management_fee_runs WHERE id = OLD.run_id AND status = 'issued')
BEGIN SELECT RAISE(ABORT, 'allocations_immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER management_fee_lines_immutable_delete;
DROP TRIGGER management_fee_lines_immutable_update;
DROP TRIGGER management_fee_lines_immutable_insert;
DROP TRIGGER common_charge_allocations_immutable_delete;
DROP TRIGGER common_charge_allocations_immutable_update;
DROP TRIGGER common_charge_allocations_immutable_insert;
DROP TRIGGER millesimal_entries_immutable_delete;
DROP TRIGGER millesimal_entries_immutable_update;
DROP TRIGGER millesimal_entries_immutable_insert;
DROP TABLE holder_installments;
DROP TABLE common_charge_allocations;
DROP TABLE common_charge_runs;
DROP TABLE common_charge_items;
DROP TABLE common_charge_budgets;
DROP TABLE millesimal_entries;
DROP TABLE millesimal_tables;
DROP TABLE management_fee_lines;
DROP TABLE management_fee_runs;
DROP TABLE management_fee_table_rows;
DROP TABLE management_fee_tables;
DROP TABLE mandate_revenues;
DROP TABLE management_mandates;
DROP TABLE right_transfers;
DROP TABLE berth_right_allocations;
DROP TABLE berth_right_vessels;
DROP TABLE berth_right_holders;
DROP TABLE berth_rights;
DROP TABLE concession_obligations;
DROP TABLE concessions;
