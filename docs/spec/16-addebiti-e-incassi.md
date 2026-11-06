## 16. Addebiti, incassi ed esportazione per la fatturazione

### 16.0 Perimetro

Come in Arenile (spec Arenile 14 e 30, ADR Arenile 0028), **la fattura resta fuori da Porto**. Il gestionale calcola
quanto deve pagare ogni cliente, registra incassi e rimborsi, segue scadenze e solleciti ed esporta i dati completi
al software di fatturazione o al commercialista, che emette le fatture e le invia allo SdI.

Non fanno parte di Porto: emissione e numerazione delle fatture, note di credito, proforma, fatturazione elettronica
(FatturaPA, SdI), fatture passive, prima nota, riconciliazione bancaria, centri di costo, registri IVA.

### 16.1 Addebiti

Un **addebito** (`charges`) è un importo dovuto dal cliente. È il ponte verso la fattura esterna.
- **Origine:** rata di un contratto (13.7), transito, servizio al banco, consumi delle utenze (20.4), oneri e spese
  condominiali (14.4, 14.5), commessa di cantiere (18.6), noleggio (19.5), diritti di subentro (14.2).
- **Contenuto:** cliente intestatario (con i dati fiscali necessari alla fattura: codice fiscale, partita IVA, codice
  destinatario, PEC, indirizzo; 11.1), righe (articolo, descrizione, quantità `qty.Milli`, prezzo unitario, sconto,
  aliquota IVA o natura dell'esenzione, `revenue_class`), periodo di competenza (`service_from`, `service_to`, usato
  da 21.3), scadenza, totale con scorporo IVA per aliquota.
- **Calcolo:** le righe derivano dallo `price_snapshot` del contratto o del transito, così l'importo esportato è
  esattamente quello calcolato dal motore prezzi (12.4).
- **Stati di pagamento:** `open → partially_paid → paid`, oppure `cancelled` con motivazione. Il saldo è
  `paid_amount = Σ incassi − Σ rimborsi`, aggiornato nella stessa transazione del pagamento (INV-50).
- **Stato di fatturazione:** `to_invoice → exported → invoiced`. `invoiced` porta il riferimento della fattura
  esterna (numero e data), inserito a mano o importato da CSV (16.5). È solo un'informazione: non blocca nulla.
- Un addebito esportato non si modifica. Una correzione annulla l'addebito e ne crea uno nuovo, oppure crea un
  addebito negativo collegato; entrambi finiscono nella prossima esportazione con il collegamento all'originale
  (INV-51).

### 16.2 Incassi e rimborsi

- `PaymentProvider` come in Arenile: driver `manual` (contanti, carta al POS, bonifico, assegno), `stripe` (carta,
  Apple/Google Pay, SEPA Direct Debit per le rate, link di pagamento nelle richieste e nei solleciti), altri come
  estensioni. Gli incassi online arrivano dal webhook del gateway (ADR Arenile 0014).
- Un incasso si abbina a uno o più addebiti, anche in parte. Gli importi non abbinati restano come credito del
  cliente (crediti come Arenile M5). Le somme degli abbinamenti non superano mai l'incasso né il residuo
  dell'addebito (INV-52).
- I rimborsi passano dal provider, oppure sono registrati come manuali.
- **Cauzioni** (noleggio, chiavi, badge): un incasso di tipo `deposit` non è mai un ricavo finché non viene trattenuto.

### 16.3 Cassa e corrispettivi

Identici ad Arenile (spec Arenile 14.1–14.2):
- sessioni di cassa per postazione, con fondo, movimenti per metodo, chiusura con conteggio e differenza;
- documento commerciale per le vendite al banco tramite `ext.FiscalDriver` (`none`, `export`, driver per i
  registratori telematici come estensioni, ponte locale dell'edizione desktop);
- corrispettivi giornalieri per aliquota e metodo, esportabili.

La ricevuta di cortesia stampata (80/58 mm o A4) riporta "Non valido ai fini fiscali" come in Arenile.

### 16.4 Scadenzario, avvisi e solleciti

- **Scadenzario:** addebiti e rate per scadenza e stato (da pagare, scaduto per fasce d'età), per cliente, contratto
  e gruppo; esportazione CSV; azioni in blocco (invia richiesta di pagamento, sollecita).
- Il job giornaliero `payments.reminders` invia un avviso N giorni prima della scadenza, uno alla scadenza e i
  solleciti successivi su livelli configurabili (cortese, formale, diffida). Ogni livello ha il suo modello e il link
  di pagamento.
- Si possono escludere dai solleciti i clienti con un piano di rientro e gli addebiti contestati. Ogni invio è
  registrato. I solleciti si fermano appena il pagamento è registrato.
- **Policy di morosità:** oltre una soglia il contratto può passare in `suspended`, e il contesto *Pagamenti* della
  mappa lo evidenzia. L'uscita della barca può essere segnalata all'ormeggiatore, mai bloccata dal sistema.
- **Estratto conto** del cliente in PDF: addebiti, incassi, saldo.

### 16.5 Esportazione verso la fatturazione

- **Esportazione "da fatturare"** dal pannello: gli addebiti in `to_invoice` di un periodo, filtrabili per cliente,
  origine e classe di ricavo. Formati: Excel e CSV in italiano (una riga per riga di addebito con i dati fiscali del
  cliente, aliquota, natura, competenza, riferimento al contratto e alla barca) e PDF riepilogativo per il
  commercialista. Gli addebiti esportati passano in `exported`. Si può riesportare.
- **Riscontro:** importazione di un CSV con `id addebito ; numero fattura ; data` per marcare gli addebiti come
  `invoiced`.
- `ext.ChargeExporter` (27.5): estensione per inviare gli addebiti direttamente a un software di fatturazione esterno
  e ricevere numero e data della fattura emessa. Il core non ne contiene alcuno (sezione 33).
- Esportazione degli incassi per metodo e periodo (versamenti, incassi Stripe con le commissioni) per la
  riconciliazione fatta fuori da Porto.
