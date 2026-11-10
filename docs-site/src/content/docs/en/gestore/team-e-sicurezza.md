---
title: Team and security
description: Users and roles, two-factor authentication, API keys, devices, webhooks and the activity log.
sidebar:
  order: 21
---

## Team

*Team* lists the people working on the marina with their role; from here you invite new users by email, change roles or
remove someone. A user can belong to the whole organization (valid for all its marinas) or to a single marina, and can
have different roles in different marinas. The last owner cannot be removed.

Roles are described in the [Overview](/en/gestore/#roles). Permissions are checked by the server on every operation,
not just by hiding menus.

## Sign-in

- Passwords of at least 12 characters, stored with Argon2id.
- **Two-factor authentication** (TOTP) from *Il mio account* (My account), with recovery codes; it can be made mandatory
  for owners and accounting (`PORTO_REQUIRE_TOTP_FOR_OWNERS`).
- Password reset by email with a single-use link.
- Limit of 5 failed attempts per account in 15 minutes.

## API keys

*Chiavi API* creates keys to connect other programs: choose the scopes; the key is shown once and can be revoked at any
time ([Authentication](/en/api/autenticazione/)).

## Devices

*Dispositivi* pairs phones, tablets and workstations with a code or QR and lets you revoke them
([Phone and offline](/en/gestore/telefono-e-offline/)).

## Webhooks

*Webhook* sends the marina's events to other systems, signed, with a delivery log and resend
([Events and webhooks](/en/api/eventi-e-webhook/)).

## Activity log

*Registro attività* shows who did what and when: every write from the panel, the API or automatic processes leaves a
row.

## Data and privacy

Data stay on the computer or server you choose: nobody, Coste360 included, can access them. Documents, IBANs and
provider secrets are encrypted; logs, events and webhooks contain no personal data. The marina is the data controller
([Customers and vessels](/en/gestore/clienti-e-imbarcazioni/#duplicates-and-gdpr)).
