# M1 — Mappa del porto e anagrafiche: work order

Status: Plan phase output. This plan is authoritative for the M1 builders. If it conflicts with `docs/spec/`, the
spec wins: stop and report the conflict to the orchestrator instead of guessing. Open decisions that the spec
leaves to later milestones (`docs/spec/33-decisioni-aperte.md`) are not decided here. §11 lists every detail this
plan decided where the spec says nothing.

Sources: spec 05, 06 (§6.2, §6.4), 07, 08, 11, 26.1, 27 (§27.1–27.4), 29, 30, 31.4, 32 (§32.1, §32.3 M1, plus M2+ to
know what is out); `docs/invariants.md` (INV-01..09, INV-70..73); ADR 0001, 0002, 0003, 0004, 0007; `docs/m0-plan.md`;
`docs/architecture.md`; `.claude/rules/*.md`; skills `new-endpoint`, `new-migration`, `new-map-element`,
`new-domain-event`, `shadcn`.

## 0. Ground rules for every builder

Everything in `docs/m0-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001). Its beach map editor (`../arenile/internal/domain/mapping`,
   `../arenile/web/admin/src/features/map-editor`, `../arenile/web/shared/src/map`) is the closest analogue. Read it
   to see how a problem was solved, such as save with `If-Match`, the generator preview, conflict dialogs or the
   kit sprite. Then write Porto's own code with integer geometry. Never copy files, code, comments or tests. Never
   modify anything under `../arenile`. Arenile's map uses `float64`, and Porto's does not: nothing carries over
   line by line.
2. Add shadcn/ui components only with the official CLI, run in `web/shared`. The CONTRACT step adds all
   components M1 needs (§7.1).
3. Do not `git commit` and do not run destructive git commands. The orchestrator commits.
4. Edit only files you own (§9). To change a file someone else owns, write the request in your final report.
5. Code, identifiers and comments are in English. Every user-facing string uses i18n keys, with Italian as the
   source language and English required. Never log or emit emails, phone numbers, names, fiscal codes, document
   numbers, IBANs, tokens or secrets.
6. Do not use floats in domain logic. Lengths are `geo.Cm` and areas are `geo.Cm2`. Angles are `geo.Angle`
   (`rotation_dd`, tenths of a degree, 0..3599, where 0 = +x, 900 = +y, y down, clockwise on screen). Masses are
   `int64` kg and powers are `int64` kW and hp. Dates are `calendar.Date`, and time comes from `clock.Clock`. The
   web client may use floats only for rendering and pointer maths. Every value it stores or sends is an integer
   cm, rounded half-even by `roundHalfEven` in `web/shared/src/map/geometry.ts`.
7. `internal/domain/layout` is pure: no store, no clock, no `time.Now`, no randomness. Generators are deterministic.
   depguard and forbidigo already list `layout` (`.golangci.yml`). Do not add exclusions.
8. API first. CONTRACT writes `api/openapi.yaml` and runs `make gen`. Builders never edit generated files. If a
   builder needs a contract change, it asks the orchestrator, who reruns CONTRACT for that change.
9. Each state change goes through its domain service, inside `store.RunInTx`, and writes the change, an
   `audit_log` row and (where §4.6 says so) an outbox event in the same transaction.
10. Every query is scoped by `store.MarinaScope` from `Principal.Authorize`. A resource of another marina, or of
    another map of the same marina, is `ErrNotFound` (404).

---

## 1. Scope

### 1.1 In M1 (spec 32.3 M1, 07, 08, 11)

**Harbour map (spec 07, 08)**
- Multiple maps per marina (`water`, `yard`, `shed`, `parking`, `boxes`, `other`), with name (it/en), icon, sort
  order, size in cm (max 500 000 × 500 000) and grid. They are listed as tabs.
- Element kinds: structures (`quay`, `pier`, `finger`, `breakwater`, `channel`, `walkway`); berths (rotated
  rectangles, plus buoys as circles); dynamic areas (`linear` on a pier side or quay stretch, `surface` polygon
  with grid, blocked cells and exits); land resources; pedestals with sockets and meters; decorations from the
  illustration kit, plus free text, depth labels, north arrow and scale bar; water and land polygons; the
  concession perimeter.
- Berth groups (`berth_groups`, overlapping memberships) and price classes. Price classes are proposed from the
  berth width by a marina rule (§11 D7) and can be assigned one by one or in bulk.
- **Full editor (spec 8.1–8.6).** It has a scaled SVG canvas with pan and zoom, fit to view, a ruler and scale bar,
  and cursor coordinates in metres. Layers can be toggled. Snapping works on the grid, on structure edges (the
  anchor goes on the edge and the rotation is perpendicular), on vertices and on alignment with other berths. Selection
  works on a single element, multiple elements (marquee, shift), a whole pier or a group. The editor can move,
  rotate, align, distribute, duplicate, mirror and move by numeric values. It has a contextual properties panel
  that edits several berths at once, undo and redo, an explicit save with `If-Match` and a conflict dialog, a
  renumbering preview and a validation panel.
- **Generators (8.3):** pier with berths, berths along a quay, dynamic area, buoy field, land-resource grid, and yard
  or shed surface. They are pure Go functions. The client asks the server for a preview and inserts the result as
  unsaved elements. The insertion is one undo step, and it is applied in the save transaction.
- **Validations (8.4):** blocking errors and warnings come from `layout.ValidateMap`. It runs inside the save
  transaction and through a dry-run endpoint that the validation panel uses.
- **Background calibration and georeferencing (7.1, 8.2):** the editor uploads a PNG, JPEG or SVG image. Scale comes
  from two points and a known distance, plus rotation and opacity. Three control points (cm ↔ WGS84) define an
  affine georeference. The concession perimeter can be imported from GeoJSON or drawn by hand. The map can be
  exported as GeoJSON when georeferenced.
- **Import and export (8.7):** the map configuration in versioned JSON (export, and import as a new map). Berths
  can be imported from CSV (code, pier, length, width, depth, category), with geometry generated along existing
  piers through a wizard: mapping, dry run, report, apply. There is also a printable A4/A3 page in scale with a
  legend.
- Changes with active occupancies (8.5): M1 has no occupancies. Deleting and resizing go through an
  `OccupancyGuard` hook, which is a no-op in M1 and is implemented in M3. Renumbering never changes ids.
- A read-only map viewer for every role with `map.view`. It has no contexts or lenses (those are M3).
- **Illustration kit** `assets/harbor-kits/porto-basic/`: original Porto symbols, a manifest, a sanitising
  generator, `<symbol>`/`<use>` rendering and asset ids validated by the backend.

**Customers, vessels, documents, deadlines (spec 11.1–11.3, 11.5 basic)**
- Customers are people or companies. They have fiscal data (fiscal code, VAT, SdI code, PEC, billing address),
  nationality, locale, several labelled emails and phones, category, tags, default payment terms (stored only),
  an encrypted IBAN, notes and versioned consents. Search covers name, fiscal code, VAT, phone, email and vessel
  name or registration. Suspected duplicates are flagged. Customers can be merged (with audit), archived, exported
  for GDPR (JSON) and anonymised.
- Vessels have registry data, `hull_type`, declared and measured dimensions (`dimensions_source`), engines, fuel,
  required power and a computed legal unit class. Parties (`owner` with share in millis, `operator`, `skipper`,
  `billing`, `contact`) link vessels to customers.
- Documents for customers and vessels have a type (11.3), number (encrypted), issue and expiry dates, and an
  optional file (PDF, PNG or JPEG, encrypted at rest). Files are served only through short-lived signed URLs to
  holders of `customers.read`. Document data is entered by the operator; Porto has no AI
  extraction (ADR 0008).
- **Basic deadlines:** the `deadlines` table is fed in M1 by document expiries (identity documents, licences,
  insurance policies and so on), in the same transaction as the document write. There is a list and filter page,
  per-kind lead days (`deadline_policies`) that drive the "due soon" status, and manual done or dismiss.
- The customer page (11.4, M1 subset) shows data, vessels with roles, documents, consents, deadlines, plus the
  merge, export and anonymise actions.

**Platform additions:** a new permission `harbor.configure`, new event types and SSE topics, new problem slugs, an
encrypted file store under `PORTO_DATA_DIR/files`, files included in `porto backup` / `restore`, per-operation body
limits and binary request bodies, and an extended `porto seed demo`.

### 1.2 Explicitly out of M1

| Out | Milestone | Note for M1 code |
|---|---|---|
| Operational map view, date selector, contexts and lenses (9.1–9.2), *Schema* tiles, arrivals and departures boxes, `GET /maps/{id}/day` | M3 | The M1 viewer is read-only geometry. The shared `HarborMap` renderer is built so M3 can add lenses. |
| Occupancy engine, allocations, `*_occupancies` tables, holds, `fit` (10), drag and drop moves, releases, presences | M3 | `OccupancyGuard` interface only. INV-01/03/05..09 stay `planned`. |
| Vessel silhouettes on the map (7.8, `vessel-shape.ts` and its Go mirror) | M3 | `hull_type` is stored on vessels now. |
| Price lists, items, pricing engine (12) | M2 | `price_class` is a label. Rule and bands live in `harbor_settings`. |
| Quotes, contracts, transits, renewals, waitlist (13) | M3/M4 | |
| Berth rights, transfers, mandates, management fees, common charges (14) | M4 | |
| Templates, PDF, signatures (15) | M4 | Section 22 (AI extraction) is dropped by ADR 0008: no `extracted*` columns. |
| Deadline notifications (email, panel badges), deadlines from contracts, installments, rights and maintenance, the insurance-blocking policy (11.5) | M4 | `deadlines.notified_at` exists and stays `[]`. |
| CSV/Excel import of customers, vessels, contracts, rights, deadlines and opening balances (11.6) | M4 | M1 imports **berths only** (8.7), CSV only (no Excel). |
| Customer dashboard financial parts: contracts, outstanding balances, installments, credits, payments, communications, work orders, rentals (11.4) | M3–M5 | |
| Charges, payments, cash, export to invoicing (16) | M5 | Anonymisation in M1 has no fiscal snapshots to keep. |
| Storage scenarios, accessibility, optimizer (17) | M9 | M1 stores `blocked_cells` and `exits` only. |
| Yard (18), rental (19), maintenance and meter readings (20) | M9–M11 | Pedestal `meters` are stored data only. |
| Reports (21), online booking (23), sensors (24), public profile (25) | M7–M12 | |
| Document retention cleanup job for transits (30) | M3 | `documents.retention_until` exists and nothing purges it yet. |
| Desktop edition, phone offline queue (31) | M6 | |
| Custom roles | after v1 | |
| Playwright frame-time acceptance at 3 000 elements | M3 acceptance | M1 ships the spec gated by `PORTO_E2E_PERF=1` (§8.6). |

---

## 2. Domain model and pure packages

### 2.1 Package layout (new in M1)

```
internal/domain/layout/          PURE: element model, ValidateMap, Footprint, area cells, generators, georef,
                                 price-class rule, measure helpers, kit asset ids (generated)
internal/domain/harbor/          service: maps CRUD, document load/save, validate, generate (preview), background
                                 upload, export/import, berth CSV import, groups, harbour settings, OccupancyGuard
internal/domain/customer/        customers, consents, search, duplicates, merge, anonymise, GDPR export, fiscal checks
internal/domain/vessel/          vessels, parties, measurements, unit class
internal/domain/document/        documents, files (via filestore), signed URLs
internal/domain/deadline/        deadlines (sync from documents), policies, list
internal/filestore/              encrypted file store on disk (PORTO_DATA_DIR/files), shared by harbor and document
```

Dependency rules (depguard, `docs/m0-plan.md` §1.1):
- `layout` imports only the stdlib (`math/big` is allowed for georef), `internal/geo` and `internal/qty`.
- `harbor` imports `layout`, `store`, `filestore`, `audit`, `event` and `auth`.
- `document` imports `filestore`, `crypto`, `deadline` and `customer`/`vessel` (read helpers only; no cycles:
  `customer` and `vessel` never import `document`).
- `filestore` imports `crypto` and the stdlib only (no store: the `stored_files` rows are written by the callers'
  store functions).

### 2.2 `internal/domain/layout` (pure)

Normative shapes. Builders may add helpers but must not rename these or change their shapes without telling the
orchestrator. The TS mirror (§5.6) uses the same names in camelCase.

```go
package layout

// Ref identifies an element inside one document: the UUID of a saved element or the client key of a new one.
type Ref string

type StructureKind string // quay, pier, finger, breakwater, channel, walkway
type Side string          // left, right, head (berth/area side relative to the structure direction); none
type MooringType string   // mooring_line, finger, alongside, piles, buoy
type AreaKind string      // linear, surface
type MooringMode string   // stern_to, alongside (linear areas)
type LandKind string      // parking, box, storeroom, locker, cradle, trailer_spot, custom
type DecorationKind string// symbol, text, depth_label, north_arrow, scale_bar

type DepthSpan struct{ From, To, Depth geo.Cm }      // along the structure axis

type Structure struct {
    Ref      Ref
    Kind     StructureKind
    Code     string
    Points   []geo.Point   // quay/pier/walkway/breakwater-as-line: polyline; channel/breakwater-as-polygon: polygon
    Closed   bool          // true = polygon
    Width    geo.Cm        // pier, walkway, breakwater line; 0 otherwise
    Rect     *geo.Rect     // finger only (rotated rectangle attached to a pier side)
    Parent   Ref           // finger -> pier; pier -> root quay (optional)
    Sides    []Side        // pier: usable sides; quay: mooring side (exactly one of left/right)
    Floating bool
    Depth    []DepthSpan
}

type Berth struct {
    Ref             Ref
    Code            string
    Structure       Ref          // optional (warning if empty)
    Side            Side
    Position        int64        // order along the structure, for sorting and renumbering
    Rect            geo.Rect     // anchor = midpoint of the mooring side; for buoys Anchor = centre
    Mooring         MooringType
    SwingRadius     geo.Cm       // buoys only, > 0
    MaxLOA, MaxBeam geo.Cm       // defaults: Length, Width - 2*margin (filled by the service before validation)
    Depth           geo.Cm       // 0 = not given (warning)
    MaxAirDraft     geo.Cm       // 0 = no limit
    OverhangAllowed bool
    Pedestal        Ref
    PedestalSocket  string
    PriceClass      string
    Active          bool
}

type DynamicArea struct {
    Ref          Ref
    Code         string
    Kind         AreaKind
    Structure    Ref; Side Side; From, To geo.Cm   // linear
    Mode         MooringMode                        // linear
    Outline      geo.Polygon                        // surface
    GridRotation geo.Angle                          // surface; multiple of 900 in M1 (spec 7.5)
    Cell, Margin geo.Cm
    MaxLOA, MaxBeam, MaxHeight geo.Cm
    Blocked      []int                              // surface cell indexes
    Exits        []int                              // surface cell indexes
}

type LandResource struct{ Ref Ref; Code string; Kind LandKind; Rect geo.Rect }   // Anchor = midpoint of the access side
type Socket struct{ Code string; Kind string /* power|water */; Amps int64; Phase string }
type Pedestal struct{ Ref Ref; Code string; At geo.Point; Sockets []Socket }
type Decoration struct{ Ref Ref; Kind DecorationKind; Asset string; At geo.Point; Rotation geo.Angle; Size geo.Cm; Points []geo.Point }

type Map struct {
    Width, Height, Grid geo.Cm
    Water, Land         []geo.Polygon
    Structures          []Structure
    Berths              []Berth
    Areas               []DynamicArea
    LandResources       []LandResource
    Pedestals           []Pedestal
    Decorations         []Decoration
}

type Settings struct {
    LOATolerance     geo.Cm // default 50 (harbor_settings.loa_tolerance_cm)
    OverlapTolerance geo.Cm // fixed 1 (spec 8.4)
    MaxElements      int    // fixed 20 000 per map
}

type Severity string // error, warning
type Issue struct {
    Code     string            // §2.3
    Severity Severity
    Kind     string            // element kind of Refs[0]
    Refs     []Ref
    Field    string            // optional, e.g. "max_loa_cm"
    Params   map[string]int64  // numbers only (cm, counts): never names or free text
}
type Report struct{ Errors, Warnings []Issue }      // deterministic order: by code, then by first Ref
func (r Report) OK() bool
func ValidateMap(m Map, s Settings) Report
func ValidAsset(id string) bool                    // from kitassets_gen.go (§2.7)
```

**Measure helpers** (`layout/measure.go`, integers only): `ISqrt(n uint64) uint64` (floor);
`PolylineLength(pts) geo.Cm` (sum of floor-sqrt segment lengths, half-even per segment via
`ISqrt(4·d²)`); `PointAt(pts, s geo.Cm) (geo.Point, geo.Angle, error)` (point at distance `s` along a polyline
and direction of its segment); `AngleOf(dx, dy int64) geo.Angle`. `AngleOf` returns the nearest tenth of a
degree, found by binary search over `geo.Sin`/`geo.Cos` and comparing cross products, with no `math.Atan2`.
`ShrinkRect(r, d)` shrinks a rect by `d` on every side, keeping its centre.

### 2.3 `ValidateMap` rules (spec 8.4)

It runs over the whole document, which is the complete post-save state of that map (§3.3). The checks are exact
integer predicates from `internal/geo`. Pairwise checks are bucketed through a uniform grid of 1 000 cm over
element bounds, so 3 000 elements validate in under 50 ms. A benchmark (`BenchmarkValidateMap_3000`) guards this.

| Code | Severity | Rule |
|---|---|---|
| `too_many_elements` | error | more than `MaxElements` elements |
| `invalid_geometry` | error | rect `Validate` fails, polygon not simple, polyline < 2 points, buoy without radius, rotation out of range, surface grid rotation not a multiple of 900 |
| `element_outside_map` | error | any element bound (berth rect or buoy circle bounds, area, land resource, structure, pedestal) not inside `[0,Width]×[0,Height]` (the spec names buoys; M1 applies it to every element) |
| `duplicate_code` | error | two elements of the same family (structures, berths, areas, land resources, pedestals) with the same code; berths, areas and land resources share one code space |
| `berth_overlap` | error | two non-buoy berths whose `ShrinkRect(rect, 1)` interiors overlap (`geo.RectsOverlap`); sharing a side is not an overlap |
| `occupiable_overlap` | error | a non-buoy berth or land resource overlapping another land resource, or a linear area's occupiable band / surface outline (shrunk by 1 cm) overlapping a non-buoy berth, a land resource or another area |
| `area_cells_outside_support` | error | linear: `From < 0`, `To > PolylineLength(structure)`, `From >= To`, side not usable on the structure, structure missing; surface: outline outside the map, `Blocked`/`Exits` index ≥ cell count, no usable cell |
| `loa_exceeds_length` | error | `MaxLOA > Length + LOATolerance` and not `OverhangAllowed` |
| `dangling_reference` | error | `Structure`/`Parent`/`Pedestal` ref not found in the document, or of the wrong kind; finger parent not a pier |
| `pedestal_socket_unknown` | error | `PedestalSocket` not a socket code of the referenced pedestal |
| `asset_unknown` | error | decoration `symbol` with `!ValidAsset(Asset)` |
| `berth_outside_water` | warning | water polygons exist and the berth rect is not inside any (`Polygon.Contains` on all corners) |
| `berth_unattached` | warning | non-buoy berth without `Structure`, or whose mooring side is farther than 50 cm from the structure edge it references |
| `depth_missing` | warning | berth `Depth == 0` and the structure depth profile does not cover its position |
| `price_class_missing` | warning | berth without `PriceClass` |
| `buoy_swing_overlap` | warning | two buoy circles overlap (`geo.CirclesOverlap`) |
| `buoy_berth_overlap` | warning | a buoy circle intersects a non-buoy berth rect |
| `pedestal_unlinked` | warning | pedestal not referenced by any berth |

The linear area band is the rectangle along the support segment `[From,To]`, on its side, with depth
`MaxBeam` for `stern_to` or `MaxBeam + Margin` for `alongside`. It is built from `PointAt`.

### 2.4 Footprint and area cells (spec 7.4, 7.5, ADR 0003)

```go
type Vessel struct{ LOA, Beam geo.Cm }
type Placement struct {
    X, Y     geo.Cm     // linear: X = position along the axis from From (Y unused); surface: top-left corner in grid space
    Rotation geo.Angle  // surface: 0, 900, 1800, 2700 relative to the grid; linear: unused
}
type Area struct {          // the footprint view of a DynamicArea (built by AreaOf(DynamicArea, Map))
    Kind     AreaKind
    Length   geo.Cm         // linear: To - From
    Mode     MooringMode
    Cell, Margin geo.Cm
    Cols, Rows int          // surface grid size (from the outline bounds in grid space, ceil)
    Usable   []bool         // surface: cell fully inside the outline and not blocked (len = Cols*Rows)
}
type Cells struct{ First, Last int; Indexes []int }   // linear: First..Last inclusive; surface: sorted Indexes
var ErrUnsnapped, ErrOutsideArea, ErrBlockedCell, ErrInvalidVessel error
func Footprint(v Vessel, p Placement, a Area) (Cells, error)
func AreaOf(d DynamicArea, m Map) (Area, error)
func CellCount(a Area) int                              // linear: ceil(Length/Cell); surface: Cols*Rows
```

- Linear, `stern_to`: interval `[x, x + beam + margin)`. `alongside`: `[x, x + loa + margin)`. `x` must be a
  multiple of 10 (`ErrUnsnapped`). It must also satisfy `x ≥ margin/2` (integer division) and
  `x + w ≤ Length` (`ErrOutsideArea`). Cells run from `⌊x/c⌋` to `⌈(x+w)/c⌉ − 1`, rounding up so that every touched
  cell counts.
- Surface: the rectangle is `loa + margin` along the rotation axis × `beam + margin`, and the rotation is a multiple
  of 900. Cells are all those intersected (same floor/ceil per axis), and the index is `row*Cols + col`. A cell
  that is not `Usable` gives `ErrBlockedCell` when it is blocked and `ErrOutsideArea` otherwise.
- `AreaOf` rotates the outline by `-GridRotation` around the outline's first vertex. Grid space has its origin at
  the minimum of the rotated bounds. A cell is usable when its four corners are inside the rotated outline
  (inclusive) and its index is not in `Blocked`.

M1 uses `Footprint` in validation (`AreaOf`, `CellCount`), in the editor preview of cells, and in the property
and parity tests. Allocations start using it in M3.

### 2.5 Generators (spec 8.3)

Spec 8.3 names `internal/domain/harbor/generator.go`. The pure algorithms live in `layout/generator*.go`, so that
depguard enforces purity. `harbor/generator.go` is the thin service method that authorises, loads the current
settings and runs the preview (§11 D3).

```go
type Generated struct {
    Structures []Structure; Berths []Berth; Areas []DynamicArea; LandResources []LandResource
    Pedestals []Pedestal; Decorations []Decoration
    Warnings []Issue
}
type Numbering struct {
    Scheme  string // continuous | odd_left_even_right | per_side
    Prefix  string // e.g. "B-"; per_side adds "L"/"R"/"H" after the prefix
    Start   int64  // default 1
    Pad     int    // zero padding 0..3
}
type PierParams struct {
    KeyPrefix       string                 // client-chosen, keys are KeyPrefix + "s1", "b1", "f1", ...
    Origin          geo.Point              // root of the pier (on the quay)
    Rotation        geo.Angle              // direction from root to head
    Length, Width   geo.Cm
    Floating        bool
    Code            string                 // pier code, e.g. "B"
    Root            Ref                    // optional quay ref
    Sides           []Side                 // left, right (head handled by HeadBerths)
    BerthsPerSide   int                    // 0 = fill the length
    StartOffset     geo.Cm                 // free length at the root before the first berth (default 0)
    BerthWidth, BerthLength       geo.Cm
    BerthWidthEnd, BerthLengthEnd geo.Cm   // 0 = constant; else linear taper towards the head (half-even per index)
    FingerEvery     int                    // 0 = mooring line, no fingers
    FingerWidth, FingerLength geo.Cm       // defaults 60 and BerthLength*3/4 (half-even)
    HeadBerths      int                    // 0..4, across the head
    Mooring         MooringType            // default mooring_line, or finger when FingerEvery > 0
    Numbering       Numbering
    Depth           geo.Cm
    PriceRule       PriceClassRule         // passed in by the service from harbor_settings
}
func GeneratePier(p PierParams) (Generated, error)
type QuayBerthsParams struct{ KeyPrefix string; Quay Structure; From, To geo.Cm; Side Side; BerthWidth geo.Cm; Count int;
    BerthLength geo.Cm; Mooring MooringType; Numbering Numbering; PriceRule PriceClassRule }   // Count 0 = fill by width
func GenerateQuayBerths(p QuayBerthsParams) (Generated, error)
type LinearAreaParams struct{ KeyPrefix, Code string; Structure Structure; Side Side; From, To geo.Cm; Mode MooringMode;
    Cell, Margin, MaxLOA, MaxBeam geo.Cm }
func GenerateLinearArea(p LinearAreaParams) (Generated, error)
type BuoyFieldParams struct{ KeyPrefix string; Outline geo.Polygon; SwingRadius geo.Cm; Layout string /* grid|staggered */;
    Spacing geo.Cm /* default 2*SwingRadius */; MaxLOA geo.Cm; Numbering Numbering; PriceRule PriceClassRule }
func GenerateBuoyField(p BuoyFieldParams) (Generated, error)
type LandGridParams struct{ KeyPrefix string; Origin geo.Point; Rotation geo.Angle; Rows, Cols int; ItemWidth, ItemLength geo.Cm;
    Aisle geo.Cm; AisleEvery int; Kind LandKind; Numbering Numbering }
func GenerateLandGrid(p LandGridParams) (Generated, error)
type SurfaceParams struct{ KeyPrefix, Code string; Outline geo.Polygon; Cell geo.Cm; GridRotation geo.Angle; Exits []geo.Point;
    Lanes []Lane /* {From, To geo.Point; Width geo.Cm} -> blocked cells */; Margin, MaxLOA, MaxHeight geo.Cm }
func GenerateSurfaceArea(p SurfaceParams) (Generated, error)

type PriceClassRule struct{ Basis string /* width */; Bands []PriceBand /* sorted by MaxCm */; Fallback string }
type PriceBand struct{ MaxCm geo.Cm; Class string }
func PriceClassFor(width geo.Cm, r PriceClassRule) string
func DefaultPriceClassRule() PriceClassRule   // width ≤300 L8, ≤350 L10, ≤400 L12, ≤450 L14, ≤500 L16, ≤550 L18, ≤650 L20, else L25
```

Pier geometry: the axis runs from `Origin` along `Rotation`. Left is the direction rotated by −900 (counter-clockwise
on screen), and right is rotated by +900. A left berth `i` has its anchor on the left edge (axis offset `Width/2`),
at the middle of its slot along the axis, with `Rotation − 900`. Right berths mirror this. With fingers, each
finger occupies `FingerWidth` along the axis between slots, after every `FingerEvery` berths, and gets code
`<pier>-F<n>`. Head berths sit across the head with the pier rotation. "Fill the length" places as many slots as
fit in `Length − StartOffset` (minus head clearance when there are head berths). Numbering:
- `continuous`: left 1..n, then right n+1..2n, then head.
- `odd_left_even_right`: 1, 3, 5… on the left and 2, 4, 6… on the right, from the root.
- `per_side`: `B-L1…`, `B-R1…`, `B-H1…`.

Every generated berth gets `Position` = its slot index × 10 (room for insertions) and `PriceClass =
PriceClassFor(width, rule)`. Generators never produce overlapping elements among themselves. An error comes only
from bad parameters (`ErrBadParams` wrapping a `ValidationError{Fields}` with `field`/`code`, mapped to
`validation_failed`). A result above 2 000 elements is `ErrBadParams` (`too_many`).

### 2.6 Georeferencing (spec 7.1)

```go
type ControlPoint struct{ At geo.Point; LatE7, LonE7 int64 }   // WGS84 × 1e7, as marinas.geo
type Georef struct{ /* affine coefficients as *big.Rat, unexported */ }
func NewGeoref(cps [3]ControlPoint) (Georef, error)            // ErrCollinear when the three map points are collinear
func (g Georef) ToWGS84(p geo.Point) (latE7, lonE7 int64)      // half-even
func (g Georef) FromWGS84(latE7, lonE7 int64) geo.Point        // half-even, cm
func FormatE7(v int64) string                                   // "12.3456789" exactly, for GeoJSON numbers (json.Number)
```

GeoJSON export and concession import use these helpers. Floats are not used anywhere. GeoJSON numbers are
written as `json.Number` from `FormatE7`, and imported numbers are parsed as decimals into `E7` (more than 7
decimals is rounded half-even).

### 2.7 Illustration kit (spec 7.7)

- `assets/harbor-kits/porto-basic/manifest.json`: `{"format":"porto-harbor-kit/1","id":"porto-basic","assets":[{"id","category","label_key"}]}`
  with ids: `harbour_office, fuel_station, toilets, showers, laundry, travel_lift, crane, slipway, light_red,
  light_green, lighthouse, fire_hydrant, waste_collection, waste_oil, pump_out, parking, entrance, bar,
  restaurant`. Categories are `services`, `operations`, `navigation`, `safety` and `environment`.
  `svg/<id>.svg` holds original Porto symbols (`viewBox="0 0 100 100"`, simple geometric line icons using only
  `currentColor`, no text). `NOTICE.md` states © Coste360 SRL, the same licence as the repository.
- `web/scripts/gen-harbor-kit.mjs` uses Node built-ins only and runs as part of `npm --prefix web run gen`. It
  checks that the manifest and files match and that the viewBox is right. It sanitises with an allowlist of
  elements (`g`, `path`, `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`) and of geometric and
  presentation attributes (`fill`/`stroke` only `none` or `currentColor`). It fails on anything else, including
  `style`, `script`, `use`, `image`, `href`, `on*`, `url(`, hex colours and text. It writes
  `web/shared/src/map/kit/generated/{manifest,symbols}.ts`, where symbols are a JSON element tree that a small
  React renderer turns into `<symbol>` elements, so `dangerouslySetInnerHTML` is never used.
- `internal/domain/layout/internal/genkit/main.go` (`//go:generate go run ./internal/genkit` in `layout/kit.go`)
  reads the manifest and writes `layout/kitassets_gen.go`, with `var kitAssets = map[string]struct{}{…}`
  for `ValidAsset`.
- `make gen` runs both. `gen-check` covers both outputs.

### 2.8 Go ↔ TypeScript parity (spec 32.1)

- `testdata/footprint/cases.json` (repo root): `{"version":1,"cases":[{"name","area":{kind,length_cm,mode,cell_cm,
  margin_cm,cols,rows,usable:[bool]},"vessel":{"loa_cm","beam_cm"},"placement":{"x_cm","y_cm","rotation_dd"},
  "expect":{"cells":{"first","last"}|{"indexes":[…]}}|{"error":"unsnapped|outside_area|blocked_cell|invalid_vessel"}}]}`.
  It holds at least 40 hand-written edge cases: the margin at the start, a cell boundary hit exactly, the last
  cell, an odd margin, each surface rotation, a blocked cell, a cell outside the outline, a zero beam. It also
  holds 400 seeded random cases written by `go run ./internal/domain/layout/internal/genvectors -seed 1 -n 400`.
  The generator is deterministic and its output is committed. `TestFootprint_VectorsUpToDate` regenerates the
  random part in memory and fails on drift.
- `testdata/footprint/area-cells.json`: cases for `AreaOf`/`CellCount` (outline, rotation, blocked) → `{cols, rows,
  usable}`.
- The Go test `TestFootprint_ParityVectors` and the Vitest test `web/shared/src/map/footprint.parity.test.ts` both
  read the same files (Vitest through `node:fs`, path resolved from the repo root). A change on one side updates
  both and the cases (`.claude/rules/geometry.md`).

---

## 3. Data model

### 3.1 New migrations (both dialects, same numbers)

| File | Content |
|---|---|
| `internal/migrations/{sqlite,postgres}/00002_harbor.sql` | `stored_files`, `harbor_settings`, `maps`, `harbor_structures`, `pedestals`, `berths`, `dynamic_areas`, `land_resources`, `map_decorations`, `berth_groups`, `berth_group_members` |
| `internal/migrations/{sqlite,postgres}/00003_registry.sql` | `customers`, `consents`, `vessels`, `vessel_parties`, `documents`, `deadlines`, `deadline_policies` |

`00001` is never edited. The conventions are those of `docs/m0-plan.md` §2 and `.claude/rules/migrations.md`. Lengths
are `INTEGER` (SQLite) / `bigint` (PG) cm. JSON is `TEXT` / `jsonb`. Booleans are `INTEGER CHECK (x IN (0,1))` /
`boolean`. Dates are `DATE` text / `date`. Ids are `TEXT` / `uuid`. Every element table carries `marina_id`, plus
a composite FK to its parent `(id, marina_id)` so that a row can never point to another marina. Down drops in
reverse order. The DDL below is authoritative in SQLite form, and PostgreSQL is its mechanical translation.

```sql
-- 00002_harbor.sql
-- +goose Up
CREATE TABLE stored_files (                       -- encrypted blobs under PORTO_DATA_DIR/files (internal/filestore)
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    purpose            TEXT NOT NULL CHECK (purpose IN ('map_background', 'document')),
    content_type       TEXT NOT NULL CHECK (content_type IN ('image/png','image/jpeg','image/svg+xml','application/pdf')),
    size_bytes         INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 15728640),
    sha256             TEXT NOT NULL,             -- of the plaintext
    storage_key        TEXT NOT NULL UNIQUE,      -- relative path "<marina_id>/<id>.bin"
    width_px           INTEGER,                   -- images only
    height_px          INTEGER,
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    deleted_at         TIMESTAMP,                 -- set, then the blob is removed after commit
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE INDEX stored_files_marina_id_idx ON stored_files (marina_id, purpose);

CREATE TABLE harbor_settings (
    id                TEXT PRIMARY KEY,
    marina_id         TEXT NOT NULL UNIQUE REFERENCES marinas (id) ON DELETE CASCADE,
    price_class_rule  TEXT NOT NULL,              -- layout.PriceClassRule JSON; default inserted lazily by the service
    loa_tolerance_cm  INTEGER NOT NULL DEFAULT 50 CHECK (loa_tolerance_cm BETWEEN 0 AND 500),
    default_grid_cm   INTEGER NOT NULL DEFAULT 50 CHECK (default_grid_cm BETWEEN 1 AND 10000),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);

CREATE TABLE maps (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('water','yard','shed','parking','boxes','other')),
    name                TEXT NOT NULL,            -- I18nText JSON {"it": "...", "en": "..."}; it required
    icon                TEXT NOT NULL DEFAULT 'map',
    width_cm            INTEGER NOT NULL CHECK (width_cm BETWEEN 100 AND 500000),
    height_cm           INTEGER NOT NULL CHECK (height_cm BETWEEN 100 AND 500000),
    grid_cm             INTEGER NOT NULL DEFAULT 50 CHECK (grid_cm BETWEEN 1 AND 10000),
    background_file_id  TEXT,
    background          TEXT,                     -- MapBackground JSON (calibration), NULL = none
    georef              TEXT,                     -- {"points":[{x_cm,y_cm,lat_e7,lon_e7} x3]}, NULL = none
    concession_outline  TEXT,                     -- polygon JSON in cm, NULL = none
    water_polygons      TEXT NOT NULL DEFAULT '[]',
    land_polygons       TEXT NOT NULL DEFAULT '[]',
    sort_order          INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (background_file_id, marina_id) REFERENCES stored_files (id, marina_id) ON DELETE SET NULL
);
CREATE INDEX maps_marina_sort_idx ON maps (marina_id, sort_order);
-- PostgreSQL: ON DELETE SET NULL on a composite FK would also null marina_id; PG 15+ syntax
-- "ON DELETE SET NULL (background_file_id)" is used there. SQLite has no column list: stored_files rows are only
-- soft-deleted (deleted_at), never hard-deleted while referenced, so the action never fires on SQLite.

CREATE TABLE harbor_structures (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL,
    map_id      TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('quay','pier','finger','breakwater','channel','walkway')),
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL DEFAULT '',
    geometry    TEXT NOT NULL,                    -- {"points":[...],"closed":bool} | {"rect":{anchor,width,length,rotation_dd}}
    width_cm    INTEGER CHECK (width_cm IS NULL OR width_cm > 0),
    parent_id   TEXT REFERENCES harbor_structures (id) ON DELETE SET NULL,
    attributes  TEXT NOT NULL DEFAULT '{}',       -- {sides[], floating, depth_profile[], elevation_cm, bollard_spacing_cm, min_depth_cm}
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX harbor_structures_map_code_uidx ON harbor_structures (map_id, code);
CREATE INDEX harbor_structures_marina_map_idx ON harbor_structures (marina_id, map_id);

CREATE TABLE pedestals (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL,
    map_id      TEXT NOT NULL,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    x_cm        INTEGER NOT NULL,
    y_cm        INTEGER NOT NULL,
    sockets     TEXT NOT NULL DEFAULT '[]',       -- [{code, kind: power|water, amps, phase}]
    meters      TEXT NOT NULL DEFAULT '[]',       -- [{kind: power|water, serial}]
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX pedestals_map_code_uidx ON pedestals (map_id, code);
CREATE INDEX pedestals_marina_map_idx ON pedestals (marina_id, map_id);

CREATE TABLE berths (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL,
    map_id               TEXT NOT NULL,
    structure_id         TEXT REFERENCES harbor_structures (id) ON DELETE SET NULL,
    code                 TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    side                 TEXT NOT NULL DEFAULT 'none' CHECK (side IN ('left','right','head','none')),
    position             INTEGER NOT NULL DEFAULT 0,
    anchor_x_cm          INTEGER NOT NULL,
    anchor_y_cm          INTEGER NOT NULL,
    rotation_dd          INTEGER NOT NULL CHECK (rotation_dd BETWEEN 0 AND 3599),
    width_cm             INTEGER NOT NULL CHECK (width_cm > 0),
    length_cm            INTEGER NOT NULL CHECK (length_cm > 0),
    max_loa_cm           INTEGER NOT NULL CHECK (max_loa_cm > 0),
    max_beam_cm          INTEGER NOT NULL CHECK (max_beam_cm > 0),
    depth_cm             INTEGER CHECK (depth_cm IS NULL OR depth_cm > 0),
    max_air_draft_cm     INTEGER CHECK (max_air_draft_cm IS NULL OR max_air_draft_cm > 0),
    max_displacement_kg  INTEGER CHECK (max_displacement_kg IS NULL OR max_displacement_kg > 0),
    mooring_type         TEXT NOT NULL CHECK (mooring_type IN ('mooring_line','finger','alongside','piles','buoy')),
    swing_radius_cm      INTEGER CHECK (swing_radius_cm IS NULL OR swing_radius_cm > 0),
    overhang_allowed     INTEGER NOT NULL DEFAULT 0 CHECK (overhang_allowed IN (0,1)),
    pedestal_id          TEXT REFERENCES pedestals (id) ON DELETE SET NULL,
    pedestal_socket      TEXT,
    power_a              INTEGER CHECK (power_a IS NULL OR power_a IN (16,32,63,125)),
    power_phase          TEXT CHECK (power_phase IS NULL OR power_phase IN ('single','three')),
    water                INTEGER NOT NULL DEFAULT 0 CHECK (water IN (0,1)),
    wifi                 INTEGER NOT NULL DEFAULT 0 CHECK (wifi IN (0,1)),
    price_class          TEXT,
    attributes           TEXT NOT NULL DEFAULT '[]',   -- tags: transit_only, superyacht, accessible, near_services, ...
    online_sellable      INTEGER NOT NULL DEFAULT 0 CHECK (online_sellable IN (0,1)),
    active               INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    CHECK (mooring_type <> 'buoy' OR swing_radius_cm IS NOT NULL)
);
CREATE UNIQUE INDEX berths_map_code_uidx ON berths (map_id, code);
CREATE INDEX berths_marina_map_idx ON berths (marina_id, map_id);
CREATE INDEX berths_structure_idx ON berths (structure_id, position);

CREATE TABLE dynamic_areas (
    id                TEXT PRIMARY KEY,
    marina_id         TEXT NOT NULL,
    map_id            TEXT NOT NULL,
    kind              TEXT NOT NULL CHECK (kind IN ('linear','surface')),
    code              TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    structure_id      TEXT REFERENCES harbor_structures (id) ON DELETE SET NULL,
    side              TEXT CHECK (side IS NULL OR side IN ('left','right','head')),
    from_cm           INTEGER,
    to_cm             INTEGER,
    outline           TEXT,                       -- polygon JSON (surface)
    mooring_mode      TEXT CHECK (mooring_mode IS NULL OR mooring_mode IN ('stern_to','alongside')),
    cell_cm           INTEGER NOT NULL CHECK (cell_cm BETWEEN 10 AND 1000),
    margin_cm         INTEGER NOT NULL CHECK (margin_cm BETWEEN 0 AND 1000),
    max_loa_cm        INTEGER CHECK (max_loa_cm IS NULL OR max_loa_cm > 0),
    max_beam_cm       INTEGER CHECK (max_beam_cm IS NULL OR max_beam_cm > 0),
    max_height_cm     INTEGER CHECK (max_height_cm IS NULL OR max_height_cm > 0),
    max_weight_kg     INTEGER CHECK (max_weight_kg IS NULL OR max_weight_kg > 0),   -- per cell
    supports          TEXT NOT NULL DEFAULT '[]', -- cradle, trailer, rack
    depth_profile     TEXT NOT NULL DEFAULT '[]',
    blocked_cells     TEXT NOT NULL DEFAULT '[]',
    exits             TEXT NOT NULL DEFAULT '[]',
    grid_rotation_dd  INTEGER NOT NULL DEFAULT 0 CHECK (grid_rotation_dd IN (0, 900, 1800, 2700)),
    cell_count        INTEGER NOT NULL CHECK (cell_count > 0),   -- computed by layout.CellCount at save
    price_class       TEXT,
    active            INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    CHECK (kind <> 'linear' OR (from_cm IS NOT NULL AND to_cm IS NOT NULL AND from_cm < to_cm AND mooring_mode IS NOT NULL AND side IS NOT NULL)),
    CHECK (kind <> 'surface' OR outline IS NOT NULL)
);
CREATE UNIQUE INDEX dynamic_areas_map_code_uidx ON dynamic_areas (map_id, code);
CREATE INDEX dynamic_areas_marina_map_idx ON dynamic_areas (marina_id, map_id);

CREATE TABLE land_resources (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL,
    map_id       TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('parking','box','storeroom','locker','cradle','trailer_spot','custom')),
    code         TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    x_cm         INTEGER NOT NULL,                -- anchor (midpoint of the access side)
    y_cm         INTEGER NOT NULL,
    rotation_dd  INTEGER NOT NULL CHECK (rotation_dd BETWEEN 0 AND 3599),
    width_cm     INTEGER NOT NULL CHECK (width_cm > 0),
    length_cm    INTEGER NOT NULL CHECK (length_cm > 0),
    price_class  TEXT,
    attributes   TEXT NOT NULL DEFAULT '[]',
    active       INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX land_resources_map_code_uidx ON land_resources (map_id, code);
CREATE INDEX land_resources_marina_map_idx ON land_resources (marina_id, map_id);

CREATE TABLE map_decorations (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL,
    map_id      TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('symbol','text','depth_label','north_arrow','scale_bar')),
    asset       TEXT,                             -- kit id when kind = symbol (validated against layout.ValidAsset)
    variant     TEXT,
    geometry    TEXT NOT NULL,                    -- {x_cm,y_cm,rotation_dd,size_cm} or {points:[...]}
    label       TEXT NOT NULL DEFAULT '{}',       -- I18nText
    depth_cm    INTEGER,                          -- depth_label only
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE,
    CHECK (kind <> 'symbol' OR asset IS NOT NULL)
);
CREATE INDEX map_decorations_marina_map_idx ON map_decorations (marina_id, map_id);

CREATE TABLE berth_groups (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    code        TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 32),
    name        TEXT NOT NULL,                    -- I18nText
    kind        TEXT NOT NULL DEFAULT 'custom' CHECK (kind IN ('structure','transit','area','custom')),
    sort_order  INTEGER NOT NULL DEFAULT 0,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX berth_groups_marina_code_uidx ON berth_groups (marina_id, code);

CREATE TABLE berth_group_members (
    id             TEXT PRIMARY KEY,
    marina_id      TEXT NOT NULL,
    group_id       TEXT NOT NULL,
    map_id         TEXT NOT NULL,                 -- map of the member (for the per-map diff at save)
    resource_type  TEXT NOT NULL CHECK (resource_type IN ('berth','dynamic_area','land_resource')),
    resource_id    TEXT NOT NULL,                 -- polymorphic: removed in the save tx when the element is deleted
    created_at     TIMESTAMP NOT NULL,
    FOREIGN KEY (group_id, marina_id) REFERENCES berth_groups (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (map_id, marina_id) REFERENCES maps (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX berth_group_members_uidx ON berth_group_members (group_id, resource_type, resource_id);
CREATE INDEX berth_group_members_resource_idx ON berth_group_members (marina_id, resource_type, resource_id);
CREATE INDEX berth_group_members_map_idx ON berth_group_members (map_id);
```

```sql
-- 00003_registry.sql
-- +goose Up
CREATE TABLE customers (
    id               TEXT PRIMARY KEY,
    marina_id        TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind             TEXT NOT NULL CHECK (kind IN ('person','company')),
    first_name       TEXT NOT NULL DEFAULT '',
    last_name        TEXT NOT NULL DEFAULT '',
    company_name     TEXT NOT NULL DEFAULT '',
    fiscal_code      TEXT CHECK (fiscal_code IS NULL OR fiscal_code = upper(fiscal_code)),
    vat_number       TEXT,                        -- with optional ISO country prefix, upper case, no spaces
    sdi_code         TEXT,
    pec              TEXT CHECK (pec IS NULL OR pec = lower(pec)),
    billing_address  TEXT NOT NULL DEFAULT '{}',  -- Address JSON (same schema as organizations)
    nationality      TEXT CHECK (nationality IS NULL OR length(nationality) = 2),
    locale           TEXT NOT NULL DEFAULT 'it',
    emails           TEXT NOT NULL DEFAULT '[]',  -- [{label, value}] value lower case
    phones           TEXT NOT NULL DEFAULT '[]',  -- [{label, value}] value E.164 when parseable, else digits
    category         TEXT CHECK (category IS NULL OR category IN ('member','resident','affiliated','charter','yard')),
    tags             TEXT NOT NULL DEFAULT '[]',
    payment_terms    TEXT,                        -- {"days":30,"end_of_month":true}; used from M5
    iban_enc         TEXT,                        -- KeyRing, AAD "iban:<id>"
    iban_last4       TEXT,
    notes            TEXT NOT NULL DEFAULT '',
    search_text      TEXT NOT NULL DEFAULT '',    -- lower-case, accent-free: names, fiscal code, VAT, emails, phone digits
    merged_into_id   TEXT REFERENCES customers (id) ON DELETE SET NULL,
    anonymized_at    TIMESTAMP,
    archived_at      TIMESTAMP,
    version          INTEGER NOT NULL DEFAULT 1,
    created_at       TIMESTAMP NOT NULL,
    updated_at       TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    CHECK (kind <> 'person' OR length(last_name) >= 1 OR anonymized_at IS NOT NULL),
    CHECK (kind <> 'company' OR length(company_name) >= 1 OR anonymized_at IS NOT NULL)
);
CREATE INDEX customers_marina_name_idx ON customers (marina_id, last_name, first_name);
CREATE INDEX customers_marina_company_idx ON customers (marina_id, company_name);
CREATE INDEX customers_marina_fiscal_idx ON customers (marina_id, fiscal_code);
CREATE INDEX customers_marina_vat_idx ON customers (marina_id, vat_number);

CREATE TABLE consents (                           -- versioned, insert-only by API (no update operation)
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL,
    customer_id         TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('privacy','marketing','third_party')),
    policy_version      TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),
    granted             INTEGER NOT NULL CHECK (granted IN (0,1)),
    source              TEXT NOT NULL CHECK (source IN ('desk','online','import','api')),
    recorded_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    recorded_at         TIMESTAMP NOT NULL,
    created_at          TIMESTAMP NOT NULL,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX consents_marina_customer_idx ON consents (marina_id, customer_id, kind, recorded_at);

CREATE TABLE vessels (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                  TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    registration          TEXT,                   -- matricola / numero di iscrizione
    registry_office       TEXT,
    flag                  TEXT CHECK (flag IS NULL OR length(flag) = 2),
    home_port             TEXT,
    hull_type             TEXT NOT NULL DEFAULT 'other' CHECK (hull_type IN ('monohull_motor','monohull_sail','catamaran','trimaran','rib','gozzo','other')),
    model                 TEXT,
    builder               TEXT,
    year_built            INTEGER CHECK (year_built IS NULL OR year_built BETWEEN 1800 AND 2200),
    hull_color            TEXT,
    hull_material         TEXT,
    loa_cm                INTEGER NOT NULL CHECK (loa_cm > 0),
    lwl_cm                INTEGER CHECK (lwl_cm IS NULL OR lwl_cm > 0),
    beam_cm               INTEGER NOT NULL CHECK (beam_cm > 0),
    draft_cm              INTEGER CHECK (draft_cm IS NULL OR draft_cm > 0),
    air_draft_cm          INTEGER CHECK (air_draft_cm IS NULL OR air_draft_cm > 0),
    displacement_kg       INTEGER CHECK (displacement_kg IS NULL OR displacement_kg > 0),
    measured_loa_cm       INTEGER CHECK (measured_loa_cm IS NULL OR measured_loa_cm > 0),
    measured_beam_cm      INTEGER CHECK (measured_beam_cm IS NULL OR measured_beam_cm > 0),
    measured_draft_cm     INTEGER CHECK (measured_draft_cm IS NULL OR measured_draft_cm > 0),
    measured_air_draft_cm INTEGER CHECK (measured_air_draft_cm IS NULL OR measured_air_draft_cm > 0),
    measured_at           TIMESTAMP,
    measured_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    dimensions_source     TEXT NOT NULL DEFAULT 'declared' CHECK (dimensions_source IN ('declared','measured')),
    engines               TEXT NOT NULL DEFAULT '[]',  -- [{make, model, power_kw, power_hp, serial, hours}]
    fuel                  TEXT CHECK (fuel IS NULL OR fuel IN ('petrol','diesel','electric','hybrid','none','other')),
    power_required_a      INTEGER CHECK (power_required_a IS NULL OR power_required_a IN (16,32,63,125)),
    notes                 TEXT NOT NULL DEFAULT '',
    search_text           TEXT NOT NULL DEFAULT '',     -- lower-case name + registration
    archived_at           TIMESTAMP,
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    CHECK (dimensions_source = 'declared' OR (measured_loa_cm IS NOT NULL AND measured_beam_cm IS NOT NULL))
);
CREATE INDEX vessels_marina_name_idx ON vessels (marina_id, name);
CREATE INDEX vessels_marina_registration_idx ON vessels (marina_id, registration);

CREATE TABLE vessel_parties (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL,
    vessel_id     TEXT NOT NULL,
    customer_id   TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner','operator','skipper','billing','contact')),
    share_millis  INTEGER CHECK (share_millis IS NULL OR share_millis BETWEEN 1 AND 1000),
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE RESTRICT,
    CHECK ((role = 'owner') = (share_millis IS NOT NULL))
);
CREATE UNIQUE INDEX vessel_parties_uidx ON vessel_parties (vessel_id, customer_id, role);
CREATE INDEX vessel_parties_marina_customer_idx ON vessel_parties (marina_id, customer_id);

CREATE TABLE documents (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    owner_type            TEXT NOT NULL CHECK (owner_type IN ('customer','vessel')),
    customer_id           TEXT,
    vessel_id             TEXT,
    type                  TEXT NOT NULL CHECK (type IN ('identity_card','passport','driving_licence','boating_licence',
                              'navigation_licence','registration_certificate','insurance_policy','safety_certificate',
                              'power_declaration','signed_contract','proxy','photo','other')),
    number_enc            TEXT,                   -- KeyRing, AAD "document-number:<id>"
    issuer                TEXT,                   -- issuing office / insurer
    issued_on             DATE,
    expires_on            DATE,
    file_id               TEXT,
    file_name             TEXT,                   -- sanitised original name, max 120 chars
    notes                 TEXT NOT NULL DEFAULT '',
    retention_until       DATE,                   -- M3 cleanup job
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (file_id, marina_id) REFERENCES stored_files (id, marina_id),
    CHECK ((owner_type = 'customer' AND customer_id IS NOT NULL AND vessel_id IS NULL)
        OR (owner_type = 'vessel' AND vessel_id IS NOT NULL AND customer_id IS NULL)),
    CHECK (issued_on IS NULL OR expires_on IS NULL OR issued_on <= expires_on)
);
CREATE INDEX documents_marina_customer_idx ON documents (marina_id, customer_id);
CREATE INDEX documents_marina_vessel_idx ON documents (marina_id, vessel_id);
CREATE INDEX documents_marina_expires_idx ON documents (marina_id, expires_on);

CREATE TABLE deadlines (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    source_type  TEXT NOT NULL,                   -- 'document' in M1; contract, installment, right, maintenance later (validated in Go)
    source_id    TEXT NOT NULL,
    kind         TEXT NOT NULL,                   -- deadline.Kind (validated in Go, extended in later milestones)
    customer_id  TEXT,
    vessel_id    TEXT,
    due_on       DATE NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','dismissed')),
    note         TEXT NOT NULL DEFAULT '',
    notified_at  TEXT NOT NULL DEFAULT '[]',      -- M4
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (vessel_id, marina_id) REFERENCES vessels (id, marina_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX deadlines_source_uidx ON deadlines (source_type, source_id, kind);
CREATE INDEX deadlines_marina_status_due_idx ON deadlines (marina_id, status, due_on);

CREATE TABLE deadline_policies (
    id          TEXT PRIMARY KEY,
    marina_id   TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    lead_days   TEXT NOT NULL DEFAULT '[60,15]',  -- sorted desc, each 1..730
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX deadline_policies_marina_kind_uidx ON deadline_policies (marina_id, kind);
```

PostgreSQL notes: `length()` becomes `char_length()`, and `upper()`/`lower()` stay the same. For the
`ON DELETE SET NULL` composite FK `maps.background_file_id`, PG uses the column-list form
(`ON DELETE SET NULL (background_file_id)`). `harbor_structures.parent_id`, `berths.structure_id`, `berths.pedestal_id` and
`dynamic_areas.structure_id` are single-column FKs. Same-map integrity of those references is checked by the save
service (`dangling_reference`), because a FK cannot express "same map". The backup registry
(`store.BackupTables()`) appends, in this order: `stored_files, harbor_settings, maps, harbor_structures,
pedestals, berths, dynamic_areas, land_resources, map_decorations, berth_groups, berth_group_members, customers,
consents, vessels, vessel_parties, documents, deadlines, deadline_policies`. `customers.merged_into_id` is a
self-FK, so restore inserts with `merged_into_id` deferred (two passes, written in `internal/backup`).

### 3.2 Deadline kinds (M1)

`deadline.Kind` values come from the document type: `identity_document` (identity_card, passport,
driving_licence), `boating_licence`, `navigation_licence`, `registration_certificate`, `insurance`
(insurance_policy), `safety_certificate`, `power_declaration` and `other_document` (proxy, other). `photo` and
`signed_contract` create no deadline. A document with `expires_on` upserts its deadline in the same transaction
(`source_type='document'`). Changing `expires_on` updates `due_on` and reopens a `done` deadline. Removing
`expires_on` or deleting the document deletes the deadline. Default policies (inserted lazily) are `[60,15]` days
for insurance and `[30]` for the others. `urgency` is computed when read: `overdue` if `due_on < today` (marina
timezone, `clock`), `due_soon` if within `max(lead_days)`, else `later`.

### 3.3 How "no overlapping berths on a saved map" is guaranteed (INV-04)

The database cannot express rotated-rectangle overlap on both dialects. The guarantee therefore rests on four
things together: one write path, validation of the complete post-save state inside the same transaction, version
serialisation, and tests.

1. **Single write path.** Only `store.SaveMapElements(ctx, tx, scope, mapID, diff)` inserts, updates or deletes
   rows of `harbor_structures`, `pedestals`, `berths`, `dynamic_areas`, `land_resources` and `map_decorations`.
   Only `harbor.(*Service).applyDocument` calls it, and both `SaveMap` and the berth CSV import go through
   `applyDocument`. `TestHarbor_SingleWritePath` parses the repository with `go/parser` and fails if any other
   function calls it (seed included: the seed uses the service).
2. **Validate the complete state in the transaction.** The save request is the whole document of the map (full
   replacement), and overlap is intra-map (each map has its own coordinate space). So the validated document is
   exactly the state after commit. `applyDocument`:
   ```
   RunInTx:
     m := store.LockMap(tx, scope, mapID)              // PG: SELECT … FOR UPDATE; SQLite: immediate tx (write gate)
     if m.Version != expected  -> ErrVersionMismatch (412), nothing written
     settings := store.HarborSettings(tx, scope)       // lazily created with defaults
     resolve keys -> new UUIDv7; check ids belong to this map (else validation_failed)
     check background file, group ids, kit assets
     report := layout.ValidateMap(doc, settings)
     if len(report.Errors) > 0 -> ErrMapInvalid{report} (422 map_invalid), tx rolled back
     guard.CheckChanges(ctx, tx, scope, before, after) // OccupancyGuard: no-op in M1, M3 refuses unsafe changes
     store.SaveMapElements(tx, …)                      // diff by id: delete removed, update changed, insert new;
                                                       // codes changed in this save are first set to "~<id>" then to
                                                       // the final value, so swaps and renumbering never trip the
                                                       // unique (map_id, code) indexes mid-statement
     store.ReplaceGroupMembers(tx, scope, mapID, members)
     store.UpdateMapVersion(tx, scope, mapID, expected) // UPDATE … SET version = version + 1 WHERE id=? AND version=? (1 row or ErrVersionMismatch)
     audit.Write("map.save", diff summary: counts added/updated/removed per kind, version)
     outbox.Write(config.map_changed, {map_id, version, change:"updated"})
   ```
   Two concurrent saves with the same `If-Match` serialise on the map row (or the SQLite write gate). The second
   one sees the new version and gets 412. That second save never runs validation against stale state.
3. **Defence in depth.** `internal/testutil/harborcheck.AssertNoOverlappingBerths(t, db)` (owned by BACKEND-MAP)
   loads every berth of every map from the database. It checks all pairs by brute force with `geo.RectsOverlap` on
   `ShrinkRect(r, 1)`, with no spatial index and no `ValidateMap`. It runs at the end of every M1 acceptance test,
   harbor integration test and concurrency test.
4. **Property tests.**
   - `TestValidateMap_RejectsOverlap` (INV-04, table plus rapid): for random berth sets, `ValidateMap` reports
     `berth_overlap` if and only if the brute-force check finds a pair.
   - `TestGenerator_NoOverlap` (INV-04, rapid): every generator's output, over random parameters at any rotation,
     has no overlap, unique codes and the expected counts.
   - `TestHarbor_SavedMapsNeverOverlap` (integration, rapid state machine, both dialects): random sequences of
     create map, save valid doc, save doc with an injected overlap, generate plus save, CSV import, renumber and
     concurrent saves. After every step `AssertNoOverlappingBerths` holds, and a rejected save leaves the version
     and rows unchanged.

---

## 4. OpenAPI additions (`api/openapi.yaml`)

### 4.1 Conventions (in addition to `docs/m0-plan.md` §4.1)

- New tags: `maps`, `harbor`, `customers`, `vessels`, `documents`, `deadlines`.
- `M = /api/admin/v1/marinas/{marinaId}`. New path parameters: `MapId`, `FileId`, `GroupId`, `CustomerId`,
  `VesselId`, `DocumentId`, `DeadlineId` (uuid).
- **No required query parameters** on `{marinaId}` routes, because the cross-marina sweep calls them bare. The
  service validates instead, after authorising.
- New vendor extension **`x-porto-max-body-bytes`** on an operation (default 1 MiB). The global `BodyLimit`
  becomes a hard cap of 16 MiB, and a per-operation middleware applies the contract value. Values: `saveMap`,
  `validateMap`, `generateMapElements`, `importMap` 8 MiB; `importBerthsCsv` 4 MiB; `uploadMapBackground`,
  `uploadDocumentFile` 16 MiB (files ≤ 15 MiB).
- **Binary bodies.** `uploadMapBackground` declares `requestBody.content` `image/png`, `image/jpeg` and
  `image/svg+xml`. `uploadDocumentFile` declares `application/pdf`, `image/png` and `image/jpeg`, each with
  `{type: string, format: binary}`. `ParsePolicies` reads the declared content types, and `RequireJSON` enforces
  JSON only for operations whose body is JSON. For binary operations the handler passes `r.Body` and the
  `Content-Type` to the service, which authorises first and then rejects other types (`unsupported_media_type`).
  So the sweep still gets 404.
- Lengths are integer cm (`*_cm`), angles are `rotation_dd` (0..3599), masses kg, coordinates `{x, y}` cm.
  `I18nText = {it: string (1..200), en?: string}`. `Point = {x: int64, y: int64}`. `Polygon = Point[]` (3..10 000).
- Read operations: `security` as in M0 (cookie, API key, device). Unsafe operations: cookie with `csrfToken`, or
  API key, or device.
- Every new unsafe operation lists 400, 401, 403, 404 and the codes in its row. Every `If-Match` operation lists
  412 and 428.

### 4.2 Permissions

Add `PermHarborConfigure = "harbor.configure"` (roles `owner` and `director`, spec 8 and 26.1 "configurazione …
mappa"). It goes in `roles.go`, in the `Permission` enum of the contract, and in the independent copy in
`TestRoles_MatrixMatchesSpec`. It is a valid API key scope. The other M1 permissions already exist: `map.view`
(all roles), `customers.read`, `customers.write`, `marina.configure`, and `presence.write` (dock measurements,
§11 D12).

### 4.3 Harbour paths

| Method & path | operationId | Perm | Request | 2xx | Errors |
|---|---|---|---|---|---|
| GET `M/maps` | listMaps | map.view | – | 200 `{items: MapSummary[]}` (sorted by sort_order) | 404 |
| POST `M/maps` | createMap | harbor.configure | `MapCreate{kind, name, icon?, width_cm, height_cm, grid_cm?}` + Idempotency-Key | 201 `MapDocument` + ETag | 400, 403, 404 |
| POST `M/maps/reorder` | reorderMaps | harbor.configure | `{map_ids: uuid[]}` (all maps of the marina) | 204 | 400 validation_failed, 403, 404 |
| POST `M/maps/import` | importMap | harbor.configure | `MapExport` + Idempotency-Key | 201 `MapSummary` | 400 validation_failed, 403, 404, 422 map_invalid |
| GET `M/maps/{mapId}` | getMap | map.view | – | 200 `MapDocument` + ETag | 404 |
| PUT `M/maps/{mapId}` | saveMap | harbor.configure | `MapSaveRequest` + If-Match (required) | 200 `MapSaveResult{document, id_map: {key: uuid}, warnings: MapIssue[]}` + ETag | 400 validation_failed, 403, 404, 412 version_conflict, 422 map_invalid, 428 |
| DELETE `M/maps/{mapId}` | deleteMap | harbor.configure | If-Match | 204 | 403, 404, 409 map_not_empty, 412, 428 |
| POST `M/maps/{mapId}/validate` | validateMap | harbor.configure | `MapSaveRequest` | 200 `MapValidationReport{errors: MapIssue[], warnings: MapIssue[]}` | 400, 403, 404 |
| POST `M/maps/{mapId}/generate` | generateMapElements | harbor.configure | `GenerateRequest{generator: GeneratorParams (oneOf by "type": pier, quay_berths, linear_area, buoy_field, land_grid, surface_area), document?: MapSaveRequest}` | 200 `GenerateResult{elements: MapElements (keys only, no ids), report: MapValidationReport (of document + elements, when document given), counts{…}}` | 400 validation_failed, 403, 404 |
| POST `M/maps/{mapId}/background` | uploadMapBackground | harbor.configure | binary image ≤ 15 MiB | 201 `MapBackgroundFile{file_id, content_type, width_px, height_px, size_bytes}` | 400 invalid_file, 403, 404, 413 file_too_large, 415 |
| GET `M/maps/{mapId}/background/{fileId}` | getMapBackground | map.view | – | 200 image bytes, `Cache-Control: private, max-age=31536000, immutable`, `Content-Security-Policy: sandbox; default-src 'none'; style-src 'unsafe-inline'`, `X-Content-Type-Options: nosniff` | 404 |
| GET `M/maps/{mapId}/export` | exportMap | harbor.configure | query `format` = `json` (default) or `geojson` | 200 `MapExport` (`application/json`) or GeoJSON FeatureCollection (`application/geo+json`), `Content-Disposition: attachment` | 404, 409 map_not_georeferenced |
| POST `M/maps/{mapId}/concession/geojson` | convertConcessionGeojson | harbor.configure | `{geojson: object, georef?: Georef}` (map georef if omitted) | 200 `{outline: Polygon}` (no write; the editor puts it into the document) | 400 validation_failed, 403, 404, 409 map_not_georeferenced |
| POST `M/maps/{mapId}/berth-import` | importBerthsCsv | harbor.configure | `BerthImportRequest{csv: string, delimiter?: ","|";"|"\t", has_header: bool, mapping: {code, pier, length, width, depth?, price_class?, side?, mooring_type?: column index}, units: "m"|"cm", placement: {start: "after_existing"|"root", side_policy: "alternate"|"column"|"left"|"right"}, dry_run: bool}` + If-Match (required when `dry_run` = false) | 200 `BerthImportReport{rows_total, valid, rejected: [{row, errors: [{field, code}]}], duplicates: [{row, code, reason: "code_in_file"|"code_on_map"}], planned: [{row, code, pier, side}], document?: MapSaveResult (when applied, + ETag)}` | 400 csv_invalid / validation_failed, 403, 404, 412, 422 map_invalid, 428 |
| GET `M/berths` | listBerths | map.view | query `map_id?`, `group_id?`, `q?`, cursor, limit | 200 page of `BerthSummary{id, map_id, code, structure_code, width_cm, length_cm, max_loa_cm, max_beam_cm, depth_cm, price_class, mooring_type, active}` | 404 |
| GET `M/berth-groups` | listBerthGroups | map.view | – | 200 `{items: BerthGroup[]}` (with member counts) | 404 |
| POST `M/berth-groups` | createBerthGroup | harbor.configure | `BerthGroupCreate{code, name, kind?}` + Idempotency-Key | 201 `BerthGroup` + ETag | 400, 403, 404, 409 conflict (code taken) |
| PATCH `M/berth-groups/{groupId}` | updateBerthGroup | harbor.configure | `{code?, name?, kind?, sort_order?}` + If-Match | 200 `BerthGroup` | 400, 403, 404, 409, 412, 428 |
| DELETE `M/berth-groups/{groupId}` | deleteBerthGroup | harbor.configure | If-Match | 204 (members removed) | 403, 404, 412, 428 |
| GET `M/harbor-settings` | getHarborSettings | map.view | – | 200 `HarborSettings{price_class_rule, loa_tolerance_cm, default_grid_cm, version}` + ETag | 404 |
| PATCH `M/harbor-settings` | updateHarborSettings | harbor.configure | partial + If-Match | 200 + ETag | 400, 403, 404, 412, 428 |

**Schemas.**
- `MapSummary{id, kind, name, icon, width_cm, height_cm, sort_order, version, counts{structures, berths,
  dynamic_areas, land_resources, pedestals, decorations}, georeferenced, has_background, updated_at}`.
- `MapDocument` = `MapMeta` + `MapElements` + `{id, version, updated_at}`.
  `MapMeta{kind, name, icon, width_cm, height_cm, grid_cm, sort_order, background: MapBackground|null, georef:
  Georef|null, concession_outline: Polygon|null, water_polygons: Polygon[], land_polygons: Polygon[]}`.
- `MapBackground{file_id, content_type, width_px, height_px, origin: Point, scale_mcm_per_px (int, milli-cm per
  image pixel, ≥ 1), rotation_dd, opacity_pct (0..100), visible}`.
- `Georef{points: [ControlPoint ×3]}`, where `ControlPoint{x, y, lat_e7, lon_e7}`.
- `MapElements{structures: Structure[], berths: Berth[], dynamic_areas: DynamicArea[], land_resources:
  LandResource[], pedestals: Pedestal[], decorations: Decoration[]}`.
- Every element has `id?: uuid` (saved element) **or** `key?: string` (1..64, `[A-Za-z0-9_:-]`, new element,
  unique in the request). Reference fields (`structure_id`, `parent_id`, `pedestal_id`, `group_ids[]`) take a
  saved element's uuid or a new element's key from the same request. Responses always carry ids.
- `Structure{id|key, kind, code, name, points?: Point[], closed?: bool, rect?: Rect, width_cm?, parent_id?,
  sides?: Side[], floating?, depth_profile?: DepthSpan[], elevation_cm?, bollard_spacing_cm?}`.
- `Rect{anchor: Point, width_cm, length_cm, rotation_dd}`.
- `Berth{id|key, code, structure_id?, side, position, anchor: Point, rotation_dd, width_cm, length_cm, max_loa_cm?,
  max_beam_cm?, depth_cm?, max_air_draft_cm?, max_displacement_kg?, mooring_type, swing_radius_cm?,
  overhang_allowed, pedestal_id?, pedestal_socket?, power_a?, power_phase?, water, wifi, price_class?,
  attributes: string[], online_sellable, active, group_ids: string[]}`. When omitted, `max_loa_cm` defaults to
  `length_cm` and `max_beam_cm` to `width_cm − 2 × 25` (min 1).
- `DynamicArea{id|key, kind, code, structure_id?, side?, from_cm?, to_cm?, outline?: Polygon, mooring_mode?, cell_cm?
  (default 50 linear / 100 surface), margin_cm? (default 50 stern_to, 100 alongside, 60 surface), max_loa_cm?,
  max_beam_cm?, max_height_cm?, max_weight_kg?, supports[], depth_profile[], blocked_cells: int[], exits: int[],
  grid_rotation_dd, cell_count (readOnly), price_class?, active, group_ids[]}`.
- `LandResource{id|key, kind, code, anchor: Point, rotation_dd, width_cm, length_cm, price_class?, attributes[],
  active, group_ids[]}`.
- `Pedestal{id|key, code, position: Point, sockets: [{code, kind, amps?, phase?}], meters: [{kind, serial}]}`.
- `Decoration{id|key, kind, asset?, variant?, position?: Point, rotation_dd?, size_cm?, points?: Point[], label?:
  I18nText, depth_cm?}`.
- `MapSaveRequest` = `MapMeta` + `MapElements`.
- `MapIssue{code, severity, element_type, refs: string[], field?, params?: {string: int64}}`.
- `MapExport{format: "porto-map/1", exported_at, map: MapMeta + MapElements (ids kept as provenance; import
  remaps every id to a new key), groups: [{code, name, kind, members: [{resource_type, code}]}]}`.
- `GeneratorParams` mirror the §2.5 params in snake_case (lengths `_cm`, `numbering{scheme, prefix, start, pad}`,
  `group_id?` applied to the generated elements, `key_prefix`).
- `BerthGroup{id, code, name, kind, sort_order, member_counts{berth, dynamic_area, land_resource}, version}`.

**Berth CSV import semantics.** Rows are parsed with `encoding/csv` (RFC 4180, BOM stripped, max 5 000 rows).
Lengths use `qty.Parse` in metres (or integers in cm). The `pier` column matches a `pier` (or `quay`) structure
code on the map. Each pier's rows are placed in file order along its sides, after the last existing berth on that
side (`after_existing`) or from the root, using `side_policy`. Berth width is the row width. Geometry comes from
`layout.PlaceAlong` (the same placement maths as `GeneratePier`, for one berth at a time). Rejected rows are
reported and never block the valid ones in the dry run. Apply is all or nothing: it builds the full document
(current + new berths) and calls `applyDocument`, so validation and INV-04 are identical to `saveMap`. The import
records `audit` action `map.berth_import` with counts only.

**Map save field validation** (`validation_failed` 400, `errors[].field` = JSON path such as `berths[12].width_cm`)
covers types, ranges, unknown ids (`unknown_element`), duplicate keys, and keys colliding with ids. Geometric and
semantic rules are `map_invalid` 422 with `issues[]` (§2.3).

### 4.4 Registry paths

| Method & path | operationId | Perm | Request | 2xx | Errors |
|---|---|---|---|---|---|
| GET `M/customers` | listCustomers | customers.read | query `q?`, `category?`, `tag?`, `archived?` (default false), cursor, limit | 200 page of `CustomerSummary{id, kind, display_name, fiscal_code, vat_number, category, tags, primary_email, primary_phone, vessel_count, archived_at, anonymized_at, merged_into_id}` | 404 |
| POST `M/customers` | createCustomer | customers.write | `CustomerInput` + Idempotency-Key | 201 `CustomerCreated{customer: Customer, possible_duplicates: CustomerSummary[]}` + ETag | 400 validation_failed (fiscal_code_invalid, vat_invalid, iban_invalid as field codes), 403, 404 |
| GET `M/customers/{customerId}` | getCustomer | customers.read | – | 200 `Customer` + ETag | 404 |
| PATCH `M/customers/{customerId}` | updateCustomer | customers.write | `CustomerPatch` + If-Match | 200 `Customer` + ETag | 400, 403, 404, 409 customer_anonymized / customer_merged, 412, 428 |
| GET `M/customers/{customerId}/overview` | getCustomerOverview | customers.read | – | 200 `CustomerOverview{customer, vessels: [{vessel: VesselSummary, roles[], share_millis?}], documents: Document[], deadlines: Deadline[], consents: Consent[] (current per kind), duplicates: CustomerSummary[]}` | 404 |
| POST `M/customers/{customerId}/archive` | archiveCustomer | customers.write | – | 204 | 403, 404, 409 |
| POST `M/customers/{customerId}/unarchive` | unarchiveCustomer | customers.write | – | 204 | 403, 404, 409 |
| POST `M/customers/{customerId}/merge` | mergeCustomers | customers.write | `{source_ids: uuid[] (1..10)}` + If-Match (target) | 200 `Customer` | 400, 403, 404, 409 customer_anonymized / customer_merged, 412, 428 |
| POST `M/customers/{customerId}/anonymize` | anonymizeCustomer | customers.write | `{confirm: true}` | 204 | 400, 403, 404, 409 customer_anonymized |
| GET `M/customers/{customerId}/export` | exportCustomer | customers.read | – | 200 `CustomerExport{format: "porto-customer/1", customer, consents[], vessels[], parties[], documents[] (metadata, decrypted numbers), deadlines[]}`, `Content-Disposition: attachment` | 404 |
| GET `M/customers/{customerId}/consents` | listConsents | customers.read | – | 200 `{items: Consent[]}` (history, newest first) | 404 |
| POST `M/customers/{customerId}/consents` | recordConsent | customers.write | `{kind, policy_version, granted, source}` + Idempotency-Key | 201 `Consent` | 400, 403, 404, 409 customer_anonymized |
| GET `M/vessels` | listVessels | customers.read | query `q?`, `customer_id?`, `hull_type?`, `archived?`, cursor, limit | 200 page of `VesselSummary{id, name, registration, hull_type, loa_cm, beam_cm, effective_loa_cm, effective_beam_cm, dimensions_source, unit_class, owners: [{customer_id, display_name}], archived_at}` | 404 |
| POST `M/vessels` | createVessel | customers.write | `VesselInput` (+ optional `parties[]`) + Idempotency-Key | 201 `Vessel` + ETag | 400, 403, 404 |
| GET `M/vessels/{vesselId}` | getVessel | customers.read | – | 200 `Vessel` (with parties, documents) + ETag | 404 |
| PATCH `M/vessels/{vesselId}` | updateVessel | customers.write | `VesselPatch` + If-Match | 200 `Vessel` + ETag | 400, 403, 404, 412, 428 |
| PUT `M/vessels/{vesselId}/parties` | replaceVesselParties | customers.write | `{parties: [{customer_id, role, share_millis?}]}` + If-Match (vessel version, bumped) | 200 `Vessel` + ETag | 400 validation_failed (owner_shares_exceed: Σ owner shares > 1000; owner_share_required), 403, 404, 412, 428 |
| POST `M/vessels/{vesselId}/measurements` | recordVesselMeasurement | customers.write **or** presence.write | `{loa_cm, beam_cm, draft_cm?, air_draft_cm?}` | 200 `Vessel` + ETag (sets `measured_*`, `measured_at`, `measured_by`, `dimensions_source = measured`, bumps version) | 400, 403, 404 |
| POST `M/vessels/{vesselId}/archive`, `/unarchive` | archiveVessel, unarchiveVessel | customers.write | – | 204 | 403, 404, 409 |
| GET `M/documents` | listDocuments | customers.read | query `customer_id?`, `vessel_id?`, `type?`, `expires_before?` (date), cursor, limit | 200 page of `Document` | 404 |
| POST `M/documents` | createDocument | customers.write | `DocumentInput{owner_type, customer_id?, vessel_id?, type, number?, issuer?, issued_on?, expires_on?, notes?}` + Idempotency-Key | 201 `Document` + ETag | 400, 403, 404, 409 customer_anonymized |
| GET `M/documents/{documentId}` | getDocument | customers.read | – | 200 `Document` + ETag | 404 |
| PATCH `M/documents/{documentId}` | updateDocument | customers.write | partial + If-Match | 200 `Document` + ETag | 400, 403, 404, 412, 428 |
| DELETE `M/documents/{documentId}` | deleteDocument | customers.write | If-Match | 204 (file and deadline removed) | 403, 404, 412, 428 |
| PUT `M/documents/{documentId}/file` | uploadDocumentFile | customers.write | binary PDF/PNG/JPEG ≤ 15 MiB, header `X-File-Name?` | 200 `Document` + ETag | 400 invalid_file, 403, 404, 413 file_too_large, 415 |
| DELETE `M/documents/{documentId}/file` | deleteDocumentFile | customers.write | – | 204 | 403, 404 |
| POST `M/documents/{documentId}/file-url` | createDocumentFileUrl | customers.read | – | 200 `{url, expires_at}` (TTL 5 min) | 403, 404 (no file → 404) |
| GET `M/documents/{documentId}/file` | getDocumentFile | customers.read | query `exp?`, `sig?` (both checked in the service) | 200 bytes, `Content-Disposition: inline; filename*=…`, `Cache-Control: no-store`, CSP sandbox, nosniff | 403 download_link_expired, 404 |
| GET `M/deadlines` | listDeadlines | customers.read | query `status?` (default open), `kind?`, `urgency?`, `due_before?`, `due_after?`, `customer_id?`, `vessel_id?`, cursor, limit (ordered by due_on, id) | 200 page of `Deadline{id, kind, source_type, source_id, customer: {id, display_name}?, vessel: {id, name}?, document_type?, due_on, status, urgency, note, version}` | 404 |
| PATCH `M/deadlines/{deadlineId}` | updateDeadline | customers.write | `{status?, note?}` + If-Match | 200 `Deadline` | 400, 403, 404, 412, 428 |
| GET `M/deadline-policies` | listDeadlinePolicies | customers.read | – | 200 `{items: [{kind, lead_days: int[]}]}` (every M1 kind, defaults included) | 404 |
| PUT `M/deadline-policies` | replaceDeadlinePolicies | marina.configure | `{items: [{kind, lead_days}]}` | 200 same | 400, 403, 404 |

**Schemas.**
- `Customer{id, kind, first_name, last_name, company_name, display_name, fiscal_code, vat_number, sdi_code, pec,
  billing_address: Address, nationality, locale, emails: [{label, value}], phones: [{label, value}], category,
  tags, payment_terms?: {days, end_of_month}, iban_masked ("IT•• •••• 1234"), notes, merged_into_id,
  anonymized_at, archived_at, version, created_at, updated_at}`. The IBAN is write-only (`iban` in
  `CustomerInput`/`Patch`, `""` clears it) and never returned in clear.
- `Vessel{…all columns of §3.1 except search_text, effective_loa_cm, effective_beam_cm, effective_draft_cm,
  unit_class: natante|imbarcazione|nave, parties: [{customer_id, display_name, role, share_millis}], documents:
  Document[], version}`.
- `Document{id, owner_type, customer_id, vessel_id, type, number (decrypted, only to customers.read), issuer,
  issued_on, expires_on, has_file, file_name, content_type, size_bytes, notes, version, created_at, updated_at}`.
- `Consent{id, kind, policy_version, granted, source, recorded_by: UserRef?, recorded_at}`.

**Customer rules.**
- Fiscal codes: a person's code is 16 characters checked with the official check character (homocodia letters
  allowed). A company's code is 11 digits, or 16 characters, with the VAT check digit. A VAT number with prefix
  `IT` or none gets the Italian check digit, and other countries get a format check only. IBANs are checked with
  ISO 13616 mod-97.
- Duplicates are suspected on the same fiscal code, the same VAT, or a shared email (case-insensitive).
- Search: `q` is normalised (lower case, accents folded, phone digits only). It matches `customers.search_text LIKE
  '%q%'` OR any vessel linked through `vessel_parties` whose `search_text` matches. Results are limited to the
  marina.
- **Merge** (one transaction): moves `vessel_parties` (dropping the duplicates that the unique index would refuse),
  `documents`, `deadlines.customer_id` and `consents` to the target. Sources get `merged_into_id = target` and
  `archived_at`. The audit row of each source records the target id, and the event is `customer.merged`. A merged
  or anonymised customer can no longer be modified (409).
- **Anonymise** (one transaction): blanks names, contacts, fiscal data, IBAN, address and notes. `display_name`
  becomes "Cliente anonimizzato" through i18n on the client (the API returns an empty string plus `anonymized_at`).
  `search_text` is set to `''`. The customer's documents and their files are deleted (soft-deleting
  `stored_files` and removing blobs after commit), along with deadlines. The customer's `vessel_parties` are
  removed. Consents are kept (legal proof) with no personal data. Event `customer.anonymized`. M5 extends this to
  the fiscal snapshots of exported charges.
- **Audit diffs** for customers, vessels and documents list the **names of the changed fields only, never values**.
  This keeps PII out of the insert-only `audit_log`, so anonymisation is complete.

**Signed file URLs.**
`url = /api/admin/v1/marinas/{m}/documents/{d}/file?exp=<unix>&sig=<b64url HMAC-SHA256(KeyRing.Derive("documents.file_url"), m|d|user_id|exp)>`.
The GET still requires the session (or a key) and `customers.read`. The signature binds the URL to the user who
created it, and it expires after 5 minutes (403 `download_link_expired`). Files are read through
`filestore.Open`, which decrypts them, and are never cached.

### 4.5 Problem types (new slugs, added to `internal/http/problem/types.go`, `errmap.go`, i18n `errors.*` it/en)

`map_invalid` 422 (with `issues: MapIssue[]`; `problem.Problem` gains `Issues any \`json:"issues,omitempty"\`` and
option `problem.WithIssues(v)`), `map_not_empty` 409, `map_not_georeferenced` 409, `invalid_file` 400,
`file_too_large` 413, `csv_invalid` 400, `customer_anonymized` 409, `customer_merged` 409,
`download_link_expired` 403. Existing ones are reused: `validation_failed`, `version_conflict`,
`precondition_required`, `conflict`, `not_found`, `unsupported_media_type`.

Sentinels (declared by CONTRACT in each package's `errors.go`, names fixed):
- `harbor.ErrMapInvalid` (type `*MapInvalidError{Report layout.Report}`, `Is` → `ErrMapInvalid`), `harbor.ErrMapNotEmpty`,
  `harbor.ErrNotGeoreferenced`, `harbor.ErrVersionMismatch`, `harbor.ErrCSVInvalid`, `harbor.ErrInvalidFile`,
  `harbor.ErrFileTooLarge`, `harbor.ErrUnsupportedMedia`.
- `customer.ErrAnonymized`, `customer.ErrMerged`, `customer.ErrVersionMismatch`.
- `vessel.ErrVersionMismatch`.
- `document.ErrInvalidFile`, `document.ErrFileTooLarge`, `document.ErrUnsupportedMedia`, `document.ErrLinkExpired`,
  `document.ErrVersionMismatch`.
- `deadline.ErrVersionMismatch`.

Field validation errors reuse `auth.ValidationError{Fields}` (already mapped to `validation_failed` with `errors[]`).

### 4.6 Events and SSE (`internal/domain/event`)

| Type | Public (webhooks) | Topic | Data (ids and enums only) |
|---|---|---|---|
| `config.map_changed` | yes (spec 27.3) | `map.changed` | `{map_id?, version?, change: created\|updated\|deleted\|imported\|reordered\|groups\|settings}` |
| `customer.created` | yes (spec 27.3) | `registry.changed` | `{customer_id}` |
| `customer.anonymized` | yes (spec 27.3) | `registry.changed` | `{customer_id}` |
| `customer.updated` | no | `registry.changed` | `{customer_id, change: updated\|archived\|unarchived\|consent}` |
| `customer.merged` | no | `registry.changed` | `{customer_id, source_ids: []}` |
| `vessel.changed` | no | `registry.changed` | `{vessel_id, change: created\|updated\|parties\|measured\|archived\|unarchived}` |
| `document.changed` | no | `registry.changed` | `{document_id, owner_type, change: created\|updated\|deleted\|file}` |
| `deadline.changed` | no | `registry.changed` | `{deadline_id, change: updated}` |

Each type gets a sample in `Samples()`, so `TestEvents_DataHasNoPII` covers it. The web `keysForTopic` handles
`map.changed` (invalidates `['marina', id, 'maps']`, `['marina', id, 'berth-groups']` and
`['marina', id, 'harbor-settings']`) and `registry.changed` (invalidates `['marina', id, 'registry']`).

### 4.7 Meta

Add these capabilities: `harbor.editor`, `harbor.georef`, `harbor.berth_import`, `registry.customers`,
`registry.documents`.

---

## 5. Web

### 5.1 Routes (`web/admin`, TanStack code-based tree)

`router.tsx` (CONTRACT) adds three route factories from feature modules:
`mapRoutes(marinaRoute)`, `registryRoutes(marinaRoute)` and `berthImportRoutes(marinaRoute)`. Each returns an
array of `createRoute` children that the builder owns.

| Route | Owner | Page | Permission (UI) |
|---|---|---|---|
| `/m/$marinaId/maps` | WEB-EDITOR | maps list (tabs/cards with counts), create dialog (kind, name, size in m, grid), drag to reorder, JSON import, delete (If-Match) | map.view (writes: harbor.configure) |
| `/m/$marinaId/maps/$mapId` | WEB-EDITOR | read-only viewer (HarborMap, layers, fit, zoom, list alternative by structure) + "Edit" button | map.view |
| `/m/$marinaId/maps/$mapId/edit` | WEB-EDITOR | the editor (§5.3) | harbor.configure |
| `/m/$marinaId/maps/$mapId/print` | WEB-EDITOR | printable page: A4/A3, portrait/landscape, scale picker (1:500, 1:1000, 1:2000, fit), legend, Tailwind `print:` variants only | map.view |
| `/m/$marinaId/maps/$mapId/import-berths` | WEB-REGISTRY | CSV import wizard (§5.5) | harbor.configure |
| `/m/$marinaId/harbor-settings` | WEB-EDITOR | groups CRUD, price-class rule bands, LOA tolerance, default grid | harbor.configure |
| `/m/$marinaId/customers` | WEB-REGISTRY | list with search, category/tag/archived filters, cursor pagination | customers.read |
| `/m/$marinaId/customers/new` | WEB-REGISTRY | create form (person/company toggle), duplicates warning after create | customers.write |
| `/m/$marinaId/customers/$customerId` | WEB-REGISTRY | customer page: tabs Data (edit, If-Match), Vessels, Documents, Consents, Deadlines; actions Merge (search + pick sources, confirm), Export (download JSON), Anonymise (ConfirmDialog with typed confirmation), Archive | customers.read |
| `/m/$marinaId/vessels` | WEB-REGISTRY | list (search, hull type, owner filter) | customers.read |
| `/m/$marinaId/vessels/new`, `/m/$marinaId/vessels/$vesselId` | WEB-REGISTRY | vessel form (dimensions in m via `LengthInput`, declared vs measured, engines repeater), parties editor (customer combobox, role, owner share in %), documents tab, "Record measurement" dialog (also for dock) | customers.read |
| `/m/$marinaId/deadlines` | WEB-REGISTRY | list grouped by urgency, filters, mark done/dismiss, link to the source; policies editor (marina.configure) | customers.read |

`nav.ts` (CONTRACT): new items `maps` (map.view), `customers` (customers.read), `vessels` (customers.read),
`deadlines` (customers.read), and `harbor-settings` (harbor.configure). Remove `modules.map` and
`modules.customers` from `UPCOMING_MODULES`. `MarinaRoutePath` extended. `nav.test.ts` updated.
`DashboardPage.tsx` (WEB-REGISTRY) gets an "upcoming deadlines" card (count by urgency, link).

### 5.2 Shared map renderer (`web/shared/src/map/`, WEB-EDITOR, ADR 0004)

```
geometry.ts        roundHalfEven, rectCorners (float, render only), polylineLength, pointAt, project onto segment,
                   bounds, pointInPolygon; cm <-> m formatting helpers re-exported from lib/format
footprint.ts       Footprint, areaOf, cellCount: TS mirror of layout (integers only, Math.floor on integers, no float
                   intermediates beyond safe-integer range)  + footprint.parity.test.ts
spatialIndex.ts    uniform grid index (cell 1000 cm), built once per document version; query(viewBox + margin)
HarborMap.tsx      <svg viewBox="0 0 width height"> in cm; layers; culling via spatialIndex; LOD: below threshold
                   (berth < 6 px on screen) filled rects without labels/glyphs; labels when code >= 9 px;
                   React.memo per element keyed by id+version; role="button" + aria-label for interactive elements
elements/          Structure, Berth (rect | circle), DynamicArea (band/outline + optional cell grid), LandResource,
                   Pedestal, Decoration (kit <use>), Background (<image> with transform), Water/Land polygons,
                   Concession outline, Grid, ScaleBar, NorthArrow
kit/KitDefs.tsx    one hidden <svg><defs> per page via portal (only used symbols), JSON-tree renderer, no innerHTML
kit/generated/     GENERATED (manifest.ts, symbols.ts)
viewport.ts        pan/zoom state (wheel, pinch via pointer events), fitToView, screen<->map transforms
tokens.ts          token class names per element kind/state (fill-*, stroke-* Tailwind classes on theme tokens)
```

Colours come only from Tailwind classes on theme tokens (`fill-primary/20`, `stroke-border` and so on), with no hex
values. Sizes and positions are SVG attributes, never `style`. The renderer is shared because M3 adds lenses on
top of it.

### 5.3 Editor architecture (`web/admin/src/features/maps/editor/`, WEB-EDITOR)

```
model/document.ts      EditorDocument: normalized elements by ref (id or key), meta, dirty flag; fromApi/toSaveRequest
model/keys.ts          new keys: "n:" + counter (session-unique)
model/commands.ts      Command {label, apply(doc), invert(doc)}: Add, Remove, Update(fields), Transform(move/rotate),
                       Batch; every user action = one command
model/history.ts       undo/redo stacks (max 200), clear on save success, Ctrl/Cmd+Z, Shift+Ctrl+Z / Ctrl+Y
model/selection.ts     Set<Ref>; single, toggle (shift), marquee, by structure (pier), by group, select all of kind
model/snap.ts          snap(point, ctx): grid (grid_cm), structure edge (project anchor onto pier/quay edge,
                       rotation perpendicular to the edge, outward), vertices, alignment guides with other berths
                       (same anchor line / same rotation); Alt disables snapping; outputs integer cm
model/transform.ts     move (drag, arrows: 10 cm, Shift: 100 cm), rotate (handle; 1°, Shift: 15°), align (left/right/
                       top/bottom/centre, along the selection's common axis), distribute (equal gaps), duplicate
                       (offset by one width), mirror (across the pier axis or a picked line), numeric move dialog
model/renumber.ts      renumber selection or pier: scheme/prefix/start/pad, preview list old->new, single command
model/validation.ts    debounced (800 ms) POST validate with toSaveRequest; maps issues to refs; stale-request guard
model/draft.ts         local draft in localStorage keyed by map id + version (try/catch); restore prompt on open
tools/                 Select, Pan, Quay (polyline), Pier (axis + width), Polygon (breakwater/channel/water/land/
                       concession), Walkway, Finger, Berth (rect, snaps to structure edge), Buoy, LinearArea (pick
                       structure side + from/to), SurfaceArea (polygon), LandResource, Pedestal, Decoration (KitPicker),
                       Text, DepthLabel, Measure (distance in m), Calibrate
panels/                Toolbar (ToggleGroup), Layers (Switch per layer), Properties (contextual per kind; multi-edit
                       shows common fields, mixed values as placeholder), Groups & price class bulk assignment,
                       ValidationPanel (errors/warnings grouped by code, click = select + zoom), Minimap (optional)
dialogs/               GeneratorDialog (one form per generator, zod, "Anteprima" -> generateMapElements with the
                       current document -> overlay in preview tokens + report; "Inserisci" -> one Add Batch command;
                       KeyPrefix = "g<n>-"), BackgroundDialog (§5.4), GeorefDialog, ConcessionImportDialog (GeoJSON
                       file -> convertConcessionGeojson), RenumberDialog, NumericMoveDialog, ConflictDialog (412:
                       shows server vs local counts and changed codes; "Ricarica" discards local, "Salva comunque"
                       refetches version and re-applies local doc after confirmation), UnsavedChangesDialog
EditorPage.tsx         layout: toolbar top, canvas centre, properties/validation right (Resizable), status bar
                       (cursor x/y in m, zoom %, selection count, unsaved badge); Save button (disabled while saving;
                       422 opens ValidationPanel with issues; 200 replaces keys via id_map and clears history)
```

- Router blocker and `beforeunload` guard against losing unsaved changes.
- Keyboard: Del, Ctrl+D, Ctrl+A, Esc, and shortcuts for tools (`V`, `H`, `B` and so on), listed in a `kbd` help
  popover.
- Every action is reachable with the keyboard, the canvas exposes elements as focusable `role="button"`, and the
  structure list on the side gives an accessible alternative.
- **Performance target:** with 3 000 elements, dragging a selection of 50 berths updates only those (memoised by
  ref and version), and pan uses `viewBox` changes only.

### 5.4 Background calibration and georeferencing (spec 8.2)

1. Upload (`uploadMapBackground`) a PNG, JPEG or SVG of 15 MiB or less, with a progress bar.
2. Place: the image is drawn with its top-left at `origin`. Scale starts at 1 px = 10 cm.
3. Calibrate: pick two points on the image and enter the real distance in metres (`LengthInput`). Then
   `scale_mcm_per_px = roundHalfEven(distance_cm × 1000 / pixelDistance)`, computed in `calibration.ts` and
   unit-tested.
4. Rotate: a slider in 0.1° steps (`rotation_dd`) and numeric input, with a "north up" helper.
5. Set opacity (0..100) and visibility.
6. Optional georeference: three points, each with map coordinates (click) and lat/lon typed as decimal degrees.
   The client parses the text to `*_e7` integers, with no float maths.
7. The concession perimeter can be imported from a GeoJSON file (needs a georeference) or drawn with the Polygon
   tool.

All of these are commands in the document, saved with the map.

### 5.5 CSV berth import wizard (`web/admin/src/features/berth-import/`, WEB-REGISTRY)

1. **File:** a CSV file of 4 MiB or less, read in the browser as text. Options are delimiter auto-detection
   (`,` `;` tab), header row, and units (m or cm).
2. **Columns:** a preview of the first 20 rows, then a column-to-field mapping (code, pier, length, width, depth,
   category, side, mooring type), with automatic guesses from Italian and English header names. Mappings are
   remembered per marina in localStorage (try/catch). Saved mapping models per management system are an open
   decision (spec 33) and out of scope.
3. **Placement:** start `after_existing` or `root`, and the side policy.
4. **Dry run:** `importBerthsCsv` with `dry_run=true`. The report shows valid and rejected rows (row, field,
   error), duplicates and planned placements, and can be downloaded as CSV.
5. **Apply:** `dry_run=false` with If-Match from the current map. 412 asks to reload, and 422 shows the map issues.
   Success links to the editor.

### 5.6 Registry UI notes (WEB-REGISTRY)

- Forms use react-hook-form + zod. Lengths are entered in metres with `LengthInput` (2 decimals, integer cm in
  state). Fiscal code, VAT and IBAN are checked on the client with the same rules as the server, with tests
  sharing the vectors in `testdata/registry/fiscal-codes.json`. Those vectors are owned by BACKEND-REGISTRY and
  read by both sides.
- Document files are uploaded with `fetch` PUT (binary) through the shared client helper `uploadBinary`, added by
  CONTRACT to `web/shared/src/api/client.ts`. Downloads call `createDocumentFileUrl` and then open the URL.
- PII is never written to `console` or to localStorage.

### 5.7 i18n namespaces

CONTRACT creates and registers these in `resources.ts`, each with a starter `title` key in it and en:
- WEB-EDITOR owns `maps`, `editor` and `kit` (asset labels; keys = manifest `label_key`).
- WEB-REGISTRY owns `customers`, `vessels`, `documents`, `deadlines` and `imports`.

CONTRACT owns `common` (nav labels) and `errors` (new slugs). Builders request additions to those two from the
orchestrator in their report, or list them as missing in the final review: `i18n-check` fails on missing keys.

---

## 6. Demo seed (spec 31.4, as far as M1 entities allow)

`porto seed demo` stays idempotent. With a new database it creates M0's data and then the M1 data. With an
existing demo marina it seeds the harbour only if the marina has no maps, and the registry only if it has no
customers. The M1 parts go **through the domain services** (`harbor.SaveMap`, `customer`, `vessel` and `document`
services) with the demo owner principal, so audit, outbox and INV-04 hold for demo data too. Randomness uses
`math/rand/v2` with a fixed seed (`rand.NewPCG(2026, 11)`). Dates are relative to the marina's today
(`clock`, Europe/Rome).

`internal/app/seed_harbor.go` (BACKEND-MAP), built with the layout generators:
- Map **"Specchio acqueo"** (`water`, 600 × 400 m, grid 50):
  - a main quay polyline, a breakwater and an entrance channel;
  - water polygon and land polygon;
  - **5 piers A–E** with **320 berths in 8 price classes** (L8, L10, L12, L14, L16, L18, L20, L25):

    | Pier | Berths | Classes |
    |---|---|---|
    | A | 80 (40 per side) | L8/L10 |
    | B | 72 | L10/L12 (fingers every 2) |
    | C | 64 | L12/L14 |
    | D | 56 | L14/L16 |
    | E | 48 | L18/L20/L25, tapered |

    Price classes come from widths through the default rule, with 40 berths per class.
  - **a 120 m dynamic pier "T"**: pier structure with a linear `stern_to` area on the left (cell 50, margin 50,
    max beam 1 200) and an `alongside` area on the right (margin 100);
  - **a buoy field "Campo boe"** of 30 buoys (staggered, swing radius 1 400);
  - **80 pedestals** (one every 4 berths, 4 sockets 16/32 A, linked);
  - decorations: harbour office, fuel station, toilets, showers, travel lift, crane, slipway, red and green
    entrance lights, lighthouse, waste collection, pump-out, north arrow, scale bar, and depth labels;
  - a georeference with three control points around a fictional location, without a background image.
- Map **"Piazzale"** (`yard`, 120 × 80 m): a surface area of 80 × 50 m (cell 100, margin 60) with 2 exits and one
  lane of blocked cells.
- Map **"Capannone"** (`shed`, 50 × 30 m): a surface area of 40 × 25 m with max height 1 400 and pillars as blocked
  cells.
- Map **"Parcheggi e box"** (`parking`): a land grid of 60 `parking` spots (6 × 10, aisle 600) and 24 `box`
  (2 × 12).
- Groups: `PONT-A` … `PONT-E`, `PONT-T`, `TRANSITI` (pier E head berths plus 10 on A, tagged `transit_only`) and
  `BOE`.
- Harbour settings use the default price-class rule.

`internal/app/seed_registry.go` (BACKEND-REGISTRY):
- **250 customers** (200 people and 50 companies). Names are fictional, from built-in Italian lists. Fiscal codes
  and VAT numbers are synthetic and valid by checksum. Emails use `@example.invalid` and phones use the
  `+39 000…` range. Categories and tags are set and consents recorded.
- **260 vessels** with a hull type mix and dimensions matching the berth classes. Dimensions are measured on 60
  of them. Parties: one owner at 1000‰ on most vessels; 20 vessels with two owners at 500/500; a company
  `billing` party on 15; `skipper` on 30.
- **About 450 documents** (metadata only, no files): identity documents for people, insurance policies for every
  vessel and some navigation licences. Expiries are spread from −30 to +400 days, so the deadlines page shows
  overdue, due-soon and later items.
- `SeedResult` gains counts (maps, berths, customers, vessels, documents), printed by the CLI.

Contracts, rights, transits, work orders, rentals, charges and payments from spec 31.4 arrive with their
milestones.

---

## 7. Shared files and their split

### 7.1 Shared files edited only by CONTRACT (then frozen for M1)

| File | CONTRACT change |
|---|---|
| `api/openapi.yaml` | all of §4 |
| `internal/http/api/api.gen.go`, `web/shared/api/generated/schema.d.ts` | `make gen` |
| `Makefile` | `gen` adds `go generate ./internal/domain/layout/...`; `gen-check` adds `internal/domain/layout/kitassets_gen.go web/shared/src/map/kit/generated` |
| `web/shared/package.json` | `gen` script: `openapi-typescript … && node ../scripts/gen-harbor-kit.mjs` |
| `internal/domain/auth/roles.go` + `auth_test.go` matrix copy | `PermHarborConfigure` |
| `internal/domain/event/event.go`, `payloads.go` | §4.6 types, topics, payload structs, samples |
| `internal/domain/meta/meta.go` | §4.7 capabilities |
| `internal/http/problem/{types,problem}.go` + tests | §4.5 slugs, `Issues` |
| `internal/http/respond/errmap.go` | §4.5 sentinels (including `*harbor.MapInvalidError` → `map_invalid` with issues) |
| `internal/http/middleware/{policy,basic}.go` + tests | `x-porto-max-body-bytes`, declared content types, `RequireJSON` per policy, `BodyLimitByPolicy` |
| `internal/http/server.go` | global cap 16 MiB; per-op body limit in `perOp` |
| `internal/http/admin/admin.go` | `Handler` fields `Harbor *harbor.Service`, `Customers *customer.Service`, `Vessels *vessel.Service`, `Documents *document.Service`, `Deadlines *deadline.Service` |
| `internal/app/app.go` | `filestore.New(filepath.Join(cfg.DataDir, "files"), cfg.Keys)`; wiring of the five services through `Deps` structs; meta |
| `internal/app/seed.go` | calls `a.seedHarbor(ctx, p, res)` and `a.seedRegistry(ctx, p, res)`; idempotency rules of §6 |
| `internal/store/backup_tables.go`, `internal/backup/**` | §3.1 table order; files under `files/` included in the tar (`files/<storage_key>`, SHA-256 in the manifest), restored into `PORTO_DATA_DIR/files`; merged_into two-pass |
| `internal/store/files.go` | `stored_files` CRUD (used by both builders) |
| `internal/filestore/**` | §7.2 |
| `web/shared/src/i18n/resources.ts`, `locales/{it,en}/{common,errors}.json` | namespaces and keys |
| `web/shared/src/api/{queryKeys,sse,index,client}.ts` | keys for maps, map(id), berthGroups, harborSettings, berths, customers, customer(id), vessels, vessel(id), documents, deadlines, deadlinePolicies, all under `['marina', id, 'maps' \| 'registry', …]`; SSE topics; hook barrel exports; `uploadBinary` |
| `web/shared/src/ui/**` | shadcn CLI: `accordion calendar context-menu hover-card menubar pagination progress resizable slider toggle toggle-group`; barrel; patterns `LengthInput` (m ↔ cm), `DatePicker` (calendar + popover, `YYYY-MM-DD`), `ElementCombobox` (popover + command) |
| `web/shared/src/lib/format.ts` | `parseMetresToCm`, `formatE7`, `parseDecimalToE7` |
| `web/admin/src/router.tsx`, `nav.ts`, `nav.test.ts` | route factories, nav items |
| `docs/invariants.md`, `docs/architecture.md` | INV-04/INV-02 test names, new INV-74 (§8.5); architecture M1 section (filled at the end by the orchestrator) |

### 7.2 New shared foundations written by CONTRACT

```go
// internal/filestore
type Store struct{}
func New(dir string, keys *crypto.KeyRing) *Store                  // creates dir 0700
type Blob struct{ StorageKey string; Size int64; SHA256 string }
func (s *Store) Put(ctx context.Context, marinaID, fileID string, r io.Reader, max int64) (Blob, error) // encrypts (AAD "file:<marina>:<id>"), writes temp + rename; ErrTooLarge
func (s *Store) Open(ctx context.Context, marinaID, fileID, storageKey string) ([]byte, error)          // decrypts
func (s *Store) Remove(storageKey string) error                                                         // idempotent
func (s *Store) Dir() string
var ErrTooLarge, ErrCorrupt error
// Content sniffing helper (http.DetectContentType + SVG root check + image.DecodeConfig for PNG/JPEG, max 16384 px per side):
func Sniff(head []byte, declared string) (contentType string, w, h int, err error)
```

Domain skeletons, which the builders own afterwards and whose constructor signatures and sentinels they keep:
`internal/domain/{harbor,customer,vessel,document,deadline}/{service.go,errors.go}`, each with
`type Deps struct{ DB *store.DB; Clock clock.Clock; Audit *audit.Writer; Outbox *event.Outbox; Keys *crypto.KeyRing;
Files *filestore.Store; Log *slog.Logger }` and `func NewService(d Deps) *Service`. Harbor `Deps` also has
`Guard OccupancyGuard` (nil = no-op).

Handler stubs are `internal/http/admin/harbor.go` and `internal/http/admin/registry.go`. Every new operation
answers 501 (`problem.InternalError` with `Status(501)`) so that the generated `ServerInterface` compiles. The
builders replace them.

Feature route stubs are `web/admin/src/features/{maps,registry,berth-import}/routes.tsx`, each returning routes to
placeholder pages. Hook stubs are `web/shared/src/api/hooks/{maps,registry,berthImport}.ts`, which export nothing
yet.

Kit: `assets/harbor-kits/porto-basic/**` (manifest, 19 original SVGs, NOTICE), `web/scripts/gen-harbor-kit.mjs` and
its Vitest test, `internal/domain/layout/{kit.go,internal/genkit/main.go,kitassets_gen.go}`, and
`web/shared/src/map/kit/generated/*` (generated).

Models: `internal/store/harbor_models.go` and `internal/store/registry_models.go` (Bun structs for every §3.1
table). After CONTRACT, `harbor_models.go` belongs to BACKEND-MAP and `registry_models.go` to BACKEND-REGISTRY.
They may add fields that exist in the migration, but never change the migration.

---

## 8. Acceptance

### 8.1 Gates (exact commands, from the repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated internal/domain/layout/kitassets_gen.go web/shared/src/map/kit/generated   # empty
make check                                                     # gen-check + lint + test (Go + web), exit 0
make test-db                                                   # integration on SQLite AND PostgreSQL (docker or PORTO_TEST_POSTGRES_URL), exit 0
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'   # numbering, up/down/up, schema parity incl. 00002/00003
make test-race                                                 # now includes TestConcurrency_MapSaveSingleWinner, TestConcurrency_BerthImportVsSave
go test ./internal/domain/layout/... -count=1 -rapid.checks=2000
go test ./internal/domain/layout/... -run '^$' -bench BenchmarkValidateMap_3000 -benchtime 20x   # < 50 ms/op
go test ./internal/domain/layout/... -run 'TestFootprint_ParityVectors|TestFootprint_VectorsUpToDate' -count=1
npm --prefix web run test -- footprint.parity                 # same vectors, TS implementation
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/acceptance/... -run 'TestAcceptance_M1|TestAcceptance_CrossMarinaIsolation|TestAcceptance_M0'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/domain/harbor/... -run 'TestHarbor_SavedMapsNeverOverlap' -rapid.checks=200
make e2e                                                       # all specs, including §8.4
scripts/agents/check-naming.sh && make licenses
# Seed smoke on a fresh data dir
rm -f tmp/data/porto.db* && make seed && go run ./cmd/porto seed demo   # second run is a no-op
```

### 8.2 Backend tests that must exist (names fixed, they are referenced by `docs/invariants.md`)

**layout**
- `TestValidateMap_RejectsOverlap` (INV-04)
- `TestValidateMap_Scenario`, a table with one row per §2.3 code, in both directions
- `TestValidateMap_AdjacentBerthsDoNotOverlap`
- `TestValidateMap_OneCmTolerance` (a 1 cm overlap is accepted, 2 cm is rejected)
- `TestRapid_ValidateMapMatchesBruteForce`
- `TestGenerator_NoOverlap` (INV-04)
- `TestRapid_GeneratorCountsAndUniqueCodes`
- `TestGeneratePier_Scenario` (numbering schemes, fingers, taper, head)
- `TestGenerateBuoyField_Scenario`
- `TestGenerateLandGrid_Scenario`
- `TestGenerateSurfaceArea_LanesBlockCells`
- `TestFootprint_Scenario`
- `TestFootprint_DisjointCellsImplyNoOverlap` (INV-02)
- `TestRapid_FootprintCoversRect` (every point of the footprint lies in an occupied cell)
- `TestFootprint_ParityVectors`
- `TestFootprint_VectorsUpToDate`
- `TestAreaOf_Scenario`
- `TestGeoref_RoundTrip` (rapid, ≤ 1 cm)
- `TestGeoref_Collinear`
- `TestFormatE7`
- `TestPriceClassFor_Bands`
- `TestAngleOf_MatchesTable`
- `TestISqrt`
- `FuzzValidateMap` (no panic)
- `purity_test.go` (no forbidden imports)

**harbor** (integration, both dialects)
- `TestHarbor_CreateSaveReload`
- `TestHarbor_SaveRejectsOverlapAndLeavesStateUnchanged`
- `TestHarbor_SaveVersionConflict`
- `TestHarbor_RenumberSwapCodes`
- `TestHarbor_KeysResolvedAndReferencesKept`
- `TestHarbor_UnknownElementIDRejected` (an id from another map or another marina)
- `TestHarbor_SaveWritesAuditAndOutbox`
- `TestHarbor_GroupMembersFollowSave`
- `TestHarbor_DeleteNonEmptyMap`
- `TestHarbor_BackgroundUploadSniffing` (rejects a PNG declared as JPEG, oversized files, SVG with script)
- `TestHarbor_ExportImportRoundTrip`
- `TestHarbor_GeoJSONExport`
- `TestHarbor_BerthImport_DryRunAndApply`
- `TestHarbor_BerthImport_RejectsBadRows`
- `TestHarbor_SingleWritePath`
- `TestHarbor_SavedMapsNeverOverlap` (rapid state machine)
- `TestConcurrency_MapSaveSingleWinner` (100 parallel saves with the same If-Match → exactly one 200; the version
  goes up by exactly 1)
- `TestConcurrency_BerthImportVsSave` (100 parallel mixed writes, final state valid)
- Handler tests asserting status and problem `type` for every operation
- A 501 is no longer present: `TestAdmin_NoStubsLeft` greps `internal/http/admin` for the stub marker

**registry** (integration, both dialects)
- `TestCustomer_Scenario` (create, patch, archive, possible duplicates)
- `TestCustomer_FiscalCodeVectors`
- `TestCustomer_SearchByVessel`
- `TestCustomer_MergeMovesEverything`
- `TestCustomer_AnonymizeScrubsPII` (reads the raw rows and the audit log: no names, emails, fiscal code or IBAN
  remain)
- `TestCustomer_AuditDiffHasFieldNamesOnly`
- `TestVessel_PartiesOwnerSharesAtMost1000`
- `TestVessel_MeasurementSetsSource` (dock with `presence.write` can, viewer cannot)
- `TestVessel_UnitClass`
- `TestDocument_FileEncryptedAtRest` (the blob on disk has no `%PDF`/PNG magic and decrypts to the original)
- `TestDocument_SignedURLExpires` (another user, tampered or expired → 403)
- `TestDocument_NumberEncrypted`
- `TestDeadline_SyncedWithDocument` (create, change expiry, remove expiry, delete)
- `TestDeadline_Urgency` (clock.Fake, marina timezone)
- `TestDeadline_PoliciesDefaults`
- Handler tests for every operation
- `TestLogs_NoPII` extended to an M1 run

**platform**
- `TestRoles_MatrixMatchesSpec` with harbor.configure
- `TestEvents_DataHasNoPII` (new samples)
- `TestPolicies_BodyLimitAndContentTypes`
- `TestBodyLimit_PerOperation`
- `TestBackup_RoundTripSQLiteToPostgres` with M1 tables and files
- `TestMigrations_*` with 00002/00003
- `TestAcceptance_CrossMarinaIsolation`, which now covers every M1 route automatically. The binary and the
  optional-query rules of §4.1 keep the bare sweep valid, and the route count threshold rises to ≥ 80.

### 8.3 Acceptance tests (`internal/acceptance`, tag integration, API only, `clock.Fake`)

- **`TestAcceptance_M1_Harbor`** (BACKEND-MAP), as the owner:
  1. Create a water map 800 × 500 m and upload a small PNG orthophoto fixture.
  2. Save the calibration and georeference.
  3. Save a quay.
  4. Run the **pier generator 6 times** (piers A–F with 34 berths per side on 4 piers and 32 per side on 2:
     4 × 68 + 2 × 64 = **400 berths**), each time with the current document.
  5. Save, then reload: 400 berths, 6 piers, codes unique.
  6. A dock user can `getMap` but gets 403 on `saveMap`.
  7. Another marina gets 404.
  8. Export JSON, import it as a second map, and check the counts.
  9. Run the CSV import dry run plus apply on pier F.
  10. Assert `config.map_changed` in the outbox, plus audit rows.
  11. Run `harborcheck.AssertNoOverlappingBerths`.
- **`TestAcceptance_M1_RejectsOverlappingSave`** (BACKEND-MAP): starting from the saved 400-berth map, move berth
  `B-12` onto `B-13` and PUT. The response is 422 `map_invalid` with an issue `berth_overlap` whose `refs` contain
  both ids. Then:
  - the version, `updated_at` and every berth row are unchanged (compared with a snapshot);
  - no outbox event or audit row is written for it;
  - `validateMap` on the same body reports the same issue;
  - a generator preview that would overlap pier A returns the issue in its `report`;
  - `AssertNoOverlappingBerths` holds.
- **`TestAcceptance_M1_Registry`** (BACKEND-REGISTRY):
  1. Create a customer (person), a duplicate by fiscal code (`possible_duplicates`) and a company.
  2. Create a vessel with owner 600 + 400 and billing by the company. A third owner share of 100 gives 400
     `owner_shares_exceed`.
  3. Record a measurement as dock.
  4. Upload an insurance policy (PDF fixture) with an expiry 10 days ahead → a `due_soon` deadline appears.
  5. Use the signed URL download (ok, then expired after advancing the clock 6 min → 403).
  6. Search by vessel name finds the owners.
  7. Merge the duplicate.
  8. Export as JSON.
  9. Anonymise → the document and blob are gone, no PII is left in the customer row or audit, and
     `customer.anonymized` is in the outbox.
  10. Viewer gets 403 `forbidden` on customers (it has a grant on the marina but no `customers.read`).

### 8.4 e2e (Playwright, `make e2e`)

- **`e2e/tests/map-editor-acceptance.spec.ts`** (WEB-EDITOR), the spec 32.3 scenario, timeout 30 min,
  `test.slow()`:
  1. Log in as the owner, go to Mappe, create the map "Porto" (800 × 500 m).
  2. Open the editor and upload `e2e/fixtures/orthophoto.png`.
  3. Calibrate by clicking two marked points and typing 100 m, then set opacity.
  4. Draw the quay with the Quay tool (4 clicks + Enter).
  5. Open the Pier generator **6 times**, picking the origin on the quay (snap) and filling the numeric fields
     (rotation, length, width, berths per side, berth size, prefix `A-`…`F-`, odd/even numbering). Each time:
     Anteprima → Inserisci.
  6. The validation panel shows 0 errors. Save.
  7. Reload the page: the status bar or list shows **400 berths** and 6 piers.
  8. Through `page.request` (`getMap`), assert 400 berths.
  9. Record the elapsed time and the number of user actions in the test annotations. Assert elapsed < 30 min.
  10. Move one berth onto its neighbour (drag with snapping disabled via Alt). The validation panel shows
      `berth_overlap`. Saving shows the 422 issues, and the server version is unchanged (`getMap` ETag). Undo,
      then save succeeds.
- `e2e/tests/map-editor.spec.ts` (WEB-EDITOR):
  - undo/redo, multi-select and bulk edit of price class, align and distribute, renumber preview, groups;
  - the 412 conflict dialog (second tab saves first);
  - the read-only viewer for the `viewer` role (create the member through the API);
  - the print page renders;
  - axe on the editor in light and dark (no serious or critical findings).
- `e2e/tests/map-perf.spec.ts` (WEB-EDITOR): skipped unless `PORTO_E2E_PERF=1`. It loads 3 000 elements through
  the API, pans for 3 s and logs frame times. The M3 acceptance makes it blocking.
- `e2e/tests/customers.spec.ts`, `vessels.spec.ts`, `documents.spec.ts`, `deadlines.spec.ts` and
  `berth-import.spec.ts` (WEB-REGISTRY):
  - create, edit and search a customer;
  - add a vessel with parties;
  - upload a PDF and view it through the signed link;
  - see the deadline and mark it done;
  - merge and anonymise with confirmations;
  - import `e2e/fixtures/berths.csv` (30 rows, 2 invalid) onto a pier through the wizard: the dry-run report,
    then apply;
  - axe on customer and vessel pages.
- `a11y.spec.ts` (M0) stays green.

### 8.5 Invariants (`docs/invariants.md`, CONTRACT writes the entries, the orchestrator flips the status at the end)

- **INV-04** → `implemented`, verified by `TestValidateMap_RejectsOverlap`, `TestGenerator_NoOverlap`,
  `TestHarbor_SavedMapsNeverOverlap`, `TestAcceptance_M1_RejectsOverlappingSave` and
  `TestConcurrency_MapSaveSingleWinner`.
- **INV-02** → `partial` (`TestFootprint_DisjointCellsImplyNoOverlap`; allocations are M3).
- **INV-70** stays `partial` with the M1 samples. **INV-73** covers the M1 routes.
- New **INV-74 — Documenti cifrati a riposo e serviti solo con link firmati.** Verified by
  `TestDocument_FileEncryptedAtRest` and `TestDocument_SignedURLExpires`. Status: implemented.

### 8.6 Done criteria per builder

Each builder finishes with its own subset green: `go test ./<its packages>/...`, `-tags=integration` on both
dialects, `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test`, and its e2e
specs. The orchestrator then runs §8.1 in full.

---

## 9. Strict file ownership

Order: **CONTRACT** runs first, alone, and finishes when `make gen && go build ./... && make check` pass with stubs.
Then **BACKEND-MAP**, **BACKEND-REGISTRY**, **WEB-EDITOR** and **WEB-REGISTRY** run in parallel. Generated files
belong to CONTRACT, which reruns `make gen` on request. Files not listed belong to the orchestrator and are
read-only for builders: `docs/spec/**`, `docs/adr/**`, `docs/m*-plan.md`, `README.md`, `AGENTS.md`, `CLAUDE.md`,
`assets/brand/**`, `.claude/**`, `.github/**`, `deploy/**` and all M0 files not listed below.

| Owner | Files |
|---|---|
| **CONTRACT** | everything in §7.1 and §7.2; `internal/migrations/{sqlite,postgres}/0000{2,3}_*.sql`; migration test updates in `internal/migrations/*_test.go` if needed; `internal/acceptance/acceptance_test.go` (only the route-count threshold and sweep fixes); `CHANGELOG.md` (M1 entries) |
| **BACKEND-MAP** | `internal/domain/layout/**` except `kitassets_gen.go` and `internal/genkit/**`; `internal/domain/harbor/**` (keeps `NewService`/`Deps`/sentinels); `internal/store/harbor.go`, `internal/store/harbor_settings.go`, `internal/store/berth_groups.go`, `internal/store/harbor_models.go` (after CONTRACT), `internal/store/harbor_integration_test.go`; `internal/http/admin/harbor.go`, `internal/http/admin/harbor_test.go`; `internal/http/view/harbor.go`; `internal/app/seed_harbor.go`; `internal/testutil/harborcheck/**`; `internal/acceptance/m1_harbor_test.go` and `internal/acceptance/testdata/harbor/**` (fixtures: orthophoto.png, berths.csv); `testdata/footprint/**` |
| **BACKEND-REGISTRY** | `internal/domain/{customer,vessel,document,deadline}/**` (keeps constructors and sentinels); `internal/store/{customers,consents,vessels,documents,deadlines}.go`, `internal/store/registry_models.go` (after CONTRACT), `internal/store/registry_integration_test.go`; `internal/http/admin/{registry,customers,vessels,documents,deadlines}.go` + `_test.go`; `internal/http/view/registry.go`; `internal/app/seed_registry.go`; `internal/acceptance/m1_registry_test.go` and `internal/acceptance/testdata/registry/**`; `testdata/registry/**` |
| **WEB-EDITOR** | `web/shared/src/map/**` except `kit/generated/**`; `web/shared/src/api/hooks/maps.ts`; `web/admin/src/features/maps/**` (incl. `routes.tsx` after CONTRACT); `web/admin/src/features/harbor-settings/**`; `web/shared/src/i18n/locales/{it,en}/{maps,editor,kit}.json`; `assets/harbor-kits/porto-basic/svg/**` (refinement of CONTRACT's symbols; must rerun `npm --prefix web run gen` and keep the manifest ids); `e2e/tests/map-*.spec.ts`, `e2e/fixtures/orthophoto.png`, `e2e/support/map.ts` |
| **WEB-REGISTRY** | `web/admin/src/features/registry/**`, `web/admin/src/features/berth-import/**` (incl. `routes.tsx` after CONTRACT); `web/shared/src/api/hooks/{registry,berthImport}.ts`; `web/shared/src/i18n/locales/{it,en}/{customers,vessels,documents,deadlines,imports}.json`; `web/admin/src/pages/DashboardPage.tsx` (deadlines card only); `e2e/tests/{customers,vessels,documents,deadlines,berth-import}.spec.ts`, `e2e/fixtures/{berths.csv,sample.pdf}`, `e2e/support/registry.ts` |

Rules for the parallel phase:
- **Router registration:** `router.tsx` is CONTRACT's. Each feature adds routes only in its own `routes.tsx`.
- **Handlers:** the handler struct is CONTRACT's. Each builder implements its operations as methods in its own
  files of `package admin`. Two builders never define the same method, because the operationIds are split
  exactly as in §4.3 (harbour, BACKEND-MAP) and §4.4 (registry, BACKEND-REGISTRY).
- **View mappers:** `view/harbor.go` and `view/registry.go` only. `view/view.go` is not edited.
- **Store:** one file per area, as listed. A builder needing a helper in someone else's store file writes it in
  its own file.
- **i18n:** only your own namespace files. `common`/`errors` additions go through the orchestrator (CONTRACT
  rerun).
- **Query keys and hooks:** use the keys CONTRACT defined. Hooks live in your own hooks file.
- **cmd/app wiring:** CONTRACT's. A builder that needs a new dependency in `Deps` asks the orchestrator.
- **go.mod / package.json:** no new dependency without the orchestrator's approval. M1 needs none beyond
  `react-day-picker` and `react-resizable-panels`, pulled in by the shadcn CLI in CONTRACT.
- Cross-builder needs: WEB-REGISTRY's import wizard reads the map through `getMap` with its own query (key
  `queryKeys.map`). WEB-EDITOR links to `/maps/$mapId/import-berths`. BACKEND-REGISTRY never imports
  `harbor`/`layout`, and BACKEND-MAP never imports registry packages.

---

## 10. Order of work inside each builder

**CONTRACT**
1. Write the migrations, then the models, `files.go` and the backup changes, and pass the migration tests on both
   dialects.
2. Write `openapi.yaml` (§4), then the permission, events, problems, errmap and middleware.
3. Write `filestore`, the domain skeletons and handler stubs, the app wiring and the seed hooks.
4. Write the kit (manifest, SVGs, generators), then `make gen`.
5. Do the web foundations: shadcn CLI, patterns, i18n namespaces, query keys, SSE, router and nav stubs.
6. Run `make check` and `make test-db`. Both must be green with the stubs, except
   `TestAcceptance_CrossMarinaIsolation`: it fails on the 501 stub routes until the builders replace them. Report
   that failure as expected. `TestAdmin_NoStubsLeft` and the sweep become blocking at the end.

**BACKEND-MAP**
1. `layout`: measure, model, ValidateMap, Footprint, generators, georef, with property tests and vectors.
2. Store queries.
3. Harbor service: load/save/validate/generate, groups, settings, background, export/import, CSV import.
4. Handlers and views.
5. `harborcheck`, the integration, concurrency and property tests, then the acceptance tests.
6. The seed.

**BACKEND-REGISTRY**
1. Fiscal checks with vectors.
2. Store queries.
3. Customer, vessel, document and deadline services (merge, anonymise, export, signed URLs, deadline sync).
4. Handlers and views.
5. Tests and acceptance.
6. The seed.

**WEB-EDITOR**
1. `geometry.ts`, `footprint.ts` and the parity test.
2. `HarborMap` with culling and LOD, and the kit defs.
3. Maps list and viewer.
4. The editor model (document, commands, history, selection, snap, transform) with unit tests.
5. Tools and panels.
6. Generator dialog, background and georeference, validation, conflict.
7. Print, harbour settings.
8. e2e.

**WEB-REGISTRY**
1. Hooks.
2. Customers list, form and page.
3. Vessels and parties.
4. Documents with upload and signed download.
5. Deadlines and the dashboard card.
6. The CSV import wizard.
7. e2e.

---

## 11. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | **Full-document save.** `PUT /maps/{id}` replaces every element of the map (spec 8.5 "documento mappa versionato"). New elements carry a client `key`, saved ones their `id`. The response returns `id_map`. Element rows have no version of their own; the map `version` (ETag) serialises writers. |
| D2 | **Generators are preview-only on the API.** `generateMapElements` returns elements that are not saved, optionally with a validation report on the current unsaved document. They are applied by the next `saveMap`, so there is one write path for INV-04. "Anteprima nel client, applicazione in una transazione" (8.3) is met by showing the server preview in the editor and applying it in the save transaction. |
| D3 | **Generator code location.** Spec 8.3 names `harbor/generator.go`. The pure algorithms live in `layout/generator*.go` so that depguard enforces purity. `harbor/generator.go` is the service entry point. |
| D4 | **Validation runs only on the server.** The editor calls `validateMap`, debounced. The TS side mirrors only `Footprint`/`areaOf`/`cellCount` (required parity) and render geometry, which avoids a second validator that could drift. |
| D5 | **Overlap scope.** Besides berth–berth (spec), every pair of occupiable elements (berths, linear area bands, surface outlines, land resources) is a blocking `occupiable_overlap`. Buoy circles against other buoys or berths are warnings, as the spec states for buoys. The out-of-map check applies to every element, not only buoys. The 1 cm tolerance means each rect is shrunk by 1 cm per side before the interior-overlap test. |
| D6 | **`loa_exceeds_length` tolerance** = `harbor_settings.loa_tolerance_cm` (default 50 cm), one value per marina. Defaults: `max_loa_cm = length_cm` (spec) and `max_beam_cm = width_cm − 50` (a 25 cm fender margin per side). |
| D7 | **Price class "from the width".** The rule is a marina table of width bands → class (`harbor_settings.price_class_rule`). The default bands map typical berth widths to the boat-length classes L8…L25, so "L12 for 12 m berths" and "derived from the width" (7.3, 8.6) both hold. |
| D8 | **Element deletion in M1 is a hard delete in the save diff.** No occupancies exist. The `OccupancyGuard` hook is called with before and after, and M3 implements "disable instead of delete" and the fit checks of 8.5. |
| D9 | **Renumbering is client-side** (editor command, preview) and is saved through `saveMap`. Ids never change. Code swaps use the two-phase rename in the store. |
| D10 | **Groups are marina-level resources.** Memberships are set per element in the map document (`group_ids`) and replaced per map at save. Group CRUD has its own endpoints. Group changes emit `config.map_changed` with `change: groups`. |
| D11 | **SSE topics `map.changed` and `registry.changed`** are added to the spec 27.4 list. The non-public events `customer.updated`, `customer.merged`, `vessel.changed`, `document.changed` and `deadline.changed` exist only to feed SSE and invalidation. Webhooks can subscribe only to the spec 27.3 types. |
| D12 | **Vessel measurements** (spec 11.2 "misurate dall'ormeggiatore") are allowed with `customers.write` or `presence.write`, so dock staff can record them. |
| D13 | **Identity documents are `documents` rows** (type identity_card, passport, driving_licence), not customer columns. The customer page shows them. Document numbers and IBANs are encrypted with the KeyRing. Documents use typed `customer_id`/`vessel_id` columns with a CHECK, instead of a polymorphic `owner_id`, so that FKs and cascades work. |
| D14 | **Audit diffs** for customers, vessels and documents contain only the names of changed fields, so that anonymisation leaves no PII in the insert-only audit log. |
| D15 | **Anonymisation** requires `customers.write` and an explicit `{confirm: true}`. It deletes the customer's documents, files, parties and deadlines, and keeps the consent history with no personal data. Merge requires `customers.write` and `If-Match` on the target. |
| D16 | **Customers are per marina.** Sharing customers across the marinas of one organisation is a later decision. |
| D17 | **Deadline kinds and source types are validated in Go, not by DB CHECK.** Later milestones add kinds without rebuilding the SQLite table. Status has a CHECK. Default lead days are `[60,15]` for insurance and `[30]` for the rest. Notifications are M4. |
| D18 | **Legal unit class** (natante < 10 m, imbarcazione 10–24 m, nave > 24 m) is computed from the effective LOA. The thresholds come from `marinas.settings.registry.unit_class_thresholds_cm` (default `[1000, 2400]`), editable through the existing marina settings. |
| D19 | **Files** (backgrounds and documents) are AES-GCM encrypted blobs under `PORTO_DATA_DIR/files/<marina_id>/<id>.bin`, tracked in `stored_files`. They are included in `porto backup`/`restore`. Max size is 15 MiB. Types: PNG, JPEG, SVG (backgrounds only) and PDF (documents only, plus PNG/JPEG). Content is sniffed, never trusted from the header. SVG backgrounds are served with a sandbox CSP and rendered only through `<image>`. |
| D20 | **Request bodies:** per-operation limits through the `x-porto-max-body-bytes` contract extension, and raw binary bodies (not multipart) for uploads. |
| D21 | **Background calibration** is stored as `origin` + `scale_mcm_per_px` (integer milli-cm per pixel) + `rotation_dd` + `opacity_pct`. The editor computes these from two points and a distance and rounds half-even. **Georeference** = three control points stored as integers (`lat_e7`, `lon_e7`). The affine transform is computed with `math/big.Rat` in `layout`. GeoJSON numbers are written from `FormatE7` (no floats). |
| D22 | **Concession perimeter** is a map column (`concession_outline`), imported from GeoJSON through a stateless conversion endpoint, or drawn by hand. |
| D23 | **Berth CSV import** is CSV only (Excel users export to CSV), with at most 5 000 rows, placement along existing piers after the last berth or from the root, and a dry run first. Apply is all or nothing through the same save path. Customer, vessel and contract imports (11.6) are M4. Saved column-mapping models per management system remain an open decision (spec 33). |
| D24 | **Illustration kit** `porto-basic` is a set of original Porto line symbols drawn in-repo (`currentColor` only), with a sanitising generator that emits a JSON element tree instead of HTML. Coste360 may replace the drawings later without changing the ids. |
| D25 | **Map limits:** 20 000 elements per map, 2 000 elements per generator run, map size 1 m … 5 km per side. The map list is not paginated (a marina has few maps). |
| D26 | **Seed** is idempotent per part (harbour if no maps, registry if no customers). It uses a fixed PRNG seed and goes through the domain services. Fictional names, `example.invalid` emails and checksum-valid synthetic fiscal codes. |
| D27 | **Cross-marina sweep compatibility:** M1 operations under `{marinaId}` have no required query parameters, and binary-body operations authorise in the service before checking the content type, so the bare sweep keeps returning 404. |
| D28 | **Map deletion** is allowed only for maps without berths, dynamic areas or land resources (409 `map_not_empty`). Structures and decorations are deleted with the map. |
| D29 | **Read-only map viewer** for `map.view` roles in M1 (geometry and layers only). Lenses and day views are M3. |
