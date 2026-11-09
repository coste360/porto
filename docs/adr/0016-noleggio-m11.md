# ADR 0016 — Noleggio (M11)

- **Stato:** proposta
- **Data:** 2026-11-09
- **Sezioni della specifica:** 19, 21, 26.1, 27, 29.2, 32 (M11); 05.2, 06.3, 06.8, 10, 11.2, 12.4, 15.4, 16.2–16.5

## Contesto

M11 porta il noleggio: flotta legata all'anagrafica delle imbarcazioni, skipper prenotabili, formule prezzate dai
listini di M2, planner unità × tempo, occupazione a slot, consegna e restituzione dal telefono, cauzione, incasso
rapido con documento commerciale, report e riquadri che M7 aveva riservato. Arenile non ha un dominio di noleggio:
l'unico schema da rileggere (righe per chiave con `ON CONFLICT DO NOTHING` e recupero degli hold scaduti) è già in
Porto (`occupancy`, `lifting`), quindi non c'è comportamento da riprodurre (ADR 0001). Dove la specifica tace le
decisioni sono in `docs/m11-plan.md` §18 (D1–D27); questo ADR registra quelle vincolanti.

## Decisione

1. **Slot di 30 minuti dell'ora locale (§4.1, D2, D6).** Uno slot `s` ∈ 0..47 è la mezz'ora `[s×30, (s+1)×30)` di
   una data locale nel fuso della marina; un intervallo va da `(data, slot)` incluso a `(data, slot)` escluso (slot
   finale 1..48). La finestra di apertura e ogni fascia escludono gli slot 4–5 (02:00–03:00), le sole etichette che
   il cambio d'ora toglie o ripete: orari, mezze giornate e giornate non le toccano mai; più giorni e settimana
   scrivono una riga per ogni etichetta e sono prezzati per data. La penale di ritardo usa i minuti reali.
2. **Nessuna doppia prenotazione per vincolo del DB (§4.3–§4.4, D8, D9, INV-18).** `rental_slots (unit_id, date,
   slot)` e `rental_skipper_slots (skipper_id, date, slot)` sono chiavi primarie e l'unica garanzia (ADR 0003): le
   righe si inseriscono con `ON CONFLICT DO NOTHING` a blocchi e si contano; un conteggio corto annulla la
   transazione e risponde `409 rental_slot_taken` o `skipper_busy` con le chiavi in conflitto. Il buffer configurabile
   dopo ogni noleggio è fatto di righe `buffer` dell'unità (non dello skipper), attraversa la mezzanotte e fa parte
   della garanzia; le fasce distano almeno il buffer. I blocchi (manutenzione, chiusura, riposo dello skipper) usano
   lo stesso percorso. Su PostgreSQL READ COMMITTED: prima i lock (prenotazione, unità, skipper) poi la verifica.
3. **Hold come la vendita online (§4.6, D10, INV-05).** "Opzione" del planner e `hold: true` dell'API scadono dopo
   `desk_hold_minutes`; al conflitto si eliminano tutte le righe delle prenotazioni `held` scadute che toccano il
   piano (recupero pigro, `slots_lost_at`), il job `rental.holds_expire` le annulla con `hold_expired`. Il canale
   `online` e la conferma da webhook (`source = provider`) sono agganci pronti per la vendita online.
4. **Stati in un solo punto (§6, INV-97).** `held → confirmed → checked_out → returned → closed`, più `cancelled` e
   `no_show`, dalla tabella pura `rental/states`; solo `rental/reservation` scrive stato, righe e scadenza dell'hold,
   con audit e outbox nella stessa transazione. Consegna e restituzione passano dai suoi metodi `*Tx`. Le righe dei
   noleggi restituiti e chiusi restano come storico; il rientro anticipato libera gli slot dopo il buffer senza
   rimborso, il ritardo li lascia invariati e addebita la penale.
5. **Prezzi solo dai listini (§5, D3, D4, D5).** Gli articoli di noleggio sono articoli con classe di ricavo
   `rental` (nessun nuovo tipo di articolo). Le regole base `flat` hanno le nuove unità `hour`, `half_day`, `day` (e
   `week`) e l'ambito `time_bands`; il motore puro `pricing.RentalQuote` le prezza per data (periodi tariffari diversi
   dentro un più giorni), settimana = settimane intere più giorni residui con tetto al prezzo di una settimana, orario
   = tariffa oraria × slot/2 half-even. Nessun modificatore, sconto o arrotondamento in M11. `EngineVersion` non
   cambia (il motore degli ormeggi ignora queste regole); lo snapshot salva `RentalEngineVersion = 1` (INV-20).
6. **Patente e skipper (§3.1–§3.2, D7, D9).** La patente serve con `always`, o con `threshold` quando la potenza
   totale supera la soglia (30 kW); mai con uno skipper. Gli orari settimanali dello skipper sono una regola morbida
   con deroga registrata in audit; i riposi sono blocchi.
7. **Consegna e restituzione dal telefono (§7–§8, D13, D14).** Identità e patente sono digitate dall'operatore con la
   foto del documento, cifrate (KeyRing) e cancellate dopo `identity_retention_days` dalla chiusura: il ruolo
   `rental` legge l'anagrafica ma non la scrive. Il contratto, composto alla conferma, si firma su schermo con il
   nuovo driver `screen` (firma elettronica semplice con pagina di evidenza, come l'OTP; la formulazione di D6 non
   cambia), oppure con OTP o su carta. La checklist, il carburante, le ore motore e le foto della barca sono l'unico
   tipo offline (`rental.checklist`); tutto il resto è online.
8. **Cauzione mai ricavo finché non trattenuta (§9.2, D15–D17, INV-98).** Contanti e POS sono pagamenti di tipo
   `deposit`; la preautorizzazione è un intent a cattura manuale (`purpose = rental_deposit`) tramite
   `ext.PaymentAuthorizer`, catturato in parte per la trattenuta o annullato; la rinuncia richiede un motivo. Alla
   restituzione la trattenuta proposta è `min(cauzione, Σ stime dei danni)` e crea l'addebito `deposit_retention`, il
   resto si restituisce; penale, carburante ed extra si incassano a parte, mai prelevati in silenzio dalla cauzione.
   Trattenere e restituire alla restituzione è consentito a `rental.manage`.
9. **Addebiti e incasso rapido (§9.1, §9.3, D18, D19, INV-99).** Un addebito principale (dallo snapshot, creato al
   primo incasso, alla consegna o al no-show) e uno di riconsegna per prenotazione, garantiti da un unique parziale;
   un riprezzo annulla e ricrea l'addebito intatto, altrimenti crea una rettifica collegata (gli addebiti esportati
   non cambiano, INV-51). Modalità scontrino con documento commerciale tramite `FiscalDriver`; fattura su richiesta
   (serve il cliente) verso l'esportazione. L'incasso rapido è un'unica operazione (contanti, POS, link di pagamento).
10. **Offline (§12, D21).** Solo `saveHandoverChecklist` e `uploadHandoverPhoto` sono `x-porto-offline`; non
    cambiano mai stato, righe, addebiti o cauzioni (INV-71) e una ripetizione ha un solo effetto (INV-83); un verbale
    già completato risponde `409 handover_locked`.
11. **Privacy (D24).** Eventi (`rental.changed` interno; `rental.confirmed`, `rental.cancelled`,
    `rental.checked_out`, `rental.returned` pubblici), SSE (`rental.changed`) e log portano id, enum e date: mai dati
    del cliente o dello skipper, numeri di documento o di patente (INV-70).
12. **Vendita online del noleggio rinviata (§1.2).** La "vendita online" di spec 32 M11 non è economica sopra M8:
    servono disponibilità pubblica, prenotazione con token, un nuovo scopo di intent con conferma da webhook, la
    pagina pubblica e le viste dell'area cliente. Esce da M11 (dopo M12) con gli agganci del punto 3; il rinvio va
    confermato dall'orchestratore e dal responsabile della specifica. Restano fuori anche i sensori (M12), la
    ripartizione dei ricavi delle unità di terzi, la firma avanzata e le penali di cancellazione.

### Precisazioni del contratto

- Il report del noleggio è l'operazione `getRentalReport` (`GET .../reports/rental`, parametro `view` =
  `utilisation`, `revenue`, `damages`, `deposits`) come i report di M9 e M10, non un `getReport` generico; i riquadri
  `rental_utilisation` e `rental_revenue` diventano disponibili quando FLEET li implementa.
- I parametri di percorso sono `rentalUnitId`, `rentalSkipperId`, `rentalFormulaId`, `rentalExtraId`,
  `rentalBookingId`, `rentalBlockId`, `rentalDamageId` e `phase` (`checkout`, `checkin`), per non collidere con
  `bookingId` della prenotazione online. I parametri di query obbligatori sono opzionali nel contratto (come
  `getRentalPlanner.from`), così un'altra marina risponde 404 prima della validazione.
- `transitionRentalBooking` accetta anche `extend` con `end` per prolungare un noleggio `checked_out` (riga
  `extend` della tabella degli stati); la modifica (`updateRentalBooking`) resta per `held` e `confirmed`. Il nuovo
  intervallo si valida con la formula della prenotazione, quindi una mezza giornata o una giornata (intervallo fisso
  della fascia o della finestra) non si allunga dentro la propria formula: `extend` accetta un `formula_id`
  facoltativo che deve prezzare il nuovo intervallo (per esempio oraria o più giorni) e riprezza tutto il noleggio
  con quella formula; senza, la risposta è `422 formula_range_mismatch`. In M11 `extend` è solo API (nessun pulsante
  in desktop o sul telefono): il ritardo al rientro è coperto dalla penale della formula.
- `updateRentalBooking` che cambia `invoice_requested` o il cliente porta con sé l'addebito principale: se è intatto
  (non esportato, non pagato, rettifiche intatte) si annulla (collegamento `replaced`) e si ricrea con la nuova
  modalità fiscale e il nuovo cliente; se è già pagato o esportato il cambio di `invoice_requested` è rifiutato
  (validazione `invoice_requested` `charge_paid`, o `charge_exported`), così un noleggio non mescola scontrino e
  fattura. Lo stesso annulla e ricrea vale per riprezzature e annullamenti della prenotazione (punto 9): mai una
  coppia +X / -X verso l'esportazione per un addebito che non è uscito. L'hook di annullamento degli addebiti di
  origine `rental` marca `replaced` anche l'addebito principale o di riconsegna annullato dall'ufficio (e annulla
  le sue rettifiche intatte, o rifiuta con `charge_immutable`), così la prenotazione si può addebitare di nuovo.
  Un link di pagamento aperto sull'addebito annullato resta quello generico di M5: se il cliente paga comunque, il
  pagamento resta credito del cliente.
- `putRentalSettings` accetta `If-Match: "0"` al primo salvataggio (impostazioni mai salvate, versione 0).
- La preautorizzazione richiede un cliente della prenotazione: `payment_intents.customer_id` è obbligatorio dal
  M5, quindi un noleggiatore occasionale senza cliente versa la cauzione in contanti o con POS (`422
  customer_required` sulla preautorizzazione). Lo stesso vale per il link di pagamento dell'incasso rapido (la
  pagina di pagamento di M5 appartiene a un cliente): per un noleggiatore occasionale il metodo `link` risponde
  `422 customer_required` e telefono e desktop lo mostrano disattivato con la spiegazione; si incassa in contanti o
  con POS, oppure si collega prima un cliente alla prenotazione.
- La pagina di preautorizzazione e il link di pagamento tornano anche come QR (`checkout_qr_svg`, `link_qr_svg`,
  disegnati da `internal/qrsvg`), che il telefono mostra perché il cliente li inquadri con il proprio telefono.
- Alla preautorizzazione si trattiene solo quanto deciso al rientro (`deposit_decision_cents`): un evento di
  addebito senza importo, o con un importo maggiore, è riportato alla decisione (INV-98).
- La cauzione dovuta alla consegna e il suo importo predefinito sono quelli della prenotazione (copiati dall'unità
  alla creazione), non quelli correnti dell'unità. Un cambio di unità con `updateRentalBooking` non aggiorna la
  cauzione della prenotazione (colonna della consegna): si indica l'importo alla presa della cauzione.
- Un addebito principale annullato e ricreato resta in `rental_booking_charges` come `replaced`, così l'unique
  parziale su `main` e `return` vale senza cancellare righe.

## Conseguenze

- Nuove tabelle in `00023_rental_fleet`, `00024_rental_bookings`, `00025_rental_handovers`, entrambi i dialetti,
  registrate nel backup; i CHECK di `number_sequences.series`, `payment_intents.purpose`,
  `signature_requests.subject_type`/`driver` e `generated_documents.subject_type` si allargano; nessuna migrazione
  precedente cambia. INV-18, INV-19, INV-97, INV-98 e INV-99 entrano in `docs/invariants.md` come `planned`.
- I pacchetti puri `rental/price`, `rental/slots` e `rental/states` sono nelle regole `pure-engines` di
  `.golangci.yml`; `pricing.RentalQuote` vive nel pacchetto puro `pricing`.
- `collect` chiama il noleggio solo tramite `collect.RentalDepositHooks` e `signature` tramite il soggetto `rental`:
  nessuno dei due importa `rental`.
- Il rinvio della vendita online richiede la conferma del responsabile della specifica.
