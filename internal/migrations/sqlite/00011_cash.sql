-- Porto M5 cash schema (SQLite): cash registers, sessions with float and close, movements and the daily fiscal
-- document numbering (spec 16.3; m5-plan §5.1, §6.2). The twin is internal/migrations/postgres/00011_cash.sql:
-- keep table, column, index and constraint names identical.
--
-- INV-55: at most one open session per register (cash_sessions_open_uidx); a closed session never changes
--         (cash_sessions_closed_*).

-- +goose Up
CREATE TABLE cash_registers (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL DEFAULT '',
    active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    is_default  INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1)),
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX cash_registers_code_uidx ON cash_registers (marina_id, code);
CREATE UNIQUE INDEX cash_registers_default_uidx ON cash_registers (marina_id) WHERE is_default = 1;

CREATE TABLE cash_sessions (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    register_id        TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    opened_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    opened_at          TIMESTAMP NOT NULL,
    business_date      DATE NOT NULL,
    float_cents        INTEGER NOT NULL DEFAULT 0 CHECK (float_cents >= 0),
    closed_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    closed_at          TIMESTAMP,
    expected           TEXT NOT NULL DEFAULT '{}',           -- {method: cents}
    counted            TEXT NOT NULL DEFAULT '{}',           -- {method: cents}
    difference_cents   INTEGER NOT NULL DEFAULT 0,
    notes              TEXT NOT NULL DEFAULT '',
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (register_id, marina_id) REFERENCES cash_registers (id, marina_id),
    CONSTRAINT cash_sessions_closed_check CHECK (status <> 'closed' OR closed_at IS NOT NULL)
);
CREATE UNIQUE INDEX cash_sessions_open_uidx ON cash_sessions (register_id) WHERE status = 'open';
CREATE INDEX cash_sessions_marina_idx ON cash_sessions (marina_id, register_id, opened_at);

CREATE TABLE cash_movements (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    session_id          TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('in','out')),
    amount_cents        INTEGER NOT NULL CHECK (amount_cents > 0),
    reason              TEXT NOT NULL DEFAULT '',
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (session_id, marina_id) REFERENCES cash_sessions (id, marina_id)
);
CREATE INDEX cash_movements_marina_idx ON cash_movements (marina_id, session_id);

CREATE TABLE fiscal_sequences (
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    day          DATE NOT NULL,
    last_number  INTEGER NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    PRIMARY KEY (marina_id, day)
);

-- +goose StatementBegin
CREATE TRIGGER cash_sessions_closed_update BEFORE UPDATE ON cash_sessions
WHEN OLD.status = 'closed'
BEGIN SELECT RAISE(ABORT, 'cash_session_closed'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cash_sessions_closed_delete BEFORE DELETE ON cash_sessions
WHEN OLD.status = 'closed'
BEGIN SELECT RAISE(ABORT, 'cash_session_closed'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cash_movements_insert_only_update BEFORE UPDATE ON cash_movements
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cash_movements_insert_only_delete BEFORE DELETE ON cash_movements
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER cash_movements_insert_only_delete;
DROP TRIGGER cash_movements_insert_only_update;
DROP TRIGGER cash_sessions_closed_delete;
DROP TRIGGER cash_sessions_closed_update;
DROP TABLE fiscal_sequences;
DROP TABLE cash_movements;
DROP TABLE cash_sessions;
DROP TABLE cash_registers;
