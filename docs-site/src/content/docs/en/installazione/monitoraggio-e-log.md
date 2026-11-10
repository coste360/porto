---
title: Monitoring and logs
description: Health checks, logs and the activity log.
sidebar:
  order: 12
---

## Health checks

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | the process is alive |
| `GET /readyz` | the database is reachable and migrations are up to date |

The Docker image has no shell: checks are made from outside (proxy, platform or monitoring system).

## Logs

Porto writes logs to standard error, as JSON (`PORTO_LOG_FORMAT=json`, default) or text, at the `PORTO_LOG_LEVEL`
level. With Docker read them with `docker compose logs porto`.

Logs never contain personal data or secrets: no emails, phone numbers, documents, tokens, passwords or keys, not even
in the request log.

## Activity log

Every write from the panel, the API or the jobs leaves a row in the **audit log**, visible in the panel under
*Registro attività* (Activity log) to users with administration permissions.

## Email

Mail leaves from an internal queue: an SMTP error does not block the operation that produced the message, and sending
is retried. Without `PORTO_SMTP_URL` messages are written as `.eml` files to `PORTO_MAIL_DIR` when set, otherwise only
logged (without the recipient).
