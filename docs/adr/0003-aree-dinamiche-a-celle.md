# ADR 0003 — Aree dinamiche a celle con vincolo unique

**Stato:** proposta · **Spec:** 7.4, 7.5, 10

## Contesto

Il *pontile dinamico* e le rimesse non hanno posti predefiniti: le barche si collocano in scala dove c'è spazio. Il
principio di Arenile (ADR Arenile 0003) vuole che la doppia occupazione sia impossibile per vincolo del database,
identico su SQLite e PostgreSQL. Alternative valutate:

1. **Vincolo di esclusione su intervalli** (`EXCLUDE USING gist` con `int4range` e `daterange`): solo PostgreSQL;
   SQLite non lo supporta.
2. **Riga di lock per area e notte più controllo di sovrapposizione nell'applicazione**: corretto se serializzato,
   ma la garanzia diventa codice applicativo, contro la regola del progetto.
3. **Celle discrete con `unique(area_id, cell, night)`**: garanzia nel database su entrambi i dialetti.

## Decisione

Opzione 3. Le aree sono divise in celle (default 50 cm lineari, 100 × 100 cm in superficie); un posizionamento occupa
**tutte le celle che il suo ingombro con margine tocca**, con arrotondamento per eccesso (spec 7.5). Celle disgiunte
implicano assenza di sovrapposizione fisica (INV-02).

## Conseguenze

- Spreco massimo di una cella per lato e per barca, compensato dalla scelta del passo per area.
- Righe proporzionali alla superficie del porto, non al numero di barche (stima in spec 10.1); archiviazione annua
  delle notti passate.
- La funzione `Footprint` è il cuore della correttezza: implementata in Go, specchiata in TypeScript per l'anteprima,
  con casi di parità condivisi.
- Cambiare il passo delle celle di un'area con occupazioni future richiede una migrazione dei dati dell'area
  (operazione esplicita con anteprima, rifiutata se crea conflitti).
