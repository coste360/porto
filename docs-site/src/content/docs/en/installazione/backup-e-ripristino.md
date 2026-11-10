---
title: Backup and restore
description: Portable backups with porto backup, restore with porto restore, automatic backups of the desktop app.
sidebar:
  order: 8
---

A Porto backup is a **portable archive** (compressed tar with one JSONL file per table and the uploaded files, still
encrypted): the same format for SQLite and PostgreSQL, so a backup taken on one database restores on the other. Raw
sensor readings are not included.

## Taking a backup

```bash
porto backup --out /data/porto-2026-11-10.porto-backup
```

With Docker:

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/porto-$(date +%F).porto-backup
```

The file lands in the data volume: copy it off the server (another disk, another site). Schedule the backup from the
host's `cron` or a systemd timer.

:::caution[The backup does not contain the key]
Documents, IBANs, provider keys and files are encrypted with `PORTO_SECRET_KEY`. Keep the key with the backups, in a
safe place: a backup without its key cannot be read.
:::

## Restoring

1. Prepare an installation with **the same Porto version** as the backup and the **same `PORTO_SECRET_KEY`**.
2. Restore:

   ```bash
   porto restore --in porto-2026-11-10.porto-backup          # on an empty database
   porto restore --in porto-2026-11-10.porto-backup --force  # wipe the database first
   ```

3. Start the server and sign in again: sessions are not included.

Restore refuses a database that already holds data (without `--force`) and an archive whose schema version differs
from the database. To bring a backup to a newer version, restore it with its own version, then update
([Updates](/en/installazione/aggiornamenti/)).

## Desktop app

The desktop app takes backups in the same format by itself: one a day, one at every close, manual ones and one before
every restore. It keeps 14 daily and 5 close backups; manual ones are never deleted. Restore starts from the panel. See
[Desktop app](/en/gestore/app-desktop/#backups).
