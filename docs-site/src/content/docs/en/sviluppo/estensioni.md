---
title: Extensions
description: The extension points of pkg/ext - payments, invoicing, signature, channels, sensors, notifications, events.
sidebar:
  order: 4
---

What varies between marinas and depends on external vendors sits behind public Go interfaces in the
[`pkg/ext`](https://github.com/coste360/porto/tree/main/pkg/ext) package. The package imports nothing from
`internal/`: an extension is written against these interfaces and registered in the binary, by passing the
implementations in `ext.Extensions` where `cmd/porto` builds the application.

| Interface | For | In the core |
|---|---|---|
| `PaymentProvider` | payment gateways and methods | `manual` (always on) and `stripe` drivers |
| `FiscalDriver` | fiscal cash registers and commercial documents | `none`, `export` and the desktop app bridge |
| `ChargeExporter` | sending charges to invoicing software and receiving invoice number and date back | none |
| `SignatureProvider` | advanced or qualified e-signature | none (paper, OTP and on-screen signature are in the core) |
| `BookingChannel` | berth booking portals | none |
| `SensorSource` | polled reading sources (vendor API, MQTT broker) | none (sensors can also push directly, [protocol](/en/api/ingestione-sensori/)) |
| `Notifier` | extra notification channels (SMS, WhatsApp) | none (email is in the core) |
| `EventSink` | receiving every outbox event, at least once | webhooks and SSE |
| `TenantResolver` | custom domains for the public pages | — |
| `AuthProvider`, `BotProtection` | external identity providers, protection of public forms | — |
| `HTTPRoutes` | extra routes on the router | — |

## Rules for extensions

- **No personal data** in logs, in metadata sent to providers and in events: only ids and enumerated values.
- **Outgoing HTTP** only through the client Porto gives the driver: `https`, no private or metadata addresses,
  redirects rechecked.
- **Money** in integer cents with an explicit ISO 4217 currency; no decimal numbers.
- Provider calls happen **outside database transactions**; the outcome usually arrives through a signed webhook.
- **Events may arrive more than once**: `EventSink.Handle` must be idempotent.
- A booking channel cannot create overlaps: incoming bookings go through the same transit service and occupy a
  concrete berth.

## Before writing a driver

Open a *new driver* issue on the repository to agree on the interface and on whether the driver belongs in the core or
in a separate extension. See [Contributing](/en/sviluppo/contribuire/) and, for payments,
[Payment drivers](/en/sviluppo/driver-di-pagamento/).
