# M11 — Noleggio

Status: Plan phase output. It is authoritative for the M11 builders. If it conflicts with `docs/spec/`, the spec
wins: stop and report to the orchestrator. No open decision of `docs/spec/33-decisioni-aperte.md` is touched: OTP
signature stays "firma elettronica semplice" (D6), and payment gateways stay behind the existing `paydriver`
registry, `ext.PaymentAuthorizer` and the test `fixture.FakeProvider`. §18 lists every detail decided here where
the spec is silent. CONTRACT records the binding ones in ADR 0016.

Sources: spec 19 (all), 05.2 (time slots instead of nights), 06.3 (`rental_slots`), 06.8, 10 (occupancy
guarantees, holds, INV-01/05), 11.2 (vessels), 12.1–12.4 (items, revenue class `rental`, periods, pure engine),
15.1/15.4 (templates `rental_contract`, `rental_handover`, signature drivers), 16.2/16.3/16.5 (deposits,
`FiscalDriver`, export), 20 (maintenance reports from rental staff), 21 (rental report and tiles), 26.1 (role
`rental`), 27 (`/rental/units`, `/rental/bookings`, events `rental.checked_out`, `rental.returned`, topic
`rental.changed`), 28 (notification "noleggio confermato"), 29.2 (offline), 31.3 (phone), 32 (M11 acceptance).
`docs/invariants.md` INV-01, 05, 20, 25, 30, 50–56, 61–64, 70–73, 83, 87. ADR 0001, 0002, 0003, 0005, 0008, 0009,
0011, 0012, 0013, 0015. Plans: `m2-plan.md` (rule selection), `m3-plan.md` §3.1–3.3 (allocate with
`ON CONFLICT DO NOTHING`, lazy reclaim, holds), `m5-plan.md` §3–5 (charges, payments, deposits, cash, fiscal,
intents), `m6b-plan.md` §4–6 (offline queue, phone shell, landing), `m7-plan.md` §1.2/§4 (reserved report key
`rental`, tiles `rental_utilisation`, `rental_revenue`), `m8-plan.md` §4–6 (hold TTL, pre-authorisation), `m10-plan.md`
§11 (`equipment_slots`, the occupancy-by-slot pattern this milestone repeats).

## 0. Ground rules (every builder)

1. **Arenile is reference only** (ADR 0001). Arenile has no rental domain. The only pattern worth re-reading is
   rows-per-key with `ON CONFLICT DO NOTHING` and expired-hold reclaim, already in Porto (`occupancy`, `lifting`).
   Never copy files, code, comments or tests. Never modify `../arenile`.
2. **No double booking, DB only** (ADR 0003): `rental_slots` PK `(unit_id, date, slot)` and `rental_skipper_slots`
   PK `(skipper_id, date, slot)` are the only guarantee. Never check-then-insert as a guarantee. Pre-checks exist
   only to produce friendlier messages.
3. **PostgreSQL READ COMMITTED**: lock first, then re-check state and version under the lock. Lock order is
   booking row, then unit row, then skipper row (`FOR UPDATE`). SQLite has a single writer through the FIFO queue.
4. **Pure packages**: `internal/domain/rental/price`, `internal/domain/rental/slots`, `internal/domain/rental/states`
   and the new `internal/domain/pricing/rental.go`. No store, no clock, no `time.Now`, no randomness, no float.
   CONTRACT adds the three rental packages to `pure-engines` in `.golangci.yml`. `pricing.EngineVersion` does not
   change, because `pricing.Quote` output is untouched.
5. Units: money is `money.Cents`. Litres, engine hours and quantities are `qty.Milli`. Percentages are basis
   points. Dates are `calendar.Date` in the marina time zone (`marinas.timezone`). A slot is a 30-minute
   wall-clock index 0..47 of a local date. Fuel level is in eighths, 0..8.
6. **One state-change point** (new INV-97): only `rental/reservation` writes `rental_bookings.status`, slot rows
   and hold expiry. Every change writes audit and outbox in the same transaction. HANDOVER calls the
   `reservation.*Tx` methods for its transitions.
7. **Money rules**: a deposit is never revenue until it is retained (INV-98). Exported charges are immutable, and
   corrections are linked adjustment charges (INV-51). Rental charges go only through `charge.CreateTx`.
   Payments go only through `payment.RecordTx`, `RefundTx` and `MatchTx`. Fiscal documents go only through
   `fiscal.RecordTx`.
8. Every query is scoped by `marina_id` from `p.Authorize`, and another marina's resource is 404. Permissions are
   checked in the domain services (§13.1). **No PII** in logs, events, SSE or webhooks (INV-70). Renter names,
   phones, emails, document and licence numbers and skipper names never leave the DB. Identity data is encrypted
   at rest (KeyRing, AAD per row).
9. **API first**: CONTRACT edits `api/openapi.yaml` and runs `make gen`. Nobody edits generated files. Migrations
   are `00023_rental_fleet.sql`, `00024_rental_bookings.sql` and `00025_rental_handovers.sql`, in both dialects
   with the same names, registered in `store.BackupTables()`. Never touch 00001–00022.
10. **UI**: shadcn only (official CLI, exported from `@porto/shared`), Tailwind token classes, no custom CSS and
    no arbitrary values. i18n in it (source) and en. Phone screens work from 360 px. No new npm or Go dependency.
    Signature drawing and planner drag use pointer events on a `<canvas>`/SVG, as M9/M10 do.
11. `cmd/porto` stays `CGO_ENABLED=0`. Do not `git commit`. No destructive git, no `rm -rf`. Docker is allowed
    only for your own throwaway containers (`scripts/with-postgres.sh`). Build with `-o` into a temp dir. Edit only
    the files you own (§16). While iterating, test only the packages you touch. Run full gates per §16.5.

## 1. Scope

### 1.1 In M11

| Area | Content |
|---|---|
| Fleet (19.1) | `rental_units` linked 1:1 to registry vessels (11.2). A unit has: code, category (`rib`, `open`, `cabin`, `sail`, `pedalo`, `sup`), seats, licence requirement (`never`, `always`, `threshold` on total engine kW), fuel type and tank, default deposit, status (`available`, `maintenance`, `retired`), home berth, rental item, ownership (`marina`, `third_party` + owner customer, information only) and last engine hours. |
| Skippers (19.1) | `rental_skippers` (name, contact encrypted, licence expiry, active, optional user link), a weekly working calendar (soft rule) and days off as blocks (hard, DB). |
| Formulas and prices (19.2, 12.4) | Formula kinds `hourly`, `half_day` (configurable bands), `day`, `multi_day`, `week`. Skipper mode `none`, `optional` or `required`. Fuel `included` or `metered`. Late-fee item, grace and step. Prices come only from M2 price lists: rental items are items with revenue class `rental`, rules use the new time units `hour`, `half_day`, `day` (and `week`), with rate periods and the new scope `time_bands`. The pure `pricing.RentalQuote` prices them. Extras (snorkelling kit, ice box, roadstead mooring) are catalogue items charged per rental or per day. |
| Planner (19.3) | A unit × time grid with views **hours / half days / days** (shared component `web/shared/src/planner`), skipper rows, drag to create, drag to move, colours by state, blocks. |
| Occupancy (19.3, 10) | `rental_slots` PK `(unit_id, date, slot)` with 30-minute slots. A half day occupies its band. A configurable buffer after each rental. `rental_skipper_slots` gives skippers the same guarantee. Holds use the M3/M8 scheme (`expires_at`, reclaim at conflict time, expiry job). |
| States (19.3) | `held → confirmed → checked_out → returned → closed`, plus `cancelled` and `no_show`, through `reservation` (audit + outbox). |
| Check-out (19.4) | Phone or tablet: identity and licence typed by the operator with document photos, rental contract signed on screen (or OTP, or paper), deposit (cash, card POS, pre-authorisation through a provider with `PaymentAuthorizer`, or waived with a reason), checklist, fuel, engine hours, photos. Handover PDF from the template. |
| Check-in (19.4) | Actual return time with a late fee per the formula, fuel and engine hours, checklist, damages with photos and estimate, extras charged, metered fuel charged, deposit released or retained (fully or partially). Return PDF. |
| Quick cash (19.5) | One step from the booking: cash, card POS or payment link. Commercial document through `FiscalDriver` (receipt mode). If the customer asks for an invoice, the charge is invoice-mode and goes to the export (16.5). |
| Reports (21) | Report `rental` (views `utilisation`, `revenue`, `damages`, `deposits`) and the tiles `rental_utilisation` and `rental_revenue` (become available). |
| Phone | `/m/$id/rental-phone` (landing for `rental.manage` without `presence.write`): today, booking card, check-out and check-in wizards, quick cash, walk-in booking, booking QR scan (`PR1:<booking_id>`). |
| Offline (29.2) | Kind `rental.checklist` only: checklist, fuel, engine hours and photos of a confirmed or checked-out rental. Everything else is online. |

### 1.2 Out of M11

| Item | Milestone | Seam left |
|---|---|---|
| **Online rental sale on `/b/{slug}`** (spec 32 M11 mentions "vendita online") | after M12. It is not cheap on M8: it needs public availability, a token-scoped booking, a new intent purpose and a webhook confirmation path, a public-page UI and customer-area views. The orchestrator must confirm the deferral (ADR 0016). | `rental_bookings.channel` CHECK (`desk`, `api`, `online`). `held` + `hold_expires_at` with the same TTL semantics as M8 (`PORTO_HOLD_TTL`). Confirmation of a held rental from a payment webhook is a reserved `reservation.ConfirmTx` source `provider`. |
| Sensors | M12 | none |
| Owner revenue sharing for `third_party` units (mandate accounting) | after M12 | `ownership`, `owner_customer_id` stored and shown only |
| Advanced (FEA/FEQ) signature of rental contracts | D5 / after M12 | the `provider` driver already exists generically |
| Automatic modifiers and discounts on rentals (customer category, promo, long-stay) | after M11 | `RentalQuote` honours customer category and channel only in base selection (D4) |
| Cancellation fees and policies for rentals | after M11 | cancel cancels the cancellable main charge. A paid charge stays, and the office refunds or keeps a credit (`refunds.issue`) |
| Fuel station sales outside rentals, fuel stock | never in core | — |

## 2. Architecture and packages

```
web: features/rental-fleet + reports/dashboard rental (FLEET) │ shared/planner + features/rental-planner (BOOKING) │ features/rental-handover + features/phone/rental (HANDOVER)
        │                                                   │                                                    │
http/admin/rental_fleet.go                     http/admin/rental_bookings.go                     http/admin/rental_handover.go
        ▼                                                   ▼                                                    ▼
internal/domain/rental/fleet ──► pricing (RentalQuote, UnitPrice)   rental/reservation ──► rental/slots, rental/states (pure)   rental/handover ──► reservation.*Tx
   └► rental/price (pure: quote wrapper, late fee, fuel, deposit)      holds, reclaim, planner, blocks, jobs                      charge, payment, cash, fiscal, collect, docs, signature
        ▼                                                   ▼                                                    ▼
internal/store (rental_fleet.go report_rental.go │ rental_bookings.go rental_slots.go rental_invariants.go │ rental_handovers.go rental_damages.go)
```

| Package | Kind | Owner |
|---|---|---|
| `internal/domain/pricing/rental.go` (+ tests) | pure: `RentalQuote`, new `TimeUnit`s and `Scope.TimeBands`, validation of rental rules | FLEET |
| `internal/domain/rental/price` | pure: booking price snapshot, late fee, fuel, return totals, deposit proposal | FLEET |
| `internal/domain/rental/fleet` | units, skippers, skipper calendars, formulas, extras, settings, `Quote` service, report seams | FLEET |
| `internal/domain/rental/slots` | pure: time ↔ slot, bands, formula/range validation, range expansion with buffer, DST | BOOKING |
| `internal/domain/rental/states` | pure transition table (CONTRACT fills it, BOOKING tests it) | CONTRACT / BOOKING |
| `internal/domain/rental/reservation` | bookings, slots, holds, reclaim, state machine (only writer), blocks, planner, availability, jobs | BOOKING |
| `internal/domain/rental/handover` | check-out, check-in, identity, signature, deposits, damages, return charge, quick cash, PDFs, phone day view, offline dedupe | HANDOVER |
| `internal/domain/report` (rental views, tiles) | M7 machinery | FLEET |

Dependencies: `handover` → `reservation` (Tx methods), `fleet` (read), `price`, `charge`, `payment`, `cash`,
`fiscal`, `collect`, `docs`, `signature`. `reservation` → `fleet` read helpers via store, `price`, `slots`,
`states`, and never `handover`. `fleet` → `pricing`, `pricelist`. `collect` calls rental through the new hook
interface `collect.RentalDepositHooks` (CONTRACT writes the call sites, HANDOVER implements it, `app.go` wires it).
`signature` gets the subject `rental` through `RegisterSubject` (HANDOVER implements the subject).

## 3. Fleet, skipper and formula model (normative, FLEET)

### 3.1 Units `rental_units`

`vessel_id` (unique per marina, existing registry vessel, its marina), `code` (`^[A-Z0-9_-]{1,16}$`, unique),
`category` CHECK (`rib`,`open`,`cabin`,`sail`,`pedalo`,`sup`), `seats` 1..99, `licence` CHECK
(`never`,`always`,`threshold`), `fuel_type` CHECK (`petrol`,`diesel`,`electric`,`none`), `tank_milli` (litres,
≥ 0, > 0 when fuel ≠ none/electric), `deposit_cents` ≥ 0, `status` CHECK (`available`,`maintenance`,`retired`),
`home_berth_id?` (berth FK), `item_id` (an item with revenue class `rental`, validated), `ownership` CHECK
(`marina`,`third_party`), `owner_customer_id?` (required iff third_party), `engine_hours_milli` (last recorded,
written only by check-in), `sort`, `version`.

- **Licence needed** (pure `price.LicenceRequired`): `never` → false. `always` → true. `threshold` → Σ
  `vessels.engines[].power_kw` (milli) > `rental_settings.licence_threshold_kw_milli` (default 30 000 = 30 kW). In
  every case it is false when the booking has a skipper (the skipper is the licensed master, D7).
- Status `maintenance`/`retired` makes the unit non-bookable for **new** bookings and reschedules (`422
  unit_not_bookable`, an app rule, not the occupancy guarantee). Existing bookings are shown with a warning.
  `retired` is hidden from the planner. Out-of-service periods are **blocks** (§4.5), which are DB-enforced.

### 3.2 Skippers `rental_skippers`, `rental_skipper_hours`

Skipper: `name` (≤ 80, PII, only shown to `rental.manage`), `contact_enc` (phone/email, KeyRing AAD
`rental-skipper:<id>`), `licence_expires_on?`, `user_id?` (unique partial), `active`, `version`. Weekly calendar
`rental_skipper_hours (skipper_id, weekday 1..7, from_slot, to_slot)`. A booking outside it is refused `422
skipper_unavailable` unless `allow_outside_hours = true` (soft, audited). Days off and absences are blocks with a
skipper target (hard). Booking with a skipper whose licence expires before the end date gives `422
skipper_licence_expired`.

### 3.3 Formulas `rental_formulas`

`code`, `name` (I18n), `kind` CHECK (`hourly`,`half_day`,`day`,`multi_day`,`week`), `band_code?` (required iff
half_day, must exist in settings), `min_slots` (hourly, default 2 = 1 h), `max_slots` (hourly, default = open
window), `min_days` (multi_day ≥ 2, week ≥ 7), `max_days` (default 30), `skipper` CHECK
(`none`,`optional`,`required`), `fuel` CHECK (`included`,`metered`), `fuel_item_id?` (required iff metered),
`late_item_id?` (null = no late fee), `grace_minutes` 0..120 (default 15), `late_step_minutes` ∈ {15, 30, 60}
(default 30), `categories` json (empty = all), `active`, `sort`, `version`. Writes need `marina.configure`.

### 3.4 Extras `rental_extras`

`item_id`, `charge_mode` CHECK (`per_rental`,`per_day`), `default_qty_milli`, `max_qty_milli`, `categories`,
`active`, `sort`. They are priced with `pricing.UnitPrice` on the start date. "Days" of `per_day` =
`price.Days(range)` (hourly and half-day = 1). Writes need `marina.configure`.

### 3.5 Settings `rental_settings` (marina PK, defaults when never saved; `marina.configure`, If-Match)

| Field | Default | Rule |
|---|---|---|
| `open_from_slot`, `open_to_slot` | 16, 38 (08:00–19:00) | `0 ≤ from < to ≤ 48`, excluding slots 4–5 (02:00–03:00) |
| `bands` | `morning` 16–26 (08:00–13:00), `afternoon` 28–38 (14:00–19:00), labels I18n | 1..4 bands, inside the open window, pairwise gap ≥ `buffer_slots` (`422 band_gap_lt_buffer`) |
| `buffer_slots` | 1 (30 min) | 0..8 |
| `desk_hold_minutes` | 60 | 5..1440 (planner "Opzione") |
| `no_show_after_minutes` | 60 | the phone flags late pickups; `no_show` stays manual |
| `licence_threshold_kw_milli` | 30 000 | > 0 |
| `skipper_item_id`, `damage_item_id` | none | items with revenue class `rental` (skipper) / any (damage). A booking with a skipper and no skipper item gives `422 no_price` |
| `checklist` | 8 seeded items (hull, engine, lifejackets, anchor, fire extinguisher, VHF, documents on board, cleanliness), each with categories | 1..30 items |
| `checkout_min_photos`, `checkin_min_photos` | 2, 1 | 0..10 boat photos (document photos counted separately) |
| `deposit_modes` | `cash`,`card_pos`,`preauth` | subset; `preauth` effective only when an enabled provider implements `ext.PaymentAuthorizer` |
| `identity_retention_days` | 365 | 30..3650, purge of identity data and document photos after `closed_at` |
| `default_register_id?` | none | cash register for quick cash |

## 4. Slot model, holds and reclaim (normative, BOOKING)

### 4.1 Keys and time

- A slot `s` ∈ 0..47 is the wall-clock half hour `[s×30 min, (s+1)×30 min)` of a local date in the marina time
  zone. A booking range is `(starts_on, start_slot)` inclusive to `(ends_on, end_slot)` exclusive, with `end_slot`
  1..48. The API carries `{date, slot}` pairs. Responses add `starts_at`/`ends_at` (RFC 3339 UTC, computed with
  `time.Date(y, m, d, h, min, 0, 0, loc)`).
- Pure `slots.FromClock(h, m)` rounds the minute **down**. `slots.Clock(s)` and `slots.Label(s)` give `"HH:MM"`
  (48 = `24:00`). `slots.Instant(date, slot, loc)` converts to an instant.
- **DST**: every local date has 48 slot labels. Slots 4–5 (02:00–03:00) are the only labels that do not exist
  (spring) or occur twice (autumn) in the European zones. The open window and every band must exclude them, so
  hourly, half-day and day rentals never touch them. Multi-day and week rentals insert rows for every label
  (including 4–5) and are priced per date, so DST changes neither rows nor price. Late fees use real elapsed
  minutes between instants (§5.4). Utilisation counts slots inside the open window (wall clock).

### 4.2 Formula ↔ range rules (pure `slots.ValidateRange(formula, settings, range)`)

| Kind | Range | Priced quantity |
|---|---|---|
| `hourly` | same date, `open_from ≤ start < end ≤ open_to`, `min_slots ≤ end−start ≤ max_slots` | slots (30 min each) |
| `half_day` | exactly the band `[from, to)` of `band_code` on one date | 1 |
| `day` | exactly `[open_from, open_to)` of one date | 1 |
| `multi_day` | `days = ends_on − starts_on + 1`, `min_days ≤ days ≤ max_days`, `start_slot ∈ [open_from, open_to)`, `end_slot ∈ (open_from, open_to]` | days (calendar dates touched) |
| `week` | as multi_day with `days ≥ 7` | weeks + remainder days (§5.2) |

Errors: `422 formula_range_mismatch {kind, expected}`, `422 outside_rental_hours`.

### 4.3 Rows

`slots.Expand(range, buffer)` returns the ordered list of `(date, slot)` from start to end, then `buffer` more
slots after the end. These carry over midnight: end slot 48 + buffer 1 means slot 0 of the next date.

| Table | PK (the guarantee) | Columns |
|---|---|---|
| `rental_slots` | `(unit_id, date, slot)` | `marina_id`, `booking_id?`, `block_id?`, `kind` CHECK (`hold`,`booked`,`buffer`,`block`), `expires_at?` |
| `rental_skipper_slots` | `(skipper_id, date, slot)` | same columns, kind CHECK (`hold`,`booked`,`block`) |

CHECKs: exactly one of `booking_id`/`block_id` is set. `kind = 'block'` ⇔ `block_id` is set. `expires_at IS NOT NULL` ⇔
the booking is held. Skipper rows = the rental range **without** buffer (D9). FKs are booking/block `ON DELETE
CASCADE`, but deletes are explicit.

Examples (buffer 1):
- Hourly 2027-06-12 10:00–13:00: unit rows slots 20–25 `booked`, 26 `buffer`, so 7 rows.
- Half-day morning: slots 16–25 + buffer 26, so 11 rows. The afternoon band (from 28) never collides.
- Multi-day 2027-06-14 09:00 → 2027-06-16 19:00: 30 (18–47) + 48 + 38 (0–37) + 1 buffer (06-16 slot 38) = 117 rows.
- Two hourly bookings 10:00–12:00 and 12:00–13:00 on the same unit: the first one's buffer row 24 collides with
  the second one's slot 24, so the second gets `409 rental_slot_taken`. 12:30 is the earliest start.

### 4.4 Allocation algorithm `reservation.allocateTx(booking, range, skipper?)`

1. `LockBooking` (when it exists), `LockRentalUnit` (status re-check: `available`; `unit_not_bookable`), and
   `LockRentalSkipper` when set (active).
2. `slots.ValidateRange`. Skipper weekly hours (soft) and licence expiry.
3. **Reclaim expired holds touching the plan**: `DELETE FROM rental_slots WHERE booking_id IN (SELECT booking_id
   FROM rental_slots WHERE unit_id = :u AND date BETWEEN :d0 AND :d1 AND expires_at < :now)`. Do the same on
   `rental_skipper_slots` for the skipper. Delete **every** row (both tables) of those bookings and set
   `slots_lost_at = now` on them, without changing their status (the job does that, §4.6).
4. `DELETE` this booking's own rows (reschedule).
5. `INSERT … ON CONFLICT DO NOTHING` the unit rows in chunks of 500, then the skipper rows. Sum the affected rows.
6. If inserted < planned, `SELECT` the conflicting keys (other booking or block) and return `*SlotConflict{target:
   unit|skipper, date, slots, booking_ids, block_ids}`. The caller's tx rolls back everything, steps 3–4 included.
   The API returns `409 rental_slot_taken` (unit) or `409 skipper_busy`. No other path decides a conflict.
7. Audit `rental_booking.allocate` and outbox `rental.changed` (written by the calling operation).

Rows are kind `hold` with `expires_at = hold_expires_at` while held, and `booked` afterwards (`confirmTx` updates
`kind`/`expires_at` of the booking's rows in one statement per table, keeping rows). If a held booking's rows were
reclaimed, `confirmTx` re-runs `allocateTx` or fails `409 rental_hold_expired`.

### 4.5 Blocks `rental_blocks`

`unit_id?` xor `skipper_id?`, range `(from_on, from_slot)` to `(to_on, to_slot)`, `reason` CHECK
(`maintenance`,`closure`,`day_off`,`other`), `note` ≤ 200, `version`. Create/update/delete need `rental.manage`.
They insert `block` rows through the same insert-and-count path. A conflict gives `409 rental_slot_taken` with
the booking ids. A block never moves bookings.

### 4.6 Holds (INV-05 extended)

| Origin | `hold_expires_at` | Converted by | On expiry |
|---|---|---|---|
| Planner "Opzione" / API `hold: true` | `now + desk_hold_minutes` | `confirm` action (`rental.manage`) | job `rental.holds_expire` (1 min): `held` with `hold_expires_at < now` → `cancelled` (`cancel_reason = hold_expired`), explicit delete of remaining rows, audit + `rental.changed` |
| Online (deferred, §1.2) | `now + PORTO_HOLD_TTL` | reserved `confirmTx(source = provider)` | same |

`extendHold` (`rental.manage`, If-Match) moves `hold_expires_at` and the rows' `expires_at` together. Lazy reclaim
(§4.4 step 3) means an expired hold never blocks a sale, even before the job runs
(`TestReservation_ExpiredHoldIsReclaimed`).

## 5. Pricing (normative, FLEET)

### 5.1 Price-list additions (pure, `EngineVersion` unchanged)

- New `TimeUnit`s: `hour`, `half_day`, `day`, alongside the existing `week`. `pricing.Validate` accepts them only on
  base rules with basis `flat` for items with revenue class `rental`. Such rules are ignored by `Quote` (no
  contract type uses them).
- `Scope.TimeBands []string` (JSON `time_bands`, `omitempty`, so published catalogues are byte-identical, INV-26)
  holds band codes. It is honoured only by `RentalQuote` and adds 1 to specificity, like the other scope lists.
- Rental items are ordinary items (`service`, unit `hour`/`lump`) with revenue class `rental`. No new `item_type` (D3).

### 5.2 `pricing.RentalQuote(cats []Catalog, in RentalInput) (RentalBreakdown, error)`

```go
type RentalKind string // hourly, half_day, day, multi_day, week
type RentalExtra struct { ItemID string; Qty qty.Milli; PerDay bool }
type RentalInput struct {
    ItemID string; Kind RentalKind; Start calendar.Date; Days int; Slots int; Band string
    SkipperItemID string                 // "" = no skipper
    Extras []RentalExtra
    Channel Channel; CustomerCategory string; QuotedOn calendar.Date
}
type RentalLine struct { Seq int; Kind string /* unit|skipper|extra */; ItemID, ItemCode string; Qty qty.Milli
    UnitCents money.Cents; Amount money.Cents; RuleIDs []string; PriceIncludesVAT bool; RevenueClass string }
type RentalBreakdown struct { Lines []RentalLine; Total money.Cents; EngineVersion int; Trace []string }
const RentalEngineVersion = 1
```

Base selection reuses `ruleCmp`. A rule matches when it is a base rule of the item, its `Unit` equals the kind's
unit, its list validity and period scope cover the **date being priced**, and its customer category, channel and
`time_bands` (half-day) scopes match. No rule gives `*NoPriceError{Reason: ReasonNoRule}`. The unit line and the
skipper line (same unit, `SkipperItemID`) use the same algorithm:

- `hourly`: `amount = UnitCents(hour rule of Start) × Slots / 2`, half-even. Qty = `Slots × 500` milli-hours.
- `half_day` / `day`: amount of the matching rule on `Start`.
- `multi_day`: Σ over each date of the range of that date's `day` rule (periods may differ per date).
- `week`: `w = Days / 7`, `r = Days mod 7`. For block `k` (start `Start + 7k`): the `week` rule of its first date.
  Remainder = `min(Σ day rules of the r remaining dates, week rule of the remainder's first date)`. No `week` rule
  gives `no_price` (the formula is not offered).
- Extras: `pricing.UnitPrice(item, Start)` × qty, × `price.Days` when `PerDay`, half-even.
- No modifiers, no discounts, no total rounding in M11 (D4). VAT is split later by `charge.CreateTx` per line, as
  for every charge.

`rental/price.Snapshot(fleet data, booking input) → Snapshot{Breakdown, Days, Licence, Kind, …}` is stored on the
booking (`price_snapshot`, `engine_version`). Recomputing with the same catalogue gives the same total (INV-20
extended, `TestRentalQuote_Recompute`).

### 5.3 Worked examples (seed price list `NOL-2027`, all prices VAT 22 % **included**; period `alta` = 2027-07-01..2027-08-31)

| Item | hour | half_day morning | half_day afternoon | day | week |
|---|---|---|---|---|---|
| `NOL-GOM58` (rib 5.8 m, 40 kW, tank 60 l) | 45.00 (alta 55.00) | 150.00 (alta 180.00) | 130.00 (alta 160.00) | 240.00 (alta 290.00) | 1,400.00 |
| `SKIPPER` | 25.00 | 100.00 | 100.00 | 180.00 | 1,000.00 |

Extras: `SNORKEL` 10.00 per rental, `FRIGO` 5.00 per day, `RADA` 15.00 per rental. Unit prices via `UnitPrice`:
`FUEL-P` 2.10 €/l, `LATE-GOM` 60.00 €/h.

| # | Input | Expected |
|---|---|---|
| E1 | hourly 2027-06-12 10:00–13:00 (6 slots) | 3 × 45.00 = **135.00** |
| E2 | hourly 2027-07-10 09:30–12:00 (5 slots, alta) | 2.5 × 55.00 = **137.50** |
| E3 | half_day morning 2027-06-12 + skipper + SNORKEL ×1 | 150.00 + 100.00 + 10.00 = **260.00**. Line 150.00: net 122.95, VAT 27.05 |
| E4 | half_day afternoon 2027-08-15 (alta) | **160.00** |
| E5 | multi_day 2027-06-29 → 2027-07-01 (3 dates: two base, one alta) + FRIGO per day ×1 | 240 + 240 + 290 + 3 × 5.00 = **785.00** |
| E6 | week formula, 9 days from 2027-06-05 | 1,400.00 + min(2 × 240.00, 1,400.00) = **1,880.00** |
| E7 | week formula, 13 days from 2027-06-05 | 1,400.00 + min(6 × 240.00 = 1,440.00, 1,400.00) = **2,800.00** |
| E8 | hourly with an hour rule of 45.01, 5 slots | 45.01 × 2.5 = 112.525 → half-even **112.52** |
| E9 | week formula, no week rule for the item | `no_price` |
| E10 | half_day for band `evening` with no rule scoped to it and no unscoped half_day rule | `no_price` |

### 5.4 Late fee (pure `price.LateFee(plannedEnd, actual time.Time, graceMin, stepMin int, hourCents money.Cents)`)

`late = minutes(actual − plannedEnd) − grace`, from real elapsed time between instants. If `late ≤ 0`, the fee is
0. Otherwise `blocks = ceil(late / step)` and `fee = blocks × round_half_even(hourCents × step / 60)`.
`hourCents` = `UnitPrice(formula.late_item_id, ends_on)`. No late item means no fee. With `LATE-GOM` 60.00, grace
15, step 30, planned end 13:00:

| Return | Late after grace | Blocks | Fee |
|---|---|---|---|
| 13:14 | −1 | 0 | 0.00 |
| 13:16 | 1 | 1 | 30.00 |
| 13:45 | 30 | 1 | 30.00 |
| 13:50 | 35 | 2 | **60.00** |
| 15:20 | 125 | 5 | 150.00 |
| 02:30 on the autumn DST day, planned end 02:00 (first occurrence), returned at the second 02:30 | 75 (real minutes) | 3 | 90.00 |

Early return gives no refund (D11). The unit and skipper rows after `slot_up(actual) + buffer` are released
(§6.3).

### 5.5 Fuel, extras, damages at return (pure `price.Return(...)`)

- Metered fuel: `litres = (out_eighths − in_eighths) × tank_milli / 8`, half-even to the milli and clamped ≥ 0,
  or an explicit `refuel_milli` typed by the operator (wins when given). `amount = qty.Mul(litres, UnitPrice(fuel_item,
  returned date))` half-even. Example: tank 60 l, 8/8 → 5/8 gives 22.500 l × 2.10 = **47.25**. `included` gives no
  charge.
- Extras used at return beyond those booked: priced as §5.2 on the start date.
- Damages: `rental_damages` with an estimate. `price.DepositProposal(deposit, Σ open estimates) = min(deposit, Σ
  estimates)`. The excess (Σ estimates − retained, when > 0) becomes a return-charge line on `damage_item_id`.

## 6. State machine (CONTRACT fills `rental/states`, BOOKING implements, INV-97)

| From | Action | To | Side effects | Events |
|---|---|---|---|---|
| – | `create` (hold) | `held` | `allocateTx` kind hold | `rental.changed` (`created`) |
| – | `create` (confirm) | `confirmed` | `allocateTx` kind booked, contract document composed (async render), optional `rental_confirmed` email | `rental.changed` (`created`), `rental.confirmed` |
| `held` | `confirm` | `confirmed` | rows → booked (or re-allocate / `409 rental_hold_expired`) | `rental.confirmed` |
| `held`, `confirmed` | `reschedule` (PATCH unit/range/skipper, If-Match) | same | re-allocate; price snapshot recomputed unless `keep_price` (`marina.configure`) | `rental.changed` (`rescheduled`) |
| `checked_out` | `extend` (new end) | same | re-allocate the tail; reprice; main charge adjusted (§9.2) | `rental.changed` (`extended`) |
| `held`, `confirmed` | `cancel` (reason) | `cancelled` | delete rows; cancel the main charge if cancellable; void the pre-auth after commit | `rental.cancelled` |
| `held` | job `rental.holds_expire` | `cancelled` (`hold_expired`) | delete rows | `rental.cancelled` |
| `confirmed` | `no_show` (after start) | `no_show` | delete rows; main charge kept (staff may cancel it); void the pre-auth | `rental.changed` (`no_show`) |
| `confirmed` | `checkOutTx` (HANDOVER only) | `checked_out` | §7 | `rental.checked_out` (public) |
| `checked_out` | `returnTx` (HANDOVER only) | `returned` | truncate rows on early return; §8 | `rental.returned` (public) |
| `returned` | `closeTx` (HANDOVER: auto at check-in when nothing is pending; collect hook after capture; manual `close`) | `closed` | requires deposit status final and the return charge decided | `rental.changed` (`closed`) |

Every action locks the booking, checks `version` (If-Match) and state under the lock, then writes audit and outbox.
Any other transition is `409 invalid_transition`. `closed`, `cancelled` and `no_show` are terminal. The `returned`
and `closed` rows stay as history (utilisation reports read rows of `booked` kind). INV-19 is checked by query Q32
(§15).

## 7. Check-out on the phone (normative, HANDOVER; target < 3 min)

Draft state lives in `rental_handovers` (one row per `(booking_id, phase)`, `phase` ∈ `checkout`,`checkin`, unique):
`checklist` json, `fuel_eighths`, `engine_hours_milli`, `identity_enc` (checkout: doc type, number, expiry,
issuer?, licence number, category, expiry; KeyRing AAD `rental-identity:<handover_id>`), `photo_ids`, `notes`,
`client_ref` (offline dedupe), `completed_at`, `version`. Files are in `rental_handover_files (handover_id, kind ∈
id_document|licence|boat|damage, file_id)` through the encrypted filestore (INV-74).

| Step | Screen | Operator input (minimum) | Server |
|---|---|---|---|
| 0 | Oggi → tap the booking (or scan `PR1:` QR) | 1 tap | `getRentalDay` (cached), `getHandover` |
| 1 | Identità | document type (prefilled `identity_card`), **number**, **expiry**, 1 photo | `putHandoverIdentity` (online, validates expiry ≥ today) |
| 1b | Patente (only when §3.1 says a licence is required) | **number**, **expiry**, category select (prefilled), 1 photo | same call. Expired licence gives `422 licence_expired` |
| 2 | Contratto | read summary, signature drawn on canvas, "Firma" | `signRentalContract {driver: screen, png}`: the contract PDF composed at confirmation is stamped with the signature image and an evidence page (operator, device, instant, SHA-256), `docs.InsertSignedTx`, request `signed` (driver `screen`, D6 label "firma elettronica semplice"). `otp` and `paper` are available from the same screen |
| 3 | Cauzione | mode tap (amount prefilled from the unit) | `cash`/`card_pos`: `payment.RecordTx(kind deposit)` (cash needs an open session, `409 cash_session_required`). `preauth`: intent `rental_deposit`, `capture_method = manual`, URL as QR on the phone, `deposit_status = pending` until the `authorized` webhook (collect hook). `waived`: reason required |
| 4 | Barca | checklist "Tutto ok" (1 tap) or toggle items, fuel slider (prefilled 8/8), **engine hours** (prefilled with the unit's last value), photos ≥ `checkout_min_photos` | `saveHandoverChecklist` + `uploadHandoverPhoto` (offline-capable, `rental.checklist`) |
| 5 | Riepilogo | "Consegna" | `checkOut` (If-Match): one tx locks the booking and checks identity (+ licence), signed contract, deposit `held`/`authorized`/`waived`, checklist saved with the minimum photos, engine hours ≥ the unit's. Creates the main charge if missing (§9), calls `reservation.CheckOutTx`, sets `checked_out_at`, composes `rental_handover` (phase checkout, async render), writes audit and `rental.checked_out`. Missing steps give `422 handover_incomplete {missing: [...]}` |
| (5b) | Incasso (optional, same screen) | method tap | `quickCash` (§9.3), when the main charge is unpaid |

Minimum interaction budget, enforced by the e2e test (licence required, cash deposit, 2 boat photos): **≤ 22
taps, ≤ 5 typed fields** (document number, document expiry, licence number, licence expiry, engine hours), **4
photos** (document, licence, 2 boat), **1 signature**, ≤ 6 HTTP writes on the critical path.

## 8. Check-in on the phone (normative, HANDOVER; target < 3 min)

| Step | Screen | Operator input (minimum) | Server |
|---|---|---|---|
| 0 | Oggi ("Rientri") → tap, or scan the QR | 1 tap | cached day view |
| 1 | Rientro | return time (prefilled with now, editable) | — |
| 2 | Barca | checklist "Tutto ok", fuel slider, **engine hours**, photos ≥ `checkin_min_photos` | `saveHandoverChecklist` (phase checkin, offline-capable) |
| 3 | Danni | "Nessun danno" (1 tap), or add: photo(s), description, **estimate** | `createRentalDamage` (online) |
| 4 | Extra | prefilled booked extras; add consumed ones (tap ±) | — |
| 5 | Riepilogo | shows late fee, fuel, extras, damage excess, deposit proposal (§5.5) from `checkInPreview` (stateless). Deposit decision: **Restituisci** / **Trattieni tutto** / **Trattieni** amount (prefilled with the proposal) | `checkIn` (If-Match), one tx: §8.1 |
| 6 | Incasso | method tap when the return charge > 0 | `quickCash(target: return)` |

### 8.1 `checkIn` transaction

1. Lock the booking, check `checked_out` and the version. Engine hours ≥ check-out (`422 engine_hours_decreasing`).
   `returned_at ≥ checked_out_at` and ≤ now + 5 min.
2. Return charge: late fee + metered fuel + extra extras + damage excess. When > 0, call `charge.CreateTx(origin
   rental, origin_id booking)`, linked as kind `return` (unique). Fiscal mode follows the booking (§9.1).
3. Deposit decision (INV-98):
   - `cash`/`card_pos`: retain `r` gives `payment.RetainDepositTx(deposit_payment, r, fiscal_mode)`, which creates
     the `deposit_retention` charge (receipt mode and fiscal document through `fiscal.RecordTx` when the booking is
     receipt-mode) and matches the deposit to it. Release `d − r` gives `payment.RefundTx(method = deposit method;
     cash needs an open session)`.
   - `preauth`: set `deposit_decision` (amount `r`). After commit, `collect.CaptureIntent` with amount `r` (`r > 0`)
     or `VoidIntent` (`r = 0`). The `succeeded` webhook runs the hook `OnRentalDepositCapturedTx`, which records the
     deposit payment (kind deposit, `card_online`, amount `r`), retains it in full and closes the booking. The job
     `rental.deposits` (1 min) retries capture or void with the same idempotency keys.
   - `waived`: nothing. `r > deposit` gives `422 deposit_exceeds`.
4. Damages → `charged` (estimate covered by retention or charged on the return charge) or `waived` (reason).
5. Unit `engine_hours_milli` updated (conditional on increase). `reservation.ReturnTx(returned_at)` releases rows
   after `slot_up(returned_at) + buffer` on early return. Audit and `rental.returned`. `rental_handover` (phase
   checkin) is composed.
6. `closeTx` in the same tx when the deposit is final (released, retained or waived) and no capture is pending.

Budget: **≤ 18 taps, ≤ 1 typed field** (engine hours), **1 photo** with no damage. The damage path adds 1 photo + 2
fields.

### 8.2 Worked check-out/check-in (acceptance `TestAcceptance_M11_CheckOutCheckIn`, Europe/Rome)

Booking: unit `GOM58` (40 kW, so a licence is required), formula `MEZZA-MATTINA` (half_day morning, fuel metered,
late item `LATE-GOM`, grace 15, step 30), 2027-06-12, extras `FRIGO` ×1 and `SNORKEL` ×1, no skipper, walk-in
renter (no customer), receipt mode, deposit 500.00 cash.

- Price snapshot: 150.00 + 5.00 + 10.00 = **165.00**. Rows: unit slots 16–25 booked + 26 buffer (11 rows).
- Check-out at 08:35: main charge 165.00 (origin `rental`, receipt) is collected in cash with a fiscal document.
  The deposit payment is 500.00 (kind deposit, cash). Fuel 8/8, engine hours 120.5.
- Check-in at 13:50, fuel 5/8, engine hours 125.0, one damage estimated at 180.00. Late fee **60.00**, fuel
  **47.25**, return charge **107.25**, paid by card POS with a fiscal document. Retention **180.00**
  (`deposit_retention` charge, receipt, fiscal document), release **320.00** (cash refund). Status `closed`.
- Checks: rental-origin charges Σ = 272.25. Deposit payment `allocated = 180.00`, `refunded = 320.00`. Cash
  session expected cash = 165.00 + 500.00 − 320.00 = **345.00**, card 107.25. Corrispettivi for the day = 165.00
  + 107.25 + 180.00 = **452.25**, and the 500.00 deposit is never in corrispettivi or revenue reports. The unit's
  `engine_hours_milli` = 125 000. The return (13:50) is after the planned end, so the rows stay unchanged as
  history (16–25 booked, 26 buffer).

## 9. Charges, quick cash and FiscalDriver (normative, HANDOVER)

### 9.1 Charges

- `rental_booking_charges (booking_id, charge_id, kind ∈ main|return|adjustment)` with **unique partial
  `(booking_id, kind) WHERE kind IN ('main','return')`** (INV-99). Charges have `origin_type = 'rental'`,
  `origin_id = booking id`, `vessel_id` = unit vessel, `customer_id` = booking customer or null.
- `fiscal_mode`: `receipt` unless `invoice_requested`. Invoice mode requires `customer_id` (`422
  customer_required`; the office links or creates the customer, since `rental` only reads customers). The flag can
  change only while no rental charge is exported. Invoice-mode charges reach the export (16.5). Receipt-mode charges
  are exported once with "Documento commerciale" (M5 D9).
- **Main charge** lines = snapshot lines (unit, skipper, extras), `issued_on` = the day it is created. It is created
  lazily at the first quick cash, at check-out, or at no-show, never for `held`.
- Reprice (reschedule, extend) after the main charge exists: unexported and unpaid main charges are cancelled and
  re-created. Otherwise `delta = new snapshot total − Σ live main + adjustments` becomes an adjustment charge
  (`corrects_charge_id` = main, `correction_kind = delta`, linked as `adjustment`). A negative delta follows M5
  §3.5 (credit). Exported charges never change (INV-51).
- Deposits never appear as charge lines. The deposit payment is matched only to its `deposit_retention` charge
  (INV-98, query B60 in §15).

### 9.2 Deposit model on the booking

`deposit_mode` CHECK (`cash`,`card_pos`,`preauth`,`waived`), `deposit_cents`, `deposit_status` CHECK
(`none`,`pending`,`held`,`authorized`,`capture_pending`,`released`,`retained`,`partially_retained`,`waived`),
`deposit_payment_id?`, `deposit_intent_id?`, `deposit_retained_cents`, `deposit_waive_reason`. Pre-auth needs an
enabled provider implementing `ext.PaymentAuthorizer`, otherwise `422 preauth_unavailable` (the manual driver never
offers it). Intent purpose `rental_deposit` (CHECK widened). The collect hooks `OnRentalDepositAuthorizedTx`
(`pending → authorized`), `OnRentalDepositCapturedTx` and `OnRentalDepositVoidedTx` run in the webhook
transaction (INV-56 dedupe unchanged).

### 9.3 Quick cash `POST M/rental/bookings/{id}/quick-cash` (`rental.manage` + `payments.collect`, or `payments.collect_cash` for cash; `Idempotency-Key`)

Body `{target: main|return|open (default open), method: cash|card_pos|link, register_id?}`. One tx: ensure the
target charge(s) exist (main from the snapshot; return only after check-in), compute the residual (`409
nothing_to_collect` when 0), then:
- `cash`/`card_pos`: `payment.RecordTx(kind payment, matches = residuals)`. Cash uses `cash.SessionForPaymentTx`.
  Receipt-mode charges get their fiscal document through the existing hook (`fiscal.RecordTx`, driver `none`,
  `export` or `bridge`). The response has `{charges, payment, fiscal_documents, receipt_url}`.
- `link`: `collect.ChargeLinkTx(charge_ids)` (new, HANDOVER, without email) returns `{url, expires_at}` for a QR.
  The provider webhook confirms through the existing path, and the fiscal document is issued there. A second link
  for the same charges returns the open one.

The desktop booking page has the same dialog (`features/rental-handover/public.ts`).

## 10. PDFs (HANDOVER, ADR 0005 machinery)

- `rental_contract` (built-in it/en): marina, renter (decrypted at render time, inside the encrypted
  `generated_documents.data_enc`), unit, formula, range, price lines, deposit, rules, signature box. It is composed at
  confirmation (`subject_type = rental_booking`) and signed at check-out. Driver `screen` is new: CHECK widening, plus
  `signature/screen.go` stamping the PNG (≤ 200 KiB, `image/png` sniffed) and an evidence page.
- `rental_handover` (built-in it/en), one document per phase: checklist with outcomes, fuel, engine hours, photo
  thumbnails (≤ 6, downscaled), damages with estimates, late fee, deposit decision, actual times. It is composed
  after commit and rendered by the docs job (never on the critical path).
  `GET M/rental/bookings/{id}/handovers/{phase}/pdf` serves it through signed links (INV-74).
- `docs/types.go`: `rental_contract`/`rental_handover` `AvailableFrom` stays "M11", plus the views
  `rental_contract_view.go` and `rental_handover_view.go`.

## 11. Reports and tiles (FLEET, M7 machinery, INV-61..64)

| Report `rental` view | Records | Totals |
|---|---|---|
| `utilisation` | unit × day: available slots (open window − block slots, 0 when retired or before creation), rented slots (`booked` rows of non-cancelled bookings inside the open window), utilisation bp | Σ available, Σ rented, aggregate bp = ratio of sums |
| `revenue` | rental-origin charge lines issued in the period: unit, category, formula, line kind, net, VAT, gross | Σ by unit / category / formula |
| `damages` | damages with booking, unit, estimate, status, retained share | Σ estimate, Σ retained |
| `deposits` | deposits taken (mode, amount), released, retained, outstanding | Σ per status |

Permission: `reports.rental` or `reports.all`. Money columns need the same (the `rental` role holds
`reports.rental`). Tiles become available: `rental_utilisation` (Σ rented / Σ available slots in the period; drill
`utilisation`) and `rental_revenue` (Σ net of rental-origin charges issued in the period; drill `revenue`;
`deposit_retention` excluded and shown in `damages`). Tile value = Σ drill records by construction, with
previous-year comparison as M7. CSV/Excel via `report/export`. `UPCOMING_REPORTS` and `UPCOMING_MODULES` lose
`rental`.

## 12. Offline (spec 29.2, ADR 0011, INV-71/83/84)

- Kind **`rental.checklist`** (enabled): `saveHandoverChecklist` (PUT, `client_ref`, device time ≤ 48 h before and
  ≤ 5 min after queueing) and `uploadHandoverPhoto` (depends on the checklist op), flagged `x-porto-offline: true`.
  They apply only to bookings in `confirmed` (phase checkout) or `checked_out` (phase checkin). They never change
  status, slots, charges or deposits (INV-71). Replays have one effect (unique `(marina_id, client_ref)`, INV-83).
  A later replay never overwrites a completed handover (`409 handover_locked`, shown as a conflict).
- Allowlist slot `rental` (today and tomorrow): `getRentalDay`, `getRentalBooking` of those bookings,
  `getRentalSettings` (checklist template), `getHandover`.
- **Online only** (the UI disables them offline with "Serve la connessione"): create, reschedule, confirm or cancel
  bookings; identity; signature; deposit; check-out and check-in commits; damages; quick cash (receipt mode never
  goes offline; existing `payment.cash` stays limited to invoice-mode charges, INV-87). The commit buttons wait for
  the queue to flush.

## 13. API (CONTRACT writes all; `M` = `/api/admin/v1/marinas/{marinaId}`; tags `rental-fleet`, `rental-bookings`, `rental-handover`)

### 13.1 Operations

| Area | operationId (path) | Permission |
|---|---|---|
| Units | `listRentalUnits` `createRentalUnit` `getRentalUnit` `updateRentalUnit` (`M/rental/units`) | `rental.manage` |
| Skippers | `listRentalSkippers` `createRentalSkipper` `updateRentalSkipper` `putRentalSkipperHours` (`M/rental/skippers`) | `rental.manage` |
| Config | `listRentalFormulas` `createRentalFormula` `updateRentalFormula`; `listRentalExtras` `createRentalExtra` `updateRentalExtra`; `getRentalSettings` `putRentalSettings` | read `rental.manage`; write `marina.configure` |
| Quote | `quoteRental` (`POST M/rental/quote`, stateless: unit, range, formula? (omitted = every valid formula sorted by total), skipper?, extras) | `rental.manage` |
| Planner | `getRentalPlanner` (`from`, `to`, `view` = hours\|half_days\|days, `category?`): unit and skipper rows with segments `{booking_id, block_id, status, kind, from, to}` | `rental.manage` |
| Availability | `searchRentalAvailability` (range, formula, category?, skipper?) | `rental.manage` |
| Bookings | `listRentalBookings` `createRentalBooking` (`Idempotency-Key`) `getRentalBooking` `updateRentalBooking` (If-Match) `transitionRentalBooking` (`{action: confirm\|cancel\|no_show\|extend_hold, reason?, until?}`, If-Match) | `rental.manage` |
| Blocks | `listRentalBlocks` `createRentalBlock` `updateRentalBlock` `deleteRentalBlock` | `rental.manage` |
| Day | `getRentalDay` (`date`: pickups, out, returns due, late) | `rental.manage` |
| Handover | `getHandover` `saveHandoverChecklist` (offline) `uploadHandoverPhoto` (offline) `putHandoverIdentity` `signRentalContract` `startRentalDeposit` `getRentalDeposit` `checkOutRental` `createRentalDamage` `updateRentalDamage` `deleteRentalDamage` `previewRentalCheckIn` `checkInRental` `closeRental` `getRentalHandoverPdf` `getRentalContractPdf` | `rental.manage` |
| Quick cash | `quickCashRental` | `rental.manage` + `payments.collect`/`collect_cash` |
| Reports | existing `getReport` key `rental` + `view`; dashboard tiles | §11 |

Conventions: If-Match on versioned resources, `Idempotency-Key` on creating POSTs (and on check-out, check-in and
quick cash), cursor pagination. Schemas use `_cents`, `_milli`, `{date, slot}`. Problem slugs: `rental_slot_taken`
(`{target, date, slots, booking_ids, block_ids}`), `skipper_busy`, `rental_hold_expired`, `invalid_transition`,
`formula_range_mismatch`, `outside_rental_hours`, `unit_not_bookable`, `skipper_required`,
`skipper_unavailable`, `skipper_licence_expired`, `licence_required`, `licence_expired`, `handover_incomplete`,
`handover_locked`, `engine_hours_decreasing`, `preauth_unavailable`, `deposit_exceeds`, `nothing_to_collect`,
`band_gap_lt_buffer`, plus the existing `no_price`, `customer_required`, `cash_session_required`,
`version_conflict`, `offline_time_out_of_range`.

### 13.2 Events and SSE

Internal: `rental.changed` `{booking_id, unit_id, skipper_id?, status, change, starts_on, ends_on}`. Public
(webhook catalogue): `rental.confirmed`, `rental.cancelled`, `rental.checked_out`, `rental.returned`, same data.
SSE topic `rental.changed`. No renter or skipper data, no amounts other than ids (INV-70). Meta capability
`rental`. Jobs: `rental.holds_expire` (1 min, BOOKING), `rental.deposits` (1 min, HANDOVER),
`rental.identity_retention` (24 h, HANDOVER: wipes `identity_enc` and the identity/licence photos after
`closed_at + identity_retention_days`, audited).

## 14. UI

### 14.1 FLEET (`web/admin/src/features/rental-fleet/**`)

**Flotta** `/m/$id/rental/fleet`: units table (category, seats, licence, status badge, home berth), unit form
(vessel picker from the registry, item, deposit, licence mode with the computed kW shown), skippers (calendar
editor of weekly hours), formulas (kind-dependent fields), extras, settings (open window, bands with a visual
strip, buffer, checklist editor). A price-coverage hint per formula × unit uses `quoteRental`. Report pages and the
two tiles live in `features/reports`/`features/dashboard` rental additions.

### 14.2 BOOKING (`web/shared/src/planner/**`, `web/admin/src/features/rental-planner/**`)

- `web/shared/src/planner`: an SVG time grid (rows × slot columns), zoom levels `hours` (one day, 30-min columns
  over the open window ± 2 h), `half_days` (7 days × bands), `days` (14/31 days). Pointer drag to create (ghost,
  snaps to slots or bands or days) and drag to move (row change = unit change). Keyboard alternative (arrows to
  move the selection, Enter to open the dialog). Segments are coloured by state with token classes: held
  `bg-chart-4/40 border-dashed`, confirmed `bg-chart-1`, checked_out `bg-chart-2`, returned `bg-chart-3`, closed
  `bg-muted`, no_show `bg-destructive/30`, buffer `bg-muted/60`, block `bg-secondary`. Accessible names on segments.
- **Planner noleggi** `/m/$id/rental/planner`: units grouped by category, a skippers section, a filter, live updates
  through SSE `rental.changed`. Releasing a drag opens the booking dialog. The formula is suggested by the range
  (exact band means half_day, open window means day, several dates mean multi_day or week, otherwise hourly), with
  the `quoteRental` alternatives list, renter (customer search or name/phone/email), skipper, extras, invoice
  flag, and buttons **Opzione** (hold) / **Conferma**. A 409 shows the conflicting segment highlighted.
- **Noleggi** list `/m/$id/rental/bookings` and **booking page** `/m/$id/rental/bookings/$bookingId` (summary,
  actions, price lines, documents). The handover, deposit, damages and quick-cash panels are imported from
  `features/rental-handover/public.ts`.

### 14.3 HANDOVER (`web/admin/src/features/phone/rental/**`, `web/admin/src/features/rental-handover/**`)

- **Phone** `/m/$id/rental-phone` (`PhoneShell`, landing row for `rental.manage` without `presence.write`, before
  the maintenance-phone row). Tabs: **Oggi** (Partenze, In mare, Rientri, late badge), **Scan** (`PR1:` QR),
  **Nuovo** (walk-in: unit available now → formula → renter → confirm, online), **Cassa** (open rental charges).
  Wizards per §7 and §8 with a step header, large touch targets (≥ 44 px), camera through
  `<input type="file" accept="image/*" capture="environment">`, a fuel `Slider` in eighths, a numeric keypad for
  hours, a signature canvas, a deposit QR (existing `qrsvg`), and offline queue status.
- Desktop panels exported through `public.ts` (handover summary, damages, deposit, quick-cash dialog) for the
  booking page.

## 15. Data (CONTRACT; both dialects, same names; `store.BackupTables()`)

| Migration | Tables / changes | Key constraints |
|---|---|---|
| `00023_rental_fleet` | `rental_settings`, `rental_units`, `rental_skippers`, `rental_skipper_hours`, `rental_formulas`, `rental_extras` | `rental_units` unique (marina_id, code), unique (marina_id, vessel_id), CHECK enums, CHECK `(ownership = 'third_party') = (owner_customer_id IS NOT NULL)`; skippers unique partial (marina_id, user_id); formulas unique (marina_id, code), CHECK `(kind = 'half_day') = (band_code IS NOT NULL)`, CHECK `(fuel = 'metered') = (fuel_item_id IS NOT NULL)`, CHECK step ∈ (15,30,60); composite FKs `(x_id, marina_id)` everywhere |
| `00024_rental_bookings` | `rental_bookings`, `rental_booking_extras`, `rental_blocks`, `rental_slots`, `rental_skipper_slots`, `rental_booking_charges`; CHECK edits (SQLite `writable_schema` procedure of 00016/00020, PostgreSQL drop/add): `number_sequences.series` + `rental` (numbers `NL<year>-NNNNNN`) | **PK (unit_id, date, slot)**, **PK (skipper_id, date, slot)**, CHECK slot 0..47, CHECK one-of booking/block, CHECK kind enums; bookings unique (marina_id, year, number), CHECK status enum, CHECK channel enum, CHECK `end > start` (date, slot), CHECK `(status = 'held') = (hold_expires_at IS NOT NULL)`, CHECK deposit enums; `rental_booking_charges` unique partial (booking_id, kind) WHERE kind IN ('main','return'), unique (charge_id); indexes (marina_id, date) on the slot tables, (booking_id) |
| `00025_rental_handovers` | `rental_handovers`, `rental_handover_files`, `rental_damages`; CHECK edits: `payment_intents.purpose` + `rental_deposit`, `signature_requests.subject_type` + `rental`, `signature_requests.driver` + `screen`, `generated_documents.subject_type` + `rental_booking` | handovers unique (booking_id, phase), unique partial (marina_id, client_ref); damages CHECK estimate ≥ 0, status enum |

Store invariant queries (`store.CheckRentalInvariants`, run by the integration suites): **Q32** a booking's unit rows
= `slots.Expand(range, buffer)` (truncated on early return), its skipper rows = range, none for `cancelled`/`no_show`,
`hold` rows only for `held` bookings; **Q33** at most one live main and one return charge per booking, and
Σ rental-origin charges of a closed booking = snapshot total + Σ adjustments + return total; **B60** every match of a
deposit payment points to a `deposit_retention` charge, `deposit_retained_cents` = that charge's matched amount,
and retained ≤ deposit.

Invariants (CONTRACT writes them in Italian in `docs/invariants.md`, status `planned`; owners flip them):
- **INV-18 — Nessuna doppia prenotazione di un'unità a noleggio né di uno skipper.** Verified by the PKs of
  `rental_slots`/`rental_skipper_slots`, `TestConcurrency_RentalUnitSlots`, `TestConcurrency_RentalSkipperSlots`,
  `TestAcceptance_M11_NoDoubleBooking`, `TestMigrations_M11Constraints`. BOOKING.
- **INV-19 — Le righe di un noleggio coincidono con il suo intervallo.** Q32, `TestRapid_RentalSlotsExpand`. BOOKING.
- **INV-97 — Un solo punto di cambio di stato dei noleggi.** Only `reservation` writes status, slots and hold
  expiry, with audit + outbox. Verified by `TestRentalStates_Table` and a grep test
  (`TestRental_StatusWritesOnlyInReservation` on `internal/store` update call sites). BOOKING.
- **INV-98 — La cauzione non è un ricavo finché non viene trattenuta.** B60, `TestRental_DepositNeverRevenue`
  (corrispettivi, R8 revenue and `rental_revenue` exclude deposits), `TestRapid_DepositSettlement`. HANDOVER.
- **INV-99 — Un solo addebito principale e uno di riconsegna per noleggio.** Unique index, Q33,
  `TestConcurrency_RentalCheckOutOnce`, `TestConcurrency_RentalQuickCashOnce`. HANDOVER.
- INV-05 extended to rental holds (`TestReservation_ExpiredHoldIsReclaimed`, BOOKING). INV-20 extended to rental
  snapshots (FLEET). INV-70/71/83 extended to the rental events and `rental.checklist` (HANDOVER).

## 16. Ownership, order and acceptance

### 16.1 CONTRACT (runs first, alone; then frozen)

- `api/openapi.yaml`: every operation and schema of §13 (offline flags on the two operations, `x-porto-idempotency`
  where domain-level) + `make gen`.
- Migrations 00023–00025 in both dialects. `internal/store/{rental_fleet_models,rental_booking_models,rental_handover_models}.go`
  and the backup registration.
- Go skeletons with signatures and `ErrNotImplemented` bodies:
  - `pricing/rental.go`: types, `RentalQuote` signature, new `TimeUnit` constants, `Scope.TimeBands` field.
  - `rental/price/{doc,types,api}.go`, `rental/slots/{doc,types,api}.go`, `rental/states/states.go` (table
    filled: pure data).
  - `rental/fleet/{types,service,errors}.go`, `rental/reservation/{types,service,errors,tx}.go`. `tx.go` holds
    `CheckOutTx`, `ReturnTx`, `CloseTx`, `RepriceTx` and `CancelTx` (signatures).
  - `rental/handover/{types,service,errors,hooks}.go` (implements `collect.RentalDepositHooks` and
    `signature.Subject`).
  - `collect.RentalDepositHooks` interface + call sites in `intents.go` (`authorizedTx`, `succeededTx`, `voidedTx`
    dispatch on purpose `rental_deposit`).
  - `payment/rental_deposit.go` signatures (`RetainDepositTx`, with fiscal mode), `collect/rental_link.go`
    signature (`ChargeLinkTx`), `signature/screen.go` signature + driver constant.
- Event constants, webhook catalogue, SSE topic `rental.changed`; problem slugs + `errmap.go`;
  `internal/http/admin/admin.go` fields + 501 stubs `rental_fleet.go`, `rental_bookings.go`, `rental_handover.go`.
- `app.go` wiring (hooks, subject registration, jobs `rental.holds_expire`, `rental.deposits`,
  `rental.identity_retention`); seed stubs `seed_rental_fleet.go`, `seed_rental_bookings.go`,
  `seed_rental_handovers.go`; meta capability; `.golangci.yml` pure rules; offline middleware set consistent with
  the openapi spec.
- Report seams: report key `rental` routed to a stub, tiles still `available=false` until FLEET.
- Web: `queryKeys.ts`, hook stubs `web/shared/src/api/hooks/{rentalfleet,rentalbookings,rentalhandover}.ts`,
  `OfflineOpKind` `rental.checklist` + `OFFLINE_OPERATION_KINDS` entries, i18n skeletons `rentalFleet.json`,
  `rentalBookings.json`, `rentalHandover.json` (it, en) + `resources.ts`, router factories `rentalFleetRoutes`,
  `rentalPlannerRoutes`, phone route `rental-phone`, `nav.ts` group "Noleggio", `landing.ts` row, shadcn
  components via the CLI if missing (`slider`, `toggle-group`, `progress`).
- Docs: ADR `docs/adr/0016-noleggio-m11.md` (Italian, "proposta": §4, §5.1–5.4, §6, §9, §12, §1.2 online deferral,
  §18), invariants §15, `docs/architecture.md` §2/§10 rows.
- Ends with `make gen && make check` green.

### 16.2 Builders (parallel, exclusive files)

| Builder | Owns |
|---|---|
| **FLEET** | `internal/domain/pricing/rental.go` + `rental_test.go` (and the rental cases in `pricing/validate.go`: only the time-unit and `time_bands` branches); `internal/domain/rental/price/**`; `internal/domain/rental/fleet/**`; `internal/store/{rental_fleet.go,report_rental.go}`; `internal/domain/report/**` rental views and tiles (`calc/rental.go`, tile wiring); `internal/http/admin/rental_fleet.go` + tests; `internal/app/seed_rental_fleet.go` (5 units: 2 rib, open, sail, sup; 2 skippers; formulas `ORARIA`, `MEZZA-MATTINA`, `MEZZA-POMERIGGIO`, `GIORNATA`, `PIU-GIORNI`, `SETTIMANA`; price list `NOL-2027` of §5.3; extras; fuel and late items); `web/admin/src/features/rental-fleet/**`, rental additions in `features/reports/**` and `features/dashboard/**`; `web/shared/src/api/hooks/rentalfleet.ts`; i18n values `rentalFleet` (+ rental keys in `reports.json`, `dashboard.json`); `internal/acceptance/m11_reports_test.go`; e2e `rental-fleet.spec.ts`; INV-20 extension |
| **BOOKING** | `internal/domain/rental/slots/**`, `rental/states/*_test.go`, `internal/domain/rental/reservation/**`; `internal/store/{rental_bookings.go,rental_slots.go,rental_blocks.go,rental_invariants.go}`; `internal/http/admin/rental_bookings.go` + tests; notify template `rental_confirmed` (it, en); `internal/app/seed_rental_bookings.go` (bookings in every state over two weeks, one expired hold, blocks); `web/shared/src/planner/**`; `web/admin/src/features/rental-planner/**`; `web/shared/src/api/hooks/rentalbookings.ts`; i18n values `rentalBookings`; `internal/acceptance/m11_booking_test.go` (NoDoubleBooking, holds, isolation); e2e `rental-planner.spec.ts`; INV-18, 19, 97, INV-05 extension |
| **HANDOVER** | `internal/domain/rental/handover/**`; `internal/domain/payment/rental_deposit.go` (+ test); `internal/domain/collect/rental_link.go` (+ test); `internal/domain/signature/screen.go` (+ test); `internal/domain/docs/{rental_contract_view.go,rental_handover_view.go}` + built-in templates `rental_contract`, `rental_handover` (it, en) + goldens; `internal/store/{rental_handovers.go,rental_damages.go,rental_charges.go}`; `internal/http/admin/rental_handover.go` + tests; `internal/app/seed_rental_handovers.go`; `web/admin/src/features/phone/rental/**`, `web/admin/src/features/rental-handover/**`; `web/shared/src/api/hooks/rentalhandover.ts`; `web/shared/src/offline/allowlist.ts` (rental slot) + tests; i18n values `rentalHandover` and phone keys; `e2e/helpers/human.ts`; `internal/acceptance/m11_handover_test.go`, `m11_offline_test.go`; e2e `rental-phone-checkout.spec.ts`, `rental-phone-checkin.spec.ts`, `rental-preauth.spec.ts`; INV-98, 99, INV-70/71/83 extensions |

### 16.3 Parallel-phase rules

- OpenAPI, migrations, models, the states table, event constants, hook interfaces and signatures are frozen.
  Changes go in the builder's report to the orchestrator, never as edits.
- BOOKING implements `reservation/tx.go` (`CheckOutTx`, `ReturnTx`, `CloseTx`, `RepriceTx`, `CancelTx`) **first** and
  tells the orchestrator. Until then HANDOVER unit-tests against a fake implementing the frozen interface
  `handover.Reservations`.
- HANDOVER and BOOKING read units, formulas and settings through FLEET's store read helpers, whose signatures are
  frozen in `rental_fleet_models.go` (CONTRACT writes `GetRentalUnit`, `GetRentalFormula`, `LoadRentalSettings`
  bodies, which are simple selects).
- Nobody edits `occupancy`, `contract`, `charge`, `cash`, `fiscal`, the rest of `payment`/`collect`/`signature`,
  `lifting` or `booking`. Missing hooks are reported.
- Web builders work against the generated client with MSW fixtures until the backend lands.
- Each builder ends with its packages' tests (both dialects for integration), `golangci-lint` clean on its files,
  `npm --prefix web run lint && npm --prefix web run typecheck`, and the `invariants-guardian` subagent.

### 16.4 Tests

**Pure (FLEET, BOOKING)**
- `TestRentalQuote_Table`: E1–E10 exactly, plus customer category and channel scope, specificity with
  `time_bands`, period change inside multi_day, week remainder cap, skipper line, extras per day. Also
  `TestRentalQuote_Recompute` and `TestRentalQuote_Purity`. The existing pricing corpus is unchanged
  (`EngineVersion` the same, JSON of existing catalogues byte-identical).
- `TestLateFee_Table` (§5.4 rows incl. the DST row), `TestFuel_Table`, `TestDepositProposal_Table`, and
  `TestRapid_DepositSettlement` (random deposit, estimates and decisions: retained + released = deposit, retained ≤
  deposit, excess = max(0, Σ estimates − retained)).
- `rental/slots`: `TestSlots_Clock`, `TestValidateRange_Table` (every kind, edges, band exact match, open-window
  limits, DST labels excluded from windows). `TestRapid_RentalSlotsExpand` checks: count = range slots + buffer;
  ordered and contiguous across midnight; 48 labels per date on DST dates; two ranges with gap ≥ buffer give
  disjoint rows, and with gap < buffer they overlap. `TestRentalStates_Table` covers every row and every refused
  pair.

**Integration** (`//go:build integration`, SQLite + PostgreSQL via `testdb`):
- `TestMigrations_M11Constraints`: every PK/unique/CHECK, CHECK edits, Up/Down/Up, parity.
- `reservation`: create/hold/confirm/reschedule/extend/cancel/no-show; `TestReservation_ExpiredHoldIsReclaimed`;
  expiry job; blocks vs bookings; unit status; skipper hours/licence; Q32 after each scenario.
- `handover`: check-out preconditions (each missing step yields `handover_incomplete`); signature `screen` (signed
  document, evidence page, request signed); deposits cash/card_pos/preauth with the fake provider (authorized →
  capture partial → succeeded webhook → retention → close; void path; job retry); check-in charges §8.2;
  damages; quick cash cash/card_pos/link (fiscal document only for receipt mode; invoice mode in export);
  `TestRental_DepositNeverRevenue`; reprice after export → adjustment (exported charge unchanged); identity
  retention job; offline replay twice → one effect, `handover_locked` after completion.
- Reports on seed (tile = Σ drill), backup round trip, a cross-marina sweep for every new route, and
  `TestEvents_DataHasNoPII`/`TestLogs_NoPII` covering the rental events.

**Concurrency** (both dialects, `-race`, ≥ 100 goroutines):
- `TestConcurrency_RentalUnitSlots` (BOOKING): 100 goroutines book random overlapping ranges (hourly, half-day,
  multi-day) on one unit, a third with one of two skippers, plus 20 reschedules/cancels and 10 blocks. Winners'
  unit rows (with buffer) are disjoint, row count = Σ winner expansions, losers get 409 with nothing written, Q32
  is empty.
- `TestConcurrency_RentalSkipperSlots` (BOOKING): 100 goroutines book the same window on 100 different units with
  the same skipper. Exactly one wins, and the other 99 get `skipper_busy` with no unit rows written.
- `TestConcurrency_RentalHoldReclaim` (BOOKING): an expired hold sits on the window (`clock.Fake` advanced), then
  100 goroutines confirm-create on it. One wins, the expired booking's rows are gone, and the job then cancels it
  once.
- `TestConcurrency_RentalCheckOutOnce` (HANDOVER): 100 check-outs of one booking give one transition, one main
  charge and one deposit payment.
- `TestConcurrency_RentalQuickCashOnce` (HANDOVER): 100 quick-cash calls with distinct keys on one booking give one
  main charge, Σ matched ≤ total and losers `nothing_to_collect`/`match_exceeds`.

**Acceptance** (`internal/acceptance/m11_*_test.go`, API only, `clock.Fake`, both dialects):
- `TestAcceptance_M11_NoDoubleBooking` (criterion 1): seed unit and formula via the API, then **100 parallel HTTP
  `createRentalBooking`** calls with the same unit and overlapping ranges (half with distinct non-overlapping
  ranges as a control). Exactly one 201 per conflicting group, the rest `409 rental_slot_taken`, `getRentalPlanner`
  shows no overlap, Q32 empty. The same is repeated with 100 parallel calls for one skipper on different units.
  Run with `-race` on both dialects.
- `TestAcceptance_M11_CheckOutCheckIn` (criterion 2, server side): the §8.2 flow with the exact numbers, ≤ 6 write
  requests for check-out and ≤ 5 for check-in, each request < 1 s on SQLite.
- `TestAcceptance_M11_Preauth` (fake provider: authorize, partial capture webhook, close).
- `TestAcceptance_M11_Offline` (queued checklist/photo replayed twice: one effect; commits refused offline-flagged).
- `TestAcceptance_M11_Reports` (tiles = Σ drill on the seed).

**Web**: Vitest covers the planner grid (slot ↔ x, drag create/move reducer, keyboard, zoom levels, colour classes
per state), booking dialog formula suggestion, fleet forms (band strip validation), the check-out and check-in
wizard reducers (step gating = server preconditions), the signature canvas export, the deposit state UI, quick cash,
and `allowlist.test.ts` (`rental.checklist` enabled, no reserved kinds left).

Playwright, phone viewport `{width: 390, height: 844}`, `isMobile`, `hasTouch`, chromium:
- `rental-phone-checkout.spec.ts` and `rental-phone-checkin.spec.ts` (criterion 2): one scripted flow each on the
  seed booking, timed end to end. `e2e/helpers/human.ts` wraps every interaction and adds **accounted human time**:
  tap 1.2 s, typed field 1.0 s + 0.3 s/char, select 2.0 s, photo 4.0 s (`setInputFiles` with a fixture JPEG), signature
  6.0 s (pointer path on the canvas), screen read 2.0 s per wizard step. The test asserts **measured wall time +
  accounted time < 180 s** for each flow, and `taps ≤ 22, typed fields ≤ 5, photos ≤ 4, signatures = 1` (check-out)
  and `taps ≤ 18, typed fields ≤ 1, photos ≤ 1` (check-in, no damage). The breakdown is printed to the report.
  `PORTO_E2E_REAL_HUMAN=1` really waits the delays, for a manual run.
- `rental-planner.spec.ts` (drag create hold, confirm, drag move, 409 on overlap), `rental-fleet.spec.ts`,
  `rental-preauth.spec.ts` (QR shown, signed fake webhook, status updates through SSE), and `a11y.spec.ts`
  additions per owner.

### 16.5 Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
go test -count=1 ./internal/domain/pricing/... ./internal/domain/rental/... ./internal/domain/report/...
go test -count=1 ./internal/domain/rental/slots/... ./internal/domain/rental/price/... -run 'TestRapid_' -rapid.checks=2000
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/store/... ./internal/domain/rental/... ./internal/domain/payment/... ./internal/domain/collect/... ./internal/domain/signature/... ./internal/domain/docs/... ./internal/domain/report/... ./internal/http/... ./internal/backup/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -timeout 30m ./internal/acceptance/... ./internal/domain/... -run '^TestConcurrency_Rental(UnitSlots|SkipperSlots|HoldReclaim|CheckOutOnce|QuickCashOnce)$'
scripts/with-postgres.sh go test -race -tags=integration -count=1 -timeout 20m ./internal/acceptance/... -run '^TestAcceptance_M11_NoDoubleBooking$'
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M11|TestAcceptance_M10|TestAcceptance_M8|TestAcceptance_M7|TestAcceptance_M5|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII|TestSSE_PayloadHasNoPII'
make test-db && make test-race
npm --prefix web run test
make e2e            # rental-fleet, rental-planner, rental-phone-checkout, rental-phone-checkin, rental-preauth, a11y, offline-queue
make licenses && scripts/agents/check-naming.sh
```

Manual check (final report): `make seed && make dev`; create a hold by dragging on the planner, confirm it,
try an overlapping drag (409). On a 390 px phone: check-out with a licence, screen signature and cash deposit; go
into airplane mode, fill the check-in checklist, reconnect; finish the check-in with a late return and a damage,
partially retain the deposit, quick-cash the balance by card, and open both handover PDFs. Then check the
corrispettivi and the rental report.

## 17. Order of work inside each builder

- FLEET: pricing rental + tests → `price` → fleet services/store → seed → handlers → reports/tiles → web.
- BOOKING: `slots` + rapid → `states` tests → `reservation/tx.go` (signal) → allocate/holds/blocks/jobs →
  concurrency tests → handlers → seed → shared planner → planner UI → acceptance.
- HANDOVER: store → identity/checklist/offline → signature screen → deposits (cash, card POS, preauth hooks) →
  check-out → damages, check-in, return charge → quick cash/link → PDFs → phone UI → e2e timing → acceptance.

## 18. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Packages under `internal/domain/rental/` (spec 04): `fleet`, `reservation`, `handover`, pure `price`, `slots`, `states`. `reservation` avoids clashing with the existing `booking` (online mooring) package. |
| D2 | Slots are 30-minute wall-clock labels 0..47 of a local date. The open window and bands exclude 02:00–03:00, so only multi-day/week rentals cross DST, priced per date. Late fees use real elapsed minutes. |
| D3 | "Articoli di tipo noleggio" = items with revenue class `rental` (12.1 lists `noleggio` among revenue classes, not item types). No `item_type` CHECK change. |
| D4 | Rental prices come only from price lists through pure `pricing.RentalQuote`: new time units `hour`, `half_day`, `day` (+ `week`), scope `time_bands`, rate periods per priced date, customer category and channel in selection. No modifiers, discounts or total rounding in M11. `EngineVersion` unchanged; `RentalEngineVersion = 1` in snapshots. |
| D5 | Week pricing = whole weeks + remainder days capped at one week price. Hourly = hour rate × slots/2, half-even. No automatic cap of hourly by half-day/day: the quote endpoint lists every valid formula sorted by total and the planner preselects the cheapest. |
| D6 | Half day occupies exactly its band. Day = the open window. Multi-day/week occupy every slot from pickup to return, overnight included. |
| D7 | Licence need: `never`/`always`/`threshold` on Σ engine kW (default 30 kW); never required when a skipper is booked. |
| D8 | Buffer rows (`kind buffer`) follow each rental on the unit only, carry over midnight, and are part of the DB guarantee. Bands must be ≥ buffer apart. |
| D9 | Skippers get their own slot table `rental_skipper_slots` (same guarantee), without buffer. Days off are hard blocks; weekly hours are a soft rule with an audited override. |
| D10 | Holds: desk "Opzione" TTL `desk_hold_minutes` (60); lazy reclaim deletes every row of an expired held booking at conflict time; the job cancels it with `hold_expired`. Online rental sale is deferred (§1.2) with the TTL/channel seam ready. |
| D11 | Early return releases the remaining slots (after buffer) with no refund; late return keeps the rows unchanged and charges the late fee (grace + step from the formula, rate from a late item in the price list). |
| D12 | Rows of returned/closed bookings stay as history; utilisation is computed from `booked` rows inside the open window minus blocks. |
| D13 | Renter identity is typed into the handover (`identity_enc`, KeyRing) with photos in handover files, not in the customer registry, because the `rental` role only reads customers (26.1). Purged after `identity_retention_days` (365) from closing. Linking/creating a customer needs `customers.write`. |
| D14 | On-screen signature is a new `signature` driver `screen` (simple electronic signature, evidence page like OTP; D6 wording unchanged). OTP and paper remain selectable. The contract PDF is composed at confirmation so check-out only stamps it. |
| D15 | Deposit modes: `cash` and `card_pos` are `kind deposit` payments; `preauth` is a manual-capture intent (purpose `rental_deposit`) through `ext.PaymentAuthorizer`, captured partially for retention or voided; `waived` needs a reason. The manual driver never offers preauth. |
| D16 | Retention and release at check-in are allowed to `rental.manage` (spec 19.4 puts them in the return flow) through `payment.RetainDepositTx`/`RefundTx` called by the rental service; the generic `retainDeposit` endpoint still needs `charges.manage`. |
| D17 | Retention proposal = min(deposit, Σ damage estimates). The excess is a return-charge line on `damage_item_id`. Late fee, fuel and extras are collected separately (quick cash), not silently taken from the deposit. |
| D18 | Charges: one main charge (snapshot, created lazily at quick cash, check-out or no-show) and one return charge per booking (DB unique); reprice = cancel/recreate when untouched, otherwise a linked delta adjustment. Receipt mode by default, invoice mode on request (needs a customer). The retention charge follows the booking's fiscal mode. |
| D19 | Quick cash is a single endpoint with targets `main`/`return`/`open` and methods cash, card POS, payment link (QR, no email); the fiscal document comes from the existing receipt-mode hook. |
| D20 | Formulas, extras and settings are configuration (`marina.configure`); units, skippers, blocks and bookings are operations (`rental.manage`). Reports: `reports.rental` or `reports.all`. |
| D21 | Offline: only `rental.checklist` (checklist, fuel, engine hours, photos) for confirmed or checked-out rentals; every commit, deposit, signature, identity and receipt-mode cash is online. |
| D22 | Cancellation keeps no fee in M11: a cancellable main charge is cancelled; a paid one stays for the office (refund or credit). `no_show` is manual (the phone flags pickups late by `no_show_after_minutes`) and keeps the main charge. |
| D23 | Booking numbers `NL<year>-NNNNNN` (`number_sequences.series` + `rental`); QR `PR1:<booking_id>` on the contract PDF and booking card. |
| D24 | Events carry ids, enums and dates only; public events `rental.confirmed`, `rental.cancelled`, `rental.checked_out`, `rental.returned`; internal `rental.changed`; SSE topic `rental.changed`. |
| D25 | Phone timing is proven by Playwright with accounted human time per interaction (tap 1.2 s, field 1.0 s + 0.3 s/char, select 2 s, photo 4 s, signature 6 s, screen 2 s) plus measured wall time < 180 s, and hard caps on taps, typed fields and photos. |
| D26 | `third_party` units store the owner only; revenue sharing with owners is deferred. |
| D27 | A shared SVG planner component lives in `web/shared/src/planner` (spec 03); the yard planner is not migrated to it in M11. |
