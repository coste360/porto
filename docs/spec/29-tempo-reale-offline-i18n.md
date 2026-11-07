## 29. Tempo reale, offline e internazionalizzazione

### 29.1 Tempo reale

SSE come in 27.4; broker in memoria nel processo, `ext.EventBus` con `LISTEN/NOTIFY` di PostgreSQL per più istanze.

### 29.2 Offline

Stesse regole di Arenile (spec Arenile 21.2, ADR Arenile 0029): PWA con shell in cache, allowlist di query del
marina corrente in IndexedDB per 48 ore (mappe, vista del giorno, arrivi e partenze, attività assegnate), coda delle
operazioni con `Idempotency-Key` generata all'accodamento.

**Consentite offline** (pannello anche dal telefono, desktop remoto): registrazione di arrivo e partenza di barche con
allocazione esistente, note e foto, letture dei contatori, ore lavorate e completamento di attività di cantiere,
esito degli interventi di manutenzione, checklist di consegna e restituzione del noleggio già confermato, incasso in
contanti su documenti o transiti esistenti.

**Non consentite offline:** creare o spostare allocazioni, transiti, contratti, noleggi; emettere documenti
commerciali tramite il registratore remoto. Richiedono la garanzia del server (INV-01).

### 29.3 Internazionalizzazione

Come Arenile (spec Arenile 20): italiano lingua sorgente, inglese, tedesco e francese per pannello, pagina pubblica,
email e modelli; nomi i18n per mappe, gruppi, articoli, periodi; unità mostrate in metri con due decimali, piedi
facoltativi sulla pagina pubblica (solo visualizzazione, i dati restano in cm).

**Lingua all'accesso** (confermata dall'utente il 2026-11-07): prima di conoscere la preferenza dell'utente
(apertura del pannello e pagina di accesso, pagina pubblica senza `?lang=`) conta solo la **prima** lingua del
browser; se non è tra quelle supportate si usa l'italiano, anche quando lingue successive lo sarebbero (un browser
tedesco che elenca anche l'inglese riceve l'italiano). Prevalgono un valore esplicito (`?lang=` sulla pagina
pubblica) e la lingua scelta nel pannello (menu utente, profilo). Implementazione: `detectLocale` in
`web/shared/src/i18n/init.ts`.
