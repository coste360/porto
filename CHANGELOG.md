# Changelog

All notable changes to Porto are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

M9 "Rimessaggio e manutenzioni" (spec 17, 20, 10, 29.2, m9-plan, ADR 0014):

- Storage contracts (`RIM-YYYY-NNNN`) on the contract domain: indoor/outdoor areas, cradles and supports, launch
  and haul-out dates, priced as contract type `storage` for every term. Every storage contract occupies a concrete
  position from creation; without one given, the optimizer places it under the area lock (no space = 409). Fit gains
  `support_not_allowed` (never overridable) and `weight_exceeds` (overridable), in Go and in the TypeScript twin.
- Pure `optimizer` package: gates and distance field on the area grid, accessibility (each boat must leave along a
  path to a door at least beam + margin wide, same-day extraction order, blocking sets, arrivals as warnings),
  `Arrange` with its own seeded PRNG, iteration budget plus caller deadline (setting `storage.optimize_budget_ms`),
  `PlaceOne` and an independent `Validate` (INV-15). In a 2,000 m² shed with 40 boats it returns a layout with no
  boat left out and no exit blocked in well under 3 s.
- Storage planner on the shed and yard map (SVG grid at scale with the timeline, drag, keyboard moves and rotation,
  tray, preset, save and restore), accessibility lens, hypotheses (scenarios) kept apart from the real state
  (INV-14) with optimize, compare and discard, transactional apply with re-check under the locks: all or nothing,
  a conflicting apply answers 409 with the full list and changes nothing (INV-06). Movement worklist with equipment
  hints, phone screen of today's movements.
- Maintenance: equipment registry, plans with periodicity (months, cycles, fixed date) and checklist, hourly job
  `maintenance.schedule`, tasks with states, outcome, costs, photos and documents (signed download links),
  `maintenance` nights on berths and land resources with move proposals on conflict (INV-92), out-of-service marking,
  field reports that never occupy anything, *Manutenzione* map lens, deadline kind `maintenance_due` and e-mail
  template `maintenance_due` (it, en).
- Utilities: meters on pedestals and resources, readings from the phone (offline, `client_ref`, photo), anomaly
  checks and confirmation, consumptions split by the nights of the actual occupants (INV-91), charged once through
  the M5 charges with origin `utility` (INV-90) with the next instalment, monthly, at transit departure or by hand;
  late readings inside a charged interval become adjustments linked to the original charge.
- Offline queue kinds `utility.reading`, `maintenance.report` and `maintenance.outcome` enabled (spec 29.2).
- Reports `storage`, `maintenance`, `utilities` and dashboard tiles `storage_occupancy`, `storage_exits_blocked`,
  `maintenance_overdue`, `utility_unbilled` on the M7 machinery (tile = sum of its drill records).
- API: tags `storage`, `maintenance`, `utilities`; events `storage.scenario_applied`, `storage.movement_done`,
  `maintenance.status_changed`, `meter.reading_recorded`; SSE topics `storage.changed`, `maintenance.changed`,
  `utilities.changed`. Migrations `00018_storage` and `00019_maintenance_utilities` (SQLite and PostgreSQL).
- Demo seed: shed and yard doors 6 m wide, a storage price list, storage contracts in the shed and the yard, one draft
  scenario, equipment, maintenance plans and tasks, berth meters with readings.

M8 "Prenotazione online" (spec 23, 13, 10.7, m8-plan, ADR 0013):

- Public booking page `/b/{slug}` and embeddable widget (`/embed.js`, iframe that resizes itself and emits
  `porto:booking` events): search by dates and vessel dimensions on online-sellable berths and linear areas, online
  quotas, price from the pricing engine, hold with countdown, customer and vessel details with versioned terms and
  privacy consents, document upload, checkout with advance payment through the payment drivers (Stripe manual
  capture for the approval flow), return page with `.ics`. Framing only for the marina's allowed origins
  (`frame-ancestors`, cached 60 s); API and admin keep `DENY`. Installable shell (manifest, service worker outside
  embed mode).
- Transits are confirmed only by the payment webhook or the manual driver (INV-09); approval flow with a hold until
  the deadline, approve/reject from the desk, automatic rejection at the deadline, capture retry; late payment
  recovered on the same berth or refunded in full.
- Online quotas as database counters with a conditional increment (INV-12, query Q19); no double sale between desk,
  online and channels (INV-13), all through `occupancy.AllocateTx`.
- Customer area: magic-link login (token in the URL fragment, same answer for unknown emails), sessions in a
  header, bookings, documents (signed 5-minute download links), receipts, pay balance, change dates and cancel
  within the policy snapshot with a refund preview (INV-88). A cancellation or a shorter stay commits together with
  its refund rows. Booking-token pages list only the documents uploaded through that booking
  (`online_booking_documents`), never those of an existing customer matched by email. Without published terms and
  privacy the page is not payable and details are refused.
- Desk (`Prenotazioni online`): list with filters and approval countdown, detail sheet (approve, reject, payment
  link, document verification); settings for rules, payment and cancellation tiers, approval, texts, arrival
  information, quotas with usage, embed snippets and channels. Dynamic areas get the "Prenotabile online" switch.
- `pkg/ext.BookingChannel` with the host service (signed webhook, pull and push jobs, reservations and events applied
  once, INV-89) and an in-memory fake channel (`PORTO_DEV_FAKE_CHANNEL`, development only); optional
  `PaymentAuthorizer` (manual capture) and `CheckoutRequest.CaptureManual`. No real third-party channel is shipped.
- Lost demand: searches with no answer are recorded as anonymous inquiries (channel `online`, deduped 30 min) and
  appear in report R4; expired unpaid bookings are `lost`, rejections `declined_other`.
- A payment match can be reversed several times, in parts, up to its amount (migration `00017`,
  `payment_matches.reversed_cents` with a CHECK and a conditional update; the API adds `reversed_cents`): a
  cancellation after a date change that refunded part of the payment now refunds the rest instead of being refused.
  Pay balance reuses the open payment request of the same charges.
- Worklist kinds `capture_failed` and `channel_modify`; jobs `bookings.approval_expire`, `bookings.capture`,
  `channels.pull`, `channels.push`, `portal.cleanup`; e-mail templates `booking_*` and `portal_login` (it, en).
- API: tags `booking-public`, `account`, `online-admin`, `channels`; webhook `/webhooks/channels/{channel}/{marinaId}`;
  problem types `booking_unavailable`, `booking_rules`, `booking_busy`, `consent_required`, `text_outdated`,
  `refund_changed`, `price_changed`, `cancellation_not_allowed`, `nothing_due`, `idempotency_mismatch`; event
  `transit.approval_requested`. Public endpoints are rate limited per IP prefix and answer `404 link_invalid`
  without enumeration.
- Migration `00016_online_booking` (SQLite and PostgreSQL): online settings, versioned texts, online bookings,
  quota counters and claims, portal links and sessions, channel connections, reservations and events;
  `consents.kind` accepts `terms`, `contracts.confirmation_source` accepts `channel`.

M7 "Report e cruscotto" (spec 21, 13.2, 26.1, 27, m7-plan, ADR 0012):

- Reports (`GET /marinas/{id}/reports/{key}`, tag `reports`): occupancy (physical and contractual, berths, land and
  dynamic areas in cm-nights / cm²-nights), occupancy efficiency, allocation waste with lost potential revenue and
  read-only swap/move suggestions (fit + occupancy engine), lost demand (unanswered and refused inquiries),
  customers and renewals, production (contract competence), accruals and deferrals (`accrual.Allocate`), detailed
  charges (competence or issue basis) and payments (receipt basis), revenue classes, receivables ageing and
  reminders, presences, rights and condominium, cash and fiscal receipts, hosted vessels, revenue per berth metre.
  Raw rows from `internal/store/report_*.go` (always `marina_id` first, no money or ratio in SQL), pure
  `internal/domain/report/{calc,align,swap,export}` (integer cents/cm/basis points, ratios recomputed from sums),
  one read transaction per request, no writes (INV-61). Migration `00015_report_indexes` (indexes only, SQLite and
  PostgreSQL).
- Permissions: `reports.operational` (occupancy, efficiency, waste without money, lost demand without value,
  presences without revenue, vessels) and `reports.all` (everything with money); customer names only with
  `customers.read`; another marina is 404.
- Manager dashboard (`GET /marinas/{id}/dashboard`, spec 21.2) replacing the overview placeholder: tiles with
  period value, same period last year, change and trend, sparkline and breakdowns; every tile opens its report
  with the same filter, and its value is the fold of the very records of that report (INV-62, acceptance test on
  the demo data). Period presets (month/year to date, last 30 days, last 12 months, custom up to 400 days), date or
  weekday alignment, and map / pier / group / category selectors (carried into every tile drill). The direction
  phone screen shows the tiles in one column.
- Period comparison (`GET /marinas/{id}/reports/comparison`): up to 5 years back, day/week/month buckets, date or
  weekday alignment (364 days per year), line chart and change table.
- Exports: every report and the comparison as Italian CSV (BOM, `;`, CRLF, decimal comma, GG/MM/AAAA, formula
  injection guard) and Excel (`internal/xlsx`, sheets "Dati" and "Riepilogo"), built from the same rows as the JSON
  with the `Totale` row from the totals (INV-63); cap 200 000 rows (`report_too_large`).
- Web: nav entry "Report" (group Analisi), report index filtered by permission, report page with filter bar
  (search params, so a tile link reproduces the filter), summary cards, shadcn chart, groups and paginated records
  with links, export buttons; responsive down to phone width. i18n namespaces `reports` and `dashboard` (it, en).
- Out of M7 (seams): yard reports and tiles (M10), rental reports and tiles (M11), shown as "Disponibile con
  M10/M11"; storage-specific reports (M9).

M6b "Offline, telefono e desktop remoto" (spec 29.2, 31.2, 31.3, m6b-plan, ADR 0011):

- PWA: installable admin panel with a hand-written service worker (`web/admin/src/sw/sw.ts`, precache list written
  after the build by `scripts/sw-precache.mjs`) that caches only the shell and never intercepts `/api/*`; update
  toast; `sw.js` served with `no-cache` and `Service-Worker-Allowed: /admin/`. Registration is skipped inside the
  desktop window and in development.
- Offline module `web/shared/src/offline`: IndexedDB cache of an allowlist of current-marina queries (maps, day view,
  arrivals and departures, collectibles, cash session) for 48 hours, tagged with user and marina, without secrets or
  signed links, deleted on logout (INV-84); operation queue with an `Idempotency-Key` generated at enqueue time,
  a single runner per origin (Web Locks), ordering per vessel lane and dependencies (photo after its presence),
  replay on reconnection (online event, page visible, SSE reconnect). Queue first (D2): arrivals and departures on
  an existing allocation (with a note), presence photos and cash collection on invoice-mode charges always go through
  the queue. Every other operation is refused offline with the reason shown inline (`OfflineReason`); online-only
  hooks call `assertOnline`. `OfflineIndicator` and `QueueSheet` show the queue, conflicts and the explicit
  "Scarta" / "Riprova come nuova operazione" actions; logout with unsent operations asks first and a marina switch
  is refused. Yard, maintenance and rental operations wait for their domains (M9-M11).
- Server: `OfflineGuard` (header `Porto-Offline-Queued-At`, operations flagged `x-porto-offline` only, 422
  `offline_not_allowed` elsewhere, INV-71); offline idempotency keys kept 7 days, stale-claim takeover after 2 minutes,
  `Retry-After` answers never stored; a domain unique key per presence (migration `00014_offline_bridge`, SQLite and
  PostgreSQL) so a replay has one effect and the same answer (INV-83); 409 `allocation_changed` when the berth seen
  by the worker changed, 409 `cash_session_required` when the cash session closed meanwhile; `offline_queued_at` in
  every audit row of a replay; new `listCollectibles` read for the dock.
- Phone screens (`web/admin/src/features/phone`, PhoneShell): `landingFor()` sends dock workers to the dock and
  directors to direction on a phone. Dock: today (arrivals, departures, vessels out, one-tap Arrivato / Partito with
  note and photo, "In coda" overlay), map of the day with four contexts, transit QR scan with the camera
  (BarcodeDetector or jsQR, photo fallback, USB keyboard-wedge readers), cash collection, and online-only
  unexpected arrival, vessel measure, releases and blocks. Direction: dashboard placeholder with today's counts,
  read-only map, deadlines of the next 30 days. Transit QR `PT1:<id>` (`getTransitQr`, SVG) with the
  "QR per l'ormeggiatore" button on the desk transit page.
- Remote desktop mode (`internal/desktop/remote`): the desktop app paired with an online Porto runs a loopback proxy
  that holds the device token (OS keychain) and never hands it to the page (INV-85), pins the server certificate
  (leaf fingerprint from the pairing payload, then its CA), caches the panel shell for an offline start and answers
  503 `remote_unreachable` while the server is down; app PIN for local administration; revocation brings the app back
  to the first run. `getMe` accepts the device token. Kiosk mode for the front desk (macOS, Windows, Linux), exit
  with Cmd/Ctrl+Shift+K and the PIN.
- Peripheral bridge (`/api/bridge/v1`, loopback only): interfaces in `internal/desktopport` with fake drivers for
  receipt printer, fiscal device, QR reader and label printer, a generic ESC/POS receipt printer (CP858, 80/58 mm)
  and a generic ZPL label printer over TCP 9100, CUPS raw or file; "Stampa scontrino" on the payment page and the
  dock cash tab. Fiscal driver `bridge` on the server: the desktop chosen in the billing settings claims documents
  with 60-second leases, at most one claim per document (INV-86), idempotent outcomes. Real fiscal device models are
  out of scope (listed in `docs/desktop-build.md`).
- Local CA of new installations name-constrained to private addresses and `.local`; certificate and iOS profile
  downloadable on the LAN, with a help page (`/admin/help/certificate`).
- Tests: `TestOfflineGuard_RefusesNonOfflineOps`, `TestConcurrency_OfflineReplay_{Presence,CashPayment,Photo}`,
  `TestConcurrency_IdempotencyTakeOver`, `TestConcurrency_FiscalBridgeClaim`, `TestAcceptance_M6bRemote`, remote
  proxy, pinning, kiosk and peripheral unit tests, Vitest for the offline module, scanner and phone screens,
  Playwright `offline-queue` (no duplicates on reconnection), `pwa`, `phone-dock`, `phone-scan`, `phone-direction`
  (Pixel 7) and desktop `remote`, `kiosk`, `lan-ca`. The M3 `dock.spec.ts` is replaced by `phone-dock.spec.ts`.

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

### Changed

- `scripts/with-postgres.sh` keeps the PostgreSQL data directory in memory (tmpfs) and removes the container with its
  volumes, so test runs no longer leave anonymous volumes behind.

### Fixed

M9 review:

- A scenario apply prices the boats brought in from the "Da collocare" tray like a desk layout save (read-only
  before the transaction, applied inside it; contract total, audit diff and `price_effect` of the apply result).
- The optimizer keeps a boat accepted with a fit override (size, weight); `support_not_allowed` stays fatal. An area
  holding an overridden boat can be optimized and applied.
- A zero-total utility run writes no `utility_billings` row: the consumptions are closed with `zero_amount`
  instead of staying pending for ever; Q21 also reports a billing without a charge.
- Cancelling a utility charge puts its consumptions back to pending in the same transaction (INV-90).
- Exit-day accessibility ignores obstacles starting on the exit day (exits before arrivals, D7).
- A maintenance reschedule keeps the nights already held; the task state table is the plan's again (an outcome on a
  task never started passes through `in_progress`).
- SQLite test databases are WAL files in the test temp dir, as in production: the memdb VFS blocked every reader
  while a writer held RESERVED and made `TestConcurrency_ScenarioApply` fail with SQLITE_BUSY under `-race`.
- New: `listStorageTrayCandidates` and the "Da collocare da altre risorse" card of the scenario editor; the storage
  contract form's marina cradle picker and price preview; M9 cases in `a11y.spec.ts`.
- New tests: `TestScenarioApply_TrayItemRepriced`, `TestArrange_FitOverride`, `TestScenarioApply_FitOverriddenBoat`,
  `TestUtility_ZeroTotalNoOrphanBilling`, `TestUtility_CancelledChargeReopensConsumptions`,
  `TestMaintenance_RescheduleKeepsPastNights`, `TestTaskTransitions_Table`,
  `TestContract_M9StorageStateChangesWriteAuditAndOutbox`, `TestAcceptance_CrossMarinaResourceIdsM9`,
  `TestConcurrency_OfflineReplay_{Reading,MaintenanceReport,MaintenanceOutcome}`.

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

- M9, open for a product decision: report access for `yard_mgr` (today refused every report, storage included) and
  `presence.write` for `yard_mgr` to record haul-out and launch. A storage contract price preview needs an end date
  (seasonal and annual ends are derived by the server only at creation).

- M6a: the window build was not opened on a real display in CI; Windows and Linux webview builds are compiled
  only by the `desktop.yml` jobs; the Linux AppImage step needs `APPIMAGETOOL_SHA256` (repository variable) or a
  local `APPIMAGETOOL`. Signing and notarisation need Coste360 certificates. M6b (PWA offline queue, per-role phone
  screens, remote mode, kiosk, local peripherals bridge) is not built.
- M7: the demo seed has no activity in the previous year, so the previous-year tiles are zero and the
  drill-down equality is exercised for real on the current year only; seeded inquiries all arrive in the future,
  so lost demand is zero for periods ending today. The dashboard page offers only period, comparison and
  alignment (the API also accepts map/structure filters).
- Demo seed: the 2 tacit-renewal contracts and the charter fleet of 8 vessels on pier E (m4-plan §10) are not
  seeded yet.
- Public quote and renewal acceptance do not return `signature_link` (m4-plan §15 D33, open for a product
  decision).
