# ADR 0010 — Edizione desktop (M6a): server nel processo, rete locale con certificato proprio

- **Stato:** proposta
- **Data:** 2026-11-07
- **Sezioni della specifica:** 03, 04, 26, 27, 29, 30, 31, 32 (M6)

## Contesto

La milestone M6a porta Porto su un computer d'ufficio senza server né internet (spec 31.2): l'app desktop
`porto-desktop` avvia un Porto completo su SQLite, mostra il pannello in una finestra del sistema e, se il titolare
lo attiva, lo rende raggiungibile dagli altri computer e dai telefoni sul Wi-Fi del porto (31.3). Arenile ha la
stessa edizione e resta un riferimento, non una fonte di codice (ADR 0001). Su diversi punti la specifica tace; le
decisioni sono elencate in `docs/m6a-plan.md` §13 (D1–D23). Questo ADR registra quelle che vincolano M6b e le
milestone successive.

## Decisione

1. **Server nel processo.** `porto-desktop` è un solo processo: la finestra (webview del sistema) occupa il thread
   principale e il server Porto (`app.App`, job, outbox) gira nelle goroutine dello stesso processo, con le stesse
   migrazioni e garanzie di `porto serve`. Nessun processo figlio, nessuna sincronizzazione: un solo server per
   porto (INV-01). Solo `cmd/porto-desktop` usa CGO (tag `webview`); `cmd/porto` resta `CGO_ENABLED=0`.
2. **Solo SQLite sul desktop.** PostgreSQL è per le installazioni server (31.1); il backup portabile esistente
   permette di passare da una all'altra (`porto restore`).
3. **Rete locale con fiducia al primo uso.** La rete locale è spenta di default e il suo listener esiste solo
   mentre è attiva (INV-79), accetta solo indirizzi privati e i nomi del proprio certificato. Il certificato è
   generato in locale: una CA locale (10 anni) e un certificato foglia (397 giorni), chiavi private cifrate con il
   KeyRing. Il telefono apre direttamente l'URL HTTPS del QR e accetta l'avviso del certificato una volta;
   l'impronta nel QR serve a chi la vuole confrontare e ai client nativi che la fissano. Nessuna pagina HTTP di
   abbinamento (non aggiungerebbe fiducia). La CA esiste già ora perché M6b la installerà come profilo per la PWA.
4. **Abbinamento del browser con sessione di dispositivo.** Il codice del QR (monouso, a breve scadenza, nel
   frammento dell'URL) si scambia una sola volta per un dispositivo `access = browser` legato a una sessione
   (`sessions.device_id`): 30 giorni di inattività, 180 assoluti; revocare il dispositivo chiude la sessione. I
   token `po_dev_` restano per i client nativi (M6b).
5. **API desktop solo in locale.** `/api/desktop/v1` risponde solo sul listener di loopback, da un peer di loopback e
   con un `Host` atteso (INV-80); l'amministrazione desktop (rete locale, backup, ripristino, chiave segreta) è del
   solo titolare, senza un nuovo permesso.
6. **Controllo della versione attivo di default, disattivabile e mai necessario.** Una richiesta al giorno con solo
   versione e sistema operativo; nessun dato del porto, nessuna installazione automatica; spento non fa alcuna
   richiesta. Nessuna telemetria.
7. **Chiudere la finestra chiude l'app** su ogni sistema in M6a (backup di chiusura, rete locale spenta); icona
   nella barra e funzionamento in background sono M6b.
8. Le altre scelte di `docs/m6a-plan.md` §13 valgono come descritte lì.

## Conseguenze

- I telefoni funzionano solo mentre l'app desktop è aperta e la rete locale attiva; il pannello lo dice.
- Pagina pubblica, pagamenti online e firma a distanza non sono raggiungibili dall'esterno: il pannello lo spiega
  (`meta.local_mode`) e indica come passare a un server online; le funzioni restano nell'API.
- M6b (coda offline della PWA, schermate per ruolo, modalità remota, chiosco, ponte verso le periferiche) parte dai
  punti di aggancio elencati in `docs/m6a-plan.md` §1.2.
- Pacchetti non firmati finché Coste360 non fornisce i certificati: gli script hanno gli agganci documentati e non
  simulano mai una firma.
