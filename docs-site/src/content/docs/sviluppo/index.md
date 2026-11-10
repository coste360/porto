---
title: Sviluppare Porto
description: Repository, prerequisiti e comandi make per sviluppare Porto.
sidebar:
  order: 1
---

Porto è un monorepo: server Go, applicazioni web React, test end-to-end, documentazione e specifica. La
[specifica](https://github.com/coste360/porto/tree/main/docs/spec) (in italiano) è autorevole; le decisioni
architetturali sono negli [ADR](https://github.com/coste360/porto/tree/main/docs/adr).

**Prerequisiti:** Go 1.27+, Node.js 22+, npm, Docker (per i test su PostgreSQL e il compose), `make`. In alternativa
il *devcontainer* incluso.

```bash
git clone https://github.com/coste360/porto.git && cd porto
make setup     # moduli Go, golangci-lint in ./bin, dipendenze web ed e2e
make seed      # database locale con il porto dimostrativo (tmp/data)
make dev       # backend su :8080 + Vite per pannello (:5173) e pagina pubblica (:5174)
```

## Comandi

Il `Makefile` è l'unica interfaccia per build, test e verifiche, per le persone e per la CI.

| Comando | Cosa fa |
|---|---|
| `make gen` | Rigenera server Go, client TypeScript e tabelle generate da `api/openapi.yaml` |
| `make lint` | `golangci-lint`, controlli del CSS, delle chiavi i18n e del contrasto, ESLint, Prettier, `tsc` |
| `make test` | Test unitari Go e Vitest (senza Docker né rete) |
| `make test-db` | Test di integrazione su SQLite e su un PostgreSQL temporaneo |
| `make test-race` | Test di concorrenza con `-race` su SQLite e PostgreSQL |
| `make e2e` / `make e2e-desktop` | Playwright sul binario reale / sull'app desktop |
| `make check` | `gen-check` + `lint` + `test` |
| `make check-full` | `check` + `test-db` + `test-race` + `e2e` + `e2e-desktop` |
| `make build` | Applicazioni web e `bin/porto` statico |
| `make licenses` | Verifica delle licenze delle dipendenze |
| `make docs-dev` / `make docs-build` | Questo sito di documentazione |

## Struttura

```text
cmd/porto/            CLI e server
cmd/porto-desktop/    launcher dell'app desktop
internal/domain/      logica di dominio, un package per area (niente HTTP, niente SQL)
internal/store/       accesso al database (Bun), unico posto con SQL
internal/http/        handler sottili, codice generato da OpenAPI, SSE, file statici
internal/migrations/  sqlite/ e postgres/, stessa numerazione
pkg/ext/              interfacce pubbliche delle estensioni
api/openapi.yaml      contratto API, unica fonte di verità
web/                  admin/, booking/, shared/ (npm workspaces)
e2e/                  test Playwright
docs-site/            questo sito (Astro Starlight)
```

Continua con [Architettura](/sviluppo/architettura/), [Invarianti](/sviluppo/invarianti/) e
[Contribuire](/sviluppo/contribuire/).

## Sviluppo con agenti di coding

Il repository contiene `AGENTS.md`, `CLAUDE.md` e la cartella `.claude/` (regole, procedure e revisori) per chi
sviluppa con agenti di coding. Riguarda solo lo sviluppo: **il prodotto non contiene funzioni di intelligenza
artificiale e non chiama servizi di IA**.
