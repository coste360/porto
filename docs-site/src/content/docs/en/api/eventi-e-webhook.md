---
title: Events and webhooks
description: Outgoing webhooks signed with HMAC-SHA256, the event catalogue and live updates (SSE).
sidebar:
  order: 4
---

Every significant state change (a confirmed transit, a paid charge, a signed contract…) produces a **domain event**,
written in the same transaction as the change: if the change is there, the event is there too. Events feed the
webhooks, the live updates of the panel and the extensions.

Events carry **only identifiers and enumerated values**, never personal data: for details the receiver reads the
resource through the API.

## Webhooks

Endpoints are configured in the panel (*Webhook*, up to 20 per marina): `https` URL, subscribed events (or all),
active flag. The **secret** (`whsec_…`) is shown once at creation.

Each delivery is a JSON `POST`:

```json
{
  "id": "evt_…",
  "type": "transit.confirmed",
  "version": 1,
  "marina_id": "…",
  "occurred_at": "2026-07-14T09:12:03Z",
  "data": { "contract_id": "…" }
}
```

with the headers:

| Header | Value |
|---|---|
| `Porto-Signature` | `t=<unix>,v1=<hex>`: HMAC-SHA256 with the secret of `"<t>.<body>"` |
| `Porto-Event` | the event type |
| `Porto-Delivery` | the delivery id (the same on later attempts) |

### Verifying the signature

1. Read `t` and the `v1` values from the header (during a secret rotation there are two: one match is enough).
2. Compute `HMAC-SHA256(secret, t + "." + body)` over the raw body bytes and compare in constant time.
3. Reject messages whose `t` is too far from the current time (for example more than 5 minutes).

```js
import { createHmac, timingSafeEqual } from 'node:crypto';

function verify(secret, header, rawBody) {
  const parts = Object.groupBy(header.split(','), (p) => p.split('=')[0]);
  const t = parts.t?.[0]?.slice(2);
  if (!t || Math.abs(Date.now() / 1000 - Number(t)) > 300) return false;
  const expected = createHmac('sha256', secret).update(`${t}.`).update(rawBody).digest();
  return (parts.v1 ?? []).some((p) => {
    const got = Buffer.from(p.slice(3), 'hex');
    return got.length === expected.length && timingSafeEqual(got, expected);
  });
}
```

### Deliveries and retries

- A `2xx` answer within 10 seconds confirms the delivery; otherwise Porto retries with growing delays for 24 hours.
- Delivery is *at least once*: use `id` or `Porto-Delivery` to drop duplicates.
- After 20 consecutive failures the endpoint is disabled; re-enable it from the panel.
- The panel shows the delivery log (kept 30 days), lets you resend deliveries, send a test event (`webhook.test`) and
  rotate the secret (the previous one stays valid for 24 hours).
- Private, loopback or link-local URLs are refused, also after DNS resolution and redirects.

### Event catalogue

The full list, with versions, is returned by `GET /api/admin/v1/event-types`. Among others:

| Area | Events |
|---|---|
| Occupancy and presence | `occupancy.allocated`, `occupancy.moved`, `occupancy.released`, `occupancy.hold_expired`, `release.created`, `release.resold`, `presence.arrived`, `presence.departed` |
| Quotes, contracts, transits | `quote.sent`, `quote.accepted`, `quote.expired`, `contract.created`, `contract.activated`, `contract.signed`, `contract.renewed`, `contract.terminated`, `transit.held`, `transit.confirmed`, `transit.cancelled`, `transit.arrived`, `transit.departed`, `transit.approval_requested`, `installment.overdue` |
| Renewals | `renewal.proposed`, `renewal.accepted`, `renewal.declined`, `renewal.expired`, `renewal.completed` |
| Berth rights | `right.created`, `right.transferred`, `right.ended`, `mandate.created`, `mandate.ended`, `mandate.revenue_accrued`, `management_fee.issued`, `common_charges.issued` |
| Charges and payments | `charge.created`, `charge.paid`, `charge.cancelled`, `charge.exported`, `charge.invoiced`, `installment.charged`, `payment.succeeded`, `payment.failed`, `payment.refunded` |
| Boatyard and maintenance | `yard_quote.accepted`, `work_order.status_changed`, `equipment.booking_changed`, `maintenance.due` |
| Rentals | `rental.confirmed`, `rental.cancelled`, `rental.checked_out`, `rental.returned` |
| Sensors | `sensor.state_changed`, `sensor.anomaly` |
| Registry and configuration | `customer.created`, `customer.anonymized`, `config.map_changed`, `config.pricing_changed`, team, API key and device events |

## Live updates (SSE)

`GET /api/admin/v1/stream` is a Server-Sent Events stream for the chosen marina. Messages are light (topic, type,
resource ids and affected nights) and carry no personal data: they tell **what to read again**, not the data itself.
After a disconnection the client resumes from `Last-Event-ID`. The same mechanism keeps office, dock and online
bookings in step.
