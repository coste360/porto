# ADR 0002 — La notte come unità di occupazione

**Stato:** proposta · **Spec:** 5, 10

## Contesto

Arenile occupa le postazioni per data e fascia (`AM`, `PM`). Nei porti l'unità commerciale e operativa è la notte:
un transito arriva il pomeriggio e parte la mattina; i contratti lunghi sono comunque misurati in giorni.

## Decisione

- L'unità di occupazione di posti, celle e risorse a terra è la **notte** (`night`, data in cui la notte inizia, nel
  fuso del marina). Arrivo D1 e partenza D2 occupano le notti `[D1, D2)`.
- Due transiti con partenza e arrivo nello stesso giorno sullo stesso posto non sono in conflitto; gli orari di
  arrivo e partenza sono attributi del soggiorno (avvisi se si sovrappongono), non dell'occupazione.
- Il noleggio e i mezzi di sollevamento usano fasce orarie in tabelle separate con lo stesso principio di vincolo.

## Conseguenze

- Volumi prevedibili (al massimo 365 righe per risorsa e anno).
- Il passaggio di una barca tra posto e piazzale nello stesso giorno è rappresentato da due allocazioni contigue.
