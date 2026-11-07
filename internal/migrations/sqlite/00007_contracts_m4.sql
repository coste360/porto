-- Porto M4 contracts schema (SQLite): contract settings, instalments, renewal campaigns and proposals, addenda,
-- charter fleets and contracts, waiting list, plus the M4 columns of contracts, quotes and deadline_policies (spec
-- 06, 13; m4-plan §5.1). The twin is internal/migrations/postgres/00007_contracts_m4.sql: keep table, column, index
-- and constraint names identical.
--
-- INV-31: the sum of the live instalments of a plan equals the contract total (checked in Go, property tested).
-- INV-32: an instalment is paid only with a settlement source (installments_paid_check).
-- INV-33: one open renewal proposal per source contract (renewal_proposals_open_uidx).

-- +goose Up
ALTER TABLE contracts ADD COLUMN signed_at TIMESTAMP;
ALTER TABLE contracts ADD COLUMN signed_document_id TEXT;          -- generated_documents.id (00009, no FK: forward)
ALTER TABLE contracts ADD COLUMN signature_request_id TEXT;        -- signature_requests.id (00009, no FK: forward)
ALTER TABLE contracts ADD COLUMN suspended_at TIMESTAMP;
ALTER TABLE contracts ADD COLUMN suspend_reason TEXT;
ALTER TABLE contracts ADD COLUMN renewed_at TIMESTAMP;
ALTER TABLE contracts ADD COLUMN termination_amount_cents INTEGER;  -- refund (< 0) or penalty (> 0), never charged in M4
ALTER TABLE contracts ADD COLUMN installment_plan_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE contracts ADD COLUMN charter INTEGER NOT NULL DEFAULT 0 CHECK (charter IN (0,1));

ALTER TABLE quotes ADD COLUMN link_rotated_at TIMESTAMP;
ALTER TABLE quotes ADD COLUMN sent_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE quotes ADD COLUMN accepted_via TEXT CHECK (accepted_via IS NULL OR accepted_via IN ('panel','link'));

ALTER TABLE deadline_policies ADD COLUMN channels TEXT NOT NULL DEFAULT '["panel"]';  -- panel, customer_email, office_email

CREATE TABLE contract_settings (
    marina_id                TEXT PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    default_policy           TEXT NOT NULL DEFAULT '{}',     -- contract policy v2 (m4-plan §3.3)
    signature_driver         TEXT NOT NULL DEFAULT 'paper' CHECK (signature_driver IN ('paper','otp','provider')),
    provider_code            TEXT,
    marina_countersign       INTEGER NOT NULL DEFAULT 0 CHECK (marina_countersign IN (0,1)),
    require_paper_scan       INTEGER NOT NULL DEFAULT 1 CHECK (require_paper_scan IN (0,1)),
    signature_days           INTEGER NOT NULL DEFAULT 14 CHECK (signature_days BETWEEN 1 AND 90),
    signature_reminder_days  INTEGER NOT NULL DEFAULT 3 CHECK (signature_reminder_days BETWEEN 1 AND 30),
    renewal_deadline_days    INTEGER NOT NULL DEFAULT 30 CHECK (renewal_deadline_days BETWEEN 1 AND 365),
    arrears_days             INTEGER NOT NULL DEFAULT 30 CHECK (arrears_days BETWEEN 0 AND 365),
    insurance_policy         TEXT NOT NULL DEFAULT 'warn' CHECK (insurance_policy IN ('warn','block')),
    required_fields          TEXT NOT NULL DEFAULT '[]',
    bank_details_enc         TEXT,                            -- KeyRing, AAD bank-details:<marina>
    office_email_enc         TEXT,                            -- KeyRing, AAD office-email:<marina>
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TIMESTAMP NOT NULL,
    updated_at               TIMESTAMP NOT NULL,
    CONSTRAINT contract_settings_provider_check CHECK (signature_driver <> 'provider' OR provider_code IS NOT NULL)
);

CREATE TABLE installments (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    contract_id          TEXT NOT NULL,
    plan_version         INTEGER NOT NULL DEFAULT 1 CHECK (plan_version >= 1),
    seq                  INTEGER NOT NULL CHECK (seq >= 1),
    kind                 TEXT NOT NULL CHECK (kind IN ('deposit','installment','single')),
    issue_on             DATE NOT NULL,
    due_on               DATE NOT NULL,
    amount_cents         INTEGER NOT NULL CHECK (amount_cents >= 0),
    status               TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','issued','partially_paid','paid','cancelled')),
    issued_at            TIMESTAMP,
    overdue_notified_at  TIMESTAMP,
    charge_id            TEXT,                                 -- M5 charges (no FK in M4)
    settlement_source    TEXT CHECK (settlement_source IS NULL OR settlement_source IN ('manual','payment')),
    settlement_method    TEXT CHECK (settlement_method IS NULL OR settlement_method IN ('bank_transfer','cash','card_pos','cheque','other')),
    settlement_ref_enc   TEXT,                                -- KeyRing, AAD installment-ref:<id>
    settled_on           DATE,
    settled_at           TIMESTAMP,
    settled_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    replaced_by_plan     INTEGER,                             -- plan_version that replaced this cancelled row
    cancelled_at         TIMESTAMP,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id) ON DELETE CASCADE,
    CHECK (issue_on <= due_on),
    CONSTRAINT installments_paid_check CHECK (status <> 'paid' OR (settlement_source IS NOT NULL AND settled_at IS NOT NULL))
);
CREATE UNIQUE INDEX installments_seq_uidx ON installments (contract_id, plan_version, seq);
CREATE INDEX installments_marina_status_idx ON installments (marina_id, status, due_on);
CREATE INDEX installments_issue_idx ON installments (marina_id, issue_on) WHERE status = 'scheduled';
CREATE INDEX installments_contract_idx ON installments (marina_id, contract_id);

CREATE TABLE renewal_campaigns (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    filter              TEXT NOT NULL DEFAULT '{}',
    price_list_id       TEXT,
    target_starts_rule  TEXT NOT NULL DEFAULT 'next_term' CHECK (target_starts_rule IN ('next_term','fixed')),
    target_starts_on    DATE,
    deadline_on         DATE NOT NULL,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','sending','open','closed','discarded')),
    message             TEXT NOT NULL DEFAULT '',
    counts              TEXT NOT NULL DEFAULT '{}',
    launched_at         TIMESTAMP,
    closed_at           TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id),
    CONSTRAINT renewal_campaigns_target_check CHECK ((target_starts_rule = 'fixed') = (target_starts_on IS NOT NULL))
);
CREATE INDEX renewal_campaigns_marina_status_idx ON renewal_campaigns (marina_id, status);

CREATE TABLE renewal_proposals (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    campaign_id         TEXT,
    source_contract_id  TEXT NOT NULL,
    new_contract_id     TEXT,
    status              TEXT NOT NULL CHECK (status IN ('previewed','proposed','sent','accepted','signed','completed',
                          'declined','expired','excluded')),
    exclusion_reason    TEXT CHECK (exclusion_reason IS NULL OR exclusion_reason IN ('arrears','insurance_expired',
                          'already_renewed','no_price','berth_unavailable','manual')),
    included_reason     TEXT,
    old_total_cents     INTEGER NOT NULL DEFAULT 0,
    new_total_cents     INTEGER,
    deadline_on         DATE NOT NULL,
    sent_at             TIMESTAMP,
    sent_count          INTEGER NOT NULL DEFAULT 0 CHECK (sent_count >= 0),
    accepted_at         TIMESTAMP,
    accepted_via        TEXT CHECK (accepted_via IS NULL OR accepted_via IN ('panel','link')),
    accepted_plan       TEXT CHECK (accepted_plan IS NULL OR accepted_plan IN ('standard','early_payment')),
    decided_at          TIMESTAMP,
    decline_reason      TEXT NOT NULL DEFAULT '',
    link_id             TEXT,                                  -- access_links.id (00009, no FK: forward)
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (campaign_id, marina_id) REFERENCES renewal_campaigns (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (source_contract_id, marina_id) REFERENCES contracts (id, marina_id),
    FOREIGN KEY (new_contract_id, marina_id) REFERENCES contracts (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT renewal_proposals_exclusion_check CHECK (status <> 'excluded' OR exclusion_reason IS NOT NULL)
);
CREATE UNIQUE INDEX renewal_proposals_open_uidx ON renewal_proposals (source_contract_id)
    WHERE status IN ('previewed','proposed','sent','accepted','signed');
CREATE UNIQUE INDEX renewal_proposals_campaign_source_uidx ON renewal_proposals (campaign_id, source_contract_id);
CREATE INDEX renewal_proposals_campaign_idx ON renewal_proposals (marina_id, campaign_id, status);
CREATE INDEX renewal_proposals_deadline_idx ON renewal_proposals (marina_id, status, deadline_on);
CREATE INDEX renewal_proposals_new_contract_idx ON renewal_proposals (new_contract_id);

CREATE TABLE contract_addenda (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    contract_id           TEXT NOT NULL,
    seq                   INTEGER NOT NULL CHECK (seq >= 1),
    kind                  TEXT NOT NULL CHECK (kind IN ('service_add','service_remove','parking','billing_customer',
                            'price_change','vessel_change')),
    effective_on          DATE NOT NULL,
    payload               TEXT NOT NULL DEFAULT '{}',
    amount_delta_cents    INTEGER NOT NULL DEFAULT 0,
    status                TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','awaiting_signature','active','cancelled')),
    document_id           TEXT,                                -- generated_documents.id (00009, no FK: forward)
    signature_request_id  TEXT,                                -- signature_requests.id (00009, no FK: forward)
    activated_at          TIMESTAMP,
    cancelled_at          TIMESTAMP,
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX contract_addenda_seq_uidx ON contract_addenda (contract_id, seq);
CREATE INDEX contract_addenda_marina_idx ON contract_addenda (marina_id, contract_id, status);

CREATE TABLE charter_fleets (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id  TEXT NOT NULL,                                -- the charter company
    name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE INDEX charter_fleets_marina_idx ON charter_fleets (marina_id, customer_id);

CREATE TABLE charter_fleet_vessels (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    fleet_id    TEXT NOT NULL,
    vessel_id   TEXT NOT NULL,
    active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (fleet_id, marina_id) REFERENCES charter_fleets (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id)
);
CREATE UNIQUE INDEX charter_fleet_vessels_uidx ON charter_fleet_vessels (fleet_id, vessel_id);
CREATE UNIQUE INDEX charter_fleet_vessels_active_uidx ON charter_fleet_vessels (vessel_id) WHERE active = 1;
CREATE INDEX charter_fleet_vessels_marina_idx ON charter_fleet_vessels (marina_id, fleet_id);

CREATE TABLE charter_contracts (
    contract_id         TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    fleet_id            TEXT NOT NULL,
    changeover_weekday  INTEGER NOT NULL CHECK (changeover_weekday BETWEEN 1 AND 7),   -- ISO weekday, 1 = Monday
    pricing_mode        TEXT NOT NULL CHECK (pricing_mode IN ('per_berth','per_vessel','flat')),
    out_nights          TEXT NOT NULL DEFAULT '[]',           -- [{from, to, berth_id?}], informative
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    FOREIGN KEY (contract_id, marina_id) REFERENCES contracts (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (fleet_id, marina_id) REFERENCES charter_fleets (id, marina_id)
);
CREATE INDEX charter_contracts_fleet_idx ON charter_contracts (marina_id, fleet_id);

CREATE TABLE waitlist_entries (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id         TEXT NOT NULL,
    vessel_id           TEXT,
    loa_cm              INTEGER NOT NULL CHECK (loa_cm > 0),
    beam_cm             INTEGER NOT NULL CHECK (beam_cm > 0),
    draft_cm            INTEGER CHECK (draft_cm IS NULL OR draft_cm > 0),
    term                TEXT NOT NULL CHECK (term IN ('annual','seasonal','monthly','period')),
    group_ids           TEXT NOT NULL DEFAULT '[]',           -- berth group ids
    requested_from      DATE NOT NULL,
    priority            INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting','offered','converted','withdrawn')),
    offered_quote_id    TEXT,
    offered_at          TIMESTAMP,
    notes               TEXT NOT NULL DEFAULT '',
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id),
    FOREIGN KEY (offered_quote_id, marina_id) REFERENCES quotes (id, marina_id)
);
CREATE INDEX waitlist_entries_marina_status_idx ON waitlist_entries (marina_id, status, created_at);

-- +goose Down
DROP TABLE waitlist_entries;
DROP TABLE charter_contracts;
DROP TABLE charter_fleet_vessels;
DROP TABLE charter_fleets;
DROP TABLE contract_addenda;
DROP TABLE renewal_proposals;
DROP TABLE renewal_campaigns;
DROP TABLE installments;
DROP TABLE contract_settings;
ALTER TABLE deadline_policies DROP COLUMN channels;
ALTER TABLE quotes DROP COLUMN accepted_via;
ALTER TABLE quotes DROP COLUMN sent_count;
ALTER TABLE quotes DROP COLUMN link_rotated_at;
ALTER TABLE contracts DROP COLUMN charter;
ALTER TABLE contracts DROP COLUMN installment_plan_version;
ALTER TABLE contracts DROP COLUMN termination_amount_cents;
ALTER TABLE contracts DROP COLUMN renewed_at;
ALTER TABLE contracts DROP COLUMN suspend_reason;
ALTER TABLE contracts DROP COLUMN suspended_at;
ALTER TABLE contracts DROP COLUMN signature_request_id;
ALTER TABLE contracts DROP COLUMN signed_document_id;
ALTER TABLE contracts DROP COLUMN signed_at;
