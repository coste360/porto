---
title: Payment drivers
description: The manual driver, the Stripe driver and how to add a gateway with PaymentProvider.
sidebar:
  order: 5
---

Porto works **without any online payment**: the `manual` driver (bank transfer, cash, card terminal, cheque) is always
on. Online gateways are optional and enabled per marina.

## Included drivers

- **`manual`**: payments recorded by the office, the cash desk or the dock; payment requests with the bank details. An
  online transit waiting for a bank transfer is confirmed by recording the payment.
- **`stripe`**: Stripe-hosted payment page (card, wallet, SEPA direct debit), SEPA mandates for instalments, charges on
  mandates, refunds, status checks and authorisation with later capture (for online bookings that need approval). It
  uses **the marina's own** keys: no commission goes to the platform.

## Setting up Stripe

In the panel, *Pagamenti online* (Online payments): secret key, publishable key, webhook secret, methods and SEPA
mandates. Keys are stored encrypted and never shown again. The panel shows the **webhook URL** to set in Stripe, in the
form `https://<host>/webhooks/payments/stripe/<marina id>`, and lets you test the connection.

## Rules

- **An online payment is confirmed only by the provider's signed webhook**, never by the browser returning to
  Porto's page. The webhook is idempotent: received twice, it records the payment once.
- A payment arriving after the berth hold expired recovers the same berth if it is still free, otherwise it is
  refunded in full.
- Metadata sent to the provider carry only the marina and payment intent ids; the description carries charge
  numbers, never personal data.

## A new gateway

A gateway implements `ext.PaymentProvider` (`pkg/ext/payment.go`): hosted payment page, optional direct debit
mandates, refunds, status and `ParseWebhook`, which verifies the provider signature with the marina's secret and
returns a payment event, or `ErrIgnoredEvent` for types Porto does not use. Authorisation with capture is optional
(`PaymentAuthorizer`). Credentials arrive per call in the context; a driver never stores or logs them. Open a *new
driver* issue before starting.
