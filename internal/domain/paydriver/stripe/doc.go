// Package stripe is the optional Stripe payment driver (m5-plan §5.7): Checkout (card, wallets, SEPA Direct
// Debit), setup mode for SEPA mandates, off-session SEPA collection, refunds, status checks and signed webhooks,
// over plain HTTPS through the egress client (no SDK, §16 D11). It is compiled in but disabled by default: a
// marina uses it only with an enabled payment_provider_settings row. Owned by CASH-DRIVERS.
package stripe

// Code is the driver code.
const Code = "stripe"
