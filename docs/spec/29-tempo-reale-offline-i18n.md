## 29. Tempo reale, offline e internazionalizzazione

### 29.1 Tempo reale

SSE come in 27.4; broker in memoria nel processo, `ext.EventBus` con `LISTEN/NOTIFY` di PostgreSQL per più istanze.

### 29.2 Offline

Stesse regole di Arenile (spec Arenile 21.2, ADR Arenile 0029): PWA con shell in cache, allowlist di query del
marina corrente in IndexedDB per 48 ore (mappe, vista del giorno, arrivi e partenze, attività assegnate), coda delle
operazioni con `Idempotency-Key` generata all'accodamento.

**Consentite offline** (pannello, desktop remoto, app mobile): registrazione di arrivo e partenza di barche con
allocazione esistente, note e foto, letture dei contatori, ore lavorate e completamento di attività di cantiere,
esito degli interventi di manutenzione, checklist di consegna e restituzione del noleggio già confermato, incasso in
contanti su documenti o transiti esistenti.

**Non consentite offline:** creare o spostare allocazioni, transiti, contratti, noleggi; emettere documenti
commerciali tramite il registratore remoto. Richiedono la garanzia del server (INV-01).

### 29.3 Internazionalizzazione

Come Arenile (spec Arenile 20): italiano lingua sorgente, inglese, tedesco e francese per pannello, pagina pubblica,
email e modelli; nomi i18n per mappe, gruppi, articoli, periodi; unità mostrate in metri con due decimali, piedi
facoltativi sulla pagina pubblica (solo visualizzazione, i dati restano in cm).
