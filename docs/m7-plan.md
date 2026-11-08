# M7 — Report e cruscotto: work order

Status: Plan phase output, authoritative for the M7 builders. If it conflicts with `docs/spec/`, the spec wins:
stop and report to the orchestrator. No open decision of `docs/spec/33-decisioni-aperte.md` is touched. §14 lists
every detail decided here where the spec is silent; CONTRACT records the binding ones in ADR 0012.

Sources: spec 21 (all), 13.2 (inquiries), 10.6 (releases), 10.7 (search), 14 (rights, condominium), 16 (charges,
payments, cash), 26.1 (permission "Report ed esportazioni"), 27 (`/reports/*`, `/dashboard`), 29 (i18n), 32 (M7
acceptance); `docs/invariants.md` (INV-01, INV-60, INV-61, INV-70, INV-73); ADR 0001, 0002, 0003, 0008, 0009;
`docs/m2-plan.md` (pricing, `berth_categories` = `price_class`), `m3-plan.md` (occupancy rows, releases,
presences, fit), `m4-plan.md` (contracts, renewals, rights, `accrual`), `m5-plan.md` (charges, payments, cash,
ageing, `internal/xlsx`), `m6b-plan.md` §6.3 (direction phone screen, D20); `.claude/rules/*.md`; skills
`new-endpoint`, `new-migration`, `shadcn`.

## 0. Ground rules

1. **Arenile is a reference, not a source** (ADR 0001). Behaviour analogues: Arenile ADR 0016 §1–3 and
   `internal/domain/report` (report as queries, two date bases, year comparison by calendar day or weekday, CSV
   `;` with decimal comma). Read for behaviour only; never copy files, code, comments or tests; never modify
   `../arenile`. Porto's xlsx writer is its own (`internal/xlsx`, M5).
2. **Reports are read-only** (INV-61): every report runs inside `store.DB.ReadTx`; no audit row, no outbox event,
   no job. `internal/domain/report/**` never imports `domain/event`, `domain/audit`, `domain/auditlog`
   (depguard, CONTRACT).
3. **SQL only in `internal/store/report_*.go`**: raw rows filtered by `marina_id` from the authorised scope.
   SQL may filter, join for labels/dimensions, and `GROUP BY` identity keys to *count physical rows* (cells of
   one allocation on one night); it never computes money, ratios, shares or indicators (D2).
4. **Aggregations and indicators are pure** (`internal/domain/report/{calc,align,swap,export}`): no store, no
   clock, no `time.Now`, no randomness, no float. Money `money.Cents`, lengths `geo.Cm`, areas `geo.Cm2`,
   quantities `qty.Milli`, ratios as integer **basis points** (`calc.BP`, half-even via `money.DivRoundHalfEven`,
   `nil` when the denominator is 0). Table-tested; property tests with `pgregory.net/rapid`.
5. **Tile == Σ drill-down records by construction**: a tile value is never computed by a different code path than
   its records (§6). Proven again by the acceptance test on the seeded demo (§11.4).
6. Permissions checked in the report service (`p.Authorize`); cross-marina = 404; no PII in logs (report
   handlers log only report key, period length, record count, duration).
7. API first: CONTRACT edits `api/openapi.yaml`, runs `make gen`; never edit generated files. Migration `00015`
   both dialects (indexes only).
8. UI: shadcn only (`npx shadcn@latest add chart` in `web/shared`, recharts MIT), Tailwind tokens, no custom CSS,
   i18n it (source) + en. Responsive down to the Pixel 7 phone project.
9. `cmd/porto` stays `CGO_ENABLED=0`; no new Go dependency.
10. Do not `git commit`; no destructive git; no `rm -rf`; Docker only for your own throwaway containers
    (`scripts/with-postgres.sh`). Edit only files you own (§12). Test speed: while iterating run only touched
    packages / Vitest files; full gates only in §13.

## 1. Scope

### 1.1 In M7

Reports of 21.3 whose domain exists (each defined in §3):

| # | Report (21.3) | Key / path `reports/{key}` | Permission |
|---|---|---|---|
| R1 | Occupazione degli ormeggi (fisica / contrattuale, aree dinamiche) | `occupancy` | `reports.operational` |
| R2 | Efficienza occupazionale | `efficiency` | `reports.operational` |
| R3 | Spreco allocativo (+ ricavo potenziale perso, suggerimenti) | `allocation-waste` | `reports.operational` (lost revenue columns only with `reports.all`) |
| R4 | Domanda persa | `lost-demand` | `reports.operational` (values only with `reports.all`) |
| R5 | Clientela | `customers` | `reports.all` |
| R6 | Competenze (produzione) | `production` | `reports.all` |
| R7 | Ratei e risconti | `accruals` | `reports.all` |
| R8 | Addebiti dettagliati (basi competenza / emissione) | `charges` | `reports.all` |
| R9 | Incassi dettagliati (base incasso) | `payments` | `reports.all` |
| R10 | Classificazione dei ricavi | `revenue-classes` | `reports.all` |
| R11 | Comparazione delle statistiche | `comparison` | per indicator (§5) |
| R12 | Scadenzario e crediti (+ solleciti) | `receivables` | `reports.all` |
| R13 | Presenze | `presences` | `reports.operational` (release revenue only with `reports.all`) |
| R14 | Diritti d'uso e condominio | `rights` | `reports.all` |
| R15 | Cassa e corrispettivi | `cash` | `reports.all` |
| R16 | Imbarcazioni ospitate (tile 21.2) | `vessels` | `reports.operational` |
| R17 | Ricavo per metro lineare di posto (tile 21.2) | `berth-revenue` | `reports.all` |

Manager dashboard of 21.2 (`GET M/dashboard`, §4) replacing the placeholder of `DashboardPage` ("La panoramica
operativa arriverà nelle prossime versioni") and the dashboard tab of the direction phone screen (M6b D20).
CSV (Italian) and Excel export of every report (§7). Comparison with date or weekday alignment (§5).

### 1.2 Out of M7 (seams left, listed in the UI as "disponibile con M10/M11")

| Item | Milestone | Seam |
|---|---|---|
| Report Cantiere (ore, carico, margini) and tiles "commesse aperte, ore lavorate, margine" | M10 | tile keys `yard_open_orders`, `yard_hours`, `yard_margin` with `available=false, reason=milestone_m10`; report key `yard` reserved, `reports.yard` permission unused; `calc` has no yard code |
| Report Noleggio (utilizzo, ricavi, danni, cauzioni) and tiles "tasso di utilizzo flotta, ricavi" | M11 | tile keys `rental_utilisation`, `rental_revenue` `available=false, reason=milestone_m11`; report key `rental` reserved; `reports.rental` unused |
| Storage-specific reports (rimessaggio planner, sheds, yard maps) | M9 | occupancy covers every map but the dashboard occupancy tile is restricted to `maps.kind = 'water'`; `map_kind` filter |
| Anonymous online searches without availability in lost demand (13.2) | M8 | lost demand reads `inquiries` only; M8 writes its anonymous rows as `inquiries` with `channel='online'` and no customer, so R4 needs no change |
| Sensor-based occupancy | M12 | none |
| Charges of `work_order`/`rental` origins | M10/M11 | already generic in R8/R10 (origin enum exists) |
| Scheduled/emailed reports, custom report builder, PDF export of reports | after v1 | none |
| Historical "as of" receivables (reconstructed past balances) | after v1 | R12 is as of a date ≤ today using current residuals; the receivables tiles have no previous-year value (D13) |

## 2. Architecture

```
web: /m/$id (dashboard) ── tile.drill ──► /m/$id/reports/$key?<same filter> ──► records table ──► entity pages
            │                                       │  (CSV / Excel buttons: same query + format)
            ▼                                       ▼
GET M/dashboard                     GET M/reports/{key}[?format=csv|xlsx]   GET M/reports/comparison
   internal/http/admin/dashboard.go     internal/http/admin/reports.go         .../report_comparison.go
            └───────────────┬──────────────────────┘────────────────────────────────┘
                            ▼
   internal/domain/report  (Service: Authorize → ReadTx → store.Report* → calc.* → Result)
        ├─ calc/    pure: one Build<Report>(Input) → Result{Totals, Groups, Records, Daily} per report;
        │           tiles are Tile(<Result>) = fold of Records (§6)
        ├─ align/   pure: periods, previous period, date/weekday alignment, buckets
        ├─ swap/    pure: swap and move suggestions (fit.Check + occupancy/engine.Solve)
        └─ export/  pure: Table model; CSV (Italian) and XLSX (internal/xlsx) from the same Table
   internal/store/report_*.go  raw rows (one file per family), marina-scoped
```

Packages (new): `internal/domain/report` (service, not pure), `internal/domain/report/calc`, `.../align`,
`.../swap`, `.../export` (pure, added to depguard `pure-engines`). Reused pure code: `accrual.Allocate/Within/
DayQuota`, `fit.Check` (+ new exported `fit.FillBP`), `occupancy/engine.Solve`, `engine.FootprintWidth`,
`cash/summary.VATTotals`, `money.DivRoundHalfEven`, `internal/xlsx`. Non-pure reuse inside the service only:
`payment.BucketOf` (ageing buckets, computed in the service and passed to calc as a field),
`pricelist.Service` catalog loading for list prices (§3.3).

## 3. Report definitions (normative)

### 3.0 Common

- **Period** `from`, `to`: inclusive `YYYY-MM-DD` in the marina time zone, `to ≥ from`, at most **400 days**
  (`422 invalid_period`). Nights in a period = dates `d` with `from ≤ d ≤ to` (ADR 0002).
- **Today** = marina-local date from `clock.Clock` (service); pure code receives it as input.
- **Filters** (only where meaningful, else ignored and echoed as `ignored_filters`): `map_id`, `map_kind`
  (`water|yard|shed|parking|boxes|other`), `structure_id` (pontile), `group_id` (`berth_groups`), `category`
  (`price_class` code), `resource_type` (`berth|dynamic_area|land_resource`), `customer_id`, `item_id`,
  `origin`, `payment_status`, `invoicing_status`, `method`, `kind`, `channel`.
- **Response envelope** (typed per report in OpenAPI): `{report, period{from,to}, today, filters, ignored_filters,
  totals{…}, groups[…] (by group_by), records[…] (page), page{page, page_size, total}, notes[]}`. `totals` are over
  **all** records (not the page). `page_size` default 100, max 1000. Records have a stable order (listed per
  report) and a `ref {type, id, label}` to the entity page (contract, transit, charge, payment, refund,
  allocation, berth, area, customer, vessel, inquiry, release, presence, cash_session, fiscal_document, right,
  common_charge_run).
- **Ratio** (bp) = `DivRoundHalfEven(num × 10000, den)`; `null` if `den = 0`. Ratios are never averaged: groups
  and buckets recompute from Σnum / Σden.
- **Effective vessel size**: measured LOA/beam when `dimensions_source = 'measured'`, else declared (current
  values; vessels have no history, D7).
- **Length bands** (cm, LOA): `<800`, `800–999`, `1000–1199`, `1200–1499`, `1500–1999`, `2000–2499`, `≥2500`
  (keys `lt8`, `m8_10`, `m10_12`, `m12_15`, `m15_20`, `m20_25`, `ge25`) (D8).
- **Occupied kinds** = `contract`, `transit`, `owner`. **Out of service** = rows of kind `block`, `maintenance`.
  `hold` rows are neither occupied nor out of service (available, free) (D4).
- **Closure** of a night for a resource: a `closures` row with `from_date ≤ d ≤ to_date` and scope marina, or the
  resource's map, structure, group membership, or the resource itself.
- Resources: current `active = 1` only (no activation history, D5).

### 3.1 R1 Occupancy (`occupancy`)

Params: `variant=physical|contractual` (default physical), `family=berth|land|linear_area|surface_area`
(default berth; berths include buoys), `group_by=resource|structure|group|category|map|month|week|night`.

Per resource `r` and night `d` in the period:
- `available(r,d)` = 1 if `r` active, not closed on `d`, and no out-of-service row on `(r,d)`; else 0.
- `occupied(r,d)` (physical) = 1 if available and an occupancy row of an occupied kind exists on `(r,d)`.
  Released nights have no row (the engine removes them, INV-03), so unsold releases are free; resold nights carry
  the resale transit row.
- contractual: `occupied ∪` nights of `releases` with `status='active'` on `(r,d)` (whether resold or not).
- Berth/land unit = **berth-night** (count). **Dynamic areas**: linear unit = **cm-night**: available =
  `(cell_count − |blocked_cells| − cells out of service on d) × cell_cm` on non-closed nights; occupied =
  occupied cells × `cell_cm`. Surface unit = **cm²-night** with `cell_cm²` per cell (D6). Releases on areas:
  contractual variant adds the cells the releasing allocation held (from `cell_occupancies` are gone, so from the
  allocation footprint: `engine.Cells`) — computed in the service from the allocation placement (D6).
- Indicator `occupancy_bp = Σ occupied / Σ available`.
- **Records** (order resource code, kind, allocation number): numerator records = one per
  `(resource, allocation)` with `occupied_units` in the period (contractual adds `kind='release'` records with the
  released units); denominator records = one per resource with `available_units`, `closed_units`,
  `out_of_service_units`. JSON `records[]` carries both with `role: numerator|denominator`.

### 3.2 R2 Efficiency (`efficiency`)

Same filters and grouping as R1 (physical occupied nights only).
- Berths and boat-holding land: `Σ_{occupied (r,d)} vessel_area / Σ_{occupied (r,d)} resource_area`,
  `vessel_area = LOA × beam` (cm², effective size), `resource_area = length_cm × width_cm`. Allocations without
  vessel (none expected for occupied kinds) are excluded from both sums and counted in `notes`.
- Linear areas: `Σ FootprintWidth(area, vessel) (incl. margins) × nights / Σ occupied_cells × cell_cm × nights`.
  Surface areas: `Σ LOA×beam × nights / Σ occupied_cells × cell_cm² × nights`.
- Records: one per `(resource, allocation)`: `nights`, `vessel_units`, `resource_units` (num/den contributions).

### 3.3 R3 Allocation waste (`allocation-waste`)

Params: `threshold_bp` (default `10000 − occupancy_settings.waste_threshold_bp` = 4000 = 40 %),
`suggestions=true|false` (default true). Scope: active allocations of occupied kinds on **berths** overlapping the
period (land and areas: their efficiency is in R2; D9).
- `waste_bp = 10000 − fit.FillBP(vessel, berth)` (LOA×beam over length×width, capped at 10000, half-even; the
  same function `fit.Check` uses). Listed when `waste_bp > threshold_bp`.
- `nights_in_period` = occupancy rows of the allocation in the period.
- **Applied price** = the allocation's contract value share on those nights: per contract
  `accrual.DayQuota(total_cents, contract_days, i)` summed over the nights of this allocation in the period
  (`contract_days = ends_on − starts_on`). Owner allocations: applied = 0 and no list price (rights are not sold
  per night) — excluded from lost revenue, shown with `lost=null`.
- **List price** = `pricing.Quote` with the price list active for the contract type on the allocation's first
  night, resource = this berth (category, length, width, groups), vessel = berth `max_loa_cm × max_beam_cm`,
  same contract type (`kind`/`term` → `pricing.ContractType`), arrival = allocation first night, departure =
  last night + 1, desk channel, no customer category, no services; its `total_cents` spread with `DayQuota` over
  the stay and summed over the nights in period. Quote errors (no active list, no rule) → `list=null`,
  `unpriced` note.
- `lost_cents = max(0, list − applied)`. Totals: count, Σ waste-weighted nights, Σ lost.
- **Suggestions** (pure `swap`, read-only, never applied by the report):
  - `swap`: pairs of listed or non-listed allocations `(A on X, B on Y)` of kinds contract/transit, same map,
    such that every night of A outside B's nights is free on Y and vice versa (free = no occupancy row, no
    closure), `fit.Check(A.vessel, Y)` and `fit.Check(B.vessel, X)` have no blocking issue, and the summed
    `vessel_area_waste × nights` strictly decreases. Greedy by gain, disjoint pairs, at most 50.
  - `move`: for a listed allocation, `engine.Solve` on a snapshot built from the report rows (berths only,
    `MaxSegments=1`, purpose by kind) and keep the first solution on a different berth with lower waste.
  - Each suggestion: `{kind, allocation_ids, from_berths, to_berths, waste_before_bp, waste_after_bp,
    gain_cm2_nights}`; the UI links to the operational map's move/swap dialog (existing M3 operations).

### 3.4 R4 Lost demand (`lost-demand`)

Rows: `inquiries` with `outcome IN ('declined_no_space','declined_size','declined_other','lost')` and at least
one night `[arrival, departure − 1]` in the period. `group_by=reason|month|week|length_band|channel|source|purpose`
(week = ISO week of each night; a multi-night inquiry contributes its nights to each bucket, its count to the
bucket of its arrival, D10).
- `nights_in_period`, `count` (1 per inquiry).
- **Estimated value** (`reports.all` only): `pricing.Quote` with vessel = inquiry LOA/beam, contract type from
  `purpose`/`term` (`transit` → transit; contract → the term, `period` → transit), arrival/departure of the
  inquiry, resource = the smallest-area active berth that `fit.Check` accepts without blocking issues (ignoring
  occupancy; its category/length/width), desk channel; the total spread by `DayQuota` and summed over nights in
  period. No compatible berth or quote error → value `null` and `unpriced` count (these are the "missing sizes").
- Records: one per inquiry (`ref inquiry`), order arrival, id.

### 3.5 R5 Customers (`customers`)

Population: customers with at least one **qualifying contract** (§3.6 statuses) overlapping the period, or new in
the period. `group_by=category|nationality|seniority|channel|value_band`.
- **New customer** in period: the customer's first qualifying contract (min `starts_on`, any kind) starts in the
  period. **Acquisition channel** = `contracts.channel` of that first contract (D11).
- **Seniority** = whole years from the first qualifying contract start to `to`; bands `0`, `1–2`, `3–4`, `5–9`,
  `≥10`.
- **Value** = production (§3.6) of the customer's contracts in the period.
- **Arrears** = overdue residual as of today (§3.12) of the customer.
- **Renewals**: §3.17.
- Records: one per customer: `name` (display name only, no contacts), category, nationality, first contract,
  seniority, new (0/1), value, arrears, contracts count. Order value desc, name.

### 3.6 R6 Production (`production`) — "Competenze" of contracts and transits

**Qualifying contracts**: kind `mooring` or `charter` with status `active|expired|renewed|terminated|suspended`;
kind `transit` with status `confirmed|arrived|departed`. Excluded: drafts, awaiting signature, cancelled, held,
pending payment, awaiting approval, expired holds, no-show (D12); kind `storage` (M9).
- Line = `accrual.Line{ID: contract id, Amount: total_cents, From: starts_on, To: ends_on − 1}` (contract value
  incl. VAT, as stored; D12). `competence = accrual.Within(line, from, to)`; `nights_in_period` = days of the line
  inside the period.
- Groups: `kind|term|month|category (resource line's price_class)|channel|customer`.
- Records: one per contract (`ref contract|transit`), order starts_on, number.
- Indicators: `production = Σ competence`; `transit_avg_night = Σ competence / Σ nights` over transits
  (cents per night, half-even), records = the transit records.

### 3.7 R7 Accruals and deferrals (`accruals`)

Closing date `C = to`; competence period `[from, to]`. Pure `calc.Accruals` over `accrual.Allocate`:
- **Charge lines** of non-cancelled charges (incl. negative adjustments): `Line{amount = net_cents}` and a second
  run with the VAT (`gross_cents − net_cents`), period `[service_from, service_to]` (line, else charge), else the
  single day `issued_on`. Competence of the period = Σ quotas in `[from, to]`; the gross quota is net quota + VAT
  quota, the same method as §3.8, so R7 and R8 (competence basis) show the same gross for the same line and period
  (`TestCalc_AccrualsCompetenceEqualsChargesCompetence`).
- **Deferral (risconto)** at `C` = for lines of charges with `issued_on ≤ C`: Σ quotas on days `> C`.
- **Accrual (rateo)** at `C` = per qualifying contract (§3.6): `matured = Within(contract line, starts_on, C)`
  minus `charged = Σ gross of its non-cancelled charges with issued_on ≤ C` (`charges.contract_id`); rateo =
  `max(0, matured − charged)` (gross; contracts carry gross values, D12).
- Invariant (property test): for each charge line with `issued_on ≤ C`: competence up to `C` + deferral =
  amount (INV-60 extended).
- Records: sections `competence` (one per charge line), `deferral` (one per line with deferral ≠ 0), `accrual`
  (one per contract with rateo > 0). Order section, date, number, seq.

### 3.8 R8 Charges (`charges`)

`basis=issued|competence` (default issued). Rows: charges (and lines) with `payment_status <> 'cancelled'`;
filters customer, item, origin (default all except `opening_balance`, D14), payment and invoicing status, fiscal
mode. Issued basis: charge in period by `issued_on`, full amounts. Competence basis: line quotas in period as in
§3.7 (net, VAT and gross each allocated; VAT = gross − net per quota is **not** recomputed: VAT is allocated
itself and the gross quota = net quota + VAT quota so totals stay exact).
`group_by=origin|customer|item|revenue_class|payment_status|invoicing_status|month|vat_rate`.
Records: one per charge line (`ref charge`), order issued_on, number, seq: net, VAT, gross, rate, status columns.
Indicator **charged** = Σ `total_cents` of charges issued in the period (issued basis, default origins).

### 3.9 R9 Payments (`payments`) — base incasso

Rows: payments with `kind IN ('payment','deposit')` and `received_on` in period (credits `kind='credit'` are
never cash, M5 D5) → `+amount_cents`; refunds `kind='refund'`, `status='succeeded'`, `business_date` in period →
`−amount_cents`. Payouts (`kind='payout'`, mandates) listed in a separate section and excluded from the
indicator. Provider fees shown, not deducted. `group_by=method|provider|source|month|customer`.
Records: one per payment or refund (`ref payment|refund`), order date, number.
Indicator **collected** = Σ records of the main section.

### 3.10 R10 Revenue classes (`revenue-classes`)

`basis=competence|issued` (default competence). Σ `net_cents` by `charge_lines.revenue_class_code` (empty code →
`unclassified`) per month (competence: §3.7 quotas; issued: by `issued_on`). Parent classes roll up children
(one level). Percent of each class = `BP(class, total)` (display; not forced to sum to 10000). Records = charge
line contributions (same as R8 rows with the month).

### 3.11 R11 Comparison — §5.

### 3.12 R12 Receivables and reminders (`receivables`)

`as_of` (default today, ≤ today). Rows: positive charges with `payment_status IN (open, partially_paid)` and
residual `total − paid − credited > 0` (current residuals; D13), `issued_on ≤ as_of`. Bucket =
`payment.BucketOf(due_on, as_of)` (`not_due, d0_30, d31_60, d61_90, d90_plus`). Also open scheduled instalments
(`installments`, `holder_installments` with `status='scheduled'`, `due_on ≤ as_of + 90`) as a "future" section
(not in totals). Reminders section: `reminder_items` with `status='sent'` and `updated_at` local date in
`[from, to]` by level. `group_by=bucket|customer|origin`.
Indicators: **receivables_open** = Σ residuals; **receivables_overdue** = Σ residuals with `due_on < as_of`.
Records: one per charge (`ref charge`), order due_on, number.

### 3.13 R13 Presences (`presences`)

- Arrivals / departures: `presences` with `event='arrived'|'departed'` and `night` in period (count; by
  day/week/month, by contract kind).
- **Absence nights** of annual mooring contracts: for each vessel of an active annual contract, nights in the
  period inside the contract and between a `departed` event and the next `arrived` event (open-ended absence
  until `min(to, today)`), counted per contract.
- Releases: `releases` with `status='active'` overlapping the period: released nights in period, resold nights
  (nights of the release range with an occupancy row of `resold_allocation_id`), credit (`credit_cents`).
- **Release revenue** (`reports.all`): competence (§3.6 DayQuota) of the resale transits on the released nights in
  the period.
- Records per section (`ref presence|contract|release`).

### 3.14 R14 Rights and condominium (`rights`)

- Holders: `berth_rights` active in the period with holders (`share_millis`) and berth (count, list).
- Charges of holder instalments: charges with `origin_type='holder_installment'` (or `right_id` set) issued in
  period: issued, collected (`paid_amount_cents`), residual.
- Allotments: `common_charge_runs` with `status='issued'` and `issued_at` local date in period, by kind
  (`advance` = ripartizione, `balance` = conguaglio): Σ `common_charge_allocations.amount_cents` by item, by holder,
  marina own share (`customer_id IS NULL`).
- Mandates: `mandate_revenues` in period and payouts/credits (payments `source='mandate'`, refunds `kind='payout'`).
- Records per section (`ref right|charge|common_charge_run|payment|refund`).

### 3.15 R15 Cash and fiscal receipts (`cash`)

- Sessions with `business_date` in period: register, status, float, expected/counted per method, `difference_cents`.
- Corrispettivi: `fiscal_documents` `status='issued'` with `business_date` in period: totals by VAT rate/nature
  (`vat` JSON, `cash/summary.VATTotals`) and by payment method (join `payments.method`); `void` documents listed
  with negative sign in a separate column, not in totals (D15).
- Records: sessions and fiscal documents (`ref cash_session|fiscal_document`).

### 3.16 R16 Hosted vessels (`vessels`)

Vessels with ≥ 1 physical occupied night (any family) in the period. `group_by=hull_type|length_band|flag|kind`.
Records: one per vessel: nights, LOA (effective), beam, hull, flag, band. Indicators: **vessels_hosted** = record
count; **vessels_avg_loa** = Σ LOA / count (cm, half-even).

### 3.17 R5 renewals detail (inside `customers`, section `renewals`)

Eligible: mooring contracts with `term IN ('annual','seasonal')`, qualifying status, last night `ends_on − 1` in the
period. Renewed: `status='renewed'` or a `renewal_proposals` row with `source_contract_id` = it and a
`new_contract_id` whose contract is qualifying. **renewal_rate** = Σ renewed / count eligible. Records one per
eligible contract (`renewed` 0/1).

### 3.18 R17 Berth revenue (`berth-revenue`)

Per active berth (filters as R1, family berth): `revenue` = Σ over its occupancy rows `(berth, d)` in the period of
an occupied kind whose allocation belongs to a qualifying contract: `DayQuota(contract total, contract_days,
index of d)`; `length_cm`. Indicator **berth_revenue_per_m** = `DivRoundHalfEven(Σ revenue × 100, Σ length_cm)`
(cents per metre). Records: one per berth (`ref berth`), order code.

## 4. Dashboard (21.2)

`GET M/dashboard?from&to&compare=previous_year|none&align=date|weekday&map_id&structure_id&group_id&category`.
Default period **year to date** (Jan 1 → today); presets in the UI: `mtd`, `ytd`, `last_30`, `last_12m`, `custom`.
Dimension filters apply to occupancy, efficiency and berth revenue tiles only (`dims_applied` per tile).

Tile = `{key, unit, available, reason?, value, numerator?, denominator?, previous{value, numerator?,
denominator?}?, delta?, delta_bp?, trend: up|down|flat|none, series[{bucket, value, previous}], breakdown[]?,
drill{report, params}}`. Units: `cents`, `bp`, `count`, `cm`, `cents_per_m`, `cents_per_night`.

| Tile key | Value | Drill (report + params) | Permission |
|---|---|---|---|
| `production` | §3.6 Σ competence | `production` | all |
| `charged` | §3.8 Σ total, issued basis | `charges?basis=issued` | all |
| `collected` | §3.9 | `payments` | all |
| `receivables_open` | §3.12 as of today (no previous) | `receivables` | all |
| `receivables_overdue` | §3.12 | `receivables?overdue=true` | all |
| `occupancy` | §3.1 physical, family berth, water maps | `occupancy?variant=physical&family=berth&map_kind=water` | operational |
| `efficiency` | §3.2 berths, water maps | `efficiency?family=berth&map_kind=water` | operational |
| `berth_revenue_per_m` | §3.18 | `berth-revenue?map_kind=water` | all |
| `transit_avg_night` | §3.6 transits | `production?kind=transit` | all |
| `new_customers` | §3.5 count; breakdown nationality top 5, channel | `customers?new=true` | all |
| `renewal_rate` | §3.17 | `customers?section=renewals` | all |
| `vessels_hosted` | §3.16; breakdown hull, band, flag | `vessels` | operational |
| `vessels_avg_loa` | §3.16 | `vessels` | operational |
| `yard_*`, `rental_*` | `available=false` | none | — |

- `previous` = same tile on the previous period (§5, `years_back=1`, chosen alignment). `delta = value −
  previous.value`; `delta_bp = BP(delta, |previous|)` (null if previous 0); trend by sign of delta (`none` without
  previous). Series granularity: day if ≤ 31 days, week if ≤ 120, else month.
- The endpoint needs `reports.operational` or `reports.all`; it returns only the tiles the principal may see (no
  403 for a partial set). Without either permission: 403 handled by the UI by not rendering the section (dock).

## 5. Comparison and alignment (`align`, R11)

`GET M/reports/comparison?indicator&from&to&years_back=1..5&align=date|weekday&granularity=day|week|month&<dims>`.
Indicators: every dashboard tile key except `receivables_*`, `vessels_hosted`, `vessels_avg_loa` (not additive by
day), plus `arrivals`, `departures`, `lost_demand_count`, `lost_demand_value`.

- Each comparable indicator exposes **daily facts** `map[date]{num, den}` built from the same records (record →
  its days: occupancy per night, production per DayQuota day, charges per issued day, payments per date, new
  customers per first-start day, renewals per last night, lost demand per night / arrival). Property: Σ daily ==
  tile.
- **Previous period**: `date` → `[AddYears(from, −k), AddYears(to, −k)]` (Feb 29 → Feb 28). `weekday` → shift
  `s = 7 × round(365.2425 × k / 7)` days (364 for k=1): `[from − s, to − s]`.
- **Alignment of series**: `date` → previous day `p` maps to current `AddYears(p, k)` (Feb 29 → Feb 28: a previous
  Feb 29 adds to Feb 28; a current Feb 29 has no previous); `weekday` → `p + s`. Never by index.
- Buckets: ISO week (Monday) or calendar month of the **current** date; ratios recomputed per bucket from Σ num /
  Σ den. Output: `{indicator, unit, current{from,to,value,num,den}, previous{…}, delta, delta_bp, series[{bucket,
  current, previous}]}`; CSV/XLSX one row per bucket.

## 6. Drill-down contract and the equality guarantee

1. Every report `calc.Build<X>(in) Result` returns `Records` with integer contributions (`value` or
   `num`/`den`) plus `Totals`; `Totals` is computed **only** by `calc.Fold(records)` (one generic fold per unit).
2. A tile is `calc.Tile<X>(result)`: it reads `result.Totals` of the very `Build<X>` call made with the drill
   params. The dashboard service builds each tile by calling the report service's internal `run<X>(ctx, tx,
   scope, params)` with exactly `tile.drill.params` + period → same SQL, same rows, same fold. Previous value:
   same call with the previous period.
3. `drill.params` is generated by the same Go function that the report handler parses (`report.ParamsOf(tile)` /
   `report.ParseParams`), round-trip tested.
4. Pagination never changes totals; CSV/XLSX contain all records of the same `Result` (§7).
5. Tests: `TestCalc_TileEqualsFold` (rapid, every report: random rows → Σ records == tile, Σ daily == tile);
   `TestDashboard_DrillParamsRoundTrip`; acceptance `TestAcceptance_M7_DashboardDrillDown` through the HTTP API on
   the seeded demo for three periods (§11.4).

## 7. CSV and Excel (`export`, REPORTS)

- `export.Table{Columns []Column{Key, Label(it/en), Type: text|int|cents|bp|cm|cm2|date|count}, Rows [][]Value}`
  built by `export.TableOf<X>(Result, lang)` from `Result.Records` (the JSON records' fields, same order).
- CSV: UTF-8 with BOM, `;`, CRLF, decimal comma, dates `GG/MM/AAAA`, cents `1234,56`, bp as percent `12,34`, cm as
  metres `12,50`, cm² as m² with 2 decimals (half-even), cm-nights as metre-nights; text cells starting with
  `= + - @` or tab/CR prefixed with `'` (formula injection). Last row `Totale` from `Result.Totals`.
- XLSX (`internal/xlsx.Write`): sheet `Dati` (same columns, numbers as decimal cells from integers, no float),
  sheet `Riepilogo` (groups), bold header, widths.
- Headers labelled in the user's locale (it default, en), formats always Italian (spec 21.3).
- `format=csv|xlsx` (or `Accept: text/csv`), `Content-Disposition: attachment; filename="porto-<key>-<from>-<to>.<ext>"`.
  Export ignores `page`; capped at 200 000 rows (`422 report_too_large`, D16).
- Tests: `TestExport_CSVItalian`, `TestExport_XLSXRoundTrip` (read back with `internal/xlsx` reader: same cells),
  `TestExport_SameRowsAsJSON` (every report: CSV rows == JSON records, value column sum == totals).

## 8. API (CONTRACT, `api/openapi.yaml`, tag `reports`; M = `/api/admin/v1/marinas/{marinaId}`)

| Method path | operationId | Handler owner |
|---|---|---|
| GET `M/dashboard` | `getDashboard` | DASHBOARD `internal/http/admin/dashboard.go` |
| GET `M/reports/comparison` | `getReportComparison` | DASHBOARD `internal/http/admin/report_comparison.go` |
| GET `M/reports/occupancy` | `getOccupancyReport` | REPORTS `internal/http/admin/reports.go` |
| GET `M/reports/efficiency` | `getEfficiencyReport` | REPORTS |
| GET `M/reports/allocation-waste` | `getAllocationWasteReport` | REPORTS |
| GET `M/reports/lost-demand` | `getLostDemandReport` | REPORTS |
| GET `M/reports/customers` | `getCustomersReport` | REPORTS |
| GET `M/reports/production` | `getProductionReport` | REPORTS |
| GET `M/reports/accruals` | `getAccrualsReport` | REPORTS |
| GET `M/reports/charges` | `getChargesReport` | REPORTS |
| GET `M/reports/payments` | `getPaymentsReport` | REPORTS |
| GET `M/reports/revenue-classes` | `getRevenueClassesReport` | REPORTS |
| GET `M/reports/receivables` | `getReceivablesReport` | REPORTS |
| GET `M/reports/presences` | `getPresencesReport` | REPORTS |
| GET `M/reports/rights` | `getRightsReport` | REPORTS |
| GET `M/reports/cash` | `getCashReport` | REPORTS |
| GET `M/reports/vessels` | `getVesselsReport` | REPORTS |
| GET `M/reports/berth-revenue` | `getBerthRevenueReport` | REPORTS |

Shared components: `ReportPeriod`, `ReportPage`, `ReportRef`, `ReportFilters`, `Tile`, `TileSeriesPoint`,
`ComparisonResult`; parameters `from`, `to`, `format` (`json|csv|xlsx`), `page`, `page_size`, `group_by` (enum per
report), dims. Responses 200 JSON / `text/csv` / `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`;
problems 403, 404, 422 (`invalid_period`, `invalid_report_filter`, `report_too_large`, `unknown_indicator`).
GET only: no Idempotency-Key, no CSRF impact. Meta capability `reports: true`.

## 9. Store (ENGINES, `internal/store/report_*.go`)

Raw row types `Report*Row` and functions `Report*(ctx, db bun.IDB, s MarinaScope, f ReportFilter)`, a fixed number
of queries per report (≤ 12, `TestReport_QueryCount`), each `WHERE marina_id = ?` first:
`ReportResources` (berths, areas, land + map kind, structure, groups, category, dims, active),
`ReportClosures`, `ReportBerthNights`/`ReportLandNights` (resource, night, allocation_id, kind),
`ReportCellNights` (area, night, allocation_id, kind, `count(*)` cells — D2), `ReportAllocations` (+ contract line
→ contract, vessel), `ReportReleases`, `ReportContracts` (qualifying + period overlap or needed for C),
`ReportVessels`, `ReportCustomers` (display name, category, nationality), `ReportFirstContracts`,
`ReportRenewals`, `ReportInquiries`, `ReportCharges`, `ReportChargeLines`, `ReportPayments`, `ReportRefunds`,
`ReportOpenCharges`, `ReportScheduledInstallments`, `ReportReminders`, `ReportPresences`, `ReportRights`,
`ReportHolderCharges`, `ReportCommonRuns`, `ReportMandates`, `ReportCashSessions`, `ReportFiscalDocuments`.
Migration `00015_report_indexes.sql` (both dialects, CONTRACT): `charges_issued_idx (marina_id, issued_on)`,
`payments_received_idx (marina_id, received_on)`, `refunds_business_date_idx (marina_id, business_date)`,
`inquiries_arrival_idx (marina_id, arrival)`. Indexes only: no backup change.

## 10. Web

### 10.1 Routes (`web/admin`, CONTRACT adds factories in `router.tsx`, nav group "Analisi")

- `/m/$marinaId` — `DashboardPage`: the `EmptyState` placeholder is replaced by `<ManagerDashboard />` (DASHBOARD);
  existing cards stay below. Mobile redirect logic unchanged.
- `/m/$marinaId/reports` — index of report cards grouped (Operativi / Economici), filtered by permission (REPORTS).
- `/m/$marinaId/reports/$report` — report page (REPORTS): `ReportFilterBar` (period presets + date pickers,
  dims via `ElementCombobox`/selects, `group_by`), summary cards from `totals`, chart (shadcn `ChartContainer`, bar
  or line by report), groups `Table`, records `Table` with pagination and `ref` links, `Totale` row, buttons
  "Esporta CSV" / "Esporta Excel" (same query + `format`). All filter state in validated search params, so a tile
  link reproduces the filter exactly.
- `/m/$marinaId/reports/comparison` — comparison page (DASHBOARD): indicator select, years back, align toggle
  (`ToggleGroup` date/weekday), granularity, line chart current vs previous, delta table, export.
- Phone `/m/$marinaId/direction?tab=dashboard` (DASHBOARD): the placeholder dashboard tab shows the tiles the role
  may see (one column, compact `TileCard`, period selector `mtd|ytd`), keeping today's counts below; a tile opens
  the report page (AppShell, responsive). Report pages must work at 412 px: filter bar in a `Sheet`, tables
  horizontally scrollable inside `ScrollArea`, chart full width.

### 10.2 Shared pieces

`web/shared/src/api/hooks/reports.ts` (REPORTS: `useReport(key, params)`, `useReportExport`) and
`dashboard.ts` (DASHBOARD: `useDashboard`, `useComparison`), query keys in `queryKeys.ts` (CONTRACT). Number
formatting via existing `web/shared/src/lib/format` (+ `formatBp`, `formatCmAsMetres` added by CONTRACT). Drill link
builder `web/shared/src/lib/reportLinks.ts` (CONTRACT, typed from the generated `Tile.drill`). i18n namespaces
`reports`, `dashboard` (it/en files created by CONTRACT; values filled by owners). shadcn `chart` added by CONTRACT.

## 11. Tests

### 11.1 Pure (table tests; names fixed)

`TestOccupancy_Table` (closures of each scope, block/maintenance, holds free, released unsold free in physical and
occupied in contractual, resold, inactive resource, linear and surface areas with blocked cells),
`TestEfficiency_Table`, `TestWaste_Table` (threshold, owner allocations, unpriced), `TestSwap_SuggestionsValid`
(rapid: every suggestion conflict-free, fit without blocking, strictly positive gain, pairs disjoint),
`TestProduction_Table` (status inclusion, terminated, transit avg night), `TestAccruals_Table`,
`TestAccruals_CompetencePlusDeferralEqualsAmount` (rapid, INV-60 extension), `TestCharges_CompetenceVATExact`,
`TestPayments_Table` (credits excluded, refunds negative, payouts separate), `TestRevenueClasses_MonthsSumToTotal`,
`TestLostDemand_Table`, `TestCustomers_Table` (new, seniority, channel, renewals), `TestReceivables_Table`,
`TestPresences_Table` (absence nights), `TestRights_Table`, `TestCash_Table`, `TestVessels_Table`,
`TestBerthRevenue_Table`, `TestCalc_TileEqualsFold` (rapid, all reports), `TestCalc_DailyEqualsTile` (rapid),
`TestAlign_DateAndWeekday` (leap years, k = 1..5, never by index), `TestAlign_Buckets`, `TestBP_HalfEvenAndNil`,
export tests of §7.

### 11.2 Integration (`//go:build integration`, `testdb` SQLite + PostgreSQL)

`TestReportStore_<Query>_Scoped` (rows of another marina never returned), `TestReport_ReadOnly` (INV-61: every
report and the dashboard run; `audit_log`, `outbox_events`, `idempotency_keys` counts and SQLite `total_changes()`
unchanged), `TestReport_Permissions` (role matrix: owner, director, accounting, viewer all; office operational
only and money columns `null`; dock/yard/rental 403), `TestReport_QueryCount`, `TestReportsAPI_Formats` (content
types, disposition, BOM), `TestMigrations_*` (00015), `TestReport_PerformanceBudget` (§11.5).

### 11.3 Cross-cutting

`TestAcceptance_CrossMarinaIsolation` covers the new routes automatically (it walks every `{marinaId}` route);
`TestLogs_NoPII` extended with a report and an export run.

### 11.4 Acceptance (`internal/acceptance/m7_reports_test.go`, API only, `clock.Fake` at a fixed date, both dialects)

`TestAcceptance_M7_DashboardDrillDown`: `app.SeedDemo`, login as owner; for periods `mtd`, `ytd`, `last_12m`:
GET dashboard; for every available tile follow `drill` (+ period), read **all** record pages and assert Σ records
== tile value (num/den for ratios, and the bp recomputed equals the tile); the same for `previous` with the previous
period; download CSV and XLSX and assert the parsed value column sum == tile. `TestAcceptance_M7_Roles` (office sees
only operational tiles; accounting all; dock gets no tiles). `TestAcceptance_M7_Comparison` (Σ series ==
current/previous totals, both alignments).

### 11.5 Performance budget (demo data, SQLite, server-side, `TestReport_PerformanceBudget`, skipped with `-short`)

Per report JSON, 366-day period, first page: ≤ **500 ms** (occupancy/efficiency/waste with suggestions ≤ **1 s**);
dashboard with previous year ≤ **2 s**; CSV of all records of the occupancy year ≤ **2 s**. Budgets × 3 under
`-race`; PostgreSQL measured and logged, not asserted. A miss is fixed by indexes/queries, not by caching.

### 11.6 Web

Vitest: `TileCard.test.tsx` (value, previous, delta, unavailable seam), `reportSearch.test.ts` (search params
round-trip, drill link == report params), `ReportPage.test.tsx` (totals row equals `totals`, export calls with
format), `ComparisonPage.test.tsx` (series mapping), `DirectionDashboardTab.test.tsx`.
Playwright: `e2e/tests/dashboard.spec.ts` (tile → report → `Totale` cell text equals tile text), `reports.spec.ts`
(filter, paginate, CSV download starts with BOM and `;` header, Excel download), `comparison.spec.ts`,
`phone-direction.spec.ts` updated (Pixel 7: tiles visible, tap opens report, no horizontal page scroll),
`a11y.spec.ts` adds dashboard and one report.

## 12. Ownership (CONTRACT first, then three builders in parallel)

### 12.1 CONTRACT (then frozen)

`api/openapi.yaml` + `make gen`; `internal/migrations/{sqlite,postgres}/00015_report_indexes.sql`;
`internal/domain/report/types.go` (exported params, `Result` and record types, report/tile keys, units, errors
`ErrInvalidPeriod`, `ErrInvalidFilter`, `ErrTooLarge`, `ErrUnknownIndicator`) and `service.go` skeleton (`New`,
method signatures returning `ErrNotImplemented`); `internal/domain/report/{calc,align,swap,export}/doc.go`;
`internal/http/admin/admin.go` (`Reports` field), handler stubs `reports.go`, `dashboard.go`,
`report_comparison.go` (501); `internal/http/errmap.go` / problem slugs; `internal/app/app.go` wiring;
`internal/domain/meta` capability; `.golangci.yml` (pure-engines + report no-write imports); web: shadcn `chart`
(+ lockfile), `router.tsx` factories `reportsRoutes`, `dashboardRoutes` (stub pages), `nav.ts` entry, `queryKeys.ts`,
`lib/format` helpers, `lib/reportLinks.ts`, i18n files `reports.json`, `dashboard.json` (it, en) + `resources.ts`,
hook file stubs; docs §12.4. Ends with `make gen && make check` green.

### 12.2 Builders

| Builder | Owns (exclusive) |
|---|---|
| **ENGINES** | `internal/domain/report/{calc,align,swap}/**`; `internal/domain/report/*.go` except `types.go` signatures (implements the service, `run<X>`, dashboard tiles and comparison facts, `ParamsOf/ParseParams`); `internal/store/report_*.go` + tests; `internal/domain/fit/api.go` (only the new exported `FillBP`); `internal/domain/pricelist/report_quote.go` (new: batch list-price quoting in a read tx, catalog cached per list, no permission re-check); tests §11.1 (except export), §11.2 except `TestReportsAPI_Formats`, `internal/acceptance/m7_reports_test.go`; `docs/invariants.md` INV-62..64 status |
| **REPORTS** | `internal/domain/report/export/**`; `internal/http/admin/reports.go` + tests; `web/admin/src/features/reports/**` (index, report page, filter bar, tables, charts, export buttons) except `comparison/`; `web/shared/src/api/hooks/reports.ts`; i18n values `reports`; tests §7, `TestReportsAPI_Formats`, Vitest `reportSearch`, `ReportPage`; `e2e/tests/reports.spec.ts` |
| **DASHBOARD** | `internal/http/admin/{dashboard,report_comparison}.go` + tests; `web/admin/src/features/dashboard/**` (`ManagerDashboard`, `TileCard`, sparkline, period selector), `web/admin/src/features/reports/comparison/**`; `web/admin/src/pages/DashboardPage.tsx` (placeholder replacement only); `web/admin/src/features/phone/DirectionPage.tsx` dashboard tab (+ a new `features/phone/components/DirectionDashboardTab.tsx`); `web/shared/src/api/hooks/dashboard.ts`; i18n values `dashboard`, `phone` (dashboard keys only), `common` (remove `dashboard.comingSoon` use); Vitest of §11.6 for these; `e2e/tests/{dashboard,comparison}.spec.ts`, `phone-direction.spec.ts`, `a11y.spec.ts` additions |

### 12.3 Parallel-phase rules

- OpenAPI, migration, `report/types.go` and web stubs are frozen; changes are requested in the builder report.
- REPORTS and DASHBOARD develop against the generated client with MSW/fixtures until ENGINES lands; handlers call
  only the service methods declared by CONTRACT.
- DASHBOARD links to report pages only through `lib/reportLinks.ts` and the `Tile.drill` payload; it never
  builds report params by hand.
- Shared Go files are edited only at the listed spots.

### 12.4 Docs (CONTRACT)

ADR `docs/adr/0012-report-e-cruscotto-m7.md` (Italian, "proposta"): report as queries on raw rows + pure calc; tile
= fold of drill-down records; occupancy and efficiency definitions incl. holds, releases, areas; production on
contract value; accruals; incasso basis; alignments; CSV/XLSX from one table; permission split operational/all;
seams M8–M11. `docs/invariants.md`: **INV-61** → implemented (`TestReport_ReadOnly`); **INV-62** a dashboard tile
equals the fold of its drill-down records (`TestCalc_TileEqualsFold`, `TestAcceptance_M7_DashboardDrillDown`);
**INV-63** CSV/XLSX rows equal the JSON records and totals (`TestExport_SameRowsAsJSON`); **INV-64** report
values are integers (cents, cm, bp) and ratios are recomputed from sums, never averaged
(`TestCalc_DailyEqualsTile`, purity lint). `docs/architecture.md` §2 and §10 (report packages).

## 13. Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
go test -count=1 ./internal/domain/report/... ./internal/domain/accrual/... ./internal/domain/fit/...
go test -count=1 ./internal/domain/report/... -run 'TestCalc_|TestAccruals_CompetencePlusDeferral|TestSwap_SuggestionsValid|TestExport_SameRows' -rapid.checks=2000   # flags after the packages: go test stops reading packages at an unknown flag
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/store/... ./internal/domain/report/... ./internal/http/...
go test -tags=integration -count=1 -run 'TestReport_PerformanceBudget' ./internal/domain/report/...
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M7|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII'
make test-db && make test-race
npm --prefix web run test
make e2e            # dashboard, reports, comparison, phone-direction (Pixel 7), a11y
make licenses && scripts/agents/check-naming.sh
```

Manual check (final report): `make seed && make dev`, owner: every tile opens its report with the same period and
the `Totale` equals the tile; CSV opens in Excel (Italian locale) with correct columns and decimals; the Excel file
opens in LibreOffice; on a phone-sized window the direction dashboard tab and a report are usable without
horizontal page scroll.

## 14. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | One typed GET per report (17) plus `dashboard` and `comparison`; `format=json|csv|xlsx`; max period 400 days. |
| D2 | SQL may `GROUP BY` identity keys only to count physical rows (cells per allocation-night); no money/ratio in SQL. |
| D3 | Ratios in integer basis points, half-even, `null` on zero denominator; groups/buckets recompute Σnum/Σden. |
| D4 | Holds are available and free for occupancy; `block`/`maintenance` rows and closures remove the night from the available set. |
| D5 | Only currently active resources are counted (no activation history exists). |
| D6 | Dynamic areas: linear in cm-nights, surface in cm²-nights (cells × cell size); contractual variant re-adds released footprints from the allocation placement. Families are never summed together; the dashboard occupancy tile is berths on water maps. |
| D7 | Vessel sizes are the current effective ones (measured over declared); no history. |
| D8 | Fixed LOA bands (<8, 8–10, 10–12, 12–15, 15–20, 20–25, ≥25 m). |
| D9 | Allocation waste and lost potential revenue on berths only; list price = quote for a vessel of the berth's max LOA × max beam, same contract type and stay; lost = max(0, list − applied); owner allocations excluded. |
| D10 | Lost demand counts an inquiry in its arrival bucket and spreads nights and value over night buckets; value priced on the smallest compatible berth's category, else `unpriced`. |
| D11 | New customer and acquisition channel come from the first qualifying contract (customers have no channel field). |
| D12 | Production and rateo use the contract value `total_cents` (VAT incl., as stored) spread by day; qualifying statuses as §3.6 (no-show and cancelled excluded; terminated keeps its full value, termination amounts appear as charges). Charge-based reports carry net, VAT and gross. |
| D13 | Receivables use current residuals as of a date ≤ today; receivables tiles have no previous-year value. |
| D14 | `opening_balance` charges are excluded from the default origins of `charged` and charge reports (selectable). |
| D15 | Corrispettivi totals use issued fiscal documents; voided ones are listed separately, not netted. |
| D16 | Export cap 200 000 rows (`report_too_large`); CSV labels follow the user locale, number/date formats are always Italian. |
| D17 | Permission split: `reports.operational` → occupancy, efficiency, waste (no money), lost demand (no value), presences (no revenue), vessels; everything with money needs `reports.all`. `reports.yard`/`reports.rental` reserved for M10/M11. |
| D18 | Dashboard default period year-to-date; previous = same dates one year back with date alignment (weekday selectable); trend from the delta sign; series day/week/month by period length. |
| D19 | Weekday alignment shift = 7 × round(365.2425 k / 7) days (364 for one year). |
| D20 | No new demo data: the acceptance test uses the existing seed (billing history reaches back 400 days, occupancy is mostly the current year). |
| D21 | Swap/move suggestions are advisory and read-only; applying one goes through the existing M3 move/swap operations from the map. |
| D22 | Report service reuses `pricelist` catalog loading through a new read-only batch quoting file, without re-checking `pricing.read` per quote (the report permission covers it). |
