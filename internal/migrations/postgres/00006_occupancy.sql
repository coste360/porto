-- Porto M3 occupancy schema (PostgreSQL): allocations and the three DB-enforced occupancy row families (berths and
-- buoys, cells of dynamic areas, land resources), releases, presences and the per-vessel presence state, the move
-- undo log, closures and the placements of quote options (spec 06, 10; ADR 0002, ADR 0003; m3-plan §5.2).
-- The SQLite twin is internal/migrations/sqlite/00006_occupancy.sql: keep table, column, index and constraint
-- names identical.
--
-- INV-01: no double occupancy is the PRIMARY KEY of berth_occupancies, cell_occupancies and land_occupancies.
-- Never replace it with application checks. Rows have no id and no timestamps (m3-plan §13 D3); their element FKs
-- are RESTRICT (history is kept: elements are deactivated, not deleted).

-- +goose Up
CREATE TABLE allocations (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    contract_line_id    UUID,
    vessel_id           UUID,
    resource_type       TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id         UUID NOT NULL,
    map_id              UUID NOT NULL,
    x_cm                BIGINT,                  -- dynamic areas only (placement)
    y_cm                BIGINT,
    rotation_dd         INTEGER CHECK (rotation_dd IS NULL OR rotation_dd BETWEEN 0 AND 3599),
    first_night         DATE NOT NULL,
    last_night          DATE NOT NULL,            -- inclusive
    kind                TEXT NOT NULL CHECK (kind IN ('hold','contract','transit','owner','block','maintenance')),
    status              TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','cancelled','expired','superseded')),
    expires_at          TIMESTAMPTZ,
    fit_override        JSONB,                     -- {user_id, reason, codes[]}
    block_reason        TEXT,
    moved_from_id       UUID,
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_line_id, marina_id) REFERENCES contract_lines (id, marina_id) ON DELETE RESTRICT,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id),
    FOREIGN KEY (moved_from_id, marina_id) REFERENCES allocations (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (last_night >= first_night),
    CONSTRAINT allocations_block_check CHECK ((kind = 'block') = (vessel_id IS NULL AND block_reason IS NOT NULL)),
    CONSTRAINT allocations_hold_check CHECK (kind <> 'hold' OR expires_at IS NOT NULL),
    CONSTRAINT allocations_placement_check CHECK ((resource_type = 'dynamic_area') = (x_cm IS NOT NULL))
);
CREATE INDEX allocations_resource_idx ON allocations (marina_id, resource_type, resource_id, first_night);
CREATE INDEX allocations_map_idx ON allocations (marina_id, map_id, status, first_night, last_night);
CREATE INDEX allocations_line_idx ON allocations (contract_line_id);
CREATE INDEX allocations_vessel_idx ON allocations (marina_id, vessel_id, status);
CREATE INDEX allocations_expiry_idx ON allocations (marina_id, status, kind, expires_at);
CREATE INDEX allocations_moved_from_idx ON allocations (moved_from_id);

CREATE TABLE berth_occupancies (
    marina_id      UUID NOT NULL,
    berth_id       UUID NOT NULL,
    night          DATE NOT NULL,
    allocation_id  UUID NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('hold','contract','transit','owner','block','maintenance')),
    expires_at     TIMESTAMPTZ,
    PRIMARY KEY (berth_id, night),
    FOREIGN KEY (berth_id, marina_id) REFERENCES berths (id, marina_id) ON DELETE RESTRICT,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX berth_occupancies_night_idx ON berth_occupancies (marina_id, night);
CREATE INDEX berth_occupancies_allocation_idx ON berth_occupancies (allocation_id);
CREATE INDEX berth_occupancies_hold_idx ON berth_occupancies (expires_at) WHERE kind = 'hold';

CREATE TABLE cell_occupancies (
    marina_id      UUID NOT NULL,
    area_id        UUID NOT NULL,
    cell           INTEGER NOT NULL CHECK (cell >= 0),
    night          DATE NOT NULL,
    allocation_id  UUID NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('hold','contract','transit','owner','block','maintenance')),
    expires_at     TIMESTAMPTZ,
    PRIMARY KEY (area_id, cell, night),
    FOREIGN KEY (area_id, marina_id) REFERENCES dynamic_areas (id, marina_id) ON DELETE RESTRICT,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX cell_occupancies_night_idx ON cell_occupancies (area_id, night, cell);
CREATE INDEX cell_occupancies_marina_night_idx ON cell_occupancies (marina_id, night);
CREATE INDEX cell_occupancies_allocation_idx ON cell_occupancies (allocation_id);
CREATE INDEX cell_occupancies_hold_idx ON cell_occupancies (expires_at) WHERE kind = 'hold';

CREATE TABLE land_occupancies (
    marina_id      UUID NOT NULL,
    resource_id    UUID NOT NULL,
    night          DATE NOT NULL,
    allocation_id  UUID NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('hold','contract','transit','owner','block','maintenance')),
    expires_at     TIMESTAMPTZ,
    PRIMARY KEY (resource_id, night),
    FOREIGN KEY (resource_id, marina_id) REFERENCES land_resources (id, marina_id) ON DELETE RESTRICT,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX land_occupancies_night_idx ON land_occupancies (marina_id, night);
CREATE INDEX land_occupancies_allocation_idx ON land_occupancies (allocation_id);
CREATE INDEX land_occupancies_hold_idx ON land_occupancies (expires_at) WHERE kind = 'hold';

CREATE TABLE releases (
    id                    UUID PRIMARY KEY,
    marina_id             UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    allocation_id         UUID NOT NULL,
    contract_id           UUID NOT NULL,
    resource_type         TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id           UUID NOT NULL,
    from_night            DATE NOT NULL,
    to_night              DATE NOT NULL,          -- inclusive
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    source                TEXT NOT NULL CHECK (source IN ('customer_phone','customer_desk','customer_link','dock','office','system')),
    resold_allocation_id  UUID,
    resold_nights         INTEGER NOT NULL DEFAULT 0 CHECK (resold_nights >= 0),
    credit_cents          BIGINT NOT NULL DEFAULT 0,
    notes                 TEXT NOT NULL DEFAULT '',
    created_by_user_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    revoked_at            TIMESTAMPTZ,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (resold_allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    CHECK (to_night >= from_night)
);
CREATE INDEX releases_resource_idx ON releases (marina_id, resource_type, resource_id, from_night);
CREATE INDEX releases_allocation_idx ON releases (allocation_id);

CREATE TABLE presences (
    id              UUID PRIMARY KEY,
    marina_id       UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    vessel_id       UUID NOT NULL,
    allocation_id   UUID,
    contract_id     UUID,
    event           TEXT NOT NULL CHECK (event IN ('arrived','departed','hauled_out','launched')),
    at              TIMESTAMPTZ NOT NULL,
    night           DATE NOT NULL,                -- local date of at (marina time zone)
    source          TEXT NOT NULL DEFAULT 'staff' CHECK (source IN ('staff','sensor','customer')),
    user_id         UUID REFERENCES users (id) ON DELETE SET NULL,
    notes           TEXT NOT NULL DEFAULT '',
    photo_file_id   UUID,                         -- stored_files row (purpose 'document': the CHECK of 00002 is frozen)
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (photo_file_id, marina_id) REFERENCES stored_files (id, marina_id)
);
CREATE INDEX presences_night_idx ON presences (marina_id, night, event);
CREATE INDEX presences_vessel_idx ON presences (marina_id, vessel_id, at);

CREATE TABLE vessel_presence (
    vessel_id           UUID PRIMARY KEY,         -- one state per vessel (m3-plan §13 D21)
    marina_id           UUID NOT NULL,
    state               TEXT NOT NULL CHECK (state IN ('in_port','out','ashore','yard')),
    since               TIMESTAMPTZ NOT NULL,
    allocation_id       UUID,
    last_presence_id    UUID NOT NULL,
    expected_return_on  DATE,
    updated_at          TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id),
    FOREIGN KEY (last_presence_id, marina_id) REFERENCES presences (id, marina_id)
);
CREATE INDEX vessel_presence_state_idx ON vessel_presence (marina_id, state);

CREATE TABLE move_undo_log (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    user_id     UUID REFERENCES users (id) ON DELETE SET NULL,
    operation   TEXT NOT NULL CHECK (operation IN ('move','swap','layout')),
    payload     JSONB NOT NULL,                    -- {entries:[{allocation_id, status, last_night, version}], created:[ids]}
    expires_at  TIMESTAMPTZ NOT NULL,
    undone_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX move_undo_log_expiry_idx ON move_undo_log (marina_id, expires_at);

CREATE TABLE closures (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    scope_type          TEXT NOT NULL CHECK (scope_type IN ('marina','map','structure','group','resource')),
    scope_id            UUID,
    resource_type       TEXT CHECK (resource_type IS NULL OR resource_type IN ('berth','dynamic_area','land_resource')),
    from_date           DATE NOT NULL,
    to_date             DATE NOT NULL,            -- inclusive
    reason              TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 200),
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    CHECK (to_date >= from_date),
    CONSTRAINT closures_scope_check CHECK ((scope_type = 'marina') = (scope_id IS NULL)),
    CONSTRAINT closures_resource_check CHECK ((scope_type = 'resource') = (resource_type IS NOT NULL))
);
CREATE INDEX closures_marina_idx ON closures (marina_id, from_date);

CREATE TABLE quote_option_placements (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL,
    option_id      UUID NOT NULL,
    seq            INTEGER NOT NULL CHECK (seq BETWEEN 1 AND 2),
    resource_type  TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id    UUID NOT NULL,
    x_cm           BIGINT,
    y_cm           BIGINT,
    rotation_dd    INTEGER CHECK (rotation_dd IS NULL OR rotation_dd BETWEEN 0 AND 3599),
    first_night    DATE NOT NULL,
    last_night     DATE NOT NULL,
    allocation_id  UUID,                          -- the hold, when the option holds
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (option_id, marina_id) REFERENCES quote_options (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (allocation_id, marina_id) REFERENCES allocations (id, marina_id) ON DELETE SET NULL (allocation_id),
    CHECK (last_night >= first_night)
);
CREATE UNIQUE INDEX quote_option_placements_seq_uidx ON quote_option_placements (option_id, seq);
CREATE INDEX quote_option_placements_marina_idx ON quote_option_placements (marina_id, option_id);

-- +goose Down
DROP TABLE quote_option_placements;
DROP TABLE closures;
DROP TABLE move_undo_log;
DROP TABLE vessel_presence;
DROP TABLE presences;
DROP TABLE releases;
DROP TABLE land_occupancies;
DROP TABLE cell_occupancies;
DROP TABLE berth_occupancies;
DROP TABLE allocations;
