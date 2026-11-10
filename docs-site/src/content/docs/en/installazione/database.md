---
title: Database
description: SQLite or PostgreSQL, migrations and moving from one database to the other.
sidebar:
  order: 6
---

Porto uses the same schema on two databases, with migrations numbered the same way.

## SQLite (default)

Without `DATABASE_URL` Porto uses the `porto.db` file in the data folder (`PORTO_DATA_DIR`). Nothing to install: the
driver is in the binary. It suits most marinas and is the only database of the desktop app.

## PostgreSQL (optional)

Recommended above 300 berths, with several marinas in the same installation, or when your infrastructure already
runs PostgreSQL. Set `DATABASE_URL`:

```bash
DATABASE_URL=postgres://porto:PASSWORD@db.internal:5432/porto?sslmode=require
```

Special characters of the password must be URL-encoded (`@` becomes `%40`). With Docker use
`deploy/docker-compose.postgres.yml` (PostgreSQL 17), see [Docker](/en/installazione/docker/#with-postgresql).

## Migrations

- With `PORTO_AUTO_MIGRATE=true` (default) `porto serve` applies migrations on start.
- With `PORTO_AUTO_MIGRATE=false` the server does not start while migrations are pending: apply them with
  `porto migrate up`. `porto migrate status --check` exits with 1 when some are pending (useful in scripts).
- Migrations only move forward: an older Porto version cannot run on the same database. To go back, restore the
  backup taken before the update.

## From SQLite to PostgreSQL (and back)

The backup format does not depend on the database:

1. on the old installation: `porto backup --out porto.porto-backup`;
2. start the new installation with **the same Porto version**, the same `PORTO_SECRET_KEY` and the new
   `DATABASE_URL`;
3. `porto restore --in porto.porto-backup` (on an empty database) or with `--force` to wipe it first.

See [Backup and restore](/en/installazione/backup-e-ripristino/).
