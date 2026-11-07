-- Porto M5 cash schema (PostgreSQL): cash registers, sessions with float and close, movements and the daily fiscal
-- document numbering (spec 16.3; m5-plan §5.1, §6.2). The twin is internal/migrations/sqlite/00011_cash.sql:
-- keep table, column, index and constraint names identical.
--
-- INV-55: at most one open session per register (cash_sessions_open_uidx); a closed session never changes
--         (cash_sessions_closed_*).

-- +goose Up
CREATE TABLE cash_registers (
    id          UUID PRIMARY KEY,
    marina_id   UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT true,
    is_default  BOOLEAN NOT NULL DEFAULT false,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX cash_registers_code_uidx ON cash_registers (marina_id, code);
CREATE UNIQUE INDEX cash_registers_default_uidx ON cash_registers (marina_id) WHERE is_default = true;

CREATE TABLE cash_sessions (
    id                 UUID PRIMARY KEY,
    marina_id          UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    register_id        UUID NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    opened_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    opened_at          TIMESTAMPTZ NOT NULL,
    business_date      DATE NOT NULL,
    float_cents        BIGINT NOT NULL DEFAULT 0 CHECK (float_cents >= 0),
    closed_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    closed_at          TIMESTAMPTZ,
    expected           JSONB NOT NULL DEFAULT '{}',           -- {method: cents}
    counted            JSONB NOT NULL DEFAULT '{}',           -- {method: cents}
    difference_cents   BIGINT NOT NULL DEFAULT 0,
    notes              TEXT NOT NULL DEFAULT '',
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (register_id, marina_id) REFERENCES cash_registers (id, marina_id),
    CONSTRAINT cash_sessions_closed_check CHECK (status <> 'closed' OR closed_at IS NOT NULL)
);
CREATE UNIQUE INDEX cash_sessions_open_uidx ON cash_sessions (register_id) WHERE status = 'open';
CREATE INDEX cash_sessions_marina_idx ON cash_sessions (marina_id, register_id, opened_at);

CREATE TABLE cash_movements (
    id                  UUID PRIMARY KEY,
    marina_id           UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    session_id          UUID NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('in','out')),
    amount_cents        BIGINT NOT NULL CHECK (amount_cents > 0),
    reason              TEXT NOT NULL DEFAULT '',
    created_by_user_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (session_id, marina_id) REFERENCES cash_sessions (id, marina_id)
);
CREATE INDEX cash_movements_marina_idx ON cash_movements (marina_id, session_id);

CREATE TABLE fiscal_sequences (
    marina_id    UUID NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    day          DATE NOT NULL,
    last_number  INTEGER NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    PRIMARY KEY (marina_id, day)
);

-- +goose StatementBegin
CREATE FUNCTION porto_cash_sessions_closed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'closed' THEN
        RAISE EXCEPTION 'cash_session_closed' USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER cash_sessions_closed BEFORE UPDATE OR DELETE ON cash_sessions
    FOR EACH ROW EXECUTE FUNCTION porto_cash_sessions_closed();
CREATE TRIGGER cash_movements_insert_only BEFORE UPDATE OR DELETE ON cash_movements
    FOR EACH ROW EXECUTE FUNCTION porto_insert_only();

-- +goose Down
DROP TRIGGER cash_movements_insert_only ON cash_movements;
DROP TRIGGER cash_sessions_closed ON cash_sessions;
DROP FUNCTION porto_cash_sessions_closed();
DROP TABLE fiscal_sequences;
DROP TABLE cash_movements;
DROP TABLE cash_sessions;
DROP TABLE cash_registers;
