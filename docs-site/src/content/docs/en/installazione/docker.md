---
title: Docker and Docker Compose
description: Install Porto on a Linux server with Docker Compose, on SQLite or PostgreSQL.
sidebar:
  order: 3
---

**Requirements:** a Linux server with Docker and Docker Compose and, for online booking, a domain pointing to it.

## Try it locally

The `compose.yaml` file at the repository root builds the image from source and starts an instance in development
mode with [Mailpit](https://mailpit.axllent.org/) to read outgoing mail. **Do not use it in production.**

```bash
git clone https://github.com/coste360/porto.git && cd porto
PORTO_BOOTSTRAP_OWNER_EMAIL=owner@porto.test PORTO_BOOTSTRAP_OWNER_NAME="Mario Rossi" \
PORTO_BOOTSTRAP_OWNER_ORG="Test Marina Ltd" PORTO_ADMIN_PASSWORD=test-password-123 \
  docker compose up --build        # or: make up
```

Open `http://localhost:8080/admin/` and sign in with those credentials (password of at least 12 characters); mail is
at `http://localhost:8025`. To load the demo marina: `docker compose exec porto /porto seed demo`
([Demo data](/en/installazione/dati-demo/)).

## Installing on a server

The production files are in `deploy/`: `docker-compose.yml` (SQLite in the `porto-data` volume) and
`docker-compose.postgres.yml` (PostgreSQL 17). The container runs as a non-root user with a read-only file system and
no capabilities.

1. **Configuration.** Start from `.env.example` and create `deploy/.env`:

   ```bash
   git clone https://github.com/coste360/porto.git && cd porto
   cat > deploy/.env <<END
   PORTO_BASE_URL=https://porto.my-marina.example
   PORTO_SECRET_KEY=$(openssl rand -base64 32)
   PORTO_SMTP_URL=
   PORTO_BOOTSTRAP_OWNER_EMAIL=owner@my-marina.example
   PORTO_BOOTSTRAP_OWNER_NAME=Mario Rossi
   PORTO_BOOTSTRAP_OWNER_ORG=Example Marina Ltd
   PORTO_ADMIN_PASSWORD=choose-a-long-password
   END
   ```

   The variables are described in [Configuration](/en/installazione/configurazione/).

2. **Image.** Once the image is published the compose file pulls it (`PORTO_VERSION`, default `latest`). Until then,
   build it on the server and set the tag:

   ```bash
   docker build -f deploy/Dockerfile -t ghcr.io/coste360/porto:dev .     # or: make docker
   echo "PORTO_VERSION=dev" >> deploy/.env
   ```

3. **Start.**

   ```bash
   docker compose -f deploy/docker-compose.yml up -d
   ```

4. **First sign-in** at `https://porto.my-marina.example/admin/`: see [First start](/en/installazione/primo-avvio/).

:::caution[Keep the secret key]
`PORTO_SECRET_KEY` encrypts provider keys, customer documents and other secrets. Keep it with your backups: without
it the encrypted data cannot be recovered.
:::

## With PostgreSQL

Add `POSTGRES_PASSWORD` to `deploy/.env` and use the other file:

```bash
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> deploy/.env
docker compose -f deploy/docker-compose.postgres.yml up -d
```

The compose file puts the password into `DATABASE_URL` without encoding it: use URL-safe characters only, like those
of `openssl rand -hex`. See [Database](/en/installazione/database/).

## Binary commands in the container

The image is *distroless* and has no shell, but `exec` runs the binary directly:

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto version
docker compose -f deploy/docker-compose.yml exec porto /porto migrate status
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/backup.porto-backup
```

## HTTPS

Put Porto behind a reverse proxy or let it obtain Let's Encrypt certificates itself: see
[HTTPS and reverse proxy](/en/installazione/reverse-proxy-https/). Health checks are done from outside with
`GET /healthz` and `GET /readyz` ([Monitoring and logs](/en/installazione/monitoraggio-e-log/)).
