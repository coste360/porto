# M8 — Prenotazione online: work order

Status: Plan phase output. It is authoritative for the M8 builders. If it conflicts with `docs/spec/`, the spec
wins: stop and report to the orchestrator. Open decisions in `docs/spec/33-decisioni-aperte.md` stay open (D8
mandatory communications, D10 native app). §16 lists every detail decided here where the spec says nothing.
CONTRACT records the binding ones in ADR 0013.

Sources: spec 23 (all of it), 13.2/13.5 (inquiries, quotes, transit states), 10.4/10.7/10.8 (holds, search, quotas,
closures), 12 (pricing channel `online`), 16 (payments, refunds), 21.3 (lost demand), 26, 27 (public API,
`BookingChannel`), 28, 29 (i18n, units), 30 (security, GDPR), 32 (M8 acceptance). `docs/invariants.md` INV-01,
05, 07, 08, 09, 30, 56, 70, 73, 75, 77. ADRs 0001, 0002, 0003, 0008, 0009. Plans: `m2-plan.md` (pricing,
`pricing.ChannelOnline`, promo codes), `m3-plan.md` (allocation, holds, `createTx`, closures, D5/D12/D18),
`m4-plan.md` (magic links, public document upload, quote acceptance with deposit), `m5-plan.md` §5.4–5.7 (intents,
webhooks, `ConfirmTransitPaidTx`, transit payment link, late payment, refunds), `m7-plan.md` (R4 lost demand reads
`inquiries`).

## 0. Ground rules (every builder)

1. **Arenile is a reference, not a source** (ADR 0001). For behaviour only, read Arenile spec 12 (public page,
   widget, embed security, customer area), spec 7.6 (online quotas as DB counters), its checkout and customer-area
   packages, and `embed.js`. Never copy files, code, comments or tests. Never touch `../arenile`. Porto naming:
   `porto:*` postMessage types, `Porto-Booking-Token`, `Porto-Customer-Session`, `data-porto` attributes.
2. **No double occupancy, DB only** (INV-01, ADR 0003). Online holds, channel reservations and desk allocations all
   go through `occupancy.AllocateTx`, which writes `berth_occupancies` / `cell_occupancies` rows. Never "check then
   insert". A unique violation maps to `ErrResourceTaken`, and the caller retries with the next solution (§4.3).
3. **Quotas are DB counters too**: a conditional `UPDATE … SET used = used + 1 WHERE used < cap` on
   `online_quota_usage`, with `CHECK (used <= cap)` (§3.3). PostgreSQL runs READ COMMITTED: take the row lock first
   (counter update or `lockContract`), then re-check the state under the lock. SQLite has one writer.
4. **INV-09**: a transit becomes `confirmed` only through `contract.confirmTx`, which is called by the provider
   webhook or reconciliation (`source=provider`), the manual driver (`source=manual`), or a verified channel
   reservation (`source=channel`, new, §8.3, ADR 0013). The browser return never confirms anything.
5. **INV-30**: every transit state change goes through `internal/domain/contract`, with audit row and outbox event
   in the same transaction. The `booking` package orchestrates the steps and never writes `contracts.status`.
6. Money is `money.Cents`, lengths are `geo.Cm`, dates are `calendar.Date` in the marina time zone, and time comes
   from `clock.Clock`. `internal/domain/booking/policy` is pure: no store, no clock, no randomness (depguard).
7. Public endpoints: rate limited, `Cache-Control: no-store` (except config), no enumeration (an unknown,
   expired or foreign token or id is always `404 link_invalid`), and no cookies. Tokens travel in headers or URL
   fragments, never in paths that get logged. CSRF does not apply because there is no cookie auth. Logs, events
   and webhooks carry no PII (INV-70).
8. API first: CONTRACT edits `api/openapi.yaml` and runs `make gen`. Never edit generated files. Migration
   `00016_online_booking.sql` for both dialects. Never edit 00001–00015.
9. UI: shadcn only (official CLI, exported from `@porto/shared`), Tailwind tokens, no custom CSS, i18n it (source)
   and en. Public page works from 320 px wide.
10. `cmd/porto` stays `CGO_ENABLED=0`. No new Go dependency. New npm dependencies only under approved licences.
11. Do not `git commit`. No destructive git commands, no `rm -rf`. Docker only for your own throwaway containers.
    Comments and identifiers in English. While iterating, run only the tests of the packages you touch. Full gates
    are §15.

## 1. Scope

### 1.1 In M8

- Public booking page `/b/{slug}` (SPA `web/booking`, mobile first, installable PWA with manifest and shell-only
  SW). First-load JS ≤ 180 KB gzip.
- Embeddable widget: `GET /embed.js` (< 3 KB) and a plain iframe. `frame-ancestors` is built per marina from
  `marinas.allowed_embed_origins`. postMessage only to a verified parent origin. `return_to` only toward
  allowed origins.
- Availability search by dates and vessel dimensions (LOA, beam, draft, hull type, or a saved vessel when a
  customer session is present), per berth category, with online rules, `online_sellable`, closures and online
  quotas. Every result shows only yes/no and a "from" price, never other customers' data.
- Price from the pure pricing engine (`pricing.ChannelOnline`, services, promo code). The quote step writes nothing.
- `POST /bookings`: transit `held` (channel `online`) plus a concrete hold allocation chosen by `occupancy.Search`,
  plus quota claims, audit and outbox, all in one transaction.
- Customer and vessel details, consents (terms, privacy, optional marketing), documents (licence and insurance
  photos, encrypted, office verification).
- Checkout with advance payment (deposit or full, per settings) through the existing `collect` intents and payment
  drivers. Hold extended to 30 minutes. Confirmation only from the webhook or the manual driver. Late payment
  handled (§5.5).
- Approval flow (`awaiting_approval`): rules by LOA, berth group, or "always". Card pre-authorisation is captured
  on approval when the provider supports it, otherwise a payment link is sent after approval. Hold until the
  approval deadline. Approve or reject from the desk. Automatic rejection at the deadline.
- Customer area: magic-link login (no account, no password), bookings (all transits of the customer), details, QR,
  documents (upload, list), receipts, pay balance, change dates (subject to availability and online rules), cancel
  with refund per the policy snapshot.
- Confirmation email with arrival instructions (VHF channel, berthing-master phone, berth if the policy reveals it,
  rules, transit QR `PT1:<contract_id>`) and the customer-area link.
- `ext.BookingChannel` in `pkg/ext` plus channel connections, pull/push jobs, webhook endpoint and an in-memory
  **fake channel** used by tests. Inbound reservations go through the same booking service and occupancy.
- Lost demand: anonymous online searches with no availability become `inquiries` rows (channel `online`, source
  `public`, no customer, no IP). Unpaid expired online holds and rejected approvals update their inquiry outcome.
- Desk: online bookings screen (list, detail, approve/reject, document verification). Configuration pages: online
  settings, cancellation policy, approval rules, versioned texts, quotas per group, sellable areas, embed snippet,
  channels.

### 1.2 Out of M8

| Item | Where | Note |
|---|---|---|
| Real third-party channel integrations | later (extension) | Only the interface and the fake. Candidates to evaluate (unverified): Navily, Dockwa, Marinanow, Pick a Pier, Mooring.it, SeaWays. |
| Dry storage, yard and rental online sales | M11 | `/b/{slug}` shows transits only. |
| Public harbour profile `/p/{slug}` with the "Prenota" button | M12 (spec 25) | M8 exposes the URL to link to. |
| Mandatory communications (maritime authority, guests on board) | open D8 | Only "people on board" is collected. No authority submission. |
| Native app, multi-marina boater account | D10 | The PWA covers the use case. Sessions are per marina. |
| German and French UI | later | Only it and en exist in the repo today. Keys are structured for later locales. |
| Mooring contracts in the customer area | later | The customer area lists transits only. Quotes and renewals keep their M4 magic-link pages. |
| Segmented online stays (place change during the stay) | — | `MaxSegments = 0` for online and external, as in M3 `search.go`. |
| Free theming (colours, fonts) | — | Theme = a preset among shipped token sets plus "hide header in embed" (§16 D20). |

## 2. Architecture and packages

```
web/booking (/b/{slug})  ──►  /api/public/v1/{slug}/booking/*, /bookings/*, /account/*
                                │
internal/http/public/{booking,account}.go  (thin)
                                │
internal/domain/booking  ─── orchestrates: rules (policy, pure) · occupancy.Search (channel online) ·
   │                         contract.CreateOnlineTransitTx · collect intents · documents · consents · lost demand
   ├── policy/ (pure: stay/advance/arrival rules, approval rules, cancellation refund, quota caps)
   └── jobs: approval expiry, capture/void retry
internal/domain/occupancy   ── quota.go: ClaimQuotaTx / ReleaseQuotaTx (same tx as AllocateTx)
internal/domain/contract    ── online.go: new state rows, CreateOnlineTransitTx, approval transitions, SourceChannel
internal/domain/collect     ── authorized / voided events, late-payment recovery or refund
internal/domain/channel     ── connections, ApplyReservation → booking.CreateChannelBookingTx, pull/push jobs
   └── fakechannel/         (implements ext.BookingChannel, tests and dev)
internal/domain/portal      ── customer login links (fragment token), sessions, customer-area orchestration
internal/domain/onlineconf  ── online settings and versioned texts (load, validate, publish)
pkg/ext/channel.go          ── BookingChannel; pkg/ext/payment.go ── PaymentAuthorizer (optional)
```

Dependency rules: `booking` may import `occupancy`, `contract`, `collect`, `pricelist`, `document`, `onlineconf`,
`booking/policy`, but not `portal` or `channel`. `portal` and `channel` import `booking`. `booking/policy` imports
only `calendar`, `money`, `geo`. CONTRACT adds these rules to `.golangci.yml`.

## 3. Availability, rules and online quotas (normative, ENGINE)

### 3.1 Online rules (`onlineconf.Settings`, evaluated by `policy.CheckStay`, pure)

`online_booking_settings` (one row per marina, versioned, `If-Match`). Defaults are in brackets.

- `min_nights` [1], `max_nights` [30] (≤ 366), `min_advance_hours` [2], `max_advance_days` [365]
  (arrival relative to "now" in marina time).
- `arrival_from`/`arrival_to` local times ["12:00"/"20:00"] (shown, and they bound `expected_arrival_time`),
  `closed_weekdays` for arrivals [[]], plus M3 closures (they block arrivals on all channels).
- `payment_mode` `deposit|full` [full], `deposit_bp` [3000] (basis points of the total, rounded half-even to the
  cent, minimum 1 cent; the price-list deposit wins when the policy defines one).
- `cancellation_tiers` [{min_days_before: 7, refund_bp: 10000}, {min_days_before: 2, refund_bp: 5000},
  {min_days_before: 0, refund_bp: 0}], `refund_as` `refund|credit` [refund].
- `approval`: {`loa_over_cm` [null], `group_ids` [[]], `always` [false], `hold_hours` [48, 1..144]}.
- `required_fields`: {`documents`: [`boating_licence`?, `insurance_policy`], `people_on_board` [true],
  `phone` [true]}.
- `reveal_berth` `never|after_confirmation` [after_confirmation].
- `late_payment_recovery` [true] (§5.5), `allow_without_provider` [false] (§5.6), `max_live_holds` [50]
  (live online holds per marina: when exceeded, `503 booking_busy`).
- `arrival_info` (i18n text: VHF channel, phone, rules link), `theme_preset`, `hide_header_in_embed`,
  `show_feet` [true].
- `marinas.booking_enabled` (exists) is the master switch. When false, every public booking operation except
  config returns `404 link_invalid`, and config returns `enabled:false`.

### 3.2 Search (channel `online` and `external`)

`occupancy.Search` accepts `online` and `external` (CONTRACT relaxes the validation, ENGINE implements):

1. Candidates are active resources with `online_sellable = true` (berths: existing column; dynamic areas: new
   column, §6), compatible by `fit` with no blocking codes. Fit overrides are never allowed online.
2. Free for every night (INV-05: expired holds are treated as free). `MaxSegments = 0`. Closures apply.
3. Quota filter: for each group with a quota that contains the resource, `used < cap` on every night, read from
   `online_quota_usage` in the same read transaction. A missing row means `used = 0`.
4. The order is the engine score of M3 10.7, unchanged.

`booking.Availability(slug, stay, vessel)` runs one search and groups the solutions by **category** = the
resource's `price_class` (M2 `berth_categories`). Resources without a class form the category `standard`. For
each online category it returns `available` (at least one solution), `from_cents` (pricing.Quote on the
category's best solution, with the default services only), `approval_required` (rules on the vessel and the
groups of that solution) and `reason` when the whole search is blocked (`closed`, `min_nights`, `max_nights`,
`advance`, `weekday`, `too_large`). Counts, berth codes and other customers' data are never exposed. Budget:
p95 < 300 ms on the demo seed (SQLite, `TestBooking_AvailabilityBudget`, skipped with `-short`).

### 3.3 Online quotas (spec 10.8; Arenile 7.6 behaviour, Porto design)

- Columns `berth_groups.online_quota_kind` (`none|count|percent`, default `none`) and `online_quota_value`
  (count ≥ 0, or percent 0..100).
- `cap(group, night)` comes from pure `policy.QuotaCap(kind, value, memberBerths)`: count = value, percent =
  floor(value × active member berths / 100). Areas count as one unit per allocation and only `count` quotas
  apply to groups that contain areas (validation error otherwise).
- Table `online_quota_usage (marina_id, group_id, night, cap, used, updated_at)`, PK `(group_id, night)`,
  `CHECK (used >= 0 AND used <= cap)`. Table `online_quota_claims (marina_id, contract_id, group_id, night)`, PK
  `(contract_id, group_id, night)`.
- `occupancy.ClaimQuotaTx(tx, scope, contractID, resource, nights, channel)` runs inside the hold transaction,
  before `AllocateTx`. It only acts for channels `online|external`. For each quota group of the resource and each
  night: `INSERT … ON CONFLICT DO NOTHING` (with the cap computed now), then
  `UPDATE … SET used = used + 1 WHERE group_id=? AND night=? AND used < cap`. Zero rows affected means
  `ErrQuotaExhausted` and the transaction rolls back. Then it inserts the claim rows. Desk and API ignore quotas
  (Arenile behaviour, §16 D3).
- `ReleaseQuotaTx(contractID, fromNight?)` decrements and deletes claims. It is called by contract on cancel,
  expire, reject and no_show, and on a stay shortened by `ChangeStay` (for the removed nights). A stay extended
  online claims the added nights. Desk moves keep the claims, because claims follow the contract and not the
  berth (§16 D4).
- Cap changes: `updateOnlineQuotas` rewrites `cap` for future nights with
  `cap = max(new_cap, used)` (never below what is sold).
- Invariant **INV-12** (§13): `used = count(claims)` per (group, night) and `used <= cap`. Query Q19 in
  `store.CheckOccupancyInvariants`.

## 4. Holds, booking creation and expiry (normative, ENGINE)

### 4.1 Transit states (CONTRACT writes the rows in `states.go`; ENGINE implements)

New rows (existing rows unchanged):

| From | Action | To | Events |
|---|---|---|---|
| held | `request_approval` | awaiting_approval | transit.changed, transit.approval_requested |
| pending_payment | `payment_authorized` | awaiting_approval | transit.changed, transit.approval_requested |
| awaiting_approval | `approve` (no authorisation: payment link) | pending_payment | transit.changed |
| awaiting_approval | `confirm` (only `confirmTx`: capture webhook, manual driver) | confirmed | transit.confirmed |
| awaiting_approval | `reject` | cancelled | transit.cancelled |
| awaiting_approval | `expire` (deadline job) | cancelled | transit.cancelled |
| awaiting_approval | `cancel` | cancelled | transit.cancelled |
| expired | `confirm_late` (only `confirmTx` with `source=provider`, online, recovery on) | confirmed | transit.confirmed |

`confirmTx` also accepts `awaiting_approval` and `expired` (the latter only via `confirm_late`), plus
`SourceChannel`. Constants: `ChannelOnline = "online"`, `ChannelExternal = "external"`, `SourceChannel = "channel"`.
INV-08 is extended to `awaiting_approval`.

### 4.2 Hold lifetimes (allocation `kind = hold`, `expires_at`)

| Step | Hold expiry |
|---|---|
| `POST /bookings` | now + `PORTO_HOLD_TTL` (existing config, default 10 min, §16 D1) |
| details, documents | unchanged (the countdown is shown) |
| `POST /bookings/{id}/pay` | `min(now + 30 min, arrival-day 23:59 marina time)`; intent `expires_at` = the same |
| intent expired | back to `held` with the remaining expiry (M5 path), else the M3 expiry job |
| `awaiting_approval` | now + `approval.hold_hours` (default 48 h) |
| `approve` → pending_payment (link) | now + 24 h (payment link default) |

`ExtendHoldTx(contractID, until)` lives in `contract/online.go` and updates `expires_at` of the live holds under
`lockContract`. If a hold has already expired and been reclaimed, it re-allocates through `convertOrReallocate`
semantics or fails with `409 hold_expired`.

### 4.3 `POST /bookings` (one transaction per attempt, ≤ 3 attempts)

1. Validate the rules (`policy.CheckStay`), the vessel (positive cm, LOA ≤ 10 000 cm), `booking_enabled`, live
   holds < `max_live_holds`. Optional `ext.BotProtection.Verify` on header `Porto-Bot-Token` when registered.
2. Read-only search (§3.2) restricted to the requested category, giving an ordered list of solutions.
3. For the first solution not yet tried, in `RunInTx`:
   `contract.CreateOnlineTransitTx` creates the vessel from the dimensions (`NewVessel`, name "—" until details),
   the held transit with channel online, price from `pricing.Quote` (binding snapshot, INV-20), and the contract
   policy snapshot with `online.cancellation_tiers` and `payment_mode`. It calls `occupancy.ClaimQuotaTx`, then
   `AllocateTx` (kind hold), then inserts `online_bookings`, writes audit (actor `public`), and emits
   `transit.held` and `occupancy.allocated`.
4. `ErrResourceTaken` or `ErrQuotaExhausted` means the next solution (fresh transaction). When none is left:
   `409 booking_unavailable` with the remaining available categories (another read).
5. Idempotency is domain level: `Idempotency-Key` (required) is stored as `online_bookings.idempotency_hash`, unique
   per marina. A replay with the same key and same body hash returns the same booking. A different body returns
   `422 idempotency_mismatch`. The middleware store excludes this operation (`x-porto-idempotency: domain`) so
   that no response containing a token is persisted.
6. Token: `booking_token = base32(HMAC-SHA256(KeyRing purpose "booking-token", booking_id ‖ token_nonce))`. The
   nonce is stored, the token never is, the comparison is constant time, and a replay recomputes the token. Sent
   back by the client as `Porto-Booking-Token`. Valid until 30 days after departure.

Response `201`: `booking_id`, `reference` (8 Crockford chars, unique per marina, shown to the customer and the
desk), `booking_token`, `status`, `hold_expires_at`, `price`, `approval_required`, `amount_due_now_cents`.

### 4.4 Expiry and abandonment

- Expired holds: the existing `contracts.expire` job and conflict-time reclaim (INV-05). ENGINE adds for channel
  online: `ReleaseQuotaTx`, and the linked inquiry gets outcome `lost`.
- `DELETE /bookings/{id}` (token, `held` only) cancels through `contract` (actor `public`, reason `abandoned`).
- Job `bookings.approval_expire` (1 min) rejects `awaiting_approval` transits whose deadline has passed (reason
  `approval_timeout`), then voids the authorisation (§6).

## 5. Details, checkout and payment (normative, ENGINE)

### 5.1 Details `PUT /bookings/{id}/details` (token, `held` only)

Body: customer `{first_name, last_name, email, phone?, country, locale}`, vessel
`{name, registration?, flag?, hull_type, draft_cm?}`, `people_on_board?`, `expected_arrival_time?`, `notes?`,
`consents {terms_version, privacy_version, marketing: bool}`.

- Customer: if exactly one non-anonymised customer of the marina has the same normalised email, reuse its id
  **without modifying it**. Otherwise create a new customer (through `customer` domain Tx primitives). The
  submitted contact is kept as `online_bookings.contact_enc` (KeyRing AAD `booking-contact:<id>`) for office
  review. Emails go to the submitted address.
- The vessel is updated (name, registration) and becomes owned by the customer.
- Consents: one `consents` row per accepted text (kind, version, sha256 of the text, source `online`). Terms and
  privacy are required (`422 consent_required`). A marina that has not published both texts takes no booking:
  details are refused with `422 booking_rules` (field `consents`, code `texts_unpublished`) and the config reports
  `payable: false`. Marketing is optional and unchecked by default. A version that is not the current published
  one returns `409 text_outdated`.
- Documents uploaded through a booking are linked in `online_booking_documents`. The token-only surfaces (booking
  view, required-documents check at `/pay`) list and count only those, never the documents of an existing customer
  matched by email (D10); the customer area and the desk see every document of the customer.
- Contract: `contract.SetOnlineCustomerTx` (`ActionUpdate`, `transit.changed`).
- Re-submitting before payment replaces the details. After payment it returns `409`.

### 5.2 Documents `POST /bookings/{id}/documents` (token, multipart ≤ 10 MiB)

Types `boating_licence`, `navigation_licence`, `insurance_policy`, `registration_certificate`, `identity_card`.
Reuses `document` (sniffing, encryption at rest, INV-74). New columns `documents.verified_at`,
`verified_by_user_id`. Missing required documents block `/pay` unless the settings allow "documents later" (§16 D9).

### 5.3 Payment `POST /bookings/{id}/pay` (token, `Idempotency-Key`)

- Requires details complete. Approval rules are re-evaluated on the final vessel data.
- **No approval**: `contract.RequestTransitPaymentTx` (held → pending_payment), then `ExtendHoldTx`, then
  `collect` intent purpose `transit` (amount = deposit or total), then `CreateCheckout` after commit. Success URL
  `PORTO_BASE_URL/b/{slug}/r/{booking_id}#st=<status_token>&rt=<return_to>`, cancel URL is the same with
  `&cancelled=1`. One live intent per transit: a second click returns the open session URL (M5 behaviour).
- **Approval, provider with `PaymentAuthorizer`**: same as above with `CheckoutRequest.CaptureManual = true`. The
  webhook event `authorized` leads to `contract.PaymentAuthorizedTx` (pending_payment → awaiting_approval) and
  `ExtendHoldTx`, then the email `booking_received` to the customer and an office notification.
- **Approval, provider without authorisation or no provider**: `contract.RequestApprovalTx`
  (held → awaiting_approval), `ExtendHoldTx`, email `booking_received`. No payment yet.
- Response: `{status, redirect_url?, status_token}`. `status_token = HMAC(KeyRing "booking-status", booking_id ‖
  exp)` with `exp` = departure + 30 days. It is read-only: status, reference, verifying. It lets the top-level
  return page work without the iframe's storage.

### 5.4 Confirmation (existing M5 path, ENGINE wires the online parts)

Webhook `succeeded` for intent purpose `transit` runs `collect.transitSucceededTx`, then
`ConfirmTransitPaidTx(source=provider)`, then `confirmTx` (hold becomes a transit allocation, billing hook creates
the transit charge), then `RecordTx` and `MatchTx`. ENGINE adds `booking.OnConfirmedTx` (registered as a contract
hook for channel online/external): it queues email `booking_confirmed` (dedupe key `booking-confirmed:<contract>`,
INV-77) with arrival info, the berth when `reveal_berth = after_confirmation`, the QR, and the customer-area link
(§7.1). The inquiry is linked as `booked`. With a deposit, the balance stays as the open residual of the charge.

### 5.5 Late payment (spec 23.3)

When `succeeded` arrives for a transit that is `expired`: if online and `late_payment_recovery`, then
`confirmTx` via `confirm_late`. `convertOrReallocate` re-allocates the same resource or any online solution of the
same category (quota claimed again). On success the transit is confirmed. Otherwise, or when recovery is off, the
payment is recorded and **refunded in full automatically** through the provider (`refunds` path, actor
`system:bookings.late_payment`), plus worklist item `late_payment` and the email `booking_late_refund`. This
replaces the M5 "credit" outcome for channel online only.

### 5.6 No online provider

If no provider with checkout is enabled and `allow_without_provider = false`, then config reports
`payable:false`, availability works, and `POST /bookings` returns `422 provider_disabled` (the page shows the
marina contacts). With `allow_without_provider = true`, `/pay` moves the booking to `awaiting_approval` (bank
transfer instructions from `contract_settings.bank_details`). The desk approves and confirms with the manual
driver once the money has arrived.

## 6. Approval flow (normative, ENGINE domain; ACCOUNT UI and handlers)

- `booking.Approve(p, marinaID, contractID, ifMatch)` (transits.desk), in one transaction:
  - **Authorised**: the intent stays `processing` with `authorized_at`. Mark `online_bookings.approved_at/by`
    (`contract.MarkApprovedTx`, which also extends the holds 24 h so the approved booking keeps its allocation) and
    write the audit row. `bookings.approval_expire` skips approved bookings, and `RejectTx` re-checks `approved_at`
    under the contract lock. After commit, `PaymentAuthorizer.Capture` with idempotency key `capture:<intent>`. The
    `succeeded` webhook runs `confirmTx` (`awaiting_approval → confirmed`). Job `bookings.capture` (1 min) retries
    approved but not captured intents. A capture failure (authorisation expired) creates worklist item
    `capture_failed`, and the desk can "send payment link" (approve again without authorisation).
  - **Not authorised**: `contract.ApproveTx` (awaiting_approval → pending_payment), `ExtendHoldTx` 24 h, transit
    payment link (M5 `CreateTransitLink` internals) emailed (`booking_approved_pay`). With no provider (§5.6) the
    transit stays `awaiting_approval` until the desk confirms with the manual driver (`ConfirmTransitWithPayment`
    is extended to `awaiting_approval`).
- `booking.Reject(p, marinaID, contractID, ifMatch, reason, notify)`: `contract.RejectTx` (cancelled, holds
  cancelled, quota released, inquiry `declined_other`), audit, email `booking_rejected` (when notify is set).
  After commit, `PaymentAuthorizer.VoidAuthorization`, retried by `bookings.capture`.
- The desk can move the hold before approving or arrival (M3 move). The customer sees nothing as long as the
  category does not change (spec 23.3).
- Event `transit.approval_requested` (data: `contract_id`, `reason` enum `loa|group|always|no_provider`) goes to
  SSE topic `contract.changed` and the webhook catalogue.

## 7. Customer area (normative, ACCOUNT; booking-side Tx methods by ENGINE)

### 7.1 Login and session

- `POST /account/login {email, locale}` always returns `202` with the same body and comparable work. When the
  normalised email matches customers with at least one transit in the marina, it queues email `portal_login`
  (dedupe key per customer and 10-minute bucket). Rate limits: 5 per 15 min per IP prefix and per email hash.
- The link is `PORTO_BASE_URL/b/{slug}/a#t=<token>`. The token sits in the **fragment**, so it never reaches the
  server or any log. Token: 32 random bytes, stored as SHA-256 in `customer_portal_links (marina_id, customer_id,
  token_hash, expires_at 30 min, used_at)`, single use. The booking confirmation email carries a long-lived link
  (`kind = booking`, valid until departure + 30 days, reusable, scoped to that customer).
- `POST /account/session {token}` returns `{session_token, expires_at}`. `customer_portal_sessions (token_hash,
  customer_id, marina_id, expires_at 8 h idle / 7 d absolute, revoked_at)`. The client stores it in
  `sessionStorage` (memory fallback) and sends it as `Porto-Customer-Session`. `DELETE /account/session` revokes
  it. INV-75 extends to portal tokens: they resolve only their marina and customer (**INV-88**).

### 7.2 Operations (all scoped to the session's customer and marina; any other id is `404`)

- `GET /account/bookings`: the customer's transits (any channel), newest first: reference, dates, status, category
  name, amounts (total, paid, residual), approval state, and the berth only when revealed.
- `GET /account/bookings/{id}`: detail with QR (when confirmed), arrival info, documents (type, uploaded,
  verified), receipts (payments: date, amount, method), cancellation preview.
- `POST /account/bookings/{id}/documents`: same rules as §5.2.
- `GET /account/documents/{documentId}/url`: short-lived signed URL (M4 mechanism), own documents only.
- `POST /account/bookings/{id}/pay-balance` (Idempotency-Key): creates or reuses a `payment_request` for the
  transit's open charges (system actor), then returns the checkout URL. `422 nothing_due` when the residual is 0.
  An open, unexpired request of the customer for exactly the same charges is reused (same link, amount refreshed
  to the residuals, expiry extended), so repeated clicks never leave several open requests for the same money.
- `GET /account/bookings/{id}/cancellation` gives a preview, and `POST …/cancel {expected_refund_cents}`
  (Idempotency-Key) cancels. Pure `policy.Refund(tiers, paid, arrival, today)` uses the **policy snapshot of
  the contract**, never the current settings. If the refund differs from `expected_refund_cents` it returns
  `409 refund_changed`. Allowed for `held|pending_payment|awaiting_approval|confirmed` before arrival day. Runs
  `contract.CancelOnlineTransitTx` (actor `customer:<id>`) plus quota release and, in the same transaction, the
  refund rows through the `payment` refund path (or the credit if `refund_as=credit`), email `booking_cancelled`:
  the cancellation never commits without its refund. Only the provider call runs after commit (a pending refund
  the payment refund path retries). The refund of a shorter stay (change-dates) is written the same way, as a
  partial reversal of the match: a later cancellation reverses the same match again for the rest (migration
  `00017`, ADR 0013 decision 8; `TestPortal_ChangeDatesThenCancelRefundsTheRest`).
- `POST /account/bookings/{id}/change-dates/preview` and `…/change-dates {arrival, departure, expected_total_cents}`:
  only `confirmed` transits before arrival. Online rules apply, the same resource is preferred, otherwise a new
  online solution of the same category. `contract.ChangeOnlineStayTx` reuses `ChangeStay` internals (quota
  claim/release, price effect). A higher price creates a supplementary charge to pay through pay-balance. A lower
  price is refunded per the cancellation tiers applied to the removed amount (§16 D12).
- `GET /account/vessels`: the customer's vessels (name, dimensions) to prefill step 1. `POST /bookings` accepts
  `vessel_id` only together with a valid `Porto-Customer-Session` of the same customer.

## 8. `BookingChannel` and the fake channel (normative; CONTRACT interface, ENGINE implementation)

### 8.1 `pkg/ext/channel.go` (Porto's own design)

```go
// BookingChannel connects an external berth marketplace (spec 23.2, 27.5). Calls never run inside a database
// transaction; credentials arrive per call (WithChannelCredentials); outbound HTTP only through the host client.
type BookingChannel interface {
    Code() string
    Capabilities() ChannelCapabilities // Push, Pull, Webhook, Ack bool
    PushAvailability(ctx context.Context, a ChannelAvailability) error // per category and night: available bool, units int
    PushRates(ctx context.Context, r ChannelRates) error               // per category and night: price cents for a reference size, currency
    PullReservations(ctx context.Context, since string) (res []ChannelReservation, next string, err error)
    ParseWebhook(r *http.Request, secret []byte, now time.Time) ([]ChannelReservation, error) // ErrInvalidSignature, ErrIgnoredEvent
    Acknowledge(ctx context.Context, ack ChannelAck) error // accepted (with Porto reference) or rejected (reason enum)
}
// ChannelReservation: EventID, ExternalRef, Kind (new|modify|cancel), MarinaRef, CategoryCode, Arrival, Departure
// (YYYY-MM-DD), LOACm, BeamCm, DraftCm int64, HullType, Guest{FirstName, LastName, Email, Phone, Country, Locale}
// (PII: never logged), AmountCents int64, Currency, PaidToChannel bool, ReceivedAt.
```

`Extensions.BookingChannels []BookingChannel`. `TestExt_NoInternalImports` still passes.

### 8.2 Host side (`internal/domain/channel`)

- `channel_connections (marina_id, channel_code, enabled, credentials_enc, webhook_secret_enc, category_map json
  {external code → Porto category}, push_enabled, pull_cursor, last_push_at, last_pull_at, version)`. Secrets are
  write-only (KeyRing AAD `channel:<marina>:<code>`).
- `channel_reservations (marina_id, channel_code, external_ref, contract_id?, status received|booked|rejected|
  cancelled, reject_reason, payload_sha256, created_at, updated_at)`, unique `(marina_id, channel_code,
  external_ref)`. `channel_events (marina_id, channel_code, event_id)` unique: an event is applied once
  (**INV-89**).
- `ApplyReservation` (one transaction per reservation):
  - `new`: rules (stay, advance) checked, then `booking.CreateChannelBookingTx`. That runs the same search with
    channel `external` restricted to the mapped category (quotas apply), then `CreateOnlineTransitTx` (channel
    external), then customer and vessel from the guest, then `confirmTx(source=channel)` when `PaidToChannel`,
    otherwise the transit stays `held` with hold 24 h for the desk. Retries on `ErrResourceTaken` as in §4.3. No
    space means `rejected` plus inquiry `declined_no_space` (channel external, source channel), then
    `Acknowledge(rejected)` after commit.
  - `cancel`: `contract.CancelTransit` path (actor `channel:<code>`). `modify`: rejected with reason
    `modify_unsupported` and a worklist item (§16 D15).
- Jobs: `channels.pull` (5 min, per enabled connection, advisory lock per connection), `channels.push` (1 min,
  pushes when a dirty flag was set by the outbox subscriber on `occupancy.*`, `transit.*`, `config.*` events,
  plus a full push every 6 h). The pushed data are the same values as §3.2 for the next 365 days, with units =
  free solutions capped by the quota.
- Webhook `POST /webhooks/channels/{channel}/{marinaId}` (rate limited, raw body ≤ 256 KiB): unknown channel or
  connection gives 404, invalid signature gives 401, otherwise apply and return 200.

### 8.3 Confirmation by channel (ADR 0013)

The channel is the merchant of record. A verified reservation (signed webhook or authenticated pull) flagged as
paid is the payment proof, so `confirmTx(source=channel)`. CONTRACT updates INV-09 and
`TestContract_ConfirmedOnlyInConfirmTransit` accordingly.

### 8.4 Fake channel (`internal/domain/channel/fakechannel`)

In-memory and goroutine-safe. `Inject(res ...ChannelReservation)` queues reservations for pull, `Pushed()`
records pushes, `Webhook(res)` builds a signed `*http.Request` (HMAC-SHA256 `t=…,v1=…` like Porto-Signature), and
the configured outcome makes `Acknowledge` errors injectable. It is registered in the app only in tests, and in
`PORTO_DEV=true` when `PORTO_DEV_FAKE_CHANNEL=true`.

## 9. Lost demand (ENGINE)

- `GET /booking/availability` with zero available categories, and no rule block, inserts an anonymous `inquiries`
  row: channel `online`, source `public`, purpose `transit`, `loa_cm`, `beam_cm`, `draft_cm`, arrival, departure,
  outcome `declined_size` when no online resource fits the dimensions at all, otherwise `declined_no_space`. No
  customer, no vessel, no IP, no user agent. Inserted in its own small write transaction after the read. A failure
  is logged (no PII) and never fails the search.
- Dedupe: an in-process TTL cache (30 min, 10 000 entries) keyed by `sha256(ip_prefix | dims | dates)`, never
  stored. A repeated search does not insert again.
- `POST /bookings` creates an inquiry (`open`, linked to the contract). Confirmed sets `booked`, expired unpaid
  sets `lost`, rejected sets `declined_other`. Channel no-space sets `declined_no_space`.
- R4 (M7) needs no change (`TestAcceptance_M8_LostDemandInR4` proves it).

## 10. Widget, embedding and security headers (PUBLIC; server bits listed)

- **iframe**: `<iframe src="https://{host}/b/{slug}?embed=1" allow="payment" title="…">`.
  **Script**: `<div data-porto="{slug}" data-lang="it"></div><script src="https://{host}/embed.js" async></script>`.
  `embed.js` (hand-written, < 3 KB gzip, ES2017, no dependencies) creates the iframe with
  `?embed=1&parent=<location.origin>`, resizes on `porto:resize`, and re-dispatches `porto:booked` (reference
  only) and `porto:step` as `CustomEvent('porto:booking', …)` on the container. It accepts messages only when
  `event.origin` is the Porto origin of the script.
- **frame-ancestors**: middleware `EmbedFraming` on the static `/b/{slug}…` HTML responses only. It resolves the
  slug to `allowed_embed_origins` (cached 60 s, `marina.EmbedOrigins`) and sets
  `Content-Security-Policy … frame-ancestors 'self' <origins>`. It removes `X-Frame-Options` when origins exist,
  otherwise it keeps `SAMEORIGIN`. Unknown slug: `frame-ancestors 'self'`. Every API, admin and other response
  keeps `frame-ancestors 'none'` and `X-Frame-Options: DENY`. `/embed.js`: `Cache-Control: public,
  max-age=3600`, `Cross-Origin-Resource-Policy: cross-origin`, `Access-Control-Allow-Origin: *`. The CSP of the
  booking HTML also gets `form-action 'self' <provider checkout origins>`.
- **postMessage**: the SPA in embed mode accepts `parent` only when it is in `PublicMarina.embed_origins` (added to
  `GET /api/public/v1/{slug}`) **and** equals `document.referrer`'s origin (or `location.ancestorOrigins[0]` when
  available). Otherwise embed messaging is disabled and the page still works. Messages are posted only to that
  origin.
- **Checkout from the iframe**: hosted checkout pages refuse framing, so the "Paga" button navigates the top window
  on the user's click (`window.open(url, '_top')`). The success URL is Porto's top-level return page
  `/b/{slug}/r/{id}#st=…&rt=…`. It polls `GET /bookings/{id}/status` with the status token. Once confirmed, it
  shows the confirmation and a "Torna al sito" button to `return_to`, accepted only when its origin is in
  `allowed_embed_origins`, otherwise hidden.
- No third-party cookies. Booking and session tokens live in `sessionStorage` (memory fallback) inside the iframe.
- Rate limits (per IP prefix, CONTRACT in `server.go`): config 120/min, availability 60/min, quote 60/min,
  createBooking 10/min, details/documents/pay 20/min, status 60/min, account login 5/15 min (plus per email hash
  in the domain), session 10/min, account reads 60/min, account writes 20/min, channel webhook 600/min.

## 11. API (CONTRACT; `api/openapi.yaml`, tags `booking-public`, `account`, `online-admin`, `channels`)

P = `/api/public/v1/{slug}` (`security: []`), M = `/api/admin/v1/marinas/{marinaId}`.

| Method and path | operationId | Notes |
|---|---|---|
| GET P/booking/config | getBookingConfig | rules, categories (code, i18n name, max LOA/beam), online services, payment mode, approval threshold, required fields, current texts (kind, version, text), payable, theme, arrival info, units; `public, max-age=60` |
| GET P/booking/availability | getBookingAvailability | query `arrival, departure, loa_cm, beam_cm, draft_cm?, hull_type?, vessel_id?`; records lost demand |
| POST P/booking/quote | quoteBooking | `{arrival, departure, vessel, category, services[], promo_code?}` gives breakdown, VAT, total, due now, tiers |
| POST P/bookings | createBooking | Idempotency-Key required, `x-porto-idempotency: domain`; `201` with token |
| GET P/bookings/{bookingId} | getBooking | `Porto-Booking-Token` |
| DELETE P/bookings/{bookingId} | abandonBooking | held only |
| PUT P/bookings/{bookingId}/details | submitBookingDetails | |
| POST P/bookings/{bookingId}/documents | uploadBookingDocument | multipart, `x-porto-max-body-bytes: 10485760` |
| POST P/bookings/{bookingId}/pay | startBookingPayment | Idempotency-Key |
| GET P/bookings/{bookingId}/status | getBookingStatus | `Porto-Booking-Token` or `Porto-Status-Token`; no PII |
| POST P/account/login | requestAccountLink | always 202 |
| POST P/account/session | openAccountSession | |
| DELETE P/account/session | closeAccountSession | |
| GET P/account/vessels | listAccountVessels | |
| GET P/account/bookings | listAccountBookings | |
| GET P/account/bookings/{contractId} | getAccountBooking | |
| POST P/account/bookings/{contractId}/documents | uploadAccountDocument | multipart |
| GET P/account/documents/{documentId}/url | getAccountDocumentUrl | |
| POST P/account/bookings/{contractId}/pay-balance | payAccountBalance | Idempotency-Key |
| GET P/account/bookings/{contractId}/cancellation | previewAccountCancellation | |
| POST P/account/bookings/{contractId}/cancel | cancelAccountBooking | Idempotency-Key |
| POST P/account/bookings/{contractId}/change-dates/preview | previewAccountDateChange | |
| POST P/account/bookings/{contractId}/change-dates | changeAccountDates | Idempotency-Key |
| GET/PUT M/online-booking/settings | getOnlineBookingSettings / updateOnlineBookingSettings | marina.configure, If-Match |
| GET/POST M/online-booking/texts | listBookingTexts / publishBookingText | marina.configure; published texts immutable |
| GET/PUT M/online-booking/quotas | getOnlineQuotas / updateOnlineQuotas | harbor.configure |
| GET M/online-booking/quotas/usage | getOnlineQuotaUsage | `from,to` ≤ 92 days |
| GET M/online-bookings | listOnlineBookings | transits.desk; filters status, channel, from, to, cursor |
| GET M/online-bookings/{contractId} | getOnlineBooking | contact, documents, consents, payment state, approval deadline |
| POST M/online-bookings/{contractId}/approve | approveOnlineBooking | transits.desk, If-Match |
| POST M/online-bookings/{contractId}/reject | rejectOnlineBooking | `{reason, notify}` |
| POST M/online-bookings/{contractId}/payment-link | sendOnlineBookingPaymentLink | after capture failure |
| POST M/documents/{documentId}/verify | verifyDocument | customers.write; `{verified: bool}` |
| GET M/booking-channels | listBookingChannels | providers.configure |
| PUT M/booking-channels/{code} | updateBookingChannel | secrets write-only, If-Match |
| POST M/booking-channels/{code}/test, /sync | testBookingChannel / syncBookingChannel | |
| GET M/channel-reservations | listChannelReservations | transits.desk |
| POST /webhooks/channels/{channel}/{marinaId} | receiveChannelWebhook | signature |

No new permission (§16 D16). Problem slugs (errmap, i18n `errors.*`): `booking_unavailable` (409, with
`available_categories`), `quota_exhausted` (internal, mapped to booking_unavailable), `hold_expired` (409),
`booking_rules` (422, field and code), `booking_busy` (503 + Retry-After), `consent_required` (422),
`text_outdated` (409), `refund_changed` (409), `price_changed` (409), `cancellation_not_allowed` (422),
`nothing_due` (422), `idempotency_mismatch` (422), `provider_disabled` (exists), `link_invalid` (exists, 404).
Meta capabilities: `online_booking`, `booking_channels`. Events: `transit.approval_requested` (new, webhook
catalogue, SSE `contract.changed`).

## 12. UI

### 12.1 Public page (PUBLIC, `web/booking/src/booking/**`, embed, return page)

The flow is a single page with steps, each step a URL hash so that back and forward work:

1. **Quando e barca**: native `<input type="date">` arrival/departure (shadcn `Input`, best on phones, zero
   weight), LOA, beam, draft in metres with 2 decimals (feet toggle display only, data in cm), hull type select,
   or saved vessels when a customer session exists. "Cerca".
2. **Posto**: category cards (name, "da € …", badge "su richiesta" when approval applies), or the empty state
   with the reason, and a "contatta il marina" link (lost demand is recorded server side).
3. **Extra e riepilogo**: online services (checkbox/quantity), promo code, price breakdown (debounced
   `quoteBooking`), cancellation tiers. "Blocca il posto" calls `createBooking` and starts the countdown.
4. **Dati**: customer and vessel form (shadcn `Field`, `Input`, `Select`), people on board, arrival time, document
   upload (camera capture `accept="image/*,application/pdf"`), consents (terms and privacy checkboxes with links
   to text dialogs, marketing unchecked).
5. **Pagamento**: amount due now, "Paga" (top navigation in embed), or "Invia richiesta" for approval.
6. **Return page** `/b/{slug}/r/{id}`: "pagamento in verifica" until status `confirmed`, `awaiting_approval`
   (received) or failure, then the confirmation (reference, QR, arrival info, `.ics` download built client side,
   customer-area hint).

The countdown comes from `hold_expires_at`. On `hold_expired` the page offers to search again with the same input.
Errors use `errors.<slug>`. axe has no serious violations. Everything is usable at 320 px with no horizontal
scroll. **Budget**: CONTRACT stops `@porto/shared/api` from re-exporting `sse` (which pulls `offline`). PUBLIC makes
`links/`, `pay/`, `account/` and the steps after step 3 `React.lazy` chunks and rewrites `scripts/budget.mjs` to
read Vite's `manifest.json`: **entry plus static imports ≤ 180 KB gzip** (spec), total JS ≤ 450 KB gzip (guard).

### 12.2 Customer area (ACCOUNT, `web/booking/src/account/**`)

`/b/{slug}/account` (email form), `/b/{slug}/a#t=` (exchange, then redirect to the list, token wiped from the
URL with `history.replaceState`), list, detail (QR, arrival info, documents with upload and verified badges,
receipts, "Paga saldo", "Cambia date" dialog with preview, "Annulla" AlertDialog with refund preview), logout.

### 12.3 Desk and configuration (ACCOUNT, `web/admin/src/features/online/**`)

- Nav group "Operatività", entry **Prenotazioni online** (`/m/$marinaId/online`): table (reference, dates, vessel
  dims, status badge, channel, approval deadline countdown, amount), filters, live via SSE invalidation. Detail
  sheet: contact (decrypted, `customers.read`), vessel, documents with preview and "Verificato" switch,
  consents, payment state, Approva / Rifiuta (dialog with reason and "notifica il cliente"), "Invia link di
  pagamento", link to the transit page and the map.
- Settings group, entry **Prenotazione online** (`/m/$marinaId/settings/online`): tabs Regole, Pagamento e
  cancellazione (tier editor), Approvazione, Testi (versions, publish new version, preview), Arrivo, Quote
  (groups with kind and value, usage heat table for 30 nights), Incorpora (allowed origins editor bound to the
  marina field, snippet generator with copy buttons, live preview iframe), Canali (connections, secrets
  write-only, category map, test, sync, recent reservations).
- Map editor inspector: "Vendibile online" switch for dynamic areas (berths already have it).

## 13. Data (CONTRACT; `00016_online_booking.sql`, SQLite and PostgreSQL, same names)

`ALTER TABLE` adds: `berth_groups.online_quota_kind TEXT NOT NULL DEFAULT 'none' CHECK (...)`,
`online_quota_value INTEGER NOT NULL DEFAULT 0 CHECK (online_quota_value >= 0)`;
`dynamic_areas.online_sellable` (bool, default false); `documents.verified_at`, `verified_by_user_id`;
`payment_intents.capture_method TEXT NOT NULL DEFAULT 'automatic' CHECK (capture_method IN
('automatic','manual'))`, `authorized_at TIMESTAMP`, `captured_at TIMESTAMP`, `voided_at TIMESTAMP` (status stays
`processing` while authorised, §16 D6).

New tables (all with `marina_id`, composite FKs `(x_id, marina_id)` as in earlier migrations):
`online_booking_settings` (PK marina_id, the §3.1 fields as typed columns plus `json` for tiers, approval,
required fields, arrival info; `version`); `booking_texts (id, marina_id, kind terms|privacy|marketing, version,
texts json i18n, sha256, published_at, published_by)` unique `(marina_id, kind, version)`;
`online_bookings (id, marina_id, contract_id unique, reference unique per marina, token_nonce, idempotency_hash
unique per marina, body_sha256, contact_enc, people_on_board, approval_reason, approval_deadline, approved_at,
approved_by_user_id, rejected_at, reject_reason, embed_parent_origin, locale, inquiry_id, created_at,
updated_at)`; `online_quota_usage`, `online_quota_claims` (§3.3); `customer_portal_links`,
`customer_portal_sessions` (§7.1); `channel_connections`, `channel_reservations`, `channel_events` (§8.2).
Indexes: `online_bookings (marina_id, created_at)`, `contracts (marina_id, channel, status)` when missing,
`online_quota_usage (marina_id, night)`. Backup registration of every table, with encrypted columns listed. Down
migration drops everything (SQLite column drops via `ALTER TABLE DROP COLUMN`, supported by modernc SQLite ≥
3.35).

### 13.1 Invariants (CONTRACT writes them in `docs/invariants.md`; ENGINE/ACCOUNT flip statuses)

- **INV-08** extended: `awaiting_approval` included. Online variant `TestCheckout_HoldHasAllocation`.
- **INV-09** extended: `source=channel` (§8.3). Online variant `TestCheckout_BrowserReturnDoesNotConfirm`,
  `TestCheckout_ConfirmOnlyFromWebhook`.
- **INV-12** online quota: `used = |claims|` and `used ≤ cap` per (group, night), on both dialects under 100
  goroutines (`TestConcurrency_OnlineQuotaNeverExceeded`, Q19).
- **INV-13** no double sale across channels: one winner per resource-night among desk, online and channel
  (`TestConcurrency_NoDoubleSaleDeskOnlineChannel`).
- **INV-88** portal and booking tokens resolve only their marina, customer or booking
  (`TestPortal_TokenScoped`, `TestBooking_TokenScoped`).
- **INV-89** channel reservation and event applied once (`TestConcurrency_ChannelReservationOnce`).

## 14. Tests

### 14.1 Pure (table tests, plus rapid where noted)

`TestPolicy_CheckStay` (min/max nights, advance, weekdays, timezone edges, DST), `TestPolicy_Approval`,
`TestPolicy_Refund` (tiers, boundaries at midnight marina time, rounding half-even, rapid: 0 ≤ refund ≤ paid),
`TestPolicy_QuotaCap` (rapid: monotone in percent), `TestPolicy_Deposit`, `TestBookingToken_RoundTrip`,
`TestStatusToken_Expiry`, `TestEmbed_OriginParsing` (Go), `TestFakeChannel_Signature`.

### 14.2 Integration (`//go:build integration`, `testdb` SQLite + PostgreSQL)

ENGINE: `TestBooking_AvailabilityFiltersOnlineSellableAndQuota`, `TestBooking_AvailabilityNoPII`,
`TestBooking_CreateHoldsConcreteResource`, `TestCheckout_HoldHasAllocation`,
`TestBooking_CreateIdempotent` (same key gives the same booking and token; different body gives 422),
`TestBooking_RetryNextSolutionOnConflict`, `TestBooking_HoldExpiryReleasesQuotaAndMarksLost`,
`TestCheckout_ConfirmOnlyFromWebhook`, `TestCheckout_BrowserReturnDoesNotConfirm`,
`TestCheckout_LatePaymentRecoveredOrRefunded`, `TestApproval_AuthorizeCaptureConfirm`,
`TestApproval_RejectVoidsAndReleases`, `TestApproval_DeadlineAutoRejects`, `TestApproval_NoProviderPaymentLink`,
`TestChannel_ApplyNewPaidConfirms`, `TestChannel_NoSpaceRejectsAndRecordsInquiry`, `TestChannel_CancelReleases`,
`TestChannel_PushAfterOccupancyChange`, `TestLostDemand_AnonymousRowDeduped`, `TestBooking_TokenScoped`,
`TestBooking_CrossMarina404`, `TestStripe_ManualCaptureMapping` (fixtures),
`TestBooking_AvailabilityBudget` (skip with `-short`).
ACCOUNT: `TestPortal_LoginNoEnumeration` (same status, body and comparable timing for known and unknown email),
`TestPortal_LinkSingleUse`, `TestPortal_TokenScoped`, `TestPortal_SessionExpiry`,
`TestPortal_CancelUsesPolicySnapshot`, `TestPortal_CancelRefundChanged409`, `TestPortal_ChangeDatesQuotaAndPrice`,
`TestPortal_PayBalanceReusesRequest`, `TestOnlineSettings_IfMatchAndValidation`, `TestBookingTexts_Immutable`,
`TestDocuments_Verify`.
PUBLIC: `TestEmbedFraming_Headers` (allowed origin, none, unknown slug, API stays DENY),
`TestEmbedJS_Served` (cache, CORP, size < 3 KB gzip).

### 14.3 Concurrency (both dialects, `-race`, ≥ 100 goroutines)

- **`TestConcurrency_NoDoubleSaleDeskOnlineChannel`** (ENGINE, `internal/acceptance/m8_concurrency_test.go`, API
  and service level): one marina, a category with 3 online berths and a 2-unit quota, 3 desk-only berths. **120
  goroutines** started on one barrier: 40 desk `CreateTransit` with explicit placements on the online berths, 40
  online `createBooking` via HTTP (`httptest`), 40 fake-channel reservations via `ApplyReservation`, on
  overlapping stays (same dates, ±1 night shifted). Then: `store.CheckOccupancyInvariants` clean (Q1, Q6, Q7, Q8,
  Q15, Q17, Q19), each (resource, night) at most one active row, online plus external claims ≤ quota on every night,
  every loser got `resource_taken` / `booking_unavailable` / channel `rejected`, and the number of winners equals
  the number of distinct free resource-nights consumed. A second round confirms half the online holds through
  fake webhooks while the expiry job runs (`clock.Fake` advanced past TTL) and checks INV-05/08/12.
- `TestConcurrency_OnlineQuotaNeverExceeded` (100 online bookings, quota 3, 10 berths).
- `TestConcurrency_ConfirmVsExpireOnline` (webhook at T−ε against reclaim at T+ε).
- `TestConcurrency_ApproveVsDeadline`, `TestConcurrency_ChannelReservationOnce` (same event 100×),
  `TestConcurrency_CreateBookingSameKey` (100× same Idempotency-Key gives one booking).

### 14.4 Acceptance (`internal/acceptance/m8_*_test.go`, API only, `clock.Fake`, both dialects)

`TestAcceptance_M8_OnlineBookingHappyPath` (config, availability, quote, create, details, document, pay, fake
webhook, confirmed, email with QR and link, portal login, booking visible, pay balance, cancel with refund
preview), `TestAcceptance_M8_ApprovalFlow`, `TestAcceptance_M8_ChannelFlow`, `TestAcceptance_M8_LostDemandInR4`,
plus the existing cross-cutting `TestAcceptance_CrossMarinaIsolation` (new routes), `TestEvents_DataHasNoPII`,
`TestLogs_NoPII` (now including public booking and account flows), `TestSSE_PayloadHasNoPII`.

### 14.5 Web

- Vitest (PUBLIC): `bookingFlow.test.tsx` (steps, hash navigation, countdown, `hold_expired` recovery),
  `embedMessaging.test.ts` (parent origin validation, messages only to the verified origin), `embedScript.test.ts`
  (embed.js in jsdom: iframe creation, resize, CustomEvent, origin check), `returnPage.test.tsx`, `units.test.ts`
  (m/ft display, cm data), `budget` script unit test on a fixture manifest.
- Vitest (ACCOUNT): `accountLogin.test.tsx` (fragment token wiped), `accountBooking.test.tsx` (cancel preview,
  refund_changed), `OnlineBookingsPage.test.tsx`, `OnlineSettingsPage.test.tsx` (tier editor validation,
  snippet).
- Playwright:
  - **`e2e/tests/phone-booking-embed.spec.ts`** (PUBLIC; phone project, Pixel 7 viewport): an admin API setup
    (outside the timer) enables booking, sets a berth online sellable, a price list, the Stripe driver in test
    mode with `PORTO_STRIPE_API_BASE` pointing to the stub, and allowed origin `http://localhost:18081`. Then
    `const t0 = Date.now()`, open the host page `http://localhost:18081/host.html` (a different origin from
    `127.0.0.1:18080`) that embeds through `embed.js`. Inside `frameLocator`: dates, dimensions, category, extras,
    "Blocca", details, consents, document photo upload (fixture), "Paga", which goes top level to the stub
    checkout, "Paga" on the stub, which sends a signed `checkout.session.completed` webhook to Porto and redirects
    to the return page, until "Prenotazione confermata". Assert `Date.now() - t0 < 120_000` (test timeout
    180 s), no horizontal scroll at 390 px, axe clean on each step, `porto:booked` received by the host, and the
    host origin removed from the allowlist means the iframe is blocked (`frame-ancestors`).
  - `e2e/tests/booking.spec.ts` (PUBLIC): replace the placeholder assertions (search, empty state, lost demand
    visible in the R4 report via API).
  - `e2e/tests/online-approval.spec.ts` (ACCOUNT): booking over the approval LOA, desk approves, stub capture,
    confirmed. Reject path with email.
  - `e2e/tests/phone-customer-area.spec.ts` (ACCOUNT): login link from the mail dir, booking list, document
    upload, cancel with refund preview.
  - `a11y.spec.ts` additions for the new admin pages (ACCOUNT).
  - Support: `e2e/support/host-server.mjs` (static host page on 18081) and `e2e/support/stripe-stub.mjs` (Checkout
    Session create/retrieve, PaymentIntent capture/cancel, refunds, hosted pay page, webhook signer with the
    configured secret) on 18082, both started as extra `webServer` entries.

## 15. Ownership, order and acceptance

### 15.1 CONTRACT (runs first, alone; then frozen)

`api/openapi.yaml` + `make gen`; `internal/migrations/{sqlite,postgres}/00016_online_booking.sql`;
`internal/store/online_models.go` (all new models), `backup_tables.go`/`backup.go` registration; `pkg/ext/channel.go`,
`pkg/ext/payment.go` (`PaymentAuthorizer` optional interface: `Capture`, `VoidAuthorization`;
`CheckoutRequest.CaptureManual`; kinds `authorized`, `authorization_voided`), `pkg/ext/ext.go`
(`BookingChannels`); `internal/domain/paydriver/paytest/` (shared fake provider with authorise/capture, moved
from the collect test env, which then imports it); `internal/domain/contract/states.go` (rows and constants of
§4.1) and `contract/online.go` **signatures only** (`CreateOnlineTransitTx`, `SetOnlineCustomerTx`,
`RequestApprovalTx`, `PaymentAuthorizedTx`, `ApproveTx`, `RejectTx`, `ExtendHoldTx`, `CancelOnlineTransitTx`,
`ChangeOnlineStayTx`, all returning `ErrNotImplemented`); `occupancy/quota.go` signatures and
`SearchInput.Channel` docs; `internal/domain/booking/{types,service,errors}.go` (all exported methods used by
handlers, portal and channel, returning `ErrNotImplemented`), `booking/policy/doc.go` + types;
`internal/domain/onlineconf/{types,load}.go` (Load implemented: defaults when no row); `internal/domain/portal/
{types,service}.go` skeleton; `internal/domain/channel/{types,service}.go` skeleton; event constants, webhook
catalogue, SSE mapping; problem slugs and `errmap.go`; `internal/http/public/public.go` fields + stubs
`booking.go`, `account.go` (501); `internal/http/admin/admin.go` fields + stubs `online_bookings.go`,
`online_settings.go`, `booking_channels.go`; `internal/http/webhooks/channels.go` stub; `internal/http/server.go`
(rate limits §10, idempotency exclusion, `/embed.js` route, `EmbedFraming` insertion calling
`middleware.EmbedFraming(resolver)` with a no-op stub in `middleware/embed.go`, AccessLog unchanged because the
tokens are in headers or fragments); `internal/app/app.go` wiring + job names; config `PORTO_DEV_FAKE_CHANNEL`;
meta capabilities; `.golangci.yml` rules (§2); web: `@porto/shared/api` barrel without `sse` (admin imports
`@porto/shared/api/sse`), `queryKeys.ts`, hook stubs `web/shared/src/api/hooks/{onlineBooking,account,
onlineAdmin}.ts`, i18n files `checkout.json`, `account.json`, `online.json` (it, en, keys skeleton) +
`resources.ts`, `web/booking/src/{routes.ts,main.tsx,i18n.ts}` (routes `booking`, `return`, `account`,
`accountLink`, lazy stubs `booking/index.tsx`, `account/index.tsx`, `booking/ReturnPage.tsx`),
`web/admin/src/router.tsx` factories `onlineRoutes`, `onlineSettingsRoutes` + `nav.ts` entries; shadcn
components via CLI if missing (none expected: all needed exist); e2e: `playwright.config.ts` webServers +
stub files `e2e/support/{host-server,stripe-stub}.mjs` (minimal, PUBLIC completes); docs: ADR
`docs/adr/0013-prenotazione-online-m8.md` (Italian, "proposta": §3.3, §4.1, §4.3, §8.3, §10, §16), invariants
§13.1, `docs/architecture.md` §2/§10 rows. Ends with `make gen && make check` green.

### 15.2 Builders (parallel, exclusive files)

| Builder | Owns |
|---|---|
| **ENGINE** | `internal/domain/booking/**` (implements bodies of CONTRACT signatures; `policy/**`, `lostdemand.go`, `jobs.go`, `hooks.go`, tests); `internal/domain/occupancy/{quota.go,search.go}` + `occupancy/engine/*.go` (online filters) + `allocate.go` (quota hook only) + tests; `internal/domain/contract/{online.go,confirm.go,transits.go,payments.go,expiry.go,hooks.go}` (online parts) + tests; `internal/domain/collect/intents.go` (authorized, voided, late recovery/refund) + tests; `internal/domain/payment/transit.go` (accept `awaiting_approval`); `internal/domain/paydriver/stripe/**` (manual capture, events); `internal/domain/channel/**` incl. `fakechannel/`; `internal/domain/harbor/{document.go,prepare.go}` (area `online_sellable`); `internal/store/{online_quota.go,online_bookings.go,channels.go,inquiries_online.go}` + Q19 in `occupancy_invariants.go`; notify templates `booking_{received,confirmed,approved_pay,rejected,cancelled,late_refund}` (it, en); `internal/http/public/booking.go`, `internal/http/webhooks/channels.go` + tests; `internal/acceptance/m8_{booking,approval,channel,concurrency,lostdemand}_test.go`; invariants statuses INV-08/09/12/13/89 |
| **PUBLIC** | `web/booking/**` except `src/account/**` (owns `main.tsx`, `routes.ts`, `i18n.ts` after CONTRACT; adds account entries only as CONTRACT left them); `web/booking/public/{embed.js,manifest.webmanifest,sw.js}`, `scripts/budget.mjs`, `vite.config.ts`; `internal/http/middleware/embed.go` + test; `internal/domain/marina/embed.go` (cached `EmbedOrigins`); `internal/http/static` changes for `/embed.js` and booking SW scope (if needed); `web/shared/src/api/hooks/onlineBooking.ts`; i18n values `booking`, `checkout`; Vitest §14.5 PUBLIC; `e2e/tests/{phone-booking-embed,booking}.spec.ts`, `e2e/support/{host-server,stripe-stub}.mjs`, `e2e/fixtures/m8/*` |
| **ACCOUNT** | `internal/domain/portal/**`; `internal/domain/onlineconf/**` except `types.go`/`load.go` signatures; `internal/store/{portal.go,online_settings.go,documents_verify.go}`; `internal/domain/document/verify.go`; `internal/http/public/account.go`; `internal/http/admin/{online_bookings,online_settings,booking_channels}.go` + tests; notify template `portal_login` (it, en); `web/booking/src/account/**`; `web/admin/src/features/online/**`; `web/admin/src/features/maps/**` (area "Vendibile online" switch only); `web/shared/src/api/hooks/{account,onlineAdmin}.ts`; i18n values `account`, `online`; Vitest §14.5 ACCOUNT; `e2e/tests/{online-approval,phone-customer-area}.spec.ts`, `a11y.spec.ts` additions; invariant status INV-88 |

### 15.3 Parallel-phase rules

- OpenAPI, migration, models, `pkg/ext`, state rows and all signatures are frozen. Changes go in the builder's
  report to the orchestrator, never as edits.
- ACCOUNT and PUBLIC build against the generated client with MSW fixtures until ENGINE lands. ACCOUNT handlers call
  only `booking.*`, `channel.*`, `portal.*` and `onlineconf.*` methods declared by CONTRACT.
- PUBLIC never imports from `src/account/**` and ACCOUNT never imports from `src/booking/**`. Shared UI pieces
  needed by both (price breakdown, QR, countdown) are created by PUBLIC in `web/booking/src/common/**` and
  ACCOUNT imports them read-only (request changes, do not edit).
- Contract-package files owned by ENGINE are the only place that changes `contracts.status`. Portal code calls
  `contract.*Tx` through booking-provided methods.
- Each builder finishes with: its packages' tests (both dialects where integration), `golangci-lint` clean on its
  files, `npm --prefix web run lint && npm --prefix web run typecheck` for web owners, and the
  `invariants-guardian` subagent (ENGINE).

### 15.4 Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
go test -count=1 ./internal/domain/booking/... ./internal/domain/portal/... ./internal/domain/channel/... ./pkg/ext/... ./internal/http/middleware/...
go test -count=1 ./internal/domain/booking/policy/... -run 'TestPolicy_' -rapid.checks=2000
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/store/... ./internal/domain/booking/... ./internal/domain/portal/... ./internal/domain/channel/... ./internal/domain/occupancy/... ./internal/domain/contract/... ./internal/domain/collect/... ./internal/http/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -timeout 30m ./internal/acceptance/... ./internal/domain/... -run '^TestConcurrency_(NoDoubleSaleDeskOnlineChannel|OnlineQuotaNeverExceeded|ConfirmVsExpireOnline|ApproveVsDeadline|ChannelReservationOnce|CreateBookingSameKey|ChannelVsDesk|ExpiredHoldReclaim|ConvertVsReclaim)$'
go test -tags=integration -count=1 -run 'TestBooking_AvailabilityBudget' ./internal/domain/booking/...
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M8|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII|TestSSE_PayloadHasNoPII'
make test-db && make test-race
npm --prefix web run test
npm --prefix web/booking run build && npm --prefix web/booking run budget          # entry+static ≤ 180 KB gzip
make e2e            # phone-booking-embed (< 2 min asserted), booking, online-approval, phone-customer-area, a11y
make licenses && scripts/agents/check-naming.sh
```

Manual check (final report): `make seed && make dev`. Embed the widget in a local HTML file served from another
port, book from a phone-sized window with the stub or Stripe test mode, approve one booking from the desk,
cancel one from the customer area. Check the response headers of `/b/{slug}` (frame-ancestors) and `/api/*`
(DENY).

## 16. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Online hold TTL stays `PORTO_HOLD_TTL` (default 10 min, m3 D5), even though spec 10.4 says "default 15 min". Changing the default is left to the orchestrator. Checkout extends to 30 min (23.3). |
| D2 | "Category" on the public page = `price_class` (M2 berth category); no class gives `standard`. Availability shows yes/no plus "from" price only. |
| D3 | Quotas apply to channels `online` and `external`. Desk and `api` ignore them (Arenile 7.6). They are enforced by a counter row with `CHECK (used <= cap)` and a conditional update, not by an app check. |
| D4 | Quota claims follow the contract (group and night at booking time). Desk moves do not re-claim or release. Cancellation, expiry, rejection, no-show and stay changes do. |
| D5 | Percent quotas = floor(percent × active member berths). Groups containing dynamic areas accept only count quotas (one unit per allocation). |
| D6 | Card pre-authorisation uses an optional `ext.PaymentAuthorizer`. An authorised intent stays `processing` with `authorized_at` set (no CHECK rewrite of `payment_intents.status`). Capture happens after approval and confirmation comes from the capture webhook. |
| D7 | Without an authorising provider, approval comes first and a 24 h payment link follows. Without any provider, online booking is off unless `allow_without_provider` (bank transfer plus manual driver). |
| D8 | Approval hold default 48 h (max 144 h, inside typical card authorisation validity). At the deadline the booking is rejected automatically (`approval_timeout`). |
| D9 | Required documents are configurable. By default the insurance policy is required before payment, and "documents later" is an option that only asks for them in the customer area. |
| D10 | Customer dedupe by exact normalised email within the marina: the existing customer is reused but never modified by the public form. The submitted contact is kept encrypted on the booking for the office. |
| D11 | Booking token = HMAC of booking id plus stored nonce (never stored, recomputable for idempotent replay). Status token = HMAC with expiry, read-only, for the top-level return page. Portal login token travels in the URL fragment. Session token in a header. No cookies on public surfaces. |
| D12 | Date change: confirmed transits before arrival only. A price increase becomes a supplementary charge paid via pay-balance. A decrease is refunded with the cancellation tier applied to the difference. |
| D13 | Late payment: recovered when the same category is still free and `late_payment_recovery`, otherwise an automatic full refund (online channel). Desk/M5 links keep the M5 credit behaviour. |
| D14 | Channel reservations flagged paid are confirmed with `source = channel` (the channel is merchant of record). INV-09 is amended in ADR 0013. Unpaid ones stay `held` 24 h for the desk. |
| D15 | Channel `modify` events are rejected (`modify_unsupported`) with a worklist item. Channels send cancel plus new. |
| D16 | No new permission: approve/reject/list = `transits.desk`, settings/texts = `marina.configure`, quotas = `harbor.configure`, channels = `providers.configure`, document verification = `customers.write`. |
| D17 | Lost demand = anonymous `inquiries` rows (no IP, no customer), deduped 30 min in memory. Expired unpaid online bookings set `lost` and rejections `declined_other`. |
| D18 | Embedded checkout navigates the top window (hosted checkouts refuse framing). `return_to` accepted only for allowed origins. postMessage only to a parent origin that is both in the allowlist and the referrer. |
| D19 | Public-page budget is measured as entry chunk plus static imports from the Vite manifest (≤ 180 KB, "al primo caricamento"), with a 450 KB guard on total JS. Native date inputs, no calendar library. |
| D20 | Theme = shipped token presets plus "hide header in embed". No arbitrary colours or CSS (no custom CSS rule). |
| D21 | Customer area lists transits of the customer from every channel. Sessions last 8 h idle / 7 days absolute. Login links are single use, 30 min. The confirmation-email link is reusable until departure + 30 days. |
| D22 | `max_live_holds` (default 50 per marina) plus rate limits bound hold flooding. Optional `ext.BotProtection` on create and login. |
| D23 | Public UI ships in it and en only (the repo has no de/fr). Spec 29 de/fr is deferred. |
| D24 | Stripe is exercised in e2e through a local stub (`PORTO_STRIPE_API_BASE`, honoured only with `PORTO_DEV=true`). No network access in tests. |
