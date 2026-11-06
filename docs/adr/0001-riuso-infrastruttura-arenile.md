# ADR 0001 — Riuso dell'infrastruttura di Arenile

**Stato:** proposta · **Spec:** 3, 31

## Contesto

Arenile ha un'infrastruttura matura e verificata: monobinario Go, SQLite/PostgreSQL, outbox, job runner, SSE,
autenticazione, token per dispositivo, edizione desktop con webview e ponte verso le periferiche, app mobile Expo,
offline con coda idempotente, pipeline di build e firma. Porto ha bisogno della stessa forma su tutte le piattaforme.
Go non consente di importare `internal/` da un altro modulo.

## Decisione

1. Porto adotta le decisioni infrastrutturali di Arenile elencate nel README degli ADR.
2. In M0 i package indipendenti dal dominio (elenco in spec 3.3) sono copiati con i loro test e un commento di
   provenienza con il commit di origine.
3. Non si introduce subito un modulo comune: estrarre ora `coste360/platform` rallenterebbe Arenile, che è in
   produzione, per un beneficio che si vede solo quando entrambi i prodotti sono stabili.
4. Le correzioni di sicurezza su un package copiato si riportano nell'altro repository entro una settimana; la CI di
   Porto ha un controllo che elenca i file con provenienza e il commit di Arenile da cui derivano.

## Conseguenze

- Partenza rapida e stessa esperienza su server, desktop e mobile.
- Debito controllato: duplicazione temporanea, da chiudere con un ADR di estrazione quando i package copiati non
  cambiano da tre mesi in nessuno dei due repository.
