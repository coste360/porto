# M10 — Cantiere: work order

Status: Plan phase output, authoritative for the M10 builders. If it conflicts with `docs/spec/`, the spec wins:
stop and report to the orchestrator. No open decision of `docs/spec/33-decisioni-aperte.md` is touched. §17 lists
every detail decided here where the spec is silent; CONTRACT records the binding ones in ADR 0015.

Sources: spec 18 (all), 20 (equipment as resources, maintenance out-of-service), 21 (yard report and tiles), 26.1
(yard roles), 27 (`/yard/quotes`, `/yard/work-orders`, `/yard/planner`, `/timesheets`, `/stock-movements`,
`/equipment-slots`, event `work_order.status_changed`, topic `yard.changed`), 29.2 (offline: hours and task
completion), 32 (formula fuzzing, M10 acceptance), 02.3 (no payroll, no full warehouse), 05 (slots), 06.8 (tables),
09.4 (travel-lift request on water ↔ land moves), 12 (items `labour`/`material`/`service`), 15.1 (templates
`yard_quote`, `yard_work_order`), 16 (charges, adjustments, export). `docs/invariants.md` INV-01, 20, 22, 30, 51, 54,
57, 61–64, 70, 71, 73, 83. ADR 0001, 0003, 0006 (formula language), 0008, 0009, 0011, 0012, 0014. Plans:
`m2-plan.md` (items, price lists, rule selection), `m3-plan.md` / `m4-plan.md` (quote states, send, magic link,
documents), `m5-plan.md` (`charge.CreateTx`, origin `work_order` reserved, `chargeplan`, adjustments, export),
`m6b-plan.md` (offline kinds `yard.timesheet` / `yard.activity_done` reserved, landing seam), `m7-plan.md` (report
key `yard`, tiles `yard_open_orders`, `yard_hours`, `yard_margin` reserved), `m9-plan.md` (`equipment` registry,
storage movements `equipment_hint`, offline patterns, `client_ref`).

## 0. Ground rules (every builder)

1. **Arenile is reference only** (ADR 0001). Arenile has no yard, no formulas, no planner: nothing to read. Never
   copy files, code, comments or tests; never modify `../arenile`.
2. **No double booking of lifting equipment, DB only** (ADR 0003): `equipment_slots` unique `(equipment_id, date,
   slot)` (and `operator_slots` unique `(employee_id, date, slot)`) are the guarantee. Never "check then insert".
3. **PostgreSQL READ COMMITTED**: lock first (`FOR UPDATE` on the quote / work order / booking / equipment row, in
   id order when several), then re-check state and versions under the lock. SQLite has one writer.
4. **Pure packages**: `formula`, `yardquote/price`, `workorder/account`, `workorder/states`, `yardplan/load`,
   `yardplan/auto`, `lifting/slots` — no store, no clock, no `time.Now`, no `math/rand`, no float. CONTRACT adds
   them to `pure-engines` / `pure-engines-no-rand` in `.golangci.yml`. `pricing` and `chargeplan` stay pure.
5. Units: money `money.Cents`; lengths `geo.Cm`; areas `geo.Cm2`; quantities, hours, formula values `qty.Milli`;
   percentages basis points (`_bp`); dates `calendar.Date` (marina tz); slots = 15-minute index 0..95 of the local
   wall-clock day.
6. Every state change (quote, work order, task, time entry, billing, booking) writes audit + outbox in the same tx.
   Yard quotes are **not** mooring quotes: they live in `yardquote`, INV-30 keeps covering `quotes`/`contracts`
   only (D3). **INV-51**: exported charges immutable; corrections are linked adjustment charges.
7. Every query scoped by `marina_id` from `p.Authorize`; cross-marina = 404. Permissions in domain services.
   No PII in logs, events, SSE (INV-70): employee names, customer data, supplier names never in events; ids, enums,
   dates, amounts of orders only.
8. API first: CONTRACT edits `api/openapi.yaml` and runs `make gen`; never edit generated files. Migrations
   `00020_yard_catalog_quotes.sql`, `00021_work_orders.sql`, `00022_equipment_slots.sql`, both dialects, same names.
   Never touch 00001–00019.
9. UI: shadcn only (official CLI, exported from `@porto/shared`), Tailwind tokens, no custom CSS, i18n it (source)
   + en. Phone screens work from 360 px. No new npm dependency (drag and drop with pointer events as in M9).
10. `cmd/porto` stays `CGO_ENABLED=0`; no new Go dependency (rapid present; fuzzing is `testing.F`).
11. Do not `git commit`; no destructive git; no `rm -rf`; Docker only for your own throwaway containers
    (`scripts/with-postgres.sh`). Edit only files you own (§16). Iterate on touched packages only; full gates §16.4.

## 1. Scope

### 1.1 In M10

| Area | Content |
|---|---|
| Catalogue (18.1) | Work types (`work_types`: taxonomy for margins), qualifications (`yard_qualifications`, each with a `labour` item = sale rate from the price lists and a default internal cost), yard settings of items (`yard_item_settings`: cost price, unit, default quantity/price formulas, qualification for labour items), formula variables (vessel, derived per marina, job inputs), band tables, work packages with immutable versions. Prices stay in M2 price lists. |
| Formula (18.2, ADR 0006) | Pure `internal/domain/formula`: recursive-descent parser, type check, fixed-point evaluation (`qty.Milli`, half-even at every `*` and `/`), functions `if min max round ceil floor band`, limits, errors at save with a test case, fuzzing. |
| Guided quote (18.3) | 5 steps: vessel/customer → packages, services, products → job variables → summary (computed qty and price, per-line overrides marked) → conditions, validity, deposit, billing mode. Labour lines carry estimated hours per qualification. Send (PDF + magic link + email, as 13.2/M4), accept (panel or link), decline, expire. Accept creates the work order in the same tx. Variations (extra works) = variation quotes on an order. |
| Work orders (18.4) | States `planned → in_progress ⇄ on_hold → completed → delivered / invoiced`, `cancelled`; responsible, planned start/delivery, vessel position (allocation link), tasks with qualification, estimated hours, finish-to-start dependencies, checklist, photos, completion %; order completion = hours-weighted mean; board "barche in lavorazione". |
| Planner (18.5) | Employees (distinct from users, optional user link), qualifications, hourly internal cost, weekly calendar, absences, department; grid employees × days (day/week/month), drag and drop with qualification and residual-capacity checks; workload per employee, department, qualification and bay; at-risk deliveries; greedy auto-plan proposal to confirm. |
| Time entries (18.6) | Timer start/stop from the phone or manual entry per task; approval by the yard manager; offline as 29.2 allows (`yard.timesheet`). Task progress/checklist/photo offline (`yard.activity_done`). |
| Materials and external costs (18.6) | Material issues and returns on an order from the catalogue with frozen unit cost and unit price (`stock_movements` kinds `issue`, `return` only); external costs (supplier text, reference, cost, billable price). |
| Final account (18.6) | Hours × sale rate and × internal cost, materials at price and cost, services, external costs, per-line comparison with the quote, margin and margin % per order and per work type; reconciliation equations (§9.3). |
| Charging (18.6, 16) | Billing modes `actual` (consuntivo), `fixed` (prezzo chiuso), `progress` (SAL); advance at acceptance; progress and final billings through `charge.CreateTx` with origin `work_order`; deposits deducted by construction; negative remainders and late corrections as linked adjustment charges; export as M5. |
| Lifting equipment (18.7) | Travel lift, crane, forklift, trailer bookable in 15-minute slots on `equipment_slots` with an enabled operator; bookings from the order, the storage contract, the storage movement or a water ↔ land move request (9.4); day calendar also on the phone. |
| Reports (21) | Report `yard` (views hours, load, margins, margins by work type, lifting) and the three reserved tiles. |
| Phone | `/m/$id/yard-phone` for `yard` and `yard_mgr`: my tasks today, timer, manual hours, checklist, progress, photo, lift calendar of the day; approvals tab for managers. |

### 1.2 Out of M10

| Item | Milestone | Seam left |
|---|---|---|
| Warehouse/stock management: stock levels per warehouse, loads (manual or CSV), adjustments, non-negative stock rule with permission, lots, inventory, reorder (spec 18.6 "giacenza", 02.3) | after M12 (orchestrator scope decision; ADR 0015 records the deferral) | `stock_movements` exists with `kind` CHECK (`issue`, `return`); a later migration widens it to `load`, `adjust` and adds `warehouses`/`stock_levels`. UI says "Giacenze: disponibili in seguito". |
| Rental | M11 | `equipment_slots` pattern reusable for `rental_slots`; tile `rental_*` untouched |
| Sensors, smart meters | M12 | none |
| Payroll, legal attendance | never (02.3) | time entries are for planning and costing only |
| Advanced electronic signature of yard quotes/variations (FES/OTP) | after M10 | approval by panel or magic link (18.4 "link o firma": link implemented); `signature_requests.subject_type` unchanged |
| Online sale of yard services, customer-area view of work orders | after M12 | none |
| Purchase invoices, supplier registry | never / after v1 | external costs carry supplier name and reference as text |
| Automatic discounts on yard lines (customer category, promo) | after M10 | manual line discount (marked) only (D9) |

## 2. Architecture and packages

```
web/admin features/yard-catalog, yard-quotes (QUOTE) │ yard-orders, reports+dashboard yard (ORDERS) │ yard-planner, lifting, phone/yard (PLANNER)
        │                                             │                                           │
http/admin/{yard_catalog,yard_quotes}.go  http/public/yard_quote_links.go   http/admin/{work_orders,timesheets,stock_movements}.go   http/admin/{employees,planner,equipment_bookings}.go
        ▼                                             ▼                                           ▼
internal/domain/yardquote ──► formula (pure)    internal/domain/workorder ──► workorder/account (pure)    internal/domain/yardplan ──► yardplan/load, yardplan/auto (pure)
   │  └► yardquote/price (pure) ──► pricing.UnitPrice   │  billing → chargeplan + charge.CreateTx            internal/domain/lifting ──► lifting/slots (pure)
   │  accept → OrderCreator (interface, impl workorder) │  report → internal/domain/report (calc yard)       │  move requests ← outbox sink occupancy.moved
   ▼                                                    ▼                                                    ▼
internal/store (yard_catalog.go yard_quotes.go | work_orders.go timesheets.go stock_movements.go external_costs.go wo_billings.go report_yard.go | employees.go assignments.go equipment_bookings.go)
```

| Package | Kind | Owner |
|---|---|---|
| `internal/domain/formula` | pure: lexer, parser, AST, checker, evaluator, derived resolution, limits | QUOTE |
| `internal/domain/pricing/unit.go` | pure addition: `UnitPrice` (existing rule selection; no change to `Quote` output, `EngineVersion` unchanged) | QUOTE |
| `internal/domain/yardquote` (+ `price/` pure) | catalogue services, packages and versions, quote CRUD/preview/send/accept/decline/expire, variation quotes, snapshot and recompute | QUOTE |
| `internal/domain/workorder` (+ `account/`, `states.go` pure) | orders, tasks, dependencies, checklist, photos, time entries, materials, external costs, final account, billing, offline dedupe | ORDERS |
| `internal/domain/report` | yard report views and tiles | ORDERS |
| `internal/domain/yardplan` (+ `load/`, `auto/` pure) | employees, calendars, absences, assignments, planner grid, load, at-risk, auto-plan | PLANNER |
| `internal/domain/lifting` (+ `slots/` pure) | bookable equipment, operators, bookings on slots, requests, move-request sink, day calendar | PLANNER |

Dependencies: `yardquote` → `formula`, `pricing`, `pricelist`, `docs`, `magiclink`, `notify`; `yardquote` never
imports `workorder` (it calls `OrderCreator`, an interface declared in `yardquote`, implemented by
`workorder.Service`, wired in `app.go`). `workorder` → `charge`, `chargeplan`, `yardquote/price` (types only),
`yardplan` read helpers via store. `lifting` → `maintenance` read helpers via store (out of service), no import of
`workorder`. `report` reads yard tables through `internal/store/report_yard.go` only (INV-61).

## 3. Catalogue and formula model (normative, QUOTE)

### 3.1 Catalogue entities

- **Items** stay the M2 `items` (types `service`, `labour`, `material`, also `product`/`fee`), priced only in price
  lists. **`yard_item_settings`** (item_id PK): `yard_enabled`, `cost_cents` (internal cost per unit: materials,
  services; 0 allowed), `qualification_id` (required for `labour`, unique: one labour item per qualification),
  `qty_formula`, `price_formula` (defaults when the item is added alone), `category` (free text ≤ 40 for the
  wizard search), `version`.
- **`work_types`**: code (`^[A-Z0-9_-]{1,16}$`), name I18n, `revenue_class_id?` (report grouping), active, sort.
- **`yard_qualifications`**: code, name I18n, `labour_item_id` (item_type `labour`, unit `hour`),
  `default_cost_cents` (estimate cost before employees are known), `lift_operator` bool (operators of lifting
  equipment), active.
- **`formula_variables`** per marina: `name` (`^[a-z][a-z0-9_]{0,31}$`, not a reserved name), `source` ∈
  `derived` | `job`, `kind` ∈ `number` | `bool` | `enum`, `unit` (`m`, `m2`, `t`, `kw`, `h`, `pcs`, `l`, `pct`,
  `none`), `expression` (derived), `min`/`max`/`default` (milli, job), `enum_values` (job enum), label I18n,
  `version`. Vessel variables are built in (§3.3) and not stored.
- **`formula_tables`**: `name`, rows `[{up_to_milli | null, value_milli}]` ascending, last `null` = open (≤ 50
  rows), version. Used by `band(x, name)`.
- **`work_packages`**: code, name I18n, `work_type_id`, `active`, `current_version`. **`work_package_versions`**
  (immutable, insert-only trigger on both dialects): `package_id`, `version` (1..), `lines` json, `job_vars`
  (names asked in step 3), `created_by_user_id`, `created_at`; unique `(package_id, version)`. A line:
  `{seq, item_id, work_type_id?, qty_formula, price_formula?, task: {title I18n, checklist[]}?, notes}`; a
  `labour` line produces a task at acceptance; any line may declare a task.
- Saving a package (`yard.manage`) validates every line (§3.5) and inserts version N+1; nothing is updated in
  place. Deactivating hides it from the wizard; versions stay.

### 3.2 Language (ADR 0006, spec 18.2)

```
expr    := or ; or := and ("or" and)* ; and := not ("and" not)* ; not := "not" not | cmp
cmp     := sum (("<"|"<="|">"|">="|"=="|"!=") sum)?
sum     := prod (("+"|"-") prod)* ; prod := unary (("*"|"/") unary)* ; unary := "-" unary | atom
atom    := NUMBER | STRING | IDENT | "true" | "false" | "(" expr ")" | IDENT "(" args? ")"
NUMBER  := [0-9]+ ("." [0-9]{1,3})?        STRING := '"' [a-z0-9_]{0,32} '"'  (only as operand of == / != with an enum)
```

Functions: `if(c, a, b)` (lazy: only the taken branch is evaluated), `min(a, b, …)`, `max(…)` (2..8 args),
`round(x, n)` (`n` literal 0..3, half-even), `ceil(x)`, `floor(x)` (to integers), `band(x, table_name)` (value of
the first row with `x ≤ up_to`, open row otherwise; no row ⇒ `band_no_row`). No loops, no user functions, no
external data. Limits: source ≤ 500 bytes, ≤ 256 AST nodes, depth ≤ 32, ≤ 32 distinct identifiers; exceeding ⇒
`formula_too_complex`.

Semantics: numbers are `qty.Milli`; `+ -` exact (overflow ⇒ `overflow`); `*` and `/` via `qty.Mul`/`qty.Div`
(half-even to the milli at each operation, left to right); `/ 0` ⇒ `div_by_zero`; comparisons and booleans are
exact; `and`/`or` short-circuit. Types: number, bool, enum (string); mixing ⇒ type error at check time. Results:
quantity formula → number ≥ 0 (`negative_quantity`); price formula → number = **euro** with 3 decimals, converted
to cents half-even (`v / 10`), ≥ 0 (`negative_price`). `formula.LanguageVersion = 1` is stored in every snapshot;
a change of semantics bumps it and keeps the old evaluator path for recompute.

### 3.3 Variables

| Name | Source | Unit | Value |
|---|---|---|---|
| `loa`, `beam`, `draft`, `air_draft`, `lwl` | vessel | m | effective cm (measured when `dimensions_source = measured`, as M2) × 10 milli; exact |
| `displacement` | vessel | t | `displacement_kg` milli (kg/1000 t × 1000) |
| `engines` | vessel | pcs | number of entries of `vessels.engines` |
| `kw_total` | vessel | kw | Σ `power_kw` of engines (milli) |
| `hull_type` | vessel | enum | `monohull_motor`, `monohull_sail`, `catamaran`, `trimaran`, `rib`, `gozzo`, `other` |
| `material` | vessel | enum | lower-case `hull_material` folded to `[a-z0-9_]`, `""` when empty |
| `age` | vessel | years | quote date year − `year_built` (≥ 0) |
| derived (e.g. `hull_area`) | marina | as declared | expression over vessel and other derived variables (acyclic, checked) |
| job (e.g. `layers`, `damage_area`) | marina | as declared | operator input in step 3, within min/max, default prefilled |
| `qty`, `list_price` | context | — | only inside **price** formulas: the line quantity and the catalogue unit price in euro |

A vessel variable that is null (e.g. no draft) becomes a required input in step 3 (`vessel_override`, stored in the
quote, never written back to the vessel). Seeded derived variable: `hull_area = loa * (beam + draft) * 0.85`.

### 3.4 Package API (signatures written by CONTRACT)

```go
package formula
const LanguageVersion = 1
type Kind uint8 // KindNumber, KindBool, KindEnum
type Value struct { Kind Kind; Num qty.Milli; Bool bool; Enum string }
type VarDecl struct { Name string; Kind Kind; Unit string; Source string; Min, Max, Default *qty.Milli; Enum []string }
type Table struct { Name string; Rows []Row } ; type Row struct { UpTo *qty.Milli; Value qty.Milli }
type Derived struct { Name string; Src string }
type Env struct { Vars map[string]Value; Tables map[string]Table }
type Program struct{ /* immutable AST */ }
func Parse(src string) (*Program, error)                          // *SyntaxError{Pos int; Code string}
func (p *Program) Check(decls map[string]VarDecl, tables map[string]Table, context []string) []Issue
func (p *Program) Eval(env Env) (Value, error)                     // *EvalError{Code string; Pos int}
func (p *Program) Idents() []string
func ResolveDerived(defs []Derived, base Env, decls map[string]VarDecl) (Env, error) // topo order, cycle = derived_cycle
func Quantity(v Value) (qty.Milli, error)
func PriceCents(v Value) (money.Cents, error)
type Issue struct { Code, Ref string; Pos int; Params map[string]string }
```

Error codes (also problem `formula_invalid` params): `syntax`, `unknown_variable`, `unknown_function`,
`unknown_table`, `arity`, `type_mismatch`, `enum_value_unknown`, `formula_too_complex`, `derived_cycle`,
`div_by_zero`, `missing_variable`, `overflow`, `band_no_row`, `negative_quantity`, `negative_price`,
`out_of_range` (job input).

### 3.5 Validation at save

Every save of a derived variable, table, item yard settings or package version: parse + check against the current
declarations; then evaluate on the **marina test vessel** (`yard_settings.test_vessel`: default LOA 12 m, beam
4 m, draft 1.5 m, monohull_sail, 1 engine 55 kW, 2010) with job defaults, plus the optional per-line test case
`{vars, expect_qty?, expect_price_cents?}`. Any issue = `422 formula_invalid` with the list (field, pos, code);
nothing saved. Saving a derived variable or table re-validates the current version of every active package and
item formula that references it; failures block the save and list the packages.

### 3.6 Versioning so old quotes keep their formula

1. Package versions are insert-only; a quote line stores `(package_id, package_version, line_seq)`.
2. Each quote stores a **snapshot** (`yard_quotes.snapshot`, json): `language_version`, vessel variable values,
   overrides, job inputs, derived definitions and their values, tables used, every line's formula texts, unit
   prices with `{price_list_id, rule_id, unit_cents}` and item VAT data at quote time.
3. `yardquote/price.Recompute(snapshot)` reproduces the stored lines exactly (**INV-28**); variables, tables,
   packages and price lists edited later never change an existing quote. Re-pricing is an explicit "Ricalcola"
   on a `draft` (new snapshot, audited).

### 3.7 Unit prices from the catalogue (pure `pricing.UnitPrice`)

`pricing.UnitPrice(cats []Catalog, itemID string, on calendar.Date, customerCategory string) (UnitPriceResult,
error)`: among the base rules of `itemID` in the given active catalogues whose list validity covers `on`, rate
period scope matching `on`, customer category scope matching (empty = any; contract type, berth, group, channel
scopes ignored), the winner by the existing `ruleCmp` (priority → specificity → recency); unit = `UnitCents` else
`AmountCents`. Result `{UnitCents, PriceListID, RuleID, PriceIncludesVAT}`; none ⇒ `*NoPriceError{Reason:
"no_rule"}` ⇒ quote line `no_price` issue (step 4 blocks send until priced by catalogue or a marked manual price).
`pricelist.Service.UnitPricesTx(ctx, tx, scope, itemIDs, on, customerCategory)` loads the active catalogues once.

## 4. Guided quote (normative, QUOTE)

### 4.1 Model

`yard_quotes`: number (series `yard_quote`, display `PC2026-000012`), `kind` (`job`, `variation`),
`work_order_id?` (variation), `customer_id`, `vessel_id`, `status` (`draft`, `sent`, `accepted`, `declined`,
`expired`), `quoted_on`, `valid_until`, `expires_at`, `billing_mode` (`actual`, `fixed`, `progress`),
`deposit_bp?` / `deposit_cents?` (one), `conditions` (≤ 4000), `notes`, `planned_start_on?`,
`planned_delivery_on?`, `job_inputs` json, `vessel_overrides` json, `snapshot` json, totals (`net_cents`,
`vat_cents`, `gross_cents`, `deposit_amount_cents`), `estimated_hours` json per qualification, `sent_at`,
`decided_at`, `accepted_via` (`panel`, `link`), `decline_reason`, `created_by_user_id`, `version`.
`yard_quote_lines`: `seq`, `source` (`package`, `item`, `manual`), `package_id?`, `package_version?`,
`package_line_seq?`, `item_id`, `work_type_id`, `qualification_id?`, `description`, `qty_formula?`,
`price_formula?`, `qty_milli`, `unit_cents`, `discount_cents`, `qty_overridden`, `price_overridden`, `net`, `vat`,
`gross`, VAT rate/nature, `price_includes_vat`, `estimated_hours_milli` (labour = qty), `task` json?.

### 4.2 Flow (wizard; every step calls `POST M/yard/quotes/preview`, stateless, no write)

1. **Vessel and customer**: existing vessel and customer (`customers.read`); vessel data shown with missing
   variables flagged.
2. **Catalogue**: packages (current version), single yard items, search and category filter; multiple selection.
3. **Variables**: the union of job variables of the chosen packages/items + missing vessel variables; typed inputs
   with unit, min, max, default.
4. **Summary**: per line qty and unit price computed (formula + catalogue); editable qty, unit price, discount
   per line (sets `qty_overridden` / `price_overridden`, shown with a "manuale" badge); totals with VAT summary;
   estimated hours per qualification; issues (no price, missing variable) block saving as `sent`.
5. **Conditions**: validity (default `yard_settings.quote_validity_days` = 30), deposit (default
   `yard_settings.deposit_bp` = 3000), billing mode (default `actual`), planned dates, conditions text.

Line computation (`yardquote/price.Line`): `env = vessel ∪ overrides ∪ derived ∪ job`; `qty = Quantity(qty_formula)`
(default `1`); `list = UnitPrice`; `unit = price_formula ? PriceCents(eval with qty, list_price) : list`; then
overrides; `amount = qty.MulCents(unit) − discount`; VAT as M5 lines: net prices `vat = net.PercentBP(rate)`,
gross prices `money.VATSplit`; totals = Σ lines; deposit = `gross.PercentBP(deposit_bp)` or the fixed amount
(≤ gross).

### 4.3 States and operations (`yard.manage`; reads `yard.read`)

| From | Operation | To | Effects |
|---|---|---|---|
| – | `createYardQuote` | `draft` | lines priced, snapshot stored; event `yard_quote.changed` |
| `draft` | `updateYardQuote` (If-Match) / `recompute` | `draft` | new snapshot |
| `draft`,`sent` | `sendYardQuote` (`manual?`) | `sent` | PDF (template `yard_quote`), magic link (`access_links.purpose = quote`, `subject_type = yard_quote`, expiry = `expires_at`), email; `422 customer_email_missing`; `yard_quote.sent` |
| `draft`,`sent` | `acceptYardQuote` (panel) / `acceptPublicYardQuote` (link) | `accepted` | under `FOR UPDATE`: not expired (`409 quote_expired`), no blocking issues; `OrderCreator.CreateFromQuoteTx` (job) or `ApplyVariationTx` (variation) in the **same tx**; `yard_quote.accepted` |
| `draft`,`sent` | `declineYardQuote` | `declined` | `yard_quote.changed` |
| `draft`,`sent` | job `yard.quotes_expire` (hourly) at `expires_at` | `expired` | `yard_quote.expired` |

A quote creates at most one work order (`work_orders.quote_id` unique; variation: `work_order_variations.quote_id`
unique) — **INV-96**. Accepting twice concurrently: one 200, the other `409 invalid_transition`.

### 4.4 Worked example (acceptance criterion 1)

Catalogue (seed and acceptance fixture; prices in an active price list, flat base rules, VAT 22 %, net prices):

| Item | Type / unit | Unit price | Cost | Package line formula (qty) |
|---|---|---|---|---|
| `SRV-ALAG` haul-out | service / m | 18.00 | 0 | `loa`; price formula `if(hull_type == "catamaran", list_price * 1.5, list_price)` |
| `SRV-LAV` hull wash | service / m² | 4.00 | 1.00 | `round(hull_area, 1)` |
| `LAB-HAND` yard hand hour (qual. `hand`, cost 22.00) | labour / h | 38.00 | — | `round(hull_area * 0.15, 1)` (task "Carteggiatura") |
| `LAB-PAINT` painter hour (qual. `painter`, cost 26.00) | labour / h | 45.00 | — | `round(hull_area * layers * 0.05, 1)` (task "Antivegetativa") |
| `MAT-AVEG` antifouling 2.5 l tin | material / piece | 95.00 | 58.00 | `ceil(hull_area * layers / 25)` |
| `SRV-VARO` launch | service / m | 18.00 | 0 | `loa` |

Package `ANTIVEG` v1 (work type `ANTIFOULING`), job variable `layers` (number, 1..4, default 2). Vessel: LOA
1200 cm, beam 400 cm, draft 150 cm, `monohull_sail`. `hull_area = 12.000 × (4.000 + 1.500) × 0.850 = 66.000 ×
0.850 = 56.100`.

| Line | Qty | Unit | Net | VAT 22 % |
|---|---|---|---|---|
| SRV-ALAG | 12.000 | 18.00 | 216.00 | 47.52 |
| SRV-LAV | 56.1 | 4.00 | 224.40 | 49.37 (49.368) |
| LAB-HAND | 8.4 (8.415 → half-even 1 dp) | 38.00 | 319.20 | 70.22 (70.224) |
| LAB-PAINT | 5.6 (5.610) | 45.00 | 252.00 | 55.44 |
| MAT-AVEG | 5 (ceil 4.488) | 95.00 | 475.00 | 104.50 |
| SRV-VARO | 12.000 | 18.00 | 216.00 | 47.52 |
| **Total** | | | **1,702.60** | **374.57** → gross **2,077.17** |

Estimated hours: `hand` 8.4, `painter` 5.6. Deposit 30 % = `207717 × 3000 / 10000 = 62315.1` → **623.15**. No line
is manual. Variant table cases: `layers = 3` ⇒ painter 8.4 h = 378.00, tins `ceil(6.732)` = 7 = 665.00;
catamaran ⇒ haul-out unit 27.00, line 324.00.

## 5. Work orders and tasks (normative, ORDERS)

### 5.1 Model

`work_orders`: number (series `work_order`, `C2026-000034`), `quote_id?` (unique), `customer_id`, `vessel_id`,
`work_type_id?` (main), `responsible_user_id?`, `planned_start_on?`, `planned_delivery_on?`, `allocation_id?`
(current position: berth, yard/shed cells, land resource; picker of the vessel's active allocations) +
`position_note`, `status`, `billing_mode`, `rate_snapshot` json (frozen at creation: unit price per item of the
budget and per labour item of every active qualification, from the quote snapshot or `UnitPrice` on the creation
date), `completion_bp` (stored, recomputed on every task change), `started_at`, `completed_at`, `delivered_at`,
`cancelled_at`, `cancel_reason`, `version`. `work_order_lines` (budget = accepted quote + variations): copy of
quote lines with `origin` (`quote`, `variation`), `consumed_qty_milli?` (services/products: actual quantity,
default = budget qty), `work_type_id`. `work_order_variations` (quote_id unique, accepted_at).

### 5.2 States (pure table `workorder/states.go`)

| From | To | Operation (permission `yard.manage`) | Guards |
|---|---|---|---|
| – | `planned` | create from quote (accept) or `createWorkOrder` (no quote: lines added from the catalogue with the quote line engine) | |
| `planned` | `in_progress` | `start` (also implicit on the first approved time entry) | |
| `in_progress` | `on_hold` / `on_hold` → `in_progress` | `hold(reason)` / `resume` | |
| `in_progress` | `completed` | `complete` | warning (not error) when tasks < 100 % |
| `completed` | `in_progress` | `reopen` | no final billing |
| `completed` | `delivered` | `deliver` (sets `delivered_at`) | |
| `completed`,`delivered` | `invoiced` | final billing (§10.3, `charges.manage`) | |
| `invoiced` | `invoiced` | `deliver` sets `delivered_at` only (D18) | |
| `planned`,`in_progress`,`on_hold` | `cancelled` | `cancel(reason)` | no live charge of the order (`409 work_order_has_charges`); future lifting bookings cancelled in the same tx |

Every transition: audit + `work_order.status_changed` `{work_order_id, from, to}` (public, webhook catalogue) +
SSE `yard.changed`. `invoiced` means "final charge issued" (Porto never invoices, D18).

### 5.3 Tasks `work_tasks`

`work_order_id`, `seq`, `title`, `qualification_id?`, `estimated_hours_milli`, `status` (`todo`, `in_progress`,
`done`, `cancelled`), `progress_bp` (0..10000; `done` ⇔ 10000), `checklist` json `[{key, label, done}]` (≤ 40),
`planned_from?`/`planned_to?` (derived from assignments, cached), `source_line_id?`, `client_ref?`, `version`;
`work_task_deps` (`task_id`, `depends_on_task_id`, unique, same order, acyclic checked in Go under the order lock);
`work_task_files` (photos, documents; stored files as M9). Order completion `completion_bp = Σ(progress_bp ×
est_hours) / Σ est_hours` (half-even); when Σ est_hours = 0, plain mean; cancelled tasks excluded. Dependencies
are finish-to-start: the planner refuses an assignment day before the last assigned day of a predecessor
(`422 dependency_violation`); starting a timer on a task whose predecessors are not `done` is a warning.

Board `GET M/yard/board`: columns by status (cancelled hidden), card = number, vessel name, LOA, completion,
planned delivery, delay days (`today − planned_delivery` when not delivered), hours approved / estimated.

## 6. Planner and load (normative, PLANNER)

### 6.1 Employees

`employees`: `code`, `first_name`, `last_name` (PII: never in events/logs), `user_id?` (unique per marina; the
phone identity), `department` (text ≤ 40), `hourly_cost_cents`, `active`, `version`; `employee_qualifications`
(employee_id, qualification_id) unique; `employee_calendars` (employee_id, weekday 1..7, `minutes` 0..960) PK
(employee_id, weekday); `employee_absences` (employee_id, from_on, to_on, kind `vacation|sick|leave|other`,
`minutes_per_day?` null = whole day, note). CRUD `yard.manage`.

### 6.2 Assignments

`task_assignments` (task_id, employee_id, work_date, `hours_milli` > 0, version) unique `(task_id, employee_id,
work_date)`. `PUT M/yard/planner/assignments` (`yard.manage`, batch, If-Match per task version): checks under the
task lock: task not `done`/`cancelled`, order not `invoiced`/`cancelled`; employee holds the task qualification
(`422 qualification_mismatch` unless `force_qualification` — audited); FS dependencies (§5.3); capacity: when
assigned(e, d) + hours > capacity(e, d) ⇒ `409 capacity_exceeded {employee_id, date, capacity, assigned}` unless
`allow_overload` (overload stays highlighted). Soft rules, not invariants.

### 6.3 Load model (pure `yardplan/load`)

- `capacity(e, d) = calendar_minutes(e, weekday(d)) − absence_minutes(e, d)` (≥ 0) as hours milli
  (`qty.FromFraction(min, 60)`); marina closures do not apply (yard has its own calendar).
- `assigned(e, d) = Σ hours_milli` of assignments; `actual(e, d) = Σ approved time entry hours`.
- `load_bp = assigned × 10000 / capacity` (half-even); capacity 0 and assigned > 0 ⇒ `overload = true`,
  `load_bp = null`. Overload ⇔ assigned > capacity.
- Aggregates (week, month, department, qualification, bay): `Σ assigned / Σ capacity` (never mean of ratios).
  **Bay** = the work position of the order (`allocation` resource: berth, surface area, land resource; orders
  without position grouped as "—"); bay load = assigned hours per day of tasks whose order sits there, no capacity
  (D22).
- **At risk** per order and qualification: `remaining = Σ est_hours × (10000 − progress_bp) / 10000` of open tasks;
  `free = Σ_{d = today..planned_delivery} Σ_{e qualified} max(capacity − assigned_to_other_orders, 0)`; at risk ⇔
  remaining > free, or planned delivery < today and not completed.

### 6.4 Auto-plan (pure `yardplan/auto.Propose`)

Input: unplanned remaining hours per task (est × remaining progress − already assigned future hours), orders
sorted by planned delivery then number, tasks by seq respecting FS, employees with qualifications and capacity,
existing assignments, `from` (tomorrow default), horizon (≤ 90 days). Greedy: for each task, day by day from
max(from, predecessors' last day + 1), pick the qualified employee with the lowest load on that day (ties by
employee id), assign min(free capacity, remaining) in 0.5 h steps, until done or horizon; never exceeds capacity.
Output proposal with unplaceable tasks listed; deterministic for equal input. `POST M/yard/planner/proposal`
(read only) and confirm via the batch assignment endpoint (versions checked).

## 7. Time entries (normative, ORDERS; phone UI by PLANNER)

`timesheets`: `employee_id`, `work_order_id`, `task_id?`, `work_date`, `started_at?`, `ended_at?`,
`hours_milli`, `source` (`timer`, `manual`), `status` (`running`, `submitted`, `approved`, `rejected`,
`voided`), `cost_rate_cents?` (frozen at approval from the employee), `cost_cents?` (= `hours.MulCents(rate)`),
`note`, `approved_by_user_id`, `approved_at`, `reject_reason`, `client_ref?`, `voids_id?`, `version`. Unique
partial `(employee_id) WHERE status = 'running'` (one running timer per employee, **INV-95**); unique partial
`(marina_id, client_ref)`.

- Permissions: `yard.timesheets` for own entries (employee linked to the caller's user); `yard.manage` for anyone
  and for approval. Order must be `planned`/`in_progress`/`on_hold`/`completed` (`409 work_order_closed`).
- **Timer**: `startTimer {task_id | work_order_id, started_at?, client_ref}` → `running`; `stopTimer {ended_at?}`
  → `submitted`, `hours = round((ended − started) in minutes / 60, 2)` (`qty.FromFraction` then `Round(2)`);
  `work_date` = local date of `started_at`. Duration > 16 h ⇒ `422 duration_too_long`; job `yard.timers_guard`
  (15 min) flags running timers older than 16 h for the manager (never closes them).
- **Manual**: `createTimeEntry {task_id|work_order_id, work_date, hours (0.25..16, step 0.25) | started_at+ended_at}`
  → `submitted`.
- **Approval** (`yard.manage`): approve (freezes cost rate and cost), reject (reason; owner edits and resubmits);
  approved entries are never edited: `void` creates nothing and marks `voided` (only while the order is not
  `invoiced`); after `invoiced` corrections are new approved entries flagged `late` and go to a conguaglio (§10.4).
- **Offline** (29.2 "ore lavorate e completamento di attività", kind `yard.timesheet`): `startTimer`, `stopTimer`
  (dependsOn bind `timesheetId`), `createTimeEntry`; kind `yard.activity_done`: `updateTaskProgress` (progress,
  checklist, status `done`), `uploadTaskPhoto` (dependsOn bind `taskId`). Device timestamps accepted when
  `queued_at − 48 h ≤ t ≤ queued_at + 5 min` (`Porto-Offline-Queued-At`), else `422 offline_time_out_of_range`.
  Server dedupe: `client_ref` unique + `Idempotency-Key` (INV-83); a replay returns the first result; task
  progress replays apply max(progress) (never decreases through replay, D25). None creates, moves or releases
  allocations or bookings (INV-71). Approval, materials, billing, bookings stay online only.

## 8. Materials and external costs (normative, ORDERS)

- `stock_movements`: `work_order_id`, `task_id?`, `item_id` (type `material` or `product`), `kind` (`issue`,
  `return`), `qty_milli` > 0, `unit_cost_cents` (default `yard_item_settings.cost_cents`, editable),
  `unit_price_cents` (default: `rate_snapshot` price when the item is in the budget, else `UnitPrice` on the
  movement date; editable with `price_overridden`), `moved_on`, `note`, `late`, `created_by_user_id`. A return
  cannot exceed the net issued qty of that item on the order (checked under the order lock). `yard.manage`
  (`yard.timesheets` may issue on own tasks when `yard_settings.workers_issue_materials`, default false). No stock
  levels (§1.2).
- `work_order_external_costs`: `supplier_name` (≤ 100), `reference` (≤ 60), `incurred_on`, `description`,
  `work_type_id`, `cost_cents` > 0, `billable` bool, `price_cents` (default `cost × (10000 +
  yard_settings.external_markup_bp) / 10000`, half-even; default markup 2000), `item_id` (default
  `yard_settings.external_item_id`, required when billable), `late`, `version`. `yard.manage`.

## 9. Final account (normative, ORDERS, pure `workorder/account`)

### 9.1 Inputs and lines

`account.Build(in Input) (Account, error)`; `Input{Mode, Budget []BudgetLine, Rates RateSnapshot, Entries
[]ApprovedEntry, Movements []Movement, External []ExternalCost, Services []ConsumedLine, ItemVAT map}`.
Actual lines (one per key; key = `item:<item_id>|wt:<work_type_id>`):

| Source | Qty | Revenue (net) | Cost |
|---|---|---|---|
| labour per qualification × work type | Σ approved hours (task's work type, else order's) | `qty.MulCents(rate of the labour item)` on the aggregate | Σ entry `cost_cents` (per entry, frozen) |
| material/product per item × work type | Σ issue − Σ return | `Σ movement qty.MulCents(unit_price)` signed | `Σ movement qty.MulCents(unit_cost)` signed |
| service/other budget lines | `consumed_qty` | `consumed_qty.MulCents(budget unit) − discount` | `consumed_qty.MulCents(item cost)` |
| external cost | 1 per row | `price_cents` if billable else 0 | `cost_cents` |

VAT per line as M5. Budget lines are the quote/variation lines (§5.1).

### 9.2 Definitions

- **Revenue** (net) of the account: `actual` mode = Σ actual line revenue; `fixed` and `progress` = Σ budget line
  net (billable external costs added only when `yard_settings.fixed_includes_external = false`, default false ⇒
  they are added, D16).
- **Cost** = labour cost + material cost + service cost + external cost (all modes).
- **Margin** = revenue − cost; **margin %** = `margin × 10000 / revenue` bp half-even, `null` when revenue = 0.
- **Per work type**: the same on the lines of that work type. **Vs quote** per key: budget qty/net vs actual
  qty/net and delta.
- Billing target (§10) = the revenue lines with VAT (actual lines or budget lines by mode).

### 9.3 Reconciliation equations (INV-93; property tests and query Q30)

1. `Σ lines.revenue = revenue_labour + revenue_material + revenue_service + revenue_external = Σ_work_types revenue`.
2. Same identity for cost and margin; `margin = Σ_work_types margin`.
3. `hours_labour(order) = Σ approved, non-voided timesheets.hours_milli` exactly; `cost_labour = Σ cost_cents`.
4. `material qty(item) = Σ issue − Σ return`; material cost/revenue = Σ signed movement amounts.
5. `cost_external = Σ external cost_cents`; `revenue_external = Σ billable price_cents`.
6. After the final billing: `Σ gross of live charges with origin work_order of the order (incl. adjustments) =
   target gross`; and per VAT bucket the same for net and VAT (as INV-57).
7. Quote vs actual: `Σ_keys (actual_net − budget_net) = actual revenue − quote net`.

### 9.4 Worked final account (acceptance criterion 2)

Order from §4.4, mode `actual`. Approved hours: hand Luca 5.0 h (cost 22.00), Marco 4.0 h (cost 24.00); painter
Giulia 5.5 h (cost 26.00). Materials: issue 6 `MAT-AVEG`, return 1 (cost 58.00, price 95.00). Services consumed as
budget. External: propeller polishing, work type `PROPULSION`, cost 150.00, billable 180.00 (markup 20 %).

| Key | Qty | Revenue | Cost | Margin |
|---|---|---|---|---|
| SRV-ALAG | 12 | 216.00 | 0.00 | 216.00 |
| SRV-LAV | 56.1 | 224.40 | 56.10 | 168.30 |
| LAB-HAND | 9.0 | 342.00 | 206.00 | 136.00 |
| LAB-PAINT | 5.5 | 247.50 | 143.00 | 104.50 |
| MAT-AVEG | 5 | 475.00 | 290.00 | 185.00 |
| SRV-VARO | 12 | 216.00 | 0.00 | 216.00 |
| EXT (PROPULSION) | 1 | 180.00 | 150.00 | 30.00 |
| **Total** | | **1,900.90** | **845.10** | **1,055.80** (5554 bp) |

By work type: ANTIFOULING revenue 1,720.90, cost 695.10, margin 1,025.80; PROPULSION 180.00 / 150.00 / 30.00.
VAT 418.20 (per line: 47.52, 49.37, 75.24, 54.45, 104.50, 47.52, 39.60), gross **2,319.10**. Vs quote: +198.30 =
+22.80 (hand +0.6 h) − 4.50 (painter −0.1 h) + 180.00 (external). Billing: advance at acceptance 623.15, final
2,319.10 − 623.15 = **1,695.95**; Σ = 2,319.10.

## 10. Charging (normative, ORDERS, through M5)

### 10.1 Model

`work_order_billings`: `work_order_id`, `seq`, `kind` (`advance`, `progress`, `final`, `adjustment`),
`requested` json (percent or amount), `charge_id?`, `status` (`charged`, `skipped_zero`), `created_by_user_id`.
Each billing creates at most one primary charge: `charges.origin_type = 'work_order'`, `origin_id = billing id`;
`charges_origin_primary_uidx` gains `work_order` (00021 recreates it; Down restores the 00019 definition) —
**INV-94**. Charge lines carry `plan_key` = line key so `chargeplan.Remaining` works across billings. Permission
`charges.manage` (advance at acceptance runs with the accepting actor inside the accept tx, D17).

### 10.2 Advance and progress

- **Advance**: at acceptance when deposit > 0 and `yard_settings.advance_on_accept` (default true): target =
  budget lines, `chargeplan.Split(Remaining(target, charged), deposit)`, due today + billing default days.
- **Progress** `POST M/yard/work-orders/{id}/billings {kind: progress, percent_bp | amount_cents}`:
  `progress` mode default amount = `completion_bp × budget gross − charged gross` (≥ 0, else `409
  nothing_to_bill`); any mode accepts an explicit amount ≤ remaining positive gross. Split over the target of
  the mode.

### 10.3 Final

Allowed in `completed`/`delivered`, under the order lock: target = account target (§9.2); `rem =
Remaining(target, charged lines of the order)`; `parts = All(rem)`; Σ > 0 ⇒ final charge (negative-remainder lines
stay as negative lines in it); Σ < 0 ⇒ negative adjustment charge (`corrects_charge_id` = latest live charge of the
order, `correction_kind = delta`; needs `refunds.issue`, else `403`); Σ = 0 ⇒ billing `skipped_zero`. Status →
`invoiced` in the same tx. After final: new approved entries, movements or external costs require `yard.manage` and
are flagged `late`.

### 10.4 Adjustments (conguaglio)

`POST …/billings {kind: adjustment}` on an `invoiced` order recomputes the account and charges `All(Remaining)`:
positive ⇒ new charge `corrects_charge_id` = latest; negative ⇒ negative adjustment (as §10.3). Exported charges
are never edited (INV-51). Cancelling a not-exported charge of an order uses the M5 cancel; the billing row stays
and the next billing recomputes from live charges.

## 11. Lifting equipment slots (normative, PLANNER)

- `equipment` gains `bookable` (default true for `travel_lift`, `crane`, `forklift`, `trailer`) and
  `default_minutes` (30). `equipment_operators` (equipment_id, employee_id) unique; operator must also have a
  qualification with `lift_operator = true`.
- `equipment_bookings`: `equipment_id`, `date`, `from_slot`, `to_slot` (exclusive, 1..96, ≥ from + 1),
  `purpose` (`haul_out`, `launch`, `move`, `other`), `vessel_id?` (required for haul_out/launch/move),
  `work_order_id?`, `contract_id?` (storage), `storage_movement_id?`, `allocation_id?` (move request),
  `operator_employee_id?`, `status` (`requested`, `booked`, `done`, `cancelled`), `source` (`office`,
  `work_order`, `storage`, `move`), `note`, `version`. `requested` has no slots.
- `equipment_slots` (marina_id, equipment_id, date, slot 0..95, booking_id) **unique (equipment_id, date, slot)**;
  `operator_slots` (marina_id, employee_id, date, slot, booking_id) **unique (employee_id, date, slot)**; both FK
  booking ON DELETE CASCADE. **INV-17**.
- `bookEquipment` / `rescheduleBooking` (`yard.manage` or `storage.manage`, If-Match): in one tx: lock booking;
  validate window (`yard_settings.lift_from_slot`/`lift_to_slot`, default 28..76 = 07:00–19:00), equipment
  bookable and not out of service that day (maintenance task with `out_of_service` covering the date ⇒ `409
  equipment_out_of_service`), capacity (`capacity_kg` < vessel displacement ⇒ `422 equipment_capacity_exceeded`,
  not overridable), operator enabled (`422 operator_not_enabled`); delete old slot rows; insert slots (and
  operator slots); a unique violation ⇒ rollback, `409 equipment_slot_taken {equipment_id, date, slots,
  booking_ids}` (or `operator_busy`). Never a pre-check as guarantee.
- `cancelBooking` deletes slot rows (cascade not relied on: explicit delete + status). `completeBooking` → `done`
  (online; phone shows it).
- Requests: buttons on the order, the storage contract (planned haul-out/launch) and the storage movement create
  `requested` bookings; with `yard_settings.lift_request_on_move` (default false) an in-process outbox sink on
  `occupancy.moved` creates a `requested` booking when the move goes water ↔ yard/shed (unique partial `(marina_id,
  allocation_id) WHERE source = 'move'`, idempotent). Pure `lifting/slots`: time ↔ slot, window, DST (slots are
  wall-clock labels; the window never includes 02:00–03:00).
- Events `equipment.booking_changed` `{booking_id, equipment_id, date, status}`; SSE `yard.changed`. Day calendar
  `GET M/equipment-bookings?date=` readable with `yard.read`, `storage.read` or `yard.timesheets`.

## 12. Reports and tiles (ORDERS, M7 machinery, INV-61..64)

| Report `yard` view | Permission | Records | Totals |
|---|---|---|---|
| `hours` | `reports.yard` or `reports.all` | approved entry per employee × order × day: employee code/name, order number, work type, qualification, hours, cost | hours by employee/order, cost |
| `load` | same | employee × day: capacity, assigned, actual, load bp, overload; group by department, qualification, bay, week, month | Σ capacity, Σ assigned, aggregate load bp |
| `margins` | same | per order with activity or invoiced in period: status, mode, revenue, cost by source, margin, margin bp, quote net, delta | Σ revenue, cost, margin, aggregate margin bp |
| `margins_by_work_type` | same | order × work type rows | per work type Σ |
| `lifting` | same | booking rows: equipment, date, slots, purpose, status | booked slots / window slots per equipment |

Tiles (available from M10, `reason` cleared): `yard_open_orders` (count of orders in `planned`, `in_progress`,
`on_hold`, `completed` at period end; drill `margins` with `open_only`), `yard_hours` (Σ approved hours in
period; drill `hours`), `yard_margin` (Σ margin of orders that reached `invoiced` in the period; drill `margins`
`invoiced_in_period`; money ⇒ `reports.yard`/`reports.all`). Tile value = Σ drill records by construction; previous
year comparison as M7. CSV/Excel via `report/export`. R8/R10 already show `work_order` charges.

## 13. API (CONTRACT writes all; `M` = `/api/admin/v1/marinas/{marinaId}`)

| Area | Operations (operationId) | Permission |
|---|---|---|
| Catalogue | `listWorkTypes` `createWorkType` `updateWorkType`; `listYardQualifications` `create…` `update…`; `getYardItemSettings` `putYardItemSettings` (`M/yard/items/{itemId}`); `listFormulaVariables` `create…` `update…` `delete…`; `listFormulaTables` `create…` `update…`; `listWorkPackages` `createWorkPackage` `getWorkPackage` (`?version=`) `saveWorkPackageVersion` (`POST …/versions`) `setWorkPackageActive`; `validateFormula` (`POST M/yard/formulas/validate` → issues + test value); `getYardSettings` `putYardSettings` | read `yard.read`; write `yard.manage` (settings: `yard.manage`) |
| Quotes | `listYardQuotes` `previewYardQuote` `createYardQuote` `getYardQuote` `updateYardQuote` `recomputeYardQuote` `sendYardQuote` `acceptYardQuote` `declineYardQuote` `getYardQuotePdf` | `yard.read` / `yard.manage` |
| Public | `GET /api/public/v1/{slug}/links/{token}` returns `PublicYardQuote` for `subject_type = yard_quote`; `POST …/links/{token}/yard-quote/accept` | link (INV-75) |
| Orders | `listWorkOrders` `getYardBoard` `createWorkOrder` `getWorkOrder` `updateWorkOrder` `transitionWorkOrder` (`{action, reason?}`) `createVariationQuote` | `yard.read` / `yard.manage` |
| Tasks | `listWorkTasks` `createWorkTask` `updateWorkTask` `deleteWorkTask` (no entries) `setTaskDependencies` `updateTaskProgress` (offline) `uploadTaskPhoto` (offline) `listTaskFiles` | `yard.manage`; progress/photo also `yard.timesheets` on assigned tasks |
| Time | `listTimesheets` `startTimer` `stopTimer` `createTimeEntry` `updateTimeEntry` `approveTimeEntries` (batch) `rejectTimeEntry` `voidTimeEntry` `getMyYardWork` (`M/yard/my-work`: assignments ±7 days, running timer) | §7 |
| Materials, costs | `listStockMovements` `createStockMovement`; `listExternalCosts` `createExternalCost` `updateExternalCost` `deleteExternalCost` | `yard.read` / `yard.manage` |
| Account, billing | `getFinalAccount` (`?format=csv|xlsx`) `listWorkOrderBillings` `createWorkOrderBilling` (`previewWorkOrderBilling` `?dry_run=true`) | `yard.read` (money) / `charges.manage` |
| Planner | `listEmployees` `createEmployee` `getEmployee` `updateEmployee` `putEmployeeCalendar` `listAbsences` `createAbsence` `deleteAbsence`; `getPlanner` (`from`, `to`, `department?`) `putAssignments` (batch) `deleteAssignment` `getYardLoad` (`group_by`, `from`, `to`) `getAtRiskOrders` `proposeYardPlan` | `yard.read` / `yard.manage` |
| Lifting | `listEquipmentBookings` (`date`, `equipment_id?`, `status?`) `createEquipmentBooking` `updateEquipmentBooking` `cancelEquipmentBooking` `completeEquipmentBooking` `putEquipmentOperators` | §11 |
| Reports | existing `getReport` with key `yard` + `view`; dashboard tiles | §12 |

Conventions: If-Match on versioned resources, `Idempotency-Key` on creating POSTs, cursor pagination, no required
query parameters on `{marinaId}` routes except `date` on the lifting list (default today). Offline flags
`x-porto-offline: true` on the five operations of §7 (+ `stopTimer`). Problem slugs: `formula_invalid`,
`no_price`, `quote_expired`, `invalid_transition`, `customer_email_missing`, `work_order_closed`,
`work_order_has_charges`, `qualification_mismatch`, `capacity_exceeded`, `dependency_violation`,
`duration_too_long`, `offline_time_out_of_range`, `timer_already_running`, `return_exceeds_issued`,
`nothing_to_bill`, `equipment_slot_taken`, `operator_busy`, `equipment_out_of_service`,
`equipment_capacity_exceeded`, `operator_not_enabled`, `outside_lift_window`, `version_conflict`.
Events: `yard_quote.changed`, `yard_quote.sent`, `yard_quote.accepted` (public), `yard_quote.expired`,
`work_order.status_changed` (public), `work_task.changed`, `timesheet.changed`, `work_order.billed`,
`equipment.booking_changed` (public); SSE topic `yard.changed`. Meta capabilities `yard`, `lifting`.

## 14. UI

### 14.1 QUOTE (`web/admin/src/features/{yard-catalog,yard-quotes}/**`, booking-side link page)

- **Catalogo cantiere** `/m/$id/yard/catalog`: tabs Tipi di lavorazione, Qualifiche, Articoli (yard settings,
  cost, qualification, default formulas, link to the price list rule), Variabili (built-in vessel list read-only,
  derived with expression, job inputs), Tabelle a fasce, Gruppi di lavorazioni (list; editor with lines table,
  formula field = shadcn `Textarea` monospace via Tailwind `font-mono`, inline issues with position, live test
  panel on the test vessel or a chosen vessel, version history read-only with diff).
- **Preventivi** `/m/$id/yard/quotes` list (status, customer, vessel, total, validity) and wizard
  `/m/$id/yard/quotes/new` (5 steps, shadcn `Stepper`-like `Tabs`), detail with lines (manual badge), totals, VAT
  summary, estimated hours, actions Invia / Accetta / Rifiuta / PDF / Ricalcola. Variation wizard reused from the
  order page through `features/yard-quotes/public.ts`.
- Public link page for `yard_quote` in `web/booking` (existing magic-link feature: QUOTE adds the subject view).

### 14.2 ORDERS (`web/admin/src/features/yard-orders/**`, reports and dashboard yard additions)

- **Barche in lavorazione** `/m/$id/yard/orders`: board (columns by status, cards §5.3, move via action menu with
  confirmation; keyboard accessible) + list view with filters.
- **Commessa** `/m/$id/yard/orders/$orderId`: tabs Riepilogo (data, position, lifting bookings list from PLANNER's
  `features/lifting/public.ts`), Attività (tasks, dependencies, checklist, photos, progress), Ore (entries, approve
  in batch), Materiali (issue/return), Costi esterni, Consuntivo (account table, vs quote, by work type, margin),
  Addebiti (billings, preview then create, links to charges), Varianti.
- **Approvazione ore** `/m/$id/yard/timesheets`; yard report pages and the three tiles.

### 14.3 PLANNER (`web/admin/src/features/{yard-planner,lifting}/**`, `features/phone/yard/**`)

- **Dipendenti** `/m/$id/yard/employees` (+ calendar, absences, qualifications).
- **Planner** `/m/$id/yard/planner`: grid employees × days (day/week/month), blocks of hours per task coloured by
  order, tray "Da pianificare", pointer drag with ghost and keyboard alternative (arrows + Enter), qualification
  mismatch and over-capacity feedback with confirm, auto-plan proposal dialog (diff, confirm).
- **Carico** `/m/$id/yard/load`: table heatmap with Tailwind token classes (`bg-chart-*`), group by employee,
  department, qualification, bay; at-risk orders list.
- **Mezzi di sollevamento** `/m/$id/yard/lifting`: day calendar (rows = equipment, columns = 15-min slots in the
  window), requests tray, booking dialog (purpose, vessel, operator, duration), conflict message from 409.
- **Phone** `/m/$id/yard-phone` (`PhoneShell`, landing for `yard`/`yard_mgr`): Oggi (my assignments from the offline
  cache), task detail (timer start/stop, manual hours keypad, checklist, progress slider, camera photo, offline
  queue status), Gru (day calendar read-only; managers mark done online), Approva (managers). Landing rows by
  CONTRACT in `landing.ts`.

## 15. Data (CONTRACT; both dialects, same names; `store.BackupTables()` registration)

| Migration | Tables / changes | Key constraints |
|---|---|---|
| `00020_yard_catalog_quotes` | `yard_settings` (marina PK, test_vessel json, quote_validity_days, deposit_bp, advance_on_accept, external_markup_bp, external_item_id, fixed_includes_external, workers_issue_materials, lift_from_slot, lift_to_slot, lift_request_on_move, version), `work_types`, `yard_qualifications`, `yard_item_settings`, `formula_variables`, `formula_tables`, `work_packages`, `work_package_versions`, `yard_quotes`, `yard_quote_lines`; CHECK edits (SQLite `writable_schema` procedure of 00016, PostgreSQL drop/add): `number_sequences.series` + `yard_quote`, `work_order`; `generated_documents.subject_type` + `yard_quote`, `work_order` | unique (marina_id, code) on catalogue tables; `yard_qualifications.labour_item_id` unique; `yard_item_settings` unique partial (qualification_id); `formula_variables` unique (marina_id, name); `work_package_versions` unique (package_id, version) + insert-only triggers; `yard_quotes` unique (marina_id, year, number), CHECK `(kind = 'variation') = (work_order_id IS NOT NULL)` (FK added in 00021 is not possible on SQLite ⇒ validated in Go, D27); CHECK deposit one-of; lines unique (quote_id, seq), CHECK qty ≥ 0, unit ≥ 0 |
| `00021_work_orders` | `work_orders`, `work_order_lines`, `work_order_variations`, `work_tasks`, `work_task_deps`, `work_task_files`, `employees`, `employee_qualifications`, `employee_calendars`, `employee_absences`, `task_assignments`, `timesheets`, `stock_movements`, `work_order_external_costs`, `work_order_billings`; recreate `charges_origin_primary_uidx` with `work_order` | `work_orders` unique (marina_id, year, number), unique partial (quote_id); `work_task_deps` unique, CHECK no self; `employees` unique partial (marina_id, user_id); `task_assignments` unique (task_id, employee_id, work_date), CHECK hours > 0; `timesheets` unique partial (employee_id) WHERE status='running', unique partial (marina_id, client_ref), CHECK `status IN ('approved') ⇒ cost_cents IS NOT NULL`, CHECK hours ≥ 0; `work_tasks` unique partial (marina_id, client_ref); `stock_movements` CHECK kind IN ('issue','return'), qty > 0; `work_order_billings` unique (work_order_id, seq), unique partial (charge_id); composite FKs `(x_id, marina_id)` everywhere |
| `00022_equipment_slots` | ALTER `equipment` add `bookable`, `default_minutes`; `equipment_operators`, `equipment_bookings`, `equipment_slots`, `operator_slots` | **unique (equipment_id, date, slot)**, **unique (employee_id, date, slot)**, CHECK slot 0..95, CHECK `to_slot > from_slot`, `status = 'requested' ⇒` no slot rows (Go + Q31), unique partial (marina_id, allocation_id) WHERE source='move' |

Invariants (CONTRACT writes them in Italian in `docs/invariants.md`, status `planned`; builders flip):
- **INV-17 — Nessuna doppia prenotazione dei mezzi di sollevamento né dell'operatore.** Unique keys of
  `equipment_slots`/`operator_slots`; `TestConcurrency_EquipmentSlots`, `TestMigrations_M10Constraints`, Q31
  (slots of a booking = its range; no slots for requested/cancelled). PLANNER.
- **INV-28 — Un preventivo di cantiere è riproducibile dal suo snapshot.** `TestRapid_QuoteRecompute`,
  `TestYardQuote_OldQuoteKeepsFormula`. QUOTE.
- **INV-29 — Le formule sono deterministiche e non vanno in panico.** `FuzzParse`, `FuzzEval`,
  `TestFormula_Purity`. QUOTE.
- **INV-93 — Il consuntivo riconcilia ore, materiali e costi esterni.** §9.3; `TestRapid_AccountReconciles`, Q30,
  `TestAcceptance_M10_FinalAccount`. ORDERS.
- **INV-94 — Gli addebiti di una commessa sommano il suo obiettivo.** One live primary charge per billing; after
  final Σ = target per VAT bucket; `TestRapid_BillingSequenceSums`, `TestConcurrency_FinalBillingOnce`. ORDERS.
- **INV-95 — Un solo cronometro attivo per dipendente.** Partial unique; `TestConcurrency_TimerStart`. ORDERS.
- **INV-96 — Un preventivo accettato crea una sola commessa.** `TestConcurrency_YardQuoteAccept`. QUOTE.
- **INV-71 / INV-83** extended to `yard.timesheet` and `yard.activity_done`. ORDERS (server), PLANNER (client).

## 16. Ownership, order and acceptance

### 16.1 CONTRACT (runs first, alone; then frozen)

`api/openapi.yaml` (tags `yard-catalog`, `yard-quotes`, `work-orders`, `yard-planner`, `lifting`; every operation
of §13; schemas with `_cents`, `_milli`, `_bp`; offline flags; problem slugs) + `make gen`; migrations 00020–00022
both dialects; `internal/store/{yard_models,workorder_models,yardplan_models,lifting_models}.go`, backup
registration; `auth.Principal.AuthorizeAny` (first held permission; same 404 semantics) — no new permission
constants; `internal/domain/formula/{doc,types,api}.go` (signatures §3.4, bodies `ErrNotImplemented`);
`pricing/unit.go` **signature only**; `yardquote/{types,service,errors,creator}.go` (incl. `OrderCreator`
interface), `yardquote/price/{doc,types}.go`; `workorder/{types,service,errors,states}.go` (states table filled:
pure data), `workorder/account/{doc,types}.go`; `yardplan/{types,service,errors}.go`, `yardplan/{load,auto}/types.go`;
`lifting/{types,service,errors}.go`, `lifting/slots/types.go`; exported methods return `ErrNotImplemented`; event
constants + webhook catalogue + SSE topic `yard.changed`; problem slugs + `errmap.go`; `internal/http/admin/admin.go`
fields + 501 stubs (`yard_catalog.go`, `yard_quotes.go`, `work_orders.go`, `timesheets.go`, `stock_movements.go`,
`employees.go`, `planner.go`, `equipment_bookings.go`), public stub `yard_quote_links.go`; `internal/app/app.go`
wiring (OrderCreator = workorder service), jobs `yard.quotes_expire` (1 h), `yard.timers_guard` (15 min), outbox
sink registration for lifting move requests, seed stubs `seed_yard_catalog.go`, `seed_yard_orders.go`,
`seed_yard_planner.go`; meta capabilities; `.golangci.yml` pure rules (§0.4); offline middleware set consistent
with openapi; `internal/domain/docs/types.go` `yard_quote`/`yard_work_order` `available_from` = M10 and view
type stubs; report seams: tiles available flags remain false until ORDERS (no logic). Web: `queryKeys.ts`, hook
stubs `web/shared/src/api/hooks/{yardquote,workorders,yardplan,lifting}.ts`, `OfflineOpKind` /
`OFFLINE_OPERATION_KINDS` entries (`yard.timesheet`, `yard.activity_done`), i18n skeletons `yardCatalog.json`,
`yardQuotes.json`, `yardOrders.json`, `yardPlanner.json`, `lifting.json` (it, en) + `resources.ts`, router
factories `yardCatalogRoutes`, `yardQuotesRoutes`, `yardOrdersRoutes`, `yardPlannerRoutes`, `liftingRoutes` +
`nav.ts` group "Cantiere", phone route stub `yard-phone` + `landing.ts` rows; shadcn components via CLI if
missing (`progress`, `toggle-group`, `slider`, `textarea`). Docs: ADR `docs/adr/0015-cantiere-m10.md` (Italian,
"proposta": §3, §4.3, §9.2–9.3, §10, §11, §1.2 stock deferral, §17), invariants §15, `docs/architecture.md`
§2/§10 rows. Ends with `make gen && make check` green.

### 16.2 Builders (parallel, exclusive files)

| Builder | Owns |
|---|---|
| **QUOTE** | `internal/domain/formula/**` (bodies, tests, fuzz corpora `testdata/fuzz/**`); `internal/domain/pricing/unit.go` + `unit_test.go`; `internal/domain/pricelist/unit_price.go` (+ test); `internal/domain/yardquote/**` (incl. `price/`); `internal/domain/docs/yard_quote_view.go` + builtin template `yard_quote` (it, en); notify template `yard_quote` (it, en); `internal/store/{yard_catalog.go,yard_quotes.go}`; `internal/http/admin/{yard_catalog.go,yard_quotes.go}` + tests; `internal/http/public/yard_quote_links.go` and the `yard_quote` case in `internal/http/public/links.go` (only that switch); `internal/app/seed_yard_catalog.go` (§4.4 catalogue, ANTIVEG package, 3 quotes); `web/admin/src/features/{yard-catalog,yard-quotes}/**`; `web/booking` yard-quote link view; `web/shared/src/api/hooks/yardquote.ts`; i18n values `yardCatalog`, `yardQuotes`; `internal/acceptance/m10_quote_test.go`; e2e `yard-catalog.spec.ts`, `yard-quote.spec.ts`, `yard-quote-link.spec.ts`; invariant statuses INV-28, 29, 96 |
| **ORDERS** | `internal/domain/workorder/**` (incl. `account/`; implements `yardquote.OrderCreator`); `internal/domain/docs/yard_work_order_view.go` + builtin template `yard_work_order`; `internal/store/{work_orders.go,work_tasks.go,timesheets.go,stock_movements.go,external_costs.go,wo_billings.go,yard_invariants.go,report_yard.go}`; `internal/http/admin/{work_orders.go,timesheets.go,stock_movements.go}` + tests; `internal/domain/report/**` yard views and tiles (`calc/yard.go`, tile wiring); `internal/app/seed_yard_orders.go`; `web/admin/src/features/yard-orders/**`, yard additions in `features/reports/**` and `features/dashboard/**`; `web/shared/src/api/hooks/workorders.ts`; i18n values `yardOrders` (+ yard keys in `reports.json`, `dashboard.json`); `internal/acceptance/m10_final_account_test.go`, `m10_reports_test.go`, `m10_isolation_test.go`; e2e `yard-orders.spec.ts`, `yard-final-account.spec.ts`; invariant statuses INV-93, 94, 95, INV-71/83 server side |
| **PLANNER** | `internal/domain/yardplan/**` (incl. `load/`, `auto/`); `internal/domain/lifting/**` (incl. `slots/`, move-request sink); `internal/store/{employees.go,assignments.go,equipment_bookings.go,lifting_invariants.go}`; `internal/http/admin/{employees.go,planner.go,equipment_bookings.go}` + tests; `internal/app/seed_yard_planner.go` (4 employees, calendars, assignments, travel lift + crane with operators, bookings); `web/admin/src/features/{yard-planner,lifting}/**` (+ `lifting/public.ts` for the order page); `web/admin/src/features/phone/yard/**`; `web/shared/src/api/hooks/{yardplan,lifting}.ts`; `web/shared/src/offline/allowlist.ts` (activities slot: `getMyYardWork`, day bookings) + tests; i18n values `yardPlanner`, `lifting`, phone keys; `internal/acceptance/m10_lifting_test.go`, `m10_offline_test.go`; e2e `yard-planner.spec.ts`, `yard-lifting.spec.ts`, `phone-yard.spec.ts`; invariant status INV-17 |

### 16.3 Parallel-phase rules

- OpenAPI, migrations, models, states table, event constants, interfaces and signatures are frozen. Changes go in
  the builder's report to the orchestrator, never as edits.
- QUOTE tests acceptance with a fake `OrderCreator`; the real chain is exercised by ORDERS' acceptance test after
  merge. ORDERS builds the order-from-quote input type defined by CONTRACT and never edits `yardquote`.
- Web builders work against the generated client with MSW fixtures until the backend lands; PLANNER's phone screens
  call ORDERS' timesheet/task operations exactly as in the contract.
- Nobody edits `charge`, `chargeplan`, `occupancy`, `contract`, `maintenance`, `storage`; missing hooks are reported.
  PLANNER reads maintenance out-of-service and storage rows via its own store queries (read only).
- Each builder ends with its packages' tests (both dialects for integration), `golangci-lint` clean on its files,
  `npm --prefix web run lint && npm --prefix web run typecheck` and the `invariants-guardian` subagent.

### 16.4 Tests

**Pure**
- `formula`: table tests per operator, precedence, half-even at each `*`/`/` (e.g. `1 / 3 * 3 = 0.999`), functions,
  band edges (equal to `up_to`, open row, no row), enums, lazy `if` (division by zero in the untaken branch is not
  an error), limits, every error code; `TestFormula_HullArea` (§4.4 values); `FuzzParse` and `FuzzEval` (no panic,
  same result twice, `Check`-clean programs never return `type_mismatch` at eval); `TestFormula_Purity`.
- `pricing/unit`: selection by priority/specificity/recency, customer category, validity, unit vs amount, no rule;
  existing corpus unchanged (`EngineVersion` same).
- `yardquote/price`: `TestQuoteLines_Antifouling12m` (every number of §4.4 and variants), overrides marked,
  deposit, VAT gross-priced items; `TestRapid_QuoteRecompute` (random packages/vessels: recompute(snapshot) =
  lines; totals = Σ lines).
- `workorder/states`, `workorder/account`: `TestAccount_Worked` (§9.4), `TestRapid_AccountReconciles` (equations
  1–5, 7 on random entries/movements/costs/modes), `TestRapid_BillingSequenceSums` (random advance/progress
  amounts + final: Σ gross = target gross, per-bucket VAT equal, no part above remainder), completion weighting.
- `yardplan/load`: capacity with absences, overload, aggregates as ratio of sums, at-risk; `yardplan/auto`:
  determinism, never over capacity, qualifications, FS order (rapid). `lifting/slots`: time ↔ slot, window.

**Integration** (`//go:build integration`, SQLite + PostgreSQL via `testdb`): `TestMigrations_M10Constraints`
(every unique/CHECK, insert-only package versions, origin index with `work_order`, CHECK edits, Up/Down/Up, parity),
catalogue validation 422 paths, `TestYardQuote_OldQuoteKeepsFormula` (edit package and derived variable after
sending: quote unchanged, recompute equal), send (PDF + link + message), public accept, expiry job, accept creates
order + tasks + advance charge in one tx (fault: failing charge rolls back everything), variations, order
transitions, cancel with charges 409, assignments rules, timers, approval freeze, return limit, final billing
(positive, zero, negative ⇒ adjustment), conguaglio after export (new charge, exported unchanged), lifting rules
(window, out of service, capacity, operator), move-request sink idempotent, reports on seed, backup round trip,
cross-marina sweep for every new route.

**Concurrency** (both dialects, `-race`, ≥ 100 goroutines): `TestConcurrency_EquipmentSlots` (100 goroutines
book random overlapping windows on one travel lift and 3 operators, plus 20 reschedules/cancels: winners' slot sets
disjoint per equipment and per operator, slot rows = Σ winner ranges, losers 409 with nothing written, Q31 empty),
`TestConcurrency_YardQuoteAccept` (100 accepts of one quote: one order), `TestConcurrency_FinalBillingOnce` (100
final/progress billings: one final, Σ ≤ target, INV-94), `TestConcurrency_TimerStart` (100 starts for one
employee: one running).

**Acceptance** (`internal/acceptance/m10_*_test.go`, API only, `clock.Fake`, both dialects):
`TestAcceptance_M10_AntifoulingQuote` (criterion 1: catalogue and price list via API, vessel 12 m, quote created
with only package id + vessel + customer — no price or quantity field in any request — lines, totals 1,702.60 /
374.57 / 2,077.17, deposit 623.15, every `manual = false`; package v2 later ⇒ quote unchanged);
`TestAcceptance_M10_FinalAccount` (criterion 2: accept → order, assignments, phone time entries incl. an offline
replay twice, approvals, materials issue/return, external cost, final account = §9.4 table exactly and equations
§9.3 checked from the API numbers, advance + final charges Σ = 2,319.10, export, late entry ⇒ conguaglio adjustment);
`TestAcceptance_M10_Lifting` (book, conflict 409, reschedule, out of service, phone day view);
`TestAcceptance_M10_Offline` (queued timer/progress/photo replayed: one effect each, no allocation/booking change);
`TestAcceptance_M10_Reports` (tiles = Σ drill on seed).

**Web**: Vitest — formula editor issues mapping, wizard reducer (steps, overrides badge, totals display), board
columns, account table, billing preview, planner reducer (drag, keyboard, capacity/qualification feedback), load
heatmap classes, lifting slot grid, phone timer state and offline kinds (`allowlist.test.ts`: no reserved kinds
left for M10). Playwright — `yard-catalog`, `yard-quote` (wizard end to end on the 12 m vessel, numbers shown),
`yard-quote-link`, `yard-orders`, `yard-final-account`, `yard-planner`, `yard-lifting`, `phone-yard` (timer
offline then reconnect, no duplicates); `a11y.spec.ts` additions per owner.

### 16.5 Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
go test -count=1 ./internal/domain/formula/... ./internal/domain/pricing/... ./internal/domain/yardquote/... ./internal/domain/workorder/... ./internal/domain/yardplan/... ./internal/domain/lifting/... ./internal/domain/report/...
go test -count=1 ./internal/domain/yardquote/price/... ./internal/domain/workorder/account/... ./internal/domain/yardplan/... -run 'TestRapid_' -rapid.checks=2000
go test -run '^$' -fuzz '^FuzzParse$' -fuzztime 60s ./internal/domain/formula/
go test -run '^$' -fuzz '^FuzzEval$' -fuzztime 60s ./internal/domain/formula/
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/store/... ./internal/domain/yardquote/... ./internal/domain/workorder/... ./internal/domain/yardplan/... ./internal/domain/lifting/... ./internal/domain/charge/... ./internal/domain/report/... ./internal/http/... ./internal/backup/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -timeout 30m ./internal/acceptance/... ./internal/domain/... -run '^TestConcurrency_(EquipmentSlots|YardQuoteAccept|FinalBillingOnce|TimerStart)$'
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M10|TestAcceptance_M9|TestAcceptance_M7|TestAcceptance_M5|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII|TestSSE_PayloadHasNoPII'
make test-db && make test-race
npm --prefix web run test
make e2e            # yard-catalog, yard-quote, yard-quote-link, yard-orders, yard-final-account, yard-planner, yard-lifting, phone-yard, a11y, offline-queue
make licenses && scripts/agents/check-naming.sh
```

Manual check (final report): `make seed && make dev`; build the antifouling quote for the 12 m demo boat in the
wizard without typing prices, send it, accept from the link, plan the tasks by drag and drop, record hours on the
phone in airplane mode and reconnect, approve, issue materials, add an external cost, open the final account,
bill final, book the travel lift twice on the same slot and see the 409.

## 17. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Unit prices of yard items come only from M2 price lists through pure `pricing.UnitPrice` (base rule selection reused; contract-type, berth, group and channel scopes ignored; customer category honoured). `EngineVersion` unchanged. |
| D2 | Labour sale rate = the `labour` item linked to each qualification (one item per qualification); internal cost = employee `hourly_cost_cents`, frozen on the time entry at approval; quote estimates use the qualification default cost. |
| D3 | Yard quotes are a separate aggregate (`yard_quotes`, domain `yardquote`) with the 13.2 lifecycle; INV-30 keeps covering mooring quotes/contracts. Audit + outbox in the same tx still apply. |
| D4 | Formula numbers are `qty.Milli` with half-even rounding at every `*` and `/`, left to right; price formulas return euro (3 decimals) converted to cents half-even. Enums compared only with `==`/`!=`. `if` is lazy. |
| D5 | Vessel variables in metres/tonnes from cm/kg exactly; missing vessel data becomes a step-3 input stored as a quote override, never written to the vessel. |
| D6 | Formula validation uses a marina test vessel plus optional per-line test cases; changing a derived variable or table re-validates dependent packages and blocks on errors. |
| D7 | Old quotes keep their formula through insert-only package versions plus a full snapshot per quote; recompute uses the snapshot only (INV-28). "Ricalcola" is explicit on drafts. |
| D8 | Work types are a separate taxonomy; each package has one work type, each line may override; margins per work type use the line's work type (labour: the task's). |
| D9 | No automatic discounts on yard lines in M10; manual per-line discount and overrides are marked and audited. |
| D10 | Billing modes: `actual` (final account), `fixed` (budget = quote + variations), `progress` (SAL on the budget by completion). Deposit = advance billing at acceptance, deducted by construction (final = remaining). |
| D11 | Variations are `yard_quotes.kind = variation` accepted by panel or magic link; approval adds budget lines and tasks. Advanced signature of yard documents deferred. |
| D12 | Magic links reuse `access_links.purpose = quote` with `subject_type = yard_quote`; `generated_documents.subject_type` and `number_sequences.series` CHECKs are widened (writable_schema on SQLite, as 00016). Numbers `PC<year>-NNNNNN` (quotes), `C<year>-NNNNNN` (orders). |
| D13 | Stock management (levels, warehouses, loads, CSV, adjustments, non-negative rule) is out of M10 by orchestrator scope; `stock_movements` holds only `issue`/`return` with frozen cost and price. Needs confirmation as a spec 18.6 deferral (ADR 0015). |
| D14 | Material price on the order = frozen quote rate when the item is in the budget, else catalogue price on the movement date; cost default from `yard_item_settings.cost_cents`. |
| D15 | External costs: supplier as text, billable with default markup `external_markup_bp` = 2000, item from settings. |
| D16 | In `fixed`/`progress` modes billable external costs are added on top of the budget unless `fixed_includes_external = true`. |
| D17 | Billing needs `charges.manage` (yard_mgr does not bill); the advance at acceptance is created inside the accept tx with the accepting actor; negative remainders need `refunds.issue`. |
| D18 | `invoiced` = final charge issued (no invoicing in Porto); delivery is tracked by `delivered_at` and may happen before or after the final billing. |
| D19 | Cancel of an order with live charges is refused; the office reverses charges first. Future lifting bookings of a cancelled order are cancelled in the same tx. |
| D20 | Assignment capacity and qualification are soft rules (409/422 with explicit override flags), not invariants. |
| D21 | Load aggregates are ratios of sums; yard capacity ignores marina closures; absences may be partial days. |
| D22 | "Bay" = the order's work position (allocation resource); bay load is hours per day without capacity. |
| D23 | Auto-plan is a deterministic greedy in 0.5 h steps by planned delivery, lowest-load qualified employee, ties by id; it proposes, never writes. |
| D24 | Timer hours rounded to 0.01 h; manual entries 0.25 h steps; one running timer per employee (DB); timers > 16 h flagged, never auto-closed. |
| D25 | Offline: `yard.timesheet` (start, stop, manual entry) and `yard.activity_done` (progress, checklist, done, photo); device times within 48 h before and 5 min after queueing; progress replays never decrease progress. |
| D26 | Approved entries are immutable; corrections before `invoiced` by void + new entry, after by late entries billed as conguaglio. |
| D27 | Polymorphic or cross-migration references (variation quote → order) are validated in Go under locks, like M9 `deadlines`. |
| D28 | Lifting slots: 15 min wall-clock index 0..95; window default 07:00–19:00 (excludes DST gaps); operator double booking is DB-enforced too (`operator_slots`). |
| D29 | Bookable kinds default travel_lift, crane, forklift, trailer; capacity vs displacement is a hard refusal; maintenance out-of-service blocks booking (read under the equipment lock). |
| D30 | Water ↔ land move requests come from an in-process outbox sink on `occupancy.moved`, off by default, idempotent per allocation; requests have no slots until booked. |
| D31 | Booking permission = `yard.manage` or `storage.manage` (new helper `AuthorizeAny`); day calendar readable by `yard.read`, `storage.read` or `yard.timesheets`. |
| D32 | Yard report money is visible with `reports.yard` (yard_mgr) as well as `reports.all`; tiles `yard_*` become available. |
| D33 | Quotes need an existing customer and vessel (yard_mgr has `customers.read`, not write): office creates new customers. |
| D34 | Events and SSE carry ids, enums, dates and order amounts only; employee and supplier names never leave the DB in events or logs. |
| D35 | Product decision after M10 (closes the M9 open gap): `yard_mgr` also gets `reports.operational` (storage, maintenance, utilities and the other operational reports without money, with their dashboard tiles; never `reports.all`) and `presence.write` (haul-out and launch from the dock screens). Spec 26.1 and the m0-plan table updated. |
