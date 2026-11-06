## 3. Architettura e stack

### 3.1 Panoramica

Stessa forma di Arenile (spec Arenile 3, ADR Arenile 0001–0005): un binario Go che contiene API REST, SSE, job
runner, outbox e le SPA React incorporate con `embed.FS`.

```
 browser ufficio / desktop          browser diportista / sito del porto      desktop remoto        sensori
        │ /admin/ /api/admin/v1/*            │ /b/{slug} /api/public/v1/{slug}/*   │ /api/device/v1    │ /api/ingest/v1
        ▼                                    ▼                                     ▼                   ▼
┌──────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ binario porto                                                                                            │
│  middleware (log, recover, auth, CSRF, rate limit, scope del marina, header di sicurezza)               │
│  http/admin · http/public · http/device · http/ingest · http/sse · http/webhooks                         │
│                         │                                                                                │
│  domain/*  harbor (mappa) · occupancy · fit · pricing · contract · charge · payment · cash · fiscal ·    │
│            customer · vessel · storage · yard · rental · maintenance · sensor · report · notify          │
│                         │                                                                                │
│  store (Bun, unico posto con SQL) ──► outbox_events ──► jobs (dispatcher, scadenze, rate, solleciti,     │
│                                                              hold scaduti, solleciti, sensori)           │
└─────────────────────────┼────────────────────────────────────────────────────────────────────────────────┘
                          ▼
              SQLite (WAL, /data/porto.db)  |  PostgreSQL (DATABASE_URL)
```

### 3.2 Stack

Backend e frontend coincidono con Arenile (spec Arenile 3.2–3.4); le aggiunte sono motivate qui.

| Componente | Scelta | Note |
|---|---|---|
| Linguaggio, router, API | Go stabile, `chi`, OpenAPI 3.1 + `oapi-codegen` | Come Arenile |
| Dati | `uptrace/bun`, `modernc.org/sqlite` (senza CGO), `pgdriver`, `goose` per dialetto | Come Arenile |
| ID, importi, date | UUIDv7; `int64` centesimi; `YYYY-MM-DD` nel fuso del marina | Come Arenile |
| Misure | `int64` **centimetri** (`geo.Cm`), aree in cm² (`geo.Cm2`) | Nuovo: `internal/geo` |
| Quantità frazionarie | Virgola fissa a 3 decimali (`qty.Milli`, `int64`) per ore, metri, m², kWh | Nuovo: `internal/qty` |
| PDF | `go-pdf/fpdf` con un renderer puro di un sottoinsieme Markdown (ADR 0005) | Contratti, preventivi, estratti conto |
| Frontend | Vite, React, TypeScript strict, TanStack Router e Query, shadcn/ui, Tailwind, i18next, react-hook-form + zod | Come Arenile (ADR Arenile 0009) |
| Mappa | SVG con coordinate in cm, `@use-gesture/react`, culling per viewport e livelli di dettaglio (ADR 0004) | Porti fino a 3.000 elementi |
| Planner (noleggio, cantiere, rimesse) | Componente griglia temporale condiviso in `web/shared/src/planner` (SVG) | Nessuna libreria Gantt esterna |
| Grafici | Componenti shadcn chart (Recharts) già nel kit | Solo pannello |
| Desktop | `cmd/porto-desktop`, webview di sistema e systray come Arenile | Spec 31.2 |

### 3.3 Rapporto con Arenile

Arenile è un **riferimento**, non una fonte di codice (ADR 0001). Porto usa lo stesso stack e gli stessi pattern
architetturali (monobinario, outbox, motori puri, vincoli del database, SSE, offline idempotente, desktop con
webview), ma il codice si scrive da zero per il dominio portuale. Quando questa specifica dice "come
Arenile" indica un comportamento da riprodurre, non file da copiare.

### 3.4 Regole di dipendenza

Identiche ad Arenile: `internal/domain/*` non importa `net/http`, `internal/http/*` né driver SQL;
`internal/http/*` non contiene SQL né logica; `pkg/ext` non importa `internal/`. I motori puri
(`fit`, `pricing`, `layout`, `accrual`, `formula`, `optimizer`) non importano `store`, `clock` né `time.Now`.
