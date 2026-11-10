---
title: Architecture
description: Single Go binary, layers, request flow, outbox, jobs and live updates.
sidebar:
  order: 2
---

Porto is a **single Go binary** that embeds the two web apps and the API. It needs no external service; the desktop
app runs the same server in its own process. The full document is
[docs/architecture.md](https://github.com/coste360/porto/blob/main/docs/architecture.md) (Italian).

```text
┌──────────────────────────────────────────────────────────────┐
│                         porto binary                         │
│  Panel SPA (PWA)      Public SPA (booking, profile)   API v1 │
│                                │                             │
│  Domain services: harbor · occupancy · pricing · contract   │
│  charge · payment · storage · yard · rental · sensor · report│
│                                │                             │
│  In-process job runner   Store (Bun, only SQL)    Outbox     │
└────────────────────────────────┼─────────────────────────────┘
                     SQLite (default) | PostgreSQL
```

## Stack

| Part | Choice |
|---|---|
| Language | Go, static binary without CGO (only the desktop app window uses CGO) |
| HTTP | `chi` and `net/http` |
| Contract | OpenAPI 3.1 spec-first, code generated with `oapi-codegen` |
| Data | `uptrace/bun` with the same code for SQLite (`modernc.org/sqlite`, pure Go) and PostgreSQL |
| Migrations | `pressly/goose`, one series per dialect with the same numbering |
| Documents | PDFs generated in Go, QR codes, Excel written in-house |
| Frontend | Vite, React, strict TypeScript, TanStack Router and Query, generated client, Tailwind and shadcn/ui, i18next |
| Map | SVG in real coordinates, with culling and levels of detail for thousands of berths |

## Layers

- `internal/http/*` decodes the request, calls a domain service, encodes the response: no logic and no SQL.
- `internal/domain/<area>` holds the logic. The first line of every service method checks the permission and obtains
  the marina scope; every operational query is filtered by `marina_id`.
- `internal/store` is the only package with SQL.
- The pure engines (geometry, fit, pricing, accruals, formulas, optimizer) use no database, clock or randomness: they
  are deterministic functions covered by table and property tests.
- `pkg/ext` holds the public interfaces and imports nothing from `internal/`.

Dependency rules are enforced by `golangci-lint` (depguard).

## A request

Request id → real IP (only from trusted proxies) → log without personal data → recover → security headers → body
limit → authentication (cookie, API key or device token) → TOTP → CSRF → limits → idempotency → generated handler →
domain service. The service opens a transaction and writes the change, the audit row and the outbox event together.
Errors become `problem+json` with a stable slug.

## Outbox, jobs and live updates

- Events written to the outbox are dispatched to webhooks, the SSE broker and extensions, at least once.
- An in-process **job runner** runs periodic work: instalment issuing, reminders, deadlines, release of expired
  options, webhook and email deliveries, sensor reconciliation, clean-ups.
- The panel receives updates through **SSE** and re-reads only the affected queries.

## Time and numbers

- Time comes from an injected clock; calendar dates are in the marina time zone and the unit of occupancy is the
  **night**.
- Money in integer cents, lengths in integer centimetres, fractional quantities in thousandths: never floating point
  in domain logic.
