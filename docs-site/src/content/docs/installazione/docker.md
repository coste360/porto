---
title: Docker e Docker Compose
description: Installare Porto su un server Linux con Docker Compose, con SQLite o PostgreSQL.
sidebar:
  order: 3
---

**Requisiti:** un server Linux con Docker e Docker Compose e, per la prenotazione online, un dominio che punti al
server.

## Prova in locale

Il file `compose.yaml` nella radice del repository compila l'immagine dai sorgenti e avvia un'istanza in modalità
sviluppo con [Mailpit](https://mailpit.axllent.org/) per leggere le email inviate. **Non usarlo in produzione.**

```bash
git clone https://github.com/coste360/porto.git && cd porto
PORTO_BOOTSTRAP_OWNER_EMAIL=titolare@porto.test PORTO_BOOTSTRAP_OWNER_NAME="Mario Rossi" \
PORTO_BOOTSTRAP_OWNER_ORG="Marina Prova SRL" PORTO_ADMIN_PASSWORD=password-di-prova \
  docker compose up --build        # oppure: make up
```

Apri `http://localhost:8080/admin/` e accedi con quelle credenziali (password di almeno 12 caratteri); le email si
leggono su `http://localhost:8025`. Per caricare il porto dimostrativo:
`docker compose exec porto /porto seed demo` ([Dati dimostrativi](/installazione/dati-demo/)).

## Installazione su un server

I file di produzione sono in `deploy/`: `docker-compose.yml` (SQLite nel volume `porto-data`) e
`docker-compose.postgres.yml` (PostgreSQL 17). Il container gira come utente non root, con file system in sola
lettura e nessuna capability.

1. **Configurazione.** Parti da `.env.example` e crea `deploy/.env`:

   ```bash
   git clone https://github.com/coste360/porto.git && cd porto
   cat > deploy/.env <<FINE
   PORTO_BASE_URL=https://porto.mio-marina.it
   PORTO_SECRET_KEY=$(openssl rand -base64 32)
   PORTO_SMTP_URL=
   PORTO_BOOTSTRAP_OWNER_EMAIL=titolare@mio-marina.it
   PORTO_BOOTSTRAP_OWNER_NAME=Mario Rossi
   PORTO_BOOTSTRAP_OWNER_ORG=Marina Esempio SRL
   PORTO_ADMIN_PASSWORD=scegli-una-password-lunga
   FINE
   ```

   Le variabili sono descritte in [Configurazione](/installazione/configurazione/).

2. **Immagine.** Quando l'immagine è pubblicata il compose la scarica da sola (`PORTO_VERSION`, predefinito
   `latest`). Finché non lo è, compilala sul server e indica il tag:

   ```bash
   docker build -f deploy/Dockerfile -t ghcr.io/coste360/porto:dev .     # oppure: make docker
   echo "PORTO_VERSION=dev" >> deploy/.env
   ```

3. **Avvio.**

   ```bash
   docker compose -f deploy/docker-compose.yml up -d
   ```

4. **Primo accesso** su `https://porto.mio-marina.it/admin/`: vedi [Primo avvio](/installazione/primo-avvio/).

:::caution[Conserva la chiave segreta]
`PORTO_SECRET_KEY` cifra le chiavi dei provider, i documenti dei clienti e gli altri segreti. Conservala insieme ai
backup: senza di essa i dati cifrati non sono recuperabili.
:::

## Con PostgreSQL

Aggiungi `POSTGRES_PASSWORD` a `deploy/.env` e usa l'altro file:

```bash
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> deploy/.env
docker compose -f deploy/docker-compose.postgres.yml up -d
```

Il compose inserisce la password in `DATABASE_URL` senza codificarla: usa solo caratteri sicuri negli URL, come
quelli di `openssl rand -hex`. Vedi [Database](/installazione/database/).

## Comandi del binario nel container

L'immagine è *distroless* e non ha una shell, ma `exec` lancia direttamente il binario:

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto version
docker compose -f deploy/docker-compose.yml exec porto /porto migrate status
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/backup.porto-backup
```

## HTTPS

Metti Porto dietro un reverse proxy oppure lascia che ottenga da sé i certificati Let's Encrypt: vedi
[HTTPS e reverse proxy](/installazione/reverse-proxy-https/). I controlli di salute vanno fatti dall'esterno con
`GET /healthz` e `GET /readyz` ([Monitoraggio e log](/installazione/monitoraggio-e-log/)).
