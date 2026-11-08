-- Porto M8 fix: a payment match can be reversed several times, in parts, up to its amount (PostgreSQL) (ADR 0013,
-- m5-plan §4.2). The twin is internal/migrations/sqlite/00017_partial_match_reversals.sql: keep names identical.
--
-- Before: one reversal per match (payment_matches_reverses_uidx), so a refund of part of a payment (online date
-- change) blocked any later refund of the rest (cancellation). Now a match carries reversed_cents, the sum of its
-- reversal rows, moved only by a conditional UPDATE (reversed_cents + delta <= amount_cents) and bounded by
-- payment_matches_reversed_check, so concurrent reversals never exceed the match. Reversal rows keep 0.
-- The insert-only trigger now allows exactly one kind of UPDATE: reversed_cents growing on a positive match, every
-- other column unchanged. Deletes stay refused.

-- +goose Up
DROP TRIGGER payment_matches_insert_only ON payment_matches;
ALTER TABLE payment_matches ADD COLUMN reversed_cents BIGINT NOT NULL DEFAULT 0;
ALTER TABLE payment_matches ADD CONSTRAINT payment_matches_reversed_check
    CHECK (reversed_cents = 0 OR (reverses_match_id IS NULL AND reversed_cents > 0 AND reversed_cents <= amount_cents));
UPDATE payment_matches m SET reversed_cents = r.total
FROM (SELECT reverses_match_id, -SUM(amount_cents) AS total FROM payment_matches
      WHERE reverses_match_id IS NOT NULL GROUP BY reverses_match_id) r
WHERE m.id = r.reverses_match_id;
DROP INDEX payment_matches_reverses_uidx;
CREATE INDEX payment_matches_reverses_idx ON payment_matches (reverses_match_id) WHERE reverses_match_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION porto_payment_matches_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'insert_only' USING ERRCODE = 'P0001';
    END IF;
    IF NEW.reversed_cents < OLD.reversed_cents
        OR NEW.id IS DISTINCT FROM OLD.id
        OR NEW.marina_id IS DISTINCT FROM OLD.marina_id
        OR NEW.payment_id IS DISTINCT FROM OLD.payment_id
        OR NEW.charge_id IS DISTINCT FROM OLD.charge_id
        OR NEW.amount_cents IS DISTINCT FROM OLD.amount_cents
        OR NEW.reverses_match_id IS DISTINCT FROM OLD.reverses_match_id
        OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id
        OR NEW.created_by_actor IS DISTINCT FROM OLD.created_by_actor
        OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'insert_only' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER payment_matches_insert_only BEFORE UPDATE OR DELETE ON payment_matches
    FOR EACH ROW EXECUTE FUNCTION porto_payment_matches_guard();

-- +goose Down
-- Fails on purpose when a match has more than one reversal (the old schema cannot hold it).
DROP TRIGGER payment_matches_insert_only ON payment_matches;
CREATE TRIGGER payment_matches_insert_only BEFORE UPDATE OR DELETE ON payment_matches
    FOR EACH ROW EXECUTE FUNCTION porto_insert_only();
DROP FUNCTION porto_payment_matches_guard();
DROP INDEX payment_matches_reverses_idx;
CREATE UNIQUE INDEX payment_matches_reverses_uidx ON payment_matches (reverses_match_id) WHERE reverses_match_id IS NOT NULL;
ALTER TABLE payment_matches DROP CONSTRAINT payment_matches_reversed_check;
ALTER TABLE payment_matches DROP COLUMN reversed_cents;
