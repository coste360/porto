---
title: Authentication
description: Panel sessions, scoped API keys, device tokens and permissions.
sidebar:
  order: 2
---

## API keys

For integrations create an **API key** in the panel (*Chiavi API* / API keys). The key starts with `po_live_`, is shown
**once** at creation and is stored only as a hash. Send it in the `Authorization` header:

```bash
curl -H "Authorization: Bearer po_live_…" https://porto.my-marina.example/api/admin/v1/meta
```

- **Scopes** are chosen at creation and are a subset of the creator's permissions; a key can never manage users.
- A key can be revoked from the panel at any time.
- Requests with `Authorization: Bearer` use no cookies and need no CSRF token.

## Panel sessions

The panel uses the `porto_session` cookie (HttpOnly, `Secure`, SameSite=Lax). Requests that change data must send the
`X-CSRF-Token` header with the value of the `porto_csrf` cookie. Sessions expire after 24 hours of inactivity and in
any case after 14 days; changing the password revokes the other sessions.

## Devices

Phones and desktop apps in remote mode pair with a single-use code or a QR (*Dispositivi* / Devices) and receive a
**device token** `po_dev_…`, bound to a user and a marina. The role is read again on every request: removing the user
from the marina revokes the device too.

## Permissions

Permissions come from roles (owner, management, office, accounting, dock staff, yard manager, yard worker, rental
staff, read only) and are checked by the server on every operation. Each operation summary in the
[reference](/api/riferimento/) names the required permission in brackets, for example `(contracts.read)`. See
[Team and security](/en/gestore/team-e-sicurezza/).

## Limits

Sign-in is limited per account (5 failures in 15 minutes) and per address; the public API has a limit per client
network (`PORTO_PUBLIC_READS_PER_MINUTE`). Over the limit the answer is `429` with `Retry-After`.
