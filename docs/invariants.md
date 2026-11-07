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
driver manuale. Verificata da: `TestContract_TransitConfirmOnlyManualDriver`,
`TestContract_ConfirmedOnlyInConfirmTransit` (M3, driver manuale: la tabella degli stati entra in `confirmed` solo
da `held` con `confirm`); `TestCheckout_BrowserReturnDoesNotConfirm`
(webhook, M5/M8). Stato: partial.

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
`internal/domain/contract/states.go`). Stato: implemented.

**INV-31 — Le rate sommano il totale.** Per ogni piano rate `Σ rate = totale del contratto` al centesimo.
Verificata da: `TestInstallmentPlan_SumsToTotal` (property test). Stato: planned.

## Diritti d'uso e condominio (spec 14)

**INV-40 — Quote dei titolari complete.** Per ogni diritto attivo la somma delle quote dei titolari è 1000 millesimi.
Verificata da: `TestRight_HolderSharesSumTo1000`. Stato: planned.

**INV-41 — Tabelle millesimali complete.** Una tabella usata in una ripartizione somma a 1000. Verificata da:
`TestCommonCharges_RejectsIncompleteTable`. Stato: planned.

**INV-42 — Ripartizione esatta.** La somma degli importi ripartiti è uguale al totale della voce (resto maggiore).
Verificata da: `TestCommonCharges_AllocationSumsExactly` (property test). Stato: planned.

## Addebiti e incassi (spec 16)

**INV-50 — Saldo coerente.** Per ogni addebito `paid_amount = Σ abbinamenti dei pagamenti riusciti − Σ rimborsi`,
aggiornato nella stessa transazione del pagamento. Verificata da: `TestCharge_PaidAmountMatchesLedger` (property
test, tag `integration`). Stato: planned.

**INV-51 — Addebiti esportati immutabili.** Un addebito `exported` o `invoiced` non cambia importi né righe; si
corregge con un addebito di rettifica collegato. Verificata da: `TestCharge_ExportedIsImmutable`. Stato: planned.

**INV-52 — Abbinamenti entro i limiti.** Per ogni pagamento `Σ abbinamenti ≤ importo` e per ogni addebito
`Σ abbinamenti ≤ totale`, anche con abbinamenti concorrenti. Verificata da: `TestConcurrency_PaymentMatching`.
Stato: planned.

**INV-53 — Esportazione completa e senza doppioni.** Ogni addebito non annullato compare in un'esportazione
`to_invoice → exported` una sola volta, salvo riesportazione esplicita. Verificata da:
`TestChargeExport_EachChargeOnce`. Stato: planned.

## Report (spec 21)

**INV-60 — Le competenze sommano l'importo.** Per ogni riga ripartita da `accrual.Allocate` la somma delle quote
giornaliere è uguale all'importo. Verificata da: `TestAccrual_SumsToAmount` (property test). Stato: planned.

**INV-61 — Report senza scritture.** Il package `report` non scrive e non emette eventi. Verificata da:
`TestReport_ReadOnly`. Stato: planned.

## Altri

**INV-70 — Nessun dato personale negli eventi SSE e nei log.** Verificata da:
`TestEvents_DataHasNoPII` (con i campioni degli eventi M1 di mappa e anagrafiche, di `config.pricing_changed` M2 e
degli eventi M3 di occupazione, presenze, preventivi, contratti e transiti), `TestSSE_PayloadHasNoPII` (con
`map_id`, `resource_ids`, `nights` di M3), `TestLogs_NoPII`. Stato: partial.

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
`(…_id, marina_id)` per strutture, colonnine, struttura padre e cliente di destinazione della fusione). Stato: partial.

**INV-74 — Documenti cifrati a riposo e serviti solo con link firmati.** I file dei documenti (e gli sfondi delle
mappe) sono salvati cifrati con il KeyRing sotto `PORTO_DATA_DIR/files`, legati a marina e id; il download di un
documento richiede `customers.read` e un link firmato di 5 minuti legato all'utente. Verificata da:
`TestDocument_FileEncryptedAtRest`, `TestDocument_SignedURLExpires`. Stato: implemented.
