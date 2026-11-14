# ADR 0019 — Test deterministici, CI a due livelli, budget di tempo fuori dai gate

- **Stato:** accettata
- **Data:** 2026-11-14
- **Sezioni della specifica:** 32 (test e qualità); cambia la natura "bloccante" dei budget di m3-plan §9.5,
  m7-plan §11.5, m8-plan §3.2, m9-plan §6.4, m11-plan (criterio 2) e m12-plan §14

## Contesto

Dopo il rilascio 1.0.0 il run completo su `main` (push e notturno) è fallito quattro volte su sei senza che il
codice fosse sbagliato, e una pull request è fallita per un e2e che passava al secondo giro. Le cause:

- **soglie di tempo misurate sul runner:** frame medio della mappa 20,2 ms contro 20 ms, cruscotto dei report 2,15 s
  contro 2 s; il risultato dipende dalla macchina condivisa, non solo dal codice;
- **timeout predefinito di `go test`:** il pacchetto `acceptance` sotto `-race` impiega 575 s, il limite è 600 s;
- **seed demo dipendente dall'ora:** i test del seed giravano sull'orologio reale e il seed del cantiere, avviato
  presto al mattino, non lasciava ore da approvare (difetto vero del seed, corretto);
- **ordine fra lavori asincroni:** una email con il PDF ancora in generazione contava come invio fallito e veniva
  ritentata dopo un minuto, oltre l'attesa dell'e2e (difetto vero, corretto: l'email aspetta il documento e parte
  quando il documento è pronto);
- **deadlock PostgreSQL** fra ingest dei sensori e modifica del dispositivo (`FOR UPDATE` contro il `FOR KEY SHARE`
  delle chiavi esterne; corretto con `FOR NO KEY UPDATE`);
- attese a tempo fisso e limiti di latenza stretti negli e2e e in un test del proxy desktop.

## Decisione

1. **Nessun test dei gate fallisce per una durata.** I budget di tempo della specifica restano e vengono misurati,
   ma:
   - nei test che verificano anche la correttezza la durata è sempre stampata e fa fallire il test solo con
     `PORTO_PERF_BUDGET=1` (`perfbudget.Check`);
   - i test che esistono solo per misurare (budget dei report e della disponibilità, carico M12) girano solo con
     `PORTO_PERF_BUDGET=1` (`perfbudget.Require`);
   - il budget di rendering della mappa resta dietro `PORTO_E2E_PERF=1`.
   Li esegue il workflow **Performance** (`perf.yml`: notturno e manuale, `make test-perf`, `make e2e-perf`), che
   non blocca pull request, merge né rilascio: un run rosso lì è un segnale da misurare, non un `main` rotto.
2. **Attese su condizioni, non sul tempo.** Un test aspetta un evento (risposta, messaggio, stato) e usa un limite
   di tempo solo come protezione contro un blocco, largo rispetto al caso normale. Gli algoritmi con un limite di
   tempo (ottimizzatore) nei test si fermano al proprio budget di iterazioni, così il risultato dipende solo dal
   seme. I test che dipendono da "oggi" usano un orologio finto a un istante fisso.
3. **CI a due livelli** (`ci.yml`):
   - **pull request** (circa 10 minuti): lint, test unitari, integrazione su SQLite senza i quattro pacchetti più
     lenti, web, codice generato, licenze, immagine Docker con smoke test, spec Playwright essenziali
     (`make e2e-smoke`, elenco `E2E_SMOKE` nel Makefile);
   - **main, notturno, manuale**: la suite completa: integrazione su SQLite e PostgreSQL, test di concorrenza con
     il race detector (`-timeout` esplicito), tutte le spec Playwright (server e desktop), govulncheck e Trivy. I due
     scanner non girano sulle pull request perché il loro database cambia ogni giorno indipendentemente dal codice.
4. **Un solo nuovo tentativo, del singolo test, mai del job.** Playwright in CI `retries: 1` (la suite desktop
   resta a 0: il primo avvio crea l'owner una volta sola). I test Go di integrazione e di concorrenza su `main`,
   nel notturno e nei run manuali passano da `scripts/gotest.sh`, che con `GOTEST_RERUN_FAILS=1` usa gotestsum e
   rilancia una volta solo i test falliti (al massimo 5, oltre è un guasto vero); sulle pull request e in locale è
   `go test` puro. Un test che passa solo al nuovo tentativo (Playwright "flaky", gotestsum "DONE 2 runs") va
   corretto, non tollerato. Stesso schema in Arenile: ADR gemello `../arenile/docs/adr/0030-test-deterministici-e-ci.md`, con lo stesso titolo (variabile `ARENILE_PERF_BUDGETS=1` al posto di `PORTO_PERF_BUDGET=1`).

## Conseguenze

- Il run completo su `main` torna una prova del codice: rosso significa un difetto da correggere.
- Una regressione di prestazioni non blocca più il merge: la si vede nel workflow Performance (e nei log dei test,
  dove ogni durata è stampata). Prima di un rilascio si guarda l'ultimo run di Performance.
- Una regressione su una spec e2e fuori da `E2E_SMOKE`, o desktop, si vede sul run di `main` dopo il merge, non
  sulla pull request; una modifica che tocca un'area senza spec essenziale può lanciare `make e2e` in locale o
  `workflow_dispatch` sul branch.
- Nuovi test: mai `waitForTimeout`/`time.Sleep` come attesa di un risultato, mai una soglia di latenza fuori da
  `perfbudget`, mai l'orologio reale dove il risultato dipende da oggi.
