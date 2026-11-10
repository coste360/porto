---
title: Backup e ripristino
description: Backup portabili con porto backup, ripristino con porto restore, backup automatici dell'app desktop.
sidebar:
  order: 8
---

Il backup di Porto è un **archivio portabile** (tar compresso con un file JSONL per tabella e i file caricati, ancora
cifrati): lo stesso formato per SQLite e PostgreSQL, quindi un backup fatto su un database si ripristina sull'altro.
Le letture grezze dei sensori non sono incluse.

## Fare un backup

```bash
porto backup --out /data/porto-2026-11-10.porto-backup
```

Con Docker:

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/porto-$(date +%F).porto-backup
```

Il file finisce nel volume dei dati: copialo fuori dal server (un altro disco, un altro sito). Pianifica il backup
dal `cron` dell'host o da un timer di systemd.

:::caution[Il backup non contiene la chiave]
Documenti, IBAN, chiavi dei provider e file sono cifrati con `PORTO_SECRET_KEY`. Conserva la chiave insieme ai
backup, in un posto sicuro: un backup senza la sua chiave non si può leggere.
:::

## Ripristinare

1. Prepara un'installazione con **la stessa versione di Porto** del backup e la **stessa `PORTO_SECRET_KEY`**.
2. Esegui il ripristino:

   ```bash
   porto restore --in porto-2026-11-10.porto-backup          # su un database vuoto
   porto restore --in porto-2026-11-10.porto-backup --force  # svuota prima il database
   ```

3. Avvia il server e accedi di nuovo: le sessioni non sono incluse.

Il ripristino rifiuta un database che contiene già dei dati (senza `--force`) e un archivio di una versione dello
schema diversa da quella del database. Per portare un backup su una versione più recente, ripristinalo con la sua
versione e poi aggiorna ([Aggiornamenti](/installazione/aggiornamenti/)).

## App desktop

L'app desktop fa da sé backup nello stesso formato: uno al giorno, uno a ogni chiusura, quelli manuali e uno prima
di ogni ripristino. Ne tiene 14 giornalieri e 5 di chiusura; i manuali non vengono mai cancellati. Il ripristino si
avvia dal pannello. Vedi [App desktop](/gestore/app-desktop/#backup).
