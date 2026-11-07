-- Porto M5 charges schema (SQLite): billing settings, origin items, billing sequences, charges with lines and
-- offsets, exports to invoicing with their items, invoice reconciliations, opening balance imports, plus the unique
-- charge index of the M4 instalment tables (spec 06.7, 16; m5-plan §3, §6.1). The twin is
-- internal/migrations/postgres/00010_charges.sql: keep table, column, index and constraint names identical.
--
-- INV-51: an exported charge is immutable: amounts, customer, origin, dates and lines (charges_immutable_*,
--         charge_lines_immutable_*); invoicing_status never goes back to to_invoice; a cancelled charge never changes.
-- INV-53: every charge is in at most one first export (charge_export_items_first_uidx).
-- INV-54: one live primary charge per origin (charges_origin_primary_uidx).

-- +goose Up
CREATE TABLE billing_settings (
    marina_id             TEXT PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    default_due_days      INTEGER NOT NULL DEFAULT 30 CHECK (default_due_days BETWEEN 0 AND 365),
    auto_payment_request  INTEGER NOT NULL DEFAULT 1 CHECK (auto_payment_request IN (0,1)),
    reminder_policy       TEXT NOT NULL DEFAULT '{}',        -- {pre_due_days, levels[{level, days, enabled}]}
    reminder_hour         INTEGER NOT NULL DEFAULT 9 CHECK (reminder_hour BETWEEN 0 AND 23),
    arrears_policy        TEXT NOT NULL DEFAULT '{"enabled":false,"days_overdue":60,"min_amount_cents":0}',
    export_reminder_days  INTEGER NOT NULL DEFAULT 30 CHECK (export_reminder_days BETWEEN 1 AND 365),
    fiscal_driver         TEXT NOT NULL DEFAULT 'none',
    receipt_footer        TEXT NOT NULL DEFAULT '',
    sepa_lead_days        INTEGER NOT NULL DEFAULT 0 CHECK (sepa_lead_days BETWEEN 0 AND 30),
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL
);

CREATE TABLE charge_origin_items (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    origin_key  TEXT NOT NULL CHECK (origin_key IN ('management_fee','common_advance','common_balance','transfer_fee',
                                                    'termination_penalty','termination_refund','deposit_retention',
                                                    'opening_balance','contract_trueup_fallback')),
    item_id     TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id)
);
CREATE UNIQUE INDEX charge_origin_items_key_uidx ON charge_origin_items (marina_id, origin_key);

CREATE TABLE billing_sequences (
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    series       TEXT NOT NULL CHECK (series IN ('charge','payment','refund','export','request')),
    period       TEXT NOT NULL,                              -- year, YYYY
    next_number  INTEGER NOT NULL DEFAULT 1 CHECK (next_number >= 1),
    PRIMARY KEY (marina_id, series, period)
);

CREATE TABLE charges (
    id                       TEXT PRIMARY KEY,
    marina_id                TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number                   TEXT NOT NULL,                  -- A{yyyy}-{n:06}
    customer_id              TEXT,                           -- billing customer; NULL only for walk-in receipts
    origin_type              TEXT NOT NULL CHECK (origin_type IN ('installment','holder_installment','transit',
                                 'contract_trueup','counter_sale','termination','deposit_retention','manual',
                                 'adjustment','opening_balance','utility','work_order','rental')),
    origin_id                TEXT NOT NULL,
    contract_id              TEXT,
    vessel_id                TEXT,
    right_id                 TEXT,
    issued_on                DATE NOT NULL,
    due_on                   DATE NOT NULL,
    service_from             DATE,
    service_to               DATE,
    total_cents              INTEGER NOT NULL,
    net_cents                INTEGER NOT NULL,
    vat_cents                INTEGER NOT NULL,
    paid_amount_cents        INTEGER NOT NULL DEFAULT 0,
    credited_cents           INTEGER NOT NULL DEFAULT 0,
    payment_status           TEXT NOT NULL DEFAULT 'open' CHECK (payment_status IN ('open','partially_paid','paid','cancelled')),
    invoicing_status         TEXT NOT NULL DEFAULT 'to_invoice' CHECK (invoicing_status IN ('to_invoice','exported','invoiced')),
    fiscal_mode              TEXT NOT NULL DEFAULT 'invoice' CHECK (fiscal_mode IN ('invoice','receipt')),
    corrects_charge_id       TEXT,
    correction_kind          TEXT CHECK (correction_kind IS NULL OR correction_kind IN ('delta','replace')),
    description              TEXT NOT NULL DEFAULT '',
    fiscal_snapshot_enc      TEXT,                           -- KeyRing, AAD charge-fiscal:<id>
    external_invoice_number  TEXT,
    external_invoice_date    DATE,
    cancel_reason            TEXT,
    first_export_id          TEXT,                           -- charge_exports.id (plain column: restored before exports)
    exported_at              TIMESTAMP,
    invoiced_at              TIMESTAMP,
    cancelled_at             TIMESTAMP,
    created_by_user_id       TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_by_actor         TEXT NOT NULL DEFAULT '',       -- user, system:<job> or provider:<code>
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TIMESTAMP NOT NULL,
    updated_at               TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (right_id, marina_id) REFERENCES berth_rights (id, marina_id),
    FOREIGN KEY (corrects_charge_id, marina_id) REFERENCES charges (id, marina_id),
    CONSTRAINT charges_total_check CHECK (total_cents <> 0),
    CONSTRAINT charges_positive_check CHECK (total_cents < 0 OR (paid_amount_cents >= 0 AND credited_cents >= 0
                                             AND paid_amount_cents + credited_cents <= total_cents)),
    CONSTRAINT charges_negative_check CHECK (total_cents > 0 OR (paid_amount_cents = 0
                                             AND credited_cents BETWEEN total_cents AND 0)),
    CONSTRAINT charges_walk_in_check CHECK (customer_id IS NOT NULL OR fiscal_mode = 'receipt'),
    CONSTRAINT charges_correction_check CHECK (correction_kind IS NULL OR corrects_charge_id IS NOT NULL)
);
CREATE UNIQUE INDEX charges_number_uidx ON charges (marina_id, number);
CREATE UNIQUE INDEX charges_origin_primary_uidx ON charges (marina_id, origin_type, origin_id)
    WHERE corrects_charge_id IS NULL AND payment_status <> 'cancelled'
      AND origin_type IN ('installment','holder_installment','transit','termination','deposit_retention');
CREATE INDEX charges_customer_idx ON charges (marina_id, customer_id, payment_status, due_on);
CREATE INDEX charges_invoicing_idx ON charges (marina_id, invoicing_status, issued_on);
CREATE INDEX charges_contract_idx ON charges (marina_id, contract_id);
CREATE INDEX charges_origin_idx ON charges (marina_id, origin_type, origin_id);
CREATE INDEX charges_corrects_idx ON charges (marina_id, corrects_charge_id);

CREATE TABLE charge_lines (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    charge_id           TEXT NOT NULL,
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    item_id             TEXT,
    item_code           TEXT NOT NULL DEFAULT '',
    description         TEXT NOT NULL DEFAULT '',
    qty_milli           INTEGER NOT NULL DEFAULT 1000,
    unit_cents          INTEGER NOT NULL DEFAULT 0,
    discount_cents      INTEGER NOT NULL DEFAULT 0,
    net_cents           INTEGER NOT NULL,
    vat_cents           INTEGER NOT NULL,
    gross_cents         INTEGER NOT NULL,
    vat_rate_bp         INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature          TEXT CHECK (vat_nature IS NULL OR vat_nature IN ('N1','N2.1','N2.2','N3.1','N3.2',
                            'N3.3','N3.4','N3.5','N3.6','N4','N5','N6.1','N6.2','N6.3','N6.4','N6.5','N6.6','N6.7','N6.8',
                            'N6.9','N7')),
    price_includes_vat  INTEGER NOT NULL DEFAULT 1 CHECK (price_includes_vat IN (0,1)),
    revenue_class_code  TEXT NOT NULL DEFAULT '',
    external_code       TEXT NOT NULL DEFAULT '',
    service_from        DATE,
    service_to          DATE,
    plan_key            TEXT NOT NULL DEFAULT '',            -- chargeplan line key (m5-plan §3.4)
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id) ON DELETE RESTRICT,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT charge_lines_sum_check CHECK (net_cents + vat_cents = gross_cents),
    CONSTRAINT charge_lines_nature_check CHECK ((vat_rate_bp = 0) = (vat_nature IS NOT NULL))
);
CREATE UNIQUE INDEX charge_lines_seq_uidx ON charge_lines (charge_id, seq);
CREATE INDEX charge_lines_marina_idx ON charge_lines (marina_id, charge_id);

CREATE TABLE charge_offsets (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    negative_charge_id  TEXT NOT NULL,
    target_charge_id    TEXT NOT NULL,
    amount_cents        INTEGER NOT NULL CHECK (amount_cents > 0),
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (negative_charge_id, marina_id) REFERENCES charges (id, marina_id),
    FOREIGN KEY (target_charge_id, marina_id) REFERENCES charges (id, marina_id)
);
CREATE INDEX charge_offsets_target_idx ON charge_offsets (target_charge_id);
CREATE INDEX charge_offsets_marina_idx ON charge_offsets (marina_id, negative_charge_id);

CREATE TABLE charge_exports (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number              TEXT NOT NULL,                       -- E{yyyy}-{n:04}
    kind                TEXT NOT NULL DEFAULT 'export' CHECK (kind IN ('export','reexport')),
    reexport_of         TEXT,                                -- charge_exports.id (plain column)
    period_from         DATE,
    period_to           DATE,
    filters             TEXT NOT NULL DEFAULT '{}',
    delivery            TEXT NOT NULL DEFAULT 'file' CHECK (delivery IN ('file','exporter')),
    exporter_code       TEXT,
    delivery_status     TEXT NOT NULL DEFAULT 'none' CHECK (delivery_status IN ('none','pending','sent','failed')),
    delivery_error      TEXT,
    delivery_ref        TEXT,
    charge_count        INTEGER NOT NULL DEFAULT 0 CHECK (charge_count >= 0),
    line_count          INTEGER NOT NULL DEFAULT 0 CHECK (line_count >= 0),
    totals              TEXT NOT NULL DEFAULT '{}',
    csv_file_id         TEXT,
    xlsx_file_id        TEXT,
    pdf_file_id         TEXT,
    csv_sha256          TEXT,
    xlsx_sha256         TEXT,
    pdf_sha256          TEXT,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (csv_file_id, marina_id) REFERENCES stored_files (id, marina_id),
    FOREIGN KEY (xlsx_file_id, marina_id) REFERENCES stored_files (id, marina_id),
    FOREIGN KEY (pdf_file_id, marina_id) REFERENCES stored_files (id, marina_id),
    CONSTRAINT charge_exports_exporter_check CHECK (delivery <> 'exporter' OR exporter_code IS NOT NULL)
);
CREATE UNIQUE INDEX charge_exports_number_uidx ON charge_exports (marina_id, number);
CREATE INDEX charge_exports_marina_idx ON charge_exports (marina_id, created_at);
CREATE INDEX charge_exports_delivery_idx ON charge_exports (marina_id, delivery_status) WHERE delivery = 'exporter';

CREATE TABLE charge_export_items (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    export_id   TEXT NOT NULL,
    charge_id   TEXT NOT NULL,
    is_first    INTEGER NOT NULL CHECK (is_first IN (0,1)),
    row_snapshot_enc TEXT,                     -- frozen row data of the export (payment status, paid, vessel, refs), encrypted
    created_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (export_id, marina_id) REFERENCES charge_exports (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id)
);
CREATE UNIQUE INDEX charge_export_items_uidx ON charge_export_items (export_id, charge_id);
CREATE UNIQUE INDEX charge_export_items_first_uidx ON charge_export_items (charge_id) WHERE is_first = 1;
CREATE INDEX charge_export_items_marina_idx ON charge_export_items (marina_id, charge_id);

CREATE TABLE invoice_reconciliations (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    source              TEXT NOT NULL CHECK (source IN ('csv','manual','exporter')),
    export_id           TEXT,
    file_sha256         TEXT,
    dry_run             INTEGER NOT NULL DEFAULT 0 CHECK (dry_run IN (0,1)),
    row_count           INTEGER NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    matched_count       INTEGER NOT NULL DEFAULT 0 CHECK (matched_count >= 0),
    report              TEXT NOT NULL DEFAULT '[]',          -- rows with charge numbers and outcomes only
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (export_id, marina_id) REFERENCES charge_exports (id, marina_id)
);
CREATE INDEX invoice_reconciliations_marina_idx ON invoice_reconciliations (marina_id, created_at);

CREATE TABLE charge_imports (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    status              TEXT NOT NULL CHECK (status IN ('dry_run','applied','undone','failed')),
    file_sha256         TEXT NOT NULL,
    format              TEXT NOT NULL CHECK (format IN ('csv','xlsx')),
    mapping             TEXT NOT NULL DEFAULT '{}',
    report              TEXT NOT NULL DEFAULT '{}',
    charge_count        INTEGER NOT NULL DEFAULT 0 CHECK (charge_count >= 0),
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    undone_at           TIMESTAMP,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE INDEX charge_imports_marina_idx ON charge_imports (marina_id, created_at);

CREATE UNIQUE INDEX installments_charge_uidx ON installments (charge_id) WHERE charge_id IS NOT NULL;
CREATE UNIQUE INDEX holder_installments_charge_uidx ON holder_installments (charge_id) WHERE charge_id IS NOT NULL;

-- Immutability (INV-51). The services refuse first (409 charge_immutable); the triggers are the backstop. due_on,
-- description, payment columns, invoice references and the fiscal snapshot stay writable on exported charges.
-- +goose StatementBegin
CREATE TRIGGER charges_immutable_update BEFORE UPDATE ON charges
WHEN OLD.payment_status = 'cancelled'
    OR (OLD.invoicing_status <> 'to_invoice' AND (
        NEW.invoicing_status = 'to_invoice'
        OR NEW.customer_id IS NOT OLD.customer_id
        OR NEW.origin_type IS NOT OLD.origin_type
        OR NEW.origin_id IS NOT OLD.origin_id
        OR NEW.total_cents IS NOT OLD.total_cents
        OR NEW.net_cents IS NOT OLD.net_cents
        OR NEW.vat_cents IS NOT OLD.vat_cents
        OR NEW.issued_on IS NOT OLD.issued_on
        OR NEW.service_from IS NOT OLD.service_from
        OR NEW.service_to IS NOT OLD.service_to
        OR NEW.fiscal_mode IS NOT OLD.fiscal_mode
        OR NEW.corrects_charge_id IS NOT OLD.corrects_charge_id))
BEGIN SELECT RAISE(ABORT, 'charge_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charges_immutable_delete BEFORE DELETE ON charges
WHEN OLD.invoicing_status <> 'to_invoice' OR OLD.payment_status = 'cancelled'
BEGIN SELECT RAISE(ABORT, 'charge_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_lines_immutable_insert BEFORE INSERT ON charge_lines
WHEN EXISTS (SELECT 1 FROM charges WHERE id = NEW.charge_id AND (invoicing_status <> 'to_invoice' OR payment_status = 'cancelled'))
BEGIN SELECT RAISE(ABORT, 'charge_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_lines_immutable_update BEFORE UPDATE ON charge_lines
WHEN EXISTS (SELECT 1 FROM charges WHERE id IN (OLD.charge_id, NEW.charge_id) AND (invoicing_status <> 'to_invoice' OR payment_status = 'cancelled'))
BEGIN SELECT RAISE(ABORT, 'charge_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_lines_immutable_delete BEFORE DELETE ON charge_lines
WHEN EXISTS (SELECT 1 FROM charges WHERE id = OLD.charge_id AND (invoicing_status <> 'to_invoice' OR payment_status = 'cancelled'))
BEGIN SELECT RAISE(ABORT, 'charge_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_offsets_insert_only_update BEFORE UPDATE ON charge_offsets
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_offsets_insert_only_delete BEFORE DELETE ON charge_offsets
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_export_items_insert_only_update BEFORE UPDATE ON charge_export_items
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER charge_export_items_insert_only_delete BEFORE DELETE ON charge_export_items
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER charge_export_items_insert_only_delete;
DROP TRIGGER charge_export_items_insert_only_update;
DROP TRIGGER charge_offsets_insert_only_delete;
DROP TRIGGER charge_offsets_insert_only_update;
DROP TRIGGER charge_lines_immutable_delete;
DROP TRIGGER charge_lines_immutable_update;
DROP TRIGGER charge_lines_immutable_insert;
DROP TRIGGER charges_immutable_delete;
DROP TRIGGER charges_immutable_update;
DROP INDEX holder_installments_charge_uidx;
DROP INDEX installments_charge_uidx;
DROP TABLE charge_imports;
DROP TABLE invoice_reconciliations;
DROP TABLE charge_export_items;
DROP TABLE charge_exports;
DROP TABLE charge_offsets;
DROP TABLE charge_lines;
DROP TABLE charges;
DROP TABLE billing_sequences;
DROP TABLE charge_origin_items;
DROP TABLE billing_settings;
