-- Porto M9 storage schema (SQLite) (spec 17, 10.5; m9-plan §3, §7, §13, ADR 0014). The twin is
-- internal/migrations/postgres/00018_storage.sql: keep table, column, index and constraint names identical.
--
-- storage_contract_details: 1:1 with a contracts row of kind storage (support, planned haul-out and launch,
--     handling notes). INV-16: every storage contract has its details and occupies a surface (Q20).
-- storage_scenarios / scenario_items / scenario_moves: what-if layouts of a surface area (working copy and the
--     recomputed diff against the real state). INV-14: a scenario never writes allocations or occupancy rows.
--     seed is the optimizer uint64 seed stored as its int64 bit pattern.
-- storage_movements: the worklist written by the transactional apply of a scenario (INV-06: all or nothing).

-- +goose Up
CREATE TABLE storage_contract_details (
    contract_id         TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    support             TEXT NOT NULL CHECK (support IN ('cradle','trailer','rack','none')),
    support_owner       TEXT NOT NULL DEFAULT 'own' CHECK (support_owner IN ('own','marina')),
    haul_out_on         DATE,
    launch_on           DATE,
    notes_handling      TEXT NOT NULL DEFAULT '' CHECK (length(notes_handling) <= 500),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id) ON DELETE RESTRICT,
    CONSTRAINT storage_contract_details_dates_check CHECK (launch_on IS NULL OR haul_out_on IS NULL OR launch_on >= haul_out_on)
);
CREATE INDEX storage_contract_details_launch_idx ON storage_contract_details (marina_id, launch_on);

CREATE TABLE storage_scenarios (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    area_id             TEXT NOT NULL,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    author_user_id      TEXT REFERENCES users (id) ON DELETE SET NULL,
    from_on             DATE NOT NULL,
    to_on               DATE NOT NULL,                 -- inclusive night window (at most 400 nights, checked in Go)
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','applied','discarded')),
    seed                INTEGER NOT NULL DEFAULT 0,
    score               TEXT NOT NULL DEFAULT '{}',   -- last indicators (StorageIndicators)
    base                TEXT NOT NULL DEFAULT '{}',   -- {allocation_id: version seen when loaded}
    applied_at          TIMESTAMP,
    applied_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (area_id, marina_id) REFERENCES dynamic_areas (id, marina_id),
    CONSTRAINT storage_scenarios_window_check CHECK (to_on >= from_on),
    CONSTRAINT storage_scenarios_applied_check CHECK ((status = 'applied') = (applied_at IS NOT NULL))
);
CREATE INDEX storage_scenarios_area_idx ON storage_scenarios (marina_id, area_id, status);

CREATE TABLE scenario_items (
    scenario_id         TEXT NOT NULL,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    allocation_id       TEXT NOT NULL,
    vessel_id           TEXT,
    x_cm                INTEGER,
    y_cm                INTEGER,
    rotation_dd         INTEGER CHECK (rotation_dd IS NULL OR rotation_dd IN (0,900,1800,2700)),
    pinned              INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0,1)),
    origin              TEXT NOT NULL DEFAULT 'area' CHECK (origin IN ('area','tray')),
    PRIMARY KEY (scenario_id, allocation_id),
    FOREIGN KEY (scenario_id, marina_id) REFERENCES storage_scenarios (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id) ON DELETE RESTRICT,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    CONSTRAINT scenario_items_placement_check CHECK ((x_cm IS NULL) = (y_cm IS NULL) AND (x_cm IS NULL) = (rotation_dd IS NULL))
);
CREATE INDEX scenario_items_allocation_idx ON scenario_items (marina_id, allocation_id);

CREATE TABLE scenario_moves (
    scenario_id         TEXT NOT NULL,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    allocation_id       TEXT NOT NULL,
    vessel_id           TEXT,
    from_resource_type  TEXT CHECK (from_resource_type IS NULL OR from_resource_type IN ('berth','dynamic_area','land_resource')),
    from_resource_id    TEXT,
    from_x_cm           INTEGER,
    from_y_cm           INTEGER,
    from_rotation_dd    INTEGER,
    to_resource_type    TEXT NOT NULL CHECK (to_resource_type IN ('berth','dynamic_area','land_resource')),
    to_resource_id      TEXT NOT NULL,
    to_x_cm             INTEGER,
    to_y_cm             INTEGER,
    to_rotation_dd      INTEGER,
    move_on             DATE NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('move','place')),
    PRIMARY KEY (scenario_id, seq),
    FOREIGN KEY (scenario_id, marina_id) REFERENCES storage_scenarios (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    CONSTRAINT scenario_moves_from_check CHECK ((from_resource_type IS NULL) = (from_resource_id IS NULL))
);
CREATE INDEX scenario_moves_marina_idx ON scenario_moves (marina_id, scenario_id);

CREATE TABLE storage_movements (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    area_id             TEXT NOT NULL,
    scenario_id         TEXT,
    allocation_id       TEXT NOT NULL,
    vessel_id           TEXT,
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    move_on             DATE NOT NULL,
    from_resource_type  TEXT CHECK (from_resource_type IS NULL OR from_resource_type IN ('berth','dynamic_area','land_resource')),
    from_resource_id    TEXT,
    from_x_cm           INTEGER,
    from_y_cm           INTEGER,
    from_rotation_dd    INTEGER,
    to_resource_type    TEXT NOT NULL CHECK (to_resource_type IN ('berth','dynamic_area','land_resource')),
    to_resource_id      TEXT NOT NULL,
    to_x_cm             INTEGER,
    to_y_cm             INTEGER,
    to_rotation_dd      INTEGER,
    equipment_hint      TEXT NOT NULL DEFAULT 'none' CHECK (equipment_hint IN ('travel_lift','trailer','cradle_mover','forklift','none')),
    temporary_position  INTEGER NOT NULL DEFAULT 0 CHECK (temporary_position IN (0,1)),
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','cancelled')),
    done_at             TIMESTAMP,
    done_by_user_id     TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (area_id, marina_id) REFERENCES dynamic_areas (id, marina_id),
    FOREIGN KEY (scenario_id, marina_id) REFERENCES storage_scenarios (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    CONSTRAINT storage_movements_done_check CHECK ((status = 'done') = (done_at IS NOT NULL))
);
CREATE INDEX storage_movements_day_idx ON storage_movements (marina_id, move_on, status);
CREATE UNIQUE INDEX storage_movements_scenario_seq_uidx ON storage_movements (scenario_id, seq) WHERE scenario_id IS NOT NULL;

-- +goose Down
DROP TABLE storage_movements;
DROP TABLE scenario_moves;
DROP TABLE scenario_items;
DROP TABLE storage_scenarios;
DROP TABLE storage_contract_details;
