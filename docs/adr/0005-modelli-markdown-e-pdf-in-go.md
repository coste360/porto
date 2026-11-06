# ADR 0005 — Modelli Markdown e PDF generato in Go

**Stato:** proposta · **Spec:** 15

## Contesto

Contratti, preventivi ed estratti conto devono essere PDF stabili (hash, firma, PDF/A) generati anche
nell'edizione desktop. Un browser headless (Chromium) violerebbe il principio "un binario, nessun servizio
esterno"; HTML arbitrario renderizzato in Go non è realistico.

## Decisione

- Modelli in un **sottoinsieme di Markdown** (titoli, paragrafi, grassetto, corsivo, elenchi, tabelle, immagini del
  marina, interruzioni di pagina) più campi di unione e blocchi `each` / `if` in un linguaggio chiuso.
- Renderer puro in Go basato su `go-pdf/fpdf`, con caratteri incorporati e uscita PDF/A-2b per i documenti da firmare.
- Anteprima nel pannello generata dallo stesso renderer (il PDF, non un'imitazione HTML).

## Conseguenze

- Layout meno libero di un editor visuale, ma documenti identici su ogni piattaforma e verificabili con test di
  regressione sul testo estratto.
- Nessuna dipendenza nuova oltre `fpdf`, già prevista dallo stack di Arenile.
