# ADR 0015 — Cantiere (M10)

- **Stato:** proposta
- **Data:** 2026-11-08
- **Sezioni della specifica:** 18, 20, 21, 26.1, 27, 29.2, 32 (M10); 02.3, 05, 06.8, 09.4, 12, 15.1, 16

## Contesto

M10 porta il cantiere: catalogo dei lavori e dei materiali con formule sulle misure della barca, preventivo guidato
prezzato dai listini di M2, commesse con attività, ore, materiali e costi esterni, consuntivo con margini e addebiti
tramite M5, planner dei dipendenti, prenotazione dei mezzi di sollevamento, report e riquadri del cantiere che M7 aveva
riservato. Arenile non ha cantiere, formule né planner: non c'è comportamento da riprodurre (ADR 0001). Dove la
specifica tace le decisioni sono in `docs/m10-plan.md` §17 (D1–D34); questo ADR registra quelle vincolanti.

## Decisione

1. **Formule pure in virgola fissa (§3.2, ADR 0006, D4).** `internal/domain/formula` è un parser a discesa ricorsiva
   con controllo dei tipi (numero, booleano, enum) e valutazione su `qty.Milli`: `+` e `-` esatti con overflow
   come errore, `*` e `/` arrotondati half-even al millesimo a ogni operazione da sinistra a destra, `if` pigro,
   `and`/`or` in corto circuito, funzioni `if min max round ceil floor band`, nessun ciclo né funzione utente né dato
   esterno, limiti di 500 byte, 256 nodi, profondità 32 e 32 identificatori. Le formule di prezzo restituiscono euro
   con 3 decimali convertiti in centesimi half-even. Mai float, mai panic (INV-29, fuzzing). `LanguageVersion` è
   salvata in ogni snapshot: cambiare la semantica la incrementa e conserva il vecchio percorso.
2. **Variabili e validazione al salvataggio (§3.3, §3.5, D5, D6).** Le variabili della barca sono incorporate e
   derivate esattamente da cm e kg; un dato mancante diventa un input del passo 3 salvato come override del
   preventivo, mai scritto sulla barca. Ogni salvataggio di variabile derivata, tabella, impostazioni di articolo o
   versione di gruppo fa parse, controllo e valutazione sulla barca di prova della marina (e sul caso di prova della
   riga); un problema è `422 formula_invalid` con l'elenco e non salva nulla. Cambiare una variabile o una tabella
   rivalida i gruppi e gli articoli che la usano.
3. **Prezzi solo dai listini (§3.7, D1, D2).** I prezzi unitari degli articoli di cantiere vengono da
   `pricing.UnitPrice`, pura, che riusa la selezione delle regole base del motore (priorità, specificità, recenza; la
   categoria cliente conta, tipo di contratto, posto, gruppo e canale no). `EngineVersion` non cambia. La tariffa
   oraria di vendita è l'articolo `labour` collegato a ogni qualifica; il costo interno è quello del dipendente,
   congelato sulla registrazione all'approvazione.
4. **Preventivi di cantiere separati e riproducibili (§3.6, §4, D3, D7, D9, D11).** I preventivi di cantiere sono un
   aggregato proprio (`yard_quotes`, dominio `yardquote`) con il ciclo di 13.2; INV-30 resta sui preventivi
   d'ormeggio, ma audit e outbox nella stessa transazione valgono anche qui. Le versioni dei gruppi sono solo in
   inserimento (trigger su entrambi i dialetti) e ogni preventivo salva uno snapshot completo: `Recompute(snapshot)`
   riproduce esattamente le righe (INV-28); "Ricalcola" è esplicito e solo sulle bozze. Le modifiche manuali di una
   riga sono marcate; nessuno sconto automatico in M10. Le varianti sono preventivi `variation` di una commessa.
5. **Accettazione che crea la commessa (§4.3, INV-96).** Accettare dal pannello o dal link blocca il preventivo,
   ricontrolla stato e scadenza e crea commessa, righe di budget, attività e acconto nella stessa transazione tramite
   l'interfaccia `yardquote.OrderCreator` implementata da `workorder` (così `yardquote` non importa `workorder`).
   `work_orders.quote_id` e `work_order_variations.quote_id` sono unici: due accettazioni concorrenti danno un 200 e
   un `409 invalid_transition`. I link riusano `access_links.purpose = quote` con `subject_type = yard_quote`; i CHECK
   di `generated_documents.subject_type` e `number_sequences.series` si allargano (SQLite con `writable_schema` come
   `00016`); numeri `PC<anno>-NNNNNN` e `C<anno>-NNNNNN` (D12).
6. **Consuntivo che riconcilia (§9.2–§9.3, D8, D16).** `workorder/account` è puro e calcola righe per articolo e tipo
   di lavorazione (manodopera per qualifica: ore approvate per la tariffa congelata; materiali: prelievi meno resi a
   costo e prezzo congelati; servizi: quantità consumata; costi esterni: costo e prezzo fatturabile), margini per
   riga, tipo di lavorazione e commessa e il confronto con il preventivo. Le equazioni di §9.3 valgono per costruzione
   (INV-93). Ricavo: `actual` = righe effettive; `fixed` e `progress` = budget più i costi esterni fatturabili salvo
   `fixed_includes_external`.
7. **Addebiti tramite M5 (§10, D10, D17, D18).** Ogni fatturazione (`work_order_billings`: acconto, SAL, finale,
   conguaglio) crea al più un addebito primario con origine `work_order` e `origin_id` = fatturazione;
   `charges_origin_primary_uidx` include `work_order` (INV-94). Le righe portano `plan_key` così
   `chargeplan.Remaining` funziona tra fatturazioni; l'acconto si sottrae per costruzione. Un residuo negativo è una
   rettifica collegata (`refunds.issue`); gli addebiti esportati non cambiano mai (INV-51). Fatturare richiede
   `charges.manage` (il capo cantiere non fattura); l'acconto all'accettazione usa l'attore che accetta.
   `invoiced` significa "addebito finale emesso": Porto non fattura.
8. **Ore, offline e cronometro (§7, D24–D26).** Un solo cronometro attivo per dipendente per vincolo unico parziale
   (INV-95); ore del cronometro arrotondate a 0,01 h, manuali a passi di 0,25 h, oltre 16 h rifiutate; le
   registrazioni approvate non si modificano (annullo prima della fatturazione finale, registrazioni tardive dopo,
   che vanno in conguaglio). Offline (29.2) solo `yard.timesheet` (avvio, stop, registrazione manuale) e
   `yard.activity_done` (avanzamento, checklist, foto): deduplica per `client_ref` più `Idempotency-Key` (INV-83),
   orari del dispositivo tra 48 h prima e 5 minuti dopo l'accodamento, l'avanzamento non diminuisce mai per una
   ripetizione, nessun effetto su allocazioni o prenotazioni (INV-71).
9. **Mezzi di sollevamento a slot (§11, D28–D31).** Le prenotazioni occupano slot di 15 minuti dell'ora locale
   (0..95, etichette dell'orologio: il giorno del cambio d'ora resta di 96 slot e la finestra predefinita 07:00–19:00
   esclude 02:00–03:00). `equipment_slots (equipment_id, date, slot)` e `operator_slots (employee_id, date, slot)` sono
   chiavi uniche: sono l'unica garanzia contro la doppia prenotazione (INV-17, ADR 0003); una violazione annulla la
   transazione e risponde `409 equipment_slot_taken` o `operator_busy`. Fuori servizio per manutenzione (`409`) e
   portata inferiore al dislocamento (`422`, mai derogabile) sono rifiuti. Le richieste nascono da commessa,
   contratto di rimessaggio, movimento e, se attivato, da un sink dell'outbox su `occupancy.moved` acqua ↔ terra
   (idempotente per allocazione). Prenotare richiede `yard.manage` o `storage.manage` (nuovo
   `Principal.AuthorizeAny`, stessa semantica 404 di `Authorize`).
10. **Planner come regole morbide (§6, D20–D23).** Qualifica e capacità sono controlli con deroga esplicita (`422
    qualification_mismatch`, `409 capacity_exceeded`), non invarianti; le dipendenze fine-inizio sì
    (`dependency_violation`). I carichi aggregati sono rapporti di somme; la capacità del cantiere ignora le chiusure
    della marina; la "postazione" è la posizione della commessa. La proposta automatica è un greedy deterministico
    che propone e non scrive mai.
11. **Magazzino rinviato (§1.2, D13).** La gestione delle giacenze di 18.6 e 02.3 (magazzini, carichi manuali o da
    CSV, rettifiche, regola di giacenza non negativa, lotti, inventario, riordino) esce da M10 per decisione
    dell'orchestratore e va confermata come rinvio della specifica: `stock_movements` contiene solo `issue` e
    `return` con costo e prezzo congelati; una migrazione successiva allargherà il CHECK e aggiungerà le giacenze.
12. **Privacy (D34).** Eventi, SSE e log portano id, enum, date e importi delle commesse: mai nomi di dipendenti,
    clienti o fornitori (INV-70). Il fornitore di un costo esterno è testo, senza anagrafica.

### Precisazioni del contratto

- Il report del cantiere è l'operazione `getYardReport` (`GET .../reports/yard`, parametro `view`) come i report di
  M9, non un `getReport` generico; i riquadri `yard_*` diventano disponibili quando ORDERS li implementa.
- Si aggiungono `listYardItems` (ricerca degli articoli di cantiere per il passo 2), `getTaskFileLink` e
  `getTaskFileFile` (foto delle attività con link firmato come M9), `listEquipmentOperators`; gli eventi interni
  `yard_catalog.changed`, `work_order.changed` e `yard_plan.changed` alimentano solo SSE (argomento `yard.changed`).
- `createWorkOrderBilling` con `dry_run=true` è l'anteprima (nessuna scrittura); le righe di budget di una commessa
  senza preventivo hanno `origin = manual`.

## Conseguenze

- Nuove tabelle in `00020`–`00022`, entrambi i dialetti, registrate nel backup; nessuna migrazione precedente
  cambia. INV-17, INV-28, INV-29, INV-93–INV-96 entrano in `docs/invariants.md` come `planned`.
- I pacchetti puri (`formula`, `yardquote/price`, `workorder/account`, `workorder/states.go`, `yardplan/load`,
  `yardplan/auto`, `lifting/slots`) sono nelle regole `pure-engines` di `.golangci.yml`.
- Il rinvio del magazzino richiede la conferma del responsabile della specifica; finché non arriva l'interfaccia
  dice "Giacenze: disponibili in seguito".
