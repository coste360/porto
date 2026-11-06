# ADR 0006 — Linguaggio di formule interno per il cantiere

**Stato:** proposta · **Spec:** 18.2

## Contesto

I prezzi del cantiere dipendono dalle caratteristiche della barca (superficie dell'opera viva, motori, lunghezza)
con formule configurate dal gestore. Le librerie generiche di espressioni lavorano in `float64`, espongono più
potere del necessario e rendono difficile garantire determinismo e importi in centesimi.

## Decisione

Package puro `internal/domain/formula`: parser a discesa ricorsiva per un linguaggio minimo (numeri, variabili,
`+ - * /`, confronti, `and`/`or`, `if`, `min`, `max`, `round`, `ceil`, `floor`, `band`), valutazione in virgola fissa a
3 decimali (`qty.Milli`), nessun ciclo, nessun accesso a dati esterni, limite di lunghezza e profondità. Le formule
sono validate al salvataggio con le variabili dichiarate e un caso di prova.

## Conseguenze

- Determinismo e arrotondamenti coerenti con `internal/money`; fuzzing del parser in CI.
- Funzioni aggiuntive richiedono una modifica del package (voluta: ogni funzione è documentata e testata).
