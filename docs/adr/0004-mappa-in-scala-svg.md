# ADR 0004 — Mappa SVG in scala reale con culling e livelli di dettaglio

**Stato:** proposta · **Spec:** 7.1, 9.1

## Contesto

Arenile disegna la spiaggia in SVG fino a 1.000 elementi. Un porto grande arriva a 1.500 posti, centinaia di risorse
a terra, colonnine e decorazioni, con silhouette delle barche in scala: circa 3.000 elementi. Canvas o WebGL darebbero
più margine ma perderebbero accessibilità (elementi focalizzabili, `aria-label`), test con Testing Library e il riuso
del renderer di Arenile.

## Decisione

- SVG con coordinate in centimetri e `viewBox` reale.
- **Culling per viewport:** si montano solo gli elementi che intersecano la vista più un margine (indice spaziale a
  griglia calcolato una volta per versione della mappa).
- **Livelli di dettaglio** per zoom: sotto soglia rettangoli pieni senza etichette né glifi; sopra soglia silhouette,
  etichette, badge.
- Elementi memoizzati per id e versione; il cambio di contesto ricalcola solo colori e glifi.
- Soglia di accettazione misurata in Playwright (spec 32.1). Se non raggiunta, si valuta un livello Canvas per lo
  sfondo e le strutture mantenendo SVG per gli elementi interattivi, con un nuovo ADR.

## Conseguenze

Stesso approccio accessibile di Arenile e stesso kit di illustrazioni; complessità aggiuntiva concentrata nel
componente mappa di `web/shared`.
