## 10. Motore di occupazione

### 10.1 Principio

Come in Arenile (spec Arenile 7, ADR Arenile 0003), l'assenza di doppie assegnazioni è garantita **dai vincoli del
database**, identici su SQLite e PostgreSQL:

| Famiglia | Tabella | Vincolo |
|---|---|---|
| Posto barca, boa | `berth_occupancies` | `unique(berth_id, night)` |
| Area dinamica | `cell_occupancies` | `unique(area_id, cell, night)` |
| Risorsa a terra | `land_occupancies` | `unique(resource_id, night)` |

Ogni riga ha `allocation_id` (il posizionamento a cui appartiene), `kind`, `expires_at` per gli hold. Un
**posizionamento** (`allocations`) lega una riga di contratto, una barca e una risorsa con il periodo e, per le aree
dinamiche, la posizione `x_cm` (e `y_cm`, `rotation` sulle superfici).

**Volumi.** Le righe sono limitate dalla dimensione del porto, non dal numero di barche: un posto produce al massimo
365 righe l'anno; un pontile dinamico di 300 m con celle da 50 cm al massimo 600 × 365 ≈ 219.000. Un porto di 800
posti e 1 km di aree dinamiche resta sotto i 2 milioni di righe l'anno; le notti passate sono archiviabili
(`occupancy_archive`, job annuale) senza perdere i report, che leggono dalle allocazioni.

### 10.2 Compatibilità (`fit`)

Funzione pura `fit.Check(vessel, target, period, rules) → Result{ok, blocking[], warnings[]}`:

- **Bloccanti:** `loa > max_loa + tolleranza`, `beam > max_beam`, `draft + franco > depth` (franco di sicurezza del
  marina, default 30 cm; sulle aree dinamiche il fondale minimo delle celle occupate), `air_draft > max_air_draft`,
  tipo di scafo escluso (catamarano su posto senza `multihull_ok`), invaso non supportato sulla superficie.
- **Avvisi:** rapporto di riempimento sotto la soglia dello spreco (21.3), fondale entro 10 cm dal limite, potenza
  della presa inferiore a quella richiesta dalla barca, assicurazione scaduta nel periodo.
- I bloccanti dimensionali sono superabili solo con il permesso `occupancy.override_fit`, una motivazione e audit
  (`fit_override`). La **sovrapposizione fisica non è mai superabile**: è un vincolo del database.

### 10.3 Occupare una risorsa

```
BEGIN
  DELETE FROM cell_occupancies
   WHERE kind = 'hold' AND expires_at < :now
     AND area_id = :area AND night BETWEEN :from AND :to;   -- pulizia locale (come Arenile 7.2)
  INSERT INTO allocations (...);
  INSERT INTO cell_occupancies (area_id, cell, night, allocation_id, kind, expires_at)
    VALUES (...per ogni cella dell'ingombro × ogni notte...)
    ON CONFLICT DO NOTHING;
  -- righe inserite < righe attese ⇒ rilettura dei conflitti ⇒ ConflictError ⇒ rollback
  audit_log + outbox occupancy.allocated
COMMIT
```

- Il piano (righe da inserire) è calcolato da una funzione pura (`occupancy/plan.go`) a partire dall'ingombro e
  dal periodo; l'applicazione partecipa alla transazione del chiamante (`bun.IDB`), come `inventory` di Arenile.
- Le scritture concorrenti su SQLite passano dalla coda FIFO del connettore (`sqlitegate.go`, copiato da Arenile).
- `ConflictError` riporta risorsa, notti, celle e allocazione in conflitto, così il pannello propone alternative.

### 10.4 Tipi di occupazione

| `kind` | Origine | Note |
|---|---|---|
| `hold` | checkout online, preventivo con opzione | scade (`PORTO_HOLD_TTL`, default 15 min online; validità del preventivo per le opzioni) |
| `contract` | contratto di ormeggio, rimessaggio, charter confermato | |
| `transit` | transito confermato | |
| `owner` | titolare di un diritto d'uso | per tutte le notti del diritto, salvo rilasci |
| `block` | blocco manuale | motivazione obbligatoria |
| `maintenance` | intervento di manutenzione programmato | 20.2 |

### 10.5 Spostamenti

`occupancy.Move(allocation, target, from_night)` in una transazione:
1. lock delle allocazioni coinvolte in ordine di id (come gli scambi di Arenile, INV Arenile 101);
2. cancellazione delle righe da `from_night` alla fine dell'allocazione originale e chiusura del periodo;
3. nuova allocazione sul target con le notti restanti (stesso contratto, stessa barca);
4. ricalcolo del prezzo solo se la policy del contratto lo prevede (`reprice_on_move`), altrimenti il prezzo
   resta e la differenza di categoria è registrata nell'audit;
5. audit, evento `occupancy.moved`, voce nel registro di annullamento (`move_undo_log`, ultimo spostamento
   annullabile per 24 ore).

Varianti: scambio di due barche, spostamento di un gruppo (charter), spostamento programmato a una data futura,
applicazione di un'ipotesi di rimessa (17.5). Tutte usano la stessa primitiva e falliscono interamente se un solo
posizionamento non è valido.

### 10.6 Rilasci e presenze

- **Occupazione** (contrattuale) e **presenza** (fisica) sono distinte. Una barca con contratto annuale fuori per
  crociera continua a occupare il posto finché non c'è un **rilascio**.
- Il rilascio indica le notti in cui il posto è rivendibile: le righe `contract` (o `owner`) di quelle notti
  vengono rimosse e nasce un `berth_release`. Fonti: richiesta del cliente (pannello, link magico, telefonata),
  ormeggiatore, oppure proposta dal sistema quando un sensore o una partenza registrata indicano un'assenza
  prolungata e il contratto lo consente (`release_policy`: `never`, `with_consent`, `automatic_after_days`).
- Se il posto rilasciato viene rivenduto, `resold_allocation_id` è valorizzato e, secondo il contratto, matura un
  credito o una quota per il conduttore o il titolare (mandato di gestione, 14.3).
- Il rientro anticipato con il posto già rivenduto non crea mai una doppia assegnazione: il sistema propone un
  posto alternativo o un'area dinamica per le notti in conflitto e lo segnala all'ufficio.

### 10.7 Ricerca e assegnazione automatica

`occupancy.Search(vessel, period, prefs, channel) → []Solution`, con il calcolo delle soluzioni in un package puro
(`occupancy/search.go`) a partire da un'istantanea letta con poche query (mai una per posto):

1. **Candidati:** posti attivi compatibili (`fit` senza bloccanti), aree dinamiche compatibili, filtrati per
   canale (`online_sellable`, quote online del gruppo, 23.4).
2. **Posti fissi:** liberi per tutte le notti.
3. **Aree lineari:** per ogni notte l'insieme degli intervalli liberi; l'intersezione sulle notti dà gli intervalli
   liberi per tutto il periodo; si propone la posizione *best fit* (lo spazio libero più piccolo che contiene la
   barca, accostata a una barca esistente per non frammentare).
4. **Punteggio** (minore è meglio): spreco di spazio (area del posto − area della barca), penalità per posti non
   dedicati ai transiti quando il canale è transito, distanza dalle preferenze, continuità con il soggiorno
   precedente della stessa barca.
5. Se nessuna soluzione copre tutto il periodo, soluzioni a **segmenti** con al massimo N cambi di posto
   (default 1, come "Trova posto" di Arenile, spec Arenile 30.6), solo se la policy del canale le consente.

L'assegnazione automatica di un transito online sceglie la prima soluzione e la occupa nella stessa transazione
dell'hold: **ogni hold occupa sempre una risorsa concreta**, quindi non esistono pool da riconciliare. L'ufficio può
spostare la barca in seguito (10.5).

### 10.8 Quote e chiusure

- **Quote online per gruppo:** `max_online_nights_percent` o `max_online_berths` per notte; calcolate come in
  Arenile 7.6 sui posti del gruppo.
- **Chiusure:** giorni o intervalli in cui un pontile, un'area o l'intero marina non accetta nuovi arrivi
  (lavori, eventi, regate), applicate da `Search` e dal controllo delle prenotazioni.
