-- Porto M7 report indexes (SQLite) (m7-plan §9). The twin is internal/migrations/postgres/00015_report_indexes.sql:
-- keep index names identical. Indexes only: no table or column changes, so no backup change.
--
-- charges_issued_idx: charges by issue date (charges, revenue classes, dashboard "charged").
-- payments_received_idx: payments by received date (base incasso).
-- refunds_business_date_idx: refunds by business date (base incasso, mandate payouts).
-- inquiries already has inquiries_marina_outcome_idx (marina_id, outcome, arrival), which covers lost demand.

-- +goose Up
CREATE INDEX charges_issued_idx ON charges (marina_id, issued_on);
CREATE INDEX payments_received_idx ON payments (marina_id, received_on);
CREATE INDEX refunds_business_date_idx ON refunds (marina_id, business_date);

-- +goose Down
DROP INDEX refunds_business_date_idx;
DROP INDEX payments_received_idx;
DROP INDEX charges_issued_idx;
