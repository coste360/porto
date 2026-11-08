-- Porto M10 yard catalogue and yard quotes schema (PostgreSQL) (spec 18.1-18.3; m10-plan §3, §4, §15, ADR 0015). The
-- twin is internal/migrations/sqlite/00020_yard_catalog_quotes.sql: keep table, column, index and constraint names identical.
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
ALTER TABLE number_sequences DROP CONSTRAINT number_sequences_series_check;
ALTER TABLE number_sequences ADD CONSTRAINT number_sequences_series_check
    CHECK (series IN ('mooring','transit','quote','storage','charter','yard_quote','work_order'));
ALTER TABLE generated_documents DROP CONSTRAINT generated_documents_subject_type_check;
ALTER TABLE generated_documents ADD CONSTRAINT generated_documents_subject_type_check
    CHECK (subject_type IN ('quote','contract','addendum','renewal_proposal',
                           'right_transfer','holder_chain','mandate_statement','fee_notice','common_charges_holder',
                           'common_charges_items','installment_notice','communication','map_print','letter',
                           'yard_quote','work_order'));

CREATE TABLE yard_settings (
    marina_id                UUID PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    test_vessel              JSONB NOT NULL DEFAULT '{}',          -- formula test vessel (m10-plan §3.5)
    quote_validity_days      INTEGER NOT NULL DEFAULT 30 CHECK (quote_validity_days BETWEEN 1 AND 365),
    deposit_bp               INTEGER NOT NULL DEFAULT 3000 CHECK (deposit_bp BETWEEN 0 AND 10000),
    advance_on_accept        BOOLEAN NOT NULL DEFAULT true,
    external_markup_bp       INTEGER NOT NULL DEFAULT 2000 CHECK (external_markup_bp BETWEEN 0 AND 100000),
    external_item_id         UUID,
    fixed_includes_external  BOOLEAN NOT NULL DEFAULT false,
    workers_issue_materials  BOOLEAN NOT NULL DEFAULT false,
    lift_from_slot           INTEGER NOT NULL DEFAULT 28 CHECK (lift_from_slot BETWEEN 0 AND 95),
    lift_to_slot             INTEGER NOT NULL DEFAULT 76 CHECK (lift_to_slot BETWEEN 1 AND 96),
    lift_request_on_move     BOOLEAN NOT NULL DEFAULT false,
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ NOT NULL,
    updated_at               TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (external_item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT yard_settings_lift_window_check CHECK (lift_to_slot > lift_from_slot)
);

CREATE TABLE work_types (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 16),
    name                JSONB NOT NULL,                       -- I18nText
    revenue_class_id    UUID,
    active              BOOLEAN NOT NULL DEFAULT true,
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (revenue_class_id, marina_id) REFERENCES revenue_classes (id, marina_id)
);
CREATE UNIQUE INDEX work_types_code_uidx ON work_types (marina_id, code);

CREATE TABLE yard_qualifications (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 16),
    name                JSONB NOT NULL,                       -- I18nText
    labour_item_id      UUID NOT NULL,                         -- item_type labour, unit hour (checked in Go)
    default_cost_cents  BIGINT NOT NULL DEFAULT 0 CHECK (default_cost_cents >= 0),
    lift_operator       BOOLEAN NOT NULL DEFAULT false,
    active              BOOLEAN NOT NULL DEFAULT true,
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (labour_item_id, marina_id) REFERENCES items (id, marina_id)
);
CREATE UNIQUE INDEX yard_qualifications_code_uidx ON yard_qualifications (marina_id, code);
CREATE UNIQUE INDEX yard_qualifications_labour_item_uidx ON yard_qualifications (labour_item_id);

CREATE TABLE yard_item_settings (
    item_id             UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    yard_enabled        BOOLEAN NOT NULL DEFAULT true,
    cost_cents          BIGINT NOT NULL DEFAULT 0 CHECK (cost_cents >= 0),
    qualification_id    UUID,                                  -- required for labour items (Go)
    qty_formula         TEXT NOT NULL DEFAULT '' CHECK (char_length(qty_formula) <= 500),
    price_formula       TEXT NOT NULL DEFAULT '' CHECK (char_length(price_formula) <= 500),
    category            TEXT NOT NULL DEFAULT '' CHECK (char_length(category) <= 40),
    test_case           JSONB,                                -- {vars, expect_qty?, expect_price_cents?}
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (qualification_id, marina_id) REFERENCES yard_qualifications (id, marina_id)
);
CREATE UNIQUE INDEX yard_item_settings_qualification_uidx ON yard_item_settings (qualification_id)
    WHERE qualification_id IS NOT NULL;
CREATE INDEX yard_item_settings_marina_idx ON yard_item_settings (marina_id);

CREATE TABLE formula_variables (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 32),
    source              TEXT NOT NULL CHECK (source IN ('derived','job')),
    kind                TEXT NOT NULL CHECK (kind IN ('number','bool','enum')),
    unit                TEXT NOT NULL DEFAULT 'none' CHECK (unit IN ('m','m2','t','kw','h','pcs','l','pct','none')),
    expression          TEXT NOT NULL DEFAULT '' CHECK (char_length(expression) <= 500),
    min_milli           BIGINT,
    max_milli           BIGINT,
    default_milli       BIGINT,                                 -- number and bool (0/1000) job variables
    default_enum        TEXT,
    enum_values         JSONB NOT NULL DEFAULT '[]',
    label               JSONB NOT NULL DEFAULT '{}',          -- I18nText
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    CONSTRAINT formula_variables_derived_check CHECK ((source = 'derived') = (expression <> '')),
    CONSTRAINT formula_variables_range_check CHECK (min_milli IS NULL OR max_milli IS NULL OR max_milli >= min_milli)
);
CREATE UNIQUE INDEX formula_variables_name_uidx ON formula_variables (marina_id, name);

CREATE TABLE formula_tables (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 32),
    label               JSONB NOT NULL DEFAULT '{}',          -- I18nText
    band_rows           JSONB NOT NULL DEFAULT '[]',          -- [{up_to_milli|null, value_milli}] ascending, <= 50
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX formula_tables_name_uidx ON formula_tables (marina_id, name);

CREATE TABLE work_packages (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 16),
    name                JSONB NOT NULL,                       -- I18nText
    work_type_id        UUID NOT NULL,
    active              BOOLEAN NOT NULL DEFAULT true,
    current_version     INTEGER NOT NULL DEFAULT 0 CHECK (current_version >= 0),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id)
);
CREATE UNIQUE INDEX work_packages_code_uidx ON work_packages (marina_id, code);

-- Insert-only: a version is never updated; it is deleted only by the cascade of its package (the trigger finds no
-- package row and lets it pass). created_by_user_id is a plain column so deleting a user never updates a version.
CREATE TABLE work_package_versions (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    package_id          UUID NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    language_version    INTEGER NOT NULL DEFAULT 1 CHECK (language_version >= 1),
    lines               JSONB NOT NULL DEFAULT '[]',
    job_vars            JSONB NOT NULL DEFAULT '[]',
    created_by_user_id  UUID,
    created_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (package_id, marina_id) REFERENCES work_packages (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX work_package_versions_uidx ON work_package_versions (package_id, version);
CREATE INDEX work_package_versions_marina_idx ON work_package_versions (marina_id, package_id);
-- +goose StatementBegin
CREATE FUNCTION porto_work_package_versions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' OR EXISTS (SELECT 1 FROM work_packages WHERE id = OLD.package_id) THEN
        RAISE EXCEPTION 'work_package_version_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER work_package_versions_immutable BEFORE UPDATE OR DELETE ON work_package_versions
    FOR EACH ROW EXECUTE FUNCTION porto_work_package_versions_immutable();

CREATE TABLE yard_quotes (
    id                    UUID PRIMARY KEY,
    marina_id             UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number                INTEGER NOT NULL CHECK (number >= 1),
    year                  INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    kind                  TEXT NOT NULL DEFAULT 'job' CHECK (kind IN ('job','variation')),
    work_order_id         UUID,                                -- variation only; validated in Go (D27)
    customer_id           UUID NOT NULL,
    vessel_id             UUID NOT NULL,
    status                TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','sent','accepted','declined','expired')),
    quoted_on             DATE NOT NULL,
    valid_until           DATE NOT NULL,
    expires_at            TIMESTAMPTZ NOT NULL,                       -- end of valid_until in the marina time zone
    billing_mode          TEXT NOT NULL DEFAULT 'actual' CHECK (billing_mode IN ('actual','fixed','progress')),
    deposit_bp            INTEGER CHECK (deposit_bp IS NULL OR deposit_bp BETWEEN 0 AND 10000),
    deposit_cents         BIGINT CHECK (deposit_cents IS NULL OR deposit_cents >= 0),
    conditions            TEXT NOT NULL DEFAULT '' CHECK (char_length(conditions) <= 4000),
    notes                 TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    planned_start_on      DATE,
    planned_delivery_on   DATE,
    job_inputs            JSONB NOT NULL DEFAULT '{}',
    vessel_overrides      JSONB NOT NULL DEFAULT '{}',
    snapshot              JSONB NOT NULL DEFAULT '{}',        -- yardquote/price.Snapshot (INV-28)
    net_cents             BIGINT NOT NULL DEFAULT 0,
    vat_cents             BIGINT NOT NULL DEFAULT 0,
    gross_cents           BIGINT NOT NULL DEFAULT 0,
    deposit_amount_cents  BIGINT NOT NULL DEFAULT 0 CHECK (deposit_amount_cents >= 0),
    estimated_hours       JSONB NOT NULL DEFAULT '{}',        -- {qualification_id: hours milli}
    sent_at               TIMESTAMPTZ,
    decided_at            TIMESTAMPTZ,
    accepted_via          TEXT CHECK (accepted_via IS NULL OR accepted_via IN ('panel','link')),
    decline_reason        TEXT NOT NULL DEFAULT '' CHECK (char_length(decline_reason) <= 500),
    created_by_user_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
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
    id                     UUID PRIMARY KEY,
    marina_id              UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    quote_id               UUID NOT NULL,
    seq                    INTEGER NOT NULL CHECK (seq >= 1),
    source                 TEXT NOT NULL CHECK (source IN ('package','item','manual')),
    package_id             UUID,
    package_version        INTEGER,
    package_line_seq       INTEGER,
    item_id                UUID NOT NULL,
    work_type_id           UUID NOT NULL,
    qualification_id       UUID,
    description            TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    qty_formula            TEXT NOT NULL DEFAULT '' CHECK (char_length(qty_formula) <= 500),
    price_formula          TEXT NOT NULL DEFAULT '' CHECK (char_length(price_formula) <= 500),
    qty_milli              BIGINT NOT NULL CHECK (qty_milli >= 0),
    unit_cents             BIGINT NOT NULL CHECK (unit_cents >= 0),
    discount_cents         BIGINT NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    qty_overridden         BOOLEAN NOT NULL DEFAULT false,
    price_overridden       BOOLEAN NOT NULL DEFAULT false,
    net_cents              BIGINT NOT NULL DEFAULT 0,
    vat_cents              BIGINT NOT NULL DEFAULT 0,
    gross_cents            BIGINT NOT NULL DEFAULT 0,
    vat_rate_bp            INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature             TEXT,
    price_includes_vat     BOOLEAN NOT NULL DEFAULT false,
    unit_cost_cents        BIGINT NOT NULL DEFAULT 0 CHECK (unit_cost_cents >= 0),   -- estimate (D2)
    estimated_hours_milli  BIGINT NOT NULL DEFAULT 0 CHECK (estimated_hours_milli >= 0),
    price_list_id          UUID,                                -- catalogue rule that priced the line (no FK)
    rule_id                TEXT,
    task                   JSONB,                              -- {title I18n, checklist[]} for the task at acceptance
    issues                 JSONB NOT NULL DEFAULT '[]',
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL,
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
DROP TRIGGER work_package_versions_immutable ON work_package_versions;
DROP FUNCTION porto_work_package_versions_immutable();
DROP TABLE work_package_versions;
DROP TABLE work_packages;
DROP TABLE formula_tables;
DROP TABLE formula_variables;
DROP TABLE yard_item_settings;
DROP TABLE yard_qualifications;
DROP TABLE work_types;
DROP TABLE yard_settings;

-- Issued yard documents carry a sha256: the immutability trigger of 00009 is lifted for this delete only.
-- The deferred foreign-key checks of the delete run at once, so that the table can be altered again in this
-- transaction (ALTER TABLE refuses a table with pending trigger events).
ALTER TABLE generated_documents DISABLE TRIGGER generated_documents_immutable;
DELETE FROM generated_documents WHERE subject_type IN ('yard_quote','work_order');
SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE generated_documents ENABLE TRIGGER generated_documents_immutable;
DELETE FROM number_sequences WHERE series IN ('yard_quote','work_order');
ALTER TABLE number_sequences DROP CONSTRAINT number_sequences_series_check;
ALTER TABLE number_sequences ADD CONSTRAINT number_sequences_series_check
    CHECK (series IN ('mooring','transit','quote','storage','charter'));
ALTER TABLE generated_documents DROP CONSTRAINT generated_documents_subject_type_check;
ALTER TABLE generated_documents ADD CONSTRAINT generated_documents_subject_type_check
    CHECK (subject_type IN ('quote','contract','addendum','renewal_proposal',
                           'right_transfer','holder_chain','mandate_statement','fee_notice','common_charges_holder',
                           'common_charges_items','installment_notice','communication','map_print','letter'));
