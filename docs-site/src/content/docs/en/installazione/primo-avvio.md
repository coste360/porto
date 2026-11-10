---
title: First start
description: Create the owner, the organization and the first marina, then set up the harbour.
sidebar:
  order: 11
---

## 1. The owner

The first user is the **owner** (role `owner`) of an **organization**, which can have one or more marinas. It is
created in one of three ways:

- **on start**, with `PORTO_BOOTSTRAP_OWNER_EMAIL`, `PORTO_BOOTSTRAP_OWNER_NAME`, `PORTO_BOOTSTRAP_OWNER_ORG` and
  `PORTO_ADMIN_PASSWORD`: the owner is created only when the database has no users yet. After the first sign-in remove
  `PORTO_ADMIN_PASSWORD` from the configuration;
- **from the command line**, together with the first marina if you like:

  ```bash
  PORTO_ADMIN_PASSWORD='choose-a-long-password' porto admin create \
    --email owner@my-marina.example --name "Mario Rossi" --org "Example Marina Ltd" \
    --marina "Example Marina" --slug example-marina --timezone Europe/Rome --locale en
  ```

  With Docker: `docker compose -f deploy/docker-compose.yml exec -e PORTO_ADMIN_PASSWORD='…' porto /porto admin create …`;
- **in the desktop app**, from the first-run page that opens by itself.

The password must have at least 12 characters.

## 2. The marina

Sign in to the panel at `https://<your-domain>/admin/`. If you did not create the marina from the command line, create
it from the organization page: name, slug (used in the public addresses `/b/{slug}` and `/p/{slug}`), time zone and
language. Porto's dates are always in the marina's time zone.

## 3. Setting up the harbour

1. **Map**: draw quays, piers and berths with the generators, or import berths from CSV
   ([Harbour map](/en/gestore/mappa-del-porto/)).
2. **Price lists**: items, berth categories, periods and prices ([Price lists](/en/gestore/listini-e-prezzi/)).
3. **Team**: invite colleagues with the right role ([Team and security](/en/gestore/team-e-sicurezza/)).
4. **Contracts and print templates** ([Contracts and transits](/en/gestore/contratti-e-transiti/)).
5. Optional: email (SMTP), online payments, online booking and the public profile.

To try first on realistic data, load the demo marina in a separate installation
([Demo data](/en/installazione/dati-demo/)).
