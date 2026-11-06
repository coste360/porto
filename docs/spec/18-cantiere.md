## 18. Cantiere

### 18.1 Modello

- **Catalogo del cantiere:** articoli di tipo `service`, `labour` (manodopera con tariffa oraria per qualifica),
  `material` (con giacenza, 18.6) e **gruppi di lavorazioni** (`work_packages`): insiemi di righe con quantità
  calcolate da formule (es. "Antivegetativa" = alaggio + lavaggio + carteggiatura + due mani di antivegetativa +
  varo).
- **Preventivo di cantiere** (`yard_quotes`) → **commessa** (`work_orders`) → **attività** (`work_tasks`) →
  **consuntivo** → addebito.

### 18.2 Parametrizzazione del prezzo

Ogni riga del catalogo può avere una **formula di quantità** e una **formula di prezzo** espresse nel linguaggio
`formula` (package puro, ADR 0006):
- variabili della barca: `loa`, `beam`, `draft`, `air_draft` (metri, virgola fissa), `displacement` (t),
  `engines`, `kw_total`, `hull_type`, `material`, `age`; variabili derivate configurabili per marina, ad esempio
  `hull_area = loa * (beam + draft) * 0.85` (superficie dell'opera viva stimata);
- variabili della lavorazione inserite dall'operatore (`layers`, `damage_area`), con tipo, unità, minimo, massimo,
  valore predefinito;
- operatori aritmetici, confronti, `if(c, a, b)`, `min`, `max`, `round(x, n)`, `ceil`, `floor`, tabelle a fasce
  (`band(loa, tabella)`); niente cicli, niente accesso a dati esterni;
- valutazione in virgola fissa a 3 decimali, risultato del prezzo in centesimi con arrotondamento half-even; errori
  di valutazione (divisione per zero, variabile mancante) sono errori di configurazione mostrati al salvataggio
  della formula con un caso di prova.

### 18.3 Preventivo guidato

Procedura in passi: (1) barca e cliente; (2) scelta di gruppi di lavorazioni, servizi e prodotti dal catalogo con
ricerca e categorie; (3) variabili richieste dalle formule; (4) riepilogo con quantità e prezzi calcolati
automaticamente, modificabili riga per riga (le modifiche manuali sono marcate); (5) condizioni, validità, acconto.
Ogni riga di manodopera porta le **ore stimate** per qualifica, usate dal planner. Il preventivo si invia e si accetta
come quelli degli ormeggi (13.2) e diventa commessa.

### 18.4 Commesse e stato di avanzamento

- Commessa: barca, cliente, preventivo di origine, responsabile, date previste (inizio, consegna), posizione della
  barca (posto, piazzale, capannone, collegata all'occupazione), stato (`planned`, `in_progress`, `on_hold`,
  `completed`, `delivered`, `invoiced`, `cancelled`).
- Attività con qualifica richiesta, ore stimate, dipendenze semplici (finisci-per-iniziare), checklist, foto,
  percentuale di completamento; il completamento della commessa è la media delle attività pesata sulle ore stimate.
- Bacheca delle **barche in lavorazione**: colonne per stato, schede con barca, avanzamento, consegna prevista,
  ritardo, ore consumate su stimate.
- Lavori aggiuntivi in corso d'opera: variante della commessa con approvazione del cliente (link o firma).

### 18.5 Planner dei dipendenti

- **Dipendenti** (`employees`, distinti dagli utenti; un dipendente può avere un utente per l'app): qualifiche,
  costo orario interno, calendario di lavoro (orari per giorno della settimana), assenze (ferie, malattia,
  permessi), reparto.
- **Planner:** griglia dipendenti × giorni (vista giorno, settimana, mese) con le attività assegnate come blocchi di
  ore; trascinamento delle attività su dipendenti e giorni, con controllo di qualifica e capacità residua.
- **Carico di lavoro:** per dipendente, reparto e qualifica, ore assegnate su ore disponibili per giorno, settimana e
  mese; sovraccarichi evidenziati (> 100 %), scadenze di consegna a rischio (ore residue > capacità fino alla data).
- Proposta di pianificazione automatica: assegnazione greedy delle attività non pianificate per data di consegna,
  rispettando qualifiche e calendari (proposta da confermare).

### 18.6 Consuntivazione, costi e profitti

- **Ore lavorate** (`timesheets`): avvio e arresto dal telefono o inserimento manuale, per attività; approvazione
  del responsabile.
- **Materiali:** articoli `material` con giacenza per magazzino, movimenti (carico manuale o da CSV,
  scarico su commessa, rettifica); la giacenza non scende sotto zero salvo permesso (avviso).
- **Consuntivo della commessa:** ore × tariffa di vendita e ore × costo interno, materiali a prezzo di vendita e di
  costo, costi esterni (subfornitori, inseriti con importo e riferimento), confronto con il preventivo per riga, margine
  e margine %.
- Addebito al cliente a consuntivo, a preventivo (prezzo chiuso) o a stati di avanzamento (SAL), con acconti
  dedotti. Gli addebiti vanno nell'esportazione per la fattura (16.5).

### 18.7 Mezzi di sollevamento

Travel lift, gru e carrelli come risorse prenotabili a fasce di 15 minuti (`equipment_slots`,
`unique(equipment_id, date, slot)`), con operatore abilitato. Alaggi e vari si prenotano dalla commessa, dal
contratto di rimessaggio o dallo spostamento acqua ↔ terra (9.4); il calendario del giorno è visibile anche
dal telefono.
