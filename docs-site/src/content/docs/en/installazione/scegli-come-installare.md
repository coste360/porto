---
title: Choose how to install
description: Desktop app, on-site server or online server with Docker - what changes and when internet is needed.
sidebar:
  order: 2
---

Porto is the same program in every case; only the place where the server runs changes.

| Where the server runs | Internet | Online booking, online payments, remote signature |
|---|---|---|
| **Desktop app** on the office PC | not needed | no (the invoicing export works) |
| **On-site server** (mini PC or NAS at the marina) | optional | yes, if the server is reachable from the internet |
| **Online server** (VPS with Docker) | needed | yes |

You move from one to another with a backup and a restore ([Backup and restore](/en/installazione/backup-e-ripristino/)).

## Desktop app

Server and data (SQLite) live on the office PC; other PCs and the dock staff's phones connect over the marina Wi-Fi.
No Coste360 service is needed. Signed packages are downloaded from Coste360:

- [Windows](https://updates.coste360.it/porto/download/latest/windows/x64)
- [macOS](https://updates.coste360.it/porto/download/latest/macos) (Apple Silicon and Intel)
- [Linux](https://updates.coste360.it/porto/download/latest/linux/x86_64) (AppImage)

Details in the [Desktop app](/en/gestore/app-desktop/) guide.

## Server with Docker

Online booking, the public profile and working from several sites need a server reachable from the internet, with a
domain and HTTPS. The simplest way: [Docker and Docker Compose](/en/installazione/docker/). Templates for
[Coolify](https://github.com/coste360/porto/tree/main/deploy/coolify) and
[CapRover](https://github.com/coste360/porto/tree/main/deploy/caprover) are ready too.

## From source

To develop, or without Docker: [Building from source](/en/installazione/compilare-dai-sorgenti/).

## Rough requirements

- **SQLite** (default) suits most marinas; **PostgreSQL** is recommended above 300 berths or with several marinas in
  the same installation ([Database](/en/installazione/database/)).
- The server is a static binary for Linux, macOS and Windows; the Docker image is for `amd64` and `arm64`.
