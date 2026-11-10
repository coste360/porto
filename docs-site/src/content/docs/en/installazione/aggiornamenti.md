---
title: Updates
description: How to update Porto, versions and update notices in the desktop app.
sidebar:
  order: 9
---

## Versions

Porto follows [Semantic Versioning](https://semver.org/): `MAJOR.MINOR.PATCH`. Before 1.0.0 a minor version may
contain breaking changes; from 1.0.0 a breaking change of the REST API, of the configuration or of the migration path
needs a new major version. What changed in each version is in the
[CHANGELOG](https://github.com/coste360/porto/blob/main/CHANGELOG.md) and in the GitHub release notes.

The `ghcr.io/coste360/porto` image has the tags `X.Y.Z`, `X.Y`, `X` (from 1.0.0) and `latest` (stable releases only).
With `PORTO_VERSION=X.Y` you receive fixes without changing minor version.

## Updating with Docker

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/before-update.porto-backup
docker compose -f deploy/docker-compose.yml pull
docker compose -f deploy/docker-compose.yml up -d
```

Migrations run on start. With `PORTO_AUTO_MIGRATE=false` the server does not start until you run `porto migrate up`.
Read the CHANGELOG before every update.

## Updating from source

```bash
bin/porto backup --out before-update.porto-backup
git pull && make build
# restart the service
```

## Going back

Migrations only move forward. If an update goes wrong, bring back the previous version and restore the backup taken
before the update with `porto restore --force`.

## Desktop app

The desktop app checks once a day whether a newer version exists and shows a notice: it never downloads or installs
anything. The request carries no marina data, only version and operating system; turn it off on first start, in the
app settings or with `PORTO_UPDATE_CHECK=false`. To update, download and install the new package: the data folder
stays where it is.
