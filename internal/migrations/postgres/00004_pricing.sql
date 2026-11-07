-- Porto M2 pricing schema (PostgreSQL): pricing settings, revenue classes, items, berth categories, price lists
-- with their rate periods, length bands, rules and item snapshots, and the immutability triggers of published
-- lists (spec 06, 12; m2-plan §3, INV-22, INV-26). The SQLite twin is internal/migrations/sqlite/00004_pricing.sql:
-- keep table, column, index names and trigger behaviour identical.

-- +goose Up
CREATE TABLE pricing_settings (
    id                          UUID PRIMARY KEY,
    marina_id                   UUID NOT NULL UNIQUE REFERENCES marinas (id) ON DELETE CASCADE,
    default_vat_rate_bp         INTEGER NOT NULL DEFAULT 2200 CHECK (default_vat_rate_bp BETWEEN 0 AND 10000),
    default_price_includes_vat  BOOLEAN NOT NULL DEFAULT false,
    activation_seq              BIGINT NOT NULL DEFAULT 0,   -- bumped by every activation: serialises activations (INV-24)
    version                     INTEGER NOT NULL DEFAULT 1,
    created_at                  TIMESTAMPTZ NOT NULL,
    updated_at                  TIMESTAMPTZ NOT NULL
);

CREATE TABLE revenue_classes (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name        JSONB NOT NULL,                    -- I18nText
    parent_id   UUID,                             -- subcategory of; one level only (service check)
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (parent_id, marina_id) REFERENCES revenue_classes (id, marina_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX revenue_classes_marina_code_uidx ON revenue_classes (marina_id, code);

CREATE TABLE items (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32 AND code = upper(code)),
    name                JSONB NOT NULL,            -- I18nText
    description         JSONB NOT NULL DEFAULT '{}',
    item_type           TEXT NOT NULL CHECK (item_type IN ('berth_fee','service','utility','product','labour','material','fee','deposit')),
    unit                TEXT NOT NULL CHECK (unit IN ('night','month','season','year','m','m2','kwh','m3','hour','piece','lump')),
    vat_rate_bp         INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature          TEXT CHECK (vat_nature IS NULL OR vat_nature IN
                          ('N1','N2.1','N2.2','N3.1','N3.2','N3.3','N3.4','N3.5','N3.6','N4','N5',
                           'N6.1','N6.2','N6.3','N6.4','N6.5','N6.6','N6.7','N6.8','N6.9','N7')),
    price_includes_vat  BOOLEAN NOT NULL DEFAULT false,
    revenue_class_id    UUID NOT NULL,
    external_code       TEXT NOT NULL DEFAULT '' CHECK (char_length(external_code) <= 64),
    active              BOOLEAN NOT NULL DEFAULT true,
    online_visible      BOOLEAN NOT NULL DEFAULT false,
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (revenue_class_id, marina_id) REFERENCES revenue_classes (id, marina_id),
    CHECK ((vat_rate_bp = 0) = (vat_nature IS NOT NULL))
);
CREATE UNIQUE INDEX items_marina_code_uidx ON items (marina_id, code);
CREATE INDEX items_marina_type_idx ON items (marina_id, item_type, active, sort_order);

CREATE TABLE berth_categories (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name        JSONB NOT NULL,                    -- I18nText
    applies_to  JSONB NOT NULL DEFAULT '["berth"]',-- subset of berth, dynamic_area, land_resource (service check)
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX berth_categories_marina_code_uidx ON berth_categories (marina_id, code);

CREATE TABLE price_lists (
    id                     UUID PRIMARY KEY,
    marina_id              UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                   TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name                   JSONB NOT NULL,         -- I18nText
    status                 TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    valid_from             DATE NOT NULL,
    valid_to               DATE NOT NULL,
    contract_types         JSONB NOT NULL DEFAULT '[]',  -- JSON array of pricing.ContractType
    settings               JSONB NOT NULL DEFAULT '{}',  -- pricing.ListSettings JSON (+ publish_matrix flag)
    source_price_list_id   UUID,                  -- renewal/copy lineage, same marina
    content_sha256         TEXT,                  -- pricing.CatalogHash at activation
    activated_at           TIMESTAMPTZ,
    activated_by_user_id   UUID REFERENCES users (id) ON DELETE SET NULL,
    archived_at            TIMESTAMPTZ,
    next_rule_seq          BIGINT NOT NULL DEFAULT 1,
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (source_price_list_id, marina_id) REFERENCES price_lists (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (valid_to >= valid_from),
    CHECK (status = 'draft' OR content_sha256 IS NOT NULL)
);
CREATE UNIQUE INDEX price_lists_marina_code_uidx ON price_lists (marina_id, code);
CREATE INDEX price_lists_marina_status_idx ON price_lists (marina_id, status, valid_from);

CREATE TABLE rate_periods (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL,
    price_list_id  UUID NOT NULL,
    code           TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name           JSONB NOT NULL,                 -- I18nText
    kind           TEXT NOT NULL CHECK (kind IN ('season','event')),
    color          TEXT NOT NULL DEFAULT 'chart-1' CHECK (color IN ('chart-1','chart-2','chart-3','chart-4','chart-5')),
    ranges         JSONB NOT NULL,                 -- JSON [{from, to}] inclusive nights
    sort_order     INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX rate_periods_list_code_uidx ON rate_periods (price_list_id, code);
CREATE INDEX rate_periods_marina_idx ON rate_periods (marina_id, price_list_id);

CREATE TABLE price_list_bands (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL,
    price_list_id  UUID NOT NULL,
    code           TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    max_cm         BIGINT CHECK (max_cm IS NULL OR max_cm BETWEEN 1 AND 20000),
    category_code  TEXT,
    sort_order     INTEGER NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX price_list_bands_code_uidx ON price_list_bands (price_list_id, code);
CREATE UNIQUE INDEX price_list_bands_order_uidx ON price_list_bands (price_list_id, sort_order);
CREATE INDEX price_list_bands_marina_idx ON price_list_bands (marina_id, price_list_id);

CREATE TABLE price_rules (
    id             UUID PRIMARY KEY,
    marina_id      UUID NOT NULL,
    price_list_id  UUID NOT NULL,
    seq            BIGINT NOT NULL,              -- recency (12.4 step 1)
    stage          TEXT NOT NULL CHECK (stage IN ('base','modifier','discount')),
    name           TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    item_id        UUID,                          -- base rules: required (CHECK); FK for in_use checks
    priority       INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN -1000 AND 1000),
    stackable      BOOLEAN NOT NULL DEFAULT false,
    active         BOOLEAN NOT NULL DEFAULT true,
    body           JSONB NOT NULL,                 -- JSON: basis, unit, amounts, includes, targets, conditions, effect, discount_kind, promo_code, scope
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
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
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL,
    price_list_id       UUID NOT NULL,
    item_id             UUID NOT NULL,
    code                TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    item_type           TEXT NOT NULL CHECK (item_type IN ('berth_fee','service','utility','product','labour','material','fee','deposit')),
    unit                TEXT NOT NULL CHECK (unit IN ('night','month','season','year','m','m2','kwh','m3','hour','piece','lump')),
    vat_rate_bp         INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature          TEXT,
    price_includes_vat  BOOLEAN NOT NULL,
    revenue_class       TEXT NOT NULL DEFAULT '',
    external_code       TEXT NOT NULL DEFAULT '',
    active              BOOLEAN NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL,
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
CREATE FUNCTION porto_price_list_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'draft' THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'price_list_immutable' USING ERRCODE = 'P0001';
    END IF;
    IF NEW.id IS NOT DISTINCT FROM OLD.id AND NEW.marina_id IS NOT DISTINCT FROM OLD.marina_id
        AND NEW.code IS NOT DISTINCT FROM OLD.code AND NEW.name IS NOT DISTINCT FROM OLD.name
        AND NEW.valid_from IS NOT DISTINCT FROM OLD.valid_from
        AND NEW.contract_types IS NOT DISTINCT FROM OLD.contract_types
        AND NEW.settings IS NOT DISTINCT FROM OLD.settings
        AND NEW.source_price_list_id IS NOT DISTINCT FROM OLD.source_price_list_id
        AND NEW.content_sha256 IS NOT DISTINCT FROM OLD.content_sha256
        AND NEW.activated_at IS NOT DISTINCT FROM OLD.activated_at
        AND (NEW.activated_by_user_id IS NOT DISTINCT FROM OLD.activated_by_user_id OR NEW.activated_by_user_id IS NULL)
        AND NEW.next_rule_seq IS NOT DISTINCT FROM OLD.next_rule_seq
        AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
        AND NEW.status IN (OLD.status, 'archived') AND NEW.valid_to <= OLD.valid_to THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'price_list_immutable' USING ERRCODE = 'P0001';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER price_lists_immutable BEFORE UPDATE OR DELETE ON price_lists
    FOR EACH ROW EXECUTE FUNCTION porto_price_list_immutable();

-- Periods, bands, rules and item snapshots of a published list are frozen. A cascade from a deleted (draft) parent finds no
-- parent row and passes.
-- +goose StatementBegin
CREATE FUNCTION porto_price_list_child_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') AND EXISTS (
        SELECT 1 FROM price_lists WHERE id = OLD.price_list_id AND status <> 'draft') THEN
        RAISE EXCEPTION 'price_list_immutable' USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') AND EXISTS (
        SELECT 1 FROM price_lists WHERE id = NEW.price_list_id AND status <> 'draft') THEN
        RAISE EXCEPTION 'price_list_immutable' USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER rate_periods_immutable BEFORE INSERT OR UPDATE OR DELETE ON rate_periods
    FOR EACH ROW EXECUTE FUNCTION porto_price_list_child_immutable();
CREATE TRIGGER price_list_bands_immutable BEFORE INSERT OR UPDATE OR DELETE ON price_list_bands
    FOR EACH ROW EXECUTE FUNCTION porto_price_list_child_immutable();
CREATE TRIGGER price_rules_immutable BEFORE INSERT OR UPDATE OR DELETE ON price_rules
    FOR EACH ROW EXECUTE FUNCTION porto_price_list_child_immutable();
CREATE TRIGGER price_list_items_immutable BEFORE INSERT OR UPDATE OR DELETE ON price_list_items
    FOR EACH ROW EXECUTE FUNCTION porto_price_list_child_immutable();

-- +goose Down
DROP TRIGGER price_list_items_immutable ON price_list_items;
DROP TRIGGER price_rules_immutable ON price_rules;
DROP TRIGGER price_list_bands_immutable ON price_list_bands;
DROP TRIGGER rate_periods_immutable ON rate_periods;
DROP FUNCTION porto_price_list_child_immutable();
DROP TRIGGER price_lists_immutable ON price_lists;
DROP FUNCTION porto_price_list_immutable();
DROP TABLE price_list_items;
DROP TABLE price_rules;
DROP TABLE price_list_bands;
DROP TABLE rate_periods;
DROP TABLE price_lists;
DROP TABLE berth_categories;
DROP TABLE items;
DROP TABLE revenue_classes;
DROP TABLE pricing_settings;
