-- Porto M6b offline replays and fiscal bridge (SQLite) (m6b-plan §9.2). The twin is
-- internal/migrations/postgres/00014_offline_bridge.sql: keep column, index and constraint names identical.
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

ALTER TABLE fiscal_documents ADD COLUMN bridge_device_id TEXT REFERENCES devices (id) ON DELETE SET NULL;
ALTER TABLE fiscal_documents ADD COLUMN bridge_claim_id TEXT;
ALTER TABLE fiscal_documents ADD COLUMN bridge_claim_until TIMESTAMP;
ALTER TABLE fiscal_documents ADD COLUMN bridge_attempts INTEGER NOT NULL DEFAULT 0;
CREATE INDEX fiscal_documents_driver_idx ON fiscal_documents (marina_id, status, driver);

DROP TRIGGER fiscal_documents_immutable_update;
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
        OR NEW.business_date IS NOT OLD.business_date
        OR NEW.bridge_device_id IS NOT OLD.bridge_device_id
        OR NEW.bridge_claim_id IS NOT OLD.bridge_claim_id
        OR NEW.bridge_claim_until IS NOT OLD.bridge_claim_until
        OR NEW.bridge_attempts IS NOT OLD.bridge_attempts))
BEGIN SELECT RAISE(ABORT, 'fiscal_document_immutable'); END;
-- +goose StatementEnd

ALTER TABLE billing_settings ADD COLUMN fiscal_bridge_device_id TEXT REFERENCES devices (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE billing_settings DROP COLUMN fiscal_bridge_device_id;

DROP TRIGGER fiscal_documents_immutable_update;
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

DROP INDEX fiscal_documents_driver_idx;
ALTER TABLE fiscal_documents DROP COLUMN bridge_attempts;
ALTER TABLE fiscal_documents DROP COLUMN bridge_claim_until;
ALTER TABLE fiscal_documents DROP COLUMN bridge_claim_id;
ALTER TABLE fiscal_documents DROP COLUMN bridge_device_id;

DROP INDEX presences_idempotency_key_uq;
ALTER TABLE presences DROP COLUMN idempotency_hash;
ALTER TABLE presences DROP COLUMN idempotency_key;
