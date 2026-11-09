-- Porto M11 rental handovers, damages and deposits schema (SQLite) (spec 19.4-19.5, 15.4, 16.2; m11-plan §7-§10,
-- §15, ADR 0016). The twin is internal/migrations/postgres/00025_rental_handovers.sql: keep table, column, index and
-- constraint names identical.
--
-- rental_handovers: the check-out and check-in drafts of a booking (one per phase). identity_enc holds the renter's
--     document and licence data typed by the operator (KeyRing, AAD rental-identity:<handover_id>); it is wiped
--     after closed_at + identity_retention_days. client_ref dedupes offline replays (INV-83).
-- rental_handover_files: document, licence, boat and damage photos (encrypted filestore, INV-74).
-- rental_damages: damages found at return with an estimate; charged (retained or on the return charge) or waived.
-- CHECK changes: payment_intents.purpose gains 'rental_deposit' (manual-capture pre-authorisation, D15);
--     signature_requests.subject_type gains 'rental' and driver gains 'screen' (D14); generated_documents.subject_type
--     gains 'rental_booking'. SQLite edits the table sql with writable_schema as 00016/00020 do; PostgreSQL drops and
--     re-adds the generated constraints.

-- +goose Up
PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'purpose IN (''payment_request'',''transit'',''sepa_collect'',''mandate_setup'')',
                       'purpose IN (''payment_request'',''transit'',''sepa_collect'',''mandate_setup'',''rental_deposit'')')
WHERE type = 'table' AND name = 'payment_intents';
UPDATE sqlite_schema
SET sql = replace(replace(sql, 'subject_type IN (''contract'',''addendum'',''right_transfer'')',
                       'subject_type IN (''contract'',''addendum'',''right_transfer'',''rental'')'),
                  'driver IN (''paper'',''otp'',''provider'')', 'driver IN (''paper'',''otp'',''provider'',''screen'')')
WHERE type = 'table' AND name = 'signature_requests';
UPDATE sqlite_schema
SET sql = replace(sql, '''communication'',''map_print'',''letter'',''yard_quote'',''work_order'')',
                       '''communication'',''map_print'',''letter'',''yard_quote'',''work_order'',''rental_booking'')')
WHERE type = 'table' AND name = 'generated_documents';
PRAGMA writable_schema = RESET;

CREATE TABLE rental_handovers (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id            TEXT NOT NULL,
    phase                 TEXT NOT NULL CHECK (phase IN ('checkout','checkin')),
    checklist             TEXT NOT NULL DEFAULT '[]',          -- [{key, ok, note}]
    fuel_eighths          INTEGER CHECK (fuel_eighths IS NULL OR fuel_eighths BETWEEN 0 AND 8),
    engine_hours_milli    INTEGER CHECK (engine_hours_milli IS NULL OR engine_hours_milli >= 0),
    refuel_milli          INTEGER CHECK (refuel_milli IS NULL OR refuel_milli >= 0),   -- checkin: litres typed
    identity_enc          TEXT,                                -- checkout only; KeyRing, AAD rental-identity:<id>
    identity_purged_at    TIMESTAMP,
    notes                 TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 1000),
    actual_at             TIMESTAMP,                           -- actual pickup / return instant
    client_ref            TEXT,                                -- offline dedupe (INV-83)
    device_at             TIMESTAMP,                           -- device time of the last offline save
    completed_at          TIMESTAMP,
    completed_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES rental_bookings (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT rental_handovers_identity_check CHECK (phase = 'checkout' OR identity_enc IS NULL)
);
CREATE UNIQUE INDEX rental_handovers_phase_uidx ON rental_handovers (booking_id, phase);
CREATE UNIQUE INDEX rental_handovers_client_ref_uidx ON rental_handovers (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX rental_handovers_marina_idx ON rental_handovers (marina_id, booking_id);

CREATE TABLE rental_damages (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    booking_id            TEXT NOT NULL,
    description           TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 500),
    estimate_cents        INTEGER NOT NULL CHECK (estimate_cents >= 0),
    status                TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','charged','waived')),
    retained_cents        INTEGER NOT NULL DEFAULT 0 CHECK (retained_cents >= 0),
    charged_cents         INTEGER NOT NULL DEFAULT 0 CHECK (charged_cents >= 0),
    waive_reason          TEXT NOT NULL DEFAULT '' CHECK (length(waive_reason) <= 200),
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (booking_id, marina_id) REFERENCES rental_bookings (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT rental_damages_cover_check CHECK (retained_cents + charged_cents <= estimate_cents)
);
CREATE INDEX rental_damages_marina_idx ON rental_damages (marina_id, booking_id);
CREATE INDEX rental_damages_status_idx ON rental_damages (marina_id, status, created_at);

CREATE TABLE rental_handover_files (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    handover_id           TEXT NOT NULL,
    damage_id             TEXT,
    kind                  TEXT NOT NULL CHECK (kind IN ('id_document','licence','boat','damage')),
    file_id               TEXT NOT NULL,
    client_ref            TEXT,                                -- offline photo dedupe (INV-83)
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (handover_id, marina_id) REFERENCES rental_handovers (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (damage_id, marina_id) REFERENCES rental_damages (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (file_id, marina_id) REFERENCES stored_files (id, marina_id),
    CONSTRAINT rental_handover_files_damage_check CHECK ((kind = 'damage') = (damage_id IS NOT NULL))
);
CREATE UNIQUE INDEX rental_handover_files_client_ref_uidx ON rental_handover_files (marina_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX rental_handover_files_marina_idx ON rental_handover_files (marina_id, handover_id);

-- +goose Down
DROP TABLE rental_handover_files;
DROP TABLE rental_damages;
DROP TABLE rental_handovers;

-- Issued rental documents carry a sha256: the immutability trigger of 00009 is lifted for this delete only.
DROP TRIGGER generated_documents_immutable_delete;
DELETE FROM signature_requests WHERE subject_type = 'rental' OR driver = 'screen';
DELETE FROM generated_documents WHERE subject_type = 'rental_booking';
-- +goose StatementBegin
CREATE TRIGGER generated_documents_immutable_delete BEFORE DELETE ON generated_documents
WHEN OLD.signed = 1 OR OLD.sha256 IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'document_immutable'); END;
-- +goose StatementEnd
DELETE FROM payment_intents WHERE purpose = 'rental_deposit';
PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'purpose IN (''payment_request'',''transit'',''sepa_collect'',''mandate_setup'',''rental_deposit'')',
                       'purpose IN (''payment_request'',''transit'',''sepa_collect'',''mandate_setup'')')
WHERE type = 'table' AND name = 'payment_intents';
UPDATE sqlite_schema
SET sql = replace(replace(sql, 'subject_type IN (''contract'',''addendum'',''right_transfer'',''rental'')',
                       'subject_type IN (''contract'',''addendum'',''right_transfer'')'),
                  'driver IN (''paper'',''otp'',''provider'',''screen'')', 'driver IN (''paper'',''otp'',''provider'')')
WHERE type = 'table' AND name = 'signature_requests';
UPDATE sqlite_schema
SET sql = replace(sql, '''communication'',''map_print'',''letter'',''yard_quote'',''work_order'',''rental_booking'')',
                       '''communication'',''map_print'',''letter'',''yard_quote'',''work_order'')')
WHERE type = 'table' AND name = 'generated_documents';
PRAGMA writable_schema = RESET;
-- DDL after the edit bumps the schema cookie for the other connections (see the header).
CREATE TABLE porto_schema_bump (id INTEGER);
DROP TABLE porto_schema_bump;
