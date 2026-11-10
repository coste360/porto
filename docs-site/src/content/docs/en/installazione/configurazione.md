---
title: Configuration
description: The PORTO_* environment variables of the server and of the desktop app.
sidebar:
  order: 5
---

The server is configured with environment variables only, also documented in
[`.env.example`](https://github.com/coste360/porto/blob/main/.env.example). Porto checks every variable on start and,
when something is wrong, lists all the errors by variable name, never by value.

## Server

| Variable | Default | Description |
|---|---|---|
| `PORTO_ADDR` | `:8080` | Listen address |
| `PORTO_BASE_URL` | — (required) | Public URL used in links and cookies; must be `https` outside `localhost` and development mode |
| `PORTO_DATA_DIR` | `./data` (`/data` in the image) | SQLite database `porto.db`, encrypted files, certificates, backups |
| `DATABASE_URL` | empty | With `postgres://` or `postgresql://`, PostgreSQL instead of SQLite |
| `PORTO_SECRET_KEY` | — (required) | Encryption key, 32 bytes in base64 (`openssl rand -base64 32`). Several comma-separated keys for rotation: the first is active |
| `PORTO_AUTO_MIGRATE` | `true` | Apply migrations when `serve` starts; with `false` the server refuses to start with pending migrations |
| `PORTO_TLS_AUTOCERT_DOMAINS` | empty | Domains for automatic Let's Encrypt certificates (listens on 443 and 80) |
| `PORTO_TRUSTED_PROXIES` | empty | CIDRs of trusted reverse proxies, whose `X-Forwarded-For` and `X-Forwarded-Proto` are read |

## Email

| Variable | Default | Description |
|---|---|---|
| `PORTO_SMTP_URL` | empty | `smtp://user:password@host:587` (STARTTLS) or `smtps://user:password@host:465` |
| `PORTO_MAIL_FROM` | `Porto <noreply@localhost>` | Sender; with SMTP it must use a real domain |
| `PORTO_MAIL_DIR` | empty | Without SMTP, folder where mail is written as `.eml` files (development and tests); empty = log only |

Porto imposes no mail provider: use any SMTP server.

## Security and limits

| Variable | Default | Description |
|---|---|---|
| `PORTO_REQUIRE_TOTP_FOR_OWNERS` | `false` | Two-factor authentication required for owners and accounting |
| `PORTO_PUBLIC_READS_PER_MINUTE` | `120` | Requests per minute per client network on the public API (1–10000) |
| `PORTO_LOGIN_ATTEMPTS_PER_MINUTE` | `30` | Sign-in attempts per minute per address (1–30 in production); the limit of 5 failures per account in 15 minutes is fixed |
| `PORTO_HOLD_TTL` | `10m` | How long a berth is held during an online booking (1m–60m) |
| `PORTO_SENSOR_RAW_RETENTION` | `30d` | Retention of raw sensor readings (1d–3650d), not included in backups |

## Logs

| Variable | Default | Description |
|---|---|---|
| `PORTO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PORTO_LOG_FORMAT` | `json` (`text` in development) | Log format |
| `PORTO_TELEMETRY` | `false` | Porto has no telemetry: the value must stay `false` |

## First user and demo

| Variable | Default | Description |
|---|---|---|
| `PORTO_BOOTSTRAP_OWNER_EMAIL`, `PORTO_BOOTSTRAP_OWNER_NAME`, `PORTO_BOOTSTRAP_OWNER_ORG` | empty | Owner and organization created on start, only when the database has no users (all three plus `PORTO_ADMIN_PASSWORD`, or none) |
| `PORTO_ADMIN_PASSWORD` | empty | Password for `porto admin create`, the bootstrap owner and `porto seed demo`; never logged |
| `PORTO_DEMO_PAIRING_USER`, `PORTO_DEMO_PAIRING_MARINA` | empty | Demo instances only: user and marina slug for the demo device pairing (both or neither) |
| `PORTO_DEMO_INSTANCE` | empty | Only for `porto seed demo --reset` on a public demo instance ([Demo data](/en/installazione/dati-demo/)); never in production |

## Development and tests

Not for production.

| Variable | Default | Description |
|---|---|---|
| `PORTO_DEV` | `false` | Development mode: key generated in `PORTO_DATA_DIR/secret.key`, non-`Secure` cookies over `http`, text logs |
| `PORTO_WEBHOOKS_ALLOW_PRIVATE` | `false` | Webhooks to private addresses and plain `http` |
| `PORTO_DEV_FAKE_CHANNEL`, `PORTO_STRIPE_API_BASE` | `false`, empty | Only with `PORTO_DEV=true`: fake booking channel and fake Stripe API for tests |
| `PORTO_UPDATE_URL` | empty | Desktop app, only with `PORTO_DEV=true`: version manifest URL instead of Coste360's |
| `PORTO_DESKTOP_LAN_LOOPBACK` | `false` | Desktop app, only with `PORTO_DEV=true`: the local network listens on `127.0.0.1` (tests) |
| `PORTO_DESKTOP_FIRST_RUN_FILE` | `false` | Desktop app, only with `PORTO_DEV=true`: writes the first-run URL to a file in the data folder (tests) |

## Compose files

| Variable | Default | Description |
|---|---|---|
| `PORTO_VERSION` | `latest` | Tag of the `ghcr.io/coste360/porto` image (`X.Y` to receive fixes, `X.Y.Z` to stay put) |
| `PORTO_PUBLISH_PORT` | `8080` | Port published on the host |
| `POSTGRES_PASSWORD` | — | Only `deploy/docker-compose.postgres.yml` (required there) |

## Desktop app

The desktop app sets address, URL, data folder and secret key (kept in the system keychain) itself and uses SQLite
only. Optional variables:

| Variable | Default | Description |
|---|---|---|
| `PORTO_UPDATE_CHECK` | `true` | Daily check for a newer version; `false` = no request at all |
| `PORTO_DESKTOP_PORT` | remembered, else chosen by the OS | Local port of the app window |
| `PORTO_DESKTOP_LAN_PORT` | `8443` | Local-network HTTPS port |
| `PORTO_DESKTOP_KEYRING` | `auto` | `auto` (system keychain, then file) or `file` (`<data>/secret.key`) |

See also [Desktop app](/en/gestore/app-desktop/).
