---
title: Scegli come installare
description: App desktop, server in sede o server online con Docker - cosa cambia e quando serve internet.
sidebar:
  order: 2
---

Porto è lo stesso programma in tutti i casi; cambia solo dove gira il server.

| Dove gira il server | Internet | Prenotazione online, pagamenti online, firma remota |
|---|---|---|
| **App desktop** sul PC dell'ufficio | non necessaria | no (l'esportazione per la fatturazione funziona) |
| **Server in sede** (mini PC o NAS nel marina) | facoltativa | sì, se il server è raggiungibile da internet |
| **Server online** (VPS con Docker) | necessaria | sì |

Si passa da un modo all'altro con un backup e un ripristino
([Backup e ripristino](/installazione/backup-e-ripristino/)).

## App desktop

Server e archivio (SQLite) stanno sul PC dell'ufficio; gli altri PC e i telefoni degli ormeggiatori si collegano
sulla Wi-Fi del porto. Nessun servizio di Coste360 è necessario. I pacchetti firmati si scaricano da Coste360 a
partire dal primo rilascio:

- [Windows](https://updates.coste360.it/porto/download/latest/windows/x64)
- [macOS](https://updates.coste360.it/porto/download/latest/macos) (Apple Silicon e Intel)
- [Linux](https://updates.coste360.it/porto/download/latest/linux/x86_64) (AppImage)

Dettagli nella guida dell'[App desktop](/gestore/app-desktop/).

## Server con Docker

Per la prenotazione online, la scheda pubblica e il lavoro da più sedi serve un server raggiungibile da internet,
con un dominio e HTTPS. È la strada più semplice: [Docker e Docker Compose](/installazione/docker/). Sono pronti
anche i modelli per [Coolify](https://github.com/coste360/porto/tree/main/deploy/coolify) e
[CapRover](https://github.com/coste360/porto/tree/main/deploy/caprover).

## Dai sorgenti

Per sviluppare, o se non vuoi usare Docker: [Compilare dai sorgenti](/installazione/compilare-dai-sorgenti/).

## Requisiti indicativi

- **SQLite** (predefinito) va bene per la maggior parte dei porti; **PostgreSQL** è consigliato oltre i 300 posti o
  con più marine nella stessa installazione ([Database](/installazione/database/)).
- Il server è un binario statico per Linux, macOS e Windows; l'immagine Docker è per `amd64` e `arm64`.
