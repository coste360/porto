# M3 — Occupazione, transiti e mappa operativa: work order

Status: Plan phase output. This plan is authoritative for the M3 builders. If it conflicts with `docs/spec/`, the
spec wins: stop and report the conflict to the orchestrator instead of guessing. Open decisions of
`docs/spec/33-decisioni-aperte.md` are not decided here (none is due by M3; D12 "seconda fila" stays excluded).
§13 lists every detail this plan decided where the spec says nothing. The state tables of §4 and the invariant
queries of §6 are normative: tests are derived from them.

Sources: spec 05, 06 (§6.3, §6.5), 07 (§7.4, §7.5, §7.6, §7.8), 09 (all), 10 (all), 11 (§11.2 measured
dimensions, §11.5 insurance), 12 (as consumed through `pricelist.Service.Quote`), 13 (§13.1, §13.2, §13.3 steps 1–2,
§13.4 base, §13.5), 23 and 13.6–13.9 (only to know what is out), 26.1, 27 (§27.2–27.4), 29 (§29.1, §29.2 to know what
is out), 31.3, 32 (§32.1, §32.3 M3 and M4+); `docs/invariants.md` (INV-01..09, INV-20, INV-30, INV-70, INV-71,
INV-73); ADR 0001, 0002, 0003, 0004, 0008; `docs/m0-plan.md`, `docs/m1-plan.md`, `docs/m2-plan.md`,
`docs/architecture.md`; `.claude/rules/*.md` (especially `occupancy.md`, `geometry.md`, `go-backend.md`,
`migrations.md`, `api-contract.md`, `frontend.md`, `security.md`); skills `new-endpoint`, `new-migration`,
`new-domain-event`, `new-map-element`, `shadcn`.

## 0. Ground rules for every builder

Everything in `docs/m0-plan.md` §0, `docs/m1-plan.md` §0 and `docs/m2-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001). The closest analogues are
   `../arenile/internal/domain/inventory` (rows per resource and date, `ON CONFLICT DO NOTHING`, expired-hold reclaim),
   `../arenile/internal/domain/availability` (assign, find spot, segments), `../arenile/internal/domain/booking`
   (states, plan, moves, swap, move undo, releases, jobs), `../arenile/internal/domain/findspot`,
   `../arenile/internal/store/sqlitegate*` and `../arenile/web/admin/src/features/{operations,planning,bookings}`.
   Read them to see how a problem was solved (row plan, conflict re-read, lock order, undo window, SSE invalidation,
   drag preview). Then write Porto's own code for nights, cells and vessels. Never copy files, code, comments or
   tests. Never modify anything under `../arenile`. Arenile occupies spots per date and half day with no geometry;
   Porto occupies berths, **cells** and land resources per **night** with fit checks: nothing carries over line by
   line.
2. shadcn/ui components only through the official CLI, run in `web/shared` (CONTRACT, §11.1). Tailwind on theme
   tokens, no custom CSS, no `style={}`; SVG sizes/positions are attributes.
3. Do not `git commit`; no destructive git commands; no `rm -rf`. The orchestrator commits.
4. Edit only files you own (§11). To change a file someone else owns, write the request in your final report.
5. Code, identifiers and comments in English. User-facing strings through i18n keys, Italian source, English
   required. Never log or emit names, emails, phones, fiscal codes, registration numbers, notes typed by staff,
   photos, tokens. Events and SSE carry ids, enums, dates and counts only (INV-70).
6. **No floats in domain logic.** Lengths `geo.Cm`, money `money.Cents`, ratios integer basis points (`_bp`),
   nights `calendar.Date`. The web keeps integers in state; `fit.ts`/`footprint.ts` use integer arithmetic only.
7. **Pure packages**: `internal/domain/fit` and `internal/domain/occupancy/engine` (plan, search, compaction,
   conflict analysis) import no `store`, no `clock`, no `time.Now`, no `math/rand`, no I/O. depguard `pure-engines`
   and `pure-engines-no-rand` list them (CONTRACT adds `occupancy/engine`; `fit` is already listed). "Today" and
   "now" are explicit inputs.
8. **INV-01 is a database guarantee.** Occupancy rows are inserted with `ON CONFLICT DO NOTHING` and the inserted
   count is compared with the plan; a shortfall re-reads the conflicts and returns `*occupancy.ConflictError`
   (`ErrResourceTaken`), which rolls the transaction back. No "check then insert" anywhere. A pre-check (search,
   drag preview, `checkPlacement`) is advisory only.
9. **API first.** CONTRACT writes `api/openapi.yaml` and runs `make gen`. Builders never edit generated files. A
   builder needing a contract change asks the orchestrator, who reruns CONTRACT.
10. Every state change of contracts, transits and quotes goes through `internal/domain/contract` (INV-30) inside
    `store.RunInTx`, with an `audit_log` row and an outbox event in the same transaction. Every allocation change
    goes through `internal/domain/occupancy` primitives inside the caller's transaction. The first line of every
    exported service method is `p.Authorize(ctx, db, marinaID, perm)`; tx-level primitives (`...Tx`) take an
    already-authorised `store.MarinaScope` and are never called from handlers.
11. Every query is scoped by `store.MarinaScope`. A resource, allocation, contract or vessel of another marina is
    `ErrNotFound` (404). Occupancy rows carry `marina_id` and composite FKs keep them in the marina.
12. Time: `clock.Clock` injected; nights are computed in the marina timezone (`marinas.timezone`) through one helper
    `occupancy.Today(clock, tz) calendar.Date`. Tests use `clock.Fake`.

---

## 1. Scope

### 1.1 In M3 (spec 32.3 M3)

**Occupancy engine (10)**
- Three DB-enforced families: `berth_occupancies` `PRIMARY KEY (berth_id, night)`, `cell_occupancies`
  `PRIMARY KEY (area_id, cell, night)`, `land_occupancies` `PRIMARY KEY (resource_id, night)` (§5.2). Berths and
  buoys share `berth_occupancies`; dynamic areas (linear and surface) use cells; land resources use land rows.
- `allocations` (positioning: contract line, vessel, resource, placement, period, kind, expiry, fit override,
  moved-from chain) with rows written by the pure plan `Footprint × nights − released nights` (INV-03).
- Kinds produced in M3: `hold`, `contract`, `transit`, `block`. `owner` (M4 rights) and `maintenance` (M9) exist in
  the CHECK and in the lenses but have no producer.
- Pure `fit.Check` (10.2) with blocking/warnings, fill ratio, `fit_override` with `occupancy.override_fit`, reason
  and audit (INV-07). Go ↔ TS parity vectors in `testdata/fit/`.
- Holds with `expires_at` from the clock: quote options (until quote expiry), draft contracts (until
  `draft_hold_days`), desk/unexpected-arrival held transits (`desk_hold_hours`). Lazy reclaim of expired holds at
  conflict time (INV-05) and job `contracts.expire` (1 min).
- **Search and assignment (10.7, 9.6)**: snapshot read with a fixed number of queries, pure solver over berths,
  linear areas (free intervals per night, intersection, best fit, adjacency) and land resources; score; segments
  with at most `max_segments` changes (default 1, desk only); option "consider free the berths whose vessel is out"
  (solutions flagged `requires_release`). Surface areas are allocated with explicit placements only (§13 D7).
- **Closures (10.8)**: per marina/map/structure/group/resource, block new arrivals in Search and in every allocate
  call with `arrival` inside the closure.
- **Moves (10.5)**: single move from a night (today, a date, or only an interval), swap of two vessels, layout
  session save on a dynamic area (batch: move, rotate, add from elsewhere, all or nothing), compaction proposal of a
  linear area, move undo log (last move undoable for 24 h). Lock order by allocation id. `reprice_on_move` policy.
- **Blocks**: manual block of a berth/cells/land resource for a period with mandatory reason.
- **Releases (10.6)**: release nights of a contract allocation (rows removed, `releases` row), automatic
  `resold_allocation_id` when a later allocation takes released nights, revoke (early return) re-occupying only
  free nights, conflicts reported with alternatives. Sources `customer_phone`, `customer_desk`, `dock`, `office`.
- **Presences (10.6, 9.5)**: `arrived`, `departed` (plus `hauled_out`/`launched` accepted by the API for M9 but not
  offered by the UI), current state per vessel (`vessel_presence`), unexpected arrival → held transit on the berth.
- **Day view** `GET maps/{mapId}/day?date=` (9.2) with per-element status, vessel, contract, presence, release,
  hold, fit ratio, deadlines flags and server-computed anomalies; **map occupancy range**
  `GET maps/{mapId}/occupancy?from=&to=` for the planner and the drag preview; **movements of the day**.
- Harbour guard: map saves that delete an element with occupancy rows, or change the cell grid of an area with
  allocations on nights ≥ today, are rejected (`409 in_use`).

**Contracts and transits (13, base)**
- `contracts` with `kind` ∈ `mooring`, `transit` (M3 producers; `storage`, `charter` in the CHECK only), mooring
  `term` ∈ `annual`, `seasonal`, `monthly`, `period`; `contract_lines` (resource lines with allocations, service
  lines); numbering `unique(marina_id, kind, year, number)` via `number_sequences`.
- Mooring contract states in M3: `draft → active → expired | terminated`, `draft → cancelled`; activation =
  manual "signed on paper" confirmation. `awaiting_signature`, `renewed`, `suspended` are in the CHECK only (M4/M5).
- Transit states in M3: `held → confirmed → arrived → departed`, `held → cancelled | expired`,
  `confirmed → cancelled | no_show`; `pending_payment` and `awaiting_approval` in the CHECK only (M5/M8).
  **Confirmation only through the manual driver** `contract.ConfirmTransit(source = manual)` (INV-09), the same
  entry point the M5 payment webhook will call with `source = provider`.
- Stay changes of a transit (extend, shorten, change arrival) with availability on added nights and re-pricing.
- **Quotes with option (13.2)**: `draft → sent → accepted | declined | expired`, up to 3 alternatives
  (`quote_options`, each with 1–2 placements = segments and its own price snapshot), optional hold per alternative
  until the quote expiry; "sent" is a manual mark in M3 (no email, no PDF). Accept from the panel runs 13.3 steps
  1–2: re-check/convert the hold, create the contract (`draft` or `active`) or the transit (`held`, or `confirmed`
  with the manual driver) in one transaction.
- **Inquiries (13.2)** with outcomes; "save search as unserved request" from the incoming-vessel panel.
- **Prices from M2**: every quote option, contract and transit stores `price_snapshot` (`pricing.Snapshot` JSON),
  `engine_version`, `price_list_id`, `total_cents`, `deposit_cents` from `pricelist.Service.Quote`; never
  recomputed silently (INV-20).

**Operational map (9) and panels (31.3)**
- Operational view on the shared renderer with **lenses**: Occupazione, Tipo di contratto, Tipo di imbarcazione,
  Presenze, Scadenze, Adeguatezza, Utenze (power/water from M1 data, no consumption), Anomalie (M3 subset, §8.4).
  Legend = filter with counts, filters, "In scala" and "Schema" draw modes (remembered per user in `localStorage`),
  date selector, quick card with actions by permission, list alternative per structure, SSE live invalidation.
- Vessel silhouettes in scale (`web/shared/src/map/vessel-shape.ts`, 7.8), LOD and culling (ADR 0004), **3 000
  elements fluid** with the blocking `map-perf` Playwright test.
- Drag & drop: berth → berth (immediate after the "from when" dialog), into/inside dynamic areas with the layout
  session (draft, Save, Restore, "Da collocare", axis sliding with 10 cm snap and neighbour snapping, free-space
  indicator, rotation by 90° on surfaces), client preview with the TS `Footprint` + `fit.ts`, server re-check.
- Desk flows: incoming-vessel search panel, transit wizard, quote editor, mooring contract wizard, contract/transit
  detail pages, release, block, presences, inquiries, berth calendar (9.7) with drag to change dates/berth.
- Phone panel `/m/$marinaId/dock` (dock role first): today's arrivals/departures, register arrival/departure with
  note and photo, unexpected arrival, release, block, link to the operational map (responsive).

**Platform**: permission `contracts.read`; events and SSE topics `occupancy.changed`, `presence.changed`,
`contract.changed` (§7.5); SSE message gains `map_id`, `resource_ids`, `nights`; problem slugs (§7.4); migrations
`00005_contracts.sql`, `00006_occupancy.sql` (both dialects); backup registration; meta capabilities; jobs;
demo seed (§10); invariants INV-01, 03, 05..09, 30 to `implemented` (§9.6).

### 1.2 Explicitly out of M3

| Out | Milestone | Note for M3 code |
|---|---|---|
| Quote → contract automatic procedure steps 3–5 (instalment plan, document from template, signature request, first charge), magic-link acceptance, quote email/PDF | M4 | `acceptQuote` runs steps 1–2 only; `quotes.sent_at` is a manual mark; `contract.Activate` is the "signed on paper" path M4 will call after signature. |
| `awaiting_signature`, `renewed`, renewals (simple, mass, tacit), addenda, change of vessel with pro rata, termination refund/penalty, waiting list, charter contracts and fleets, group moves | M4 | States exist in the CHECK; transitions return `409 invalid_transition`. Termination in M3 only truncates allocations. |
| Instalments and scadenzario (INV-31) | M4 | `deposit_cents` is stored from the snapshot, nothing is scheduled. |
| Berth rights, `owner` occupancy, management mandates, release credits (`credit_cents`) | M4 | `credit_cents` column defaults 0; lens "Diritti d'uso" not shown. |
| Charges, payments, cash, payment links, `pending_payment`, "incassa" action, lens "Stato pagamenti", suspension for arrears | M5 | Manual-driver confirmation records `confirmation_ref` text and `pay_at_departure`; no money moves. |
| Desktop edition, offline queue of presences (29.2), QR scan of transits, Devices page | M6 | Presence POSTs accept `Idempotency-Key` already; no IndexedDB queue in M3 (INV-71 stays planned). |
| Reports (lost demand, waste, occupancy), `occupancy_archive` job | M7 | Inquiries and allocations carry the data. |
| Online booking, online holds with `PORTO_HOLD_TTL`, online quotas per group (10.8), `awaiting_approval`, `BookingChannel`, public search | M8 | Search takes `channel`; only `desk` and `api` are accepted in M3. `TestCheckout_*` stay planned. |
| Dry storage contracts, planner, scenarios, optimizer, supports/cradles on surfaces, hauling/launching moves and travel-lift requests, accessibility of exits | M9 | Surface areas accept explicit placements; `hauled_out`/`launched` presence events are stored but not offered. |
| Maintenance occupancy and lens | M9 | Kind `maintenance` exists. |
| Yard (lens "in cantiere"), rentals (`rental_slots`) | M10 / M11 | |
| Sensors, sensor lens, sensor-driven release proposals, `automatic_after_days` releases | M12 | `release_policy` is stored; M3 releases are always created by staff. |
| Go mirror of the vessel silhouette (prints with vessels), printing the operational map | M4 (with the PDF renderer) | M3 silhouettes are TS only; "Stampa mappa" keeps the M1 print page. |
| AI anywhere (ADR 0008) | never | |

---

## 2. Packages

```
internal/domain/fit/                    PURE  Check, Vessel/Target/Rules/Result, fill ratio, issue codes, parity runner
internal/domain/occupancy/engine/       PURE  plan rows (Footprint × nights − released), free intervals, search solver,
                                              scoring, segments, compaction, conflict grouping, day-view anomalies
internal/domain/occupancy/              service: tx primitives (Allocate, Convert, Truncate, Cancel, Extend, Move,
                                              Swap, ApplyLayout, Release, Revoke, Block, ExpireHolds), Search, Check,
                                              DayView, MapOccupancy, Closures, Settings, Undo, harbour guard
internal/domain/contract/               quotes, contracts, transits, numbering, state machines, price snapshots,
                                              inquiries, jobs (contracts.expire)
internal/domain/presence/               presence events, vessel_presence, unexpected arrival, movements of the day
                                              (calls contract tx hooks for transit arrived/departed)
```

Import graph (enforced by review, cycles are compile errors): `presence → contract → occupancy → {fit,
occupancy/engine, layout, store}`; `contract → pricelist` (Quote only); `occupancy` never imports `contract`.
`http/admin` imports all four services.

### 2.1 `internal/domain/fit` (pure, OCCUPANCY-ENGINE; CONTRACT writes the types)

```go
type Vessel struct {
    LOA, Beam      geo.Cm          // effective: measured when dimensions_source = measured (11.2)
    Draft, AirDraft *geo.Cm        // nil = unknown → check skipped
    Hull           string          // vessels.hull_type
    PowerRequiredA *int64
    InsuranceUntil *calendar.Date  // latest expires_on of a valid insurance_policy document; nil = none on file
}
type TargetKind string // "berth" | "linear_area" | "surface_area" | "land"
type Target struct {
    Kind             TargetKind
    MaxLOA, MaxBeam  geo.Cm        // berth max_loa/max_beam; area max_loa/max_beam (0 = unlimited); land length/width
    Length, Width    geo.Cm        // berth length/width (fill ratio); land length/width
    Depth            *geo.Cm       // berth depth_cm; area: min depth of the occupied interval (caller computes from depth_profile)
    MaxAirDraft      *geo.Cm       // berth max_air_draft_cm; surface max_height_cm
    MultihullOK      bool          // berth attribute tag "multihull_ok"; areas: true
    OverhangAllowed  bool
    PowerA           *int64
    LandKind         string        // land only: parking|box|storeroom|locker|cradle|trailer_spot|custom
}
type Rules struct {
    LOATolerance     geo.Cm  // harbor_settings.loa_tolerance_cm
    SafetyClearance  geo.Cm  // occupancy_settings.safety_clearance_cm (default 30)
    DepthWarnMargin  geo.Cm  // 10
    WasteThresholdBP int64   // occupancy_settings.waste_threshold_bp (default 6000)
}
type Issue struct{ Code string; Limit, Actual geo.Cm }   // codes below
type Result struct {
    OK        bool     // no blocking issue
    Blocking  []Issue  // sorted by code
    Warnings  []Issue
    FillBP    int64    // vessel LOA×Beam / (Length×Width) in bp, half-even; 10000 for areas; 0 for land non-boat kinds
    Overridable bool   // every blocking code is dimensional
}
func Check(v Vessel, t Target, lastNight calendar.Date, r Rules) Result
```

Blocking codes: `loa_exceeds` (LOA > MaxLOA + LOATolerance), `beam_exceeds` (Beam > MaxBeam), `draft_exceeds`
(Draft + SafetyClearance > Depth), `air_draft_exceeds`, `multihull_not_allowed` (hull ∈ catamaran, trimaran and not
MultihullOK), `vessel_invalid` (LOA or Beam ≤ 0, not overridable). Land: `parking|box|storeroom|locker|custom` never
block; `cradle|trailer_spot` use LOA/Beam vs Length/Width + tolerance. Warnings: `fill_below_threshold`
(FillBP < WasteThresholdBP, berths only), `depth_near_limit` (Draft + clearance within DepthWarnMargin of Depth),
`power_insufficient` (PowerA < PowerRequiredA), `insurance_missing`, `insurance_expires_in_period`
(InsuranceUntil < lastNight), `overhang` (LOA > MaxLOA within tolerance and not OverhangAllowed). All dimensional
blocking codes are overridable; overlap is never a fit issue (it is INV-01).

Parity: `testdata/fit/cases.json` `{"version":1,"cases":[{"name","vessel":{…},"target":{…},"last_night","rules":{…},
"expect":{"ok","blocking":[codes],"warnings":[codes],"fill_bp"}}]}` with ≥ 60 hand cases (each code, each boundary
±1 cm, nil fields, every land kind) plus 300 seeded cases from `go run ./internal/domain/fit/internal/genvectors
-seed 1 -n 300` (committed; `TestFit_VectorsUpToDate` fails on drift). Go `TestFit_ParityVectors`; TS
`web/shared/src/map/fit.parity.test.ts` reads the same file.

### 2.2 `internal/domain/occupancy/engine` (pure, OCCUPANCY-ENGINE; CONTRACT writes `types.go`)

```go
type Family string            // "berth" | "cell" | "land"
type ResourceType string      // "berth" | "dynamic_area" | "land_resource"
type Row struct{ Resource string; Cell int; Night calendar.Date } // Cell = 0 for berth/land
type Placement struct{ X, Y geo.Cm; Rotation geo.Angle }          // layout.Placement in M3 units
type AllocSpec struct {
    ResourceType ResourceType; ResourceID string
    Placement    *Placement      // areas only
    First, Last  calendar.Date   // inclusive nights
    Released     []NightRange    // excluded nights (sorted, disjoint)
}
func Plan(spec AllocSpec, area *layout.Area, vessel layout.Vessel) ([]Row, error) // Footprint × nights − Released; sorted
func Nights(first, last calendar.Date) int
// Search
type Snapshot struct {
    Today     calendar.Date
    Berths    []BerthCand; Areas []AreaCand; Lands []LandCand   // with fit inputs, groups, transit-dedicated flag, online flag
    Busy      map[string][]NightRange          // berth/land id → occupied nights (any kind, expired holds excluded)
    AreaBusy  map[string]map[calendar.Date][]Interval // linear area → night → occupied cell intervals [first,last]
    OutOK     map[string]OutCand                // berth id → contract allocation whose vessel is out for the period
    Closures  []Closure
    Previous  map[string]string                 // vessel id → last resource id (continuity)
}
type Request struct {
    Vessel fit.Vessel; VesselID string; Arrival, Departure calendar.Date
    Purpose string // "transit" | "contract"
    ResourceTypes []ResourceType; Prefs Prefs; Channel string; MaxSegments int; ConsiderOut bool; Limit int // default 20
}
type Solution struct {
    Key      string                 // stable: resource ids + x + nights
    Segments []Segment              // 1..MaxSegments+1, contiguous, cover [Arrival, Departure)
    Score    int64                  // lower is better
    Fit      fit.Result             // of the worst segment
    RequiresRelease *OutCand        // when the berth is taken by a contract whose vessel is out
}
type Segment struct{ ResourceType ResourceType; ResourceID string; Placement *Placement; First, Last calendar.Date }
func Solve(s Snapshot, r Request, rules fit.Rules) []Solution
func FreeIntervals(cellCount int, busy [][]Interval) []Interval // intersection over nights of the complements
func Compact(area layout.Area, placements []Placed, gapTarget geo.Cm) []Proposal  // §3.6
func Anomalies(in AnomalyInput) []Anomaly                       // §8.4
```

Scoring (10.7 step 4), integers: `score = waste_cm2/100 + 50_000·[purpose=transit ∧ ¬transit_dedicated] +
20_000·pref_misses + 10_000·[previous ≠ resource] + 200_000·(segments−1) + 300_000·[requires_release]`, where
`waste_cm2 = Length×Width − LOA×Beam` (berths), the free-interval slack in cm × beam (linear areas: interval length −
footprint width, times 100), land `Length×Width − LOA×Beam` for cradles else 0. Ties → resource code, then x. Linear
best fit: for each free interval that contains the footprint width, candidate x = interval start snapped up to 10 cm
(`≥ margin/2` at the area start) and interval end − width snapped down (adjacent to the next vessel); keep the
smallest interval first. Segments: only when no single-resource solution exists and `MaxSegments ≥ 1`, split at
every night boundary where a candidate frees/blocks, at most `MaxSegments` changes, both segments must pass fit.

### 2.3 `internal/domain/occupancy` (OCCUPANCY-ENGINE; CONTRACT writes skeleton, types and sentinels)

Tx primitives used by `contract` and by the occupancy service itself (all take `tx bun.IDB`, `scope`, `now
time.Time` from the caller's clock, and return the allocations they wrote so the caller can emit events):

```go
type NewAllocation struct {
    ContractLineID *string; VesselID *string; Kind Kind // hold|contract|transit|block
    Spec engine.AllocSpec; ExpiresAt *time.Time; FitOverride *FitOverride; BlockReason string
    MovedFromID *string; UserID string; Arrival bool // Arrival=true applies closures to Spec.First
}
func (s *Service) AllocateTx(ctx, tx, scope, now, a NewAllocation) (*store.Allocation, error)
func (s *Service) ConvertTx(ctx, tx, scope, now, allocIDs []string, kind Kind, expiresAt *time.Time) error // hold→contract/transit, keeps rows
func (s *Service) TruncateTx(ctx, tx, scope, allocID string, fromNight calendar.Date) error  // drops nights ≥ fromNight
func (s *Service) CancelTx(ctx, tx, scope, allocID string, status AllocStatus) error         // cancelled|expired|superseded; deletes rows
func (s *Service) ResizeTx(ctx, tx, scope, now, allocID string, first, last calendar.Date) error // extend/shorten in place
func (s *Service) ExpireHoldsTx(ctx, tx, scope, now) ([]store.Allocation, error)
func (s *Service) MoveTx(ctx, tx, scope, now, m MoveSpec) (MoveResult, error)                // §3.5
func (s *Service) LayoutTx(ctx, tx, scope, now, l LayoutSpec) (MoveResult, error)            // §3.5
```

Errors (names fixed): `ErrResourceTaken` (`*ConflictError{ResourceType, ResourceID, From, To calendar.Date, Cells
[]int, AllocationIDs []string}`), `ErrFitBlocked` (`*FitError{Result}`), `ErrOverrideForbidden`,
`ErrOverrideReasonRequired`, `ErrPlacementInvalid` (`*PlacementError{Reason: unsnapped|outside_area|blocked_cell|
invalid_vessel|placement_required|placement_not_allowed}`), `ErrClosed` (`*ClosureError{ClosureID}`),
`ErrHoldExpired`, `ErrAllocationNotActive`, `ErrVersionMismatch`, `ErrUndoUnavailable`, `ErrReleaseInvalid`,
`ErrElementInUse`, `ErrNotFound` (wraps `store.ErrNotFound`).

Service methods (with permission) are listed with the API in §7.

---

## 3. Occupancy engine semantics (normative)

### 3.1 Rows and the allocate algorithm (10.3)

```
AllocateTx(a):
  1. resolve resource (same marina, active) → map_id, family, layout.Area for areas
  2. closures: if a.Arrival and a closure covers Spec.First on the resource scope → ErrClosed
  3. fit (unless kind = block): fit.Check(effective vessel, target, Last) → blocking and no override → ErrFitBlocked;
     override present → requires PermOccupancyOverrideFit (checked by the calling service) + non-empty reason
  4. rows := engine.Plan(spec, area, vessel)   // ErrPlacementInvalid on Footprint errors
  5. DELETE expired holds touching the plan:   DELETE FROM <family> WHERE kind='hold' AND expires_at < :now
       AND <resource cols> AND night BETWEEN :first AND :last; mark their allocations status='expired'
       (rows of those allocations outside the range are deleted too: an expired hold is gone as a whole)
  6. INSERT allocations row (status active, version 1)
  7. INSERT <family> rows in chunks of 500 … ON CONFLICT DO NOTHING; sum RowsAffected
  8. inserted < len(rows) ⇒ SELECT the conflicting rows (same keys, other allocation_id) ⇒ *ConflictError ⇒ return
     (the caller's RunInTx rolls back everything, including step 5)
  9. resale: if released nights of another allocation on the same resource intersect the rows → set
     releases.resold_allocation_id (first only) and add the nights to resold_nights
 10. audit row `allocation.create` + outbox `occupancy.allocated` (written by the service that owns the operation:
     occupancy for blocks/moves, contract for contracts/transits/quotes)
```

- Rows carry `marina_id`, `allocation_id`, `kind`, `expires_at` copied from the allocation; `ConvertTx` updates
  both in one statement per family.
- `vessel_id` is NULL only for `kind = block` (CHECK).
- SQLite: every write transaction already goes through the FIFO write queue (`internal/store/sqlitequeue.go`);
  PostgreSQL: concurrent inserts of the same key wait on the first transaction and then do nothing, so step 8
  is exact on both dialects. No `SERIALIZABLE` needed; `IsSerializationFailure` is not expected and maps to 409.
- Volumes: annual contract on a berth 365 rows; on a linear area of 50 cm cells a 4 m beam + 50 cm margin =
  10 cells × 365 = 3 650 rows. Day-view queries hit `(marina_id, night)` indexes.

### 3.2 Allocation statuses

`active` (has rows for every non-released night of [first_night, last_night]), `cancelled` (removed before use,
no rows), `expired` (hold lost, no rows), `superseded` (moved entirely to another allocation, no rows). Truncation
keeps `active` with a smaller `last_night`. Only `active` allocations have rows (INV-03, Q2 of §6).

### 3.3 Holds (INV-05)

| Origin | `expires_at` | Converted by | On expiry |
|---|---|---|---|
| Quote option with `hold = true` | end of `valid_until` (23:59:59 marina time → UTC) | `acceptQuote` → `contract`/`transit` (or kept as `hold` with draft expiry for a draft contract) | job: allocation `expired`, quote → `expired` |
| Draft mooring contract | `now + draft_hold_days` (default 7) | `activateContract` → `contract` | job: allocation `expired`, contract stays `draft` with `hold_lost = true`; activation re-allocates or fails 409 |
| Held transit (desk, unexpected arrival, accepted transit quote) | `now + desk_hold_hours` (default 24) | `confirmTransit` (manual driver) → `transit` | job: allocation `expired`, transit → `expired` |

Lazy reclaim (step 5 of §3.1) marks the allocation `expired` but never changes contract/quote/transit status: the
job `contracts.expire` reconciles every minute (contracts whose holds are expired or missing), writing audit and
outbox. `TestOccupancy_ExpiredHoldIsReclaimed` proves a sale succeeds on an expired-but-not-yet-cleaned hold.

### 3.4 Releases and revoke (10.6)

- `Release(allocation, from, to, source)`: allocation must be `active`, kind `contract`, `from ≥ today`,
  `[from,to] ⊆ [first,last]`, not overlapping an active release of the same allocation; rows of those nights are
  deleted; `releases` row `active`. Transits cannot be released (use shorten). Event `release.created`.
- Resale: §3.1 step 9; event `release.resold` (once per release, first resale).
- `Revoke(release, from?, to?)` (early return): for each night of the range not yet re-occupied, re-insert the
  allocation's rows; nights already resold → `409 resource_taken` with the conflicting nights and, in `details`,
  `alternatives` from a search for those nights (the office then moves the vessel, §10.6 last bullet). Partial
  revoke splits the release (`to_night` shortened, a new release for the tail). Revoke never produces double
  occupancy because re-insertion uses §3.1 step 7.
- INV-03 accounts for releases: expected rows = Plan(spec with `Released` = active releases of the allocation).

### 3.5 Moves, swap, layout session, undo (10.5, 9.4) — INV-06

`MoveSpec{AllocationID, ExpectedVersion, Target{ResourceType, ResourceID, Placement}, FromNight, ToNight *Date,
FitOverride, Reprice *bool}`:

1. `SELECT … FOR UPDATE` (PostgreSQL; SQLite is serialised) every allocation involved **ordered by id**; check
   `status = active`, `version = ExpectedVersion` (else `ErrVersionMismatch` → 412).
2. `FromNight` default today; must be within `[first, last]` and ≥ today (past nights never move).
3. Delete rows from `FromNight` (to `ToNight` when given) of the source; if `FromNight = first` the source becomes
   `superseded`, else truncated to `FromNight − 1`.
4. New allocation on the target for `[FromNight, ToNight ?? last]` with the same contract line, vessel, kind,
   expiry and the source's active releases intersected; `moved_from_id = source`. With `ToNight` (interval move) a
   third allocation returns to the source resource and placement for `[ToNight+1, last]` with
   `moved_from_id = new`.
5. Fit on the target (override as §3.1). Insert rows (§3.1 steps 7–8): any conflict fails the whole move.
6. Price effect: `pricelist.Quote` for the remaining nights on old and new resource (computed **before** the write
   transaction by the contract service, §4.6); applied only when the contract policy `reprice_on_move` is true (or
   `Reprice` explicitly set by a `contracts.write` user), else only reported and audited (`price_delta_cents`).
7. `move_undo_log` row (all touched allocation ids with their pre-move `status`, `last_night`, `version` and the new
   allocation ids), expires `now + 24 h`; audit `allocation.move`; outbox `occupancy.moved`.

**Swap** `Swap(a, b, FromNight)`: one transaction, lock a and b by id order, delete both rows from `FromNight`,
create the two new allocations crossing resources (placements: for areas the other vessel's x/y/rotation; if the
footprint does not fit → `ErrPlacementInvalid` with `vessel_id`), insert, fit both, one undo entry.

**Layout session save** `ApplyLayout(area, date, FromNight, base: [{allocation_id, version}], ops: [{allocation_id,
placement}…])`: every allocation of the area intersecting `FromNight` must appear in `base` with its current version
(else 412: the draft is stale and is revalidated on the client); ops may also bring allocations from other resources
into the area. All rows from `FromNight` of all touched allocations are deleted **first**, then all new allocations
are inserted, so vessels can exchange places in one save. Any conflict, fit block or invalid placement → whole save
rolls back and the error names the allocation (`details.allocation_id`). Every allocation of the draft must end with
a placement: unplaced vessels make the save fail with `422 validation_failed` (field `unplaced`) (§13 D8).

**Compaction** (pure `engine.Compact`): for a linear area and `[from, to]`, slide every vessel whose allocation
covers the whole range toward the axis start keeping order and the area margin, snapped to 10 cm, so the free space
becomes one contiguous interval at the end; vessels not covering the whole range are fixed obstacles. Returns the
proposed placements; the client shows a preview and applies it through the layout save (one transaction).

**Undo** `Undo(undoID)`: allowed when `now < expires_at`, it is the latest undo entry for each touched allocation,
and every new allocation is still `active` with version 1 and unchanged nights; then the new allocations are
cancelled (`superseded`), sources restored (status, last_night, rows re-inserted through §3.1 steps 7–8; conflict ⇒
`409 undo_unavailable`). Event `occupancy.moved` with `undo = true`. A price applied by the move (D34) is reversed
in the same transaction: each of its adjustments still in the snapshot gets an opposite `move_undo` adjustment,
with audit `contract.reprice`/`transit.reprice` (`undo: true`) and `*.changed` (`repriced`) (INV-27).

### 3.6 Search (10.7) and check

`Search` loads the snapshot with ≤ 8 queries regardless of the harbour size (`store.LoadSearchSnapshot`): active
berths/areas/land of the marina with fit inputs and group ids; harbor settings; occupancy rows of those resources for
`[arrival, departure)` grouped by resource (berth/land) and by `(area, night)` as cell ranges (SQL returns rows; Go
compresses to intervals); presences (for `ConsiderOut`: berth with `contract` rows for all nights, vessel presence
`out` since before arrival and `expected_return_on` ≥ departure or null, contract `release_policy ≠ never`); closures;
previous resource of the vessel. `engine.Solve` then runs in memory. Performance budget: 800 berths + 1 km of linear
areas, 30 nights, ≤ 150 ms on SQLite (`BenchmarkSearch_Demo`).

`Check(placement, vessel, period, allocation_id?)` (drag drop and dialogs): fit + `Plan` + read of conflicting rows
(ignoring the moving allocation's own rows) → `{ok, fit, conflicts:[{allocation_id, vessel_id, from, to, cells}],
price_effect?}`. Advisory only.

### 3.7 Harbour guard

`occupancy.Guard` implements `harbor.OccupancyGuard` (interface added by CONTRACT to `harbor.Deps`):
`CheckMapChange(ctx, tx, scope, change MapChange) error` called by `applyDocument` before the write: for each
berth/area/land resource being deleted, or area whose `kind`, `cell_cm`, `margin_cm`, `mooring_mode`, `from_cm`,
`to_cm`, `outline`, `blocked_cells`, `grid_rotation_dd` changes, if any allocation of that resource is `active`
with `last_night ≥ today` → `ErrElementInUse` (→ `409 in_use`, `details.references`). Deleting an element with only
past rows is also refused by the FK `ON DELETE RESTRICT` of the occupancy tables (history is kept; deactivate it
instead). Berth dimension changes are allowed; resulting misfits appear as the `vessel_exceeds_limits` anomaly.

---

## 4. Contracts, quotes, transits, presences (normative)

### 4.1 Mooring contract states (M3 transitions; anything else → `409 invalid_transition`)

| From | Action (service method) | To | Allocation effect | Event (public) |
|---|---|---|---|---|
| – | `CreateContract(activate=false)` | `draft` | `AllocateTx(kind=hold, expires=now+draft_hold_days)` per resource line | `contract.created` |
| – | `CreateContract(activate=true)` | `active` | `AllocateTx(kind=contract)` | `contract.created` + `contract.activated` |
| `draft` | `ActivateContract` ("firmato su carta") | `active` | `ConvertTx(→contract)`; holds lost → re-`AllocateTx` (409 on conflict) | `contract.activated` |
| `draft` | `CancelContract` | `cancelled` | `CancelTx(cancelled)` | `contract.changed` (internal, `change=cancelled`) |
| `active` | `TerminateContract(effective_on, reason)` | `terminated` | `TruncateTx(effective_on)` for each allocation of the contract (allocations starting on/after → `CancelTx`); active releases after `effective_on` closed | `contract.terminated` |
| `active` | job `contracts.expire` when `today ≥ ends_on` | `expired` | none (rows are history) | `contract.changed` (`change=expired`) |

`awaiting_signature`, `renewed`, `suspended` are in the CHECK and in the generated enum; no M3 transition reaches them.

### 4.2 Transit states

| From | Action | To | Allocation effect | Event |
|---|---|---|---|---|
| – | `CreateTransit` | `held` | `AllocateTx(kind=hold, expires=now+desk_hold_hours)` | `transit.held` |
| – | `CreateTransit(confirm={driver:manual,…})` | `confirmed` | allocate `hold` then `ConfirmTransit` in the same tx | `transit.held` + `transit.confirmed` |
| `held` | `ConfirmTransit(source=manual)` (only entry point; M5 adds `source=provider` from the webhook) | `confirmed` | `ConvertTx(→transit, expires=nil)`; hold lost → re-allocate or 409 | `transit.confirmed` |
| `held` | `CancelTransit` | `cancelled` | `CancelTx` | `transit.cancelled` |
| `held` | job (hold expired or missing) | `expired` | `CancelTx(expired)` if still present | `transit.changed` (`change=expired`) + `occupancy.hold_expired` |
| `confirmed` | `CancelTransit` | `cancelled` | `CancelTx` | `transit.cancelled` |
| `confirmed` | `MarkNoShow` (allowed when `today ≥ arrival`) | `no_show` | `CancelTx` | `transit.changed` (`change=no_show`) |
| `confirmed` | presence `arrived` | `arrived` | none | `transit.arrived` |
| `held` | presence `arrived` | `held` (unchanged) | none; on later `ConfirmTransit` the transit goes `held → confirmed → arrived` in one tx (two audit rows, two events) | – |
| `arrived` | presence `departed` on day D | `departed` | if `D ≤ last_night`: `TruncateTx(D)` (nights freed, price unchanged in M3) | `transit.departed` |
| `held`, `confirmed` | `ChangeStay(arrival?, departure?)` | same | `ResizeTx` (added nights must be free: 409 with `alternatives`); with `extension_target` a second contiguous allocation (`moved_from_id`) on another resource for the added nights | `transit.changed` (`change=stay`) + `occupancy.allocated`/`occupancy.released` |
| `arrived` | `ChangeStay(departure)` | same | as above, arrival is frozen | same |

`pending_payment`, `awaiting_approval` are in the CHECK only. `ConfirmTransit` requires `customer_id` (422
`customer_required`) and stores `confirmation_source` (`manual`), `confirmation_ref` (free text, never logged),
`pay_at_departure` (bool), `confirmed_at`, `confirmed_by_user_id`. Permission: `transits.desk`.

### 4.3 Quote states

| From | Action | To | Effect | Event |
|---|---|---|---|---|
| – | `CreateQuote` | `draft` | options priced; `hold=true` options allocate `hold` until the quote expiry | `quote.changed` (internal, `change=created`) + `occupancy.allocated` per hold |
| `draft` | `UpdateQuote` (If-Match; options replaced) | `draft` | holds of removed/changed options cancelled, new ones allocated | `quote.changed` |
| `draft` | `MarkQuoteSent` | `sent` | none (no email in M3) | `quote.sent` |
| `draft`, `sent` | `AcceptQuote(option_id, activate?, confirm?)` | `accepted` | 13.3 steps 1–2: chosen option's holds converted (or allocated now; conflict ⇒ 409 with `alternatives`, nothing created); other options' holds cancelled; contract (`draft`/`active`) or transit (`held`/`confirmed`) created with the option's snapshot | `quote.accepted` + contract/transit events |
| `draft`, `sent` | `DeclineQuote(reason)` | `declined` | holds cancelled | `quote.changed` (`change=declined`) |
| `draft`, `sent` | job at `valid_until` end | `expired` | holds `expired` | `quote.expired` + `occupancy.hold_expired` |

Accepting an expired quote → `409 quote_expired`. A quote has 1–3 options, each with 1–2 placements (segments that
cover `[starts_on, ends_on)` contiguously). `quote.kind` ∈ `mooring` (with `term`), `transit`.

### 4.4 Presences

- `RecordPresence(vessel_id, event, at?, allocation_id?, notes?)` (`presence.write`): `at` default now; `night` =
  local date of `at`. `allocation_id` default = the vessel's active allocation covering that night (or the previous
  night for `departed`). Updates `vessel_presence` (state `in_port` after `arrived`/`launched`, `out` after
  `departed`, `ashore` after `hauled_out`) in the same tx; transit hooks of §4.2 through
  `contract.OnArrivalTx`/`OnDepartureTx`. Arrival of a vessel already `in_port` → `409 invalid_transition`.
- `UnexpectedArrival(vessel_id | new_vessel{name, loa_cm, beam_cm, hull_type, draft_cm?}, target placement,
  departure?)` (`presence.write` + `transits.desk`): creates the vessel when new (registry service, estimated
  dimensions as declared), a `held` transit (customer null, departure default arrival + 1) on the target, and the
  `arrived` presence, in one tx. Fit blocks are reported as 422 unless overridden.
- Photo: `POST presences/{id}/photo` (multipart, ≤ 8 MiB, jpeg/png/webp) stored encrypted through `filestore`
  (purpose `presence_photo`), served with the M1 signed-link mechanism, requires `customers.read`.

### 4.5 Numbering

`number_sequences(marina_id, series, year, next_number)`, series `mooring`, `transit`, `quote`; year = local year of
creation. `UPDATE … SET next_number = next_number + 1 … RETURNING next_number - 1` (insert-or-ignore first). The
unique constraint `contracts_number_uidx (marina_id, kind, year, number)` (and `quotes_number_uidx`) is the guarantee;
display `ORM-2026-0001`, `TRN-2026-0001`, `PRV-2026-0001` (`ContractCode` helper in `contract`).

### 4.6 Prices

- Every pricing call goes through `pricelist.Service.Quote` with: `contract_type` = transit → `transit`; mooring
  `annual|seasonal|monthly` → same; `period` → `monthly` (§13 D10); `arrival/departure` of the stay; `resource`
  = the first segment's resource; `vessel_id`; `customer_id`; `channel`; services of the request; `quoted_on` = today.
- Pricing runs **before** opening the write transaction (it has its own read tx; SQLite write queue must not be held
  while pricing). The write transaction stores the snapshot as given; if the price list changed between the two
  (the snapshot's `price_list_id` is no longer active for the arrival), the write fails `409 conflict` and the
  client retries.
- Stored on quote options, contracts (copied from the accepted option, never recomputed) and transits:
  `price_list_id`, `price_snapshot` (JSON of `pricing.NewSnapshot`), `engine_version`, `total_cents`,
  `deposit_cents`. Stay changes and repricing moves replace the snapshot and record old/new totals in the audit diff.
- `no_price` from the engine is surfaced as 422 `no_price` (M2 slug) on create/accept/stay change; the search returns
  solutions with `price: null` and `price_error` instead of failing.

### 4.7 Contract policy JSON (`contracts.policy`, versioned)

`{"version":1,"reprice_on_move":false,"release_policy":"with_consent"|"never","allow_segments":true}`; defaults from
`occupancy_settings`. Cancellation and renewal policies are M4 (unknown keys rejected).

---

## 5. Data model

### 5.1 Migration `00005_contracts.sql` (both dialects, same names; CONTRACT)

Dates `TEXT 'YYYY-MM-DD'` on SQLite and `DATE` on PostgreSQL (as `price_lists.valid_from`); instants `TIMESTAMP`/
`timestamptz`; JSON `TEXT`/`jsonb`; booleans `INTEGER CHECK IN (0,1)`/`boolean`. Every table has `id` (UUIDv7),
`marina_id` (FK marinas), `created_at`, `updated_at` unless noted, `UNIQUE (id, marina_id)` for composite FKs.

| Table | Columns | Constraints / indexes |
|---|---|---|
| `occupancy_settings` | `marina_id` PK, `safety_clearance_cm` (30, 0..300), `waste_threshold_bp` (6000, 0..10000), `draft_hold_days` (7, 1..60), `desk_hold_hours` (24, 1..168), `quote_validity_days` (7, 1..120), `max_segments` (1, 0..2), `absent_days_threshold` (7, 1..365), `arrival_grace_minutes` (120), `default_arrival_time` ('14:00'), `default_departure_time` ('11:00'), `reprice_on_move` (false), `release_policy` ('with_consent' CHECK never/with_consent), `version` | row created lazily |
| `number_sequences` | `marina_id`, `series` CHECK (mooring, transit, quote, storage, charter), `year`, `next_number` (≥1) | PK (marina_id, series, year) |
| `inquiries` | `channel` CHECK (desk, online, api, external), `source` CHECK (phone, email, desk, public, channel), `purpose` CHECK (transit, contract), `term` null, `vessel_id` null (FK same marina), `customer_id` null, `loa_cm`, `beam_cm`, `draft_cm` null, `arrival`, `departure`, `outcome` CHECK (open, quoted, booked, declined_no_space, declined_size, declined_other, lost), `quote_id` null, `contract_id` null, `notes`, `created_by_user_id`, `version` | idx (marina_id, outcome, arrival); CHECK departure > arrival |
| `quotes` | `number`, `year`, `status` CHECK (draft, sent, accepted, declined, expired), `kind` CHECK (mooring, transit), `term` null CHECK (annual, seasonal, monthly, period), `customer_id` NOT NULL, `vessel_id` NOT NULL, `starts_on`, `ends_on`, `valid_until`, `expires_at` (instant of valid_until end), `channel`, `policy` json, `notes`, `inquiry_id` null, `accepted_option_id` null, `contract_id` null, `sent_at`, `decided_at`, `decline_reason`, `created_by_user_id`, `version` | `quotes_number_uidx` unique (marina_id, year, number); idx (marina_id, status, expires_at); idx (marina_id, customer_id); CHECK (kind='mooring') = (term IS NOT NULL); CHECK ends_on > starts_on |
| `quote_options` | `quote_id` (FK cascade), `seq` (1..3), `label`, `hold` bool, `price_list_id`, `price_snapshot` json, `engine_version`, `total_cents`, `deposit_cents`, `fit_override` json null | unique (quote_id, seq); idx (marina_id, quote_id) |
| `quote_option_placements` | `option_id` (FK cascade), `seq` (1..2), `resource_type` CHECK, `resource_id`, `x_cm`, `y_cm`, `rotation_dd` null, `first_night`, `last_night`, `allocation_id` null (hold) | **created in 00006** (§5.2) |
| `contracts` | `kind` CHECK (mooring, transit, storage, charter), `term` null CHECK (annual, seasonal, monthly, period), `number`, `year`, `status` CHECK (draft, awaiting_signature, active, expired, renewed, terminated, cancelled, suspended, held, pending_payment, confirmed, arrived, departed, no_show, awaiting_approval), `customer_id` null, `billing_customer_id` null, `vessel_id` NOT NULL, `starts_on`, `ends_on` (exclusive), `channel`, `quote_id` null, `price_list_id` null, `price_snapshot` json null, `engine_version`, `total_cents`, `deposit_cents`, `policy` json, `expected_arrival_time` null, `expected_departure_time` null, `notes`, `hold_lost` bool, `activated_at`, `signed_on_paper` bool, `confirmed_at`, `confirmed_by_user_id`, `confirmation_source` null CHECK (manual, provider), `confirmation_ref` null, `pay_at_departure` bool, `cancelled_at`, `cancel_reason`, `terminated_on` null, `terminate_reason`, `created_by_user_id`, `version` | `contracts_number_uidx` unique (marina_id, kind, year, number); idx (marina_id, kind, status, starts_on); idx (marina_id, vessel_id); idx (marina_id, customer_id); CHECK ends_on > starts_on; CHECK kind-status coherence: `kind='transit'` ⇒ status ∈ transit states, else ∈ contract states; CHECK `kind='mooring'` ⇒ term NOT NULL; CHECK `status IN ('confirmed','arrived','departed','no_show')` ⇒ customer_id NOT NULL |
| `contract_lines` | `contract_id` (FK cascade), `seq`, `line_kind` CHECK (resource, service), `item_id` null (FK items same marina), `resource_type` null, `resource_id` null, `qty_milli`, `amount_cents` (snapshot share, informative) | unique (contract_id, seq); idx (marina_id, contract_id); CHECK line_kind='resource' ⇔ resource_type NOT NULL |

FKs to customers, vessels, items, price_lists, quotes, contracts are composite `(x_id, marina_id)` (INV-73).

### 5.2 Migration `00006_occupancy.sql` (both dialects; CONTRACT)

| Table | Columns | Constraints / indexes |
|---|---|---|
| `allocations` | `contract_line_id` null (FK contract_lines same marina, RESTRICT), `vessel_id` null (FK vessels same marina), `resource_type` CHECK (berth, dynamic_area, land_resource), `resource_id`, `map_id` (FK maps same marina), `x_cm`, `y_cm`, `rotation_dd` null, `first_night`, `last_night`, `kind` CHECK (hold, contract, transit, owner, block, maintenance), `status` CHECK (active, cancelled, expired, superseded), `expires_at` null, `fit_override` json null (`{user_id, reason, codes[]}`), `block_reason` null, `moved_from_id` null (self FK), `created_by_user_id`, `version` | idx `allocations_resource_idx` (marina_id, resource_type, resource_id, first_night); idx (marina_id, map_id, status, first_night, last_night); idx (contract_line_id); idx (marina_id, vessel_id, status); idx (marina_id, status, kind, expires_at); CHECK last_night ≥ first_night; CHECK kind='block' ⇔ (vessel_id IS NULL AND block_reason IS NOT NULL); CHECK kind='hold' ⇒ expires_at NOT NULL; CHECK resource_type='dynamic_area' ⇔ x_cm NOT NULL |
| `berth_occupancies` | `marina_id`, `berth_id`, `night`, `allocation_id`, `kind`, `expires_at` null (no id, no timestamps: §13 D3) | **PRIMARY KEY (berth_id, night)**; FK (berth_id, marina_id) → berths **ON DELETE RESTRICT**; FK (allocation_id, marina_id) → allocations ON DELETE CASCADE; idx (marina_id, night); idx (allocation_id); partial idx (expires_at) WHERE kind='hold' |
| `cell_occupancies` | `marina_id`, `area_id`, `cell` (≥0), `night`, `allocation_id`, `kind`, `expires_at` | **PRIMARY KEY (area_id, cell, night)**; FK (area_id, marina_id) → dynamic_areas RESTRICT; FK allocation CASCADE; idx `cell_occupancies_night_idx` (area_id, night, cell) for free-interval reads; idx (marina_id, night); idx (allocation_id); partial hold idx |
| `land_occupancies` | `marina_id`, `resource_id`, `night`, `allocation_id`, `kind`, `expires_at` | **PRIMARY KEY (resource_id, night)**; FK land_resources RESTRICT; same indexes |
| `releases` | `allocation_id` (FK), `contract_id`, `resource_type`, `resource_id`, `from_night`, `to_night`, `status` CHECK (active, revoked), `source` CHECK (customer_phone, customer_desk, customer_link, dock, office, system), `resold_allocation_id` null, `resold_nights` (0), `credit_cents` (0), `notes`, `created_by_user_id`, `revoked_at`, `version` | idx (marina_id, resource_type, resource_id, from_night); idx (allocation_id); CHECK to_night ≥ from_night |
| `vessel_presence` | `marina_id`, `vessel_id`, `state` CHECK (in_port, out, ashore, yard), `since`, `allocation_id` null, `last_presence_id`, `expected_return_on` null, `updated_at` | PK (vessel_id); FK vessel same marina; idx (marina_id, state) |
| `presences` | `vessel_id`, `allocation_id` null, `contract_id` null, `event` CHECK (arrived, departed, hauled_out, launched), `at`, `night`, `source` CHECK (staff, sensor, customer), `user_id`, `notes`, `photo_file_id` null (FK stored_files) | idx (marina_id, night, event); idx (marina_id, vessel_id, at) |
| `move_undo_log` | `user_id`, `operation` CHECK (move, swap, layout), `payload` json (`{entries:[{allocation_id, status, last_night, version}], created:[ids]}`), `expires_at`, `undone_at` null | idx (marina_id, expires_at) |
| `closures` | `scope_type` CHECK (marina, map, structure, group, resource), `scope_id` null, `resource_type` null, `from_date`, `to_date`, `reason` (≤ 200, no PII by policy), `created_by_user_id`, `version` | idx (marina_id, from_date); CHECK to_date ≥ from_date; CHECK scope_type='marina' ⇔ scope_id IS NULL |
| `quote_option_placements` | created here (not in 00005) so its FK `(allocation_id, marina_id)` → allocations ON DELETE SET NULL exists on both dialects without `ALTER TABLE … ADD CONSTRAINT` (unsupported on SQLite); columns as listed in §5.1 | unique (option_id, seq); idx (marina_id, option_id) |

Down migrations drop in reverse FK order. `TestMigrations_UpDownUp`, `TestMigrations_SchemaParity`,
`TestMigrations_SameNumbering` cover both files; new `TestMigrations_OccupancyPrimaryKeys` asserts the three
primary keys and the RESTRICT FKs exist on both dialects, and that a duplicate insert fails.

### 5.3 Backup registration (CONTRACT)

`store.BackupTables()` FK order after M2: `occupancy_settings`, `number_sequences`, `inquiries`, `quotes`,
`quote_options`, `contracts`, `contract_lines`, `allocations` (sorted so `moved_from_id` predecessors come first:
order by `created_at, id`; the backup writer already preserves table order, the restore inserts with deferred FKs
on PostgreSQL and `PRAGMA defer_foreign_keys` on SQLite), `quote_option_placements`, `berth_occupancies`,
`cell_occupancies`, `land_occupancies`, `releases`, `presences`, `vessel_presence`, `move_undo_log` (excluded:
transient, listed in `BackupExcluded()`), `closures`. The round-trip test seeds one row of each (via services in
`backup_integration_test.go`) and compares counts and checksums across SQLite → PostgreSQL → SQLite.

---

## 6. Invariant queries (normative; `store.CheckOccupancyInvariants(ctx, db, marinaID, today) ([]Violation, error)`, OCCUPANCY-ENGINE)

Each query must return **zero rows**. `<F>` iterates the three families with their resource column and
`resource_type` (`berth_occupancies.berth_id`/`berth`, `cell_occupancies.area_id`/`dynamic_area`,
`land_occupancies.resource_id`/`land_resource`).

```sql
-- Q1 no double occupancy (structural; must hold even if a PK were missing)
SELECT berth_id, night FROM berth_occupancies WHERE marina_id=:m GROUP BY berth_id, night HAVING count(*)>1;
SELECT area_id, cell, night FROM cell_occupancies WHERE marina_id=:m GROUP BY area_id, cell, night HAVING count(*)>1;
SELECT resource_id, night FROM land_occupancies WHERE marina_id=:m GROUP BY resource_id, night HAVING count(*)>1;
-- Q2 rows only for active allocations of the same marina
SELECT o.* FROM <F> o JOIN allocations a ON a.id=o.allocation_id
 WHERE o.marina_id=:m AND (a.status<>'active' OR a.marina_id<>o.marina_id);
-- Q3 row within the allocation period, same resource, same kind and expiry
SELECT o.* FROM <F> o JOIN allocations a ON a.id=o.allocation_id
 WHERE o.marina_id=:m AND (o.night<a.first_night OR o.night>a.last_night OR a.resource_type<>:rt
       OR a.resource_id<>o.<rescol> OR a.kind<>o.kind OR coalesce(a.expires_at,'')<>coalesce(o.expires_at,''));
-- Q4 no row on a released night of its allocation
SELECT o.* FROM <F> o JOIN releases r ON r.allocation_id=o.allocation_id AND r.status='active'
 WHERE o.marina_id=:m AND o.night BETWEEN r.from_night AND r.to_night;
-- Q5 releases inside their allocation and not overlapping each other
SELECT r.id FROM releases r JOIN allocations a ON a.id=r.allocation_id
 WHERE r.marina_id=:m AND r.status='active' AND (r.from_night<a.first_night OR r.to_night>a.last_night);
SELECT r1.id FROM releases r1 JOIN releases r2 ON r1.allocation_id=r2.allocation_id AND r1.id<r2.id
 WHERE r1.marina_id=:m AND r1.status='active' AND r2.status='active'
   AND r1.from_night<=r2.to_night AND r2.from_night<=r1.to_night;
-- Q6 no overlap between active allocations of the same contract line (a line is in one place per night)
SELECT a1.id, a2.id FROM allocations a1 JOIN allocations a2
  ON a1.contract_line_id=a2.contract_line_id AND a1.id<a2.id
 WHERE a1.marina_id=:m AND a1.status='active' AND a2.status='active'
   AND a1.first_night<=a2.last_night AND a2.first_night<=a1.last_night;
-- Q7 live contracts/transits are covered: every night of [starts_on, ends_on) of each resource line of a contract
--    in (active, held, confirmed, arrived) is covered by exactly one active allocation of the line
--    (computed: SUM(nights of active allocations of the line) = nights(starts_on, ends_on) − nights truncated by
--    departure/termination, and Q6 holds). Implemented as a Go loop over one query returning lines with their
--    allocations; violation = gap or overlap. Draft contracts with hold_lost = true are exempt.
-- Q8 INV-08: transits in held/pending_payment/confirmed/arrived have ≥ 1 active allocation; quotes in draft/sent
--    with hold options have an active hold allocation per hold placement unless expires_at < :now
SELECT c.id FROM contracts c WHERE c.marina_id=:m AND c.kind='transit'
   AND c.status IN ('held','pending_payment','confirmed','arrived')
   AND NOT EXISTS (SELECT 1 FROM contract_lines l JOIN allocations a ON a.contract_line_id=l.id
                    WHERE l.contract_id=c.id AND a.status='active');
-- Q9 dead contracts hold no future nights
SELECT a.id FROM allocations a JOIN contract_lines l ON l.id=a.contract_line_id JOIN contracts c ON c.id=l.contract_id
 WHERE a.marina_id=:m AND a.status='active'
   AND (c.status IN ('cancelled','no_show') OR (c.kind='transit' AND c.status='expired'));
-- (an expired mooring contract keeps its rows as history; a terminated one keeps only nights < terminated_on:
--  SELECT a.id … WHERE c.status='terminated' AND a.status='active' AND a.last_night >= c.terminated_on)
-- Q10 kinds coherent with the contract state: held transit ⇒ hold; confirmed/arrived/departed ⇒ transit;
--     draft mooring ⇒ hold; active/terminated/expired mooring ⇒ contract
-- Q11 no hold row with expires_at < :now after the job ran (asserted after running contracts.expire in tests)
SELECT count(*) FROM <F> WHERE marina_id=:m AND kind='hold' AND expires_at < :now;   -- must be 0
-- Q12 moved_from chain: successor starts the night after the predecessor ends (or replaces it), same line and vessel
SELECT a.id FROM allocations a JOIN allocations p ON p.id=a.moved_from_id
 WHERE a.marina_id=:m AND a.status='active'
   AND (coalesce(a.contract_line_id,'')<>coalesce(p.contract_line_id,'') OR coalesce(a.vessel_id,'')<>coalesce(p.vessel_id,'')
        OR (p.status='active' AND a.first_night<>p.last_night+1 AND a.first_night<=p.last_night));
-- Q13 vessel_presence matches the last presence event of the vessel
-- Q14 INV-07: every active non-block allocation passes fit.Check or has fit_override (Go loop with fit.Check)
-- Q15 INV-03: for every active allocation the set of its rows equals engine.Plan(spec, area, vessel) exactly
--     (Go loop: recompute Plan, read rows by allocation_id, compare sorted slices)
-- Q16 releases.resold_allocation_id points to an active-or-past allocation on the same resource overlapping the release
```

Q7, Q10, Q13–Q15 are Go loops in the same function; `Violation{Query string; IDs []string; Detail string}`.

---

## 7. OpenAPI additions (`api/openapi.yaml`, CONTRACT)

### 7.1 Conventions (in addition to m0/m1/m2 plans)

- New tags `occupancy`, `contracts`, `presences`. `M = /api/admin/v1/marinas/{marinaId}`. New path params
  `AllocationId`, `QuoteId`, `ContractId`, `ReleaseId`, `ClosureId`, `InquiryId`, `PresenceId`, `UndoId`, `AreaId`.
- No required query parameters on `{marinaId}` routes (the cross-marina sweep calls them bare): `date` defaults to
  today, `from/to` to today..today+30.
- Lengths `*_cm`, money `*_cents`, ratios `*_bp`, dates `YYYY-MM-DD`, instants RFC 3339; no `number` type
  (`TestContract_NoFloats` extended).
- Shared schemas: `ResourceRef{resource_type, resource_id}`, `PlacementInput{resource_type, resource_id, x_cm?, y_cm?,
  rotation_dd?}`, `NightRange{from, to}` (inclusive nights), `FitResult{ok, overridable, fill_bp, blocking[FitIssue],
  warnings[FitIssue]}`, `FitIssue{code, limit_cm?, actual_cm?}`, `FitOverrideInput{reason}`, `Allocation`,
  `PriceSummary{price_list_id, total_cents, deposit_cents, engine_version}`, `OccupancySolution`, `ConflictDetail`.
- Every creating or state-changing POST accepts `Idempotency-Key`; versioned resources (`Quote`, `Contract`,
  `Allocation`, `Closure`, `OccupancySettings`, `Inquiry`) return `ETag`; actions on them require `If-Match`
  (412/428) except presences.
- PII gating: responses embed `vessel{id, name, loa_cm, beam_cm, hull_type, draft_cm}` always and
  `customer{id, display_name}` only when the caller has `customers.read` (else `customer: {id}` only). The day view
  never contains customer names without `customers.read`.

### 7.2 Permissions

Add `PermContractsRead = "contracts.read"` to `roles.go`, the contract `Permission` enum and the independent copy in
`TestRoles_MatrixMatchesSpec`: owner, director, office, accounting, dock (§13 D1). Existing permissions used:
`contracts.write` (owner, director, office), `transits.desk` (+ dock), `occupancy.move` (+ dock),
`presence.write` (+ dock), `occupancy.override_fit` (owner, director), `map.view` (all), `harbor.configure`
(owner, director), `customers.read`. Override: any request carrying `fit_override` additionally requires
`occupancy.override_fit` (checked in the service; 403 otherwise).

### 7.3 Paths

Occupancy (handlers `internal/http/admin/occupancy.go`, OCCUPANCY-ENGINE):

| Method & path | operationId | Perm | Request → 2xx | Errors |
|---|---|---|---|---|
| GET `M/occupancy-settings` | getOccupancySettings | map.view | 200 `OccupancySettings` + ETag | 404 |
| PUT `M/occupancy-settings` | updateOccupancySettings | harbor.configure | + If-Match → 200 | 400, 403, 412, 428 |
| GET `M/maps/{mapId}/day` | getMapDay | map.view | query `date?` → 200 `DayView` (§8.3) | 404 |
| GET `M/maps/{mapId}/occupancy` | getMapOccupancy | map.view | query `from?`, `to?` (≤ 400 nights), `resource_ids?` → 200 `{from, to, allocations: [MapAllocation{id, resource_type, resource_id, x_cm, y_cm, rotation_dd, cells:{first,last}\|{indexes}, first_night, last_night, kind, status, released: NightRange[], vessel?, contract?{id, kind, term, status, code}}]}` | 400, 404 |
| GET `M/allocations/{allocationId}` | getAllocation | map.view | 200 `Allocation` (+ contract summary, releases, moved_from chain ids) + ETag | 404 |
| POST `M/occupancies/search` | searchOccupancy | transits.desk (purpose contract also contracts.write) | `{purpose, vessel_id? \| vessel?{loa_cm, beam_cm, draft_cm?, air_draft_cm?, hull_type}, arrival, departure, term?, resource_types?, prefs?{group_ids?, power_a?, water?, structure_ids?}, consider_out?, max_segments?, customer_id?, services?}` → 200 `{solutions: OccupancySolution[{key, segments[{resource_type, resource_id, code, map_id, x_cm?, y_cm?, rotation_dd?, first_night, last_night}], score, fit, requires_release?{allocation_id, contract_id}, price?: PriceSummary, price_error?: {type, reason}}], considered: int}` | 400, 403, 404 |
| POST `M/occupancies/check` | checkPlacement | map.view | `{allocation_id? \| vessel_id?, target: PlacementInput, from_night, to_night}` → 200 `{ok, fit, placement_error?, conflicts: ConflictDetail[], closure_id?, price_effect?{old_total_cents, new_total_cents, delta_cents}}` | 400, 404 |
| POST `M/occupancies/move` | moveAllocation | occupancy.move | `{allocation_id, version, target: PlacementInput, from_night?, to_night?, fit_override?, reprice?}` → 200 `{allocations: Allocation[], undo_id, undo_until, price_effect?}` | 400, 403, 404, 409 resource_taken/closure_active, 412, 422 fit_blocked/placement_invalid |
| POST `M/occupancies/swap` | swapAllocations | occupancy.move | `{a: {allocation_id, version}, b: {…}, from_night?, fit_override?}` → 200 same | same |
| POST `M/dynamic-areas/{areaId}/layout` | saveAreaLayout | occupancy.move | `{date, from_night?, base: [{allocation_id, version}], placements: [{allocation_id, x_cm, y_cm?, rotation_dd?}], fit_overrides?: [{allocation_id, reason}]}` → 200 same | 400, 403, 404, 409, 412, 422 (`details.allocation_id`, `unplaced`) |
| POST `M/dynamic-areas/{areaId}/compact` | proposeCompaction | occupancy.move | `{from, to}` → 200 `{placements: [{allocation_id, x_cm}], freed_cm, base: [{allocation_id, version}]}` | 400, 404 |
| POST `M/move-undo/{undoId}` | undoMove | occupancy.move | → 200 `{allocations}` | 404, 409 undo_unavailable |
| POST `M/blocks` | createBlock | occupancy.move | `{target: PlacementInput, from, to, reason}` (+ vessel dims `loa_cm`/`beam_cm` for areas) → 201 `Allocation` | 400, 403, 409, 422 |
| DELETE `M/blocks/{allocationId}` | deleteBlock | occupancy.move | If-Match → 204 (block rows from today removed; past kept) | 404, 409 (not a block), 412 |
| GET `M/releases` | listReleases | contracts.read | query `from?`, `to?`, `resource_id?`, `status?` → page `Release` | 404 |
| POST `M/releases` | createRelease | occupancy.move | `{allocation_id, from_night, to_night, source, notes?}` → 201 `Release` | 400, 403, 404, 409 release_invalid |
| POST `M/releases/{releaseId}/revoke` | revokeRelease | occupancy.move | `{from_night?, to_night?}` + If-Match → 200 `{release, restored_nights, remaining?: Release}` | 404, 409 resource_taken (`details.alternatives`), 412 |
| GET `M/closures` | listClosures | map.view | query `from?`, `to?` → `{items}` | 404 |
| POST `M/closures` | createClosure | harbor.configure | `{scope_type, scope_id?, from_date, to_date, reason}` → 201 | 400, 403 |
| DELETE `M/closures/{closureId}` | deleteClosure | harbor.configure | If-Match → 204 | 404, 412 |

Contracts, quotes, transits, inquiries (handlers `internal/http/admin/{quotes,contracts,transits,inquiries}.go`,
CONTRACTS):

| Method & path | operationId | Perm | Request → 2xx | Errors |
|---|---|---|---|---|
| GET `M/quotes` | listQuotes | contracts.read | `status?`, `kind?`, `customer_id?`, `q?`, cursor → page `QuoteSummary` | 404 |
| POST `M/quotes` | createQuote | contracts.write | `{kind, term?, customer_id, vessel_id, starts_on, ends_on, valid_until?, channel?, policy?, notes?, inquiry_id?, options: [{label?, hold, placements: PlacementInput+{first_night, last_night}[], services?, fit_override?}]}` → 201 `Quote` + ETag | 400, 403, 404, 409 resource_taken/closure_active, 422 fit_blocked/no_price |
| GET `M/quotes/{quoteId}` | getQuote | contracts.read | 200 `Quote` (options with placements, prices, hold allocation ids, `expires_at`) + ETag | 404 |
| PATCH `M/quotes/{quoteId}` | updateQuote | contracts.write | draft only, If-Match, same fields → 200 | 409 invalid_transition, 412, 428, + create errors |
| POST `M/quotes/{quoteId}/send` | markQuoteSent | contracts.write | If-Match → 200 | 409, 412 |
| POST `M/quotes/{quoteId}/accept` | acceptQuote | contracts.write | `{option_id, activate?: bool, confirm?: ManualConfirmation}` + If-Match → 200 `{quote, contract}` | 409 invalid_transition/quote_expired/resource_taken, 412, 422 |
| POST `M/quotes/{quoteId}/decline` | declineQuote | contracts.write | `{reason?}` + If-Match → 200 | 409, 412 |
| GET `M/contracts` | listContracts | contracts.read | `kind?` (default mooring), `status?`, `term?`, `customer_id?`, `vessel_id?`, `active_on?`, `q?`, cursor → page `ContractSummary` | 404 |
| POST `M/contracts` | createContract | contracts.write | `{term, customer_id, billing_customer_id?, vessel_id, starts_on, ends_on?, placements: (PlacementInput+{first_night,last_night})[], extra_resources?: [{land_resource_id, item_id?}], services?, policy?, notes?, activate?, fit_override?}` (`ends_on` default by term: annual +1 year, seasonal = the price list's season window end, monthly +1 month) → 201 `Contract` + ETag | 400, 403, 404, 409, 422 |
| GET `M/contracts/{contractId}` | getContract | contracts.read | 200 `Contract` (lines, allocations timeline, releases, presences summary, audit link) + ETag; also serves transits | 404 |
| PATCH `M/contracts/{contractId}` | updateContract | contracts.write | If-Match; draft: notes, billing customer, policy; active: notes only → 200 | 409, 412, 428 |
| POST `M/contracts/{contractId}/activate` | activateContract | contracts.write | If-Match → 200 | 409 invalid_transition/resource_taken, 412 |
| POST `M/contracts/{contractId}/cancel` | cancelContract | contracts.write | `{reason?}` + If-Match → 200 | 409, 412 |
| POST `M/contracts/{contractId}/terminate` | terminateContract | contracts.write | `{effective_on, reason}` + If-Match → 200 | 400, 409, 412 |
| GET `M/transits` | listTransits | contracts.read | `status?`, `arrival_from?`, `arrival_to?`, `on?`, `q?`, cursor → page `ContractSummary` | 404 |
| POST `M/transits` | createTransit | transits.desk | `{vessel_id \| new_vessel, customer_id?, arrival, departure, placements \| solution_key+search_request, channel? (desk\|api), expected_arrival_time?, notes?, services?, fit_override?, confirm?: ManualConfirmation, inquiry_id?}` → 201 `Contract` | 400, 403, 404, 409, 422 |
| POST `M/transits/{contractId}/confirm` | confirmTransit | transits.desk | `ManualConfirmation{reference?, pay_at_departure?}` + If-Match → 200 | 409 invalid_transition/hold_expired→re-allocate fail, 412, 422 customer_required |
| POST `M/transits/{contractId}/cancel` | cancelTransit | transits.desk | `{reason?}` + If-Match → 200 | 409, 412 |
| POST `M/transits/{contractId}/no-show` | markTransitNoShow | transits.desk | If-Match → 200 | 409, 412 |
| POST `M/transits/{contractId}/stay` | changeTransitStay | transits.desk | `{arrival?, departure?, extension_target?: PlacementInput, fit_override?}` + If-Match → 200 `{contract, price_effect}` | 409 resource_taken (`details.alternatives`), 412, 422 |
| PATCH `M/transits/{contractId}` | updateTransit | transits.desk | If-Match; `customer_id`, `notes`, `expected_*_time` → 200 | 409, 412 |
| GET `M/inquiries` | listInquiries | contracts.read | `outcome?`, `from?`, `to?`, cursor | 404 |
| POST `M/inquiries` | createInquiry | transits.desk | `{purpose, term?, source, channel?, vessel_id? \| loa_cm+beam_cm, draft_cm?, arrival, departure, customer_id?, outcome?, notes?}` → 201 | 400, 403 |
| PATCH `M/inquiries/{inquiryId}` | updateInquiry | transits.desk | If-Match; outcome, links, notes → 200 | 409, 412 |

Presences (handlers `internal/http/admin/presences.go`, CONTRACTS):

| Method & path | operationId | Perm | Request → 2xx | Errors |
|---|---|---|---|---|
| GET `M/presences` | listPresences | map.view | `date?`, `vessel_id?`, cursor → page `Presence` (notes only with customers.read) | 404 |
| POST `M/presences` | recordPresence | presence.write | `{vessel_id, event, at?, allocation_id?, notes?, expected_return_on?}` → 201 `{presence, contract?}` | 400, 403, 404, 409 invalid_transition |
| POST `M/presences/unexpected-arrival` | recordUnexpectedArrival | presence.write (+ transits.desk) | `{vessel_id \| new_vessel{name, loa_cm, beam_cm, hull_type, draft_cm?}, target: PlacementInput, departure?, notes?, fit_override?}` → 201 `{presence, contract}` | 400, 403, 409, 422 |
| POST `M/presences/{presenceId}/photo` | uploadPresencePhoto | presence.write | multipart `file` → 200 `{file_id}` | 400 invalid_file, 413 file_too_large |
| GET `M/presences/{presenceId}/photo` | getPresencePhotoLink | customers.read | → 200 `{url, expires_at}` (signed, 5 min) | 404 |
| GET `M/movements` | getMovements | map.view | `date?`, `map_id?` → 200 `{date, arrivals_expected[], departures_expected[], vessels_out[], arrived_without_booking[], arrived[], departed[]}` each `{vessel, contract?{id, code, kind, status}, resource?{type, id, code, map_id}, expected_time?, presence_id?}` | 404 |

`x-porto-max-body-bytes`: `saveAreaLayout` 1 MiB, photo 8 MiB. Every new route is in the cross-marina sweep;
CONTRACT raises the route-count threshold in `acceptance_test.go` to the new total.

### 7.4 Problem types (`internal/http/problem/types.go`, `internal/http/respond/errmap.go`, i18n `errors.*`)

New slugs: `resource_taken` 409 (`details: ConflictDetail{resource_type, resource_id, from, to, cells?, allocation_ids,
alternatives?: OccupancySolution[]}`), `fit_blocked` 422 (`details: FitResult`), `placement_invalid` 422
(`details: {reason, allocation_id?}`), `invalid_transition` 409 (`details: {from, action}`), `quote_expired` 409,
`hold_expired` 409, `closure_active` 409 (`details: {closure_id, from_date, to_date}`), `undo_unavailable` 409,
`release_invalid` 409 (`details: {reason}`), `customer_required` 422. Reused: `in_use` (harbour guard),
`no_price`, `validation_failed`, `version_conflict`, `precondition_required`, `conflict`, `not_found`, `forbidden`.

Sentinel mapping (CONTRACT writes errmap entries for the §2.3 names and `contract.ErrInvalidTransition`
(`*TransitionError{From, Action}`), `contract.ErrQuoteExpired`, `contract.ErrCustomerRequired`,
`contract.ErrPriceListChanged` (→ `conflict`), `contract.ErrVersionMismatch`, `presence.ErrInvalidTransition`,
`harbor.ErrElementInUse` (→ `in_use`)).

### 7.5 Events and SSE

| Type | Public | Topic | Data (ids, enums, dates, counts only) |
|---|---|---|---|
| `occupancy.allocated` | yes | `occupancy.changed` | `{allocation_ids, resource_type, resource_ids, map_id, kind, from, to, contract_id?}` |
| `occupancy.moved` | yes | `occupancy.changed` | `{allocation_ids, from_resource_ids, resource_ids, map_ids, map_id, from, to, operation: move\|swap\|layout\|undo, undo?}` |
| `occupancy.released` | yes | `occupancy.changed` | `{allocation_ids, resource_ids, map_id, from, to, reason: release\|truncate\|cancel\|block_removed}` |
| `occupancy.hold_expired` | yes | `occupancy.changed` | `{allocation_ids, resource_ids, map_id, from, to, quote_id?, contract_id?}` |
| `release.created` / `release.resold` | yes | `occupancy.changed` | `{release_id, allocation_id, resold_allocation_id?, resource_ids, map_id, from, to}` |
| `presence.arrived` / `presence.departed` | yes | `presence.changed` | `{presence_id, vessel_id, allocation_id?, contract_id?, resource_ids, map_id?, night}` |
| `quote.sent` / `quote.accepted` / `quote.expired` | yes | `contract.changed` | `{quote_id, status, contract_id?}` |
| `contract.created` / `contract.activated` / `contract.terminated` | yes | `contract.changed` | `{contract_id, kind, term?, status, from, to}` |
| `transit.held` / `transit.confirmed` / `transit.cancelled` / `transit.arrived` / `transit.departed` | yes | `contract.changed` | `{contract_id, status, from, to, confirmation_source?}` |
| `quote.changed`, `contract.changed`, `transit.changed`, `inquiry.changed`, `config.occupancy_changed` | no | `contract.changed` (`config.occupancy_changed` → `occupancy.changed`) | `{…_id, change}` |

Samples for all in `Samples()` (`TestEvents_DataHasNoPII`). The SSE `Message` (`internal/http/sse/sse.go`) gains
optional `map_id string`, `resource_ids []string`, `nights {from, to}` filled from the event data keys `map_id`,
`resource_ids`, `from`/`to` (spec 27.4); existing fields stay (`TestSSE_PayloadHasNoPII` updated with the new
allow-list). Web: `SSE_TOPICS` += `occupancy.changed`, `presence.changed`, `contract.changed`;
`keysForMessage(msg, marinaId)` (new, CONTRACT) returns: `occupancy.changed` → `dayView(m, map_id)` (all maps when
absent), `mapOccupancy(m, map_id)`, `allocation(*)`, `movements(m)`, `releases(m)`; `presence.changed` →
`dayView(m, map_id)`, `movements(m)`, `presences(m)`; `contract.changed` → `quotes(m)`, `contracts(m)`,
`transits(m)`, `inquiries(m)`, `movements(m)`, `dayView(m)` (contract status colours). Debounce 250 ms per key.

Audit actions: `allocation.{create,move,swap,layout,undo,cancel,truncate,resize,expire,fit_override}`,
`block.{create,delete}`, `release.{create,revoke,resold}`, `closure.{create,delete}`, `occupancy_settings.update`,
`quote.{create,update,send,accept,decline,expire}`, `contract.{create,update,activate,cancel,terminate,expire}`,
`transit.{create,update,confirm,cancel,no_show,stay,expire,arrive,depart}`, `inquiry.{create,update}`,
`presence.record`. Diffs carry field names, statuses, dates, ids, totals; never notes, confirmation refs or names.

### 7.6 Meta and jobs

Capabilities `occupancy.engine`, `occupancy.search`, `occupancy.layout`, `contracts.mooring_base`, `transits.desk`,
`quotes.option`, `presences`. Jobs (registered by CONTRACT in `app.go`, implemented by CONTRACTS):
`contracts.expire` every 1 min (expired holds via `occupancy.ExpireHoldsTx`, transit/quote/contract reconciliation of
§3.3 and §4.1, one transaction per marina per run, batch ≤ 500 allocations), plus `RunAtStart`.

---

## 8. Web

### 8.1 Routes (`web/admin`, TanStack code-based)

`router.tsx` (CONTRACT) adds `operationsRoutes(marinaRoute)` (WEB-OPSMAP) and `deskRoutes(marinaRoute)` (WEB-DESK).

| Route | Owner | Page | Permission (UI) |
|---|---|---|---|
| `/m/$marinaId/ops` (`?map=&date=&lens=&focus=&mode=scale\|schema`) | WEB-OPSMAP | operational view: map tabs, date selector (today, arrows, calendar), lens tabs + "Altri", legend-filter with counts, filters (group, structure, category, LOA band, contract type, free text), map, side panel slot (incoming vessel search, quick card), bottom panels (movements), list alternative per structure, undo toast | map.view |
| `/m/$marinaId/ops/area/$areaId` (`?date=`) | WEB-OPSMAP | dynamic area layout session (draft, Save/Restore, "Da collocare", compaction preview) | occupancy.move (read map.view) |
| `/m/$marinaId/planner` (`?map=&from=&group=&structure=`) | WEB-DESK | berth calendar 9.7: rows = berths/areas, columns = nights (14/31/62), bars per allocation from `getMapOccupancy`, drag horizontal (stay change for transits; contracts read-only in M3) and vertical (move), create bar on free space → new transit dialog | map.view |
| `/m/$marinaId/quotes`, `/quotes/new`, `/quotes/$quoteId` | WEB-DESK | list, editor (customer, vessel, period, term, options with placements picked from search or map link, hold toggle, prices), actions send/accept/decline, expiry countdown | contracts.read (write contracts.write) |
| `/m/$marinaId/contracts`, `/contracts/new`, `/contracts/$contractId` | WEB-DESK | mooring contracts list, wizard (customer, vessel, term/period, placement via search, extra land resources, services, price preview, activate now / draft), detail (lines, allocations timeline, releases, presences, actions activate/cancel/terminate, "show on map") | contracts.read |
| `/m/$marinaId/transits`, `/transits/$contractId` | WEB-DESK | transits list (today/upcoming/in port), detail (confirm with manual driver, cancel, no-show, stay change, customer link) | contracts.read |
| `/m/$marinaId/inquiries` | WEB-DESK | list with outcomes and filters, edit outcome | contracts.read |
| `/m/$marinaId/dock` | WEB-DESK | phone panel (§8.6) | presence.write or map.view |
| `/m/$marinaId/occupancy-settings` | WEB-DESK | occupancy settings form + closures table | harbor.configure |

`nav.ts` (CONTRACT): items `ops` (map.view, `common:nav.ops`), `planner` (map.view), `transits`, `quotes`,
`contracts`, `inquiries` (contracts.read), `dock` (presence.write), `occupancy-settings` under settings
(harbor.configure); remove `common:nav.modules.contracts` from `UPCOMING_MODULES`. For the `dock` role the default
landing route becomes `/m/$marinaId/dock` on viewports < 768 px (`DashboardPage` link card, WEB-DESK).

### 8.2 Shared map additions (`web/shared/src/map/`, WEB-OPSMAP, ADR 0004)

```
lenses/index.ts          Lens = (el: DayElement, ctx: LensContext) => LensStyle{colorToken, glyph?, badge?, legendKey}
lenses/{occupancy,contractType,vesselType,presence,deadlines,fitness,utilities,anomalies}.ts (+ tests)
lenses/legend.ts         legend keys per lens, counts(dayView, lens, filters) → Record<legendKey, number>
vessel-shape.ts          pure path generator (loa_cm, beam_cm, hull_type) → SVG path in cm (integers rounded half-even)
fit.ts                   TS mirror of internal/domain/fit (integers) + fit.parity.test.ts (testdata/fit/cases.json)
dayView.ts               index DayView by resource id; placements per area; schema-mode tile layout (pure)
elements/Vessel.tsx      silhouette at placement (berth: centred on the berth axis, bow toward the structure;
                         area: at x/y/rotation), name when ≥ 9 px, memo by allocation id + version + lens
elements/LensFill.tsx    fills berth/land shapes with the lens token class and glyph (<use> of kit glyph symbols)
elements/DragGhost.tsx   footprint ghost + cell strip coloured ok/warn/conflict, free-space indicator in metres
HarborMap.tsx            new optional props: `lensStyles?: ReadonlyMap<string, LensStyle>`, `dimmed?: ReadonlySet<string>`,
                         `vessels?: readonly VesselItem[]`, `highlight?: ReadonlySet<string>`, `mode?: 'scale'|'schema'`;
                         editor behaviour unchanged when absent (existing tests stay green)
```

Colours: CONTRACT adds `--harbor-free`, `--harbor-annual`, `--harbor-seasonal`, `--harbor-monthly`,
`--harbor-period`, `--harbor-transit`, `--harbor-hold`, `--harbor-released`, `--harbor-blocked`,
`--harbor-maintenance`, `--harbor-owner`, `--harbor-in`, `--harbor-out`, `--harbor-away-long`,
`--harbor-arrival`, `--harbor-departure`, `--harbor-ok`, `--harbor-warn`, `--harbor-bad`, `--harbor-override`,
`--harbor-muted` to `web/shared/src/styles/globals.css` (light and dark, contrast ≥ 3:1 against the water token,
checked by `web/scripts/contrast-check.mjs` new, CONTRACT) and the Tailwind theme mapping (`fill-harbor-*`,
`stroke-harbor-*`, `bg-harbor-*`). Lenses return class names only.

LOD (M3 thresholds): below 6 px berth width → filled rect with lens colour, no glyph, no silhouette; 6–14 px →
silhouette without name; ≥ 14 px → silhouette, glyph, badge, name ≥ 9 px. Culling as M1. Lens switch recomputes
only `lensStyles` (memoised per day view version + lens + filters).

### 8.3 Day view contract (`DayView`, server computed by OCCUPANCY-ENGINE, consumed by WEB-OPSMAP)

```
DayView{ map_id, date, map_version, generated_at,
  elements: [DayElement{
    resource_type, resource_id, code,
    status: free|hold|contract|transit|block|maintenance|owner|released|closed,   // night = date
    allocation_id?, allocation_version?, kind?, hold_expires_at?,
    contract?{id, code, kind, term?, status},
    vessel?{id, name, loa_cm, beam_cm, hull_type, draft_cm?},  customer?{id, display_name?},
    presence?{state: in_port|out|ashore|yard, since, expected_return_on?, away_days},
    release?{id, from, to, resold: bool},
    arrival_today: bool, departure_today: bool,            // transit arrival = date, departure = date
    fit?{fill_bp, override: bool, blocking_codes[]},
    deadlines?{contract_ends_on?, insurance: ok|expiring|expired|missing, documents: ok|expiring|expired},
    utilities?{power_a?, power_phase?, water},
    anomalies: string[] }],
  area_placements: [{area_id, allocation_id, allocation_version, x_cm, y_cm?, rotation_dd?, cells, status, kind,
                     contract?, vessel, customer?, presence?, fit?, anomalies}],
  unplaced: [{vessel, presence_id, reason: arrived_without_booking}],
  counts: { anomalies: int } }
```

Built with ≤ 10 queries (berths/areas/land of the map; rows of `date` and `date − 1` per family joined to
allocations, contract lines, contracts, vessels; vessel_presence; releases covering date; deadlines/documents
expiring within 30 days for the vessels and customers present; closures). Budget: 3 000 elements, 2 000 occupied,
≤ 250 ms on SQLite (`BenchmarkDayView_3000`).

### 8.4 Anomalies in M3 (pure `engine.Anomalies`, codes are `occupancyEnums:anomaly.*`)

`presence_without_allocation` (vessel in_port, no active allocation covering date), `not_arrived` (transit
confirmed with arrival = date and now > expected arrival time + grace, or arrival < date, no arrival presence),
`departure_overdue` (transit arrived, departure < date, still in_port), `vessel_exceeds_limits` (current fit has
blocking codes, with or without override), `insurance_expired_active` (active contract/transit, insurance expired
or missing on date), `hold_lost_draft` (draft contract with `hold_lost`). Sensor and double-presence anomalies are
M12.

### 8.5 Drag & drop and layout session (WEB-OPSMAP)

- Drag start on a vessel/berth with an allocation (permission `occupancy.move`): fetch `getMapOccupancy` for the
  allocation's remaining nights on the current map (cached per drag), build a local busy index; on hover compute
  `footprint()` + `fit()` locally → ghost green/yellow/red with conflicting vessel and nights; linear areas slide
  on the axis with 10 cm snap and snap to neighbour edge + margin; surfaces rotate with `R` or a handle (90°).
- Drop on a fixed berth: "Da quando" dialog (today / from date / only an interval), price effect from
  `checkPlacement`, then `moveAllocation` with `version`; result → undo toast (`undoMove`) for 24 h (toast 30 s,
  then "Annulla ultimo spostamento" in the quick card).
- Drop into/inside a dynamic area: opens (or continues) the layout session of that area: draft kept in React state
  and `sessionStorage` (per user, area, date) with the `base` versions; "Da collocare" list; Save →
  `saveAreaLayout`; 412 → reload base and re-apply the draft ops, showing which vessels changed; Restore discards.
- Keyboard alternative: quick card "Sposta" → dialog with target combobox (`ElementCombobox`) and x in metres.
- Swap: drop a vessel onto an occupied berth with `Shift` or via the quick card "Scambia con…".

### 8.6 Desk flows and phone panel (WEB-DESK)

- `IncomingVesselPanel` (9.6): Transito/Contratto toggle, resource types, existing vessel combobox or dimensions
  (`LengthInput`), dates (nights shown), preferences, "Considera liberi i posti con la barca fuori", max segments;
  results list (price, fit warnings, segments, `requires_release`) synchronised with the map highlight; actions
  "Nuovo transito", "Nuovo preventivo", "Contratto"; "Salva come richiesta non evasa" (`createInquiry` outcome
  `declined_no_space`). A `requires_release` solution opens the release dialog for the holder's allocation first.
- Transit wizard: vessel (existing or new), customer (optional until confirm), stay, placement (from the search,
  map pick or prefilled), price preview, "Conferma e registra pagamento manuale" (manual driver) or "Tieni in
  opzione" (held, shows expiry).
- Quote editor, contract wizard, details and actions as §8.1; `ProblemAlert` shows `resource_taken` alternatives
  as one-click options; `fit_blocked` shows the codes and, with `occupancy.override_fit`, an override form (reason).
- Presences: `TodayMovementsPanel` (arrivals expected, departures expected, out, arrived without booking) with one-tap
  "Arrivato"/"Partito" (with optional note and photo), synchronised with the ops date.
- Phone panel `/dock`: bottom tabs Oggi (movements), Mappa (link to `/ops` in compact mode), Arrivo non previsto
  (vessel quick create with estimated dimensions + berth combobox or "scegli sulla mappa"), Rilasci/Blocchi;
  touch targets ≥ 44 px, works at 360 px width; uses shadcn `drawer` for bottom sheets.

### 8.7 Cross-builder component contracts (fixed; CONTRACT writes stubs with these exact props)

```ts
// web/admin/src/features/desk/public.ts  (WEB-DESK implements; WEB-OPSMAP imports only from here)
import type { components } from '@porto/shared/api';
type Solution = components['schemas']['OccupancySolution'];
type PlacementInput = components['schemas']['PlacementInput'];
export type DeskPrefill = { target?: PlacementInput; arrival?: string; departure?: string; vesselId?: string; solution?: Solution };
export function IncomingVesselPanel(p: { marinaId: string; date: string; onSolutions: (s: Solution[]) => void;
  onFocusSolution?: (s: Solution | null) => void; onPickOnMap?: () => Promise<PlacementInput | null> }): JSX.Element;
export function TodayMovementsPanel(p: { marinaId: string; date: string; mapId?: string;
  onLocate?: (ref: { resource_type: string; resource_id: string; map_id?: string }) => void }): JSX.Element;
export function NewTransitDialog(p: { marinaId: string; open: boolean; onOpenChange: (o: boolean) => void; prefill?: DeskPrefill }): JSX.Element;
export function NewQuoteDialog(p: { marinaId: string; open: boolean; onOpenChange: (o: boolean) => void; prefill?: DeskPrefill }): JSX.Element;
export function PresenceDialog(p: { marinaId: string; open: boolean; onOpenChange: (o: boolean) => void; vesselId: string; allocationId?: string; event: 'arrived' | 'departed' }): JSX.Element;
export function ReleaseDialog(p: { marinaId: string; open: boolean; onOpenChange: (o: boolean) => void; allocationId: string; defaultFrom: string }): JSX.Element;
export function BlockDialog(p: { marinaId: string; open: boolean; onOpenChange: (o: boolean) => void; target: PlacementInput; defaultFrom: string }): JSX.Element;
export function UnexpectedArrivalDialog(p: { marinaId: string; open: boolean; onOpenChange: (o: boolean) => void; target?: PlacementInput }): JSX.Element;

// web/admin/src/features/operations/public.ts  (WEB-OPSMAP implements; WEB-DESK imports only from here)
export function opsMapSearch(p: { mapId?: string; date?: string; focus?: string; lens?: string }): Record<string, string>;
export function PlacementPicker(p: { marinaId: string; mapId?: string; date: string; vessel: { loa_cm: number; beam_cm: number; hull_type: string };
  from: string; to: string; onPick: (t: PlacementInput) => void; onCancel: () => void }): JSX.Element; // map-in-dialog with ghost preview
```

Fixed test ids (WEB-OPSMAP renders them, WEB-DESK e2e uses them): `ops-map`, `ops-date`, `ops-lens-tab-{lens}`,
`ops-legend-item-{key}`, `ops-legend-count-{key}`, `ops-quick-card`, `ops-quick-action-{action}`, `ops-unplaced-count`,
`ops-layout-save`, `ops-layout-restore`, `ops-undo`; elements keep `role="button"` with `aria-label` "`{code}`,
`{status}`, `{vessel}`" from `opsMap:element.aria`.

### 8.8 i18n namespaces

CONTRACT creates and registers: `occupancyEnums` (complete: allocation kinds/statuses, contract kinds/terms/statuses,
transit statuses, quote statuses, inquiry outcomes/sources, release sources, presence events/states, closure scopes,
fit issue codes `fit.*`, anomaly codes `anomaly.*`, lens names and legend keys `lens.*`, placement error reasons,
conflict texts), `opsMap` (WEB-OPSMAP), `desk` and `dock` (WEB-DESK), plus `common` nav keys and `errors` slugs.
Builders request additions to CONTRACT-owned namespaces in their report; `i18n-check` fails on missing keys.

---

## 9. Tests and acceptance

### 9.1 Gates (exact commands, from the repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check                                                   # gen-check + lint (depguard incl. fit, occupancy/engine) + unit, exit 0
make test-db                                                 # integration on SQLite AND PostgreSQL, exit 0
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
go test ./internal/domain/fit/... ./internal/domain/occupancy/engine/... -count=1 -rapid.checks=2000
go test ./internal/domain/fit/... -count=1 -run 'TestFit_ParityVectors|TestFit_VectorsUpToDate' -v
npm --prefix web run test -- --run web/shared/src/map/fit.parity.test.ts web/shared/src/map/footprint.parity.test.ts
scripts/with-postgres.sh go test -tags=integration -count=1 -race ./internal/domain/occupancy/... ./internal/domain/contract/... ./internal/domain/presence/... ./internal/store/...
make test-race                                               # every TestConcurrency_* (§9.3) on SQLite + PostgreSQL with -race
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 20m ./internal/acceptance/... -run 'TestAcceptance_M3|TestAcceptance_CrossMarinaIsolation|TestAcceptance_CrossMarinaResourceIds|TestAcceptance_M2|TestAcceptance_M1|TestAcceptance_M0'
go test -tags=integration ./internal/domain/occupancy/... -run '^$' -bench 'BenchmarkSearch_Demo|BenchmarkDayView_3000|BenchmarkAllocate_Annual' -benchtime 20x
make e2e                                                     # all specs incl. §9.5
PORTO_E2E_PERF=1 npm --prefix e2e exec playwright test tests/map-perf.spec.ts   # blocking budget (§9.5)
scripts/agents/check-naming.sh && make licenses
rm -f tmp/data/porto.db* && make seed && go run ./cmd/porto seed demo   # second run is a no-op; prints contracts/transits/quotes/allocations
```

### 9.2 Unit and property tests (names fixed; referenced by `docs/invariants.md`)

OCCUPANCY-ENGINE: `TestFit_Scenario` (every code, boundaries), `TestFit_ParityVectors`, `TestFit_VectorsUpToDate`,
`TestRapid_FitMonotone` (shrinking the vessel never adds a blocking code), `TestPlan_RowsAreFootprintTimesNights`,
`TestRapid_PlanExcludesReleased`, `TestFreeIntervals_Scenario`, `TestRapid_FreeIntervalsDisjointFromBusy`,
`TestSolve_Scenario` (fixed berth, linear best fit adjacency, surface excluded, closures, transit-dedicated
penalty, continuity, segments ≤ max, consider-out), `TestRapid_SolutionsAreFreeAndFit` (every solution's plan is
disjoint from the snapshot's busy rows and passes fit), `TestCompact_Scenario`, `TestRapid_CompactKeepsOrderAndFrees`,
`TestAnomalies_Scenario`, `TestEngine_Purity` (imports), `FuzzPlan`.

### 9.3 Integration and concurrency tests (tag `integration`, `testdb` both dialects)

OCCUPANCY-ENGINE (`internal/domain/occupancy/*_integration_test.go`, `internal/store/occupancy_integration_test.go`):
- `TestOccupancy_AllocateScenario` (berth, linear, surface, land; fit block, override with/without permission,
  closure, placement errors), `TestOccupancy_ExpiredHoldIsReclaimed` (INV-05), `TestOccupancy_ConflictRollsBackAll`
  (multi-segment: second segment conflict leaves zero rows and no allocation), `TestAllocation_RowsMatchFootprint`
  (INV-03, rapid over random sequences of allocate/move/release/revoke/truncate/resize on a small harbour, after each
  step `CheckOccupancyInvariants` = ∅), `TestAllocation_FitOrOverride` (INV-07), `TestOccupancy_MoveScenario`
  (from today, from date, interval, reprice flag, undo within/after 24 h, undo blocked by a later move),
  `TestOccupancy_LayoutSaveExchangePlaces`, `TestOccupancy_LayoutStaleBase412`, `TestOccupancy_ReleaseResaleRevoke`,
  `TestOccupancy_HarbourGuard` (delete occupied berth → 409 in_use; cell size change with future allocations → 409;
  past-only rows → FK restrict mapped to 409), `TestOccupancy_DayViewScenario`, `TestOccupancy_SearchSnapshotQueryCount`
  (≤ 8 queries via a Bun query hook), `TestOccupancy_WritesAuditAndOutbox`.
- **`TestConcurrency_BerthAllocation`**: 100 goroutines allocate overlapping random periods on the same berth
  (each period includes night N) → exactly one success touching night N; for every night ≤ 1 row; successes'
  periods pairwise disjoint; failures are all `ErrResourceTaken`.
- **`TestConcurrency_CellAllocation`**: linear area 60 m, 100 goroutines place vessels at random x with overlapping
  cell ranges and the same nights → winners have pairwise disjoint cell sets, `CheckOccupancyInvariants` = ∅; plus a
  surface variant (`TestConcurrency_CellAllocationSurface`) with random rotations.
- **`TestConcurrency_LandAllocation`**, **`TestConcurrency_ChannelVsDesk`** (50 `desk` + 50 `api` transits on the same
  berth and nights → one winner), **`TestConcurrency_SwapVessels`** (INV-06: 100 parallel swaps/moves on the same two
  allocations → each either fully applied or not, final state has both vessels on distinct berths, invariants ∅),
  **`TestConcurrency_MoveVsAllocate`** (50 moves into berth X vs 50 transits on X), **`TestConcurrency_ExpiredHoldReclaim`**
  (100 goroutines on a berth with an expired hold → one winner, the hold allocation `expired`),
  **`TestConcurrency_LayoutSaveSingleWinner`** (100 saves from the same base → one 200, others 412/409),
  **`TestConcurrency_RevokeVsResale`**.

CONTRACTS (`internal/domain/contract/*_integration_test.go`, `presence/…`):
- `TestContract_StateChangeWritesAuditAndOutbox` (INV-30: every transition of §4.1–§4.3 writes exactly one audit row
  and its outbox event in the same tx; a forced failure after the state write leaves neither),
  `TestContract_TransitionTable` (every (state, action) pair not in §4 → `invalid_transition`),
  `TestContract_TransitConfirmOnlyManualDriver` (INV-09: no other code path sets `confirmed`; asserted by a grep test
  `TestContract_ConfirmedOnlyInConfirmTransit` over `internal/**` for `StatusConfirmed` assignments),
  `TestContract_HoldTransitHasAllocation` (INV-08 for desk/api channels), `TestQuote_OptionHoldLifecycle`,
  `TestQuote_AcceptConvertsHold`, `TestQuote_AcceptConflictCreatesNothing`, `TestContract_SnapshotStored` (snapshot
  recomputes to the same total, INV-20), `TestContract_PriceListChangedDuringCreate`, `TestContract_Numbering`,
  `TestContract_TerminateTruncates`, `TestTransit_StayChange`, `TestTransit_EarlyDepartureFreesNights`,
  `TestPresence_ArrivalDepartureState`, `TestPresence_UnexpectedArrival`, `TestContract_ExpireJob`,
  **`TestConcurrency_QuoteAcceptVsTransit`**, **`TestConcurrency_NumberingUnique`** (100 parallel creates → numbers
  1..100, no gaps, no duplicates).

Every handler file has a `_test.go` asserting status and problem `type` per error row of §7.3 and a cross-marina 404.

### 9.4 Acceptance tests (`internal/acceptance`, tag integration, API only, `clock.Fake`, CONTRACTS)

**`TestAcceptance_M3_DeskFlow`**: owner sets up a map (pier with 20 berths, linear area 60 m, 5 parking) and an
active transit + annual price list (helpers from M1/M2 acceptance); dock: search transit 3 nights for a 11,34 m
vessel → solutions with prices; create transit held on the first solution, confirm (manual driver) → `confirmed`;
day view shows `transit`; record arrival → `arrived`; office: annual contract with parking, activate; release 10
nights; transit search with `consider_out=false` finds the released berth; book it → `release.resold`; revoke the
release for resold nights → 409 with alternatives; drag-equivalent `moveAllocation` and `undoMove`; layout save on
the area exchanging two vessels; quote with 2 options and holds, accept option 2 → contract draft, option 1 hold
gone; another quote expires after advancing the clock and running the job → `expired`, hold rows gone; yard role:
`getMapDay` 200 without customer names, `createTransit` 403; viewer: `listContracts` 403; another marina's ids → 404;
outbox has the public events of §7.5 with no PII; `CheckOccupancyInvariants` = ∅.

**`TestAcceptance_M3_SeasonSimulation`** (spec 32.3 M3 acceptance; both dialects; ≤ 10 min on PostgreSQL):
- Harbour: 2 piers × 60 berths (8 categories, 10 transit-dedicated, 4 `multihull_ok`), 1 linear area 120 m (cells
  50 cm, `stern_to`), 1 linear area 80 m `alongside`, 1 surface area 30 × 20 m (cells 100 cm), 20 land resources;
  300 vessels from a seeded generator (LOA 5–24 m, realistic beams, 10 % multihull), 200 customers; active price
  lists for transit and annual/seasonal/monthly.
- Generator `math/rand/v2` `rand.New(rand.NewPCG(20260401, 3))` (seed printed on failure; `PORTO_SEASON_SEED`
  overrides); `clock.Fake` starts `Y-03-25 09:00 Europe/Rome` and advances 2–6 hours per step through
  `Y-10-31`, running `contracts.expire` whenever it crosses a minute boundary of a hold expiry.
- **2 000 operations** through the HTTP API (httptest server of the real `app.App`), weighted: annual/seasonal/monthly
  contract create+activate 8 %, draft contract 2 %, transit search+create+confirm 30 %, held transit left to expire
  4 %, transit stay change 6 %, arrival/departure presences 14 %, unexpected arrival 2 %, move berth→berth 6 %,
  move into/inside an area 4 %, swap 2 %, layout save 3 %, compaction apply 1 %, undo 2 %, release 4 %, revoke 2 %,
  terminate 1 %, cancel/no-show 3 %, quote with holds + accept/decline/expire 5 %, block/unblock 1 %. Targets are
  chosen from the search (≈ 80 %) or at random (≈ 20 %, so conflicts are exercised).
- Each operation is classified `ok` or an expected error (`resource_taken`, `fit_blocked`, `placement_invalid`,
  `closure_active`, `invalid_transition`, `version_conflict`, `undo_unavailable`, `quote_expired`); any other status
  fails the test. For failed operations the test asserts **no effect**: counts of allocations, of each occupancy
  table, of contracts and the max outbox id are unchanged (checked on every failure).
- **Oracle model**: the test keeps an in-memory map `(family, resource, cell, night) → allocation_id` updated only
  from successful responses (allocation ids and placements returned by the API) and, at the end, compares it with the
  database rows **exactly** (same key set, same allocation per key).
- After every 100 operations and at the end: `store.CheckOccupancyInvariants` (Q1–Q16) returns no violation; at the
  end also every contract/transit status change counted by the generator has its audit row and outbox event.
- Report (`t.Log`): ok/expected-error counts per operation kind; the test fails if fewer than 1 400 operations
  succeeded or any kind has zero successes (the simulation must really exercise every path).

`TestAcceptance_CrossMarinaIsolation` and `TestAcceptance_CrossMarinaResourceIds` cover every new `{marinaId}` route
and every new resource id parameter.

### 9.5 Web tests

Vitest — WEB-OPSMAP: each lens (fixture day view → tokens/glyphs/legend keys), legend counts = filtered elements,
`vessel-shape` (dimensions, hull types, integer coords), `fit.parity.test.ts`, drag preview state machine (ok/warn/
conflict from a busy index), linear axis snapping and neighbour snap, layout session reducer (add/remove/move,
stale base re-apply), schema-mode tile layout, SSE `keysForMessage` narrowing by map. WEB-DESK: search form →
request mapping (integers), solution list ↔ highlight callbacks, transit wizard steps, quote editor options/holds,
contract wizard defaults per term, `ProblemAlert` alternatives, movements panel actions, planner bar geometry and
drag → request, dock panel at 360 px (Testing Library with `matchMedia` mock).

Playwright (`make e2e`):
- `e2e/support/occupancy.ts` (WEB-DESK owns after CONTRACT stubs; fixed signatures):
  ```ts
  export async function setupOpsHarbour(page: Page, opts?: { berths?: number; linearAreaM?: number }): Promise<{ marinaId: string; mapId: string; berths: BerthDoc[]; areaId: string }>;
  export async function setupPrices(page: Page, marinaId: string): Promise<void>; // active transit + annual lists via API
  export async function createTransitViaApi(page: Page, marinaId: string, o: { vesselId: string; berthId?: string; areaX?: number; arrival: string; departure: string; confirm?: boolean }): Promise<{ id: string; allocationId: string }>;
  export async function createContractViaApi(page: Page, marinaId: string, o: { vesselId: string; customerId: string; berthId: string; term: 'annual' | 'seasonal'; startsOn: string; activate?: boolean }): Promise<{ id: string; allocationId: string }>;
  export async function seedPerfOccupancy(page: Page, marinaId: string, mapId: string, n: number): Promise<void>; // n vessels + confirmed transits, 10 parallel requests
  ```
- WEB-OPSMAP: `ops-map.spec.ts` (lens tabs, legend counts and filter, date change, quick card actions by role, live
  update: a transit created through `page.request` in a second context recolours the berth within 3 s via SSE, list
  alternative, dark theme, axe), `ops-drag.spec.ts` (berth→berth with "Da quando", conflict red ghost, undo),
  `dynamic-area.spec.ts` (layout session: two vessels exchanged and saved, Restore, stale save from a second tab
  shows the reload notice, compaction preview), `map-perf.spec.ts` (see below).
- WEB-DESK: `desk-transit.spec.ts` (incoming-vessel search → highlight → transit → manual confirm → map shows
  transit), `quotes.spec.ts` (options with hold, accept), `contracts.spec.ts` (wizard, activate, terminate),
  `presences.spec.ts`, `dock.spec.ts` (phone viewport 390×844: arrival, unexpected arrival, release), `planner.spec.ts`
  (extend a transit by dragging), **`occupancy-acceptance.spec.ts`** (the M3 scenario end to end from the UI on the
  seeded-by-API harbour: search, quote, accept, contract, transit, arrival, drag move, layout save, release; axe on
  ops, quote editor, contract wizard, dock in light and dark).
- **`map-perf.spec.ts` becomes blocking** (WEB-OPSMAP): still gated by `PORTO_E2E_PERF=1` (CI job `e2e-perf`, which
  CONTRACT asks the orchestrator to add to `.github/workflows/ci.yml`), map of 3 040 berths + 1 linear area,
  `seedPerfOccupancy(…, 2000)`, opens `/admin/m/{id}/ops?map=…&date=…` (lens Occupazione, mode scale), pans 3 s at
  the overview zoom and 3 s at a 4× zoom (silhouettes visible), then switches lens to Presenze and pans 3 s; for each
  run `avg frame ≤ 20 ms` (≥ 50 fps) and `p95 ≤ 34 ms` are **asserted**; the annotations also log a run under
  `CDPSession Emulation.setCPUThrottlingRate(4)` (tablet approximation, informational). The editor perf run of M1
  stays as an informational second test.

### 9.6 Invariants (`docs/invariants.md`; CONTRACT writes the test names, the orchestrator flips the status at the end)

INV-01 → implemented (`TestConcurrency_BerthAllocation`, `…CellAllocation`, `…CellAllocationSurface`,
`…LandAllocation`, `…ChannelVsDesk`); INV-02 → implemented (adds `TestRapid_SolutionsAreFreeAndFit` and the
season oracle); INV-03 → implemented (`TestAllocation_RowsMatchFootprint`, Q15); INV-05 →
`TestOccupancy_ExpiredHoldIsReclaimed`, `TestConcurrency_ExpiredHoldReclaim`; INV-06 → `TestConcurrency_SwapVessels`,
`TestConcurrency_LayoutSaveSingleWinner` (`TestScenarioApply_AllOrNothing` stays planned for M9: status partial);
INV-07 → `TestAllocation_FitOrOverride`; INV-08 → partial (`TestContract_HoldTransitHasAllocation`; online in M8);
INV-09 → partial (`TestContract_TransitConfirmOnlyManualDriver`; webhook in M5/M8); INV-30 → implemented; INV-70
stays partial with the new samples; INV-73 covers the M3 routes. New **INV-10 — Le allocazioni di una riga di
contratto non si sovrappongono e coprono il periodo.** (Q6, Q7) verified by `TestAcceptance_M3_SeasonSimulation` and
`TestAllocation_RowsMatchFootprint`. Stato: implemented.

### 9.7 Done criteria per builder

Each builder finishes with its subset green (`go test` of its packages, `-tags=integration` on both dialects,
`-race` for concurrency, `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test`,
its e2e specs) and runs the `invariants-guardian` subagent (OCCUPANCY-ENGINE and CONTRACTS). The orchestrator then
runs §9.1 in full.

---

## 10. Demo seed (`internal/app/seed_occupancy.go`, CONTRACTS)

Runs after M1/M2 parts when the demo marina has no contracts (idempotent: a second run is a no-op), through the
domain services as the demo owner with a seeded `math/rand/v2` (PCG 31, 4) and the app clock ("today" T):
- Annual contracts on ~200 of the 320 berths (dates `Y-01-01..Y+1-01-01` or from-date anniversaries), each with a
  vessel that fits (or 3 with a recorded `fit_override` "misure in deroga"), 30 with a parking land resource line,
  activated; 12 seasonal (`Y-05-01..Y-10-01`), 6 monthly around T; 2 contracts "posto in area" on the dynamic pier.
- Transits: if T is in April–October, ~140 transits within T−20..T+30 on transit berths, the dynamic pier and
  released berths (mix: departed in the past with presences, arrived now, confirmed upcoming, 6 held with expiry
  tomorrow, 3 cancelled, 2 no-show); otherwise the same shape in the next season.
- 10 releases on annual berths in T..T+30 (4 resold by transits), 5 blocks (works, reason text), 1 closure of pier E
  head for a regatta weekend, 25 vessels `out` with expected return, 8 quotes (2 sent with holds expiring in 3 days,
  1 accepted → draft contract, 1 declined, 1 expired, 3 drafts), 12 inquiries (6 `declined_no_space`).
- Prints counts (contracts by kind, transits by status, quotes, allocations, rows per family) through `SeedResult`.
- After seeding, `CheckOccupancyInvariants` must be empty (`TestSeed_DemoOccupancyCoherent`, CONTRACTS).

---

## 11. Strict file ownership

Order: **CONTRACT** runs first, alone, and finishes when `make gen && go build ./... && make check` pass with stubs.
Then **OCCUPANCY-ENGINE**, **CONTRACTS**, **WEB-OPSMAP**, **WEB-DESK** run in parallel. Generated files belong to
CONTRACT, which reruns `make gen` on request. Files not listed belong to the orchestrator and are read-only for
builders: `docs/spec/**`, `docs/adr/**`, `docs/m*-plan.md`, `docs/architecture.md`, `README.md`, `AGENTS.md`,
`CLAUDE.md`, `assets/**`, `.claude/**`, `.github/**`, `deploy/**`, and all M0–M2 files not listed below.

### 11.1 CONTRACT (then frozen for M3 unless the orchestrator reruns it)

| File | Change |
|---|---|
| `api/openapi.yaml`; `internal/http/api/api.gen.go`, `web/shared/api/generated/**` | §7, `make gen` |
| `internal/migrations/{sqlite,postgres}/00005_contracts.sql`, `00006_occupancy.sql`; `internal/migrations/*_test.go` | §5, `TestMigrations_OccupancyPrimaryKeys` |
| `internal/store/contract_models.go`, `internal/store/occupancy_models.go` | Bun structs for every §5 table (then owned by CONTRACTS / OCCUPANCY-ENGINE respectively; may add fields that exist in the migration) |
| `internal/store/backup_tables.go`, `internal/store/backup_integration_test.go`, `internal/backup/**` | §5.3 |
| `internal/domain/auth/roles.go` + `TestRoles_MatrixMatchesSpec` copy | `contracts.read` |
| `internal/domain/event/event.go`, `payloads.go` (+ samples) | §7.5 types, topics, payload structs |
| `internal/http/sse/sse.go`, `sse_test.go` | `map_id`, `resource_ids`, `nights` |
| `internal/domain/meta/meta.go` | §7.6 capabilities |
| `internal/http/problem/types.go` + test, `internal/http/respond/errmap.go` + test | §7.4 |
| `internal/http/admin/admin.go`, `internal/http/admin/m3_stubs.go` | `Handler` fields `Occupancy`, `Contracts`, `Presence`; every new operation answers 501; builders delete their methods from the stub file as they implement them (OCCUPANCY-ENGINE: occupancy rows of §7.3; CONTRACTS: the rest); the last one deletes the file |
| `internal/app/app.go` | wiring through `Deps`, `harbor.Deps.OccupancyGuard`, job `contracts.expire` registration calling `contract.Service.RunExpiry` |
| `internal/app/seed.go` | calls `a.seedOccupancy(ctx, p, res)` after M2; `SeedResult` M3 counts |
| `internal/domain/harbor/{service.go,save.go,errors.go}` | `OccupancyGuard` interface in Deps (nil-safe), call in `applyDocument` (§3.7), `ErrElementInUse`, FK-violation mapping |
| `.golangci.yml` | add `**/internal/domain/occupancy/engine/**` to `pure-engines` and `pure-engines-no-rand` |
| `internal/domain/fit/{doc.go,types.go,api.go}` | §2.1 types, `Check` stub (then OCCUPANCY-ENGINE) |
| `internal/domain/occupancy/{service.go,errors.go,types.go}`, `internal/domain/occupancy/engine/{doc.go,types.go}` | skeletons, §2.2/§2.3 types and sentinels (then OCCUPANCY-ENGINE) |
| `internal/domain/contract/{service.go,errors.go,types.go,states.go}` | skeleton, sentinels, status constants, **transition tables of §4 as data** (then CONTRACTS, which may not change the tables without the orchestrator) |
| `internal/domain/presence/{service.go,errors.go}` | skeleton (then CONTRACTS) |
| `internal/store/occupancy_invariants.go` | signature `CheckOccupancyInvariants` + `Violation` returning nil (then OCCUPANCY-ENGINE) |
| `web/shared/src/styles/globals.css`, Tailwind theme mapping, `web/scripts/contrast-check.mjs`, `web/package.json` script | `--harbor-*` tokens (§8.2) |
| `web/shared/src/ui/drawer.tsx` + barrel | `npx shadcn@latest add drawer` |
| `web/shared/src/i18n/resources.ts`, `locales/{it,en}/{common,errors,occupancyEnums}.json` + empty `opsMap.json`, `desk.json`, `dock.json` | §8.8 |
| `web/shared/src/api/{queryKeys,sse,index}.ts` (+ tests) | `queryKeys.ops(m)` root (`dayView(m, mapId, date)`, `mapOccupancy(m, mapId, from, to)`, `allocation(m, id)`, `occupancySettings(m)`, `closures(m)`), `queryKeys.desk(m)` root (`quotes`, `quote(id)`, `contracts`, `contract(id)`, `transits`, `inquiries`, `releases`, `movements(date)`, `presences(date)`), `SSE_TOPICS`, `keysForMessage` |
| `web/shared/src/api/hooks/{opsmap,desk}.ts` | empty stubs (then WEB-OPSMAP / WEB-DESK) |
| `web/admin/src/router.tsx`, `nav.ts`, `nav.test.ts` | §8.1 factories and nav |
| `web/admin/src/features/operations/{routes.tsx,public.ts}`, `web/admin/src/features/desk/{routes.tsx,public.ts}` | stubs with §8.7 signatures (then WEB-OPSMAP / WEB-DESK) |
| `e2e/support/occupancy.ts` | stub with §9.5 signatures (then WEB-DESK) |
| `internal/acceptance/acceptance_test.go` | route-count threshold only |
| `docs/invariants.md`, `CHANGELOG.md` | §9.6 entries, M3 entries |

### 11.2 Builders

| Owner | Files |
|---|---|
| **OCCUPANCY-ENGINE** | `internal/domain/fit/**`, `testdata/fit/**`; `internal/domain/occupancy/**` incl. `engine/**` (keeps §2 exported names/shapes); `internal/store/{occupancy.go,allocations.go,occupancy_reads.go,releases.go,closures.go,occupancy_settings.go,move_undo.go,occupancy_invariants.go}`, `internal/store/occupancy_models.go` (after CONTRACT), `internal/store/occupancy_integration_test.go`; `internal/http/admin/occupancy.go` + `_test.go`; `internal/http/view/occupancy.go` |
| **CONTRACTS** | `internal/domain/contract/**` (keeps sentinels, statuses and §4 tables), `internal/domain/presence/**`; `internal/store/{contracts.go,quotes.go,inquiries.go,presences.go,numbering.go}`, `internal/store/contract_models.go` (after CONTRACT), `internal/store/contracts_integration_test.go`; `internal/http/admin/{quotes,contracts,transits,inquiries,presences}.go` + `_test.go`; `internal/http/view/contracts.go`; `internal/app/seed_occupancy.go`; `internal/acceptance/m3_*_test.go`, `internal/acceptance/testdata/m3/**` |
| **WEB-OPSMAP** | `web/shared/src/map/{lenses/**,vessel-shape.ts,fit.ts,dayView.ts,elements/Vessel.tsx,elements/LensFill.tsx,elements/DragGhost.tsx}` + tests, `web/shared/src/map/HarborMap.tsx` + `index.ts` (additive props only); `web/shared/src/api/hooks/opsmap.ts`; `web/admin/src/features/operations/**`; `web/shared/src/i18n/locales/{it,en}/opsMap.json`; `e2e/tests/{ops-map,ops-drag,dynamic-area,map-perf}.spec.ts`, `e2e/support/opsmap.ts` |
| **WEB-DESK** | `web/admin/src/features/desk/**` (search panel, quotes, contracts, transits, inquiries, movements, dialogs, planner, occupancy settings and closures), `web/admin/src/features/dock/**`, `web/admin/src/pages/DashboardPage.tsx` (dock card); `web/shared/src/api/hooks/desk.ts`; `web/shared/src/i18n/locales/{it,en}/{desk,dock}.json`; `e2e/tests/{desk-transit,quotes,contracts,presences,dock,planner,occupancy-acceptance}.spec.ts`; `e2e/support/occupancy.ts` (after CONTRACT) |

### 11.3 Rules for the parallel phase

- **Engine-first contract.** CONTRACTS codes against the §2.3 primitives' signatures; until OCCUPANCY-ENGINE lands,
  its unit tests use a fake implementing the internal interface `allocator` (the `...Tx` methods) in
  `contract/allocator.go`; integration tests and the season simulation run once the engine is in. OCCUPANCY-ENGINE
  never imports `contract` or `presence`.
- **Handlers**: split exactly as §7.3 (occupancy table → OCCUPANCY-ENGINE; contracts/quotes/transits/inquiries/
  presences → CONTRACTS), all in `package admin`; only `m3_stubs.go` is shared (each deletes its own methods).
- **Store**: one file per area as listed; M1/M2 store files are not edited (read helpers for vessels, berths,
  documents go in `occupancy_reads.go` / `contracts.go`).
- **Day view vs contracts**: `occupancy_reads.go` joins `contracts`/`contract_lines` read-only for the day view and
  movements' resource info; the `movements` endpoint is CONTRACTS' and may call `occupancy.Service.DayView` helpers.
- **Web**: WEB-OPSMAP imports desk components only through `features/desk/public.ts`; WEB-DESK imports map pieces
  only through `features/operations/public.ts` and `@porto/shared/map`. Router registration only in each builder's
  `routes.tsx`; paths are disjoint (§8.1). Hooks in your own file; query keys are CONTRACT's.
- **i18n**: only your own namespaces; `occupancyEnums`/`common`/`errors` through the orchestrator.
- **e2e helpers**: WEB-OPSMAP uses `e2e/support/occupancy.ts` only through the §9.5 signatures; WEB-DESK keeps them
  working. Test ids of §8.7 are WEB-OPSMAP's to keep stable.
- **go.mod / package.json**: no new dependency without the orchestrator's approval; none is needed (`rapid`,
  `math/rand/v2`, shadcn CLI already available).

---

## 12. Order of work inside each builder

**CONTRACT** 1. Migrations 00005/00006 both dialects, migration tests. 2. Models, backup registration. 3. OpenAPI §7,
`make gen`; roles, events, SSE message, problems, errmap, meta. 4. Domain skeletons (fit, occupancy, engine,
contract with transition tables, presence), harbour guard hook, depguard, handler stubs, app wiring, job
registration, seed hook. 5. Web foundations: tokens + contrast check, drawer, i18n namespaces (`occupancyEnums`
complete), query keys, SSE, router/nav, route and public stubs, e2e helper stub. 6. `docs/invariants.md`,
CHANGELOG. 7. `make check`, `make test-db` green with stubs (cross-marina sweep fails only on 501 stubs: report).

**OCCUPANCY-ENGINE** 1. `fit` + vectors + parity. 2. `engine.Plan`, `FreeIntervals` with property tests. 3. Store
rows insert/conflict/reclaim, allocate/convert/truncate/cancel/resize with integration tests and the four
`TestConcurrency_*` allocation tests. 4. Moves, swap, layout, undo, releases, revoke, blocks, closures, settings,
harbour guard. 5. Search snapshot + `Solve` + `Check`. 6. Day view, map occupancy, anomalies. 7.
`CheckOccupancyInvariants` and `TestAllocation_RowsMatchFootprint`. 8. Handlers + views; delete stub methods.
9. Compaction, benchmarks.

**CONTRACTS** 1. Numbering, store files. 2. Transits (create, confirm via manual driver, cancel, no-show, stay) with
the allocator fake. 3. Mooring contracts (create, activate, cancel, terminate, expiry). 4. Quotes with options and
holds, accept/decline/expire. 5. Presences, unexpected arrival, movements, photo. 6. Job `contracts.expire`.
7. Handlers + views; delete stub methods. 8. Integration and concurrency tests on the real engine. 9. Acceptance
desk flow, season simulation. 10. Seed.

**WEB-OPSMAP** 1. Hooks. 2. `fit.ts` + parity, `vessel-shape.ts`, lenses, legend counts. 3. HarborMap additive
props, Vessel/LensFill, LOD. 4. Ops page (tabs, date, legend-filter, filters, quick card, list alternative, SSE).
5. Drag & drop berth moves, undo. 6. Layout session, Da collocare, compaction. 7. `PlacementPicker`, schema mode.
8. e2e specs; perf budget last (profile, memoise, then assert).

**WEB-DESK** 1. Hooks. 2. Dialog stubs → real `public.ts` components (search panel first, it unblocks OPSMAP
integration). 3. Transit wizard and detail. 4. Quotes. 5. Contracts. 6. Movements panel and presences. 7. Dock phone
panel. 8. Planner. 9. Occupancy settings and closures. 10. e2e helpers, specs, acceptance spec last.

---

## 13. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | **New permission `contracts.read`** (owner, director, office, accounting, dock) for lists/details of quotes, contracts, transits, inquiries and releases. 26.1 has only the write row; accounting (M5 charges) and dock (transit details at the desk) need reads; viewer and yard roles do not (customer data). The day view and movements use `map.view` with customer data gated by `customers.read`. |
| D2 | **Quotes are `contracts.write` only**; the dock sells transits directly (`transits.desk`) without quotes (26.1 "transiti al banco"). |
| D3 | **Occupancy rows have a composite primary key** equal to the spec's unique constraint and no `id`/timestamps (deviation from the 6 convention for the three row tables only), to keep 2 M rows/year small; FKs to elements are `RESTRICT` (history kept, elements are deactivated, not deleted), to allocations `CASCADE`. |
| D4 | **Allocation statuses** `active`, `cancelled`, `expired`, `superseded`; only `active` allocations have rows; truncation keeps `active`. Reports (M7) read allocations of any status. |
| D5 | **Hold lifetimes**: quote options until the end of `valid_until` (marina time); draft contracts `draft_hold_days` (7); desk and unexpected-arrival held transits `desk_hold_hours` (24). `PORTO_HOLD_TTL` (10 min) is reserved for online holds (M8). Expiry never changes contract state inside occupancy: the `contracts.expire` job reconciles (§3.3). |
| D6 | **Draft mooring contracts hold their berth** (kind `hold`); "attiva" = "firmato su carta" (13.3 step 5 manual confirmation). Create with `activate=true` skips draft. Losing the hold sets `hold_lost` and activation re-allocates or fails 409. |
| D7 | **Search covers berths, linear areas and land resources**; surface areas (yards, sheds) are allocated only with explicit placements (map drag or picker) until the M9 planner/optimizer. |
| D8 | **Every allocation has a concrete placement** (10.7 "ogni hold occupa sempre una risorsa concreta" applied to all kinds). "Togli dall'area" exists only inside a layout draft; saving with unplaced vessels fails. "Da collocare" lists draft-removed vessels and vessels arrived without booking. |
| D9 | **"Considera liberi i posti con la barca fuori"** returns solutions flagged `requires_release`; booking them requires creating the release first (staff-recorded consent, source `customer_phone`/`customer_desk`/`office`), which the UI chains. No automatic or sensor releases in M3; contracts with `release_policy = never` are never proposed. |
| D10 | **Contract type for pricing**: transit → `transit`; mooring `annual`/`seasonal`/`monthly` → same; `period` → `monthly` (priced by the list's split rule). Multi-segment stays are priced once on the first segment's resource; moves keep the price unless `reprice_on_move`. |
| D11 | **Price before write**: `pricelist.Quote` runs before the write transaction (own read tx), and the write fails 409 `conflict` if the snapshot's list is no longer the active one for the arrival; no pricing inside the SQLite write queue. |
| D12 | **Transit confirmation in M3 = manual driver only** (`confirmation_source = manual`, free-text reference, `pay_at_departure`). It is the single function the M5/M8 payment webhook will call with `provider`. `pending_payment` is not reachable in M3. |
| D13 | **Held transit + arrival**: the presence is recorded but the transit stays `held`; confirming later chains `confirmed → arrived`. Early departure of an arrived transit frees the remaining nights; late departure produces `departure_overdue` until a stay change. |
| D14 | **Cancel / no-show / expiry delete all rows** of the allocation (past nights included); history lives in allocations, contracts, audit. `no_show` allowed from the arrival date. |
| D15 | **Releases apply to `contract` allocations only** (transits are shortened instead); `from ≥ today`; revoke re-occupies only free nights and reports resold nights with alternatives; `resold_allocation_id` = first resale, `resold_nights` counts nights; credits are M4. |
| D16 | **Moves**: past nights never move; interval moves create a return allocation; swap uses the other vessel's placement on areas; the undo window is 24 h and only the latest move of each allocation is undoable, and only if the created allocations are untouched. |
| D17 | **Layout save** requires the full `base` of the area's allocations intersecting `from_night` with versions (stale → 412 and client re-apply); deletes all touched rows before inserting so vessels may exchange places. |
| D18 | **Closures** block new arrivals (allocations whose first night falls in the closure) for `desk`/`api` channels, not stays already running nor moves; scopes marina/map/structure/group/resource; managed by `harbor.configure`. Arenile-like online quotas are M8. |
| D19 | **Fit details**: LOA tolerance from `harbor_settings.loa_tolerance_cm`; beam has no tolerance; multihull needs the berth attribute tag `multihull_ok` (areas always allow); unknown depth/draft/air draft skip the check; insurance is a warning only in M3 (the blocking policy of 11.5 is M4 with contracts' document checks); land kinds other than cradle/trailer spot never block. |
| D20 | **Numbering** per marina, series and local creation year with a counter row; codes `ORM-`, `TRN-`, `PRV-` + year + 4 digits; the unique constraint is the guarantee. |
| D21 | **Presence state** is a per-vessel row (`vessel_presence`) updated in the presence transaction, so a vessel cannot be in port twice; the "two active presences" anomaly is a sensor matter (M12). Arrival of a vessel already in port → 409. |
| D22 | **SSE message** keeps `entity_ids` and adds the optional `map_id`, `resource_ids`, `nights{from,to}` of spec 27.4; the client narrows day-view invalidation by `map_id` and debounces 250 ms. |
| D23 | **Lenses in M3**: Occupazione, Tipo di contratto, Tipo di imbarcazione, Presenze, Scadenze, Adeguatezza, Utenze (static power/water), Anomalie (§8.4). Pagamenti (M5), Diritti d'uso (M4), Manutenzione (M9), Sensori (M12) are not shown. Legend counts are computed client-side from the day view; the anomaly list is computed by the server. |
| D24 | **Draw mode** (scale/schema) and the last lens are remembered per user in `localStorage` (per-viewer convenience, safe to lose); schema tiles keep order along the structure with a minimum 11 px code. |
| D25 | **Silhouettes** are TS only in M3 (operational map); the Go mirror and printing with vessels come with the M4 PDF renderer. "Stampa mappa" keeps the M1 print page. |
| D26 | **Perf budget**: on the CI Chromium runner avg frame ≤ 20 ms and p95 ≤ 34 ms over 3 pans (overview, 4× zoom, lens switch) on 3 040 berths with 2 000 occupied; a 4× CPU-throttled run is logged as the tablet approximation. Falling short requires the ADR 0004 Canvas-background fallback via a new ADR, not a lower budget. |
| D27 | **Contract `ends_on` defaults**: annual = starts_on + 1 year (Feb 29 → Feb 28), seasonal = the season window end of the active seasonal list (else required), monthly = + 1 calendar month (end-of-month clamp), period = required. |
| D28 | **Quote options**: 1–3 alternatives with 1–2 segments each; holds per alternative optional; accepting one releases the others' holds in the same transaction. "Sent" is a manual mark (no email/PDF until M4). |
| D29 | **Unexpected arrival** creates the vessel with declared estimated dimensions (the dock can then record a measurement, M1 dialog), a `held` transit without customer (required before confirmation) departing the next day by default, and the arrival presence, in one transaction. |
| D30 | **Season simulation** uses the HTTP API of the real app with a fixed PCG seed (overridable by `PORTO_SEASON_SEED`), an in-memory oracle compared exactly with the rows, the invariant queries every 100 operations, a no-effect check after every failed operation, and requires ≥ 1 400 successes with every operation kind exercised. |
| D31 | **Day view anomalies** use `arrival_grace_minutes` (120) after the expected arrival time (`expected_arrival_time` or `default_arrival_time` 14:00); "absent for more than N days" uses `absent_days_threshold` (7). |
| D32 | **Phone panel**: `/dock` is a responsive route of the admin SPA (31.3), no offline queue in M3 (M6); presence POSTs already accept `Idempotency-Key` so M6 can queue them without API changes. |
| D33 | **Lazy-reclaim exemptions of the invariant queries** (proposed during the M3 fix pass, to be signed off by the orchestrator with the invariants-guardian): Q8 accepts an `expired` allocation only for a transit still `held` (hold reclaimed at conflict time, §3.3, not yet closed by `contracts.expire`); Q7 exempts only a `held` transit with an `expired` allocation that no active allocation succeeds (`moved_from_id`); Q16 checks the resold allocation's marina and resource but not the overlap with the release (the reseller may later be truncated or moved, and the release clipped). Quotes: Q8 requires an active `hold` allocation for every placement of a hold option of a `draft`/`sent` quote with `valid_until ≥ today`. |
| D34 | **Move price effect** (spec 10.5 step 4, §3.5 step 6): the remaining nights `[from_night, to_night ?? last]` of each moved contract allocation are quoted on the old and on the new resource (services excluded) before the write transaction; `delta = new − old`, summed per contract, reported as `price_effect` by move, swap, layout save and check. It is applied when the contract policy has `reprice_on_move` or the move request sets `reprice` (any explicit `reprice` needs `contracts.write`, else 403): the total moves by the delta and the snapshot keeps its pinned request and breakdown and gains an `adjustments` entry (INV-20 still reproduces the original quote); audit `contract.reprice`/`transit.reprice` and `contract.changed`/`transit.changed` (`repriced`) in the move transaction. The `allocation.move` audit diff always carries `price_delta_cents` and the old/new totals. A leg that cannot be priced is not reported, unless the price must be applied (then the move fails with the pricing error). Applying is refused (`409 conflict`, the move writes nothing) for a contract without a price snapshot or when the total would go below zero. Moves inside the same area keep the price. |
| D35 | **Moving a quote hold** moves it as a whole (`from_night` = its first night, no interval): the option placement follows the new allocation (resource, position, nights), and an undo points it back. Partial moves of a quote hold are refused (`422 validation_failed`, `quote_hold_partial`). |
| D36 | **Revoke never rewrites past nights**: the default range of a revoke starts today at the earliest; an explicit range starting before today, or a release entirely in the past, is `409 release_invalid` (`past_nights`). |
| D37 | **Segments over linear areas**: when no single resource covers the stay, segment solutions chain berths, linear areas and land resources; on a linear area a segment takes the longest run some placement holds from its first night (found by bisection, free runs are monotone). Surface areas stay out of the search (D7). |
