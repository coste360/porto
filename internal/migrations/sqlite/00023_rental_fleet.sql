-- Porto M11 rental fleet schema (SQLite) (spec 19.1-19.2; m11-plan §3, §15, ADR 0016). The twin is
-- internal/migrations/postgres/00023_rental_fleet.sql: keep table, column, index and constraint names identical.
--
-- rental_settings: one row per marina (defaults in Go when never saved): open window and half-day bands in
--     30-minute slots (0..48 of the local wall-clock day), buffer, desk hold TTL, licence threshold, checklist.
-- rental_units: rental boats, each linked 1:1 to a registry vessel of the marina (11.2).
-- rental_skippers / rental_skipper_hours: bookable skippers and their weekly working hours (a soft rule; days off
--     are hard blocks in rental_blocks, 00024).
-- rental_formulas / rental_extras: configuration of formulas (hourly, half day, day, multi day, week) and extras;
--     prices come only from the M2 price lists (items with revenue class rental).

-- +goose Up
CREATE TABLE rental_settings (
    marina_id                   TEXT PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    open_from_slot              INTEGER NOT NULL DEFAULT 16 CHECK (open_from_slot BETWEEN 0 AND 47),
    open_to_slot                INTEGER NOT NULL DEFAULT 38 CHECK (open_to_slot BETWEEN 1 AND 48),
    bands                       TEXT NOT NULL DEFAULT '[]',        -- [{code, from_slot, to_slot, label: I18nText}] 1..4
    buffer_slots                INTEGER NOT NULL DEFAULT 1 CHECK (buffer_slots BETWEEN 0 AND 8),
    desk_hold_minutes           INTEGER NOT NULL DEFAULT 60 CHECK (desk_hold_minutes BETWEEN 5 AND 1440),
    no_show_after_minutes       INTEGER NOT NULL DEFAULT 60 CHECK (no_show_after_minutes BETWEEN 0 AND 1440),
    licence_threshold_kw_milli  INTEGER NOT NULL DEFAULT 30000 CHECK (licence_threshold_kw_milli > 0),
    skipper_item_id             TEXT,
    damage_item_id              TEXT,
    checklist                   TEXT NOT NULL DEFAULT '[]',        -- [{key, label: I18nText, categories}] 1..30
    checkout_min_photos         INTEGER NOT NULL DEFAULT 2 CHECK (checkout_min_photos BETWEEN 0 AND 10),
    checkin_min_photos          INTEGER NOT NULL DEFAULT 1 CHECK (checkin_min_photos BETWEEN 0 AND 10),
    deposit_modes               TEXT NOT NULL DEFAULT '["cash","card_pos","preauth"]',
    identity_retention_days     INTEGER NOT NULL DEFAULT 365 CHECK (identity_retention_days BETWEEN 30 AND 3650),
    default_register_id         TEXT,
    version                     INTEGER NOT NULL DEFAULT 1,
    created_at                  TIMESTAMP NOT NULL,
    updated_at                  TIMESTAMP NOT NULL,
    FOREIGN KEY (skipper_item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (damage_item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (default_register_id, marina_id) REFERENCES cash_registers (id, marina_id),
    CONSTRAINT rental_settings_window_check CHECK (open_to_slot > open_from_slot)
);

CREATE TABLE rental_units (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    vessel_id            TEXT NOT NULL,
    code                 TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 16),
    category             TEXT NOT NULL CHECK (category IN ('rib','open','cabin','sail','pedalo','sup')),
    seats                INTEGER NOT NULL CHECK (seats BETWEEN 1 AND 99),
    licence              TEXT NOT NULL DEFAULT 'threshold' CHECK (licence IN ('never','always','threshold')),
    fuel_type            TEXT NOT NULL CHECK (fuel_type IN ('petrol','diesel','electric','none')),
    tank_milli           INTEGER NOT NULL DEFAULT 0 CHECK (tank_milli >= 0),
    deposit_cents        INTEGER NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),
    status               TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available','maintenance','retired')),
    home_berth_id        TEXT,
    item_id              TEXT NOT NULL,
    ownership            TEXT NOT NULL DEFAULT 'marina' CHECK (ownership IN ('marina','third_party')),
    owner_customer_id    TEXT,
    engine_hours_milli   INTEGER NOT NULL DEFAULT 0 CHECK (engine_hours_milli >= 0),  -- written only by check-in
    note                 TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    sort                 INTEGER NOT NULL DEFAULT 0,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (home_berth_id, marina_id) REFERENCES berths (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (owner_customer_id, marina_id) REFERENCES customers (id, marina_id),
    CONSTRAINT rental_units_owner_check CHECK ((ownership = 'third_party') = (owner_customer_id IS NOT NULL)),
    CONSTRAINT rental_units_tank_check CHECK (fuel_type IN ('none','electric') OR tank_milli > 0)
);
CREATE UNIQUE INDEX rental_units_code_uidx ON rental_units (marina_id, code);
CREATE UNIQUE INDEX rental_units_vessel_uidx ON rental_units (marina_id, vessel_id);
CREATE INDEX rental_units_status_idx ON rental_units (marina_id, status, category);

CREATE TABLE rental_skippers (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                 TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),   -- PII: rental.manage only
    contact_enc          TEXT,                                 -- KeyRing, AAD rental-skipper:<id>
    licence_expires_on   DATE,
    user_id              TEXT REFERENCES users (id) ON DELETE SET NULL,
    active               INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    sort                 INTEGER NOT NULL DEFAULT 0,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX rental_skippers_user_uidx ON rental_skippers (marina_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX rental_skippers_marina_idx ON rental_skippers (marina_id, active);

CREATE TABLE rental_skipper_hours (
    skipper_id           TEXT NOT NULL,
    weekday              INTEGER NOT NULL CHECK (weekday BETWEEN 1 AND 7),   -- ISO: 1 = Monday
    from_slot            INTEGER NOT NULL CHECK (from_slot BETWEEN 0 AND 47),
    to_slot              INTEGER NOT NULL CHECK (to_slot BETWEEN 1 AND 48),
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    PRIMARY KEY (skipper_id, weekday, from_slot),
    FOREIGN KEY (skipper_id, marina_id) REFERENCES rental_skippers (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT rental_skipper_hours_window_check CHECK (to_slot > from_slot)
);
CREATE INDEX rental_skipper_hours_marina_idx ON rental_skipper_hours (marina_id, skipper_id);

CREATE TABLE rental_formulas (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                 TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 24),
    name                 TEXT NOT NULL,                        -- I18nText (it required)
    kind                 TEXT NOT NULL CHECK (kind IN ('hourly','half_day','day','multi_day','week')),
    band_code            TEXT,                                 -- half_day only: a band of rental_settings
    min_slots            INTEGER NOT NULL DEFAULT 2 CHECK (min_slots BETWEEN 1 AND 48),
    max_slots            INTEGER CHECK (max_slots IS NULL OR max_slots BETWEEN 1 AND 48),   -- NULL = open window
    min_days             INTEGER NOT NULL DEFAULT 1 CHECK (min_days BETWEEN 1 AND 366),
    max_days             INTEGER NOT NULL DEFAULT 30 CHECK (max_days BETWEEN 1 AND 366),
    skipper              TEXT NOT NULL DEFAULT 'none' CHECK (skipper IN ('none','optional','required')),
    fuel                 TEXT NOT NULL DEFAULT 'included' CHECK (fuel IN ('included','metered')),
    fuel_item_id         TEXT,
    late_item_id         TEXT,                                 -- NULL = no late fee
    grace_minutes        INTEGER NOT NULL DEFAULT 15 CHECK (grace_minutes BETWEEN 0 AND 120),
    late_step_minutes    INTEGER NOT NULL DEFAULT 30 CHECK (late_step_minutes IN (15,30,60)),
    categories           TEXT NOT NULL DEFAULT '[]',           -- unit categories; empty = all
    active               INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    sort                 INTEGER NOT NULL DEFAULT 0,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (fuel_item_id, marina_id) REFERENCES items (id, marina_id),
    FOREIGN KEY (late_item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT rental_formulas_band_check CHECK ((kind = 'half_day') = (band_code IS NOT NULL)),
    CONSTRAINT rental_formulas_fuel_item_check CHECK ((fuel = 'metered') = (fuel_item_id IS NOT NULL)),
    CONSTRAINT rental_formulas_days_check CHECK (max_days >= min_days
        AND (kind <> 'multi_day' OR min_days >= 2) AND (kind <> 'week' OR min_days >= 7)),
    CONSTRAINT rental_formulas_slots_check CHECK (max_slots IS NULL OR max_slots >= min_slots)
);
CREATE UNIQUE INDEX rental_formulas_code_uidx ON rental_formulas (marina_id, code);

CREATE TABLE rental_extras (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    item_id              TEXT NOT NULL,
    charge_mode          TEXT NOT NULL DEFAULT 'per_rental' CHECK (charge_mode IN ('per_rental','per_day')),
    default_qty_milli    INTEGER NOT NULL DEFAULT 1000 CHECK (default_qty_milli >= 0),
    max_qty_milli        INTEGER NOT NULL DEFAULT 10000 CHECK (max_qty_milli > 0),
    categories           TEXT NOT NULL DEFAULT '[]',           -- unit categories; empty = all
    active               INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    sort                 INTEGER NOT NULL DEFAULT 0,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CONSTRAINT rental_extras_qty_check CHECK (default_qty_milli <= max_qty_milli)
);
CREATE UNIQUE INDEX rental_extras_item_uidx ON rental_extras (marina_id, item_id);

-- +goose Down
DROP TABLE rental_extras;
DROP TABLE rental_formulas;
DROP TABLE rental_skipper_hours;
DROP TABLE rental_skippers;
DROP TABLE rental_units;
DROP TABLE rental_settings;
