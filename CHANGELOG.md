# Changelog

All notable changes to Porto are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

M3 — Occupazione, transiti e mappa operativa (spec 32.3):

- Migrations `00005_contracts.sql` (occupancy settings, number sequences, inquiries, quotes and options, contracts
  and contract lines) and `00006_occupancy.sql` (allocations, `berth_occupancies`, `cell_occupancies`,
  `land_occupancies` with their primary keys as the no-double-occupancy guarantee (INV-01) and RESTRICT foreign
  keys to the map elements, releases, presences, vessel presence, move undo log, closures, quote option
  placements) for SQLite and PostgreSQL; new tables in backups (`move_undo_log` excluded).
- OpenAPI operations of the occupancy engine (settings, day view, map occupancy, allocation, search, placement
  check, move, swap, area layout save, compaction, undo, blocks, releases, closures), of quotes, contracts,
  transits and inquiries, and of presences (record, unexpected arrival, photo, movements of the day); problem types `resource_taken`, `fit_blocked`, `placement_invalid`,
  `invalid_transition`, `quote_expired`, `hold_expired`, `closure_active`, `undo_unavailable`, `release_invalid`,
  `customer_required`.
- Permission `contracts.read` (owner, director, office, accounting, dock); M3 domain events and the SSE topics
  `occupancy.changed`, `presence.changed`, `contract.changed`; SSE messages carry `map_id`, `resource_ids` and
  `nights` for narrowed invalidation; meta capabilities; job `contracts.expire`.
- Skeletons of `fit` and `occupancy/engine` (pure), `occupancy`, `contract` (with the state tables) and
  `presence`; the harbour guard hook refuses deleting map elements that have occupancy rows (`in_use`).
- Web: lens colour tokens `--harbor-*` with a contrast check, shadcn `drawer`, i18n namespaces `occupancyEnums`,
  `opsMap`, `desk`, `dock`, query keys and SSE invalidation for M3, operations and desk routes and navigation.
- Pure `fit.Check` (Go and TypeScript, shared parity vectors) and occupancy engine (plan, free intervals, search
  with best fit in linear areas, segments and "consider out", compaction, anomalies).
- Occupancy service: allocations writing exactly `Footprint × nights` rows, holds reclaimed when expired, moves
  (from a night or an interval), swaps, dynamic-area layout sessions, 24 h undo, blocks, releases with resale and
  revoke, closures, search with prices from the M2 engine, day view and map occupancy, harbour guard.
- Contracts: transits (held, manual confirmation, stay change, cancel, no-show), mooring contracts (draft,
  activate, cancel, terminate), quotes with 1–3 options and holds, inquiries, presences, numbering, price
  snapshots, the `contracts.expire` job and the demo seed (`porto seed demo`).
- Web: operational view (lenses, legend filter, scale/schema modes, quick card, drag & drop with "Da quando",
  swap, undo), dynamic-area layout session, incoming-vessel search, transit wizard, quote editor, contract wizard,
  berth planner, movements of the day, occupancy settings and closures, phone/tablet dock panel.
- CI job `e2e-perf`: the operational map budget (3,000 berths, 2,000 occupied) is asserted.
- Move price effect (m3-plan §13 D34): move, swap, layout save and placement check report `price_effect` for the
  remaining nights; it is applied with the contract policy `reprice_on_move` or an explicit `reprice`
  (`contracts.write`), recorded as a snapshot adjustment with audit and `*.changed` (`repriced`).
- Segment solutions of the search also chain linear areas (D37); a moved quote hold stays the option's hold (D35);
  revoking a release never re-occupies past nights (D36); fit overrides given by an API key record the key and
  its creator (INV-07).
- Invariant queries: Q8 checks the holds of valid quotes and accepts an expired allocation only for a held transit;
  Q7 exempts only a held transit with a reclaimed hold (D33); Q14 flags an override without its author.
- Season simulation: surface area, moves into and inside areas, layout saves and compaction, an oracle compared
  cell by cell built from mutation responses only, ≥ 1 400 successes, an audit row and outbox event per counted
  status change; the cross-marina sweep covers the M3 resource ids.
- Invariants review fixes: a lazy reclaim never expires a hold converted concurrently (the update re-checks kind
  and expiry, rows deleted only for the allocations it expired; `TestConcurrency_ConvertVsReclaim`); confirming a
  transit or activating a draft contract re-allocates every lapsed allocation of a line, not only the first one
  (interval moves, extensions); undoing a repriced move reverses the price with a `move_undo` snapshot
  adjustment, a `*.reprice` audit row (`undo: true`) and `*.changed` (INV-27); a reprice without a price
  snapshot or below a zero total is refused (`409 conflict`); `ResizeTx` keeps `moved_from_id` chains ordered
  (INV-11).

M2 — Listini e prezzi (spec 32.3):

- Migration `00004_pricing.sql` (pricing settings, revenue classes, items, berth categories, price lists, rate
  periods, length bands, price rules) for SQLite and PostgreSQL, with triggers that freeze published price lists
  (INV-22); new tables in backups (lists restored as drafts, status set in a final pass).
- Pure pricing engine `internal/domain/pricing`: rule selection, the seven bases, dimension source and length
  rounding, minimum beam ratio, split rules (exact, pro rata, whole months, best combination), modifiers,
  included and added services, discounts, VAT split, total rounding, deposits, a reproducible snapshot with
  `engine_version` (INV-20) and a trace; validation, matrix, coverage, renewal and diff on the same quote path
  (INV-23). 287 hand-derived price cases in `testdata/pricing/`, rapid properties, fuzz and benchmarks.
- Services `catalog` (pricing settings, revenue classes, items, berth categories with sync and usage counts) and
  `pricelist` (document save, validation, activation with truncation of predecessors or 409 overlap (INV-24),
  archive, renewal with a dry-run matrix diff, quote entry point for later milestones), with permission checks
  (`pricing.configure` owner/director, `pricing.read`), audit rows and `config.pricing_changed` events.
- OpenAPI operations for all of the above plus the price simulator, the price matrix and the coverage check;
  problem types `no_price`, `promo_not_applicable`, `price_list_invalid`, `price_list_immutable`,
  `price_list_overlap`, `in_use` with `details` and `price_list_issues`; meta capabilities and
  `pricing_engine_version`.
- Admin UI: items, classifications (berth categories, revenue classes, customer categories, hull types), pricing
  settings, price lists with timeline, the price list editor (general, periods, bands, rules, validation),
  activation, renewal wizard, simulator with breakdown and trace, matrix (with print page) and coverage.
- Demo seed: 11 items, an active list for the current year and its renewed draft.
- Published list catalogue is stable (INV-26): activation copies the engine fields of every item the list
  references (code, type, unit, VAT rate or nature, VAT inclusion, revenue class code, external code, active) into
  `price_list_items`, frozen with the list (INV-22). Active and archived lists price with that snapshot, drafts
  with the live items, so items and revenue classes stay editable while `content_sha256` keeps matching the list.
- Transit stays with the `best_combination` split rule go through the dynamic programming even when their nights
  are an exact multiple of the night unit (7 nights can be one week block; m2-plan D31).
- `GET /marinas/{marinaId}/customer-categories` (pricing.read): counts per customer category on the
  classifications page.
- Quantity inputs parse with the locale's separators, as the exact inverse of their formatting ("1.500" in Italian
  is 1500).

M1 — Mappa del porto e anagrafiche (spec 32.3), contract:

- Migrations `00002_harbor.sql` (stored files, harbour settings, maps, structures, pedestals, berths, dynamic
  areas, land resources, decorations, berth groups) and `00003_registry.sql` (customers, consents, vessels, vessel
  parties, documents, deadlines, deadline policies) for SQLite and PostgreSQL; new tables in backups.
- OpenAPI operations for maps, map documents, generators, backgrounds, import and export, berth CSV import, berths,
  berth groups, harbour settings, customers, consents, vessels, documents with files and deadlines.
- Permission `harbor.configure` (owner, director); events `config.map_changed`, `customer.created`,
  `customer.anonymized` and SSE topics `map.changed`, `registry.changed`; new problem types.
- Per-operation request body limits (`x-porto-max-body-bytes`) and raw binary uploads.
- Encrypted file store under `PORTO_DATA_DIR/files`, included in `porto backup` and `porto restore`.
- Illustration kit `porto-basic` (original line symbols) with sanitising generators for Go and TypeScript.
- Web foundations: shadcn components (accordion, calendar, context menu, hover card, menubar, pagination,
  progress, resizable, slider, toggle, toggle group), `LengthInput`, `DatePicker`, `ElementCombobox`, i18n
  namespaces, query keys, navigation and route stubs.

M0 — Fondamenta (spec 32.3):

- Repository layout, `Makefile` (`setup`, `dev`, `seed`, `gen`, `gen-check`, `fmt`, `lint`, `test`, `test-db`,
  `test-race`, `e2e`, `check`, `check-full`, `build`, `docker`, `licenses`), `.golangci.yml` with dependency rules.
- Shared primitives: `clock`, `money`, `calendar`, `reqctx`, `crypto` (KeyRing, HKDF), and the pure packages
  `geo` (integer geometry in centimetres, fixed-point trigonometry) and `qty` (fixed-point quantities).
- Configuration through `PORTO_*` environment variables, documented in `.env.example`.
- Migrations for SQLite and PostgreSQL (`00001_init.sql`): platform tables, organizations and marinas.
- CLI `porto`: `serve`, `migrate up|status`, `admin create`, `seed demo` (minimal), `backup`, `restore`, `version`.
- Authentication: sessions, CSRF protection, TOTP with recovery codes, password reset, login rate limits.
- Roles and permissions of spec 26.1, team and memberships at organization and marina level.
- API keys (`po_live_`), device pairing and device tokens (`po_dev_`).
- Insert-only audit log, transactional outbox with dispatcher, in-process job runner, SSE with replay.
- Outgoing webhooks signed with `Porto-Signature`, exponential backoff, anti-SSRF egress client.
- Email notifications through an outbox with SMTP, file and log transports.
- `application/problem+json` errors with stable types; `Idempotency-Key` on creating requests.
- OpenAPI 3.1 contract (`api/openapi.yaml`) with generated Go server and TypeScript client.
- Web workspaces: admin SPA (login, TOTP, password reset, shell, marina settings, team, API keys, devices,
  webhooks, audit, account, organization), booking placeholder, shared UI (shadcn/ui), theme, i18n (it, en), brand.
- Docker image (distroless, non-root), compose files for development, SQLite and PostgreSQL.
- CI: Go lint and tests on SQLite and PostgreSQL, race tests, govulncheck, web checks, generated-code drift,
  licences, Docker build with Trivy and smoke test, end-to-end tests, naming check.
- Development container and Claude Code support (`.claude/` rules, agents, skills, hooks).
- Documentation: `docs/architecture.md`, `CONTRIBUTING.md`, invariants INV-72 and INV-73.
