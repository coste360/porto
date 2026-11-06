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
`TestConcurrency_LandAllocation`, `TestConcurrency_ChannelVsDesk`. Stato: planned.

**INV-02 — Celle disgiunte implicano barche fisicamente separate.** Se due posizionamenti sulla stessa area hanno
insiemi di celle disgiunti, i loro ingombri (con margini) non si intersecano. Verificata da:
`TestFootprint_DisjointCellsImplyNoOverlap` (property test). Stato: planned.

**INV-03 — Ogni allocazione è coerente con le sue righe.** Per ogni allocazione attiva le righe di occupazione sono
esattamente quelle prodotte da `Footprint × notti` della sua posizione e del suo periodo. Verificata da:
`TestAllocation_RowsMatchFootprint` (property test, tag `integration`). Stato: planned.

**INV-04 — Nessuna mappa salvata ha posti sovrapposti.** Verificata da: `TestValidateMap_RejectsOverlap`,
`TestGenerator_NoOverlap`. Stato: planned.

**INV-05 — Hold scaduti non bloccano le vendite.** Un hold con `expires_at < now` viene ignorato e ripulito al
momento del conflitto anche se il job non è passato. Verificata da: `TestOccupancy_ExpiredHoldIsReclaimed`.
Stato: planned.

**INV-06 — Spostamenti e ipotesi sono atomici.** Uno spostamento, uno scambio o l'applicazione di un'ipotesi o la
modifica completa o lascia tutto invariato. Verificata da: `TestConcurrency_SwapVessels`,
`TestScenarioApply_AllOrNothing`. Stato: planned.

**INV-07 — Compatibilità rispettata o derogata esplicitamente.** Ogni allocazione attiva supera `fit.Check` senza
bloccanti oppure ha `fit_override` con utente e motivazione. Verificata da: `TestAllocation_FitOrOverride`.
Stato: planned.

**INV-08 — Una prenotazione online occupa sempre una risorsa concreta.** Ogni transito in `held`, `pending_payment`
o `confirmed` ha almeno un'allocazione. Verificata da: `TestCheckout_HoldHasAllocation`. Stato: planned.

**INV-09 — Conferma solo dal pagamento.** Un transito online passa a `confirmed` solo dal webhook del provider o dal
driver manuale. Verificata da: `TestCheckout_BrowserReturnDoesNotConfirm`. Stato: planned.

## Prezzi (spec 12)

**INV-20 — Snapshot riproducibile.** Ricalcolare un `price_snapshot` con la stessa `engine_version` e gli stessi
input restituisce lo stesso totale. Verificata da: `TestQuote_SnapshotReproducible` (property test). Stato: planned.

**INV-21 — Il motore prezzi è puro.** `internal/domain/pricing` non importa `store`, `clock`, `time.Now`. Verificata
da: regola di `depguard` in `.golangci.yml`. Stato: planned.

## Contratti (spec 13)

**INV-30 — Un solo punto di cambio di stato.** Ogni cambio di stato di contratti e transiti passa da
`internal/domain/contract` e scrive audit ed evento nella stessa transazione. Verificata da:
`TestContract_StateChangeWritesAuditAndOutbox`. Stato: planned.

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

**INV-70 — Nessun dato personale negli eventi SSE e nei log.** Verificata da: `TestSSE_PayloadHasNoPII`.
Stato: planned.

**INV-71 — Offline senza effetti sulle risorse.** Nessuna operazione accodata offline crea o sposta allocazioni né
emette documenti numerati. Verificata da: test Vitest della allowlist della coda. Stato: planned.
