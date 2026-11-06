## 2. Perimetro e tracciabilità dei requisiti

### 2.1 Fonte dei requisiti

Il perimetro funzionale è definito dalla brochure di riferimento del mercato (prodotti per marina: gestionale,
app di prenotazione dei posti barca, sistema di occupazione in tempo reale, portolano). Ogni funzione del gestionale citata lì ha
una sezione di questa specifica. La tabella seguente è la **matrice di tracciabilità**: un requisito senza riga
è fuori perimetro; una riga senza sezione è un difetto della specifica.

### 2.2 Matrice

| Area (brochure) | Requisito | Sezione | Milestone |
|---|---|---|---|
| Clientela e imbarcazioni | Gestione clienti e barche | 11.1–11.3 | M1 |
| | Cruscotto cliente e panoramica dei sospesi | 11.4 | M5 |
| | Scadenze del cliente | 11.5 | M4 |
| | Scadenze assicurative sulle imbarcazioni | 11.5 | M1 |
| | Avvisi automatici sui pagamenti | 16.4, 28.3 | M5 |
| | Comunicazioni via email ai clienti | 28.2 | M4 |
| Contrattualistica | Prenotazioni e preventivi | 13.2, 13.3 | M3 |
| | Contratti e transiti | 13.4, 13.5 | M3 |
| | Rateazione sui contratti, scadenzario rate | 13.7 | M4 |
| | Contratti charter | 13.6 | M4 |
| | Rinnovo semplificato e massivo | 13.8 | M4 |
| | Contratti di cessione | 14.1–14.3 | M4 |
| | Calcolo oneri di gestione | 14.4 | M4 |
| | Spese condominiali | 14.5 | M4 |
| | Procedura automatica preventivo → contratto | 13.3 | M4 |
| | Modelli di stampa personalizzati | 15.1–15.3 | M4 |
| | Firma elettronica (InfoCert) | 15.4 | M4 |
| Fatturazione e pagamenti | Fatturazione dei contratti semplificata | 16.1, 16.5: addebiti dalle rate ed esportazione "da fatturare" | M5 |
| | Ciclo attivo di vendita | 16.1–16.5, fino all'esportazione; la fattura è esterna | M5 |
| | Scontrini e corrispettivi | 16.3 (come Arenile) | M5 |
| | Casse | 16.3 | M5 |
| | Registrazione transazioni di pagamento | 16.2 | M5 |
| | Classificazione dei ricavi | 12.1 (`revenue_class`), 21.3 | M2 |
| | Fatture, proforma, note di credito, fatturazione elettronica, fatture passive, prima nota, conti bancari, centri di costo | **esterni**, vedi 2.3 | — |
| Listini e articoli | Listini per periodi | 12.2 | M2 |
| | Prezzo dinamico per dimensione e al m² | 12.3 | M2 |
| | Anagrafica, codifica e classificazione articoli e servizi | 12.1 | M2 |
| | Rinnovo dei listini semplificato | 12.6 | M2 |
| Piano ormeggi e risorse | Pontili, gruppi e posti barca | 7, 8 | M1 |
| | Presenze delle barche | 10.6, 9.5 | M3 |
| | Mappa interattiva di ormeggi e risorse | 9 | M3 |
| | Risorse a terra | 7.6, 20.3 | M1 |
| | Ricerca semplificata delle disponibilità | 9.6, 10.7 | M3 |
| | Avvisi per le manutenzioni degli ormeggi | 20.1, 20.2 | M9 |
| Mappa | Una mappa, molteplici contesti (contratto, tipo barca, presenza, pagamenti) | 9.2 | M3 |
| | Più mappe (parcheggi, box, piazzali) | 7.1, 7.6 | M1 |
| | Pontile dinamico: drag & drop su pontili, piazzali, capannoni, rimesse | 9.4, 10.3 | M3 |
| | Ottimizzazione rimesse e ipotesi di spostamento | 17.3–17.5 | M9 |
| Cantiere | Preventivi con parametrizzazione del prezzo | 18.2, 18.3 | M10 |
| | Procedura guidata prodotti, servizi, gruppi di lavorazioni | 18.3 | M10 |
| | Planner dei dipendenti, orari e carico | 18.5 | M10 |
| | Carico giornaliero, settimanale, mensile | 18.5 | M10 |
| | Barche in lavorazione e stato di completamento | 18.4 | M10 |
| | Consuntivazione materiali e ore, costi e profitti | 18.6 | M10 |
| Noleggio | Planner noleggi, flotta, consegna e restituzione | 19 | M11 |
| | Noleggi orari, mezza giornata, giornalieri | 19.2 | M11 |
| | Incasso rapido e addebito | 19.5 | M11 |
| Manager | Dashboard statistica e indicatori, drill-down | 21.2 | M7 |
| Report | Occupazione, efficienza occupazionale, spreco allocativo | 21.3 | M7 |
| | Clientela, competenze, ratei e risconti | 21.3 | M7 |
| | Addebiti e incassi dettagliati, classificazione dei ricavi, comparazione | 21.3 | M7 |
| Cloud e integrazioni | Prenotazione online, pagamenti, software di fatturazione di terze parti | 23, 16.5, 27 | M5–M8 |
| App di prenotazione | Prenotazione immediata con pagamento anticipato | 23 | M8 |
| Sensori | Occupazione dei posti barca in tempo reale | 24 | M12 |
| Portolano | Scheda tecnica pubblica del singolo porto (la directory dei porti è fuori perimetro) | 25 | M12 |
| Multipiattaforma | Server del gestore, desktop, pannello dal telefono (non SaaS); app mobile nativa fuori da questo repository | 31 | M6 |
| **Aggiunte Porto** | Vista a tessere, legenda come filtro, contesto Anomalie, disposizione con Salva/Ripristina, riquadro "Da collocare" | 9.1, 9.2, 9.4 | M3 |
| | Ricerca con posti di barche fuori, calendario degli ormeggi | 9.6, 9.7 | M3 |
| | Richieste e domanda persa | 13.2, 21.3 | M3, M7 |
| | Addenda ai contratti | 13.4 | M4 |
| | Importazione da altri gestionali | 11.6 | M1–M4 |
| | Scheda della concessione demaniale del porto (manuale) | 14.6 | M4 |

### 2.3 Fuori perimetro (prima versione)

- **Fatturazione e contabilità**, come in Arenile: emissione e numerazione di fatture, proforma e note di credito,
  fatturazione elettronica (FatturaPA, SdI), fatture passive, prima nota, conti bancari e riconciliazione, centri
  di costo, registri IVA, contabilità generale. Porto calcola gli addebiti, registra incassi e rimborsi e li
  esporta al software di fatturazione o al commercialista (16.5); un'estensione `ChargeExporter` può inviarli
  direttamente.
- Gestione del personale (paghe, presenze legali): il cantiere usa orari e ore lavorate solo per pianificare e
  consuntivare.
- Magazzino completo (lotti, inventario fisico, riordino): solo articoli di materiale con movimenti e giacenza
  (18.6).
- Integrazioni con la piattaforma Coste360 e con directory esterne (portolano): Porto è un gestionale autonomo.
- Intelligenza artificiale e servizi di IA esterni (trascrizione automatica dei documenti): Porto lavora offline e
  i dati dei documenti li inserisce l'operatore (sezione 22, ADR 0008).
- Servizio in cloud gestito (SaaS): ogni porto ha la propria installazione (sezione 31).
- Ormeggi in seconda fila (barche affiancate a un'altra barca).
- Comunicazioni obbligatorie alle autorità marittime o di pubblica sicurezza: estensioni per porto, dopo verifica
  normativa (sezione 33).
