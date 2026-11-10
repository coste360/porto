---
title: Developing Porto
description: Repository, prerequisites and make commands to develop Porto.
sidebar:
  order: 1
---

Porto is a monorepo: Go server, React web apps, end-to-end tests, documentation and specification. The
[specification](https://github.com/coste360/porto/tree/main/docs/spec) (in Italian) is authoritative; architectural
decisions are in the [ADRs](https://github.com/coste360/porto/tree/main/docs/adr).

**Prerequisites:** Go 1.27+, Node.js 22+, npm, Docker (for PostgreSQL tests and the compose stack), `make`. Or the
included *devcontainer*.

```bash
git clone https://github.com/coste360/porto.git && cd porto
make setup     # Go modules, golangci-lint in ./bin, web and e2e dependencies
make seed      # local database with the demo marina (tmp/data)
make dev       # backend on :8080 + Vite for the panel (:5173) and the public page (:5174)
```

## Commands

The `Makefile` is the single interface for builds, tests and checks, for people and for CI.

| Command | What it does |
|---|---|
| `make gen` | Regenerates the Go server, TypeScript client and generated tables from `api/openapi.yaml` |
| `make lint` | `golangci-lint`, CSS, i18n key and contrast checks, ESLint, Prettier, `tsc` |
| `make test` | Go unit tests and Vitest (no Docker, no network) |
| `make test-db` | Integration tests on SQLite and on a temporary PostgreSQL |
| `make test-race` | Concurrency tests with `-race` on SQLite and PostgreSQL |
| `make e2e` / `make e2e-desktop` | Playwright on the real binary / on the desktop app |
| `make check` | `gen-check` + `lint` + `test` |
| `make check-full` | `check` + `test-db` + `test-race` + `e2e` + `e2e-desktop` |
| `make build` | Web apps and static `bin/porto` |
| `make licenses` | Dependency licence check |
| `make docs-dev` / `make docs-build` | This documentation site |

## Layout

```text
cmd/porto/            CLI and server
cmd/porto-desktop/    desktop app launcher
internal/domain/      domain logic, one package per area (no HTTP, no SQL)
internal/store/       database access (Bun), the only place with SQL
internal/http/        thin handlers, code generated from OpenAPI, SSE, static files
internal/migrations/  sqlite/ and postgres/, same numbering
pkg/ext/              public extension interfaces
api/openapi.yaml      API contract, the single source of truth
web/                  admin/, booking/, shared/ (npm workspaces)
e2e/                  Playwright tests
docs-site/            this site (Astro Starlight)
```

Continue with [Architecture](/en/sviluppo/architettura/), [Invariants](/en/sviluppo/invarianti/) and
[Contributing](/en/sviluppo/contribuire/).

## Development with coding agents

The repository contains `AGENTS.md`, `CLAUDE.md` and the `.claude/` folder (rules, procedures and reviewers) for
people developing with coding agents. It concerns development only: **the product has no artificial intelligence
features and calls no AI service**.
