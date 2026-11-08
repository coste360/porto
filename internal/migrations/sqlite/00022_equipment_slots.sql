-- Porto M10 lifting equipment slots schema (SQLite) (spec 18.7, 20; m10-plan §11, §15, ADR 0015). The twin is
-- internal/migrations/postgres/00022_equipment_slots.sql: keep table, column, index and constraint names identical.
--
-- equipment gains bookable (default true for travel lifts, cranes, forklifts and trailers) and default_minutes.
-- equipment_operators: employees enabled to operate a machine.
-- equipment_bookings: requests (no slots) and bookings of a window of 15-minute slots (0..95 of the local
--     wall-clock day, to_slot exclusive) for a haul-out, launch, move or other job.
-- equipment_slots / operator_slots: one row per booked slot. INV-17: their unique keys are the only guarantee that
--     a machine and an operator are never booked twice (ADR 0003); never replace them with an application check.
--     A requested or cancelled booking has no slot rows (Go + query Q31).

-- +goose Up
ALTER TABLE equipment ADD COLUMN bookable INTEGER NOT NULL DEFAULT 0 CHECK (bookable IN (0,1));
ALTER TABLE equipment ADD COLUMN default_minutes INTEGER NOT NULL DEFAULT 30 CHECK (default_minutes BETWEEN 15 AND 1440);
UPDATE equipment SET bookable = (kind IN ('travel_lift','crane','forklift','trailer'));

CREATE TABLE equipment_operators (
    equipment_id        TEXT NOT NULL,
    employee_id         TEXT NOT NULL,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    PRIMARY KEY (equipment_id, employee_id),
    FOREIGN KEY (equipment_id, marina_id) REFERENCES equipment (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX equipment_operators_employee_idx ON equipment_operators (marina_id, employee_id);

CREATE TABLE equipment_bookings (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    equipment_id          TEXT,                                -- NULL only while requested (or a cancelled request)
    date                  DATE,
    from_slot             INTEGER CHECK (from_slot IS NULL OR from_slot BETWEEN 0 AND 95),
    to_slot               INTEGER CHECK (to_slot IS NULL OR to_slot BETWEEN 1 AND 96),   -- exclusive
    purpose               TEXT NOT NULL CHECK (purpose IN ('haul_out','launch','move','other')),
    vessel_id             TEXT,
    work_order_id         TEXT,
    contract_id           TEXT,                                -- storage contract
    storage_movement_id   TEXT,
    allocation_id         TEXT,                                -- move request (water <-> land)
    operator_employee_id  TEXT,
    status                TEXT NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','booked','done','cancelled')),
    source                TEXT NOT NULL DEFAULT 'office' CHECK (source IN ('office','work_order','storage','move')),
    note                  TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    done_at               TIMESTAMP,
    cancelled_at          TIMESTAMP,
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (equipment_id, marina_id) REFERENCES equipment (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (work_order_id, marina_id) REFERENCES work_orders (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (storage_movement_id, marina_id) REFERENCES storage_movements (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (operator_employee_id, marina_id) REFERENCES employees (id, marina_id),
    CONSTRAINT equipment_bookings_window_check CHECK ((from_slot IS NULL) = (to_slot IS NULL)
        AND (to_slot IS NULL OR to_slot > from_slot) AND (from_slot IS NULL OR date IS NOT NULL)),
    CONSTRAINT equipment_bookings_booked_check CHECK (status IN ('requested','cancelled')
        OR (equipment_id IS NOT NULL AND from_slot IS NOT NULL)),
    CONSTRAINT equipment_bookings_vessel_check CHECK (purpose = 'other' OR vessel_id IS NOT NULL)
);
CREATE UNIQUE INDEX equipment_bookings_move_uidx ON equipment_bookings (marina_id, allocation_id) WHERE source = 'move';
CREATE INDEX equipment_bookings_date_idx ON equipment_bookings (marina_id, date, equipment_id);
CREATE INDEX equipment_bookings_status_idx ON equipment_bookings (marina_id, status);
CREATE INDEX equipment_bookings_work_order_idx ON equipment_bookings (marina_id, work_order_id);

CREATE TABLE equipment_slots (
    equipment_id        TEXT NOT NULL,
    date                DATE NOT NULL,
    slot                INTEGER NOT NULL CHECK (slot BETWEEN 0 AND 95),
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id          TEXT NOT NULL,
    PRIMARY KEY (equipment_id, date, slot),
    FOREIGN KEY (equipment_id, marina_id) REFERENCES equipment (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES equipment_bookings (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX equipment_slots_booking_idx ON equipment_slots (booking_id);
CREATE INDEX equipment_slots_marina_idx ON equipment_slots (marina_id, date);

CREATE TABLE operator_slots (
    employee_id         TEXT NOT NULL,
    date                DATE NOT NULL,
    slot                INTEGER NOT NULL CHECK (slot BETWEEN 0 AND 95),
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id          TEXT NOT NULL,
    PRIMARY KEY (employee_id, date, slot),
    FOREIGN KEY (employee_id, marina_id) REFERENCES employees (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES equipment_bookings (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX operator_slots_booking_idx ON operator_slots (booking_id);
CREATE INDEX operator_slots_marina_idx ON operator_slots (marina_id, date);

-- +goose Down
DROP TABLE operator_slots;
DROP TABLE equipment_slots;
DROP TABLE equipment_bookings;
DROP TABLE equipment_operators;
ALTER TABLE equipment DROP COLUMN default_minutes;
ALTER TABLE equipment DROP COLUMN bookable;
