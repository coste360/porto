## 32. Test, qualità e roadmap

### 32.1 Test

Regole di Arenile (spec Arenile 25, AGENTS.md): `go test ./...` senza Docker né rete; integrazione con tag
`integration` su SQLite sempre e PostgreSQL se `PORTO_TEST_POSTGRES_URL`; concorrenza `TestConcurrency_*` con
almeno 100 tentativi paralleli sulla stessa risorsa e `-race`; tabelle `TestXxx_Scenario`; property test con
`pgregory.net/rapid`; Vitest e Testing Library; Playwright in `e2e/`; test di accettazione di sistema in
`internal/acceptance` con `clock.Fake`, solo tramite API, con verifica delle invarianti sul database.

Specifici di Porto:
- **Geometria:** property test di `layout.Footprint` (celle disgiunte ⇒ rettangoli disgiunti; ogni punto
  dell'ingombro sta in una cella occupata), di `ValidateMap` (sovrapposizioni) e dei generatori (numero di posti,
  nessuna sovrapposizione, codici unici).
- **Parità Go ↔ TypeScript:** casi JSON condivisi in `testdata/footprint/` eseguiti da entrambe le implementazioni
  di `Footprint`, di `fit` e delle silhouette.
- **Prezzi:** almeno 200 casi del motore (basi, frazionamenti, modificatori, arrotondamenti); la matrice coincide con
  il simulatore.
- **Esportazione:** il file "da fatturare" di un anno simulato contiene ogni addebito esattamente una volta, e i
  totali per aliquota coincidono con quelli degli snapshot di prezzo.
- **Rate, ripartizioni, competenze:** property test che le somme coincidano con i totali (INV-31, INV-42, INV-60).
- **Formule di cantiere:** fuzzing del parser, determinismo, nessun panico.
- **Ottimizzatore:** determinismo con lo stesso seme, nessuna collocazione invalida, tempo massimo rispettato.
- **Prestazioni della mappa:** test Playwright che misura il frame time del pan su una mappa di 3.000 elementi.

### 32.2 Agenti di sviluppo

Stessa organizzazione di Arenile (spec Arenile 27): `AGENTS.md`, `CLAUDE.md`, `.claude/` con regole per percorso
(`availability.md` diventa `occupancy.md`, più `geometry.md`, `charges.md`), skill (`/new-endpoint`,
`/new-migration`, `/new-price-rule-test`, `/new-domain-event`, `/new-map-element`, `/new-payment-driver`,
`/pre-pr-check`), subagent `reviewer`, `invariants-guardian`, `test-writer`, hook di formattazione e di protezione
dei file generati e delle migrazioni.

### 32.3 Roadmap e criteri di accettazione

**M0 — Fondamenta.** Repository, CI, Dockerfile, compose; primitive (`clock`, `money`, `calendar`, `crypto`) scritte da zero sul modello di Arenile;
`geo`, `qty`; autenticazione, ruoli (26.1), audit, outbox, job runner, SSE; supporto agli agenti; ADR 0001–0006.
*Accettazione:* `docker compose up` avvia un'istanza vuota, `porto admin create` permette il login, CI verde su
SQLite e PostgreSQL.

**M1 — Mappa del porto e anagrafiche.** Mappe multiple, editor completo (8), generatori, validazioni, calibrazione
dello sfondo e georeferenziazione, risorse a terra, colonnine, kit di illustrazioni; clienti, barche, documenti,
scadenze di base; importazione CSV dei posti. *Accettazione:* da un'ortofoto un gestore configura 6 pontili e 400 posti
in meno di 30 minuti; nessuna mappa salvata ha posti sovrapposti (property test e verifica nel test di accettazione).

**M2 — Listini e prezzi.** Articoli, classificazioni, listini per periodi, tutte le basi (12.3), motore puro,
simulatore, matrice, copertura, rinnovo dei listini. *Accettazione:* 200 casi di prezzo; matrice = simulatore.

**M3 — Occupazione, transiti e mappa operativa.** Motore di occupazione con i tre vincoli, `fit`, hold, ricerca e
assegnazione, preventivi con opzione, contratti di ormeggio base e transiti dal pannello, spostamenti con drag & drop
e pontile dinamico, rilasci, presenze, vista operativa con tutti i contesti disponibili a questa milestone, SSE.
*Accettazione:* 100 tentativi paralleli sullo stesso posto e sulle stesse celle non producono mai una doppia
occupazione; una stagione simulata di 2.000 operazioni (contratti, transiti, spostamenti, rilasci) non lascia
incoerenze tra allocazioni e righe di occupazione; 3.000 elementi fluidi sul tablet di riferimento.

**M4 — Contrattualistica completa.** Preventivo → contratto automatico, rate e scadenzario, rinnovi semplificati e
massivi, charter, diritti d'uso, cessioni, mandati, oneri, spese condominiali, modelli di stampa e renderer PDF,
firma (`paper`, `otp`, interfaccia per InfoCert), acquisizione documenti con IA, comunicazioni ai clienti, motore
delle scadenze. *Accettazione:* campagna di rinnovo di 300 contratti dalla proposta alla firma e all'acconto senza
interventi sul database; ripartizione delle spese su 400 posti con somma esatta.

**M5 — Addebiti e incassi.** Addebiti da tutte le origini, incassi e rimborsi con abbinamento, crediti, cassa e
corrispettivi (come Arenile), scadenzario, richieste di pagamento e solleciti, Stripe con SEPA, estratto conto,
esportazione "da fatturare" con riscontro delle fatture, interfaccia `ChargeExporter`, cruscotto del cliente con
sospesi. *Accettazione:* in un anno simulato di rate, transiti e servizi ogni addebito finisce nell'esportazione
esattamente una volta; saldi dei clienti, incassi e rimborsi coincidono; nessun abbinamento supera i limiti con 100
tentativi paralleli.

**M6 — Multipiattaforma.** Edizione desktop (locale, rete locale, remota con ponte), pannello per ormeggiatori sul telefono con
offline, pagina *Dispositivi*. *Accettazione:* come Arenile M6 (installazione senza terminale in meno di 10
minuti); dal telefono in rete locale l'ormeggiatore apre il pannello in meno di un minuto con il QR; operazioni
offline senza duplicati alla riconnessione.

**M7 — Report e cruscotto.** Tutti i report del 21.3, cruscotto con drill-down, confronti. *Accettazione:* ogni
indicatore del cruscotto coincide con la somma dei record del suo drill-down sui dati demo.

**M8 — Prenotazione online.** Pagina pubblica, widget, checkout con pagamento anticipato, area del cliente,
approvazione, interfaccia `BookingChannel`. *Accettazione:* prenotazione completa da smartphone dentro un iframe in
meno di 2 minuti; nessuna doppia vendita tra banco, online e canale in un test di carico parallelo.

**M9 — Rimessaggio e manutenzioni.** Contratti di rimessaggio, planner, ipotesi, accessibilità, ottimizzatore,
applicazione transazionale; piani e interventi di manutenzione; letture e addebito delle utenze. *Accettazione:* in un
capannone di 2.000 m² con 40 barche l'ottimizzatore produce in meno di 3 s una disposizione valida senza uscite
bloccate, e applicarla non lascia mai lo stato a metà.

**M10 — Cantiere.** Catalogo e formule, preventivo guidato, commesse, planner e carico, ore, materiali, consuntivo e
margini, mezzi di sollevamento. *Accettazione:* il preventivo di un'antivegetativa per una barca di 12 m è calcolato
senza inserire prezzi a mano e il consuntivo riconcilia ore, materiali e costi esterni.

**M11 — Noleggio.** Flotta, planner orario/mezza giornata/giorno, consegna e restituzione dal telefono, cauzioni,
vendita online. *Accettazione:* nessuna doppia prenotazione di un'unità con 100 tentativi paralleli; consegna e
restituzione da telefono in meno di 3 minuti ciascuna.

**M12 — Sensori e scheda pubblica.** Ingestione, stati con isteresi, riconciliazione e anomalie, contesto *Sensori*,
occupazione aggregata pubblica; scheda pubblica del porto. *Accettazione:* 1.000 sensori con
una lettura al minuto ciascuno su SQLite senza ritardi della vista operativa oltre 2 secondi.
