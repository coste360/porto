-- Porto M5 payments schema (SQLite): payments and their matches to charges, refunds and payouts, fiscal documents,
-- payment provider settings, intents and provider events, SEPA mandates, payment requests, reminders, dunning
-- exclusions and the office worklist states (spec 06.7, 16.2-16.4, 28.3; m5-plan §4, §5, §6.3). The twin is
-- internal/migrations/postgres/00012_payments.sql: keep table, column, index and constraint names identical.
--
-- INV-50: charges.paid_amount_cents = Σ matches of the charge, payments.allocated_cents = Σ matches of the payment
--         (query B50; matches are insert-only).
-- INV-52: allocated + refunded never exceed the amount (payments_allocation_check; conditional UPDATEs in Go).
-- INV-56: a provider event is applied once (payment_provider_events_uidx); payments unique by provider reference.
-- INV-58: each (charge, level) reminded at most once (reminder_items_level_uidx); one run per marina and day.

-- +goose Up
CREATE TABLE payments (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number                TEXT NOT NULL,                     -- P{yyyy}-{n:06}
    customer_id           TEXT,                              -- NULL only for walk-in counter sales
    kind                  TEXT NOT NULL CHECK (kind IN ('payment','deposit','credit')),
    method                TEXT NOT NULL CHECK (method IN ('cash','card_pos','bank_transfer','cheque','other',
                              'card_online','wallet','sepa_debit','credit_note')),
    source                TEXT NOT NULL CHECK (source IN ('manual','provider','backfill','adjustment','goodwill',
                              'release','mandate','opening_balance')),
    provider_code         TEXT,
    provider_ref          TEXT,
    intent_id             TEXT,                              -- payment_intents.id (plain column)
    adjustment_charge_id  TEXT,
    release_id            TEXT,                              -- releases.id (plain column)
    mandate_id            TEXT,                              -- management_mandates.id (plain column)
    amount_cents          INTEGER NOT NULL CHECK (amount_cents > 0),
    allocated_cents       INTEGER NOT NULL DEFAULT 0,
    refunded_cents        INTEGER NOT NULL DEFAULT 0,
    fee_cents             INTEGER NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
    currency              TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    received_on           DATE NOT NULL,
    cash_session_id       TEXT,
    vat_split             TEXT NOT NULL DEFAULT '[]',        -- [{vat_rate_bp, vat_nature, gross, net, vat}]
    reference_enc         TEXT,                              -- KeyRing, AAD payment-ref:<id>
    notes                 TEXT NOT NULL DEFAULT '',
    idempotency_key       TEXT,
    idempotency_hash      TEXT,                              -- SHA-256 of the canonical input
    recorded_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    recorded_by_actor     TEXT NOT NULL DEFAULT '',
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (adjustment_charge_id, marina_id) REFERENCES charges (id, marina_id),
    FOREIGN KEY (cash_session_id, marina_id) REFERENCES cash_sessions (id, marina_id),
    CONSTRAINT payments_allocation_check CHECK (allocated_cents >= 0 AND refunded_cents >= 0
                                                AND allocated_cents + refunded_cents <= amount_cents),
    CONSTRAINT payments_credit_check CHECK ((kind = 'credit') = (method = 'credit_note')),
    CONSTRAINT payments_credit_session_check CHECK (kind <> 'credit' OR cash_session_id IS NULL),
    CONSTRAINT payments_cash_session_check CHECK (method <> 'cash' OR source <> 'manual' OR cash_session_id IS NOT NULL),
    CONSTRAINT payments_provider_check CHECK (source <> 'provider' OR provider_code IS NOT NULL)
);
CREATE UNIQUE INDEX payments_number_uidx ON payments (marina_id, number);
CREATE UNIQUE INDEX payments_idempotency_uidx ON payments (marina_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX payments_provider_ref_uidx ON payments (marina_id, provider_code, provider_ref) WHERE provider_ref IS NOT NULL;
CREATE INDEX payments_customer_idx ON payments (marina_id, customer_id, received_on);
CREATE INDEX payments_session_idx ON payments (marina_id, cash_session_id);

CREATE TABLE payment_matches (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    payment_id          TEXT NOT NULL,
    charge_id           TEXT NOT NULL,
    amount_cents        INTEGER NOT NULL CHECK (amount_cents <> 0),
    reverses_match_id   TEXT,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_by_actor    TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (payment_id, marina_id) REFERENCES payments (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id),
    FOREIGN KEY (reverses_match_id, marina_id) REFERENCES payment_matches (id, marina_id)
);
CREATE INDEX payment_matches_payment_idx ON payment_matches (payment_id);
CREATE INDEX payment_matches_charge_idx ON payment_matches (charge_id);
CREATE INDEX payment_matches_marina_idx ON payment_matches (marina_id, created_at);
CREATE UNIQUE INDEX payment_matches_reverses_uidx ON payment_matches (reverses_match_id) WHERE reverses_match_id IS NOT NULL;

CREATE TABLE refunds (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    number               TEXT NOT NULL,                      -- R{yyyy}-{n:06}
    payment_id           TEXT,
    kind                 TEXT NOT NULL CHECK (kind IN ('refund','payout')),
    method               TEXT NOT NULL CHECK (method IN ('cash','card_pos','bank_transfer','cheque','other',
                             'card_online','wallet','sepa_debit')),
    source               TEXT NOT NULL CHECK (source IN ('manual','provider')),
    provider_code        TEXT,
    provider_ref         TEXT,
    intent_id            TEXT,
    status               TEXT NOT NULL CHECK (status IN ('pending','succeeded','failed')),
    amount_cents         INTEGER NOT NULL CHECK (amount_cents > 0),
    reason               TEXT NOT NULL DEFAULT '',
    business_date        DATE NOT NULL,
    cash_session_id      TEXT,
    mandate_id           TEXT,                               -- management_mandates.id (plain column)
    customer_id          TEXT,
    failure_code         TEXT,
    completed_at         TIMESTAMP,
    idempotency_key      TEXT,
    recorded_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    recorded_by_actor    TEXT NOT NULL DEFAULT '',
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (payment_id, marina_id) REFERENCES payments (id, marina_id),
    FOREIGN KEY (cash_session_id, marina_id) REFERENCES cash_sessions (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    CONSTRAINT refunds_payment_check CHECK ((kind = 'payout') = (payment_id IS NULL)),
    CONSTRAINT refunds_provider_check CHECK (source <> 'provider' OR provider_code IS NOT NULL)
);
CREATE UNIQUE INDEX refunds_number_uidx ON refunds (marina_id, number);
CREATE UNIQUE INDEX refunds_provider_ref_uidx ON refunds (marina_id, provider_code, provider_ref) WHERE provider_ref IS NOT NULL;
CREATE UNIQUE INDEX refunds_idempotency_uidx ON refunds (marina_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX refunds_payment_idx ON refunds (marina_id, payment_id);
CREATE INDEX refunds_session_idx ON refunds (marina_id, cash_session_id);

CREATE TABLE fiscal_documents (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    payment_id     TEXT NOT NULL,
    charge_id      TEXT NOT NULL,
    refund_id      TEXT,
    driver         TEXT NOT NULL,
    number         TEXT NOT NULL,                            -- YYYYMMDD-NNNN
    business_date  DATE NOT NULL,
    status         TEXT NOT NULL CHECK (status IN ('pending','issued','failed','void')),
    lines          TEXT NOT NULL DEFAULT '[]',
    vat            TEXT NOT NULL DEFAULT '[]',
    total_cents    INTEGER NOT NULL,
    external_ref   TEXT,
    error_code     TEXT,
    attempts       INTEGER NOT NULL DEFAULT 0,
    issued_at      TIMESTAMP,
    voided_at      TIMESTAMP,
    version        INTEGER NOT NULL DEFAULT 1,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (payment_id, marina_id) REFERENCES payments (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id),
    FOREIGN KEY (refund_id, marina_id) REFERENCES refunds (id, marina_id)
);
CREATE UNIQUE INDEX fiscal_documents_number_uidx ON fiscal_documents (marina_id, number);
CREATE INDEX fiscal_documents_day_idx ON fiscal_documents (marina_id, business_date, status);
CREATE INDEX fiscal_documents_payment_idx ON fiscal_documents (marina_id, payment_id);

CREATE TABLE payment_provider_settings (
    id                     TEXT PRIMARY KEY,
    marina_id              TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    provider_code          TEXT NOT NULL,
    enabled                INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    mode                   TEXT NOT NULL DEFAULT 'test' CHECK (mode IN ('test','live')),
    credentials_enc        TEXT,                             -- KeyRing, AAD paycreds:<marina>:<provider>
    webhook_secret_enc     TEXT,                             -- KeyRing, AAD paycreds:<marina>:<provider>
    publishable_key_last4  TEXT NOT NULL DEFAULT '',
    sepa_enabled           INTEGER NOT NULL DEFAULT 0 CHECK (sepa_enabled IN (0,1)),
    methods                TEXT NOT NULL DEFAULT '[]',
    statement_descriptor   TEXT NOT NULL DEFAULT '',
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX payment_provider_settings_uidx ON payment_provider_settings (marina_id, provider_code);

CREATE TABLE payment_intents (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    purpose               TEXT NOT NULL CHECK (purpose IN ('payment_request','transit','sepa_collect','mandate_setup')),
    provider_code         TEXT NOT NULL,
    customer_id           TEXT NOT NULL,
    amount_cents          INTEGER NOT NULL CHECK (amount_cents >= 0),
    currency              TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    method                TEXT,                              -- requested method: card, sepa_debit
    request_id            TEXT,                              -- payment_requests.id (plain column)
    contract_id           TEXT,
    sepa_mandate_id       TEXT,                              -- sepa_mandates.id (plain column)
    provider_session_ref  TEXT,
    provider_payment_ref  TEXT,
    checkout_url          TEXT,
    status                TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','open','processing','succeeded',
                              'failed','expired','cancelled')),
    attempt               INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    idempotency_key       TEXT NOT NULL,                     -- <purpose>:<subject>:<attempt>
    expires_at            TIMESTAMP,
    last_checked_at       TIMESTAMP,
    failure_code          TEXT,
    succeeded_at          TIMESTAMP,
    created_by_actor      TEXT NOT NULL DEFAULT '',
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id)
);
CREATE UNIQUE INDEX payment_intents_idempotency_uidx ON payment_intents (marina_id, idempotency_key);
CREATE UNIQUE INDEX payment_intents_session_uidx ON payment_intents (provider_code, provider_session_ref) WHERE provider_session_ref IS NOT NULL;
CREATE UNIQUE INDEX payment_intents_payment_uidx ON payment_intents (provider_code, provider_payment_ref) WHERE provider_payment_ref IS NOT NULL;
CREATE INDEX payment_intents_status_idx ON payment_intents (marina_id, status, last_checked_at);
CREATE INDEX payment_intents_contract_idx ON payment_intents (marina_id, contract_id);

CREATE TABLE payment_intent_charges (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    intent_id     TEXT NOT NULL,
    charge_id     TEXT NOT NULL,
    amount_cents  INTEGER NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    open_flag     INTEGER NOT NULL DEFAULT 1 CHECK (open_flag IN (0,1)),  -- 1 while the intent is open/processing
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (intent_id, marina_id) REFERENCES payment_intents (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id)
);
CREATE UNIQUE INDEX payment_intent_charges_uidx ON payment_intent_charges (intent_id, charge_id);
CREATE UNIQUE INDEX payment_intent_charges_open_uidx ON payment_intent_charges (charge_id) WHERE open_flag = 1;
CREATE INDEX payment_intent_charges_marina_idx ON payment_intent_charges (marina_id, charge_id);

CREATE TABLE payment_provider_events (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    provider_code  TEXT NOT NULL,
    event_id       TEXT NOT NULL,
    kind           TEXT NOT NULL DEFAULT '',
    intent_id      TEXT,
    status         TEXT NOT NULL CHECK (status IN ('applied','ignored','deferred')),
    received_at    TIMESTAMP NOT NULL,
    created_at     TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX payment_provider_events_uidx ON payment_provider_events (marina_id, provider_code, event_id);
CREATE INDEX payment_provider_events_received_idx ON payment_provider_events (marina_id, received_at);

CREATE TABLE sepa_mandates (
    id                         TEXT PRIMARY KEY,
    marina_id                  TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id                TEXT NOT NULL,
    provider_code              TEXT NOT NULL,
    provider_customer_ref_enc  TEXT,                         -- KeyRing, AAD sepa-customer:<id>
    payment_method_ref_enc     TEXT,                         -- KeyRing, AAD sepa-pm:<id>
    mandate_ref                TEXT,
    iban_last4                 TEXT NOT NULL DEFAULT '',
    status                     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','revoked','failed')),
    setup_intent_id            TEXT,                         -- payment_intents.id (plain column)
    activated_at               TIMESTAMP,
    revoked_at                 TIMESTAMP,
    created_by_user_id         TEXT REFERENCES users (id) ON DELETE SET NULL,
    version                    INTEGER NOT NULL DEFAULT 1,
    created_at                 TIMESTAMP NOT NULL,
    updated_at                 TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX sepa_mandates_active_uidx ON sepa_mandates (customer_id, provider_code) WHERE status = 'active';
CREATE INDEX sepa_mandates_marina_idx ON sepa_mandates (marina_id, customer_id);

CREATE TABLE payment_requests (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id         TEXT NOT NULL,
    token_hash          TEXT NOT NULL,                       -- SHA-256 of the token, never the token
    amount_cents        INTEGER NOT NULL CHECK (amount_cents >= 0),
    currency            TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','paid','cancelled','expired')),
    source              TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','auto','reminder','transit')),
    expires_at          TIMESTAMP,
    sent_at             TIMESTAMP,
    sent_count          INTEGER NOT NULL DEFAULT 0 CHECK (sent_count >= 0),
    last_message_id     TEXT,
    paid_at             TIMESTAMP,
    cancelled_at        TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_by_actor    TEXT NOT NULL DEFAULT '',
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX payment_requests_token_uidx ON payment_requests (token_hash);
CREATE INDEX payment_requests_marina_idx ON payment_requests (marina_id, status, created_at);
CREATE INDEX payment_requests_customer_idx ON payment_requests (marina_id, customer_id);

CREATE TABLE payment_request_charges (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    request_id    TEXT NOT NULL,
    charge_id     TEXT NOT NULL,
    amount_cents  INTEGER NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    created_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (request_id, marina_id) REFERENCES payment_requests (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id)
);
CREATE UNIQUE INDEX payment_request_charges_uidx ON payment_request_charges (request_id, charge_id);
CREATE INDEX payment_request_charges_marina_idx ON payment_request_charges (marina_id, charge_id);

CREATE TABLE reminder_runs (
    id                TEXT PRIMARY KEY,
    marina_id         TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    run_on            DATE NOT NULL,
    status            TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','done','failed')),
    counts            TEXT NOT NULL DEFAULT '{}',
    started_at        TIMESTAMP NOT NULL,
    finished_at       TIMESTAMP,
    created_by_actor  TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX reminder_runs_day_uidx ON reminder_runs (marina_id, run_on);

CREATE TABLE reminder_items (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    run_id              TEXT,                                -- NULL for manual reminders
    charge_id           TEXT NOT NULL,
    customer_id         TEXT NOT NULL,
    level               TEXT NOT NULL CHECK (level IN ('pre_due','due','courteous','formal','final_notice')),
    status              TEXT NOT NULL CHECK (status IN ('queued','awaiting_approval','skipped_excluded','skipped_paid',
                            'skipped_no_email','sent','dismissed')),
    request_id          TEXT,                                -- payment_requests.id (plain column)
    message_id          TEXT,                                -- message_log.id (plain column)
    decided_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    decided_at          TIMESTAMP,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (run_id, marina_id) REFERENCES reminder_runs (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX reminder_items_level_uidx ON reminder_items (charge_id, level);
CREATE INDEX reminder_items_marina_idx ON reminder_items (marina_id, status, created_at);
CREATE INDEX reminder_items_run_idx ON reminder_items (marina_id, run_id);

CREATE TABLE dunning_exclusions (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id         TEXT,
    charge_id           TEXT,
    kind                TEXT NOT NULL CHECK (kind IN ('payment_plan','disputed','other')),
    until_on            DATE,
    note                TEXT NOT NULL DEFAULT '',
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    removed_at          TIMESTAMP,
    removed_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (charge_id, marina_id) REFERENCES charges (id, marina_id),
    CONSTRAINT dunning_exclusions_target_check CHECK ((customer_id IS NULL) <> (charge_id IS NULL))
);
CREATE INDEX dunning_exclusions_marina_idx ON dunning_exclusions (marina_id, customer_id, charge_id);

CREATE TABLE work_item_states (
    id                TEXT PRIMARY KEY,
    marina_id         TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    source_type       TEXT NOT NULL,
    source_id         TEXT NOT NULL,
    assignee_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    status            TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','done','dismissed')),
    note              TEXT NOT NULL DEFAULT '',
    occurrence        TEXT NOT NULL DEFAULT '',
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX work_item_states_uidx ON work_item_states (marina_id, source_type, source_id);

-- Insert-only matches (INV-50) and frozen fiscal documents: an issued document only becomes void.
-- +goose StatementBegin
CREATE TRIGGER payment_matches_insert_only_update BEFORE UPDATE ON payment_matches
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER payment_matches_insert_only_delete BEFORE DELETE ON payment_matches
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER fiscal_documents_immutable_update BEFORE UPDATE ON fiscal_documents
WHEN OLD.status = 'void'
    OR (OLD.status = 'issued' AND (
        NEW.status NOT IN ('issued','void')
        OR NEW.number IS NOT OLD.number
        OR NEW.lines IS NOT OLD.lines
        OR NEW.vat IS NOT OLD.vat
        OR NEW.total_cents IS NOT OLD.total_cents
        OR NEW.payment_id IS NOT OLD.payment_id
        OR NEW.charge_id IS NOT OLD.charge_id
        OR NEW.business_date IS NOT OLD.business_date))
BEGIN SELECT RAISE(ABORT, 'fiscal_document_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER fiscal_documents_immutable_delete BEFORE DELETE ON fiscal_documents
WHEN OLD.status IN ('issued','void')
BEGIN SELECT RAISE(ABORT, 'fiscal_document_immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER fiscal_documents_immutable_delete;
DROP TRIGGER fiscal_documents_immutable_update;
DROP TRIGGER payment_matches_insert_only_delete;
DROP TRIGGER payment_matches_insert_only_update;
DROP TABLE work_item_states;
DROP TABLE dunning_exclusions;
DROP TABLE reminder_items;
DROP TABLE reminder_runs;
DROP TABLE payment_request_charges;
DROP TABLE payment_requests;
DROP TABLE sepa_mandates;
DROP TABLE payment_provider_events;
DROP TABLE payment_intent_charges;
DROP TABLE payment_intents;
DROP TABLE payment_provider_settings;
DROP TABLE fiscal_documents;
DROP TABLE refunds;
DROP TABLE payment_matches;
DROP TABLE payments;
