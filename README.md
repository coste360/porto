<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/brand/porto/outlined/porto_logo_horizontal_inverted_2color.svg">
  <img src="assets/brand/porto/outlined/porto_logo_horizontal_primary.svg" alt="Porto" width="320">
</picture>

# Porto

**Il gestionale gratuito per porti turistici, marine, approdi e cantieri nautici, con codice sorgente disponibile.**
Mappa del porto in scala reale, contratti di ormeggio e transiti, prenotazione online dei posti barca senza commissioni, addebiti e incassi, rimessaggio, cantiere e noleggio: un unico binario da installare sul tuo server, oppure un'app desktop che funziona anche senza internet.

[![Licenza: BSL 1.1](https://img.shields.io/badge/licenza-BSL%201.1-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-TypeScript-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io%2Fcoste360%2Fporto-2496ED?logo=docker&logoColor=white)](https://github.com/coste360/porto/pkgs/container/porto)
[![SQLite | PostgreSQL](https://img.shields.io/badge/DB-SQLite%20%7C%20PostgreSQL-003B57?logo=sqlite&logoColor=white)](#architettura)
[![CI](https://github.com/coste360/porto/actions/workflows/ci.yml/badge.svg)](https://github.com/coste360/porto/actions)
<!-- TODO(release): badge della versione e link alle note di rilascio quando esiste il primo tag. -->

[Perché Porto](#gestionale-per-porti-turistici-marine-e-cantieri-navali-gratuito-e-a-codice-sorgente-disponibile) ·
[Funzionalità](#funzionalità) ·
[Accessibilità](#accessibilità) ·
[Installazione](#installazione-in-5-minuti) ·
[Widget](#incorporare-il-widget-nel-tuo-sito) ·
[Architettura](#architettura) ·
[Sviluppo](#sviluppo) ·
[Roadmap](#roadmap) ·
[FAQ](#domande-frequenti)

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/harbour-map-dark-theme.png">
  <img src="docs/screenshots/harbour-map-piers.png" alt="Vista operativa di Porto: i pontili C, D ed E in scala reale, con i posti colorati per stato di occupazione, la legenda con i conteggi e la ricerca del posto per un'imbarcazione in arrivo." width="900">
</picture>

</div>

---

> [!IMPORTANT]
> **Stato del progetto: tutte le milestone della specifica, da M0 (fondamenta) a M12 (sensori e scheda pubblica del porto), sono implementate e verificate da test automatici (unitari, di integrazione su SQLite e PostgreSQL, di concorrenza, di accettazione, di accessibilità e nel browser). Il primo rilascio pubblico è in preparazione.**
> La [tabella di stato](#stato-delle-funzionalità) indica cosa è disponibile, con i limiti noti, e cosa è pianificato. Porto è fornito così com'è, senza garanzie (vedi [LICENSE](LICENSE)): per un'installazione verificata e con assistenza scrivi a [Coste360](https://www.coste360.it/contatti). Segui il repository (*Watch → Releases*) per essere avvisato dei rilasci.

**Porto** è un **software gratuito per la gestione di porti turistici, marine, approdi e cantieri nautici, con codice sorgente disponibile**. Copre l'intero ciclo di un marina: la mappa del porto (banchine, pontili, finger, campi boe, piazzali, capannoni, parcheggi e box), clienti e imbarcazioni, listini, contratti di ormeggio e transiti, diritti d'uso e spese condominiali, addebiti e incassi, rimessaggio, cantiere, noleggio, prenotazione online dei posti barca, sensori di occupazione e report.

Lo installi dove vuoi tu: sul PC dell'ufficio con l'**app desktop** per Windows, macOS e Linux, che lavora senza internet; su un mini PC in sede; oppure su un tuo server online, che serve anche la **pagina di prenotazione dei transiti** da incorporare nel sito del porto. Ormeggiatori, cantiere e noleggio usano il pannello dal telefono, anche quando la Wi-Fi di banchina cade. In ogni caso i dati restano tuoi e nessuna prenotazione paga una percentuale a qualcuno.

Porto **non emette fatture**: prepara gli addebiti e li esporta verso il software di fatturazione che usi già, tu o il tuo commercialista.

È sviluppato da [Coste360 SRL](https://www.coste360.it/), la piattaforma italiana per il monitoraggio delle coste e delle concessioni demaniali marittime.

---

## Gestionale per porti turistici, marine e cantieri navali gratuito e a codice sorgente disponibile

Perché scegliere Porto come software di gestione del porto turistico o del cantiere, al posto di un gestionale portuale a canone o di un portale che trattiene una commissione su ogni transito:

| Perché | In pratica |
|---|---|
| **Gratuito per chi gestisce il proprio porto** | Nessun canone, nessuna versione "Pro", nessun limite di posti barca, contratti o utenti per i porti, gli approdi, i cantieri e i rimessaggi che gestisci direttamente. Codice sorgente disponibile con licenza BSL 1.1, che diventa AGPL-3.0 dopo 3 anni. |
| **Zero commissioni sui transiti online** | Il diportista prenota e paga sul tuo sito. I pagamenti passano dal **tuo account Stripe** (carta, wallet, addebito SEPA) oppure da bonifico e cassa con il driver manuale: nessuna percentuale alla piattaforma. |
| **Funziona anche senza internet** | L'app desktop tiene archivio e server sul PC dell'ufficio; i telefoni degli ormeggiatori si collegano con un QR sulla Wi-Fi del porto. Gli arrivi, le partenze e gli incassi registrati in banchina senza rete vengono inviati al ritorno della connessione, senza doppioni. |
| **La mappa è in scala reale** | Ogni posto, pontile e capannone è misurato in centimetri e ogni barca occupa il suo ingombro. Due barche nello stesso spazio nella stessa notte sono impedite dai vincoli del database, non da un controllo che si può saltare. |
| **La fattura resta al tuo software** | Porto calcola rate, transiti, servizi, consumi e oneri e li esporta "da fatturare" in CSV, Excel o PDF. Niente doppioni: ogni addebito finisce nell'esportazione una volta sola e, una volta esportato, non cambia più. |
| **Nessuna intelligenza artificiale, nessuna telemetria** | Porto non usa servizi di IA ([ADR 0008](docs/adr/0008-nessuna-intelligenza-artificiale.md)) e nessun dato del porto va a Coste360: l'app desktop controlla solo se esiste una nuova versione, e si può disattivare. I dati dei documenti li inserisce l'operatore. |
| **I dati sono del gestore** | Clienti, contratti e incassi stanno nel tuo database. Esportazioni sempre disponibili, backup con un comando, ripristinabile sia su SQLite sia su PostgreSQL. |
| **Nessun servizio esterno obbligatorio** | Niente Redis, code esterne o object storage: SQLite incluso, PostgreSQL facoltativo. Email, pagamenti online, firma remota e sensori sono driver opzionali. |
| **Configurabile senza codice** | Mappe, categorie dei posti, listini, periodi, modelli di contratto e regole di prenotazione sono dati. Ogni porto è diverso, il software si adatta. |
| **Aperto ed estendibile** | API REST descritte in OpenAPI 3.1, webhook firmati e interfacce Go per pagamenti, esportazione verso la fatturazione, firma, canali di prenotazione e sensori. |

---

## Funzionalità

Panoramica delle aree funzionali previste dalla specifica. Lo stato di ciascuna è nella [tabella di stato](#stato-delle-funzionalità).

<table>
  <tr>
    <td width="40%"><img src="docs/screenshots/manager-dashboard.png" alt="Cruscotto del gestore: produzione, addebitato, incassato, sospesi, occupazione ed efficienza allocativa confrontati con l'anno precedente" width="100%"></td>
    <td width="40%"><img src="docs/screenshots/harbour-map-payments-lens.png" alt="Mappa del porto nel contesto Pagamenti: i posti colorati per stato dei pagamenti del cliente" width="100%"></td>
    <td width="20%"><img src="docs/screenshots/phone-online-booking.png" alt="Pagina pubblica di prenotazione da smartphone: scelta della categoria di posto barca con il prezzo per le date e le misure indicate" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><sub>Cruscotto con il confronto sull'anno precedente</sub></td>
    <td align="center"><sub>La stessa mappa nel contesto Pagamenti</sub></td>
    <td align="center"><sub>Prenotazione online da smartphone</sub></td>
  </tr>
  <tr>
    <td colspan="2"><img src="docs/screenshots/dry-storage-shed-planner.png" alt="Pianificazione di un capannone di rimessaggio in scala, con le barche trascinabili, la lente di accessibilità delle uscite e la cronologia sotto la pianta" width="100%"></td>
    <td><img src="docs/screenshots/phone-dock-today.png" alt="Pannello dell'ormeggiatore sul telefono: arrivi previsti di oggi con i pulsanti Arrivato e Incassa" width="100%"></td>
  </tr>
  <tr>
    <td colspan="2" align="center"><sub>Rimessaggio: pianta del capannone in scala e cronologia</sub></td>
    <td align="center"><sub>Ormeggiatore in banchina</sub></td>
  </tr>
</table>

### Mappa del porto in scala reale
- **Editor della mappa** con più mappe per porto (specchio acqueo, piazzale, capannone, parcheggi e box), sfondo calibrato su un'ortofoto o una planimetria e georeferenziazione.
- **Generatori**: pontile con posti su uno o due lati (anche a scalare verso la testata), finger ogni N posti o corpo morto, posti lungo una banchina, campo boe a griglia o a quinconce, griglie di posti auto e box, piazzali e capannoni a celle con varchi e corsie.
- Elementi di ogni tipo: posti barca, **aree dinamiche** (un lato di pontile o una banchina dove le barche si affiancano secondo la lunghezza), risorse a terra, colonnine con prese, decorazioni e servizi.
- Validazioni (sovrapposizioni, posti sotto la misura minima), versioni della mappa, importazione dei posti da CSV, gruppi e categorie di prezzo assegnati per posto o in blocco.

### Vista operativa, transiti e banchina
- **Vista operativa** della notte scelta, **in scala** o a **schema** (tessere leggibili anche senza ortofoto), con tema chiaro e scuro e stampa della mappa.
- **Contesti** sulla stessa mappa: occupazione, tipo di contratto, tipo di imbarcazione, presenze, pagamenti, scadenze, adeguatezza barca/posto, diritti d'uso, manutenzione, sensori, utenze, anomalie.
- Ricerca del posto per misure dell'imbarcazione (lunghezza, larghezza, pescaggio, tipo di scafo) su posti fissi e aree dinamiche, con verifica di compatibilità e prezzo dal listino.
- **Pontile dinamico**: posizionamento con trascinamento, spostamenti "da quando", scambi, compattazione e annullamento entro 24 ore.
- Transiti, preventivi con fino a tre opzioni e posti tenuti in opzione, calendario dei posti, rilasci con rivendita, blocchi e chiusure, arrivi, partenze e presenze.
- Aggiornamento in tempo reale tra ufficio, banchina e prenotazioni online (Server-Sent Events).

### Clienti e imbarcazioni
- Anagrafica di clienti e barche con proprietari e altri soggetti, documenti con file allegati (identità, licenza, polizza), scadenze e consensi versionati.
- Documenti e IBAN cifrati a riposo; i file si scaricano solo con link firmati a breve scadenza.
- Unione dei doppioni, esportazione dei dati del cliente e anonimizzazione su richiesta.

### Listini e motore dei prezzi
- **Motore dei prezzi** come funzione pura: periodi tariffari, fasce di lunghezza, sette basi di calcolo, frazionamenti (esatto, pro rata, mesi interi, miglior combinazione), maggiorazioni, servizi inclusi e aggiuntivi, sconti, IVA, arrotondamenti e acconti.
- Listini versionati: un listino attivato non si modifica più e ogni prezzo resta ricalcolabile dal suo snapshot.
- **Simulatore**, **matrice** di listino (anche da stampare), verifica di copertura e rinnovo del listino con il confronto delle differenze.

### Contratti di ormeggio, rinnovi e firma
- Contratti annuali, stagionali, mensili e di periodo, transiti, contratti di rimessaggio e charter con flotte a rotazione.
- **Dal preventivo al contratto**: invio per email con PDF e link, accettazione online del cliente, firma, attivazione.
- Rate e **scadenzario**, emissione delle rate, rinnovi singoli e **campagne di rinnovo** massive, addendum, cambio di imbarcazione, lista d'attesa, importazione da CSV.
- **Modelli di stampa** in Markdown con campi di unione e PDF generato in Go; **firma** su carta (con scansione), con OTP o su schermo; un'interfaccia per i provider di firma avanzata.

### Diritti d'uso, cessioni, oneri e spese condominiali
- Diritti d'uso dei posti con titolari e quote, cessioni firmate in sequenza, **mandati di gestione** con i ricavi maturati dal titolare.
- Oneri di gestione, tabelle millesimali, bilanci e **riparti delle spese condominiali** in acconto e saldo con somma esatta al centesimo, rate dei titolari.
- Dati della concessione demaniale del porto.

### Addebiti, incassi e cassa
- **Addebiti** da ogni origine (rate, transiti, servizi, consumi, commesse, noleggi, oneri), rettifiche collegate all'addebito originale, crediti del cliente.
- **Esportazione "da fatturare"** in CSV, Excel e PDF e riscontro delle fatture emesse fuori da Porto (importazione CSV o Excel); interfaccia `ChargeExporter` per i formati dei singoli software di fatturazione.
- **Incassi** con abbinamento agli addebiti, rimborsi, cauzioni, mandati SEPA, estratto conto, scadenzario dei crediti e anzianità, solleciti a livelli con approvazione dell'ultimo avviso.
- **Richieste di pagamento** con link per il cliente; driver **manuale** sempre disponibile e driver **Stripe** facoltativo con le chiavi del gestore (carta, wallet, addebito SEPA), confermato solo da webhook firmati.
- **Cassa** con apertura, chiusura e riepilogo Z, vendite al banco, ricevute di cortesia e corrispettivi. Vedi la [nota fiscale](#nota-fiscale).

### Prenotazione online dei posti barca
- Pagina pubblica `https://tuo-dominio/b/{slug}`: il diportista indica date e misure della barca, sceglie la categoria di posto, vede il prezzo del listino e paga l'anticipo. Nessun account richiesto.
- **Nessuna doppia vendita** tra banco, pagina online e canali, garantita dal database; contingenti di posti vendibili online per gruppo.
- Il transito si conferma solo dal webhook del pagamento o dal driver manuale; approvazione facoltativa dal banco con scadenza; un pagamento arrivato tardi recupera lo stesso posto oppure viene rimborsato per intero.
- **Area del cliente** con link magico via email: prenotazioni, documenti, ricevute, saldo, cambio date e cancellazione secondo la policy con l'anteprima del rimborso.
- Widget da incorporare nel sito del porto: vedi [Incorporare il widget](#incorporare-il-widget-nel-tuo-sito). Interfaccia `BookingChannel` per collegare portali di terzi (nessun canale di terzi è incluso).

### Rimessaggio e manutenzioni
- Contratti di rimessaggio al coperto e all'aperto, invasi e supporti, date di alaggio e varo.
- **Planner del capannone e del piazzale** in scala con cronologia, trascinamento e rotazione anche da tastiera, **ipotesi** separate dallo stato reale.
- **Ottimizzatore** che dispone le barche controllando che ognuna possa uscire dal varco nel giorno previsto; applicazione tutto o niente.
- Manutenzioni programmate (per mesi, cicli o data), interventi con esito, costi, foto e documenti, risorse fuori servizio.
- **Utenze**: contatori su colonnine e risorse, letture dal telefono anche offline, consumi ripartiti sulle notti degli occupanti reali e addebitati una volta sola.

### Cantiere
- Catalogo dei lavori con **formule** (per esempio la superficie dello scafo dalle misure della barca), pacchetti di lavoro, qualifiche e tariffe dai listini.
- **Preventivo guidato** senza prezzi scritti a mano, accettazione dal pannello o dal link del cliente; la commessa nasce dall'accettazione con attività e acconto.
- Ore lavorate con timer anche dal telefono e offline, approvazione, materiali, costi esterni con ricarico, **consuntivo** con ricavi, costi e margine.
- Planner di cantiere e carico di lavoro per dipendente, reparto e qualifica; **mezzi di sollevamento** (travel lift, gru, muletto, carrello) prenotati a slot senza sovrapposizioni.

### Noleggio
- Flotta collegata alle imbarcazioni, skipper, formule orarie, a mezza giornata, giornaliere, multi-giorno e settimanali, extra.
- Planner per unità e orario, prenotazioni in opzione e confermate, **nessuna doppia prenotazione** di un'unità o di uno skipper per vincolo del database.
- **Consegna e riconsegna dal telefono**: documento e patente, contratto firmato sullo schermo, cauzione in contanti, POS o preautorizzazione, checklist, carburante, danni con foto; incasso rapido.

### Sensori di occupazione e scheda pubblica del porto
- Ingestione delle letture di sensori e gateway con chiavi per dispositivo e firma HMAC, protocollo aperto per i produttori in [docs/sensor-protocol.md](docs/sensor-protocol.md).
- Stato fisico dei posti con isteresi, **riconciliazione** con l'occupazione attesa e avvisi; i sensori **propongono**, non cambiano mai contratti o occupazioni senza la conferma di un operatore.
- **Scheda pubblica del porto** su `/p/{slug}`: posizione, contatti, canale VHF, servizi, fondali, foto, mappa semplificata, matrice dei prezzi dei transiti e posti liberi aggregati per categoria, senza dati personali.

### Report e cruscotto
- Report di occupazione (fisica e contrattuale), efficienza allocativa, domanda persa, clienti e rinnovi, produzione per competenza, ratei e risconti, addebiti, incassi, crediti, cassa, rimessaggio, manutenzioni, utenze, cantiere, noleggio, sensori.
- **Cruscotto** del gestore: ogni riquadro apre il report che lo compone, con gli stessi filtri, e il suo valore coincide con la somma di quei record.
- Confronto fino a 5 anni, esportazione di ogni report in CSV e Excel.

### Più marine, utenti e ruoli
- **Più marine nella stessa installazione** (organizzazioni con più porti), con l'isolamento dei dati verificato da test automatici.
- Ruoli: titolare, direzione, ufficio, amministrazione, ormeggiatore, capo cantiere, operaio, addetto al noleggio, sola lettura.
- Password Argon2id, autenticazione a due fattori TOTP, chiavi API con scope, dispositivi abbinati con token revocabili.
- Lingue: italiano e inglese.

### Notifiche e comunicazioni
- Email tramite il tuo server SMTP (nessun provider imposto): preventivi, conferme, richieste di firma e di pagamento, ricevute, scadenze, solleciti, rinnovi.
- **Comunicazioni ai clienti** filtrate per gruppo, pontile, tipo di contratto o selezione sulla mappa, nel rispetto dei consensi, con disiscrizione in un clic e registro degli invii.
- Interfaccia `Notifier` per canali aggiuntivi (SMS, WhatsApp) come estensioni.

### API e webhook
- **API REST** `/api/admin/v1` e `/api/public/v1` descritte in `api/openapi.yaml` (OpenAPI 3.1), errori RFC 9457, `Idempotency-Key`, `If-Match` sulle risorse versionate, paginazione a cursore.
- **Webhook in uscita** firmati HMAC-SHA256 (`Porto-Signature`), con tentativi, registro e reinvio.
- Punti di estensione Go in `pkg/ext`: pagamenti, esportazione verso la fatturazione, driver fiscali, firma, canali di prenotazione, sensori, notifiche, autenticazione SSO.

### Desktop, telefono e offline
- **App desktop** per Windows, macOS e Linux: server e archivio sul PC dell'ufficio, nessun servizio Coste360 necessario, backup giornalieri e alla chiusura, ripristino dal pannello.
- **Rete locale**: gli altri PC e i telefoni sulla Wi-Fi del porto usano lo stesso archivio con TLS generato in locale e abbinamento tramite QR.
- **Modalità remota**: l'app come client di un Porto online, con modalità chiosco per il banco e ponte verso le periferiche locali (stampante di ricevute ESC/POS, stampante di etichette ZPL, lettore QR).
- **Pannello dal telefono** installabile come PWA, con schermate per banchina, direzione, cantiere, rimessaggio e noleggio e una **coda offline** per le operazioni ammesse (arrivi e partenze, foto, letture, ore, incassi), reinviate senza doppioni.

### Privacy e sicurezza
- **GDPR**: dati minimi, consensi versionati, esportazione e anonimizzazione del cliente, conservazione configurabile dei documenti.
- Segreti cifrati con AES-GCM, header di sicurezza e `frame-ancestors` per marina, limiti delle richieste, client di uscita anti-SSRF, audit di ogni scrittura, nessuna email, telefono o documento in chiaro nei log.

---

## Accessibilità

La prenotazione online di un posto barca deve funzionare per tutti i diportisti. Porto punta al livello **AA delle WCAG 2.1** (norma europea EN 301 549) sulla pagina pubblica, sul widget e sull'area del cliente, e applica gli stessi controlli alle pagine principali del pannello.

**Cosa è supportato**
- **Mappa leggibile anche senza vista**: ogni posto è un elemento con ruolo di pulsante e un nome completo (codice, stato, barca); il pulsante **Elenco** mostra gli stessi dati come lista.
- **Il colore non è l'unico segnale**: sulla mappa ogni stato ha anche un glifo (stella per l'annuale, valigia per il transito, orologio per l'opzione, chiave per la manutenzione) spiegato nella legenda.
- **Contrasto** verificato da test automatici sui colori del tema chiaro e scuro e su quelli dei contesti della mappa.
- **Tema chiaro e scuro** in tutto il pannello.
- **Due lingue**: italiano e inglese per pannello, pagina pubblica ed email.

**Come è verificato**: i test end-to-end eseguono axe-core con le regole WCAG 2.0 e 2.1 di livello A e AA, in tema chiaro e scuro, sulla pagina di prenotazione, sull'accesso all'area del cliente, sul login, sul cruscotto e sui report, sulle prenotazioni online del banco e sulle pagine di rimessaggio, manutenzioni, utenze e noleggio; una violazione grave o critica fa fallire i test.

**European Accessibility Act**: dal 28 giugno 2025 la direttiva UE 2019/882 (in Italia D.Lgs. 82/2022) chiede che i servizi di prenotazione online per i consumatori siano accessibili e che il fornitore lo dichiari; le microimprese sono esentate. Porto fornisce lo strumento, ma la responsabilità del servizio resta del porto.

**Limiti noti**: la pagina di pagamento è del provider (Stripe); testi, foto e il sito che ospita il widget dipendono dal porto; l'editor della mappa è pensato per mouse o touch; non è ancora stato fatto un audit con utenti di tecnologie assistive.

---

## Stato delle funzionalità

Legenda: **Disponibile** = implementato e verificato da test automatici · **Interfaccia** = punto di estensione pronto, nessun driver incluso · **Pianificato** = previsto, non ancora iniziato.

Tutte le milestone da M0 a M12 sono implementate: fondamenta, mappa e anagrafiche, listini, occupazione e transiti, contrattualistica, addebiti e incassi, edizione desktop e telefono con offline, report e cruscotto, prenotazione online, rimessaggio e manutenzioni, cantiere, noleggio, sensori e scheda pubblica. Il primo rilascio pubblico è in preparazione: fino ad allora il riferimento è il ramo `main` e il [CHANGELOG](CHANGELOG.md). Limiti noti: i pacchetti dell'app desktop si compilano ma la firma e la notarizzazione richiedono i certificati di Coste360; nessun software di fatturazione specifico, nessun provider di firma avanzata, nessun canale di prenotazione di terzi e nessun modello reale di registratore telematico è incluso (sono interfacce); il magazzino del cantiere si limita ai materiali scaricati sulle commesse; il noleggio non si vende ancora dalla pagina pubblica; non c'è ancora un partner hardware di riferimento per i sensori.

| Area | Funzionalità | Milestone | Stato |
|---|---|:-:|---|
| Progetto | Specifica tecnica completa, invarianti, ADR, licenza | M0 | Disponibile |
| Fondamenta | Struttura del repository, Makefile, CI, Dockerfile, docker compose, devcontainer | M0 | Disponibile |
| Fondamenta | Configurazione da variabili d'ambiente, CLI (`serve`, `migrate`, `admin create`, `seed demo`, `backup`, `restore`, `version`) | M0 | Disponibile |
| Fondamenta | Migrazioni SQLite e PostgreSQL, autenticazione, ruoli, audit log, outbox, job runner, SSE, webhook firmati | M0 | Disponibile |
| Agenti AI | `AGENTS.md`, `CLAUDE.md`, `.claude/` (regole, skill, subagent, hook) | M0 | Disponibile |
| Mappa | Editor, generatori, validazioni, sfondo calibrato e georeferenziazione, risorse a terra, colonnine, kit di illustrazioni, importazione CSV dei posti | M1 | Disponibile |
| Anagrafiche | Clienti, barche, documenti cifrati, scadenze, consensi, unione, esportazione e anonimizzazione | M1 | Disponibile |
| Prezzi | Articoli, categorie, listini per periodi, motore puro, simulatore, matrice, copertura, rinnovo | M2 | Disponibile |
| Occupazione | Motore di occupazione con vincoli del database, verifica di compatibilità, ricerca, opzioni, spostamenti, scambi, compattazione, rilasci, chiusure | M3 | Disponibile |
| Operatività | Vista operativa in scala e a schema con i contesti, transiti, preventivi, presenze, calendario dei posti | M3 | Disponibile |
| Contratti | Preventivo → contratto, rate e scadenzario, rinnovi e campagne, charter, addendum, lista d'attesa | M4 | Disponibile |
| Diritti d'uso | Titolari, cessioni, mandati, oneri, tabelle millesimali, spese condominiali, concessione | M4 | Disponibile |
| Documenti | Modelli di stampa, PDF, firma su carta, OTP e su schermo, comunicazioni, motore delle scadenze | M4 | Disponibile (firma avanzata o qualificata: Interfaccia) |
| Addebiti | Addebiti da ogni origine, rettifiche, crediti, esportazione "da fatturare" CSV/Excel/PDF, riscontro delle fatture | M5 | Disponibile (formato del singolo software di fatturazione: Interfaccia) |
| Incassi | Incassi e abbinamenti, rimborsi, cauzioni, estratto conto, solleciti, richieste di pagamento, mandati SEPA, driver Stripe | M5 | Disponibile |
| Cassa | Cassa con riepilogo Z, vendite al banco, ricevute di cortesia, corrispettivi | M5 | Disponibile |
| Desktop | App per Windows, macOS e Linux, backup e ripristino, rete locale con abbinamento tramite QR | M6 | Disponibile (pacchetti non ancora firmati) |
| Telefono e offline | Pannello PWA, schermate per ruolo, coda offline senza doppioni, modalità remota, chiosco, ponte verso le periferiche | M6 | Disponibile (registratori telematici reali: Interfaccia) |
| Report | Report completi, cruscotto con drill-down, confronto tra anni, esportazioni CSV ed Excel | M7 | Disponibile |
| Prenotazione online | Pagina pubblica, widget, pagamento anticipato, approvazione, area del cliente, contingenti | M8 | Disponibile |
| Canali | Collegamento a portali di prenotazione di terzi | M8 | Interfaccia |
| Rimessaggio | Contratti, planner, ipotesi, ottimizzatore, accessibilità delle uscite | M9 | Disponibile |
| Manutenzioni e utenze | Piani, interventi, fuori servizio, contatori, letture, consumi addebitati | M9 | Disponibile |
| Cantiere | Catalogo e formule, preventivo guidato, commesse, ore, materiali, consuntivo, planner, mezzi di sollevamento | M10 | Disponibile (magazzino completo: Pianificato) |
| Noleggio | Flotta, skipper, formule, planner, consegna e riconsegna dal telefono, cauzioni | M11 | Disponibile (vendita online del noleggio: Pianificato) |
| Sensori | Ingestione, isteresi, riconciliazione, avvisi, contesto *Sensori* sulla mappa | M12 | Disponibile (partner hardware di riferimento: da definire) |
| Scheda pubblica | Profilo del porto con mappa semplificata, prezzi dei transiti e posti liberi aggregati | M12 | Disponibile |
| Accessibilità | Controlli axe WCAG 2.1 AA in tema chiaro e scuro, contrasto, alternativa a elenco della mappa | — | Disponibile |
| Dopo la prima versione | Firma InfoCert, esportatori per i software di fatturazione; da valutare: comunicazioni obbligatorie, mercati esteri, ormeggio in seconda fila, app per diportisti | — | Pianificato |

---

## Installazione in 5 minuti

**Dove si scarica.** Su GitHub trovi il codice sorgente e le istruzioni per compilarlo ed eseguirlo: i binari pronti **non** vengono pubblicati qui. L'immagine Docker `ghcr.io/coste360/porto` sarà pubblica e gratuita a partire dal primo rilascio; fino ad allora si compila dai sorgenti con un comando (vedi sotto). I pacchetti pronti dell'app desktop per Windows, macOS e Linux saranno distribuiti da Coste360 *(indirizzo di download in arrivo)*. <!-- TODO(release): link alla pagina di download dell'app desktop e conferma della pubblicazione su GHCR. -->

**Quale installazione?**

| Dove gira il server | Internet | Prenotazione online, firma remota |
|---|---|---|
| **App desktop** sul PC dell'ufficio | non necessaria | no (l'esportazione per la fatturazione funziona) |
| **Server in sede** (mini PC o NAS nel marina) | facoltativa | se raggiungibile da internet |
| **Server online** (VPS con Docker) | necessaria | sì |

Si passa da un modo all'altro con `porto backup` e `porto restore`. Per l'app desktop (compilazione, pacchetti, rete locale, modalità remota) vedi [docs/desktop-build.md](docs/desktop-build.md); qui sotto c'è l'installazione con Docker.

**Requisiti:** un server Linux con Docker e Docker Compose e, per la prenotazione online, un dominio che punti al server.

**Prova in locale** (compila l'immagine dai sorgenti, nessuna configurazione):

```bash
git clone https://github.com/coste360/porto.git && cd porto
PORTO_BOOTSTRAP_OWNER_EMAIL=titolare@porto.test PORTO_BOOTSTRAP_OWNER_NAME="Mario Rossi" \
PORTO_BOOTSTRAP_OWNER_ORG="Marina Prova SRL" PORTO_ADMIN_PASSWORD=password-di-prova \
  docker compose up --build        # oppure: make up
```

Poi apri `http://localhost:8080/admin/` e accedi con le credenziali indicate (la password deve avere almeno 12 caratteri); le email inviate si leggono in Mailpit su `http://localhost:8025`. Per caricare il porto dimostrativo, con un utente per ogni ruolo: `docker compose exec porto /porto seed demo`. Il file `compose.yaml` nella radice usa la modalità sviluppo: non usarlo in produzione.

**Installazione su un server**

**1. Prepara la configurazione** partendo da [`.env.example`](.env.example) e dal compose di produzione [`deploy/docker-compose.yml`](deploy/docker-compose.yml) (SQLite) o [`deploy/docker-compose.postgres.yml`](deploy/docker-compose.postgres.yml) (PostgreSQL 17):

```bash
git clone https://github.com/coste360/porto.git && cd porto
cat > deploy/.env <<FINE
PORTO_BASE_URL=https://porto.mio-marina.it
PORTO_SECRET_KEY=$(openssl rand -base64 32)
PORTO_SMTP_URL=
PORTO_BOOTSTRAP_OWNER_EMAIL=titolare@mio-marina.it
PORTO_BOOTSTRAP_OWNER_NAME=Mario Rossi
PORTO_BOOTSTRAP_OWNER_ORG=Marina Esempio SRL
PORTO_ADMIN_PASSWORD=scegli-una-password-lunga
FINE
```

`PORTO_SMTP_URL` è facoltativo (`smtp://utente:password@host:587`). Le altre variabili sono descritte in [Configurazione](#configurazione).

> [!WARNING]
> Conserva `PORTO_SECRET_KEY` insieme ai backup: cifra le chiavi dei provider, i documenti dei clienti e gli altri segreti. Senza di essa i dati cifrati non sono recuperabili.

**2. Immagine.** Quando l'immagine è pubblicata il compose la scarica da sola. Finché non lo è, compilala sul server (serve solo Docker) e indica il tag in `deploy/.env`:

```bash
docker build -f deploy/Dockerfile -t ghcr.io/coste360/porto:dev .     # oppure: make docker
echo "PORTO_VERSION=dev" >> deploy/.env
```

**3. Avvia**

```bash
docker compose -f deploy/docker-compose.yml up -d
```

**4. Primo utente (titolare).** Con le variabili `PORTO_BOOTSTRAP_OWNER_*` del passo 1 il titolare e la sua organizzazione vengono creati al primo avvio, solo se il database non ha ancora utenti; dopo il primo accesso puoi togliere `PORTO_ADMIN_PASSWORD` da `deploy/.env`. In alternativa crealo a mano, anche con il primo marina: l'immagine è distroless e non ha una shell, ma `exec` lancia direttamente il binario.

```bash
docker compose -f deploy/docker-compose.yml exec -e PORTO_ADMIN_PASSWORD='scegli-una-password-lunga' porto \
  /porto admin create --email titolare@mio-marina.it --name "Mario Rossi" --org "Marina Esempio SRL" \
  --marina "Marina Esempio" --slug marina-esempio
```

**5. Accedi al pannello** su `https://porto.mio-marina.it/admin/`: crea il marina dalla pagina dell'organizzazione (se non l'hai creato al passo 4), disegna la mappa con i generatori o importa i posti da CSV, prepara il listino e, se vuoi la prenotazione online, i testi, le regole e i pagamenti nelle impostazioni.

Per HTTPS puoi mettere Porto dietro un reverse proxy (Caddy, Traefik, Nginx) oppure far gestire i certificati Let's Encrypt al binario con `PORTO_TLS_AUTOCERT_DOMAINS`: in questo caso Porto ascolta sulle porte 443 e 80, che vanno pubblicate nel compose al posto della 8080.

### Comandi del binario

```text
porto serve                     # avvia il server (migrazioni automatiche se PORTO_AUTO_MIGRATE=true)
porto migrate up|status [--check]
                                # applica o mostra le migrazioni (--check: esce con 1 se ce ne sono in sospeso)
porto admin create --email E --name N --org ORG [--marina NOME --slug SLUG --timezone TZ --locale it|en]
                                # crea il titolare (password da PORTO_ADMIN_PASSWORD, --password-stdin o richiesta)
porto seed demo [--password-stdin]
                                # carica un porto dimostrativo completo, con un utente per ruolo
porto backup --out FILE         # backup portabile: tabelle in JSONL e file cifrati, uguale per SQLite e PostgreSQL
porto restore --in FILE [--force]
                                # ripristino da backup (--force svuota prima il database)
porto version                   # versione del binario
```

### Aggiornamento

```bash
docker compose -f deploy/docker-compose.yml exec porto /porto backup --out /data/prima-aggiornamento.porto-backup
docker compose -f deploy/docker-compose.yml pull && docker compose -f deploy/docker-compose.yml up -d
```

Le migrazioni vengono applicate all'avvio; con `PORTO_AUTO_MIGRATE=false` il server non parte finché non esegui `porto migrate up`. Leggi il [CHANGELOG](CHANGELOG.md) prima di ogni aggiornamento. L'app desktop controlla una volta al giorno se c'è una nuova versione e lo segnala, senza mai scaricare o installare nulla (disattivabile con `PORTO_UPDATE_CHECK=false`).

---

## Configurazione

Tutta la configurazione del server avviene tramite variabili d'ambiente, documentate anche in [`.env.example`](.env.example).

| Variabile | Default | Descrizione |
|---|---|---|
| `PORTO_ADDR` | `:8080` | Indirizzo di ascolto |
| `PORTO_BASE_URL` | — | URL pubblico usato nei link e nei cookie; `https` obbligatorio fuori da `localhost` e dalla modalità sviluppo |
| `PORTO_DATA_DIR` | `./data` (`/data` nell'immagine) | Database SQLite `porto.db`, file cifrati, certificati, backup |
| `DATABASE_URL` | vuoto | Se valorizzato con `postgres://` o `postgresql://`, usa PostgreSQL al posto di SQLite |
| `PORTO_SECRET_KEY` | — | Chiave di cifratura (32 byte in base64). Più chiavi separate da virgola per la rotazione: la prima è attiva. In modalità sviluppo, se assente, viene generata in `PORTO_DATA_DIR/secret.key` |
| `PORTO_AUTO_MIGRATE` | `true` | Applica le migrazioni all'avvio di `serve`; con `false` il server non parte con migrazioni in sospeso |
| `PORTO_TLS_AUTOCERT_DOMAINS` | vuoto | Domini per i certificati TLS automatici (ascolto su 443 e 80) |
| `PORTO_TRUSTED_PROXIES` | vuoto | CIDR dei reverse proxy fidati, per ricavare l'IP reale |
| `PORTO_SMTP_URL` | vuoto | Server email, `smtp://utente:password@host:587` (STARTTLS) o `smtps://…:465` |
| `PORTO_MAIL_FROM` | `Porto <noreply@localhost>` | Mittente delle email; con SMTP deve usare un dominio reale |
| `PORTO_MAIL_DIR` | vuoto | Senza SMTP, cartella in cui scrivere le email come file `.eml` (sviluppo e test); vuota = solo log |
| `PORTO_REQUIRE_TOTP_FOR_OWNERS` | `false` | Rende obbligatoria l'autenticazione a due fattori per titolari e amministrazione |
| `PORTO_PUBLIC_READS_PER_MINUTE` | `120` | Richieste al minuto per rete di provenienza sull'API pubblica |
| `PORTO_HOLD_TTL` | `10m` | Durata del blocco temporaneo del posto durante una prenotazione online |
| `PORTO_SENSOR_RAW_RETENTION` | `30d` | Conservazione delle letture grezze dei sensori (escluse dal backup) |
| `PORTO_LOG_LEVEL` | `info` | Livello di log (`debug`, `info`, `warn`, `error`) |
| `PORTO_LOG_FORMAT` | `json` (`text` in sviluppo) | Formato dei log |
| `PORTO_TELEMETRY` | `false` | Porto non ha telemetria: il valore deve restare `false` |
| `PORTO_BOOTSTRAP_OWNER_EMAIL`, `PORTO_BOOTSTRAP_OWNER_NAME`, `PORTO_BOOTSTRAP_OWNER_ORG` | vuoto | Titolare e organizzazione creati all'avvio solo se il database non ha utenti (tutte e tre più `PORTO_ADMIN_PASSWORD`) |
| `PORTO_ADMIN_PASSWORD` | vuoto | Password per `porto admin create`, il titolare iniziale e `porto seed demo`; mai registrata nei log |
| `PORTO_DEMO_PAIRING_USER`, `PORTO_DEMO_PAIRING_MARINA` | vuoto | Solo istanze dimostrative: utente e slug del marina per l'abbinamento dimostrativo dei dispositivi |
| `PORTO_WEBHOOKS_ALLOW_PRIVATE` | `false` | Solo sviluppo e test: webhook verso indirizzi privati e `http` |
| `PORTO_DEV` | `false` | Modalità sviluppo (chiave generata, cookie senza `Secure` su `http`, log testuali); mai in produzione |
| `PORTO_DEV_FAKE_CHANNEL`, `PORTO_STRIPE_API_BASE` | `false`, vuoto | Solo con `PORTO_DEV=true`: canale di prenotazione finto e API Stripe finta per i test |
| `PORTO_VERSION`, `PORTO_PUBLISH_PORT` | `latest`, `8080` | Solo nei file compose: tag dell'immagine e porta pubblicata |

L'app desktop imposta da sé indirizzo, URL, cartella dati e chiave (custodita nel portachiavi del sistema). Le sue variabili facoltative sono `PORTO_UPDATE_CHECK` (`true`), `PORTO_DESKTOP_PORT`, `PORTO_DESKTOP_LAN_PORT` (`8443`) e `PORTO_DESKTOP_KEYRING` (`auto` o `file`): dettagli in [docs/desktop-build.md](docs/desktop-build.md).

**SQLite o PostgreSQL?** SQLite (file `porto.db` nella cartella dati) è il default ed è adatto alla maggior parte dei porti. PostgreSQL è consigliato oltre i 300 posti o con più marine nella stessa installazione: usa [`deploy/docker-compose.postgres.yml`](deploy/docker-compose.postgres.yml) con `POSTGRES_PASSWORD` in `deploy/.env`. Il compose inserisce la password in `DATABASE_URL` senza codifica, quindi usa solo caratteri URL-safe (ad esempio `openssl rand -hex 24`); in un `DATABASE_URL` scritto a mano i caratteri speciali vanno codificati (`@` diventa `%40`). Per passare da SQLite a PostgreSQL esegui `porto backup` sulla vecchia installazione e `porto restore` sulla nuova.

Endpoint operativi: `/healthz` (processo vivo), `/readyz` (database raggiungibile e migrazioni allineate).

---

## Incorporare il widget nel tuo sito

La pagina di prenotazione è raggiungibile direttamente su `https://{host}/b/{slug}` (con `?lang=en` per l'inglese) e la scheda pubblica del porto su `https://{host}/p/{slug}`: puoi linkarle dal sito, dai social o da Google Business.

**Script (consigliato)**: crea l'iframe, ne adatta l'altezza al contenuto e ti avvisa dei passi della prenotazione.

```html
<div data-porto="marina-esempio" data-lang="it"></div>
<script src="https://{host}/embed.js" async></script>
```

**Iframe**: funziona ovunque sia consentito incollare HTML (l'altezza resta fissa).

```html
<iframe src="https://{host}/b/marina-esempio?embed=1"
        style="width:100%;height:720px;border:0" allow="payment" loading="lazy"
        title="Prenota un posto barca"></iframe>
```

- Funziona con **WordPress** (blocco "HTML personalizzato"), Wix, Squarespace, Joomla, siti statici e qualsiasi CMS.
- `embed.js` pesa meno di 3 KB compresso e rilancia sul contenitore l'evento `porto:booking` (`CustomEvent`) a ogni passo e alla prenotazione, utile per tracciare le conversioni. Attributi facoltativi: `data-title` e `data-height`.
- Per sicurezza l'incorporamento è consentito solo dai domini indicati dal gestore (`frame-ancestors` per marina), i messaggi vanno solo al sito verificato e non si usano cookie di terze parti.
- Il pagamento si apre a tutta finestra (le pagine di pagamento non si possono incorporare) e al termine la pagina di Porto mostra l'esito, con il pulsante "Torna al sito" se il sito è tra quelli autorizzati.
- Aspetto: preset di colori inclusi e intestazione nascondibile quando la pagina è incorporata; in primo piano c'è il nome del porto, con un discreto "powered by Porto".

---

## Architettura

Porto è un **monobinario Go** che incorpora le due applicazioni web e l'API. Nessun servizio esterno obbligatorio. L'app desktop avvia lo stesso server dentro il proprio processo.

```text
┌──────────────────────────────────────────────────────────────┐
│                         binario porto                        │
│                                                              │
│  ┌──────────────┐  ┌──────────────────┐  ┌────────────────┐  │
│  │ SPA pannello │  │ SPA pubblica     │  │ API REST v1    │  │
│  │ (PWA, embed) │  │ booking, scheda  │  │ + SSE, ingest  │  │
│  └──────────────┘  └──────────────────┘  └───────┬────────┘  │
│                                                  │           │
│  ┌───────────────────────────────────────────────▼────────┐  │
│  │ Servizi di dominio                                     │  │
│  │ harbor · occupancy · pricing · contract · charge ·     │  │
│  │ payment · storage · yard · rental · sensor · report    │  │
│  └──────────────────────────┬─────────────────────────────┘  │
│                             │                                │
│  ┌──────────────┐  ┌────────▼───────┐  ┌──────────────────┐  │
│  │ Job runner   │  │ Store (Bun)    │  │ Outbox eventi    │  │
│  │ in-process   │  │ unico SQL      │  │ webhook, email   │  │
│  └──────────────┘  └────────┬───────┘  └──────────────────┘  │
└─────────────────────────────┼────────────────────────────────┘
                              │
             SQLite (default) │ PostgreSQL (opzionale)
```

### Backend

| Componente | Scelta |
|---|---|
| Linguaggio | Go, binario statico senza CGO (solo la finestra dell'app desktop usa CGO) |
| HTTP | `chi`, `net/http` standard |
| Contratto API | OpenAPI 3.1 spec-first, codice generato con `oapi-codegen` |
| Accesso dati | `uptrace/bun` (stesso codice per SQLite e PostgreSQL) |
| Database | `modernc.org/sqlite` (puro Go) · PostgreSQL via `pgdriver` |
| Migrazioni | `pressly/goose`, una serie per dialetto con la stessa numerazione |
| Sicurezza | Argon2id, AES-GCM per i segreti, TOTP |
| Documenti | PDF/A generati in Go (`go-pdf/fpdf`), QR (`skip2/go-qrcode`), Excel scritto in proprio |
| Desktop | Webview di sistema (`webview_go`), portachiavi del sistema (`go-keyring`) |
| Log | `log/slog` in JSON |

### Frontend

| Componente | Scelta |
|---|---|
| Build | Vite + React + TypeScript strict, npm workspaces in `web/` |
| Applicazioni | `web/admin` (pannello, PWA con coda offline), `web/booking` (pagina pubblica, area del cliente, scheda del porto, `embed.js`), `web/shared` (client API, mappa, tema, i18n) |
| Dati | TanStack Query e Router + client tipizzato generato (`openapi-typescript`, `openapi-fetch`) |
| UI | Tailwind CSS sui token del tema + shadcn/ui (Radix), react-hook-form + zod; nessun CSS personalizzato |
| Mappa | SVG in coordinate reali renderizzato da React, culling e livelli di dettaglio per migliaia di posti |
| i18n | i18next, italiano come lingua sorgente, inglese obbligatorio |

### Principi non negoziabili

- **Nessuna doppia occupazione**: posti, celle delle aree dinamiche, risorse a terra, unità a noleggio e mezzi di sollevamento sono protetti da vincoli unici del database, verificati da test di concorrenza con almeno 100 tentativi paralleli e `-race` su entrambi i dialetti.
- **Importi in centesimi interi**, lunghezze in centimetri interi, quantità frazionarie in millesimi: mai virgola mobile.
- **La notte è l'unità di occupazione**; le date sono di calendario nel fuso del marina e il tempo arriva da un orologio iniettato.
- **Motori puri**: geometria, compatibilità, prezzi, competenze, formule e ottimizzatore non usano database, orologio o casualità (l'ottimizzatore riceve un seme esplicito).
- **Ogni cambio di stato** di contratti e transiti scrive audit log ed evento outbox nella stessa transazione; il transito si conferma solo dal webhook del pagamento o dal driver manuale.
- **Addebiti esportati immutabili**: le correzioni sono addebiti di rettifica collegati.
- **Ogni query operativa è filtrata per marina**; **API-first**: pannello, pagina pubblica e desktop usano solo le API documentate.

Approfondimenti: [specifica tecnica](docs/spec/README.md) · [architettura](docs/architecture.md) · [invarianti](docs/invariants.md) · [decisioni architetturali (ADR)](docs/adr/README.md) · [decisioni aperte](docs/spec/33-decisioni-aperte.md).

---

## Sviluppo

**Prerequisiti:** Go 1.27+, Node.js 22+, npm, Docker (per i test su PostgreSQL e il compose), `make`. In alternativa, apri il repository nel **devcontainer** incluso.

```bash
git clone https://github.com/coste360/porto.git
cd porto
make setup     # moduli Go, golangci-lint in ./bin, dipendenze web ed e2e
make seed      # database locale con il porto dimostrativo (tmp/data)
make dev       # backend su :8080 + Vite per pannello (:5173) e pagina pubblica (:5174)
```

Il `Makefile` è l'**unica interfaccia** per build, test e verifiche, per le persone e per gli agenti.

| Target | Cosa fa |
|---|---|
| `make setup` | Installa moduli Go, `golangci-lint` e le dipendenze web ed e2e (una tantum) |
| `make dev` | Backend con ricarica automatica + Vite per le due SPA |
| `make seed` | Database locale con il porto dimostrativo |
| `make gen` | Rigenera server Go, client TypeScript e tabelle generate da `api/openapi.yaml` |
| `make lint` | `golangci-lint`, controllo del CSS, delle chiavi i18n e del contrasto, ESLint, Prettier, `tsc` |
| `make test` | Test unitari Go e Vitest (senza Docker né rete) |
| `make test-db` | Test di integrazione su SQLite e su un PostgreSQL temporaneo |
| `make test-race` | Test `TestConcurrency_*` con `-race` su SQLite e PostgreSQL |
| `make e2e` / `make e2e-desktop` | Playwright sul binario reale / sull'app desktop |
| `make check` | `gen-check` + `lint` + `test` (obiettivo sotto i 2 minuti) |
| `make check-full` | `check` + `test-db` + `test-race` + `e2e` + `e2e-desktop` (prima di una pull request) |
| `make build` | Compila le SPA e `bin/porto` statico, senza CGO |
| `make desktop-build` / `make desktop-package` | Launcher desktop / pacchetti `.dmg`, installer Windows, AppImage (non firmati) |
| `make docker` / `make up` | Immagine container / istanza locale con `docker compose up --build` |
| `make licenses` | Verifica le licenze delle dipendenze Go e npm |
| `make fmt` | Formatta Go (`gofumpt`, `goimports`) e frontend (Prettier) |

### Struttura del repository

```text
porto/
├── cmd/porto/            # entrypoint e CLI del server
├── cmd/porto-desktop/    # launcher dell'app desktop
├── internal/
│   ├── domain/<area>/    # logica di dominio, un package per area (niente HTTP, niente SQL)
│   ├── store/            # accesso al database (Bun), unico posto con SQL
│   ├── http/             # handler sottili, codice generato da OpenAPI, SSE, file statici
│   ├── migrations/       # sqlite/ e postgres/
│   ├── jobs/             # job runner in-process
│   ├── backup/           # backup e ripristino portabili
│   ├── desktop/ lan/     # app desktop, rete locale e certificati
│   └── clock/ money/ calendar/ geo/ qty/ crypto/   # tempo, centesimi, date, geometria in cm, quantità, cifratura
├── pkg/ext/              # interfacce pubbliche per le estensioni
├── api/openapi.yaml      # contratto API, unica fonte di verità
├── web/                  # admin/, booking/, shared/ (npm workspaces)
├── deploy/               # Dockerfile e compose di produzione
├── packaging/desktop/    # pacchetti dell'app desktop
├── docs/                 # specifica, architettura, invarianti, ADR, piani delle milestone
├── e2e/                  # test Playwright (server e desktop)
├── testdata/             # casi condivisi tra Go e TypeScript (geometria, prezzi, importi)
├── assets/               # marchio Porto (diritti riservati) e kit di illustrazioni della mappa
├── .claude/              # regole, skill, subagent e hook per gli agenti di sviluppo
├── AGENTS.md  CLAUDE.md  Makefile
└── LICENSE  CONTRIBUTING.md  CHANGELOG.md
```

---

## Roadmap

Il dettaglio con i criteri di accettazione è nella sezione 32 della [specifica](docs/spec/32-test-qualita-roadmap.md); ogni milestone ha il suo piano in `docs/` (per esempio [docs/m8-plan.md](docs/m8-plan.md)).

- [x] **M0 — Fondamenta**: repository, CI, Docker, primitive (tempo, centesimi, date, geometria in centimetri, quantità), configurazione, migrazioni per SQLite e PostgreSQL, CLI, autenticazione e ruoli, audit log, outbox, job runner, SSE, supporto agli agenti. *Accettazione:* `docker compose up` avvia un'istanza vuota e `porto admin create` permette il login (verificato anche dallo smoke test dell'immagine in CI).
- [x] **M1 — Mappa del porto e anagrafiche**: mappe multiple, editor completo con generatori e validazioni, calibrazione dello sfondo e georeferenziazione, risorse a terra, colonnine, kit di illustrazioni; clienti, barche, documenti e scadenze; importazione CSV dei posti. *Accettazione:* da un'ortofoto si configurano 6 pontili e 400 posti in meno di 30 minuti, senza posti sovrapposti (property test e test end-to-end dell'editor).
- [x] **M2 — Listini e prezzi** *(287 casi di prezzo ricavati a mano, più property test e fuzzing)*: articoli, classificazioni, listini per periodi, tutte le basi di calcolo, motore puro, simulatore, matrice, copertura, rinnovo. *Accettazione:* almeno 200 casi di prezzo; la matrice coincide con il simulatore.
- [x] **M3 — Occupazione, transiti e mappa operativa**: motore di occupazione con i vincoli del database, compatibilità, opzioni, ricerca e assegnazione, preventivi, contratti di ormeggio e transiti, spostamenti, pontile dinamico, rilasci, presenze, vista operativa, SSE. *Accettazione:* 100 tentativi paralleli sullo stesso posto non producono mai una doppia occupazione; una stagione simulata non lascia incoerenze; la mappa operativa con 3.000 posti resta nel budget di prestazioni (verificato in CI).
- [x] **M4 — Contrattualistica completa**: preventivo → contratto, rate e scadenzario, rinnovi e campagne, charter, diritti d'uso, cessioni, mandati, oneri, spese condominiali, modelli e PDF, firma, comunicazioni, scadenze. *Accettazione:* campagna di rinnovo di 300 contratti dalla proposta alla firma e all'acconto senza interventi sul database; riparto delle spese su 400 posti con somma esatta.
- [x] **M5 — Addebiti e incassi**: addebiti da tutte le origini, incassi e abbinamenti, rimborsi, crediti, cassa e corrispettivi, scadenzario, richieste di pagamento e solleciti, Stripe con SEPA, estratto conto, esportazione "da fatturare" con riscontro delle fatture. *Accettazione:* in un anno simulato ogni addebito finisce nell'esportazione esattamente una volta e saldi, incassi e rimborsi coincidono.
- [x] **M6 — Multipiattaforma** *(pacchetti desktop non ancora firmati)*: app desktop con server locale, backup e ripristino, rete locale con abbinamento tramite QR, pannello dal telefono con coda offline, modalità remota, chiosco, ponte verso le periferiche. *Accettazione:* dal telefono in rete locale l'ormeggiatore apre il pannello in meno di un minuto con il QR (test Playwright); operazioni offline senza duplicati alla riconnessione.
- [x] **M7 — Report e cruscotto**: tutti i report, cruscotto con drill-down, confronti tra anni, esportazioni CSV ed Excel. *Accettazione:* ogni indicatore del cruscotto coincide con la somma dei record del suo drill-down sui dati demo.
- [x] **M8 — Prenotazione online**: pagina pubblica, widget, pagamento anticipato, approvazione, area del cliente, contingenti, interfaccia per i canali. *Accettazione:* prenotazione completa da smartphone dentro un iframe su un altro sito in meno di 2 minuti (test Playwright); nessuna doppia vendita tra banco, online e canale.
- [x] **M9 — Rimessaggio e manutenzioni**: contratti di rimessaggio, planner, ipotesi, accessibilità delle uscite, ottimizzatore, manutenzioni, letture e addebito delle utenze. *Accettazione:* in un capannone di 2.000 m² con 40 barche l'ottimizzatore produce una disposizione valida, senza uscite bloccate, ben sotto i 3 secondi.
- [x] **M10 — Cantiere**: catalogo e formule, preventivo guidato, commesse, planner e carico, ore, materiali, consuntivo e margini, mezzi di sollevamento. *Accettazione:* il preventivo di un'antivegetativa per una barca di 12 m si calcola senza inserire prezzi a mano e il consuntivo riconcilia ore, materiali e costi esterni.
- [x] **M11 — Noleggio**: flotta, planner, consegna e riconsegna dal telefono, cauzioni, incasso rapido. *Accettazione:* nessuna doppia prenotazione di un'unità con tentativi paralleli.
- [x] **M12 — Sensori e scheda pubblica**: ingestione, isteresi, riconciliazione e avvisi, contesto *Sensori*, scheda pubblica con occupazione aggregata. *Accettazione:* 1.000 sensori con una lettura al minuto su SQLite senza ritardi della vista operativa oltre 2 secondi.
- [ ] **Primo rilascio pubblico** <!-- TODO(release): numero di versione, data e note di rilascio. -->
- **Dopo la prima versione** (vedi le [decisioni aperte](docs/spec/33-decisioni-aperte.md)): firma InfoCert, esportatori per i software di fatturazione più usati, partner hardware per i sensori, comunicazioni obbligatorie, mercati esteri, vendita online del noleggio, magazzino del cantiere, ormeggio in seconda fila. L'app mobile nativa per lo staff è fuori da questo repository.

---

## Sviluppo con agenti AI

Porto è progettato per essere sviluppato anche da agenti di coding (Claude Code e strumenti compatibili con `AGENTS.md`): tutto ciò che serve per compilare, testare e capire dove va una modifica è nel repository, e ogni verifica che conta è automatica.

- [`AGENTS.md`](AGENTS.md): istruzioni condivise per tutti gli agenti (regole non negoziabili, comandi, modo di lavorare).
- [`CLAUDE.md`](CLAUDE.md): importa `AGENTS.md` e aggiunge le note specifiche di Claude Code.
- `.claude/`: regole caricate per area (`rules/`: contratto API, backend Go, migrazioni, occupazione, geometria, addebiti, frontend, sicurezza), procedure ripetibili (`skills/`, es. `/new-endpoint`, `/new-migration`, `/new-map-element`, `/new-price-rule-test`, `/pre-pr-check`), subagent di revisione (`agents/`: `reviewer`, `invariants-guardian`, `test-writer`) e hook che proteggono i file generati e formattano ogni file modificato.
- `scripts/agents/check-naming.sh` (job `naming` della CI) e `make check` chiudono il ciclo: un task piccolo è finito quando le verifiche passano.

Riguarda solo gli strumenti con cui si sviluppa Porto. **Il prodotto non contiene funzioni di intelligenza artificiale e non chiama servizi di IA** ([ADR 0008](docs/adr/0008-nessuna-intelligenza-artificiale.md)).

---

## Contribuire

Segnalazioni dai porti e dai cantieri sul campo, correzioni, traduzioni, driver di pagamento, esportatori per i software di fatturazione e documentazione sono benvenuti.

1. Leggi [CONTRIBUTING.md](CONTRIBUTING.md), [`AGENTS.md`](AGENTS.md) e la [specifica](docs/spec/README.md), che è autorevole.
2. Apri una issue prima di una modifica non banale: le [decisioni aperte](docs/spec/33-decisioni-aperte.md) non si decidono nel codice.
3. Usa Conventional Commits, aggiungi una riga al [CHANGELOG](CHANGELOG.md) ed esegui `make check` (e `make check-full` per modifiche a occupazione, prezzi, addebiti, pagamenti, migrazioni o flusso pubblico).

I contributi vengono distribuiti con la stessa licenza del progetto (BSL 1.1, poi AGPL-3.0). Contribuire non dà diritto a rivendere Porto o a offrirlo come servizio a terzi: per questo serve una licenza commerciale di Coste360 SRL.

---

## Sicurezza

Non aprire issue pubbliche per le vulnerabilità. Scrivi a **security@coste360.it** con la descrizione, la versione o il commit interessato, il database e i passi per riprodurre; non includere dati personali reali.

---

## Nota fiscale

Porto **non emette fatture** e non trasmette nulla al Sistema di Interscambio: le fatture si emettono con il software di fatturazione del gestore, alimentato dall'esportazione "da fatturare" (CSV, Excel, PDF). Il riscontro importa le fatture emesse per collegarle agli addebiti; un addebito esportato non cambia più e ogni correzione è un addebito di rettifica collegato.

Porto **non è un registratore telematico**. Per i corrispettivi il driver `none` presuppone che il gestore emetta il documento commerciale con il proprio registratore; il driver `export` produce il riepilogo giornaliero per aliquota e metodo di pagamento da consegnare al commercialista; dall'app desktop un registratore collegato al PC può ricevere i documenti tramite il ponte delle periferiche, ma Porto non include driver per modelli reali. Le ricevute di cortesia stampate da Porto sono **non fiscali**.

La conformità della configurazione (aliquote IVA, emissione dei documenti, conservazione) **resta responsabilità del gestore e del suo consulente**.

---

## Licenza e marchio

Porto è **source available** (codice sorgente disponibile) con licenza **[Business Source License 1.1](LICENSE)** (BSL 1.1, identificativo SPDX `BUSL-1.1`), lo stesso modello adottato da progetti come Sentry, CockroachDB e Terraform. In pratica:

- **Gratuito per chi gestisce il proprio porto.** Puoi installare e usare Porto in produzione, senza pagare nulla, per gestire i porti turistici, le marine, gli approdi e i punti d'ormeggio (compresi i campi boe), i cantieri navali e i rimessaggi che possiedi o gestisci direttamente (anche tramite società controllate), con i servizi accessori dello stesso sito come ormeggi, carburante, noleggio, lavori di cantiere, bar e ristorante, anche pubblicando la pagina di prenotazione e il widget per i tuoi clienti. Gestisci direttamente una struttura se tu (o una società del tuo gruppo) ne hai la concessione, per esempio la concessione demaniale marittima, la licenza o il titolo, oppure la gestisci per conto tuo e non come fornitore di un altro.
- **Libero per provarlo e migliorarlo.** Puoi leggere il codice, copiarlo, modificarlo, testarlo e contribuire al progetto.
- **Non rivendibile.** Non puoi usare Porto, o una sua versione modificata, per offrire servizi a terzi a pagamento o con altro vantaggio commerciale: hosting, servizio gestito o SaaS, installazione, configurazione, personalizzazione, manutenzione, assistenza, rivendita, né includerlo in un prodotto o servizio per terzi che sostituisce Porto o offre funzioni simili, come gestionali o sistemi di prenotazione dei posti barca per porti, approdi e cantieri. Un consorzio o un'associazione che non controlla i porti soci non li gestisce direttamente: per offrire Porto ai soci serve una licenza commerciale. Per questi usi serve una **licenza commerciale** di Coste360 SRL: scrivi a **info@coste360.it**.
- **Diventa AGPL-3.0 dopo 3 anni.** Ogni versione di Porto passa automaticamente alla licenza open source **[GNU AGPL-3.0 o successiva](https://www.gnu.org/licenses/agpl-3.0.html)** tre anni dopo il suo primo rilascio pubblico (la *Change Date* della BSL).
- **Cosa resta libero.** Inserire la pagina di prenotazione o il widget in un sito o in un'app non richiede licenza, purché chi lo fa non installi, ospiti, configuri, personalizzi, mantenga o assista Porto. Aiutare un gestore gratis è permesso, se l'aiuto non serve a vendere o promuovere un'offerta a pagamento legata a Porto o alla gestione, alle prenotazioni o al sito del porto. Un modello di installazione generico o "in un clic", con cui il gestore avvia Porto da sé sul server che noleggia, è permesso se il fornitore non gestisce Porto per lui e non vende un'offerta dedicata a Porto.

Il testo vincolante è quello del file [LICENSE](LICENSE); questa sintesi non lo sostituisce.

Il nome **Porto**, il logo e i file del marchio in [`assets/brand/porto/`](assets/brand/porto/NOTICE.md) sono di Coste360 SRL, con tutti i diritti riservati, ed **esclusi dalla licenza del codice**: la licenza non concede alcun diritto sul nome o sul logo. Le regole d'uso sono nell'[ADR 0007](docs/adr/0007-marchio.md).

---

## Domande frequenti

<details>
<summary><strong>Esiste un gestionale per porti turistici gratuito?</strong></summary>

Sì, Porto è un gestionale portuale gratuito con codice sorgente disponibile: un software per la gestione di porti turistici, marine, approdi e cantieri nautici che installi sul tuo server o sul PC dell'ufficio, senza canoni né licenze a pagamento. Comprende mappa del porto, contratti di ormeggio e transiti, prenotazione online dei posti barca, addebiti e incassi, rimessaggio, cantiere, noleggio e report.
</details>

<details>
<summary><strong>Porto è davvero gratuito?</strong></summary>

Sì, per chi gestisce la propria struttura. Se lo usi per i porti, gli approdi, i cantieri o i rimessaggi che possiedi o gestisci direttamente non ci sono canoni, versioni a pagamento o limiti sul numero di posti barca, contratti o utenti. L'unico costo è il computer o il server su cui gira. Una licenza commerciale serve solo a chi vuole offrire Porto come servizio a terzi.
</details>

<details>
<summary><strong>Ho solo un cantiere o un rimessaggio, senza posti in acqua: posso usarlo?</strong></summary>

Sì. Cantieri navali e rimessaggi rientrano nell'uso gratuito, come i porti. Puoi usare solo le parti che ti servono: mappa del piazzale e del capannone, contratti di rimessaggio, ottimizzatore, preventivi e commesse di cantiere, mezzi di sollevamento, addebiti e incassi.
</details>

<details>
<summary><strong>Sono un'agenzia, un tecnico o una software house: posso installarlo per i miei clienti?</strong></summary>

Non senza una licenza commerciale. La licenza gratuita copre solo le strutture che il gestore possiede o gestisce direttamente. Installare, configurare, personalizzare, ospitare (hosting o SaaS), mantenere o assistere Porto per conto di altri porti a pagamento, o rivenderlo, richiede una licenza commerciale di Coste360 SRL: scrivi a **info@coste360.it**. Se invece sei il gestore, puoi far installare Porto dai tuoi dipendenti o installarlo tu stesso su un server che hai noleggiato.

Se realizzi il sito del porto puoi inserirci il widget o il link alla pagina di prenotazione senza licenza, purché non installi, ospiti, configuri o assisti Porto.
</details>

<details>
<summary><strong>Siamo un consorzio o un'associazione di porti: possiamo usarlo per i soci?</strong></summary>

Ogni porto socio può installare e usare Porto gratis per sé. Un consorzio o un'associazione che non controlla i porti soci non li gestisce direttamente, quindi per installare, ospitare o assistere Porto per i soci serve una licenza commerciale di Coste360 SRL: scrivi a **info@coste360.it**.
</details>

<details>
<summary><strong>Perché non è open source in senso stretto?</strong></summary>

Perché vogliamo che Porto resti gratuito per i gestori e che nessuno possa prenderlo e rivenderlo come servizio a pagamento agli stessi porti a cui lo diamo gratis. La Business Source License 1.1 protegge questo uso gratuito dai rivenditori, ma lascia il codice leggibile, modificabile e aperto ai contributi. Ogni versione diventa poi open source a tutti gli effetti con licenza **AGPL-3.0** tre anni dopo il suo rilascio.
</details>

<details>
<summary><strong>Porto emette le fatture elettroniche?</strong></summary>

No. Porto calcola e tiene gli addebiti (rate, transiti, servizi, consumi, commesse, noleggi, oneri) e li esporta "da fatturare" in CSV, Excel o PDF per il software di fatturazione che usi già; poi il riscontro collega le fatture emesse agli addebiti. Vedi la [nota fiscale](#nota-fiscale).
</details>

<details>
<summary><strong>Ci sono commissioni sui transiti prenotati online?</strong></summary>

No. I pagamenti online passano dal tuo account Stripe e l'incasso arriva direttamente a te: paghi solo le commissioni del circuito di pagamento, nessuna percentuale alla piattaforma. Puoi anche non attivare i pagamenti online e confermare i transiti dal banco con bonifico o in cassa.
</details>

<details>
<summary><strong>Posso usarlo con il mio sito WordPress?</strong></summary>

Sì. Il widget di prenotazione dei posti barca si incolla in un blocco "HTML personalizzato" di WordPress, oppure in Wix, Joomla, Squarespace o qualsiasi sito, con uno script di una riga o un iframe. Puoi anche usare il link diretto alla pagina di prenotazione o alla scheda del porto. Vedi [Incorporare il widget](#incorporare-il-widget-nel-tuo-sito).
</details>

<details>
<summary><strong>Funziona senza internet?</strong></summary>

Sì. Con l'app desktop server e archivio stanno sul PC dell'ufficio e non serve alcun servizio esterno; gli altri PC e i telefoni degli ormeggiatori si collegano sulla Wi-Fi del porto. Il pannello sul telefono tiene in coda arrivi, partenze, foto, letture e incassi registrati senza rete e li invia al ritorno della connessione, senza doppioni. Servono internet, e quindi un server raggiungibile, solo la prenotazione online, i pagamenti online e la firma a distanza.
</details>

<details>
<summary><strong>Porto usa l'intelligenza artificiale?</strong></summary>

No. Porto non contiene funzioni di IA e non invia dati a servizi di IA: i dati dei documenti li inserisce l'operatore, con il file allegato e controlli deterministici ([ADR 0008](docs/adr/0008-nessuna-intelligenza-artificiale.md)). Non c'è nemmeno telemetria.
</details>

<details>
<summary><strong>Posso gestire più marine?</strong></summary>

Sì. Un'unica installazione gestisce più marine, ciascuna con le proprie mappe, i propri listini, contratti, utenti e la propria pagina di prenotazione. Per molte strutture o porti con più di 300 posti è consigliato PostgreSQL.
</details>

<details>
<summary><strong>Quali tipi di ormeggio e di spazi supporta?</strong></summary>

Posti fissi su pontili e banchine, con finger o a corpo morto, posti in testata, campi boe, aree dinamiche dove le barche si affiancano secondo la lunghezza, piazzali e capannoni a celle per il rimessaggio, parcheggi e box. Le barche occupano il loro ingombro reale e Porto controlla che stiano nel posto. L'ormeggio in seconda fila (affiancamento a un'altra barca) non è nella prima versione.
</details>

<details>
<summary><strong>I dati dei miei clienti sono al sicuro e conformi al GDPR?</strong></summary>

I dati restano sul computer o sul server che scegli tu: nessuno, nemmeno Coste360, vi accede. Porto raccoglie solo i dati necessari, cifra documenti e IBAN, versiona i consensi, consente esportazione e anonimizzazione dei dati del cliente e non registra email, telefoni o documenti in chiaro nei log. Il titolare del trattamento resta il gestore.
</details>

---

## Crediti

Porto è ideato e sviluppato da **[Coste360 SRL](https://www.coste360.it/)**, la piattaforma italiana per il monitoraggio delle coste e delle concessioni demaniali marittime. Condivide lo stack e l'architettura con il gestionale per stabilimenti balneari di Coste360.

Se Porto ti è utile, lascia una stella al repository e parlane con altri porti e cantieri: è il modo migliore per farlo crescere.

---

## In English

**Porto** is a free, self-hosted, source-available management system for marinas, tourist ports, berthing points and boatyards. It covers the whole marina cycle: a to-scale harbour map (quays, piers, berths, buoy fields, dynamic alongside areas, yards, sheds, parking and boxes) with an operational view of every night, customers and vessels, price lists with a pure pricing engine, mooring contracts, transits, renewals and e-signature, berth-use rights and condominium charges, charges and payments, dry storage with a layout optimizer, boatyard quotes and work orders, boat rental, occupancy sensors, a public marina profile and reports. Online berth booking for transits can be embedded in any website via iframe or a one-line script, with no booking commissions: online payments go through the marina's own Stripe account. Porto does not issue invoices: charges are exported to the invoicing software the marina already uses. It ships as a single Go binary and Docker image (`ghcr.io/coste360/porto`, published from the first release) with SQLite by default and optional PostgreSQL, and as a desktop app for Windows, macOS and Linux that works fully offline, with phones joining over the marina's local network and an offline queue for dock operations. It contains no AI features and no telemetry. All milestones from M0 to M12 are implemented and covered by automated tests; the first public release is being prepared. It is provided as is, without warranty, and is developed by [Coste360 SRL](https://www.coste360.it/).

It is source-available under the [Business Source License 1.1](LICENSE): free for marinas, berthing points, boatyards and dry-storage facilities that run it for sites they own or operate directly; offering it to third parties as a paid service (hosting, SaaS, installation, customisation, support, resale) requires a commercial license from Coste360 SRL (info@coste360.it). Each version converts to AGPL-3.0 or later three years after its first public release. The Porto name and logo are not covered by the code licence. The public booking page, the widget and the customer area target **WCAG 2.1 level AA**, checked with axe-core in light and dark themes by the end-to-end tests. The user interface is available in Italian and English.
