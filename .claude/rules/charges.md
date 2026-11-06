---
paths:
  - "internal/domain/charge/**"
  - "internal/domain/payment/**"
  - "internal/domain/accrual/**"
  - "internal/domain/rights/**"
  - "internal/domain/pricing/**"
  - "internal/store/*charge*"
  - "internal/store/*payment*"
---
# Charges, payments and pricing (Porto, from M2)

Spec: 12 (prices), 14 (rights and common charges), 16 (charges and payments), 21 (reports);
`docs/invariants.md` INV-20..21, INV-31, INV-40..42, INV-50..53, INV-60.

- Money is `money.Cents` (`int64`). Splits use the largest-remainder method so that parts always sum to the
  total (INV-31, INV-42, INV-60). Prove it with a rapid property test.
- Porto does not issue invoices: charges are **exported** to the invoicing software. An `exported` or
  `invoiced` charge never changes amounts or lines; corrections are linked adjustment charges (INV-51).
- Each non-cancelled charge appears in a `to_invoice → exported` export exactly once unless re-exported
  explicitly (INV-53).
- `paid_amount` equals successful payment matches minus refunds, updated in the same transaction as the payment
  (INV-50). Matches never exceed the payment or the charge, even concurrently (INV-52,
  `TestConcurrency_PaymentMatching`).
- The pricing engine is pure (INV-21) and every quote stores a reproducible `price_snapshot` with
  `engine_version` (INV-20).
- Millesimal tables sum to 1000 and holder shares to 1000 (INV-40, INV-41).
- Run the `invariants-guardian` subagent before declaring done.
