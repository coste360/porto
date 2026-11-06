-- Porto M1 harbour schema (PostgreSQL): stored files, harbour settings, maps and their elements, berth groups
-- (spec 06 §6.2, 07, 08; m1-plan §3.1). The SQLite twin is internal/migrations/sqlite/00002_harbor.sql:
-- keep table, column and index names identical.

-- +goose Up
CREATE TABLE stored_files (                       -- encrypted blobs under PORTO_DATA_DIR/files (internal/filestore)
    id                 UUID PRIMARY KEY,
    marina_id          UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    purpose            TEXT NOT NULL CHECK (purpose IN ('map_background', 'document')),
    map_id             UUID,                      -- map_background only: the map whose upload created it (no FK: the map
                                                  -- may be deleted while another map still references the file)
    content_type       TEXT NOT NULL CHECK (content_type IN ('image/png','image/jpeg','image/svg+xml','application/pdf')),
    size_bytes         BIGINT NOT NULL CHECK (size_bytes BETWEEN 1 AND 15728640),
    sha256             TEXT NOT NULL,             -- of the plaintext
    storage_key        TEXT NOT NULL UNIQUE,      -- relative path "<marina_id>/<id>.bin"
    width_px           INTEGER,                   -- images only
    height_px          INTEGER,
    created_by_user_id UUID REFERENCES users (id) ON DELETE SET NULL,
    deleted_at         TIMESTAMPTZ,                 -- set, then the blob is removed after commit
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE INDEX stored_files_marina_id_idx ON stored_files (marina_id, purpose);

CREATE TABLE harbor_settings (
    id                UUID PRIMARY KEY,
    marina_id         UUID NOT NULL UNIQUE REFERENCES marinas (id) ON DELETE CASCADE,
    price_class_rule  JSONB NOT NULL,              -- layout.PriceClassRule JSON; default inserted lazily by the service
    loa_tolerance_cm  BIGINT NOT NULL DEFAULT 50 CHECK (loa_tolerance_cm BETWEEN 0 AND 500),
    default_grid_cm   BIGINT NOT NULL DEFAULT 50 CHECK (default_grid_cm BETWEEN 1 AND 10000),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL
);

CREATE TABLE maps (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('water','yard','shed','parking','boxes','other')),
    name                JSONB NOT NULL,            -- I18nText JSON {"it": "...", "en": "..."}; it required
    icon                TEXT NOT NULL DEFAULT 'map',
    width_cm            BIGINT NOT NULL CHECK (width_cm BETWEEN 100 AND 500000),
    height_cm           BIGINT NOT NULL CHECK (height_cm BETWEEN 100 AND 500000),
    grid_cm             BIGINT NOT NULL DEFAULT 50 CHECK (grid_cm BETWEEN 1 AND 10000),
    background_file_id  UUID,
    background          JSONB,                     -- MapBackground JSON (calibration), NULL = none
    georef              JSONB,                     -- {"points":[{x_cm,y_cm,lat_e7,lon_e7} x3]}, NULL = none
    concession_outline  JSONB,                     -- polygon JSON in cm, NULL = none
    water_polygons      JSONB NOT NULL DEFAULT '[]',
    land_polygons       JSONB NOT NULL DEFAULT '[]',
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (background_file_id, marina_id) REFERENCES stored_files (id, marina_id) ON DELETE SET NULL (background_file_id)
);
CREATE INDEX maps_marina_sort_idx ON maps (marina_id, sort_order);
-- PostgreSQL: ON DELETE SET NULL on a composite FK would also null marina_id; PG 15+ syntax
-- "ON DELETE SET NULL (background_file_id)" is used there. SQLite has no column list: stored_files rows are only
-- soft-deleted (deleted_at), never hard-deleted while referenced, so the action never fires on SQLite.

CREATE TABLE harbor_structures (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL,
    map_id      UUID NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('quay','pier','finger','breakwater','channel','walkway')),
    code        TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL DEFAULT '',
    geometry    JSONB NOT NULL,                    -- {"points":[...],"closed":bool} | {"rect":{anchor,width,length,rotation_dd}}
    width_cm    BIGINT CHECK (width_cm IS NULL OR width_cm > 0),
    parent_id   UUID,                               -- same marina: composite FK below
    attributes  JSONB NOT NULL DEFAULT '{}',       -- {sides[], floating, depth_profile[], elevation_cm, bollard_spacing_cm, min_depth_cm}
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    -- Composite FKs keep references inside the marina (INV-73). They are deferred: the single save path
    -- (store.SaveMapElements) deletes removed rows before it updates the rows that pointed at them, and the
    -- validated document never keeps a dangling reference, so the check at commit always holds.
    FOREIGN KEY (parent_id, marina_id) REFERENCES harbor_structures (id, marina_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX harbor_structures_map_code_uidx ON harbor_structures (map_id, code);
CREATE INDEX harbor_structures_marina_map_idx ON harbor_structures (marina_id, map_id);

CREATE TABLE pedestals (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL,
    map_id      UUID NOT NULL,
    code        TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    x_cm        BIGINT NOT NULL,
    y_cm        BIGINT NOT NULL,
    sockets     JSONB NOT NULL DEFAULT '[]',       -- [{code, kind: power|water, amps, phase}]
    meters      JSONB NOT NULL DEFAULT '[]',       -- [{kind: power|water, serial}]
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX pedestals_map_code_uidx ON pedestals (map_id, code);
CREATE INDEX pedestals_marina_map_idx ON pedestals (marina_id, map_id);

CREATE TABLE berths (
    id                   UUID PRIMARY KEY,
    marina_id            UUID NOT NULL,
    map_id               UUID NOT NULL,
    structure_id         UUID,                     -- same marina: composite FK below
    code                 TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    side                 TEXT NOT NULL DEFAULT 'none' CHECK (side IN ('left','right','head','none')),
    position             BIGINT NOT NULL DEFAULT 0,
    anchor_x_cm          BIGINT NOT NULL,
    anchor_y_cm          BIGINT NOT NULL,
    rotation_dd          INTEGER NOT NULL CHECK (rotation_dd BETWEEN 0 AND 3599),
    width_cm             BIGINT NOT NULL CHECK (width_cm > 0),
    length_cm            BIGINT NOT NULL CHECK (length_cm > 0),
    max_loa_cm           BIGINT NOT NULL CHECK (max_loa_cm > 0),
    max_beam_cm          BIGINT NOT NULL CHECK (max_beam_cm > 0),
    depth_cm             BIGINT CHECK (depth_cm IS NULL OR depth_cm > 0),
    max_air_draft_cm     BIGINT CHECK (max_air_draft_cm IS NULL OR max_air_draft_cm > 0),
    max_displacement_kg  BIGINT CHECK (max_displacement_kg IS NULL OR max_displacement_kg > 0),
    mooring_type         TEXT NOT NULL CHECK (mooring_type IN ('mooring_line','finger','alongside','piles','buoy')),
    swing_radius_cm      BIGINT CHECK (swing_radius_cm IS NULL OR swing_radius_cm > 0),
    overhang_allowed     BOOLEAN NOT NULL DEFAULT false,
    pedestal_id          UUID,                     -- same marina: composite FK below
    pedestal_socket      TEXT,
    power_a              INTEGER CHECK (power_a IS NULL OR power_a IN (16,32,63,125)),
    power_phase          TEXT CHECK (power_phase IS NULL OR power_phase IN ('single','three')),
    water                BOOLEAN NOT NULL DEFAULT false,
    wifi                 BOOLEAN NOT NULL DEFAULT false,
    price_class          TEXT,
    attributes           JSONB NOT NULL DEFAULT '[]',   -- tags: transit_only, superyacht, accessible, near_services, ...
    online_sellable      BOOLEAN NOT NULL DEFAULT false,
    active               BOOLEAN NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (structure_id, marina_id) REFERENCES harbor_structures (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (pedestal_id, marina_id) REFERENCES pedestals (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (mooring_type <> 'buoy' OR swing_radius_cm IS NOT NULL)
);
CREATE UNIQUE INDEX berths_map_code_uidx ON berths (map_id, code);
CREATE INDEX berths_marina_map_idx ON berths (marina_id, map_id);
CREATE INDEX berths_structure_idx ON berths (structure_id, position);

CREATE TABLE dynamic_areas (
    id                UUID PRIMARY KEY,
    marina_id         UUID NOT NULL,
    map_id            UUID NOT NULL,
    kind              TEXT NOT NULL CHECK (kind IN ('linear','surface')),
    code              TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    structure_id      UUID,                        -- same marina: composite FK below
    side              TEXT CHECK (side IS NULL OR side IN ('left','right','head')),
    from_cm           BIGINT,
    to_cm             BIGINT,
    outline           JSONB,                       -- polygon JSON (surface)
    mooring_mode      TEXT CHECK (mooring_mode IS NULL OR mooring_mode IN ('stern_to','alongside')),
    cell_cm           BIGINT NOT NULL CHECK (cell_cm BETWEEN 10 AND 1000),
    margin_cm         BIGINT NOT NULL CHECK (margin_cm BETWEEN 0 AND 1000),
    max_loa_cm        BIGINT CHECK (max_loa_cm IS NULL OR max_loa_cm > 0),
    max_beam_cm       BIGINT CHECK (max_beam_cm IS NULL OR max_beam_cm > 0),
    max_height_cm     BIGINT CHECK (max_height_cm IS NULL OR max_height_cm > 0),
    max_weight_kg     BIGINT CHECK (max_weight_kg IS NULL OR max_weight_kg > 0),   -- per cell
    supports          JSONB NOT NULL DEFAULT '[]', -- cradle, trailer, rack
    depth_profile     JSONB NOT NULL DEFAULT '[]',
    blocked_cells     JSONB NOT NULL DEFAULT '[]',
    exits             JSONB NOT NULL DEFAULT '[]',
    grid_rotation_dd  INTEGER NOT NULL DEFAULT 0 CHECK (grid_rotation_dd IN (0, 900, 1800, 2700)),
    cell_count        INTEGER NOT NULL CHECK (cell_count > 0),   -- computed by layout.CellCount at save
    price_class       TEXT,
    active            BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (structure_id, marina_id) REFERENCES harbor_structures (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (kind <> 'linear' OR (from_cm IS NOT NULL AND to_cm IS NOT NULL AND from_cm < to_cm AND mooring_mode IS NOT NULL AND side IS NOT NULL)),
    CHECK (kind <> 'surface' OR outline IS NOT NULL)
);
CREATE UNIQUE INDEX dynamic_areas_map_code_uidx ON dynamic_areas (map_id, code);
CREATE INDEX dynamic_areas_marina_map_idx ON dynamic_areas (marina_id, map_id);

CREATE TABLE land_resources (
    id           UUID PRIMARY KEY,
    marina_id    UUID NOT NULL,
    map_id       UUID NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('parking','box','storeroom','locker','cradle','trailer_spot','custom')),
    code         TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    x_cm         BIGINT NOT NULL,                -- anchor (midpoint of the access side)
    y_cm         BIGINT NOT NULL,
    rotation_dd  INTEGER NOT NULL CHECK (rotation_dd BETWEEN 0 AND 3599),
    width_cm     BIGINT NOT NULL CHECK (width_cm > 0),
    length_cm    BIGINT NOT NULL CHECK (length_cm > 0),
    price_class  TEXT,
    attributes   JSONB NOT NULL DEFAULT '[]',
    active       BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX land_resources_map_code_uidx ON land_resources (map_id, code);
CREATE INDEX land_resources_marina_map_idx ON land_resources (marina_id, map_id);

CREATE TABLE map_decorations (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL,
    map_id      UUID NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('symbol','text','depth_label','north_arrow','scale_bar')),
    asset       TEXT,                             -- kit id when kind = symbol (validated against layout.ValidAsset)
    variant     TEXT,
    geometry    JSONB NOT NULL,                    -- {x_cm,y_cm,rotation_dd,size_cm} or {points:[...]}
    label       JSONB NOT NULL DEFAULT '{}',       -- I18nText
    depth_cm    BIGINT,                          -- depth_label only
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    CHECK (kind <> 'symbol' OR asset IS NOT NULL)
);
CREATE INDEX map_decorations_marina_map_idx ON map_decorations (marina_id, map_id);

CREATE TABLE berth_groups (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name        JSONB NOT NULL,                    -- I18nText
    kind        TEXT NOT NULL DEFAULT 'custom' CHECK (kind IN ('structure','transit','area','custom')),
    sort_order  INTEGER NOT NULL DEFAULT 0,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX berth_groups_marina_code_uidx ON berth_groups (marina_id, code);

CREATE TABLE berth_group_members (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL,
    group_id       UUID NOT NULL,
    map_id         UUID NOT NULL,                 -- map of the member (for the per-map diff at save)
    resource_type  TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id    UUID NOT NULL,                 -- polymorphic: removed in the save tx when the element is deleted
    created_at     TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (group_id, marina_id) REFERENCES berth_groups (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX berth_group_members_uidx ON berth_group_members (group_id, resource_type, resource_id);
CREATE INDEX berth_group_members_resource_idx ON berth_group_members (marina_id, resource_type, resource_id);
CREATE INDEX berth_group_members_map_idx ON berth_group_members (map_id);

-- +goose Down
DROP TABLE berth_group_members;
DROP TABLE berth_groups;
DROP TABLE map_decorations;
DROP TABLE land_resources;
DROP TABLE dynamic_areas;
DROP TABLE berths;
DROP TABLE pedestals;
DROP TABLE harbor_structures;
DROP TABLE maps;
DROP TABLE harbor_settings;
DROP TABLE stored_files;
