## 23. Prenotazione online dei posti barca

### 23.1 Obiettivo

Il diportista prenota un transito in pochi passaggi con **pagamento anticipato** e ha la certezza dell'ormeggio: la
disponibilità mostrata è quella reale del gestionale, senza sincronizzazioni.

### 23.2 Superfici

- **Pagina pubblica** `/b/{slug}` (SPA `web/booking`, mobile first, installabile come PWA, budget 180 KB gzip di JS
  al primo caricamento), **widget** incorporabile con `embed.js` e iframe nel sito del marina (stesse regole di
  Arenile, spec Arenile 12: `frame-ancestors` per marina, tema, `return_to` solo verso origini autorizzate).
- **Scheda pubblica del porto** (sezione 25) con il pulsante "Prenota".
- **Canali esterni** (marketplace di ormeggi) tramite `ext.BookingChannel` (27.5): disponibilità e prezzi in uscita,
  prenotazioni in ingresso che passano dallo stesso servizio dei transiti e occupano risorse concrete.
- Un'app nativa per i diportisti non fa parte della prima versione (sezione 33): la PWA copre il caso d'uso.

### 23.3 Flusso

```
1 date di arrivo e partenza, barca (dimensioni, tipo, o barca salvata col link magico)
      GET /availability  → per categoria: disponibile sì/no, prezzo "da"; mai dati di altri clienti
2 preferenze (utenze, ormeggio), extra, condizioni
      POST /quote        → pricing.Quote (puro), nessuna scrittura
3 conferma               POST /bookings → transito held + allocazione concreta scelta da occupancy.Search (10.7)
                                          (una transazione: allocazione hold, audit, outbox)
4 dati del cliente e della barca, documenti (foto della licenza e della polizza; IA solo con consenso, 22.4)
5 pagamento              POST /bookings/{id}/pay → gateway (Stripe Checkout), hold esteso fino a 30 minuti
6 webhook del gateway    → transito confirmed, allocazione hold → transit, addebito pagato (in esportazione se chiede fattura)
7 email di conferma con istruzioni di arrivo: canale VHF, telefono dell'ormeggiatore, posto (se la policy lo
  rivela prima dell'arrivo), regolamento, QR del transito per l'ormeggiatore
```

- Conferma **solo** dal webhook del pagamento (o dal driver manuale), mai dal ritorno del browser (come Arenile).
- Pagamento tardivo dopo la scadenza dell'hold: registrato e rimborsato per intero, salvo che la risorsa sia ancora
  libera e la policy consenta il recupero.
- **Su richiesta:** barche oltre una lunghezza, o gruppi configurati, vanno in `awaiting_approval`; il pagamento è una
  preautorizzazione catturata all'approvazione.
- Il posto è sempre assegnato al momento dell'hold (10.7); l'ufficio può spostare la barca prima dell'arrivo senza
  effetti per il cliente se la categoria non cambia.

### 23.4 Configurazione

- Gruppi e posti vendibili online (`online_sellable`), quote per notte (10.8), soggiorno minimo e massimo,
  anticipo minimo e massimo, orari di arrivo, giorni di chiusura.
- Policy di cancellazione per fasce di anticipo (rimborso %, credito), acconto o pagamento totale.
- Campi obbligatori del diportista (documenti, assicurazione, numero di persone a bordo).
- **Area del cliente** con link magico: dettagli, modifica delle date (soggetta a disponibilità), cancellazione con
  rimborso secondo la policy, caricamento documenti, ricevute.
