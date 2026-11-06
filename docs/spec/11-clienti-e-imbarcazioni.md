## 11. Clienti e imbarcazioni

### 11.1 Clienti

- Persona fisica o giuridica: nome e cognome o ragione sociale, codice fiscale, partita IVA, codice destinatario SdI
  e PEC (non usati da Porto, ma inclusi nell'esportazione verso il software di fatturazione, 16.5), indirizzo di
  fatturazione, nazionalità, lingua, contatti (più email e
  telefoni con etichetta), documento d'identità (tipo, numero, rilascio, scadenza), consensi versionati (come
  Arenile), note, tag, categoria cliente (socio, residente, convenzionato, charter, cantiere), condizioni di
  pagamento predefinite, IBAN per il SEPA.
- **Ruoli** di un cliente verso una barca: `owner` (proprietario, con quota di proprietà), `operator`
  (conduttore/armatore), `skipper`, `billing` (intestatario degli addebiti e quindi delle fatture, anche diverso dal proprietario, es.
  società di leasing o di charter), `contact`.
- Ricerca per nome, codice fiscale, partita IVA, telefono, email, nome o matricola della barca.
- Unione di duplicati con audit; anonimizzazione ed esportazione GDPR come Arenile (spec Arenile 23), salvo i dati
  che la legge impone di conservare (documenti fiscali: anonimizzazione differita alla fine del periodo di
  conservazione).

### 11.2 Imbarcazioni

- Nome, matricola o numero di iscrizione, ufficio d'iscrizione, bandiera, porto di registrazione, tipo
  (`hull_type`, 7.8), modello e cantiere costruttore, anno, colore dello scafo, materiale.
- **Dimensioni** in cm: lunghezza fuori tutto (`loa_cm`), lunghezza al galleggiamento facoltativa, baglio
  (`beam_cm`), pescaggio (`draft_cm`), altezza dal galleggiamento (`air_draft_cm`), dislocamento (`displacement_kg`).
  Le dimensioni dichiarate e quelle **misurate** dall'ormeggiatore sono distinte; listini e `fit` usano le misurate
  se presenti (`dimensions_source`).
- Motori (numero, marca, potenza in kW e CV, matricole, ore), alimentazione, potenza elettrica richiesta.
- Fotografie, posizione abituale, contratti in corso, storico delle occupazioni, presenze, commesse di cantiere.
- **Tipo di unità** per la normativa: natante, imbarcazione, nave da diporto (calcolato dalla lunghezza con soglie
  configurabili, solo informativo).

### 11.3 Documenti

Archivio documentale per cliente e barca (`documents`): tipo (carta d'identità, passaporto, patente, patente
nautica, licenza di navigazione, certificato di iscrizione, polizza assicurativa, certificato di sicurezza,
dichiarazione di potenza, contratto firmato, delega, altro), file (PDF o immagine, cifrato a riposo), numero,
emissione, **scadenza**, dati estratti (con IA facoltativa, sezione 22) e chi li ha confermati.

### 11.4 Cruscotto del cliente

Pagina unica per cliente: barche, contratti attivi e storici, posti occupati oggi, **sospesi** (addebiti aperti per
età: a scadere, scaduti 0–30, 31–60, 61–90, oltre 90 giorni), rate future, crediti, pagamenti, documenti
in scadenza, comunicazioni inviate, commesse e noleggi, note e attività. Azioni rapide: nuovo preventivo, incassa,
estratto conto PDF, invia sollecito, invia comunicazione.

### 11.5 Scadenze

Motore unico delle scadenze (`deadlines`), alimentato da: fine dei contratti, rate, documenti del cliente e della
barca, **assicurazioni** (obbligatoria per accedere all'ormeggio secondo il regolamento del marina), diritti d'uso,
manutenzioni. Per tipo di scadenza il marina configura gli anticipi di avviso (es. 60 e 15 giorni) e il canale
(pannello, email al cliente, email all'ufficio). Le scadenze compaiono nel cruscotto, nel contesto *Scadenze* della
mappa (9.2) e nella lista di lavoro dell'ufficio. Policy configurabile: **assicurazione scaduta** = avviso, oppure
blocco del rinnovo e dei nuovi contratti finché non viene caricata una polizza valida.

### 11.6 Importazione da altri gestionali

Nessun porto parte da zero: la migrazione è una funzione del prodotto, non un servizio a parte.
- Procedura guidata per **clienti, barche, posti** (8.7), **contratti aperti** con piano rate residuo, **diritti
  d'uso**, **scadenze** e **saldi di apertura** per cliente, da CSV o Excel.
- Abbinamento delle colonne con modelli salvati per i gestionali più diffusi (da individuare con i primi clienti,
  sezione 33), anteprima, **prova senza scrittura** che riporta righe valide, scartate e duplicati sospetti
  (codice fiscale, partita IVA, matricola), poi importazione in una transazione per lotto con report scaricabile.
- I contratti importati occupano i posti con le normali regole (10.3): i conflitti di occupazione sono errori del
  report, mai sovrapposizioni.
- Le importazioni sono annullabili finché sui record importati non ci sono operazioni successive.
