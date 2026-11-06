---
name: new-payment-driver
description: Add a payment provider driver behind pkg/ext.PaymentProvider (online gateway or manual method) with signed webhooks, idempotent confirmation and matching to charges. Use from M4/M5 when a new payment method or gateway is integrated.
argument-hint: "[provider key]"
---
# New payment driver: $ARGUMENTS

Procedure for the payment milestones; `internal/domain/payment` does not exist in M0.

Read first: `docs/spec/16-addebiti-e-incassi.md` (16.2 payments and refunds), `docs/spec/23-prenotazione-online.md`
(23.3 flow), `docs/spec/27-api-eventi-estensioni.md` (27.1 `/webhooks/payments/{provider}/{marina_id}`, 27.5
`pkg/ext`), `docs/invariants.md` INV-08, INV-09, INV-50, INV-52, `.claude/rules/charges.md`, `security.md`.

1. **Interface.** Implement `ext.PaymentProvider` in its own package (core drivers under
   `internal/domain/payment/<key>`, third-party ones as extensions); register it in `internal/app`.
2. **Configuration** per marina, secrets encrypted with the KeyRing; never logged or returned by the API after
   creation.
3. **Outbound calls** only through the egress client; amounts in `money.Cents` with an explicit currency.
4. **Webhook.** Verify the provider signature on the raw body before parsing; reject old timestamps; dedupe by
   provider event id (idempotent). Only this webhook (or the manual driver) may confirm a transit (INV-09).
5. **Confirmation** runs in one transaction: payment row, matches to charges (never above payment or residual,
   INV-52), `paid_amount` update (INV-50), contract/transit state through `internal/domain/contract`, audit and
   `payment.succeeded` / `payment.failed` / `payment.refunded` outbox events (no card or personal data).
6. **Refunds** go through the provider or are recorded as manual.
7. **Tests.** Fake provider server via `httptest`; signature valid/invalid/replayed; duplicate webhook delivers
   once; `TestConcurrency_PaymentMatching` still passes; browser return without webhook does not confirm
   (`TestCheckout_BrowserReturnDoesNotConfirm`).
8. Licence check for any SDK (`make licenses`); prefer plain HTTP over an SDK. `make check-full`, then the
   `invariants-guardian` subagent.
