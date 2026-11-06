## 31. Multipiattaforma: deploy, desktop e app mobile

Porto adotta **la stessa infrastruttura multipiattaforma di Arenile** (spec Arenile 24 e 29): un solo server per
marina con l'archivio, e più client che si collegano allo stesso modo. **Non è un servizio in cloud:** il server è
sempre del gestore (PC dell'ufficio, server in sede o un suo server online) e Porto non dipende da servizi
di Coste360 per funzionare.

### 31.1 Modi di installazione

| Dove gira il server | Internet | Prenotazione online, firma remota, IA | Note |
|---|---|---|---|
| **Server online del gestore**: VPS o Docker | necessaria | sì | Serve solo per la prenotazione online e l'accesso da fuori porto: `ghcr.io/coste360/porto`, compose con SQLite o PostgreSQL |
| **Desktop locale**: PC dell'ufficio del porto | non necessaria | no (l'esportazione per la fatturazione funziona) | 31.2, con modalità rete locale |
| **Server in sede**: mini PC o NAS nel marina | facoltativa | se esposto (dominio o tunnel) | Stessa immagine dell'online |

Per i porti con più di 300 posti o più marine nella stessa installazione è consigliato PostgreSQL. Esclusi i modelli
ibridi con sincronizzazione tra due server (garanzia INV-01); il passaggio tra modi avviene con `porto backup` e
`porto restore`.

### 31.2 Edizione desktop

Launcher `cmd/porto-desktop`, identico a quello di Arenile (spec Arenile 29.1) e derivato dal suo codice:
- Windows (installer `.exe`, x64 e ARM64), macOS (`.dmg` notarizzato, Apple Silicon e Intel), Linux (AppImage);
  binari firmati, distribuiti dal portale Coste360 previo login.
- **Modalità locale:** server su `127.0.0.1`, pannello nella finestra dell'app (WKWebView, WebView2), dati nella
  cartella utente, chiave nel portachiavi del sistema, backup automatici giornalieri e alla chiusura, aggiornamenti
  con controllo della versione disattivabile.
- **Modalità rete locale:** gli altri PC dell'ufficio e i telefoni degli ormeggiatori sulla Wi-Fi del porto usano lo
  stesso archivio, con TLS autogenerato e abbinamento tramite QR.
- **Modalità remota:** client di un'istanza online con token per dispositivo, kiosk per il banco, **ponte verso le
  periferiche locali**: stampante delle ricevute, registratore telematico (`FiscalDriver` locale), lettore QR,
  stampante di etichette.
- Pagina pubblica, pagamenti online, firma remota e IA non sono disponibili in
  modalità locale: il pannello spiega come passare a un'istanza online.

### 31.3 App mobile

Repository separato `coste360/porto-mobile` (Expo + React Native + TypeScript), client generato da
`api/openapi.yaml` pubblicato come asset di release, abbinamento con QR e token per dispositivo, privacy first
(nessuna telemetria, nessun dato verso Coste360), offline con coda (29.2), demo abbinabile come Arenile (ADR Arenile
0027). Un'unica app con funzioni filtrate dal ruolo:

| Ruolo | Funzioni |
|---|---|
| Ormeggiatore | Mappa del giorno con contesti, arrivi e partenze, registrazione presenze, scansione del QR del transito, arrivo non previsto (barca e posto), misura della barca, foto, rilasci, blocchi, letture dei contatori, segnalazioni di manutenzione, incasso in contanti |
| Capo cantiere e operaio | Attività del giorno, avvio e arresto delle ore, checklist e foto, consumo di materiali, calendario del travel lift |
| Piazzale | Lista dei movimenti dell'ipotesi applicata, planner della rimessa in sola lettura |
| Noleggio | Consegna e restituzione con checklist, foto, carburante, cauzione, firma su schermo |
| Manutenzione | Interventi assegnati, esito, foto |
| Direzione | Cruscotto, mappa, scadenze |

Fotocamera per documenti (IA, 22) e QR; nessuna notifica push nella prima versione (come Arenile).

### 31.4 Comandi e configurazione

```
porto serve | migrate up|status | admin create | seed demo | backup --out FILE | restore --in FILE | version
```

Variabili: `PORTO_ADDR`, `PORTO_BASE_URL`, `PORTO_DATA_DIR`, `DATABASE_URL`, `PORTO_SECRET_KEY`,
`PORTO_AUTO_MIGRATE`, `PORTO_TLS_AUTOCERT_DOMAINS`, `PORTO_SMTP_URL`, `PORTO_MAIL_FROM`, `PORTO_HOLD_TTL`,
`PORTO_LOG_LEVEL`, `PORTO_TRUSTED_PROXIES`, `PORTO_REQUIRE_TOTP_FOR_OWNERS`, `PORTO_TELEMETRY` (false),
`PORTO_SENSOR_RAW_RETENTION` (30 giorni), `PORTO_DEMO_PAIRING_*`. Dockerfile multi-stage distroless e compose come
Arenile (spec Arenile 24.2–24.3).

`porto seed demo` carica un marina dimostrativo completo: 5 pontili, 320 posti di 8 categorie, un pontile dinamico di
120 m, un campo boe, piazzale e capannone, parcheggi e box, 250 clienti e barche, contratti annuali, diritti d'uso,
transiti estivi, commesse di cantiere, flotta di noleggio, addebiti e incassi.
