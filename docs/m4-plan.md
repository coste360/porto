# M4 — Contrattualistica completa: work order

Status: Plan phase output. This plan is authoritative for the M4 builders. If it conflicts with `docs/spec/`, the
spec wins: stop and report the conflict to the orchestrator instead of guessing. Open decisions of
`docs/spec/33-decisioni-aperte.md` due by M4 are **not** decided here: D5 (InfoCert contract and API) stays open, so
InfoCert exists only as the `ext.SignatureProvider` interface with no implementation; D6 (legal value of the OTP
signature) stays open, so every OTP screen, email and evidence page calls it "firma elettronica semplice" and the docs
say its legal value is for the operator and their adviser to assess. §15 lists every detail this plan decides where
the spec is silent. The state tables of §3 and the invariant rules of §11 are normative: tests are derived from them.

Sources: spec 05, 06 (§6.4–§6.7), 10.4, 11.4–11.6, 12.6, 13 (all), 14 (all), 15 (all), 16.0–16.1 and 16.4 (only to
know what is M5), 21.3 (accruals), 23.3–23.4 (magic-link area, only the acceptance part), 26.1, 27, 28, 29.2, 30,
32 (§32.1, §32.3 M4 and M5+), 33; `docs/invariants.md` (INV-01..11, INV-20, INV-27, INV-30, INV-31, INV-40..42,
INV-60, INV-70..74); ADR 0001, 0002, 0003, 0005, 0008; `docs/m0-plan.md` .. `docs/m3-plan.md`,
`docs/architecture.md`; `.claude/rules/*.md` (`go-backend.md`, `migrations.md`, `api-contract.md`, `charges.md`,
`occupancy.md`, `frontend.md`, `security.md`, `arenile-reference.md`); skills `new-endpoint`, `new-migration`,
`new-domain-event`, `shadcn`.

## 0. Ground rules for every builder

Everything in `docs/m0-plan.md` §0 .. `docs/m3-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001). Closest analogues: `../arenile/internal/domain/renewal`
   (campaigns, one open proposal per source, idempotent sync job, one transaction per proposal, magic link),
   `../arenile/internal/domain/printing` (documents, logo, network printing), `../arenile/internal/domain/notify`
   (templates, dedupe, claim and backoff), `../arenile/internal/domain/waitlist`, `../arenile/docs/adr/0017-scelte-m5b.md`
   and `0022-modelli-email-e-promemoria.md`. Read them to see how a problem was solved; then write Porto's own code
   for contracts with instalments, rights, holders and signatures. Never copy files, code, comments or tests. Never
   modify anything under `../arenile`.
2. shadcn/ui components only through the official CLI, run in `web/shared` (CONTRACT, §13.1). Tailwind on theme
   tokens, no custom CSS, no `style={}`.
3. Do not `git commit`; no destructive git commands; no `rm -rf`. The orchestrator commits.
4. Edit only files you own (§13). To change a file someone else owns, write the request in your final report.
5. Code, identifiers and comments in English. User-facing strings through i18n keys, Italian source, English
   required (documents and emails: `it` and `en` built-in templates). Never log or emit names, emails, phones,
   fiscal codes, IBANs, registration numbers, staff notes, OTP codes, magic-link tokens, document contents.
   Events and SSE carry ids, enums, dates, amounts and counts only (INV-70).
6. **Money is `money.Cents`**, splits use the shared helpers only: `money.Allocate` (largest remainder, INV-42,
   holders' shares), `money.DivRoundHalfEven` / `Cents.MulRat` / `Cents.PercentBP` (half-even), and the M4 pure
   `schedule` package (remainder on the last instalment, INV-31). No floats in domain logic; ratios are `_bp`,
   shares `_millis`. The only float code is the PDF drawing layer `internal/domain/docs/pdf` (fpdf works in float
   millimetres); it never sees money values as numbers, only formatted strings.
7. **Pure packages** (no `store`, `clock`, `time.Now`, `math/rand`, I/O): `internal/domain/schedule`,
   `internal/domain/accrual`, `internal/domain/allot`, `internal/domain/docs/tmpl`, `internal/domain/docs/markdown`,
   `internal/domain/docs/layout`. CONTRACT adds them to depguard `pure-engines` and `pure-engines-no-rand`
   (`accrual` is already listed).
8. **INV-01 stays a database guarantee.** Every allocation change (renewal holds, owner occupancy of rights,
   mandate releases, charter vessel changes, change of vessel) goes through `internal/domain/occupancy` tx
   primitives inside the caller's transaction. No "check then insert".
9. **API first.** CONTRACT writes `api/openapi.yaml` and runs `make gen`. Builders never edit generated files.
10. **INV-30**: every state change of contracts, transits, quotes, renewal proposals and campaigns, addenda and
    instalments goes through `internal/domain/contract` inside `store.RunInTx`, with an `audit_log` row and an
    outbox event in the same transaction. Rights, transfers, mandates, fee runs and common-charge runs follow the
    same rule inside `internal/domain/rights`; templates, documents, signatures and communications inside
    `internal/domain/docs`, `internal/domain/signature`, `internal/domain/comms`. The first line of every exported
    service method is `p.Authorize(ctx, db, marinaID, perm)`; `...Tx` primitives take an authorised
    `store.MarinaScope` and are never called from handlers.
11. Every query scoped by `store.MarinaScope`; foreign ids of another marina are `ErrNotFound` (404). New tables have
    `UNIQUE (id, marina_id)` and composite FKs `(x_id, marina_id)`.
12. Time: `clock.Clock` injected; dates in the marina timezone through `occupancy.Today(clock, tz)`. Tests use
    `clock.Fake`. Jobs take "now" from the clock.
13. **Encryption at rest**: every generated PDF, signed PDF, signature evidence, uploaded scan and customer upload
    is a `stored_files` row (purpose `document`: the 00002 CHECK is frozen, as in M3) written through
    `internal/filestore` (KeyRing AES-GCM, AAD bound to marina and file id). Merge data snapshots, OTP evidence,
    message payloads and provider credentials are KeyRing ciphertexts in `*_enc` columns with purpose-specific AAD.
    Downloads only through the M1 signed-link mechanism (5 minutes, bound to the user) and `customers.read`.
14. **No external services**: PDF in pure Go, email only through the existing transport (SMTP / mail dir / log),
    OTP by email (an `ext.Notifier` for `sms`, if an embedder registers one, is used as an additional channel; none
    ships), no InfoCert code, no AI (ADR 0008), no Coste360 integration, no charges or invoices (M5).

---

## 1. Scope

### 1.1 In M4 (spec 32.3 M4 and the M4 rows of spec 2.2)

**Contracts (13)**
- **Quote → contract automatic procedure (13.3)**, all five steps: availability re-check (M3), contract `draft`
  with the quote's data, price and conditions, **instalment plan from the payment policy**, **document from the
  template** and, when the marina's signature driver is not `paper`, **signature request** with the contract in
  `awaiting_signature`; on signature (OTP, provider) or manual "firmato su carta" the contract becomes `active`,
  allocations become `contract`, and the **first instalment (deposit) is issued** (`issued`, payment request email
  with bank details; the charge itself is M5).
- **Quote sending (13.2)**: email with the PDF from the template and a **magic link** to the customer's acceptance
  page (choice of alternative, missing data, document upload, OTP signature, deposit instructions).
- **States** (§3.1): `awaiting_signature`, `renewed`, `suspended` become reachable; manual suspend/resume (the
  automatic arrears policy is M5).
- **Mooring contracts (13.4)**: **addenda** (service added, parking, billing customer change, agreed price change)
  with effective date, own document, own signature and effect on future instalments; **change of vessel** with
  re-pricing and pro rata adjustment of future instalments; **termination** with refund/penalty computed from the
  cancellation policy and stored as a proposed adjustment (the adjustment charge is M5).
- **Charter contracts (13.6)**: `charter_fleets` of a charter company's registered vessels, contract `kind=charter`
  on a set of berths or a dynamic-area extension for a season, changeover weekday, planned "vessel out" nights,
  price per berth / per vessel / flat, turnaround services as items, rotation of fleet vessels on the contract's
  berths without new contracts and without re-pricing.
- **Instalments and schedule (13.7)**: payment policy (deposit % or amount at signature, instalments at fixed dates
  or intervals, early-payment discount, SEPA flag), pure plan with exact sum (INV-31), issue and due dates, job
  `installments.issue` (status `issued` + payment request email), **scadenzario** with filters, CSV export and bulk
  actions (issue now, send reminder email), **manual settlement** of an instalment (the M4 "manual driver"; M5
  payment matching calls the same entry point).
- **Renewals (13.8)**: simplified renewal from an active contract (next-year contract, same berth/vessel/services,
  new price list price, plan from the policy, allocation held as **prelazione** until the proposal deadline);
  **mass renewal campaigns** (filter, preview of old/new prices, exclusions, sending with magic link, online
  acceptance, OTP signature, deposit, idempotent sync job, one transaction per proposal); **tacit renewal** clause
  (job creates the new active contract at expiry and notifies the customer).
- **Waiting list (13.9)**: entries by size and group ordered by date; on termination/cancellation of a compatible
  annual berth the first entry is proposed to the office (a deadline item); the office sends a quote.
- **Imports (11.6, deferred from M1)**: CSV import with saved column mapping, dry run and per-file transaction of
  customers + vessels, open contracts with residual instalment plan, berth rights with holders, deadlines; undo of a
  run while its records have no later operations. Excel and opening balances are M5 (§1.2).

**Rights, holders and common expenses (14)**
- **Berth rights (14.1)** with holders (shares in millis, Σ = 1000, INV-40), kinds `ownership_share`,
  `long_term_use`, `membership`, title data, documents, default end = concession expiry; **`owner` occupancy** for
  the whole period through the occupancy engine; **authorised vessels** with fit check, one primary vessel carried
  by the owner allocation.
- **Transfers (14.2)**: effective date, declared price (informative), takeover fee from an item, requirement check
  (unsettled fees: block or warn), documents and signature; one transaction closes the old right, opens the new one,
  moves future `owner` occupancy, reassigns or pro-rates future fee instalments, writes `right.transferred`; holder
  chain per berth printable.
- **Management mandates (14.3)**: period, revenue share bp, guaranteed minimum, allowed channels; `owner` nights of
  the period released automatically (berth sellable); revenue of resold nights attributed per mandate (job, accrual
  by nights); periodic statement per holder (PDF + CSV). Compensation with fees and payouts are M5 (§1.2).
- **Management fees (14.4)**: fee tables (per berth category, per m² of berth, per length band, optional ISTAT bp),
  yearly run with simulation (totals per category, diff vs previous year, excluded rights with reason), pro rata for
  rights starting/ending in the year, split among co-holders by share, holder instalment plan (e.g. two semesters).
- **Common expenses (14.5)**: millesimal tables (usable only at Σ = 1000, INV-41), yearly budget by item each on a
  table, **exact allocation** per item × millis / 1000 with largest remainder (INV-42), advance instalments,
  actuals (manual or CSV) and balance per holder (debit or credit), printable statements per holder and per item.
- **Concession (14.6)**: manual concession card, annual fee instalments, obligations with due dates → deadlines;
  its expiry is the default end of rights and multi-year contracts.
- **Accruals (21.3, INV-60)**: pure `accrual.Allocate` (used in M4 by fee pro rata and mandate revenue; reports M7).

**Documents and signature (15)**
- **Print templates (15.1)** for every kind listed in 15.1 that has M4 data (§6.2 table; yard/rental/receipt/
  statement kinds exist in the catalogue with their field lists marked `available_from`), closed extended-Markdown
  language with merge fields, filters, `each`/`if` blocks, tables, header/footer, page break, logo, signature
  fields; documented field catalogue per kind; preview on demo data and on a real record; locales per template;
  versions kept; every generated document records template version and PDF hash.
- **PDF renderer** in pure Go: `github.com/go-pdf/fpdf` (MIT) + own Markdown-subset layout, A4, embedded Go fonts
  (`golang.org/x/image/font/gofont`, BSD-3), **PDF/A-2b** finalisation (XMP, own sRGB ICC output intent, file id)
  for documents to sign, deterministic bytes (same data ⇒ same hash).
- **Archive (15.2)**: `generated_documents` with SHA-256, template version, source record, user; signed documents
  never regenerated (a change makes a new contract version / addendum).
- **Signature (15.4)**: `ext.SignatureProvider` interface; drivers `paper` (print, scan upload, manual
  confirmation) and `otp` (email OTP, evidence page appended by incremental update, original bytes preserved);
  `infocert` = interface only; multiple signers in order or in parallel; reminders and expiry job; provider webhook
  route (dispatches to a registered provider, 404 when none); signed PDF archived; `contract.signed` activates the
  contract.
- **Map print (M3 D25)**: PDF of a map for a date with berth outlines, codes and vessel silhouettes (Go mirror of
  `web/shared/src/map/vessel-shape.ts` with parity vectors).

**Communications and deadlines (11.5, 28)**
- **Transactional emails** of M4 (28.1 subset): quote, signature request, OTP code, contract activated, renewal
  proposal, instalment payment request (bank details, no link), instalment due notice, deadline notices (contract,
  documents, insurance, right end, concession obligations), mandate statement, office notices.
- **Customer communications (28.2)**: mass sends from a filter or a map selection, template or free text with merge
  fields, attachments (uploaded PDF or generated document), preview per recipient, scheduling, consent rules
  (service vs promotional), per-customer message log with delivery status, one-click unsubscribe. Extra channels via
  registered `ext.Notifier`s only.
- **Deadline engine (11.5)**: one engine fed by contract ends, instalments, customer and vessel documents,
  insurance, rights, mandates, concession obligations, renewal proposal deadlines and waiting-list offers;
  per-kind lead days and channels (panel, customer email, office email); idempotent jobs `deadlines.sync` and
  `deadlines.notify`; insurance policy `warn` / `block` (blocks renewals and new contracts while no valid policy).

**Platform**: permissions (§7.2), events and SSE topics (§7.5), problem slugs (§7.4), migrations
`00007`–`00009` both dialects (§5), backup registration, meta capabilities, jobs (§7.6), public magic-link API
(§7.3.6), demo seed (§10), invariants INV-31, INV-40, INV-41, INV-42, INV-60 to `implemented` and the new INV-32..35,
INV-43..44, INV-75 (§11).

### 1.2 Explicitly out of M4

| Out | Milestone | Note for M4 code |
|---|---|---|
| Charges (`charges`, `charge_lines`), payments, refunds, credits, cash, receipts, Stripe/SEPA collection, payment links, `pending_payment`, export "da fatturare", `ChargeExporter` | M5 | Instalments and holder instalments have `charge_id` (null in M4) and statuses; "issued" = due without a charge. Manual settlement (`settlement_source = manual`) is the single entry point M5 payment matching will call with `payment`. Termination refunds, change-of-vessel and transfer pro rata adjustments and common-charge balances are stored as amounts to charge, never as charges. |
| Payment reminders by level (`payments.reminders`), arrears policy → automatic `suspended`, lens "Stato pagamenti", customer dashboard with open balances, statement of account PDF, office worklist with owners and states (28.3) | M5 | M4 has manual suspend/resume, a manual "send reminder" email per instalment and the deadline list; the 28.3 worklist is M5. |
| Mandate revenue compensation with fees, payouts, holder credits | M5 | M4 computes and prints the statement; `mandate_revenues.settled_at` stays null. |
| Excel import/export, opening balances import | M5 | CSV only in M4. |
| ESC/POS receipt printing, receipts | M5/M6 | Template kind `receipt` exists in the catalogue (`available_from: M5`). |
| Desktop edition, offline queue (INV-71) | M6 | Nothing in M4 is allowed offline; no M4 operation is in the offline allowlist. |
| Reports (accrual reports, renewal rate, rights reports) | M7 | `accrual` is pure and tested now. |
| Online booking, public search, checkout, customer area beyond the magic-link pages of §7.3.6 | M8 | The magic-link pages cover quote acceptance, renewal proposals, signing and unsubscribe only. |
| Storage contracts (`kind=storage`), yard and rental documents | M9–M11 | Template kinds exist with `available_from`. |
| InfoCert driver (D5), SMS provider | after D5 / extension | Interface and webhook route only. |
| AI anywhere, `docai`, document extraction (ADR 0008) | never | `docs/architecture.md` §10 still lists `docai` for M4: the orchestrator removes it (ADR 0008 wins). |
| Custom roles | after first version | |

---

## 2. Packages

```
internal/domain/schedule/        PURE  instalment plans: deposit, fixed dates, intervals, rounding unit, early-payment
                                        alternative; Σ = total (INV-31)                                     CONTRACTS
internal/domain/contract/        + instalments service, scadenzario, settlement (manual driver), renewals (simple,
                                        campaigns, proposals, tacit), charter, addenda, change of vessel, termination
                                        amounts, suspension, waiting list, quote email + public acceptance, signature
                                        subject hooks, imports of contracts, jobs                           CONTRACTS
internal/domain/allot/           PURE  millesimal and holder allocation over money.Allocate (INV-42), fee bases,
                                        pro rata, ISTAT                                                    RIGHTS-CHARGES
internal/domain/accrual/         PURE  Allocate(lines, period) per-day quotas (INV-60)                    RIGHTS-CHARGES
internal/domain/rights/          rights, holders, authorised vessels, owner occupancy, transfers, mandates and
                                        revenues, fee tables and runs, millesimal tables, budgets, allocation runs,
                                        holder instalments + settlement, concession, rights imports, jobs  RIGHTS-CHARGES
internal/domain/docs/            templates, versions, field catalogue, compose/render pipeline, generated documents,
                                        signed links, map print                                             DOCUMENTS
internal/domain/docs/tmpl/       PURE  template language: lexer, parser, validator against the catalogue, evaluator
internal/domain/docs/markdown/   PURE  Markdown subset → block AST; HTML writer (emails, escaped)
internal/domain/docs/layout/     PURE  block AST → pages of positioned boxes (integer 1/100 mm), golden text
internal/domain/docs/pdf/        fpdf drawing, fonts, PDF/A-2b finaliser, incremental append (evidence page)
internal/domain/docs/views/      typed merge views per template kind (CONTRACT writes, frozen)
internal/domain/signature/       requests, signers, drivers paper/otp/provider adapter, OTP, evidence, reminders,
                                        webhook dispatch, subject callbacks                                 DOCUMENTS
internal/domain/comms/           mass communications, recipients, consent filter, unsubscribe, message registry DOCUMENTS
internal/domain/magiclink/       customer access links (hash-stored tokens, purpose, expiry, one subject) DOCUMENTS
internal/domain/deadline/        + engine: sources, sync, notify, policies with channels, insurance policy DOCUMENTS
internal/domain/notify/          + M4 templates, attachments, notifier fan-out                          DOCUMENTS
internal/csvimport/              generic CSV reader, mapping, row report, import runs (CONTRACT writes, then DOCUMENTS)
pkg/ext/                         + SignatureProvider and its types, Extensions.SignatureProviders     CONTRACT
```

Import graph (cycles are compile errors): `contract → {occupancy, pricelist, schedule, docs, signature, magiclink,
deadline (read helpers only)}`; `rights → {occupancy, schedule, allot, accrual, docs, signature, pricelist (item price
only)}`; `signature → {docs, magiclink, notify}`; `docs → {docs/*, filestore, notify}`; `comms → {docs, notify,
magiclink, customer}`; `deadline → store` only. **Callbacks go through interfaces registered at wiring**:
`signature.Subject` (implemented by `contract` for `contract`/`addendum`, by `rights` for `right_transfer`) and
`docs.DataSource` (implemented by `contract`, `rights`, `comms`, `deadline` for their template kinds). Neither
`docs` nor `signature` imports `contract` or `rights`. `occupancy` imports none of the M4 packages.

---

## 3. Contracts (normative)

### 3.1 Contract states (mooring and charter; transit table of m3-plan §4.2 unchanged)

Additions to `MooringTransitions` (CONTRACT writes them in `states.go`; a `CharterTransitions` table is identical to
the mooring table except creation from quotes). Every row writes one audit row and the listed events.

| From | Action | To | Effect | Events |
|---|---|---|---|---|
| – | `create` / `create_active` (M3) | `draft` / `active` | M4: plus `PlanInstallmentsTx` from the policy; `create_active` also issues the first instalment | `contract.created` (+ `contract.activated`) |
| `draft` | `request_signature` (`RequestSignature`, or automatic in quote accept / renewal accept when the driver ≠ paper) | `awaiting_signature` | document composed (§6), signature request opened (§6.5), hold expiry extended to the signature deadline | `contract.changed` (`signature_requested`) |
| `draft` | `activate` ("firmato su carta", M3) | `active` | M4: requires a paper signature record when `require_paper_scan` is set (§15 D11); converts holds; issues the first instalment | `contract.signed` + `contract.activated` |
| `awaiting_signature` | `sign` (only from `signature.Subject.OnSignedTx`) | `active` | `ConvertTx(→contract)` (holds lost → re-allocate or the signature completes but the contract goes `draft` with `hold_lost` and 409 is reported to the office, §15 D12); first instalment issued; `signed_at`, `signed_document_id` | `contract.signed` + `contract.activated` |
| `awaiting_signature` | `activate` (paper override by staff, signature request cancelled) | `active` | as `draft → active` | `contract.signed` + `contract.activated` |
| `awaiting_signature` | `signature_failed` (request declined, expired or cancelled) | `draft` | hold expiry back to `draft_hold_days` from now (or the proposal deadline) | `contract.changed` (`signature_failed`) |
| `awaiting_signature` | `cancel` | `cancelled` | signature request cancelled, holds cancelled, instalments `cancelled` | `contract.changed` (`cancelled`) |
| `active` | `suspend(reason)` | `suspended` | none on allocations (the dock is warned, exit never blocked, 16.4) | `contract.changed` (`suspended`) |
| `suspended` | `resume` | `active` | none | `contract.changed` (`resumed`) |
| `suspended` | `terminate` | `terminated` | as `active → terminate` | `contract.terminated` |
| `active` | `terminate(effective_on, reason)` | `terminated` | M3 truncation + M4: future `scheduled` instalments `cancelled`, `termination_amount_cents` from the cancellation policy (§3.6) | `contract.terminated` |
| `active` | `expire` (job at `ends_on`) | `renewed` if a successor (`renewal_proposals.new_contract_id`) is `active`; else `expired` | none (rows are history) | `contract.renewed` / `contract.changed` (`expired`) |
| `active` | `tacit_renew` (job at `ends_on`, policy `renewal.tacit=true`, no open proposal, not excluded) | `renewed` | successor created `active` (same berth/vessel/services, new list price, plan, allocation `contract`), customer notified | `contract.renewed` (+ successor `contract.created` + `contract.activated`) |
| `active`, `suspended`, `draft`, `awaiting_signature` | `update` / `addendum` (§3.5) / `change_vessel` (§3.5) | same | as described | `contract.changed` |

Anything else → `409 invalid_transition`. `StatusRenewed` is assigned only by the expiry job (both rows above).
`contract.signed` is emitted only by `sign` and `activate` (paper). Charter contracts follow the same table.

### 3.2 Quote → contract (13.3), quote sending

- `AcceptQuote` (panel) and `AcceptQuotePublic` (magic link) share `acceptTx`: M3 steps 1–2 unchanged, then in the
  same transaction: step 3 `PlanInstallmentsTx` (policy from the quote, else marina `contract_settings`), step 4
  `docs.ComposeTx(kind = contract_<term>|charter, subject = contract)` (row `pending`, rendered after commit, §6.4)
  and, when `contract_settings.signature_driver ≠ paper` and the request does not say `activate`, the
  `request_signature` transition with signers {customer (+ co-signers of the quote), marina (optional,
  `marina_countersign`)}; step 5 happens on `sign`/`activate`.
- `activate: true` (panel only, contracts.write) keeps the M3 "signed on paper" shortcut.
- **Send** (`POST quotes/{id}/send`, replaces the M3 manual mark; the M3 manual mark stays as `?manual=true`):
  composes the quote PDF (kind `quote`), creates a magic link (purpose `quote`, expiry = quote `expires_at`), queues
  the email (template `quote`, PDF attached) to the customer's first email; no email ⇒ `422 customer_email_missing`
  unless `manual=true`. Re-sending rotates the link (the previous one stops working).
- **Public acceptance** (§7.3.6): the page shows options and prices (no other customers' data), lets the customer
  fill missing data (address, vessel registration, insurance expiry; fields listed in `contract_settings.required_fields`),
  upload documents (insurance policy, identity: M1 `documents`, encrypted), accept an option; the contract is
  created as above and the response carries the signing link; deposit instructions are shown (bank details from
  `contract_settings.bank_details`); deposit settlement is done by the office (manual driver, §3.3).

### 3.3 Instalments (13.7, INV-31, INV-32)

- **Policy JSON** (`contracts.policy`, version 2; v1 rows are read as v2 with defaults; unknown keys rejected):
  ```json
  {"version":2,"reprice_on_move":false,"release_policy":"with_consent","allow_segments":true,
   "payment":{"deposit":{"bp":3000}|{"cents":50000}|null,"deposit_due_days":7,
              "schedule":{"mode":"fixed_dates","dates":[{"due_on":"MM-DD"|"YYYY-MM-DD","bp":3500}]}
                        |{"mode":"interval","every_months":3,"count":4,"first_due_offset_months":1}
                        |{"mode":"single","due_offset_days":30},
              "issue_days_before_due":15,"rounding_unit_cents":1,
              "early_payment":{"discount_bp":300,"pay_by_offset_days":15}|null,"sepa":false},
   "renewal":{"tacit":false,"notice_days":60},
   "cancellation":{"notice_days":30,"refund":"pro_rata"|"none","penalty_bp":0}}
  ```
  Defaults per marina in `contract_settings.default_policy`; per price list contract type overrides are out (§15 D3).
- **Pure plan** `schedule.Plan(in schedule.Input) ([]schedule.Item, error)`; `Input{Total money.Cents, StartsOn,
  EndsOn, Today calendar.Date, Payment PaymentPolicy}`; `Item{Seq, Kind (deposit|installment|single), IssueOn,
  DueOn, Amount}`. Rules: deposit = `bp` share (half-even) or fixed cents (≤ total), due `max(today, starts−?)` =
  `today + deposit_due_days`, issued at signature; the rest is split in `n` parts, each `floor(rest·w_i/Σw / unit)·unit`
  (equal weights for `interval`), **the remainder goes to the last instalment** (INV-31: Σ = total, every amount ≥ 0,
  last ≥ 0); fixed `MM-DD` dates resolve inside `[starts_on, ends_on)` (year roll-over), dates before `today` are
  merged into the first due item; `issue_on = due_on − issue_days_before_due` clamped to `today`. Total 0 ⇒ no
  instalments. Negative totals are rejected (adjustments are M5).
- **Early payment**: the plan exposes an alternative `single` plan with the discount applied; choosing it (panel or
  public acceptance) adds a snapshot adjustment `early_payment` (INV-27 holds: total = breakdown + adjustments) and
  replaces the plan in the same transaction.
- **Statuses**: `scheduled → issued → paid`; `scheduled|issued → cancelled` (termination, replan); `partially_paid`
  in the CHECK only (M5). "Overdue" is computed (`issued` and `due_on < today`); job `installments.issue` marks
  `overdue_notified_at` once and emits `installment.overdue`.
- **Issue**: job `installments.issue` (every 10 min, idempotent: `UPDATE … SET status='issued' WHERE status='scheduled'
  AND issue_on <= today`, batch 500, one tx per marina per batch) queues the payment request email (dedupe key
  `installment:<id>:issued`); bulk "issue now" uses the same `IssueTx`.
- **Settlement (manual driver, INV-32)**: `SettleInstallment(id, {method: bank_transfer|cash|card_pos|cheque|other,
  reference?, settled_on})` (permission `payments.collect`) is the **only** code path that sets `paid`; conditional
  `UPDATE … WHERE status IN ('issued','scheduled') AND version = ?`; stores `settlement_source='manual'`,
  `settled_at`, `settled_by_user_id`, `settlement_ref_enc`. Bulk settle takes up to 500 ids, one tx, all or nothing.
  Settling a deposit of a renewal contract completes its proposal (§3.4) in the same tx.
- **Replan**: addenda, change of vessel and early payment replace the `scheduled` (not yet issued) instalments with
  a new plan of the remaining total (`total − Σ issued/paid`); history stays (`cancelled` rows with `replaced_by_seq`).
  INV-31 is checked on the live set (`status <> 'cancelled'`).
- **Scadenzario**: `GET installments` (filters period, status incl. `overdue`, customer, contract, group), CSV export
  (Italian: `;`, decimal comma), bulk actions issue now / send reminder.

### 3.4 Renewals (13.8)

**Simple renewal** `RenewContract(id, {starts_on?, price_list_id?, deadline?, send?: bool})` (contracts.write): one
open proposal per source contract (`renewal_proposals_open_uidx` partial unique on `source_contract_id` WHERE status
IN open states) — a second call gets `409 renewal_exists`. Creates, in one tx: the successor contract `draft`
(period = next term from `ends_on`, D27 of M3 for defaults; same berth/area placement, vessel, services, policy),
price from `pricelist.Service.Quote` for the new period (run before the tx, M3 D11), instalment plan, allocation
`hold` with `expires_at = deadline end` (prelazione), and the proposal `proposed` (or `sent` when `send`). A hold
conflict ⇒ `409 resource_taken` and nothing is written (the campaign records it as exclusion `berth_unavailable`).

**Campaigns**: `renewal_campaigns` (name, filter json, target period, price list, deadline, status, counts).

| Campaign state | Action | To |
|---|---|---|
| – | `create(filter, deadline, price_list_id?, message?)` | `draft` |
| `draft` | `preview` (recomputes, idempotent) | `draft` (preview rows = proposals `excluded`/`previewed`) |
| `draft` | `launch` | `sending` (job sends) → `open` when every proposal is `sent`/`excluded`/`failed` |
| `open` | `close` (manual) or deadline passed (job) | `closed` (open proposals → `expired`) |
| `draft` | `discard` | `discarded` (preview proposals deleted with their holds) |

Filter: `{groups?, structures?, terms?: [annual,seasonal,…], ends_from?, ends_to?, customer_categories?,
contract_ids?}` over `active` contracts. Exclusions with reason (preview shows them, staff may include manually
with a recorded reason except `berth_unavailable`): `arrears` (an `issued` instalment overdue > `arrears_days` or a
holder instalment overdue), `insurance_expired` (no valid insurance document for the vessel on the new start date,
11.5), `already_renewed` (open proposal or successor), `no_price` (no active list for the new period),
`berth_unavailable` (hold conflict), `manual`.

Preview creates the proposals and their successor drafts with holds (status `previewed`) so the prelazione starts at
preview; `launch` flips them to `proposed` and the job sends them. Each proposal is processed in **its own
transaction** (never one tx for the campaign).

| Proposal state | Action | To | Effect |
|---|---|---|---|
| – | preview / simple renew | `previewed` / `proposed` / `excluded(reason)` | successor `draft` + hold (not for `excluded`) |
| `previewed` | campaign `launch` | `proposed` | – |
| `proposed` | job send (or panel resend) | `sent` | proposal PDF (kind `renewal_proposal`), magic link (purpose `renewal`, expiry = deadline end), email, `sent_at` |
| `sent` | customer or office `accept({plan: standard|early_payment})` | `accepted` | `request_signature` on the successor (driver ≠ paper) or, with paper, waits for "firmato su carta" |
| `accepted` | successor `sign` / `activate` | `signed` (deposit > 0) or `completed` (deposit 0) | first instalment issued |
| `signed` | deposit instalment settled (§3.3) | `completed` | – |
| `proposed`, `sent` | `decline(reason?)` (customer or office) | `declined` | successor cancelled, hold released |
| `previewed`, `proposed`, `sent`, `accepted` | deadline passed (job) or campaign closed | `expired` | successor `cancelled` unless `active`; holds released; signature request cancelled |
| `excluded` | `include(reason)` (office, not for `berth_unavailable`) | `previewed`/`proposed` | successor + hold created now (409 if the berth is taken) |

Job `renewals.sync` (every 1 min, `RunAtStart`, idempotent, one tx per proposal, batch 200): sends due proposals of
`sending` campaigns (dedupe `renewal:<proposal_id>:sent:<n>`), expires past-deadline proposals, reconciles
`accepted → signed/completed` from the successor state and deposit (recovery path; the main path is in the same tx
as `sign`/settle), closes campaigns past deadline. The successor's activation never depends on the job.

**Tacit renewal**: §3.1 row; excluded contracts (same reasons) are not renewed tacitly and get a deadline
`tacit_renewal_blocked` for the office.

### 3.5 Addenda, change of vessel, charter

- `contract_addenda` (seq per contract, kind `service_add|service_remove|parking|billing_customer|price_change|vessel_change`,
  effective_on, payload json, amount_delta_cents, status `draft → awaiting_signature → active | cancelled`). The
  addendum has its own document (kind `addendum`) and signature (subject `addendum`); on activation its effect is
  applied in the same tx: lines added/closed (land resources allocate/truncate through occupancy), billing customer
  changed, `price_snapshot.adjustments += {kind: "addendum", addendum_id, old_cents, new_cents}` (INV-27), future
  instalments replanned (§3.3). The original contract, its document and signatures never change.
- **Change of vessel** = addendum `vessel_change`: fit check of the new vessel on each allocation from
  `effective_on` (422 `fit_blocked` unless overridden), pricing on the new vessel for the remaining nights before the
  tx, pro rata delta = `new_remaining − old_remaining` as adjustment, allocations: `TruncateTx(effective_on)` + new
  `AllocateTx(kind=contract, same placement, new vessel, same contract line)` (no `moved_from_id`: INV-11 requires
  the same vessel), `contracts.vessel_id` updated.
- **Termination amounts**: `termination_amount_cents` = refund (negative) or penalty (positive) from
  `policy.cancellation` (`pro_rata`: unused nights' share of the paid instalments via `accrual`; `penalty_bp` of the
  remaining total), stored on the contract and in the audit diff; no charge (M5).
- **Charter**: `charter_fleets` (customer = charter company, name) + `charter_fleet_vessels` (vessel, active);
  `charter_contracts` (contract_id PK, fleet_id, changeover_weekday 1..7, pricing_mode `per_berth|per_vessel|flat`,
  out_nights json `[{from,to,berth_id?}]` informative). Creation `CreateCharter` (contracts.write): placements on the
  contract's berths/area extents, each resource line allocated `kind=contract` with an initial fleet vessel (fit
  checked); `contracts.vessel_id` = the lead vessel (§15 D7). Pricing: `per_berth` = Σ quote of each berth for the
  season, `per_vessel` = Σ quote of each fleet vessel on its initial berth, `flat` = staff amount (snapshot kind
  `manual` with user and reason). **Rotation** `RotateCharter(contract, from_night, assignments[{line_id, vessel_id}])`:
  vessels of the fleet only; swaps between lines use `occupancy.SwapTx`-equivalent moves inside the contract, a new
  vessel on a line uses Truncate + Allocate (as change of vessel) — never repriced, never a new contract.
  Turnaround services are service lines with items (M2).

### 3.6 Waiting list (13.9)

`waitlist_entries` (customer, vessel or dims `loa_cm/beam_cm/draft_cm`, term, groups json, requested_from,
priority, status `waiting|offered|converted|withdrawn`, offered_quote_id). An outbox sink in `contract`
(`contract.terminated` and `contract.changed{cancelled}` of annual mooring contracts) finds the first `waiting` entry
whose dimensions fit the freed berth (`fit.Check`) and creates a deadline `waitlist_offer` for the office (no hold,
no email to the customer); "send quote" from the entry creates a quote prefilled and marks `offered`.

### 3.7 Contract imports (11.6)

`POST imports` (CONTRACT generic endpoint) with `entity` ∈ `customers_vessels` (DOCUMENTS), `contracts` (CONTRACTS),
`rights` (RIGHTS-CHARGES), `deadlines` (DOCUMENTS); `dry_run` first; ≤ 5 000 rows; one tx per file; report rows
valid/rejected/suspected duplicates (fiscal code, VAT, registration). Contracts import: customer/vessel by code or
fiscal code, berth code, term, period, total, residual plan rows (due_on, amount, status scheduled|issued|paid);
allocations through the normal engine (conflicts are report errors, never overlaps); Σ residual plan = total − paid
(INV-31). Undo: `POST imports/{runId}/undo` while every created record still has version 1 and no dependent rows
(else `409 import_not_undoable`).

---

## 4. Rights, mandates, fees, common expenses (normative, RIGHTS-CHARGES)

### 4.1 Rights and owner occupancy

- `berth_rights` (berth_id, kind, from_on, to_on (inclusive last night; default = concession expiry − 1 day, else
  required), title json `{deed, number, notary, date}`, status `active|transferred|ended|cancelled`, version);
  `berth_right_holders` (right_id, customer_id, share_millis 1..1000) — Σ = 1000 checked in the tx of every write
  (INV-40, `TestRight_HolderSharesSumTo1000`); `berth_right_vessels` (right_id, vessel_id, primary bool, fit json,
  fit_override json) unique (right_id, vessel_id), partial unique primary per right.
- **Owner occupancy**: one `owner` allocation chain per right linked by `berth_right_allocations (right_id,
  allocation_id)`. `occupancy.AllocateOwnerTx` (new, RIGHTS-CHARGES, §13.2) writes kind `owner`, no expiry, berth
  only (rights on dynamic areas are out, §15 D15), `vessel_id` = the primary authorised vessel or NULL (the
  existing `allocations_block_check` allows NULL with no block reason), fit checked only when a vessel is set.
  Nights from `max(from_on, today)`; past nights of imported rights are not written. Conflict ⇒ `409
  resource_taken`, nothing written. Changing the primary vessel = Truncate + AllocateOwner from a night (no
  `moved_from_id`).
- Ending, cancelling or transferring a right truncates/cancels its owner allocations in the same tx.
- Rights on a berth: at most one active right per night is guaranteed by the occupancy rows (14.1), not by a check.

### 4.2 Transfers

`right_transfers` (right_id, effective_on, to_holders json `[{customer_id, share_millis}]`, declared_price_cents,
takeover_item_id, takeover_fee_cents (item price from the active catalogue, M2), requirement_policy snapshot
`block|warn`, requirement_issues json, fees_policy `reassign|prorata`, status `draft → awaiting_signature →
completed | cancelled`, new_right_id, document ids). Requirement check: unsettled overdue holder instalments of the
outgoing holders ⇒ `422 transfer_requirements` (block) or warning stored (warn). Completion (on signature of all
signers — outgoing holders, incoming holders, marina — or "firmato su carta"): one tx closes the right
(`to_on = effective_on − 1`, status `transferred`), creates the new right from `effective_on` to the old `to_on`,
truncates owner occupancy at `effective_on` and allocates the new chain, reassigns future `scheduled` fee
instalments (`reassign`: same amounts, new holders split by shares with `money.Allocate`; `prorata`: the year's
fee is split by days with `accrual` between old and new holders and replanned), writes `right.transferred`.
Holder chain: `GET berths/{berthId}/right-history` + PDF (kind `holder_chain`).

### 4.3 Mandates

`management_mandates` (right_id, from_on, to_on, owner_share_bp 0..10000, min_guaranteed_cents, channels json
`["transit","period"]`, settlement `credit|transfer` (informative in M4), status `active|ended|cancelled`).
Creation (rights.manage): only nights ≥ today; the owner allocation is split so the mandate nights have **no**
`owner` rows (Truncate at `from_on` + AllocateOwner from `to_on + 1` when the right continues); overlapping mandates
on a right ⇒ `409 mandate_overlap` (exclusion constraint emulated by a per-right lock row: `berth_rights.version`
bump in the same tx). End early / cancel: re-allocate owner rows on the nights still free (nights already sold stay
sold; reported back). **Revenues**: job `mandates.accrue` (daily + on demand; idempotent upsert keyed
`(mandate_id, allocation_id)`) finds allocations (`transit`, `contract`) on the berth inside the mandate period,
takes each contract's `total_cents` and spreads it by night with `accrual.Allocate`, sums the nights inside the
mandate → `revenue_cents`, owner share = `PercentBP(owner_share_bp)` → `mandate_revenues`; emits
`mandate.revenue_accrued` when a row changes. Statement `GET mandates/{id}/statement?from&to` (JSON, CSV, PDF kind
`mandate_statement`) with the guaranteed minimum shown as `max(0, min − Σ share)` top-up line.

### 4.4 Management fees

- `management_fee_tables` (year, name, basis `per_category|per_m2_berth|per_length_band`, istat_bp, status
  `draft|active`), `management_fee_table_rows` (table_id, category_id? | band `{from_cm,to_cm}`? , amount_cents or
  rate_cents_per_m2).
- Amount of a right for the year (pure `allot.FeeAmount`): base by basis (`per_m2_berth` = `length_cm × width_cm`
  cm² × rate / 10 000 half-even; bands on `berths.length_cm`), × `(10 000 + istat_bp) / 10 000` half-even, × pro
  rata `days_in_year_covered / days_in_year` half-even; split among holders with `money.Allocate(shares)`.
- `management_fee_runs` (year, table_id, status `draft → simulated → issued | discarded`, plan policy json (same
  schema as `policy.payment`), totals json); `management_fee_lines` (run_id, right_id, berth_id, category, base_cents,
  days, amount_cents, previous_cents, excluded bool, exclusion_reason). Simulation is the `simulated` run (re-run =
  replace lines). **Issue** creates `holder_installments` per holder line from the plan (`schedule.Plan`); one issued
  run per (marina, year, table): partial unique index → concurrent issues: one wins (`409 conflict`).

### 4.5 Millesimal tables, budgets and allocation (INV-41, INV-42)

- `millesimal_tables` (name, code, status `draft|active|archived`), `millesimal_entries` (table_id, berth_id,
  millis ≥ 0) unique (table_id, berth_id). Activation requires Σ millis = 1000 (`422 table_incomplete`, INV-41) and is
  re-checked in every allocation tx (a later edit of an active table is refused: active tables are immutable; edit =
  new version table). CSV import of entries (berth code; millis).
- `common_charge_budgets` (year, status `draft → approved → closed`), `common_charge_items` (budget_id, name,
  table_id, budget_cents, actual_cents null, sort).
- **Runs** `common_charge_runs` (budget_id, kind `advance|balance`, seq, share_bp (advance share of the budget),
  status `draft → issued | discarded`), `common_charge_allocations` (run_id, item_id, berth_id, right_id null,
  customer_id null, berth_share_cents, amount_cents). Algorithm (pure `allot.Common`): for each item amount
  `A_i` (advance: `A_i × share_bp/10 000` half-even per item; balance: `actual_i − Σ advances_i`), split over the
  table's berths with `money.Allocate(A_i, millis)` (largest remainder, Σ = `A_i` exactly); each berth share is
  split over the holders of the right active on the reference date (`budget year-01-01` for advances, `12-31` for
  balance; §15 D17) with `money.Allocate(share, share_millis)`; a berth without an active right has
  `customer_id = NULL` (marina's own share), so **Σ per item = item amount and Σ run = Σ items, exactly** (INV-42).
  Issue creates holder instalments (advance: one per run; balance: one per holder, negative = credit kind).
- Statements: per holder (PDF kind `common_charges_holder`) and per item (`common_charges_items`), CSV.

### 4.6 Holder instalments and settlement

`holder_installments` (source_type `management_fee|common_advance|common_balance|transfer_fee` — concession fees
are the marina's own cost and appear only as deadlines; source_id, run_id, right_id, customer_id, seq, issue_on,
due_on, amount_cents (negative allowed only for `common_balance` credits), status like §3.3, `charge_id` null,
settlement columns). Same issue job (`installments.issue` covers both tables), same manual settlement rule
(`SettleHolderInstallment`, permission `payments.collect`, INV-32). Σ of the instalments of a holder line = its
amount (INV-31).

### 4.7 Concession

`concessions` (authority, deed_kind, deed_number, granted_on, expires_on, annual_fee_cents, surfaces json,
prescriptions text, status), `concession_obligations` (concession_id, kind `fee_instalment|deposit|insurance|periodic_report|other`,
due_on, recurrence `none|yearly`, amount_cents?, done_on). Updating `expires_on` also updates
`marinas.concession_expires_on` in the same tx (default end of new rights). Permission `marina.configure` to write,
`rights.manage` to read.

---

## 5. Data model (CONTRACT; both dialects, same names; never edit 00001–00006)

Conventions of m3-plan §5: dates `TEXT`/`DATE`, instants `TIMESTAMP`/`timestamptz`, JSON `TEXT`/`jsonb`, ids
`TEXT`/`UUID` as in 00006, booleans `INTEGER CHECK IN (0,1)`/`boolean`. Every table: `id`, `marina_id` (FK marinas),
`created_at`, `updated_at`, `version` where mutable, `UNIQUE (id, marina_id)`, composite FKs, an index starting with
`marina_id`. Additions to existing tables only through `ALTER TABLE … ADD COLUMN` without FK (SQLite cannot add
composite FKs); relations are new tables.

### 5.1 `00007_contracts_m4.sql`

| Table / change | Columns | Constraints |
|---|---|---|
| ALTER `contracts` | `signed_at` ts null, `signed_document_id` text null, `signature_request_id` text null, `suspended_at` ts null, `suspend_reason` text null, `renewed_at` ts null, `termination_amount_cents` bigint null, `installment_plan_version` int default 0, `charter` bool default 0 | – (kinds/status CHECK already allow M4 values) |
| ALTER `quotes` | `link_rotated_at` ts null, `sent_count` int default 0, `accepted_via` text null (`panel|link`) | – |
| ALTER `deadline_policies` | `channels` text default `'["panel"]'` (`panel`, `customer_email`, `office_email`) | – |
| `contract_settings` | marina_id PK, `default_policy` json, `signature_driver` CHECK (paper, otp, provider), `provider_code` null, `marina_countersign` bool, `require_paper_scan` bool, `signature_days` (14, 1..90), `signature_reminder_days` (3), `renewal_deadline_days` (30), `arrears_days` (30), `insurance_policy` CHECK (warn, block), `required_fields` json, `bank_details_enc` (IBAN, holder, bank; KeyRing AAD `bank-details:<marina>`), `office_email_enc`, `version` | lazily created |
| `installments` | `contract_id`, `seq`, `kind` CHECK (deposit, installment, single), `issue_on`, `due_on`, `amount_cents` (≥ 0), `status` CHECK (scheduled, issued, partially_paid, paid, cancelled), `issued_at`, `overdue_notified_at`, `charge_id` null (M5), `settlement_source` null CHECK (manual, payment), `settlement_method` null, `settlement_ref_enc`, `settled_on`, `settled_at`, `settled_by_user_id`, `replaced_by_plan` int null, `plan_version` int | unique (contract_id, plan_version, seq); idx (marina_id, status, due_on); idx (marina_id, issue_on) WHERE status='scheduled'; CHECK paid ⇒ settlement_source NOT NULL |
| `renewal_campaigns` | `name`, `filter` json, `price_list_id` null, `target_starts_rule` CHECK (next_term, fixed), `target_starts_on` null, `deadline_on`, `status` CHECK (draft, sending, open, closed, discarded), `message` text, counts json, `launched_at`, `closed_at`, `created_by_user_id` | idx (marina_id, status) |
| `renewal_proposals` | `campaign_id` null, `source_contract_id`, `new_contract_id` null, `status` CHECK (previewed, proposed, sent, accepted, signed, completed, declined, expired, excluded), `exclusion_reason` null, `included_reason` null, `old_total_cents`, `new_total_cents` null, `deadline_on`, `sent_at`, `sent_count`, `accepted_at`, `accepted_via`, `decided_at`, `decline_reason`, `link_id` null | **`renewal_proposals_open_uidx` unique (source_contract_id) WHERE status IN ('previewed','proposed','sent','accepted','signed')**; unique (campaign_id, source_contract_id); idx (marina_id, campaign_id, status); idx (marina_id, status, deadline_on) |
| `contract_addenda` | `contract_id`, `seq`, `kind` CHECK (service_add, service_remove, parking, billing_customer, price_change, vessel_change), `effective_on`, `payload` json, `amount_delta_cents`, `status` CHECK (draft, awaiting_signature, active, cancelled), `document_id` null, `signature_request_id` null, `activated_at` | unique (contract_id, seq) |
| `charter_fleets`, `charter_fleet_vessels` | fleet: `customer_id`, `name`; vessels: `fleet_id`, `vessel_id`, `active` | unique (fleet_id, vessel_id); unique (vessel_id) WHERE active (a vessel in one active fleet) |
| `charter_contracts` | `contract_id` PK, `fleet_id`, `changeover_weekday` 1..7, `pricing_mode` CHECK, `out_nights` json | FK contract cascade |
| `waitlist_entries` | `customer_id`, `vessel_id` null, `loa_cm`, `beam_cm`, `draft_cm` null, `term`, `groups` json, `requested_from`, `priority` int, `status` CHECK (waiting, offered, converted, withdrawn), `offered_quote_id` null, `notes` | idx (marina_id, status, created_at) |
| `number_sequences` | (no change: `charter` series already allowed) | – |

### 5.2 `00008_rights.sql`

Tables of §4: `berth_rights` (+ CHECK to_on ≥ from_on, kind/status CHECKs, idx (marina_id, berth_id, from_on)),
`berth_right_holders` (unique (right_id, customer_id), CHECK share 1..1000), `berth_right_vessels`,
`berth_right_allocations` (PK (allocation_id), FK right cascade, FK allocation (allocation_id, marina_id) RESTRICT),
`right_transfers`, `management_mandates` (CHECK to_on ≥ from_on, share 0..10000), `mandate_revenues` (unique
(mandate_id, allocation_id)), `management_fee_tables`, `management_fee_table_rows`, `management_fee_runs`
(**`management_fee_runs_issued_uidx` unique (marina_id, year, table_id) WHERE status='issued'**),
`management_fee_lines` (unique (run_id, right_id)), `millesimal_tables` (unique (marina_id, code)),
`millesimal_entries` (unique (table_id, berth_id), CHECK millis ≥ 0), `common_charge_budgets` (unique (marina_id, year)),
`common_charge_items`, `common_charge_runs` (**unique (budget_id, kind, seq)**; **unique (budget_id) WHERE
kind='balance' AND status='issued'**), `common_charge_allocations` (idx (run_id, item_id), idx (marina_id,
customer_id)), `holder_installments` (unique (source_type, source_id, customer_id, seq); same status/settlement
columns as `installments`), `concessions`, `concession_obligations`. Triggers on both dialects: `millesimal_entries`
of an `active` table are immutable (INV-41 at the DB level for edits; Σ checked in Go at activation),
`common_charge_allocations` of an `issued` run are immutable.

### 5.3 `00009_documents.sql`

| Table | Columns | Constraints |
|---|---|---|
| `templates` | `kind` (catalogue kind, validated in Go), `code`, `name`, `is_default` bool, `archived_at` | unique (marina_id, code); idx (marina_id, kind) |
| `template_versions` | `template_id`, `version`, `locale`, `body_md`, `header_md`, `footer_md`, `page` json (`{margins_mm, font_size_pt}`), `status` CHECK (draft, published, superseded), `sha256`, `published_at`, `published_by_user_id` | unique (template_id, version, locale); trigger: published/superseded rows immutable except `status` published→superseded |
| `generated_documents` | `kind`, `template_version_id` null (built-in default: null + `builtin_ref`), `builtin_ref` null, `locale`, `subject_type` CHECK (quote, contract, addendum, renewal_proposal, right_transfer, holder_chain, mandate_statement, fee_notice, common_charges_holder, common_charges_items, installment_notice, communication, map_print, letter), `subject_id`, `status` CHECK (pending, rendering, ready, failed, superseded), `data_enc` (KeyRing AAD `docdata:<id>`), `file_id` null (FK stored_files), `sha256` null, `pages`, `pdfa` bool, `signed` bool, `signed_from_id` null (self FK: signed PDF = new row pointing to the unsigned one), `lease_until`, `error_code`, `created_by_user_id` | idx (marina_id, subject_type, subject_id); idx (status, lease_until) WHERE status IN ('pending','rendering'); trigger: `sha256`, `file_id` immutable once set; rows with `signed = 1` immutable |
| `signature_requests` | `subject_type` CHECK (contract, addendum, right_transfer), `subject_id`, `document_id`, `driver` CHECK (paper, otp, provider), `provider_code` null, `envelope_ref` null, `order_mode` CHECK (sequential, parallel), `status` CHECK (pending_document, sent, partially_signed, signed, declined, expired, cancelled), `expires_at`, `last_reminder_at`, `signed_document_id` null, `completed_at` | **unique (subject_type, subject_id) WHERE status IN ('pending_document','sent','partially_signed')** (one open request per subject); idx (marina_id, status, expires_at) |
| `signature_signers` | `request_id`, `seq`, `role` CHECK (customer, co_holder, outgoing_holder, incoming_holder, legal_representative, marina), `customer_id` null, `user_id` null, `display_name_enc`, `email_enc`, `email_hash`, `status` CHECK (waiting, invited, signed, declined), `link_id` null, `signed_at`, `evidence_enc`, `paper_scan_file_id` null | unique (request_id, seq) |
| `signature_otps` | `signer_id`, `code_hmac`, `expires_at`, `attempts` (0..5), `consumed_at`, `sent_at` | idx (signer_id, created_at); conditional UPDATE `attempts = attempts + 1 WHERE attempts < 5` |
| `access_links` | `purpose` CHECK (quote, renewal, signature, unsubscribe), `subject_type`, `subject_id`, `customer_id` null, `token_hash` (SHA-256 of the token), `expires_at`, `revoked_at`, `last_used_at`, `use_count` | unique (token_hash); idx (marina_id, subject_type, subject_id) |
| `comm_campaigns` | `name`, `category` CHECK (service, promotional), `audience` json (filter or explicit customer ids / map selection), `template_id` null, `subject`, `body_md`, `attachments` json (`[{document_id}|{file_id}]`), `scheduled_at` null, `status` CHECK (draft, scheduled, sending, sent, cancelled), counts json, `created_by_user_id` | idx (marina_id, status, scheduled_at) |
| `comm_recipients` | `campaign_id`, `customer_id`, `status` CHECK (pending, queued, skipped_no_consent, skipped_no_email, failed), `message_id` null | unique (campaign_id, customer_id) |
| `customer_messages` | `customer_id`, `message_id` (FK message_log), `category` CHECK (service, promotional, transactional), `source_type`, `source_id`, `template` | unique (message_id); idx (marina_id, customer_id, created_at) |
| `signature_settings` (provider credentials) | marina_id PK, `provider_code`, `credentials_enc`, `webhook_secret_enc`, `version` | – |
| `import_runs`, `import_run_items` | run: `entity` CHECK (customers_vessels, contracts, rights, deadlines), `status` CHECK (dry_run, applied, undone, failed), `report` json (no PII: row numbers and codes), `file_sha256`; items: `run_id`, `entity_type`, `entity_id` | idx (marina_id, created_at) |

`message_log.channel` CHECK stays `email`; notifier fan-out (SMS) is not logged in `message_log` (§15 D22).

### 5.4 Backup, models, encryption

- `store.BackupTables()` FK order after M3: `contract_settings`, `charter_fleets`, `charter_fleet_vessels`,
  `charter_contracts`, `installments`, `renewal_campaigns`, `renewal_proposals`, `contract_addenda`,
  `waitlist_entries`, `concessions`, `concession_obligations`, `berth_rights`, `berth_right_holders`,
  `berth_right_vessels`, `berth_right_allocations`, `right_transfers`, `management_mandates`, `mandate_revenues`,
  `management_fee_tables`, `management_fee_table_rows`, `management_fee_runs`, `management_fee_lines`,
  `millesimal_tables`, `millesimal_entries`, `common_charge_budgets`, `common_charge_items`, `common_charge_runs`,
  `common_charge_allocations`, `holder_installments`, `templates`, `template_versions`, `generated_documents`,
  `signature_requests`, `signature_signers`, `access_links`, `comm_campaigns`, `comm_recipients`,
  `customer_messages`, `signature_settings`, `import_runs`, `import_run_items`. Excluded (`BackupExcludedTables`):
  `signature_otps` (10-minute codes). Forward references (`contracts.signed_document_id`,
  `renewal_proposals.new_contract_id`, `generated_documents.signed_from_id`) are plain columns or deferred FKs; the
  restore loads triggers-protected tables through `lateColumns` as M2 did for price lists (immutable rows inserted
  as draft and flipped). Encrypted blobs travel as they are (still encrypted) in `files/`.
- Store model files (CONTRACT writes the structs for every column; owners may add methods):
  `internal/store/contract_m4_models.go`, `rights_models.go`, `docs_models.go`.
- Migration tests: `TestMigrations_UpDownUp`, `…SchemaParity`, `…SameNumbering` cover 00007–00009; new
  `TestMigrations_M4Constraints` asserts on both dialects: the open-proposal partial unique, the open signature
  request partial unique, the issued fee run and balance run uniques, the immutability triggers, the paid ⇒
  settlement CHECK.

---

## 6. Documents, templates, PDF, signature, communications, deadlines (normative, DOCUMENTS)

### 6.1 Template language (`docs/tmpl`, pure, closed)

- Tokens: `{{path}}`, `{{path | filter arg?}}` (filters: `meters` (cm → "11,34 m"), `cm`, `money` (cents →
  "1.234,56 €" by locale), `date` (`long|short`), `upper`, `lower`, `default "text"`, `bp` ("30 %"), `millis`
  ("333,334 ‰"), `count`), `{{#each path}}…{{/each}}` (with `this.x`, `@index`, `@first`, `@last`, nesting ≤ 4),
  `{{#if expr}}…{{else}}…{{/if}}` with `expr` = `path`, `!path`, `path == "lit"`, `path != "lit"`, `path > n`,
  `path < n` (ints only), `{{signature "role"}}`, `{{pagebreak}}`, `{{logo}}`, `{{page}}`/`{{pages}}` (header/footer
  only), `{{today}}`.
- Paths are validated at save/publish time against the **field catalogue** of the template kind (`docs.Fields(kind)`
  derived by reflection from the typed view structs in `docs/views` with `doc:"name,description_key"` tags);
  unknown path, wrong filter for the type, `each` on a non-list ⇒ `422 template_invalid` with `details:
  [{line, col, code, path?}]`. No function calls, no includes, no file or network access, output size ≤ 2 MiB,
  loop iterations ≤ 10 000 (`template_limits`).
- Evaluation first produces Markdown text where every inserted value is **escaped** (Markdown control characters
  backslash-escaped, newlines in values become spaces except for `notes`-type fields which become hard line
  breaks), then `docs/markdown` parses it: values can never create structure (`FuzzTemplate_ValueEscaping`).

### 6.2 Template kinds and catalogue

| Kind | View struct (`docs/views`) | Data source (owner) | M4 |
|---|---|---|---|
| `quote` | `QuoteView{Marina, Customer, Vessel, Quote, Options[{Label, Placements, Price, Lines}], ValidUntil}` | contract | yes |
| `contract_annual`, `contract_seasonal`, `contract_monthly`, `contract_period`, `charter` | `ContractView{Marina, Customer, BillingCustomer, Vessel, Contract, Lines, Berths, Installments, Policy, Fleet?}` | contract | yes |
| `general_conditions`, `harbour_rules`, `letter` | `MarinaView` (+ `Customer` for letter) | docs | yes |
| `addendum` | `AddendumView` | contract | yes |
| `renewal_proposal` | `RenewalView{…, OldPrice, NewPrice, Deadline, Link}` | contract | yes |
| `payment_request`, `reminder` (manual), `installment_notice` | `InstallmentView` | contract / rights | yes |
| `holder_chain`, `mandate_statement`, `fee_notice`, `common_charges_holder`, `common_charges_items`, `transfer` | `RightsViews…` | rights | yes |
| `communication` | `CommunicationView{Marina, Customer, Vessels, Contracts}` | comms | yes |
| `receipt`, `statement_of_account`, `yard_work_order`, `yard_quote`, `rental_contract`, `rental_handover` | – | – | catalogue entry with `available_from` (M5, M10, M11); cannot be published |

Built-in default templates for every M4 kind in `it` and `en` are embedded files
`internal/domain/docs/defaults/<locale>/<kind>.md` (DOCUMENTS writes them); a marina template overrides the
default; documents in the customer's locale when a version exists, else the marina default locale, else `it`.

### 6.3 Markdown subset, layout, PDF

- Blocks: headings `#`..`###`, paragraphs, `**bold**`, `*italic*`, bullet and numbered lists (1 level of nesting),
  pipe tables (header row, alignment `:---:`), `---` rule, `{{pagebreak}}`, `{{logo}}` (marina logo from settings,
  PNG/JPEG only), `{{signature "role"}}` (box with role label, signer name and date; positions recorded for the
  evidence page). Anything else is literal text.
- `docs/layout` (pure, integer units of 1/100 mm, font metrics from the embedded fonts parsed once): A4 portrait,
  margins from the version's `page`, header/footer per page, widow/orphan control for 2 lines, table rows never
  split, page numbers. Output `Layout{Pages[{Boxes[{x,y,w,h,kind,text,style}]}], SignatureFields}` and
  `Layout.Text()` (golden text per page used by the regression tests, ADR 0005).
- `docs/pdf`: draws a `Layout` with fpdf (`AddUTF8FontFromBytes` with Go Regular/Bold/Italic/BoldItalic/Mono, never
  the core fonts), `SetCatalogSort(true)`, `SetCreationDate/SetModificationDate(generated_at)`, compression on,
  producer "Porto"; **deterministic**: same layout + same instant ⇒ same bytes (`TestPDF_Deterministic`).
- **PDF/A-2b finaliser** `pdf.FinalizePDFA(raw, meta) ([]byte, error)` (own code): appends an incremental update with
  an XMP metadata stream (pdfaid part 2, conformance B, dc:title, xmp dates), an `/OutputIntents` array with an
  sRGB ICC profile **generated by own code** from the published sRGB primaries and tone curve (`pdf/icc.go`,
  ~500 bytes, no third-party profile), a new Catalog revision and a trailer `/ID` = two copies of the SHA-256-derived
  16-byte id. Every document of a `signature_requests.document_id` is finalised; others are plain PDF.
- **Incremental append** `pdf.AppendPages(original, extra []byte) ([]byte, error)`: imports the objects of a second
  PDF produced by the same renderer (renumbered after the original `/Size`), adds its pages to the page tree and
  writes an incremental update; the original bytes are a byte-exact prefix of the result (`TestPDF_AppendKeepsPrefix`).
  Used for the OTP evidence page and the paper scan cover page.

### 6.4 Compose and render pipeline, archive

- `docs.ComposeTx(ctx, tx, scope, ComposeRequest{Kind, SubjectType, SubjectID, Locale?, TemplateID?, PDFA bool,
  CreatedBy})` (called by `contract`, `rights`, `comms`): loads the published version, asks the registered
  `DataSource` for the view **inside the tx**, stores the JSON snapshot encrypted (`data_enc`), status `pending`.
  Returns the row id. The PDF is rendered **after commit** by `docs.RenderPending(ctx, id)` called by the request
  handler post-commit and by job `documents.render` (every 5 s, wakeable, lease 2 min, retries with backoff, ≤ 5
  attempts then `failed` + office deadline `document_failed`). Rendering never runs inside a write transaction.
- Rendering = evaluate template on the snapshot → markdown → layout → pdf (→ PDF/A) → `filestore.Put` (encrypted)
  → `UPDATE … SET status='ready', sha256, file_id WHERE status='rendering' AND id=?` (idempotent). Re-rendering a
  `ready` row is a no-op; a re-render from the same snapshot must give the same `sha256`
  (`TestDocs_RenderIsReproducible`).
- Preview: `POST templates/{id}/preview` with `{version?, locale, sample: "demo" | {subject_type, subject_id}}` renders
  synchronously (≤ 2 s, size-limited) and returns the PDF bytes; nothing is stored.
- Signed documents (`signed = true`) are new rows; neither they nor their unsigned source are ever re-rendered; a
  change to a signed contract is an addendum (§3.5).

### 6.5 Signature

- `pkg/ext` (CONTRACT): `SignatureProvider` exactly as spec 15.4 plus the types `SignatureRequest{MarinaID,
  RequestID, Document []byte, DocumentSHA256, Title, Signers []Signer{Seq, Role, Name, Email, Phone, Locale},
  Sequential bool, ExpiresAt, CallbackURL}`, `SignatureEnvelope{Ref, Status, Signers []SignerStatus}`,
  `SignedDocument{PDF, Evidence []byte}`, `SignatureEvent{EnvelopeRef, Status, SignerSeq?, OccurredAt}`;
  `Extensions.SignatureProviders []SignatureProvider`. No implementation ships (InfoCert, D5).
- Internal `signature.Driver` interface (`Open`, `Status`, `Cancel`) implemented by `paperDriver`, `otpDriver` and
  `providerDriver` (adapter over `ext.SignatureProvider`, credentials decrypted from `signature_settings` only in
  memory, outbound calls only through the egress client).
- **Request lifecycle**: `OpenTx(subject, document_id, signers, order_mode, driver)` → `pending_document` (document
  not yet `ready`) → `sent` once the document is ready (job/post-render hook: invites signers — all for `parallel`,
  the first `waiting` for `sequential` — with a magic link purpose `signature` per signer and the email
  `signature_request`) → `partially_signed` → `signed` (all signers) | `declined` (any signer) | `expired` (job at
  `expires_at`) | `cancelled` (subject cancelled or staff). Reminders every `signature_reminder_days` (job
  `signatures.sync`, every 5 min, idempotent, dedupe `sig:<signer>:reminder:<n>`); provider drivers are polled by
  the same job (`Status`) and pushed by `POST /webhooks/signature/{provider}/{marinaId}` (verifies with
  `ParseWebhook` and the stored secret; unknown provider or marina ⇒ 404; replay-safe: status transitions are
  conditional updates).
- **OTP driver** (simple electronic signature): the signer opens the link (records `opened_at`, IP, user agent hash),
  views the PDF, confirms identity data (name and, for persons, fiscal code as stored), requests a code: 6 random
  digits, stored as HMAC (KeyRing-derived key `signature-otp`), TTL 10 min, ≤ 5 attempts per code (conditional
  update), ≤ 3 codes per 15 min per signer (`429 otp_rate_limited`), sent by email (and by every registered
  `ext.Notifier` of channel `sms` when the signer has a phone). Correct code ⇒ in one tx: signer `signed`,
  `evidence_enc` = `{declared_identity, email_hash, ip, user_agent_sha256, opened_at, otp_sent_at, signed_at,
  document_sha256, request_id}`; when the request is complete the evidence page (kind `signature_evidence`,
  built-in template, one section per signer) is rendered after commit and appended with `pdf.AppendPages` to the
  finalised PDF → new `generated_documents` row `signed = true`, `signed_from_id`; then `Subject.OnSignedTx` runs in
  the tx that stores the signed row. Wrong code ⇒ `422 otp_invalid` (`details.attempts_left`); exhausted ⇒ `409
  otp_locked` (a new code can be requested).
- **Paper driver**: the office prints the PDF, uploads the scan (`POST signature-requests/{id}/paper-scan`, PDF/JPEG/
  PNG ≤ 15 MiB, encrypted) and confirms (`POST …/confirm-paper` per signer or all); the signed row = original PDF
  + a cover page "firmato su carta, scansione archiviata" appended + scan file id. `require_paper_scan=false` allows
  confirmation without a scan (M3 behaviour for `activate`).
- **Subjects** (`signature.Subject` interface, CONTRACT writes it): `Kind() string`, `OnSignedTx(ctx, tx, scope,
  subjectID, signedDocumentID string, at time.Time) error`, `OnFailedTx(ctx, tx, scope, subjectID, status string) error`.
  `contract` registers `contract` and `addendum`; `rights` registers `right_transfer`. A failing `OnSignedTx` rolls
  back the whole completion (the signer stays able to retry; §15 D12 for lost holds).

### 6.6 Magic links

`magiclink.Service.CreateTx(scope, purpose, subject_type, subject_id, customer_id, expires_at) (token string, id)`:
32-byte random token (base32), stored as SHA-256; rotating a link revokes the previous one for the same
(purpose, subject, customer); `Resolve(slug, token)` constant-time by hash lookup, checks marina slug, expiry and
revocation, records `last_used_at`. Public URLs `{PORTO_BASE_URL}/b/{slug}/m/{purpose}/{token}` (served by the
booking SPA under `/m/`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Robots-Tag: noindex`).
Public API rate limit: 30 req/min per token and per IP prefix. Tokens never appear in logs (the access log redacts
`/m/…/<token>` and `/api/public/v1/{slug}/links/<token>`).

### 6.7 Communications (28)

- Transactional emails (DOCUMENTS adds the templates to `notify` in `it` and `en`; subject/text/html each):
  `quote`, `signature_request`, `signature_otp`, `signature_reminder`, `contract_activated`, `renewal_proposal`,
  `renewal_tacit_notice`, `installment_payment_request`, `installment_reminder`, `deadline_notice_customer`,
  `deadline_notice_office`, `mandate_statement`, `communication` (renders the campaign body Markdown through
  `docs/markdown` HTML writer, escaped), with attachments: `notify.Message.Attachments []Attachment{Name,
  ContentType, Content}` (multipart/mixed in `buildRFC5322`; total ≤ 10 MiB); attachments are referenced in the
  encrypted payload by `generated_documents`/`stored_files` id and loaded at send time.
- Every customer email also inserts `customer_messages` (registry per customer, `GET customers/{id}/messages`
  with status from `message_log`).
- **Mass communications** `comm_campaigns`: audience = filter `{groups, structures, contract_kinds, terms,
  holders: bool, present_from/to, with_deadlines_within_days}` or `customer_ids` or `{map_id, resource_ids}`
  (map selection); preview per recipient (rendered body for one customer); `schedule(at)` or `send now`; job
  `comms.send` (every 1 min, idempotent: recipients `pending → queued` with `message_log` dedupe
  `comm:<campaign>:<customer>`). **Consent**: `service` goes to customers with an active contract/right/transit in
  the audience (no marketing consent needed); `promotional` only to customers whose latest `marketing` consent is
  granted; others are `skipped_no_consent`. Promotional emails carry a one-click unsubscribe link (purpose
  `unsubscribe`, `List-Unsubscribe` + `List-Unsubscribe-Post` headers) that inserts a `marketing` consent
  `granted=false`, `source=online`. Extra channels: if a registered `ext.Notifier` exists for `sms` and the
  campaign selects it, the job calls it (best effort, no `message_log` row).

### 6.8 Deadline engine (11.5)

- Kinds added (validated in Go, `deadline.Kinds()`): `contract_end`, `installment_due`, `holder_installment_due`,
  `right_end`, `mandate_end`, `concession_expiry`, `concession_obligation`, `renewal_deadline`,
  `tacit_renewal_blocked`, `waitlist_offer`, `document_failed`, `signature_expiring`; M1 document kinds unchanged
  (insurance included).
- **Sources** (read-only store queries in `internal/store/deadline_sources.go`): active contracts (`ends_on`),
  instalments `issued`/`scheduled` (`due_on`), holder instalments, rights (`to_on`), mandates, concession and
  obligations, proposals `sent`/`accepted` (`deadline_on`), waitlist entries offered, failed documents, signature
  requests expiring within 3 days; M1 documents already sync on write.
- **`deadlines.sync`** (every 15 min + `RunAtStart`; per marina one tx per 500 sources): computes the desired set
  `(source_type, source_id, kind) → due_on, customer_id, vessel_id` and upserts through the existing unique index
  `deadlines_source_uidx`: new ⇒ insert `open`; changed due ⇒ update and reopen; source settled/ended ⇒ `done`;
  source cancelled/deleted ⇒ `dismissed`; manual `dismissed` rows are not reopened unless the due date changes.
  Running it twice changes nothing (`TestDeadlines_SyncIdempotent`).
- **`deadlines.notify`** (hourly): for each open deadline and each lead day `L` of its kind's policy with
  `today ≥ due_on − L` and `L` not yet in `notified_at[]`, sends per channel (`customer_email` with the customer's
  email, `office_email` to `contract_settings.office_email`; `panel` = no email) with dedupe
  `deadline:<id>:<L>:<channel>`, then appends `L` to `notified_at[]` in the same tx as the enqueue. Overdue deadlines
  notify once more at `due_on` (L = 0) when `0` is in the policy.
- **Insurance policy** (`contract_settings.insurance_policy`): `block` ⇒ `CreateContract`, `AcceptQuote`,
  `RenewContract`, campaign inclusion and tacit renewal fail `422 insurance_required` (`details.vessel_id`) when the
  vessel has no insurance document valid on the contract start; `warn` ⇒ the response carries `warnings:
  ["insurance_missing"]`. The check helper lives in `deadline` (`deadline.InsuranceValid(ctx, db, scope, vesselID,
  on)`), called by `contract`.

---

## 7. OpenAPI (`api/openapi.yaml`, CONTRACT)

### 7.1 Conventions

As m3-plan §7.1. New tags `installments`, `renewals`, `charter`, `rights`, `common-charges`, `templates`,
`documents-generated`, `signatures`, `communications`, `imports`, `concession`, `customer-links` (public). New path
params `InstallmentId`, `CampaignId`, `ProposalId`, `AddendumId`, `FleetId`, `WaitlistId`, `RightId`, `TransferId`,
`MandateId`, `FeeTableId`, `FeeRunId`, `MillesimalTableId`, `BudgetId`, `CommonRunId`, `HolderInstallmentId`,
`TemplateId`, `GeneratedDocumentId`, `SignatureRequestId`, `SignerId`, `CommCampaignId`, `ImportRunId`,
`ObligationId`, `Token`. No required query parameters on `{marinaId}` routes. Versioned resources return `ETag`;
state actions require `If-Match`; creating POSTs accept `Idempotency-Key`. Binary responses (`application/pdf`,
`text/csv`) are listed with their content type. PII gating as M3 (`customer{id, display_name}` only with
`customers.read`; holder names likewise).

### 7.2 Permissions (`roles.go`, contract enum, `TestRoles_MatrixMatchesSpec` copy)

New: `templates.configure` (owner, director: 26.1 "modelli"), `communications.send` (owner, director, office),
`providers.configure` (owner: 26.1 "credenziali dei provider"). Existing used: `contracts.read`, `contracts.write`
(quotes, contracts, renewals, charter, addenda, waitlist, contract settings except provider), `payments.collect`
(manual settlement of instalments and holder instalments), `rights.manage` (rights, transfers, mandates, fees,
common charges, concession read), `marina.configure` (concession write), `customers.read` (PII, documents,
generated document downloads also need `contracts.read` or `rights.manage` by subject), `deadlines` routes of M1
(`customers.read`), `map.view` (rights lens, map print).

### 7.3 Paths (M = `/api/admin/v1/marinas/{marinaId}`)

#### 7.3.1 Contracts, instalments, renewals, charter (handlers CONTRACTS: `internal/http/admin/{installments,renewals,charter,addenda,waitlist,contract_settings}.go`, edits to `quotes.go`, `contracts.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET/PUT `M/contract-settings` | get/updateContractSettings | contracts.read / contracts.write (+ providers.configure for `provider_code`) | `ContractSettings` (bank details masked unless `payments.collect`) + ETag | 400, 403, 412, 428 |
| POST `M/quotes/{quoteId}/send` | sendQuote (replaces markQuoteSent; `manual?: bool`) | contracts.write | If-Match → 200 `{quote, document_id?, message_id?}` | 409, 412, 422 customer_email_missing |
| POST `M/quotes/{quoteId}/accept` | acceptQuote (M3, extended: `plan?: standard\|early_payment`, `signers?`) | contracts.write | → 200 `{quote, contract, signature_request_id?, document_id}` | + 422 insurance_required |
| POST `M/contracts/{contractId}/request-signature` | requestContractSignature | contracts.write | `{driver?, signers?[{role, customer_id?, user_id?}], order_mode?}` + If-Match → 200 | 409 invalid_transition/signature_open, 412 |
| POST `M/contracts/{contractId}/activate` | (M3) | contracts.write | M4: `{paper_scan_file_id?}` | + 422 paper_scan_required |
| POST `M/contracts/{contractId}/suspend` / `resume` | suspendContract / resumeContract | contracts.write | `{reason}` / – + If-Match → 200 | 409, 412 |
| POST `M/contracts/{contractId}/terminate` | (M3) | contracts.write | response adds `termination_amount_cents` | – |
| POST `M/contracts/{contractId}/renew` | renewContract | contracts.write | `{starts_on?, price_list_id?, deadline_on?, send?}` + If-Match → 201 `RenewalProposal` | 409 renewal_exists/resource_taken/invalid_transition, 422 no_price/insurance_required |
| GET `M/contracts/{contractId}/installments` | listContractInstallments | contracts.read | → 200 `{plan_version, items: Installment[], total_cents, sum_cents, alternatives?: {early_payment}}` | 404 |
| POST `M/contracts/{contractId}/installments/replan` | replanInstallments | contracts.write | `{plan: standard\|early_payment, payment?: PaymentPolicy}` + If-Match → 200 | 409, 412, 422 |
| GET `M/installments` | listInstallments (scadenzario) | contracts.read | `from?`, `to?`, `status?` (incl. overdue), `customer_id?`, `contract_id?`, `group_id?`, `format?=csv`, cursor → page / CSV | 404 |
| POST `M/installments/issue` | issueInstallments | contracts.write | `{ids[] ≤ 500}` → 200 `{issued, skipped}` | 400 |
| POST `M/installments/remind` | remindInstallments | contracts.write | `{ids[] ≤ 500}` → 200 `{queued, skipped_no_email}` | 400 |
| POST `M/installments/{installmentId}/settle` | settleInstallment | payments.collect | `{method, reference?, settled_on?}` + If-Match → 200 | 409 invalid_transition, 412 |
| POST `M/installments/settle` | settleInstallments (bulk) | payments.collect | `{items[{id, version}] ≤ 500, method, reference?, settled_on?}` → 200 `{settled}` all or nothing | 409 (details: first failing id), 412 |
| GET/POST `M/renewal-campaigns` | list/createRenewalCampaign | contracts.read / contracts.write | `{name, filter, deadline_on, price_list_id?, target_starts_rule?, message?}` → 201 | 400, 403 |
| GET/PATCH `M/renewal-campaigns/{campaignId}` | get/updateRenewalCampaign | read/write | draft only | 409, 412 |
| POST `M/renewal-campaigns/{campaignId}/preview` | previewRenewalCampaign | contracts.write | If-Match → 200 `{counts, proposals page}` (old/new prices) | 409, 412 |
| POST `M/renewal-campaigns/{campaignId}/launch` / `close` / `discard` | launch/close/discardRenewalCampaign | contracts.write | If-Match → 200 | 409, 412 |
| GET `M/renewal-campaigns/{campaignId}/proposals` | listCampaignProposals | contracts.read | `status?`, `format?=csv`, cursor | 404 |
| GET `M/renewal-proposals/{proposalId}` | getRenewalProposal | contracts.read | → 200 + ETag | 404 |
| POST `M/renewal-proposals/{proposalId}/{send\|accept\|decline\|include}` | send/accept/decline/includeRenewalProposal | contracts.write | accept `{plan?}`; decline `{reason?}`; include `{reason}` + If-Match → 200 | 409, 412, 422 |
| GET/POST `M/charter-fleets`, GET/PATCH `M/charter-fleets/{fleetId}` | …CharterFleet | contracts.read / write | `{customer_id, name, vessel_ids[]}` | 409 vessel_in_other_fleet |
| POST `M/charters` | createCharter | contracts.write | `{fleet_id, starts_on, ends_on, placements[{target, vessel_id}], pricing_mode, flat_cents?, flat_reason?, changeover_weekday, out_nights?, services?, policy?, activate?}` → 201 `Contract` | 409, 422 |
| POST `M/charters/{contractId}/rotate` | rotateCharter | contracts.write | `{from_night, assignments[{line_id, vessel_id}]}` + If-Match → 200 | 409, 412, 422 fit_blocked/vessel_not_in_fleet |
| GET/POST `M/contracts/{contractId}/addenda` | list/createAddendum | contracts.read / write | `{kind, effective_on, payload, fit_override?}` → 201 `Addendum` (with `price_effect`) | 409, 422 |
| POST `M/contracts/{contractId}/addenda/{addendumId}/{request-signature\|activate\|cancel}` | … | contracts.write | If-Match → 200 | 409, 412 |
| GET/POST `M/waitlist`, PATCH `M/waitlist/{waitlistId}`, POST `M/waitlist/{waitlistId}/quote` | … | contracts.read / write | `QuoteInput` prefill → 201 `Quote` | 409 |

#### 7.3.2 Rights, mandates, fees, common charges, concession (handlers RIGHTS-CHARGES: `internal/http/admin/{rights,mandates,fees,common_charges,concession}.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET/POST `M/rights`, GET/PATCH `M/rights/{rightId}` | list/create/get/updateRight | rights.manage | `{berth_id, kind, from_on, to_on?, title, holders[{customer_id, share_millis}], vessels?[{vessel_id, primary}], fit_override?}` → 201 | 409 resource_taken, 422 shares_invalid/fit_blocked |
| POST `M/rights/{rightId}/{end\|cancel}` | end/cancelRight | rights.manage | `{on, reason}` + If-Match | 409, 412 |
| PUT `M/rights/{rightId}/vessels` | setRightVessels | rights.manage | `{vessels[{vessel_id, primary}], from_night?, fit_override?}` + If-Match | 422 |
| GET `M/berths/{berthId}/right-history` | getRightHistory | rights.manage | `format?=pdf` | 404 |
| GET `M/maps/{mapId}/rights` | getMapRights | map.view | `date?` → `{items[{berth_id, right_id?, status: marina\|holder\|mandate, unpaid_fees: bool}]}` (no names) | 404 |
| GET/POST `M/right-transfers`, GET `…/{transferId}` | … | rights.manage | `{right_id, effective_on, to_holders, declared_price_cents?, takeover_item_id?, fees_policy}` → 201 (`requirement_issues`) | 422 transfer_requirements/shares_invalid |
| POST `M/right-transfers/{transferId}/{request-signature\|complete\|cancel}` | … | rights.manage | complete = paper confirmation `{paper_scan_file_id?}` + If-Match | 409, 412 |
| GET/POST `M/mandates`, GET `…/{mandateId}`, POST `…/{mandateId}/{end\|cancel}` | … | rights.manage | `{right_id, from_on, to_on, owner_share_bp, min_guaranteed_cents?, channels, settlement}` | 409 mandate_overlap/resource_taken |
| GET `M/mandates/{mandateId}/statement` | getMandateStatement | rights.manage | `from?`, `to?`, `format?=json\|csv\|pdf` | 404 |
| POST `M/mandates/accrue` | accrueMandates | rights.manage | → 200 `{rows}` (on-demand job run for the marina) | – |
| CRUD `M/fee-tables`, `…/{feeTableId}` (+ `activate`) | … | rights.manage | rows by basis | 409, 422 |
| POST `M/fee-runs`, GET `…/{feeRunId}` (+ `lines`), POST `…/{feeRunId}/{simulate\|issue\|discard}` | … | rights.manage | `{year, table_id, plan}`; simulate → `{totals_by_category, diff_previous, excluded[{right_id, reason}]}` | 409 conflict, 412 |
| CRUD `M/millesimal-tables`, `…/{millesimalTableId}/entries` (PUT full set, CSV import `?format=csv`), POST `…/activate` | … | rights.manage | `{entries[{berth_id, millis}]}` → `{sum_millis}` | 422 table_incomplete, 409 table_active |
| CRUD `M/common-charge-budgets`, `…/{budgetId}/items`, POST `…/{budgetId}/{approve\|close}`, PUT `…/{budgetId}/actuals` (JSON or CSV) | … | rights.manage | items `{name, table_id, budget_cents}` | 422 |
| POST `M/common-charge-budgets/{budgetId}/runs`, GET `M/common-charge-runs/{commonRunId}` (+ `allocations`, `format?=csv`), POST `…/{commonRunId}/{issue\|discard}` | … | rights.manage | `{kind: advance\|balance, share_bp?}` → 201 `{totals_by_item[{item_id, amount_cents, allocated_cents}], holders}` | 409, 422 table_incomplete |
| GET `M/common-charge-runs/{commonRunId}/statements` | getCommonChargeStatements | rights.manage | `customer_id?`, `format=pdf\|csv`, `by=holder\|item` | 404 |
| GET `M/holder-installments`, POST `…/{holderInstallmentId}/settle`, POST `M/holder-installments/settle` | … | rights.manage / payments.collect | as instalments | 409, 412 |
| GET/PUT `M/concession`, CRUD `M/concession/obligations`, POST `…/{obligationId}/done` | … | rights.manage / marina.configure | `Concession` + ETag | 412 |

#### 7.3.3 Templates, documents, map print (handlers DOCUMENTS: `internal/http/admin/{templates,generated_documents}.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET `M/template-kinds` | listTemplateKinds | contracts.read | → `[{kind, fields: FieldDef[{path, type, list, description_key}], available_from?}]` | – |
| GET/POST `M/templates`, GET/PATCH `M/templates/{templateId}` | … | contracts.read / templates.configure | `{kind, code, name}` | 409 code_taken |
| GET/POST `M/templates/{templateId}/versions` | list/createTemplateVersion | templates.configure | `{locale, body_md, header_md?, footer_md?, page?}` → 201 draft (validated) | 422 template_invalid |
| POST `M/templates/{templateId}/versions/{version}/publish` | publishTemplateVersion | templates.configure | `{locale}` → 200 | 409, 422 |
| POST `M/templates/{templateId}/preview` | previewTemplate | templates.configure | `{version?, locale, body_md?, sample}` → 200 `application/pdf` (+ `X-Porto-Pages`) | 422 template_invalid/template_limits |
| GET `M/generated-documents` | listGeneratedDocuments | contracts.read | `subject_type?`, `subject_id?` | 404 |
| GET `M/generated-documents/{generatedDocumentId}` | getGeneratedDocument | by subject (§7.2) | → `{status, sha256, pages, signed, signed_from_id, template_version}` | 404 |
| GET `M/generated-documents/{generatedDocumentId}/link` | getGeneratedDocumentLink | + customers.read | → `{url, expires_at}` (5 min, user-bound) | 404, 409 not_ready |
| POST `M/generated-documents/{generatedDocumentId}/render` | retryRender | contracts.write | → 202 | 409 |
| GET `M/maps/{mapId}/print` | printMap | map.view | `date?`, `lens?` (occupancy, contract_type, vessel_type) → `application/pdf` (no customer names without customers.read) | 404 |

#### 7.3.4 Signature (handlers DOCUMENTS: `internal/http/admin/signatures.go`, `internal/http/webhooks/signature.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET `M/signature-requests` | listSignatureRequests | contracts.read | `status?`, `subject_type?` | 404 |
| GET `M/signature-requests/{signatureRequestId}` | getSignatureRequest | contracts.read | signers without emails unless customers.read | 404 |
| POST `M/signature-requests/{signatureRequestId}/{remind\|cancel}` | … | contracts.write | If-Match | 409, 412 |
| POST `M/signature-requests/{signatureRequestId}/paper-scan` | uploadPaperScan | contracts.write | raw PDF/JPEG/PNG ≤ 15 MiB (`x-porto-max-body-bytes`) → `{file_id}` | 400 invalid_file, 413 |
| POST `M/signature-requests/{signatureRequestId}/confirm-paper` | confirmPaperSignature | contracts.write (rights.manage for transfers) | `{signer_ids?, paper_scan_file_id?}` + If-Match → 200 | 409, 422 paper_scan_required |
| GET/PUT `M/signature-settings` | get/updateSignatureSettings | providers.configure | `{provider_code, credentials (write-only), webhook_secret (write-only)}` | 422 provider_unknown |
| POST `/webhooks/signature/{provider}/{marinaId}` | signatureWebhook | provider signature | raw body → 204 | 400, 401 invalid_signature, 404 |

#### 7.3.5 Communications, deadlines, imports (handlers DOCUMENTS: `internal/http/admin/{communications,imports}.go`, edits to `deadlines.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET/POST `M/communications`, GET/PATCH `M/communications/{commCampaignId}` | … | communications.send | `{name, category, audience, template_id? \| subject+body_md, attachments?, scheduled_at?}` | 422 template_invalid |
| POST `M/communications/{commCampaignId}/preview` | previewCommunication | communications.send | `{customer_id?}` → `{recipients: {total, skipped_no_consent, skipped_no_email}, sample: {subject, html, text}}` | 404 |
| POST `M/communications/{commCampaignId}/{send\|schedule\|cancel}` | … | communications.send | If-Match → 200 | 409, 412 |
| GET `M/communications/{commCampaignId}/recipients` | listCommunicationRecipients | communications.send | statuses, `format?=csv` | 404 |
| GET `M/customers/{customerId}/messages` | listCustomerMessages | customers.read | page | 404 |
| GET/PUT `M/deadline-policies` (M1, extended with `channels`) | … | (M1 perms) | | |
| POST `M/deadlines/sync` | syncDeadlines | contracts.write | → 200 `{created, updated, closed}` | – |
| POST `M/imports` | createImport | by entity (customers.write, contracts.write, rights.manage) | `{entity, csv, delimiter?, has_header, mapping, dry_run}` → 200 `ImportReport` | 400 csv_invalid, 422 |
| GET `M/imports`, GET `M/imports/{importRunId}`, POST `M/imports/{importRunId}/undo` | … | same | | 409 import_not_undoable |

#### 7.3.6 Public magic-link API (`/api/public/v1/{slug}/links/{token}`, handlers `internal/http/public/{links,signing}.go`; quote/renewal = CONTRACTS, signing/unsubscribe = DOCUMENTS; `security: []`, rate limited)

| Method & path | operationId | Body → 2xx | Errors |
|---|---|---|---|
| GET `…/links/{token}` | resolvePublicLink | → `{purpose, marina{name, locales}, expires_at, subject}` where subject is `PublicQuote` / `PublicRenewal` / `PublicSignature` / `PublicUnsubscribe` (only this customer's data) | 404 link_invalid (expired/revoked/unknown, same answer) |
| GET `…/links/{token}/document` | getPublicLinkDocument | → `application/pdf` (quote, proposal or document to sign) | 404, 409 not_ready |
| POST `…/links/{token}/quote/accept` | acceptPublicQuote | `{option_id, customer?{address, …required_fields}, vessel?{registration, …}, plan?}` → 200 `{contract_code, signature_link?, deposit?{amount_cents, due_on, bank_details}}` | 409 quote_expired/resource_taken, 422 |
| POST `…/links/{token}/documents` | uploadPublicDocument | raw file + `?type=insurance_policy\|identity_card…&expires_on=` → 201 `{document_id}` | 400, 413, 415 |
| POST `…/links/{token}/renewal/{accept\|decline}` | accept/declinePublicRenewal | `{plan?}` / `{reason?}` → 200 `{status, signature_link?, deposit?}` | 409 proposal_closed |
| POST `…/links/{token}/signature/{otp\|verify\|decline}` | request/verify/declinePublicSignature | otp `{identity_confirmed: true}`; verify `{code}`; decline `{reason?}` → 200 `{signer_status, request_status}` | 409 otp_locked, 422 otp_invalid, 429 otp_rate_limited |
| POST `…/links/{token}/unsubscribe` | publicUnsubscribe | → 204 (also accepts `List-Unsubscribe-Post` form body) | 404 |

Every new `{marinaId}` route is in `TestAcceptance_CrossMarinaIsolation` and every new id param in
`TestAcceptance_CrossMarinaResourceIds`; CONTRACT raises the route-count threshold. Public link routes are covered by
`TestPublicLinks_CrossMarina` (a token of marina A with slug B ⇒ 404).

### 7.4 Problem types (`internal/http/problem/types.go`, `respond/errmap.go`, i18n `errors.*`)

New slugs: `renewal_exists` 409, `proposal_closed` 409, `signature_open` 409, `invalid_signature` 401 (webhook),
`otp_invalid` 422 (`details.attempts_left`), `otp_locked` 409, `otp_rate_limited` 429, `link_invalid` 404,
`not_ready` 409, `template_invalid` 422 (`details[{line, col, code, path?}]`), `template_limits` 422,
`paper_scan_required` 422, `insurance_required` 422 (`details.vessel_id`), `customer_email_missing` 422,
`shares_invalid` 422 (`details.sum_millis`), `table_incomplete` 422 (`details.sum_millis`), `table_active` 409,
`transfer_requirements` 422 (`details.issues[]`), `mandate_overlap` 409, `vessel_in_other_fleet` 409,
`vessel_not_in_fleet` 422, `import_not_undoable` 409, `provider_unknown` 422, `code_taken` 409. Reused:
`invalid_transition`, `resource_taken`, `fit_blocked`, `no_price`, `quote_expired`, `version_conflict`,
`precondition_required`, `conflict`, `validation_failed`, `csv_invalid`, `invalid_file`, `file_too_large`,
`not_found`, `forbidden`. CONTRACT writes sentinels: `contract.ErrRenewalExists`, `ErrProposalClosed`,
`ErrInsuranceRequired`, `ErrCustomerEmailMissing`, `ErrPaperScanRequired`, `rights.ErrSharesInvalid`,
`ErrTableIncomplete`, `ErrTableActive`, `ErrTransferRequirements`, `ErrMandateOverlap`, `docs.ErrTemplateInvalid`
(`*TemplateError`), `ErrTemplateLimits`, `ErrNotReady`, `signature.ErrOTPInvalid` (`*OTPError{AttemptsLeft}`),
`ErrOTPLocked`, `ErrOTPRateLimited`, `ErrSignatureOpen`, `ErrInvalidWebhookSignature`, `ErrProviderUnknown`,
`magiclink.ErrInvalid`, `csvimport.ErrNotUndoable`.

### 7.5 Events and SSE

| Type | Public | Topic | Data (ids, enums, dates, amounts, counts only) |
|---|---|---|---|
| `contract.signed` | yes | `contract.changed` | `{contract_id, kind, term?, status, signature_request_id?, driver}` |
| `contract.renewed` | yes | `contract.changed` | `{contract_id, successor_contract_id, kind, term?, from, to, tacit}` |
| `installment.overdue` | yes | `contract.changed` | `{installment_id, contract_id, seq, due_on, amount_cents}` |
| `installment.changed` (internal: issued, settled, cancelled, replanned) | no | `contract.changed` | `{installment_id?, contract_id, change, plan_version}` |
| `renewal.proposed` / `renewal.accepted` / `renewal.declined` / `renewal.expired` / `renewal.completed` | yes | `contract.changed` | `{proposal_id, campaign_id?, source_contract_id, new_contract_id?, status}` |
| `renewal_campaign.changed` | no | `contract.changed` | `{campaign_id, status, counts}` |
| `right.created` / `right.transferred` / `right.ended` | yes | `rights.changed` | `{right_id, berth_id, kind, from, to, new_right_id?, transfer_id?}` |
| `mandate.created` / `mandate.ended` | yes | `rights.changed` (+ `occupancy.*` from the primitives) | `{mandate_id, right_id, berth_id, from, to}` |
| `mandate.revenue_accrued` | yes | `rights.changed` | `{mandate_id, rows, revenue_cents, owner_share_cents, through}` |
| `management_fee.issued`, `common_charges.issued` | yes | `rights.changed` | `{run_id, year, kind?, total_cents, holders}` |
| `rights.changed` (internal: tables, budgets, runs, holder instalments, concession) | no | `rights.changed` | `{entity, id, change}` |
| `document.generated` | no | `documents.changed` | `{document_id, kind, subject_type, subject_id, status, signed}` |
| `signature.requested` / `signature.completed` / `signature.failed` | no | `documents.changed` | `{signature_request_id, subject_type, subject_id, status, driver}` |
| `template.changed`, `communication.changed` | no | `documents.changed` | `{template_id \| campaign_id, change}` |
| `deadline.changed` (M1, now also from the engine) | no | `registry.changed` | unchanged |

New topics `rights.changed`, `documents.changed` (spec 27.4 lists the M3 ones; these are additive, §15 D21). Web
`SSE_TOPICS` += both; `keysForMessage`: `contract.changed` adds `installments(m)`, `renewals(m)`, `contract(id)`
installments; `rights.changed` → `rights(m)`, `mapRights(m, *)`, `fees(m)`, `commonCharges(m)`,
`holderInstallments(m)`; `documents.changed` → `templates(m)`, `generatedDocs(m)`, `signatures(m)`,
`communications(m)`. Samples for every new type in `Samples()` (`TestEvents_DataHasNoPII`).

Audit actions: `installment.{plan,issue,settle,cancel,replan,remind}`, `renewal_campaign.{create,update,preview,launch,close,discard}`,
`renewal_proposal.{create,send,accept,decline,expire,include,complete}`, `contract.{request_signature,sign,suspend,resume,renew,tacit_renew,signature_failed}`,
`addendum.{create,request_signature,activate,cancel}`, `charter.{create,rotate}`, `fleet.{create,update}`,
`waitlist.{create,update,offer}`, `contract_settings.update`, `right.{create,update,end,cancel,vessels}`,
`transfer.{create,request_signature,complete,cancel}`, `mandate.{create,end,cancel,accrue}`,
`fee_table.*`, `fee_run.{create,simulate,issue,discard}`, `millesimal_table.*`, `budget.*`, `common_run.*`,
`holder_installment.{issue,settle}`, `concession.*`, `template.{create,update,version_create,publish}`,
`document.{compose,render,fail}`, `signature.{open,invite,otp_sent,otp_failed,sign,decline,expire,cancel,paper_confirm}`,
`signature_settings.update`, `communication.{create,update,schedule,send,cancel}`, `consent.unsubscribe`,
`deadline.sync`, `import.{dry_run,apply,undo}`. Diffs carry ids, statuses, dates, amounts; never names, emails,
codes, references, template bodies (only their sha256).

### 7.6 Meta and jobs

Capabilities `contracts.full`, `installments`, `renewals.campaigns`, `charter`, `rights`, `common_charges`,
`templates.pdf`, `signature.paper`, `signature.otp`, `signature.providers` (only when a provider is registered),
`communications`, `deadlines.engine`, `imports.csv`. Jobs (registered by CONTRACT in `app.go`, implemented by the
owners): `installments.issue` (10 min, CONTRACTS, covers `holder_installments` through `rights.IssueDueTx`),
`renewals.sync` (1 min, CONTRACTS), `contracts.expire` (M3, extended with renewed/tacit, CONTRACTS),
`mandates.accrue` (24 h at 02:00 marina time + RunAtStart, RIGHTS-CHARGES), `documents.render` (5 s wakeable,
DOCUMENTS), `signatures.sync` (5 min, DOCUMENTS), `comms.send` (1 min, DOCUMENTS), `deadlines.sync` (15 min,
DOCUMENTS), `deadlines.notify` (1 h, DOCUMENTS), `links.cleanup` (24 h, DOCUMENTS: links expired > 90 days deleted,
OTP rows > 24 h deleted).

---

## 8. Web

### 8.1 Routes (`web/admin`, TanStack code-based; CONTRACT adds `contractsM4Routes`, `rightsRoutes`, `documentsRoutes` factories in `router.tsx` and nav items)

| Route | Page | Permission (UI) |
|---|---|---|
| `/m/$marinaId/contracts/$contractId` (M3 page, extended) | tabs: Dati, Rate (plan, alternatives, settle, replan), Documenti (generated, signed, download link), Firma (request status, signers, remind, paper scan + confirm), Addenda, Rinnovo (proposal state, renew action), Storico | contracts.read |
| `/m/$marinaId/quotes/$quoteId` (extended) | send by email (PDF preview), link state, accept with plan/signers | contracts.read |
| `/m/$marinaId/installments` | scadenzario: filters, overdue ageing, bulk issue/remind/settle (dialog with method/reference), CSV | contracts.read |
| `/m/$marinaId/renewals` + `/renewals/$campaignId` | **campaign wizard**: 1 filter → 2 preview (old/new price table, totals, exclusions with reasons, include) → 3 message & deadline → 4 launch; then the campaign monitor (counts by state, SSE live, proposals table, resend, accept/decline on behalf, CSV) | contracts.read |
| `/m/$marinaId/charters` + fleet dialog + charter wizard + rotation board (berths × fleet vessels, drag to rotate from a night) | contracts.read |
| `/m/$marinaId/waitlist` | entries, quote from entry | contracts.read |
| `/m/$marinaId/rights` + `/rights/$rightId` | rights list, wizard (berth picker from the map, holders with live Σ‰, vessels with fit), holder chain PDF, end/cancel | rights.manage |
| `/m/$marinaId/transfers` + `/transfers/$transferId` | transfer wizard (requirements panel, fee, signers), status | rights.manage |
| `/m/$marinaId/mandates` + detail | mandate form (period on a mini planner), revenue table, statement export | rights.manage |
| `/m/$marinaId/fees` | fee tables editor, run wizard (simulate → totals by category, diff vs previous year, excluded → issue) | rights.manage |
| `/m/$marinaId/common-charges` | millesimal tables (grid editor + CSV import, live Σ, activate), budgets with items, actuals (form + CSV), runs (allocation table per item/holder, sums), statements | rights.manage |
| `/m/$marinaId/holder-installments` | as scadenzario for holders | rights.manage |
| `/m/$marinaId/concession` | concession card and obligations | rights.manage |
| `/m/$marinaId/templates` + `/templates/$templateId` | **templates editor**: kinds list, versions per locale, split view Markdown editor (textarea from shadcn, toolbar inserting fields from the catalogue tree, `each`/`if` snippets) + PDF preview (iframe of the blob from `previewTemplate`, sample demo or record picker), validation errors inline by line, publish | templates.configure |
| `/m/$marinaId/signatures` | requests list by status, detail with signers timeline | contracts.read |
| `/m/$marinaId/communications` + `/communications/$id` | wizard: audience (filter or map selection via `@porto/shared/map` selection mode) → content (template or free text with fields) → attachments → preview per recipient (counts skipped) → schedule/send; recipients table | communications.send |
| `/m/$marinaId/deadlines` (M1 page, extended) | engine kinds, filters by kind/urgency, sync now, policy editor with channels | customers.read |
| `/m/$marinaId/imports` | import wizard (entity, upload CSV, mapping, dry-run report, apply, undo) | per entity |
| `/m/$marinaId/settings/contracts` | contract settings (default policy editor with plan preview, signature driver, insurance policy, bank details, required fields) | contracts.write |
| `/m/$marinaId/settings/signature` | provider credentials | providers.configure |
| ops map lens **Diritti d'uso** + Tipo di contratto adds charter/right/mandate | `getMapRights` composed with the day view | map.view |
| ops map "Stampa" | `printMap` | map.view |

Customer pages in `web/booking` (public SPA, `/b/$slug/m/$purpose/$token`): quote acceptance (options, missing
data form, document upload, plan choice, accept → signing), renewal proposal (old/new price, accept/decline),
signing (PDF viewer through `<iframe>` of the blob, identity confirmation, request OTP, code input with attempts
left, success page with deposit instructions), unsubscribe confirmation. Phone-first layout (390 px), axe clean.

### 8.2 Components and shared pieces

CONTRACT adds through the shadcn CLI in `web/shared`: `textarea`, `progress`, `stepper`-equivalent is composed from
existing `tabs`/`card` (no non-shadcn component), `input-otp` (`npx shadcn@latest add input-otp`), `resizable`
(editor split view), `calendar` if not present. Shared hooks files per area (`web/shared/src/api/hooks/{contractsM4,
rights,documents,publicLinks}.ts`, stubs by CONTRACT, filled by WEB). Query keys (CONTRACT) under
`queryKeys.desk(m)` (`installments`, `renewals`, `campaign(id)`, `proposal(id)`, `charters`, `fleets`, `waitlist`,
`contractSettings`), `queryKeys.rights(m)` (`rights`, `right(id)`, `mapRights(mapId, date)`, `transfers`,
`mandates`, `feeTables`, `feeRuns`, `millesimalTables`, `budgets`, `commonRuns`, `holderInstallments`, `concession`),
`queryKeys.documents(m)` (`templates`, `template(id)`, `templateKinds`, `generatedDocs(subject)`, `signatures`,
`signature(id)`, `communications`, `communication(id)`, `imports`, `customerMessages(id)`).

### 8.3 i18n namespaces (CONTRACT creates the files; WEB fills except enums)

`contractsM4`, `renewals`, `charter`, `rights`, `commonCharges`, `templates`, `signature`, `communications`,
`imports`, `docsEnums` (CONTRACT writes complete: template kinds, field descriptions keys, statuses of every new
table, exclusion reasons, deadline kinds), `customerLinks` (booking SPA). `errors.json` gets the §7.4 slugs
(CONTRACT).

---

## 9. Tests and acceptance

### 9.1 Gates (exact commands, from the repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated            # empty
make check                                                    # gen-check + lint (depguard with the new pure packages) + unit
make test-db                                                  # integration on SQLite AND PostgreSQL
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
go test ./internal/domain/schedule/... ./internal/domain/allot/... ./internal/domain/accrual/... ./internal/domain/docs/tmpl/... ./internal/domain/docs/markdown/... ./internal/domain/docs/layout/... -count=1 -rapid.checks=5000
go test ./internal/domain/docs/tmpl/... -run '^$' -fuzz FuzzTemplate_Parse -fuzztime 60s
go test ./internal/domain/docs/tmpl/... -run '^$' -fuzz FuzzTemplate_ValueEscaping -fuzztime 60s
go test ./internal/domain/docs/pdf/... -count=1 -run 'TestPDF_'
scripts/with-postgres.sh go test -tags=integration -count=1 -race ./internal/domain/contract/... ./internal/domain/rights/... ./internal/domain/docs/... ./internal/domain/signature/... ./internal/domain/comms/... ./internal/domain/deadline/... ./internal/domain/magiclink/... ./internal/store/...
make test-race                                                # every TestConcurrency_* incl. §9.3 on both dialects
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 30m ./internal/acceptance/... -run 'TestAcceptance_M4|TestAcceptance_M3|TestAcceptance_M2|TestAcceptance_M1|TestAcceptance_M0|TestAcceptance_CrossMarina|TestPublicLinks_'
make e2e                                                      # all specs incl. §9.5
scripts/agents/check-naming.sh && make licenses              # fpdf MIT, x/image BSD-3 accepted
rm -f tmp/data/porto.db* && make seed && go run ./cmd/porto seed demo   # second run no-op; prints M4 counts
```

### 9.2 Unit and property tests (names fixed; referenced by `docs/invariants.md`)

- CONTRACTS: **`TestInstallmentPlan_SumsToTotal`** (rapid: random totals 0..10^9 cents, deposit bp/cents, modes,
  counts 1..24, rounding units 1/100/1000, dates; Σ = total, all ≥ 0, last absorbs the remainder, issue ≤ due,
  dates inside the period or today), `TestInstallmentPlan_Scenario` (table: annual 30 % + 3 quarters, fixed dates
  rolled over the year, past dates merged, single, early payment alternative, zero total), `TestSchedule_Purity`,
  `TestContract_TransitionTableM4`, `TestRenewal_ExclusionRules`, `TestRenewal_NextPeriod`, `TestCharter_PricingModes`,
  `TestTermination_Amounts`.
- RIGHTS-CHARGES: **`TestCommonCharges_AllocationSumsExactly`** (rapid: 1..400 berths, millis summing to 1000 with
  zeros allowed, 1..12 items with totals −10^8..10^9 cents, 1..4 holders per berth with shares summing to 1000,
  berths without rights; Σ per item = item, Σ run = Σ items, Σ per berth over holders = berth share, |share −
  exact| < 1 cent per berth step), **`TestRight_HolderSharesSumTo1000`**, **`TestAccrual_SumsToAmount`** (rapid:
  amounts, 1..3 660 days, periods; Σ day quotas = amount, remainder on the last day, quotas outside the period 0),
  `TestFeeAmount_Scenario` (per category, per m², bands, ISTAT, pro rata), `TestAllot_Purity`, `TestAccrual_Purity`.
- DOCUMENTS: `TestTemplate_Scenario` (every token, filter, block, error code with line/col), `FuzzTemplate_Parse`
  (no panic, bounded output), **`FuzzTemplate_ValueEscaping`** (values never change the block structure),
  `TestTemplate_CatalogueMatchesViews`, `TestMarkdown_Scenario`, `TestLayout_Golden` (golden text per page of every
  built-in template on the demo sample, it/en; `-update` flag), `TestLayout_TableRowsNeverSplit`,
  `TestPDF_Deterministic`, `TestPDF_PDFAMarkers` (XMP pdfaid 2/B, OutputIntent with the ICC, `/ID`, fonts embedded,
  no core fonts, xref valid by own parser), `TestPDF_AppendKeepsPrefix`, `TestPDF_ICCProfileValid` (header, tag
  table, size), `TestSilhouette_ParityVectors` (Go mirror ↔ `testdata/silhouette/*.json` used by Vitest too),
  `TestOTP_CodeHashing`, `TestMagicLink_TokenHashing`.

### 9.3 Integration and concurrency tests (tag `integration`, `testdb` both dialects, `-race`)

CONTRACTS: `TestContract_StateChangeWritesAuditAndOutbox` (extended to every §3 row), `TestQuote_AcceptRunsAllSteps`
(plan + document + signature request in one tx; forced failure after the document row leaves nothing),
`TestQuote_SendRotatesLink`, `TestInstallment_IssueJobIdempotent`, `TestInstallment_SettleOnlyManualDriver` (INV-32
grep test over `internal/**` for assignments of `InstallmentPaid`), `TestRenewal_SimpleHoldsBerth`,
`TestRenewal_CampaignPreviewIdempotent`, `TestRenewal_SyncJobIdempotent`, `TestRenewal_ExpiredReleasesHold`,
`TestContract_ExpireMarksRenewed`, `TestContract_TacitRenewal`, `TestAddendum_PriceChangeReplans` (INV-27, INV-31),
`TestContract_ChangeVesselProRata`, `TestCharter_RotationNoReprice`, `TestWaitlist_OfferOnTermination`,
`TestImport_ContractsResidualPlan`, `TestImport_UndoGuard`, and:
- **`TestConcurrency_ContractTwoOperators`**: two operator sessions (office and director) act on the same contract
  version: 100 goroutines split between `activate` (paper), `cancel`, `request-signature`, `update` with the same
  `If-Match` ⇒ exactly one 200, the others 412 or 409; final state ∈ the table; one audit row and one event set.
- **`TestConcurrency_RenewSameContract`** (100 parallel `renew` ⇒ one proposal and one hold; others `409
  renewal_exists`, no orphan draft contracts or allocations).
- **`TestConcurrency_InstallmentSettle`** (100 parallel settles of the same instalment and of overlapping bulk sets
  ⇒ each instalment paid once, one audit row each).
- **`TestConcurrency_SignVsCancelContract`** (OTP verify vs contract cancel ⇒ either active+signed or cancelled with
  the request cancelled; never active without signed document, never signed document on a cancelled contract).
- **`TestConcurrency_CampaignLaunchAndSync`** (two launches + 4 concurrent `renewals.sync` runs ⇒ each proposal
  emailed once: dedupe keys unique, `sent_count = 1`).
- **`TestConcurrency_AddendumVsTerminate`**, **`TestConcurrency_QuotePublicAcceptVsPanelAccept`**.

RIGHTS-CHARGES: `TestRight_OwnerOccupancy` (rows for every night, conflict with a contract ⇒ 409 nothing written,
CheckOccupancyInvariants ∅ incl. new Q17), `TestRight_PrimaryVesselChange`, `TestTransfer_CompleteMovesEverything`,
`TestTransfer_RequirementsBlockOrWarn`, `TestMandate_ReleasesOwnerNights`, `TestMandate_RevenueAccrualIdempotent`,
`TestMandate_EndRestoresFreeNights`, `TestFeeRun_SimulateIssue`, `TestCommonCharges_RejectsIncompleteTable`
(INV-41), `TestCommonCharges_BalanceEqualsActualMinusAdvances`, `TestMillesimal_ActiveImmutable` (trigger),
**`TestConcurrency_RightTransferSingle`** (100 parallel completions of two transfers of the same right ⇒ one right
chain), **`TestConcurrency_MandateOverlap`**, **`TestConcurrency_FeeRunIssueOnce`**,
**`TestConcurrency_CommonChargeRunOnce`**, **`TestConcurrency_RightVsContractSameBerth`** (INV-01 with `owner`).

DOCUMENTS: `TestDocs_ComposeRenderArchive` (encrypted at rest: raw blob ≠ PDF, sha256 of the decrypted file =
row), `TestDocs_RenderIsReproducible`, `TestDocs_RenderJobRecovers`, `TestDocs_SignedNeverRerendered`,
`TestSignature_OTPFlow`, `TestSignature_OTPLockout`, `TestSignature_SequentialSigners`, `TestSignature_PaperFlow`,
`TestSignature_ExpiryAndReminders`, `TestSignature_ProviderAdapter` (fake `ext.SignatureProvider` in the test only:
request, webhook, status, download), `TestSignature_WebhookUnknownProvider404`, `TestComms_ConsentRules`,
`TestComms_Unsubscribe`, `TestComms_SendJobIdempotent`, `TestDeadlines_SyncIdempotent`,
`TestDeadlines_NotifyOncePerLeadDay`, `TestDeadlines_InsurancePolicyBlock`, `TestNotify_Attachments`,
`TestMagicLink_RotateRevokes`, `TestPublicLinks_CrossMarina`, **`TestConcurrency_OTPAttempts`** (100 parallel wrong
codes ⇒ exactly 5 counted attempts, then `otp_locked`; one parallel correct code among wrong ones ⇒ at most one
signature), **`TestConcurrency_SignatureCompleteOnce`** (two last signers in parallel ⇒ one signed document, one
`OnSignedTx`), **`TestConcurrency_DeadlineSyncParallel`** (4 parallel syncs ⇒ no duplicate rows).

Every handler file has a `_test.go` asserting status and problem `type` per error row of §7.3 and a cross-marina 404.

### 9.4 Acceptance tests (`internal/acceptance`, tag integration, API only, `clock.Fake`, in-memory `fixture.Mailbox`)

"Without DB intervention" means: the test writes nothing to the database except through HTTP API calls (admin and
public), advances only the fake clock and runs jobs through `h.runJobs`; it reads emails from the mailbox as the
customer would; it may **read** the database for invariant checks.

**`TestAcceptance_M4_RenewalCampaign300`** (CONTRACTS; spec 32.3 M4; both dialects; ≤ 10 min on PostgreSQL):
1. Owner sets up (API, M1–M3 helpers): 2 piers × 170 berths, 330 customers with emails (locales it/en), vessels with
   insurance documents (5 with insurance expiring before the new start), annual price list Y and its renewal Y+1
   (+5 %, M2 renewal API, activated), contract settings: signature `otp`, policy deposit 30 % due in 7 days + 3
   quarterly instalments, insurance policy `warn`; office creates and activates **310 annual contracts** for Y
   (`Y-01-01..Y+1-01-01`); clock at `Y-10-01 09:00 Europe/Rome`; 5 contracts get an overdue unsettled instalment
   (issued in the past, not settled).
2. Office creates a campaign (filter: annual, ends in Y+1-01-01, both piers, deadline `Y-11-15`), **preview** ⇒ 300
   `previewed` with old/new prices (new = engine price for Y+1, asserted against `pricing/simulate`), 10 `excluded`
   (5 `arrears`, 5 `insurance_expired`); every previewed proposal has a successor draft with an active `hold` on the
   same berth until `Y-11-15` end (CheckOccupancyInvariants ∅).
3. **Launch**; `renewals.sync` runs (twice, concurrently once) ⇒ exactly 300 emails `renewal_proposal` with the PDF
   attached, one magic link each; campaign `open`.
4. For 280 customers, through the **public API** with the token parsed from their email: resolve, download the PDF,
   accept (260 `standard`, 20 `early_payment`), request OTP, read the `signature_otp` email, verify (10 of them first
   send a wrong code once and see `attempts_left = 4`) ⇒ successor `active`, `contract.signed`, signed document with
   evidence (sha256 of the decrypted file = row; the unsigned PDF is a byte prefix), allocations `contract`, first
   instalment `issued` with the `installment_payment_request` email. 10 customers decline via the link (holds
   released at once); 10 do nothing.
5. Accounting settles the 280 deposits through `POST installments/settle` in 3 bulk calls (≤ 100 each; one bulk
   call is sent twice with the same `Idempotency-Key`) ⇒ 280 proposals `completed`.
6. Clock to `Y-11-16 00:05`, `renewals.sync` ⇒ 10 `expired`, their successor drafts `cancelled`, holds gone, berths
   free for Y+1 (a transit search for Y+1-01-10 on those berths finds them).
7. Clock to `Y+1-01-01 00:10`, `contracts.expire` ⇒ 280 source contracts `renewed` (`contract.renewed`), the other
   30 `expired`; `installments.issue` at the quarterly issue dates issues the next instalments.
8. Assertions: Σ instalments = total for all 280 successors (INV-31; early-payment ones = 1 instalment with the
   discount adjustment, INV-27); every status change has its audit row and outbox event (counted per type); outbox
   data and logs contain no email, name or token (INV-70); `CheckOccupancyInvariants` ∅; no proposal open; campaign
   closed by the job after the deadline; counts printed with `t.Log`.

**`TestAcceptance_M4_CommonCharges400`** (RIGHTS-CHARGES; spec 32.3 M4; both dialects):
1. Harbour of 400 berths on 5 piers (API), concession expiring `Y+40-12-31`; 370 rights created through the import
   API (CSV) and 10 through the API: 300 single holders, 60 with 2 holders (500/500 and 333/667), 20 with 3 holders
   (333/333/334); 20 berths stay the marina's.
2. Three millesimal tables imported by CSV: `general` over 400 berths with uneven millis (seeded generator summing to
   exactly 1000, includes zeros), `pier_B` over 80 berths, `services` over 400; activation of a table with Σ = 999 ⇒
   `422 table_incomplete`; fix and activate.
3. Budget Y with 7 items on the three tables (totals with odd cents, e.g. 1 234 567, 98 765 43, 1); approve; advance
   run `share_bp = 5000` ⇒ for every item Σ allocations = `round_half_even(item × 0.5)` exactly; Σ over all holders
   + marina share = Σ items exactly; issue ⇒ holder instalments Σ = run total.
4. Actuals via CSV (different from budget, one item lower than advances), balance run ⇒ Σ balance = Σ actual − Σ
   advances exactly; credits negative for the lower item's holders; statements PDF per holder (one sampled) and per
   item render and their golden totals match the API.
5. A transfer of one co-held right during the year and a mandate on another do not change the sums (reference date
   rule §15 D17); concurrency: two `issue` calls on the balance run ⇒ one 200, one 409.
6. `CheckOccupancyInvariants` ∅ (owner rows for all active rights); INV-40 for every right; no PII in events.

**`TestAcceptance_M4_QuoteToContract`** (CONTRACTS): quote with 2 options sent by email (PDF attached, link); the
customer uploads an insurance policy and accepts option 2 via the link with missing address data ⇒ contract
`awaiting_signature`, plan created, PDF/A document; signs with OTP ⇒ `active` automatically, first instalment
issued; office settles; a second quote accepted from the panel with `paper` (`require_paper_scan`) ⇒ upload scan +
confirm ⇒ `active`; an addendum (parking) signed by OTP replans the future instalments; a termination shows the
refund amount; dock role: 403 on settlement; viewer: 403; another marina's ids ⇒ 404.

**`TestAcceptance_M4_RightsLifecycle`** (RIGHTS-CHARGES): right with 2 holders ⇒ owner rows; mandate July–August ⇒
nights released, a transit sold on the berth inside the mandate, `mandates.accrue` ⇒ revenue row and statement;
transfer with OTP signatures of 3 signers in sequence ⇒ new right chain, owner rows moved, future fee instalments
reassigned; fee run simulate (excluded one with reason) → issue → holder instalments; diritti lens endpoint returns
the expected statuses.

**`TestAcceptance_M4_DocumentsDeadlinesComms`** (DOCUMENTS): template version created with a field error (422 with
line/col), fixed, previewed on demo and on a real contract, published; generated document records the version and
hash; deadline sync run twice ⇒ same rows; lead-day notifications sent once per lead day and channel across clock
advances; insurance policy `block` stops a contract creation with `insurance_required`; a promotional communication
to a filter skips customers without marketing consent; unsubscribe from the link removes the customer from the next
promotional send but not from a service send; map print returns a PDF.

`TestAcceptance_CrossMarinaIsolation`, `TestAcceptance_CrossMarinaResourceIds` and `TestPublicLinks_CrossMarina`
cover every new route and id parameter.

### 9.5 Web tests

Vitest (WEB): campaign wizard reducer (filter → preview → message → launch), proposals table states, plan preview
from a policy (calls the API, renders Σ = total), settle dialog mapping, templates editor (field tree insertion,
error markers by line, preview blob handling), millesimal grid live Σ and CSV parse, holders Σ‰ validation, rights
lens (fixture → tokens/legend counts), silhouette parity (`testdata/silhouette`), signing page states (OTP input,
attempts left, locked, rate limited), communications audience builder, booking SPA routes by purpose.

Playwright (`make e2e`, WEB; helpers `e2e/support/contractsM4.ts`, `e2e/support/rights.ts`, `e2e/support/links.ts`
with fixed signatures written by CONTRACT as stubs):
```ts
export async function setupRenewalScenario(page: Page, opts?: { contracts?: number }): Promise<{ marinaId: string; contractIds: string[] }>;
export async function latestLinkFromMail(request: APIRequestContext, to: string, purpose: 'quote'|'renewal'|'signature'|'unsubscribe'): Promise<string>; // via the dev mail dir (M0 mail.ts)
export async function latestOtpFromMail(request: APIRequestContext, to: string): Promise<string>;
export async function setupRightsHarbour(page: Page, opts?: { berths?: number }): Promise<{ marinaId: string; berthIds: string[]; rightIds: string[] }>;
```
Specs: `renewal-campaign.spec.ts` (wizard on 20 contracts, launch, customer accepts and signs on a 390×844 viewport
in a second context, office settles, monitor updates via SSE within 3 s), `quote-acceptance.spec.ts` (send, public
acceptance with upload, OTP signing, contract active), `installments.spec.ts` (scadenzario filters, bulk settle,
CSV download), `contract-signature.spec.ts` (paper scan upload + confirm; addendum), `charter.spec.ts` (fleet,
charter, rotation drag), `rights.spec.ts` (right wizard with holders, mandate, transfer), `common-charges.spec.ts`
(tables CSV import, Σ indicator, budget, advance run, statements), `templates.spec.ts` (edit, error marker, preview
renders, publish), `communications.spec.ts` (audience from map selection, preview, send, unsubscribe page),
`deadlines-m4.spec.ts`, `imports.spec.ts`, **`contracts-m4-acceptance.spec.ts`** (the M4 scenario end to end from the
UI: quote → link → sign → active → settle → renew → campaign of 10 → sign → deposit; axe on every new page in light
and dark).

### 9.6 Done criteria per builder

Each builder finishes with its subset green (`go test` of its packages, `-tags=integration` on both dialects, `-race`
for concurrency, `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test`, its e2e
specs) and runs the `invariants-guardian` subagent (CONTRACTS, RIGHTS-CHARGES, DOCUMENTS). The orchestrator runs §9.1
in full.

---

## 10. Demo seed (`internal/app/seed_contracts_m4.go`, CONTRACTS; `seed_rights.go`, RIGHTS-CHARGES; `seed_documents.go`, DOCUMENTS)

Runs after the M3 seed when the demo marina has no instalments (idempotent; second run no-op), through the services
as the demo owner, seeded `math/rand/v2` (PCG 41, 5), app clock T:
- CONTRACTS: instalment plans for every active M3 contract (default policy: deposit 30 % + 3 quarters; past
  instalments `paid` through the manual driver with method `bank_transfer`, 6 `issued` overdue), contract settings
  (driver `otp`, bank details demo IBAN `IT60X0542811101000000123456`), one renewal campaign for next year (preview
  done: ~180 previewed, exclusions; launched with 40 sent, 12 accepted of which 8 signed and 5 completed, 3 declined),
  2 tacit-renewal contracts, 1 charter fleet of 8 vessels with a seasonal charter on 8 berths of pier E, 3 addenda
  (one awaiting signature), 6 waiting-list entries.
- RIGHTS-CHARGES: concession (expiry T+30 years, 4 obligations), 60 rights on pier F (12 co-held), 4 mandates (one
  current), revenue accrued, 1 completed transfer, fee table per m² and an issued run for the year, 2 millesimal
  tables (general, pier F), budget with 6 items, advance run issued.
- DOCUMENTS: built-in templates materialised as marina templates for `contract_annual` (it/en) with one customised
  version, generated documents for the sent proposals and signed contracts, 1 service and 1 promotional
  communication sent, deadline sync run.
- Prints M4 counts through `SeedResult`; `TestSeed_DemoM4Coherent`: CheckOccupancyInvariants ∅, INV-31/40/41/42 on
  the seeded data.
- Deferred after fix round 1: the 2 tacit-renewal contracts and the charter fleet with its seasonal charter on pier
  E are not seeded yet (pier E is densely occupied by the M3 seed: the 8 free berths and a tacit policy override per
  contract need their own change). `TestSeed_DemoM4Coherent` covers what is seeded.

---

## 11. Invariants (`docs/invariants.md`; CONTRACT writes the entries, the orchestrator flips statuses)

- INV-31 → implemented (`TestInstallmentPlan_SumsToTotal`, `TestImport_ContractsResidualPlan`; holder instalments
  included).
- **INV-32 — Rate pagate solo dal saldo.** An instalment (contract or holder) becomes `paid` only through
  `contract.SettleInstallment` / `rights.SettleHolderInstallment` (manual driver in M4, payment matching in M5).
  Verified by `TestInstallment_SettleOnlyManualDriver`, `TestConcurrency_InstallmentSettle`. Stato: partial (M5
  payments).
- **INV-33 — Una sola proposta di rinnovo aperta per contratto**, DB partial unique. `TestConcurrency_RenewSameContract`,
  `TestMigrations_M4Constraints`. Stato: implemented.
- **INV-34 — Contratto attivo dopo firma ha il documento firmato.** Every contract that went through
  `awaiting_signature` and is `active` has `signed_document_id` pointing to a `ready`, `signed` document whose
  sha256 matches the decrypted file; signed documents are immutable. `TestConcurrency_SignVsCancelContract`,
  `TestDocs_SignedNeverRerendered`, query Q18 (CONTRACTS adds to `store.CheckContractInvariants`). Stato: implemented.
- **INV-35 — Rinnovato solo con successore attivo.** `renewed` ⇒ an `active` (or later-state) successor exists.
  `TestContract_ExpireMarksRenewed`, `TestAcceptance_M4_RenewalCampaign300`. Stato: implemented.
- INV-40, INV-41, INV-42 → implemented (tests of §9.2/§9.3).
- **INV-43 — Ogni diritto attivo occupa il suo posto.** Every active right has `owner` rows for each night in
  `[max(from_on, first night written), to_on]` except mandate nights (query Q17 of `CheckOccupancyInvariants`,
  RIGHTS-CHARGES). `TestRight_OwnerOccupancy`, `TestAcceptance_M4_CommonCharges400`. Stato: implemented.
- **INV-44 — Ripartizioni emesse immutabili.** Allocations of issued common-charge runs and lines of issued fee runs
  never change (triggers). `TestMillesimal_ActiveImmutable`, `TestMigrations_M4Constraints`. Stato: implemented.
- INV-60 → implemented (`TestAccrual_SumsToAmount`).
- INV-70 partial with the M4 samples; INV-73 covers the M4 routes; INV-74 extended to generated and signed documents
  (`TestDocs_ComposeRenderArchive`).
- **INV-75 — Link dei clienti solo per il proprio soggetto.** A magic-link token resolves only its marina, purpose,
  subject and customer; expired or revoked tokens answer 404 like unknown ones; tokens are stored hashed.
  `TestPublicLinks_CrossMarina`, `TestMagicLink_RotateRevokes`. Stato: implemented.

---

## 12. Order of work inside each builder

**CONTRACT** 1. Migrations 00007–00009 both dialects + `TestMigrations_M4Constraints`. 2. Store models, backup
registration and round-trip seeds. 3. `go get github.com/go-pdf/fpdf@v0.9.0 golang.org/x/image@latest` (orchestrator
approved here; `make licenses` must pass). 4. OpenAPI §7, `make gen`; roles, events/payloads/samples, topics, SSE
web topics, problems, errmap, meta. 5. Skeletons with sentinels and exported signatures: `schedule`, `allot`,
`accrual` (types + stubs), `docs` (`ComposeTx`, `DataSource`, `views/**` complete), `docs/{tmpl,markdown,layout,pdf}`
(doc.go + types), `signature` (`Subject`, `Driver`, service skeleton), `magiclink`, `comms`, `rights`,
`csvimport` (generic reader + `ImportReport` + registry of entity handlers), contract M4 state tables in
`states.go`, `pkg/ext` SignatureProvider. 6. Handler stubs `internal/http/admin/m4_stubs.go`,
`internal/http/public/m4_stubs.go`, `internal/http/webhooks/signature.go` stub and router mount; app wiring and job
registration with no-op implementations; seed hooks. 7. depguard pure list. 8. Web foundations: shadcn components,
i18n namespaces (`docsEnums` complete), query keys, SSE, router/nav, route and public stubs, booking SPA route
skeleton, e2e helper stubs. 9. `docs/invariants.md`, CHANGELOG. 10. `make check`, `make test-db` green with stubs.

**CONTRACTS** 1. `schedule` + property test. 2. Instalments store/service, issue job, settlement, scadenzario, CSV.
3. Quote send + public acceptance + 13.3 steps 3–5 with fakes of `docs`/`signature` until DOCUMENTS lands. 4. States
M4 (signature hooks, suspend/resume, termination amounts). 5. Simple renewal, campaigns, proposals, sync job,
expiry → renewed, tacit. 6. Addenda, change of vessel, charter, waitlist. 7. Contracts import. 8. Handlers, public
quote/renewal handlers, views; delete stub methods. 9. Integration and concurrency tests. 10. Acceptance (quote to
contract, renewal campaign 300). 11. Seed.

**RIGHTS-CHARGES** 1. `allot`, `accrual` + property tests. 2. `occupancy/owner.go` and Q17. 3. Rights, holders,
vessels. 4. Concession. 5. Fee tables and runs. 6. Millesimal tables, budgets, runs, statements. 7. Holder
instalments, settlement, `IssueDueTx`. 8. Mandates and revenue job. 9. Transfers (signature subject). 10. Rights
import. 11. Handlers, map rights endpoint. 12. Tests, acceptance (400 berths, lifecycle). 13. Seed.

**DOCUMENTS** 1. `tmpl` + fuzz, `markdown`, `layout` + golden. 2. `pdf` (fonts, determinism, PDF/A, ICC, append).
3. Templates store/service, catalogue, defaults it/en, preview. 4. Compose/render pipeline + job. 5. `magiclink`.
6. `notify` attachments + M4 email templates. 7. Signature (otp, paper, provider adapter, webhook, sync job).
8. Deadline engine (sources, sync, notify, insurance helper, policy channels). 9. Communications + unsubscribe.
10. Customers/vessels and deadlines imports. 11. Map print + Go silhouette. 12. Handlers, public signing and
unsubscribe. 13. Tests, acceptance. 14. Seed.

**WEB** 1. Hooks. 2. Contract detail tabs, instalments, settle. 3. Booking SPA link pages (unblocks e2e of the other
flows). 4. Renewal wizard and monitor. 5. Templates editor. 6. Rights, transfers, mandates, fees, common charges,
concession. 7. Charter, waitlist, addenda. 8. Signatures, communications, deadlines, imports, settings. 9. Lens
Diritti d'uso, map print. 10. Vitest, e2e helpers, specs, acceptance spec last.

---

## 13. Strict file ownership

Order: **CONTRACT** runs first, alone, and finishes when `make gen && go build ./... && make check` pass with stubs.
Then **CONTRACTS**, **RIGHTS-CHARGES**, **DOCUMENTS**, **WEB** run in parallel. Generated files belong to CONTRACT,
which reruns `make gen` on request. Files not listed belong to the orchestrator and are read-only for builders:
`docs/spec/**`, `docs/adr/**`, `docs/m*-plan.md`, `docs/architecture.md`, `README.md`, `AGENTS.md`, `CLAUDE.md`,
`assets/**`, `.claude/**`, `.github/**`, `deploy/**`, `go.mod`/`go.sum` (after CONTRACT), and every M0–M3 file not
listed below.

### 13.1 CONTRACT (then frozen for M4 unless the orchestrator reruns it)

| File | Change |
|---|---|
| `api/openapi.yaml`; `internal/http/api/api.gen.go`; `web/shared/api/generated/**` | §7, `make gen` |
| `go.mod`, `go.sum` | `github.com/go-pdf/fpdf v0.9.0` (MIT), `golang.org/x/image` (BSD-3) |
| `internal/migrations/{sqlite,postgres}/00007_contracts_m4.sql`, `00008_rights.sql`, `00009_documents.sql`; `internal/migrations/*_test.go` | §5 |
| `internal/store/contract_m4_models.go`, `rights_models.go`, `docs_models.go` | Bun structs (then owned by CONTRACTS / RIGHTS-CHARGES / DOCUMENTS) |
| `internal/store/backup_tables.go`, `internal/store/backup_integration_test.go`, `internal/backup/**` | §5.4 |
| `internal/domain/auth/roles.go` + `TestRoles_MatrixMatchesSpec` copy | §7.2 |
| `internal/domain/event/{event.go,payloads.go}` (+ samples) | §7.5 |
| `internal/http/sse/sse.go`, `sse_test.go` | new topics allowed |
| `internal/domain/meta/meta.go` | §7.6 |
| `internal/http/problem/types.go` + test, `internal/http/respond/errmap.go` + test | §7.4 |
| `internal/http/admin/admin.go`, `internal/http/admin/m4_stubs.go`; `internal/http/public/public.go` (Handler fields), `internal/http/public/m4_stubs.go`; `internal/http/webhooks/signature.go` (stub), `internal/http/server.go` (mount `/webhooks/signature/…`, access-log redaction of link tokens) | every new operation answers 501; builders delete their own methods from the stub files; the last deletes the file |
| `internal/app/app.go` | wiring, subject and data-source registration, job registration (§7.6) |
| `internal/app/seed.go` | calls `seedContractsM4`, `seedRights`, `seedDocuments` after M3; `SeedResult` M4 counts |
| `.golangci.yml` | pure list (§0.7) |
| `pkg/ext/ext.go` | `SignatureProvider` + types + `Extensions.SignatureProviders` |
| `internal/domain/schedule/{doc.go,types.go}`, `internal/domain/allot/{doc.go,types.go}`, `internal/domain/accrual/{doc.go,types.go}` | types and stub functions |
| `internal/domain/contract/{states.go,errors.go}` | §3 tables and sentinels (then CONTRACTS, which may not change the tables without the orchestrator) |
| `internal/domain/rights/{service.go,errors.go,types.go,states.go}` | skeleton |
| `internal/domain/docs/{service.go,errors.go,types.go,datasource.go}`, `internal/domain/docs/views/**` (complete, frozen), `internal/domain/docs/{tmpl,markdown,layout,pdf}/doc.go` | skeleton |
| `internal/domain/signature/{service.go,errors.go,subject.go,driver.go}`, `internal/domain/magiclink/{service.go,errors.go}`, `internal/domain/comms/{service.go,errors.go}` | skeleton |
| `internal/csvimport/**` | generic reader, mapping, report, run store calls, entity handler registry (then DOCUMENTS owns the package; entity handlers live in each owner's domain package) |
| `web/shared/src/ui/{textarea,progress,input-otp,resizable,…}.tsx` + barrel | shadcn CLI |
| `web/shared/src/i18n/resources.ts`, `locales/{it,en}/{common,errors,docsEnums}.json` + empty §8.3 namespaces | §8.3 |
| `web/shared/src/api/{queryKeys,sse,index}.ts` (+ tests) | §8.2, §7.5 |
| `web/shared/src/api/hooks/{contractsM4,rights,documents,publicLinks}.ts` | empty stubs |
| `web/admin/src/router.tsx`, `nav.ts`, `nav.test.ts` | §8.1 factories and nav |
| `web/admin/src/features/{contracts-m4,rights,documents}/{routes.tsx,public.ts}`, `web/booking/src/{routes.tsx,links/index.tsx}` | stubs |
| `e2e/support/{contractsM4,rights,links}.ts` | §9.5 signatures, stubs |
| `internal/acceptance/acceptance_test.go` | route-count threshold only |
| `docs/invariants.md`, `CHANGELOG.md` | §11 entries, M4 entries |

### 13.2 Builders

| Owner | Files |
|---|---|
| **CONTRACTS** | `internal/domain/schedule/**`; `internal/domain/contract/**` (keeps sentinels, statuses and §3 tables; new files `installments.go`, `settle.go`, `renewals.go`, `campaigns.go`, `proposals.go`, `tacit.go`, `charter.go`, `addenda.go`, `vessel_change.go`, `termination.go`, `waitlist.go`, `signing.go` (Subject impl), `docdata.go` (DataSource impl for its kinds), `public.go`, `import.go`); `internal/store/{installments.go,renewals.go,charter.go,addenda.go,waitlist.go,contract_settings.go,contract_invariants.go}`, `internal/store/contract_m4_models.go` (after CONTRACT), `internal/store/contracts_m4_integration_test.go`; `internal/http/admin/{installments,renewals,charter,addenda,waitlist,contract_settings}.go` + tests and the M4 edits of `quotes.go`, `contracts.go`; `internal/http/public/links_contracts.go` (quote/renewal operations of §7.3.6 incl. `resolvePublicLink` dispatch and `uploadPublicDocument`); `internal/http/view/contracts_m4.go`; `internal/app/seed_contracts_m4.go`; `internal/acceptance/m4_{renewal,quote}_test.go`, `internal/acceptance/testdata/m4/contracts/**` |
| **RIGHTS-CHARGES** | `internal/domain/allot/**`, `internal/domain/accrual/**`, `internal/domain/rights/**`; **`internal/domain/occupancy/owner.go`** (new: `AllocateOwnerTx`, owner truncate/split helpers; may call unexported helpers of the package, must not change existing exported signatures or other files of `occupancy` except the next item) and the Q17 addition in `internal/store/occupancy_invariants.go` (new function `checkRightsOccupancy` + one call line; nothing else in the file); `internal/store/{rights.go,mandates.go,fees.go,common_charges.go,holder_installments.go,concession.go}`, `internal/store/rights_models.go` (after CONTRACT), `internal/store/rights_integration_test.go`; `internal/http/admin/{rights,mandates,fees,common_charges,concession}.go` + tests; `internal/http/view/rights.go`; `internal/app/seed_rights.go`; `internal/acceptance/m4_{common_charges,rights}_test.go`, `internal/acceptance/testdata/m4/rights/**` |
| **DOCUMENTS** | `internal/domain/docs/**` except `views/**`; `internal/domain/signature/**`, `internal/domain/magiclink/**`, `internal/domain/comms/**`, `internal/domain/deadline/**`, `internal/domain/notify/**` (M4 additions; M0 templates unchanged); `internal/csvimport/**` (after CONTRACT) and the customers/vessels and deadlines entity handlers (`internal/domain/customer/import.go`, `internal/domain/vessel/import.go`, `internal/domain/deadline/import.go`: new files only); `internal/store/{templates.go,generated_documents.go,signatures.go,access_links.go,comms.go,deadline_sources.go,imports.go}`, `internal/store/docs_models.go` (after CONTRACT), `internal/store/deadlines.go` (M4 additions), `internal/store/docs_integration_test.go`; `internal/http/admin/{templates,generated_documents,signatures,communications,imports}.go` + tests and the M4 edits of `deadlines.go`; `internal/http/public/{signing,unsubscribe}.go`; `internal/http/webhooks/**` (after CONTRACT); `internal/http/view/docs.go`; `testdata/silhouette/**`, `internal/domain/docs/testdata/**`; `internal/app/seed_documents.go`; `internal/acceptance/m4_documents_test.go`, `internal/acceptance/testdata/m4/docs/**` |
| **WEB** | `web/admin/src/features/{contracts-m4,rights,documents}/**`; M4 edits inside `web/admin/src/features/desk/{contracts,quotes}/**` (detail tabs, send/accept dialogs) and `web/admin/src/features/registry/deadlines/**`; `web/admin/src/features/operations/**` only for the Diritti d'uso lens wiring and the print action (lens function in `web/shared/src/map/lenses/rights.ts` + test, new file; `lenses/index.ts` one registry line); `web/shared/src/api/hooks/{contractsM4,rights,documents,publicLinks}.ts`; `web/shared/src/map/vessel-shape.parity.test.ts` (new, reads `testdata/silhouette`); `web/booking/src/**` (after CONTRACT stubs); `web/shared/src/i18n/locales/{it,en}/{contractsM4,renewals,charter,rights,commonCharges,templates,signature,communications,imports,customerLinks}.json` and `deadlines.json` additions; `e2e/tests/{renewal-campaign,quote-acceptance,installments,contract-signature,charter,rights,common-charges,templates,communications,deadlines-m4,imports,contracts-m4-acceptance}.spec.ts`, `e2e/support/{contractsM4,rights,links}.ts` (after CONTRACT), `e2e/fixtures/m4/**` |

### 13.3 Rules for the parallel phase

- **Interfaces first.** CONTRACTS and RIGHTS-CHARGES code against `docs.ComposeTx`, `docs.DataSource`,
  `signature.Service.OpenTx`, `signature.Subject`, `magiclink.Service.CreateTx`, `deadline.InsuranceValid` as written
  by CONTRACT; until DOCUMENTS lands, their unit tests use fakes in their own packages (`contract/fakes_test.go`,
  `rights/fakes_test.go`); integration tests and acceptance run once DOCUMENTS is in. DOCUMENTS never imports
  `contract` or `rights`.
- **Data sources**: each owner implements `docs.DataSource` for its kinds (§6.2) filling the frozen `docs/views`
  structs; DOCUMENTS owns the catalogue derivation and the default templates, and must not rename view fields (a
  change goes through the orchestrator → CONTRACT).
- **Occupancy**: only RIGHTS-CHARGES touches `internal/domain/occupancy`, and only `owner.go` + the Q17 hook.
  CONTRACTS uses existing primitives (`AllocateTx`, `ConvertTx`, `TruncateTx`, `CancelTx`, `MoveTx`) for renewals,
  charter rotation, change of vessel and addenda; if a primitive is missing, it asks the orchestrator.
- **Instalment issue job**: CONTRACTS owns `installments.issue` and calls `rights.Service.IssueDueTx` (exported by
  RIGHTS-CHARGES with the signature in CONTRACT's skeleton) for holder instalments.
- **Public handlers**: all in `package public`; CONTRACTS owns `links_contracts.go` (and the purpose dispatch of
  `resolvePublicLink`, which calls `signature.PublicView`/`comms.PublicView` for the other purposes), DOCUMENTS owns
  `signing.go` and `unsubscribe.go`. Only the stub files are shared (each deletes its own methods).
- **Admin handlers**: split exactly as §7.3; all in `package admin`.
- **Store**: one file per area as listed; M1–M3 store files are not edited except those named above.
- **Imports**: `csvimport` (DOCUMENTS) defines `EntityHandler{DryRun, Apply, Undo}`; CONTRACTS implements
  `contracts` in `contract/import.go`, RIGHTS-CHARGES `rights` in `rights/import.go`; registration in `app.go`
  (CONTRACT).
- **Web**: feature folders are disjoint; WEB is a single builder and owns the whole M4 UI; it imports map pieces only
  through `@porto/shared/map` and `features/operations/public.ts`. i18n: only the namespaces listed; `docsEnums`,
  `common`, `errors` through the orchestrator.
- **go.mod / package.json**: no dependency beyond §13.1 without the orchestrator's approval.

---

## 14. Spec and repository notes for the orchestrator

- `docs/architecture.md` §10 lists `docai` for M4: remove it (ADR 0008) and add the M4 packages of §2.
- `docs/spec/27-api-eventi-estensioni.md` 27.4 topics: `rights.changed` and `documents.changed` are additive (§15
  D21); consider recording them in the spec or an ADR at the end of M4.
- ADR 0005 is "proposta": the PDF/A choices of §6.3 (own ICC, incremental update, Go fonts) could be recorded when the
  ADR is accepted.

---

## 15. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | **Deposit in M4 = issued instalment settled by the manual driver.** "Acconto" (13.3 step 5, 32.3 acceptance) is the first instalment `issued` at signature with a payment request email carrying bank details; the office or accounting records it with `SettleInstallment` (`payments.collect`). That entry point is the one M5 payment matching and the Stripe webhook will call (`settlement_source = payment`). No charge, no money ledger in M4. |
| D2 | **Instalment rounding**: parts are floored to the rounding unit (default 1 cent), the remainder goes to the last instalment (13.7, INV-31); deposits in bp use half-even. Floor (not half-even) for the parts guarantees the last instalment is never negative. |
| D3 | **Payment policy** lives in `contracts.policy` v2 (no new table), defaults from `contract_settings.default_policy`; per-price-list policies are not in M4. v1 policies are read as v2 with the defaults. |
| D4 | **Renewal proposals hold the berth from preview** (prelazione starts when staff see the preview, not at send), so the preview prices and availability are what the customer receives; discarding the draft campaign releases everything. |
| D5 | **Renewal exclusions**: arrears = an instalment `issued` and overdue > `arrears_days` (30) or an overdue holder instalment; insurance per 11.5 on the new start date; staff can include excluded proposals with a reason except `berth_unavailable`. |
| D6 | **Renewal deadline** default = launch + `renewal_deadline_days` (30); the proposal link expires at the deadline end; signature started before the deadline may finish until the signature request expiry (`signature_days`), after which the proposal expires. |
| D7 | **Charter**: `contracts.vessel_id` holds the fleet's lead vessel (NOT NULL column of M3); the fleet and charter fields live in `charter_contracts`; each resource line always has a concrete fleet vessel in its allocation; rotation never reprices. |
| D8 | **Change of vessel and charter rotation** truncate and re-allocate the same contract line (no `moved_from_id`, because INV-11 requires the same vessel on a chain). |
| D9 | **Suspension** is manual in M4 (contracts.write); the automatic arrears policy is M5. Suspended contracts keep their allocations; the dock sees the status. |
| D10 | **Termination refund/penalty** is computed and stored (`termination_amount_cents`), never charged in M4. |
| D11 | **Paper signature**: `require_paper_scan` (default true) requires an uploaded scan before "firmato su carta"; with false the M3 one-click activation remains. The signed archive row is the original PDF plus a cover page appended by incremental update, plus the scan file. |
| D12 | **Signature after a lost hold**: the signature is recorded (the customer's act is valid), the contract goes back to `draft` with `hold_lost`, the office gets a deadline `signature_expiring`-like item and must reactivate (re-allocate or move). Holds of contracts in `awaiting_signature` are extended to the signature expiry so this is rare. |
| D13 | **OTP**: 6 digits, HMAC-stored, 10-minute TTL, 5 attempts per code, 3 codes per 15 minutes per signer; email channel always, `sms` only through a registered `ext.Notifier`; identity confirmation is a checkbox over the stored data (no identity verification service); the evidence page and docs call it "firma elettronica semplice" (D6 of spec 33 stays open). |
| D14 | **PDF/A-2b** by own incremental-update finaliser over fpdf output with an own-generated sRGB ICC profile and Go fonts (BSD); no external validator in CI; structural markers tested. The evidence page is appended to the finalised PDF so the unsigned PDF is a byte-exact prefix of the signed one. |
| D15 | **Rights are on berths only** (spec 14.1 "posto"); rights on dynamic areas or land resources are out. Owner allocations carry the primary authorised vessel or NULL (the existing CHECK allows it); other authorised vessels moor after switching the primary vessel. |
| D16 | **Mandates** remove `owner` rows for the mandate nights (rather than using `releases`, whose `contract_id` is NOT NULL); revenue = accrual of each sold contract's total over its nights, intersected with the mandate; compensation and payouts are M5. |
| D17 | **Reference date of the holders** for common charges: advances use the holders on `year-01-01`, balance on `year-12-31` (a mid-year transfer moves the balance to the new holder; pro rata between holders is not applied to common charges); fees use day pro rata (14.4). |
| D18 | **Marina-owned berths** in a millesimal table receive their share with `customer_id = NULL` (the marina's own cost), so allocations always sum to the item total exactly. |
| D19 | **Holder instalments** are a separate table from contract instalments (different owners and sources), same statuses, same settlement rule, same issue job; negative amounts only for common-charge balance credits. |
| D20 | **Templates**: own closed handlebars-like language evaluated before Markdown parsing with escaped values; field catalogue derived from typed view structs (one source of truth for validation, documentation and data sources); built-in defaults embedded in the binary for it/en; marina templates are versioned per locale and published versions are immutable (trigger). |
| D21 | **New SSE topics** `rights.changed` and `documents.changed` (spec 27.4 is an extract); deadlines keep `registry.changed`. |
| D22 | **SMS/WhatsApp** only through registered `ext.Notifier`s, best effort, not recorded in `message_log` (its channel CHECK is `email`); the per-customer registry records emails. |
| D23 | **Rendering never inside a write transaction** (SQLite write queue): compose stores an encrypted data snapshot in the tx; rendering runs after commit (request path) with the `documents.render` job as recovery; PDFs are deterministic so retries give the same hash. |
| D24 | **Magic links**: one active link per (purpose, subject, customer), rotated on resend, SHA-256-stored, 404 for unknown/expired/revoked alike, served under `/b/{slug}/m/` with no-referrer/no-store headers, rate limited 30/min. |
| D25 | **New permissions** `templates.configure` (owner, director), `communications.send` (owner, director, office), `providers.configure` (owner); settlement uses the existing `payments.collect`; rights, fees and common charges use `rights.manage` for reads and writes; concession writes `marina.configure`. |
| D26 | **Waiting list** proposes to the office through a `waitlist_offer` deadline (no automatic hold, no automatic email), since 13.9 says the office sends the quote. |
| D27 | **Imports** (11.6, deferred from M1): CSV only, ≤ 5 000 rows, one tx per file, dry run first, undo while records are untouched; Excel and opening balances in M5. |
| D28 | **Deadline engine** is a periodic reconciliation over read-only source queries (no cross-package hooks), idempotent via the existing unique `(source_type, source_id, kind)`; notifications are deduplicated per deadline, lead day and channel. |
| D29 | **Insurance policy** `warn` (default) or `block` in `contract_settings`; `block` stops new contracts, quote acceptance, renewals and tacit renewals for vessels without a valid policy on the start date. |
| D30 | **Map print** (M3 D25) is a DOCUMENTS feature with a Go mirror of the vessel silhouette validated by shared parity vectors in `testdata/silhouette/`. |
| D31 | **Mass campaign transactions**: preview and send process one proposal per transaction (Arenile ADR 0017 pattern); a crash mid-campaign resumes from the job without duplicates (dedupe keys and conditional updates). |
| D32 | **Early-payment discount** is an INV-27 adjustment on the snapshot (`early_payment`), chosen at acceptance or by replan, giving a single instalment. |
| D33 | **`signature_link` on public acceptance (§7.3.6) is not returned in M4**: the signature request opens in the acceptance transaction but its signers are invited only after the document renders (render job, then `signatures.sync`), so no signer link exists yet when the response is written; the customer signs from the emailed link. The field stays in the schema, documented as reserved. Returning it needs a product decision (render synchronously inside the request, or have the acceptance page poll until the link exists); it is left open for the orchestrator. |
