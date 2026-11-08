-- Porto M10 yard catalogue and yard quotes schema (SQLite) (spec 18.1-18.3; m10-plan §3, §4, §15, ADR 0015). The
-- twin is internal/migrations/postgres/00020_yard_catalog_quotes.sql: keep table, column, index and constraint names identical.
--
-- yard_settings: per-marina yard defaults (missing row = defaults): formula test vessel, quote validity and deposit,
--     advance at acceptance, external cost markup and item, billing of external costs in fixed mode, lifting window.
-- work_types: taxonomy of yard work used for margins (m10-plan D8).
-- yard_qualifications: trades (hand, painter, ...), each with its labour item (sale rate in the price lists) and a
--     default internal cost for estimates (D2).
-- yard_item_settings: yard data of an M2 item: internal cost, default quantity/price formulas, qualification of a
--     labour item (one labour item per qualification).
-- formula_variables / formula_tables: derived and job variables and band tables of the formula language (ADR 0006).
-- work_packages / work_package_versions: packages of lines with formulas; versions are insert-only (INV-28), so an
--     old quote keeps its formula.
-- yard_quotes / yard_quote_lines: guided quotes (separate from mooring quotes, D3) with the full snapshot used by
--     recompute. A variation quote points to its work order (validated in Go: work_orders comes in 00021, D27).
-- CHECK changes: number_sequences.series gains 'yard_quote' and 'work_order'; generated_documents.subject_type gains
--     'yard_quote' and 'work_order' (D12). SQLite edits the table sql with writable_schema as 00016 does (RESET reloads
--     this connection's schema; the DDL that follows bumps the schema cookie for the others); PostgreSQL drops and
--     re-adds the generated constraints.

-- +goose Up
PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'')',
                       'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'',''yard_quote'',''work_order'')')
WHERE type = 'table' AND name = 'number_sequences';
UPDATE sqlite_schema
SET sql = replace(sql, '''communication'',''map_print'',''letter'')',
                       '''communication'',''map_print'',''letter'',''yard_quote'',''work_order'')')
WHERE type = 'table' AND name = 'generated_documents';
PRAGMA writable_schema = RESET;

CREATE TABLE yard_settings (
    marina_id                TEXT PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    test_vessel              TEXT NOT NULL DEFAULT '{}',          -- formula test vessel (m10-plan §3.5)
    quote_validity_days      INTEGER NOT NULL DEFAULT 30 CHECK (quote_validity_days BETWEEN 1 AND 365),
    deposit_bp               INTEGER NOT NULL DEFAULT 3000 CHECK (deposit_bp BETWEEN 0 AND 10000),
    advance_on_accept        INTEGER NOT NULL DEFAULT 1 CHECK (advance_on_accept IN (0,1)),
    external_markup_bp       INTEGER NOT NULL DEFAULT 2000 CHECK (external_markup_bp BETWEEN 0 AND 100000),
    external_item_id         TEXT,
    fixed_includes_external  INTEGER NOT NULL DEFAULT 0 CHECK (fixed_includes_external IN (0,1)),
    workers_issue_materials  INTEGER NOT NULL DEFAULT 0 CHECK (workers_issue_materials IN (0,1)),
    lift_from_slot           INTEGER NOT NULL DEFAULT 28 CHECK (lift_from_slot BETWEEN 0 AND 95),
    lift_to_slot             INTEGER NOT NULL DEFAULT 76 CHECK (lift_to_slot BETWEEN 1 AND 96),
    lift_request_on_move     INTEGER NOT NULL DEFAULT 0 CHECK (lift_request_on_move IN (0,1)),
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TIMESTAMP NOT NULL,
    updated_at               TIMESTAMP NOT NULL,
    FOREIGN KEY (external_item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT yard_settings_lift_window_check CHECK (lift_to_slot > lift_from_slot)
);

CREATE TABLE work_types (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 16),
    name                TEXT NOT NULL,                       -- I18nText
    revenue_class_id    TEXT,
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (revenue_class_id, marina_id) REFERENCES revenue_classes (id, marina_id)
);
CREATE UNIQUE INDEX work_types_code_uidx ON work_types (marina_id, code);

CREATE TABLE yard_qualifications (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 16),
    name                TEXT NOT NULL,                       -- I18nText
    labour_item_id      TEXT NOT NULL,                         -- item_type labour, unit hour (checked in Go)
    default_cost_cents  INTEGER NOT NULL DEFAULT 0 CHECK (default_cost_cents >= 0),
    lift_operator       INTEGER NOT NULL DEFAULT 0 CHECK (lift_operator IN (0,1)),
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (labour_item_id, marina_id) REFERENCES items (id, marina_id)
);
CREATE UNIQUE INDEX yard_qualifications_code_uidx ON yard_qualifications (marina_id, code);
CREATE UNIQUE INDEX yard_qualifications_labour_item_uidx ON yard_qualifications (labour_item_id);

CREATE TABLE yard_item_settings (
    item_id             TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    yard_enabled        INTEGER NOT NULL DEFAULT 1 CHECK (yard_enabled IN (0,1)),
    cost_cents          INTEGER NOT NULL DEFAULT 0 CHECK (cost_cents >= 0),
    qualification_id    TEXT,                                  -- required for labour items (Go)
    qty_formula         TEXT NOT NULL DEFAULT '' CHECK (length(qty_formula) <= 500),
    price_formula       TEXT NOT NULL DEFAULT '' CHECK (length(price_formula) <= 500),
    category            TEXT NOT NULL DEFAULT '' CHECK (length(category) <= 40),
    test_case           TEXT,                                -- {vars, expect_qty?, expect_price_cents?}
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (qualification_id, marina_id) REFERENCES yard_qualifications (id, marina_id)
);
CREATE UNIQUE INDEX yard_item_settings_qualification_uidx ON yard_item_settings (qualification_id)
    WHERE qualification_id IS NOT NULL;
CREATE INDEX yard_item_settings_marina_idx ON yard_item_settings (marina_id);

CREATE TABLE formula_variables (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 32),
    source              TEXT NOT NULL CHECK (source IN ('derived','job')),
    kind                TEXT NOT NULL CHECK (kind IN ('number','bool','enum')),
    unit                TEXT NOT NULL DEFAULT 'none' CHECK (unit IN ('m','m2','t','kw','h','pcs','l','pct','none')),
    expression          TEXT NOT NULL DEFAULT '' CHECK (length(expression) <= 500),
    min_milli           INTEGER,
    max_milli           INTEGER,
    default_milli       INTEGER,                                 -- number and bool (0/1000) job variables
    default_enum        TEXT,
    enum_values         TEXT NOT NULL DEFAULT '[]',
    label               TEXT NOT NULL DEFAULT '{}',          -- I18nText
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    CONSTRAINT formula_variables_derived_check CHECK ((source = 'derived') = (expression <> '')),
    CONSTRAINT formula_variables_range_check CHECK (min_milli IS NULL OR max_milli IS NULL OR max_milli >= min_milli)
);
CREATE UNIQUE INDEX formula_variables_name_uidx ON formula_variables (marina_id, name);

CREATE TABLE formula_tables (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 32),
    label               TEXT NOT NULL DEFAULT '{}',          -- I18nText
    band_rows           TEXT NOT NULL DEFAULT '[]',          -- [{up_to_milli|null, value_milli}] ascending, <= 50
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX formula_tables_name_uidx ON formula_tables (marina_id, name);

CREATE TABLE work_packages (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 16),
    name                TEXT NOT NULL,                       -- I18nText
    work_type_id        TEXT NOT NULL,
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    current_version     INTEGER NOT NULL DEFAULT 0 CHECK (current_version >= 0),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id)
);
CREATE UNIQUE INDEX work_packages_code_uidx ON work_packages (marina_id, code);

-- Insert-only: a version is never updated; it is deleted only by the cascade of its package (the trigger finds no
-- package row and lets it pass). created_by_user_id is a plain column so deleting a user never updates a version.
CREATE TABLE work_package_versions (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    package_id          TEXT NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    language_version    INTEGER NOT NULL DEFAULT 1 CHECK (language_version >= 1),
    lines               TEXT NOT NULL DEFAULT '[]',
    job_vars            TEXT NOT NULL DEFAULT '[]',
    created_by_user_id  TEXT,
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (package_id, marina_id) REFERENCES work_packages (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX work_package_versions_uidx ON work_package_versions (package_id, version);
CREATE INDEX work_package_versions_marina_idx ON work_package_versions (marina_id, package_id);
-- +goose StatementBegin
CREATE TRIGGER work_package_versions_no_update BEFORE UPDATE ON work_package_versions
BEGIN SELECT RAISE(ABORT, 'work_package_version_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER work_package_versions_no_delete BEFORE DELETE ON work_package_versions
WHEN EXISTS (SELECT 1 FROM work_packages WHERE id = OLD.package_id)
BEGIN SELECT RAISE(ABORT, 'work_package_version_immutable'); END;
-- +goose StatementEnd

CREATE TABLE yard_quotes (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number                INTEGER NOT NULL CHECK (number >= 1),
    year                  INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    kind                  TEXT NOT NULL DEFAULT 'job' CHECK (kind IN ('job','variation')),
    work_order_id         TEXT,                                -- variation only; validated in Go (D27)
    customer_id           TEXT NOT NULL,
    vessel_id             TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','sent','accepted','declined','expired')),
    quoted_on             DATE NOT NULL,
    valid_until           DATE NOT NULL,
    expires_at            TIMESTAMP NOT NULL,                       -- end of valid_until in the marina time zone
    billing_mode          TEXT NOT NULL DEFAULT 'actual' CHECK (billing_mode IN ('actual','fixed','progress')),
    deposit_bp            INTEGER CHECK (deposit_bp IS NULL OR deposit_bp BETWEEN 0 AND 10000),
    deposit_cents         INTEGER CHECK (deposit_cents IS NULL OR deposit_cents >= 0),
    conditions            TEXT NOT NULL DEFAULT '' CHECK (length(conditions) <= 4000),
    notes                 TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    planned_start_on      DATE,
    planned_delivery_on   DATE,
    job_inputs            TEXT NOT NULL DEFAULT '{}',
    vessel_overrides      TEXT NOT NULL DEFAULT '{}',
    snapshot              TEXT NOT NULL DEFAULT '{}',        -- yardquote/price.Snapshot (INV-28)
    net_cents             INTEGER NOT NULL DEFAULT 0,
    vat_cents             INTEGER NOT NULL DEFAULT 0,
    gross_cents           INTEGER NOT NULL DEFAULT 0,
    deposit_amount_cents  INTEGER NOT NULL DEFAULT 0 CHECK (deposit_amount_cents >= 0),
    estimated_hours       TEXT NOT NULL DEFAULT '{}',        -- {qualification_id: hours milli}
    sent_at               TIMESTAMP,
    decided_at            TIMESTAMP,
    accepted_via          TEXT CHECK (accepted_via IS NULL OR accepted_via IN ('panel','link')),
    decline_reason        TEXT NOT NULL DEFAULT '' CHECK (length(decline_reason) <= 500),
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    CONSTRAINT yard_quotes_variation_check CHECK ((kind = 'variation') = (work_order_id IS NOT NULL)),
    CONSTRAINT yard_quotes_deposit_check CHECK (deposit_bp IS NULL OR deposit_cents IS NULL),
    CONSTRAINT yard_quotes_validity_check CHECK (valid_until >= quoted_on),
    CONSTRAINT yard_quotes_planned_check CHECK (planned_delivery_on IS NULL OR planned_start_on IS NULL
        OR planned_delivery_on >= planned_start_on),
    CONSTRAINT yard_quotes_accepted_check CHECK (status <> 'accepted' OR accepted_via IS NOT NULL)
);
CREATE UNIQUE INDEX yard_quotes_number_uidx ON yard_quotes (marina_id, year, number);
CREATE INDEX yard_quotes_status_idx ON yard_quotes (marina_id, status, expires_at);
CREATE INDEX yard_quotes_customer_idx ON yard_quotes (marina_id, customer_id);
CREATE INDEX yard_quotes_work_order_idx ON yard_quotes (marina_id, work_order_id);

CREATE TABLE yard_quote_lines (
    id                     TEXT PRIMARY KEY,
    marina_id              TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    quote_id               TEXT NOT NULL,
    seq                    INTEGER NOT NULL CHECK (seq >= 1),
    source                 TEXT NOT NULL CHECK (source IN ('package','item','manual')),
    package_id             TEXT,
    package_version        INTEGER,
    package_line_seq       INTEGER,
    item_id                TEXT NOT NULL,
    work_type_id           TEXT NOT NULL,
    qualification_id       TEXT,
    description            TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    qty_formula            TEXT NOT NULL DEFAULT '' CHECK (length(qty_formula) <= 500),
    price_formula          TEXT NOT NULL DEFAULT '' CHECK (length(price_formula) <= 500),
    qty_milli              INTEGER NOT NULL CHECK (qty_milli >= 0),
    unit_cents             INTEGER NOT NULL CHECK (unit_cents >= 0),
    discount_cents         INTEGER NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    qty_overridden         INTEGER NOT NULL DEFAULT 0 CHECK (qty_overridden IN (0,1)),
    price_overridden       INTEGER NOT NULL DEFAULT 0 CHECK (price_overridden IN (0,1)),
    net_cents              INTEGER NOT NULL DEFAULT 0,
    vat_cents              INTEGER NOT NULL DEFAULT 0,
    gross_cents            INTEGER NOT NULL DEFAULT 0,
    vat_rate_bp            INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature             TEXT,
    price_includes_vat     INTEGER NOT NULL DEFAULT 0 CHECK (price_includes_vat IN (0,1)),
    unit_cost_cents        INTEGER NOT NULL DEFAULT 0 CHECK (unit_cost_cents >= 0),   -- estimate (D2)
    estimated_hours_milli  INTEGER NOT NULL DEFAULT 0 CHECK (estimated_hours_milli >= 0),
    price_list_id          TEXT,                                -- catalogue rule that priced the line (no FK)
    rule_id                TEXT,
    task                   TEXT,                              -- {title I18n, checklist[]} for the task at acceptance
    issues                 TEXT NOT NULL DEFAULT '[]',
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (quote_id, marina_id) REFERENCES yard_quotes (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id),
    FOREIGN KEY (qualification_id, marina_id) REFERENCES yard_qualifications (id, marina_id),
    CONSTRAINT yard_quote_lines_package_check CHECK ((source = 'package')
        = (package_id IS NOT NULL AND package_version IS NOT NULL AND package_line_seq IS NOT NULL))
);
CREATE UNIQUE INDEX yard_quote_lines_seq_uidx ON yard_quote_lines (quote_id, seq);
CREATE INDEX yard_quote_lines_marina_idx ON yard_quote_lines (marina_id, quote_id);

-- +goose Down
DROP TABLE yard_quote_lines;
DROP TABLE yard_quotes;
DROP TABLE work_package_versions;
DROP TABLE work_packages;
DROP TABLE formula_tables;
DROP TABLE formula_variables;
DROP TABLE yard_item_settings;
DROP TABLE yard_qualifications;
DROP TABLE work_types;
DROP TABLE yard_settings;

-- Issued yard documents carry a sha256: the immutability trigger of 00009 is lifted for this delete only.
DROP TRIGGER generated_documents_immutable_delete;
DELETE FROM generated_documents WHERE subject_type IN ('yard_quote','work_order');
-- +goose StatementBegin
CREATE TRIGGER generated_documents_immutable_delete BEFORE DELETE ON generated_documents
WHEN OLD.signed = 1 OR OLD.sha256 IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'document_immutable'); END;
-- +goose StatementEnd
DELETE FROM number_sequences WHERE series IN ('yard_quote','work_order');
PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'',''yard_quote'',''work_order'')',
                       'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'')')
WHERE type = 'table' AND name = 'number_sequences';
UPDATE sqlite_schema
SET sql = replace(sql, '''communication'',''map_print'',''letter'',''yard_quote'',''work_order'')',
                       '''communication'',''map_print'',''letter'')')
WHERE type = 'table' AND name = 'generated_documents';
PRAGMA writable_schema = RESET;
-- DDL after the edit bumps the schema cookie for the other connections (see the header).
CREATE TABLE porto_schema_bump (id INTEGER);
DROP TABLE porto_schema_bump;
