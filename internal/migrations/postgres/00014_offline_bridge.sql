-- Porto M6b offline replays and fiscal bridge (PostgreSQL) (m6b-plan §9.2). The twin is
-- internal/migrations/sqlite/00014_offline_bridge.sql: keep column, index and constraint names identical.
--
-- presences.idempotency_key / idempotency_hash: domain dedupe of a presence recorded with an Idempotency-Key (the
--     second layer under the idempotency middleware): one presence per (marina, key); the hash of the request body
--     tells a replay (same hash) from a reused key (different hash).
-- fiscal_documents.bridge_*: documents of the core driver 'bridge' are issued by a desktop in remote mode, which
--     claims them with a 60 s lease (bridge_claim_id, bridge_claim_until) and reports the outcome; bridge_device_id
--     is the claiming device, bridge_attempts counts the claims. The immutability trigger is extended so that
--     these columns change only while the document is not issued or void.
-- billing_settings.fiscal_bridge_device_id: the desktop device (kind desktop, access token) of the marina allowed
--     to claim its bridge documents.

-- +goose Up
ALTER TABLE presences ADD COLUMN idempotency_key TEXT;
ALTER TABLE presences ADD COLUMN idempotency_hash TEXT;
CREATE UNIQUE INDEX presences_idempotency_key_uq ON presences (marina_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

ALTER TABLE fiscal_documents ADD COLUMN bridge_device_id UUID REFERENCES devices (id) ON DELETE SET NULL;
ALTER TABLE fiscal_documents ADD COLUMN bridge_claim_id TEXT;
ALTER TABLE fiscal_documents ADD COLUMN bridge_claim_until TIMESTAMPTZ;
ALTER TABLE fiscal_documents ADD COLUMN bridge_attempts INTEGER NOT NULL DEFAULT 0;
CREATE INDEX fiscal_documents_driver_idx ON fiscal_documents (marina_id, status, driver);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION porto_fiscal_documents_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status IN ('issued', 'void') THEN
            RAISE EXCEPTION 'fiscal_document_immutable' USING ERRCODE = 'P0001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status = 'void' OR (OLD.status = 'issued' AND (
        NEW.status NOT IN ('issued', 'void')
        OR NEW.number IS DISTINCT FROM OLD.number
        OR NEW.lines IS DISTINCT FROM OLD.lines
        OR NEW.vat IS DISTINCT FROM OLD.vat
        OR NEW.total_cents IS DISTINCT FROM OLD.total_cents
        OR NEW.payment_id IS DISTINCT FROM OLD.payment_id
        OR NEW.charge_id IS DISTINCT FROM OLD.charge_id
        OR NEW.business_date IS DISTINCT FROM OLD.business_date
        OR NEW.bridge_device_id IS DISTINCT FROM OLD.bridge_device_id
        OR NEW.bridge_claim_id IS DISTINCT FROM OLD.bridge_claim_id
        OR NEW.bridge_claim_until IS DISTINCT FROM OLD.bridge_claim_until
        OR NEW.bridge_attempts IS DISTINCT FROM OLD.bridge_attempts)) THEN
        RAISE EXCEPTION 'fiscal_document_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE billing_settings ADD COLUMN fiscal_bridge_device_id UUID REFERENCES devices (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE billing_settings DROP COLUMN fiscal_bridge_device_id;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION porto_fiscal_documents_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status IN ('issued', 'void') THEN
            RAISE EXCEPTION 'fiscal_document_immutable' USING ERRCODE = 'P0001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status = 'void' OR (OLD.status = 'issued' AND (
        NEW.status NOT IN ('issued', 'void')
        OR NEW.number IS DISTINCT FROM OLD.number
        OR NEW.lines IS DISTINCT FROM OLD.lines
        OR NEW.vat IS DISTINCT FROM OLD.vat
        OR NEW.total_cents IS DISTINCT FROM OLD.total_cents
        OR NEW.payment_id IS DISTINCT FROM OLD.payment_id
        OR NEW.charge_id IS DISTINCT FROM OLD.charge_id
        OR NEW.business_date IS DISTINCT FROM OLD.business_date)) THEN
        RAISE EXCEPTION 'fiscal_document_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP INDEX fiscal_documents_driver_idx;
ALTER TABLE fiscal_documents DROP COLUMN bridge_attempts;
ALTER TABLE fiscal_documents DROP COLUMN bridge_claim_until;
ALTER TABLE fiscal_documents DROP COLUMN bridge_claim_id;
ALTER TABLE fiscal_documents DROP COLUMN bridge_device_id;

DROP INDEX presences_idempotency_key_uq;
ALTER TABLE presences DROP COLUMN idempotency_hash;
ALTER TABLE presences DROP COLUMN idempotency_key;
