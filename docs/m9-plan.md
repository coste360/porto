# M9 — Rimessaggio e manutenzioni: work order

Status: Plan phase output, authoritative for the M9 builders. If it conflicts with `docs/spec/`, the spec wins:
stop and report to the orchestrator. No open decision of `docs/spec/33-decisioni-aperte.md` is touched (D12
second-row mooring stays out). §16 lists every detail decided here where the spec is silent; CONTRACT records the
binding ones in ADR 0014.

Sources: spec 17 (all), 20 (all), 07.4–7.7 (surfaces, `blocked_cells`, `exits`, `supports`, `max_weight_kg`,
pedestals, land resources), 10 (occupancy, fit, kinds, moves 10.5), 12.3 (`metered`), 13.1 (contract kinds), 16
(charges, adjustments), 21 (reports), 26.1 (permissions), 27 (`/storage/scenarios`, `/maintenance/*`,
`/meter-readings`, events `maintenance.due`), 29.2 (offline), 32 (optimizer tests, M9 acceptance).
`docs/invariants.md` INV-01..03, 06, 07, 10, 30, 51, 54, 61, 62, 70, 71, 73, 83. ADR 0001, 0002, 0003, 0008, 0009,
0011, 0012. Plans: `m1-plan.md` (surface grids, exits, pedestals), `m2-plan.md` (contract type `storage` → month,
`metered` basis), `m3-plan.md` (allocations, `LayoutTx`, D7: surfaces only with explicit placements, presences
`hauled_out`/`launched` reserved), `m5-plan.md` (`charge.CreateTx`, origin `utility` reserved, adjustments),
`m6b-plan.md` (offline kinds reserved, landing seams), `m7-plan.md` (report seams for storage, tile machinery).

## 0. Ground rules (every builder)

1. **Arenile is reference only** (ADR 0001). Arenile has no storage optimizer, no maintenance, no meters: there is
   nothing to read for those. Never copy files, code, comments or tests; never modify `../arenile`.
2. **No double occupancy, DB only** (INV-01, ADR 0003): storage placements are `allocations` + `cell_occupancies`
   rows written by `occupancy.AllocateTx`/`LayoutTx`; maintenance nights are `berth_occupancies`/
   `land_occupancies` rows of kind `maintenance`. Never "check then insert". Scenarios never write occupancy rows.
3. **PostgreSQL READ COMMITTED**: lock first (`store.LockAllocations` in id order, `FOR UPDATE` on the scenario /
   contract / meter row), then re-check state and versions under the lock. SQLite has one writer (gate).
4. **Pure packages**: `layout`, `fit`, `optimizer`, `maintenance/due`, `utility/consume` — no store, no clock, no
   `time.Now`, no `math/rand` (optimizer has its own seeded PRNG), no float. CONTRACT adds the three new paths
   to `pure-engines` and `pure-engines-no-rand` in `.golangci.yml`.
5. Units: money `money.Cents`, lengths `geo.Cm`, areas `geo.Cm2`, meter indexes and consumptions `qty.Milli`, dates
   `calendar.Date` (marina tz), night as occupancy unit (ADR 0002).
6. **INV-30**: storage contract state changes only through `internal/domain/contract` (audit + outbox in the tx).
   **INV-51**: exported charges are immutable; utility corrections are linked adjustment charges.
7. Every query scoped by `marina_id` from `p.Authorize`; cross-marina = 404. Permissions in domain services.
   No PII in logs, events, SSE (INV-70): ids and enums only (meter values are not PII but stay out of events).
8. API first: CONTRACT edits `api/openapi.yaml` and runs `make gen`; never edit generated files. Migrations
   `00018_storage.sql` and `00019_maintenance_utilities.sql`, both dialects, same names. Never touch 00001–00017.
9. UI: shadcn only (official CLI, exported from `@porto/shared`), Tailwind tokens, no custom CSS, i18n it
   (source) + en. Phone screens work from 360 px.
10. `cmd/porto` stays `CGO_ENABLED=0`; no new Go dependency (rapid already present). No new npm dependency
    (drag and drop uses the existing map pointer handling of `HarborMap`/`DragGhost`).
11. Do not `git commit`; no destructive git; no `rm -rf`; Docker only for your own throwaway containers
    (`scripts/with-postgres.sh`). Edit only files you own (§15). Iterate on touched packages only; full gates §15.4.

## 1. Scope

### 1.1 In M9

| Area | Content |
|---|---|
| Storage contracts (17.1) | `contracts.kind = storage`, terms annual / seasonal / monthly / period, on a surface area (`yard` or `shed` map) with x, y, rotation; support (`cradle`, `trailer`, `rack`), own or marina cradle (land resource `cradle` as extra line), planned haul-out and launch dates, services as items; priced with contract type `storage` and the area `price_class` (covered/uncovered). Activate, cancel, terminate, expire through the existing contract paths. Presence events `hauled_out`/`launched` offered for storage allocations. |
| Fit (10.2) | new blocking codes `support_not_allowed` (not overridable) and `weight_exceeds` (overridable), Go + TS parity. |
| Planner (17.2) | per surface area: plan (scale map at a date) + timeline (bar per boat), date slider, cell colouring, *Accessibilità* lens, layout session Save/Restore (existing `saveAreaLayout`), "Da collocare" tray, **Preset** = optimizer proposal into the draft without saving. |
| Scenarios (17.3) | `storage_scenarios`, `scenario_items`, `scenario_moves`; several per area with name/author; drag and drop on the copy; indicators; side-by-side compare (scenario vs scenario or vs real). |
| Accessibility (17.4) | pure `optimizer.Accessibility`; extraction order per launch date; warnings in planner and movement list. |
| Optimizer (17.5) | pure `optimizer.Arrange` (seeded, deterministic, iteration budget + caller deadline), `optimizer.PlaceOne` for new contracts. |
| Apply (17.5, 10.5) | `POST storage/scenarios/{id}/apply`: all moves in one tx via `occupancy.LayoutTx`, re-validated on the current state, one conflict cancels all and returns the full list; produces the movement worklist (order, equipment hint) readable on the phone. |
| Maintenance (20.1, 20.2) | `equipment` registry (lifting and other machines), plans with periodicity (months / cycles / fixed date), lead, assignee or supplier, checklist; job `maintenance.schedule`; tasks with priority, states, outcome, costs, photos/documents; phone reports with photo and map position; `maintenance` occupancy on berths and land resources with move proposals; out-of-service marking; *Manutenzione* map lens; deadlines integration. |
| Meters and consumption (20.4) | `utility_meters` (pedestal socket meters for power/water, hour/cycle counters for equipment), readings from the phone (photo optional), anomaly detection, consumption = Δ split by nights among occupants, charge with the `metered` item (M5 `utility` origin) with the next instalment run or at the end of a transit stay. |
| Offline (29.2) | enable the reserved kinds `utility.reading`, `maintenance.report`, `maintenance.outcome` (+ their photo uploads); allowlist slot `activities.assigned` filled with the maintenance worklist. |
| Reports (21) | `storage`, `maintenance`, `utilities` reports; tiles `storage_occupancy`, `storage_exits_blocked`, `maintenance_overdue`, `utility_unbilled`; occupancy report/tiles gain `map_kind` yard/shed drill. |
| Phone | `/m/$id/storage-phone` (yard_mgr: movements, accessibility warnings), `/m/$id/maintenance-phone` (worklist, report, outcome), dock tab "Letture" (readings). |

### 1.2 Out of M9

| Item | Milestone | Seam left |
|---|---|---|
| Yard work orders, quotes, planner, timesheets, formulas; haul-out/launch as work orders; `equipment_slots` | M10 | `equipment` table exists; movement `equipment_hint` is text; storage services are plain items |
| Rental | M11 | none |
| Smart meters via `ext.SensorSource`, sensors | M12 | `meter_readings.source` CHECK includes `sensor` (unused) |
| Online sale of storage | after M12 | `/b/{slug}` shows transits only |
| Free rotations on surfaces (not multiples of 90°) | after v1 (spec 7.5) | none |
| Storage renewal campaigns (M4 renewals are mooring) | after M9 | "Duplica per la prossima stagione" creates a new draft storage contract (copy) |
| Movement completion offline, scenario edits offline | never (29.2 forbids allocation changes offline; completion kept online, D18) | — |
| Maintenance occupancy on dynamic areas and structures as cells | after M9 | structure tasks may occupy the berths of the structure instead (D24) |

## 2. Architecture and packages

```
web/admin features/storage (PLANNER) ─┐   features/maintenance, features/utilities, phone/{maintenance,readings} (UTILITIES)
          │                          │                  │
   http/admin/storage*.go (STORAGE)   │       http/admin/{maintenance,utilities,equipment}.go (UTILITIES)
          ▼                          │                  ▼
 internal/domain/storage ── snapshot ─► optimizer (pure)      internal/domain/maintenance ─► maintenance/due (pure)
   │  contracts → contract.CreateStorageTx (INV-30)           internal/domain/utility ─► utility/consume (pure)
   │  apply → occupancy.LayoutTx (one tx)                       │  maintenance occupancy → occupancy.AllocateTx
   ▼                                                            │  charges → charge.CreateTx (origin utility)
 internal/store (storage*.go | maintenance.go utilities.go equipment.go | report_{storage,maintenance,utilities}.go)
 internal/domain/report (PLANNER: calc keys, tiles)
```

| Package | Kind | Owner |
|---|---|---|
| `internal/domain/optimizer` | pure: grid, distance field, accessibility, arrange, place-one, objective, validate, PRNG | STORAGE |
| `internal/domain/storage` | service: storage contracts (wraps `contract`), plan view, accessibility view, arrange preset, scenarios, apply, movements | STORAGE |
| `internal/domain/fit` (+ `web/shared/src/map/fit.ts`, vectors) | pure: support and weight codes | STORAGE |
| `internal/domain/maintenance` (+ `due/` pure) | equipment, plans, tasks, schedule job, maintenance occupancy, deadlines source | UTILITIES |
| `internal/domain/utility` (+ `consume/` pure) | meters, readings, anomalies, consumptions, utility charging job | UTILITIES |
| `internal/domain/report` | storage / maintenance / utilities reports and tiles | PLANNER |

Dependencies: `storage` → `contract`, `occupancy`, `optimizer`, `fit`, `layout`; `maintenance` → `occupancy`,
`deadline`; `utility` → `charge`, `contract` (read helpers only), `maintenance` (cycle counters read). `optimizer`
imports only `layout`, `fit`, `geo`, `calendar`, stdlib (depguard).

## 3. Storage contracts (normative, STORAGE)

### 3.1 Model

- `contracts` row with `kind = 'storage'`, number series `storage` (exists), `term` ∈ `annual`, `seasonal`,
  `monthly`, `period` (required in Go; the DB CHECK is mooring-only and stays, Q20 checks it). States and
  transitions = the mooring table (`draft ↔ hold`, `active`, `expired`, `terminated`, `cancelled`); CONTRACT adds
  `StorageTransitions` (same rows, kind storage) in `contract/states.go`. Renewal states are never entered.
- One resource line on a **surface** dynamic area (`resource_type = dynamic_area`, x, y, rotation 0/900/1800/2700)
  or a land resource `trailer_spot`. Linear areas and berths are refused (`storage_target_invalid`).
- Optional marina cradle: extra land-resource line (`land_resources.kind = 'cradle'`) for the whole period
  (reuses `ExtraResource`), occupying `land_occupancies`.
- Services (haul-out, launch, washing, engine winterisation) = service lines with items (M2), unchanged.
- New 1:1 table `storage_contract_details` (§13): `support` (`cradle`, `trailer`, `rack`, `none`),
  `support_owner` (`own`, `marina`), `haul_out_on` (planned, nullable), `launch_on` (planned, nullable),
  `notes_handling` (≤ 500 chars, no PII expected). **Exit date** of a storage allocation =
  `coalesce(launch_on, ends_on)`; **arrival date** = `coalesce(haul_out_on, starts_on)` clamped into the period.
  For other kinds in a surface area (transits, mooring segments placed ashore, holds) exit = `last_night + 1`.
- Default `ends_on` by term: annual +1 year; seasonal = season window end of the active `storage` price list
  covering `starts_on` (`ListSettings.SeasonWindow`), else required; monthly +1 month; period required.
- Price: `pricing.ContractStorage` for every storage term (fix `PricingType(kind, term)`: kind storage → storage),
  category = area `price_class` (covered vs uncovered is configured as two price classes, e.g. `shed`, `yard`).
  Bases `per_m2`, `length_band`, `flat` work unchanged (reference unit month, m2-plan D5).
- Instalments, charges, termination, expiry: unchanged M4/M5 paths (they are kind-agnostic; STORAGE verifies with
  tests and fixes kind filters where a path is mooring-only, e.g. expiry list, contract list filter).

### 3.2 Creation `POST M/storage/contracts`

Input: term, customer, billing customer, vessel, starts_on, ends_on?, `area_id`, `placement?` {x_cm, y_cm,
rotation_dd}, `cradle_resource_id?`, support, support_owner, haul_out_on?, launch_on?, services, policy, notes,
activate, fit_override? Permission `storage.manage` (spec 26.1 "Rimessaggio") — **not** `contracts.write`
(yard_mgr must create storage contracts; office has both).

- Without `placement` the service computes one with `optimizer.PlaceOne` on the current snapshot of the area for
  the period (obstacles = all active rows of the period), so every contract occupies a concrete position (as
  every hold, 10.7). No position → `409 storage_no_space` (the UI offers to open a scenario).
- `fit.Check` with the surface target (max LOA/height, support list, weight per cell, §3.3); blocking dimensional
  codes overridable with `occupancy.override_fit`; `support_not_allowed` never.
- Writes through `contract.CreateStorageTx` (new, `contract/storage.go`): contract + lines + allocations (hold for
  draft, contract when activated) + details + instalment plan + audit `contract.create` + events, one tx.
- `PATCH M/storage/contracts/{contractId}` (If-Match): support, dates `haul_out_on`/`launch_on`, notes (audit
  `storage.details_update`, event `contract.changed` topic). Period/placement changes use existing resize/move.

### 3.3 Fit additions (STORAGE, Go + TS parity)

`fit.Vessel` gains `Support string`, `DisplacementKg *int64`; `fit.Target` gains `Supports []string` (empty =
any), `MaxWeightKg *int64` (caller computes `max_weight_kg × hull cells`, hull cells = cells of LOA×beam without
margin at the placement, like `Depth`). Codes: `support_not_allowed` (blocking, not overridable, `Overridable =
false` when present), `weight_exceeds` (blocking, overridable). Surface `MaxAirDraft` = `max_height_cm` (exists).
Update `web/shared/src/map/fit.ts`, the vector generator and `testdata` vectors; `TestFit_ParityVectors` and
`fit.parity.test.ts` stay green.

## 4. Cells, occupancy and the area snapshot (normative, STORAGE)

- Nothing changes in `cell_occupancies` (PK `(area_id, cell, night)`), `allocations`, `layout.Footprint`. A
  storage placement writes `Footprint × nights` rows (INV-03).
- `storage.Snapshot(area, from, to)` reads with a few queries (never one per boat): area grid (`layout.AreaOf`),
  exits (cell indexes), every active allocation overlapping `[from, to]` on the area with vessel size, support,
  displacement, stay, exit/arrival dates, version, kind; blocks and maintenance rows are **fixed obstacles**.
- Exits: stored as cell indexes (M1). A **gate** is a 4-connected component of exit cells. `ValidateGates` (pure)
  rejects at planner load (`422 area_exits_invalid` on arrange/apply; warning badge in the plan) gates whose cells
  are not usable or not on the grid border (a usable cell with a side on the grid edge or next to a non-usable
  cell that is outside the outline).
- Grid coarsening: when `cols × rows > 40,000`, the optimizer works on a grid coarsened by the smallest integer
  `k` with `(cols/k)·(rows/k) ≤ 40,000` (a coarse cell is free only if all its fine cells are usable and free);
  every output placement is a fine placement (`x = col·k·cell`) and is re-validated with `layout.Footprint`.

## 5. Accessibility (normative, STORAGE, pure `optimizer`)

```go
func Accessibility(a Grid, present []Placed, exiting []string, date calendar.Date) []Access
type Access struct { ID string; Accessible bool; Blocking []string; Order int } // Order: extraction order that day
```

Model (spec 17.4, documented heuristic):
1. **Free grid**: usable cells minus blocked minus cells of other present boats on the date (boats whose stay
   covers night `date − 1` and that do not leave earlier that day), plus the boat's own cells.
2. **Moving rectangle**: the boat's footprint box in cells (`⌈(LOA+margin)/cell⌉ × ⌈(beam+margin)/cell⌉`, the
   footprint's real cell box when misaligned). States = (top-left cell, orientation ∈ {0, 90}). Moves: one cell in
   4 directions when every cell of the new box is free (O(1) with a summed-area table of the free grid); rotation
   in place when the `max(w,h)` square at the top-left is free.
3. **Goal**: a state whose box has one whole side on the grid border with every cell of that side in the same
   gate. A boat leaving bow first through a door thus needs a door at least `beam + margin` wide (its narrow
   side), as the spec requires.
4. BFS (breadth first, deterministic neighbour order N, E, S, W, rotate). States ≤ 2N.
5. **Same-day exits** (extraction order per launch date): repeat — among the boats leaving that day not yet out,
   take the accessible one nearest to a gate (gate distance field, tie by id), remove it, assign `Order`; stop when
   none is accessible; the rest are blocked. Exits happen before arrivals of the same day.
6. **Blocking vessels** of a blocked boat: remove present boats one at a time in order of gate distance (nearest
   first, tie by id) until accessible; then try to put each removed boat back in reverse order and keep it if the
   boat stays accessible. The remaining removed set is `Blocking` (heuristic minimal cut, not minimum).
7. **Arrivals** (D9): the same check with present = boats covering night `arrival` that arrived before (same-day
   arrivals in reverse greedy order). Reported as `arrival_blocked` warnings, weaker than exits in the objective.

`AccessibilityTimeline(grid, items, from, to)` evaluates every exit and arrival event of the window (events are
the distinct exit/arrival dates, ≤ 2n) and returns per boat `{exit_on, accessible, blocking[], order}` and per date
the extraction order. Planner, scenario indicators, movement list and reports all call this function.

Warnings: an exit (contract end, planned launch, later M10 work order) of a non-accessible boat is
`exit_blocked` in the planner, in scenario indicators, in the movement list and in the `storage` report.

## 6. Optimizer (normative, STORAGE, pure `internal/domain/optimizer`)

### 6.1 API

```go
type Input struct {
    Grid      Grid            // from layout.Area (+ coarsening), gates, gate distance field
    Fixed     []Obstacle      // blocks, maintenance, non-scenario allocations: cells × night range
    Items     []Item          // id, LOA, beam, support, weight, first/last night, exit_on, arrival_on,
                              // current *Placement (nil = from outside / unplaced), Pinned bool
    Fit       FitParams       // fit.Rules + area target (max LOA/height, supports, weight/cell)
    Seed      uint64
    MaxIter   int             // default 20,000; deterministic budget
    Margin    geo.Cm
}
type Result struct {
    Placements []Placement    // per item: placed position or Unplaced{reason}
    Score      Score          // lexicographic vector, §6.2
    Access     []Access       // timeline of §5 for the result
    Iterations int; StoppedBy string // "iterations" | "deadline" | "optimal"
}
func Arrange(ctx context.Context, in Input) (Result, error)        // ctx = caller deadline (default 2.5 s)
func PlaceOne(in Input, item Item) (*Placement, error)               // construction step only, others fixed
func Validate(in Input, r Result) error                              // independent validity checker
```

`context` is stdlib and is the only "time" the package sees; it is checked every 64 iterations. With an
iteration budget that is not cut by the deadline, the output is a function of `(Input, Seed)` only.

### 6.2 Objective (lexicographic, all `int64`, smaller is better)

1. `unplaced` — items without a valid position;
2. `exits_blocked` — exit events of §5 that are blocked (boats leaving first near the gates);
3. `arrivals_blocked` (D9);
4. `moves` — items with a current placement in the area whose placement changes (rotation 0↔180 and 90↔270 are
   the same rectangle and are not moves);
5. `−largest_free_rect_cm2` — largest axis-aligned rectangle of free usable cells on the scenario start night
   (histogram/stack algorithm, O(N)).

### 6.3 Algorithm

1. **Prepare**: drop items failing `fit` (unplaced with the fit codes; pinned items that fail are an input error
   `422 pinned_invalid`). Gate distance field: multi-source BFS from gate cells over usable non-blocked cells.
   Pinned items and `Fixed` become per-night obstacles.
2. **Construction** (two variants, keep the better score):
   - *A, keep current*: items with a valid current placement that conflicts with nothing already kept stay; then B
     for the rest.
   - *B, exit-ordered bottom-left fill*: items sorted by `(exit_on, −LOA×beam, id)`; for each, candidate positions
     = every top-left cell × orientation {0, 90} whose box is free of time-overlapping placed items and obstacles
     (per-item free bitmap + summed-area table → O(1) per candidate); choose the minimum
     `(gate distance of the box, row, col, orientation)` — bottom-left measured from the exit, so boats leaving
     first sit nearest the gates. Exact position in cm = cell origin (snap to the cell grid).
3. **Local search** (late-acceptance hill climbing, history length 50, integer scores): neighbourhoods chosen by
   the PRNG — relocate an item to one of its top-8 candidates, swap two time-overlapping items, insert an
   unplaced item (evicting at most one other item to unplaced), restore an item's current placement. A neighbour
   is accepted when its score ≤ current or ≤ the score 50 steps ago. Keep the best seen. Stop at `MaxIter`, ctx
   done, or when `unplaced = exits_blocked = arrivals_blocked = moves = 0` and the free-rect term equals the free
   area (optimal).
4. **Validate** the best result with `Validate` (Footprint inside usable cells, no blocked cell, pairwise disjoint
   cells for time-overlapping items and against obstacles, pinned unchanged, fit OK). A failure is a bug: the
   service returns `500` and logs the seed (never ships an invalid layout).

Determinism: own PRNG (`rng.go`, splitmix64 seeding xoshiro256**), no map iteration in decisions (sorted slices),
stable sorts with id tie-breaks. Default seed = FNV-1a 64 of the scenario id (stored on the scenario so a rerun
reproduces); request may pass `seed`.

### 6.4 Complexity and the 3 s budget

N = grid cells (≤ 40,000 after coarsening), n = items (≤ 300, `422 too_many_items` beyond), E ≤ 2n events.
- Construction: O(n · (N + 2N)) with per-item summed-area tables. 2,000 cells × 40 items ≈ 0.25 M steps.
- One evaluation: O(E · (N + 2N·4)) BFS + O(N) free rect; ≈ 0.5–1 ms for N = 2,000, n = 40 (incremental: only
  events whose present set changed are re-evaluated, cached by `(date, placement hash)`).
- Service budget: `storage.optimize_budget` default 2,500 ms passed as ctx deadline around `Arrange`, plus
  snapshot read (< 200 ms) and response; the HTTP call must finish < 3 s for the acceptance shed.
- Acceptance fixture (`optimizer/internal/fixture`, seeded): shed 50 m × 40 m = 2,000 m², cell 100 cm, margin
  60 cm, one 6 m door on a short side, two rows of pillar cells blocked, 40 boats LOA 6–14 m, beam 2.3–4.5 m,
  stays Oct–May, launch dates spread Mar–Jun; the generator builds a witness layout first (so a valid solution
  exists) and then shuffles current placements. Required: `unplaced = 0`, `exits_blocked = 0`, `Validate = nil`,
  wall time < 3 s (asserted in the test without `-race`; skipped under `-race`).

## 7. Scenarios (normative, STORAGE)

### 7.1 Model (§13)

- `storage_scenarios`: area, name (≤ 80), `author_user_id`, `from_on`, `to_on` (inclusive night window, ≤ 400
  nights), `status` (`draft`, `applied`, `discarded`), `seed`, `score` json (last indicators), `base` json
  (allocation id → version seen when the item was loaded), `applied_at`, `applied_by_user_id`, `version`.
- `scenario_items`: one row per allocation in the working copy: `allocation_id`, `vessel_id`, `x_cm`, `y_cm`,
  `rotation_dd` (null = unplaced), `pinned`, `origin` (`area` = was in the area, `tray` = brought in from another
  resource). Unique `(scenario_id, allocation_id)`.
- `scenario_moves`: recomputed on every save: `seq`, `allocation_id`, `vessel_id`, from (resource + x/y/rot or
  null), to (x/y/rot), `move_on` = `max(from_on, allocation.first_night, today)`, `kind` (`move`, `place`).
  Moves are the diff working copy vs real state; the order is the recommended execution order (§7.4).
- **Never** written by a scenario: `allocations`, any `*_occupancies` (INV-14).

### 7.2 Operations (`storage.manage`; reads `storage.read`)

| Operation | Behaviour |
|---|---|
| create | from the real state of the area for the window (all vessel allocations overlapping it) → items `area`; base versions recorded |
| update (PATCH name, window) | If-Match; changing the window reloads items not yet touched |
| save placements (PUT items) | If-Match; validates each item with Footprint + fit (422 per item), recomputes moves and indicators; overlaps inside the copy are allowed while editing but flagged (`overlap` indicator) and block apply |
| add from tray / remove | bring an allocation of another surface area or land resource of the same marina in (`tray`), or drop a `tray` item; the candidates come from `listStorageTrayCandidates` (`storage.read`) |
| optimize | runs `Arrange` on the copy (pinned respected), stores result + seed + stats; returns indicators |
| compare | `GET …/compare?with=<scenarioId>|real`: both indicator sets and per-boat position diff |
| apply | §7.3 |
| discard / delete | status `discarded` (applied scenarios cannot be deleted; history) |

Indicators (pure, from §5/§6): used surface m² (hull and footprint), boats blocked per date, number of moves,
unplaced boats, overlaps, largest free rectangle, `stale` (any base version ≠ current version, computed on read).

### 7.3 Transactional apply `POST M/storage/scenarios/{id}/apply` (If-Match, Idempotency-Key)

1. Outside the tx: price effects of tray items (`priceLayout` path), read-only.
2. `RunInTx`:
   a. `SELECT … FOR UPDATE` the scenario; status must be `draft` and version = If-Match, else `409
      scenario_not_draft` / `412`.
   b. Build a `LayoutSpec` (area, `Date = FromNight = max(from_on, today)`, `Base` = scenario base versions,
      `Ops` = every placed item); `occupancy.LayoutTx` locks every allocation of the area present at FromNight
      plus every item (id order), re-checks versions (`ErrVersionMismatch` → stale), deletes and re-inserts rows.
   c. Before calling `LayoutTx`, under the same locks, `storage.validateOnCurrent` re-reads the current rows of the
      window (excluding the scenario's own allocations) and checks every op with Footprint, fit and cell
      conflicts, collecting **all** conflicts (`{allocation_id, reason: stale|resource_taken|fit|unplaced|overlap,
      cells?, nights?}`). Any conflict → return `409 scenario_conflicts` with the list; nothing written. The DB
      unique keys remain the authority: a conflict missed here surfaces as `ConflictError` from `LayoutTx` and
      rolls back everything.
   d. Write `storage_movements` (worklist) from `scenario_moves`, set scenario `applied`, audit
      `storage.scenario_apply` (moves count, allocation ids), outbox `storage.scenario_applied` (+ the
      `occupancy.moved` events `LayoutTx` writes).
3. Response: the movements with order and equipment hint.

All-or-nothing = one transaction; any error at any step (including injected faults, §14.3) leaves allocations,
occupancy rows, scenario, worklist, audit and outbox unchanged (INV-06).

### 7.4 Movement worklist

`storage_movements`: area, scenario (nullable: also created by single planner moves? no — only by apply),
allocation, vessel, `seq`, `move_on`, from/to, `equipment_hint` (`travel_lift` when from water / to water,
`trailer` for support trailer, `cradle_mover` for cradles, `forklift` for racks), `status` (`pending`, `done`,
`cancelled`), `done_at`, `done_by_user_id`. Order: by `move_on`, then boats leaving their place to free others
first (moves whose source cells are a target of another move come first; cycles broken by a temporary position
hint "spostamento provvisorio"), then gate distance. `GET M/storage/movements?date&status`, `POST
M/storage/movements/{id}/done` (online, `storage.manage`; event `storage.movement_done`). Accessibility warnings
for the day (`exit_blocked`) are listed with the movements.

## 8. Maintenance (normative, UTILITIES)

### 8.1 Elements

`element_type` ∈ `berth` (incl. buoys), `structure` (quays, piers, fingers, walkways/gangways, floating modules),
`dynamic_area`, `land_resource` (incl. cradles), `pedestal`, `equipment`. New table `equipment` (spec 6.8, M10
extends it): code, name, kind (`travel_lift`, `crane`, `forklift`, `trailer`, `vehicle`, `plant`, `other`),
`map_id?` + `x_cm/y_cm?`, `capacity_kg?`, `active`. CRUD `M/equipment` (`maintenance.manage`).

### 8.2 Plans `maintenance_plans` (+ `maintenance_plan_targets`)

Name, `intervention` (`underwater_inspection`, `pennant_replacement`, `chain_check`, `rcd_test`, `general`,
`custom` + label), targets (element type + ids), periodicity `{kind: months|cycles|fixed_date, every?, month_day?,
meter_id?}`, `lead_days` (0–180), `assignee_user_id?` or `supplier_name?` (text, no supplier registry),
`checklist` [{key, label}] (≤ 40), `requires_free` (book `maintenance` nights), `nights` (1–60), `active`, version.

Pure `maintenance/due.Next(plan, lastDone *Date, lastCycles qty.Milli, currentCycles qty.Milli, from Date) → (due Date, ok)`:
months = last done (or plan start) + N months (end-of-month clamp); fixed_date = next occurrence of MM-DD;
cycles = due when `currentCycles − lastCycles ≥ every` (cycles from the equipment's `cycles`/`hours` meter, §9).

Job `maintenance.schedule` (hourly, per marina): for each active plan target compute `due`; when `today ≥ due −
lead_days` and no task exists for `(plan_id, element_type, element_id, due_on)` (unique index) insert a task
`open` (`source = plan`), audit, event `maintenance.due`, and a deadline row (`deadlines.source_type =
'maintenance_task'`, kind `maintenance_due`) so it appears in the M4 deadlines list and notifications.

### 8.3 Tasks `maintenance_tasks`

Element, `plan_id?`, `source` (`plan`, `office`, `report`), title, description (≤ 2000), priority (`low`,
`normal`, `high`, `urgent`), `status` (`open`, `scheduled`, `in_progress`, `done`, `cancelled`), `due_on?`,
`scheduled_from?`/`scheduled_to?` nights, `out_of_service` bool, `position_x_cm/y_cm` + `map_id` (phone report),
`outcome` (`ok`, `fixed`, `needs_followup`, `not_done`) + notes, checklist results json, `internal_minutes`,
`external_cost_cents`, `supplier_ref`, `assignee_user_id`, `reported_by_user_id`, `allocation_ids` json
(maintenance allocations), `client_ref` (offline dedupe), version. Files: `maintenance_task_files` (stored
files, purpose document, photos and documents).

Transitions (pure table in `maintenance/states.go`): open → scheduled → in_progress → done; open|scheduled|
in_progress → cancelled; open → in_progress; done is final (follow-up = new task linked `follows_task_id`).

- **Schedule** (`maintenance.manage`, If-Match): sets nights; with `requires_free` on a `berth`/`land_resource`
  (or a `structure` with `occupy_berth_ids`) creates `maintenance` allocations via `occupancy.AllocateTx` (vessel
  null, no block reason) in the same tx. Conflict with assigned vessels → `409 maintenance_conflicts` listing each
  allocation with up to 3 alternatives from `occupancy.Search` for its vessel; the office moves them (existing move
  UI) and retries. Never overrides an occupancy.
- **Out of service**: while a task with `out_of_service` is `scheduled`/`in_progress` covering today, the element
  is out of service: berths/land resources hold `maintenance` rows (not sellable, DB-enforced), pedestals/
  equipment/structures are flagged in the map lens and lists (and pedestal meters keep accepting readings).
- **Complete** (`maintenance.manage`, or `maintenance.report` for the assignee): outcome, checklist, costs,
  photos. Completing early does not release the remaining maintenance nights (offline-safe, INV-71); the office
  shortens with `POST …/release` (online). Cancel releases future maintenance nights (online).
- **Report** (`maintenance.report`: yard, rental, dock, …): creates an `open` task (`source = report`) with
  element or map position, description, photo. Never schedules, never occupies.
- Events: `maintenance.due`, `maintenance.status_changed` (ids, element type, status); SSE topic
  `maintenance.changed`. Lens *Manutenzione* (`GET M/maintenance/map?map_id`): per element open/overdue/out of
  service counts.

## 9. Meters, readings and consumption charging (normative, UTILITIES)

### 9.1 Meters `utility_meters`

Code (unique per marina), `kind` (`power`, `water`, `hours`, `cycles`), unit (`kwh`, `m3`, `h`, `cycle`),
`pedestal_id?` + `socket_code?` + `serial?`, `resource_type?/resource_id?` (the one berth or land resource the
socket feeds; null = not chargeable automatically), `equipment_id?` (hours/cycles), `item_id?` (tariff override),
`multiplier_milli` (CT ratio, default 1000), `rollover_milli?` (register maximum), `daily_threshold_milli?`,
`active`, version. CRUD `M/utility-meters` (`marina.configure`). A meter is created from the pedestal's
`meters` json in the UI (prefill), the pedestal JSON stays map data.

### 9.2 Readings `meter_readings`

`meter_id`, `read_at` (timestamptz), `read_on` (marina-local date of `read_at`), `value_milli ≥ 0` (cumulative
index), `kind` (`regular`, `initial`, `replacement_old`, `replacement_new`), `source` (`staff`, `import`,
`sensor` reserved), `photo_file_id?`, `user_id`, `note`, `status` (`valid`, `anomaly`, `confirmed`, `void`),
`anomaly_codes` json, `client_ref` (idempotency key of the queued op), version. Unique `(meter_id, read_at)` and
`(marina_id, client_ref)` partial where not null.

- `POST M/meter-readings` (`presence.write`: spec 26.1 "letture" with arrivals/departures; offline). Input:
  meter id, value as decimal string (`qty.Parse`, 3 decimals), note, `read_at?` (default: `Porto-Offline-Queued-At`
  when replayed within 48 h and not in the future, else server now). Replay with the same `client_ref` returns the
  stored reading (INV-83).
- Anomalies (pure `consume.Check`): `negative` (Δ < 0 without replacement and no rollover), `rollover` (Δ < 0 with
  rollover set: Δ = rollover − prev + cur, flagged), `over_threshold` (Δ > threshold × days, threshold from the
  meter or utility settings per kind), `out_of_order` (`read_at` earlier than a reading already used by a charged
  consumption), `gap` (> 120 days). Anomalous readings produce consumptions in status `anomaly`; the office
  confirms (`POST …/confirm`, `charges.manage`) or voids (`POST …/void`; refused `409 reading_charged` when used by
  a charged consumption).
- `POST M/meter-readings/{id}/photo` (offline, dependent op), files purpose document, encrypted (INV-74).
- Event `meter.reading_recorded` (ids only), SSE topic `utilities.changed`.

### 9.3 Consumption (pure `utility/consume`)

- Interval between consecutive non-void readings of a meter (ordered by `read_at`; replacement pairs restart the
  index): `Δ = (cur − prev) × multiplier / 1000` in `qty.Milli` (half-even), nights `prev.read_on …
  cur.read_on − 1` (a same-day pair is assigned to night `prev.read_on`).
- **Split by nights**: occupants of the meter's resource per night (allocations of kind `contract`/`transit`/
  `owner` with their contract; `owner` → the right holder's billing contract is out: owner nights are
  `unassigned` unless a contract exists, D29) → `consume.Split(Δ, nights, segments)` = shares proportional to
  nights with **largest remainder** in milli, ties to the later segment; Σ shares = Δ exactly (INV-91). Nights with
  no occupant → `unassigned` share (marina's own consumption, reported, never charged).
- Rows `utility_consumptions`: meter, from/to reading, `contract_id?`, `allocation_id?`, `customer_id?` (billing),
  first/last night, `qty_milli`, `status` (`pending`, `anomaly`, `unassigned`, `charged`, `excluded`),
  `charge_id?`, `billing_id?`. Recomputed for an interval when a reading is inserted inside it, only while every
  affected consumption is `pending`/`anomaly`/`unassigned` (charged intervals are frozen; the new reading gets
  `out_of_order`).

### 9.4 Charging (UTILITIES, through M5)

- Tariff: item = meter `item_id` or utility settings `power_item_id` / `water_item_id` (seed: `UTL-KWH`,
  `UTL-H2O`, basis `metered`); unit price = `charge.ItemPrice(item, last_night)` from the active list; amount per
  line = `qty.MulCents(unit)` half-even (M2 `metered`), VAT per item. A missing price leaves the consumptions
  `pending` and lists `no_price` in the run result.
- **When**: job `utilities.charge` (hourly) and the manual run `POST M/utility-charges/run?contract_id`
  (`charges.manage`):
  - mooring and storage contracts (`utility_settings.mode = with_installment`, default): pending consumptions of a
    contract are charged on the day its next instalment charge is issued (job runs after `installments.issue`;
    query: contracts with an instalment charge issued today, or ended contracts) with `due_on` = that instalment's
    due date; mode `monthly` charges on the 1st of each month;
  - transits: when the transit is `departed` or its last night has passed.
- One charge per run per contract: `utility_billings` row (origin `utility`, origin_id = billing id) + charge via
  `charge.CreateTx` with one line per consumption (description "Energia 12,345 kWh 01/06–30/06, colonnina P12"),
  `service_from/to` = nights. Under the contract lock (`lockContract`), consumptions are claimed with `UPDATE …
  SET status='charged', charge_id=?, billing_id=? WHERE id IN (…) AND status='pending' AND charge_id IS NULL`;
  affected rows ≠ expected → rollback and retry once (INV-90). CONTRACT adds `utility` to the partial unique
  index `charges_origin_primary_uidx` (one live primary charge per billing).
- Corrections after charging: never edit the charge (INV-51). A confirmed correction (void of a charged reading
  is refused; a corrected reading creates a delta consumption) is charged as an `adjustment` charge linked with
  `corrects_charge_id` by the same run.
- `GET M/utility-consumptions?status&contract_id&meter_id`, `POST …/{id}/exclude` (reason) and `…/{id}/assign`
  (contract for `unassigned`), `charges.manage`.

## 10. Offline (UTILITIES, spec 29.2, ADR 0011 rules)

| Kind (reserved in M6b) | Operation(s) flagged `x-porto-offline: true` | Server dedupe |
|---|---|---|
| `utility.reading` | `recordMeterReading`, `uploadMeterReadingPhoto` (dependsOn bind `readingId`) | `client_ref` unique + Idempotency-Key |
| `maintenance.report` | `reportMaintenance`, `uploadMaintenancePhoto` (dependsOn bind `taskId`) | `client_ref` unique |
| `maintenance.outcome` | `completeMaintenanceTask` (outcome, checklist, minutes, notes) | state re-check: done with same `client_ref` = same result; done by someone else = `409 task_already_done` shown as conflict |

None creates, moves or releases allocations (INV-71 extended). `OFFLINE_OPERATION_KINDS`, `OfflineOpKind`, the
server set (`TestPolicies_OfflineSet`) and the openapi flags change together (CONTRACT). Allowlist slot
`activities.assigned` = `GET M/maintenance/worklist` (assigned + due ≤ 7 days) and `GET M/utility-meters?route=`
(meters of the dock round) for 48 h. Movement completion and scenario operations stay online-only.

## 11. Reports and dashboard tiles (PLANNER, M7 machinery, INV-61..64)

| Report key | Permission | Records | Totals |
|---|---|---|---|
| `storage` | `reports.operational` | per allocation on surface areas: area, map kind (yard/shed), vessel (id, LOA, beam), contract kind/number, nights in period, hull m²-nights, footprint m²-nights, exit date, `exit_blocked` (from §5 at the exit date), production of the storage contract (`reports.all` only) | available m²-nights (usable cells × cell² × nights, minus block/maintenance rows), occupied footprint m²-nights, occupancy BP, boats, blocked exits |
| `maintenance` | `reports.operational` | per task: element, intervention, source, priority, status, due, done, overdue days, minutes, external cost (`reports.all`) | tasks by status, overdue, minutes, external costs |
| `utilities` | `reports.operational` (amounts `reports.all`) | per consumption: meter, kind, resource, contract, nights, qty, status, charged amount | qty by kind and status, charged cents, pending value (at current price), unassigned qty (losses) |

Tiles (`calc.Tile*`, drill = report with same filter, value = Σ records by construction):
`storage_occupancy` (storage report, occupancy BP), `storage_exits_blocked` (storage report, `exits_blocked_only`,
period = next 30 days, no previous-year value), `maintenance_overdue` (maintenance report, `overdue_only`),
`utility_unbilled` (utilities report, `status=pending`, value cents, `reports.all`). The M7 occupancy tile keeps
`map_kind = water`; the occupancy report accepts `map_kind=yard|shed` (already) and the storage page links there.
CSV/Excel via `report/export` unchanged. Production (R6) counts `storage` contracts from M9 (`calc.Qualifying`).

## 12. UI

### 12.1 PLANNER (`web/admin/src/features/storage/**`, `features/phone/storage/**`)

- **Storage areas list** `/m/$id/storage`: surface areas (yard/shed), occupancy today, boats, blocked exits next
  30 days, scenarios count; button "Nuovo contratto di rimessaggio".
- **Planner** `/m/$id/storage/$areaId`: split view — plan (`HarborMap` focused on the area, scale silhouettes,
  cell colouring free/occupied/blocked/exit, date slider and day stepper; arrivals/exits of the day highlighted)
  and timeline (one bar per boat from arrival to exit, sorted by exit; red marker on blocked exits; click selects
  on the plan). Lens *Accessibilità*: blocked boats red, blocking boats outlined, extraction order numbers on the
  selected date. Layout session (existing `layoutSession` reducer reused, not copied) with Save / Ripristina,
  tray "Da collocare", button **Preset** (calls `arrange`, fills the draft, nothing saved), "Apri come ipotesi".
  Drag and drop: pointer drag with `DragGhost`, snap to cells, rotate with `R` / button, invalid ghost red with
  the reason (fit code, overlap, blocked cell); keyboard alternative (arrow keys move one cell, `R` rotate) for
  a11y.
- **Scenarios** `/m/$id/storage/$areaId/scenarios` + `/…/scenarios/$scenarioId`: list (name, author, window,
  status, stale badge), editor = planner on the copy (pin toggle, optimize with seed field under "Avanzate",
  indicators card: surface used, blocked per date, moves, unplaced, overlaps), **compare** page with two plans
  side by side at the same date + indicator table + moved boats list; **Applica** with confirmation (moves count),
  409 conflicts shown as a list with links to the boats.
- **Movements** `/m/$id/storage/movements`: day list, print-friendly; phone `/m/$id/storage-phone`: today's
  movements (order, from→to, equipment), mark done (online), blocked exits warnings, read-only plan.
- **Storage contract screens**: create dialog/page (customer, vessel, term, dates, area + position picker on the
  plan or "posizione automatica", support, cradle picker, planned haul-out/launch, services, price preview) and
  the storage section in the contract detail (details card, edit dates, link to planner). Contract list filter
  `kind=storage`.
- **Reports and tiles**: report pages for `storage`, `maintenance`, `utilities` in `features/reports/**` and the
  four tiles in the dashboard.

### 12.2 UTILITIES (`web/admin/src/features/{maintenance,utilities}/**`, `features/phone/{maintenance,readings}/**`)

- Maintenance: plans list/editor (targets picker by element type, periodicity, checklist builder), tasks board
  (filters status/priority/element; overdue first), task detail (schedule with nights and conflict dialog listing
  vessels with alternatives, start, complete with checklist/costs, photos, cancel), equipment registry, map lens
  *Manutenzione* in `web/shared/src/map/lenses/maintenance.ts`.
- Utilities: meters list/editor (prefill from pedestals), readings list per meter with anomalies to confirm/void,
  consumptions review (pending, anomaly, unassigned → assign/exclude), "Addebita consumi" run with result
  summary, utility settings (items, mode, thresholds).
- Phone: `/m/$id/maintenance-phone` (worklist from the offline cache, task detail with checklist and outcome,
  "Segnala" with camera photo and map position tap), dock tab **Letture** (registered through a new
  `DockSlots.extraTabs` slot): meter search by code/QR/berth, previous value and expected range, numeric keypad
  input, optional photo, offline queue status. Landing rows for `storage-phone` / `maintenance-phone` (CONTRACT
  writes the rows in `landing.ts`).

## 13. Data (CONTRACT; both dialects, same names; `store.BackupTables()` registration)

`00018_storage.sql`:

| Table | Columns (all with `marina_id`, composite FKs `(x_id, marina_id)`) | Constraints |
|---|---|---|
| `storage_contract_details` | `contract_id` PK, support CHECK, support_owner CHECK, haul_out_on, launch_on, notes_handling, version, timestamps | FK contracts RESTRICT; CHECK `launch_on IS NULL OR haul_out_on IS NULL OR launch_on >= haul_out_on` |
| `storage_scenarios` | id, area_id, name, author_user_id, from_on, to_on, status CHECK, seed BIGINT, score json, base json, applied_at, applied_by_user_id, version | CHECK to_on ≥ from_on; idx (marina_id, area_id, status) |
| `scenario_items` | scenario_id, allocation_id, vessel_id, x_cm, y_cm, rotation_dd, pinned, origin CHECK | unique (scenario_id, allocation_id); FK scenario CASCADE, allocation RESTRICT; CHECK placed-or-null triple |
| `scenario_moves` | scenario_id, seq, allocation_id, vessel_id, from_resource_type/id, from_x/y/rot, to_x/y/rot, move_on, kind CHECK | unique (scenario_id, seq); FK CASCADE |
| `storage_movements` | id, area_id, scenario_id?, allocation_id, vessel_id, seq, move_on, from/to columns, equipment_hint, status CHECK, done_at, done_by_user_id, version | idx (marina_id, move_on, status) |

`00019_maintenance_utilities.sql`: `equipment`; `maintenance_plans`; `maintenance_plan_targets` (unique (plan_id,
element_type, element_id)); `maintenance_tasks` (unique partial `(plan_id, element_type, element_id, due_on)`
where plan_id not null; unique partial `(marina_id, client_ref)`; CHECK status/priority/outcome enums; CHECK
`status='done' ⇒ outcome NOT NULL`); `maintenance_task_files`; `utility_settings` (marina_id PK, power_item_id,
water_item_id, mode CHECK, thresholds, version); `utility_meters` (unique (marina_id, code)); `meter_readings`
(unique (meter_id, read_at), unique partial (marina_id, client_ref), CHECK value_milli ≥ 0); `utility_billings`;
`utility_consumptions` (unique (from_reading_id, to_reading_id, coalesce allocation) via
`(from_reading_id, to_reading_id, share_key)` with `share_key` = allocation id or `'unassigned'`; CHECK
`status='charged' ⇔ charge_id IS NOT NULL`); recreate `charges_origin_primary_uidx` with `utility` added (Down
restores the 00010 definition). Element FKs are validated in Go (polymorphic `element_type/element_id`, like
`deadlines`).

Invariants (CONTRACT writes them in `docs/invariants.md`, builders flip statuses):
- **INV-06** → implemented by `TestScenarioApply_AllOrNothing`, `TestConcurrency_ScenarioApply` (STORAGE).
- **INV-14 — Le ipotesi non toccano lo stato reale.** Create/save/optimize/compare/discard of a scenario never
  changes `allocations` or occupancy rows. `TestScenario_NoRealWrites`.
- **INV-15 — L'ottimizzatore produce solo disposizioni valide e riproducibili.** `TestRapid_ArrangeValid`,
  `TestArrange_Deterministic`, `TestArrange_Acceptance2000m2`.
- **INV-16 — Ogni contratto di rimessaggio ha i suoi dettagli e occupa una superficie.** Query Q20 of
  `store.CheckStorageInvariants`.
- **INV-90 — Un consumo è addebitato al più una volta.** Each consumption in ≤ 1 live charge; Q21.
  `TestConcurrency_UtilityChargeOnce`.
- **INV-91 — Le quote di consumo sommano la differenza di lettura.** `TestRapid_SplitSums`, Q22.
- **INV-92 — Le notti di manutenzione esistono solo per interventi pianificati.** Every `maintenance` allocation is
  referenced by a `scheduled`/`in_progress`/`done` task; Q23.
- **INV-71** extended to readings and maintenance outcomes/reports (no allocation change).

## 14. Tests

### 14.1 Pure

- `optimizer`: `TestGrid_Scenario` (gates, distance field, coarsening), `TestAccessibility_Scenario` (door width =
  beam+margin passes, narrower fails, corridor, rotation in place, same-day extraction order, blocking set
  minimality on hand cases), `TestRapid_AccessibilityMonotone` (removing a present boat never makes another
  inaccessible), `TestRapid_ArrangeValid` (random sheds/items/obstacles/pinned: `Validate` nil, pinned unchanged,
  no fit failure placed; `-rapid.checks=2000`), `TestRapid_ArrangeNeverWorseThanCurrent` (when the current layout
  is valid, score ≤ score(current)), `TestArrange_Deterministic` (same seed+budget ⇒ identical JSON; different
  seeds may differ), `TestArrange_Acceptance2000m2` (§6.4, < 3 s, 0 unplaced, 0 blocked exits, 10 fixture seeds),
  `BenchmarkArrange_2000m2_40`, `TestOptimizer_Purity`.
- `fit`: support/weight cases + regenerated parity vectors (`TestFit_ParityVectors`, `fit.parity.test.ts`).
- `maintenance/due`: months (end of month), fixed date (leap day), cycles; `TestRapid_DueNotBeforeLastDone`.
- `utility/consume`: Δ, multiplier, rollover, replacement pairs, anomalies table; `TestRapid_SplitSums` (Σ = Δ,
  each share within 1 milli of the exact proportion).
- `report/calc`: storage/maintenance/utilities builders, tiles = Σ records (`TestCalc_StorageTiles`, …).

### 14.2 Integration (`//go:build integration`, SQLite + PostgreSQL via `testdb`)

`TestMigrations_M9Constraints` (all uniques/CHECKs, origin index with utility, Up/Down/Up, schema parity),
`TestStorage_CreateContractAutoPlace`, `TestStorage_NoSpace409`, `TestStorage_FitSupportNotOverridable`,
`TestStorage_PricingStorageType`, `TestStorage_ExpiryAndTerminate`, `TestScenario_NoRealWrites`,
`TestScenario_StaleOnVersionChange`, `TestScenarioApply_AllOrNothing` (fault injection, §14.3),
`TestScenarioApply_ConflictListComplete` (three planted conflicts ⇒ three entries, nothing written),
`TestScenarioApply_MovementsOrder`, `TestMaintenance_ScheduleJobIdempotent`, `TestMaintenance_OccupancyConflicts`,
`TestMaintenance_OutOfServiceNotSellable`, `TestMaintenance_ReportNeverOccupies`, `TestUtility_ReadingReplaySameKey`,
`TestUtility_BackdatedReadingFrozenWhenCharged`, `TestUtility_SplitAcrossOccupants`, `TestUtility_ChargeWithInstallment`,
`TestUtility_TransitChargedAtDeparture`, `TestUtility_AdjustmentAfterExport`, report tests on seeded data,
backup round trip with the new tables, cross-marina sweep for every new route (`TestAcceptance_CrossMarina…`).

### 14.3 Fault injection and concurrency (both dialects, `-race`, ≥ 100 goroutines)

- **Fault injection** (`TestScenarioApply_AllOrNothing`): the test installs a throwaway trigger on the test
  database (`test_fault_counter` table + trigger on INSERT into `cell_occupancies`, `allocations`,
  `storage_movements`, `audit_log` that raises when the counter reaches 0; `RAISE(ABORT)` on SQLite, plpgsql
  `RAISE EXCEPTION` on PostgreSQL). For k = 1 … total writes of the apply (stepping by a prime on long runs):
  apply fails, and a full snapshot (allocations, cell rows, scenario, movements, audit count, outbox count) equals
  the pre-apply snapshot; finally with no fault the apply succeeds and every item is at its target (INV-03 Q15).
  No production hook.
- `TestConcurrency_ScenarioApply`: 100 goroutines on one area: 40 apply the same scenario (exactly one 200, others
  `409 scenario_not_draft`), 30 apply 3 other scenarios of the area (at most one applies overall; losers `409
  scenario_conflicts`/stale), 30 desk moves/allocations into target cells. After: `CheckOccupancyInvariants`
  empty, applied scenario ⇔ its items in place, failed ⇒ no partial position.
- `TestConcurrency_StorageCreateAutoPlace`: 100 storage contracts with auto placement in a small shed: winners =
  capacity, no overlap (INV-01/02).
- `TestConcurrency_UtilityChargeOnce`: 100 parallel charge runs + job on the same contract: each consumption in
  exactly one charge (INV-90).
- `TestConcurrency_MaintenanceScheduleVsAllocation`: 100 parallel schedule vs desk allocation on the same berth
  nights: exactly one wins.

### 14.4 Acceptance (`internal/acceptance/m9_*_test.go`, API only, `clock.Fake`, both dialects)

`TestAcceptance_M9_StorageShed`: create a 2,000 m² shed map via API, 40 storage contracts with launch dates,
shuffle positions with layout saves, create scenario, `optimize` (HTTP round trip < 3 s asserted on SQLite
without race), check indicators 0 unplaced / 0 blocked exits, apply, verify every allocation equals the scenario,
`CheckOccupancyInvariants` and `CheckStorageInvariants` empty; then a second scenario made stale by a desk move
returns 409 with the list and leaves the state identical. `TestAcceptance_M9_MaintenanceCycle` (plan → job → task
→ schedule with conflict → move → schedule → offline outcome replayed twice → done once). `TestAcceptance_M9_Utilities`
(readings across an occupant change, anomaly confirm, charge with the next instalment, export, correction as
adjustment). `TestAcceptance_M9_Offline` (queued readings/report/outcome replayed: one effect each, INV-83,
INV-71).

### 14.5 Web

Vitest: planner reducer (drag, rotate, snap, tray, preset fill, restore), scenario compare diff, accessibility
lens colouring, storage contract form, reports/tiles pages (PLANNER); plan editor, task transitions UI, conflict
dialog, readings keypad and range hint, offline kinds registry (`allowlist.test.ts` updated: no reserved kinds
left for M9), phone screens (UTILITIES). Playwright: `storage-planner.spec.ts` (drag a boat, invalid ghost,
save/restore, preset), `storage-scenarios.spec.ts` (create, optimize, compare, apply, conflict path),
`storage-contract.spec.ts`, `storage-phone.spec.ts` (PLANNER); `maintenance.spec.ts`, `phone-maintenance.spec.ts`
(report with photo offline then reconnect), `phone-readings.spec.ts` (offline readings, replay without
duplicates), `utilities-charging.spec.ts` (UTILITIES); `a11y.spec.ts` additions by each owner for its pages.

## 15. Ownership, order and acceptance

### 15.1 CONTRACT (runs first, alone; then frozen)

`api/openapi.yaml` (tags `storage`, `maintenance`, `utilities`; every operation of §3.2, §7, §8, §9, `GET
M/storage/areas`, `GET M/storage/areas/{areaId}/plan` (`from`/`to` optional), `GET …/accessibility` (`date`
optional), `POST …/arrange`, `POST …/evaluate`; offline flags §10; problem slugs; no required query parameters on
`{marinaId}` routes) + `make gen`; migrations `00018_storage.sql`, `00019_maintenance_utilities.sql` (both
dialects); `internal/store/{storage_models,maintenance_models,utility_models}.go`, `backup_tables.go`/`backup.go`
registration; `internal/domain/contract/states.go` (`StorageTransitions`) and `contract/storage.go` **signature
only** (`CreateStorageTx`, `UpdateStorageDetailsTx` returning `ErrNotImplemented`); `internal/domain/optimizer/
{doc,types}.go` (types and signatures of §5/§6, bodies `panic("not implemented")` not reachable from tests);
`internal/domain/storage/{types,service,errors}.go`, `internal/domain/maintenance/{types,service,errors,
states}.go`, `internal/domain/utility/{types,service,errors}.go` (exported methods returning `ErrNotImplemented`);
pure sub-package `doc.go` + types (`maintenance/due`, `utility/consume`); event constants (`storage.
scenario_applied`, `storage.movement_done`, `maintenance.status_changed`, `meter.reading_recorded`; `maintenance.
due` exists), webhook catalogue, SSE topics `storage.changed`, `maintenance.changed`, `utilities.changed`; problem
slugs + `errmap.go`; `internal/http/admin/admin.go` fields + stubs `storage.go`, `storage_scenarios.go`,
`maintenance.go`, `equipment.go`, `utilities.go` (501); `internal/app/app.go` wiring, jobs `maintenance.schedule`
(1 h), `utilities.charge` (1 h, after `installments.issue`), seed call sites (`seed_storage.go`,
`seed_utilities.go` stubs); meta capabilities `storage`, `maintenance`, `utilities`; `.golangci.yml` pure rules
(§0.4, depguard optimizer imports); middleware offline set (`policy.go` list) consistent with openapi;
`internal/domain/report` seam constants only (`calc.Key{Storage,Maintenance,Utilities}`, tile keys, reasons) —
no logic; web: `queryKeys.ts`, hook stubs `web/shared/src/api/hooks/{storage,maintenance,utilities}.ts`,
`OfflineOpKind`/`OFFLINE_OPERATION_KINDS` entries, `DockSlots.extraTabs`, i18n files `storage.json`,
`maintenance.json`, `utilities.json` (it, en skeleton) + `resources.ts`, `web/admin/src/router.tsx` factories
`storageRoutes`, `maintenanceRoutes`, `utilitiesRoutes` + `nav.ts` entries, phone routes stubs
`storage-phone`, `maintenance-phone`, `landing.ts` rows; shadcn components via CLI if missing (`slider`,
`toggle-group` if absent); docs: ADR `docs/adr/0014-rimessaggio-manutenzioni-m9.md` (Italian, "proposta": §5, §6,
§7.3, §9.3–9.4, §10, §16), invariants §13, `docs/architecture.md` §2/§10 rows. Ends with `make gen && make check`
green.

### 15.2 Builders (parallel, exclusive files)

| Builder | Owns |
|---|---|
| **STORAGE** | `internal/domain/optimizer/**` (bodies, `rng.go`, `internal/fixture/**`, tests, benchmark); `internal/domain/storage/**`; `internal/domain/contract/storage.go` (+ tests) and `contract/pricing.go` (`PricingType` storage only), kind filters for storage in `contract/{expiry,read}.go`; `internal/domain/fit/**` + `web/shared/src/map/fit.ts` + `fit.parity.test.ts` + vectors (`testdata/`); `internal/domain/presence/service.go` (offer `hauled_out`/`launched` for storage allocations; the API already accepts them); `internal/store/{storage.go,scenarios.go,storage_movements.go,storage_invariants.go}`; `internal/http/admin/{storage.go,storage_scenarios.go}` + tests; `internal/app/seed_storage.go` (shed/yard storage contracts, one scenario); `internal/acceptance/m9_storage_test.go`; invariant statuses INV-06/14/15/16 |
| **PLANNER** | `web/admin/src/features/storage/**`; `web/admin/src/features/phone/storage/**`; storage section in contract detail via `features/storage/public.ts` mounted from the contract page slot (CONTRACT leaves an import line); `web/shared/src/api/hooks/storage.ts`; `web/shared/src/map/lenses/accessibility.ts`; `internal/domain/report/**` (storage/maintenance/utilities reports, tiles, `calc.Qualifying` storage) + `internal/store/report_{storage,maintenance,utilities}.go`; `web/admin/src/features/reports/**` and `features/dashboard/**` additions; i18n values `storage` (+ report/tile keys in `reports.json`, `dashboard.json`); Vitest §14.5 PLANNER; `e2e/tests/storage-*.spec.ts`, `e2e/fixtures/m9/storage*`; `internal/acceptance/m9_reports_test.go` (tiles = Σ drill on seed) |
| **UTILITIES** | `internal/domain/maintenance/**` (incl. `due/`); `internal/domain/utility/**` (incl. `consume/`); `internal/domain/deadline` source hook for `maintenance_task` (one file `deadline/maintenance.go`); `internal/store/{maintenance.go,equipment.go,utilities.go,utility_invariants.go}`; `internal/http/admin/{maintenance.go,equipment.go,utilities.go}` + tests; `internal/app/seed_utilities.go`; notify template `maintenance_due` (it, en); `web/admin/src/features/{maintenance,utilities}/**`; `web/admin/src/features/phone/{maintenance,readings}/**` (dock tab via `DockSlots.extraTabs`); `web/shared/src/api/hooks/{maintenance,utilities}.ts`; `web/shared/src/map/lenses/maintenance.ts`; `web/shared/src/offline/allowlist.ts` (activities slot) + tests; i18n values `maintenance`, `utilities`; Vitest §14.5 UTILITIES; `e2e/tests/{maintenance,phone-maintenance,phone-readings,utilities-charging}.spec.ts`; `internal/acceptance/m9_{maintenance,utilities,offline}_test.go`; invariant statuses INV-90/91/92, INV-71 |

### 15.3 Parallel-phase rules

- OpenAPI, migrations, models, state rows, event constants, signatures are frozen. Changes go in the builder's
  report to the orchestrator, never as edits.
- PLANNER builds against the generated client with MSW fixtures until STORAGE lands; it calls only operations of
  the contract. PLANNER's report SQL reads the frozen M9 tables directly (read-only, INV-61).
- UTILITIES uses `occupancy.AllocateTx`, `occupancy.Search`, `charge.CreateTx`, `charge.ItemPrice`,
  `contract` read helpers as they are; it never edits `occupancy`, `charge`, `contract` files. STORAGE never edits
  `charge`. If a needed hook is missing, report it.
- Only STORAGE touches `fit` and its TS twin; PLANNER and UTILITIES consume `checkFit` read-only.
- Each builder ends with its packages' tests (both dialects for integration), `golangci-lint` clean on its files,
  `npm --prefix web run lint && npm --prefix web run typecheck` (web owners) and the `invariants-guardian`
  subagent (STORAGE, UTILITIES).

### 15.4 Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
go test -count=1 ./internal/domain/optimizer/... ./internal/domain/fit/... ./internal/domain/maintenance/... ./internal/domain/utility/... ./internal/domain/report/...
go test -count=1 ./internal/domain/optimizer/... ./internal/domain/utility/consume/... ./internal/domain/maintenance/due/... -run 'TestRapid_' -rapid.checks=2000
go test -count=1 -run 'TestArrange_Acceptance2000m2|TestArrange_Deterministic' -v ./internal/domain/optimizer/...   # < 3 s per fixture, no -race
go test -run '^$' -bench 'BenchmarkArrange_2000m2_40' -benchtime 5x ./internal/domain/optimizer/...
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/store/... ./internal/domain/storage/... ./internal/domain/contract/... ./internal/domain/occupancy/... ./internal/domain/maintenance/... ./internal/domain/utility/... ./internal/domain/charge/... ./internal/domain/report/... ./internal/http/... ./internal/backup/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -timeout 30m ./internal/acceptance/... ./internal/domain/... -run '^(TestConcurrency_(ScenarioApply|StorageCreateAutoPlace|UtilityChargeOnce|MaintenanceScheduleVsAllocation|CellAllocationSurface|LayoutSaveSingleWinner)|TestScenarioApply_AllOrNothing)$'
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M9|TestAcceptance_M7|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII|TestSSE_PayloadHasNoPII'
make test-db && make test-race
npm --prefix web run test
make e2e            # storage-planner, storage-scenarios, storage-contract, storage-phone, maintenance, phone-maintenance, phone-readings, utilities-charging, a11y, offline-queue
make licenses && scripts/agents/check-naming.sh
```

Manual check (final report): `make seed && make dev`; open the shed planner, drag a boat into a blocking spot and
see the exit warning, press Preset, open as scenario, compare with real, apply; record a reading on the phone in
airplane mode and reconnect; complete a maintenance task offline.

## 16. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Storage contracts reuse the mooring state table (`StorageTransitions` = same rows); no renewal campaigns in M9. |
| D2 | Storage price = contract type `storage` for every term; covered/uncovered is the area `price_class`. Seasonal end = season window of the active `storage` list. |
| D3 | Creating a storage contract needs `storage.manage` (not `contracts.write`) so `yard_mgr` can do it (26.1 "Rimessaggio"). Accounting/activation paths keep their permissions. |
| D4 | Every storage contract occupies a concrete position from creation; without a position given, `PlaceOne` chooses it; no space = 409. "Da collocare" = tray items in a session or scenario (allocations from other resources), not allocation-less contracts. |
| D5 | Exit date = planned launch or contract end; arrival = planned haul-out or start; other kinds on surfaces use their nights. |
| D6 | Fit gains `support_not_allowed` (never overridable: physical) and `weight_exceeds` (overridable, dimensional); weight per cell = displacement / hull cells (without margin). |
| D7 | Accessibility: translating rectangle on cell steps, rotation in place when the bounding square is free; goal = a whole side of the box on a gate on the grid border (door ≥ beam + margin). Exits before arrivals on the same day; same-day extraction order is greedy nearest-to-gate. |
| D8 | Blocking set = removal by gate distance then reverse re-insertion pruning (heuristic minimal, documented). |
| D9 | Arrivals are checked too (entry path) and rank after exits in the objective; they are warnings, never errors. |
| D10 | Objective order: unplaced, exits blocked, arrivals blocked, moves (vs real), −largest free rectangle on the start night. |
| D11 | Optimizer budget = iterations (deterministic) plus a caller deadline via `context` (default 2.5 s, setting `storage.optimize_budget_ms` 500–10,000); determinism is guaranteed when the deadline does not cut. Own PRNG; default seed = FNV-1a of the scenario id. |
| D12 | Grids above 40,000 cells are coarsened by an integer factor for the optimizer only; results are re-validated on the fine grid. Max 300 items per run. |
| D13 | Rotations 0/180 and 90/270 are the same rectangle: not counted as moves; the optimizer emits 0 or 900 unless the current value is equivalent (kept). |
| D14 | Scenario = working copy (`scenario_items`) + recomputed `scenario_moves`; overlaps allowed while editing, blocking apply. Stale = any base version changed; shown on read, enforced at apply. |
| D15 | Apply uses `occupancy.LayoutTx` as the single primitive (10.5); the full conflict list comes from a validation under the same locks before writing; DB keys stay authoritative. `FromNight = max(from_on, today)`. |
| D16 | Fault injection uses test-only DB triggers, not a production hook. |
| D17 | Movement worklist only from scenario apply (single planner moves keep the M3 undo log); equipment hint is a text enum until M10. |
| D18 | Movement completion is online-only (not in the 29.2 list); readings, maintenance reports and outcomes are offline. |
| D19 | Readings permission = `presence.write` (26.1 groups "letture" with arrivals); meter configuration = `marina.configure`; consumption review and charging = `charges.manage`. |
| D20 | Meter ↔ one resource for automatic charging; shared sockets are left unassigned for manual assignment. |
| D21 | Consumption nights = `prev.read_on … cur.read_on − 1`; split by nights with largest remainder in milli; unoccupied nights are `unassigned` (never charged). |
| D22 | Utility charging mode default `with_installment` (charged on the day the next instalment charge is issued, same due date); transits at departure or after the last night; one `utility_billings` origin per run and contract. |
| D23 | Charged intervals are frozen; late readings inside them are `out_of_order`; corrections become linked adjustment charges, never edits (INV-51). |
| D24 | Maintenance occupancy only on berths and land resources (structures via their berths); dynamic areas get flags, not cells, in M9. |
| D25 | `equipment` table created in M9 (registry only); M10 adds slots and work-order usage. Cycle/hour periodicity reads `hours`/`cycles` meters. |
| D26 | Plan-generated tasks are unique per `(plan, element, due_on)`; tasks also produce `deadlines` rows (`maintenance_task`) for the M4 deadline list and notifications. |
| D27 | Completing a task does not release maintenance nights (offline-safe); release and cancel are online office actions. |
| D28 | New SSE topics `storage.changed`, `maintenance.changed`, `utilities.changed`; new events listed in §15.1; payloads ids and enums only. |
| D29 | Owner (`owner` kind) nights have no contract to charge in M9: their consumption is `unassigned` and can be assigned manually to a contract. |
| D30 | Storage report m² uses usable cells × cell² as available surface and footprint (with margins) as occupied; hull m² is reported alongside. The M7 occupancy tile stays on water maps. |
