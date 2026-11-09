-- Porto M12 public marina profile schema (SQLite) (spec 25, 06.8; m12-plan §11, §12, §15, ADR 0017). The twin is
-- internal/migrations/postgres/00027_marina_profile.sql: keep table, column, index and constraint names identical.
--
-- marina_profiles: one row per marina (defaults prefilled from marinas when never saved), published on /p/{slug}.
--     Position is WGS84 x 10^7 integers (marina.Geo), depths and lengths integer cm (geo.Cm), I18n texts JSON
--     {it, en}. photos and access_images reference stored_files rows (purpose 'document', re-encoded without
--     metadata); only ids referenced by a published profile are served publicly. phone, email and website are the
--     marina's business contacts. No personal data and no per-berth occupancy are ever published (INV-103).

-- +goose Up
CREATE TABLE marina_profiles (
    marina_id                 TEXT PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    published                 INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0,1)),
    published_at              TIMESTAMP,
    locales                   TEXT NOT NULL DEFAULT '["it"]',
    entrance_lat_e7           INTEGER CHECK (entrance_lat_e7 IS NULL OR entrance_lat_e7 BETWEEN -900000000 AND 900000000),
    entrance_lon_e7           INTEGER CHECK (entrance_lon_e7 IS NULL OR entrance_lon_e7 BETWEEN -1800000000 AND 1800000000),
    address                   TEXT NOT NULL DEFAULT '{}',      -- {street, postal_code, city, province, country}
    phone                     TEXT NOT NULL DEFAULT '' CHECK (length(phone) <= 40),
    email                     TEXT NOT NULL DEFAULT '' CHECK (length(email) <= 254),
    website                   TEXT NOT NULL DEFAULT '' CHECK (length(website) <= 300),
    vhf_channels              TEXT NOT NULL DEFAULT '[]',      -- 0..4 ints 1..88
    office_hours              TEXT NOT NULL DEFAULT '[]',      -- [{weekday 1..7, from "HH:MM", to}]
    dock_hours                TEXT NOT NULL DEFAULT '[]',
    hours_note                TEXT NOT NULL DEFAULT '{}',      -- I18n <= 300
    berths_total_override     INTEGER CHECK (berths_total_override IS NULL OR berths_total_override >= 0),
    transit_berths_override   INTEGER CHECK (transit_berths_override IS NULL OR transit_berths_override >= 0),
    max_loa_cm                INTEGER CHECK (max_loa_cm IS NULL OR max_loa_cm > 0),
    depth_entrance_cm         INTEGER CHECK (depth_entrance_cm IS NULL OR depth_entrance_cm >= 0),
    depth_quay_cm             INTEGER CHECK (depth_quay_cm IS NULL OR depth_quay_cm >= 0),
    depth_min_cm              INTEGER CHECK (depth_min_cm IS NULL OR depth_min_cm >= 0),
    depth_max_cm              INTEGER CHECK (depth_max_cm IS NULL OR depth_max_cm >= 0),
    seabed                    TEXT CHECK (seabed IS NULL OR seabed IN ('sand','mud','rock','weed','mixed','other')),
    shelter                   TEXT NOT NULL DEFAULT '[]',      -- subset of N, NE, E, SE, S, SW, W, NW
    access_notes              TEXT NOT NULL DEFAULT '{}',      -- I18n <= 2000
    access_images             TEXT NOT NULL DEFAULT '[]',      -- <= 4 [{file_id, alt: I18n, sort}]
    lights                    TEXT NOT NULL DEFAULT '{}',      -- I18n <= 1000
    services                  TEXT NOT NULL DEFAULT '{}',      -- typed services (m12-plan §12.1)
    photos                    TEXT NOT NULL DEFAULT '[]',      -- <= 12 [{file_id, alt: I18n, sort}]
    show_map                  INTEGER NOT NULL DEFAULT 0 CHECK (show_map IN (0,1)),
    map_id                    TEXT,
    show_prices               INTEGER NOT NULL DEFAULT 0 CHECK (show_prices IN (0,1)),
    show_occupancy            INTEGER NOT NULL DEFAULT 0 CHECK (show_occupancy IN (0,1)),
    occupancy_categories      TEXT NOT NULL DEFAULT '[]',      -- berth category codes, 1..20 when show_occupancy
    poi                       TEXT NOT NULL DEFAULT '[]',      -- [{kind, x_cm, y_cm}] on the published map
    description               TEXT NOT NULL DEFAULT '{}',      -- I18n <= 1000 (meta description)
    version                   INTEGER NOT NULL DEFAULT 1,
    created_at                TIMESTAMP NOT NULL,
    updated_at                TIMESTAMP NOT NULL,
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id),
    CONSTRAINT marina_profiles_depth_check CHECK (depth_min_cm IS NULL OR depth_max_cm IS NULL OR depth_min_cm <= depth_max_cm),
    CONSTRAINT marina_profiles_position_check CHECK ((entrance_lat_e7 IS NULL) = (entrance_lon_e7 IS NULL)),
    CONSTRAINT marina_profiles_publish_check CHECK (published = 0 OR (published_at IS NOT NULL AND entrance_lat_e7 IS NOT NULL))
);
CREATE INDEX marina_profiles_published_idx ON marina_profiles (published, marina_id);

-- +goose Down
DROP TABLE marina_profiles;
