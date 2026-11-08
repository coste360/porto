-- Porto M9 maintenance and utilities schema (PostgreSQL) (spec 20; m9-plan §8, §9, §13, ADR 0014). The twin is
-- internal/migrations/sqlite/00019_maintenance_utilities.sql: keep table, column, index and constraint names
-- identical.
--
-- equipment: registry of lifting machines, vehicles and plants (M10 adds slots and work-order usage).
-- utility_meters / meter_readings: pedestal socket, water, hour and cycle meters and their cumulative readings
--     (qty milli). A replayed offline reading is deduplicated by (marina_id, client_ref) (INV-83).
-- maintenance_plans / maintenance_plan_targets: periodic plans (months, cycles of a meter, fixed date) on
--     polymorphic elements (element_type/element_id validated in Go, like deadlines).
-- maintenance_tasks / maintenance_task_files: tasks from plans (unique per plan, element and due date), the office
--     or field reports. INV-92: maintenance allocations exist only for scheduled, in-progress or done tasks (Q23).
-- utility_settings: tariff items, charging mode and daily thresholds per marina (missing row = defaults).
-- utility_billings / utility_consumptions: consumptions split by occupant nights and charged once through the M5
--     charges (INV-90, INV-91); charges_origin_primary_uidx gains origin 'utility' (one live primary charge per
--     billing).

-- +goose Up
CREATE TABLE equipment (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name                TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    kind                TEXT NOT NULL CHECK (kind IN ('travel_lift','crane','forklift','trailer','vehicle','plant','other')),
    map_id              UUID,
    x_cm                BIGINT,
    y_cm                BIGINT,
    capacity_kg         BIGINT CHECK (capacity_kg IS NULL OR capacity_kg > 0),
    active              BOOLEAN NOT NULL DEFAULT true,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id),
    CONSTRAINT equipment_position_check CHECK ((x_cm IS NULL) = (y_cm IS NULL) AND (x_cm IS NULL OR map_id IS NOT NULL))
);
CREATE UNIQUE INDEX equipment_code_uidx ON equipment (marina_id, code);

CREATE TABLE utility_meters (
    id                    UUID PRIMARY KEY,
    marina_id             UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                  TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    kind                  TEXT NOT NULL CHECK (kind IN ('power','water','hours','cycles')),
    unit                  TEXT NOT NULL CHECK (unit IN ('kwh','m3','h','cycle')),
    pedestal_id           UUID,
    socket_code           TEXT,
    serial                TEXT,
    resource_type         TEXT CHECK (resource_type IS NULL OR resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id           UUID,
    equipment_id          UUID,
    item_id               UUID,
    multiplier_milli      BIGINT NOT NULL DEFAULT 1000 CHECK (multiplier_milli > 0),
    rollover_milli        BIGINT CHECK (rollover_milli IS NULL OR rollover_milli > 0),
    daily_threshold_milli BIGINT CHECK (daily_threshold_milli IS NULL OR daily_threshold_milli > 0),
    active                BOOLEAN NOT NULL DEFAULT true,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (pedestal_id, marina_id) REFERENCES pedestals (id, marina_id),
    FOREIGN KEY (equipment_id, marina_id) REFERENCES equipment (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT utility_meters_resource_check CHECK ((resource_type IS NULL) = (resource_id IS NULL)),
    CONSTRAINT utility_meters_kind_unit_check CHECK ((kind = 'power' AND unit = 'kwh') OR (kind = 'water' AND unit = 'm3')
        OR (kind = 'hours' AND unit = 'h') OR (kind = 'cycles' AND unit = 'cycle'))
);
CREATE UNIQUE INDEX utility_meters_code_uidx ON utility_meters (marina_id, code);
CREATE INDEX utility_meters_pedestal_idx ON utility_meters (marina_id, pedestal_id);
CREATE INDEX utility_meters_resource_idx ON utility_meters (marina_id, resource_type, resource_id);

CREATE TABLE maintenance_plans (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    intervention        TEXT NOT NULL CHECK (intervention IN ('underwater_inspection','pennant_replacement','chain_check','rcd_test','general','custom')),
    intervention_label  TEXT,
    period_kind         TEXT NOT NULL CHECK (period_kind IN ('months','cycles','fixed_date')),
    period_every        BIGINT CHECK (period_every IS NULL OR period_every > 0),   -- months, or meter units in milli
    period_month_day    TEXT CHECK (period_month_day IS NULL OR char_length(period_month_day) = 5),   -- MM-DD
    period_meter_id     UUID,
    period_starts_on    DATE,
    lead_days           INTEGER NOT NULL DEFAULT 0 CHECK (lead_days BETWEEN 0 AND 180),
    assignee_user_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    supplier_name       TEXT,
    checklist           JSONB NOT NULL DEFAULT '[]',   -- [{key, label}] (at most 40)
    requires_free       BOOLEAN NOT NULL DEFAULT false,
    nights              INTEGER NOT NULL DEFAULT 1 CHECK (nights BETWEEN 1 AND 60),
    active              BOOLEAN NOT NULL DEFAULT true,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (period_meter_id, marina_id) REFERENCES utility_meters (id, marina_id),
    CONSTRAINT maintenance_plans_custom_check CHECK (intervention <> 'custom' OR intervention_label IS NOT NULL),
    CONSTRAINT maintenance_plans_period_check CHECK (
        (period_kind = 'months' AND period_every IS NOT NULL)
        OR (period_kind = 'cycles' AND period_every IS NOT NULL AND period_meter_id IS NOT NULL)
        OR (period_kind = 'fixed_date' AND period_month_day IS NOT NULL))
);
CREATE INDEX maintenance_plans_active_idx ON maintenance_plans (marina_id, active);

CREATE TABLE maintenance_plan_targets (
    plan_id             UUID NOT NULL,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    element_type        TEXT NOT NULL CHECK (element_type IN ('berth','structure','dynamic_area','land_resource','pedestal','equipment')),
    element_id          UUID NOT NULL,
    PRIMARY KEY (plan_id, element_type, element_id),
    FOREIGN KEY (plan_id, marina_id) REFERENCES maintenance_plans (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX maintenance_plan_targets_element_idx ON maintenance_plan_targets (marina_id, element_type, element_id);

CREATE TABLE maintenance_tasks (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    element_type        TEXT CHECK (element_type IS NULL OR element_type IN ('berth','structure','dynamic_area','land_resource','pedestal','equipment')),
    element_id          UUID,
    plan_id             UUID,
    intervention        TEXT CHECK (intervention IS NULL OR intervention IN ('underwater_inspection','pennant_replacement','chain_check','rcd_test','general','custom')),
    source              TEXT NOT NULL CHECK (source IN ('plan','office','report')),
    title               TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description         TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    priority            TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','urgent')),
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','scheduled','in_progress','done','cancelled')),
    due_on              DATE,
    scheduled_from      DATE,
    scheduled_to        DATE,
    out_of_service      BOOLEAN NOT NULL DEFAULT false,
    map_id              UUID,
    position_x_cm       BIGINT,
    position_y_cm       BIGINT,
    checklist           JSONB NOT NULL DEFAULT '[]',   -- snapshot of the plan checklist
    checklist_results   JSONB NOT NULL DEFAULT '[]',   -- [{key, done, note}]
    outcome             TEXT CHECK (outcome IS NULL OR outcome IN ('ok','fixed','needs_followup','not_done')),
    outcome_notes       TEXT,
    outcome_client_ref  TEXT,                          -- client_ref of the (offline) completion, dedupes replays
    internal_minutes    INTEGER CHECK (internal_minutes IS NULL OR internal_minutes >= 0),
    external_cost_cents BIGINT CHECK (external_cost_cents IS NULL OR external_cost_cents >= 0),
    supplier_ref        TEXT,
    assignee_user_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    reported_by_user_id UUID REFERENCES users (id) ON DELETE SET NULL,
    allocation_ids      JSONB NOT NULL DEFAULT '[]',   -- maintenance allocations of the task
    follows_task_id     UUID,
    client_ref          TEXT,                          -- client_ref of the (offline) report
    done_at             TIMESTAMPTZ,
    done_by_user_id     UUID REFERENCES users (id) ON DELETE SET NULL,
    cancel_reason       TEXT,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (plan_id, marina_id) REFERENCES maintenance_plans (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id),
    FOREIGN KEY (follows_task_id, marina_id) REFERENCES maintenance_tasks (id, marina_id),
    CONSTRAINT maintenance_tasks_element_check CHECK ((element_type IS NULL) = (element_id IS NULL)),
    CONSTRAINT maintenance_tasks_target_check CHECK (element_id IS NOT NULL OR map_id IS NOT NULL),
    CONSTRAINT maintenance_tasks_done_outcome_check CHECK (status <> 'done' OR outcome IS NOT NULL),
    CONSTRAINT maintenance_tasks_schedule_check CHECK (scheduled_to IS NULL OR scheduled_from IS NULL OR scheduled_to >= scheduled_from)
);
CREATE UNIQUE INDEX maintenance_tasks_plan_due_uidx ON maintenance_tasks (plan_id, element_type, element_id, due_on)
    WHERE plan_id IS NOT NULL;
CREATE UNIQUE INDEX maintenance_tasks_client_ref_uidx ON maintenance_tasks (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX maintenance_tasks_status_idx ON maintenance_tasks (marina_id, status, due_on);
CREATE INDEX maintenance_tasks_element_idx ON maintenance_tasks (marina_id, element_type, element_id);
CREATE INDEX maintenance_tasks_assignee_idx ON maintenance_tasks (marina_id, assignee_user_id, status);

CREATE TABLE maintenance_task_files (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    task_id             UUID NOT NULL,
    file_id             UUID NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('photo','document')),
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (task_id, marina_id) REFERENCES maintenance_tasks (id, marina_id),
    FOREIGN KEY (file_id, marina_id) REFERENCES stored_files (id, marina_id)
);
CREATE UNIQUE INDEX maintenance_task_files_uidx ON maintenance_task_files (task_id, file_id);
CREATE INDEX maintenance_task_files_marina_idx ON maintenance_task_files (marina_id, task_id);

CREATE TABLE utility_settings (
    marina_id                   UUID PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    power_item_id               UUID,
    water_item_id               UUID,
    mode                        TEXT NOT NULL DEFAULT 'with_installment' CHECK (mode IN ('with_installment','monthly')),
    power_daily_threshold_milli BIGINT CHECK (power_daily_threshold_milli IS NULL OR power_daily_threshold_milli > 0),
    water_daily_threshold_milli BIGINT CHECK (water_daily_threshold_milli IS NULL OR water_daily_threshold_milli > 0),
    version                     INTEGER NOT NULL DEFAULT 1,
    created_at                  TIMESTAMPTZ NOT NULL,
    updated_at                  TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (power_item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (water_item_id, marina_id) REFERENCES items (id, marina_id)
);

CREATE TABLE meter_readings (
    id                    UUID PRIMARY KEY,
    marina_id             UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    meter_id              UUID NOT NULL,
    read_at               TIMESTAMPTZ NOT NULL,
    read_on               DATE NOT NULL,                 -- marina-local date of read_at
    value_milli           BIGINT NOT NULL CHECK (value_milli >= 0),
    kind                  TEXT NOT NULL DEFAULT 'regular' CHECK (kind IN ('regular','initial','replacement_old','replacement_new')),
    source                TEXT NOT NULL DEFAULT 'staff' CHECK (source IN ('staff','import','sensor')),
    photo_file_id         UUID,
    user_id               UUID REFERENCES users (id) ON DELETE SET NULL,
    note                  TEXT CHECK (note IS NULL OR char_length(note) <= 500),
    status                TEXT NOT NULL DEFAULT 'valid' CHECK (status IN ('valid','anomaly','confirmed','void')),
    anomaly_codes         JSONB NOT NULL DEFAULT '[]',
    client_ref            TEXT,
    void_reason           TEXT,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (meter_id, marina_id) REFERENCES utility_meters (id, marina_id),
    FOREIGN KEY (photo_file_id, marina_id) REFERENCES stored_files (id, marina_id)
);
CREATE UNIQUE INDEX meter_readings_meter_at_uidx ON meter_readings (meter_id, read_at);
CREATE UNIQUE INDEX meter_readings_client_ref_uidx ON meter_readings (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX meter_readings_meter_idx ON meter_readings (marina_id, meter_id, read_at);
CREATE INDEX meter_readings_status_idx ON meter_readings (marina_id, status);

CREATE TABLE utility_billings (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    contract_id         UUID NOT NULL,
    kind                TEXT NOT NULL DEFAULT 'primary' CHECK (kind IN ('primary','adjustment')),
    run_on              DATE NOT NULL,
    charge_id           UUID,                          -- the charge with origin (utility, id); set in the same tx
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id)
);
CREATE INDEX utility_billings_contract_idx ON utility_billings (marina_id, contract_id, run_on);

CREATE TABLE utility_consumptions (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    meter_id            UUID NOT NULL,
    from_reading_id     UUID NOT NULL,
    to_reading_id       UUID NOT NULL,
    share_key           TEXT NOT NULL,                 -- allocation id, or 'unassigned'
    contract_id         UUID,
    allocation_id       UUID,
    customer_id         UUID,                          -- billing customer
    resource_type       TEXT CHECK (resource_type IS NULL OR resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id         UUID,
    first_night         DATE NOT NULL,
    last_night          DATE NOT NULL,
    qty_milli           BIGINT NOT NULL,               -- may be negative only for a correction delta
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','anomaly','unassigned','charged','excluded')),
    charge_id           UUID,
    billing_id          UUID,
    exclude_reason      TEXT,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (meter_id, marina_id) REFERENCES utility_meters (id, marina_id),
    FOREIGN KEY (from_reading_id, marina_id) REFERENCES meter_readings (id, marina_id),
    FOREIGN KEY (to_reading_id, marina_id) REFERENCES meter_readings (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id),
    FOREIGN KEY (billing_id, marina_id) REFERENCES utility_billings (id, marina_id),
    CONSTRAINT utility_consumptions_nights_check CHECK (last_night >= first_night),
    CONSTRAINT utility_consumptions_charged_check CHECK ((status = 'charged') = (charge_id IS NOT NULL))
);
CREATE UNIQUE INDEX utility_consumptions_share_uidx ON utility_consumptions (from_reading_id, to_reading_id, share_key);
CREATE INDEX utility_consumptions_status_idx ON utility_consumptions (marina_id, status, contract_id);
CREATE INDEX utility_consumptions_meter_idx ON utility_consumptions (marina_id, meter_id, first_night);

DROP INDEX charges_origin_primary_uidx;
CREATE UNIQUE INDEX charges_origin_primary_uidx ON charges (marina_id, origin_type, origin_id)
    WHERE corrects_charge_id IS NULL AND payment_status <> 'cancelled'
      AND origin_type IN ('installment','holder_installment','transit','termination','deposit_retention','utility');

-- +goose Down
DROP INDEX charges_origin_primary_uidx;
CREATE UNIQUE INDEX charges_origin_primary_uidx ON charges (marina_id, origin_type, origin_id)
    WHERE corrects_charge_id IS NULL AND payment_status <> 'cancelled'
      AND origin_type IN ('installment','holder_installment','transit','termination','deposit_retention');

DROP TABLE utility_consumptions;
DROP TABLE utility_billings;
DROP TABLE meter_readings;
DROP TABLE utility_settings;
DROP TABLE maintenance_task_files;
DROP TABLE maintenance_tasks;
DROP TABLE maintenance_plan_targets;
DROP TABLE maintenance_plans;
DROP TABLE utility_meters;
DROP TABLE equipment;
