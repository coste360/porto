---
title: Aggiornamenti
description: Come aggiornare Porto, versioni e controllo delle novità nell'app desktop.
sidebar:
  order: 9
---

## Versioni

Porto segue il [Semantic Versioning](https://semver.org/): `MAJOR.MINOR.PATCH`. Prima della 1.0.0 una versione minore
può contenere modifiche non compatibili; dalla 1.0.0 una modifica non compatibile dell'API REST, della configurazione
o del percorso delle migrazioni richiede una nuova versione principale. Le novità di ogni versione sono nel
[CHANGELOG](https://github.com/coste360/porto/blob/main/CHANGELOG.md) e nelle note di rilascio su GitHub.

L'immagine `ghcr.io/coste360/porto` ha i tag `X.Y.Z`, `X.Y`, `X` (dalla 1.0.0) e `latest` (solo versioni stabili).
Con `PORTO_VERSION=X.Y` ricevi le correzioni senza cambiare versione minore.

## Aggiornare con Docker

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/prima-aggiornamento.porto-backup
docker compose -f deploy/docker-compose.yml pull
docker compose -f deploy/docker-compose.yml up -d
```

Le migrazioni vengono applicate all'avvio. Con `PORTO_AUTO_MIGRATE=false` il server non parte finché non esegui
`porto migrate up`. Leggi il CHANGELOG prima di ogni aggiornamento.

## Aggiornare dai sorgenti

```bash
bin/porto backup --out prima-aggiornamento.porto-backup
git pull && make build
# riavvia il servizio
```

## Tornare indietro

Le migrazioni vanno solo avanti. Se un aggiornamento non va, riporta la versione precedente e ripristina il backup
fatto prima dell'aggiornamento con `porto restore --force`.

## App desktop

L'app desktop controlla una volta al giorno se esiste una nuova versione e lo segnala con un avviso: non scarica e non
installa mai nulla. La richiesta non contiene dati del porto, solo versione e sistema operativo; si disattiva al primo
avvio, dalle impostazioni dell'app o con `PORTO_UPDATE_CHECK=false`. Per aggiornare scarica e installa il nuovo
pacchetto: la cartella dati resta dov'è.
