## 13. Contrattualistica

### 13.1 Modello

- `contracts`: `kind` ∈ `mooring` (ormeggio), `transit`, `storage` (rimessaggio a terra, sezione 17), `charter`;
  `term` per l'ormeggio ∈ `annual`, `seasonal`, `monthly`, `period`; cliente intestatario, barca (o flotta per i
  charter), periodo, condizioni (policy di cancellazione, rilascio, spostamento, rinnovo), listino applicato e
  `price_snapshot`, piano rate, documenti firmati, servizi inclusi (posto auto, utenze forfettarie).
- `contract_lines`: una riga per risorsa assegnata e servizio; le righe di risorsa hanno le allocazioni (10.1).
- Numerazione per marina, tipo e anno con vincolo `unique(marina_id, kind, year, number)`.
- Ogni cambio di stato passa da `internal/domain/contract`, unico punto che lo modifica, e scrive audit ed evento
  outbox nella stessa transazione (come `booking` di Arenile).

### 13.2 Prenotazioni e preventivi

- **Richieste** (`inquiries`): ogni domanda di posto (telefono, email, banco, pagina pubblica, canale) con barca o
  misure, periodo e tipo; esito (`quoted`, `booked`, `declined_no_space`, `declined_size`, `declined_other`,
  `lost`). Le ricerche online senza disponibilità sono registrate in forma anonima (misure, date). Alimentano il
  report della domanda persa (21.3).
- Il **preventivo** (`quotes`) nasce dalla ricerca delle disponibilità (9.6), dal cruscotto del cliente o dalla
  richiesta online (23.3): barca, periodo, soluzioni proposte (anche più alternative con prezzi diversi), validità,
  condizioni.
- **Opzione sul posto:** facoltativa, occupa il posto con `kind = hold` fino alla scadenza del preventivo
  (`option_expires_at`); un job la rilascia e porta il preventivo in `expired`.
- Stati: `draft → sent → accepted | declined | expired`; un preventivo accettato genera il contratto (13.3).
- Invio via email con link magico per l'accettazione online (scelta dell'alternativa, dati mancanti, documenti,
  firma e acconto) e PDF dal modello di stampa.

### 13.3 Procedura automatica preventivo → contratto

Su `quote.accepted` (dal pannello o dal link del cliente), in una transazione:
1. verifica che il posto sia ancora disponibile (converte l'opzione o occupa ora; conflitto ⇒ errore con
   alternative, nessun contratto parziale);
2. crea il contratto `draft` con dati, prezzo e condizioni del preventivo;
3. genera il piano rate dalla policy (13.7);
4. compone il documento dal modello (15.1) e, se il marina usa la firma elettronica, apre la richiesta di firma
   (15.4) e porta il contratto in `awaiting_signature`;
5. alla firma (o alla conferma manuale "firmato su carta") il contratto diventa `active`, le allocazioni diventano
   `contract`, nasce l'addebito della prima rata o dell'acconto con la richiesta di pagamento (16.1).

Stati del contratto:

```
draft ─▶ awaiting_signature ─▶ active ─▶ expired
  │              │               │  ├──▶ renewed (sostituito dal contratto dell'anno dopo)
  └──▶ cancelled ◀┘               │  └──▶ terminated (recesso, risoluzione, con data)
                                  └──▶ suspended (morosità, policy del marina: posto bloccato all'uscita)
```

### 13.4 Contratti di ormeggio

- Annuale (anno solare o da data a data), stagionale (periodo della stagione), mensile, a periodo.
- Posto assegnato o "posto in area" (area dinamica): il contratto garantisce lo spazio, la posizione può cambiare.
- **Addenda:** modifiche a un contratto attivo (servizio aggiunto, posto auto, cambio di intestatario degli
  addebiti, variazione di prezzo concordata) con data di efficacia; il contratto originale e le sue firme restano
  immutabili, l'addendum ha il suo documento, la sua firma e il suo effetto sulle rate future.
- Cambio di barca durante il contratto (nuova barca compatibile, prezzo ricalcolato se la base dipende dalla barca,
  con conguaglio pro rata su conferma).
- Recesso con data, calcolo del rimborso o della penale dalla policy, addebito di rettifica (negativo) proposto e rimborso o credito.

### 13.5 Transiti

- Notti dalla data di arrivo a quella di partenza (esclusa). Canali `desk`, `online`, `api`, canali esterni.
- Stati: `held → pending_payment → confirmed → arrived → departed`, con `cancelled`, `no_show`, `expired`,
  `awaiting_approval` (barche oltre una lunghezza o con richieste particolari).
- Allungamento e riduzione del soggiorno dalla scheda (verifica di disponibilità sulle notti aggiunte, ricalcolo,
  addebito integrativo o di rettifica).
- Il transito dal banco può essere registrato e incassato alla partenza in un solo passaggio (documento commerciale
  tramite `FiscalDriver` per i privati; per chi chiede fattura l'addebito finisce nell'esportazione, 16.5).

### 13.6 Contratti charter

- Intestati a una società di charter, coprono una **flotta** (`charter_fleet`: barche registrate della società)
  su un insieme di posti o su un'estensione di area dinamica, per una stagione.
- Giorno di cambio (es. sabato) e notti di "barca fuori" previste dal calendario del charter.
- Prezzo per posto, per barca o forfettario; servizi di turnaround (pulizia, rifornimento acqua) come articoli.
- Le barche della flotta ruotano sui posti del contratto senza nuovi contratti: lo spostamento tra posti dello
  stesso contratto non ricalcola il prezzo.

### 13.7 Rateazione e scadenzario

- **Policy di pagamento** del contratto: acconto (% o importo) alla firma, rate a date fisse o a intervalli,
  pagamento anticipato con sconto, addebito SEPA.
- Il piano rate (`installments`) è calcolato da una funzione pura: la somma delle rate è esattamente il totale; il
  resto dell'arrotondamento va sull'ultima rata (INV-31).
- Ogni rata ha data di emissione e data di scadenza. Alla data di emissione il job `installments.issue` crea
  l'addebito (16.1) e invia la richiesta di pagamento con il link, se configurato.
- **Scadenzario:** elenco delle rate per periodo, stato (futura, addebitata, pagata, parzialmente pagata,
  scaduta), cliente, contratto; esportazione CSV; azioni in blocco (addebita ora, sollecita); vedi anche 16.4.

### 13.8 Rinnovi

- **Rinnovo semplificato:** dal contratto attivo, "rinnova" crea il contratto dell'anno successivo con stesso posto,
  barca e servizi, prezzo dal listino nuovo (12.6) e piano rate della policy; l'allocazione dell'anno successivo
  è occupata come **prelazione** (`hold` fino alla scadenza della proposta).
- **Rinnovo massivo:** campagna su un filtro (gruppo, tipo di contratto, scadenza), con anteprima di prezzi vecchi e
  nuovi, esclusioni (morosi, assicurazione scaduta), invio delle proposte via email con link magico, accettazione,
  firma e acconto online. Stati della proposta e job di sincronizzazione come i rinnovi di Arenile (ADR Arenile
  0017): idempotenti, una transazione per proposta.
- Rinnovo tacito facoltativo (clausola del contratto): alla scadenza il job crea il nuovo contratto attivo e
  notifica il cliente.

### 13.9 Lista d'attesa

Richieste di posto annuale per dimensioni e gruppo, ordinate per data; alla liberazione di un posto compatibile
(evento di disdetta o recesso) il sistema propone il primo della lista all'ufficio, che invia il preventivo.
