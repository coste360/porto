## 12. Listini, articoli e motore prezzi

### 12.1 Articoli e servizi

`items`: anagrafica di tutto ciò che si vende o si addebita.
- `code` univoco per marina (codifica configurabile, es. `ORM-ANN`, `TRN-NTT`, `SRV-ALG`), descrizione i18n, tipo
  (`berth_fee`, `service`, `utility`, `product`, `labour`, `material`, `fee`, `deposit`), unità di misura
  (`night`, `month`, `season`, `year`, `m`, `m2`, `kwh`, `m3`, `hour`, `piece`, `lump`).
- **Fiscale:** aliquota IVA o natura dell'esenzione (codici `N1`–`N7`, riportati nell'esportazione così il
  software di fatturazione li riceve già pronti), prezzo IVA inclusa o esclusa
  (`price_includes_vat`, default esclusa per i contratti B2B, configurabile).
- **Classificazione dei ricavi:** categoria e sottocategoria (`revenue_class`: ormeggi annuali, transiti,
  rimessaggio, servizi, cantiere manodopera, cantiere materiali, noleggio, utenze, oneri) e codice dell'articolo nel
  software di fatturazione esterno (`external_code`, per l'esportazione).
- Stato attivo, visibilità online, ordinamento.

### 12.2 Listini per periodi

- Un listino (`price_lists`) ha validità (da–a), stato (`draft`, `active`, `archived`) e regole. Più listini attivi
  non si sovrappongono per la stessa combinazione di articolo e ambito.
- **Periodi tariffari** (`rate_periods`, come Arenile): alta, media, bassa stagione, eventi (salone nautico,
  regate), con intervalli di date.
- Ambito di una regola: articolo, tipo di contratto (annuale, stagionale, mensile, transito, rimessaggio, charter),
  gruppo o categoria di posto, canale (`desk`, `online`, `api`, canale esterno), categoria del cliente.

### 12.3 Basi di calcolo

Ogni regola di prezzo ha una `basis`:

| Base | Formula (prima di modificatori e sconti) | Uso tipico |
|---|---|---|
| `per_berth` | importo per categoria del posto | posto venduto a prescindere dalla barca |
| `length_band` | importo della fascia di lunghezza (es. 10,01–12,00 m) | listini a fasce, il più diffuso |
| `per_meter` | `€/m × loa` (arrotondamento al decimetro configurabile) | transiti |
| `per_m2` | `€/m² × loa × beam` (con minimi, es. beam ≥ 1/3 loa) | prezzo al metro quadro |
| `per_m2_berth` | `€/m² × lunghezza × larghezza del posto` | oneri, posti venduti come superficie |
| `flat` | importo fisso | servizi |
| `metered` | `€/unità × consumo` | acqua, energia |

- La dimensione usata (`loa` della barca, lunghezza del posto, o la maggiore delle due) è un parametro del listino
  (`dimension_source`): così si può far pagare il posto quando la barca è più piccola.
- **Unità temporali:** prezzo per notte, settimana, mese, stagione, anno. Un contratto annuale usa il prezzo annuale;
  un periodo arbitrario usa la regola di frazionamento del listino (`pro_rata_daily`, `whole_months`,
  `best_combination`: la combinazione più conveniente di mesi, settimane e notti, calcolata dal motore).
- Le aritmetiche sono intere: €/m in centesimi per metro, dimensioni in cm, risultato in centesimi con arrotondamento
  half-even (`internal/money`), e la precisione intermedia in centesimi × cm.

### 12.4 Motore prezzi

Funzione pura come in Arenile (ADR Arenile 0004): `pricing.Quote(input, catalog, rules) → PriceBreakdown`, senza
database né orologio. Passi per ogni riga:

1. **Prezzo base** per ogni notte (o unità del contratto): vince una regola per priorità, poi specificità, poi
   recenza; nessuna regola ⇒ `ErrNoPrice`.
2. **Modificatori** (`stackable` si sommano, non stackable vince il più favorevole al cliente): multiscafo +X %,
   barca oltre la lunghezza del posto (sbalzo), alta stagione, soggiorno lungo (−X % oltre N notti), ultimo minuto,
   prenotazione anticipata, canale.
3. **Servizi** inclusi o aggiunti (utenze forfettarie, posto auto, alaggio).
4. **Sconti** sul totale: convenzione o categoria del cliente → codice promozionale → automatici.
5. **Arrotondamento** del totale (al euro, ai 5 euro, nessuno).
6. **Acconto** e **piano rate** dalla policy del contratto (13.7).
7. **Scorporo IVA** per aliquota sulle righe.

L'output `PriceBreakdown` (righe, importi per notte, regole applicate, IVA per aliquota) è salvato come
`price_snapshot` con `engine_version` su preventivi e contratti; ricalcolare con la stessa versione restituisce lo
stesso totale (invariante INV-20).

### 12.5 Simulatore e matrice

- **Simulatore:** barca (o dimensioni), periodo, tipo di contratto, posto o categoria, cliente → breakdown completo
  con le regole che hanno vinto.
- **Matrice di listino:** fasce di lunghezza × periodi × tipo di contratto, calcolata dal motore (non scritta a
  mano), stampabile e pubblicabile sulla pagina pubblica.
- **Verifica di copertura:** combinazioni di categoria, periodo e tipo di contratto senza prezzo.

### 12.6 Rinnovo dei listini

"Nuovo listino da esistente": copia di regole e periodi sull'anno successivo con spostamento delle date, aumento
percentuale o fisso per articolo, categoria o tutto il listino, adeguamento ISTAT facoltativo (indice inserito dal
gestore), arrotondamento; anteprima delle differenze con la matrice prima e dopo; attivazione a una data.
