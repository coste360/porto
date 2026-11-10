---
title: Architettura
description: Monobinario Go, livelli, flusso di una richiesta, outbox, job e tempo reale.
sidebar:
  order: 2
---

Porto è un **monobinario Go** che incorpora le due applicazioni web e l'API. Non richiede servizi esterni; l'app
desktop avvia lo stesso server nel proprio processo. Il documento completo è
[docs/architecture.md](https://github.com/coste360/porto/blob/main/docs/architecture.md).

```text
┌──────────────────────────────────────────────────────────────┐
│                         binario porto                        │
│  SPA pannello (PWA)   SPA pubblica (booking, scheda)  API v1 │
│                                │                             │
│  Servizi di dominio: harbor · occupancy · pricing · contract │
│  charge · payment · storage · yard · rental · sensor · report│
│                                │                             │
│  Job runner in-process   Store (Bun, unico SQL)   Outbox     │
└────────────────────────────────┼─────────────────────────────┘
                     SQLite (default) | PostgreSQL
```

## Stack

| Parte | Scelta |
|---|---|
| Linguaggio | Go, binario statico senza CGO (solo la finestra dell'app desktop usa CGO) |
| HTTP | `chi` e `net/http` |
| Contratto | OpenAPI 3.1 spec-first, codice generato con `oapi-codegen` |
| Dati | `uptrace/bun` sullo stesso codice per SQLite (`modernc.org/sqlite`, puro Go) e PostgreSQL |
| Migrazioni | `pressly/goose`, una serie per dialetto con la stessa numerazione |
| Documenti | PDF generati in Go, QR, Excel scritto in proprio |
| Frontend | Vite, React, TypeScript strict, TanStack Router e Query, client generato, Tailwind e shadcn/ui, i18next |
| Mappa | SVG in coordinate reali, con culling e livelli di dettaglio per migliaia di posti |

## Livelli

- `internal/http/*` decodifica la richiesta, chiama un servizio di dominio, codifica la risposta: niente logica e
  niente SQL.
- `internal/domain/<area>` contiene la logica. La prima riga di ogni metodo di servizio verifica il permesso e
  ottiene lo scope del marina; ogni query operativa è filtrata per `marina_id`.
- `internal/store` è l'unico package con SQL.
- I motori puri (geometria, compatibilità, prezzi, competenze, formule, ottimizzatore) non usano database, orologio o
  casualità: sono funzioni deterministiche testate a tabella e con property test.
- `pkg/ext` contiene le interfacce pubbliche e non importa nulla di `internal/`.

Le regole di dipendenza sono verificate da `golangci-lint` (depguard).

## Una richiesta

Request id → IP reale (solo dai proxy fidati) → log senza dati personali → recover → intestazioni di sicurezza →
limite del corpo → autenticazione (cookie, chiave API o token di dispositivo) → TOTP → CSRF → limiti → idempotenza →
handler generato → servizio di dominio. Il servizio apre una transazione e scrive insieme la modifica, la riga di
audit e l'evento nell'outbox. Gli errori diventano `problem+json` con uno slug stabile.

## Outbox, job e tempo reale

- Gli eventi scritti nell'outbox vengono distribuiti da un dispatcher ai webhook, al broker SSE e alle estensioni,
  almeno una volta.
- Un **job runner** nel processo esegue i lavori periodici: emissione delle rate, solleciti, scadenze, rilascio delle
  opzioni scadute, consegne di webhook ed email, riconciliazione dei sensori, pulizie.
- Il pannello riceve gli aggiornamenti via **SSE** e rilegge solo le query interessate.

## Tempo e numeri

- Il tempo arriva da un orologio iniettato; le date di calendario sono nel fuso del marina e l'unità di occupazione è
  la **notte**.
- Importi in centesimi interi, lunghezze in centimetri interi, quantità frazionarie in millesimi: mai virgola mobile
  nella logica di dominio.
