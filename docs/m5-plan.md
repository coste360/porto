# M5 — Addebiti e incassi: work order

Status: Plan phase output. This plan is authoritative for the M5 builders. If it conflicts with `docs/spec/`, the
spec wins: stop and report the conflict to the orchestrator instead of guessing. Open decision **D4** of
`docs/spec/33-decisioni-aperte.md` (target invoicing software) is **not** decided here: the core exports a generic
Italian CSV/Excel file and a summary PDF, `ext.ChargeExporter` exists as an interface with no implementation, and no
vendor format, vendor name or vendor API appears in code, docs or tests. §16 lists every detail this plan decides
where the spec is silent. The state tables of §3–§5 and the invariant rules of §12 are normative: tests are derived
from them.

Sources: spec 02 (M5 rows), 05, 06 (§6.5, §6.7), 09.2 (lens *Pagamenti*), 10.6 (release credits), 11.4 (customer
dashboard), 11.6 (opening balances, Excel), 12.1/12.4 (VAT, natures, `revenue_class`, snapshot), 13.3 step 5, 13.4
(termination adjustment), 13.7, 14.2–14.5 (fees, transfer fee, mandate compensation, common-charge balances), 16
(all), 21.3 (only to know what is M7), 23 (only to know what is M8), 26.1, 27, 28.1/28.3, 29, 30, 32 (§32.1, §32.3 M5
and M6+), 33; `docs/invariants.md` (INV-09, INV-25, INV-27, INV-31, INV-32, INV-50..53, INV-70..77); ADR 0001, 0002,
0005, 0008; `docs/m0-plan.md` .. `docs/m4-plan.md`, `docs/architecture.md`; `.claude/rules/*.md` (`charges.md`,
`go-backend.md`, `migrations.md`, `api-contract.md`, `frontend.md`, `security.md`, `arenile-reference.md`); skills
`new-endpoint`, `new-migration`, `new-domain-event`, `new-payment-driver`, `shadcn`.

## 0. Ground rules for every builder

Everything in `docs/m0-plan.md` §0 .. `docs/m4-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001). Closest analogues: `../arenile/internal/domain/payment`
   (ledger, ports, frozen VAT split, idempotent record), `../arenile/internal/domain/credit`,
   `../arenile/internal/domain/refund`, `../arenile/internal/domain/cash` (sessions, movements, close with
   difference, Z summary), `../arenile/internal/domain/fiscal` (corrispettivi, `none` driver, daily numbering,
   deferred drivers), `../arenile/internal/domain/payment/stripe` and `gateway` (webhook as the only online
   confirmation, dedupe, late and double payments, reconciliation by intent), `../arenile/docs/spec/13-pagamenti.md`,
   `14-cassa-e-modulo-fiscale.md`, ADR `0014-pagamenti-online-m4.md`, `0016-scelte-m5a.md`, `0017-scelte-m5b.md`,
   `0028-cassa-avanzata-e-convenzioni.md`. Read them to understand the behaviour ("come Arenile"), then write
   Porto's own code for charges with lines, instalments, transits and holders. Never copy files, code, comments or
   tests. Never modify anything under `../arenile`.
2. shadcn/ui components only through the official CLI, run in `web/shared` (CONTRACT, §14.1). Tailwind on theme
   tokens, no custom CSS, no `style={}`.
3. Do not `git commit`; no destructive git commands; no `rm -rf`. The orchestrator commits.
4. Edit only files you own (§14). To change a file someone else owns, write the request in your final report.
5. Code, identifiers and comments in English. User-facing strings through i18n keys, Italian source, English
   required (emails and PDFs: `it` and `en` built-in templates). Never log or emit names, emails, phones, fiscal
   codes, VAT numbers, SDI codes, PEC, IBANs, card data, provider secrets, payment references typed by staff,
   payment-link tokens, invoice file contents. Events and SSE carry ids, enums, dates, amounts and counts only
   (INV-70).
6. **Money is `money.Cents`** (`int64`), signed where a sign is meaningful (adjustment charges). Every split uses
   the shared helpers only: `money.Allocate` (largest remainder), `money.VATSplit`, `money.DivRoundHalfEven`,
   `Cents.MulRat` / `PercentBP` (half-even), and the new pure `chargeplan` package (§3.4) for charge lines. No
   floats anywhere in domain, export or fiscal code (Excel cells are written as decimal strings with a numeric
   type, never through `float64`).
7. **Pure packages** (no `store`, `clock`, `time.Now`, `math/rand`, I/O): `internal/domain/chargeplan`,
   `internal/domain/cash/summary`, `internal/domain/collect/levels`, `internal/xlsx` (works on `io.Reader` /
   `io.Writer` only). CONTRACT adds them to depguard `pure-engines` and `pure-engines-no-rand`.
8. **Limits are database guarantees** (INV-52, INV-53, INV-54). Matching, refunds, exports, cash sessions and fiscal
   numbering use conditional `UPDATE … WHERE <limit still holds>` with the affected count checked, CHECK
   constraints and partial unique indexes; **lock before reading what you update** (PostgreSQL READ COMMITTED):
   `store.LockPayment`, `store.LockChargesSorted`, `store.LockCashSession`, `store.LockContract` take
   `SELECT … FOR UPDATE` on PostgreSQL and are no-ops on SQLite (single writer through the write queue). Lock order
   everywhere: **contract → instalments → payment → charges (ascending id) → cash session → sequences**. Never a
   "check then insert" in Go.
9. **API first.** CONTRACT writes `api/openapi.yaml` and runs `make gen`. Builders never edit generated files.
10. **Single state-change points with audit + outbox in the same transaction**: charges only through
    `internal/domain/charge`; payments, matches, refunds and credits only through `internal/domain/payment`;
    intents, payment requests, reminders, SEPA mandates and provider events only through `internal/domain/collect`;
    cash sessions and fiscal documents only through `internal/domain/cash` and `internal/domain/fiscal`; contract,
    transit and instalment states still only through `internal/domain/contract` (INV-30), holder instalments only
    through `internal/domain/rights`. The first line of every exported service method is
    `p.Authorize(ctx, db, marinaID, perm)`; `...Tx` primitives take an authorised `store.MarinaScope` and an actor
    (user or `system:<job>` / `provider:<code>`) and are never called from handlers.
11. Every query scoped by `store.MarinaScope`; foreign ids of another marina are `ErrNotFound` (404). New tables
    have `UNIQUE (id, marina_id)` and composite FKs `(x_id, marina_id)`.
12. Time: `clock.Clock` injected; business dates in the marina timezone (`occupancy.Today(clock, tz)`). Tests use
    `clock.Fake`. Jobs take "now" from the clock. The Stripe signature tolerance is checked against the injected
    clock, never the wall clock.
13. **Offline first.** Porto works with no network: the manual driver is always present and every M5 flow
    (charges, payments, refunds, credits, cash, receipts, corrispettivi, payment requests with bank details,
    reminders, statements, exports, reconciliation) works with it alone. Stripe is an **optional** driver, compiled
    in, **disabled by default** (no settings row = disabled), enabled per marina by `providers.configure`. No call to
    a provider ever runs inside a database transaction (SQLite write queue, m0-plan): intents are written first,
    the provider is called after commit, the outcome is applied in a new transaction (as Arenile ADR 0014 §5).
    Outbound HTTP only through the egress client (`event.NewEgress`), secrets only through the KeyRing.
14. **No invoicing in the core**: no invoice numbers generated by Porto, no credit notes, no proforma, no FatturaPA
    XML, no SdI, no bookkeeping, no bank reconciliation. Porto computes charges, collects money and exports.
    Exported charges are immutable (INV-51); corrections are linked adjustment charges. No AI (ADR 0008), no
    Coste360 integration, no cloud services.

---

## 1. Scope

### 1.1 In M5 (spec 32.3 M5 and the M5 rows of spec 2.2)

**Charges (16.1)**
- `charges` + `charge_lines` from **every M5 origin**: contract instalments (13.7: created by `installments.issue`
  at the issue date, also for deposits at signature), **holder instalments** (14.4 fees, 14.5 common-charge advances
  and balances, 14.2 transfer fees), **transits** (at confirmation, manual driver or provider webhook), **counter
  sales** ("servizio al banco": items sold at the desk or the cash register, with or without a customer), **contract
  true-ups** (residual difference when a contract's last instalment is issued or a reprice happens after it),
  **termination adjustments** (13.4: the stored `termination_amount_cents` proposed and created by staff),
  **deposit retentions** (16.2 cauzioni), **manual charges** (free charge with catalogue items) and **adjustments**
  of any charge. Utilities (20.4), yard work orders (18.6) and rentals (19.5) are later origins (§1.2); the
  `origin_type` CHECK already lists them so no migration is needed then.
- Lines derive from the `price_snapshot` (12.4) through the pure `chargeplan` package: per line item, quantity,
  unit price, discount, VAT rate or nature, revenue class, external code, service period; totals with VAT split per
  rate; **Σ of the charges of an origin per (rate, nature) equals the snapshot target exactly** (§3.4, INV-57).
- Payment status `open → partially_paid → paid`, `cancelled` with reason; invoicing status
  `to_invoice → exported → invoiced`; immutability once exported (INV-51, triggers); corrections by **linked
  adjustment charges** (negative delta or cancel-and-replace) that go to the next export with the link to the
  original.
- **Backfill** of M4 data: issued and paid instalments and holder instalments without `charge_id` get their charge
  (and, when paid, a manual payment of the settled method) once, idempotently (§3.7).
- **Opening balances import** (11.6, deferred from M4): CSV **or Excel** with dry run and undo; imported charges are
  already `invoiced` in the previous software (§3.8).

**Payments, refunds, credits (16.2)**
- `ext.PaymentProvider` registry: driver **`manual`** always present (cash, POS card, bank transfer, cheque, other),
  driver **`stripe`** optional (card and wallets through Checkout, **SEPA Direct Debit** for instalments with a
  mandate, payment links in requests and reminders, refunds through the provider). Other providers as extensions.
- Payments (`kind` payment, deposit, credit) matched to one or more charges, partially too; unmatched amounts stay as
  **customer credit** (credit = Σ unallocated amounts of the customer's payments, as Arenile); matches never exceed
  the payment or the charge residual (INV-52, DB + locks, 100 parallel attempts); match reversals; refunds manual or
  through the provider (never above what is unallocated, including refunds in flight); deposits never revenue until
  retained (retention = charge + match).
- **Credits**: from overpayments, from negative adjustment charges, goodwill credits, release credits (10.6, staff
  grant referencing a release), **mandate compensation** (14.3: owner share settled as credit or as a payout);
  application of credit to open charges.
- **Rewiring of the M3/M4 hooks**: manual settlement of instalments and holder instalments becomes "manual payment
  matched to the instalment's charge" (INV-32: `settlement_source = payment`); transit confirmation with payment at
  the desk; payment link → `pending_payment` → confirmation only from the provider webhook (INV-09).

**Cash and corrispettivi (16.3, "come Arenile")**
- Cash registers per marina (default `main`), sessions with float, payments and refunds by method, movements in/out,
  close with counted amounts and difference, printable Z report; at most one open session per register (DB).
- Fiscal documents through `ext.FiscalDriver`: core drivers `none` (tracking only, internal daily number) and
  `export` (daily corrispettivi file for the accountant); device drivers and the desktop bridge are extensions/M6.
- Daily **corrispettivi** per VAT rate and method, exportable (CSV, Excel).
- Courtesy receipt 80 mm / 58 mm (print-ready HTML) or A4 (PDF) with "Non valido ai fini fiscali".

**Scadenzario, requests, reminders (16.4, 28.1, 28.3)**
- **Scadenzario** of charges and instalments by due date and state (to pay, overdue by age buckets), per customer,
  contract and group; CSV/Excel; bulk actions (send payment request, remind).
- **Payment requests** with a public payment page (bank details always; "Paga online" only when a provider is
  enabled); automatic request at instalment issue (replaces the M4 bank-details email).
- Daily job `payments.reminders`: notice N days before due, on due, then **levels** (courteous, formal, final
  notice) each with its template and payment link; exclusions (repayment plan, disputed charge); every send logged;
  reminders stop as soon as the charge is paid; final notice requires approval in the office worklist.
- **Arrears policy**: beyond a threshold the contract goes `suspended` (automatic, reason `arrears`); lens
  *Pagamenti* on the map; the dock is warned, the exit is never blocked.
- **Office worklist** (28.3) with assignee and state: reminders to approve, charges to export, failed provider
  payments, late payments to refund, SEPA failures, deadlines (M4 engine).

**Statement, dashboard (11.4, 16.4)**
- Customer **statement of account** (JSON, PDF, CSV): charges, payments, refunds, credits, running balance.
- **Customer dashboard** (11.4): vessels, active and past contracts, berths today, **arrears by age** (not due,
  0–30, 31–60, 61–90, > 90), future instalments, credits, payments, expiring documents, communications sent, notes;
  quick actions (new quote, collect, statement PDF, send reminder, send communication). Work orders and rentals
  sections are empty placeholders until M10/M11.
- Receivables overview ("panoramica dei sospesi") by customer with ageing, CSV/Excel.

**Export to invoicing (16.5)**
- **"Da fatturare" export**: `to_invoice` charges of a period, filterable by customer, origin and revenue class;
  **CSV and Excel in Italian** (one row per charge line with the customer's fiscal data, rate, nature, service
  period, contract and vessel reference) and a **summary PDF** for the accountant; exported charges become
  `exported`; explicit re-export; **every charge in exactly one first export** (INV-53, DB partial unique).
- **Invoice reconciliation**: CSV `id addebito ; numero fattura ; data` (or charge number), dry run, report; manual
  entry per charge; `invoiced` is information only, it blocks nothing.
- `ext.ChargeExporter` interface (send charges to an external invoicing software, receive invoice number and date);
  the core ships none (D4).
- **Payments export** by method and period (desk payments, Stripe payments with fees) for reconciliation outside
  Porto.

**Platform**: migrations `00010`–`00012` both dialects (§6), backup registration, permissions (existing constants,
§7.2), events and SSE topic `billing.changed` (§7.5), problem slugs (§7.4), meta capabilities and jobs (§7.6),
`pkg/ext` `PaymentProvider`, `FiscalDriver`, `ChargeExporter` (§5.6), own minimal `internal/xlsx` reader/writer
(no new dependency), public payment page in `web/booking`, demo seed (§10), invariants INV-32, INV-50..53 to
`implemented`, INV-09 extended, new INV-54..58 (§12).

### 1.2 Explicitly out of M5

| Out | Milestone | Note for M5 code |
|---|---|---|
| Invoices, invoice numbering, credit notes, proforma, FatturaPA/SdI, purchase invoices, bookkeeping, bank reconciliation, VAT registers, cost centres | never (spec 16.0, 2.3) | `invoiced` only stores the external number and date. |
| A concrete `ChargeExporter` or vendor-specific export format | after D4 | Generic CSV/Excel only; interface + job plumbing tested with a fake exporter in tests. |
| Fiscal device drivers (registratori telematici), the desktop local fiscal bridge, ESC/POS network printing | extensions / M6 | `ext.FiscalDriver` with `none` and `export`; deferred-driver contract (`Deferred() bool`) documented; receipts are browser-printed HTML. |
| Offline queue of payments on phones (INV-71), desktop edition | M6 | Payment POSTs are idempotent (`Idempotency-Key` + `payments.idempotency_key` unique) so M6 can queue them; no M5 operation is added to an offline allowlist. |
| Billing/revenue/accrual reports, ratei e risconti, revenue-class comparisons, dashboard KPIs | M7 | `charge_lines.service_from/to` and `revenue_class_code` are ready; M5 only lists and exports. |
| Online booking checkout, customer area, cancellation policies of online bookings, `awaiting_approval` | M8 | M5 has the payment link for held transits created by staff and the public payment page; the online checkout reuses `collect` intents in M8. |
| Utility readings and their charges (20.4) | M9 | `origin_type` value `utility` exists in the CHECK; no generator. |
| Storage contracts charges | M9 | They are contracts: instalment charges work unchanged once storage contracts exist. |
| Yard work-order charges (18.6) | M10 | `origin_type` value `work_order` in the CHECK. |
| Rental charges and rental deposits workflows (19.5) | M11 | `origin_type` value `rental`; deposit payments and retention exist generically. |
| Release credit computed automatically from a contract policy | after M5 (needs a policy field, spec 10.6 "secondo il contratto") | Staff grant a credit referencing the release; `releases.credit_cents` is updated in the same tx. |
| Automatic refund of late/duplicate online payments | — (§16 D14) | Late or excess provider money becomes customer credit plus a worklist item; refund is one click. |
| Disputes/chargebacks handling from Stripe | after M5 | `charge.dispute.*` events are recorded as `ignored` with a worklist item. |
| SMS/WhatsApp reminders | extension | Through registered `ext.Notifier`s only (M4 D22 rule). |
| Custom roles | after first version | |
| AI anywhere (ADR 0008) | never | |

---

## 2. Packages

```
internal/domain/chargeplan/      PURE  billable target from a price snapshot + adjustments, cumulative split of an
                                        amount over line remainders, VAT per line by cumulative rounding, totals
                                        per (rate, nature), export totals                                 CHARGES-EXPORT
internal/domain/charge/          charges from every origin (contract/rights hooks impl), true-up, manual charges,
                                        adjustments, cancel, termination charge, backfill, opening balances import,
                                        export + files + re-export, invoice reconciliation, ChargeExporter jobs,
                                        billing settings, origin items, export summary data         CHARGES-EXPORT
internal/xlsx/                   PURE  minimal XLSX writer (inline strings, decimal numbers, dates, bold header,
                                        column widths) and first-sheet reader; stdlib only            CHARGES-EXPORT
internal/domain/payment/         payments (manual + RecordTx for providers), matching + reversals, refunds,
                                        credits, deposits retention, mandate settlement, origin settlement callbacks
                                        (contract/rights), statement, balances, ageing, customer dashboard, map
                                        payment status, worklist, ledger invariants                        PAYMENTS
internal/domain/collect/         payment intents, payment requests + public pay page, provider events (webhook
                                        apply, dedupe), reconciliation job, SEPA mandates + collection, reminders,
                                        exclusions, arrears policy                                      CASH-DRIVERS
internal/domain/collect/levels/  PURE  reminder level selection from due date, today, policy, last sent level
internal/domain/paydriver/       provider registry (core manual + registered), settings decrypt per call
internal/domain/paydriver/manual/  manual driver (no I/O)
internal/domain/paydriver/stripe/  Stripe driver over plain HTTPS (egress client): Checkout, setup mode (SEPA
                                        mandate), off-session SEPA PaymentIntent, refunds, retrieve, webhook
                                        signature + event mapping                                       CASH-DRIVERS
internal/domain/cash/            registers, sessions, movements, close, Z report data, counter sales orchestration,
                                        receipts (HTML/PDF)                                             CASH-DRIVERS
internal/domain/cash/summary/    PURE  expected/counted/difference per method, Z totals per rate      CASH-DRIVERS
internal/domain/fiscal/          fiscal documents via ext.FiscalDriver (none, export), daily numbering,
                                        corrispettivi aggregation and files                           CASH-DRIVERS
pkg/ext/                         + PaymentProvider, FiscalDriver, ChargeExporter and their types       CONTRACT
```

Import graph (cycles are compile errors): `contract` and `rights` do **not** import any M5 package: they call
`contract.BillingHooks` / `rights.BillingHooks` interfaces (CONTRACT writes them and their call sites, §8.1),
implemented by `charge`. `charge → {chargeplan, xlsx, docs (ephemeral render), store}` and calls
`charge.PaymentRequester` (interface, implemented by `collect`) to queue the request email of a new charge.
`payment → {contract, rights, charge (read helpers), docs, store}`: it calls the exported M5 entry points
`contract.Service.ApplyChargeSettlementTx` / `rights.Service.ApplyChargeSettlementTx` (§8.2).
`collect → {payment, charge, contract, paydriver, docs, notify, store}`; `cash → {payment, charge, fiscal, docs,
store}`; `fiscal → {cash/summary, xlsx, store}`; `paydriver/* → pkg/ext` only. Neither `charge` nor `payment`
imports `collect` or `cash`.

---

## 3. Charges (normative, CHARGES-EXPORT)

### 3.1 Charge model

A charge is an amount the customer owes, with lines, VAT, service period and due date. Fields (table §6.1):
`number` (`A{yyyy}-{n:06}`, per marina and year from `billing_sequences`, assigned at creation, never reused),
`customer_id` (the **billing** customer: `contracts.billing_customer_id ?? customer_id`; holder for holder
instalments; nullable only for counter sales to a walk-in customer, which are `fiscal_mode = receipt`),
`origin_type`, `origin_id`, `contract_id?`, `vessel_id?`, `right_id?`, `issued_on`, `due_on`,
`service_from/service_to` (min/max of lines), `total_cents` (signed, ≠ 0), `net_cents`, `vat_cents`,
`paid_amount_cents`, `credited_cents`, `payment_status`, `invoicing_status`, `fiscal_mode` (`invoice` default,
`receipt` for counter sales with a commercial document), `corrects_charge_id?`, `correction_kind?`
(`delta`, `replace`), `description` (resolved in the marina's first locale; no PII), `fiscal_snapshot_enc`
(customer fiscal data: name/company, fiscal code, VAT number, SDI code, PEC, billing address; KeyRing AAD
`charge-fiscal:<id>`), `external_invoice_number`, `external_invoice_date`, `cancel_reason`, `version`.

Lines: `seq`, `item_id?`, `item_code`, `description`, `qty_milli` (`qty.Milli`), `unit_cents`, `discount_cents`,
`net_cents`, `vat_cents`, `gross_cents` (= net + vat, signed like the charge), `vat_rate_bp`, `vat_nature`
(`N1`…`N7` when rate = 0), `price_includes_vat`, `revenue_class_code`, `external_code`, `service_from`,
`service_to`, `plan_key` (the `chargeplan` line key, §3.4). Totals: `total = Σ gross`, `net = Σ net`,
`vat = Σ vat` (CHECK through a trigger on insert of the last line is not possible; enforced in Go and by query B57,
§12).

**Snapshot of fiscal data**: written at creation from the billing customer; refreshed at the first export from the
live customer unless the customer is anonymised (then the creation snapshot stays). Anonymisation (M1
`customer.Anonymize`) never touches charges or their snapshots (spec 30).

### 3.2 State tables

Payment status (only `internal/domain/payment` changes `paid_amount_cents`; only `charge` cancels):

| From | Event | To | Rule |
|---|---|---|---|
| – | create (total > 0) | `open` | |
| – | create (total < 0, adjustment) | `paid` | negative charges are "applied" at creation (§3.5): `credited_cents = total` (offset to the corrected charge and/or a credit payment) |
| `open`, `partially_paid` | match / reversal / offset | `open` / `partially_paid` / `paid` | from `paid_amount + credited` vs `total` in the same UPDATE (0 → open, < total → partially_paid, = total → paid) |
| `paid` | reversal / refund-unmatch | `partially_paid` / `open` | |
| `open` | cancel(reason) | `cancelled` | only while `invoicing_status = to_invoice`, `paid_amount = 0`, `credited = 0`; otherwise `409 charge_immutable` (use an adjustment) |

Invoicing status (only `charge`):

| From | Action | To | Rule |
|---|---|---|---|
| – | create | `to_invoice` | except opening balances: `invoiced` (§3.8) |
| `to_invoice` | first export | `exported` | conditional `UPDATE … WHERE invoicing_status = 'to_invoice'` per charge inside the export tx; rows not updated are skipped (INV-53) |
| `exported`, `invoiced` | reconcile / manual invoice ref | `invoiced` | updates number/date (re-entry is allowed and audited; information only) |
| `exported`, `invoiced` | re-export | same | `charge_export_items.first = false` |

Triggers (both dialects, §6.1): when `invoicing_status <> 'to_invoice'` the columns `customer_id`, `origin_*`,
`total_cents`, `net_cents`, `vat_cents`, `issued_on`, `service_*`, `fiscal_mode`, `corrects_charge_id` are
immutable and `charge_lines` of the charge cannot be inserted, updated or deleted; `invoicing_status` never goes
back to `to_invoice`; a `cancelled` charge never changes again. `due_on` and `description` stay editable on exported
charges (not exported amounts; audited).

### 3.3 Origins and generation (all inside the caller's transaction, through `charge` Tx primitives)

| Origin | `origin_type` / `origin_id` | When | Lines | `due_on` | Notes |
|---|---|---|---|---|---|
| Contract instalment (also deposit) | `installment` / instalment id | `contract.BillingHooks.OnInstallmentIssuedTx` from `IssueTx` (job `installments.issue`, bulk "issue now", deposit at signature, settlement of a `scheduled` instalment) | `chargeplan.Split` of the instalment amount over the contract's remaining target (§3.4) | instalment `due_on` | sets `installments.charge_id` in the same tx; emits `installment.charged`; then `PaymentRequester.RequestForChargeTx` (dedupe key `installment:<id>:issued`, the M4 key, so no double email) |
| Contract true-up | `contract_trueup` / contract id | after the last live instalment is issued, and on `OnContractRepricedTx` when no `scheduled` instalment remains | remaining target per line (may be negative ⇒ adjustment, §3.5) | today + `billing_settings.default_due_days` | `corrects_charge_id` = latest charge of the contract; skipped when remaining = 0 |
| Holder instalment | `holder_installment` / id | `rights.BillingHooks.OnHolderInstallmentIssuedTx` | one line: item from `charge_origin_items` for the source (`management_fee`, `common_advance`, `common_balance`, `transfer_fee` = `right_transfers.takeover_item_id ?? origin item`), qty 1, gross = amount, service period = run year / transfer date | instalment `due_on` | negative `common_balance` ⇒ negative charge (credit, §3.5); no origin item configured ⇒ the issue job skips the row with a worklist item `billing_item_missing` (never a charge without VAT data) |
| Transit | `transit` / contract id | `contract.BillingHooks.OnTransitConfirmedTx` (manual or provider confirmation) | target of the transit snapshot (one charge for the whole stay) | `pay_at_departure` ? departure date : arrival date (≥ today) | reprice / change of stay ⇒ `OnContractRepricedTx` ⇒ sync (§3.5); `cancelled` / `no_show` after confirmation ⇒ `OnContractClosedTx` ⇒ cancel if still cancellable, else negative delta of the residual target with reason (staff can then refund or keep as credit) |
| Counter sale | `counter_sale` / charge id (self) | `cash.CounterSale` (§5.3) or `POST charges` with `origin=counter_sale` | items with price from the active price list (`pricelist.Service.ItemPrice`) or staff unit price (`charges.manage`), qty, discount | today | `fiscal_mode = receipt` when a fiscal document is issued |
| Termination | `termination` / contract id | staff action `POST contracts/{id}/termination-charge` (once; unique live primary) | `termination_amount_cents` > 0: one line, item `termination_penalty`; < 0: negative charge, item `termination_refund`, `corrects_charge_id` = latest contract charge | today + default due days | negative needs `refunds.issue`; the M4 stored amount stays as the proposal (13.4 "proposto") |
| Deposit retention | `deposit_retention` / payment id | `payment.Retain` (§4.4) | item `deposit_retention` (or chosen), gross = retained amount | today | matched to the deposit payment in the same tx |
| Manual | `manual` / charge id (self) | `POST charges` | catalogue items | given or default | `charges.manage` |
| Adjustment | `adjustment` / corrected charge id | §3.5 | §3.5 | §3.5 | |
| Opening balance | `opening_balance` / import run id | §3.8 | one line, item `opening_balance` | file | `invoiced` at creation |
| Later | `utility`, `work_order`, `rental` | M9–M11 | – | – | CHECK only |

**One live primary charge per origin** (INV-54): partial unique index on `(marina_id, origin_type, origin_id)` WHERE
`corrects_charge_id IS NULL AND payment_status <> 'cancelled' AND origin_type IN ('installment','holder_installment',
'transit','termination','deposit_retention')`. A concurrent second creation hits the index and the tx returns the
existing charge (idempotent issue).

**No zero charges**: a computed total of 0 creates nothing (an instalment of 0 cannot exist, INV-31 plans skip
them; a fully free transit has no charge).

### 3.4 `chargeplan` (pure)

```go
type Line struct { Key string; ItemID, ItemCode, Description, RevenueClass, ExternalCode string; Qty qty.Milli;
    VATRateBP int64; VATNature string; PriceIncludesVAT bool; Net, VAT, Gross money.Cents;
    ServiceFrom, ServiceTo calendar.Date }
type Target struct { Lines []Line }                              // what the origin must charge in total
func ContractTarget(snap pricing.Snapshot, adj []Adjustment) (Target, error)
func Remaining(t Target, charged []ChargedLine) []Remainder       // per key: gross, vat still to charge (signed)
func Split(rem []Remainder, amount money.Cents) ([]Part, error)    // lines of one charge
func Totals(lines []Line) []VATBucket                              // per (rate, nature), sorted like pricing
```

Rules:
- **Target** = the snapshot `Breakdown.Lines` (key `snap:<seq>`, net/vat/gross as computed by the engine, so with no
  adjustment `Totals(target) == Breakdown.VATSummary` exactly) plus every snapshot adjustment (INV-27) distributed
  over the lines it concerns with `money.Allocate` on gross weights: `move`, `move_undo`, `vessel_change` → berth
  lines; `early_payment`, addendum `price_change` → all lines; addendum `service_add` → a new line of the addendum's
  item (key `add:<addendum_id>`); `service_remove` → the removed line. The adjusted gross of a line is re-split with
  `money.VATSplit` (gross-priced items) or `net × rate` half-even (net-priced items), so `net + vat = gross` per line
  (INV-25). Σ target gross = contract `total_cents` (asserted; a mismatch is `ErrTargetMismatch`, logged with ids only: the issue of that
  instalment fails, a worklist item `billing_target_mismatch` is opened, and no wrong charge is ever written).
- **Split** of an instalment amount A over remainders: weights = positive remaining gross per line; parts =
  `money.Allocate(A, weights)` capped at each remainder (A ≤ Σ positive remaining always holds while instalments sum
  to the total; if A exceeds it the excess is allocated over all lines by target gross and corrected by the true-up).
  VAT of a part by **cumulative rounding** per line: `vat_cum_k = round_half_even(vat_line × gross_cum_k /
  gross_line)`, `vat_part = vat_cum_k − vat_cum_{k−1}`, `net_part = gross_part − vat_part`; when the line's gross is
  fully charged its VAT is fully charged. Hence Σ over the charges of an origin per line = target per line, per
  (rate, nature) = target totals (INV-57).
- Qty on instalment lines: `qty_milli` = target qty × part gross / line gross (half-even, informative); when the
  derived qty is 0 the line carries qty 1000 and `unit_cents` = part gross. The export reports net, VAT and gross,
  which are exact.
- Property tests: `TestChargePlan_SplitSumsToTarget` (rapid: random snapshots of 1..30 lines with mixed rates,
  natures, net/gross pricing, 1..24 instalments summing to the total, random adjustments between issues, true-up at
  the end ⇒ per line and per (rate, nature) equality, every part ≤ remaining except the documented overflow),
  `TestChargePlan_TargetEqualsSnapshotWithoutAdjustments`, `TestChargePlan_VATCumulativeNeverDrifts`,
  `TestChargePlan_Scenario` (table: annual 30 % + 3 quarters at 22 %, mixed 22 %/10 %/N4, early payment, move
  reprice after the second instalment, service added by addendum).

### 3.5 Corrections, adjustments, sync

- `Adjust(chargeID, {mode: delta|replace, lines?, reason})` (charges.manage; refunds.issue when the resulting new
  charge total is negative):
  - **to_invoice, no matches, no offsets**: edit in place is `PATCH charges/{id}` (lines, due date, billing
    customer; If-Match); `Adjust` is refused with `409 use_edit`.
  - **delta**: a new charge `origin_type = adjustment`, `origin_id = corrects_charge_id = original`, lines = the
    given delta lines (negative quantities or amounts allowed) → total ≠ 0.
  - **replace**: one tx creates the negative full reversal of the original (lines negated) and a new positive charge
    with the corrected lines (same origin as the original, `correction_kind = replace`, `corrects_charge_id` = the
    original). Matches of the original stay; the reversal offsets the original's residual (below) and any surplus
    becomes credit; staff then apply credit to the replacement (UI does it in the same dialog).
- **Negative charges are applied at creation** (same tx): `offset = min(|total|, residual of corrected charge)` is
  written as `charge_offsets(negative_charge_id, target_charge_id, amount)` raising the target's `credited_cents`
  by a conditional UPDATE (`paid_amount + credited + ? <= total`); the rest becomes a **credit payment**
  (`payments.kind = credit`, `source = adjustment`, `adjustment_charge_id`) through `payment.GrantCreditTx`. The
  negative charge ends `paid` with `credited_cents = total`. Without `corrects_charge_id` (e.g. negative common-charge
  balance) the whole amount becomes credit.
- **Sync of an origin** (`charge.SyncOriginTx`, used by transit reprice/close and contract true-up): compute
  `Remaining(target, charged)`; if Σ = 0 nothing; if the latest primary charge is `to_invoice` with no matches and no
  offsets, rewrite its lines in place (version + 1, audit `charge.update`); else create an adjustment charge for the
  remaining (positive or negative) linked to the latest charge.
- Every adjustment is a normal charge: it goes to the next export with `corrects_charge_number` (INV-51).

### 3.6 Export "da fatturare", files, re-export, reconciliation, `ChargeExporter`

- **Preview** `POST charge-exports/preview {period_from, period_to, customer_id?, origin_types?, revenue_class?}`:
  selects `to_invoice`, not cancelled, `issued_on ∈ [from, to]`; a revenue-class filter selects charges having at
  least one line of the class and exports the **whole** charge (a charge is never split across exports). Returns
  counts, totals per (rate, nature), per origin, and warnings (`customer_fiscal_incomplete`: missing fiscal code and
  VAT number, missing SDI code and PEC for companies; walk-in receipts are fine).
- **Create** `POST charge-exports` (charges.export, `Idempotency-Key`): one tx: `billing_sequences` number, the
  selection re-read **with locks**, `UPDATE charges SET invoicing_status='exported', first_export_id=?, exported_at=?
  WHERE id IN (…) AND invoicing_status='to_invoice'` in chunks of 500 (rows not updated are dropped from the export),
  `charge_export_items(first = true)` for the updated rows (partial unique `charge_export_items(charge_id) WHERE
  first` = INV-53 in the DB), fiscal snapshots refreshed, totals stored, audit, `charge.exported`. Files are built
  **after commit** from the stored items (deterministic order: charge number, line seq), encrypted into
  `stored_files` (purpose `document`), sha256 stored; a crash before files is recovered by the next download
  (lazy build). ≤ 20 000 charges per export (`422 export_too_large` with the count).
- **Re-export** `POST charge-exports/reexport {export_id | charge_ids[] ≤ 20 000}`: new export `kind = reexport`,
  items `first = false`, statuses unchanged.
- **CSV** (Italian, UTF-8 with BOM, `;`, CRLF, decimal comma, dates `GG/MM/AAAA`), one row per charge line:
  `Numero addebito; Data addebito; Scadenza; Origine; Rif. contratto; Rif. rata; Imbarcazione; Matricola; Cliente;
  Codice fiscale; Partita IVA; Codice destinatario; PEC; Indirizzo; CAP; Comune; Provincia; Nazione; Riga; Codice
  articolo; Codice esterno; Descrizione; Quantità; Prezzo unitario; Sconto; Imponibile; Aliquota IVA; Natura; IVA;
  Totale riga; Classe di ricavo; Competenza dal; Competenza al; Rettifica di; Documento commerciale; Stato pagamento;
  Pagato; Id addebito`. Golden test `TestExportCSV_Golden`. **Excel** (`internal/xlsx`): same columns in sheet
  "Righe", plus sheet "Riepilogo IVA" (per rate/nature) and "Addebiti" (one row per charge). **PDF summary** (docs
  kind `charge_export_summary`, rendered ephemerally, §8.3): period, filters, counts, totals per rate/nature, per
  origin, per revenue class, list of charges with totals.
- **Reconciliation** `POST invoice-reconciliations` (charges.export; multipart CSV or JSON rows; `dry_run`): columns
  `id addebito` **or** `numero addebito`, `numero fattura`, `data` (`GG/MM/AAAA` or ISO); ≤ 20 000 rows; one tx;
  report rows `ok | unknown_charge | not_exported | cancelled | invalid_date | duplicate_row`; `invoiced` set with a
  conditional UPDATE `WHERE invoicing_status IN ('exported','invoiced')`; stored in `invoice_reconciliations` (report
  JSON with charge numbers only). Manual `PUT charges/{id}/invoice {number, date}`.
- **`ext.ChargeExporter`** (§5.6): `POST charge-exports {…, exporter_code}` creates the export as above with
  `delivery = exporter`, `delivery_status = pending`; job `charges.exporter` (1 min) calls `Export` after commit with
  the batch (charges, lines, fiscal data decrypted in memory only), stores `delivery_status` (`sent | failed`,
  `error_code`), and calls `Poll` for sent batches to receive invoice refs, applied like a reconciliation (source
  `exporter`). No exporter registered ⇒ `exporter_code` rejected `422 provider_unknown`.
- **Payments export** `GET payments/export?from&to&method?&format=csv|xlsx` (charges.export): one row per payment
  and refund with business date, method, provider, provider reference, gross, provider fee, net of fee, customer
  name and fiscal code, matched charge numbers.

### 3.7 Backfill of M4 data (job `charges.backfill`)

RunAtStart + hourly, idempotent, per marina, batches of 200, one tx per instalment: for each `installments` /
`holder_installments` row with `status IN ('issued','partially_paid','paid')` and `charge_id IS NULL`: create the
charge as §3.3 (with `issued_on = issued_at` date); when `paid`, a manual payment (`source = backfill`, method =
`settlement_method`, `received_on = settled_on`, reference re-encrypted from `settlement_ref_enc`) matched in full;
`settlement_source` stays `manual` for these historical rows (INV-32 allows it; new settlements are `payment`).
Conditional `UPDATE … SET charge_id = ? WHERE charge_id IS NULL`. `TestCharge_BackfillM4Idempotent`.

### 3.8 Opening balances import (11.6)

`POST opening-balances/import` (multipart `.csv` or `.xlsx`, `dry_run`, mapping `{customer_match: fiscal_code |
vat_number | customer_code, amount, due_on, description?, reference?}`; charges.manage): ≤ 5 000 rows, one tx;
customer match must be unique (`ambiguous_customer` / `unknown_customer` rows); amount ≠ 0 (negative = customer
credit ⇒ credit payment `source = opening_balance`); creates `opening_balance` charges `invoicing_status =
invoiced` (`external_invoice_number` = reference or `SALDO-APERTURA`); `charge_imports` row with report; undo
(`POST opening-balances/imports/{id}/undo`) cancels its charges while none has matches/offsets
(`409 import_not_undoable`). XLSX input also enabled for the M4 imports: `csvimport` gets `xlsx.go` turning the first
sheet into rows (CHARGES-EXPORT; the M4 entity handlers are unchanged).

---

## 4. Payments, matching, refunds, credits (normative, PAYMENTS)

### 4.1 Ledger

`payments`: `number` (`P{yyyy}-{n:06}`), `customer_id?` (null only for walk-in counter sales), `kind`
(`payment` | `deposit` | `credit`), `method` (`cash`, `card_pos`, `bank_transfer`, `cheque`, `other`, `card_online`,
`wallet`, `sepa_debit`, `credit_note` for kind credit), `source` (`manual`, `provider`, `backfill`, `adjustment`,
`goodwill`, `release`, `mandate`, `opening_balance`), `provider_code?`, `provider_ref?` (unique per marina and
provider), `intent_id?`, `amount_cents` (> 0), `allocated_cents`, `refunded_cents`, `fee_cents` (provider fee,
informative), `received_on` (business date, marina tz), `cash_session_id?` (required for method cash), `vat_split`
json (frozen per (rate, nature) of what it paid, filled at match time for fiscal use), `reference_enc`, `notes`,
`idempotency_key?` (unique per marina), `recorded_by`, `version`.
CHECK `allocated_cents ≥ 0`, `refunded_cents ≥ 0`, `allocated_cents + refunded_cents ≤ amount_cents`;
`kind = 'credit'` ⇒ method `credit_note` and no cash session; `kind = 'deposit'` ⇒ allocations only through retention.

`payment_matches` (insert-only, trigger): `payment_id`, `charge_id`, `amount_cents` (≠ 0), `reverses_match_id?`,
`created_by`. Since M8 (migration `00017`, ADR 0013 decision 8) a match may be reversed several times, in parts, up
to its amount: `reversed_cents` (the sum of its reversals, the only column the trigger lets grow) is moved by a
conditional UPDATE and bounded by `payment_matches_reversed_check`; until M8 an unique index allowed one reversal. **Credit** of a customer = Σ (`amount − allocated −
refunded`) of their `payment` and `credit` payments (deposits are listed separately and are not credit).

**INV-50** (restated precisely): for every charge `paid_amount_cents = Σ payment_matches.amount_cents` of the charge
(reversals negative) and for every payment `allocated_cents = Σ` its matches; refunds come only from unallocated
money, so "Σ incassi − Σ rimborsi" of a charge is its matched total. Query B50 of `store.CheckBillingInvariants`.

### 4.2 Recording and matching (one transaction each)

`payment.Record(p, marina, RecordInput{customer_id?, kind, method, amount, received_on?, reference?, matches?
[{charge_id, amount}] | auto_match: oldest_due_first, cash_register_id?, idempotency_key, send_receipt?})`
(payments.collect; `payments.collect_cash` allows only `method = cash`): validation; idempotency replay (same key and
same input ⇒ the stored payment; different input ⇒ `422 idempotency_mismatch`; concurrent loser hits the unique index
and returns the winner); cash ⇒ `cash.SessionForPaymentTx` (open session of the register, `409
cash_session_required`); insert; then `MatchTx` for each requested match; fiscal hook for receipt-mode charges
(§5.4); audit `payment.record`; `payment.succeeded`; optional receipt email (template `payment_receipt`).

`MatchTx(payment, [(charge, amount)])` — the only code that changes `paid_amount_cents`:
1. `LockPayment`, `LockChargesSorted` (ascending id).
2. `UPDATE payments SET allocated_cents = allocated_cents + :sum WHERE id = ? AND allocated_cents + refunded_cents +
   :sum <= amount_cents` — affected 0 ⇒ `409 match_exceeds` (`details.limit = payment`).
3. Per charge: `UPDATE charges SET paid_amount_cents = paid_amount_cents + :a, payment_status = CASE … END,
   version = version + 1 WHERE id = ? AND total_cents > 0 AND payment_status IN ('open','partially_paid') AND
   paid_amount_cents + credited_cents + :a <= total_cents` — affected 0 ⇒ `409 match_exceeds` (`details.limit =
   charge`, `charge_id`).
4. Insert matches; `vat_split` of the payment updated with the matched share of each charge's VAT buckets
   (`money.Allocate` of the amount over the charge's per-bucket residual gross).
5. **Origin settlement callback** when a charge's status changed: `installment` → `contract.Service.
   ApplyChargeSettlementTx(installment_id, status, payment)`; `holder_installment` → `rights.Service.
   ApplyChargeSettlementTx`; others nothing. (§8.2)
6. Audit `payment.match`, event `charge.paid` when it reached `paid`, `payment.changed`.

`ReverseMatch(match)` (refunds.issue): locks, re-reads the match, adds the amount (default: what is left) to
`reversed_cents` with `UPDATE … WHERE reversed_cents + :a <= amount_cents` (zero rows = `already_reversed`), inserts
the opposite row, decrements both sides with conditional UPDATEs (`paid_amount_cents - :a >= 0`), callback (instalment back to `partially_paid`/`issued` through
`ApplyChargeSettlementTx`), audit `payment.unmatch`.

Auto-match `oldest_due_first`: the customer's open charges ordered by `due_on, number`, filled until the payment is
exhausted; the remainder stays as credit. **Apply credit** `POST customers/{id}/credits/apply {charge_ids? }` =
MatchTx from the customer's payments with unallocated money (oldest first).

### 4.3 Refunds and payouts

`refunds`: `payment_id?` (null for payouts), `kind` (`refund`, `payout`), `method`, `source` (`manual`,
`provider`), `provider_ref?`, `intent_id?`, `status` (`pending`, `succeeded`, `failed`), `amount_cents` (> 0),
`reason`, `business_date`, `cash_session_id?` (cash refunds), `mandate_id?`, `recorded_by`.
`payment.Refund(paymentID, {amount, via_provider, method?, reason, unmatch?[{match_id, amount}]})` (refunds.issue):
one tx: optional reversals first, `LockPayment`, `UPDATE payments SET refunded_cents = refunded_cents + :a WHERE …
AND allocated_cents + refunded_cents + :a <= amount_cents` (in-flight refunds count, as Arenile ADR 0014 §5),
insert refund (`succeeded` for manual; `pending` + intent for provider, sent after commit by `collect`), audit
`refund.create`, `payment.refunded`. A failed provider refund releases the amount (`refunded_cents - :a`) and marks
`failed`. Provider refunds are possible only when the payment's provider supports refunds.

### 4.4 Deposits, credits, mandates

- **Deposit** (`kind = deposit`, keys, badges): never matched to charges, not in corrispettivi as revenue. `Retain
  (paymentID, {amount, item_id?, reason})` (charges.manage): creates a `deposit_retention` charge and matches the
  deposit to it (allowed for deposits only through this path); `Release` = refund.
- **Goodwill / release credit** `POST customers/{id}/credits {amount, reason, source: goodwill|release,
  release_id?}` (refunds.issue): credit payment; for `release` also `UPDATE releases SET credit_cents = credit_cents
  + ?` (release of the customer's contract, `resold_allocation_id` not null, else `422 release_not_resold`).
- **Mandate compensation** (14.3) `POST mandates/{id}/settle {through, mode: credit|payout}` (refunds.issue): sums
  `mandate_revenues` owner shares not yet settled up to `through` plus the guaranteed-minimum top-up of the closed
  period; per holder share (`money.Allocate` by holder millis) a credit payment (`source = mandate`) or a payout
  refund (`kind = payout`, method bank_transfer, `succeeded` when recorded); `mandate_revenues.settled_at` set with a
  conditional UPDATE `WHERE settled_at IS NULL`; audit `mandate.settle`.

### 4.5 Statement, balances, ageing, dashboard, worklist, lens

- **Balance** of a customer = Σ residual of open positive charges (`total − paid − credited`) − credit (§4.1).
  **Statement** `GET customers/{id}/statement?from&to&format=json|pdf|csv` (charges.manage): opening balance at
  `from` (same formula on rows dated before), rows by date (charge +total, payment −amount, refund +amount, credit
  −amount, offsets are not rows: the negative charge is), running balance, closing balance = balance at `to`; PDF via
  docs kind `statement_of_account` (ephemeral, §8.3). `TestStatement_ClosingEqualsBalance`.
- **Ageing**: buckets on `due_on` vs today: `not_due`, `d0_30`, `d31_60`, `d61_90`, `d90_plus` (residuals of open
  charges). `GET receivables/ageing` per customer (+ totals, CSV/XLSX).
- **Scadenzario** `GET receivables` (charges.manage): union of open charges (residual, bucket, last reminder level)
  and `scheduled` instalments (future, no charge yet) with filters `due_from/due_to`, `bucket`, `status`,
  `customer_id`, `contract_id`, `group_id`, `origin_type`; CSV/XLSX; bulk actions are §5 endpoints.
- **Customer dashboard** `GET customers/{id}/dashboard` (customers.read; billing sections only with charges.manage,
  otherwise omitted): §1.1 list; computed by read queries, no new table.
- **Map payment status** `GET maps/{mapId}/payment-status?date` (map.view): per allocated resource on that night the
  lens state of spec 9.2 — `ok`, `due_soon` (an open charge or issued instalment due ≤ 15 days), `overdue`,
  `overdue_60`, `disputed` (an active dunning exclusion of kind disputed) — plus `suspended` flag; amounts only with
  charges.manage. Used by the lens and by the dock's departure warning (never a block).
- **Worklist** (28.3) `GET worklist` / `PATCH worklist/{source_type}/{source_id} {assignee_user_id?, status}`:
  items computed from sources (reminder items awaiting approval, `to_invoice` charges older than
  `billing_settings.export_reminder_days`, failed intents, late provider payments left as credit, SEPA mandate
  failures, `billing_item_missing`, `billing_target_mismatch`, open deadlines of the M4 engine), each merged with
  `work_item_states` (assignee, `open|in_progress|done|dismissed`, unique per source); items visible only with the
  source's permission.

---

## 5. Cash, fiscal, collection, drivers (normative, CASH-DRIVERS)

### 5.1 Cash registers and sessions (as Arenile)

- `cash_registers` (code, name, active, is_default; default `main` created lazily by `EnsureDefaultRegister`).
- `cash_sessions` (register_id, status `open|closed`, opened_by, opened_at, `float_cents`, closed_by, closed_at,
  `expected` json per method, `counted` json per method, `difference_cents`, notes, version); **partial unique**
  `(register_id) WHERE status = 'open'` (INV-55); closed sessions immutable (trigger).
- `cash_movements` (session_id, kind `in|out`, amount > 0, reason, by) — a withdrawal larger than the expected cash
  is refused (`409 cash_insufficient`).
- `SessionForPaymentTx(register?)`: `LockCashSession` + no-op conditional `UPDATE … WHERE status = 'open'` so a
  payment lands before a close (and is in its expected totals) or fails `409 cash_session_closed`. Cash refunds
  also need an open session. Non-cash desk payments attach to the open session of the chosen/default register when
  one exists (Z totals by method), never required.
- Close `POST cash-sessions/{id}/close {counted[{method, amount}], notes}` (If-Match): `summary.Compute` (pure):
  expected per method = float (cash) + Σ payments − Σ refunds of the session ± movements; counted missing = 0;
  difference = Σ counted − Σ expected; frozen with a conditional UPDATE (`status = 'open' AND version = ?`).
- Z report `GET cash-sessions/{id}/z-report?format=html|pdf`: header, operator ids → names (customers.read not
  needed: staff names), totals per method and per VAT rate (from payments' `vat_split`), fiscal document range,
  movements, difference.
- Permissions: `payments.collect` or `payments.collect_cash` for session open/close/movements; registers write
  `marina.configure`.

### 5.2 Fiscal documents and corrispettivi

- `ext.FiscalDriver` (§5.6); core drivers `none` (default: no I/O, document `issued` inline with the internal
  number, tracking only — the operator uses their own registered device) and `export` (same as `none` plus the daily
  corrispettivi file offered on the fiscal page). Selected in `billing_settings.fiscal_driver`.
- `fiscal.RecordTx(payment, charge)` runs in the payment tx for **receipt-mode** charges (counter sales with
  `fiscal: true`, and staff choosing "documento commerciale" for a walk-in transit paid at the desk): next number
  `YYYYMMDD-NNNN` from `fiscal_sequences (marina_id, day)` (upsert + `UPDATE … RETURNING`/read under lock; unique
  `(marina_id, number)`), `fiscal_documents` row with lines from the charge (gross per rate), status `issued`
  (inline drivers) or `pending` (others: job `fiscal.issue` calls the device after commit, conditional UPDATE of the
  outcome; a driver error implementing `Deferred() bool` keeps `pending`). Void `POST fiscal/documents/{id}/void`
  (refunds.issue) with the driver's `Void`.
- **Corrispettivi** `GET fiscal/daily-totals?from&to&group=day|month&format=json|csv|xlsx` (charges.export): from
  `fiscal_documents` (issued, not void) and their refunds: per business date × VAT rate/nature × method: gross, net,
  VAT (split with `money.VATSplit` per row; totals = sums of rows; grand total = Σ receipt-mode payments − refunds of
  the range: `TestCorrispettivi_TotalsEqualPayments`, rapid).
- Invoiced charges (`fiscal_mode = invoice`) never enter corrispettivi (the invoice is issued outside); receipt-mode
  charges are still exported once (INV-53) with column `Documento commerciale` so the accountant does not invoice
  them twice (§16 D9).

### 5.3 Counter sales and receipts

- `POST counter-sales {customer_id?, vessel_id?, lines[{item_id, qty_milli, unit_cents?, discount_cents?}],
  payment{method, amount_cents?, register_id?}, fiscal_document: bool, receipt: none|print|email}`
  (payments.collect; collect_cash ⇒ cash only; `unit_cents` override needs charges.manage), `Idempotency-Key`: one
  tx: charge (`counter_sale`), payment, match, fiscal document when requested; response `{charge, payment,
  fiscal_document?, receipt_url}`. Without `customer_id` the charge has `customer_id = NULL`, `fiscal_mode =
  receipt` and `fiscal_document` must be true (an invoiceable charge needs a customer: `422 customer_required`).
- **Receipts** `GET payments/{id}/receipt?format=80|58|a4`: 80/58 mm = self-contained print-ready HTML (no script,
  no external resources; marina header, payment number, date, lines or matched charges, VAT split, method, footer
  from `billing_settings.receipt_footer`, the fixed text "Non valido ai fini fiscali"); A4 = PDF via docs kind
  `receipt` (ephemeral). Email `payment_receipt` with the A4 PDF attached when requested.

### 5.4 Intents, providers, webhooks (INV-09, INV-56)

- **Registry** `paydriver.Registry`: core `manual` + `ext.Extensions.PaymentProviders` + `stripe` (core, but
  usable by a marina only with an enabled `payment_provider_settings` row). `GET payment-providers` lists codes,
  capabilities and per-marina enabled state.
- **Settings** `payment_provider_settings (marina_id, provider_code, enabled, mode test|live, credentials_enc,
  webhook_secret_enc, sepa_enabled, methods json, statement_descriptor, version)`; secrets write-only (GET returns
  `has_secret_key`, `has_webhook_secret`, last 4 of the publishable key), KeyRing AAD
  `paycreds:<marina>:<provider>`; `POST …/test` calls the provider's balance/account endpoint.
- **Intents** `payment_intents`: `purpose` (`payment_request`, `transit`, `sepa_collect`, `mandate_setup`),
  `provider_code`, `customer_id`, `amount_cents`, `currency`, `request_id?`, `contract_id?`, `charge_ids` via
  `payment_intent_charges`, `provider_session_ref?`, `provider_payment_ref?`, `status` (`created → open →
  processing → succeeded | failed | expired | cancelled`; forward only, except a `succeeded` after
  `failed/expired/cancelled` is applied because money arrived), `attempt`, `idempotency_key`
  (`<purpose>:<subject>:<attempt>`), `expires_at`, `last_checked_at`, `failure_code`. Unique
  `(provider_code, provider_session_ref)`, `(provider_code, provider_payment_ref)`.
- **Webhook** `POST /webhooks/payments/{provider}/{marinaId}` (public, rate limited): handler reads the raw body
  (≤ 256 KiB), resolves provider + marina settings (unknown ⇒ 404), `ParseWebhook(r, secret, now)` (invalid ⇒
  `401 invalid_signature`, no secret configured ⇒ 401), then `collect.ApplyProviderEvent` in **one tx**: insert
  `payment_provider_events (marina_id, provider_code, event_id)` (unique: a duplicate, even concurrent, returns 200
  with no effect), lock the intent by ref (unknown but plausibly ours ⇒ 503 so the provider retries; foreign ⇒
  `ignored`), then by event:
  - `succeeded` for purpose `transit`: if the transit is `pending_payment`/`held` →
    `contract.Service.ConfirmTransitPaidTx(source = provider)` (creates the transit charge through the billing
    hook), then `payment.RecordTx(source = provider, method, provider_ref, fee)` + `MatchTx` to the transit charge;
    transit no longer confirmable (expired with lost hold and re-allocation failed, cancelled) ⇒ payment recorded
    as unallocated credit + worklist item `late_payment` + office email (§16 D14).
  - `succeeded` for `payment_request` / `sepa_collect`: `RecordTx` + `MatchTx` over the intent's charges in due
    order capped at residuals; excess stays credit (+ worklist `overpayment`); request `paid` when all charges paid.
  - `processing` (SEPA async) → intent `processing`; `failed` → intent `failed` + `payment.failed` + worklist item;
    `expired` → intent `expired` (transit back to `held` if its hold is still valid, else the expiry job handles it).
  - refund events → complete the pending refund by intent (or record a provider-side refund with derived ref).
  - mandate events (`setup succeeded`, `mandate revoked`) → `sepa_mandates` status.
  - Each applied event: audit (actor `provider:<code>`), outbox events. **The browser return URL never confirms**
    (`TestCheckout_BrowserReturnDoesNotConfirm`, M5 transit variant).
- **Reconciliation** job `payments.reconcile` (15 min, only marinas with an enabled provider): intents `open` or
  `processing` older than 10 minutes, ordered by `last_checked_at`, ≤ 50 per run: `Status(ref)` and the outcome goes
  through the same `ApplyProviderEvent` code with a synthetic event id `reconcile:<ref>:<status>` (applied once).
- **Expiry** job `payments.intents_expire` (1 min): `open` intents past `expires_at` → `expired` (provider session
  closed after commit, best effort); payment requests past expiry → `expired`; transits `pending_payment` whose
  intent expired → `held` (hold valid) or the M3 expiry path.

### 5.5 Payment requests, links, reminders, SEPA, arrears

- **Payment request** `payment_requests (customer_id, token_hash, amount_cents, status open|paid|cancelled|expired,
  expires_at, sent_at, sent_count, last_message_id, created_by)` + `payment_request_charges`. Token: 32 random
  bytes, base32, stored as SHA-256 (as M4 magic links, INV-75 rules; separate table because `access_links.purpose`
  CHECK is frozen, §16 D12). Created by staff (`POST payment-requests`, bulk per customer from charge ids) or
  automatically by `RequestForChargeTx` when `billing_settings.auto_payment_request` (default true) for instalment and
  holder-instalment charges. Email `payment_request` (it/en): amount, charges, due dates, bank details from
  `contract_settings.bank_details`, link `PORTO_BASE_URL/b/{slug}/pay/{token}`. Amount = Σ residuals at send time;
  re-send rotates nothing (same link) unless `rotate: true`.
- **Public pay page** (`/api/public/v1/{slug}/pay/{token}`, `security: []`, rate limited 30/min, no-store,
  no-referrer): `GET` → marina name, amount, currency, charges (number, description, due, residual), bank details
  (IBAN, holder, causale = charge numbers), `online: bool` (provider enabled with checkout capability), status;
  `POST …/checkout {method: card|sepa_debit}` → creates an intent (`payment_request`) and returns `redirect_url`
  (provider hosted page); `GET …/status` for the return page ("pagamento in verifica" until the webhook lands).
  Unknown/expired/cancelled tokens ⇒ 404 `link_invalid` like unknown ones.
- **Transit payment link** `POST transits/{id}/payment-link {expires_in_hours? (1..72, default 24), send?: bool}`
  (transits.desk; provider enabled else `422 provider_disabled`): `contract.RequestTransitPaymentTx` (held →
  `pending_payment`, hold expiry extended to the link expiry) + intent `transit` (amount = transit total or the
  price-list deposit when the policy asks for a deposit) + optional email; the checkout session is created on
  click from the pay page (so the link stays valid while sessions expire).
- **SEPA Direct Debit**: `POST customers/{id}/sepa-mandates` (charges.manage) creates a `mandate_setup` intent and a
  setup link (email `sepa_mandate_request`); the webhook activates `sepa_mandates (customer_id, provider_code,
  provider_customer_ref_enc, payment_method_ref_enc, mandate_ref, iban_last4, status pending|active|revoked|failed)`
  (partial unique active per customer and provider). Contracts whose policy has `payment.sepa = true` and whose
  customer has an active mandate: job `payments.sepa_collect` (15 min) creates for each **issued, open** instalment
  charge with `due_on ≤ today + sepa_lead_days` (default 0) one `sepa_collect` intent (idempotency
  `sepa:<charge_id>:<attempt>`; at most one open intent per charge, partial unique on `payment_intent_charges`
  WHERE intent open/processing) and calls `ChargeMandate` after commit; outcome by webhook (days later). Failure ⇒
  worklist + reminder flow continues normally.
- **Reminders** (`payments.reminders`, hourly; runs once per marina day after `reminder_hour` marina time, default 9):
  `reminder_runs (marina_id, run_on)` unique ⇒ one run per day even with two instances; for each open charge not
  excluded: `levels.Next(due_on, today, policy, last_level)` (pure) → `pre_due` (N days before, default 7), `due`
  (on due date), then levels `courteous` (+7), `formal` (+21), `final_notice` (+45) by default
  (`billing_settings.reminder_policy`, levels editable: days and enabled); `reminder_items (run_id, charge_id,
  customer_id, level, status queued|awaiting_approval|skipped_excluded|skipped_paid|skipped_no_email|sent)` with
  **unique (charge_id, level)** (each level once per charge). One email per customer and level per run with all
  their charges at that level (dedupe key `reminder:<customer>:<level>:<run_on>`, INV-77), with the payment request
  link (a request is created/refreshed for those charges). `final_notice` items wait for approval
  (`POST reminder-items/{id}/approve` / `dismiss`, charges.manage) and appear in the worklist. **Stop on payment**:
  the residual is re-read under the charge lock when queuing; a paid charge is `skipped_paid`. Manual bulk
  "sollecita" `POST reminders/send {charge_ids[], level?}` uses the same path (level = next due level).
- **Exclusions** `dunning_exclusions (customer_id? | charge_id?, kind payment_plan|disputed|other, until?, note,
  created_by, removed_at)`; CHECK exactly one target; active = not removed and (until null or ≥ today).
- **Arrears policy** (`billing_settings.arrears_policy {enabled: false, days_overdue: 60, min_amount_cents: 0}`):
  daily in the reminders run: active mooring/charter contracts whose own charges have residual ≥ min overdue more
  than N days and no active exclusion ⇒ `contract.Service.SuspendForArrearsTx` (re-checks the overdue sum, the
  minimum and the customer exclusion after locking the contract and its overdue charges, else `ErrNotInArrears`;
  `TestConcurrency_ArrearsVsSettle`) (`active → suspended`, reason
  `arrears`, actor `system:payments.arrears`, `contract.changed`), never on transits; resume stays manual
  (contracts.write) — the office decides after payment (worklist item `arrears_paid` when such a contract has no
  overdue charge left).

### 5.6 `pkg/ext` interfaces (CONTRACT writes them; Porto's own design)

```go
// PaymentProvider is a payment gateway or method driver (spec 16.2). Credentials arrive per call
// (WithProviderCredentials); outbound HTTP only through the client the host gives the driver at construction.
type PaymentProvider interface {
    Code() string
    Capabilities() PaymentCapabilities // Checkout, MandateSetup, MandateCharge, Refund, StatusCheck bool; Methods []string
    CreateCheckout(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error)       // ErrNotSupported
    CreateMandateSetup(ctx context.Context, req MandateSetupRequest) (*CheckoutSession, error)
    ChargeMandate(ctx context.Context, req MandateChargeRequest) (*ProviderPayment, error)
    Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
    Status(ctx context.Context, ref ProviderRef) (*PaymentEvent, error)
    ParseWebhook(r *http.Request, secret []byte, now time.Time) (*PaymentEvent, error)       // ErrInvalidSignature, ErrIgnoredEvent
    CloseCheckout(ctx context.Context, sessionRef string) error
}
// PaymentEvent: EventID, Kind (succeeded, processing, failed, expired, refund_succeeded, refund_failed,
// mandate_active, mandate_revoked), SessionRef, PaymentRef, RefundRef, MandateRef, AmountCents, Currency,
// FeeCents, Method, FailureCode, Metadata map[string]string (marina_id, intent_id only), OccurredAt.

type FiscalDriver interface {
    Code() string
    Issue(ctx context.Context, doc FiscalDocumentRequest) (*FiscalDocumentResult, error)
    Void(ctx context.Context, ref string) error
    Status(ctx context.Context) (DeviceStatus, error)
}

// ChargeExporter sends exported charges to an external invoicing software (spec 16.5). Porto ships none (33 D4).
type ChargeExporter interface {
    Code() string
    Export(ctx context.Context, batch ChargeBatch) (*ExportReceipt, error)
    Poll(ctx context.Context, receipt ExportReceipt) ([]InvoiceRef, error) // InvoiceRef{ChargeID, Number, Date}
}
```

`Extensions` gains `PaymentProviders`, `FiscalDrivers`, `ChargeExporters`. Amounts are `int64` cents with an
explicit ISO currency; no `internal/` import (TestExt_NoInternalImports exists).

### 5.7 Stripe driver (`internal/domain/paydriver/stripe`)

Plain HTTPS through the egress client (no SDK, §16 D11), form-encoded requests, pinned `Stripe-Version` constant,
`Idempotency-Key` header = intent idempotency key, base URL `https://api.stripe.com` (tests: `httptest.Server`
passed to the constructor; `PORTO_STRIPE_API_BASE` honoured only with `PORTO_DEV=true`). Checkout Session
`mode=payment` (card + wallets; `sepa_debit` when enabled), `mode=setup` with `sepa_debit` for mandates; off-session
PaymentIntent `confirm=true` with the stored customer and payment method for SEPA collection; Refunds; retrieve
session / PaymentIntent (with `latest_charge.balance_transaction` for the fee) / refund. Metadata only `marina_id`,
`intent_id`. Webhook: `Stripe-Signature` `t=…,v1=…` (multiple v1 accepted during secret rotation), HMAC-SHA256 of
`t.body`, constant-time compare, tolerance 5 min both ways on the injected `now`; events mapped:
`checkout.session.completed` (paid → succeeded; unpaid async → processing), `checkout.session.async_payment_
succeeded|failed`, `checkout.session.expired`, `payment_intent.succeeded|processing|payment_failed`,
`charge.refunded`, `refund.updated`, `setup_intent.succeeded`, `mandate.updated`; others `ErrIgnoredEvent`
(recorded `ignored`). Errors reduced to type and code (never request echoes). Tests: `TestStripe_Signature{Valid,
Invalid,Replay,Rotated,FutureTimestamp}`, `TestStripe_EventMapping` (fixtures in `testdata/stripe/*.json`, hand
written), `TestStripe_CheckoutRequest` (form fields golden), `TestStripe_ErrorsCarryNoPII`.

---

## 6. Data model (CONTRACT; both dialects, same names; never edit 00001–00009)

Conventions of m3-plan §5 / m4-plan §5 (ids `TEXT`/`UUID`, dates `TEXT`/`DATE`, instants `TIMESTAMP`/`timestamptz`,
JSON `TEXT`/`jsonb`, booleans `INTEGER CHECK IN (0,1)`/`boolean`, every table `id`, `marina_id`, `created_at`,
`updated_at`, `version` where mutable, `UNIQUE (id, marina_id)`, composite FKs, an index starting with `marina_id`).
Existing tables only get `ADD COLUMN` without FK and new indexes.

### 6.1 `00010_charges.sql`

| Table / change | Columns | Constraints |
|---|---|---|
| `billing_settings` | marina_id PK, `default_due_days` (30, 0..365), `auto_payment_request` bool (1), `reminder_policy` json, `reminder_hour` (9), `arrears_policy` json, `export_reminder_days` (30), `fiscal_driver` (`none`), `receipt_footer` text, `sepa_lead_days` (0), `version` | lazily created |
| `charge_origin_items` | `origin_key` CHECK (management_fee, common_advance, common_balance, transfer_fee, termination_penalty, termination_refund, deposit_retention, opening_balance, contract_trueup_fallback), `item_id` | unique (marina_id, origin_key); FK (item_id, marina_id) items |
| `billing_sequences` | marina_id, `series` CHECK (charge, payment, refund, export, request), `period` text (year), `next_number` ≥ 1 | PK (marina_id, series, period) |
| `charges` | §3.1 columns; `origin_type` CHECK (installment, holder_installment, transit, contract_trueup, counter_sale, termination, deposit_retention, manual, adjustment, opening_balance, utility, work_order, rental); `payment_status` CHECK (open, partially_paid, paid, cancelled); `invoicing_status` CHECK (to_invoice, exported, invoiced); `fiscal_mode` CHECK (invoice, receipt); `correction_kind` CHECK (delta, replace) null; `first_export_id` null; `exported_at`, `invoiced_at`, `cancelled_at`; `created_by_user_id` null, `created_by_actor` | unique (marina_id, number); **`charges_origin_primary_uidx`** (§3.3); idx (marina_id, customer_id, payment_status, due_on); idx (marina_id, invoicing_status, issued_on); idx (marina_id, contract_id); idx (marina_id, origin_type, origin_id); CHECK total ≠ 0; CHECK total > 0 ⇒ paid ≥ 0 AND credited ≥ 0 AND paid + credited ≤ total; CHECK total < 0 ⇒ paid = 0 AND credited BETWEEN total AND 0; CHECK customer_id NULL ⇒ fiscal_mode = 'receipt'; FK (corrects_charge_id, marina_id) charges; FK customers, contracts, vessels composite |
| `charge_lines` | §3.1 columns | unique (charge_id, seq); FK (charge_id, marina_id) charges RESTRICT; CHECK net + vat = gross; CHECK (vat_rate_bp = 0) = (vat_nature IS NOT NULL) |
| `charge_offsets` | `negative_charge_id`, `target_charge_id`, `amount_cents` > 0 | insert-only trigger; idx (target_charge_id) |
| `charge_exports` | `number`, `kind` CHECK (export, reexport), `reexport_of` null, `period_from`, `period_to`, `filters` json, `delivery` CHECK (file, exporter), `exporter_code` null, `delivery_status` CHECK (none, pending, sent, failed), `delivery_error` null, `delivery_ref` null, `charge_count`, `line_count`, `totals` json, `csv_file_id`, `xlsx_file_id`, `pdf_file_id`, `csv_sha256`…, `created_by_user_id` | unique (marina_id, number); idx (marina_id, created_at) |
| `charge_export_items` | `export_id`, `charge_id`, `first` bool | unique (export_id, charge_id); **`charge_export_items_first_uidx` unique (charge_id) WHERE first** (INV-53); insert-only trigger |
| `invoice_reconciliations` | `source` CHECK (csv, manual, exporter), `file_sha256` null, `dry_run` bool, `rows`, `matched`, `report` json, `created_by_user_id` | idx (marina_id, created_at) |
| `charge_imports` | `status` CHECK (dry_run, applied, undone, failed), `file_sha256`, `format` CHECK (csv, xlsx), `report` json, `charge_count`, `created_by_user_id` | idx (marina_id, created_at) |
| index on M4 tables | `installments_charge_uidx` unique (charge_id) WHERE charge_id IS NOT NULL; same on `holder_installments` | |
| triggers | §3.2 immutability; `charge_lines` frozen when parent not `to_invoice`; `charge_offsets`, `charge_export_items` insert-only | both dialects (`BEFORE UPDATE/DELETE … RAISE(ABORT)` / plpgsql) |

### 6.2 `00011_cash.sql`

`cash_registers` (unique (marina_id, code); partial unique (marina_id) WHERE is_default), `cash_sessions`
(**`cash_sessions_open_uidx` unique (register_id) WHERE status = 'open'**; trigger: closed rows immutable),
`cash_movements` (CHECK amount > 0, kind CHECK; insert-only), `fiscal_sequences` (PK (marina_id, day), `last_number`).

### 6.3 `00012_payments.sql`

| Table | Constraints (columns in §4–§5) |
|---|---|
| `payments` | unique (marina_id, number); unique (marina_id, idempotency_key) WHERE not null; unique (marina_id, provider_code, provider_ref) WHERE provider_ref not null; CHECKs §4.1; FK (cash_session_id, marina_id) cash_sessions; idx (marina_id, customer_id, received_on); idx (marina_id, cash_session_id) |
| `payment_matches` | idx (payment_id), idx (charge_id); idx (reverses_match_id) WHERE not null (unique until `00017`); CHECK amount ≠ 0; CHECK `payment_matches_reversed_check` (0 ≤ reversed ≤ amount, 0 on reversal rows, `00017`); insert-only trigger (only `reversed_cents` may grow) |
| `refunds` | CHECK amount > 0; kind/status/source CHECKs; CHECK kind = 'payout' ⇒ payment_id NULL; idx (marina_id, payment_id) |
| `fiscal_documents` | `payment_id`, `charge_id`, `driver`, `number`, `status` CHECK (pending, issued, failed, void), `lines` json, `vat` json, `external_ref`, `error_code`, `issued_at`, `voided_at`; unique (marina_id, number); trigger: issued rows immutable except → void |
| `payment_provider_settings` | unique (marina_id, provider_code) |
| `payment_intents`, `payment_intent_charges` | uniques §5.4; partial unique (charge_id) on `payment_intent_charges` WHERE `open_flag = 1` (denormalised flag maintained in the same tx as the intent status; §16 D16) |
| `payment_provider_events` | unique (marina_id, provider_code, event_id); `status` CHECK (applied, ignored, deferred); rows older than 180 days deleted by `payments.reconcile` |
| `sepa_mandates` | partial unique (customer_id, provider_code) WHERE status = 'active' |
| `payment_requests`, `payment_request_charges` | unique (token_hash); unique (request_id, charge_id) |
| `reminder_runs`, `reminder_items` | unique (marina_id, run_on); **unique (charge_id, level)** |
| `dunning_exclusions` | CHECK (customer_id IS NULL) <> (charge_id IS NULL) |
| `work_item_states` | unique (marina_id, source_type, source_id) |

### 6.4 Backup, models, encryption

- `store.BackupTables()` FK order after M4: `billing_settings`, `charge_origin_items`, `billing_sequences`,
  `charges`, `charge_lines`, `charge_offsets`, `charge_exports`, `charge_export_items`, `invoice_reconciliations`,
  `charge_imports`, `cash_registers`, `cash_sessions`, `cash_movements`, `fiscal_sequences`, `payments`,
  `payment_matches`, `refunds`, `fiscal_documents`, `payment_provider_settings`, `payment_intents`,
  `payment_intent_charges`, `sepa_mandates`, `payment_requests`, `payment_request_charges`, `reminder_runs`,
  `reminder_items`, `dunning_exclusions`, `work_item_states`. Excluded: `payment_provider_events` (dedupe cache;
  after a restore providers' retries are harmless because payments are unique by `provider_ref`). Forward
  references (`charges.first_export_id`, `charges.corrects_charge_id`, `installments.charge_id`) are plain columns or
  self FKs loaded through `lateColumns`; trigger-protected rows (exported charges, insert-only tables) restore through
  the M2/M4 late-flip mechanism (insert as `to_invoice`, lines, then flip). Encrypted columns travel encrypted.
- Store model files (CONTRACT writes structs for every column; owners add methods): `internal/store/
  billing_models.go` (→ CHARGES-EXPORT), `payment_models.go` (payments, matches, refunds, work item states →
  PAYMENTS), `collect_models.go` (settings, intents, events, mandates, requests, reminders, exclusions →
  CASH-DRIVERS), `cash_models.go` (registers, sessions, movements, fiscal → CASH-DRIVERS).
- Encryption AADs: `charge-fiscal:<id>`, `payment-ref:<id>`, `paycreds:<marina>:<provider>`,
  `sepa-customer:<id>`, `sepa-pm:<id>`.
- Migration tests: `TestMigrations_UpDownUp`, `…SchemaParity`, `…SameNumbering` cover 00010–00012; new
  `TestMigrations_M5Constraints` asserts on both dialects: origin primary partial unique, export first partial
  unique, open session partial unique, `(charge_id, level)` unique, payment allocation CHECK, charge limit CHECKs,
  immutability triggers (exported charge amount update fails, line insert on exported charge fails, match update
  fails, closed session update fails).

---

## 7. OpenAPI (`api/openapi.yaml`, CONTRACT)

### 7.1 Conventions

As m4-plan §7.1. New tags `charges`, `charge-exports`, `payments`, `credits`, `receivables`, `cash`, `fiscal`,
`payment-requests`, `reminders`, `payment-providers`, `billing-settings`, `worklist`, `payments-public`,
`webhooks-payments`. New path params `ChargeId`, `ExportId`, `PaymentId`, `MatchId`, `RefundId`, `RegisterId`,
`SessionId`, `FiscalDocumentId`, `RequestId`, `RunId`, `ReminderItemId`, `ExclusionId`, `MandateIdSepa`
(`sepaMandateId`), `ImportId`, `PayToken`. Money fields `*_cents` int64; `qty_milli` int64; dates `YYYY-MM-DD`.
Lists with cursor pagination; `format=csv|xlsx` variants listed with `text/csv` and
`application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`. Versioned resources return `ETag`; state actions
require `If-Match`; creating POSTs and every payment/refund/counter sale accept `Idempotency-Key`. PII gating as M3
(`customer{id, display_name}` only with `customers.read`).

### 7.2 Permissions (existing constants in `roles.go`; no new permission)

| Permission (26.1 row) | Roles | M5 use |
|---|---|---|
| `charges.manage` (Addebiti, scadenzario, solleciti) | owner, director, office, accounting | charges CRUD/cancel/adjust (positive), receivables, statements, dashboard billing sections, payment requests, reminders, exclusions, SEPA mandate requests, opening balances, deposits retention |
| `charges.export` (Esportazione, riscontro) | owner, director, accounting | exports, re-exports, files, reconciliation, invoice refs, corrispettivi, payments export |
| `payments.collect` (Incassi e cassa) | owner, director, office, accounting, rental | record/match payments, apply credit, cash sessions, counter sales, receipts, settle instalments (M4 routes) |
| `payments.collect_cash` | + dock | cash payments and counter sales in cash, cash session open/close/movements |
| `refunds.issue` (Rimborsi e addebiti di rettifica) | owner, director, accounting | refunds, payouts, reversals, negative adjustments/termination refunds, credits grant, mandate settlement, fiscal void |
| `providers.configure` | owner | payment provider settings |
| `marina.configure` | owner, director | billing settings, origin items, cash registers |
| `transits.desk` | as M3 | transit payment link, confirm with payment |
| `map.view` | all | map payment status (enum only) |

`TestRoles_MatrixMatchesSpec` unchanged (the constants already exist from M0); new `TestBilling_PermissionsPerRoute`
asserts the table above route by route.

### 7.3 Paths (M = `/api/admin/v1/marinas/{marinaId}`)

#### 7.3.1 Charges, exports, settings (handlers CHARGES-EXPORT: `internal/http/admin/{charges,charge_exports,billing_settings,opening_balances}.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET `M/charges` | listCharges | charges.manage or payments.collect | filters `payment_status?`, `invoicing_status?`, `origin_type?`, `customer_id?`, `contract_id?`, `issued_from/to?`, `due_from/to?`, `overdue?`, `revenue_class?`, `q?` (number), `format?=csv\|xlsx`, cursor | 400 |
| POST `M/charges` | createCharge | charges.manage | `{origin_type: manual\|counter_sale, customer_id?, contract_id?, vessel_id?, due_on?, description?, lines[{item_id, qty_milli, unit_cents?, discount_cents?, description?, service_from?, service_to?}]}` → 201 `Charge` + ETag | 422 no_price/customer_required |
| GET `M/charges/{chargeId}` | getCharge | charges.manage or payments.collect | → `Charge` (lines, matches, offsets, corrections chain, export refs, reminder history) + ETag | 404 |
| PATCH `M/charges/{chargeId}` | updateCharge | charges.manage | If-Match; to_invoice and unpaid: lines, due_on, customer_id, description; exported: due_on, description | 409 charge_immutable, 412 |
| POST `M/charges/{chargeId}/cancel` | cancelCharge | charges.manage | `{reason}` + If-Match → 200 | 409 charge_immutable |
| POST `M/charges/{chargeId}/adjust` | adjustCharge | charges.manage (+ refunds.issue if negative) | `{mode: delta\|replace, lines[], reason}` + If-Match → 201 `{adjustments: Charge[]}` | 409 use_edit/charge_cancelled, 422 |
| PUT `M/charges/{chargeId}/invoice` | setChargeInvoice | charges.export | `{number, date}` → 200 | 409 not_exported |
| POST `M/contracts/{contractId}/termination-charge` | createTerminationCharge | charges.manage (+ refunds.issue if negative) | If-Match (contract) → 201 `Charge` | 409 no_termination_amount/charge_exists |
| GET `M/charge-exports` | listChargeExports | charges.export | cursor | – |
| POST `M/charge-exports/preview` | previewChargeExport | charges.export | `ExportFilter` → 200 `{count, totals_by_vat[], totals_by_origin[], warnings[]}` | 400 |
| POST `M/charge-exports` | createChargeExport | charges.export | `ExportFilter + {exporter_code?}` (Idempotency-Key) → 201 `ChargeExport` | 422 export_empty/export_too_large/provider_unknown |
| POST `M/charge-exports/reexport` | reexportCharges | charges.export | `{export_id} \| {charge_ids[]}` → 201 | 422 |
| GET `M/charge-exports/{exportId}` | getChargeExport | charges.export | → 200 (+ items page) | 404 |
| GET `M/charge-exports/{exportId}/file` | downloadChargeExport | charges.export | `format=csv\|xlsx\|pdf` → binary | 404 |
| GET/POST `M/invoice-reconciliations` | list/createInvoiceReconciliation | charges.export | multipart `file` or JSON `{rows[]}`, `dry_run` → 200 report | 422 csv_invalid |
| GET/PUT `M/billing-settings` | get/updateBillingSettings | charges.manage / marina.configure | `BillingSettings` (+ origin items) + ETag | 412, 422 |
| POST `M/opening-balances/import`, GET `M/opening-balances/imports`, POST `…/imports/{importId}/undo` | … | charges.manage | multipart, `dry_run`, mapping → 200 report | 409 import_not_undoable, 422 |
| GET `M/payments/export` | exportPayments | charges.export | `from, to, method?, format` → binary | 400 |

#### 7.3.2 Payments, credits, receivables, dashboard (handlers PAYMENTS: `internal/http/admin/{payments,credits,receivables,customer_billing,worklist}.go`; M4/M3 edits listed in §14.2)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET `M/payments` | listPayments | payments.collect or charges.manage | `method?`, `kind?`, `source?`, `customer_id?`, `from/to?`, `unallocated?`, `session_id?`, cursor | – |
| POST `M/payments` | recordPayment | payments.collect / collect_cash (cash only) | `RecordPayment` (Idempotency-Key) → 201 `Payment` | 409 match_exceeds/cash_session_required, 422 idempotency_mismatch |
| GET `M/payments/{paymentId}` | getPayment | as list | matches, refunds, fiscal docs | 404 |
| POST `M/payments/{paymentId}/matches` | matchPayment | payments.collect | `{matches[{charge_id, amount_cents}]}` → 200 | 409 match_exceeds |
| POST `M/payments/{paymentId}/matches/{matchId}/reverse` | reverseMatch | refunds.issue | → 200 (reverses what is left) | 409 already_reversed (nothing left) |
| POST `M/payments/{paymentId}/refunds` | refundPayment | refunds.issue | `{amount_cents, via_provider?, method?, reason, unmatch?[]}` (Idempotency-Key) → 201 `Refund` | 409 refund_exceeds, 422 provider_refund_unsupported |
| POST `M/payments/{paymentId}/retain` | retainDeposit | charges.manage | `{amount_cents, item_id?, reason}` → 201 `Charge` | 409 not_deposit/refund_exceeds |
| GET `M/refunds` | listRefunds | payments.collect or charges.manage | filters | – |
| GET `M/credits` | listCredits | charges.manage | customers with credit > 0 | – |
| GET/POST `M/customers/{customerId}/credits` | get/grantCustomerCredit | charges.manage / refunds.issue | grant `{amount_cents, reason, source: goodwill\|release, release_id?}` → 201 `Payment` | 422 release_not_resold |
| POST `M/customers/{customerId}/credits/apply` | applyCustomerCredit | payments.collect | `{charge_ids?}` → 200 `{matched_cents}` | 409 credit_insufficient |
| GET `M/customers/{customerId}/statement` | getCustomerStatement | charges.manage | `from, to, format=json\|pdf\|csv` | 404 |
| GET `M/customers/{customerId}/dashboard` | getCustomerDashboard | customers.read (+ charges.manage sections) | → `CustomerDashboard` | 404 |
| GET `M/receivables` | listReceivables | charges.manage | filters §4.5, `format?` | – |
| GET `M/receivables/ageing` | getReceivablesAgeing | charges.manage | `format?` | – |
| GET `M/maps/{mapId}/payment-status` | getMapPaymentStatus | map.view | `date` → `{resources[{resource_type, resource_id, state, suspended, residual_cents?}]}` | 404 |
| POST `M/mandates/{mandateId}/settle` | settleMandate | refunds.issue | `{through, mode: credit\|payout}` → 200 `{credited_cents, paid_out_cents}` | 409 nothing_to_settle |
| GET `M/worklist`, PATCH `M/worklist/{sourceType}/{sourceId}` | listWorklist / updateWorkItem | contracts.read or charges.manage (per item) | `{assignee_user_id?, status}` → 200 | 404 |
| POST `M/transits/{contractId}/confirm` (M3, extended) | confirmTransit | transits.desk (+ payments.collect / collect_cash for `payment`) | `ManualConfirmation + {payment?{method, amount_cents, register_id?}, fiscal_document?}` | + 409 match_exceeds/cash_session_required |
| POST `M/installments/{installmentId}/settle`, `M/installments/settle`, `M/holder-installments/…/settle` (M4) | unchanged contract | payments.collect | M5 semantics §8.2; response adds `payment_id` | as M4 |

#### 7.3.3 Cash, fiscal, collection, providers (handlers CASH-DRIVERS: `internal/http/admin/{cash,fiscal,payment_requests,reminders,payment_providers,sepa}.go`, `internal/http/webhooks/payments.go`, `internal/http/public/pay.go`)

| Method & path | operationId | Perm | Body → 2xx | Errors |
|---|---|---|---|---|
| GET/POST `M/cash-registers`, PATCH `M/cash-registers/{registerId}` | … | payments.collect / marina.configure | `{code, name, active}` | 409 code_taken/register_in_use |
| GET `M/cash-sessions`, GET `M/cash-sessions/current` | list/getCurrentCashSession | payments.collect or collect_cash | `register_id?` | 404 |
| POST `M/cash-sessions` | openCashSession | payments.collect or collect_cash | `{register_id?, float_cents}` → 201 | 409 cash_session_open |
| GET `M/cash-sessions/{sessionId}` | getCashSession | as above | + live summary | 404 |
| POST `M/cash-sessions/{sessionId}/movements` | addCashMovement | as above | `{kind, amount_cents, reason}` → 201 | 409 cash_session_closed/cash_insufficient |
| POST `M/cash-sessions/{sessionId}/close` | closeCashSession | as above | `{counted[{method, amount_cents}], notes?}` + If-Match → 200 | 409, 412 |
| GET `M/cash-sessions/{sessionId}/z-report` | getZReport | as above | `format=html\|pdf` | 404 |
| POST `M/counter-sales` | createCounterSale | payments.collect / collect_cash | §5.3 (Idempotency-Key) → 201 | 409, 422 customer_required/no_price |
| GET `M/payments/{paymentId}/receipt` | getPaymentReceipt | payments.collect or charges.manage or collect_cash (own cash) | `format=80\|58\|a4` → html/pdf | 404 |
| GET `M/fiscal/daily-totals` | getFiscalDailyTotals | charges.export | `from, to, group, format` | 400 |
| GET `M/fiscal/documents`, POST `…/{fiscalDocumentId}/{retry\|void}` | … | charges.export / refunds.issue | → 200 | 409 |
| GET/POST `M/payment-requests`, POST `M/payment-requests/bulk` | … | charges.manage | `{customer_id, charge_ids[], expires_on?, send?}` / `{charge_ids[] ≤ 500, send}` → 201 | 422 charges_not_open/customer_email_missing |
| GET `M/payment-requests/{requestId}`, POST `…/{send\|cancel}` | … | charges.manage | If-Match | 409 |
| POST `M/transits/{contractId}/payment-link` | createTransitPaymentLink | transits.desk | `{expires_in_hours?, send?}` + If-Match → 200 `{request, url}` | 409 invalid_transition, 422 provider_disabled |
| GET `M/reminder-runs`, GET `M/reminder-runs/{runId}`, POST `M/reminder-runs` | … | charges.manage | run now (same day ⇒ the existing run) | – |
| POST `M/reminders/send` | sendReminders | charges.manage | `{charge_ids[] ≤ 500, level?}` → 200 `{queued, skipped}` | 400 |
| POST `M/reminder-items/{reminderItemId}/{approve\|dismiss}` | … | charges.manage | → 200 | 409 |
| GET/POST `M/dunning-exclusions`, DELETE `…/{exclusionId}` | … | charges.manage | `{customer_id?\|charge_id?, kind, until?, note?}` | 422 |
| GET `M/payment-providers` | listPaymentProviders | payments.collect | → `[{code, capabilities, enabled, mode}]` | – |
| GET/PUT `M/payment-providers/{providerCode}/settings`, POST `…/test` | … | providers.configure | secrets write-only | 422 provider_unknown, 502 provider_error |
| GET/POST `M/customers/{customerId}/sepa-mandates`, POST `…/{sepaMandateId}/revoke` | … | charges.manage | → 201 `{mandate, setup_url}` | 422 provider_disabled |
| POST `/webhooks/payments/{provider}/{marinaId}` | paymentWebhook | none (signature) | raw body → 200/204 | 401 invalid_signature, 404, 503 |
| GET `/api/public/v1/{slug}/pay/{payToken}`, POST `…/checkout`, GET `…/status` | getPayPage / startPayCheckout / getPayStatus | none (token), rate limited | §5.5 | 404 link_invalid, 422 provider_disabled |

Every new `{marinaId}` route is in `TestAcceptance_CrossMarinaIsolation` and every new id param in
`TestAcceptance_CrossMarinaResourceIds` (CONTRACT raises the threshold); `TestPayLink_CrossMarina` covers the
public token (token of marina A with slug B ⇒ 404); `TestPaymentWebhook_CrossTenant` (event signed for A posted to
B ⇒ 401/404, nothing applied).

### 7.4 Problem types (`internal/http/problem/types.go`, `respond/errmap.go`, i18n `errors.*`)

New slugs: `charge_immutable` 409, `charge_cancelled` 409, `use_edit` 409, `match_exceeds` 409
(`details{limit, charge_id?}`), `refund_exceeds` 409, `already_reversed` 409, `credit_insufficient` 409,
`not_deposit` 409, `nothing_to_settle` 409, `charge_exists` 409, `no_termination_amount` 409, `not_exported` 409,
`export_empty` 422, `export_too_large` 422 (`details.count`), `customer_required` 422, `charges_not_open` 422,
`release_not_resold` 422, `cash_session_required` 409, `cash_session_open` 409, `cash_session_closed` 409,
`cash_insufficient` 409, `register_in_use` 409, `provider_disabled` 422, `provider_refund_unsupported` 422,
`provider_error` 502 (`details.code`), `idempotency_mismatch` 422 (if not already present), `billing_item_missing`
422. Reused: `invalid_signature` 401, `link_invalid` 404, `provider_unknown` 422, `import_not_undoable` 409,
`csv_invalid`, `invalid_file`, `file_too_large`, `customer_email_missing`, `invalid_transition`, `no_price`,
`version_conflict`, `precondition_required`, `validation_failed`, `code_taken`, `not_found`, `forbidden`. CONTRACT
writes sentinels: `charge.ErrImmutable`, `ErrCancelled`, `ErrUseEdit`, `ErrExportEmpty`, `ErrExportTooLarge`,
`ErrChargeExists`, `ErrNoTerminationAmount`, `ErrNotExported`, `ErrBillingItemMissing`, `ErrTargetMismatch`;
`payment.ErrMatchExceeds` (`*LimitError`), `ErrRefundExceeds`, `ErrAlreadyReversed`, `ErrCreditInsufficient`,
`ErrNotDeposit`, `ErrNothingToSettle`, `ErrIdempotencyMismatch`, `ErrReleaseNotResold`; `cash.ErrSessionRequired`,
`ErrSessionOpen`, `ErrSessionClosed`, `ErrInsufficient`, `ErrRegisterInUse`; `collect.ErrProviderDisabled`,
`ErrProviderRefundUnsupported`, `ErrProviderError` (`*ProviderError{Code}`), `ErrChargesNotOpen`,
`ErrInvalidWebhookSignature`, `ErrLinkInvalid`, `ErrRetryLater` (503).

### 7.5 Events and SSE

| Type | Public | Topic | Data (ids, enums, dates, amounts, counts only) |
|---|---|---|---|
| `charge.created` | yes | `billing.changed` | `{charge_id, origin_type, origin_id, contract_id?, customer_id?, total_cents, issued_on, due_on, corrects_charge_id?}` |
| `charge.paid` | yes | `billing.changed` | `{charge_id, total_cents, paid_amount_cents}` |
| `charge.cancelled` | yes | `billing.changed` | `{charge_id, origin_type}` |
| `charge.exported` | yes | `billing.changed` | `{export_id, kind, charge_count, period_from, period_to}` |
| `charge.invoiced` | yes | `billing.changed` | `{charge_id, invoice_date, source}` |
| `charge.changed` (internal: updated, offset, due_changed) | no | `billing.changed` | `{charge_id, change}` |
| `installment.charged` | yes | `contract.changed` + `billing.changed` | `{installment_id, holder: bool, contract_id?, right_id?, charge_id}` |
| `payment.succeeded` | yes | `billing.changed` | `{payment_id, customer_id?, kind, method, source, provider_code?, amount_cents, allocated_cents}` |
| `payment.failed` | yes | `billing.changed` | `{intent_id, provider_code, purpose, failure_code?}` |
| `payment.refunded` | yes | `billing.changed` | `{refund_id, payment_id?, kind, amount_cents, status}` |
| `payment.changed` (internal: matched, reversed, credit_applied) | no | `billing.changed` | `{payment_id, change, charge_ids}` |
| `cash.changed` (internal: opened, movement, closed) | no | `billing.changed` | `{register_id, session_id, change}` |
| `fiscal_document.changed` (internal) | no | `billing.changed` | `{fiscal_document_id, status}` |
| `payment_request.changed` (internal: created, sent, paid, cancelled, expired) | no | `billing.changed` | `{request_id, status, charge_count}` |
| `reminder.changed` (internal: run, queued, approved, dismissed) | no | `billing.changed` | `{run_id?, item_id?, level?, change, counts?}` |
| `billing.changed` (internal: settings, exclusions, worklist, providers, mandates) | no | `billing.changed` | `{entity, id, change}` |
| `transit.pending_payment` (internal via `contract.changed`) and `contract.changed` (`suspended` reason arrears) | as M3/M4 | `contract.changed` | unchanged shapes, `reason` enum added |

`billing.changed` is the 27.4 topic. Web `SSE_TOPICS` += `billing.changed`; `keysForMessage`: `billing.changed` →
`charges(m)`, `charge(id)`, `payments(m)`, `receivables(m)`, `cash(m)`, `exports(m)`, `reminders(m)`,
`customerBilling(m, *)`, `mapPayments(m, *)`, `worklist(m)`. Samples for every new type in `Samples()`
(`TestEvents_DataHasNoPII`); `TestSSE_M5Topics`.

Audit actions: `charge.{create,update,cancel,adjust,offset,invoice,backfill}`, `charge_export.{create,reexport,
deliver}`, `invoice_reconciliation.apply`, `opening_balance.{dry_run,apply,undo}`, `billing_settings.update`,
`payment.{record,match,unmatch,retain,credit_grant,credit_apply}`, `refund.{create,complete,fail}`,
`mandate.settle`, `cash_register.*`, `cash_session.{open,movement,close}`, `counter_sale.create`,
`fiscal_document.{issue,fail,void}`, `payment_request.{create,send,cancel,expire,paid}`, `intent.{create,apply}`,
`provider_settings.update`, `sepa_mandate.{request,activate,revoke}`, `reminder.{run,queue,approve,dismiss}`,
`dunning_exclusion.{create,remove}`, `work_item.update`, `contract.suspend` (reason arrears), `transit.request_payment`.
Diffs carry ids, statuses, dates, amounts; never names, fiscal data, references, tokens, secrets.

### 7.6 Meta and jobs

Capabilities `charges`, `charge_exports`, `payments`, `credits`, `cash`, `fiscal.none`, `fiscal.export`,
`fiscal.drivers` (registered extension codes), `payment_requests`, `reminders`, `payments.providers` (registered
codes incl. `stripe`), `payments.sepa`, `charge_exporters` (only when one is registered), `imports.xlsx`,
`opening_balances`. Jobs (registered by CONTRACT in `app.go`, implemented by owners, each idempotent):
`charges.backfill` (1 h + RunAtStart, CHARGES-EXPORT), `charges.exporter` (1 min, CHARGES-EXPORT; no-op without
exporters), `payments.reminders` (1 h, CASH-DRIVERS; includes the arrears policy), `payments.sepa_collect` (15 min,
CASH-DRIVERS), `payments.reconcile` (15 min, CASH-DRIVERS; also deletes provider events > 180 days),
`payments.intents_expire` (1 min, CASH-DRIVERS), `fiscal.issue` (30 s wakeable, CASH-DRIVERS; no-op with inline
drivers). The M4 `installments.issue` job now creates charges through the hook (§8.1).

---

## 8. Rewiring of the M3/M4 hooks

### 8.1 Hook points written by CONTRACT (call sites only; implementations by the owners)

- `internal/domain/contract/billing.go` (new, CONTRACT): `type BillingHooks interface { OnInstallmentIssuedTx;
  OnTransitConfirmedTx; OnContractRepricedTx(reason: move|move_undo|change_stay|addendum|vessel_change|
  early_payment|replan); OnContractClosedTx(change: cancelled|no_show|terminated) }` with a no-op default; field
  `Deps.Billing`; call sites added in the existing functions: `IssueTx` (after the status update; the M4
  `installment_payment_request` email queuing line is **removed** in favour of the hook, which queues the M5
  `payment_request` email with the same dedupe key), `confirmTx`, the reprice paths (`moveprice.go`,
  `applyAdjustment`, replan, change of stay), `terminate`/`cancel`/`no_show`. An error from a hook aborts the
  caller's tx.
- `internal/domain/rights/billing.go` (new, CONTRACT): `BillingHooks { OnHolderInstallmentIssuedTx }`; call site in
  `IssueDueTx`.
- `internal/domain/contract/states.go` (CONTRACT): transit rows `held —request_payment→ pending_payment`,
  `pending_payment —confirm→ confirmed` (only `confirmTx`, source manual or provider), `pending_payment
  —payment_expired→ held`, `pending_payment —expire→ expired`, `pending_payment —cancel→ cancelled`; mooring/charter
  row `active —suspend(arrears)→ suspended` already exists (reason enum added).
- `charge` implements `contract.BillingHooks` and `rights.BillingHooks` (wired in `app.go`).

### 8.2 Settlement through payments (PAYMENTS)

- New `internal/domain/contract/payments.go` (PAYMENTS): `ApplyChargeSettlementTx(scope, actor, installmentID,
  chargeStatus, payment)` — the instalment follows its charge: `partially_paid` (new transition, M4 CHECK already
  allows it) or `paid` through the existing `settleTx` with `source = payment` and the payment's method (INV-32),
  or back to `issued`/`partially_paid` on reversal (`unsettle`, audit `installment.unsettle`); deposit completion of
  renewal proposals keeps working (it is inside `settleTx`). `ConfirmTransitPaidTx(scope, actor, contractID,
  source)` (wrapper over `confirmTx`, used by the webhook and by desk confirmation with payment),
  `RequestTransitPaymentTx`, `RevertTransitPaymentTx` (pending_payment → held), `SuspendForArrearsTx`.
- `SettleInstallment` / `SettleInstallments` (M4 routes, `settle.go`, PAYMENTS edits): an instalment with a charge
  is settled by `payment.RecordTx(manual, method, amount = charge residual, reference)` + `MatchTx`, which drives
  the status through `ApplyChargeSettlementTx`; a `scheduled` instalment is first issued (`IssueTx` → charge) in the
  same tx. The direct `SettleInstallmentRow` call with `source = manual` remains reachable only from the backfill
  path for historical rows (`TestInstallment_SettleOnlyManualDriver` is updated: settled ⇔ via payment or
  backfill). Bulk settlement keeps "all or nothing" and its lock order (contracts sorted, then payment, then charges
  sorted).
- Same for `rights.SettleHolderInstallment(s)` (`internal/domain/rights/holders.go`, PAYMENTS edits only those
  functions) and new `internal/domain/rights/payments.go` (`ApplyChargeSettlementTx`).
- Transit desk confirmation with `payment` (M3 route): `ConfirmTransitPaidTx(source = manual)` → charge via hook →
  `RecordTx` + `MatchTx` in one tx. Without `payment` the M3 behaviour stays (charge created, open, due on arrival
  or departure).

### 8.3 Ephemeral documents

`receipt`, `statement_of_account` and `charge_export_summary` are rendered with the M4 pipeline (template language,
layout, fpdf) but **not archived** in `generated_documents` (its `subject_type` CHECK is frozen and these documents
are reproducible from the ledger): `docs.Service.RenderEphemeral(ctx, scope, kind, locale, view) ([]byte, error)`
renders the marina's published template of the kind or the built-in default, deterministic bytes. The export
summary PDF is then stored encrypted in `stored_files` with its sha256 on the export row; statements and receipts
are streamed. Marina templates of these kinds become publishable in the templates editor (catalogue
`available_from` cleared by CONTRACT).

---

## 9. Web

### 9.1 Routes (`web/admin`, TanStack code-based; CONTRACT adds a `billingRoutes` factory in `router.tsx`, nav group "Amministrazione")

| Route | Page | Permission (UI) |
|---|---|---|
| `/m/$marinaId/charges` + `/charges/$chargeId` | list with filters and saved views (Da incassare, Scaduti, Da fatturare, Rettifiche), CSV/XLSX; detail: lines, VAT summary, status badges, matches, offsets, corrections chain, exports, reminders; actions edit (to_invoice), cancel, adjust (delta/replace dialog with live totals), set invoice ref, request payment, collect | charges.manage |
| `/m/$marinaId/charges/new` | manual charge / counter sale on account: customer picker, item lines with price from the list, VAT preview | charges.manage |
| `/m/$marinaId/payments` + `/payments/$paymentId` | list, record payment dialog (customer, method, amount, **matching table** of open charges with auto-fill oldest first, remaining as credit shown live), detail with matches, reverse, refund dialog (unmatch + amount, via provider when possible), receipt print/email, deposit retain | payments.collect |
| `/m/$marinaId/cash` | current session per register: open with float, live totals by method, **Vendita al banco** (item grid/search, qty, customer optional, method, fiscal document toggle, receipt 80/58/A4), movements, close wizard (counted per method, difference), Z report; history tab | payments.collect or collect_cash |
| `/m/$marinaId/fiscal` | corrispettivi by day/month per rate and method, CSV/XLSX; fiscal documents list, retry/void | charges.export |
| `/m/$marinaId/receivables` | scadenzario (charges + future instalments) with age buckets, filters, bulk "Invia richiesta di pagamento", "Sollecita", CSV/XLSX; ageing per customer tab (sospesi) | charges.manage |
| `/m/$marinaId/reminders` | runs and items by level, approval queue for final notices, exclusions (customer/charge, kind, until) | charges.manage |
| `/m/$marinaId/payment-requests` | requests list, status, resend/cancel, copy link | charges.manage |
| `/m/$marinaId/exports` + `/exports/new` + `/exports/$exportId` | export wizard (period, filters → preview totals per VAT and origin, warnings with links to customers → confirm → downloads CSV/XLSX/PDF); history; re-export; detail | charges.export |
| `/m/$marinaId/exports/reconcile` | upload CSV, dry-run report, apply | charges.export |
| `/m/$marinaId/opening-balances` | import wizard (CSV/XLSX, mapping, dry run, apply, undo) | charges.manage |
| `/m/$marinaId/customers/$customerId` (M1 page, extended) | tab **Cruscotto** (11.4 sections, ageing bar, quick actions) and tab **Estratto conto** (period, table with running balance, PDF/CSV) | customers.read (+ charges.manage) |
| `/m/$marinaId/worklist` | office worklist: grouped by kind, assignee picker, status | contracts.read or charges.manage |
| `/m/$marinaId/settings/billing` | billing settings (due days, auto request, origin items per key with item picker, reminder levels editor, arrears policy, receipt footer, fiscal driver), cash registers | marina.configure |
| `/m/$marinaId/settings/payments` | providers: manual always on; Stripe card with enable switch, test/live mode, secret key and webhook secret (write-only, masked), SEPA toggle, webhook URL to copy, test connection; banner "Porto funziona anche senza pagamenti online" | providers.configure |
| ops map lens **Pagamenti** | `getMapPaymentStatus` composed with the day view; legend states of 9.2; dock sees the warning badge on departure | map.view |
| transit detail (M3, extended) | "Incassa" (confirm with payment), "Invia link di pagamento" (when a provider is enabled), charge panel | transits.desk |
| contract detail **Rate** tab (M4, extended) | charge number/status per instalment, collect, payment request, SEPA mandate state | contracts.read |
| mandate detail (M4, extended) | "Liquida" (credit or payout) | refunds.issue |

Public page in `web/booking`: `/b/$slug/pay/$token` — amount, charges, bank transfer instructions (IBAN with copy
button, causale), "Paga online" button only when `online`, return page `/b/$slug/pay/$token/return` polling status
("Pagamento in verifica" → "Pagamento ricevuto"); phone-first (390 px), axe clean.

### 9.2 Components and shared pieces

CONTRACT adds through the shadcn CLI in `web/shared` whatever is missing among `toggle-group`, `scroll-area`,
`hover-card`, `chart` (only if the cash/ageing bars need it; otherwise plain progress bars from `progress`).
Shared hooks file `web/shared/src/api/hooks/billing.ts` (stub by CONTRACT, filled by WEB) and
`web/shared/src/api/hooks/payPublic.ts`. Query keys `queryKeys.billing(m)`: `charges(filter)`, `charge(id)`,
`payments(filter)`, `payment(id)`, `receivables(filter)`, `ageing`, `cashSessions(register)`, `cashSession(id)`,
`registers`, `fiscalTotals(range)`, `fiscalDocs`, `exports`, `export(id)`, `reconciliations`, `requests`,
`reminderRuns`, `reminderRun(id)`, `exclusions`, `providers`, `providerSettings(code)`, `billingSettings`,
`customerDashboard(id)`, `customerStatement(id, range)`, `customerCredits(id)`, `mapPayments(mapId, date)`,
`worklist`. Money formatting helper `formatCents` exists (M2); a `MoneyInput` (shadcn `input` + parsing to cents
with `money` rules, no floats: string → cents parser mirrored from Go `ParseCents` with shared vectors
`testdata/money/parse.json`) is added by CONTRACT in `web/shared/src/money/`.

### 9.3 i18n namespaces (CONTRACT creates the files; WEB fills except enums)

`charges`, `payments`, `cash`, `fiscal`, `receivables`, `reminders`, `exports`, `billingSettings`,
`paymentProviders`, `customerBilling`, `worklist`, `billingEnums` (CONTRACT writes complete: every status, origin,
method, kind, level, exclusion kind, worklist kind, ageing bucket, lens state), `payPublic` (booking SPA).
`errors.json` gets the §7.4 slugs (CONTRACT).

---

## 10. Demo seed (`internal/app/seed_billing.go` CHARGES-EXPORT, `seed_payments.go` PAYMENTS, `seed_cash.go` CASH-DRIVERS)

Runs after the M4 seed when the demo marina has no charges (idempotent; second run no-op), through the services as
the demo owner, seeded `math/rand/v2` (PCG 51, 5), app clock T:
- CHARGES-EXPORT: billing settings (due 30 days, auto requests on), origin items (creates items `ONERI` 22 %,
  `COND` 22 %, `SUBENTRO` 22 %, `PENALE` N1, `RIMBORSO` 22 %, `CAUZIONE` N1, `SALDO` N1) — VAT choices are demo data, documented as such; backfill of the M4 instalments (charges for every
  issued/paid instalment, payments for paid ones); charges for confirmed M3 transits; 1 addendum price change
  adjustment; 1 termination charge; 2 exports of last month (one reconciled from a demo CSV with invoice numbers
  `FT-<n>`), current month left `to_invoice`; 12 opening balances imported.
- PAYMENTS: payments history (60 % bank transfer, 25 % POS card, 15 % cash) with partial payments, 3 customers with
  credit (one overpayment, one goodwill, one mandate settlement as credit), 1 refund, 1 deposit (keys) with partial
  retention, statements coherent.
- CASH-DRIVERS: default register + `banchina` register, 3 closed sessions in the last week (one with a −2,00 €
  difference) and 1 open today, 40 counter sales (fuel coupon, water, electricity tokens, crane, laundry as demo
  items from the M2 catalogue), fiscal driver `none` with documents, payment requests for the overdue instalments, a
  reminder run of yesterday with courteous/formal items and 1 final notice awaiting approval, 2 exclusions
  (repayment plan, disputed), arrears policy disabled; **no provider settings row** (Stripe disabled: the demo is
  fully offline).
- Prints M5 counts through `SeedResult`; `TestSeed_DemoM5Coherent`: `CheckBillingInvariants` ∅,
  `CheckContractInvariants` ∅, `CheckOccupancyInvariants` ∅, every closed session reconciles.

---

## 11. Tests and acceptance

### 11.1 Gates (exact commands, from the repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated            # empty
make check                                                    # gen-check + lint (depguard with the new pure packages) + unit
make test-db                                                  # integration on SQLite AND PostgreSQL
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
go test ./internal/domain/chargeplan/... ./internal/domain/cash/summary/... ./internal/domain/collect/levels/... ./internal/xlsx/... -count=1 -rapid.checks=5000
go test ./internal/xlsx/... -run '^$' -fuzz FuzzXLSX_Read -fuzztime 60s
go test ./internal/domain/paydriver/... -count=1
scripts/with-postgres.sh go test -tags=integration -count=1 -race ./internal/domain/charge/... ./internal/domain/payment/... ./internal/domain/collect/... ./internal/domain/cash/... ./internal/domain/fiscal/... ./internal/domain/contract/... ./internal/domain/rights/... ./internal/store/...
make test-race                                                # every TestConcurrency_* incl. §11.3 on both dialects
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M5|TestAcceptance_M4|TestAcceptance_M3|TestAcceptance_M2|TestAcceptance_M1|TestAcceptance_M0|TestAcceptance_CrossMarina|TestPublicLinks_|TestPayLink_'
make e2e                                                      # all specs incl. §11.5
scripts/agents/check-naming.sh && make licenses              # no new Go or npm dependency in M5
rm -f tmp/data/porto.db* && make seed && go run ./cmd/porto seed demo   # second run no-op; prints M5 counts
```

`scripts/with-postgres.sh` runs the command with `PORTO_TEST_POSTGRES_URL` set, so `testdb` runs **both dialects**.

### 11.2 Unit and property tests (names fixed; referenced by `docs/invariants.md`)

- CHARGES-EXPORT: **`TestChargePlan_SplitSumsToTarget`**, `TestChargePlan_TargetEqualsSnapshotWithoutAdjustments`,
  `TestChargePlan_VATCumulativeNeverDrifts`, `TestChargePlan_Scenario` (§3.4); `TestXLSX_RoundTrip` (rapid: random
  sheets of strings/decimals/dates written then read back equal), `FuzzXLSX_Read` (no panic, bounded memory: ≤ 50 MiB
  uncompressed, ≤ 100 000 cells, zip-bomb ratio check), `TestXLSX_OpensInSpreadsheetSchema` (parts and content types
  golden); `TestExportCSV_Golden`, `TestExportXLSX_Golden` (sheet XML golden), `TestExportRows_Deterministic`,
  `TestReconcileCSV_Parse` (table: separators, BOM, dates, numbers, bad rows).
- PAYMENTS: `TestAgeing_Buckets` (table), `TestStatement_RunningBalance` (pure builder), `TestMatchPlan_OldestFirst`
  (rapid: never above residuals or payment, remainder = credit), `TestVATSplit_PaymentShare` (rapid: Σ payment
  shares per bucket over full payment of a charge = charge buckets).
- CASH-DRIVERS: **`TestCashSummary_Reconciles`** (rapid: expected = float + payments − refunds ± movements per
  method; difference = Σ counted − Σ expected), `TestCorrispettivi_TotalsEqualPayments` (rapid), `TestLevels_Next`
  (table + rapid: levels strictly increasing, never a level twice, pre-due/due/levels by days),
  `TestStripe_Signature{Valid,Invalid,Replay,Rotated,FutureTimestamp}`, `TestStripe_EventMapping`,
  `TestStripe_CheckoutRequest`, `TestStripe_ErrorsCarryNoPII`, `TestReceiptHTML_NoScriptAndNotFiscal` (contains
  "Non valido ai fini fiscali", no `<script`, no external URL), `TestPayToken_HashOnly`.

### 11.3 Integration and concurrency tests (tag `integration`, `testdb` both dialects, `-race`)

CHARGES-EXPORT: `TestInstallment_IssueCreatesCharge` (job, bulk, deposit at signature; `charge_id` set once;
`installment.charged`; one payment-request email per instalment), `TestHolderInstallment_IssueCreatesCharge`
(fees, advances, negative balance → credit, transfer fee item, missing origin item → worklist, no charge),
`TestTransit_ConfirmCreatesCharge` (manual, pay at departure due date, change of stay before export rewrites, after
export adds delta, cancel after confirm), `TestContract_TrueUpAfterLastInstalment`, **`TestCharge_ExportedIsImmutable`**
(INV-51: service refusal + trigger on both dialects for amounts and lines; adjustments linked),
**`TestChargeExport_EachChargeOnce`** (INV-53: overlapping periods, filters, re-export flags, cancelled never
exported), **`TestConcurrency_ChargeExportParallel`** (100 goroutines creating exports over the same period: every
charge in exactly one first export, no item without status `exported`), `TestCharge_OneLivePrimaryPerOrigin` +
**`TestConcurrency_IssueSameInstallment`** (INV-54: 100 parallel issues of the same instalment ⇒ one charge),
`TestChargeExport_FilesMatchItems` (CSV/XLSX parsed back = items × lines), `TestInvoiceReconciliation_CSV`,
`TestChargeExporter_FakeRoundTrip` (fake exporter registered in the test: send, poll, invoiced),
`TestOpeningBalances_ImportUndo`, `TestCharge_BackfillM4Idempotent`, `TestCharge_AnonymisedCustomerKeepsSnapshot`.

PAYMENTS: **`TestCharge_PaidAmountMatchesLedger`** (INV-50, rapid integration: random sequences of record, match,
reverse, refund, credit apply, retention, adjustments with offsets ⇒ B50 ∅ after each step),
**`TestConcurrency_PaymentMatching`** (INV-52: one payment of 100,00 and 100 goroutines matching 1,00..5,00 to 10
charges of 30,00 each, and 100 payments matching the same charge: Σ never above payment nor charge, affected-count
losers get `match_exceeds`, both dialects), `TestConcurrency_RefundVsMatch` (refunds and matches on the same
payment in parallel never exceed the amount), `TestConcurrency_CreditApply` (two staff applying the same credit),
`TestConcurrency_InstallmentSettle` (M4 name, rewired: 100 parallel settlements ⇒ one payment, instalment paid
once), `TestInstallment_SettleOnlyManualDriver` (updated: paid ⇔ payment or backfill), `TestConcurrency_ReplanVsSettle`
(M4 name, still green), `TestPayment_IdempotentRecord` (same key replays, different body 422, concurrent losers
return the winner), `TestCredits_UnallocatedIsCredit`, `TestDeposit_RetainAndRelease`, `TestMandate_SettleCredit`
and `…Payout`, `TestStatement_ClosingEqualsBalance`, `TestMapPaymentStatus_States`, `TestWorklist_Assign`,
`TestTransit_DeskConfirmWithPayment`.

CASH-DRIVERS: `TestCash_OneOpenSessionPerRegister` + **`TestConcurrency_OpenSession`** (100 opens ⇒ one),
**`TestConcurrency_CashCloseVsPayment`** (payments race a close: each payment is in the closed totals or failed
`cash_session_closed`), `TestCounterSale_AllInOneTx` (failure in fiscal step rolls back charge and payment),
**`TestConcurrency_FiscalSequence`** (100 receipts same day ⇒ numbers 1..100 unique), `TestFiscal_DeferredDriver`,
`TestPaymentWebhook_ConfirmsTransit` (pending_payment → confirmed, source provider, charge paid),
`TestPaymentWebhook_Deduplicated`, **`TestConcurrency_WebhookDuplicate`** (100 identical deliveries ⇒ one payment),
`TestPaymentWebhook_OutOfOrder` (expired after succeeded changes nothing; succeeded after expired applied),
`TestPaymentWebhook_LatePayment` (credit + worklist), `TestPaymentWebhook_CrossTenant`,
**`TestCheckout_BrowserReturnDoesNotConfirm`** (INV-09), `TestReconcile_FixesMissedWebhook`, `TestSEPA_MandateAndCollect`
(fake Stripe server: setup → active; due charge → intent; processing; succeeded days later ⇒ instalment paid),
`TestRefund_ViaProviderPendingThenSucceeded`, `TestPaymentRequest_PublicPage` (manual-only marina shows bank
details and `online=false`), `TestPayLink_CrossMarina`, `TestReminders_LevelsOncePerCharge`,
**`TestConcurrency_ReminderRunOnce`** (100 parallel runs of the same day ⇒ one run, one email per customer/level),
`TestReminders_StopWhenPaid`, `TestReminders_Exclusions`, `TestReminders_FinalNoticeNeedsApproval`,
`TestArrears_SuspendsContract` (never transits; resume manual), `TestProviderSettings_SecretsWriteOnly`,
`TestOffline_AllFlowsWithManualDriver` (marina without provider rows: request, reminder, payment, refund, export
work; no outbound HTTP attempted — egress client replaced by one that fails the test on any call).

Shared: `TestEvents_DataHasNoPII` (M5 samples), `TestSSE_M5Topics`, `TestLogs_NoPII` (webhook and export paths),
`TestBilling_PermissionsPerRoute`, `TestMigrations_M5Constraints`.

### 11.4 Acceptance tests (`internal/acceptance`, tag integration, API only, `clock.Fake`, in-memory mailbox, both dialects)

**`TestAcceptance_M5_SimulatedYear`** (CHARGES-EXPORT; spec 32.1 "Esportazione" and 32.3 M5). Scenario generator
`m5_sim_test.go` (CHARGES-EXPORT; deterministic PCG(51, 5); data under `testdata/m5/year/`) over a fresh marina
with the M2 demo catalogue (rates 22 %, 10 %, 4 %, N1, N4 items), clock from 2027-01-01 to 2027-12-31 one day at a
time (jobs run each day: `installments.issue`, `charges.backfill`, `payments.reminders`, `payments.intents_expire`):
- 60 mooring contracts (annual, seasonal, monthly; policies deposit 30 % + quarters, fixed dates, single, early
  payment) with services; 8 addenda (price change, service add/remove), 6 vessel changes, 10 repriced moves, 4
  terminations with termination charges (penalty and refund), 3 replans;
- 20 rights with fee runs (two semesters), common-charge advance + balance (credits for some holders), 2 transfers
  with takeover fee;
- 400 transits (desk confirm with payment, pay at departure, change of stay before and after export, cancel after
  confirm, no-show, 30 via payment link with the **fake provider** registered in the test fixture: webhooks with
  duplicates and out-of-order events);
- 600 counter sales (half with fiscal document, some walk-in), 10 manual charges, 15 adjustments (delta and
  replace) of exported charges, 5 deposit retentions;
- payments: random manual (cash in daily sessions opened/closed each day, POS, bank transfers, cheques), partial,
  overpayments (credit), credit applications, 40 refunds (manual and provider), 20 match reversals;
- exports every Monday for the previous week (filters by origin on some weeks, a re-export of one week), monthly
  invoice reconciliation CSV for 90 % of exported charges.

Assertions (after the last day, plus B-queries daily):
1. **Exactly once**: every non-cancelled charge (except opening balances) appears in exactly one first export;
   cancelled charges never; the union of the parsed CSV files of all first exports has each charge's lines exactly
   once (re-exports excluded) and equals the DB items; XLSX files parse to the same rows.
2. **VAT per rate equals the snapshots**: for every contract that reached its last instalment (or true-up) and every
   transit, Σ of the exported lines of its origins (instalments, true-up, adjustments of those) per (rate, nature) of
   net, VAT and gross = `chargeplan.Totals(ContractTarget(snapshot, adjustments))`; for contracts and transits with no
   adjustments also = `Breakdown.VATSummary` byte for byte; holder instalments per (rate, nature) = Σ of their
   instalment amounts split by the origin item VAT.
3. Per export: stored totals = Σ of its file rows per (rate, nature); grand total = Σ charge totals.
4. Invariants: `CheckBillingInvariants` (B50..B58) ∅ every day; `CheckContractInvariants` and
   `CheckOccupancyInvariants` ∅ at the end; invoiced charges = reconciled rows.

**`TestAcceptance_M5_LedgerReconciles`** (PAYMENTS) runs the same simulation (shared generator, read-only for
PAYMENTS) and asserts: for every customer the statement closing balance = the API balance = Σ residuals of open charges −
credit, and = Σ charge totals − Σ payment amounts + Σ refunds over the whole year; Σ payments − Σ refunds per method and day = cash session
totals (cash) and payments export; Σ matches = Σ paid amounts; every refund ≤ unallocated at its time (replay of the
ledger); every instalment `paid` ⇔ its charge `paid`; every confirmed transit paid online has exactly one provider
payment and was confirmed only by a webhook event.

**`TestAcceptance_M5_CashDay`** (CASH-DRIVERS): two registers, sessions, 120 counter sales with and without fiscal
document, refunds, movements, close with counted amounts ⇒ Z report totals = summary; corrispettivi of the day per
rate = Σ fiscal documents; receipts HTML for 80/58 and PDF A4 render.

**`TestAcceptance_M5_OnlinePayments`** (CASH-DRIVERS, fake Stripe `httptest` server with real signatures): payment
link on a held transit → pending_payment → browser return (no confirm) → signed webhook → confirmed + charge paid;
duplicate and replayed webhooks; payment request for 3 instalment charges paid by card; SEPA mandate setup and
collection of two instalments (one failing then retried by reminder link); provider refund pending → succeeded;
reconciliation of a missed webhook; then provider disabled ⇒ public page shows bank details only.

**`TestAcceptance_M5_OfflineManualOnly`** (CASH-DRIVERS): the whole charge → request → reminder → manual payment →
refund → export → reconciliation path on a marina with no provider and an egress client that fails on any call.

### 11.5 Web tests

Vitest + Testing Library for each page (WEB): `MoneyInput` parsing vectors shared with Go, matching table
(auto-fill, never above residual, credit preview), close-session difference, export wizard, lens function
`web/shared/src/map/lenses/payments.ts` + test, pay page states. Playwright specs (WEB): `charges.spec.ts`,
`payments-matching.spec.ts`, `cash-session.spec.ts` (open, counter sale with receipt print view, close, Z),
`receivables.spec.ts` (bulk request and remind; mailbox shows the emails), `reminders.spec.ts`, `exports.spec.ts`
(export, download CSV/XLSX/PDF, reconcile CSV), `customer-dashboard.spec.ts` (ageing, statement PDF),
`payment-settings.spec.ts` (disabled by default, secrets masked), `pay-link.spec.ts` (booking page, manual-only:
bank details, no online button; axe), `payments-lens.spec.ts`, `billing-m5-acceptance.spec.ts` (charges of a seeded
month → export → reconcile → statement), all on the real binary without network.

### 11.6 Done criteria per builder

Each builder finishes with its subset green (`go test` of its packages, `-tags=integration` on both dialects,
`-race` for concurrency, `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test`,
its e2e specs) and runs the `invariants-guardian` subagent (CHARGES-EXPORT, PAYMENTS, CASH-DRIVERS) and the
`reviewer` subagent. The orchestrator runs §11.1 in full.

---

## 12. Invariants (`docs/invariants.md`; CONTRACT writes the entries, the orchestrator flips statuses)

- **INV-09** extended: transits reach `confirmed` from `pending_payment` only through `confirmTx` called by the
  provider webhook (`ConfirmTransitPaidTx(source=provider)`) or the manual driver; the browser return never
  confirms. Verified also by `TestCheckout_BrowserReturnDoesNotConfirm`, `TestPaymentWebhook_ConfirmsTransit`.
  Stato: partial (online checkout M8).
- **INV-32** → implemented: instalments become `paid` only via payment matching (`ApplyChargeSettlementTx` →
  `settleTx(source=payment)`) or the M4 backfill of historical manual settlements.
- **INV-50** → implemented as restated in §4.1 (query B50, `TestCharge_PaidAmountMatchesLedger`).
- **INV-51** → implemented (`TestCharge_ExportedIsImmutable`, triggers, `TestMigrations_M5Constraints`).
- **INV-52** → implemented (`TestConcurrency_PaymentMatching`, `TestConcurrency_RefundVsMatch`, CHECKs, query B52).
- **INV-53** → implemented with the precision: opening-balance charges are born `invoiced` and are not exported
  (`TestChargeExport_EachChargeOnce`, `TestConcurrency_ChargeExportParallel`, `TestAcceptance_M5_SimulatedYear`,
  partial unique `charge_export_items_first_uidx`).
- **INV-54 — Un solo addebito primario vivo per origine.** For instalments, holder instalments, transits,
  terminations and deposit retentions at most one non-cancelled charge without `corrects_charge_id` per origin (DB
  partial unique). `TestCharge_OneLivePrimaryPerOrigin`, `TestConcurrency_IssueSameInstallment`. Stato:
  implemented.
- **INV-55 — Una sola sessione di cassa aperta per cassa; chiusura coerente.** Partial unique; a closed session's
  difference = Σ counted − (float + payments − refunds ± movements) and never changes. `TestCash_OneOpenSessionPerRegister`,
  `TestConcurrency_OpenSession`, `TestConcurrency_CashCloseVsPayment`, `TestCashSummary_Reconciles`, query B55.
  Stato: implemented.
- **INV-56 — Eventi dei provider applicati una sola volta.** Each provider event id (and each synthetic
  reconciliation id) produces effects at most once; payments are unique by provider reference.
  `TestConcurrency_WebhookDuplicate`, `TestPaymentWebhook_Deduplicated`, `TestReconcile_FixesMissedWebhook`. Stato:
  implemented.
- **INV-57 — Gli addebiti di un'origine sommano il suo obiettivo.** For every contract with no scheduled instalment
  left and every confirmed transit, Σ of its charges per line key and per (rate, nature) = `chargeplan` target; for
  unadjusted snapshots = `VATSummary`. `TestChargePlan_SplitSumsToTarget`, `TestAcceptance_M5_SimulatedYear`, query
  B57. Stato: implemented.
- **INV-58 — Promemoria al più una volta per livello.** Each (charge, level) is reminded at most once; one reminder
  run per marina and day; paid charges are never reminded. `TestConcurrency_ReminderRunOnce`,
  `TestReminders_StopWhenPaid`. Stato: implemented.
- INV-70 partial with the M5 samples; INV-73 covers the M5 routes; INV-74 extended to export files (encrypted
  stored files, downloads only with `charges.export`); INV-75 rules applied to payment-request tokens
  (`TestPayLink_CrossMarina`); INV-77 dedupe keys for payment requests and reminders.

`store.CheckBillingInvariants(ctx, db, scope)` (CONTRACT writes the dispatcher in `internal/store/billing_invariants.go`
calling `checkChargesInvariants` (B51 lines/total coherence, B53, B54, B57: CHARGES-EXPORT, file
`billing_invariants_charges.go`), `checkPaymentsInvariants` (B50, B52, credit ≥ 0, instalment ⇔ charge status:
PAYMENTS, `billing_invariants_payments.go`), `checkCashInvariants` (B55, fiscal numbering unique, reminders B58:
CASH-DRIVERS, `billing_invariants_cash.go`); CONTRACT creates the three files with stubs returning nil).

---

## 13. Order of work inside each builder

**CONTRACT** 1. Migrations 00010–00012 both dialects + `TestMigrations_M5Constraints`. 2. Store models, backup
registration and round-trip seeds (one row per new table). 3. OpenAPI §7, `make gen`; events/payloads/samples,
topic `billing.changed`, SSE web topic, problems, errmap, meta, route-permission test skeleton. 4. `pkg/ext`
payment/fiscal/exporter files + `Extensions` fields. 5. Skeletons with sentinels and exported signatures:
`chargeplan` (types + stub funcs), `charge` (service, errors, types, `PaymentRequester`, hook impl stubs), `payment`
(service, errors, types, `RecordTx`, `MatchTx`, `ReverseMatchTx`, `RefundTx`, `GrantCreditTx` signatures),
`collect` (service, errors, `ApplyProviderEvent`, `RequestForChargeTx`), `collect/levels`, `paydriver` registry +
`manual` (complete, trivial), `paydriver/stripe` (doc.go), `cash`, `cash/summary`, `fiscal` (with `none`/`export`
driver types), `xlsx` (doc.go), `docs` additions (§8.3: kind `charge_export_summary`, views for `receipt`,
`statement_of_account`, `charge_export_summary` complete and frozen, catalogue `available_from` cleared, signature
of `Service.RenderEphemeral`). 6. Hook points §8.1 (contract/rights billing.go, call sites, M4 email line removed,
states rows) with no-op default so M4 tests stay green. 7. Handler stubs `internal/http/admin/m5_stubs.go`,
`internal/http/public/m5_stubs.go`, `internal/http/webhooks/payments.go` stub, router mount of
`/webhooks/payments/{provider}/{marinaId}` (raw body preserved, access-log redaction of pay tokens); app wiring,
registries, job registration with no-op implementations; seed hooks. 8. depguard pure list. 9. Web foundations:
shadcn components, i18n namespaces (`billingEnums` complete), query keys, SSE, router/nav, route stubs, booking pay
route skeleton, `MoneyInput` + vectors, e2e helper stubs `e2e/support/billing.ts`. 10. `docs/invariants.md`
(§12), CHANGELOG. 11. `make check`, `make test-db` green with stubs.

**CHARGES-EXPORT** 1. `chargeplan` + property tests. 2. `xlsx` + fuzz. 3. Charge store, numbering, create/edit/
cancel. 4. Contract and rights hook implementations (instalments, holders, transits, true-up, sync, close). 5.
Adjustments, offsets (calls `payment.GrantCreditTx`), termination charge. 6. Backfill job. 7. Export (preview,
create, files, re-export, PDF via `docs/ephemeral.go`), reconciliation, ChargeExporter job. 8. Opening balances +
csvimport xlsx. 9. Billing settings. 10. Handlers; delete stub methods. 11. Integration/concurrency tests. 12.
Simulation generator + `TestAcceptance_M5_SimulatedYear`. 13. Seed.

**PAYMENTS** 1. Payment store, `RecordTx`, `MatchTx`, reversals, idempotency. 2. `contract/payments.go`,
`rights/payments.go`, rewiring of settle functions. 3. Refunds (manual; provider path through `collect` after
commit), credits, deposits, mandate settlement. 4. Balances, ageing, receivables, statement (+ PDF), dashboard, map
status, worklist. 5. Transit desk confirm with payment (handler edit). 6. Handlers. 7. Concurrency and ledger
tests. 8. `TestAcceptance_M5_LedgerReconciles`. 9. Seed.

**CASH-DRIVERS** 1. `cash/summary`, cash registers/sessions/movements, Z report. 2. `fiscal` (drivers none/export,
numbering, corrispettivi). 3. Counter sales and receipts (HTML + PDF). 4. `paydriver` registry, provider settings,
Stripe driver with fake server tests. 5. Intents, webhook handler and `ApplyProviderEvent`, reconciliation, expiry
jobs. 6. Payment requests, public pay page, transit payment link. 7. SEPA mandates and collection. 8. `levels`,
reminders, exclusions, arrears. 9. Handlers. 10. Tests, acceptance (cash day, online payments, offline). 11. Seed.

**WEB** 1. Hooks. 2. Charges list/detail/new. 3. Payments with matching and refunds. 4. Cash and counter sale,
receipts print view. 5. Receivables, payment requests, reminders, worklist. 6. Exports wizard, reconciliation,
opening balances. 7. Customer dashboard and statement tabs. 8. Settings billing and payments. 9. Lens Pagamenti,
transit and contract extensions, mandate settle. 10. Booking pay page. 11. Vitest, e2e helpers, specs, acceptance
spec last.

---

## 14. Strict file ownership

Order: **CONTRACT** runs first, alone, and finishes when `make gen && go build ./... && make check && make test-db`
pass with stubs. Then **CHARGES-EXPORT**, **PAYMENTS**, **CASH-DRIVERS**, **WEB** run in parallel. Generated files
belong to CONTRACT, which reruns `make gen` on request. Files not listed belong to the orchestrator and are read-only
for builders: `docs/spec/**`, `docs/adr/**`, `docs/m*-plan.md`, `docs/architecture.md`, `README.md`, `AGENTS.md`,
`CLAUDE.md`, `assets/**`, `.claude/**`, `.github/**`, `deploy/**`, `go.mod`/`go.sum`, `web/package*.json`, and every
M0–M4 file not listed below.

### 14.1 CONTRACT (then frozen for M5 unless the orchestrator reruns it)

| File | Change |
|---|---|
| `api/openapi.yaml`; `internal/http/api/api.gen.go`; `web/shared/api/generated/**` | §7, `make gen` |
| `internal/migrations/{sqlite,postgres}/00010_charges.sql`, `00011_cash.sql`, `00012_payments.sql`; `internal/migrations/m5_integration_test.go` | §6 |
| `internal/store/{billing_models,payment_models,collect_models,cash_models}.go` | Bun structs (then owned per §6.4) |
| `internal/store/billing_invariants.go` (dispatcher) + stub files `billing_invariants_{charges,payments,cash}.go` | §12 (stub files then owned by the builders) |
| `internal/store/locks.go` | `LockPayment`, `LockChargesSorted`, `LockCashSession` (additive) |
| `internal/store/backup_tables.go`, `internal/store/backup_integration_test.go`, `internal/backup/**` | §6.4 |
| `internal/domain/auth/roles_test.go` (only `TestBilling_PermissionsPerRoute` helper table) | §7.2 |
| `internal/domain/event/{event.go,payloads.go}` (+ samples) | §7.5 |
| `internal/http/sse/sse.go`, `sse_test.go` | topic `billing.changed` |
| `internal/domain/meta/meta.go` | §7.6 |
| `internal/http/problem/types.go` + test, `internal/http/respond/errmap.go` + test | §7.4 |
| `internal/http/admin/admin.go`, `internal/http/admin/m5_stubs.go`; `internal/http/public/public.go` (Handler fields), `internal/http/public/m5_stubs.go`; `internal/http/webhooks/webhooks.go` (Handler fields) and `internal/http/webhooks/payments.go` (stub); `internal/http/server.go` (mount, raw body, token redaction) | every new operation answers 501; builders delete their own methods from the stub files; the last deletes the file |
| `internal/app/app.go` | wiring, registries (`PaymentProviders`, `FiscalDrivers`, `ChargeExporters`), hook registration, job registration (§7.6) |
| `internal/app/seed.go` | calls `seedBilling`, `seedPayments`, `seedCash` after M4; `SeedResult` M5 counts |
| `internal/config/config.go` (+ test) | `PORTO_STRIPE_API_BASE` (dev only) |
| `.golangci.yml` | pure list (§0.7) |
| `pkg/ext/{payment.go,fiscal.go,exporter.go}` (new), `pkg/ext/ext.go` (`Extensions` fields) | §5.6 |
| `internal/domain/chargeplan/{doc.go,types.go}`, `internal/domain/charge/{service.go,errors.go,types.go,hooks.go}`, `internal/domain/payment/{service.go,errors.go,types.go,tx.go}`, `internal/domain/collect/{service.go,errors.go,types.go}`, `internal/domain/collect/levels/doc.go`, `internal/domain/paydriver/{registry.go,manual/manual.go}`, `internal/domain/paydriver/stripe/doc.go`, `internal/domain/cash/{service.go,errors.go,types.go}`, `internal/domain/cash/summary/doc.go`, `internal/domain/fiscal/{service.go,errors.go,types.go}`, `internal/xlsx/doc.go` | skeletons (then owned per §14.2) |
| `internal/domain/contract/billing.go` (new), call-site lines in `installments.go`, `confirm.go`, `moveprice.go`, `transits.go`, `termination.go`, `addenda.go`, `vessel_change.go`, `states.go`, `errors.go` | §8.1 (one call line each + the removed M4 email line; nothing else) |
| `internal/domain/rights/billing.go` (new), one call line in the file holding `IssueDueTx` | §8.1 |
| `internal/domain/docs/types.go` (kind + catalogue), `internal/domain/docs/views/views.go` (M5 views, frozen), `internal/domain/docs/service.go` (`RenderEphemeral` signature returning `ErrNotImplemented`) | §8.3 |
| `internal/domain/notify/templates.go` | names of the M5 templates (`payment_request`, `payment_receipt`, `reminder_pre_due`, `reminder_due`, `reminder_courteous`, `reminder_formal`, `reminder_final_notice`, `sepa_mandate_request`, `late_payment_office`); template files are written by the owners |
| `web/shared/src/ui/**` + barrel | shadcn CLI |
| `web/shared/src/money/{MoneyInput.tsx,parse.ts,parse.test.ts}`, `testdata/money/parse.json` | §9.2 |
| `web/shared/src/i18n/resources.ts`, `locales/{it,en}/{common,errors,billingEnums}.json` + empty §9.3 namespaces | §9.3 |
| `web/shared/src/api/{queryKeys,sse,index}.ts` (+ tests), `web/shared/src/api/hooks/{billing,payPublic}.ts` (stubs) | §9.2 |
| `web/admin/src/router.tsx`, `nav.ts`, `nav.test.ts`; `web/admin/src/features/billing/{routes.tsx,public.ts}` (stubs); `web/booking/src/routes.ts` (pay route), `web/booking/src/pay/index.tsx` (stub) | §9.1 |
| `e2e/support/billing.ts` | helper signatures, stubs |
| `internal/acceptance/acceptance_test.go` | route-count threshold only |
| `docs/invariants.md`, `CHANGELOG.md` | §12 entries, M5 entries |

### 14.2 Builders

| Owner | Files |
|---|---|
| **CHARGES-EXPORT** | `internal/domain/chargeplan/**`, `internal/domain/charge/**`, `internal/xlsx/**`; `internal/domain/docs/ephemeral.go` (new: `RenderEphemeral` implementation; may replace only the stub body in `docs/service.go`) and the built-in templates `internal/domain/docs/defaults/{it,en}/charge_export_summary.md` (new files); `internal/csvimport/xlsx.go` (new) + one dispatch line in `csvimport.go`; `internal/store/{charges.go,charge_exports.go,billing_settings.go,charge_imports.go,billing_invariants_charges.go}`, `internal/store/billing_models.go` (after CONTRACT), `internal/store/charges_integration_test.go`; `internal/http/admin/{charges,charge_exports,billing_settings,opening_balances}.go` + tests; `internal/http/view/billing.go`; `internal/app/seed_billing.go` (charge emails belong to CASH-DRIVERS); `internal/acceptance/{m5_year_test.go,m5_sim_test.go}`, `internal/acceptance/testdata/m5/year/**` |
| **PAYMENTS** | `internal/domain/payment/**`; `internal/domain/contract/payments.go` (new) and, in `internal/domain/contract/settle.go`, the bodies of `SettleInstallment`/`SettleInstallments`/`settleTx` (signatures unchanged, `settleTx` gains the `unsettle` sibling in `payments.go`); `internal/domain/rights/payments.go` (new) and, in `internal/domain/rights/holders.go`, only the bodies of `SettleHolderInstallment(s)`; `internal/store/{payments.go,payment_matches.go,refunds.go,receivables.go,work_items.go,mandate_settlement.go,billing_invariants_payments.go}`, `internal/store/payment_models.go` (after CONTRACT), `internal/store/payments_integration_test.go`; `internal/http/admin/{payments,credits,receivables,customer_billing,worklist}.go` + tests, the M5 edit of `transits.go` (confirm request mapping only) and of `mandates.go` (settle route only); `internal/http/view/payments.go`; `internal/domain/docs/defaults/{it,en}/statement_of_account.md` (new); `internal/app/seed_payments.go`; `internal/acceptance/m5_ledger_test.go`, `internal/acceptance/testdata/m5/ledger/**` |
| **CASH-DRIVERS** | `internal/domain/collect/**`, `internal/domain/paydriver/**`, `internal/domain/cash/**`, `internal/domain/fiscal/**`; `internal/domain/notify/templates/{it,en}/{payment_request,payment_receipt,reminder_*,sepa_mandate_request,late_payment_office}.*.tmpl` (new files); `internal/domain/docs/defaults/{it,en}/receipt.md` (new); `internal/store/{cash.go,fiscal.go,payment_intents.go,payment_providers.go,payment_requests.go,reminders.go,sepa_mandates.go,billing_invariants_cash.go}`, `internal/store/{collect_models,cash_models}.go` (after CONTRACT), `internal/store/collect_integration_test.go`; `internal/http/admin/{cash,fiscal,payment_requests,reminders,payment_providers,sepa}.go` + tests; `internal/http/webhooks/payments.go` (after CONTRACT) + test; `internal/http/public/pay.go` + test; `internal/http/view/collect.go`; `testdata/stripe/**`; `internal/app/seed_cash.go`; `internal/acceptance/{m5_cash_test.go,m5_online_test.go,m5_offline_test.go}`, `internal/acceptance/testdata/m5/{cash,online}/**`, `internal/testutil/fixture/fakeprovider.go` (new: fake `ext.PaymentProvider` + signer, also used by CHARGES-EXPORT and PAYMENTS acceptance read-only) |
| **WEB** | `web/admin/src/features/billing/**` (all M5 admin pages); M5 edits inside `web/admin/src/features/registry/customers/**` (dashboard and statement tabs), `web/admin/src/features/desk/transits/**` (collect, payment link), `web/admin/src/features/contracts-m4/**` (Rate tab charge columns, SEPA), `web/admin/src/features/rights/**` (mandate settle), `web/admin/src/features/operations/**` only for the Pagamenti lens wiring (lens function `web/shared/src/map/lenses/payments.ts` + test, new; `lenses/index.ts` one registry line); `web/shared/src/api/hooks/{billing,payPublic}.ts`; `web/booking/src/pay/**` (after CONTRACT stub); `web/shared/src/i18n/locales/{it,en}/{charges,payments,cash,fiscal,receivables,reminders,exports,billingSettings,paymentProviders,customerBilling,worklist,payPublic}.json`; `e2e/tests/{charges,payments-matching,cash-session,receivables,reminders,exports,customer-dashboard,payment-settings,pay-link,payments-lens,billing-m5-acceptance}.spec.ts`, `e2e/support/billing.ts` (after CONTRACT), `e2e/fixtures/m5/**` |

### 14.3 Rules for the parallel phase

- **Interfaces first.** Builders code against the signatures CONTRACT wrote: `payment.RecordTx`, `MatchTx`,
  `ReverseMatchTx`, `RefundTx`, `GrantCreditTx`; `charge.PaymentRequester.RequestForChargeTx`;
  `charge.Service.{CreateTx, SyncOriginTx, ResidualTx}`; `contract.Service.{ApplyChargeSettlementTx,
  ConfirmTransitPaidTx, RequestTransitPaymentTx, RevertTransitPaymentTx, SuspendForArrearsTx}`;
  `rights.Service.ApplyChargeSettlementTx`; `cash.Service.SessionForPaymentTx`; `fiscal.Service.RecordTx`;
  `docs.Service.RenderEphemeral`. Until the owner lands, unit tests use fakes in the caller's own package
  (`charge/fakes_test.go`, `payment/fakes_test.go`, `collect/fakes_test.go`, `cash/fakes_test.go`); integration
  tests and acceptance run once all three backend builders are in. A signature change goes through the orchestrator
  → CONTRACT.
- **Who writes money columns**: `charges.paid_amount_cents` / `payment_status` (except cancel) and
  `payments.allocated_cents/refunded_cents` only in PAYMENTS' store files; `charges.credited_cents` only through
  `store.ApplyOffset` in `charges.go` (CHARGES-EXPORT), called by `charge` in the same tx as the negative charge;
  intents/request/reminder statuses only in CASH-DRIVERS' store files.
- **Contract and rights packages**: CONTRACT writes the hook files and call sites; PAYMENTS owns `contract/payments.go`,
  `rights/payments.go` and the settle bodies listed; nobody else edits `internal/domain/contract/**` or
  `internal/domain/rights/**`. If a builder needs another entry point there, it asks the orchestrator.
- **Docs**: only the new files listed (ephemeral render, three built-in template files); views are frozen
  (CONTRACT). The data sources for the three kinds live in the owners' packages (`charge/exportdoc.go`,
  `payment/statementdoc.go`, `cash/receiptdoc.go`).
- **Notify templates**: only new files with the names CONTRACT registered; M4 templates untouched
  (`installment_payment_request` stays for backward compatibility but is no longer queued).
- **Admin handlers**: split exactly as §7.3; all in `package admin`. Public handlers: CASH-DRIVERS owns `pay.go`.
- **Store**: one file per area as listed; M1–M4 store files are not edited except `locks.go` (CONTRACT) and the
  index-only migration.
- **Acceptance fixture**: `internal/testutil/fixture/fakeprovider.go` (CASH-DRIVERS, new file in the existing package) and `m5_sim_test.go` (CHARGES-EXPORT) are shared
  read-only; PAYMENTS' ledger acceptance imports the generator and must not modify it (requests through the
  orchestrator).
- **Web**: WEB is a single builder and owns the whole M5 UI; it imports map pieces only through
  `@porto/shared/map` and `features/operations/public.ts`. i18n: only the namespaces listed; `billingEnums`,
  `common`, `errors` through the orchestrator.
- **go.mod / package.json**: no new dependency in M5 (own XLSX, plain-HTTP Stripe).

---

## 15. Spec and repository notes for the orchestrator

- `docs/architecture.md` §10 "successive" lists `charge`, `payment`, `cash`, `fiscal`: replace with the M5 packages
  of §2 (`chargeplan`, `charge`, `payment`, `collect`, `paydriver`, `cash`, `fiscal`, `xlsx`).
- Spec 06.7 lists a `customer_credits` table: M5 derives credit from unallocated payments (§16 D5), as Arenile M5;
  recorded in ADR 0009 (proposed).
- Spec 27.1 webhook path `/webhooks/{payments,signature}/{provider}/{marina_id}` is followed literally.
- `docs/invariants.md` INV-50 wording ("Σ abbinamenti dei pagamenti riusciti − Σ rimborsi") is restated in §4.1
  (refunds come only from unallocated money); the orchestrator may want the Italian text updated accordingly.
- An ADR "Scelte M5" could record D1–D20 below once accepted (licence of nothing new; offline-first payments).

---

## 16. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | **Charge lines of instalments** come from the contract's snapshot target split by `chargeplan` with cumulative VAT rounding, so the exported VAT per rate of a contract equals its snapshot (32.1) without any per-instalment re-pricing; a final true-up charge absorbs reprices made after the last instalment. |
| D2 | **Charge dates**: `issued_on` = creation date (instalment issue date, transit confirmation date); the export period selects on `issued_on`. Transit charges are created at confirmation (also for pay at departure, with due on departure), not at departure, so the export of a period contains every confirmed stay once. |
| D3 | **Corrections**: unexported unpaid charges are edited in place; everything else is corrected by a linked adjustment charge (delta) or a reversal + replacement (replace); negative charges are applied at creation as offsets to the corrected charge and the rest as customer credit. No "credit note" concept exists in Porto. |
| D4 | **No zero charges**; negative charges only as adjustments, termination refunds, negative common-charge balances. |
| D5 | **Credit = unallocated payments** (as Arenile M5): non-cash credits (adjustment remainders, goodwill, release, mandate, opening balances) are `payments.kind = credit` rows, never in cash or corrispettivi. No separate `customer_credits` table. |
| D6 | **Refunds only from unallocated money**: refunding a paid charge is "reverse match + refund" in one tx; in-flight provider refunds count against the limit. |
| D7 | **Instalment settlement in M5 always creates a payment** (`settlement_source = payment`); historical M4 manual settlements are backfilled into charges + payments (`source = backfill`) and keep `settlement_source = manual`. |
| D8 | **Transit payment**: desk confirmation may carry a payment; online confirmation only through the provider webhook after `pending_payment`; the link's checkout session is created on click from the public pay page so emailed links survive session expiry. |
| D9 | **Receipt-mode charges** (commercial document issued) are exported like the others with the document number, so INV-53 holds and the accountant sees them, but they are not invoiced; corrispettivi include only receipt-mode payments. |
| D10 | **Fiscal drivers**: `none` (default) and `export` in the core; device drivers and the desktop bridge are extensions/M6; the receipt printout is browser HTML (80/58 mm) or PDF (A4), always "Non valido ai fini fiscali". |
| D11 | **Stripe without SDK**: plain HTTPS over the egress client (no new dependency, all outbound traffic under the anti-SSRF policy), pinned API version, own signature verification on the injected clock. Arenile's SDK choice is not reused. |
| D12 | **Payment-request tokens** live in `payment_requests.token_hash` (not `access_links`, whose purpose CHECK is frozen), with the same rules as magic links (hash only, 404 for unknown/expired/revoked, rate limited, no-referrer, no-store). |
| D13 | **Reminder levels** default: pre-due 7 days, due, courteous +7, formal +21, final notice +45 (approval required); one email per customer, level and day; each (charge, level) at most once; reminders run once per marina day after 09:00 marina time. |
| D14 | **Late/excess online payments** are never auto-refunded: they become customer credit + worklist item + office email; staff refund with one click (provider refund). Offline-friendly and avoids refunds the office did not see. |
| D15 | **Arrears policy** disabled by default; when enabled it suspends mooring/charter contracts (never transits) with reason `arrears`; resume is manual. The dock sees the lens/warning; the exit is never blocked. |
| D16 | **One open intent per charge** via a denormalised `open_flag` on `payment_intent_charges` with a partial unique index (portable on both dialects), so SEPA collection and payment links cannot charge the same charge twice concurrently; excess money still lands as credit if it happens through two different requests. |
| D17 | **Charge and payment numbers** (`A2027-000123`, `P2027-000045`, exports `E2027-0012`) are internal references for matching and reconciliation, not fiscal numbers; new `billing_sequences` table because `number_sequences.series` CHECK is frozen. |
| D18 | **Excel** is produced and read by an own minimal `internal/xlsx` (stdlib only, first sheet on read, inline strings, decimal numbers as text-free numeric cells built from integer cents); Excel input is added to all imports (11.6). |
| D19 | **Opening balances** are born `invoiced` (they were invoiced by the previous software) and are therefore excluded from the "da fatturare" export (INV-53 precision). |
| D20 | **Ephemeral PDFs**: receipts, statements and export summaries are rendered on demand with the M4 renderer and not archived in `generated_documents`; the export summary is stored with the export. |
| D21 | **Mandate compensation** is a staff action per mandate and period: owner shares (plus guaranteed-minimum top-up) become credit (offset against fees by applying credit) or a recorded payout; no automatic bank transfer. |
| D22 | **Release credits** are staff grants referencing a resold release (no automatic policy in M5). |
| D23 | **Counter sales without a customer** must carry a commercial document (receipt mode); an invoiceable charge always has a customer. A walk-in customer row is never created. |
| D24 | **Map payment lens** returns enum states to every `map.view` user (the dock needs the warning) and amounts only with `charges.manage`. |
| D25 | **Worklist** is computed from sources plus a small `work_item_states` table (assignee, status); no separate item generator job. |
| D26 | **Origin items** for charges without a price snapshot (fees, common charges, transfer fee, termination, deposit retention, opening balance) come from `charge_origin_items`; a missing item blocks only that charge (worklist), never produces a line without VAT data. |
| D27 | **Export size** ≤ 20 000 charges per export; files built after commit from the stored items and lazily rebuilt if missing; same items ⇒ same bytes (sha256 stored). The CSV and the workbook are not stored (`stored_files` accepts PDF and images only): what their rows show beyond the immutable charge (due date, still editable under INV-51, payment status, paid amount, vessel, contract, instalment, corrected charge, commercial document) is frozen, encrypted, on each `charge_export_items.row_snapshot_enc` when the export is created, so every download is the bytes of the first build. A re-export freezes the data of its own time. |
| D28 | **No new permissions**: the M0 constants `charges.manage`, `charges.export`, `payments.collect`, `payments.collect_cash`, `refunds.issue`, `providers.configure` cover 26.1 exactly. |
