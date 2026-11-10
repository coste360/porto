---
title: Database
description: SQLite o PostgreSQL, migrazioni e passaggio da un database all'altro.
sidebar:
  order: 6
---

Porto usa lo stesso schema su due database, con migrazioni numerate allo stesso modo.

## SQLite (predefinito)

Senza `DATABASE_URL` Porto usa il file `porto.db` nella cartella dati (`PORTO_DATA_DIR`). Non serve installare
nulla: il driver è incluso nel binario. È adatto alla maggior parte dei porti ed è l'unico database dell'app
desktop.

## PostgreSQL (facoltativo)

È consigliato oltre i 300 posti, con più marine nella stessa installazione o se la tua infrastruttura usa già
PostgreSQL. Imposta `DATABASE_URL`:

```bash
DATABASE_URL=postgres://porto:PASSWORD@db.interno:5432/porto?sslmode=require
```

I caratteri speciali della password vanno codificati (`@` diventa `%40`). Con Docker usa
`deploy/docker-compose.postgres.yml` (PostgreSQL 17), vedi [Docker](/installazione/docker/#con-postgresql).

## Migrazioni

- Con `PORTO_AUTO_MIGRATE=true` (predefinito) `porto serve` applica le migrazioni all'avvio.
- Con `PORTO_AUTO_MIGRATE=false` il server non parte finché ci sono migrazioni in sospeso: applicale con
  `porto migrate up`. `porto migrate status --check` esce con codice 1 se ce ne sono (utile negli script).
- Le migrazioni vanno solo avanti: non si torna a una versione precedente di Porto sullo stesso database. Per
  tornare indietro si ripristina il backup fatto prima dell'aggiornamento.

## Da SQLite a PostgreSQL (e viceversa)

Il formato di backup non dipende dal database:

1. sulla vecchia installazione: `porto backup --out porto.porto-backup`;
2. avvia la nuova installazione **con la stessa versione di Porto**, la stessa `PORTO_SECRET_KEY` e il nuovo
   `DATABASE_URL`;
3. `porto restore --in porto.porto-backup` (su un database vuoto) oppure con `--force` per svuotarlo prima.

Vedi [Backup e ripristino](/installazione/backup-e-ripristino/).
