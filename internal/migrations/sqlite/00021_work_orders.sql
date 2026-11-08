-- Porto M10 work orders, planner and time entries schema (SQLite) (spec 18.4-18.6; m10-plan §5-§10, §15, ADR
-- 0015). The twin is internal/migrations/postgres/00021_work_orders.sql: keep table, column, index and constraint names identical.
--
-- work_orders / work_order_lines / work_order_variations: orders (commesse) created by an accepted yard quote (one
--     order per quote, INV-96) or by hand; budget lines copied from the quote and its variations.
-- work_tasks / work_task_deps / work_task_files: tasks with qualification, estimated hours, checklist, finish-to-start
--     dependencies (acyclic, checked in Go) and photos. Offline task changes dedupe by (marina_id, client_ref).
-- employees / employee_qualifications / employee_calendars / employee_absences: yard staff (distinct from users,
--     optional user link), weekly minutes, absences; never in events or logs (INV-70).
-- task_assignments: planned hours per task, employee and day (planner).
-- timesheets: time entries (timer or manual); one running timer per employee (INV-95); offline replays dedupe by
--     (marina_id, client_ref) (INV-83); approval freezes the cost rate and cost.
-- stock_movements: material issues and returns on an order with frozen cost and price (no stock levels in M10, D13).
-- work_order_external_costs: supplier costs (text), billable with a markup.
-- work_order_billings: advance, progress, final and adjustment billings, each with at most one primary charge
--     (charges.origin_type 'work_order', origin_id = billing id; charges_origin_primary_uidx gains it, INV-94).

-- +goose Up
CREATE TABLE work_orders (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number               INTEGER NOT NULL CHECK (number >= 1),
    year                 INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    quote_id             TEXT,
    customer_id          TEXT NOT NULL,
    vessel_id            TEXT NOT NULL,
    work_type_id         TEXT,
    responsible_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    planned_start_on     DATE,
    planned_delivery_on  DATE,
    allocation_id        TEXT,                                 -- current position (berth, cells, land resource)
    position_note        TEXT NOT NULL DEFAULT '' CHECK (length(position_note) <= 200),
    status               TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','in_progress','on_hold','completed',
                             'delivered','invoiced','cancelled')),
    billing_mode         TEXT NOT NULL DEFAULT 'actual' CHECK (billing_mode IN ('actual','fixed','progress')),
    deposit_cents        INTEGER NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),
    rate_snapshot        TEXT NOT NULL DEFAULT '{}',         -- frozen unit prices (m10-plan §5.1)
    completion_bp        INTEGER NOT NULL DEFAULT 0 CHECK (completion_bp BETWEEN 0 AND 10000),
    notes                TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    hold_reason          TEXT NOT NULL DEFAULT '' CHECK (length(hold_reason) <= 500),
    started_at           TIMESTAMP,
    completed_at         TIMESTAMP,
    delivered_at         TIMESTAMP,
    invoiced_at          TIMESTAMP,
    cancelled_at         TIMESTAMP,
    cancel_reason        TEXT NOT NULL DEFAULT '' CHECK (length(cancel_reason) <= 500),
    created_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (quote_id, marina_id) REFERENCES yard_quotes (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    CONSTRAINT work_orders_planned_check CHECK (planned_delivery_on IS NULL OR planned_start_on IS NULL
        OR planned_delivery_on >= planned_start_on),
    CONSTRAINT work_orders_cancelled_check CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
    CONSTRAINT work_orders_invoiced_check CHECK (status <> 'invoiced' OR invoiced_at IS NOT NULL)
);
CREATE UNIQUE INDEX work_orders_number_uidx ON work_orders (marina_id, year, number);
CREATE UNIQUE INDEX work_orders_quote_uidx ON work_orders (quote_id) WHERE quote_id IS NOT NULL;
CREATE INDEX work_orders_status_idx ON work_orders (marina_id, status, planned_delivery_on);
CREATE INDEX work_orders_vessel_idx ON work_orders (marina_id, vessel_id);

CREATE TABLE work_order_variations (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    work_order_id       TEXT NOT NULL,
    quote_id            TEXT NOT NULL,
    accepted_at         TIMESTAMP NOT NULL,
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (quote_id, marina_id) REFERENCES yard_quotes (id, marina_id)
);
CREATE UNIQUE INDEX work_order_variations_quote_uidx ON work_order_variations (quote_id);
CREATE INDEX work_order_variations_order_idx ON work_order_variations (marina_id, work_order_id);

CREATE TABLE work_order_lines (
    id                     TEXT PRIMARY KEY,
    marina_id              TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    work_order_id          TEXT NOT NULL,
    seq                    INTEGER NOT NULL CHECK (seq >= 1),
    origin                 TEXT NOT NULL CHECK (origin IN ('quote','variation','manual')),
    variation_id           TEXT,
    quote_line_id          TEXT,                                -- yard_quote_lines.id copied (no FK)
    item_id                TEXT NOT NULL,
    work_type_id           TEXT NOT NULL,
    qualification_id       TEXT,
    description            TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    qty_milli              INTEGER NOT NULL CHECK (qty_milli >= 0),
    unit_cents             INTEGER NOT NULL CHECK (unit_cents >= 0),
    discount_cents         INTEGER NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    net_cents              INTEGER NOT NULL DEFAULT 0,
    vat_cents              INTEGER NOT NULL DEFAULT 0,
    gross_cents            INTEGER NOT NULL DEFAULT 0,
    vat_rate_bp            INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature             TEXT,
    price_includes_vat     INTEGER NOT NULL DEFAULT 0 CHECK (price_includes_vat IN (0,1)),
    unit_cost_cents        INTEGER NOT NULL DEFAULT 0 CHECK (unit_cost_cents >= 0),
    estimated_hours_milli  INTEGER NOT NULL DEFAULT 0 CHECK (estimated_hours_milli >= 0),
    consumed_qty_milli     INTEGER CHECK (consumed_qty_milli IS NULL OR consumed_qty_milli >= 0),
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (variation_id, marina_id) REFERENCES work_order_variations (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id),
    FOREIGN KEY (qualification_id, marina_id) REFERENCES yard_qualifications (id, marina_id),
    CONSTRAINT work_order_lines_variation_check CHECK ((origin = 'variation') = (variation_id IS NOT NULL))
);
CREATE UNIQUE INDEX work_order_lines_seq_uidx ON work_order_lines (work_order_id, seq);
CREATE INDEX work_order_lines_marina_idx ON work_order_lines (marina_id, work_order_id);

CREATE TABLE work_tasks (
    id                     TEXT PRIMARY KEY,
    marina_id              TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    work_order_id          TEXT NOT NULL,
    seq                    INTEGER NOT NULL CHECK (seq >= 1),
    title                  TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    qualification_id       TEXT,
    work_type_id           TEXT,
    estimated_hours_milli  INTEGER NOT NULL DEFAULT 0 CHECK (estimated_hours_milli >= 0),
    status                 TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo','in_progress','done','cancelled')),
    progress_bp            INTEGER NOT NULL DEFAULT 0 CHECK (progress_bp BETWEEN 0 AND 10000),
    checklist              TEXT NOT NULL DEFAULT '[]',        -- [{key, label, done}] <= 40
    planned_from           DATE,                                -- cached from assignments
    planned_to             DATE,
    source_line_id         TEXT,
    client_ref             TEXT,                                -- offline creation/progress dedupe (INV-83)
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (qualification_id, marina_id) REFERENCES yard_qualifications (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id),
    FOREIGN KEY (source_line_id, marina_id) REFERENCES work_order_lines (id, marina_id),
    CONSTRAINT work_tasks_done_check CHECK (status = 'cancelled' OR (status = 'done') = (progress_bp = 10000)),
    CONSTRAINT work_tasks_planned_check CHECK (planned_to IS NULL OR planned_from IS NULL OR planned_to >= planned_from)
);
CREATE UNIQUE INDEX work_tasks_seq_uidx ON work_tasks (work_order_id, seq);
CREATE UNIQUE INDEX work_tasks_client_ref_uidx ON work_tasks (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX work_tasks_status_idx ON work_tasks (marina_id, status);

CREATE TABLE work_task_deps (
    task_id             TEXT NOT NULL,
    depends_on_task_id  TEXT NOT NULL,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_task_id),
    FOREIGN KEY (task_id, marina_id) REFERENCES work_tasks (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_task_id, marina_id) REFERENCES work_tasks (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT work_task_deps_self_check CHECK (task_id <> depends_on_task_id)
);
CREATE INDEX work_task_deps_depends_idx ON work_task_deps (depends_on_task_id);
CREATE INDEX work_task_deps_marina_idx ON work_task_deps (marina_id, task_id);

CREATE TABLE work_task_files (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    task_id             TEXT NOT NULL,
    file_id             TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('photo','document')),
    client_ref          TEXT,                                 -- offline photo dedupe (INV-83)
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (task_id, marina_id) REFERENCES work_tasks (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (file_id, marina_id) REFERENCES stored_files (id, marina_id)
);
CREATE UNIQUE INDEX work_task_files_uidx ON work_task_files (task_id, file_id);
CREATE UNIQUE INDEX work_task_files_client_ref_uidx ON work_task_files (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX work_task_files_marina_idx ON work_task_files (marina_id, task_id);

CREATE TABLE employees (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 16),
    first_name          TEXT NOT NULL CHECK (length(first_name) BETWEEN 1 AND 60),
    last_name           TEXT NOT NULL CHECK (length(last_name) BETWEEN 1 AND 60),
    user_id             TEXT REFERENCES users (id) ON DELETE SET NULL,
    department          TEXT NOT NULL DEFAULT '' CHECK (length(department) <= 40),
    hourly_cost_cents   INTEGER NOT NULL DEFAULT 0 CHECK (hourly_cost_cents >= 0),
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX employees_code_uidx ON employees (marina_id, code);
CREATE UNIQUE INDEX employees_user_uidx ON employees (marina_id, user_id) WHERE user_id IS NOT NULL;

CREATE TABLE employee_qualifications (
    employee_id         TEXT NOT NULL,
    qualification_id    TEXT NOT NULL,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    PRIMARY KEY (employee_id, qualification_id),
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (qualification_id, marina_id) REFERENCES yard_qualifications (id, marina_id)
);
CREATE INDEX employee_qualifications_qualification_idx ON employee_qualifications (marina_id, qualification_id);

CREATE TABLE employee_calendars (
    employee_id         TEXT NOT NULL,
    weekday             INTEGER NOT NULL CHECK (weekday BETWEEN 1 AND 7),   -- ISO, 1 = Monday
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    minutes             INTEGER NOT NULL CHECK (minutes BETWEEN 0 AND 960),
    PRIMARY KEY (employee_id, weekday),
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX employee_calendars_marina_idx ON employee_calendars (marina_id, employee_id);

CREATE TABLE employee_absences (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    employee_id         TEXT NOT NULL,
    from_on             DATE NOT NULL,
    to_on               DATE NOT NULL,                        -- inclusive
    kind                TEXT NOT NULL CHECK (kind IN ('vacation','sick','leave','other')),
    minutes_per_day     INTEGER CHECK (minutes_per_day IS NULL OR minutes_per_day BETWEEN 1 AND 960),   -- NULL = whole day
    note                TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT employee_absences_range_check CHECK (to_on >= from_on)
);
CREATE INDEX employee_absences_employee_idx ON employee_absences (marina_id, employee_id, from_on);

CREATE TABLE task_assignments (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    task_id               TEXT NOT NULL,
    employee_id           TEXT NOT NULL,
    work_date             DATE NOT NULL,
    hours_milli           INTEGER NOT NULL CHECK (hours_milli > 0),
    forced_qualification  INTEGER NOT NULL DEFAULT 0 CHECK (forced_qualification IN (0,1)),
    overload              INTEGER NOT NULL DEFAULT 0 CHECK (overload IN (0,1)),
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (task_id, marina_id) REFERENCES work_tasks (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id)
);
CREATE UNIQUE INDEX task_assignments_uidx ON task_assignments (task_id, employee_id, work_date);
CREATE INDEX task_assignments_employee_idx ON task_assignments (marina_id, employee_id, work_date);
CREATE INDEX task_assignments_date_idx ON task_assignments (marina_id, work_date);

CREATE TABLE timesheets (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    employee_id          TEXT NOT NULL,
    work_order_id        TEXT NOT NULL,
    task_id              TEXT,
    work_date            DATE NOT NULL,
    started_at           TIMESTAMP,
    ended_at             TIMESTAMP,
    hours_milli          INTEGER NOT NULL DEFAULT 0 CHECK (hours_milli >= 0),
    source               TEXT NOT NULL CHECK (source IN ('timer','manual')),
    status               TEXT NOT NULL CHECK (status IN ('running','submitted','approved','rejected','voided')),
    cost_rate_cents      INTEGER CHECK (cost_rate_cents IS NULL OR cost_rate_cents >= 0),
    cost_cents           INTEGER CHECK (cost_cents IS NULL OR cost_cents >= 0),
    note                 TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    late                 INTEGER NOT NULL DEFAULT 0 CHECK (late IN (0,1)),         -- recorded after the final billing (conguaglio)
    approved_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    approved_at          TIMESTAMP,
    reject_reason        TEXT NOT NULL DEFAULT '' CHECK (length(reject_reason) <= 500),
    voided_at            TIMESTAMP,
    voided_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    voids_id             TEXT,
    client_ref           TEXT,                                  -- offline dedupe of start or manual entry (INV-83)
    stop_client_ref      TEXT,                                  -- offline dedupe of the stop
    created_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (task_id, marina_id) REFERENCES work_tasks (id, marina_id),
    FOREIGN KEY (voids_id, marina_id) REFERENCES timesheets (id, marina_id),
    CONSTRAINT timesheets_approved_check CHECK (status <> 'approved' OR (cost_cents IS NOT NULL AND cost_rate_cents IS NOT NULL)),
    CONSTRAINT timesheets_running_check CHECK (status <> 'running' OR (started_at IS NOT NULL AND ended_at IS NULL)),
    CONSTRAINT timesheets_interval_check CHECK (ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at)
);
CREATE UNIQUE INDEX timesheets_running_uidx ON timesheets (employee_id) WHERE status = 'running';
CREATE UNIQUE INDEX timesheets_client_ref_uidx ON timesheets (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE UNIQUE INDEX timesheets_stop_client_ref_uidx ON timesheets (marina_id, stop_client_ref) WHERE stop_client_ref IS NOT NULL;
CREATE INDEX timesheets_order_idx ON timesheets (marina_id, work_order_id, status);
CREATE INDEX timesheets_employee_idx ON timesheets (marina_id, employee_id, work_date);

CREATE TABLE stock_movements (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    work_order_id       TEXT NOT NULL,
    task_id             TEXT,
    item_id             TEXT NOT NULL,                         -- item_type material or product (Go)
    kind                TEXT NOT NULL CHECK (kind IN ('issue','return')),
    qty_milli           INTEGER NOT NULL CHECK (qty_milli > 0),
    unit_cost_cents     INTEGER NOT NULL CHECK (unit_cost_cents >= 0),
    unit_price_cents    INTEGER NOT NULL CHECK (unit_price_cents >= 0),
    price_overridden    INTEGER NOT NULL DEFAULT 0 CHECK (price_overridden IN (0,1)),
    moved_on            DATE NOT NULL,
    note                TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    late                INTEGER NOT NULL DEFAULT 0 CHECK (late IN (0,1)),
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (task_id, marina_id) REFERENCES work_tasks (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id)
);
CREATE INDEX stock_movements_order_idx ON stock_movements (marina_id, work_order_id);
CREATE INDEX stock_movements_item_idx ON stock_movements (marina_id, item_id, moved_on);

CREATE TABLE work_order_external_costs (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    work_order_id       TEXT NOT NULL,
    supplier_name       TEXT NOT NULL CHECK (length(supplier_name) BETWEEN 1 AND 100),
    reference           TEXT NOT NULL DEFAULT '' CHECK (length(reference) <= 60),
    incurred_on         DATE NOT NULL,
    description         TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    work_type_id        TEXT,
    cost_cents          INTEGER NOT NULL CHECK (cost_cents > 0),
    billable            INTEGER NOT NULL DEFAULT 1 CHECK (billable IN (0,1)),
    price_cents         INTEGER NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    item_id             TEXT,
    late                INTEGER NOT NULL DEFAULT 0 CHECK (late IN (0,1)),
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (work_type_id, marina_id) REFERENCES work_types (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT work_order_external_costs_item_check CHECK (NOT billable OR item_id IS NOT NULL)
);
CREATE INDEX work_order_external_costs_order_idx ON work_order_external_costs (marina_id, work_order_id);

CREATE TABLE work_order_billings (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    work_order_id       TEXT NOT NULL,
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    kind                TEXT NOT NULL CHECK (kind IN ('advance','progress','final','adjustment')),
    requested           TEXT NOT NULL DEFAULT '{}',          -- {percent_bp?, amount_cents?}
    status              TEXT NOT NULL CHECK (status IN ('charged','skipped_zero')),
    charge_id           TEXT,
    gross_cents         INTEGER NOT NULL DEFAULT 0,              -- gross of the charge (negative for a refund adjustment)
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id),
    CONSTRAINT work_order_billings_charge_check CHECK ((status = 'charged') = (charge_id IS NOT NULL))
);
CREATE UNIQUE INDEX work_order_billings_seq_uidx ON work_order_billings (work_order_id, seq);
CREATE UNIQUE INDEX work_order_billings_charge_uidx ON work_order_billings (charge_id) WHERE charge_id IS NOT NULL;
CREATE INDEX work_order_billings_marina_idx ON work_order_billings (marina_id, work_order_id);

DROP INDEX charges_origin_primary_uidx;
CREATE UNIQUE INDEX charges_origin_primary_uidx ON charges (marina_id, origin_type, origin_id)
    WHERE corrects_charge_id IS NULL AND payment_status <> 'cancelled'
      AND origin_type IN ('installment','holder_installment','transit','termination','deposit_retention','utility','work_order');

-- +goose Down
DROP INDEX charges_origin_primary_uidx;
CREATE UNIQUE INDEX charges_origin_primary_uidx ON charges (marina_id, origin_type, origin_id)
    WHERE corrects_charge_id IS NULL AND payment_status <> 'cancelled'
      AND origin_type IN ('installment','holder_installment','transit','termination','deposit_retention','utility');

DROP TABLE work_order_billings;
DROP TABLE work_order_external_costs;
DROP TABLE stock_movements;
DROP TABLE timesheets;
DROP TABLE task_assignments;
DROP TABLE employee_absences;
DROP TABLE employee_calendars;
DROP TABLE employee_qualifications;
DROP TABLE employees;
DROP TABLE work_task_files;
DROP TABLE work_task_deps;
DROP TABLE work_tasks;
DROP TABLE work_order_lines;
DROP TABLE work_order_variations;
DROP TABLE work_orders;
