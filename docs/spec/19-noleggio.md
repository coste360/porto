## 19. Noleggio

### 19.1 Flotta

- Barche della flotta di noleggio (`rental_units`): imbarcazione dell'anagrafica (11.2) di proprietà del marina o in
  gestione per conto terzi, categoria (gommone, open, cabinato, vela, pedalò, sup), posti, patente richiesta
  (sì/no con soglia configurabile), carburante, cauzione, stato (`available`, `maintenance`, `retired`), posto
  d'ormeggio di base.
- Skipper come risorsa prenotabile (`rental_skippers`) con calendario.

### 19.2 Formule e prezzi

- Formule: oraria, mezza giornata (mattina, pomeriggio, fasce configurabili), giornata, più giorni, settimana; con o
  senza skipper; carburante incluso o a consumo.
- Prezzi dal motore prezzi (12.4) con articoli di tipo noleggio, periodi tariffari e fasce orarie; extra (attrezzatura
  da snorkeling, ghiacciaia, ormeggio in rada).

### 19.3 Planner

- Griglia unità × tempo con cambio di visualizzazione **ore / mezze giornate / giorni** (componente planner condiviso);
  creazione di una prenotazione in pochi click trascinando sul planner; colori per stato.
- Occupazione: `rental_slots` con `unique(unit_id, date, slot)` dove lo slot è di 30 minuti; una mezza giornata
  occupa gli slot della sua fascia. Buffer configurabile tra due noleggi per pulizia e rifornimento. Stessa garanzia
  del motore di occupazione (sezione 10) e stesso schema di hold per la vendita online.
- Stati: `held → confirmed → checked_out → returned → closed`, con `cancelled` e `no_show`.

### 19.4 Consegna e restituzione

- **Consegna** (check-out), anche dal telefono o dal tablet: identità e patente (dati inseriti dall'operatore, foto del documento), firma del
  contratto di noleggio (15.4, firma su schermo o OTP), cauzione (preautorizzazione con il gateway o contanti),
  checklist della barca, livello del carburante, ore motore, foto.
- **Restituzione** (check-in): ora effettiva (ritardo addebitato secondo la formula), carburante e ore motore,
  checklist, danni con foto e stima, addebito degli extra, rilascio o trattenuta (totale o parziale) della cauzione.
- Verbale di consegna e restituzione in PDF dal modello.

### 19.5 Incasso rapido

Dalla scheda del noleggio, in un solo passaggio: incasso (contanti, carta, link di pagamento) e documento
commerciale tramite `FiscalDriver` (16.3). Se il cliente chiede la fattura, l'addebito finisce nell'esportazione
(16.5). La cauzione non è mai un ricavo finché non viene trattenuta.
