# AGENTS.md

Porto ("porto" in the logotype) is a management system for marinas and boatyards: harbour map (quays, piers, berths, buoy
fields, dynamic areas, yards, sheds, land resources), customers and vessels, price lists, mooring contracts and
transits, berth rights and common charges, charges and payments (invoices are issued outside Porto, as in Arenile:
we export charges to the invoicing software), dry storage, boatyard, rentals, online berth
booking, occupancy sensors, reports. Go backend + React SPAs in a single binary, desktop edition (the mobile app is out of this repository),
same infrastructure as Arenile (`../arenile`). Maintained by Coste360 SRL.

The specification (Italian) lives in `docs/spec/`, one file per section; it is authoritative.
**Status: M0 foundations in progress.** Start from `docs/spec/32-test-qualita-roadmap.md` (M0).

## Before writing code
- Read the spec sections referenced by the task, `docs/invariants.md` and `docs/adr/`.
- When a rule says "come Arenile", read the referenced Arenile spec/ADR/code in `../arenile/` to understand the
  behaviour, then write Porto's own code. **Never copy Arenile code** (ADR 0001): same stack and patterns, new code.
- Open decisions are in `docs/spec/33-decisioni-aperte.md`; do not decide them in code, ask.

## Non-negotiable rules (inherited from Arenile, plus harbour-specific ones)
- Money is `int64` cents (`money.Cents`). Lengths are `int64` centimetres (`geo.Cm`), areas cm². Fractional
  quantities are fixed-point milli (`qty.Milli`). Never float in domain logic.
- Calendar dates are `YYYY-MM-DD` in the marina timezone; occupancy unit is the **night** (ADR 0002). Time comes from
  an injected `clock.Clock`.
- No double occupancy is enforced by DB unique constraints (`berth_occupancies`, `cell_occupancies`,
  `land_occupancies`, `rental_slots`, `equipment_slots`). Never replace them with app-level checks (ADR 0003).
- `layout.Footprint`, `fit`, `pricing`, `accrual`, `formula`, `optimizer` are pure: no DB, no clock, no randomness
  (the optimizer takes an explicit seed).
- Every state change of contracts and transits goes through `internal/domain/contract` and writes audit log and
  outbox event in the same transaction.
- Transit confirmation only from the payment webhook or the manual driver.
- No invoicing, e-invoicing (SdI), bookkeeping or purchase invoices in the core: charges are exported. Exported
  charges are immutable; corrections are linked adjustment charges.
- AI extraction proposes; a user confirms. Nothing extracted is saved without confirmation.
- Every operational query is scoped by `marina_id` from the auth context.
- API first: change `api/openapi.yaml`, run `make gen`, then implement. Never edit generated files.
- Never modify a migration already on `main`; add a new one, for both dialects.
- Frontend UI = shadcn/ui only, Tailwind on theme tokens, no custom CSS (as Arenile ADR 0009).
- Brand files live in `assets/brand/porto/` (rights reserved, not covered by the code licence); use them only via
  the shared `BrandLogo` component and follow `docs/adr/0007-marchio.md`. Never redraw the logo.
- Never log emails, phone numbers, documents, tokens or secrets in clear. Code and identifiers in English;
  user-facing strings through i18n keys, Italian is the source language.

## Commands
Same `make` targets as Arenile: `setup`, `dev`, `seed`, `check`, `check-full`, `gen`, `fmt`, `test`, `test-db`,
`test-race`, `lint`, `e2e`.

## Working style
Small scoped changes, Conventional Commits, a new ADR to change an existing decision, stop and ask when a task
conflicts with an invariant or the spec.
