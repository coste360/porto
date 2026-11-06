# ADR 0001 — Arenile come riferimento, stesso stack

**Stato:** accettata (decisione del titolare, 2026-11-06) · **Spec:** 3, 31

## Contesto

Arenile ha un'infrastruttura matura: monobinario Go, SQLite/PostgreSQL, outbox, job runner, SSE, autenticazione,
token per dispositivo, edizione desktop, app mobile Expo, offline con coda idempotente. Porto ha bisogno della stessa
forma su tutte le piattaforme, ma è un prodotto diverso, con un dominio diverso.

## Decisione

1. Porto adotta **lo stesso stack** di Arenile: Go, `chi`, OpenAPI 3.1 spec-first con `oapi-codegen`, Bun,
   SQLite (`modernc.org/sqlite`) e PostgreSQL, `goose`, Argon2id; Vite, React, TypeScript strict, TanStack Router e
   Query, **shadcn/ui** con Tailwind sui token del tema, i18next, react-hook-form + zod, Vitest, Playwright. L'app mobile
   non fa parte di questo repository.
2. Porto adotta gli **stessi pattern architetturali**, elencati nel README degli ADR: monobinario con SPA
   incorporate, vincoli del database per la disponibilità, motori puri, outbox nella stessa transazione, SSE senza
   dati personali, coda offline idempotente, edizione desktop con webview.
3. **Il codice di Arenile non si copia.** È un riferimento da leggere per capire come un problema è stato risolto.
   Il codice di Porto si scrive da zero, con nomi, modelli e API pensati per il dominio portuale. Si possono
   riprendere i componenti shadcn generati dalla CLI ufficiale, che non sono codice di Arenile.
4. Nessun modulo condiviso tra i due prodotti.

## Conseguenze

- Il codice nasce senza i residui del dominio balneare, senza commenti di provenienza e senza l'obbligo di riportare le
  correzioni tra i due repository.
- La partenza è più lenta della copia; la compensa avere già decisioni, pattern e test di Arenile come guida.
- Chi lavora su Porto legge Arenile per orientarsi, ma ogni scelta va riverificata sul dominio portuale.
