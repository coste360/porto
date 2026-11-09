-- Porto M11 rental bookings and occupancy schema (SQLite) (spec 19.3, 10; m11-plan §4, §6, §9, §15, ADR 0016). The
-- twin is internal/migrations/postgres/00024_rental_bookings.sql: keep table, column, index and constraint names
-- identical.
--
-- rental_bookings: a rental of a unit (optionally with a skipper) from (starts_on, start_slot) inclusive to
--     (ends_on, end_slot) exclusive, in 30-minute slots of the local wall-clock day. Only internal/domain/rental/
--     reservation writes status, slot rows and hold expiry (INV-97).
-- rental_booking_extras: extras booked (source booking) or consumed at return (source return).
-- rental_blocks: out-of-service periods of a unit or days off of a skipper (hard, through the slot tables).
-- rental_slots / rental_skipper_slots: one row per occupied slot. INV-18: their primary keys are the only guarantee
--     that a unit or a skipper is never booked twice (ADR 0003); never replace them with an application check.
--     Unit rows carry the buffer after each rental (kind buffer); skipper rows never do (D9).
-- rental_booking_charges: the charges of a booking. INV-99: at most one main and one return charge per booking
--     (unique partial index); a cancelled and re-created main charge keeps its link as kind 'replaced'.
-- CHECK change: number_sequences.series gains 'rental' (numbers NL<year>-NNNNNN). SQLite edits the table sql with
--     writable_schema as 00016/00020 do (RESET reloads this connection's schema; the DDL that follows bumps the
--     schema cookie for the others); PostgreSQL drops and re-adds the generated constraint.

-- +goose Up
PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'',''yard_quote'',''work_order'')',
                       'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'',''yard_quote'',''work_order'',''rental'')')
WHERE type = 'table' AND name = 'number_sequences';
PRAGMA writable_schema = RESET;

CREATE TABLE rental_bookings (
    id                      TEXT PRIMARY KEY,
    marina_id               TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number                  INTEGER NOT NULL CHECK (number >= 1),
    year                    INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    unit_id                 TEXT NOT NULL,
    formula_id              TEXT NOT NULL,
    skipper_id              TEXT,
    starts_on               DATE NOT NULL,
    start_slot              INTEGER NOT NULL CHECK (start_slot BETWEEN 0 AND 47),
    ends_on                 DATE NOT NULL,
    end_slot                INTEGER NOT NULL CHECK (end_slot BETWEEN 1 AND 48),   -- exclusive
    status                  TEXT NOT NULL CHECK (status IN ('held','confirmed','checked_out','returned','closed',
                                'cancelled','no_show')),
    channel                 TEXT NOT NULL DEFAULT 'desk' CHECK (channel IN ('desk','api','online')),
    hold_expires_at         TIMESTAMP,
    slots_lost_at           TIMESTAMP,                           -- rows reclaimed by a conflicting allocation
    customer_id             TEXT,
    renter_enc              TEXT,                                -- KeyRing, AAD rental-renter:<id> (walk-in name, phone, email)
    party_size              INTEGER CHECK (party_size IS NULL OR party_size BETWEEN 1 AND 99),
    invoice_requested       INTEGER NOT NULL DEFAULT 0 CHECK (invoice_requested IN (0,1)),
    allow_outside_hours     INTEGER NOT NULL DEFAULT 0 CHECK (allow_outside_hours IN (0,1)),
    price_snapshot          TEXT NOT NULL DEFAULT '{}',          -- rental/price.Snapshot (INV-20)
    engine_version          INTEGER NOT NULL DEFAULT 1 CHECK (engine_version >= 1),
    total_cents             INTEGER NOT NULL DEFAULT 0 CHECK (total_cents >= 0),
    deposit_mode            TEXT CHECK (deposit_mode IS NULL OR deposit_mode IN ('cash','card_pos','preauth','waived')),
    deposit_cents           INTEGER NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),
    deposit_status          TEXT NOT NULL DEFAULT 'none' CHECK (deposit_status IN ('none','pending','held','authorized',
                                'capture_pending','released','retained','partially_retained','waived')),
    deposit_payment_id      TEXT,
    deposit_intent_id       TEXT,
    deposit_decision_cents  INTEGER CHECK (deposit_decision_cents IS NULL OR deposit_decision_cents >= 0),
    deposit_retained_cents  INTEGER NOT NULL DEFAULT 0 CHECK (deposit_retained_cents >= 0),
    deposit_waive_reason    TEXT NOT NULL DEFAULT '' CHECK (length(deposit_waive_reason) <= 200),
    checked_out_at          TIMESTAMP,
    returned_at             TIMESTAMP,
    closed_at               TIMESTAMP,
    cancelled_at            TIMESTAMP,
    cancel_reason           TEXT NOT NULL DEFAULT '' CHECK (length(cancel_reason) <= 200),
    no_show_at              TIMESTAMP,
    note                    TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_by_user_id      TEXT REFERENCES users (id) ON DELETE SET NULL,
    version                 INTEGER NOT NULL DEFAULT 1,
    created_at              TIMESTAMP NOT NULL,
    updated_at              TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (unit_id, marina_id) REFERENCES rental_units (id, marina_id),
    FOREIGN KEY (formula_id, marina_id) REFERENCES rental_formulas (id, marina_id),
    FOREIGN KEY (skipper_id, marina_id) REFERENCES rental_skippers (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (deposit_payment_id, marina_id) REFERENCES payments (id, marina_id),
    FOREIGN KEY (deposit_intent_id, marina_id) REFERENCES payment_intents (id, marina_id),
    CONSTRAINT rental_bookings_range_check CHECK (ends_on > starts_on OR (ends_on = starts_on AND end_slot > start_slot)),
    CONSTRAINT rental_bookings_hold_check CHECK ((status = 'held') = (hold_expires_at IS NOT NULL)),
    CONSTRAINT rental_bookings_deposit_check CHECK (deposit_retained_cents <= deposit_cents),
    CONSTRAINT rental_bookings_invoice_check CHECK (invoice_requested = 0 OR customer_id IS NOT NULL)
);
CREATE UNIQUE INDEX rental_bookings_number_uidx ON rental_bookings (marina_id, year, number);
CREATE INDEX rental_bookings_range_idx ON rental_bookings (marina_id, starts_on, ends_on);
CREATE INDEX rental_bookings_status_idx ON rental_bookings (marina_id, status);
CREATE INDEX rental_bookings_unit_idx ON rental_bookings (marina_id, unit_id, starts_on);
CREATE INDEX rental_bookings_customer_idx ON rental_bookings (marina_id, customer_id);
CREATE INDEX rental_bookings_hold_idx ON rental_bookings (status, hold_expires_at) WHERE status = 'held';

CREATE TABLE rental_booking_extras (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id          TEXT NOT NULL,
    extra_id            TEXT NOT NULL,
    qty_milli           INTEGER NOT NULL CHECK (qty_milli > 0),
    source              TEXT NOT NULL DEFAULT 'booking' CHECK (source IN ('booking','return')),
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES rental_bookings (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (extra_id, marina_id) REFERENCES rental_extras (id, marina_id)
);
CREATE UNIQUE INDEX rental_booking_extras_uidx ON rental_booking_extras (booking_id, extra_id, source);
CREATE INDEX rental_booking_extras_marina_idx ON rental_booking_extras (marina_id, booking_id);

CREATE TABLE rental_blocks (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    unit_id             TEXT,
    skipper_id          TEXT,
    from_on             DATE NOT NULL,
    from_slot           INTEGER NOT NULL CHECK (from_slot BETWEEN 0 AND 47),
    to_on               DATE NOT NULL,
    to_slot             INTEGER NOT NULL CHECK (to_slot BETWEEN 1 AND 48),   -- exclusive
    reason              TEXT NOT NULL CHECK (reason IN ('maintenance','closure','day_off','other')),
    note                TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (unit_id, marina_id) REFERENCES rental_units (id, marina_id),
    FOREIGN KEY (skipper_id, marina_id) REFERENCES rental_skippers (id, marina_id),
    CONSTRAINT rental_blocks_target_check CHECK ((unit_id IS NULL) <> (skipper_id IS NULL)),
    CONSTRAINT rental_blocks_range_check CHECK (to_on > from_on OR (to_on = from_on AND to_slot > from_slot))
);
CREATE INDEX rental_blocks_range_idx ON rental_blocks (marina_id, from_on, to_on);

CREATE TABLE rental_slots (
    unit_id             TEXT NOT NULL,
    date                DATE NOT NULL,
    slot                INTEGER NOT NULL CHECK (slot BETWEEN 0 AND 47),
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id          TEXT,
    block_id            TEXT,
    kind                TEXT NOT NULL CHECK (kind IN ('hold','booked','buffer','block')),
    expires_at          TIMESTAMP,                             -- set iff the booking is held
    PRIMARY KEY (unit_id, date, slot),
    FOREIGN KEY (unit_id, marina_id) REFERENCES rental_units (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES rental_bookings (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (block_id, marina_id) REFERENCES rental_blocks (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT rental_slots_owner_check CHECK ((booking_id IS NULL) <> (block_id IS NULL)),
    CONSTRAINT rental_slots_block_check CHECK ((kind = 'block') = (block_id IS NOT NULL)),
    CONSTRAINT rental_slots_expiry_check CHECK ((kind <> 'hold' OR expires_at IS NOT NULL) AND (expires_at IS NULL OR kind IN ('hold','buffer')))
);
CREATE INDEX rental_slots_booking_idx ON rental_slots (booking_id);
CREATE INDEX rental_slots_block_idx ON rental_slots (block_id);
CREATE INDEX rental_slots_marina_idx ON rental_slots (marina_id, date);

CREATE TABLE rental_skipper_slots (
    skipper_id          TEXT NOT NULL,
    date                DATE NOT NULL,
    slot                INTEGER NOT NULL CHECK (slot BETWEEN 0 AND 47),
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id          TEXT,
    block_id            TEXT,
    kind                TEXT NOT NULL CHECK (kind IN ('hold','booked','block')),
    expires_at          TIMESTAMP,                             -- set iff the booking is held
    PRIMARY KEY (skipper_id, date, slot),
    FOREIGN KEY (skipper_id, marina_id) REFERENCES rental_skippers (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES rental_bookings (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (block_id, marina_id) REFERENCES rental_blocks (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT rental_skipper_slots_owner_check CHECK ((booking_id IS NULL) <> (block_id IS NULL)),
    CONSTRAINT rental_skipper_slots_block_check CHECK ((kind = 'block') = (block_id IS NOT NULL)),
    CONSTRAINT rental_skipper_slots_expiry_check CHECK ((kind = 'hold') = (expires_at IS NOT NULL))
);
CREATE INDEX rental_skipper_slots_booking_idx ON rental_skipper_slots (booking_id);
CREATE INDEX rental_skipper_slots_block_idx ON rental_skipper_slots (block_id);
CREATE INDEX rental_skipper_slots_marina_idx ON rental_skipper_slots (marina_id, date);

CREATE TABLE rental_booking_charges (
    booking_id          TEXT NOT NULL,
    charge_id           TEXT NOT NULL,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('main','return','adjustment','replaced')),
    created_at          TIMESTAMP NOT NULL,
    PRIMARY KEY (booking_id, charge_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES rental_bookings (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id)
);
CREATE UNIQUE INDEX rental_booking_charges_charge_uidx ON rental_booking_charges (charge_id);
CREATE UNIQUE INDEX rental_booking_charges_kind_uidx ON rental_booking_charges (booking_id, kind) WHERE kind IN ('main','return');
CREATE INDEX rental_booking_charges_marina_idx ON rental_booking_charges (marina_id, booking_id);

-- +goose Down
DROP TABLE rental_booking_charges;
DROP TABLE rental_skipper_slots;
DROP TABLE rental_slots;
DROP TABLE rental_blocks;
DROP TABLE rental_booking_extras;
DROP TABLE rental_bookings;
DELETE FROM number_sequences WHERE series = 'rental';
PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'',''yard_quote'',''work_order'',''rental'')',
                       'series IN (''mooring'',''transit'',''quote'',''storage'',''charter'',''yard_quote'',''work_order'')')
WHERE type = 'table' AND name = 'number_sequences';
PRAGMA writable_schema = RESET;
-- DDL after the edit bumps the schema cookie for the other connections (see the header).
CREATE TABLE porto_schema_bump (id INTEGER);
DROP TABLE porto_schema_bump;
