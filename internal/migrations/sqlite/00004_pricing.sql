-- Porto M2 pricing schema (SQLite): pricing settings, revenue classes, items, berth categories, price lists
-- with their rate periods, length bands, rules and item snapshots, and the immutability triggers of published
-- lists (spec 06, 12; m2-plan §3, INV-22, INV-26). The PostgreSQL twin is internal/migrations/postgres/00004_pricing.sql:
-- keep table, column, index names and trigger behaviour identical.

-- +goose Up
CREATE TABLE pricing_settings (
    id                          TEXT PRIMARY KEY,
    marina_id                   TEXT NOT NULL UNIQUE REFERENCES marinas (id) ON DELETE CASCADE,
    default_vat_rate_bp         INTEGER NOT NULL DEFAULT 2200 CHECK (default_vat_rate_bp BETWEEN 0 AND 10000),
    default_price_includes_vat  INTEGER NOT NULL DEFAULT 0 CHECK (default_price_includes_vat IN (0,1)),
    activation_seq              INTEGER NOT NULL DEFAULT 0,   -- bumped by every activation: serialises activations (INV-24)
    version                     INTEGER NOT NULL DEFAULT 1,
    created_at                  TIMESTAMP NOT NULL,
    updated_at                  TIMESTAMP NOT NULL
);

CREATE TABLE revenue_classes (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL,                    -- I18nText
    parent_id   TEXT,                             -- subcategory of; one level only (service check)
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMP,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (parent_id, marina_id) REFERENCES revenue_classes (id, marina_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX revenue_classes_marina_code_uidx ON revenue_classes (marina_id, code);

CREATE TABLE items (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32 AND code = upper(code)),
    name                TEXT NOT NULL,            -- I18nText
    description         TEXT NOT NULL DEFAULT '{}',
    item_type           TEXT NOT NULL CHECK (item_type IN ('berth_fee','service','utility','product','labour','material','fee','deposit')),
    unit                TEXT NOT NULL CHECK (unit IN ('night','month','season','year','m','m2','kwh','m3','hour','piece','lump')),
    vat_rate_bp         INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature          TEXT CHECK (vat_nature IS NULL OR vat_nature IN
                          ('N1','N2.1','N2.2','N3.1','N3.2','N3.3','N3.4','N3.5','N3.6','N4','N5',
                           'N6.1','N6.2','N6.3','N6.4','N6.5','N6.6','N6.7','N6.8','N6.9','N7')),
    price_includes_vat  INTEGER NOT NULL DEFAULT 0 CHECK (price_includes_vat IN (0,1)),
    revenue_class_id    TEXT NOT NULL,
    external_code       TEXT NOT NULL DEFAULT '' CHECK (length(external_code) <= 64),
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    online_visible      INTEGER NOT NULL DEFAULT 0 CHECK (online_visible IN (0,1)),
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (revenue_class_id, marina_id) REFERENCES revenue_classes (id, marina_id),
    CHECK ((vat_rate_bp = 0) = (vat_nature IS NOT NULL))
);
CREATE UNIQUE INDEX items_marina_code_uidx ON items (marina_id, code);
CREATE INDEX items_marina_type_idx ON items (marina_id, item_type, active, sort_order);

CREATE TABLE berth_categories (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL,                    -- I18nText
    applies_to  TEXT NOT NULL DEFAULT '["berth"]',-- subset of berth, dynamic_area, land_resource (service check)
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMP,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX berth_categories_marina_code_uidx ON berth_categories (marina_id, code);

CREATE TABLE price_lists (
    id                     TEXT PRIMARY KEY,
    marina_id              TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                   TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name                   TEXT NOT NULL,         -- I18nText
    status                 TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    valid_from             DATE NOT NULL,
    valid_to               DATE NOT NULL,
    contract_types         TEXT NOT NULL DEFAULT '[]',  -- JSON array of pricing.ContractType
    settings               TEXT NOT NULL DEFAULT '{}',  -- pricing.ListSettings JSON (+ publish_matrix flag)
    source_price_list_id   TEXT,                  -- renewal/copy lineage, same marina
    content_sha256         TEXT,                  -- pricing.CatalogHash at activation
    activated_at           TIMESTAMP,
    activated_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    archived_at            TIMESTAMP,
    next_rule_seq          INTEGER NOT NULL DEFAULT 1,
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (source_price_list_id, marina_id) REFERENCES price_lists (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (valid_to >= valid_from),
    CHECK (status = 'draft' OR content_sha256 IS NOT NULL)
);
CREATE UNIQUE INDEX price_lists_marina_code_uidx ON price_lists (marina_id, code);
CREATE INDEX price_lists_marina_status_idx ON price_lists (marina_id, status, valid_from);

CREATE TABLE rate_periods (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    price_list_id  TEXT NOT NULL,
    code           TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name           TEXT NOT NULL,                 -- I18nText
    kind           TEXT NOT NULL CHECK (kind IN ('season','event')),
    color          TEXT NOT NULL DEFAULT 'chart-1' CHECK (color IN ('chart-1','chart-2','chart-3','chart-4','chart-5')),
    ranges         TEXT NOT NULL,                 -- JSON [{from, to}] inclusive nights
    sort_order     INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX rate_periods_list_code_uidx ON rate_periods (price_list_id, code);
CREATE INDEX rate_periods_marina_idx ON rate_periods (marina_id, price_list_id);

CREATE TABLE price_list_bands (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    price_list_id  TEXT NOT NULL,
    code           TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    max_cm         INTEGER CHECK (max_cm IS NULL OR max_cm BETWEEN 1 AND 20000),
    category_code  TEXT,
    sort_order     INTEGER NOT NULL,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX price_list_bands_code_uidx ON price_list_bands (price_list_id, code);
CREATE UNIQUE INDEX price_list_bands_order_uidx ON price_list_bands (price_list_id, sort_order);
CREATE INDEX price_list_bands_marina_idx ON price_list_bands (marina_id, price_list_id);

CREATE TABLE price_rules (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    price_list_id  TEXT NOT NULL,
    seq            INTEGER NOT NULL,              -- recency (12.4 step 1)
    stage          TEXT NOT NULL CHECK (stage IN ('base','modifier','discount')),
    name           TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    item_id        TEXT,                          -- base rules: required (CHECK); FK for in_use checks
    priority       INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN -1000 AND 1000),
    stackable      INTEGER NOT NULL DEFAULT 0 CHECK (stackable IN (0,1)),
    active         INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    body           TEXT NOT NULL,                 -- JSON: basis, unit, amounts, includes, targets, conditions, effect, discount_kind, promo_code, scope
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CHECK (stage <> 'base' OR item_id IS NOT NULL)
);
CREATE UNIQUE INDEX price_rules_list_seq_uidx ON price_rules (price_list_id, seq);
CREATE INDEX price_rules_list_stage_idx ON price_rules (price_list_id, stage);
CREATE INDEX price_rules_item_idx ON price_rules (marina_id, item_id);

-- Engine fields of the items a published list references, copied at activation (INV-26): a list that is not a
-- draft prices with these rows, never with the live items, so items stay editable and the list's content hash
-- stays equal to CatalogHash(LoadCatalog(list)). revenue_class is the class code at activation. Frozen with the
-- list (INV-22 triggers below).
CREATE TABLE price_list_items (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL,
    price_list_id       TEXT NOT NULL,
    item_id             TEXT NOT NULL,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    item_type           TEXT NOT NULL CHECK (item_type IN ('berth_fee','service','utility','product','labour','material','fee','deposit')),
    unit                TEXT NOT NULL CHECK (unit IN ('night','month','season','year','m','m2','kwh','m3','hour','piece','lump')),
    vat_rate_bp         INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature          TEXT,
    price_includes_vat  INTEGER NOT NULL CHECK (price_includes_vat IN (0,1)),
    revenue_class       TEXT NOT NULL DEFAULT '',
    external_code       TEXT NOT NULL DEFAULT '',
    active              INTEGER NOT NULL CHECK (active IN (0,1)),
    created_at          TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK ((vat_rate_bp = 0) = (vat_nature IS NOT NULL))
);
CREATE UNIQUE INDEX price_list_items_list_item_uidx ON price_list_items (price_list_id, item_id);
CREATE INDEX price_list_items_marina_idx ON price_list_items (marina_id, price_list_id);
CREATE INDEX price_list_items_item_idx ON price_list_items (marina_id, item_id);

-- INV-22: a published (active or archived) list changes only its status towards archived, valid_to by
-- truncation, archived_at, version and updated_at (activated_by_user_id may become NULL when the user is
-- deleted). The service checks first (409 price_list_immutable); the triggers are the backstop.
-- +goose StatementBegin
CREATE TRIGGER price_lists_immutable_update BEFORE UPDATE ON price_lists
WHEN OLD.status <> 'draft' AND NOT (
    NEW.id IS OLD.id AND NEW.marina_id IS OLD.marina_id AND NEW.code IS OLD.code AND NEW.name IS OLD.name
    AND NEW.valid_from IS OLD.valid_from AND NEW.contract_types IS OLD.contract_types
    AND NEW.settings IS OLD.settings AND NEW.source_price_list_id IS OLD.source_price_list_id
    AND NEW.content_sha256 IS OLD.content_sha256 AND NEW.activated_at IS OLD.activated_at
    AND (NEW.activated_by_user_id IS OLD.activated_by_user_id OR NEW.activated_by_user_id IS NULL)
    AND NEW.next_rule_seq IS OLD.next_rule_seq AND NEW.created_at IS OLD.created_at
    AND NEW.status IN (OLD.status, 'archived') AND NEW.valid_to <= OLD.valid_to
)
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_lists_immutable_delete BEFORE DELETE ON price_lists
WHEN OLD.status <> 'draft'
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER rate_periods_immutable_insert BEFORE INSERT ON rate_periods
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = NEW.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER rate_periods_immutable_update BEFORE UPDATE ON rate_periods
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id IN (OLD.price_list_id, NEW.price_list_id) AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER rate_periods_immutable_delete BEFORE DELETE ON rate_periods
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = OLD.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_list_bands_immutable_insert BEFORE INSERT ON price_list_bands
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = NEW.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_list_bands_immutable_update BEFORE UPDATE ON price_list_bands
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id IN (OLD.price_list_id, NEW.price_list_id) AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_list_bands_immutable_delete BEFORE DELETE ON price_list_bands
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = OLD.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_rules_immutable_insert BEFORE INSERT ON price_rules
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = NEW.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_rules_immutable_update BEFORE UPDATE ON price_rules
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id IN (OLD.price_list_id, NEW.price_list_id) AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_rules_immutable_delete BEFORE DELETE ON price_rules
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = OLD.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER price_list_items_immutable_insert BEFORE INSERT ON price_list_items
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = NEW.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_list_items_immutable_update BEFORE UPDATE ON price_list_items
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id IN (OLD.price_list_id, NEW.price_list_id) AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER price_list_items_immutable_delete BEFORE DELETE ON price_list_items
WHEN EXISTS (SELECT 1 FROM price_lists WHERE id = OLD.price_list_id AND status <> 'draft')
BEGIN SELECT RAISE(ABORT, 'price_list_immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER price_list_items_immutable_delete;
DROP TRIGGER price_list_items_immutable_update;
DROP TRIGGER price_list_items_immutable_insert;
DROP TRIGGER price_rules_immutable_delete;
DROP TRIGGER price_rules_immutable_update;
DROP TRIGGER price_rules_immutable_insert;
DROP TRIGGER price_list_bands_immutable_delete;
DROP TRIGGER price_list_bands_immutable_update;
DROP TRIGGER price_list_bands_immutable_insert;
DROP TRIGGER rate_periods_immutable_delete;
DROP TRIGGER rate_periods_immutable_update;
DROP TRIGGER rate_periods_immutable_insert;
DROP TRIGGER price_lists_immutable_delete;
DROP TRIGGER price_lists_immutable_update;
DROP TABLE price_list_items;
DROP TABLE price_rules;
DROP TABLE price_list_bands;
DROP TABLE rate_periods;
DROP TABLE price_lists;
DROP TABLE berth_categories;
DROP TABLE items;
DROP TABLE revenue_classes;
DROP TABLE pricing_settings;
