# Demo data

`porto seed demo` creates the organization "Porto Demo" with the marina "Marina Demo" (slug `demo`), one user per
role (`<role>@demo.porto.test`, same password for all) and a full year of operations, everything written through
the domain services (`internal/app/seed*.go`). Every date is relative to the marina's today (Europe/Rome), the
random choices use fixed seeds, and every part is idempotent: a second run adds nothing.

```sh
PORTO_ADMIN_PASSWORD='choose-a-demo-password' porto seed demo     # or --password-stdin
make seed                                                           # local dev database (tmp/data)
```

Without a password one is generated and shown once. The run takes about one minute on SQLite (laptop, 2026); the
output ends with the counts of yard, rental, online booking and sensor data and the seed time.

## What the demo shows

| Area | Content |
|------|---------|
| Harbour, registry | 4 maps, 350 berths, 250 customers, 260 boats with documents |
| Contracts | ~150 annual contracts, ~140 transits around today, renewal campaign for next year, rights, common charges |
| Previous year | annual contracts, 240 transits and payments of last year (dashboard comparisons) |
| Renewal rate | 12 seasonal contracts last year, 10 renewed for this season, 6 of those renewed for next year (2 proposals lapsed); the contract expiry job is replayed on 2 October of both years. The dashboard "Tasso di rinnovo" has a value this year and last year once the season is over (from October) |
| Billing | charges, exports, payments, cash sessions, reminders. The year's contracts are signed in January (seasonal ones in March, transits days before arrival) on four payment clauses (quarterly, monthly direct debit, four and two instalments): charges and payments are spread over the months as last year's, so "Mese in corso" and "Da inizio anno" compare with last year at a believable ratio. Opening balances date from the start of the history |
| Online booking | page `/b/demo`; 7 online bookings: waiting for approval, approved and waiting for the bank transfer, confirmed by bank transfer (manual driver), rejected |
| Storage | shed and yard storage contracts placed by the optimizer |
| Yard | 10 quotes (2 draft, 2 sent, 5 accepted, 1 declined); 8 work orders (planned, in progress, on hold, completed, delivered, invoiced with final account) on the boats on the hard, with tasks, approved and pending hours, materials, external costs |
| Planner | assignments from four days ago to the next two weeks across the employees and the bays (yard, shed); the yard and yard manager users are linked to employees (phone screen) |
| Lifting | travel lift and crane: haul-outs done in the past days, bookings today, tomorrow and during the week, one request to plan |
| Rental | 10 units, 3 skippers; ~30 bookings: closed rentals of the last three days, today (fixed bands of the day, not the seed time) two out at sea (one late on purpose, the other back at closing time), two back, pickups from 17:00, one option; the week ahead with skippers and extras; a cancellation, a no-show, an expired option |
| Sensors | 43 devices on the pier and a linear area, 6 hours of readings, one open alert of every kind |

The demo sensor readings stop at the seed (the demo source is in memory), so the seed sets the sensor thresholds
`offline_after_minutes` and `silent_alert_minutes` to one day: the pier stays "live" until the next nightly reset.
Customer emails of the online bookings use the reserved `.test` domain, so no mail can reach anyone.

## Public demo instance: nightly reset

A public demo is recreated every night with

```sh
PORTO_DEMO_INSTANCE=true PORTO_ADMIN_PASSWORD='…' porto seed demo --reset
```

`--reset` wipes the database and the stored files, then seeds the demo again. It is guarded twice:

1. it runs only with `PORTO_DEMO_INSTANCE=true` in the environment (otherwise it exits with a usage error and
   touches nothing);
2. it refuses (`the database is not a demo database`) unless the database holds only the demo organization.
   `porto seed demo` records the id of the organization it creates in `PORTO_DATA_DIR/demo-organization`; the
   reset accepts any organization name, any number of marinas and any users inside that organization, so what
   visitors do as the demo owner (renaming the organization or the marina, adding marinas, adding or removing
   team members) does not block it, while any other organization does. A demo seeded before the marker existed
   is checked by name instead (one organization named "Porto Demo", no marina other than `demo`, every user a
   member of it) until its first reset writes the marker. An empty database passes.

The wipe rolls every migration back and applies them again (the path of `porto restore --force`), so it works the
same on SQLite and PostgreSQL and needs no file or database privileges beyond the application's own. The
`files/` directory under `PORTO_DATA_DIR` and the mail directory (`PORTO_MAIL_DIR`, when set) are emptied. Sessions are wiped too: visitors log in again.

Do not set `PORTO_BOOTSTRAP_OWNER_*` on a demo instance (it creates a second organization and the reset refuses).
Never set `PORTO_DEMO_INSTANCE` on a production instance.

Stop the server during the reset: the schema is dropped under it otherwise, and in-flight requests fail. The
recipe in `deploy/demo/` does that: `deploy/demo/reset.sh` stops the service, runs the reset in a one-off
container of the same image and volume, starts the service again and waits for `/readyz`; schedule it from the
host's cron or a systemd timer (the image has no shell, so in-container schedulers cannot run it):

```cron
0 4 * * * cd /srv/porto && PULL=1 deploy/demo/reset.sh >> /var/log/porto-demo-reset.log 2>&1
```

Without that recipe, the equivalent is `docker compose stop porto`, then
`docker compose run --rm --no-deps -e PORTO_DEMO_INSTANCE=true porto seed demo --reset`, then
`docker compose up -d porto`. `PORTO_ADMIN_PASSWORD` comes from the service's environment.

The reset is not atomic. When the guard refuses, nothing is touched. When a later step fails (the wipe or the
seed), the database may already be empty or hold a partial demo: `reset.sh` starts the server anyway and exits
with an error, and the next run (or a manual one) loads the demo again, since a partial demo still carries the
marker.
