# M2 — Listini e prezzi: work order

Status: Plan phase output. This plan is authoritative for the M2 builders. If it conflicts with `docs/spec/`, the
spec wins: stop and report the conflict to the orchestrator instead of guessing. Open decisions of
`docs/spec/33-decisioni-aperte.md` are not decided here (none of them is due by M2). §12 lists every detail this
plan decided where the spec says nothing; the engine semantics of §2.4 are normative because the 200-case corpus
is derived by hand from them.

Sources: spec 05, 06 (§6.1, §6.5), 07 (§7.3 `price_class`), 11 (§11.1 customer categories, §11.2 dimensions and
hull type), 12 (all), 13 (§13.1 contract kinds and terms, §13.7 deposit policy), 16 (§16.1 charge lines as
consumers of prices), 21 (§21.3 revenue classes, lost revenue at list price), 23 and 25 (only to know what is
out), 26.1, 27 (§27.1–27.4), 32 (§32.1, §32.3 M2, M3+ to know what is out); `docs/invariants.md` (INV-20, INV-21,
INV-31, INV-50..53, INV-70, INV-73); ADR 0001, 0002, 0008; `docs/m0-plan.md`, `docs/m1-plan.md`,
`docs/architecture.md`; `.claude/rules/*.md` (especially `charges.md`, `go-backend.md`, `migrations.md`,
`api-contract.md`, `frontend.md`, `security.md`); skills `new-endpoint`, `new-migration`, `new-price-rule-test`,
`new-domain-event`, `shadcn`.

## 0. Ground rules for every builder

Everything in `docs/m0-plan.md` §0 and `docs/m1-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001). Its pure engine (`../arenile/internal/domain/pricing`:
   engine, discounts, tools for matrix/coverage/copy season, property and purity tests) and its loader
   (`../arenile/internal/domain/pricingconfig`) are the closest analogue. Read them to see how a problem was solved
   (rule selection, non-stackable choice, largest-remainder allocation of discounts and rounding, VAT split, matrix
   probes). Then write Porto's own code for nights, lengths and price lists. Never copy files, code, comments or
   tests. Never modify anything under `../arenile`. Arenile prices days and half days of beach packs with
   VAT-inclusive prices and seasons; Porto prices nights, months, seasons and years of berths, by length, with
   per-item VAT treatment: nothing carries over line by line.
2. shadcn/ui components only through the official CLI, run in `web/shared` (CONTRACT, §7.1). Tailwind on theme
   tokens, no custom CSS, `print:` variants for print pages.
3. Do not `git commit`; no destructive git commands; no `rm -rf`. The orchestrator commits.
4. Edit only files you own (§9). To change a file someone else owns, write the request in your final report.
5. Code, identifiers and comments in English. User-facing strings through i18n keys, Italian source, English
   required. Never log or emit names, emails, phones, fiscal codes, IBANs, promo codes typed by customers, tokens.
6. **No floats in domain logic.** Money is `money.Cents`; lengths `geo.Cm` (`int64` cm); areas cm²; percentages
   are integer basis points (`_bp`, 1 % = 100 bp); quantities `qty.Milli`; every division rounds half-even through
   `money.DivRoundHalfEven`, `Cents.MulRat`/`MulRatChecked`, `money.VATSplit`, `money.Allocate`, `qty.Milli.MulCents`.
   The web client may parse "12,50 €" and "22 %" only into integer cents/bp with the shared helpers (§7.1).
7. `internal/domain/pricing` is **pure** (INV-21): no store, no clock, no `time.Now`, no randomness, no I/O.
   depguard `pure-engines` already lists it (`.golangci.yml`); do not add exclusions. Today's date is an explicit
   input (`Input.QuotedOn`). Tests may read `testdata/pricing` with `os` and the same `//nolint:depguard` comment
   used by `layout/footprint_parity_test.go`, in `_test.go` files only.
8. API first. CONTRACT writes `api/openapi.yaml` and runs `make gen`. Builders never edit generated files. A builder
   needing a contract change asks the orchestrator, who reruns CONTRACT.
9. Every state change goes through `catalog` or `pricelist` services inside `store.RunInTx`, with an `audit_log`
   row and a `config.pricing_changed` outbox event in the same transaction. The first line of every service
   method is `p.Authorize(ctx, db, marinaID, perm)`.
10. Every query is scoped by `store.MarinaScope`. A resource of another marina, or a rule/period/band of another
    price list, is `ErrNotFound` (404).

---

## 1. Scope

### 1.1 In M2 (spec 32.3 M2, 12, plus the classifications of 07 and 11 that prices consume)

**Items and services (12.1)**
- `items`: per-marina `code` (unique, upper case, pattern `^[A-Z0-9][A-Z0-9._-]{0,31}$`, e.g. `ORM-ANN`,
  `TRN-NTT`, `SRV-ALG`), name and description (I18nText), `item_type` ∈ `berth_fee`, `service`, `utility`,
  `product`, `labour`, `material`, `fee`, `deposit`; `unit` ∈ `night`, `month`, `season`, `year`, `m`, `m2`, `kwh`,
  `m3`, `hour`, `piece`, `lump`.
- Fiscal: `vat_rate_bp` (0..10000) or, when 0, `vat_nature` (FatturaPA codes, §12 D9); `price_includes_vat`
  (default from pricing settings, default false = VAT excluded, spec 12.1).
- Revenue classification: `revenue_class_id` (class with optional parent = category/subcategory, table
  `revenue_classes`, nine defaults of spec 12.1) and `external_code` (code in the external invoicing software).
- `active`, `online_visible`, `sort_order`. Items referenced by any price rule cannot be deleted (409 `in_use`):
  they are deactivated.

**Classifications**
- **Berth categories** (`berth_categories`, spec 07 `price_class`, 12.2 "gruppo o categoria di posto"): the
  registry of the codes that berths, dynamic areas and land resources carry in `price_class` (M1, free text), with
  name, `applies_to`, sort order, usage counts, and a "sync" that creates the missing categories from the
  harbour price-class rule (M1 `harbor_settings.price_class_rule`) and the codes in use. Categories are what
  `per_berth` prices and what rule scopes and the coverage check enumerate.
- **Length bands** (12.3 `length_band`, 12.5 matrix rows): an ordered list per price list (`price_list_bands`),
  each band inclusive up to `max_cm` (the last may be open), optionally linked to a berth category.
- **Vessel classification** consumed by prices: effective LOA/beam (measured when `dimensions_source =
  measured`, spec 11.2) and `hull_type` (multihull = `catamaran`, `trimaran`). No new vessel table.
- **Customer categories** (11.1): the fixed enum of M1 (`member`, `resident`, `affiliated`, `charter`, `yard`),
  used as a rule scope and by discounts; shown read-only on the classifications page with counts.
- **Berth groups** (M1 `berth_groups`) used as a rule scope.

**Price lists by periods (12.2)**
- `price_lists` with code, name, validity `valid_from..valid_to`, `status` `draft → active → archived`,
  `contract_types` it serves, primary berth-fee item per contract type, engine settings (§2.3) and rules.
- `rate_periods` per price list: `season` periods (high, mid, low) and `event` periods (boat show, regattas) with
  date ranges; events override seasons on the nights they cover.
- Rule scope: item, contract type (`annual`, `seasonal`, `monthly`, `transit`, `storage`, `charter`), berth
  category, berth group, channel (`desk`, `online`, `api`, `external`), customer category, rate period.
- **No overlap:** for any night and contract type at most one `active` price list (INV-24, stricter than "same item
  and scope", §12 D3). Activation "at a date" truncates the predecessor's `valid_to`.
- **Published lists are immutable** (INV-22, DB triggers): only drafts are edited; an active list is changed by
  creating a copy/renewal and activating it.

**All pricing bases of 12.3**: `per_berth`, `length_band`, `per_meter` (with configurable length rounding),
`per_m2` (with minimum beam ratio), `per_m2_berth`, `flat`, `metered`; `dimension_source` ∈ `vessel`, `berth`,
`max`; time units `night`, `week`, `month`, `season`, `year` (plus `stay` for one-off services); split rules
`pro_rata_daily`, `whole_months`, `best_combination`; integer arithmetic with half-even rounding.

**Pure engine 12.4** in `internal/domain/pricing`: base price per block (priority → specificity → recency,
`ErrNoPrice`), modifiers (multihull, overhang, season, long stay, last minute, early booking, channel; stackable
sum, non-stackable most favourable to the customer), services (included and added), discounts (customer category →
promo code → automatic), total rounding (none, euro, 5 euro), deposit from the policy, VAT split per rate/nature,
explanation trace, `EngineVersion`, reproducible snapshot (INV-20).

**Tools (12.5, 12.6)**
- **Simulator**: vessel (or dimensions), period, contract type, berth (or category), customer (or category),
  channel, promo code, services → full breakdown with winning rules and trace. Works on active, archived and draft
  lists (explicit id) so a draft can be tried before activation.
- **Matrix**: bands (or categories) × periods × contract type, computed by the **same engine** through probes,
  printable. Each cell carries its probe; simulating the probe gives the same breakdown (INV-23).
- **Coverage check**: combinations of category/band × period × contract type with no price, plus elements whose
  `price_class` is empty or not a defined category.
- **Renewal / copy** ("nuovo listino da esistente"): copy rules, periods and bands shifting dates by N years
  (0 = plain copy), percent or fixed increase per item, category or whole list, optional ISTAT index entered by the
  manager, amount rounding; preview of differences with the matrix before/after; creates a draft; activation at a
  date.

**Platform additions**: permissions `pricing.configure` and `pricing.read`; event `config.pricing_changed` and SSE
topic `pricing.changed`; problem slugs (§4.5); migration `00004_pricing.sql` for both dialects; backup tables;
meta capabilities; demo seed (§6); invariants INV-22..25 (§8.5).

### 1.2 Explicitly out of M2

| Out | Milestone | Note for M2 code |
|---|---|---|
| Quotes (`quotes`, options, alternatives), inquiries, the availability search that feeds them (13.2, 9.6) | M3 | Quotes will call `pricelist.Service.Quote` and store `pricing.Snapshot` as `price_snapshot` + `engine_version`. No `price_snapshot` column is created in M2. |
| Transits and mooring contracts, `contract_lines`, contract `policy` JSON (13.1) | M3 (base) / M4 (full) | The engine's deposit input is a plain `DepositPolicy`; the contract's policy will map to it. |
| Instalment plans and scadenzario (13.7, INV-31) | M4 | Engine step 6 computes only the **deposit**; the instalment split is a pure function written in M4 (`contract/installments.go`). |
| Renewal campaigns of contracts (13.8) | M4 | Price-list renewal (12.6) is M2 and unrelated to contract renewals. |
| Agreements as a separate entity (`convenzioni` with validity, partner) | after M2 if needed | M2 models "convenzione o categoria del cliente" as customer-category discounts (§12 D11). |
| Promo code usage limits, per-customer counters, online redemption | M8 | M2 promo codes are a discount-rule condition (`promo_code`), no counters. |
| Charges, charge lines, export "da fatturare", e-invoicing (16) | M5 | `items.external_code`, `vat_nature`, `revenue_class` are stored for the export; nothing is charged in M2. |
| Accrual by service period (`accrual`, 21.3) | M7 | Lines carry `service_from`/`service_to` so M7 can accrue them. |
| Reports and dashboard (lost demand value, allocation waste at list price, revenue per metre) (21) | M7 | They will call the simulator service with probes; no report code in M2. |
| Occupancy-based or availability-based modifiers (dynamic pricing), closures (`closures`), `seasons` table of 6.1 | M3 / later | No occupancy input exists in M2 (§12 D2). |
| Publication of the transit matrix on the public page / profile (12.5 "pubblicabile", 25) | M8 / M12 | M2 has the printable matrix only; `price_lists.settings.publish_matrix` flag is stored for later. |
| Storage scenarios and optimizer pricing (17), yard formulas and work-order pricing (18, ADR 0006), rental slot pricing (19), meter readings and utility billing (20) | M9–M11 | `metered` basis and `storage`/`charter` contract types exist in the engine; their consumers come later. |
| Online booking prices "da" and `BookingChannel.PushRates` (23) | M8 | |
| Multi-currency | after M12 (D9) | Amounts are cents of the marina currency (`marinas.currency`); the engine is currency-agnostic. |
| AI anywhere (ADR 0008) | never | |

---

## 2. Pure engine `internal/domain/pricing`

### 2.1 Package layout

```
internal/domain/pricing/
  doc.go           package documentation: the normative semantics of §2.4 in English (PRICING-ENGINE)
  types.go         normative types (§2.2), written by CONTRACT, then owned by PRICING-ENGINE
  api.go           exported entry points (§2.2) — CONTRACT writes stubs returning ErrNotImplemented
  errors.go        sentinels and typed errors (CONTRACT)
  trace.go         trace codes (CONTRACT; the i18n keys pricingEnums:trace.<code> mirror them)
  validate.go      Validate (catalog/list issues)
  select.go        rule matching, specificity, winner selection
  basis.go         amounts per basis, dimension source, length rounding, min beam
  split.go         blocks: exact units, pro_rata_daily, whole_months, best_combination (DP)
  modifiers.go     modifiers per block
  services.go      included and added services
  discounts.go     the three discount groups, largest-remainder allocation
  finalize.go      rounding, VAT, deposit
  matrix.go        Matrix + probes, Coverage
  renew.go         Renew (pure transform of a list) + diff
  hash.go          canonical JSON + sha256 (CatalogHash)
  dates.go         AddMonths/AddYears with end-of-month clamping, Feb 29 → Feb 28
  *_test.go        unit tests, rapid properties, corpus runner, purity test, fuzz
testdata/pricing/  corpus (§2.7)
```

Allowed imports: stdlib (no `os`, `net/*`, `crypto/rand`, `math/rand*`, `time.Now`; `crypto/sha256`,
`encoding/json`, `sort`, `slices` are fine), `internal/money`, `internal/qty`, `internal/calendar`, `internal/geo`.
`calendar` is used only for `Date` arithmetic (it imports `time` for dates, which is fine; `time.Now` is banned by
forbidigo).

### 2.2 Normative types and API (CONTRACT writes them; PRICING-ENGINE may add fields/helpers, never rename)

JSON tags are snake_case and are **the same** as the OpenAPI schemas of §4 (the service maps store rows to these
types; the corpus files use them directly).

```go
package pricing

const EngineVersion = 1 // bump whenever the output for an existing input can change

type ContractType string // annual, seasonal, monthly, transit, storage, charter
type Channel string      // desk, online, api, external
type Stage string        // base, modifier, discount
type Basis string        // per_berth, length_band, per_meter, per_m2, per_m2_berth, flat, metered
type TimeUnit string     // night, week, month, season, year, stay
type SplitRule string    // pro_rata_daily, whole_months, best_combination
type DimensionSource string // vessel, berth, max
type TotalRounding string   // none, euro, five_euro
type AmountRounding string  // none, ten_cents, euro (renewal only)
type DiscountKind string    // customer, promo, automatic
type ItemType string        // berth_fee, service, utility, product, labour, material, fee, deposit
type PeriodKind string      // season, event

type Item struct {
    ID, Code         string
    Type             ItemType
    Unit             string        // informational unit of measure (12.1)
    VATRateBP        int64         // 0..10000
    VATNature        string        // "" or a FatturaPA nature when VATRateBP == 0
    PriceIncludesVAT bool
    RevenueClass     string        // revenue class code (copied to lines)
    ExternalCode     string
}

type DateRange struct{ From, To calendar.Date } // inclusive nights
type RatePeriod struct {
    Code   string
    Kind   PeriodKind
    Ranges []DateRange
}
type Band struct {
    Code     string
    MaxCm    *geo.Cm // nil = open-ended (only the last band)
    Category string  // optional berth category linked to the band (matrix probes)
}

type LengthRounding struct{ StepCm geo.Cm /* 1, 10, 50, 100 */; Mode string /* up, half_even */ }
type Ratio struct{ Num, Den int64 } // Den > 0
type DepositPolicy struct {
    PercentBP   *int64       // 0..10000 of the total
    AmountCents *money.Cents // fixed, capped at the total
}

type ListSettings struct {
    DimensionSource DimensionSource
    LengthRounding  LengthRounding           // default {10, up}
    MinBeamRatio    *Ratio                   // per_m2: beam >= loa × Num/Den (default 1/3); nil = no minimum
    Split           map[ContractType]SplitRule // missing key = Split["*"] or pro_rata_daily
    Rounding        map[ContractType]TotalRounding // missing = Rounding["*"] or none
    Deposits        map[ContractType]DepositPolicy
    SeasonWindow    *DateRange               // required when seasonal is served
    PrimaryItems    map[ContractType]string  // item id of the berth fee per contract type
}

type PriceList struct {
    ID, Code      string
    ValidFrom     calendar.Date
    ValidTo       calendar.Date
    ContractTypes []ContractType
    Settings      ListSettings
    Periods       []RatePeriod
    Bands         []Band
}

type Scope struct { // empty slice = any
    ContractTypes      []ContractType
    Categories         []string
    GroupIDs           []string
    Channels           []Channel
    CustomerCategories []string
    Periods            []string // rate period codes
}
type Conditions struct { // modifiers and discounts; nil = absent
    HullTypes     []string
    OverhangMinCm *geo.Cm
    MinNights     *int
    MaxNights     *int
    LeadMinDays   *int
    LeadMaxDays   *int
}
type Effect struct{ PercentBP *int64; AmountCents *money.Cents } // exactly one
type Inclusion struct{ ItemID string; Qty qty.Milli }

type Rule struct {
    ID          string
    Seq         int64        // recency: higher = more recent (assigned by the service on save)
    Stage       Stage
    Name        string
    Active      bool
    Priority    int64        // -1000..1000, higher wins
    Stackable   bool
    // base
    ItemID      string
    Basis       Basis
    Unit        TimeUnit
    AmountCents *money.Cents          // flat, per_berth (single), length_band (single)
    UnitCents   *money.Cents          // per_meter (¢/m), per_m2 and per_m2_berth (¢/m²), metered (¢/unit)
    Amounts     map[string]money.Cents // length_band: band code → ¢; per_berth: category → ¢
    Includes    []Inclusion
    // modifier / discount
    TargetItemIDs   []string
    TargetItemTypes []ItemType // default [berth_fee] when both target lists are empty
    Conditions      Conditions
    Effect          Effect
    DiscountKind    DiscountKind
    PromoCode       string     // upper case; discount_kind = promo
    Scope           Scope
}

type Catalog struct {
    PriceList PriceList
    Items     []Item
    Rules     []Rule
}

type Vessel struct{ LOA, Beam geo.Cm; HullType string }      // effective dimensions chosen by the service
type Resource struct {                                       // all optional
    Category string; Length, Width geo.Cm; GroupIDs []string
}
type ServiceRequest struct{ ItemID string; Qty qty.Milli }   // Qty 0 = 1

type Input struct {
    ContractType     ContractType
    Arrival          calendar.Date  // first night
    Departure        calendar.Date  // exclusive (ADR 0002): nights [Arrival, Departure)
    ItemID           string         // "" = PriceList.Settings.PrimaryItems[ContractType]
    Vessel           *Vessel
    Resource         Resource
    Channel          Channel        // "" = desk
    CustomerCategory string
    PromoCode        string
    QuotedOn         calendar.Date  // "today" in the marina timezone, explicit
    Services         []ServiceRequest
    Deposit          *DepositPolicy // overrides the list policy
}

type Block struct {
    From, To    calendar.Date   // To exclusive; may exceed Departure (best_combination overshoot)
    Unit        TimeUnit
    Nights      int             // nights of the stay covered by this block
    Period      string          // period of the first night ("" = none)
    RuleID      string
    Base        money.Cents
    Modifiers   []Applied
    Amount      money.Cents     // base + modifiers, >= 0
}
type Applied struct{ RuleID string; Delta money.Cents }

type Line struct {
    Seq              int
    Kind             string        // berth, service, included
    ItemID, ItemCode string
    ItemType         ItemType
    RevenueClass     string
    ExternalCode     string
    Qty              qty.Milli
    Blocks           []Block       // berth lines; services have one block
    Base             money.Cents   // Σ block base
    ModifierTotal    money.Cents
    Discounts        []Applied
    Amount           money.Cents   // after discounts, in the item's price terms (net or gross)
    RoundingShare    money.Cents
    Gross, Net, VAT  money.Cents   // Gross = Net + VAT
    VATRateBP        int64
    VATNature        string
    PriceIncludesVAT bool
    ServiceFrom      calendar.Date
    ServiceTo        calendar.Date // last night (inclusive)
}

type VATBucket struct{ RateBP int64; Nature string; Net, VAT, Gross money.Cents }
type TraceStep struct {
    Code   string            // trace.go constants
    Line   int               // -1 = whole quote
    Block  int               // -1 = none
    RuleID string
    Params map[string]string // integers as decimal strings; no PII
}

type Breakdown struct {
    EngineVersion  int
    PriceListID    string
    CatalogHash    string        // sha256 of the canonical catalog (hash.go)
    ContractType   ContractType
    Nights         int
    Lines          []Line
    Subtotal       money.Cents   // Σ line amount before discounts (Base + ModifierTotal)
    DiscountTotal  money.Cents
    RoundingTotal  money.Cents   // signed
    Total          money.Cents   // Σ Gross
    Net, VAT       money.Cents
    VATSummary     []VATBucket   // sorted by RateBP desc, then Nature
    Deposit        money.Cents
    Trace          []TraceStep
    Warnings       []string      // e.g. stay_exceeds_validity
}

type Snapshot struct { // stored by M3/M4 as price_snapshot; INV-20
    EngineVersion int
    Input         Input
    Catalog       Catalog
    Breakdown     Breakdown
}

// Entry points (api.go)
func Quote(in Input, cat Catalog) (Breakdown, error)
func NewSnapshot(in Input, cat Catalog) (Snapshot, error)       // Quote + copies
func Recompute(s Snapshot) (Breakdown, error)                   // Quote(s.Input, s.Catalog); INV-20
func Validate(cat Catalog) []Issue                              // Issue{Code, Severity: error|warning, Ref, Params}
func CatalogHash(cat Catalog) string
func Matrix(cat Catalog, spec MatrixSpec) (MatrixResult, error)
func Coverage(cat Catalog, spec CoverageSpec) CoverageReport
func Renew(cat Catalog, spec RenewalSpec) (Catalog, error)      // new draft list content (ids cleared)
func Diff(before, after Catalog, spec MatrixSpec) (MatrixDiff, error)

type MatrixSpec struct {
    ContractType     ContractType
    ItemID           string        // "" = primary item
    Rows             string        // bands (default), categories
    Categories       []CoverageCategory // rows = categories
    Channel          Channel
    CustomerCategory string
    QuotedOn         calendar.Date
}
type MatrixResult struct {
    ContractType ContractType
    ItemID       string
    Rows         []MatrixRow    // {Key, Kind: band|category, ProbeLOA, ProbeBeam}
    Columns      []MatrixColumn // {Key: period code | "year" | "season", Arrival, Departure}
    Cells        []MatrixCell   // {Row, Col int; Probe Input; Breakdown *Breakdown; Error string}
}
type CoverageCategory struct{ Code string; LOA, Beam, BerthLength, BerthWidth geo.Cm }
type CoverageSpec struct{ Categories []CoverageCategory; QuotedOn calendar.Date }
type CoverageReport struct{ Gaps []Gap /* {ContractType, Category, Band, Period, ItemID, Reason} */ }
type RenewalSpec struct {
    ShiftYears     int            // 0..5; 0 = copy
    ISTATBP        int64          // 0..10000, applied first to every adjustable amount
    Adjustments    []Adjustment   // {Scope: all|item|category, ItemID, Category, PercentBP *int64, AmountCents *money.Cents}
    AmountRounding AmountRounding
    Code           string
    ValidFrom, ValidTo *calendar.Date // overrides after the shift
}
```

Errors (`errors.go`): `ErrNotImplemented` (CONTRACT stub only), `ErrNoPrice` (typed `*NoPriceError{ItemID,
ContractType, Night, Unit, Category, Band, Reason}` with `Reason` ∈ `no_rule`, `no_band`, `no_amount`,
`outside_validity`, `no_primary_item`, `unit_missing`), `ErrInvalidInput` (typed `*InputError{Field, Code}`:
`dimensions_required`, `departure_before_arrival`, `too_many_nights` (> 3 660), `unknown_item`,
`contract_type_not_served`, `invalid_channel`), `ErrPromoNotApplicable`, `ErrOverflow`.

### 2.3 List settings defaults

`dimension_source = vessel`; `length_rounding = {step_cm: 10, mode: up}` (to the decimetre, spec 12.3);
`min_beam_ratio = 1/3`; `split = {"*": pro_rata_daily}`; `rounding = {"*": none}`; `deposits = {}` (no deposit);
`season_window` required when `seasonal` ∈ `contract_types` (validation error otherwise).

### 2.4 Engine semantics (normative)

Notation: `N` = nights of the stay = `Arrival.DaysUntil(Departure)` (1..3660). `half_even(a/b)` =
`money.DivRoundHalfEven`. "Night d" = the date the night starts.

**0. Preconditions.** `Departure > Arrival`, `N ≤ 3660`, `ContractType ∈ PriceList.ContractTypes`, channel valid,
item resolved (`ItemID` or primary item, else `ErrNoPrice{Reason: no_primary_item}`), `Arrival` within
`[ValidFrom, ValidTo]` (else `ErrNoPrice{outside_validity}`). The list valid on the arrival night prices the whole
stay (§12 D4); nights after `ValidTo` are priced with the same list and produce warning `stay_exceeds_validity`.

**1. Period of a night.** The `event` period whose range contains the night, else the `season` period whose
range contains it, else none (`""`). Validation guarantees seasons never overlap each other and events never
overlap each other.

**2. Rule matching** (for an item, a time unit, a block starting at night `d`): a rule matches when it is active,
`Stage = base`, `ItemID` equals the item, `Unit` equals the unit, every non-empty scope list contains the input
value (`Periods` contains the period of `d`; `Categories` contains `Resource.Category`; `GroupIDs` intersects
`Resource.GroupIDs`; an empty input value never matches a non-empty list), and the rule **can price the input**:
`length_band` needs a band for the length (or `AmountCents`), and an amount for that band in `Amounts` (or
`AmountCents` as the default for all bands); `per_berth` needs `Amounts[category]` (or `AmountCents`);
`per_meter`/`per_m2` need the vessel (or berth per `dimension_source`); `per_m2_berth` needs berth length and
width. Winner: highest `Priority`; then highest **specificity** = number of non-empty scope lists (0..6); then
highest `Seq`; then lowest `ID` (string compare). Input order of rules never matters.

**3. Dimensions.** `L`, `B` used by the bases:
- `vessel`: `L = Vessel.LOA`, `B = Vessel.Beam`; `berth`: `L = Resource.Length`, `B = Resource.Width`
  (`dimensions_required` when missing); `max`: `L = max(LOA, Length)`, `B = max(Beam, Width)` (with no resource
  it is `vessel`; with no vessel it is `berth`).
- Length rounding (only for `per_meter`, `per_m2`, `per_m2_berth`, never for band selection): `up` =
  ceil to the next multiple of `step_cm`; `half_even` = nearest multiple, ties to the even multiple.
  Example: 1 234 cm, step 10, up → 1 240; half_even → 1 230; 1 235 half_even → 1 240 (124 even);
  1 225 → 1 220.
- `per_m2` minimum beam: `B' = max(B, half_even(L × Num / Den))` computed on the rounded `L`, then `B'` is rounded
  with the same length rounding.

**4. Base amount of one unit** (per rule basis):

| Basis | Unit amount |
|---|---|
| `per_berth` | `Amounts[Resource.Category]`, else `AmountCents` |
| `length_band` | band = first band with `LOA_or_L ≤ MaxCm` (raw `L`, inclusive; open band catches the rest); `Amounts[band.Code]`, else `AmountCents` |
| `per_meter` | `half_even(UnitCents × L / 100)` |
| `per_m2` | `half_even(UnitCents × L × B' / 10 000)` (128-bit `MulRat`) |
| `per_m2_berth` | `half_even(UnitCents × Length × Width / 10 000)` with berth dims rounded by the length rounding, ignoring `dimension_source` |
| `flat` | `AmountCents` |
| `metered` | `Qty.MulCents(UnitCents)` (services only; quantity from the request) |

**5. Blocks of the berth line** (reference unit by contract type, §12 D5: `annual → year`, `seasonal → season`,
`monthly → month`, `transit → night`, `storage → month`, `charter → season`).
1. **Exact match:** if the stay is exactly `k ≥ 1` consecutive reference units — `year`: `Departure =
   AddYears(Arrival, k)`; `season`: `[Arrival, Departure) = SeasonWindow` (k = 1); `month`: `Departure =
   AddMonths(Arrival, k)`; `night`: always (k = N), **except when the contract type's split rule is
   `best_combination`**: then a transit never takes this exact path and goes to (2), so week and month rules can
   price it (spec 12.3, §12 D5 and D31) — the line is `k` blocks of the reference unit, each priced by
   the winning rule at its first night. A missing rule for a block is `ErrNoPrice{unit_missing}`.
2. Otherwise the list's split rule for the contract type applies:
   - **`pro_rata_daily`**: full reference units from the arrival as in (1), then one remainder block of `r`
     nights priced `half_even(U × r / D)`, where `U` is the unit amount of the winning reference-unit rule at the
     remainder's first night and `D` the length in nights of the full unit starting there (`year`: 365 or 366;
     `month`: days of that calendar month block; `season`: nights of the window; `night`: 1). Trace
     `split.pro_rata`.
   - **`whole_months`**: `m = ceil` number of calendar-month blocks from the arrival covering the stay; each block
     priced by the winning `month` rule at its first night; the last block may extend past `Departure`. Exact
     year/season matches of (1) still win. Missing `month` rule → `ErrNoPrice{unit_missing}`.
   - **`best_combination`**: blocks of `night` (1), `week` (7) and `month` (calendar month from the block's start)
     — only units with a matching rule at that night — chosen by dynamic programming over night index `j`
     (0..N): `dp[0] = 0`; from each reachable `j`, a block of unit `u` starting at night `j` reaches
     `min(N, j + len)` at cost `dp[j] + amount(u, j)` where `amount` includes the block's modifiers (step 6).
     Blocks may overshoot the departure. Ties: lower cost, then fewer blocks, then the path whose first
     differing block has the longer unit. No path → `ErrNoPrice{no_rule}`.
3. Each block records `From`, `To` (exclusive; `AddMonths` clamps to the month end: Jan 31 + 1 month = Feb 28/29,
   §12 D6), `Nights` covered within the stay, `Period` of its first night, `RuleID`, `Base`.

**6. Modifiers** (per block, on `Base` of that block). A modifier rule matches the block when active, `Stage =
modifier`, it targets the line's item (`TargetItemIDs` contains it, or `TargetItemTypes` contains its type,
default `[berth_fee]`), its scope matches (as in 2, with the block's first-night period) and every present
condition holds: `HullTypes` contains `Vessel.HullType`; `OverhangMinCm`: `Vessel.LOA − Resource.Length ≥ value`
(false without vessel or resource length); `MinNights`/`MaxNights` on `N`; `LeadMinDays`/`LeadMaxDays` on
`QuotedOn.DaysUntil(Arrival)`. The delta of a modifier is `half_even(Base × PercentBP / 10 000)` or `AmountCents`
(per block). **All stackable** matching modifiers apply (deltas summed, each computed on `Base`: additive, not
compounded). **Among non-stackable** ones exactly one applies: the one with the lowest delta (most favourable to
the customer); ties → priority, specificity, seq, id as in 2. `Amount = max(0, Base + Σ deltas)`.

**7. Services.**
- *Included*: each `Includes` entry of the winning base rule of the **first** block becomes an `included` line
  with the item's data, `Qty`, amount 0, no VAT (trace `service.included`).
- *Added*: each `ServiceRequest` is a `service` line priced with the item's winning base rule at the arrival
  night (step 2; unit `stay` or `night`). Amount = unit amount × `Qty` (for `metered`, the quantity is already in
  the amount; for others `Qty.MulCents`), × `N` when the rule unit is `night`. Modifiers apply to services as in 6
  (one block: arrival night). Unknown or inactive item → `ErrInvalidInput{unknown_item}`.

**8. Discounts** (on the quote, in three groups, in order **customer → promo → automatic**). A discount rule
belongs to the group of its `DiscountKind`; it matches when active, its scope (period = arrival night) and
conditions hold, and for `customer` its `Scope.CustomerCategories` is non-empty and contains the input category;
for `promo` its `PromoCode` equals `upper(Input.PromoCode)`. Eligible amount `E` of a rule = Σ current `Amount` of
the lines it targets (default `[berth_fee]`). Per group: stackable rules all apply, plus the non-stackable rule
with the largest reduction; each reduction is `half_even(E × PercentBP / 10 000)` or `min(AmountCents, E)`,
computed on the amounts at the **start of the group**; the group total is capped at the eligible amounts. The
reduction of each rule is spread over its eligible lines with `money.Allocate` weighted by the current line
amounts (zero lines excluded), so no line goes negative. A non-empty `Input.PromoCode` with no matching promo
rule (or none with eligible amount) → `ErrPromoNotApplicable`.

**9. VAT of a line** (from `Amount`): `PriceIncludesVAT` → `Gross₀ = Amount`; else `Gross₀ = Amount +
half_even(Amount × VATRateBP / 10 000)`.

**10. Total rounding** (contract type's `TotalRounding`): `T₀ = Σ Gross₀`; `none` → `T = T₀`; `euro` /
`five_euro` → `T` = nearest multiple of 100 / 500 cents, ties to the even multiple (e.g. 12 350 → 12 400 euro,
12 250 → 12 200; five_euro 1 250 → 1 000, 1 750 → 2 000). `delta = T − T₀` is spread over lines with
`Gross₀ > 0` by `money.Allocate` weighted by `Gross₀` (`RoundingShare`). `Gross = Gross₀ + RoundingShare`.

**11. Net/VAT split:** for every line `Net, VAT = money.VATSplit(Gross, VATRateBP)` (rate 0 → `VAT = 0`). For
VAT-excluded lines without rounding share this returns `Net = Amount` exactly (property test). `VATSummary` sums
lines by `(RateBP, Nature)`. `Total = Σ Gross = Σ buckets.Gross`, `Net + VAT = Total` always.

**12. Deposit:** `Input.Deposit` else `Settings.Deposits[ContractType]`; `PercentBP` → `half_even(Total × bp /
10 000)`; `AmountCents` → `min(AmountCents, Total)`; none → 0.

**13. Determinism and hashing.** No maps are iterated without sorting; lines are ordered berth line, included
lines (in `Includes` order), added services (request order); trace in execution order. `CatalogHash` = sha256 of
the canonical JSON of the catalog with rules sorted by `(Stage, Seq, ID)`, periods by code, bands by order,
items by code. `Recompute(NewSnapshot(in, cat))` equals the original breakdown byte-for-byte after JSON
marshalling (INV-20).

**14. Limits** (validation and `ErrOverflow`): amounts and unit cents ≤ 100 000 000 ¢; |percent| ≤ 100 000 bp
(modifiers), 1..10 000 bp (discounts); lengths ≤ 20 000 cm; quantities ≤ 1 000 000.000; rules ≤ 2 000 per list;
periods ≤ 50; bands ≤ 60.

### 2.5 Matrix, coverage, renewal — same engine by construction

- `Matrix` never computes a price itself: for each (row, column) it builds a **probe** `Input` and calls `Quote`.
  The cell stores the probe and the resulting breakdown (or the error code). The service returns the probe in the
  API shape (`PricingSimulateRequest`, explicit dimensions, explicit `price_list_id`, `quoted_on`), and
  `simulatePrice` maps a request to `pricing.Input` with the same function used for probes
  (`pricelist.inputFromRequest`), so a simulate call with the probe reaches the identical `Quote` call.
- Probes:
  - rows `bands`: `LOA = MaxCm` (open band: previous `MaxCm + 1`, or 100 cm for a single open band), `Beam =
    half_even(LOA × 1/3)` (or the list's `MinBeamRatio`), `HullType = monohull_motor`, `Resource.Category =
    band.Category`, no resource length/width (so `dimension_source = berth` lists use category rows);
  - rows `categories`: the `CoverageCategory` dims passed by the service (§5.3 of the service);
  - columns: `transit` → one column per period (seasons then events, by code), probe = 1 night at the first night
    of the period's first range inside validity; `monthly`, `storage` → one column per period, probe = 1 calendar
    month from that night; `annual` → one column `year`, probe `[ValidFrom, AddYears(ValidFrom, 1))`; `seasonal`
    and `charter` → one column `season`, probe = the season window;
  - `Channel`, `CustomerCategory`, `QuotedOn` from the spec (defaults desk, none, today from the service).
- `Coverage` runs the same probes for every contract type of the list × categories (and bands) × columns and
  reports each `ErrNoPrice` as a gap with its reason.
- `Renew` copies periods, bands, settings and rules; shifts every date by `ShiftYears` (`AddYears`, Feb 29 →
  Feb 28); applies to **base-rule amounts only** (`AmountCents`, `UnitCents`, every `Amounts` value; modifier and
  discount effects are copied unchanged, §12 D13): first ISTAT `half_even(a × (10 000 + ISTATBP) / 10 000)`, then
  the most specific adjustment (item, else category — for `Amounts` keyed by category or bands linked to that
  category, else all): percent `half_even(a × (10 000 + bp) / 10 000)` or fixed `a + AmountCents` (never below 0),
  then `AmountRounding` (`ten_cents`/`euro`: nearest multiple, ties even). `Seq` values are preserved, ids cleared.
- `Diff(before, after)` = `Matrix` on both (probes of `after` use shifted dates) → per cell old/new total, delta
  cents and delta bp (`half_even(delta × 10 000 / old)`, null when old is 0).

### 2.6 Tests in `internal/domain/pricing` (names fixed; referenced by `docs/invariants.md`)

- `TestQuote_Corpus` — runs every case of `testdata/pricing/cases/*.json` as a subtest named
  `<file>/<case name>`; compares every field present in `expect`.
- `TestCorpus_AtLeast200` — fails when the corpus has fewer than 200 cases or a quota of §2.7 is not met, or a case
  lacks `why`.
- `TestCorpus_NamesUnique`, `TestCorpus_CatalogsValid` (every fixture passes `Validate` with no errors, except
  fixtures named `invalid-*`).
- `TestQuote_SnapshotReproducible` (rapid; INV-20): random catalog + input → `Recompute(NewSnapshot)` equal after
  JSON round trip; `EngineVersion` recorded.
- `TestRapid_SplitStayAdditive`: catalogs with only `night` rules, no stay-dependent conditions
  (`MinNights`/`MaxNights`/lead), no discounts, no `stay` services, rounding `none`, split `pro_rata_daily`:
  `Quote([a,c)).Total = Quote([a,b)).Total + Quote([b,c)).Total` for every `a < b < c` (spec 12.3 "per notte").
- `TestRapid_NoNegativeAmounts`: every block, line, gross, net, VAT, total and deposit ≥ 0, for any rules including
  −100 % modifiers and oversized amount discounts.
- `TestRapid_VATSumsExactly`: `Σ line.Net + Σ line.VAT = Total`, `Σ buckets = Total`, `Net + VAT = Gross` per line
  (INV-25).
- `TestRapid_ExcludedVATNetEqualsAmount`: rounding none ⇒ for VAT-excluded lines `Net = Amount`.
- `TestRapid_RuleOrderIndependent`: shuffling `Rules`, `Items`, `Periods` gives the same breakdown.
- `TestRapid_RoundingWithinHalfStep`: `|RoundingTotal| ≤ step/2` and `Total mod step = 0` for euro/five_euro.
- `TestRapid_BestCombinationNotWorse`: with the same rules, `best_combination` total ≤ the all-nights total and ≤
  `whole_months` total (when those exist).
- `TestRapid_LengthMonotone`: `per_meter`/`length_band` with non-decreasing band amounts and no modifiers: a
  longer vessel never pays less.
- `TestRapid_DepositAtMostTotal`.
- `TestMatrix_EqualsQuote` (rapid; INV-23): for random catalogs every cell's breakdown equals `Quote(cell.Probe,
  cat)`; also run on every corpus fixture.
- `TestRenew_ZeroAdjustmentShiftsOnly`, `TestRenew_Scenario` (ISTAT then item percent, category fixed, rounding),
  `TestRenew_LeapDay`.
- `TestValidate_Scenario` (one row per issue code of §4.8, both directions).
- `TestAddMonths_Clamp`, `TestLengthRounding_Table`, `TestBestCombination_TieBreak`.
- `FuzzQuote` (no panic; errors only from the documented set).
- `purity_test.go` (parses imports: no `store`, `clock`, `os`, `net`, `math/rand`, `crypto/rand`, no `time.Now`).
- Benchmark `BenchmarkQuote_Transit30Nights` (< 1 ms/op) and `BenchmarkMatrix_8x5` (< 20 ms/op).

### 2.7 The ≥ 200 hand-derived price cases (`testdata/pricing/`, PRICING-ENGINE)

```
testdata/pricing/
  README.md                  how to add a case; the rule: expectations are computed by hand, never by running the engine
  catalogs/<name>.json       pricing.Catalog fixtures (engine JSON), e.g. demo-like.json, bands-basic.json,
                             per-meter.json, per-m2.json, vat-mix.json, events.json, splits.json, discounts.json,
                             invalid-overlapping-seasons.json
  cases/<NN>-<group>.json    {"cases": [Case, ...]}
```

`Case`:
```json
{
  "name": "transit_per_meter_high_season_rounded_up_to_decimetre",
  "spec": "12.3 per_meter",
  "catalog": "per-meter",
  "patch": {"settings": {"length_rounding": {"step_cm": 10, "mode": "up"}}},
  "input": {"contract_type": "transit", "arrival": "2027-07-10", "departure": "2027-07-12",
            "vessel": {"loa": 1234, "beam": 410, "hull_type": "monohull_sail"}, "quoted_on": "2027-06-01"},
  "expect": {"total": 18600, "net": 15246, "vat": 3354, "deposit": 0,
             "lines": [{"item_code": "TRN-NTT", "amount": 18600, "gross": 18600,
                        "blocks": [{"from": "2027-07-10", "base": 9300}, {"from": "2027-07-11", "base": 9300}]}]},
  "why": "L 1234 → 1240 (step 10, up); 750 ¢/m × 1240 / 100 = 9300 per night × 2 = 18600 gross (VAT incl.); VATSplit(18600, 2200): net = half_even(18600 × 10000 / 12200) = 15246, VAT 3354"
}
```

- `patch` is an optional JSON merge patch (RFC 7386) applied to the fixture, so a group can vary one setting.
- `expect` fields are optional except `total` (or `error`: `"no_price:<reason>"`, `"invalid_input:<code>"`,
  `"promo_not_applicable"`). Every listed field is compared exactly; `lines` compares listed lines by index and
  their listed fields; `trace_codes` (optional) must appear in order as a subsequence.
- `why` is mandatory: the hand derivation in integer cents with each rounding written out.

Quotas (checked by `TestCorpus_AtLeast200`; group = file prefix):

| File | Group | Min cases |
|---|---|---|
| `01-per-berth.json` | per_berth amounts, default amount, missing category | 12 |
| `02-length-band.json` | band boundaries (1200 vs 1201), open band, no band, band default amount, dimension sources | 20 |
| `03-per-meter.json` | rounding step 1/10/50/100, up vs half-even, ties | 20 |
| `04-per-m2.json` | min beam ratio on/off, rounding of B', large boats | 14 |
| `05-per-m2-berth.json` | berth dims, missing berth dims | 8 |
| `06-flat-metered.json` | flat, metered with milli quantities | 14 |
| `07-dimension-source.json` | vessel, berth, max, missing dims | 10 |
| `08-units-exact.json` | exact year/season/month/night, leap years, month-end clamping | 10 |
| `09-pro-rata.json` | remainders of year/month/season, 365 vs 366 | 12 |
| `10-whole-months.json` | partial months, overshoot | 10 |
| `11-best-combination.json` | nights vs week vs month, overshoot cheaper, tie-breaks, periods crossing | 14 |
| `12-periods-selection.json` | seasons, events overriding, no period, priority/specificity/seq/id, `no_rule`, outside validity | 20 |
| `13-modifiers.json` | multihull, overhang, season, long stay, last minute, early booking, channel; stackable sum; non-stackable choice; clamp at 0 | 24 |
| `14-services.json` | included, added flat/night/metered, unknown item | 10 |
| `15-discounts.json` | customer → promo → automatic order, caps, allocation across lines, promo not applicable | 16 |
| `16-rounding.json` | none, euro, five_euro, ties, allocation across lines and VAT rates | 10 |
| `17-deposit.json` | percent, fixed capped, override, none | 8 |
| `18-vat.json` | 22/10/5/4 %, natures N1/N4/N2.2, incl vs excl, mixed rates | 14 |
| **Total** | | **246** |

Builders compute expectations by hand (a spreadsheet is fine, but the `why` text must show integer steps). When
the engine disagrees with a case, the case is re-derived by hand from §2.4 before touching either.

---

## 3. Data model

### 3.1 New migration (both dialects, same number)

| File | Content |
|---|---|
| `internal/migrations/{sqlite,postgres}/00004_pricing.sql` | `pricing_settings`, `revenue_classes`, `items`, `berth_categories`, `price_lists`, `rate_periods`, `price_list_bands`, `price_rules`, immutability triggers |

`00001`–`00003` are never edited. Conventions as `docs/m1-plan.md` §3.1 and `.claude/rules/migrations.md`: ids
`TEXT`/`uuid` (UUIDv7), dates `DATE` text/`date`, JSON `TEXT`/`jsonb`, booleans `INTEGER CHECK (x IN (0,1))` /
`boolean`, money `INTEGER`/`bigint` cents, every child row carries `marina_id` and a composite FK `(parent_id,
marina_id)`. The DDL below is authoritative in SQLite form; PostgreSQL is its mechanical translation (triggers in
plpgsql). Down drops triggers then tables in reverse order.

```sql
-- 00004_pricing.sql
-- +goose Up
CREATE TABLE pricing_settings (
    id                          TEXT PRIMARY KEY,
    marina_id                   TEXT NOT NULL UNIQUE REFERENCES marinas (id) ON DELETE CASCADE,
    default_vat_rate_bp         INTEGER NOT NULL DEFAULT 2200 CHECK (default_vat_rate_bp BETWEEN 0 AND 10000),
    default_price_includes_vat  INTEGER NOT NULL DEFAULT 0 CHECK (default_price_includes_vat IN (0,1)),
    activation_seq              INTEGER NOT NULL DEFAULT 0,   -- bumped by every activation: serialises activations (INV-24)
    version                     INTEGER NOT NULL DEFAULT 1,
    created_at                  TIMESTAMP NOT NULL,
    updated_at                  TIMESTAMP NOT NULL
);

CREATE TABLE revenue_classes (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL,                    -- I18nText
    parent_id   TEXT,                             -- subcategory of; one level only (service check)
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMP,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (parent_id, marina_id) REFERENCES revenue_classes (id, marina_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX revenue_classes_marina_code_uidx ON revenue_classes (marina_id, code);

CREATE TABLE items (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32 AND code = upper(code)),
    name                TEXT NOT NULL,            -- I18nText
    description         TEXT NOT NULL DEFAULT '{}',
    item_type           TEXT NOT NULL CHECK (item_type IN ('berth_fee','service','utility','product','labour','material','fee','deposit')),
    unit                TEXT NOT NULL CHECK (unit IN ('night','month','season','year','m','m2','kwh','m3','hour','piece','lump')),
    vat_rate_bp         INTEGER NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    vat_nature          TEXT CHECK (vat_nature IS NULL OR vat_nature IN
                          ('N1','N2.1','N2.2','N3.1','N3.2','N3.3','N3.4','N3.5','N3.6','N4','N5',
                           'N6.1','N6.2','N6.3','N6.4','N6.5','N6.6','N6.7','N6.8','N6.9','N7')),
    price_includes_vat  INTEGER NOT NULL DEFAULT 0 CHECK (price_includes_vat IN (0,1)),
    revenue_class_id    TEXT NOT NULL,
    external_code       TEXT NOT NULL DEFAULT '' CHECK (length(external_code) <= 64),
    active              INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    online_visible      INTEGER NOT NULL DEFAULT 0 CHECK (online_visible IN (0,1)),
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (revenue_class_id, marina_id) REFERENCES revenue_classes (id, marina_id),
    CHECK ((vat_rate_bp = 0) = (vat_nature IS NOT NULL))
);
CREATE UNIQUE INDEX items_marina_code_uidx ON items (marina_id, code);
CREATE INDEX items_marina_type_idx ON items (marina_id, item_type, active, sort_order);

CREATE TABLE berth_categories (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL,                    -- I18nText
    applies_to  TEXT NOT NULL DEFAULT '["berth"]',-- subset of berth, dynamic_area, land_resource (service check)
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMP,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX berth_categories_marina_code_uidx ON berth_categories (marina_id, code);

CREATE TABLE price_lists (
    id                     TEXT PRIMARY KEY,
    marina_id              TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code                   TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name                   TEXT NOT NULL,         -- I18nText
    status                 TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    valid_from             DATE NOT NULL,
    valid_to               DATE NOT NULL,
    contract_types         TEXT NOT NULL DEFAULT '[]',  -- JSON array of pricing.ContractType
    settings               TEXT NOT NULL DEFAULT '{}',  -- pricing.ListSettings JSON (+ publish_matrix flag)
    source_price_list_id   TEXT,                  -- renewal/copy lineage, same marina
    content_sha256         TEXT,                  -- pricing.CatalogHash at activation
    activated_at           TIMESTAMP,
    activated_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    archived_at            TIMESTAMP,
    next_rule_seq          INTEGER NOT NULL DEFAULT 1,
    version                INTEGER NOT NULL DEFAULT 1,
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (source_price_list_id, marina_id) REFERENCES price_lists (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CHECK (valid_to >= valid_from),
    CHECK (status = 'draft' OR content_sha256 IS NOT NULL)
);
CREATE UNIQUE INDEX price_lists_marina_code_uidx ON price_lists (marina_id, code);
CREATE INDEX price_lists_marina_status_idx ON price_lists (marina_id, status, valid_from);

CREATE TABLE rate_periods (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    price_list_id  TEXT NOT NULL,
    code           TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name           TEXT NOT NULL,                 -- I18nText
    kind           TEXT NOT NULL CHECK (kind IN ('season','event')),
    color          TEXT NOT NULL DEFAULT 'chart-1' CHECK (color IN ('chart-1','chart-2','chart-3','chart-4','chart-5')),
    ranges         TEXT NOT NULL,                 -- JSON [{from, to}] inclusive nights
    sort_order     INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX rate_periods_list_code_uidx ON rate_periods (price_list_id, code);

CREATE TABLE price_list_bands (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    price_list_id  TEXT NOT NULL,
    code           TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    max_cm         INTEGER CHECK (max_cm IS NULL OR max_cm BETWEEN 1 AND 20000),
    category_code  TEXT,
    sort_order     INTEGER NOT NULL,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX price_list_bands_code_uidx ON price_list_bands (price_list_id, code);
CREATE UNIQUE INDEX price_list_bands_order_uidx ON price_list_bands (price_list_id, sort_order);

CREATE TABLE price_rules (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    price_list_id  TEXT NOT NULL,
    seq            INTEGER NOT NULL,              -- recency (12.4 step 1)
    stage          TEXT NOT NULL CHECK (stage IN ('base','modifier','discount')),
    name           TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    item_id        TEXT,                          -- base rules: required (CHECK); FK for in_use checks
    priority       INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN -1000 AND 1000),
    stackable      INTEGER NOT NULL DEFAULT 0 CHECK (stackable IN (0,1)),
    active         INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    body           TEXT NOT NULL,                 -- JSON: basis, unit, amounts, includes, targets, conditions, effect, discount_kind, promo_code, scope
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id),
    CHECK (stage <> 'base' OR item_id IS NOT NULL)
);
CREATE UNIQUE INDEX price_rules_list_seq_uidx ON price_rules (price_list_id, seq);
CREATE INDEX price_rules_list_stage_idx ON price_rules (price_list_id, stage);
CREATE INDEX price_rules_item_idx ON price_rules (marina_id, item_id);
```

Items referenced only inside `body` (targets, includes, primary items in `settings`) are checked by the service for
`in_use`; `price_rules.item_id` gives the DB-level guarantee for base rules.

```sql
-- Item snapshot of a published list (INV-26, §12 D32): the engine fields of every item the list references,
-- written by the activation while the list is still a draft, frozen by the INV-22 triggers.
CREATE TABLE price_list_items (
    id, marina_id, price_list_id, item_id, code, item_type, unit, vat_rate_bp, vat_nature, price_includes_vat,
    revenue_class /* class code */, external_code, active, created_at,
    FOREIGN KEY (price_list_id, marina_id) REFERENCES price_lists (id, marina_id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (item_id, marina_id) REFERENCES items (id, marina_id) DEFERRABLE INITIALLY DEFERRED
);
-- price_list_items_list_item_uidx (price_list_id, item_id), price_list_items_marina_idx (marina_id, price_list_id),
-- price_list_items_item_idx (marina_id, item_id)
```

### 3.2 Immutability of published price lists (INV-22)

Triggers on both dialects (SQLite `BEFORE … WHEN … RAISE(ABORT, 'price_list_immutable')`; PostgreSQL plpgsql
`RAISE EXCEPTION 'price_list_immutable' USING ERRCODE = 'P0001'`):

1. `price_lists` BEFORE UPDATE when `OLD.status <> 'draft'`: abort unless every column except `status`,
   `valid_to`, `archived_at`, `version`, `updated_at` is unchanged **and** `NEW.status` ∈ {`OLD.status`,
   `archived`} (never back to `draft`; `archived` never back to `active`) **and** `NEW.valid_to ≤ OLD.valid_to`
   (only truncation by a successor's activation).
2. `rate_periods`, `price_list_bands`, `price_rules`, `price_list_items` BEFORE INSERT / UPDATE / DELETE: abort when the parent
   `price_lists` row exists and its status is not `draft`. (A cascade from a deleted parent finds no parent row and
   passes; CONTRACT proves it with `TestMigrations_PriceListCascade` on both dialects.)
3. `price_lists` BEFORE DELETE when `OLD.status <> 'draft'`: abort. (Marinas are archived, never deleted; tests
   drop whole databases.)

The store maps the trigger error to `store.ErrImmutable`; the service checks status first and returns
`pricelist.ErrImmutable` → 409 `price_list_immutable`, so the trigger is the backstop, not the UX.

**Backup/restore** (`internal/backup`, CONTRACT): table order `pricing_settings, revenue_classes (two-pass:
parent_id NULL first, then update), items, berth_categories, price_lists (inserted with status forced to
'draft', content_sha256 kept), rate_periods, price_list_bands, price_rules, price_list_items`, then a final pass sets each list's
real `status`, `activated_at`, `archived_at`. `TestBackup_RoundTripSQLiteToPostgres` covers an active and an
archived list.

### 3.3 Versioning

- Every configuration row has `version` for `If-Match` (items, revenue classes, categories, settings, lists).
- A price list is edited as one **document** (`PUT /price-lists/{id}`, `If-Match` on the list version), like the
  map document of M1 (m1-plan §11 D1): periods, bands and rules are replaced by the diff in one transaction; new
  rows carry a client `key`, saved ones their `id`; the response returns `id_map`. New rules get `seq =
  next_rule_seq++` in document order; edited rules get a new `seq` only when their body changes (recency = last
  change).
- Activation computes `content_sha256 = pricing.CatalogHash(...)` and stores it with the status change. Archived
  and active lists are immutable; reproducing an old price uses the snapshot (M3+) or the archived list.
- Activation serialisation: inside the activation transaction the service first runs `UPDATE pricing_settings SET
  activation_seq = activation_seq + 1 WHERE marina_id = ?` (row lock on PostgreSQL, single writer on SQLite), then
  reads the active lists, checks INV-24 and truncates predecessors. `TestConcurrency_PriceListActivation` proves it.

---

## 4. OpenAPI additions (`api/openapi.yaml`)

### 4.1 Conventions (in addition to m0-plan §4.1 and m1-plan §4.1)

- New tags: `pricing`, `price-lists`. `M = /api/admin/v1/marinas/{marinaId}`. New path params `ItemId`,
  `RevenueClassId`, `CategoryId`, `PriceListId` (uuid).
- No required query parameters on `{marinaId}` routes (the cross-marina sweep calls them bare); defaults apply.
- Money fields `*_cents` (int64), basis points `*_bp` (int64), lengths `*_cm`, quantities `*_milli` (int64), dates
  `YYYY-MM-DD`. No `number` type anywhere in these schemas (CI grep in `TestContract_NoFloats`, CONTRACT).
- `x-porto-max-body-bytes`: `savePriceList` and `validatePriceList` 4 MiB; others default.
- Schemas mirror §2.2 JSON: `PriceListSettings`, `RatePeriod`, `Band`, `PriceRule` (discriminated by `stage`),
  `Scope`, `Conditions`, `Effect`, `PricingSimulateRequest`, `PriceBreakdown`, `PriceMatrix`, `PriceCoverage`,
  `RenewalRequest`, `RenewalPreview`, `PriceListIssue`.

### 4.2 Permissions

Add to `roles.go`, the contract `Permission` enum, and the independent copy in `TestRoles_MatrixMatchesSpec`:
- `PermPricingConfigure = "pricing.configure"` — `owner`, `director` (spec 26.1 "configurazione … listini").
- `PermPricingRead = "pricing.read"` — every role except `yard` (`owner`, `director`, `office`, `accounting`,
  `dock`, `yard_mgr`, `rental`, `viewer`): office quotes, dock sells transits at the desk, accounting reads items
  and VAT, yard_mgr/rental price storage and rentals later, viewer reads reports (§12 D1). Both are valid API key
  scopes.

### 4.3 Catalog paths (handlers `internal/http/admin/catalog.go`, PRICING-SERVICES)

| Method & path | operationId | Perm | Request | 2xx | Errors |
|---|---|---|---|---|---|
| GET `M/pricing-settings` | getPricingSettings | pricing.read | – | 200 `PricingSettings{default_vat_rate_bp, default_price_includes_vat, version}` + ETag (row created lazily) | 404 |
| PUT `M/pricing-settings` | updatePricingSettings | pricing.configure | same + If-Match | 200 + ETag | 400, 403, 404, 412, 428 |
| GET `M/revenue-classes` | listRevenueClasses | pricing.read | query `include_archived?` | 200 `{items: RevenueClass[]}` (defaults inserted lazily, §6) | 404 |
| POST `M/revenue-classes` | createRevenueClass | pricing.configure | `{code, name, parent_id?, sort_order?}` + Idempotency-Key | 201 + ETag | 400, 403, 404, 409 conflict (code) |
| PATCH `M/revenue-classes/{revenueClassId}` | updateRevenueClass | pricing.configure | partial + If-Match (`archived: bool` archives) | 200 + ETag | 400, 403, 404, 409, 412, 428 |
| DELETE `M/revenue-classes/{revenueClassId}` | deleteRevenueClass | pricing.configure | If-Match | 204 | 403, 404, 409 in_use, 412, 428 |
| GET `M/items` | listItems | pricing.read | query `type?`, `active?`, `q?`, `cursor?`, `limit?` | 200 page of `Item` | 404 |
| POST `M/items` | createItem | pricing.configure | `ItemCreate{code, name, description?, item_type, unit, vat_rate_bp?, vat_nature?, price_includes_vat?, revenue_class_id, external_code?, active?, online_visible?, sort_order?}` + Idempotency-Key (defaults from settings) | 201 `Item` + ETag | 400, 403, 404, 409 conflict |
| GET `M/items/{itemId}` | getItem | pricing.read | – | 200 `Item{…, usage: {price_lists: int}}` + ETag | 404 |
| PATCH `M/items/{itemId}` | updateItem | pricing.configure | partial + If-Match (code immutable once used by an active list) | 200 + ETag | 400, 403, 404, 409, 412, 428 |
| DELETE `M/items/{itemId}` | deleteItem | pricing.configure | If-Match | 204 | 403, 404, 409 in_use, 412, 428 |
| GET `M/berth-categories` | listBerthCategories | pricing.read | query `include_archived?` | 200 `{items: BerthCategory[{…, usage: {berths, dynamic_areas, land_resources}}], unknown_in_use: [{code, count}]}` | 404 |
| POST `M/berth-categories` | createBerthCategory | pricing.configure | `{code, name, applies_to, sort_order?}` + Idempotency-Key | 201 + ETag | 400, 403, 404, 409 |
| PATCH `M/berth-categories/{categoryId}` | updateBerthCategory | pricing.configure | partial + If-Match (code immutable) | 200 + ETag | 400, 403, 404, 412, 428 |
| DELETE `M/berth-categories/{categoryId}` | deleteBerthCategory | pricing.configure | If-Match | 204 | 403, 404, 409 in_use (elements or rules use it), 412, 428 |
| POST `M/berth-categories/sync` | syncBerthCategories | pricing.configure | – (+ Idempotency-Key) | 200 `{created: BerthCategory[]}` from the M1 price-class rule bands and the codes in use | 403, 404 |
| GET `M/customer-categories` | listCustomerCategoryCounts | pricing.read | – | 200 `{items: [{code, count}] (enum order, zeros included), uncategorised}`; counts of non-archived customers only, no customer data | 404 |

### 4.4 Price list paths (handlers `internal/http/admin/pricelists.go`, PRICING-SERVICES)

| Method & path | operationId | Perm | Request | 2xx | Errors |
|---|---|---|---|---|---|
| GET `M/price-lists` | listPriceLists | pricing.read | query `status?`, `contract_type?`, `on?` (date) | 200 `{items: PriceListSummary[{id, code, name, status, valid_from, valid_to, contract_types, rule_count, version, source_price_list_id}]}` | 404 |
| POST `M/price-lists` | createPriceList | pricing.configure | `PriceListCreate{code, name, valid_from, valid_to, contract_types}` + Idempotency-Key | 201 `PriceListDocument` + ETag | 400, 403, 404, 409 conflict |
| GET `M/price-lists/{priceListId}` | getPriceList | pricing.read | – | 200 `PriceListDocument{id, code, name, status, valid_from, valid_to, contract_types, settings, periods[], bands[], rules[], content_sha256?, activated_at?, version, issues[]}` + ETag | 404 |
| PUT `M/price-lists/{priceListId}` | savePriceList | pricing.configure | `PriceListSave{code, name, valid_from, valid_to, contract_types, settings, periods[{id?\|key, …}], bands[…], rules[…]}` + If-Match | 200 `{document, id_map, issues[]}` + ETag (saved even with validation errors: drafts may be incomplete) | 400 validation_failed (schema/refs), 403, 404, 409 price_list_immutable, 412, 428 |
| POST `M/price-lists/{priceListId}/validate` | validatePriceList | pricing.read | optional `PriceListSave` (dry run; empty = saved document) | 200 `{issues: PriceListIssue[]}` | 400, 403, 404 |
| DELETE `M/price-lists/{priceListId}` | deletePriceList | pricing.configure | If-Match | 204 | 403, 404, 409 price_list_immutable, 412, 428 |
| POST `M/price-lists/{priceListId}/activate` | activatePriceList | pricing.configure | `{effective_from?: date}` + If-Match | 200 `{document, truncated: [{id, code, valid_to}]}` + ETag | 400, 403, 404, 409 price_list_immutable / price_list_overlap (`conflicts[]`), 412, 422 price_list_invalid (`issues[]`), 428 |
| POST `M/price-lists/{priceListId}/archive` | archivePriceList | pricing.configure | If-Match | 200 + ETag | 403, 404, 409 (draft cannot be archived: delete it), 412, 428 |
| POST `M/price-lists/{priceListId}/renewal` | renewPriceList | pricing.configure | `RenewalRequest{dry_run, code, name, shift_years (0..5), istat_bp?, adjustments[{scope: all\|item\|category, item_id?, category?, percent_bp?, amount_cents?}], amount_rounding, valid_from?, valid_to?, matrix?: {contract_types?, channel?, customer_category?}}` (+ Idempotency-Key when not dry run) | dry run 200 `RenewalPreview{document (unsaved), diffs: [{contract_type, before: PriceMatrix, after: PriceMatrix, cells: [{row, col, old_cents?, new_cents?, delta_cents?, delta_bp?}]}], summary: {rules, amounts_changed}}`; apply 201 `PriceListDocument` (draft, `source_price_list_id`) + ETag | 400, 403, 404, 409 conflict (code) |

`effective_from` default = the list's `valid_from`. Activation rules (INV-24): for each other `active` list of the
marina sharing a contract type and overlapping `[effective_from, valid_to]`: if its `valid_from < effective_from`
its `valid_to` becomes `effective_from − 1` (reported in `truncated`); otherwise 409 `price_list_overlap`. The
activated list's `valid_from` is set to `effective_from` in the same update (still draft at that moment).

### 4.5 Pricing tools (handlers `internal/http/admin/pricing_tools.go`, PRICING-SERVICES)

| Method & path | operationId | Perm | Request | 2xx | Errors |
|---|---|---|---|---|---|
| POST `M/pricing/simulate` | simulatePrice | pricing.read | `PricingSimulateRequest{price_list_id?, contract_type, arrival, departure, item_id?, vessel_id? \| vessel?: {loa_cm, beam_cm, hull_type}, resource?: {type: berth\|dynamic_area\|land_resource, id} \| category?, berth_length_cm?, berth_width_cm?, group_ids?, customer_id? \| customer_category?, channel?, promo_code?, quoted_on?, services?: [{item_id, qty_milli?}], deposit?: {percent_bp?\|amount_cents?}}` | 200 `PriceBreakdown{…§2.2, resolved: {price_list_id, price_list_code, item_id, loa_cm, beam_cm, hull_type, category, berth_length_cm, berth_width_cm, group_ids, customer_category, channel, quoted_on, dimension_origin: declared\|measured\|input}}` | 400 validation_failed, 403, 404, 422 no_price (`details`), 422 promo_not_applicable |
| GET `M/price-lists/{priceListId}/matrix` | getPriceMatrix | pricing.read | query `contract_type?` (default first served), `item_id?`, `rows?` (bands\|categories), `channel?`, `customer_category?`, `quoted_on?` | 200 `PriceMatrix{price_list_id, contract_type, item_id, quoted_on, rows[], columns[], cells[{row, col, total_cents?, net_cents?, base_cents?, unit?, error?, probe: PricingSimulateRequest}]}` | 400, 403, 404 |
| GET `M/price-lists/{priceListId}/coverage` | getPriceCoverage | pricing.read | query `quoted_on?` | 200 `PriceCoverage{gaps: [{contract_type, category?, band?, period?, item_id, reason}], elements_without_category: {berths, dynamic_areas, land_resources}, unknown_categories: [{code, count}], checked: int}` | 403, 404 |

Resolution in `simulatePrice` (service, §5.2): `price_list_id` absent → the active list serving `contract_type`
on `arrival` (none → 422 `no_price` reason `no_active_list`); `vessel_id` → effective dims (measured when
`dimensions_source = measured`) and `hull_type`; `resource` → its `price_class`, length/width (`berths.length_cm`,
`width_cm`; area: `max_loa_cm`/`max_beam_cm`; land: `length_cm`/`width_cm`), group ids; explicit fields override
resolved ones; `customer_id` → `customers.category`; `quoted_on` default = today in the marina timezone. The
customer, vessel and promo code never appear in logs; `resolved` carries no names.

### 4.6 Problem types (new slugs: `internal/http/problem/types.go`, `internal/http/respond/errmap.go`, i18n `errors.*`)

`no_price` 422 (`details: {item_id, contract_type, night, unit, category, band, reason}`; `problem.Problem` gains
`Details any \`json:"details,omitempty"\`` + `problem.WithDetails`), `promo_not_applicable` 422,
`price_list_invalid` 422 (with `issues`, reusing M1's `WithIssues`), `price_list_immutable` 409,
`price_list_overlap` 409 (`details: {conflicts: [{id, code, valid_from, valid_to}]}`), `in_use` 409 (`details:
{references: int}`). Reused: `validation_failed`, `version_conflict`, `precondition_required`, `conflict`,
`not_found`, `forbidden`.

Sentinels (CONTRACT, names fixed): `pricing.ErrNoPrice` (`*NoPriceError`), `pricing.ErrPromoNotApplicable`,
`pricing.ErrInvalidInput` (`*InputError` → `validation_failed` field error); `catalog.ErrInUse`,
`catalog.ErrVersionMismatch`; `pricelist.ErrImmutable`, `pricelist.ErrOverlap` (`*OverlapError{Conflicts}`),
`pricelist.ErrInvalid` (`*InvalidError{Issues}`), `pricelist.ErrVersionMismatch`, `pricelist.ErrNoActiveList`
(→ `no_price` reason `no_active_list`); `store.ErrImmutable`.

### 4.7 Events and SSE

| Type | Public (webhooks) | Topic | Data (ids and enums only) |
|---|---|---|---|
| `config.pricing_changed` | yes (spec 27.3) | `pricing.changed` | `{price_list_id?, item_id?, category_id?, revenue_class_id?, version?, change: settings\|revenue_class\|item\|category\|created\|saved\|activated\|archived\|deleted\|renewed, truncated_ids?: []}` |

A sample goes in `Samples()` (`TestEvents_DataHasNoPII`). Web `keysForTopic('pricing.changed')` invalidates
`queryKeys.pricing(marinaId)` (every pricing key lives under `['marina', id, 'pricing', …]`). Audit actions:
`pricing_settings.update`, `revenue_class.{create,update,archive,delete}`, `item.{create,update,delete}`,
`berth_category.{create,update,delete,sync}`, `price_list.{create,save,activate,truncate,archive,delete,renew}`;
diffs list changed field names and counts, never promo codes.

### 4.8 Validation issues (`pricing.Validate`, shown in the editor and returned by save/validate/activate)

Errors (block activation): `period_overlap` (two seasons or two events), `range_invalid` (to < from),
`period_code_unknown` (rule scope), `band_order` (max_cm not increasing), `band_open_not_last`,
`band_code_unknown` (amounts key), `rule_item_unknown`, `rule_item_inactive`, `rule_basis_invalid` (basis vs
fields: e.g. `per_meter` without `unit_cents`, `metered` on a berth_fee primary item), `rule_unit_invalid`
(`stay` on a berth fee, `season` without window), `rule_effect_invalid`, `promo_without_code`,
`customer_discount_without_category`, `seasonal_without_window`, `primary_item_missing` (a served contract type
without primary item), `contract_type_unknown`, `amount_out_of_range`.
Warnings: `period_outside_validity`, `validity_not_covered_by_periods` (nights with no period),
`category_unknown` (not in `berth_categories`), `split_unit_missing` (e.g. `whole_months` without a `month` rule),
`no_base_rule_for_contract_type`, `duplicate_rule` (same item, unit, scope, priority).

### 4.9 Meta

Capabilities: `pricing.engine`, `pricing.simulator`, `pricing.matrix`, `pricing.coverage`, `pricing.renewal`;
`meta.pricing_engine_version = pricing.EngineVersion`.

---

## 5. Services (`internal/domain/catalog`, `internal/domain/pricelist`, PRICING-SERVICES)

### 5.1 Packages

```
internal/domain/catalog/      pricing settings, revenue classes (lazy defaults), items, berth categories, sync, in_use checks
internal/domain/pricelist/    price-list documents (create, save diff, validate, delete), activation (INV-24), archive,
                              renewal (dry run + apply), simulate (input resolution), matrix, coverage, catalog loading
```

- `catalog` imports `store`, `audit`, `event`, `auth`, `domainerr`. `pricelist` imports `pricing`, `catalog`
  (read helpers only), `store`, `audit`, `event`, `auth`, `clock`. `pricing` imports neither (pure).
- `pricelist.Service.Quote(ctx, p, marinaID, req QuoteRequest) (pricing.Breakdown, Resolved, error)` is the single
  entry point that M3 (quotes, transits) and M7 (reports) will call; the simulate handler calls it too.
- `pricelist.LoadCatalog(ctx, tx, scope, listID) (pricing.Catalog, error)` maps rows to `pricing.Catalog` (items
  restricted to those referenced by the list plus requested services): live items for a draft, the activation
  snapshot `price_list_items` for an active or archived list (§12 D32, INV-26).
- Store files (PRICING-SERVICES): `internal/store/catalog.go` (settings, revenue classes, items, categories, usage
  counts), `internal/store/pricelists.go` (list document load/diff save, activation queries, truncation),
  `internal/store/pricing_inputs.go` (read-only lookups of berths, dynamic areas, land resources, groups, vessels,
  customers category, distinct `price_class` counts — kept here so M1 store files stay untouched),
  `internal/store/pricing_integration_test.go`.
- Coverage categories (§2.5): for each berth category in use, `LOA = max(max_loa_cm)` and `Beam =
  max(max_beam_cm)` over its elements, `BerthLength/Width` = max of the elements' length/width; a category linked
  to a band uses the band probe instead; categories without elements are skipped.

### 5.2 Tests (names fixed; integration tag; both dialects via `testdb`)

- `TestCatalog_ItemScenario` (create with settings defaults, VAT/nature CHECKs, code uniqueness, patch, in_use on
  delete), `TestCatalog_RevenueClassDefaults`, `TestCatalog_CategorySync`, `TestCatalog_CrossMarinaRefs`
  (revenue class / item of another marina → 404).
- `TestPriceList_SaveDocumentDiff` (keys → ids, seq assignment, id_map), `TestPriceList_SaveVersionConflict`,
  `TestPriceList_PublishedIsImmutable` (service 409 **and** raw SQL updates/inserts/deletes on children and
  pricing columns fail on both dialects; INV-22), `TestPriceList_ActivateRequiresValid`,
  `TestPriceList_ActivateAtDateTruncatesPredecessor`, `TestPriceList_ActivateOverlapRejected`,
  `TestPriceList_ArchiveAndDelete`, `TestPriceList_RenewalDryRunAndApply`, `TestPriceList_WritesAuditAndOutbox`.
- `TestConcurrency_PriceListActivation` (100 parallel activations of overlapping drafts for the same contract type
  → for every night at most one active list; INV-24), `TestConcurrency_PriceListSaveSingleWinner` (100 parallel
  saves with the same If-Match → one 200).
- `TestPricing_SimulateResolvesInputs` (vessel measured vs declared, berth category/dims/groups, customer
  category, default active list, quoted_on in the marina timezone with `clock.Fake`).
- `TestPricing_MatrixEqualsSimulate` (INV-23): for the demo-like list and every contract type, every matrix cell's
  `probe` posted to `simulatePrice` returns a breakdown deep-equal to the cell (total, lines, VAT).
- `TestPricing_CoverageFindsGaps`, `TestPricing_NoPIIInLogs` (promo code and customer id absent from logs).
- Handler tests asserting status and problem `type` for every operation; `TestAdmin_NoStubsLeft` stays green.

---

## 6. Demo seed (`internal/app/seed_pricing.go`, PRICING-SERVICES)

Idempotent: runs only if the demo marina has no price lists. Through the `catalog` and `pricelist` services with the
demo owner principal (audit, outbox, triggers hold). `Y` = current year in the marina timezone (`clock`).
`SeedResult` gains `items`, `price_lists`, `price_rules`.

- **Revenue classes** (defaults, spec 12.1): `mooring_annual`, `transit`, `storage`, `services`, `yard_labour`,
  `yard_materials`, `rental`, `utilities`, `fees`; plus subclass `mooring_seasonal` under `mooring_annual`.
- **Berth categories**: `L8 L10 L12 L14 L16 L18 L20 L25` (applies_to berth, dynamic_area) from the M1 default rule,
  `BOA` (buoys), `PARK`, `BOX` (land resources) — via `syncBerthCategories` then names it/en.
- **Items**:

  | Code | Type | Unit | VAT | Incl. | Revenue class |
  |---|---|---|---|---|---|
  | `ORM-ANN` | berth_fee | year | 22 % | no | mooring_annual |
  | `ORM-STG` | berth_fee | season | 22 % | no | mooring_seasonal |
  | `ORM-MES` | berth_fee | month | 22 % | no | mooring_annual |
  | `TRN-NTT` | berth_fee | night | 22 % | yes | transit |
  | `SRV-UTF` | utility | season | 22 % | no | utilities (flat utilities, included in annual) |
  | `SRV-ALG` | service | lump | 22 % | no | services (haul-out and launch) |
  | `SRV-AUTO` | service | night | 22 % | yes | services (car park) |
  | `UTL-KWH` | utility | kwh | 22 % | yes | utilities |
  | `UTL-H2O` | utility | m3 | 10 % | yes | utilities |
  | `FEE-PRA` | fee | lump | 22 % | no | fees (paperwork) |
  | `DEP-CAU` | deposit | lump | 0 % N1 | – | fees (security deposit, excluded art. 15) |

- **Price list `LST-Y` "Listino Y"**, active, valid `Y-01-01..Y-12-31`, contract types annual, seasonal, monthly,
  transit; primary items as above; `dimension_source = vessel`; length rounding 10 cm up; min beam 1/3; split
  `{"*": pro_rata_daily, transit: best_combination}`; rounding `{annual: euro, seasonal: euro, "*": none}`;
  deposits `{annual: 30 %, seasonal: 50 %, monthly: 100 %, transit: 30 %}`; season window `Y-04-01..Y-09-30`.
  - Periods: `BASSA` season (`01-01..03-31`, `10-16..12-31`), `MEDIA` season (`04-01..06-15`, `09-01..10-15`),
    `ALTA` season (`06-16..08-31`), `SALONE` event (`09-17..09-22`, a fictional boat show).
  - Bands: `L8` ≤ 800, `L10` ≤ 1 000, `L12` ≤ 1 200, `L14` ≤ 1 400, `L16` ≤ 1 600, `L18` ≤ 1 800, `L20` ≤ 2 000,
    `L25` ≤ 2 500 (each linked to the same-named category).
  - Base rules (€; length_band amounts by band L8…L25):

    | Rule | Item / unit / basis | Amounts |
    |---|---|---|
    | Annual | ORM-ANN / year / length_band | 2 900, 3 800, 4 900, 6 200, 7 800, 9 600, 11 800, 16 500; includes SRV-UTF × 1 |
    | Seasonal | ORM-STG / season / length_band | 1 900, 2 450, 3 200, 4 050, 5 050, 6 250, 7 650, 10 700 |
    | Monthly BASSA | ORM-MES / month / length_band, periods [BASSA] | 290, 380, 490, 620, 780, 960, 1 180, 1 650 |
    | Monthly MEDIA | same, [MEDIA] | 380, 490, 640, 810, 1 010, 1 250, 1 530, 2 150 |
    | Monthly ALTA+SALONE | same, [ALTA, SALONE] | 520, 680, 880, 1 120, 1 400, 1 730, 2 120, 2 970 |
    | Transit night | TRN-NTT / night / per_meter, one rule per period | BASSA 3,00 €/m, MEDIA 4,50, ALTA 7,50, SALONE 9,00 |
    | Transit week | TRN-NTT / week / per_meter, per period | 18,00, 27,00, 45,00, 54,00 €/m |
    | Transit buoy | TRN-NTT / night / per_berth, categories [BOA], priority 10 | BOA 25,00 € |
    | Services | SRV-ALG flat 180 €, SRV-AUTO flat 8 €/night, UTL-KWH metered 0,45 €/kWh, UTL-H2O metered 3,20 €/m³, FEE-PRA flat 25 €, DEP-CAU flat 200 €, SRV-UTF flat 0 € | |

  - Modifiers: `MULTISCAFO` +50 % non-stackable (hull catamaran/trimaran, all berth fees); `SBALZO` +20 %
    non-stackable (overhang ≥ 50 cm, transit and monthly); `LUNGA-SOSTA` −10 % stackable (transit, min 14 nights);
    `ANTICIPO` −5 % stackable (transit, lead ≥ 60 days); `ULTIMO-MINUTO` −10 % non-stackable (transit, lead ≤ 2,
    periods BASSA); `ONLINE` −3 % stackable (channel online).
  - Discounts: `SOCI` customer −10 % non-stackable (categories member; annual, seasonal); `RESIDENTI` customer −5 %
    (resident; annual); `BENVENUTO` promo `BENVENUTOY` −10 % (transit).
- **Draft `LST-(Y+1)`** created by `renewPriceList` from `LST-Y`: shift 1 year, ISTAT 1,2 %, +3 % on `TRN-NTT`,
  amount rounding euro, not activated (the renewal and diff pages have data to show).

---

## 7. Shared files and foundations

### 7.1 Shared files edited only by CONTRACT (then frozen for M2)

| File | CONTRACT change |
|---|---|
| `api/openapi.yaml` | all of §4 |
| `internal/http/api/api.gen.go`, `web/shared/api/generated/schema.d.ts` | `make gen` |
| `internal/migrations/{sqlite,postgres}/00004_pricing.sql` | §3.1, §3.2 |
| `internal/migrations/*_test.go` | parity/up-down-up includes 00004; `TestMigrations_PriceListCascade` |
| `internal/store/pricing_models.go` | Bun structs for every §3.1 table (then owned by PRICING-SERVICES; may add fields that exist in the migration) |
| `internal/store/types.go` (or `db.go`) | `ErrImmutable` + trigger error mapping for both drivers |
| `internal/store/backup_tables.go`, `internal/backup/**` | §3.2 order and the status two-pass |
| `internal/domain/auth/roles.go` + `TestRoles_MatrixMatchesSpec` copy | §4.2 |
| `internal/domain/event/event.go`, `payloads.go` | §4.7 type, topic, payload struct, sample |
| `internal/domain/meta/meta.go` | §4.9 |
| `internal/http/problem/{types,problem}.go` + tests | §4.6 slugs, `Details` |
| `internal/http/respond/errmap.go` + test | §4.6 sentinels |
| `internal/http/admin/admin.go` | `Handler` fields `Catalog *catalog.Service`, `PriceLists *pricelist.Service` |
| `internal/http/admin/pricing_stubs.go` | every new operation answers 501 (stub marker) so `ServerInterface` compiles; PRICING-SERVICES deletes methods from it as it implements them, and deletes the file at the end |
| `internal/app/app.go` | wiring of `catalog` and `pricelist` through `Deps`; meta |
| `internal/app/seed.go` | calls `a.seedPricing(ctx, p, res)` after M1 parts; idempotency rule of §6 |
| `internal/domain/pricing/{doc.go,types.go,api.go,errors.go,trace.go}` | §2.2 types and stubs (then owned by PRICING-ENGINE, which keeps exported names and shapes) |
| `internal/domain/{catalog,pricelist}/{service.go,errors.go}` | skeletons: `type Deps struct{ DB *store.DB; Clock clock.Clock; Audit *audit.Writer; Outbox *event.Outbox; Log *slog.Logger }`, `func NewService(d Deps) *Service`, sentinels (then owned by PRICING-SERVICES) |
| `web/shared/src/i18n/resources.ts`, `locales/{it,en}/{common,errors,pricingEnums}.json` + new empty-ish `pricing.json`, `pricingTools.json` | namespaces (§7.3), nav labels, error slugs, all enum labels and trace codes |
| `web/shared/src/api/{queryKeys,sse,index}.ts` | `queryKeys.pricing(m)` root and children: `pricingSettings`, `revenueClasses`, `items`, `item(id)`, `berthCategories`, `priceLists`, `priceList(id)`, `priceMatrix(id, params)`, `priceCoverage(id, params)`; `keysForTopic('pricing.changed')`; barrel exports |
| `web/shared/src/api/hooks/{pricing,pricingTools}.ts` | empty stubs (then owned by WEB-PRICELISTS / WEB-SIMULATOR) |
| `web/shared/src/lib/format.ts` + test | `parseEuroToCents(text): number \| null` (it/en separators, ≤ 2 decimals, no float arithmetic: string split), `formatBp(bp, locale)`, `parsePercentToBp(text)`, `formatQtyMilli(milli, locale)`, `parseQtyToMilli(text)` |
| `web/shared/src/ui/patterns/{MoneyInput,PercentInput}.tsx` + tests | inputs bound to cents / bp, built on `input-group` |
| `web/shared/src/ui/**` | shadcn CLI: none expected (table, tabs, sheet, calendar, command, popover, toggle-group exist). A builder needing one asks the orchestrator |
| `web/admin/src/router.tsx`, `nav.ts`, `nav.test.ts` | factories `pricingRoutes(marinaRoute)` and `pricingToolRoutes(marinaRoute)`; nav items §5.1; remove nothing from `UPCOMING_MODULES` (contracts and charges stay upcoming) |
| `web/admin/src/features/pricing/routes.tsx`, `web/admin/src/features/pricing-tools/routes.tsx` | stubs returning placeholder pages (then owned by the web builders) |
| `web/admin/src/features/pricing-tools/components/PriceMatrixTable.tsx` | stub with the fixed props of §8.3 (then owned by WEB-SIMULATOR) |
| `e2e/support/pricing.ts` | stub helpers with the fixed signatures of §8.4 (then owned by WEB-PRICELISTS) |
| `docs/invariants.md` | INV-20/21 test names, INV-22..25 (§10.5) |
| `CHANGELOG.md` | M2 entries |

### 7.2 Web routes (`web/admin`, TanStack code-based)

| Route | Owner | Page | Permission (UI) |
|---|---|---|---|
| `/m/$marinaId/items` | WEB-PRICELISTS | items DataTable (filters type/active/search), create/edit Sheet (code, names it/en, type, unit, VAT rate select 22/10/5/4/0 + custom via `PercentInput`, nature select when 0, incl/excl switch, revenue class select, external code, online visible, sort), deactivate, delete with `in_use` message | pricing.read (writes pricing.configure) |
| `/m/$marinaId/pricing/classifications` | WEB-PRICELISTS | tabs: Categorie posti (CRUD, usage counts, unknown codes in use with "Sincronizza"), Classi di ricavo (two-level tree CRUD), Categorie clienti (read-only enum + counts), Tipi di scafo (read-only, multihull marked) | pricing.read |
| `/m/$marinaId/pricing/settings` | WEB-PRICELISTS | default VAT rate and incl/excl (If-Match) | pricing.configure |
| `/m/$marinaId/price-lists` | WEB-PRICELISTS | list grouped by status, validity timeline per contract type, create dialog, actions (open, copy/renew, archive, delete draft) | pricing.read |
| `/m/$marinaId/price-lists/$priceListId` | WEB-PRICELISTS | document editor: tabs Generale (validity, contract types, primary items, dimension source, length rounding, min beam, split and rounding per contract type, season window, deposits), Periodi (periods table + year timeline built from Tailwind grid cells coloured with `chart-*` tokens; ranges with `DatePicker`), Fasce (bands table with computed "da–a" in metres via `LengthInput`), Regole (DataTable grouped by stage; rule Sheet with basis-dependent fields, amounts grid per band/category with `MoneyInput`, scope multi-selects, conditions, effect, includes), Validazione (issues from `validatePriceList`, debounced dry run, click → field). Save with If-Match and conflict dialog; read-only banner when not draft; actions Attiva (dialog with effective date, shows `truncated`/overlap), Archivia, Rinnova, links Simula / Matrice / Copertura | pricing.read (edit pricing.configure) |
| `/m/$marinaId/price-lists/$priceListId/renew` | WEB-PRICELISTS | wizard: 1 target (code, name, shift years, validity), 2 adjustments (ISTAT bp, rows scope/item/category/percent/amount, rounding), 3 preview (`renewPriceList` dry run; per contract type `PriceMatrixTable` in `diff` mode), 4 create draft → navigate to editor | pricing.configure |
| `/m/$marinaId/pricing/simulator` | WEB-SIMULATOR | form (list select default "attivo alla data", contract type, arrival/departure `DatePicker`s showing nights, vessel combobox or manual LOA/beam/hull, resource combobox (berths/areas/land via `ElementCombobox` over `listBerths`) or category select + berth dims, groups, customer combobox or category, channel, promo, quoted on, services repeater with qty, deposit override) → `BreakdownView`: lines, blocks table (dates, unit, period, winning rule link to the editor, base, modifiers), discounts, rounding, VAT summary, deposit, trace rendered with `pricingEnums:trace.*`; `?probe=` prefill; "copy as JSON" | pricing.read |
| `/m/$marinaId/price-lists/$priceListId/matrix` | WEB-SIMULATOR | contract type tabs, item select, rows bands/categories, channel, customer category, quoted on; `PriceMatrixTable`; each cell links to the simulator with its `probe`; print button | pricing.read |
| `/m/$marinaId/price-lists/$priceListId/matrix/print` | WEB-SIMULATOR | A4 landscape, every contract type, header with `BrandLogo`, list code/validity, VAT note; Tailwind `print:` only | pricing.read |
| `/m/$marinaId/price-lists/$priceListId/coverage` | WEB-SIMULATOR | summary cards (checked, gaps, elements without category, unknown categories), gaps table filterable by contract type/period/category with "apri regole" link, links to maps for elements without category | pricing.read |

`nav.ts` (CONTRACT): items `price-lists` (pricing.read, label `common:nav.priceLists`), `items` (pricing.read),
`pricing-classifications` (pricing.read), `pricing-simulator` (pricing.read); `pricing-settings` under the
settings group (pricing.configure). `MarinaRoutePath` extended accordingly.

Shared component contract (WEB-SIMULATOR implements, WEB-PRICELISTS imports):
```ts
// web/admin/src/features/pricing-tools/components/PriceMatrixTable.tsx
export type PriceMatrixTableProps =
  | { mode: 'values'; matrix: components['schemas']['PriceMatrix']; onCellClick?: (probe: components['schemas']['PricingSimulateRequest']) => void }
  | { mode: 'diff'; before: components['schemas']['PriceMatrix']; after: components['schemas']['PriceMatrix'];
      cells: components['schemas']['RenewalPreview']['diffs'][number]['cells'] };
export function PriceMatrixTable(props: PriceMatrixTableProps): JSX.Element;
// web/admin/src/features/pricing-tools/probe.ts
export function encodeProbe(p: PricingSimulateRequest): string; // base64url JSON for ?probe=
export function decodeProbe(s: string): PricingSimulateRequest | null;
```

### 7.3 i18n namespaces

CONTRACT creates and registers: `pricingEnums` (CONTRACT-owned, complete: contract types, item types, units,
bases, time units, split rules, dimension sources, roundings, channels, stages, discount kinds, period kinds,
customer categories, hull types, VAT natures with descriptions, issue codes `issues.*`, trace codes `trace.*`,
no-price reasons), `pricing` (WEB-PRICELISTS), `pricingTools` (WEB-SIMULATOR), plus `common` nav keys and `errors`
slugs. Builders request additions to CONTRACT-owned namespaces in their report; `i18n-check` fails on missing keys.

### 7.4 Engine ↔ services ↔ web contract points

- The engine's JSON shapes are the API shapes; `internal/http/view/pricing.go` converts `pricing.Breakdown` to the
  generated type field by field (no re-computation).
- The web never computes prices: totals, VAT and matrix values come from the server. Inputs are converted to
  integers with `parseEuroToCents`, `parsePercentToBp`, `parseMetresToCm`, `parseQtyToMilli`.

---

## 8. Acceptance

### 8.1 Gates (exact commands, from the repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated   # empty
make check                                                    # gen-check + lint (depguard pure-engines incl. pricing) + Go/web unit tests, exit 0
make test-db                                                  # integration on SQLite AND PostgreSQL, exit 0
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'   # 00004 numbering, up/down/up, parity, cascade
go test ./internal/domain/pricing/... -count=1 -run 'TestQuote_Corpus|TestCorpus_' -v | tee /tmp/porto-corpus.txt
grep -c -- '--- PASS: TestQuote_Corpus/' /tmp/porto-corpus.txt  # >= 200, and no FAIL lines
go test ./internal/domain/pricing/... -count=1 -rapid.checks=2000   # all properties incl. TestMatrix_EqualsQuote, TestQuote_SnapshotReproducible
go test ./internal/domain/pricing/... -run '^$' -fuzz FuzzQuote -fuzztime 60s
go test ./internal/domain/pricing/... -run '^$' -bench 'BenchmarkQuote_Transit30Nights|BenchmarkMatrix_8x5' -benchtime 50x
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/domain/pricelist/... ./internal/domain/catalog/... ./internal/store/...
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/acceptance/... -run 'TestAcceptance_M2|TestAcceptance_CrossMarinaIsolation|TestAcceptance_M1|TestAcceptance_M0'
make test-race                                                # includes TestConcurrency_PriceListActivation, TestConcurrency_PriceListSaveSingleWinner
make e2e                                                      # all specs, including §8.4
scripts/agents/check-naming.sh && make licenses
rm -f tmp/data/porto.db* && make seed && go run ./cmd/porto seed demo   # second run is a no-op; prints items/price_lists/price_rules
```

(`/tmp/porto-corpus.txt` is a throwaway log of the gate; builders use their scratch directory.)

### 8.2 Acceptance test (`internal/acceptance/m2_pricing_test.go`, tag integration, API only, `clock.Fake`)

**`TestAcceptance_M2_Pricing`** (PRICING-SERVICES), as the owner, `clock` at `Y-03-01` Europe/Rome:
1. `syncBerthCategories` after an M1 map with berths in L10/L12; create revenue class defaults (implicit), items
   `TRN-NTT` (VAT incl.) and `ORM-ANN`.
2. Create list `T-Y` (transit, annual), save periods BASSA/ALTA, bands L10/L12, transit `per_meter` night rules
   and an annual `length_band` rule, rounding annual euro; `validatePriceList` → 0 errors; activate.
3. `simulatePrice` transit 3 nights in ALTA for an 11,34 m vessel → expected total computed in the test by hand
   (`why` comment) — e.g. 7,50 €/m × 11,40 m × 3 = 256,50 €; VAT split 210,25 + 46,25.
4. `getPriceMatrix` transit: the L12 × ALTA cell total equals a simulate of the cell's probe; then **every** cell of
   every contract type equals its probe's simulation (INV-23).
5. `getPriceCoverage` → 0 gaps; save a draft copy (`renewPriceList` shift 0, code `T-Y-B`) without the BASSA rule
   → coverage of the draft lists the gap (transit, BASSA).
6. `savePriceList` on the active list → 409 `price_list_immutable`; raw SQL update of a rule amount → trigger error.
7. `renewPriceList` dry run +5 % → every diff cell `delta_bp` = 500 ± rounding; apply → draft `T-(Y+1)`; activate
   with `effective_from = Y-07-01` on a second list for transit → predecessor truncated to `Y-06-30`; activating an
   overlapping list starting on `T-Y`'s `valid_from` → 409 `price_list_overlap`.
8. Dock: `simulatePrice` 200, `createPriceList` 403; yard: `getPriceMatrix` 403; another marina's ids → 404.
9. Outbox has `config.pricing_changed` for create/save/activated/renewed with no PII; audit rows exist.

`TestAcceptance_CrossMarinaIsolation` covers every new `{marinaId}` route automatically; the route-count threshold
rises to ≥ 100 (CONTRACT edits only the threshold).

### 8.3 Web unit tests (Vitest)

- WEB-PRICELISTS: item form (VAT/nature exclusivity, money parsing), period timeline rendering, band "da–a"
  computation, rule Sheet per basis (fields shown/hidden, amounts grid), document diff/save with keys, read-only
  mode for active lists, renewal wizard steps.
- WEB-SIMULATOR: `encodeProbe/decodeProbe` round trip, simulator form → request mapping (integers only),
  `BreakdownView` renders lines/VAT/trace from a fixture breakdown, `PriceMatrixTable` values and diff modes,
  coverage filters.

### 8.4 e2e (Playwright, `make e2e`)

Helpers (`e2e/support/pricing.ts`, WEB-PRICELISTS; fixed signatures, CONTRACT stubs them):
```ts
export async function createItemViaUi(page: Page, item: { code: string; name: string; type: 'berth_fee' | 'service'; unit: string; vatPercent: string; includesVat: boolean }): Promise<void>;
export async function createTransitPriceListViaUi(page: Page, opts: { code: string; year: number; itemCode: string;
  periods: { code: string; from: string; to: string }[]; perMeterEuro: Record<string, string> }): Promise<{ priceListId: string }>;
export async function activatePriceListViaUi(page: Page, priceListId: string): Promise<void>;
```

- **`e2e/tests/pricing-acceptance.spec.ts`** (WEB-SIMULATOR), the M2 scenario: log in as owner of a fresh marina
  (API setup of a small map with an L12 berth through `e2e/support/map.ts`); create item `TRN-NTT` and a transit
  list with BASSA/ALTA periods and per-metre prices through the UI helpers; activate; open the simulator, enter an
  11,34 m monohull, 3 nights in ALTA, berth category L12 → the total shown is **€ 256,50**; open the matrix
  (transit) → the L12 × ALTA cell shows the price for one night of the band's probe (12,00 m × 7,50 € = **€ 90,00**);
  click it → the simulator opens prefilled with the probe and shows **the same € 90,00**; assert through
  `page.request` that `getPriceMatrix` cell total equals `simulatePrice(probe)` total.
- `e2e/tests/price-lists.spec.ts`, `items.spec.ts`, `price-list-renewal.spec.ts` (WEB-PRICELISTS): CRUD of items and
  categories, editor save/conflict dialog (second tab), validation issues shown, activation with effective date,
  read-only active list, renewal wizard preview diff then draft creation; axe on items and editor (light, dark).
- `e2e/tests/simulator.spec.ts`, `coverage.spec.ts` (WEB-SIMULATOR): no-price problem shown with reason, promo
  not applicable message, multihull modifier visible in trace, matrix print page renders, coverage gap after
  removing a rule in a draft; axe on simulator and matrix.
- Existing M0/M1 specs stay green.

### 8.5 Invariants (`docs/invariants.md`; CONTRACT writes the entries, the orchestrator flips the status at the end)

- **INV-20** → `implemented`, verified by `TestQuote_SnapshotReproducible`.
- **INV-21** → `implemented`, verified by depguard `pure-engines` and `internal/domain/pricing/purity_test.go`.
- New **INV-22 — Listini pubblicati immutabili.** Un listino `active` o `archived` non cambia regole, periodi, fasce
  né impostazioni; cambia solo stato verso `archived` e `valid_to` per troncamento. Verificata da:
  `TestPriceList_PublishedIsImmutable` (trigger su entrambi i dialetti). Stato: implemented.
- New **INV-23 — Matrice = simulatore.** Ogni cella della matrice è il risultato del motore sul suo input di prova,
  identico a una simulazione dello stesso input. Verificata da: `TestMatrix_EqualsQuote`,
  `TestPricing_MatrixEqualsSimulate`, e2e `pricing-acceptance`. Stato: implemented.
- New **INV-24 — Un solo listino attivo per notte e tipo di contratto.** Verificata da:
  `TestConcurrency_PriceListActivation`, `TestPriceList_ActivateOverlapRejected`. Stato: implemented.
- New **INV-25 — Lo scorporo IVA somma il totale.** Per ogni breakdown `Σ imponibile + Σ IVA = totale`, per riga e
  per aliquota. Verificata da: `TestRapid_VATSumsExactly`. Stato: implemented.
- INV-70 stays `partial` with the new sample; INV-73 covers the M2 routes.

### 8.6 Done criteria per builder

Each builder finishes with its own subset green (`go test` of its packages, `-tags=integration` on both dialects,
`npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test`, its e2e specs) and runs
the `invariants-guardian` subagent for pricing changes. The orchestrator then runs §8.1 in full.

---

## 9. Strict file ownership

Order: **CONTRACT** runs first, alone, and finishes when `make gen && go build ./... && make check` pass with stubs
(the corpus and acceptance tests do not exist yet). Then **PRICING-ENGINE**, **PRICING-SERVICES**,
**WEB-PRICELISTS** and **WEB-SIMULATOR** run in parallel. Generated files belong to CONTRACT, which reruns
`make gen` on request. Files not listed belong to the orchestrator and are read-only for builders:
`docs/spec/**`, `docs/adr/**`, `docs/m*-plan.md`, `docs/architecture.md`, `README.md`, `AGENTS.md`, `CLAUDE.md`,
`assets/**`, `.claude/**`, `.github/**`, `deploy/**`, and all M0/M1 files not listed below.

| Owner | Files |
|---|---|
| **CONTRACT** | everything in §7.1; `internal/acceptance/acceptance_test.go` (route-count threshold only) |
| **PRICING-ENGINE** | `internal/domain/pricing/**` (after CONTRACT; keeps §2.2 exported names, shapes and JSON tags); `testdata/pricing/**` |
| **PRICING-SERVICES** | `internal/domain/catalog/**`, `internal/domain/pricelist/**` (keep `NewService`/`Deps`/sentinels); `internal/store/{catalog,pricelists,pricing_inputs}.go`, `internal/store/pricing_models.go` (after CONTRACT), `internal/store/pricing_integration_test.go`; `internal/http/admin/{catalog,pricelists,pricing_tools}.go` + `_test.go`, `internal/http/admin/pricing_stubs.go` (shrinks to deletion); `internal/http/view/pricing.go`; `internal/app/seed_pricing.go`; `internal/acceptance/m2_pricing_test.go`, `internal/acceptance/testdata/pricing/**` |
| **WEB-PRICELISTS** | `web/admin/src/features/pricing/**` (items, classifications, settings, price lists, editor, renewal; `routes.tsx` after CONTRACT); `web/shared/src/api/hooks/pricing.ts`; `web/shared/src/i18n/locales/{it,en}/pricing.json`; `e2e/tests/{price-lists,items,price-list-renewal}.spec.ts`; `e2e/support/pricing.ts` (after CONTRACT) |
| **WEB-SIMULATOR** | `web/admin/src/features/pricing-tools/**` (simulator, `BreakdownView`, matrix, matrix print, coverage, `PriceMatrixTable`, `probe.ts`; `routes.tsx` after CONTRACT); `web/shared/src/api/hooks/pricingTools.ts`; `web/shared/src/i18n/locales/{it,en}/pricingTools.json`; `e2e/tests/{pricing-acceptance,simulator,coverage}.spec.ts` |

Rules for the parallel phase:
- **Engine first contract.** PRICING-SERVICES codes against the §2.2 types and stub `api.go`; until PRICING-ENGINE
  lands, its unit tests use a tiny fake via an internal interface `quoter` (`func(pricing.Input, pricing.Catalog)
  (pricing.Breakdown, error)`) defaulting to `pricing.Quote`. Integration and acceptance tests run once the engine
  is in. PRICING-ENGINE never imports store or services.
- **Router registration:** `router.tsx` is CONTRACT's; each web builder adds routes only in its `routes.tsx`. The
  two factories use disjoint paths (§7.2): matrix/coverage paths under `price-lists/$priceListId/…` are declared
  by WEB-SIMULATOR as flat children of the marina route.
- **Handlers:** operations are split exactly as §4.3/§4.4 (catalog + price lists) and §4.5 (tools), all in
  PRICING-SERVICES files of `package admin`; no other builder touches `internal/http/**`.
- **Store:** one file per area as listed; M1 store files are not edited (read helpers go in `pricing_inputs.go`).
- **i18n:** only your own namespace; `pricingEnums`/`common`/`errors` through the orchestrator.
- **Query keys and hooks:** CONTRACT's keys; hooks in your own file. WEB-PRICELISTS's renewal page calls
  `renewPriceList` through its own hook and renders `PriceMatrixTable` (WEB-SIMULATOR) in `diff` mode; WEB-SIMULATOR
  reads price lists with its own `useQuery` on `queryKeys.priceList(id)`.
- **e2e helpers:** WEB-SIMULATOR uses `e2e/support/pricing.ts` only through the §8.4 signatures; WEB-PRICELISTS
  must keep them working against its UI.
- **go.mod / package.json:** no new dependency without the orchestrator's approval; none is needed (rapid already
  in go.mod from M1).

---

## 10. Order of work inside each builder

**CONTRACT**
1. Migration 00004 (both dialects, triggers), models, `ErrImmutable` mapping, backup two-pass; migration tests on
   both dialects.
2. `openapi.yaml` (§4), `make gen`; permissions, event, problems (`Details`), errmap, meta.
3. `pricing` types/stubs, `catalog`/`pricelist` skeletons, handler stubs, app wiring, seed hook.
4. Web foundations: format helpers, `MoneyInput`/`PercentInput`, i18n namespaces (`pricingEnums` complete),
   query keys, SSE, router/nav, route stubs, `PriceMatrixTable` stub, e2e helper stub.
5. `docs/invariants.md` entries, CHANGELOG.
6. `make check`, `make test-db` green with stubs, except `TestAcceptance_CrossMarinaIsolation` (501 stubs, report
   as expected).

**PRICING-ENGINE**
1. `dates.go`, `basis.go`, `select.go` with table tests.
2. `split.go` (exact, pro rata, whole months, best combination DP), `modifiers.go`, `services.go`,
   `discounts.go`, `finalize.go`; `Quote`, snapshot, hash.
3. `validate.go`.
4. Corpus: fixtures + 246 hand-derived cases, runner, quota test.
5. Properties, fuzz, purity, benchmarks.
6. `matrix.go`, `renew.go`, `Diff` with `TestMatrix_EqualsQuote` and renewal tests; `doc.go`.

**PRICING-SERVICES**
1. Store queries (`catalog.go`, `pricelists.go`, `pricing_inputs.go`) with integration tests.
2. `catalog` service; handlers and views for §4.3.
3. `pricelist`: document save/validate/delete, activation (INV-24 lock), archive, renewal.
4. Simulate resolution, matrix, coverage; handlers for §4.4/§4.5; delete the stubs.
5. Concurrency, immutability, matrix = simulate tests; acceptance test.
6. Seed.

**WEB-PRICELISTS**
1. Hooks. 2. Items. 3. Classifications and settings. 4. Price list list page. 5. Document editor (general,
periods timeline, bands, rules Sheet, validation, save/conflict, activation). 6. Renewal wizard. 7. e2e helpers and
specs.

**WEB-SIMULATOR**
1. Hooks, `probe.ts`. 2. `BreakdownView`. 3. Simulator page. 4. `PriceMatrixTable` + matrix page + print.
5. Coverage page. 6. e2e (acceptance last, once the helpers exist).

---

## 11. Notes for M3+ (not to build now)

- Quotes and transits call `pricelist.Service.Quote` and store `pricing.NewSnapshot(...)` as `price_snapshot` with
  `engine_version`; re-quoting a confirmed stay uses `pricing.Recompute(snapshot)`, never the current list.
- M5 charge lines come from `Breakdown.Lines` (`item_id`, `qty`, `Net`, `VATRateBP`, `VATNature`,
  `RevenueClass`, `ExternalCode`, `ServiceFrom/To`), so the export totals per rate equal the snapshot's
  `VATSummary` (spec 32.1 "Esportazione").
- M7 lost-demand and allocation-waste reports price probes through the same service.

---

## 12. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | **Permissions.** `pricing.configure` (owner, director: spec 26.1 "configurazione … listini") for every write; new `pricing.read` (all roles except `yard`) for lists, items, matrix, coverage and the simulator, because office, dock (desk transits), accounting (VAT), yard_mgr/rental (storage, rentals later) and viewer (reports) need prices. |
| D2 | **No occupancy- or availability-dependent pricing in M2** (no occupancy data exists before M3). Rule conditions are limited to those listed in §2.4 step 6. |
| D3 | **At most one active list per night and contract type** (INV-24), stricter than spec 12.2 "per la stessa combinazione di articolo e ambito", which it implies. A marina with separate transit and annual lists uses `contract_types` to split them. Enforced in the activation transaction, serialised by `pricing_settings.activation_seq` (not by a DB constraint: date ranges cannot be unique-indexed portably). |
| D4 | **The list valid on the arrival night prices the whole stay**; nights past `valid_to` keep that list's rules (warning `stay_exceeds_validity`). A stay is never priced by two lists. |
| D5 | **Reference unit per contract type**: annual → year, seasonal → season, monthly → month, transit → night, storage → month, charter → season. Exact multiples of the reference unit use it directly; other periods use the list's split rule (per contract type, default `pro_rata_daily`). Exception: with `best_combination` a transit (reference unit night, always an exact multiple) still goes through the split rule (D31). |
| D6 | **Calendar arithmetic**: months are calendar months from the block start with end-of-month clamping (Jan 31 → Feb 28/29); years shift Feb 29 to Feb 28; a block is priced by the rate period of its first night; `best_combination` uses night/week (7)/calendar month blocks, may overshoot the departure, ties → fewer blocks → longer unit first. |
| D7 | **Rate periods** belong to a price list (copied on renewal). Kinds `season` and `event`; events override seasons; seasons never overlap seasons, events never overlap events. The `seasons` and `closures` tables of spec 6.1 are not created in M2 (closures are occupancy, M3). |
| D8 | **Modifiers** are additive on the block base (stackable deltas summed, not compounded), plus one non-stackable (the lowest delta); amounts clamp at 0. Fixed-amount modifiers are per block. |
| D9 | **VAT natures**: the current FatturaPA list (N1, N2.1, N2.2, N3.1–N3.6, N4, N5, N6.1–N6.9, N7), since N2/N3/N6 without subcode are no longer accepted by SdI; spec's "N1–N7" is read as this family. Rate 0 ⇔ nature present (DB CHECK). |
| D10 | **VAT is computed per line**: excluded-VAT lines get `half_even(amount × rate)`; total rounding is on the gross total and allocated to lines by largest remainder; every line is then split with `money.VATSplit`, so `Net + VAT = Gross` per line and in total, and excluded lines keep `Net = amount` when there is no rounding share. |
| D11 | **"Convenzione o categoria del cliente"** = discount rules of kind `customer` scoped to customer categories (`affiliated` = convenzionato). No agreements entity in M2. |
| D12 | **Promo codes** are a condition of `promo` discount rules (upper-case, per list); no usage counters (M8). An unknown code is `422 promo_not_applicable` in the simulator. |
| D13 | **Renewal adjusts base-rule amounts only** (amounts, unit prices, band/category amounts); modifier and discount effects, percentages and promo codes are copied unchanged. Order: ISTAT, then the most specific adjustment (item > category > all), then amount rounding. Shift 0 years = plain copy. The new list is always a draft; activation is a separate step with `effective_from`, which truncates predecessors. |
| D14 | **Immutability by DB triggers** (like `audit_log`, INV-72) plus a service check for the 409; backup restores lists as draft and sets the status in a final pass. |
| D15 | **Price list editing is a full-document save** with `If-Match` and client keys (same pattern as the M1 map document); drafts may be saved with validation errors, activation requires none. Rule recency `seq` is assigned by the service from `next_rule_seq` on creation and on body change. |
| D16 | **Berth categories table** registers the M1 `price_class` codes (free text stays on elements, no FK, so M1 migrations stay untouched); unknown codes in use are reported, and a sync creates them. |
| D17 | **Length bands are per price list**, inclusive upper bounds in cm (`10,01–12,00 m` = `max_cm 1200`, from = previous + 1 cm), optionally linked to a category for matrix probes; band selection uses the raw length, never the rounded one. |
| D18 | **Length rounding** applies to `per_meter`, `per_m2` and `per_m2_berth` only, with step 1/10/50/100 cm and mode up/half-even (default 10 cm up = "al decimetro"). `per_m2` minimum beam ratio default 1/3 (spec example), configurable per list. |
| D19 | **Matrix probes**: band rows use `LOA = max_cm` (price "fino a"), beam = LOA × min ratio, monohull motor, the band's category; columns per contract type as §2.5; channel/customer category/quoted-on are explicit parameters echoed in each probe. The matrix is "list price"; personalised conditions are visible only by changing those parameters. |
| D20 | **Coverage** checks every served contract type × (categories in use with elements' max dims, and bands) × columns with the same probes; categories with no elements are skipped. |
| D21 | **Deposit only** in M2 (step 6): percent of the total or fixed amount capped at the total, default per contract type on the list, overridable in the input. Instalment plans are M4 (INV-31). |
| D22 | **Total rounding** modes none / euro / five_euro, nearest multiple with ties to even, per contract type (default none). Renewal amount rounding none / ten_cents / euro. |
| D23 | **Item codes** follow `^[A-Z0-9][A-Z0-9._-]{0,31}$`; "codifica configurabile" means the marina chooses its codes (the seed shows the `ORM-/TRN-/SRV-` convention); no automatic numbering. |
| D24 | **Default price_includes_vat** comes from `pricing_settings` (default false, spec "default esclusa per i contratti B2B"); the demo marks transits and consumer utilities VAT-inclusive. |
| D25 | **Revenue classes** are a per-marina table with the nine spec defaults inserted lazily, one optional sub-level (category/subcategory). |
| D26 | **Services** are priced at the arrival night's period with their own base rules (`stay` once, `night` × N, `metered` × quantity); included services come from the winning base rule of the first block and are zero-amount lines. |
| D27 | **Simulator works on drafts** when the list id is given explicitly, so a draft can be tested before activation; without an id it uses the active list for the arrival night. |
| D28 | **SSE topic `pricing.changed`** is added to the spec 27.4 list for invalidation; the only event type is the public `config.pricing_changed` (spec 27.3). |
| D29 | **Effective dimensions** for pricing: measured when the vessel's `dimensions_source = measured`, else declared (spec 11.2); explicit simulator fields override them (`dimension_origin: input`). |
| D30 | **Limits**: 2 000 rules, 50 periods, 60 bands per list; stays ≤ 3 660 nights; amounts ≤ 1 000 000 €; lengths ≤ 200 m. |
| D31 | **Transit stays and `best_combination`**: when the split rule of the transit contract type is `best_combination`, a transit is priced by the dynamic programming over night/week/month blocks even though its nights are an exact multiple of the reference unit night, so a 7-night stay can be one week block and 8 nights a week plus a night (spec 12.3 "miglior combinazione", demo seed). With any other split rule a transit is `N` night blocks. Engine `internal/domain/pricing/split.go` `berthBlocks`, doc `internal/domain/pricing/doc.go` step 5, corpus `testdata/pricing/cases/11-best-combination.json` (`transit_split_rule_*`, `transit_pro_rata_*`). |
| D32 | **Published lists price with an item snapshot** (INV-26). Activation copies the engine fields of every item the list references (code, type, unit, VAT rate/nature, VAT inclusion, revenue class code, external code, active) into `price_list_items`, in the activation transaction before the status change, and `content_sha256` is computed on that same catalog. `LoadCatalog` reads the snapshot for active and archived lists and the live items for drafts (a requested service outside the snapshot is loaded live: the list has no rule for it). Items and revenue classes therefore stay fully editable (no 409 `in_use` on update); changes reach drafts, copies and renewals only. Deleting an item still requires that no list references it. |
