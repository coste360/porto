-- Porto M8 fix: a payment match can be reversed several times, in parts, up to its amount (SQLite) (ADR 0013,
-- m5-plan §4.2). The twin is internal/migrations/postgres/00017_partial_match_reversals.sql: keep names identical.
--
-- Before: one reversal per match (payment_matches_reverses_uidx), so a refund of part of a payment (online date
-- change) blocked any later refund of the rest (cancellation). Now a match carries reversed_cents, the sum of its
-- reversal rows, moved only by a conditional UPDATE (reversed_cents + delta <= amount_cents) and bounded by
-- payment_matches_reversed_check, so concurrent reversals never exceed the match. Reversal rows keep 0.
-- The insert-only trigger now allows exactly one kind of UPDATE: reversed_cents growing on a positive match, every
-- other column unchanged. Deletes stay refused.

-- +goose Up
ALTER TABLE payment_matches ADD COLUMN reversed_cents INTEGER NOT NULL DEFAULT 0
    CONSTRAINT payment_matches_reversed_check
    CHECK (reversed_cents = 0 OR (reverses_match_id IS NULL AND reversed_cents > 0 AND reversed_cents <= amount_cents));
DROP TRIGGER payment_matches_insert_only_update;
UPDATE payment_matches SET reversed_cents = (
    SELECT -SUM(r.amount_cents) FROM payment_matches r WHERE r.reverses_match_id = payment_matches.id)
WHERE id IN (SELECT reverses_match_id FROM payment_matches WHERE reverses_match_id IS NOT NULL);
DROP INDEX payment_matches_reverses_uidx;
CREATE INDEX payment_matches_reverses_idx ON payment_matches (reverses_match_id) WHERE reverses_match_id IS NOT NULL;
-- +goose StatementBegin
CREATE TRIGGER payment_matches_insert_only_update BEFORE UPDATE ON payment_matches
WHEN NEW.reversed_cents < OLD.reversed_cents
    OR NEW.id IS NOT OLD.id
    OR NEW.marina_id IS NOT OLD.marina_id
    OR NEW.payment_id IS NOT OLD.payment_id
    OR NEW.charge_id IS NOT OLD.charge_id
    OR NEW.amount_cents IS NOT OLD.amount_cents
    OR NEW.reverses_match_id IS NOT OLD.reverses_match_id
    OR NEW.created_by_user_id IS NOT OLD.created_by_user_id
    OR NEW.created_by_actor IS NOT OLD.created_by_actor
    OR NEW.created_at IS NOT OLD.created_at
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd

-- +goose Down
-- Fails on purpose when a match has more than one reversal (the old schema cannot hold it).
DROP TRIGGER payment_matches_insert_only_update;
DROP INDEX payment_matches_reverses_idx;
CREATE UNIQUE INDEX payment_matches_reverses_uidx ON payment_matches (reverses_match_id) WHERE reverses_match_id IS NOT NULL;
ALTER TABLE payment_matches DROP COLUMN reversed_cents;
-- +goose StatementBegin
CREATE TRIGGER payment_matches_insert_only_update BEFORE UPDATE ON payment_matches
BEGIN SELECT RAISE(ABORT, 'insert_only'); END;
-- +goose StatementEnd
