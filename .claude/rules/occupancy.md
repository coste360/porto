---
paths:
  - "internal/domain/occupancy/**"
  - "internal/domain/presence/**"
  - "internal/domain/contract/**"
  - "internal/domain/checkout/**"
  - "internal/store/*occupanc*"
  - "internal/store/*allocation*"
---
# Occupancy (Porto, from M3)

Spec: 10 (occupancy engine), 23 (online booking); ADR 0002 (night as the unit), ADR 0003 (cells);
`docs/invariants.md` INV-01..09.

- The unit of occupancy is the **night** (`calendar.Date` in the marina time zone), never a time range.
- No double occupancy is guaranteed by unique constraints on `berth_occupancies`, `cell_occupancies`,
  `land_occupancies` (and later `rental_slots`, `equipment_slots`). Never replace them with "check then insert"
  in Go. A unique violation maps to a domain error (e.g. `ErrResourceTaken`) → 409.
- An allocation writes exactly the rows `Footprint × nights` (INV-03); moves, swaps and scenario application
  are one transaction: all or nothing (INV-06).
- Expired holds never block a sale: reclaim them at conflict time even if the cleanup job has not run (INV-05).
- Compatibility: every active allocation passes `fit.Check` or carries `fit_override` with user and reason
  (INV-07).
- Online transits are confirmed only by the payment webhook or the manual driver, never by the browser return
  (INV-08, INV-09).
- Every state change of contracts and transits goes through `internal/domain/contract` and writes audit and
  outbox in the same transaction (INV-30).
- Tests: `TestConcurrency_*` with at least 100 parallel attempts on the same resource and the same cells, on
  SQLite and PostgreSQL, under `-race`. Run the `invariants-guardian` subagent before declaring done.
