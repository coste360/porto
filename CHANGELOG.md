# Changelog

All notable changes to Porto are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

Brand (ADR 0007, decision D13):

- `scripts/brand/build-brand.mjs` (`make brand`, standalone Node project with opentype.js) outlines the "porto"
  wordmark of the editable kit sources with Titillium Web 700 (OFL-1.1) into `assets/brand/porto/outlined/`
  (horizontal and stacked, every colour variant of the kit); the horizontal logos in `web/shared/src/brand/` now
  use these files instead of the Inter Bold outlines, and the brand copy test checks them against `outlined/`.

M4 — Contrattualistica completa (spec 32.3), contract phase:

- Migrations `00007_contracts_m4.sql` (contract settings, instalments, renewal campaigns and proposals with the
  one-open-proposal partial unique (INV-33), addenda, charter fleets and contracts, waiting list; M4 columns of
  contracts, quotes and deadline policies), `00008_rights.sql` (concession and obligations, berth rights with
  holders, vessels and owner allocations, transfers, mandates and revenues, management fee tables and runs,
  millesimal tables, budgets, common charge runs and allocations, holder instalments; immutability triggers of
  active tables and issued runs (INV-41, INV-44)) and `00009_documents.sql` (templates and versions, generated
  documents, signature requests, signers and OTP codes, access links, communications and recipients, customer
  message registry, signature settings, import runs) for SQLite and PostgreSQL; new tables in backups
  (`signature_otps` excluded; frozen parents restored as drafts and flipped at the end).
- OpenAPI operations of contract settings, quote sending, signature requests, suspension, renewals and campaigns,
  instalments and the scadenzario, charter, addenda, waiting list, rights, transfers, mandates, management fees,
  millesimal tables, budgets and common charge runs, holder instalments, concession, templates and preview,
  generated documents, map print, signatures and provider settings, the signature provider webhook,
  communications, customer messages, deadline sync, CSV imports and the public customer-link pages; new problem
  types; permissions `templates.configure`, `communications.send`, `providers.configure`; M4 events and the SSE
  topics `rights.changed` and `documents.changed`; meta capabilities; jobs `installments.issue`, `renewals.sync`,
  `mandates.accrue`, `documents.render`, `signatures.sync`, `comms.send`, `deadlines.sync`, `deadlines.notify`,
  `links.cleanup`.
- `ext.SignatureProvider` with its types and `Extensions.SignatureProviders` (no implementation ships: InfoCert
  stays an interface, spec 33 D5); skeletons of `schedule`, `allot`, `accrual` (pure), `docs` with the frozen
  merge views, `docs/tmpl`, `docs/markdown`, `docs/layout`, `docs/pdf`, `signature`, `magiclink`, `comms`,
  `rights`, the generic `csvimport`; M4 state tables of contracts, proposals, campaigns and addenda.
- Dependencies `github.com/go-pdf/fpdf` (MIT) and `golang.org/x/image` (BSD-3) for the pure-Go PDF renderer.

M4 — Contrattualistica completa (spec 32.3), builders and integration:

- Instalment plans (deposit, fixed dates, intervals, early-payment alternative; Σ = total, INV-31), the scadenzario
  with CSV, bulk issue, reminders and manual settlement (INV-32); signature request, suspension, paper activation
  with scan, termination amounts; quote sending by email with PDF and magic link, public acceptance that saves the
  customer's missing address, fiscal code and vessel registration; simple renewals and mass renewal campaigns
  (preview with exclusions per reason, launch, `renewals.sync`, public accept/decline, OTP signature); addenda,
  change of vessel, charter fleets and rotation, waiting list, contracts CSV import.
- Berth rights with holders and owner occupancy (Q17, INV-43), transfers signed by OTP in sequence, mandates with
  accrued revenue, management fee runs, millesimal tables, budgets, advance and balance runs with exact allocation
  (INV-40..42, INV-44), holder instalments, concession; synchronous statement and holder-chain PDFs
  (`docs.Service.RenderView`).
- Closed template language, Markdown subset, A4 layout and PDF/A-2b renderer in pure Go; generated documents with
  download links (`GET generated-documents/{id}/file`); OTP, paper and provider signatures; magic links;
  communications with consent and one-click unsubscribe; the deadline engine with lead-day emails; CSV imports.
- Admin UI for contracts M4, rights and common charges, templates, signatures, communications and imports; the
  customer link pages in the booking app; the rights toggle and print on the operations map.
- Demo seed: contract settings and plans, a renewal campaign for next year with panel decisions, addenda, waiting
  list, rights, mandates, fees, common charges, templates and communications.
- Acceptance tests `TestAcceptance_M4_RenewalCampaign300`, `TestAcceptance_M4_QuoteToContract`,
  `TestAcceptance_M4_CommonCharges400`, `TestAcceptance_M4_RightsLifecycle`,
  `TestAcceptance_M4_DocumentsDeadlinesComms` on SQLite and PostgreSQL.

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

### Fixed

M4 review, fix round 1:

- A blocked tacit renewal (arrears, expired insurance, no price, berth unavailable) now writes an open
  `tacit_renewal_blocked` deadline on the source contract (due today, reason as note, outbox `deadline.changed`) in
  the transaction of its audit row (`contract.tacit_renew_blocked`), idempotent through `deadlines_source_uidx`.
- INV-31 under PostgreSQL: settlements and the `installments.issue` job lock the contract before the instalment
  (lock order contract → instalments, contracts locked in id order in bulk settlements and issue batches);
  `store.CancelInstallments` and the replan fail with 412 when a row changed between read and cancel.
- Renewals lock and re-check the source inside the proposal transaction (active, same version); the tacit renewal
  re-checks open proposals and signed successors under the source lock; a campaign preview skips a source that
  changed meanwhile.
- A signer user must hold a grant on the marina (`store.MarinaMemberUser`, INV-73); any other user is 404.
- The deadline engine resets `notified_at` through a marina-scoped store update instead of raw SQL.
- `store.CheckContractInvariants` checks C40 (INV-40, holder shares of active rights sum to 1000).
- `make licenses` also checks the `scripts/brand` npm project; `/porto` (a stray root build) is git-ignored.
- New tests: `TestContract_TacitRenewal`, `TestContract_ExpireMarksRenewed`, `TestRenewal_StaleSourceRefused`,
  `TestContract_M4StateChangesWriteAuditAndOutbox`, `TestQuote_SendRotatesLink`, `TestInstallment_IssueJobIdempotent`,
  `TestRenewal_CampaignPreviewIdempotent`, `TestRenewal_SyncJobIdempotent`, `TestContract_ChangeVesselProRata`,
  `TestImport_UndoGuard`, `TestRight_HolderSharesWritePaths`, `TestSignature_SignerUserScopedToMarina`,
  `TestDeadlines_DueChangeResetsNotified`, `TestSeed_DemoM4Coherent` and the concurrency tests
  `TestConcurrency_{ReplanVsSettle,RenewVsTerminate,RenewVsTacit,ContractTwoOperators,CampaignLaunchAndSync,AddendumVsTerminate,QuotePublicAcceptVsPanelAccept}`.

### Known gaps

- Demo seed: the 2 tacit-renewal contracts and the charter fleet of 8 vessels on pier E (m4-plan §10) are not
  seeded yet.
- Public quote and renewal acceptance do not return `signature_link` (m4-plan §15 D33, open for a product
  decision).
