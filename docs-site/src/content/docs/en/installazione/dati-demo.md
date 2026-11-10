---
title: Demo data
description: The demo marina of porto seed demo and the public demo instance with its nightly reset.
sidebar:
  order: 10
---

`porto seed demo` creates the organization "Porto Demo" with the marina "Marina Demo" (slug `demo`), one user per role
(`<role>@demo.porto.test`, the same password for all) and a full year of operations, written through the same
services the panel uses. Dates are relative to the marina's today and every part is idempotent: a second run adds
nothing.

```bash
PORTO_ADMIN_PASSWORD='choose-a-demo-password' porto seed demo     # or --password-stdin
make seed                                                          # development database in tmp/data
docker compose exec porto /porto seed demo                         # with the local compose stack
```

Without a password one is generated and shown once. Loading takes about one minute on SQLite.

## What it contains

| Area | Content |
|---|---|
| Harbour and registry | 4 maps, 350 berths, 250 customers, 260 boats with documents |
| Contracts | about 150 annual contracts, about 140 transits around today, renewal campaign for next year, berth rights, common charges |
| Previous year | last year's contracts, transits and payments, for the dashboard comparisons |
| Billing | charges, exports, payments, cash sessions, reminders |
| Online booking | page `/b/demo` with 7 bookings in different states (awaiting approval, approved waiting for the bank transfer, confirmed, rejected) |
| Dry storage | shed and yard storage contracts placed by the optimizer |
| Boatyard | 10 quotes, 8 work orders in every state, employee planner, haul-outs and launches with travel lift and crane |
| Rentals | 10 units, 3 skippers, about 30 bookings (out at sea, returns, options, cancellations) |
| Sensors | 43 sensors on a pier and a linear area, 6 hours of readings, one open alert of each kind |

Demo customers use the reserved `.test` domain: no message can reach anyone.

## Public demo with a nightly reset

For a public demo reloaded every night there is a recipe in
[`deploy/demo`](https://github.com/coste360/porto/tree/main/deploy/demo): `reset.sh` stops the service, runs
`porto seed demo --reset` with `PORTO_DEMO_INSTANCE=true`, starts it again and waits for `/readyz`.

`--reset` wipes database, uploaded files and mail and loads the demo again. It runs only with
`PORTO_DEMO_INSTANCE=true` and refuses a database holding anything but the demo organization.

:::danger
Never set `PORTO_DEMO_INSTANCE` on a real instance and never use the demo recipe for a real marina: the demo password
is public and every change is lost at the reset.
:::
