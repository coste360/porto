# Invarianti di dominio

Proprietà che devono valere **sempre**, qualunque sia la sequenza di operazioni. Stesse regole di Arenile: ogni
invariante indica il test che la verifica e lo stato (`planned`, `partial`, `implemented`); i numeri non si riusano;
un cambiamento su occupazione, prezzi, contratti, addebiti o pagamenti deve lasciarle valide tutte
(subagent `invariants-guardian`). I numeri sono raggruppati per area (01–19 occupazione, 20–29 prezzi, 30–39
contratti, 40–49 diritti e condominio, 50–59 addebiti e incassi, 60–69 report, 70+ altri).

## Occupazione (spec 10)

**INV-01 — Nessuna doppia occupazione di una risorsa.** Per ogni `(berth_id, night)`, `(area_id, cell, night)`,
`(resource_id, night)` esiste al più una riga, anche con 100 richieste parallele, su SQLite e PostgreSQL. Garantita
dai vincoli unique, non da controlli applicativi; vale per ogni canale (banco, online, API, canali esterni).
Verificata da: `TestConcurrency_BerthAllocation`, `TestConcurrency_CellAllocation`,
`TestConcurrency_CellAllocationSurface`, `TestConcurrency_LandAllocation`, `TestConcurrency_ChannelVsDesk`,
`TestMigrations_OccupancyPrimaryKeys` (le tre chiavi primarie e le FK RESTRICT su entrambi i dialetti), query Q1
di `store.CheckOccupancyInvariants`. Stato: implemented.

**INV-02 — Celle disgiunte implicano barche fisicamente separate.** Se due posizionamenti sulla stessa area hanno
insiemi di celle disgiunti, i loro ingombri (con margini) non si intersecano. Verificata da:
`TestFootprint_DisjointCellsImplyNoOverlap` (property test), con `TestFootprint_ParityVectors` per la parità Go ↔
TypeScript. In M1 vale per `layout.Footprint`; da M3 anche per le allocazioni: `TestRapid_SolutionsAreFreeAndFit`,
`TestConcurrency_CellAllocationSurface`, la query Q15 e l'oracolo cella per cella di
`TestAcceptance_M3_SeasonSimulation` (aree lineari e di superficie, spostamenti in area, sessioni di layout e
compattazione). Stato: implemented.

**INV-03 — Ogni allocazione è coerente con le sue righe.** Per ogni allocazione attiva le righe di occupazione sono
esattamente quelle prodotte da `Footprint × notti` della sua posizione e del suo periodo. Verificata da:
`TestAllocation_RowsMatchFootprint` (property test, tag `integration`), `TestPlan_RowsAreFootprintTimesNights`,
`TestRapid_PlanExcludesReleased`, query Q15 di `store.CheckOccupancyInvariants`. Stato: implemented.

**INV-04 — Nessuna mappa salvata ha posti sovrapposti.** Un solo percorso di scrittura (`applyDocument`) valida lo
stato completo della mappa dopo il salvataggio dentro la stessa transazione, serializzata sulla versione della
mappa (m1-plan §3.3). Rettangoli di posti e risorse a terra più stretti di `layout.MinRectSide` (2 cm) sono
rifiutati come `invalid_geometry`, perché sotto quella misura il controllo di sovrapposizione con tolleranza non
è definito. Verificata da: `TestValidateMap_RejectsOverlap`, `TestGenerator_NoOverlap`,
`TestHarbor_SavedMapsNeverOverlap`, `TestHarbor_SingleWritePath`, `TestAcceptance_M1_RejectsOverlappingSave`,
`TestConcurrency_MapSaveSingleWinner`. Stato: implemented.

**INV-05 — Hold scaduti non bloccano le vendite.** Un hold con `expires_at < now` viene ignorato e ripulito al
momento del conflitto anche se il job non è passato. Il recupero non scade mai un hold convertito nel frattempo
(conferma o attivazione concorrente): l'aggiornamento ricontrolla tipo `hold` e scadenza e cancella le righe solo
delle allocazioni effettivamente scadute. Verificata da: `TestOccupancy_ExpiredHoldIsReclaimed`,
`TestConcurrency_ExpiredHoldReclaim`, `TestConcurrency_ConvertVsReclaim` (conferma a T−ε contro allocazione a T+ε
sullo stesso posto, 100 goroutine, SQLite e PostgreSQL, `-race`); M8: `TestConcurrency_ConfirmVsExpireOnline`
(webhook tardivo con `late_payment_recovery` contro `contracts.expire` e transiti di banco sullo stesso posto e
notti: al più un vincitore, rimborso integrale del pagamento tardivo se vince il banco). Stato: implemented.

**INV-06 — Spostamenti e ipotesi sono atomici.** Uno spostamento, uno scambio o l'applicazione di un'ipotesi o la
modifica completa o lascia tutto invariato. Verificata da: `TestConcurrency_SwapVessels`,
`TestConcurrency_LayoutSaveSingleWinner`, `TestOccupancy_LayoutSaveExchangePlaces` (M3);
`TestScenarioApply_AllOrNothing` (ipotesi, M9: iniezione di errori a ogni scrittura con trigger di test, lo stato
di allocazioni, righe di occupazione, ipotesi, lista degli spostamenti, audit e outbox resta identico),
`TestConcurrency_ScenarioApply` (M9: esattamente un'ipotesi vince e lo stato reale è il suo layout per intero,
righe di cella = ingombro × notti; SQLite di test su file WAL come in produzione), `TestScenarioApply_TrayItemRepriced`
(il prezzo delle barche portate dal vassoio è applicato nella stessa transazione) (M9). Stato: implemented.

**INV-07 — Compatibilità rispettata o derogata esplicitamente.** Ogni allocazione attiva supera `fit.Check` senza
bloccanti oppure ha `fit_override` con utente e motivazione (per una chiave API: la chiave e l'utente che l'ha
creata). Fanno eccezione le allocazioni `maintenance` senza imbarcazione (M9, `occupancy/allocate.go`: non c'è una
barca da confrontare, vedi INV-92). Da M9 l'ottimizzatore conserva una barca derogata (una bocciatura derogabile non la
esclude, `support_not_allowed` sì). Verificata da: `TestAllocation_FitOrOverride`, query Q14 di
`store.CheckOccupancyInvariants` (deroga senza motivazione o senza autore); da M9 `TestArrange_FitOverride` e
`TestScenarioApply_FitOverriddenBoat`. Stato: implemented.

**INV-08 — Una prenotazione online occupa sempre una risorsa concreta.** Ogni transito in `held`, `pending_payment`,
`awaiting_approval` (M8) o `confirmed` ha almeno un'allocazione attiva; solo un transito ancora `held` può avere invece un'allocazione
`expired` finché il job non lo chiude (un hold scaduto recuperato al momento del conflitto, INV-05), e la conferma
di un hold perso rialloca o fallisce con `resource_taken`. Ogni preventivo `draft` o `sent` ancora valido ha un hold
attivo per ogni posizionamento delle opzioni con opzione; un hold di preventivo spostato resta l'hold dell'opzione.
Verificata da: `TestContract_HoldTransitHasAllocation` (banco e API, M3), `TestInvariants_Q8QuoteHold`, query Q8;
`TestAcceptance_M8_OnlineBookingHappyPath`, `TestCheckout_HoldHasAllocation`, `TestApproval_AuthorizeCaptureConfirm`,
`TestAcceptance_M8_ApprovalFlow` (online e `awaiting_approval`, M8; query Q8 estesa ad `awaiting_approval`),
`TestBooking_CreateHoldsConcreteResource` (un posto concreto della categoria per ogni notte, hold con la scadenza
della prenotazione) e `TestBooking_RetryNextSolutionOnConflict` (un tentativo perso torna indietro per intero e si
prenota la soluzione successiva). Una prenotazione approvata dal banco che
resta `awaiting_approval` (cattura o conferma manuale in attesa) ha l'hold esteso alla finestra del link di
pagamento e il job della scadenza approvazione non la rifiuta mai (lock poi ricontrollo di `approved_at`):
`TestApproval_ApprovedNotTimedOut`, `TestConcurrency_ApproveVsDeadline`. Stato: implemented.

**INV-09 — Conferma solo dal pagamento.** Un transito online passa a `confirmed` solo dal webhook del provider o dal
driver manuale. Da M5 un transito con link di pagamento è `pending_payment` e arriva a `confirmed` solo tramite
`confirmTx`, chiamato dal webhook del provider (`ConfirmTransitPaidTx(source=provider)`) o dal driver manuale; il
ritorno del browser non conferma mai. Verificata da: `TestContract_TransitConfirmOnlyManualDriver`,
`TestContract_ConfirmedOnlyInConfirmTransit` (la tabella degli stati entra in `confirmed` solo da `held` o
`pending_payment` con `confirm`); `TestPaymentWebhook_ConfirmsTransit` (che verifica
anche che il ritorno del browser non conferma; webhook, M5; checkout online M8). M8 (ADR 0013): `confirmTx` entra in
`confirmed` anche da `awaiting_approval` (webhook della cattura o driver manuale) e, con `confirm_late`, da `expired`
(solo webhook del provider per un transito online con `late_payment_recovery`); una prenotazione verificata di un
canale esterno segnata come pagata al canale (firma del webhook o pull autenticato) conferma con `source = channel`,
perché il canale è il merchant of record. Nessun'altra via: il ritorno del browser non conferma mai. Verificata anche
da: `TestCheckout_BrowserReturnDoesNotConfirm`, `TestCheckout_ConfirmOnlyFromWebhook`,
`TestChannel_ApplyNewPaidConfirms`, `TestCheckout_LatePaymentRecoveredOrRefunded`, `TestApproval_AuthorizeCaptureConfirm`,
`TestConcurrency_ConfirmVsExpireOnline` (M8). Stato: implemented.

**INV-10 — Le allocazioni di una riga di contratto non si sovrappongono e coprono il periodo.** Per ogni riga di
risorsa di un contratto o transito vivo ogni notte del periodo è coperta da esattamente un'allocazione attiva
(salvo le notti troncate da partenza o cessazione); le bozze non sono controllate (`hold_lost`) e un transito
`held` è esente solo se ha un'allocazione `expired` senza un successore attivo (hold recuperato non ancora chiuso
dal job). Il collegamento di un rilascio rivenduto (Q16) richiede solo la stessa risorsa e lo stesso porto:
l'allocazione che ha rivenduto può poi essere troncata o spostata. Verificata da: query Q6 e Q7 di
`store.CheckOccupancyInvariants`, `TestInvariants_Q7AfterHoldLapse`, `TestContract_ConfirmAfterLapseWithIntervalMove`
e `TestContract_ActivateAfterLapseWithIntervalMove` (conferma o attivazione dopo la perdita degli hold di una riga
con più allocazioni: ogni allocazione scaduta che chiude una catena viva è riallocata una volta),
`TestAllocation_RowsMatchFootprint`, `TestAcceptance_M3_SeasonSimulation`. Stato: implemented.

**INV-11 — Le catene `moved_from_id` sono ordinate.** Un'allocazione attiva che deriva da un'altra
(`moved_from_id`) ha la stessa riga di contratto e la stessa imbarcazione e, se il predecessore è ancora attivo,
comincia dopo la sua ultima notte; nessuna operazione (spostamento, spostamento a intervallo, estensione,
ridimensionamento, annullamento) porta un'allocazione prima di un predecessore attivo o dopo l'inizio di un successore
attivo (`ResizeTx` rifiuta questi intervalli come `validation_failed`; l'annullamento di uno spostamento che
restituirebbe a una sorgente notti prese nel frattempo da un suo vicino di catena è `ErrUndoUnavailable`). Verificata
da: query Q12 di `store.CheckOccupancyInvariants`, `TestOccupancy_UndoKeepsChainOrdered`,
`TestAllocation_RowsMatchFootprint` (property test, anche con `-rapid.checks=2000`). Stato: implemented.

**INV-12 — Le quote online sono contatori nel database.** Per ogni `(gruppo, notte)` di `online_quota_usage`:
`used` = numero delle righe di `online_quota_claims` e `0 <= used <= cap`, anche con 100 prenotazioni online
parallele, su SQLite e PostgreSQL. Garantita dal CHECK `online_quota_usage_limit_check` e dall'incremento
condizionale (`UPDATE … SET used = used + 1 WHERE used < cap`, zero righe = quota esaurita), mai da un controllo
applicativo; solo i canali `online` ed `external` consumano quota, banco e API la ignorano. Le quote seguono il
contratto: annullamento, scadenza, rifiuto, no-show e accorciamento le rilasciano; gli spostamenti dal banco no.
Verificata da: `TestMigrations_M8Constraints`, `TestConcurrency_OnlineQuotaNeverExceeded`, query Q19 di
`store.CheckOccupancyInvariants`, `TestBooking_AvailabilityFiltersOnlineSellableAndQuota`,
`TestBooking_HoldExpiryReleasesQuotaAndMarksLost`, `TestPortal_ChangeDatesQuotaAndPrice` (il cambio date sposta le
quote con il soggiorno e le rifiuta oltre il cap), `TestChannel_CancelReleases` (l'annullamento del canale rilascia
quote e notti). Stato: implemented.

**INV-13 — Nessuna doppia vendita tra canali.** Su ogni `(risorsa, notte)` vince al più una tra banco, prenotazione
online e canale esterno, perché tutti passano da `occupancy.AllocateTx` (INV-01); chi perde riceve
`resource_taken`, `booking_unavailable` o il rifiuto del canale, e il numero dei vincitori è quello delle
risorse-notte libere consumate. Verificata da: `TestConcurrency_NoDoubleSaleDeskOnlineChannel` (120 goroutine,
entrambi i dialetti, `-race`; le prenotazioni online e dei canali passano per il motore e le quote, quelle del banco
con il posizionamento esplicito). Stato: implemented.

**INV-14 — Le ipotesi non toccano lo stato reale.** Creare, salvare, ottimizzare, confrontare o scartare
un'ipotesi di rimessaggio non modifica `allocations` né alcuna riga di occupazione: l'ipotesi vive solo in
`storage_scenarios`, `scenario_items` e `scenario_moves`; solo l'applicazione (INV-06) scrive lo stato reale, con
`occupancy.LayoutTx`. Verificata da: `TestScenario_NoRealWrites` (M9). Stato: implemented.

**INV-15 — L'ottimizzatore produce solo disposizioni valide e riproducibili.** Ogni disposizione restituita da
`optimizer.Arrange` supera il controllo indipendente `optimizer.Validate` (ingombri dentro le celle utilizzabili,
nessuna cella bloccata, celle disgiunte tra barche con soggiorni sovrapposti e rispetto agli ostacoli, barche
bloccate ferme, compatibilità rispettata); con lo stesso seme e un budget di iterazioni non interrotto dalla
scadenza il risultato è identico. Verificata da: `TestRapid_ArrangeValid`, `TestArrange_Deterministic`,
`TestArrange_Acceptance2000m2` (capannone di 2.000 m² con 40 barche: nessuna barca fuori, nessuna uscita
bloccata, meno di 3 s) (M9). Stato: implemented.

**INV-16 — Ogni contratto di rimessaggio ha i suoi dettagli e occupa una superficie.** Ogni contratto `storage`
ha la sua riga di `storage_contract_details`; ogni contratto `storage` attivo ha allocazioni attive su un'area di
superficie (o un posto carrello) che coprono ogni notte del suo periodo. Le bozze ricevono la posizione alla
creazione (D4: senza posizione indicata la sceglie `PlaceOne`, senza spazio 409), verificato da
`TestStorage_CreateContractAutoPlace` e `TestStorage_NoSpace409`. Verificata da: query Q20 di
`store.CheckStorageInvariants` (M9). Stato: implemented.

**INV-17 — Nessuna doppia prenotazione dei mezzi di sollevamento né dell'operatore.** Uno slot di 15 minuti di un
mezzo (travel lift, gru, muletto, carrello) appartiene al più a una prenotazione, e lo stesso vale per l'operatore:
le chiavi uniche di `equipment_slots (equipment_id, date, slot)` e `operator_slots (employee_id, date, slot)` sono
l'unica garanzia (ADR 0003); una violazione annulla la transazione e risponde `409 equipment_slot_taken` (o
`operator_busy`), mai un controllo preventivo. Le righe di slot di una prenotazione coincidono con la sua fascia; una
richiesta o una prenotazione annullata non ha righe di slot. Verificata da: `TestConcurrency_EquipmentSlots`,
`TestMigrations_M10Constraints`, `TestAcceptance_M10_Lifting`, query Q31 di `store.CheckLiftingInvariants` (M10).
Stato: implemented.

**INV-18 — Nessuna doppia prenotazione di un'unità a noleggio né di uno skipper.** Uno slot di 30 minuti di
un'unità (con il buffer che segue ogni noleggio) appartiene al più a una prenotazione o a un blocco, e lo stesso vale
per lo skipper: le chiavi primarie di `rental_slots (unit_id, date, slot)` e `rental_skipper_slots (skipper_id,
date, slot)` sono l'unica garanzia (ADR 0003); le righe si inseriscono con `ON CONFLICT DO NOTHING` e si contano, un
conteggio corto annulla la transazione e risponde `409 rental_slot_taken` (o `skipper_busy`), mai un controllo
preventivo. Verificata da: `TestConcurrency_RentalUnitSlots`, `TestConcurrency_RentalSkipperSlots`,
`TestAcceptance_M11_NoDoubleBooking`, `TestMigrations_M11BookingConstraints` (chiavi e CHECK di prenotazioni e slot)
e `TestMigrations_M11Constraints` (vincoli di flotta, collegamenti agli addebiti, consegne e CHECK estesi da 00025)
(M11). Stato: implemented.

**INV-19 — Le righe di un noleggio coincidono con il suo intervallo.** Le righe di unità di una prenotazione sono
esattamente `slots.Expand(intervallo, buffer)` (troncate in caso di rientro anticipato), quelle dello skipper
l'intervallo senza buffer; una prenotazione annullata o `no_show` non ha righe e solo una prenotazione `held` ha righe
`hold`. Verificata da: query Q32 di `store.CheckRentalInvariants` (eseguita dopo ogni scenario delle suite di
`reservation` e dai test di concorrenza), `TestRapid_RentalSlotsExpand` (M11). Stato: implemented.

## Prezzi (spec 12)

**INV-20 — Snapshot riproducibile.** Ricalcolare un `price_snapshot` con la stessa `engine_version` e gli stessi
input restituisce lo stesso totale. Verificata da: `TestQuote_SnapshotReproducible` (property test,
`pricing.Recompute(pricing.NewSnapshot(...))` uguale dopo il giro JSON). Dal M11 vale anche per gli snapshot dei
noleggi (`rental_bookings.price_snapshot` con `RentalEngineVersion`): `TestRentalQuote_Recompute` (giro JSON del
`RentalBreakdown`), `TestRentalQuote_Purity` (ordine di regole e articoli ininfluente) e `TestFleet_QuoteAndSnapshot`
(lo stesso `SnapshotTx` sullo stesso listino dà lo stesso totale). Stato: implemented.

**INV-21 — Il motore prezzi è puro.** `internal/domain/pricing` non importa `store`, `clock`, `time.Now`. Verificata
da: regole `pure-engines` e `pure-engines-no-rand` (anche `math/rand` e `math/rand/v2`) di `depguard` in
`.golangci.yml` e `internal/domain/pricing/purity_test.go`. Stato: implemented.

**INV-22 — Listini pubblicati immutabili.** Un listino `active` o `archived` non cambia regole, periodi, fasce né
impostazioni; cambia solo stato verso `archived` e `valid_to` per troncamento. Verificata da:
`TestPriceList_PublishedIsImmutable` e `TestMigrations_PriceListImmutable` (trigger su entrambi i dialetti).
Stato: implemented.

**INV-26 — Catalogo dei listini pubblicati stabile.** Per ogni listino `active` o `archived`
`content_sha256 = CatalogHash(LoadCatalog(listino))` per sempre: all'attivazione i campi degli articoli letti dal
motore (codice, tipo, unità, IVA o natura, prezzo IVA inclusa, codice della classe di ricavo, codice esterno,
attivo) sono copiati in `price_list_items`, immutabile come il listino (INV-22), e i listini pubblicati prezzano
con quella copia. Articoli e classi di ricavo restano modificabili: le modifiche valgono per le bozze (e quindi per
copie e rinnovi), mai per i listini pubblicati. Verificata da: `TestPriceList_PublishedCatalogStable`,
`TestMigrations_PriceListImmutable`. Stato: implemented.

**INV-23 — Matrice = simulatore.** Ogni cella della matrice è il risultato del motore sul suo input di prova,
identico a una simulazione dello stesso input. Verificata da: `TestMatrix_EqualsQuote`,
`TestPricing_MatrixEqualsSimulate`, e2e `pricing-acceptance`. Stato: implemented.

**INV-24 — Un solo listino attivo per notte e tipo di contratto.** Verificata da:
`TestConcurrency_PriceListActivation`, `TestPriceList_ActivateOverlapRejected`. Stato: implemented.

**INV-25 — Lo scorporo IVA somma il totale.** Per ogni breakdown `Σ imponibile + Σ IVA = totale`, per riga e per
aliquota. Verificata da: `TestRapid_VATSumsExactly`. Stato: implemented.

**INV-27 — Il totale è il preventivo più le rettifiche.** Per ogni contratto o transito con `price_snapshot`
`total_cents = Breakdown.Total + Σ(new_cents − old_cents)` delle `adjustments` dello snapshot. Uno spostamento
riprezzato aggiunge una rettifica `move`; il suo annullamento aggiunge la rettifica opposta `move_undo` (con audit
`contract.reprice`/`transit.reprice` `undo: true` ed evento `*.changed`) nella stessa transazione, così il totale
torna quello di prima dello spostamento; un cambio di soggiorno riprezza da capo. Un riprezzamento senza snapshot o
che porterebbe il totale sotto zero è rifiutato (`409 conflict`). Verificata da: `TestContract_TotalMatchesSnapshot`
(property test su sequenze di spostamenti, riprezzamenti, annullamenti e cambi di soggiorno, tag `integration`),
`TestContract_MoveReprice`. Stato: implemented.

**INV-28 — Un preventivo di cantiere è riproducibile dal suo snapshot.** `yardquote/price.Recompute(snapshot)`
restituisce esattamente le righe e i totali salvati di ogni preventivo di cantiere: le versioni dei gruppi di
lavorazioni sono solo in inserimento (trigger su entrambi i dialetti) e lo snapshot congela variabili, tabelle,
formule, prezzi unitari e IVA; modificare in seguito gruppi, variabili, tabelle o listini non cambia un preventivo
esistente ("Ricalcola" è esplicito e solo sulle bozze). Verificata da: `TestRapid_QuoteRecompute`,
`TestYardQuote_OldQuoteKeepsFormula`, `TestMigrations_M10Constraints` (M10). Stato: implemented.

**INV-29 — Le formule sono deterministiche e non vanno in panico.** `internal/domain/formula` è puro (niente store,
orologio, casualità né float): la stessa formula sullo stesso ambiente dà sempre lo stesso valore, qualsiasi input
produce un risultato o un errore tipizzato, mai un panic, e un programma che supera il controllo dei tipi non dà mai
`type_mismatch` in valutazione. Verificata da: `FuzzParse`, `FuzzEval`, `TestFormula_Purity`, regole `pure-engines`
di `.golangci.yml` (M10). Stato: implemented.

## Contratti (spec 13)

**INV-30 — Un solo punto di cambio di stato.** Ogni cambio di stato di contratti e transiti passa da
`internal/domain/contract` e scrive audit ed evento nella stessa transazione. Verificata da:
`TestContract_StateChangeWritesAuditAndOutbox`, `TestContract_TransitionTable` (le tabelle di stato di
`internal/domain/contract/states.go`); le righe M4 da `TestContract_M4StateChangesWriteAuditAndOutbox` (resume,
request_signature, signature_failed, addendum, change_vessel), `TestContract_SuspendTerminateAmounts` (suspend),
`TestQuote_AcceptRunsAllSteps` (sign), `TestContract_ExpireMarksRenewed` (expire, expire_renewed) e
`TestContract_TacitRenewal` (tacit_renew); le righe M5 da `TestContract_M5StateChangesWriteAuditAndOutbox`
(request_payment, payment_expired, pending_payment → confirmed/cancelled/expired, suspend per morosità con
`SuspendForArrearsTx`); le righe M8 da `TestContract_M8StateChangesWriteAuditAndOutbox` (create e update dalla
pagina pubblica, conferma dal provider e dal canale con attore `channel:<codice>`, payment_authorized, approve,
reject, expire dal job `bookings.approval_expire`, change_stay e cancel dall'area cliente con attore
`customer:<id>`, rollback che non lascia né audit né evento); le righe M9 dei contratti di rimessaggio da
`TestContract_M9StorageStateChangesWriteAuditAndOutbox` (create in bozza, `storage.details_update`, activate,
terminate, cancel). Stato: implemented.

**INV-31 — Le rate sommano il totale.** Per ogni piano rate `Σ rate = totale del contratto` al centesimo (sulle
rate vive, `status <> 'cancelled'`; con rate già emesse o pagate, il ripiano copre `totale − Σ emesse/pagate`); lo
stesso vale per le rate dei titolari rispetto alla loro riga (oneri, condominio). Verificata da:
`TestInstallmentPlan_SumsToTotal` (property test), `TestImport_ContractsResidualPlanAndUndo`,
`TestAddendum_PriceChangeReplans`, `TestConcurrency_ReplanVsSettle` (ripiani in parallelo a saldi e al job di
emissione: ordine dei lock contratto → rate) e la query C31 di `store.CheckContractInvariants`. Per le rate dei
titolari: la query C31h (ogni riga oneri emessa e non esclusa = Σ rate vive delle sue fonti; ogni coppia (riparto
condominiale, cliente) = Σ dei suoi riparti; si saltano le fonti con rate di un titolo annullato), eseguita dai test
dei titoli (`TestTransfer_CompleteMovesEverything`, `TestRight_HolderSharesWritePaths`), da
`TestInvariants_C31hHolderInstallments` (la query rileva le derive), da `TestSeed_DemoM4Coherent` e da
`TestConcurrency_TransferVsHolderSettle` (cessioni reassign/prorata in parallelo a saldi ed emissioni); emissione e
saldo delle rate dei titolari prendono il lock del titolo (ordine titolo → rate dei titolari,
`TestRights_HolderIssueAndSettleWaitForRightLock`) e la cessione fallisce con 412 se una rata letta non è più
programmata. Stato: implemented.

**INV-32 — Rate pagate solo dal saldo.** Una rata (di contratto o di titolare) diventa `paid` solo tramite
l'abbinamento di un pagamento al suo addebito (`ApplyChargeSettlementTx` → `settleTx(source=payment)`, M5) o, per
le rate storiche saldate a mano in M4, tramite il backfill che crea addebito e pagamento (`source = backfill`,
`settlement_source = manual`), oppure quando una rettifica negativa compensa per intero il suo addebito (offset di
`applyNegativeTx` → `FollowChargeOriginTx`, `settlement_source = payment` senza pagamento: il denaro è la nota di
rettifica, non un incasso); il CHECK `installments_paid_check` / `holder_installments_paid_check` richiede la
fonte del saldo. Verificata da: `TestInstallment_SettleOnlyManualDriver` (aggiornato in M5: pagata ⇔ pagamento o
backfill), `TestConcurrency_InstallmentSettle`, `TestCharge_BackfillM4Idempotent`, `TestMigrations_M4Constraints`.
Stato: implemented.

**INV-33 — Una sola proposta di rinnovo aperta per contratto.** Vincolo del database: indice univoco parziale
`renewal_proposals_open_uidx` su `source_contract_id` per gli stati aperti. Verificata da:
`TestConcurrency_RenewSameContract`, `TestMigrations_M4Constraints`. Stato: implemented.

**INV-34 — Contratto attivo dopo firma ha il documento firmato.** Ogni contratto passato da `awaiting_signature` e
`active` ha `signed_document_id` che punta a un documento `ready` e `signed` il cui SHA-256 corrisponde al file
decifrato; i documenti firmati sono immutabili (trigger). Verificata da: `TestConcurrency_SignVsCancelContract`,
`TestDocs_SignedNeverRerendered`, query Q18 di `store.CheckContractInvariants`. Stato: implemented.

**INV-35 — Rinnovato solo con successore attivo.** Un contratto `renewed` ha un successore (`renewal_proposals.
new_contract_id` o rinnovo tacito) `active` o in uno stato successivo. Verificata da:
`TestRenewal_SimpleHoldsBerthAndExpiry`, `TestContract_ExpireMarksRenewed`, `TestContract_TacitRenewal`,
`TestRenewal_StaleSourceRefused`, `TestConcurrency_RenewVsTacit`, `TestAcceptance_M4_RenewalCampaign300`, query C35
di `store.CheckContractInvariants`. Stato: implemented.

**INV-36 — Una sola richiesta di firma aperta per soggetto.** Vincolo del database: indice univoco parziale
`signature_requests_open_uidx`; una seconda apertura è `ErrSignatureOpen`. Verificata da:
`TestSignature_OpenTwiceIsSignatureOpen`, `TestMigrations_M4Constraints`. Stato: partial (manca un
`TestConcurrency_RequestSignatureSameSubject`).

## Diritti d'uso e condominio (spec 14)

**INV-40 — Quote dei titolari complete.** Per ogni diritto attivo la somma delle quote dei titolari è 1000 millesimi.
Controllata nella transazione di ogni scrittura (ogni quota è 1..1000 per CHECK). Verificata da:
`TestRight_HolderSharesSumTo1000` (property test), `TestRight_HolderSharesWritePaths` (creazione, modifica dei
titolari, cessione) e la query C40 di `store.CheckContractInvariants`, eseguita al termine dei test di
`internal/domain/rights` e da `TestSeed_DemoM4Coherent`. Stato: implemented.

**INV-41 — Tabelle millesimali complete.** Una tabella usata in una ripartizione somma a 1000: si attiva solo a
Σ = 1000 e le righe di una tabella attiva non cambiano (trigger su entrambi i dialetti). Verificata da:
`TestCommonCharges_RejectsIncompleteTable`, `TestMillesimal_ActiveImmutable`. Stato: implemented.

**INV-42 — Ripartizione esatta.** La somma degli importi ripartiti è uguale al totale della voce (resto maggiore),
per voce, per posto sui titolari e per l'intera ripartizione; i posti senza diritto attivo ricevono la quota del
marina (`customer_id` nullo). Verificata da: `TestCommonCharges_AllocationSumsExactly` (property test),
`TestCommonCharges_BalanceEqualsActualMinusAdvances`, `TestAcceptance_M4_CommonCharges400`. Stato: implemented.

**INV-43 — Ogni diritto attivo occupa il suo posto.** Ogni diritto attivo ha righe `owner` per ogni notte in
`[max(from_on, prima notte scritta), to_on]` tranne le notti dei mandati di gestione (query Q17 di
`CheckOccupancyInvariants`). Verificata da: `TestRight_OwnerOccupancy`, `TestAcceptance_M4_CommonCharges400`.
Stato: implemented.

**INV-44 — Ripartizioni emesse immutabili.** Le righe di `common_charge_allocations` di una ripartizione emessa e
di `management_fee_lines` di un calcolo oneri emesso non cambiano (trigger su entrambi i dialetti). Verificata
da: `TestMillesimal_ActiveImmutable`, `TestMigrations_M4Constraints`. Stato: implemented.

## Addebiti e incassi (spec 16)

**INV-50 — Saldo coerente.** Per ogni addebito `paid_amount_cents = Σ payment_matches.amount_cents` dell'addebito
(gli storni sono righe negative) e per ogni pagamento `allocated_cents = Σ` dei suoi abbinamenti, aggiornati nella
stessa transazione (solo `internal/domain/payment`; gli abbinamenti accettano solo inserimenti, salvo la crescita di
`reversed_cents` (`00017`, INV-52), trigger su entrambi i dialetti). I rimborsi escono solo dal denaro non abbinato, quindi "Σ incassi − Σ rimborsi" di un addebito è il suo
abbinato (m5-plan §4.1). Verificata da: `TestCharge_PaidAmountMatchesLedger` (property test, tag `integration`),
query B50 di `store.CheckBillingInvariants`. Stato: implemented.

**INV-51 — Addebiti esportati immutabili.** Un addebito `exported` o `invoiced` non cambia cliente, origine, importi,
date di emissione e competenza, modalità fiscale né righe; lo stato di fatturazione non torna mai `to_invoice` e un
addebito annullato non cambia più (trigger `charges_immutable*`, `charge_lines_immutable*` su entrambi i dialetti;
scadenza e descrizione restano modificabili; le righe di un addebito annullato, anche `to_invoice`, non si
inseriscono, modificano o cancellano più). Si corregge con un addebito di rettifica collegato
(`corrects_charge_id`, delta o storno e sostituzione) che va nella prossima esportazione. Verificata da:
`TestCharge_ExportedIsImmutable`, `TestMigrations_M5Constraints`. Stato: implemented.

**INV-52 — Abbinamenti entro i limiti.** Per ogni pagamento `allocated + refunded ≤ amount` (CHECK
`payments_allocation_check`) e per ogni addebito `paid + credited ≤ total` (CHECK `charges_positive_check`), anche
con abbinamenti e rimborsi concorrenti: UPDATE condizionali con il numero di righe controllato, lock in ordine
contratto/titolo → pagamento → addebiti per id, anche dal webhook del provider. Verificata da:
`TestConcurrency_PaymentMatching`, `TestConcurrency_RefundVsMatch`, `TestConcurrency_WebhookVsDeskSettle`,
`TestMigrations_M5Constraints`, query B52. Uno storno di abbinamento può essere parziale e ripetuto, mai oltre
l'importo dell'abbinamento (migrazione `00017`, ADR 0013 decisione 8): `payment_matches.reversed_cents` è la somma
dei suoi storni, si muove solo con l'UPDATE condizionale `reversed_cents + Δ <= amount_cents` (lock del pagamento,
poi ricontrollo) ed è limitato dal CHECK `payment_matches_reversed_check`; il trigger degli abbinamenti ammette solo
la sua crescita. Verificata da: `TestConcurrency_PartialReversals` (100 goroutine sullo stesso abbinamento, entrambi
i dialetti, `-race`), `TestPayment_RecordMatchReverseRefund`, query B52r, `TestPortal_ChangeDatesThenCancelRefundsTheRest`
(rimborso del cambio date e poi dell'annullamento: la somma dei rimborsi è il pagato),
`TestPortal_ChangeDatesBackAndForthRefundsEachTime` (tornare a date già avute è un nuovo rimborso, mai il replay
del primo: la chiave d'idempotenza contiene la versione del contratto). Stato: implemented.

**INV-53 — Esportazione completa e senza doppioni.** Ogni addebito non annullato compare in una sola prima
esportazione `to_invoice → exported` (indice unico parziale `charge_export_items_first_uidx`), salvo riesportazione
esplicita (`is_first = false`); gli addebiti annullati non sono mai esportati; i saldi di apertura nascono
`invoiced` (fatturati dal software precedente) e non entrano nel "da fatturare" né si riesportano (la riesportazione
per id accetta solo addebiti con una prima esportazione e non annullati). I file CSV ed Excel di un'esportazione sono
sempre gli stessi byte della prima generazione (dati delle righe congelati su `charge_export_items`). Verificata da:
`TestChargeExport_EachChargeOnce`, `TestChargeExport_ReexportRefusesNeverExported`,
`TestChargeExport_FilesFrozenAfterPayment`, `TestConcurrency_ChargeExportParallel`, `TestAcceptance_M5_SimulatedYear`,
`TestMigrations_M5Constraints`. Stato: implemented.

**INV-54 — Un solo addebito primario vivo per origine.** Per rate, rate dei titolari, transiti, recessi e trattenute
di cauzione al più un addebito non annullato senza `corrects_charge_id` per origine (indice unico parziale
`charges_origin_primary_uidx`); un'emissione concorrente restituisce l'addebito esistente. Verificata da:
`TestCharge_OneLivePrimaryPerOrigin`, `TestConcurrency_IssueSameInstallment`, `TestMigrations_M5Constraints`.
Stato: implemented.

**INV-55 — Una sola sessione di cassa aperta per cassa; chiusura coerente.** Indice unico parziale
`cash_sessions_open_uidx`; la differenza di una sessione chiusa è `Σ contato − (fondo + incassi − rimborsi ±
movimenti)` e non cambia più (trigger `cash_sessions_closed*`). Verificata da: `TestCash_OneOpenSessionPerRegister`,
`TestConcurrency_OpenSession`, `TestConcurrency_CashCloseVsPayment`, `TestCashSummary_Reconciles`, query B55.
Stato: implemented.

**INV-56 — Eventi dei provider applicati una sola volta.** Ogni id di evento del provider (e ogni id sintetico della
riconciliazione) produce effetti al più una volta (indice unico `payment_provider_events_uidx`); i pagamenti e i
rimborsi sono unici per riferimento del provider. Verificata da: `TestConcurrency_WebhookDuplicate`,
`TestPaymentWebhook_Deduplicated`, `TestReconcile_FixesMissedWebhook`. Stato: implemented.

**INV-57 — Gli addebiti di un'origine sommano il suo obiettivo.** Per ogni contratto senza rate programmate residue e
ogni transito confermato, la somma dei suoi addebiti per chiave di riga e per (aliquota, natura) è uguale
all'obiettivo di `chargeplan` (snapshot più rettifiche); per gli snapshot senza rettifiche è uguale a
`Breakdown.VATSummary`. Verificata da: `TestChargePlan_SplitSumsToTarget`, `TestAcceptance_M5_SimulatedYear`, query
B57. Stato: implemented.

**INV-58 — Promemoria al più una volta per livello.** Ogni (addebito, livello) è sollecitato al più una volta (indice
unico `reminder_items_level_uidx`); un solo giro di solleciti per porto e giorno (`reminder_runs_day_uidx`); gli
addebiti pagati non sono mai sollecitati. Verificata da: `TestConcurrency_ReminderRunOnce`,
`TestReminders_StopWhenPaidAndExclusions`, `TestMigrations_M5Constraints`. Stato: implemented.

**INV-59 — L'estratto conto chiude al saldo del cliente.** Il saldo di chiusura dell'estratto conto di un cliente
(addebiti, incassi, crediti non legati a un addebito negativo, rimborsi, cauzioni trattenute) è uguale al saldo di
`BalanceTx` (residui degli addebiti aperti − credito); il credito di un addebito negativo (rettifica, saldo di
apertura negativo) non è una seconda riga. Verificata da: `TestStatement_ClosingEqualsBalance` (con saldi di
apertura positivi e negativi). Stato: implemented.

## Report (spec 21)

**INV-60 — Le competenze sommano l'importo.** Per ogni riga ripartita da `accrual.Allocate` la somma delle quote
giornaliere è uguale all'importo (resto sull'ultimo giorno). In M4 la usano il pro rata degli oneri e i ricavi dei
mandati. Verificata da: `TestAccrual_SumsToAmount` (property test). Stato: implemented.

**INV-61 — Report senza scritture.** Il package `report` non scrive e non emette eventi: ogni report e il cruscotto
girano in `store.DB.ReadTx`; `internal/domain/report/**` non importa `domain/event`, `domain/audit`,
`domain/auditlog` (depguard `report-readonly`, M7). Verificata da: `TestReport_ReadOnly`. Stato: implemented.

**INV-62 — Un riquadro del cruscotto è la somma dei suoi record.** Il valore di ogni riquadro (numeratore e
denominatore per i rapporti) è il fold dei record del report che il suo drill-down apre con gli stessi parametri e lo
stesso periodo; idem per l'anno precedente. Verificata da: `TestCalc_TileEqualsFold`,
`TestDashboard_DrillParamsRoundTrip`, `TestReport_DashboardDrillDown`, `TestAcceptance_M7_DashboardDrillDown`. Stato:
implemented.

**INV-63 — Esportazioni uguali al JSON.** Le righe del CSV e dell'Excel di un report sono i suoi record JSON (stesso
ordine) e la riga `Totale` sono i suoi totali. Verificata da: `TestExport_SameRowsAsJSON` (property test),
`TestExport_EveryReport`, `TestAcceptance_M7_DashboardDrillDown` (somma della colonna del CSV e dell'Excel uguale al
riquadro). Stato: implemented.

**INV-64 — Valori dei report interi, rapporti ricalcolati.** I valori dei report sono interi (centesimi, cm, cm²,
punti base); i rapporti di gruppi, bucket e periodi si ricalcolano da Σ numeratori / Σ denominatori, mai come media.
Verificata da: `TestCalc_DailyEqualsTile`, `TestBP_HalfEvenAndNil`, depguard `pure-engines`. Stato: implemented.

## Altri

**INV-70 — Nessun dato personale negli eventi SSE e nei log.** Verificata da:
`TestEvents_DataHasNoPII` (con i campioni degli eventi M1 di mappa e anagrafiche, di `config.pricing_changed` M2 e
degli eventi M3 di occupazione, presenze, preventivi, contratti e transiti e degli eventi M4 di firme, rate,
rinnovi, diritti, mandati, ripartizioni, documenti e comunicazioni e degli eventi M5 di addebiti, esportazioni,
pagamenti, rimborsi, cassa, documenti commerciali, richieste di pagamento, solleciti e impostazioni: solo id, enum,
date, importi e conteggi), `TestSSE_PayloadHasNoPII` (con `map_id`, `resource_ids`, `nights` di M3),
`TestSSE_M4Topics`, `TestSSE_M5Topics`, `TestLogs_NoPII` (anche webhook di pagamento ed esportazioni, M5);
da M6a `TestAcceptance_M6aLanPairing` e `TestAcceptance_M6aPairTOTPRotatesSession` (codice di abbinamento, token
della sessione del dispositivo e token di primo avvio assenti dai log); da M8 `TestBooking_AvailabilityNoPII` (la
disponibilità pubblica non espone codici posto, id, clienti, barche, riferimenti né conteggi) e
`TestLostDemand_AnonymousRowDeduped` (domanda persa senza cliente, barca né dati del client); e `TestLauncher_StartStop` (log del
launcher senza token, email o password); da M6b `TestConcurrency_OfflineReplay_Presence` (`assertNoPII` sui log e
payload degli eventi `presence.arrived` senza note né nomi), `TestRemoteProxy_TokenNeverReachesPage` (token di
dispositivo assente da risposte e log del proxy remoto) e `TestFiscalAgent_LostOutcomeNoReissue` (log dell'agente
fiscale con soli id e stato, senza token né riferimenti del dispositivo); da M9 i campioni di
`TestEvents_DataHasNoPII` comprendono gli eventi di rimessaggio (ipotesi applicate, spostamenti), manutenzione
(`maintenance.status_changed`, `maintenance.due`) e utenze (`meter.reading_recorded`): solo id, enum e conteggi, mai
valori di lettura né note; da M10 i campioni comprendono gli eventi di cantiere (`yard_catalog.changed`,
`yard_quote.changed`, `yard_quote.sent`, `yard_quote.accepted`, `yard_quote.expired`, `work_order.status_changed`,
`work_order.changed`, `work_task.changed`, `timesheet.changed`, `work_order.billed`, `yard_plan.changed`) e
`equipment.booking_changed`: solo id, enum, date e importi della commessa, mai nomi di dipendenti, clienti o
fornitori. Stato: partial.

**INV-71 — Offline senza effetti sulle risorse.** Nessuna operazione accodata offline crea o sposta allocazioni né
emette documenti numerati. La coda accetta solo le operazioni con `x-porto-offline: true` e il server rifiuta con 422
`offline_not_allowed` ogni richiesta marcata `Porto-Offline-Queued-At` su un'altra operazione (M6b). Verificata da:
`TestOfflineGuard_RefusesNonOfflineOps`, `TestPolicies_OfflineSet`, `TestPresence_OfflineRequiresAllocation`,
`TestPayment_OfflineReceiptModeRefused`, `allowlist.test.ts` (Vitest, allowlist della coda). Da M9 vale anche per
le letture dei contatori, le segnalazioni e gli esiti delle manutenzioni (`utility.reading`, `maintenance.report`,
`maintenance.outcome`): nessuna crea, sposta o rilascia allocazioni; completare un intervento non rilascia le notti
di manutenzione (`TestMaintenance_ReportNeverOccupies`, `TestAcceptance_M9_Offline`, `queue.m9.test.ts`). Da M10 vale
per le ore e il completamento delle attività di cantiere (`yard.timesheet`: `startTimer`, `stopTimer`,
`createTimeEntry`; `yard.activity_done`: `updateTaskProgress`, `uploadTaskPhoto`): nessuna crea, sposta o rilascia
allocazioni o prenotazioni dei mezzi (`TestAcceptance_M10_Offline`; stato M10: lato server implemented,
`TestWorkOrder_Timers`, `TestWorkOrder_TasksAndDependencies`). Stato: implemented (M6b, M9).

**INV-72 — Audit log solo in inserimento.** Le righe di `audit_log` non si modificano né si cancellano, su SQLite e
PostgreSQL (trigger del database). Un ripristino desktop registra `desktop.restore` anche nel database ripristinato,
non solo in quello che viene messo da parte. Verificata da: `TestAuditLog_InsertOnly`,
`TestDesktopRestore_OlderSchemaAndAudit` (`internal/http/desktop`). Stato: partial.

**INV-73 — Query operative limitate al marina.** Ogni query operativa usa il `marina_id` dello scope autorizzato;
una risorsa di un altro marina risponde 404. Verificata da: `TestAcceptance_CrossMarinaIsolation` (tutte le rotte
con `{marinaId}`, comprese quelle M1 di mappa, clienti, imbarcazioni, documenti e scadenze e quelle M2 di articoli, classificazioni e listini),
`TestAcceptance_CrossMarinaResourceIds` (il proprio `{marinaId}` con id di risorse di un altro marina, comprese
le rotte M2 con `{revenueClassId}`, `{itemId}`, `{categoryId}` e `{priceListId}` e le rotte M3 con `{contractId}`,
`{quoteId}`, `{allocationId}`, `{releaseId}`, `{inquiryId}`, `{presenceId}`, `{undoId}`, `{areaId}` e `{closureId}`),
`TestStore_ElementReferencesStayInMarina` e `TestRegistryStore_MergeTargetStaysInMarina` (FK composite
`(…_id, marina_id)` per strutture, colonnine, struttura padre e cliente di destinazione della fusione);
da M4 `TestSignature_SignerUserScopedToMarina` (un firmatario utente deve avere un accesso al marina); da M6a
`TestDeviceSession_NarrowedToDeviceMarina` e `TestDeviceSession_OtherMarinaIs404` (vedi INV-81); da M9
`TestAcceptance_CrossMarinaResourceIdsM9` (le rotte con `{scenarioId}`, `{movementId}`, `{meterId}`, `{readingId}`,
`{consumptionId}`, `{equipmentId}`, `{planId}` e `{taskId}` con id reali di un altro marina); da M10
`TestAcceptance_CrossMarinaResourceIdsM10` (le rotte con `{workOrderId}`, `{taskId}`, `{timesheetId}`,
`{externalCostId}` e `{fileId}` con id reali di commessa, attività, ora, costo esterno e foto di un altro marina) e
`TestAcceptance_CrossOrgUserLinksM10` (responsabile della commessa e utente collegato al dipendente devono avere un
accesso al marina: un utente di un'altra organizzazione risponde come un id sconosciuto); da M11
`TestAcceptance_CrossMarinaResourceIdsM11` (tutte le rotte `{marinaId}/rental/` con id reali di un altro marina:
`{rentalUnitId}`, `{rentalSkipperId}`, `{rentalFormulaId}`, `{rentalExtraId}`, `{rentalBookingId}`,
`{rentalBlockId}` e `{rentalDamageId}`). Stato: partial.

**INV-74 — Documenti cifrati a riposo e serviti solo con link firmati.** I file dei documenti (e gli sfondi delle
mappe) sono salvati cifrati con il KeyRing sotto `PORTO_DATA_DIR/files`, legati a marina e id; il download di un
documento richiede `customers.read` e un link firmato di 5 minuti legato all'utente. Verificata da:
`TestDocument_FileEncryptedAtRest`, `TestDocument_SignedURLExpires`; da M4 anche i documenti generati e firmati,
le scansioni e le evidenze di firma (`TestDocs_ComposeRenderArchive`). Stato: implemented.

**INV-75 — Link dei clienti solo per il proprio soggetto.** Un token di link (magic link) risolve solo il suo
marina, scopo, soggetto e cliente; token scaduti o revocati rispondono 404 come quelli sconosciuti; i token sono
salvati come hash e non compaiono nei log. Verificata da: `TestPublicLinks_CrossMarina`,
`TestMagicLink_RotateRevokes`. Stato: implemented.

**INV-76 — Codice OTP monouso con tentativi limitati.** Un codice OTP di firma si consuma una sola volta e accetta
al massimo N tentativi errati (aggiornamenti condizionati `CountOTPAttempt` e `ConsumeOTP`). Verificata da:
`TestSignature_OTPLockout`, `TestConcurrency_OTPAttempts`. Stato: partial (manca un `TestConcurrency_OTPVerify`
con 100 verifiche parallele del codice corretto).

**INV-77 — Un solo messaggio per chiave di deduplica.** Ogni `dedupe_key` è accodata una sola volta per marina
(indice univoco `message_log_dedupe_uidx`); su questo si basa l'idempotenza dei job `comms.send`, delle notifiche
delle scadenze e degli invii delle campagne di rinnovo. Verificata da: `TestAcceptance_M4_RenewalCampaign300`,
`TestConcurrency_CampaignLaunchAndSync` (`sent_count = 1`), `TestDeadlines_NotifyOncePerLeadDay`. Stato: partial
(manca un `TestConcurrency_CommsSendOnce` sul trasporto reale).

**INV-78 — Un codice di abbinamento crea al massimo un dispositivo.** Un codice di abbinamento si consuma una sola
volta con un UPDATE condizionato (`store.ConsumePairingCode`), qualunque sia la via di scambio: token di
dispositivo (`/api/device/v1/pair`) o sessione del browser (`/api/device/v1/pair-session`, M6a). Verificata da:
`TestConcurrency_PairingCodeSingleUse`, `TestConcurrency_PairSessionSingleUse`,
`TestConcurrency_PairingMixedEndpoints` (`internal/domain/device`). Stato: implemented.

**INV-79 — Il listener della rete locale esiste solo se attivato e accetta solo peer privati.** L'edizione desktop
ascolta su interfacce diverse dal loopback solo mentre il titolare tiene attiva la rete locale; le connessioni da
indirizzi non privati (fuori da RFC 1918, ULA, link-local) sono chiuse prima di leggere un byte. Verificata da:
`TestLANServer_OnlyWhenEnabled`, `TestPrivateListener_RejectsPublicPeers` (test unitari in `internal/lan`, senza
database). Stato: implemented.

**INV-80 — L'API desktop risponde solo in locale.** `/api/desktop/v1` risponde solo sul listener di loopback
dell'edizione desktop, da un peer di loopback e con un `Host` atteso; altrove (edizione server, rete locale) è 404.
Verificata da: `TestDesktopAPI_LoopbackOnly` (`internal/http`), `TestHostCheck_RejectsRebinding`
(`internal/http/middleware`). Stato: implemented.

**INV-81 — La sessione di un dispositivo vale solo per il suo marina e muore con il dispositivo.** Un browser
abbinato con il QR riceve una sessione legata al dispositivo: anche per un ruolo di organizzazione agisce solo sul
marina del dispositivo (`auth.NarrowGrants`; un altro marina risponde 404), è rifiutata appena il dispositivo è
revocato e la revoca la cancella nella stessa transazione. Per un utente con TOTP la sessione resta in attesa per
10 minuti al massimo e la conferma del secondo fattore la sostituisce con una nuova (il token in attesa smette di
valere). Verificata da: `TestDeviceSession_NarrowedToDeviceMarina`, `TestDeviceSession_RevokedDeviceRejected`,
`TestDeviceSession_TOTPUserMustVerify`, `TestDeviceSession_PendingTOTPExpiresQuickly`,
`TestConcurrency_DeviceSessionVerifyOnce` (`internal/domain/auth`), `TestDeviceSession_RevokeKillsSession`,
`TestDeviceSession_OtherMarinaIs404` (`internal/domain/device`), `TestAcceptance_M6aPairTOTPRotatesSession`.
Stato: implemented.

**INV-82 — Il token di primo avvio crea al massimo un titolare.** Il token di primo avvio dell'edizione desktop è
monouso, vive solo in memoria come hash e vale solo finché non esiste alcun utente: dopo il primo titolare (anche
creato da CLI o bootstrap) sia il primo avvio sia il ripristino dal primo avvio rispondono 409 `first_run_done`;
un ripristino fallito non consuma il token. Verificata da: `TestFirstRunToken_SingleUse` (`internal/desktop`),
`TestConcurrency_FirstRunSingleOwner`, `TestDesktopFirstRunRestore_TokenAndOwner`,
`TestDesktopFirstRunRestore_SingleUse` (`internal/http/desktop`). Stato: implemented.

**INV-83 — Un'operazione offline ripetuta ha un solo effetto.** Un'operazione della coda offline ripetuta un numero
qualsiasi di volte, anche in parallelo o dopo una risposta persa, produce un solo effetto e la stessa risposta: chiave
di idempotenza tenuta 7 giorni e chiave unica di dominio per effetto. Verificata da:
`TestConcurrency_OfflineReplay_Presence`, `TestConcurrency_OfflineReplay_CashPayment`,
`TestConcurrency_OfflineReplay_Photo`, `TestConcurrency_IdempotencyTakeOver`,
`TestPresence_DomainKeyAfterLostMiddlewareRow`, `TestPresence_PhotoDomainKeyAfterLostMiddlewareRow` (la foto
uguale a quella corrente, per SHA-256, risponde con lo stesso file), `queue.test.ts` e `runner.test.ts` (Vitest),
`offline-queue.spec.ts`; da M9 per `utility.reading`, `maintenance.report` e `maintenance.outcome`:
`TestConcurrency_OfflineReplay_Reading`, `TestConcurrency_OfflineReplay_MaintenanceReport`,
`TestConcurrency_OfflineReplay_MaintenanceOutcome` (100 repliche in parallelo con lo stesso `client_ref`: una riga,
la stessa risposta, un audit e un evento), `TestUtility_ReadingReplaySameKey`, `TestMaintenance_CompleteOfflineReplay`,
`TestAcceptance_M9_Offline`; da M10 per `yard.timesheet` e `yard.activity_done` (unique parziali `(marina_id,
client_ref)` di `timesheets`, `work_tasks`, `work_task_files` e `stop_client_ref`; una ripetizione dell'avanzamento
non lo fa mai diminuire): `TestAcceptance_M10_Offline`, `TestAcceptance_M10_FinalAccount`, `TestWorkOrder_Timers`,
`TestConcurrency_TimerStart` (stato M10: lato server implemented).
Stato: implemented (M6b, M9).

**INV-84 — I dati offline appartengono a un utente e a un marina.** Cache e coda offline sono legate all'utente e al
marina correnti, non contengono segreti né link firmati, si cancellano all'uscita; le operazioni di un altro utente
non si inviano mai con la sessione corrente. Verificata da: `persist.test.ts`, `queue.test.ts`, `runner.test.ts`
(Vitest), `hooks.test.tsx` (sessione scaduta o dispositivo revocato: `me` nullo cancella la cache anche a coda vuota,
le operazioni restano ferme), `queue.test.ts` e `MarinaLayout.test.tsx` (ingresso in un altro marina rifiutato a
livello di route con operazioni non inviate), `offline-queue.spec.ts` (cancellazione all'uscita). Stato: implemented
(M6b).

**INV-85 — Il bridge risponde solo su loopback e il token remoto non raggiunge la pagina.** `/api/bridge/v1` e
`/api/desktop/v1` rispondono solo sul listener loopback dell'app (404 altrove e nell'edizione server); in modalità
remota il token di dispositivo resta nel proxy e non compare in risposte, redirect, pagine di errore o log.
Verificata da: `TestBridgeAPI_LoopbackOnly`, `TestRemoteProxy_TokenNeverReachesPage`. Stato: implemented (M6b).

**INV-86 — Un documento fiscale è emesso da al massimo una presa in carico.** I documenti del driver `bridge` sono
presi in carico con un lease condizionale: un documento ha al massimo una presa in carico valida alla volta ed è
emesso una sola volta. Sul desktop l'agente prende in carico un documento alla volta, registra l'esito `issued` in un
giornale locale prima di inviarlo e non chiama più il dispositivo per un documento registrato (esito perso, lease
scaduto, riavvio): lo reinvia, anche con la nuova presa in carico. Resta solo la finestra di un arresto tra la stampa
e la scrittura del giornale (il contratto `ext.FiscalDriver` chiede l'idempotenza su `DocumentID` dove il dispositivo
la consente). Verificata da: `TestConcurrency_FiscalBridgeClaim`, `TestFiscalBridge_OutcomeIdempotent`,
`TestFiscalAgent_LostOutcomeNoReissue`. Stato: implemented (M6b).

**INV-87 — L'incasso offline è solo su documenti esistenti.** Un pagamento in contanti rigiocato dalla coda offline è
abbinato per intero ad addebiti esistenti: somma degli abbinamenti uguale all'importo (nessun credito non abbinato),
ogni abbinamento positivo e non oltre il residuo dell'addebito (INV-52), nessun addebito in modalità scontrino
(422 `offline_not_allowed` altrimenti); il client lo rifiuta già all'accodamento. Verificata da:
`TestPayment_OfflineAmountEqualsMatches`, `TestPayment_OfflineReceiptModeRefused`, `queue.test.ts` e
`hooks.test.tsx` (Vitest). Stato: implemented (M6b).

**INV-88 — Token del portale e delle prenotazioni solo per il proprio soggetto.** Un link o una sessione dell'area
cliente risolve solo il suo porto e il suo cliente; un token di prenotazione (HMAC dell'id con il nonce salvato) o di
stato risolve solo la sua prenotazione. Qualunque altro porto, cliente, prenotazione o id è `404 link_invalid`, senza
distinguere sconosciuto, scaduto o altrui (nessuna enumerazione); i token viaggiano solo in header o nel frammento
dell'URL, mai nel percorso o nei log. Estende INV-75 ai token del portale (M8). Verificata da:
`TestPortal_TokenScoped`, `TestPortal_LinkSingleUse`, `TestPortal_SessionExpiry` (token del portale) e
`TestBooking_TokenScoped` (token di prenotazione: token altrui, mancante, alterato, id sconosciuto e slug di un altro
porto sono tutti `404 link_invalid`), `TestBooking_CrossMarina404` (prenotazione, sessione dell'area cliente e
ufficio di un altro porto esistente: sempre 404, mai nelle sue liste). Le superfici a solo token (pagina della prenotazione, controllo dei documenti
richiesti a `/pay`) mostrano e contano solo i documenti caricati tramite quella prenotazione
(`online_booking_documents`), mai quelli di un cliente esistente collegato per email:
`TestBooking_TokenSurfaceHidesCustomerDocuments`. Le risposte memorizzate dall'idempotenza delle scritture anonime
sono legate all'hash del token o della sessione: `TestIdempotency_AnonymousScopeBindsCredential`. Stato:
implemented.

**INV-89 — Prenotazioni ed eventi dei canali applicati una volta.** Un evento di un canale è applicato al più una
volta (`channel_events_uidx` su porto, canale, id evento) e una prenotazione esterna esiste una sola volta
(`channel_reservations_uidx` su porto, canale, riferimento esterno), anche con lo stesso evento ricevuto 100 volte in
parallelo da webhook e pull. Verificata da: `TestMigrations_M8Constraints`, `TestConcurrency_ChannelReservationOnce`,
`TestChannel_CancelReleases` (lo stesso annullamento ricevuto due volte annulla una sola volta),
`TestAcceptance_M8_ChannelFlow`.
Stato: implemented.

**INV-90 — Un consumo è addebitato al più una volta.** Ogni riga di `utility_consumptions` compare in al più un
addebito vivo: il passaggio a `charged` è un UPDATE condizionato (`status = 'pending' AND charge_id IS NULL`) sotto
il lock del contratto, `charge_id` è valorizzato se e solo se lo stato è `charged` (vincolo
`utility_consumptions_charged_check`) e ogni addebito di origine `utility` ha un solo addebito primario vivo
(`charges_origin_primary_uidx` include `utility`, `00019`). Verificata da: `TestMigrations_M9Constraints`,
`TestConcurrency_UtilityChargeOnce`, query Q21 di `store.CheckUtilityInvariants` (M9; anche: nessuna riga di
`utility_billings` senza addebito). Annullare un addebito `utility` riporta i suoi consumi a `pending` nella stessa
transazione (`TestUtility_CancelledChargeReopensConsumptions`); un totale zero non scrive né addebito né riga di
fatturazione e chiude i consumi con `zero_amount` (`TestUtility_ZeroTotalNoOrphanBilling`). Stato: implemented.

**INV-91 — Le quote di consumo sommano la differenza di lettura.** Per ogni intervallo tra due letture consecutive
la somma delle quote (occupanti e quota non assegnata) è uguale alla differenza (con moltiplicatore e giro del
registro), al millesimo; ogni quota dista meno di 1 millesimo dalla proporzione esatta delle notti. Verificata da:
`TestRapid_SplitSums`, `TestUtility_SplitAcrossOccupants`, query Q22 di `store.CheckUtilityInvariants` (M9). Le notti
di ogni occupante vengono dalle righe di occupazione (una notte rilasciata e rivenduta è del rivenditore). Stato:
implemented.

**INV-92 — Le notti di manutenzione esistono solo per interventi pianificati.** Ogni allocazione attiva di tipo
`maintenance` è elencata in un intervento `scheduled`, `in_progress` o `done` (un intervento annullato conserva solo
le notti passate: l'annullamento rilascia quelle dal giorno stesso); una segnalazione non occupa mai nulla
e la pianificazione non scavalca mai una barca (409 `maintenance_conflicts`). Verificata da:
`TestMaintenance_OccupancyConflicts`, `TestMaintenance_ReportNeverOccupies`,
`TestMaintenance_OutOfServiceNotSellable`, `TestConcurrency_MaintenanceScheduleVsAllocation`,
`TestAcceptance_M9_MaintenanceCycle`, query Q23 di `store.CheckUtilityInvariants` (M9). `occupancy.AllocateTx`
accetta un'allocazione `maintenance` senza barca (nessun controllo di compatibilità): le notti restano protette dai
vincoli unique delle tabelle di occupazione (ADR 0003). Stato: implemented.

## Cantiere (spec 18)

**INV-93 — Il consuntivo riconcilia ore, materiali e costi esterni.** Per ogni commessa il consuntivo soddisfa le
equazioni di `docs/m10-plan.md` §9.3: ricavi, costi e margini delle righe sommano i totali per fonte (manodopera,
materiali, servizi, costi esterni) e per tipo di lavorazione; le ore di manodopera sono la somma esatta delle ore
approvate non annullate e il loro costo la somma dei costi congelati all'approvazione; la quantità di un materiale
è prelevato meno reso; costi e ricavi esterni sono le somme delle righe; lo scostamento dal preventivo somma la
differenza tra ricavo effettivo e imponibile del preventivo. Verificata da: `TestRapid_AccountReconciles`,
`TestAccount_Worked`, query Q30 di `store.CheckYardInvariants`, `TestWorkOrder_FinalAccountWorked`,
`TestAcceptance_M10_FinalAccount` (M10). Stato: implemented.

**INV-94 — Gli addebiti di una commessa sommano il suo obiettivo.** Ogni fatturazione di commessa
(`work_order_billings`) ha al più un addebito primario vivo (`charges_origin_primary_uidx` include `work_order`,
`00021`); dopo l'addebito finale la somma lordo degli addebiti vivi di origine `work_order` della commessa
(rettifiche comprese) è uguale al lordo dell'obiettivo, e lo stesso per imponibile e IVA per aliquota (come INV-57).
Gli addebiti esportati non cambiano mai: le correzioni sono rettifiche collegate (INV-51). Verificata da:
`TestRapid_BillingSequenceSums`, `TestConcurrency_FinalBillingOnce`, `TestMigrations_M10Constraints` (M10). Stato:
implemented.

**INV-95 — Un solo cronometro attivo per dipendente.** Al più una riga di `timesheets` per dipendente è `running`
(unique parziale `timesheets_running_uidx`); un secondo avvio risponde `409 timer_already_running`. Verificata da:
`TestConcurrency_TimerStart`, `TestMigrations_M10Constraints` (M10). Stato: implemented.

**INV-96 — Un preventivo accettato crea una sola commessa.** Un preventivo di cantiere `job` crea al più una
commessa (`work_orders.quote_id` unico) e una variante al più una variazione (`work_order_variations.quote_id`
unico), nella stessa transazione dell'accettazione; due accettazioni concorrenti danno un 200 e un `409
invalid_transition`. Verificata da: `TestConcurrency_YardQuoteAccept`, `TestMigrations_M10Constraints` (M10). Stato:
implemented.

## Noleggio (spec 19)

**INV-97 — Un solo punto di cambio di stato dei noleggi.** Solo `internal/domain/rental/reservation` scrive
`rental_bookings.status`, le righe di `rental_slots` e `rental_skipper_slots` e `hold_expires_at`, consultando la
tabella pura `rental/states`; ogni cambio scrive audit e outbox nella stessa transazione. La consegna e la
restituzione passano dai metodi `*Tx` di `reservation`. Verificata da: `TestRentalStates_Table`,
`TestRental_StatusWritesOnlyInReservation` (M11). Stato: implemented.

**INV-98 — La cauzione non è un ricavo finché non viene trattenuta.** Un pagamento di tipo cauzione si abbina solo al
suo addebito `deposit_retention`; `deposit_retained_cents` è l'importo abbinato e non supera la cauzione; la cauzione
non compare mai nei corrispettivi, nel report dei ricavi né nel riquadro `rental_revenue` finché non è trattenuta.
Si trattiene solo quanto deciso al rientro: un evento di addebito della pre-autorizzazione senza importo, o con un
importo maggiore della decisione, è riportato alla decisione. Verificata da: query B60 di
`store.CheckRentalInvariants`, `TestRental_DepositNeverRevenue`, `TestRapid_DepositSettlement`,
`TestAcceptance_M11_Preauth` (anche con l'evento senza importo) (M11). Stato: implemented.

**INV-99 — Un solo addebito principale e uno di riconsegna per noleggio.** Per ogni prenotazione esiste al più un
collegamento `main` e uno `return` in `rental_booking_charges` (unique parziale). Un addebito principale non esportato
e non pagato si annulla invece di rettificarlo (riprezzatura, cambio di cliente o di `invoice_requested`,
annullamento della prenotazione) e il suo collegamento diventa `replaced`; lo stesso fa l'hook di annullamento degli
addebiti di origine `rental` quando l'ufficio annulla un addebito principale o di riconsegna, così la prenotazione
si può addebitare di nuovo. Un addebito esportato o pagato si corregge solo con rettifiche collegate (INV-51), e
dopo un pagamento `invoice_requested` non cambia più: un noleggio non mescola scontrino e fattura. Per un noleggio
chiuso l'addebito principale vivo più le sue rettifiche è lo snapshot (Q33b conta 0 quando non ne resta nessuno).
Verificata da: `rental_booking_charges_kind_uidx`, query Q33, `TestConcurrency_RentalCheckOutOnce`,
`TestConcurrency_RentalQuickCashOnce`, `TestRental_CancelledMainChargeRecreated`,
`TestRental_MainChargeReplacedOrAdjusted`, `TestAcceptance_M11_InvoiceSwitch`, `TestMigrations_M11Constraints`
(M11). Stato: implemented.

Estensioni di M11 a invarianti esistenti: INV-05 vale anche per gli hold dei noleggi
(`TestReservation_ExpiredHoldIsReclaimed`, `TestConcurrency_RentalHoldReclaim`; il recupero blocca le prenotazioni
scadute con `SKIP LOCKED` su PostgreSQL, così non attende mai una conferma in corso); INV-20 vale anche per gli snapshot dei noleggi
(`TestRentalQuote_Recompute`, `RentalEngineVersion`); INV-70, INV-71 e INV-83 valgono per gli eventi `rental.*` e
per il tipo offline `rental.checklist` (`TestAcceptance_M11_Offline`, il caso PUT di `runner.test.ts`, e
`TestAcceptance_M11_OfflineStaleReplay`: un salvataggio in coda più vecchio di quello registrato non lo sovrascrive).
