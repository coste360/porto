# ADR 0014 — Rimessaggio e manutenzioni (M9)

- **Stato:** proposta
- **Data:** 2026-11-08
- **Sezioni della specifica:** 17, 20, 10.2, 10.5, 12.3, 13.1, 16, 21, 26.1, 27, 29.2, 32 (M9)

## Contesto

M9 porta i contratti di rimessaggio su piazzali e capannoni, il pianificatore sulla mappa a celle, le ipotesi di
sistemazione separate dallo stato reale, l'accessibilità (ogni barca deve poter uscire), un ottimizzatore puro e
l'applicazione transazionale di un'ipotesi; poi piani e interventi di manutenzione su posti, strutture, colonnine e
mezzi, le letture dei contatori e l'addebito dei consumi tramite gli addebiti di M5. Arenile non ha nulla di tutto
questo: non c'è comportamento da riprodurre (ADR 0001). Dove la specifica tace, le decisioni sono in
`docs/m9-plan.md` §16 (D1–D30); questo ADR registra quelle vincolanti.

## Decisione

1. **Contratti di rimessaggio sul dominio dei contratti (§3, D1–D5).** Un contratto `storage` usa la stessa tabella
   degli stati dell'ormeggio (`StorageTransitions`) e passa sempre da `internal/domain/contract`
   (`CreateStorageTx`, `UpdateStorageDetailsTx`) con audit e outbox nella stessa transazione (INV-30). I dati propri
   stanno in `storage_contract_details` (supporto, proprietà dell'invaso, alaggio e varo previsti). Il prezzo usa il
   tipo di contratto `storage` per ogni durata; coperto o scoperto è la `price_class` dell'area. Ogni contratto occupa
   da subito una posizione concreta (righe di `cell_occupancies`, INV-01): senza posizione la sceglie
   `optimizer.PlaceOne`, e senza spazio la risposta è `409 storage_no_space`. Il permesso di creazione è
   `storage.manage` (spec 26.1 "Rimessaggio"), così il capo cantiere crea contratti di rimessaggio.
2. **Compatibilità (§3.3, D6).** `fit` aggiunge `support_not_allowed` (bloccante, mai derogabile: è fisico) e
   `weight_exceeds` (bloccante, derogabile come le misure); il peso per cella è il dislocamento diviso le celle dello
   scafo senza margine. Go e TypeScript restano allineati sui vettori condivisi.
3. **Accessibilità come euristica documentata (§5, D7–D9).** Il rettangolo della barca trasla cella per cella (o
   ruota sul posto quando il quadrato che lo contiene è libero) nella griglia libera della data finché un lato intero
   sta sul bordo dentro un varco (componente connessa delle celle di uscita): una barca che esce di prua chiede una
   porta larga almeno baglio più margine. Le uscite di un giorno si estraggono in ordine di vicinanza al varco e
   precedono gli arrivi; le barche che bloccano si trovano togliendo in ordine di distanza e reinserendo all'indietro
   (insieme minimale, non minimo). Gli arrivi bloccati sono avvisi, mai errori.
4. **Ottimizzatore puro e riproducibile (§6, D10–D13).** `internal/domain/optimizer` non importa store, orologio,
   `math/rand` né usa float; ha un PRNG proprio (splitmix64 per il seme di xoshiro256**), ordinamenti stabili con
   pareggio per id e nessuna decisione sull'ordine delle mappe. L'obiettivo è lessicografico su interi: barche non
   collocate, uscite bloccate, arrivi bloccati, spostamenti rispetto allo stato reale (0/180 e 90/270 sono lo stesso
   rettangolo), meno il più grande rettangolo libero della prima notte. Due costruzioni (tenere le posizioni attuali;
   riempimento dal varco in ordine di uscita) e poi un hill climbing con accettazione ritardata. Il budget è un numero
   di iterazioni (deterministico) più la scadenza del chiamante nel `context` (2,5 s di default, impostazione
   `storage.optimize_budget_ms` 500–10.000): con un budget non interrotto il risultato dipende solo da input e seme
   (seme di default: FNV-1a dell'id dell'ipotesi, salvato). Griglie oltre 40.000 celle si lavorano ingrandite di un
   fattore intero e ogni posizione si riconvalida sulla griglia fine; al massimo 300 barche per esecuzione.
   `Validate` controlla ogni risultato in modo indipendente: un fallimento è un difetto, risponde 500 e non arriva mai
   una disposizione non valida (INV-15).
5. **Ipotesi separate dallo stato reale (§7.1–7.2, D14).** Un'ipotesi è una copia di lavoro (`scenario_items`) con
   gli spostamenti ricalcolati a ogni salvataggio (`scenario_moves`) e le versioni delle allocazioni lette alla
   creazione (`base`). Non scrive mai `allocations` né righe di occupazione (INV-14). Le sovrapposizioni nella copia
   sono ammesse durante la modifica e bloccano l'applicazione; "superata" (`stale`) si calcola in lettura e si impone
   all'applicazione.
6. **Applicazione tutto o niente (§7.3, D15, D16).** `POST storage/scenarios/{id}/apply` blocca l'ipotesi
   (`FOR UPDATE`), poi sotto i lock di `occupancy.LayoutTx` ricontrolla ogni posizione sullo stato attuale (versioni,
   ingombro, compatibilità, celle) e raccoglie **tutti** i conflitti: anche uno solo annulla tutto con
   `409 scenario_conflicts` e la lista completa. Altrimenti scrive allocazioni, righe di occupazione, lista degli
   spostamenti, stato dell'ipotesi, audit ed eventi in una sola transazione (INV-06). I vincoli unique restano
   l'autorità: un conflitto sfuggito al controllo arriva come errore di `LayoutTx` e annulla tutto. Il test di
   atomicità usa trigger temporanei installati dal test sul database di prova, mai un aggancio nel codice di
   produzione.
7. **Manutenzioni (§8, D24–D27).** Mezzi, piani (mesi, cicli di un contatore, data fissa) e interventi con una
   tabella degli stati pura. Il job `maintenance.schedule` crea un intervento per `(piano, elemento, scadenza)`
   (indice unico) e una riga di scadenza `maintenance_task`. Le notti di manutenzione sono allocazioni `maintenance`
   su posti e risorse a terra scritte con `occupancy.AllocateTx`; se una barca le occupa, la pianificazione risponde
   `409 maintenance_conflicts` con fino a tre alternative per barca e non scavalca mai nessuno (INV-92). Completare
   un intervento non rilascia le notti (sicuro offline); rilascio e annullamento sono azioni dell'ufficio online.
8. **Consumi (§9, D19–D23, D29).** Le letture sono indici cumulativi in `qty.Milli`; i consumi sono le differenze tra
   letture consecutive (moltiplicatore, giro del registro, sostituzione del contatore), divisi per notti tra gli
   occupanti con il metodo dei resti più grandi (la somma è esatta, INV-91); le notti senza occupante e quelle dei
   titolari di diritti restano non assegnate e non si addebitano. L'addebito passa da `charge.CreateTx` con origine
   `utility` (una riga `utility_billings` per esecuzione e contratto), di default insieme alla rata successiva; i
   consumi si prendono con un UPDATE condizionato sotto il lock del contratto e `charges_origin_primary_uidx` include
   `utility` (INV-90). Gli intervalli addebitati sono congelati: correzioni tardive diventano addebiti di rettifica
   collegati, mai modifiche (INV-51).
9. **Offline (§10, D18).** Diventano accodabili `recordMeterReading` e `uploadMeterReadingPhoto` (tipo
   `utility.reading`), `reportMaintenance` e `uploadMaintenancePhoto` (`maintenance.report`),
   `completeMaintenanceTask` (`maintenance.outcome`), con deduplica di dominio su `client_ref` oltre
   all'`Idempotency-Key` (INV-83). Nessuna crea, sposta o rilascia allocazioni (INV-71). Spostamenti di rimessaggio e
   ipotesi restano solo online.

## Conseguenze

- Nuove migrazioni `00018_storage` e `00019_maintenance_utilities` su entrambi i dialetti; `00019` ricrea
  `charges_origin_primary_uidx` con `utility` e il Down rimette la definizione di `00010`.
- Nuovi eventi `storage.scenario_applied`, `storage.movement_done`, `maintenance.due` (pubblico),
  `maintenance.status_changed`, `meter.reading_recorded` e argomenti SSE `storage.changed`, `maintenance.changed`,
  `utilities.changed`; i dati sono id, enumerazioni e date, mai valori dei contatori (INV-70).
- Il tempo di risposta dell'ottimizzatore dipende dalla macchina: il test di accettazione (capannone di 2.000 m²,
  40 barche, meno di 3 s) gira senza `-race`; con il budget di iterazioni il risultato resta riproducibile.
- Commesse di cantiere, slot dei mezzi, noleggio, contatori intelligenti e vendita online del rimessaggio restano
  fuori da M9; la tabella `equipment` e il testo `equipment_hint` sono gli agganci per M10.
