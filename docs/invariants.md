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
sullo stesso posto, 100 goroutine, SQLite e PostgreSQL, `-race`). Stato: implemented.

**INV-06 — Spostamenti e ipotesi sono atomici.** Uno spostamento, uno scambio o l'applicazione di un'ipotesi o la
modifica completa o lascia tutto invariato. Verificata da: `TestConcurrency_SwapVessels`,
`TestConcurrency_LayoutSaveSingleWinner`, `TestOccupancy_LayoutSaveExchangePlaces` (M3);
`TestScenarioApply_AllOrNothing` (ipotesi, M9). Stato: partial.

**INV-07 — Compatibilità rispettata o derogata esplicitamente.** Ogni allocazione attiva supera `fit.Check` senza
bloccanti oppure ha `fit_override` con utente e motivazione (per una chiave API: la chiave e l'utente che l'ha
creata). Verificata da: `TestAllocation_FitOrOverride`, query Q14 di `store.CheckOccupancyInvariants` (deroga senza
motivazione o senza autore). Stato: implemented.

**INV-08 — Una prenotazione online occupa sempre una risorsa concreta.** Ogni transito in `held`, `pending_payment`
o `confirmed` ha almeno un'allocazione attiva; solo un transito ancora `held` può avere invece un'allocazione
`expired` finché il job non lo chiude (un hold scaduto recuperato al momento del conflitto, INV-05), e la conferma
di un hold perso rialloca o fallisce con `resource_taken`. Ogni preventivo `draft` o `sent` ancora valido ha un hold
attivo per ogni posizionamento delle opzioni con opzione; un hold di preventivo spostato resta l'hold dell'opzione.
Verificata da: `TestContract_HoldTransitHasAllocation` (banco e API, M3), `TestInvariants_Q8QuoteHold`, query Q8;
`TestCheckout_HoldHasAllocation` (online, M8). Stato: partial.

**INV-09 — Conferma solo dal pagamento.** Un transito online passa a `confirmed` solo dal webhook del provider o dal
driver manuale. Da M5 un transito con link di pagamento è `pending_payment` e arriva a `confirmed` solo tramite
`confirmTx`, chiamato dal webhook del provider (`ConfirmTransitPaidTx(source=provider)`) o dal driver manuale; il
ritorno del browser non conferma mai. Verificata da: `TestContract_TransitConfirmOnlyManualDriver`,
`TestContract_ConfirmedOnlyInConfirmTransit` (la tabella degli stati entra in `confirmed` solo da `held` o
`pending_payment` con `confirm`); `TestPaymentWebhook_ConfirmsTransit` (che verifica
anche che il ritorno del browser non conferma; webhook, M5; checkout online M8). Stato: partial (checkout online M8).

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
ridimensionamento) porta un'allocazione prima di un predecessore attivo o dopo l'inizio di un successore attivo
(`ResizeTx` rifiuta questi intervalli come `validation_failed`). Verificata da: query Q12 di
`store.CheckOccupancyInvariants`, `TestAllocation_RowsMatchFootprint` (property test, anche con
`-rapid.checks=2000`). Stato: implemented.

## Prezzi (spec 12)

**INV-20 — Snapshot riproducibile.** Ricalcolare un `price_snapshot` con la stessa `engine_version` e gli stessi
input restituisce lo stesso totale. Verificata da: `TestQuote_SnapshotReproducible` (property test,
`pricing.Recompute(pricing.NewSnapshot(...))` uguale dopo il giro JSON). Stato: implemented.

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

## Contratti (spec 13)

**INV-30 — Un solo punto di cambio di stato.** Ogni cambio di stato di contratti e transiti passa da
`internal/domain/contract` e scrive audit ed evento nella stessa transazione. Verificata da:
`TestContract_StateChangeWritesAuditAndOutbox`, `TestContract_TransitionTable` (le tabelle di stato di
`internal/domain/contract/states.go`); le righe M4 da `TestContract_M4StateChangesWriteAuditAndOutbox` (resume,
request_signature, signature_failed, addendum, change_vessel), `TestContract_SuspendTerminateAmounts` (suspend),
`TestQuote_AcceptRunsAllSteps` (sign), `TestContract_ExpireMarksRenewed` (expire, expire_renewed) e
`TestContract_TacitRenewal` (tacit_renew); le righe M5 da `TestContract_M5StateChangesWriteAuditAndOutbox`
(request_payment, payment_expired, pending_payment → confirmed/cancelled/expired, suspend per morosità con
`SuspendForArrearsTx`). Stato: implemented.

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
stessa transazione (solo `internal/domain/payment`; gli abbinamenti accettano solo inserimenti, trigger su entrambi
i dialetti). I rimborsi escono solo dal denaro non abbinato, quindi "Σ incassi − Σ rimborsi" di un addebito è il suo
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
`TestMigrations_M5Constraints`, query B52. Stato: implemented.

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

**INV-61 — Report senza scritture.** Il package `report` non scrive e non emette eventi. Verificata da:
`TestReport_ReadOnly`. Stato: planned.

## Altri

**INV-70 — Nessun dato personale negli eventi SSE e nei log.** Verificata da:
`TestEvents_DataHasNoPII` (con i campioni degli eventi M1 di mappa e anagrafiche, di `config.pricing_changed` M2 e
degli eventi M3 di occupazione, presenze, preventivi, contratti e transiti e degli eventi M4 di firme, rate,
rinnovi, diritti, mandati, ripartizioni, documenti e comunicazioni e degli eventi M5 di addebiti, esportazioni,
pagamenti, rimborsi, cassa, documenti commerciali, richieste di pagamento, solleciti e impostazioni: solo id, enum,
date, importi e conteggi), `TestSSE_PayloadHasNoPII` (con `map_id`, `resource_ids`, `nights` di M3),
`TestSSE_M4Topics`, `TestSSE_M5Topics`, `TestLogs_NoPII` (anche webhook di pagamento ed esportazioni, M5).
Stato: partial.

**INV-71 — Offline senza effetti sulle risorse.** Nessuna operazione accodata offline crea o sposta allocazioni né
emette documenti numerati. Verificata da: test Vitest della allowlist della coda. Stato: planned.

**INV-72 — Audit log solo in inserimento.** Le righe di `audit_log` non si modificano né si cancellano, su SQLite e
PostgreSQL (trigger del database). Verificata da: `TestAuditLog_InsertOnly`. Stato: partial.

**INV-73 — Query operative limitate al marina.** Ogni query operativa usa il `marina_id` dello scope autorizzato;
una risorsa di un altro marina risponde 404. Verificata da: `TestAcceptance_CrossMarinaIsolation` (tutte le rotte
con `{marinaId}`, comprese quelle M1 di mappa, clienti, imbarcazioni, documenti e scadenze e quelle M2 di articoli, classificazioni e listini),
`TestAcceptance_CrossMarinaResourceIds` (il proprio `{marinaId}` con id di risorse di un altro marina, comprese
le rotte M2 con `{revenueClassId}`, `{itemId}`, `{categoryId}` e `{priceListId}` e le rotte M3 con `{contractId}`,
`{quoteId}`, `{allocationId}`, `{releaseId}`, `{inquiryId}`, `{presenceId}`, `{undoId}`, `{areaId}` e `{closureId}`),
`TestStore_ElementReferencesStayInMarina` e `TestRegistryStore_MergeTargetStaysInMarina` (FK composite
`(…_id, marina_id)` per strutture, colonnine, struttura padre e cliente di destinazione della fusione);
da M4 `TestSignature_SignerUserScopedToMarina` (un firmatario utente deve avere un accesso al marina). Stato: partial.

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
