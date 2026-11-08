# M6b — Offline, phone screens and remote desktop: work order

Status: Plan phase output, authoritative for the M6b builders. If it conflicts with `docs/spec/`, the spec wins:
stop and report to the orchestrator. No open decision of `docs/spec/33-decisioni-aperte.md` is touched. §13 lists
every detail decided here where the spec is silent; CONTRACT records the binding ones in ADR 0011.

Sources: spec 29.2 (offline, "come Arenile" 21.2 / ADR Arenile 0029), 31.2 (remote mode, bridge), 31.3 (phone,
role table), 26 (roles), 27 (API), 30 (security), 32 (M6 acceptance); `docs/invariants.md` (INV-01, INV-08/09,
INV-30, INV-52, INV-70, INV-71, INV-73, INV-78..82); ADR 0001, 0008, 0009, 0010; `docs/m0-plan.md` (idempotency
store, devices), `docs/m3-plan.md` §8.6 (dock panel), `docs/m5-plan.md` §5 (cash, fiscal, receipts),
`docs/m6a-plan.md` §1.2 (seams); `.claude/rules/*.md`; skills `new-endpoint`, `new-migration`, `shadcn`.

## 0. Ground rules

All of `docs/m6a-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001). Analogues to read for behaviour only: Arenile spec 21.2,
   ADR 0029 (transient vs final conflicts), its panel offline queue, desktop remote mode and bridge. Never copy
   files, code, comments or tests; never modify `../arenile`.
2. **The server is the single source of truth** (INV-01). The client never decides availability, numbering or
   amounts; a queued operation is a request that the server may refuse. No sync, no client-side merge.
3. **Offline never creates or moves allocations, transits, contracts or rentals, and never issues numbered
   commercial documents** (29.2, INV-71). Enforced twice: the web queue accepts only allowlisted operations, and
   the server refuses any request marked as an offline replay on a non-allowlisted operation (§4.4).
4. Replays are idempotent: `Idempotency-Key` generated at enqueue time, same key on every retry, server dedupe in
   the DB, same response on replay. A conflict is shown to the user; nothing is silently overwritten.
5. Offline data is scoped to the current user and marina, cleared on logout and on revoke, and never contains
   secrets (no tokens, no signed links).
6. API first (CONTRACT edits `api/openapi.yaml`, runs `make gen`); permissions in domain services; every marina
   query through `store.MarinaScope`; migration `00014` both dialects; no PII in logs, SSE, events.
7. UI: shadcn only (CLI in `web/shared`), Tailwind tokens, no custom CSS, i18n it (source) + en.
8. `cmd/porto` stays `CGO_ENABLED=0`; CGO only in `cmd/porto-desktop` files with the `webview` tag; CI and
   `make check` never need the webview toolchain. Every peripheral driver is pure Go.
9. Do not `git commit`; no destructive git; no `rm -rf`; Docker only for your own throwaway containers
   (`scripts/with-postgres.sh`). Edit only files you own (§11); request other changes in your report.
10. Test speed: while iterating run only the touched packages / Vitest files. Full gates only in §12.

## 1. Scope

### 1.1 In M6b

- **PWA**: installable admin panel (manifest exists), hand-written service worker caching the shell, update prompt.
- **Offline read cache**: allowlisted queries of the current marina persisted in IndexedDB for 48 h.
- **Offline queue** of the operations 29.2 allows *that exist today*: arrival/departure of a vessel with an
  existing allocation (with notes), presence photos, cash collection on existing charges (documents or transits).
  Server-side idempotency hardened for exactly these operations.
- Forbidden operations refused clearly in the UI when offline, and refused by the server when replayed.
- **Phone screens** for the dock role (all 31.3 dock functions whose domain exists) and the direction role
  (dashboard placeholder, map, deadlines); landing seam for the other roles.
- **Transit QR**: payload format, QR endpoint, camera scan on the phone (and keyboard-wedge scanners on desks).
- **Remote desktop mode**: the desktop app as a client of an online Porto with a per-device token, fingerprint
  pinning, shell cache for offline start, kiosk for the front desk.
- **Bridge to local peripherals** behind interfaces: receipt printer (fake + generic ESC/POS), local fiscal driver
  (fake only), QR reader (keyboard wedge + fake), label printer (fake + generic ZPL over TCP).
- Local CA download for phones (service worker on the LAN origin), name-constrained CA.

### 1.2 Out of M6b

| Item | Where | Seam left by M6b |
|---|---|---|
| Meter readings offline (29.2 "letture dei contatori") | M9 (utility readings, spec 20.4) | queue registry entry kind `utility.reading` reserved, not enabled |
| Maintenance reports and outcomes offline, dock "segnalazioni di manutenzione" | M9 | kind `maintenance.report` / `maintenance.outcome` reserved |
| Yard hours and activity completion offline, yard phone screens | M10 | kind `yard.timesheet` / `yard.activity_done` reserved; landing seam |
| Rental delivery/return checklists offline, rental phone screens | M11 | kind `rental.checklist` reserved; landing seam |
| "Assigned activities" in the offline cache | M9–M11 | allowlist slot `activities.assigned` (empty) |
| Piazzale (storage) phone screens | M9 | landing seam |
| Dashboard KPIs on the direction phone screen | M7 | placeholder card |
| Transit QR in confirmation emails / customer area | M8 | payload format `PT1:` fixed here (§6.4) |
| Sensors, reports | M12, M7 | — |
| Native mobile app | never in this repo (31.3) | `porto://pair` payload kept |
| Real fiscal device models: Epson FP-81 II RT / FP-90III RT (FPMate XML over HTTP), RCH Print!F / MyCash, Custom KUBE II / Q3X (Custom XML), Olivetti Form 200 RT / Nettuna 7000, Ditron Zip/Sole, Axon, Micrelec | extensions | `ext.FiscalDriver` on the desktop + bridge claim API (§8.5) |
| Windows raw USB printing (winspool), serial (COM) QR readers, Bluetooth printers | later | `peripheral.Transport` interface |
| Background Sync API, push notifications | later | queue runs in the page |
| Tray icon / keep running, autostart, mDNS, signed packages, offline owner password recovery (M6a D22) | later | M6a seams unchanged |
| Offline map background images | later | schema mode works without them |
| Vessel measurement, releases, blocks, unexpected arrival offline | never offline (§4.2) | online-only buttons |

## 2. Architecture overview

```
 phone / LAN browser / server browser            desktop window (remote mode)
 ┌───────────────────────────────┐               ┌────────────────────────────────────────────┐
 │ SPA /admin/                   │               │ SPA /admin/ (same build, served via proxy) │
 │  sw.js (shell only)           │               │  no SW: shell cache is in the Go proxy     │
 │  IndexedDB porto-offline      │               │  IndexedDB porto-offline (same queue)      │
 │   cache (48 h) · ops · blobs  │               │  bridge client → /api/bridge/v1            │
 └──────────┬────────────────────┘               └──────────┬─────────────────────────────────┘
            │ cookie session (+CSRF)                        │ http://127.0.0.1:<port> (proxy cookie)
            ▼                                               ▼
   Porto server (online / in sede / desktop LAN)   porto-desktop remote: internal/desktop/remote
   Idempotency (7 d for offline ops) ·             proxy → https://online (Bearer po_dev_, pinned TLS)
   OfflineGuard · domain dedupe keys               /api/desktop/v1 + /api/bridge/v1 served locally
                                                   fiscal agent: claim → local FiscalDriver → outcome
```

## 3. PWA and service worker (OFFLINE)

### 3.1 Build

- No PWA plugin (Vite 8 compatibility): `web/admin/src/sw/sw.ts` is a second Rollup input built to
  `dist/sw.js` (no hash). `build.manifest: true`. Post-build script `web/admin/scripts/sw-precache.mjs` (run by the
  admin `build` script) reads `dist/.vite/manifest.json`, lists `index.html`, every emitted JS/CSS/font, the
  manifest and icons, and replaces the `self.__PORTO_PRECACHE__` placeholder in `dist/sw.js` with
  `{version: <app version>, build: <sha256 of the sorted asset list>, urls: [...]}`. No new runtime dependency.
- `site.webmanifest` gains `"orientation": "any"`, `"categories": ["business"]`, `shortcuts` (dock: Oggi, Scansiona).
- `internal/http/static`: `/admin/sw.js` served with `Cache-Control: no-cache` and `Service-Worker-Allowed:
  /admin/`; `/admin/.vite/*` not served.

### 3.2 Strategy

- Scope `/admin/`. Precache the list on `install` into cache `porto-shell-<build>`; delete older `porto-shell-*`
  on `activate`.
- Navigations under `/admin/`: network first with a 3 s timeout, fallback to the cached `index.html` (a deploy is
  picked up as soon as the network answers).
- Hashed assets: cache first. **`/api/*`, `/healthz`, `/readyz`, SSE, signed file links: never intercepted** (the
  SW does not even call `respondWith`). The SW never stores API data; API data lives in IndexedDB (§4).
- **Update prompt**: the new SW waits; `web/admin/src/pwa/register.ts` detects `registration.waiting` and shows a
  persistent toast "Nuova versione disponibile · Aggiorna" (`offline:update.*`); on click it posts
  `{type:'SKIP_WAITING'}` and reloads on `controllerchange`. Never auto-reload while a form is dirty. The queue
  survives updates (IndexedDB schema versioned, §4.5).
- Not registered when `isDesktopWindow()` (desktop window: local mode needs none, remote mode caches in Go) or when
  `meta.edition === 'desktop'` on loopback. Registration failure is caught and reported in the offline status.

### 3.3 Origins and certificates

| Origin | SW | Notes |
|---|---|---|
| `https://` server with a public certificate | yes | normal case |
| `http://127.0.0.1` / `http://localhost` (dev, e2e) | yes | secure context |
| desktop LAN `https://192.168.x.y:8443`, self-signed leaf accepted with a warning | **no** (Chromium refuses SW on certificate errors) | queue and cache still work while the tab is open; the status shows "Avvio offline non disponibile: installa il certificato del porto" |
| desktop LAN after installing the Porto local CA | yes | §9.6 |

Camera (`getUserMedia`) needs a secure context too; an accepted self-signed LAN origin is secure for that purpose.

## 4. Offline data and queue (OFFLINE)

### 4.1 Module layout (`web/shared/src/offline/`)

```
index.ts        public API (CONTRACT stub, signatures below)
db.ts           IndexedDB "porto-offline" (idb-keyval-free, own thin wrapper over indexedDB), stores: cache, ops, blobs, meta
persist.ts      TanStack Query persister restricted to the allowlist, maxAge 48 h
allowlist.ts    OFFLINE_QUERY_ALLOWLIST and OFFLINE_OPERATIONS (= x-porto-offline operationIds)
queue.ts        enqueue, state machine, ordering, dependencies (pure logic + db)
runner.ts       single runner per origin (Web Locks API, BroadcastChannel fallback), backoff, classification
classify.ts     PURE: response → done | retry(after) | conflict | auth
online.ts       useOnlineStatus (navigator events + failed fetch + HEAD /healthz probe every 15 s while offline)
overlay.ts      PURE: pending ops overlaid on cached movements/day view (badges "In coda")
purge.ts        purgeOfflineData()
```

```ts
export type OfflineOpKind = 'presence.record' | 'presence.photo' | 'payment.cash'; // M9–M11 kinds reserved, §1.2
export type OpState = 'pending' | 'sending' | 'retrying' | 'conflict' | 'blocked' | 'expired' | 'done';
export interface QueuedOp {
  id: string; kind: OfflineOpKind; key: string;            // Idempotency-Key, fixed at enqueue
  userId: string; marinaId: string; createdAt: string;      // client clock, RFC 3339
  lane: string;                                             // ordering key, §4.3
  dependsOn?: { opId: string; bind: Record<string, string> } // e.g. {presenceId: 'presence.id'}
  request: { operationId: string; path: string; body?: unknown; blobId?: string; contentType?: string };
  state: OpState; attempts: number; nextAttemptAt?: string;
  problem?: { status: number; type: string; detail?: unknown }; result?: unknown;
  label: { key: string; params: Record<string, string> };  // i18n, for the queue sheet
}
export function enqueue(op: NewOp): Promise<QueuedOp>;
export function useQueue(marinaId: string): { ops: QueuedOp[]; pending: number; conflicts: number;
  sendNow(): void; discard(id: string): Promise<void>; retryAsNew(id: string, patch?: unknown): Promise<void> };
export function useOnlineStatus(): { online: boolean; lastOnlineAt?: string; swActive: boolean };
export class OfflineForbiddenError extends Error { operationId: string }
export function assertOnline(operationId: string): void;   // throws OfflineForbiddenError when offline
export function purgeOfflineData(): Promise<void>;
export const OFFLINE_OPERATIONS: readonly string[];
export const OFFLINE_QUERY_ALLOWLIST: readonly AllowlistEntry[];
```

Dependencies added to `web/shared` by CONTRACT: `@tanstack/react-query-persist-client` (MIT, same major as
`@tanstack/react-query`), `jsqr@1.4.0` (Apache-2.0, PHONE uses it); dev: `fake-indexeddb` (Apache-2.0). Exact
versions pinned, older than two weeks. Nothing else.

### 4.2 Allowlists

**Operations (queueable)** — the only operations with `x-porto-offline: true` in the OpenAPI:

| operationId | Kind | Notes |
|---|---|---|
| `recordPresence` | `presence.record` | events `arrived` / `departed` only, `allocation_id` and `expected_resource_id` required in the queued body (existing allocation, §5.2); `notes` ≤ 1000 |
| `uploadPresencePhoto` | `presence.photo` | depends on the presence op when the presence was queued too; JPEG re-encoded client side to ≤ 2048 px, ≤ 8 MiB |
| `recordPayment` | `payment.cash` | `method: cash`, `kind: payment`, `matches` to existing charges, `cash_register_id` of a cached open session; refused at enqueue when any matched charge has `fiscal_mode = receipt` (§5.3) |

Everything else is online-only. While offline the UI disables the control and shows `offline:forbidden.<reason>`
inline (not only a tooltip, phones have no hover); `assertOnline` guards the mutation hooks of: unexpected arrival
(creates a transit), releases, blocks, moves/swaps, transit create/confirm/cancel/stay, contracts, quotes, vessel
measurement, counter sales, transit payment-at-desk, refunds, cash session open/close/movements, receipts/fiscal.
Reasons: `allocation` ("richiede la garanzia di disponibilità del server"), `document` ("emette un documento"),
`online_only`.

**Queries (cached 48 h, current user + marina)** — matched on TanStack query keys from `queryKeys.ts`:

| Query | operationId | Used by |
|---|---|---|
| marina me / permissions | `getMe`, `getMeta` | shell, landing |
| maps list and geometry | `listMaps`, `getMap` | map |
| day view (today and tomorrow) | `getMapDay` | map, scan resolution |
| movements (today and tomorrow) | `getMovements` | Oggi |
| presences of today | `listPresences` (date = today) | presences |
| payment lens of today | `getMapPaymentStatus` | warnings |
| collectibles (today) | `listCollectibles` (new, §5.4) | Cassa |
| current cash session | `getCurrentCashSession` | Cassa |
| deadlines (next 30 days) | `listDeadlines` | direction |
| activities (assigned) | — (M9–M11 slot) | — |

Never persisted: signed links (`FileLink`, photo/document URLs), anything from `/auth/*` other than `me`, desktop,
device, API keys, webhooks, audit, exports. `persist.ts` dehydrates only allowlisted successful queries;
`buster = appVersion + ':' + userId + ':' + marinaId`; on hydrate entries older than 48 h are dropped. When
offline, screens show "Dati aggiornati alle HH:MM" from `dataUpdatedAt` (`offline:staleSince`).

### 4.3 Queue semantics

- **Queue first**: allowlisted mutations always go through `enqueue` (online too); the runner sends at once when
  online. One code path, tested once.
- **Key**: `crypto.randomUUID()` at enqueue (`newIdempotencyKey`), stored with the op, reused for every attempt.
  The replay adds `Porto-Offline-Queued-At: <createdAt>` (§4.4).
- **Lanes**: `vessel:<id>` for presences and their photos, `charge:<id>` (first matched charge) for payments.
  The runner sends one request at a time, oldest first, skipping lanes that have a `conflict`/`blocked` head;
  other lanes continue. A photo whose parent presence op is not `done` stays `pending`; when the parent ends in
  conflict the photo becomes `blocked`.
- **Dependencies**: `dependsOn.bind` maps path parameters to fields of the parent's stored result
  (`presenceId ← result.presence.id`). Resolution is pure and unit tested.
- **Classification** (`classify.ts`, pure):

| Response | Outcome |
|---|---|
| 2xx (replayed or not) | `done`; result stored; queries invalidated |
| network error, 0, 408, 429, 500–504, 409 `idempotency_in_progress`, 409 `concurrent_update` | `retrying`, same key; backoff 1 s × 2ⁿ, cap 5 min, ±20 % jitter, `Retry-After` honoured |
| 401 | runner paused; "Accedi di nuovo" banner; after login, ops of a *different* user are never sent (discard only) |
| 403, 404, 409 (other), 412, 413, 415, 422 | `conflict`: problem kept; dependents `blocked` |
| op older than 6 days (server keeps offline keys 7 days, §4.4) | `expired`: never resent with the old key |

- **Conflict resolution** (queue sheet, `QueueSheet`): shows the label, the problem (`errors.<slug>`, e.g.
  `allocation_changed` with the current berth code, `invalid_transition` "già registrato", `match_exceeds` "già
  incassato", `cash_session_required`), and actions **Scarta** and, where meaningful, **Riprova come nuova
  operazione** (new key, optional edit such as accepting the new berth). Never automatic.
- **Triggers**: `online` event, SSE reconnect, `visibilitychange` to visible, every 30 s while non-empty,
  "Invia ora". Runner holds `navigator.locks.request('porto-offline-runner')` so two tabs never send the same op.
- **Photos**: Blob in `blobs` (keyed by op id), deleted when the op is `done` or discarded;
  `navigator.storage.persist()` requested on first enqueue; enqueue refused with `offline:quotaFull` when
  `storage.estimate()` leaves < 20 MiB.
- **Logout / revoke / user change**: logout with pending ops asks for confirmation ("N operazioni non inviate
  andranno perse"); then `purgeOfflineData()` deletes the database and the SW shell cache is kept (no user data).
  A 401 whose `getMe` retry also fails (device revoked, session expired) purges the cache but **keeps** the ops
  frozen for the same user until they log in again or discard; ops are never sent with another user's session.
- **Marina switch** is refused while the current marina has unsent ops ("Invia o scarta prima di cambiare").
  Cache entries of other marinas are purged on switch.
- **UI**: `OfflineIndicator` (shell header, always visible on phone layouts: online/offline dot, pending count,
  conflicts in destructive colour) and `QueueSheet` (shadcn `drawer`). Pending ops are overlaid on cached
  movements/day view (`overlay.ts`): the row shows "In coda" and the next action, so the dock worker can do an
  arrival then a departure offline.

### 4.4 Server guarantees (OFFLINE)

- **Policy flag**: `middleware.Policy.Offline` read from `x-porto-offline: true` (same loader as
  `x-porto-max-body-bytes`). `TestPolicies_OfflineSet` pins the set to the three operations of §4.2;
  `offline.allowlist.test.ts` asserts `OFFLINE_OPERATIONS` equals the operations flagged in `api/openapi.yaml`.
- **`middleware.OfflineGuard`** (after `Enforce`, before `Idempotency`): a request with `Porto-Offline-Queued-At`
  on an operation without the flag → **422 `offline_not_allowed`**; on a flagged operation the header must be a
  valid RFC 3339 instant not after now + 10 min and must come with an `Idempotency-Key` (else 422
  `validation_failed`); the instant goes into `reqctx` and every audit row of the request records
  `offline_queued_at` (no other data). This makes INV-71 enforceable server side.
- **Idempotency middleware** for flagged operations: TTL **7 days** (24 h otherwise); a claim still
  `in_progress` after 2 minutes is taken over by a conditional UPDATE on `(id, state, updated_at)` (a crash between
  the handler commit and `Complete` must not block the key for days); responses with `Retry-After` are not stored
  (as 5xx). The Idempotency-Key parameter is declared on all three operations (`uploadPresencePhoto` lacks it today).
- **Domain dedupe** (second layer, survives a lost middleware row or a takeover):
  - presences: `presences.idempotency_key` + `idempotency_hash`, unique `(marina_id, idempotency_key)` where not
    null (00014). `presence.Record` with a key returns the existing presence (same hash) or 422
    `idempotency_key_reused`; a concurrent unique violation re-reads the winner (as `payment.Record` does);
  - payments: existing `payments.idempotency_key` unique (M5) — verify the HTTP path passes the header (it does,
    `internal/http/admin/payments.go`) and add the concurrency test;
  - photos: natural (one photo per presence, a replay overwrites with identical bytes); the handler passes the key
    so the middleware replays the stored `PresencePhoto`.
- **Transient conflicts**: if `store.RunInTx` surfaces PostgreSQL serialization failures / SQLite busy as a plain
  409, OFFLINE adds `store.ErrBusy` (wrapping `ErrConflict`) and problem `concurrent_update` (409, `Retry-After:
  1`), as a Porto decision analogous to Arenile ADR 0029; otherwise nothing changes.

### 4.5 IndexedDB schema

DB `porto-offline` version 1: `meta` (`schema`, `userId`, `marinaId`, `appVersion`), `cache` (persister blob per
`userId:marinaId`), `ops` (key `id`, indexes `byUserMarina`, `byLaneCreated`), `blobs`. Upgrades are additive;
an unknown future version is opened read-only and the user is told to update.

## 5. Server changes per allowed operation (OFFLINE unless marked)

### 5.1 Summary

| Operation | Natural conflicts surfaced | New |
|---|---|---|
| `recordPresence` | 409 `invalid_transition` (office already recorded it), 409 `allocation_changed`, 404 vessel/allocation gone | `expected_resource_id`, domain key |
| `uploadPresencePhoto` | 404 presence, 413, 415 | Idempotency-Key param, policy flag |
| `recordPayment` (cash) | 409 `match_exceeds` (already paid), 409 `cash_session_required` / `cash_session_closed`, 422 `offline_not_allowed` (receipt-mode charge) | offline guard on receipt mode |

### 5.2 Presence

- `PresenceCreate.expected_resource_id` (optional, ≤ 36): when present with `allocation_id`, the allocation must be
  active, of the vessel, and on that resource for the night, else **409 `allocation_changed`** with problem
  extensions `{current_resource_id, current_resource_code}` (null when cancelled). Without it, behaviour unchanged.
- Offline replays (`reqctx` has `offline_queued_at`) require `allocation_id`, `expected_resource_id` and
  `event ∈ {arrived, departed}` (422 `offline_not_allowed` otherwise): an arrival without allocation is not an
  offline operation, and without the expected resource a moved allocation could not surface as a conflict.
- Photo replays: the same bytes (SHA-256) as the presence's current photo answer with the current file id, nothing
  written again (domain key of the effect, INV-83).
- `at` defaults to the queued instant for replays (the client sends it anyway).

### 5.3 Cash collection

- Offline replay of `recordPayment` requires `method = cash`, `kind = payment`, at least one match, every match
  positive and their sum equal to the amount (no unmatched credit, INV-87), no `auto_match`, and no matched charge
  with `fiscal_mode = receipt` → else 422 `offline_not_allowed`. The client refuses the same cases at enqueue
  (`validateNewOp`, `OfflineForbiddenError('document')`): the caller states the receipt mode of the matched charges
  (`issuesDocument` / `receiptMode`, unknown counts as receipt), and receipt-mode cash is always a direct online POST,
  never a queued op (a lost first answer must not turn it into a refused offline replay). A cash payment on an
  invoice-mode charge creates no fiscal document; its courtesy receipt is printed later from the payment page.
- `received_on` = marina date at enqueue (past dates already allowed). The open session is re-resolved at replay;
  a closed session is a conflict (`cash_session_required`), never a silent move to another session.

### 5.4 `listCollectibles` (new read, for the dock Cassa tab and its cache)

`GET /api/admin/v1/marinas/{marinaId}/collectibles?date=` (default today; `payments.collect` or
`payments.collect_cash`): open charges (residual > 0, not cancelled) of contracts with an allocation on the night
or a movement (arrival/departure) on the date. Item `{charge_id, number, contract_id, contract_code, vessel{id,
name}, customer_id, customer_display_name?` (only with `customers.read`), `description, due_on, residual_cents,
fiscal_mode}`; paginated as usual. The dock role cannot use `listCharges` (needs `payments.collect`), hence this
narrow read. Service in `internal/domain/payment/collectibles.go`, store query in
`internal/store/collectibles.go`.

## 6. Phone screens (PHONE)

### 6.1 Landing and layout

- `landingFor(permissions, kind, opts?)` (`web/admin/src/lib/landing.ts`) returns:
  `presence.write` or (`map.view` and `transits.desk`) → `/m/<id>/dock`; `reports.all` or `marina.configure`
  without `presence.write` → `/m/<id>/direction` on mobile; yard, yard_mgr, rental, maintenance-only → `/m/<id>`
  (seam table in a comment, filled in M9–M11). Used after pairing (kind `mobile`) and after login on viewports
  < 768 px (`DashboardPage` redirect instead of the M3 link card).
- `PhoneShell` (`web/admin/src/features/phone/PhoneShell.tsx`): top bar (BrandLogo mark, marina name,
  `OfflineIndicator`, queue sheet), content, bottom tab bar (shadcn `tabs` styled with tokens), touch targets ≥
  44 px, works at 360 px, safe-area padding via Tailwind `pb-[env(safe-area-inset-bottom)]`. Phone routes render
  inside `PhoneShell` instead of `AppShell`; a "Versione completa" link opens the normal shell.

### 6.2 Dock (`/m/$marinaId/dock?tab=`)

| Tab | Content | Offline |
|---|---|---|
| **Oggi** | arrivals expected, departures expected, in port, out, arrived without booking (from `getMovements`), each row with one-tap **Arrivato / Partito** (sheet: note, photo from camera `<input capture="environment">`), **Incassa** when collectibles exist; payment warning on departures | arrive/depart/photo queued; overlay badges |
| **Mappa** | day map of the main map (`HarborMap` compact, schema mode default) with context switch (presence, arrivals/departures, payments, contract type) and legend counts; tap → bottom sheet with the element context (vessel, contract, presence, payment state) and its actions | read from cache; actions as in Oggi |
| **Scansiona** | camera QR (§6.4); result opens the transit card: vessel, berth, nights, payment state, actions Arrivato/Partito/Incassa/Foto | resolution from cached day view + movements; unknown id offline → "non trovato nei dati del giorno" |
| **Cassa** | current session state; collectibles list; collect cash (amount prefilled with residual, editable down) → queued `payment.cash`; receipt link online | queued; disabled with reason when no cached open session or receipt-mode charge |
| **Altro** | Arrivo non previsto (existing `UnexpectedArrivalForm`), Misura barca (`recordVesselMeasurement`, `LengthInput`), Rilasci, Blocchi (existing forms), queue sheet, device info, logout | all online-only with the inline reason |

The M3 `DockPage` becomes this screen (same route, same `data-testid="dock-page"`); presences are the Oggi tab.
Desk components are reused through their public exports; PHONE does not edit `features/desk/**`.

### 6.3 Direction (`/m/$marinaId/direction?tab=`)

Cruscotto (placeholder card "Cruscotto completo con M7", today counts from cached movements: arrivals, departures,
in port), Mappa (same component as dock, read-only actions), Scadenze (`listDeadlines` next 30 days, grouped by
week, tap opens the full page).

### 6.4 Transit QR

- Payload (fixed, D10): `PT1:<contract_id>` (UUID). Opaque, no personal data, no URL (a desktop in local mode
  has no reachable base URL for customers); resolution always needs an authenticated panel.
- `GET /api/admin/v1/marinas/{marinaId}/transits/{contractId}/qr` (`getTransitQr`, `contracts.read` or
  `transits.desk`, 404 for non-transits and other marinas) → `image/svg+xml` via `internal/qrsvg`. Handler
  `internal/http/admin/transit_qr.go`, service method in `internal/domain/contract/transit_qr.go` (authorisation
  + kind check only). The desk transit detail gets a "QR per l'ormeggiatore" button (one-line insert, PHONE);
  M8 will put it in emails.
- Scanner `web/shared/src/scan/`: `parseTransitQr(text)` (pure), `CameraScanner` (uses `BarcodeDetector` when
  available, else `jsqr` on frames from a `<video>` at ≤ 10 fps, torch toggle when supported, stops tracks on
  unmount), file fallback "Scegli una foto del QR" (also used by e2e), `useKeyboardWedge(onScan)` for USB HID
  readers (≥ 8 chars within 50 ms ending with Enter; ignored inside text inputs). Manual entry of the transit
  code as last resort (online search).
- `Permissions-Policy` becomes `camera=(self), microphone=(), geolocation=(), payment=()` (CONTRACT, basic.go).

## 7. Remote desktop mode (REMOTE)

### 7.1 Model

`desktop.json` `mode: remote` (accepted from M6b) with `remote{url, device_id, marina_id, marina_name, leaf_fp?,
ca_fp?, paired_at}`; the device token `po_dev_…` lives in the `SecretStore` (account
`device-token/<data dir hash>`), never in `desktop.json`, never in the page. No database, no `app.App`: the launcher
starts `remote.Proxy` on `127.0.0.1:<port>` and opens the window on it.

### 7.2 Pairing

1. On the online instance an office user opens *Dispositivi* → kind **Desktop** → the dialog shows the code and a
   "Copia collegamento" button with the `porto://pair?v=1&url=…&code=…&fp=…` payload (exists since M6a).
2. Desktop first run offers "Collega a un Porto online": paste the link, or URL + code; set the **app PIN**
   (6–12 digits, Argon2id in `desktop.json`) used for local administration in remote mode (§7.5).
3. `POST /api/desktop/v1/remote/pair` (first-run token or PIN) → the launcher calls `POST <url>/api/device/v1/pair`
   (`kind: desktop`, platform, app version) with the TLS policy of §7.3 → stores token, `getDeviceSelf` for marina
   and permissions → restarts into remote mode.
4. Unpair (`DELETE /api/desktop/v1/remote`, PIN): `unpairDeviceSelf` best effort, delete token, remote shell cache
   and settings → back to first run. A 401 from the remote (revoked) does the same locally and shows
   "Dispositivo revocato".

### 7.3 TLS and pinning

- URL must be `https` (`http` only with `PORTO_DEV=true`).
- Payload with `fp` (self-signed / LAN target): accept the connection only if the leaf SHA-256 equals `fp`; at
  pairing record `ca_fp` of the presented chain's issuer when the chain verifies against that issuer. Afterwards a
  leaf rotated under the same CA is accepted (chain verifies to the pinned CA, SAN matches host); any other
  certificate is refused (`remote_pin_mismatch`, shown with both fingerprints). Built on `lan.PinnedClientConfig`.
- Payload without `fp`: system roots and hostname verification, no pinning (public CAs rotate leaves every 90 days).

### 7.4 Proxy (`internal/desktop/remote`)

- Local routes: `/api/desktop/v1/*` (remote subset of the desktop API), `/api/bridge/v1/*` (§8), `/__porto/enter`.
  Everything else is reverse-proxied to `remote.url` with `Authorization: Bearer <token>` added, `Cookie` and
  `Set-Cookie` stripped both ways, `Host` rewritten, `X-Forwarded-*` dropped, SSE flushed per event, upload size
  limits passed through. CSRF is not needed upstream (bearer).
- **Who may use the proxy**: `HostCheck` (`127.0.0.1:<port>`, `localhost:<port>`); a per-launch random secret passed
  to the window in the URL fragment of `/__porto/enter#k=…`, exchanged once for an `HttpOnly; SameSite=Strict`
  cookie `porto_remote`; every request needs it (401 otherwise), unsafe methods also need `Sec-Fetch-Site:
  same-origin` or a matching `Origin`. The token never appears in responses, logs or the page (INV-85).
- **Shell cache**: successful `GET /admin/` (index) and `/admin/assets/*`, `/admin/*.png|svg|webmanifest` are
  stored in `<data>/remote-cache/` (index replaced atomically, assets by name, pruned to the assets referenced by
  the last two index versions). Remote unreachable → index and assets from the cache; `/api/*` → **503
  `remote_unreachable`** problem, which the web queue treats as retryable. `/api/desktop/v1/status` reports
  `remote.reachable`. Remote `meta.version` major different from the app's → banner "aggiorna l'app".
- Dev only (`PORTO_DEV=true`): `POST /api/desktop/v1/dev/remote-offline {offline}` makes the proxy behave as
  unreachable (e2e).

### 7.5 Administration in remote mode

No local users exist. Peripheral settings, kiosk, unpair and kiosk exit require the **app PIN** in header
`Porto-App-Pin` (rate limited 5 / 15 min, audit to the local log only, never the PIN). Local mode keeps the M6a
rule (owner, loopback) for the same endpoints.

### 7.6 Kiosk

- `desktop.json kiosk{enabled}`, available in local and remote mode, set from *App desktop* (owner or PIN).
- `Window` honours `WindowOptions{Kiosk: true}` (reserved in M6a): full screen without decorations, no resize,
  no menus (macOS presentation options hide Dock and menu bar; Windows popup window sized to the monitor,
  topmost; GTK fullscreen), dev tools off, context menu off, navigation limited to the panel origin (`webview.Init`
  script blocks `window.open` and external `<a target>`; `openExternal` disabled), close shortcuts ignored.
- Exit: Cmd/Ctrl+Shift+K opens `/admin/desktop/kiosk-exit` → PIN (local mode: owner password) →
  `POST /api/desktop/v1/kiosk/exit` → window leaves kiosk until the next launch.
- Kiosk start route: `/admin/m/<id>` (desk). Platform code lives in `cmd/porto-desktop/kiosk_{darwin,windows,linux}.go`
  (`webview` tag); the decision logic (`KioskPolicy`) is pure Go and tested.

## 8. Bridge and peripherals (REMOTE)

### 8.1 Interfaces (`internal/desktopport/peripheral.go`, CONTRACT)

```go
type ReceiptPrinter interface { Code() string; Status(ctx context.Context) PeripheralStatus; Print(ctx context.Context, r ReceiptDoc) error }
type LabelPrinter   interface { Code() string; Status(ctx context.Context) PeripheralStatus; Print(ctx context.Context, l LabelDoc) error }
type QRReader       interface { Code() string; Next(ctx context.Context) (string, error) } // blocks until a scan or ctx done
// Local fiscal device: the existing ext.FiscalDriver (Issue/Void/Status; Deferred() errors keep the doc pending).
type Peripherals struct { Receipt ReceiptPrinter; Label LabelPrinter; Reader QRReader; Fiscal ext.FiscalDriver }
type PeripheralStatus struct { Driver string; Ready bool; Message string } // message is a code, never PII
type Transport interface { Write(ctx context.Context, b []byte) error }    // tcp://host:9100, cups://queue, file:// (dev)
```

`ReceiptDoc` is built from the new `ReceiptData` (§10) — marina header, number, date, lines, VAT summary, method,
total, footer, "Non valido ai fini fiscali" — so no driver parses HTML.

### 8.2 Drivers shipped (`internal/peripheral/**`, pure Go, no new dependency)

| Package | Driver | Notes |
|---|---|---|
| `fake` | receipt, label, reader, fiscal | append JSON lines to `<data>/peripherals/<kind>.jsonl`; fiscal `mode: ok\|offline\|fail` (offline returns a `Deferred()` error); reader fed by `fake.Push` (tests) |
| `escpos` | generic ESC/POS receipt printer | own encoder: `ESC @`, code page CP858 (€), bold, alignment, 48/32 columns for 80/58 mm, feed, partial cut `GS V 66 0`; golden-byte tests |
| `zpl` | generic ZPL label printer | text label (`^XA … ^XZ`), test label only in M6b |
| `transport` | tcp (5 s dial/write timeout), cups (`lp -d <queue> -o raw`, darwin/linux; refused on windows), file (dev only) | target validated: tcp host must be a private/loopback IP or a `.local` name |
| — | QR reader `keyboard` | no Go driver: USB HID readers type into the page (`useKeyboardWedge`) |

Settings `desktop.json peripherals{receipt{driver: none|fake|escpos, target, width: 80|58}, label{driver:
none|fake|zpl, target}, reader{driver: keyboard|fake}, fiscal{driver: none|fake}}`.

### 8.3 Bridge API (`/api/bridge/v1`, tag `bridge`)

Served on the loopback listener only (local mode: in-process server, `LoopbackOnly` + session cookie, principal
permissions apply; remote mode: the proxy, `porto_remote` cookie, permissions of the device user from
`getDeviceSelf` cached 5 minutes). 404 on the LAN listener and when no desktop runtime (INV-85).

| Method path | operationId | Perm | Request → Response | Errors |
|---|---|---|---|---|
| GET B/status | getBridgeStatus | any authenticated | → `BridgeStatus{receipt, label, reader, fiscal: PeripheralStatus}` | — |
| POST B/receipts | printBridgeReceipt | `payments.collect` or `payments.collect_cash` | `{payment_id, width?}` → 202 | 404, 503 `peripheral_unavailable` |
| POST B/test | testBridgePeripheral | owner / PIN | `{device: receipt\|label\|fiscal}` → 204 | 503 |
| GET B/scanner/next | nextBridgeScan | any authenticated | `?timeout=1..25` → 200 `{text}` / 204 | — |

The receipt print fetches `getPaymentReceipt?format=json` (local: in-process service; remote: through the proxy
client) and renders with the configured driver. Web: `web/shared/src/desktop/bridge.ts` gains `bridge.status()`,
`bridge.printReceipt(paymentId)`, `bridge.nextScan()`; the payment page and the dock Cassa tab show "Stampa
scontrino" when `bridge.status().receipt.ready` (one-line inserts).

### 8.4 Fiscal through the bridge

- **Local mode**: the desktop is the server; the configured local `ext.FiscalDriver` (fake in M6b) is registered
  into `app.Options.Extensions` as code `desktop`; the M5 path (`fiscal.issue` job, `Deferred()`) is unchanged.
- **Remote mode**: new core driver code **`bridge`** on the server (`internal/domain/fiscal/bridge.go`): selected
  in billing settings together with `fiscal_bridge_device_id` (a `kind: desktop`, `access: token` device of the
  marina). The `fiscal.issue` job skips `bridge` documents (they stay `pending`). The desktop's fiscal agent
  (`internal/desktop/remote/fiscalagent.go`) every 10 s while reachable:
  `POST /api/device/v1/fiscal/claim {max ≤ 10}` → documents with a `claim_id` and a 60 s lease (conditional
  UPDATE: `status = 'pending' AND (bridge_claim_until IS NULL OR bridge_claim_until < now)`), only for the device
  set in billing settings (else 403); issue each on the local driver; `POST
  /api/device/v1/fiscal/documents/{documentId}/outcome {claim_id, status: issued|failed|deferred, external_ref?,
  issued_at?, error_code?}` with `Idempotency-Key = <documentId>:<claim_id>` → 204; stale claim → 409
  `claim_expired`. `issued` follows the M5 immutability trigger. The agent claims **one** document per call (up to
  10 per run), so a slow print never outlives its lease, and journals every `issued` outcome in
  `<data>/fiscal-journal.json` before sending it: a lost answer, a 409 after an expired lease or a restart never call
  the device again; the journalled outcome is resent (under the new `claim_id` when the document is claimed again)
  and leaves the journal on 2xx or 404 (INV-86). Offline: nothing is issued until reachable
  (29.2: no commercial documents offline); documents wait `pending`, visible in the fiscal page.
- Audit `fiscal.bridge_issue` (ids, status, no lines); outbox unchanged.

### 8.5 Local CA for phones (REMOTE, `internal/lan`)

- New CAs carry **name constraints** (permitted IP ranges 10/8, 172.16/12, 192.168/16, fc00::/7, 127/8; DNS
  `.local`, `localhost`); an M6a CA without constraints is reported `ca_constrained: false` and the CA download is
  offered only after "Rigenera tutto".
- On the LAN listener, public: `GET /lan/porto-ca.crt` (DER, `application/x-x509-ca-cert`) and
  `GET /lan/porto-ca.mobileconfig` (unsigned iOS profile with the CA only); `LanStatus` += `ca_constrained`,
  `ca_download_url`. The pairing page and the offline status link the help page `/admin/help/certificate` (WEB part
  owned by OFFLINE): steps for Android (install CA certificate) and iOS (profile + Certificate Trust Settings),
  fingerprint to compare.

## 9. Contract details (CONTRACT)

### 9.1 OpenAPI

- `x-porto-offline: true` and header parameter `OfflineQueuedAt` (`Porto-Offline-Queued-At`, date-time) on
  `recordPresence`, `uploadPresencePhoto` (+ `IdempotencyKey`), `recordPayment`. `IdempotencyKey` description:
  "replayed for 24 hours, 7 days for offline operations".
- `PresenceCreate.expected_resource_id`. Problem slugs: `allocation_changed` (409), `offline_not_allowed` (422),
  `concurrent_update` (409, only if §4.4 applies), `remote_unreachable` (503), `remote_pin_mismatch` (502),
  `peripheral_unavailable` (503), `claim_expired` (409), `app_pin_invalid` (401) + `errors.*` it/en.
- New operations: `listCollectibles`, `getTransitQr`, `claimFiscalDocuments`
  (`POST /api/device/v1/fiscal/claim`, deviceAuth), `reportFiscalOutcome`
  (`POST /api/device/v1/fiscal/documents/{documentId}/outcome`, deviceAuth, Idempotency-Key), bridge operations of
  §8.3, desktop operations `pairDesktopRemote` (`POST D/remote/pair`), `unpairDesktopRemote` (`DELETE D/remote`),
  `getDesktopPeripherals` / `updateDesktopPeripherals` (`GET|PUT D/peripherals`), `setDesktopKiosk`
  (`PUT D/kiosk`), `exitDesktopKiosk` (`POST D/kiosk/exit`), `setDesktopRemoteOffline` (`POST
  D/dev/remote-offline`, dev only). Security schemes: `appPin` (apiKey header `Porto-App-Pin`), `remoteCookie`
  (apiKey cookie `porto_remote`).
- Schemas: `Collectible`, `CollectiblePage`, `ReceiptData` (`getPaymentReceipt` `format` enum += `json`),
  `FiscalClaim`, `FiscalOutcome`, `BridgeStatus`, `PeripheralStatus`, `PeripheralSettings`, `RemoteStatus`;
  `DesktopStatus` += `mode`, `remote?{url, marina{id,name}, reachable, paired_at}`, `kiosk`;
  `LanStatus` += `ca_constrained`, `ca_download_url`; billing settings += `fiscal_bridge_device_id` (nullable).

### 9.2 Migration `00014_offline_bridge.sql` (both dialects, same number)

- `presences`: `idempotency_key TEXT NULL`, `idempotency_hash TEXT NULL`, unique index
  `presences_idempotency_key_uq (marina_id, idempotency_key) WHERE idempotency_key IS NOT NULL`.
- `fiscal_documents`: `bridge_device_id TEXT NULL REFERENCES devices (id) ON DELETE SET NULL`, `bridge_claim_id
  TEXT NULL`, `bridge_claim_until TIMESTAMP NULL`, `bridge_attempts INTEGER NOT NULL DEFAULT 0`; index on
  `(marina_id, status, driver)`. The issued-immutability trigger still allows these columns only while `pending`.
- `billing_settings`: `fiscal_bridge_device_id TEXT NULL REFERENCES devices (id) ON DELETE SET NULL`.
- Down reverses (SQLite table rebuild where needed). Store models updated; backup tables unchanged in membership.

### 9.3 Go seams

`internal/desktopport`: `ModeRemote` accepted, `RemoteSettings`, `KioskSettings`, `PeripheralSettings`,
peripheral interfaces (§8.1), `Runtime` += `Peripherals() Peripherals`, `Kiosk(...)`, `PairRemote(...)`,
`UnpairRemote(...)`; `middleware.Policy.Offline`; `reqctx` `OfflineQueuedAt`; `fiscal` driver code constant
`bridge`; `app.Options.Peripherals`; `internal/http/bridge` stub; `internal/peripheral/doc.go`;
`internal/desktop/remote/doc.go`. depguard: `internal/peripheral/**` imports stdlib, `desktopport`, `pkg/ext` only;
`internal/desktop/remote` must not import `internal/store` or `internal/app`.

### 9.4 Web seams

Stubs with the signatures of §4.1 (`web/shared/src/offline/**`), `web/shared/src/scan/index.ts`
(`parseTransitQr`, `CameraScanner`, `useKeyboardWedge`), `web/shared/src/desktop/bridge.ts` additions; routes:
`/m/$marinaId/dock` (kept), `/m/$marinaId/direction`, `/help/certificate` (public), `/desktop/kiosk-exit`,
`/desktop/remote` (first-run branch); i18n namespaces `offline`, `phone` (it/en) + keys in `desktop`, `errors`;
`queryKeys.ts` keys `collectibles`, `transitQr`, `bridge.status`, `desktop.peripherals`; Playwright main config
gains project `phone` (`devices['Pixel 7']`, `serviceWorkers: 'allow'`, `testMatch: /(phone|offline)-.*\.spec\.ts/`).

## 10. Tests

### 10.1 OFFLINE

Go (integration, both dialects via `testdb`):
- **`TestConcurrency_OfflineReplay_Presence`**: full HTTP stack (router, Enforce, OfflineGuard, Idempotency,
  handler), one dock user, 100 goroutines POST the same key + body with `Porto-Offline-Queued-At`; each goroutine
  retries `idempotency_in_progress` until done; assert exactly one `presences` row, one `presence.record` audit
  row, one `presence.arrived` outbox event, vessel state `in_port`, and all final bodies byte-equal (replays carry
  `Idempotent-Replayed: true`). `-race`.
- **`TestConcurrency_OfflineReplay_CashPayment`**: same for `recordPayment` cash on one charge: one payment, one
  match, `paid_amount` once, session expected cash once.
- **`TestConcurrency_OfflineReplay_Photo`**: one stored file version, identical responses.
- `TestPresence_DomainKeyAfterLostMiddlewareRow` (delete the idempotency row, replay → same presence, no new
  row), `TestIdempotency_OfflineTTLSevenDays`, `TestIdempotency_StaleClaimTakeover` (only flagged routes),
  `TestIdempotency_RetryAfterNotStored`, `TestOfflineGuard_RefusesNonOfflineOps` (sweep: every operation without
  the flag + header → 422; e.g. `recordUnexpectedArrival`, `createBlock`, `createRelease`, `createTransit`,
  `confirmTransit`), `TestOfflineGuard_RequiresKeyAndValidInstant`, `TestPolicies_OfflineSet`,
  `TestPresence_ExpectedResourceConflict` (moved allocation → 409 `allocation_changed`, nothing written),
  `TestPresence_OfflineRequiresAllocation`, `TestPayment_OfflineReceiptModeRefused`,
  `TestPayment_OfflineClosedSessionConflict`, `TestCollectibles_ScopeAndPermissions` (dock 200 without customer
  names unless `customers.read`; other marina 404), audit rows carry `offline_queued_at`,
  `TestLogs_NoPII` / `TestEvents_DataHasNoPII` extended.
Vitest (`fake-indexeddb`, fake timers): classification table; backoff schedule and `Retry-After`; key stable across
retries and reloads; lane ordering and skipping conflicted lanes; dependency binding (photo after presence; parent
conflict → blocked); expiry after 6 days; queue-first path online; runner single-leader across two instances;
401 pause and different-user freeze; logout purge and marina-switch refusal; persister keeps only allowlisted
keys, drops > 48 h, never persists `FileLink`; `OFFLINE_OPERATIONS` equals the `x-porto-offline` set of
`api/openapi.yaml`; `assertOnline` throws for every forbidden hook; overlay of pending ops; SW precache script
produces a deterministic list.
Playwright (`phone` project, Pixel 7): **`offline-queue.spec.ts`** — seed via API (dock user, transit A arriving
today, vessel B departing today, invoice-mode charge on C, open cash session); dock logs in on the phone; wait for
the cache warm-up marker; `context.setOffline(true)`; Arrivato on A with note and photo (`setInputFiles`),
Partito on B, Incassa on C; forbidden buttons (Arrivo non previsto, Rilascio, Blocco, Misura) disabled with the
reason text; reload offline → shell from the SW, pending count 4; `setOffline(false)` → queue drains; via API:
exactly one presence for A and B, the photo, one payment on C. **Lost response**: `page.route` on
`recordPresence` performs `route.fetch()` then `route.abort()` once → the runner retries with the same key →
still one presence. **Conflict**: enqueue an arrival offline, move the allocation via the office API, reconnect →
conflict card with the new berth, no presence written, "Scarta" empties the queue. **Logout** purges IndexedDB
(`indexedDB.databases()` empty). `pwa.spec.ts`: manifest valid, SW controls the page after reload, update toast
when a new `sw.js` build is served (test helper swaps the build hash).

### 10.2 PHONE

Vitest: `landingFor` table (all nine roles × mobile/desktop), `parseTransitQr`, `useKeyboardWedge` timing,
`CameraScanner` with a mocked `BarcodeDetector` and with the `jsqr` path, dock tabs render at 360 px, forbidden
controls show the reason offline, scan resolution from cached day view, direction tabs. Go:
`TestTransitQr_PayloadAndScope` (SVG decodes to `PT1:<id>`, non-transit 404, other marina 404).
Playwright (`phone` project): `phone-dock.spec.ts` (each tab, arrival/departure online, photo, collect, release and
block online, unexpected arrival online), `phone-scan.spec.ts` (fetch the transit QR, use the file fallback, card
opens, Arrivato works), `phone-direction.spec.ts`, and M6a `lan-pairing.spec.ts` kept (< 60 s, now lands on the
new dock screen).

### 10.3 REMOTE

Go unit: `TestRemoteSettings_Validate`, `TestRemotePin_RateLimitAndHash`, `TestRemoteTLS_PinLeafThenCA`
(leaf rotated under pinned CA accepted, foreign cert refused), `TestRemoteTLS_NoFpUsesSystemRoots`,
`TestRemoteProxy_InjectsTokenStripsCookies`, **`TestRemoteProxy_TokenNeverReachesPage`** (responses, redirects,
error pages, logs), `TestRemoteProxy_RequiresLaunchCookieAndHost`, `TestRemoteProxy_SSEStreams`,
`TestRemoteProxy_ShellCacheOffline` (index + assets served, `/api` → 503 `remote_unreachable`),
`TestRemoteProxy_RevokedDeviceResets`, `TestKioskPolicy`, `TestESCPOS_Golden` (80 and 58 mm, €), `TestZPL_Golden`,
`TestTransport_TargetValidation`, `TestFakeDrivers`, `TestLANCert_NameConstraints`, `TestLAN_CADownload`
(constrained only). Integration (both dialects): **`TestConcurrency_FiscalBridgeClaim`** (100 goroutines claim
the same pending documents → each document claimed by exactly one, issued once), `TestFiscalBridge_OutcomeIdempotent`,
`TestFiscalBridge_WrongDevice403`, `TestFiscalBridge_JobSkipsBridgeDocs`, `TestBridgeAPI_LoopbackOnly` (LAN 404,
server edition 404), `TestBridgeAPI_PrintReceiptFake`. Acceptance `internal/acceptance/m6b_remote_test.go`:
**`TestAcceptance_M6bRemote`** — a real server app (httptest TLS), pair a remote runtime with the pinned client,
GET `/api/admin/v1/me` through the proxy, record a presence through the proxy, receipt-mode counter sale with
`fiscal_driver = bridge` → document `pending` → fake agent issues → `issued` with `external_ref`; revoke → proxy
resets. Playwright (`e2e/tests-desktop/`): `remote.spec.ts` (webServer adds `bin/porto serve` on 18100; desktop
headless first run → "Collega a un Porto online" with URL + code from the API helper (http allowed in dev) →
panel loads through the proxy → bridge status shows fake drivers → "Stampa scontrino" writes the fake file →
dev remote-offline → reload shows the cached shell, a presence is queued, back online → sent once),
`kiosk.spec.ts` (settings toggle and exit page with PIN; window behaviour is manual), `lan-ca.spec.ts` (download
endpoints on the LAN listener, help page).

## 11. Ownership (CONTRACT first, then three builders in parallel)

### 11.1 CONTRACT (then frozen)

`api/openapi.yaml` + `make gen` outputs; `internal/migrations/{sqlite,postgres}/00014_offline_bridge.sql`;
`internal/store/models.go` (fields only); `internal/desktopport/*.go`; `internal/reqctx` (offline instant);
`internal/http/middleware/policy.go` (`Offline` field + loader only) and `basic.go` (Permissions-Policy only);
`internal/http/problem/types.go`, `internal/http/errmap.go` / `respond/errmap.go` (new slugs);
`internal/http/server.go` (bridge registration, `OfflineGuard` hook point); `internal/app/app.go`
(`Options.Peripherals`, desktop `bridge` wiring); `internal/domain/fiscal/types.go` (driver code `bridge`
constant); stubs `internal/http/bridge/bridge.go`, `internal/peripheral/doc.go`, `internal/desktop/remote/doc.go`,
`internal/http/middleware/offline.go`; `.golangci.yml` (depguard §9.3), `go.mod`; web: `package.json` files and
lockfile (§4.1 deps), stubs of §9.4, `router.tsx`, i18n files and `resources.ts`, `queryKeys.ts`,
`e2e/playwright.config.ts` (phone project), `e2e/playwright.desktop.config.ts` (second webServer), shadcn adds if
missing (`drawer`, `sheet`, `badge`, `progress` — check `web/shared/src/ui/` first); docs §11.4. Ends with
`make gen && make check` green.

### 11.2 Builders

| Builder | Owns (exclusive) |
|---|---|
| **OFFLINE** | `web/shared/src/offline/**`; `web/admin/src/sw/**`, `web/admin/scripts/sw-precache.mjs`, `web/admin/vite.config.ts`, `web/admin/package.json` (`build` script only), `web/admin/public/site.webmanifest`; `web/admin/src/pwa/**`; `web/shared/src/ui/patterns/{OfflineIndicator,QueueSheet,OfflineReason}.tsx`; the queue-backed hooks `useRecordPresence`, `useUploadPresencePhoto` (in `web/shared/src/api/hooks/desk.ts`) and the cash branch of `useRecordPayment` (`billing.ts`), plus `assertOnline` inserts in the forbidden hooks of `desk.ts`, `billing.ts`, `registry.ts`, `opsmap.ts` (one line each); `web/shared/src/api/context.tsx` (persister + purge on logout); `web/admin/src/features/help/**` (certificate page); `internal/http/static/**` (sw headers); `internal/http/middleware/{idempotency,offline}.go`; `internal/store/idempotency*.go`; `internal/domain/presence/**`; `internal/store/presences*.go`; `internal/domain/payment/{record.go,collectibles.go}` + `internal/store/collectibles.go`; handlers `internal/http/admin/{presences,payments,collectibles}.go`; `store.ErrBusy` / `concurrent_update` if §4.4 applies (`internal/store/db.go` RunInTx error mapping only); tests of §10.1; `e2e/tests/{offline-queue,pwa}.spec.ts`, `e2e/support/offline.ts` |
| **PHONE** | `web/admin/src/features/phone/**` (new); `web/admin/src/features/dock/**` (re-export / redirect to the phone dock); `web/admin/src/lib/landing.ts` (+ test); `web/admin/src/pages/DashboardPage.tsx` (narrow-viewport redirect only); `web/shared/src/scan/**`; the one-line "QR per l'ormeggiatore" insert in the desk transit detail; `internal/http/admin/transit_qr.go`, `internal/domain/contract/transit_qr.go`; i18n values `phone`; tests of §10.2; `e2e/tests/{phone-dock,phone-scan,phone-direction}.spec.ts`, `e2e/support/phone.ts` |
| **REMOTE** | `internal/desktop/**` (incl. new `remote/`), `cmd/porto-desktop/**` (kiosk files), `internal/http/desktop/**`, `internal/http/bridge/**`, `internal/peripheral/**`, `internal/lan/**` (name constraints, CA download), `internal/domain/fiscal/bridge.go` + `fiscal.issue` skip, `internal/store/fiscal_bridge.go`, `internal/http/device/fiscal.go`, billing settings field validation (`internal/domain/charge` settings file, field only); `web/admin/src/features/desktop/**` (remote pairing, peripherals, kiosk), `web/admin/src/pages/DevicesPage.tsx` ("Copia collegamento" for desktop kind), `web/shared/src/desktop/**`, "Stampa scontrino" one-line inserts (payment page, dock Cassa via a `BridgeReceiptButton` exported from `web/shared/src/desktop`); i18n values `desktop`; `internal/acceptance/m6b_remote_test.go`; tests of §10.3; `e2e/tests-desktop/{remote,kiosk,lan-ca}.spec.ts`, `e2e/support/desktop.ts`; `docs/desktop-build.md`; `scripts/agents/check-naming.sh` (new paths) |

### 11.3 Parallel-phase rules

- OpenAPI, migration, `desktopport` and the web stub signatures are frozen; a builder needing a change writes it in
  its report.
- PHONE consumes the offline module only through `web/shared/src/offline/index.ts`; until OFFLINE lands, PHONE
  tests mock it. REMOTE uses `BridgeReceiptButton` in the dock Cassa tab only through PHONE's slot prop
  `receiptAction` (PHONE renders whatever is passed; REMOTE does not edit `features/phone/**`).
- Error slugs shared by OFFLINE and PHONE UI come from CONTRACT's i18n keys.
- Shared Go files are edited only at the listed spots.

### 11.4 Docs (CONTRACT)

ADR `docs/adr/0011-offline-telefono-remoto-m6b.md` (Italian, "proposta"): queue-first with two-layer dedupe and
7-day offline keys; server-side `OfflineGuard`; hand-written SW, no API data in the SW; remote mode as a local
proxy holding the device token; pinning leaf then CA; app PIN; bridge on loopback; fiscal bridge claims;
name-constrained local CA; `PT1:` transit QR. `docs/invariants.md`: **INV-71** status → implemented
(`TestOfflineGuard_RefusesNonOfflineOps`, Vitest allowlist); **INV-83** an offline operation replayed any number of
times has one effect (`TestConcurrency_OfflineReplay_*`, `offline-queue.spec.ts`); **INV-84** offline data is
scoped to user and marina and purged on logout (Vitest + e2e); **INV-85** the bridge answers only on loopback and
the device token of the remote mode never reaches the page (`TestBridgeAPI_LoopbackOnly`,
`TestRemoteProxy_TokenNeverReachesPage`); **INV-86** a fiscal document is issued by at most one bridge claim
(`TestConcurrency_FiscalBridgeClaim`; on the desktop the agent claims one document at a time and journals issued
outcomes before reporting them, `TestFiscalAgent_LostOutcomeNoReissue`); **INV-87** offline cash is collection on
existing documents only (`TestPayment_OfflineAmountEqualsMatches`). `docs/architecture.md` §2, §8, §9 updated (offline module, remote mode,
peripherals).

## 12. Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
CGO_ENABLED=0 go build -o bin/porto-desktop ./cmd/porto-desktop && bin/porto-desktop -version
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/http/... ./internal/domain/presence/... ./internal/domain/payment/... ./internal/domain/fiscal/... ./internal/store/... ./internal/desktop/... ./internal/peripheral/... ./internal/lan/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -run '^TestConcurrency_(OfflineReplay_Presence|OfflineReplay_CashPayment|OfflineReplay_Photo|FiscalBridgeClaim)$' ./internal/...
make desktop-test && make test-db && make test-race
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M6|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII'
npm --prefix web run test
make e2e            # incl. phone project: offline-queue, pwa, phone-dock, phone-scan, phone-direction (Pixel 7)
make e2e-desktop    # incl. lan-pairing < 60 s, remote, kiosk, lan-ca
make licenses && scripts/agents/check-naming.sh
# macOS host with Xcode CLT: make desktop-build DESKTOP_TAGS=webview && dist/desktop/bin/darwin_arm64/porto-desktop -version
```

Manual checks (final report): real Android phone on the desktop LAN: install the CA, add to home screen, airplane
mode, record an arrival with photo and a cash collection, reconnect → one of each in the office panel; a real
ESC/POS network printer prints a courtesy receipt from a remote-mode desktop; kiosk mode on macOS hides Dock and
menu bar and exits only with the PIN.

## 13. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Offline operations in M6b are exactly `recordPresence` (arrived/departed with an allocation, notes), `uploadPresencePhoto`, `recordPayment` cash; the other 29.2 items wait for their domains (M9–M11). Notes are presence notes (no generic note entity exists). |
| D2 | Queue first: allowlisted mutations always go through the queue, online too. |
| D3 | Two-layer dedupe: middleware (7-day TTL for offline operations, stale-claim takeover after 2 min) plus a domain unique key per effect (presences new, payments existing). Clients stop replaying an op after 6 days. |
| D4 | Server-side `OfflineGuard` via the `Porto-Offline-Queued-At` header: a replay on a non-offline operation is 422 `offline_not_allowed`; the queued instant is audited. |
| D5 | Conflict detection for presences by `expected_resource_id` (the berth the worker saw), not by allocation version (an arrival changes contract state and would make chained offline ops conflict falsely). |
| D6 | Offline cash only on invoice-mode charges; receipt-mode charges need the server online because they number a commercial document (29.2, INV-71). Offline cash needs a cached open session; a closed session at replay is a conflict. |
| D7 | Measurement, releases, blocks, unexpected arrival are online-only (they change fit or occupancy); 29.2 does not list them. |
| D8 | Hand-written service worker, no PWA plugin; the SW caches only the shell and never API data. |
| D9 | One IndexedDB database per origin, records tagged with user and marina; logout deletes it; marina switch refused with unsent ops. |
| D10 | Transit QR payload `PT1:<contract_id>`, opaque, scanned only by the panel. |
| D11 | Remote mode is a loopback reverse proxy in the desktop app that holds the device token and caches the shell; the page never sees the token. |
| D12 | Pinning: leaf fingerprint from the pairing payload, then the CA that issued it; no pinning for public-CA servers. |
| D13 | Remote-mode local administration protected by an app PIN (no local users); local mode stays owner + loopback. |
| D14 | Kiosk exit with Cmd/Ctrl+Shift+K and PIN (owner password in local mode). |
| D15 | Bridge is an HTTP API on the loopback origin (testable headless), not JS bindings. |
| D16 | Fiscal in remote mode through server driver `bridge` + device claim/outcome API with 60 s leases; only fake local fiscal driver shipped; real models are extensions. |
| D17 | Generic ESC/POS (CP858, 80/58 mm) and ZPL drivers over TCP 9100 / CUPS raw / file (dev); no Windows raw USB in M6b. |
| D18 | New local CAs are name-constrained to private addresses and `.local`; CA download only for constrained CAs. |
| D19 | Dock role reads open charges through the narrow `listCollectibles`, not `listCharges`. |
| D20 | Direction phone screen: placeholder dashboard with today counts, map, deadlines (30 days). |
| D21 | `concurrent_update` (transient 409 with `Retry-After`) added only if Porto's transaction errors currently surface as a plain 409. |
| D22 | Map background images are not cached offline in M6b. |
