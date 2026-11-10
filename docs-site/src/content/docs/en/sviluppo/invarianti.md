---
title: Invariants
description: The domain rules Porto always guarantees, each one checked by tests.
sidebar:
  order: 3
---

Invariants are the rules that must hold in every state of the system. They are listed one by one, with the test that
checks each, in [docs/invariants.md](https://github.com/coste360/porto/blob/main/docs/invariants.md) (about a hundred
entries by area, in Italian). A change that touches one must keep it true and checked.

## The main ones

- **No double occupancy.** Berths, cells of dynamic areas, land resources, rental units, skippers and lifting
  equipment are protected by **unique database constraints**, not by application checks. Concurrency tests fire at
  least 100 parallel attempts, with `-race`, on SQLite and PostgreSQL.
- **The night is the unit of occupancy**: a stay occupies the nights from arrival to departure, excluded.
- **Exact amounts.** Integer cents everywhere; an instalment plan adds up exactly to the total; the split of common
  charges adds up exactly to the budget (largest remainder method).
- **Reproducible prices.** An activated price list never changes and every price can be recomputed from its
  snapshot; the price list matrix matches the simulator.
- **Contract states in one place.** Every state change of contracts and transits goes through a single service and
  writes audit and event in the same transaction.
- **Transits confirmed only by payment.** An online transit is confirmed only by the signed payment webhook or by the
  manual driver, never by the browser returning.
- **Exported charges are immutable.** Each charge enters the "to invoice" export once; afterwards corrections are
  linked adjustment charges.
- **Consistent allocations.** A payment's allocations never exceed the payment or the charge balance; deposits are not
  revenue until they are retained.
- **Marina isolation.** Every operational query is filtered by marina; another marina's resource answers 404.
- **No personal data** in logs, events, SSE messages and webhooks.
- **Sensors propose, they do not decide**: no reading changes a contract or an occupancy without an operator's
  confirmation.
- **Scenarios never touch the real state**: a dry-storage scenario is applied all or nothing, rechecked against the
  current state.
- **The dashboard matches the reports**: every tile's value is the sum of the records of its drill-down.
