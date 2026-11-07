# Changelog

All notable changes to Porto are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

M6a "Edizione desktop locale e rete locale" (spec 31, m6a-plan):

- Desktop launcher `cmd/porto-desktop` (ADR 0010): the server runs in-process on 127.0.0.1 and the panel opens in
  the system webview (WKWebView, WebView2, WebKitGTK; build tag `webview`, CGO) or in the browser (default build,
  `CGO_ENABLED=0`; the main binary `cmd/porto` stays without CGO). Data in the per-user folder, SQLite only, secret
  key in the OS keychain (`go-keyring`) with a 0600 file fallback and a visible warning, never regenerated for an
  existing database; single instance with activation; first run in the window (single-use token in the URL
  fragment) or restore from a backup; daily, exit, manual and pre-restore backups with retention and a fallback
  folder; restore swaps the database and restarts the server in place (backups from before an app update restore,
  newer ones are refused with `restore_backup_newer`); rotating log; version check on by default,
  switchable off (`PORTO_UPDATE_CHECK=false`), never required, notify only.
- Local-network mode (`internal/lan`): HTTPS listener only while the owner enables it, private peers only
  (INV-79), self-generated local CA and leaf (ECDSA P-256, keys KeyRing-encrypted at rest), periodic refresh for
  address changes and renewal, Host check against the certificate names. The desktop API answers only on the
  loopback listener (INV-80).
- QR pairing of phones in the browser: `pairDeviceSession` exchanges a single-use code (INV-78, race-safe in DB)
  for a device-bound session cookie (idle 30 days, absolute 180 days), the QR opens `https://…/admin/pair#c=…&fp=…`;
  revoking the device kills its sessions; a device session acts only on its device's marina. Users with two-factor
  login verify it on the phone (`verifyDeviceSessionTotp`, throttled like a login): the pending session lives 10
  minutes and is replaced by a new one on success. Migration `00013_device_sessions` (SQLite and PostgreSQL).
- Rate limits and the login throttle key private and loopback peers by full address (a marina shares one /24);
  session cookies are Secure whenever the request arrived over TLS.
- Web: *Dispositivi* page (kind, access, state, pairing QR with countdown and fingerprint, revoke), `PairPage`,
  *App desktop* settings (local network, backups and restore, updates, secret key, data folders), first-run page,
  move-online guide, `LocalModeNotice` on the pages of features unavailable locally (public page, online payments,
  payment links, remote signature), update banner; downloads, print and external links through the desktop
  bridge (`web/shared/src/lib/download.ts`). i18n it and en.
- Packaging scripts `scripts/desktop/` and `packaging/desktop/`: macOS universal `.app`/`.dmg`, Windows x64 Inno
  Setup installer, Linux x86_64 AppImage, all `-unsigned`; signing and notarisation hooks are documented in
  `docs/desktop-build.md` and never faked. Make targets `desktop-build`, `desktop-package-{macos,windows,linux}`,
  `desktop-test`, `e2e-desktop` (also in `check-full` and in the CI e2e job); CI workflow `desktop.yml`
  (builds, uploads nothing); `make licenses` also checks the webview build.
- Tests: desktop unit and integration tests (launcher start/stop, restore, first run single owner, owner-only and
  loopback-only desktop API), LAN unit tests, `TestConcurrency_PairSessionSingleUse`,
  `TestConcurrency_PairingMixedEndpoints`, `TestAcceptance_M6aLanPairing`, Vitest specs and the Playwright desktop
  suite (`first-run`, `lan-pairing` with a Pixel 7 phone reaching the dock panel in under 60 s, `local-mode`).

M5 "Addebiti e incassi", contract phase (m5-plan §14.1):

- Migrations `00010_charges`, `00011_cash`, `00012_payments` (SQLite and PostgreSQL): billing settings, origin items,
  billing sequences, charges with lines, offsets, exports and items, invoice reconciliations, opening balance
  imports; cash registers, sessions, movements, fiscal numbering; payments, matches, refunds, fiscal documents,
  provider settings, intents, provider events, SEPA mandates, payment requests, reminders, dunning exclusions and
  worklist states. Database guarantees: one live primary charge per origin (INV-54), one first export per charge
  (INV-53), immutable exported and cancelled charges and their lines (INV-51), payment and charge limits (INV-52),
  insert-only matches, offsets, export items and cash movements, one open session per register (INV-55), frozen
  closed sessions and issued fiscal documents, one provider event once (INV-56), each (charge, level) reminded once
  and one run per day (INV-58); `TestMigrations_M5Constraints`.
- Store models, backup registration (charges restored as to_invoice/open and flipped at the end), lock helpers
  `LockPayment`, `LockChargesSorted`, `LockCashSession`, the `CheckBillingInvariants` dispatcher.
- OpenAPI: 85 M5 operations (charges, exports and reconciliation, payments, refunds, credits, receivables,
  statement, customer dashboard, map payment lens, worklist, cash, counter sales, receipts, corrispettivi, payment
  requests, reminders, exclusions, payment providers, SEPA mandates, payment webhook, public pay page), 25 problem
  types.
- Events `charge.*`, `installment.charged`, `payment.*`, `cash.changed`, `fiscal_document.changed`,
  `payment_request.changed`, `reminder.changed`, `billing.changed` on the SSE topic `billing.changed`.
- `pkg/ext`: `PaymentProvider`, `FiscalDriver`, `ChargeExporter` (none ships with Porto, spec 33 D4) and their
  `Extensions` fields; core payment driver `manual`; core fiscal drivers `none` and `export`.
- Billing hooks of contracts and rights (`contract.BillingHooks`, `rights.BillingHooks`), the M5 transit
  transitions `held → pending_payment → confirmed | held | expired | cancelled`, M5 jobs (no-ops), meta
  capabilities, `PORTO_STRIPE_API_BASE` (dev only).
- Web: SSE topic, query keys, billing hooks types, `MoneyInput` with vectors shared with Go
  (`testdata/money/parse.json`), i18n namespaces (`billingEnums` complete), admin routes and navigation of the
  "Amministrazione" pages, booking route `/b/{slug}/pay/{token}`.

M5 "Addebiti e incassi", builders and integration:

- Charges from every origin (instalments with cumulative VAT rounding through the pure `chargeplan`, true-up,
  transits, holder instalments, manual charges, counter sales, terminations, deposit retentions, opening balances),
  edits of unexported charges, adjustments (delta and replace) with offsets and customer credit, backfill of M4
  instalments into charges and backfill payments.
- "Da fatturare" export (preview, CSV, Excel through the own stdlib `internal/xlsx`, PDF summary), re-export, invoice
  reconciliation from CSV/Excel, the `ChargeExporter` interface (no vendor, spec 33 D4), Excel input for the imports.
- Payments ledger: idempotent recording, explicit and oldest-first matching, reversals, refunds only from
  unallocated money, credits (goodwill, release, adjustment, mandate, opening balance), deposits and retentions,
  mandate settlement; statement of account (JSON, CSV, PDF), scadenzario and ageing, customer dashboard, map
  payments lens, office worklist.
- Cash registers and sessions with Z report, counter sales with 80/58 mm and A4 courtesy receipts ("Non valido ai
  fini fiscali"), fiscal documents and corrispettivi (drivers `none` and `export`).
- Collection: payment requests with public pay page and tokenised links, reminders with levels and approval of the
  final notice, arrears policy (disabled by default), SEPA mandates; optional Stripe driver (plain HTTPS over the
  egress client, signed and idempotent webhooks), registered but disabled until a marina configures it; every flow
  works offline with the manual driver.
- Web: the 18 "Amministrazione" pages, customer dashboard and statement tabs, transit and contract charge panels,
  booking pay page, Pagamenti lens and the dock departure warning (never a block).
- `scripts/with-postgres.sh` and CI give PostgreSQL 1 GB of shared memory, `max_locks_per_transaction` 512 and
  `max_connections` 300 for the M5 integration and concurrency tests.
- Worklist signals that recur (arrears paid, disputes, billing mismatches) reopen a closed item on a new occurrence
  instead of being dropped.


Brand (ADR 0007, decision D13):

- `scripts/brand/build-brand.mjs` (`make brand`, standalone Node project with opentype.js) outlines the "porto"
  wordmark of the editable kit sources with Titillium Web 700 (OFL-1.1) into `assets/brand/porto/outlined/`
  (horizontal and stacked, every colour variant of the kit); the horizontal logos in `web/shared/src/brand/` now
  use these files instead of the Inter Bold outlines, and the brand copy test checks them against `outlined/`.
- The outlined wordmark is enlarged to match the Inter x-height of the kit's standard files (176 px horizontal,
  150 px stacked instead of 137/118, approved on 2026-11-07): per-layout `WORDMARK_SIZE` in the build script,
  checked against the Inter wordmark measured from `01_Loghi_SVG/`; the kit's editable sources are unchanged.
- Coste360 signature (decision D1: "Porto" is a Coste360 product, credited like Arenile): shared component
  `MadeByCoste360` ("un prodotto di" / "a product by" + official Coste360 logo, light and dark, link to
  coste360.it in a new tab) in the admin sidebar footer (hidden when collapsed to icons), under the sign-in card
  and in the footer of the pages outside a marina; the logo copies in `web/shared/src/brand/` are checked
  byte-identical to `assets/brand/coste360/`.

Map editor (M1 decision D30):

- Validation warning `berth_below_min_size` for non-buoy berths narrower than 150 cm or shorter than 300 cm
  (`layout.MinBerthWidth`/`MinBerthLength`, mirrored in `web/shared/src/map/geometry.ts` with a parity test); it
  never blocks saving, the hard limit stays `MinRectSide` = 2 cm.

Docs:

- Spec 29.3 records the sign-in locale rule (first browser language only, Italian fallback); D1 is removed from
  the open decisions and recorded in ADR 0007.

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

M5 polish:

- Contract lines showed 0,00 € while the contract total was correct: each line now carries its share of the price
  snapshot, computed when the contract is read (shares always sum to the total).
- The berth-use rights list shows the berth code instead of its internal id.
- `web/.gitignore` no longer hides `src/**/coverage/` source folders (the price list coverage page was missing
  from a clean checkout).

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

- M6a: the window build was not opened on a real display in CI; Windows and Linux webview builds are compiled
  only by the `desktop.yml` jobs; the Linux AppImage step needs `APPIMAGETOOL_SHA256` (repository variable) or a
  local `APPIMAGETOOL`. Signing and notarisation need Coste360 certificates. M6b (PWA offline queue, per-role phone
  screens, remote mode, kiosk, local peripherals bridge) is not built.
- Demo seed: the 2 tacit-renewal contracts and the charter fleet of 8 vessels on pier E (m4-plan §10) are not
  seeded yet.
- Public quote and renewal acceptance do not return `signature_link` (m4-plan §15 D33, open for a product
  decision).
