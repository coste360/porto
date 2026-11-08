# ADR 0012 — Report e cruscotto (M7)

- **Stato:** proposta
- **Data:** 2026-11-08
- **Sezioni della specifica:** 21, 13.2, 10.7, 26.1, 27, 29, 32 (M7)

## Contesto

M7 porta i report della 21.3 sui domini che esistono oggi (occupazione, contratti e transiti, rilasci, presenze,
diritti e condominio, addebiti, incassi, cassa) e il cruscotto del manager della 21.2 con il drill-down fino ai
record. Arenile ha report, cruscotto, allineamento dei periodi e scrittura Excel (ADR Arenile 0016): restano un
riferimento di comportamento, non una fonte di codice (ADR 0001). Dove la specifica tace, le decisioni sono in
`docs/m7-plan.md` §14 (D1–D22); questo ADR registra quelle vincolanti.

## Decisione

1. **Report come query su righe grezze più calcolo puro.** Ogni report gira in una transazione di sola lettura
   (`store.DB.ReadTx`): niente audit, niente outbox, niente job (INV-61). L'SQL sta solo in
   `internal/store/report_*.go`, filtra per `marina_id` dello scope autorizzato e restituisce righe grezze; può
   contare righe fisiche (celle di un'allocazione in una notte) ma non calcola importi, rapporti o indicatori.
   Aggregazioni e indicatori sono funzioni pure (`report/calc`, `align`, `swap`, `export`): interi (centesimi, cm,
   cm², punti base), nessun float, nessun orologio.
2. **Un riquadro è il fold dei suoi record.** I totali di un report si calcolano solo come fold dei suoi record; un
   riquadro del cruscotto legge i totali della stessa chiamata che il suo drill-down fa, con gli stessi parametri
   (generati dal codice che li interpreta) e lo stesso periodo. La paginazione non cambia i totali; CSV ed Excel
   contengono tutti i record dello stesso risultato (INV-62, INV-63).
3. **Rapporti in punti base, mai mediati.** Occupazione, efficienza, tasso di rinnovo e ricavo per metro si
   calcolano half-even da Σ numeratori / Σ denominatori a ogni livello (gruppo, bucket, periodo); denominatore zero
   è `null` (INV-64).
4. **Occupazione ed efficienza.** Occupano le righe `contract`, `transit`, `owner`; `block` e `maintenance` e le
   chiusure tolgono la notte dal disponibile; le opzioni (`hold`) sono disponibili e libere. I rilasci non rivenduti
   sono liberi nella variante fisica e occupati in quella contrattuale. Aree dinamiche lineari in cm-notte, di
   superficie in cm²-notte; famiglie diverse non si sommano. Contano le risorse attive oggi e le misure attuali
   delle barche (non c'è storico).
5. **Produzione sul valore del contratto.** Le competenze di contratti e transiti sono il valore del contratto
   (IVA inclusa, come registrato) ripartito per giorno con `accrual.DayQuota`; esclusi bozze, opzioni, annullati e
   no-show. I risconti si calcolano sulle righe di addebito (`accrual.Allocate`), i ratei come maturato del contratto
   meno addebitato alla data di chiusura.
6. **Due basi di data come Arenile.** Gli addebiti si leggono per emissione o per competenza; gli incassi per data
   di incasso (rimborsi negativi, crediti esclusi, liquidazioni ai mandanti separate).
7. **Confronto per data o per giorno della settimana.** Il periodo precedente è lo stesso intervallo k anni prima
   (29 febbraio → 28 febbraio) o spostato di 7 × round(365,2425 k / 7) giorni; le serie si allineano giorno per
   giorno, mai per indice.
8. **CSV ed Excel dalla stessa tabella.** CSV italiano (UTF-8 con BOM, `;`, virgola decimale, date GG/MM/AAAA,
   protezione dalle formule) ed Excel con il writer di Porto (`internal/xlsx`), entrambi da un'unica tabella costruita
   dai record; etichette nella lingua dell'utente, formati sempre italiani; al massimo 200 000 righe.
9. **Permessi.** `reports.operational` apre occupazione, efficienza, spreco (senza importi), domanda persa (senza
   valore), presenze (senza ricavi) e imbarcazioni ospitate; ogni importo richiede `reports.all`. Il cruscotto
   mostra solo i riquadri permessi. I nomi dei clienti compaiono solo a chi ha anche `customers.read`.
10. **Agganci per M8–M11.** Cantiere (M10) e noleggio (M11) hanno chiavi di report e di riquadro riservate,
    restituite come non disponibili; la domanda persa leggerà le ricerche online senza disponibilità di M8 come
    richieste con canale `online`; i report del rimessaggio arrivano con M9.

## Conseguenze

- Un indicatore nuovo è un record nuovo più un fold: il cruscotto e l'esportazione lo seguono senza codice a parte.
- I report non usano cache: le prestazioni si garantiscono con indici (migrazione 00015) e un budget misurato sui
  dati dimostrativi (`TestReport_PerformanceBudget`).
- Senza storico di risorse e barche, un report su un periodo passato usa lo stato attuale di risorse e misure.
