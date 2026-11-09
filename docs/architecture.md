# Architettura di Porto

Stato: **M0 — Fondamenta** (spec [32.3](spec/32-test-qualita-roadmap.md)). Questo documento descrive il codice così
com'è costruito in M0 e dove si innestano i domini delle milestone successive. La fonte autorevole resta la
specifica: [03 Architettura e stack](spec/03-architettura-e-stack.md), [04 Struttura del
repository](spec/04-struttura-del-repository.md). L'ordine di lavoro di M0 è in [`m0-plan.md`](m0-plan.md).

Porto usa lo stesso stack e gli stessi pattern di Arenile, ma il codice è scritto da zero per il dominio portuale
([ADR 0001](adr/0001-riuso-infrastruttura-arenile.md)).

## 1. Forma generale

Un solo binario Go (`porto`) contiene API REST, SSE, job runner, outbox e le due SPA React incorporate con
`embed.FS`. Il database è SQLite (file `porto.db` in `PORTO_DATA_DIR`, WAL, driver `modernc.org/sqlite` senza CGO)
oppure PostgreSQL (`DATABASE_URL`). Stesso schema logico sui due dialetti.

```
 browser (pannello)        browser (pagina pubblica)      desktop remoto
   /admin/  /api/admin/v1     /b/{slug}  /api/public/v1/{slug}   /api/device/v1
        │                          │                              │
        ▼                          ▼                              ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ porto serve                                                                  │
│  middleware: request id · real ip · access log · recover · security headers │
│              · body limit · authenticate · require auth · TOTP gate · CSRF  │
│              · rate limit · Idempotency-Key                                  │
│  http/admin · http/device · http/public · http/sse · http/static            │
│        │  (decodifica → servizio di dominio → codifica, errori problem+json)│
│  domain/  auth · team · marina · apikey · device · audit · event · webhook  │
│           · notify · meta                                                    │
│        │                                                                     │
│  store (Bun, unico package con SQL) ──► outbox_events ──► jobs               │
│                                          (dispatcher, webhook, email, pulizie)│
└────────┼─────────────────────────────────────────────────────────────────────┘
         ▼
  SQLite /data/porto.db   |   PostgreSQL (DATABASE_URL)
```

## 2. Mappa dei package (M0)

| Package | Ruolo |
|---|---|
| `cmd/porto` | CLI: `serve`, `migrate up\|status`, `admin create`, `seed demo`, `backup`, `restore`, `version` (spec [31.4](spec/31-multipiattaforma.md)) |
| `internal/app` | composition root: configurazione, DB, servizi, job, server HTTP, estensioni |
| `internal/config` | lettura e validazione delle variabili `PORTO_*` (tutti gli errori insieme, mai i valori segreti) |
| `internal/clock`, `money`, `calendar`, `reqctx`, `crypto` | primitive condivise: orologio iniettabile, centesimi, date `YYYY-MM-DD`, contesto della richiesta, KeyRing/HKDF |
| `internal/geo`, `internal/qty` | puri: geometria intera in cm (`geo.Cm`, `Rect`, `Polygon`, trigonometria a tabella) e quantità a virgola fissa (`qty.Milli`) |
| `internal/http` | assemblaggio del router, mappa errori di dominio → problem |
| `internal/http/problem` | RFC 9457 `application/problem+json` con slug stabili |
| `internal/http/api` | **generato** da `api/openapi.yaml` (oapi-codegen) |
| `internal/http/middleware` | catena di middleware (§3) |
| `internal/http/{admin,device,public}` | handler sottili delle tre superfici |
| `internal/http/sse` | `GET /api/admin/v1/stream`, `ReplayBus` con `Last-Event-ID` |
| `internal/http/static` | SPA incorporate, fallback a `index.html`, pagina segnaposto |
| `internal/domain/auth` | utenti, Argon2id, sessioni, TOTP e codici di recupero, reset password, principal, ruoli e permessi, limiti di login |
| `internal/domain/team` | membership, inviti, protezione dell'ultimo owner |
| `internal/domain/marina` | organizzazioni e marina (creazione, modifica con `If-Match`, archiviazione) |
| `internal/domain/apikey`, `device` | chiavi API `po_live_`, abbinamento dei dispositivi e token `po_dev_` |
| `internal/domain/audit` | scrittura del registro di audit (solo inserimenti) |
| `internal/domain/event` | tipi di evento, outbox, dispatcher, broker in memoria, client di uscita anti-SSRF, firma |
| `internal/domain/webhook` | endpoint, consegne con backoff, disattivazione, conservazione |
| `internal/domain/notify` | coda email (`message_log`), modelli, trasporti SMTP/file/log |
| `internal/domain/meta` | versione e capacità (`GET /api/admin/v1/meta`) |
| `internal/store` | repository Bun: **l'unico posto con SQL**; scope di marina e organizzazione |
| `internal/migrations` | goose per dialetto: `sqlite/` e `postgres/`, stessi numeri |
| `internal/jobs` | job runner nel processo (§6) |
| `internal/backup` | backup portabile (tar.gz con JSONL per tabella) tra SQLite e PostgreSQL |
| `internal/testutil/testdb` | esegue un test su SQLite e, se `PORTO_TEST_POSTGRES_URL` è impostata, su PostgreSQL |
| `internal/acceptance` | test di accettazione di sistema M0, solo tramite API |
| `cmd/porto-desktop` | edizione desktop (M6a): launcher con server su `127.0.0.1` nel processo, finestra webview (tag `webview`, unico package con CGO) o browser |
| `internal/desktopport` | tipi e interfacce condivise dell'edizione desktop (`Runtime`, `LANController`, `PairTarget`), senza logica |
| `internal/desktop` | launcher desktop: cartella dati, impostazioni, chiave nel portachiavi, istanza unica, primo avvio, backup, controllo versione, log (implementa `desktopport.Runtime`) |
| `internal/lan` | modalità rete locale: CA e certificato locali, listener solo peer privati, controllo `Host` (implementa `desktopport.LANController`) |
| `internal/http/desktop` | handler di `/api/desktop/v1`, solo sul listener di loopback (INV-80) |
| `internal/desktop/remote` | modalità remota (M6b): proxy loopback verso un Porto online che custodisce il token di dispositivo, fissaggio del certificato, cache della shell, agente fiscale bridge; senza database né `app.App` |
| `internal/peripheral` | driver delle periferiche locali (M6b): finti, ESC/POS generico, ZPL, trasporti tcp/cups/file; Go puro, solo `desktopport` e `pkg/ext` |
| `internal/http/bridge` | handler di `/api/bridge/v1` (M6b), solo su loopback come l'API desktop (INV-85) |
| `internal/domain/report` | report, cruscotto e comparazione (M7): permessi `reports.operational`/`reports.all`, lettura in `ReadTx` (INV-61), righe grezze da `internal/store/report_*.go`; sotto-package puri `calc` (record, totali come fold, riquadri), `align` (periodi e allineamento data/giorno della settimana), `swap` (suggerimenti di scambio), `export` (CSV italiano ed Excel dalla stessa tabella) |
| `internal/domain/booking` | prenotazione online (M8): configurazione pubblica, disponibilità con regole e quote online, preventivo, creazione (transito `held` + hold concreto + quote nella stessa transazione), dati e consensi, documenti, checkout sugli intent di `collect`, approvazione, operazioni dell'area cliente sui transiti, prenotazioni dei canali, domanda persa, job `bookings.*`; non scrive mai `contracts.status` (passa da `contract`, INV-30); sotto-package puro `policy` (regole di soggiorno, approvazione, acconto, rimborso, cap delle quote) |
| `internal/domain/onlineconf` | impostazioni della prenotazione online per marina (`If-Match`, default se mai salvate) e testi versionati immutabili (condizioni, privacy, marketing) |
| `internal/domain/portal` | area cliente (M8): link di accesso con token nel frammento, sessioni in header, orchestrazione delle operazioni sui transiti del cliente tramite `booking` (INV-88) |
| `internal/domain/channel` | canali di prenotazione esterni (M8): connessioni con segreti cifrati, mappa delle categorie, prenotazioni applicate una volta (INV-89) tramite `booking`, job `channels.pull`/`channels.push`; `fakechannel` in memoria per test e sviluppo |
| `internal/domain/optimizer` | puro (M9): griglia dell'area di superficie con varchi e campo delle distanze, accessibilità (rettangolo che trasla cella per cella fino a un varco sul bordo, ordine di estrazione per data di varo), `Arrange` con PRNG proprio e seme esplicito, budget di iterazioni e scadenza dal `context`, `PlaceOne`, `Validate` indipendente (INV-15); importa solo `layout`, `fit`, `geo`, `calendar` |
| `internal/domain/storage` | rimessaggio (M9): contratti `storage` tramite `contract.CreateStorageTx` (INV-30), istantanea dell'area, pianta e accessibilità, preset dell'ottimizzatore, ipotesi che non scrivono mai lo stato reale (INV-14), applicazione in una transazione con `occupancy.LayoutTx` e ricontrollo sotto i lock (INV-06), lista degli spostamenti |
| `internal/domain/maintenance` | manutenzioni (M9): mezzi, piani con bersagli e periodicità, job `maintenance.schedule` con righe di scadenza, interventi con tabella degli stati, notti `maintenance` su posti e risorse a terra con `occupancy.AllocateTx` (INV-92), segnalazioni ed esiti anche offline; sotto-package puro `due` |
| `internal/domain/utility` | consumi (M9): contatori, letture dal telefono (offline, `client_ref`), anomalie, ripartizione per notti degli occupanti (sotto-package puro `consume`, INV-91), addebito con origine `utility` tramite `charge.CreateTx` una sola volta (INV-90), job `utilities.charge` |
| `internal/domain/formula` | puro (M10): linguaggio delle formule del catalogo cantiere (ADR 0006), parser a discesa ricorsiva, controllo dei tipi, valutazione in virgola fissa `qty.Milli` con arrotondamento half-even a ogni `*` e `/`, funzioni `if min max round ceil floor band`, limiti, mai panic (INV-29) |
| `internal/domain/yardquote` | catalogo e preventivi di cantiere (M10): tipi di lavorazione, qualifiche, impostazioni cantiere degli articoli, variabili e tabelle a fasce, gruppi di lavorazioni con versioni solo in inserimento, preventivo guidato con snapshot riproducibile (INV-28, sotto-package puro `price`), invio con PDF e link, accettazione che crea la commessa tramite l'interfaccia `OrderCreator` nella stessa transazione (INV-96); job `yard.quotes_expire` |
| `internal/domain/workorder` | commesse (M10): stati (tabella pura `states.go`), attività con dipendenze, checklist e foto, ore con cronometro e approvazione (INV-95, offline `yard.timesheet` e `yard.activity_done`), prelievi e resi di materiale, costi esterni, consuntivo (sotto-package puro `account`, INV-93), addebiti di acconto, SAL, finale e conguaglio tramite `charge.CreateTx` con origine `work_order` (INV-94); implementa `yardquote.OrderCreator`; job `yard.timers_guard` |
| `internal/domain/yardplan` | planner del cantiere (M10): dipendenti, qualifiche, calendari, assenze, assegnazioni con controlli di qualifica, dipendenze e capacità, carico per dipendente, reparto, qualifica e postazione (sotto-package puro `load`), commesse a rischio, proposta automatica deterministica (sotto-package puro `auto`) |
| `internal/domain/lifting` | mezzi di sollevamento (M10): prenotazioni a slot di 15 minuti con operatore abilitato, garantite dalle chiavi uniche di `equipment_slots` e `operator_slots` (INV-17, ADR 0003), richieste da commessa, rimessaggio e spostamenti acqua ↔ terra (sink dell'outbox su `occupancy.moved`), sotto-package puro `slots`; implementa `workorder.BookingCanceller` |
| `internal/domain/rental/fleet` | flotta a noleggio (M11): unità legate alle imbarcazioni dell'anagrafica, skipper con orari settimanali, formule, extra, impostazioni (finestra, fasce, buffer, checklist), preventivo senza stato e snapshot di prezzo per `reservation` tramite `pricing.RentalQuote` |
| `internal/domain/rental/reservation` | prenotazioni a noleggio (M11): righe a slot di 30 minuti di unità e skipper garantite dalle chiavi primarie di `rental_slots` e `rental_skipper_slots` (INV-18, ADR 0003), hold con recupero pigro (INV-05), blocchi, planner, disponibilità, vista del giorno; unico scrittore di stato e righe (INV-97); job `rental.holds_expire` |
| `internal/domain/rental/handover` | consegna e restituzione (M11): identità e patente cifrate, firma su schermo, cauzione (contanti, POS, preautorizzazione, rinuncia; mai ricavo finché non trattenuta, INV-98), checklist e foto offline (`rental.checklist`), danni, addebito di riconsegna, incasso rapido con documento commerciale (INV-99), PDF; implementa `collect.RentalDepositHooks` e `signature.Subject`; job `rental.deposits`, `rental.identity_retention` |
| `internal/domain/rental/price`, `slots`, `states` | puri (M11): snapshot di prezzo, penale di ritardo, carburante, cauzione; modello degli slot e validazione delle fasce; tabella delle transizioni |
| `pkg/ext` | interfacce pubbliche delle estensioni (sottoinsieme M0 di spec [27.5](spec/27-api-eventi-estensioni.md)) |
| `web/shared`, `web/admin`, `web/booking` | workspace npm: libreria condivisa (client generato, shadcn/ui, tema, i18n, marchio), pannello, pagina pubblica |
| `e2e/` | Playwright sul binario reale |

Regole di dipendenza (spec [3.4](spec/03-architettura-e-stack.md), applicate da `depguard` in `.golangci.yml`):
`domain/*` non importa `net/http` (eccezioni: client di uscita e trasporto SMTP), `internal/http/*` né driver SQL;
`internal/http/*` non contiene SQL; `pkg/ext` non importa `internal/`; i package puri non importano `store`,
`clock` né usano `time.Now`.

## 3. Flusso di una richiesta

Ordine dei middleware: `RequestID` → `RealIP` (X-Forwarded-* solo da `PORTO_TRUSTED_PROXIES`) → `AccessLog` (senza
dati personali) → `Recover` → `SecurityHeaders` → `BodyLimit` (1 MiB) → rotte statiche; per `/api/*`:
`Authenticate` → `RequireAuth` → `TOTPGate` → `CSRF` → rate limit → `Idempotency` → handler generati.

1. `Authenticate` riconosce il cookie `porto_session`, `Authorization: Bearer po_live_…` (chiave API) o
   `Bearer po_dev_…` (dispositivo) e mette un `auth.Principal` nel contesto.
2. `CSRF`: le richieste non sicure autenticate con cookie devono avere `X-CSRF-Token` uguale al cookie
   `porto_csrf` (confronto a tempo costante); l'autenticazione bearer è esente.
3. `Idempotency`: sulle POST di creazione la stessa `Idempotency-Key` con lo stesso corpo restituisce la risposta
   salvata; corpo diverso → 422, richiesta ancora in corso → 409.
4. L'handler decodifica e chiama il servizio di dominio. La **prima riga** di ogni metodo di servizio è
   `p.Authorize(ctx, db, marinaID, perm)` (o `AuthorizeOrg`), che restituisce lo scope della marina.
5. Il servizio apre una transazione (`store.RunInTx`): modifica, riga di audit ed evento nell'outbox insieme.
6. Gli errori sentinella del dominio diventano problem+json con slug stabile (`internal/http/errmap.go`); il
   pannello li mostra con la chiave i18n `errors.<slug>`.

## 4. Autenticazione e permessi

Spec [26](spec/26-utenti-ruoli.md) e [30](spec/30-sicurezza-privacy.md).

- **Login**: email e password (Argon2id); errore identico e lavoro comparabile per email sconosciuta (hash
  fittizio); limiti per account (5 errori / 15 min) e per prefisso IP. Con TOTP attivo serve il codice o un codice
  di recupero; ogni codice TOTP vale una volta sola.
- **Sessioni**: token casuale, salvato come hash; scadenza per inattività 24 h e assoluta 14 giorni; rotazione al
  login e alla conferma del TOTP; il cambio password revoca le altre sessioni.
- **Reset password**: token monouso via email (outbox `message_log`), revoca tutte le sessioni.
- **Ruoli** (spec 26.1): `owner`, `director`, `office`, `accounting`, `dock`, `yard_mgr`, `yard`, `rental`, `viewer`.
  I permessi sono costanti nel codice (`internal/domain/auth/roles.go`) e si controllano nei servizi di dominio.
- **Membership**: a livello di organizzazione (vale per tutte le sue marina) o di singola marina; i permessi
  effettivi sono l'unione.
- **Chiavi API**: scope fissati alla creazione, sottoinsieme dei permessi di chi le crea, mai `users.manage`.
- **Dispositivi**: codice di abbinamento monouso → token `po_dev_` legato a utente e marina; il ruolo si rilegge
  a ogni richiesta, quindi revocare la membership revoca anche il dispositivo.
- **Policy TOTP**: con `PORTO_REQUIRE_TOTP_FOR_OWNERS=true` owner e accounting devono attivarlo; fino ad allora la
  sessione raggiunge solo l'iscrizione al TOTP.

## 5. Isolamento per marina

Modello: **organizzazioni → marina**. Ogni query operativa riceve `marina_id` dallo scope autorizzato, mai dal
corpo della richiesta. Una risorsa di un'altra marina risponde **404** (non 403), così la sua esistenza non trapela.
Il test `TestAcceptance_CrossMarinaIsolation` percorre ogni rotta con `{marinaId}` (invariante INV-73). Gli eventi,
le consegne webhook e i messaggi SSE portano il `marina_id` e sono recapitati solo a chi ha accesso a quella marina.

## 6. Eventi, outbox e job

Spec [27.3–27.4](spec/27-api-eventi-estensioni.md), [28](spec/28-notifiche-e-comunicazioni.md),
[29.1](spec/29-tempo-reale-offline-i18n.md).

```
servizio di dominio ── tx: modifica + audit_log + outbox_events ──► commit ──► sveglia outbox.dispatch
                                                                                     │
          ┌──────────────────────────────────────────────────────────────────────────┤
          ▼                                   ▼                                       ▼
  broker in memoria ──► ReplayBus ──► SSE   webhook_deliveries ──► webhooks.deliver   EventSink delle estensioni
  (topic marina.changed, team.changed)       (firma Porto-Signature, backoff,
                                              client di uscita anti-SSRF)
```

- Busta dell'evento: `{id, type, version, marina_id, occurred_at, data}`; `data` contiene solo id ed enum, mai dati
  personali (INV-70).
- Il dispatcher prende gli eventi con un lease (`FOR UPDATE SKIP LOCKED` su PostgreSQL), quindi due istanze non li
  spediscono due volte (`TestConcurrency_OutboxClaimNoDoubleDispatch`).
- Webhook in uscita: `Porto-Signature: t=<unix>,v1=<hmac-sha256>`, con due firme durante la rotazione del segreto;
  backoff esponenziale e disattivazione dopo errori ripetuti; HTTPS e indirizzi pubblici soltanto.
- SSE: `GET /api/admin/v1/stream?marina_id=…`, messaggi leggeri `{topic, type, marina_id, entity_ids}`, heartbeat,
  ripresa con `Last-Event-ID`; il client invalida le query.
- Email: accodate in `message_log` nella transazione e spedite dal job con trasporto SMTP, file `.eml`
  (`PORTO_MAIL_DIR`) o log.

Job nel processo ([m0-plan](m0-plan.md) §1.3): `outbox.dispatch` (1 s, risvegliabile),
`webhooks.deliver` (5 s), `email.deliver` (5 s), `sessions.cleanup` (1 h), `idempotency.cleanup` (1 h),
`webhooks.retention` (24 h). M8 aggiunge `bookings.approval_expire` (1 min), `bookings.capture` (1 min),
`channels.pull` (5 min), `channels.push` (1 min), `portal.cleanup` (1 h) e l'evento pubblico `transit.approval_requested` (topic SSE
`contract.changed`).

## 7. Dati

Spec [6.1](spec/06-schema-dati.md) (organizzazione e marina) e [6.9](spec/06-schema-dati.md) (piattaforma). La
migrazione `00001_init.sql` esiste per entrambi i dialetti con lo stesso numero e lo stesso schema logico: tabelle
di piattaforma (utenti, sessioni, TOTP, reset, membership, chiavi API, dispositivi, idempotenza, audit, outbox,
webhook, messaggi) più `organizations` e `marinas`. `audit_log` accetta solo inserimenti (trigger su entrambi i
dialetti, INV-72). Una migrazione già su `main` non si modifica mai.

Backup: `porto backup --out FILE` produce un tar.gz con `manifest.json` e un file JSONL per tabella; `porto restore`
lo carica su un database vuoto e migrato di uno qualsiasi dei due dialetti (passaggio SQLite ↔ PostgreSQL).

## 8. Frontend

Spec [29](spec/29-tempo-reale-offline-i18n.md), [31.3](spec/31-multipiattaforma.md),
[ADR 0007](adr/0007-marchio.md).

- `web/shared`: client `openapi-fetch` generato da `api/openapi.yaml`, componenti shadcn/ui aggiunti con la CLI
  ufficiale, tema a token (chiaro/scuro), i18n (italiano sorgente, inglese), `BrandLogo`, helper di test.
- `web/admin` (`/admin/`): login, TOTP, reset password, selettore della marina, shell con barra laterale e le pagine
  M0 (impostazioni marina, team, chiavi API, dispositivi, webhook, audit, account, organizzazione).
- `web/booking` (`/b/{slug}`): segnaposto della pagina pubblica.
- Niente CSS personalizzato: solo classi Tailwind sui token del tema (controllato in lint).
- Offline (M6b, spec 29.2, ADR 0011): il pannello è una PWA con service worker scritto a mano che mette in cache
  solo la shell (`/api/*` mai intercettato). `web/shared/src/offline` tiene in IndexedDB l'allowlist di query del
  marina e dell'utente correnti (48 ore) e la coda delle sole operazioni con `x-porto-offline` (arrivo e partenza con
  allocazione, foto della presenza, incasso in contanti su addebiti in modalità fattura), con `Idempotency-Key`
  generata all'accodamento e `Porto-Offline-Queued-At` alla ripetizione; il server rifiuta l'header su ogni altra
  operazione (`middleware.OfflineGuard`) e deduplica anche nel dominio. I conflitti si mostrano all'utente.
- Telefono (M6b, 31.3): `landingFor()` porta ogni ruolo alla sua schermata; le route con `staticData.shell =
  'phone'` (banchina `/m/$marinaId/dock`, direzione `/m/$marinaId/direction`) usano `PhoneShell` al posto di
  `AppShell`. Il QR del transito (`PT1:<contract_id>`) si scansiona con la fotocamera o un lettore a tastiera.

## 9. Distribuzione

- `deploy/Dockerfile`: build multi-stadio (Node per le SPA, Go statico con `CGO_ENABLED=0`, runtime distroless
  `nonroot`), volume `/data`, `ENTRYPOINT ["/porto"]` con comando `serve`.
- `compose.yaml` (sviluppo e accettazione M0, con Mailpit), `deploy/docker-compose.yml` (produzione SQLite),
  `deploy/docker-compose.postgres.yml` (produzione PostgreSQL). Variabili in `.env.example`.
- CI (`.github/workflows/ci.yml`): Go (lint, unit, integrazione SQLite + PostgreSQL, race, govulncheck), web,
  codice generato senza differenze, licenze, immagine Docker (Trivy e prova di avvio), e2e, nomi.
- Edizione desktop (M6a, spec [31.2](spec/31-multipiattaforma.md), ADR 0010): `cmd/porto-desktop` avvia nello
  stesso processo un Porto completo su SQLite (dati nella cartella dell'utente, chiave segreta nel portachiavi del
  sistema, backup giornalieri e alla chiusura), lo mostra nella webview del sistema (WKWebView, WebView2,
  WebKitGTK) e, se attivata, lo espone sulla rete locale in HTTPS con un certificato proprio. Il meta riporta
  `edition: desktop` e `local_mode: true`. Pacchetti non firmati (`make desktop-package-*`,
  `docs/desktop-build.md`); `make check` non richiede la toolchain webview.
- Modalità remota (M6b, ADR 0011): l'app desktop abbinata a un Porto online non avvia un server ma un proxy su
  loopback (`internal/desktop/remote`) con il token di dispositivo nel portachiavi, il certificato fissato (foglia,
  poi CA), la cache della shell per l'avvio offline, il PIN dell'app per l'amministrazione locale, il chiosco per la
  reception e il bridge `/api/bridge/v1` verso stampante scontrini, stampante etichette, lettore QR e dispositivo
  fiscale locale (`internal/peripheral`). In modalità remota i documenti del driver fiscale `bridge` sono presi in
  carico dal desktop con lease di 60 secondi (`/api/device/v1/fiscal/*`).

## 10. Domini pianificati

Ogni dominio entra nella sua milestone (spec [32.3](spec/32-test-qualita-roadmap.md)) dentro `internal/domain/`,
con le stesse regole: servizio con controllo dei permessi, scope di marina, transazione con audit e outbox.

| Milestone | Package | Spec |
|---|---|---|
| M1 | `harbor` (mappe, strutture, posti, aree dinamiche, risorse a terra, generatori), `layout` (puro), `customer`, `vessel` | [07](spec/07-mappa-modello-geometrico.md), [08](spec/08-mappa-editor.md), [11](spec/11-clienti-e-imbarcazioni.md) |
| M2 | `catalog`, `pricing` (puro), `pricelist` | [12](spec/12-listini-articoli-prezzi.md) |
| M3 | `occupancy`, `fit` (puro), `presence`, `contract` (transiti di base) | [09](spec/09-mappa-vista-operativa.md), [10](spec/10-motore-di-occupazione.md) |
| M4 | `contract` completo, `schedule`, `rights`, `allot`, `accrual`, `docs` (+ `tmpl`, `markdown`, `layout`, `pdf`), `signature`, `magiclink`, `comms`, `csvimport`, motore scadenze in `deadline` (niente AI, ADR 0008) | [13](spec/13-contrattualistica.md), [14](spec/14-cessioni-oneri-condominio.md), [15](spec/15-modelli-di-stampa-e-firma.md) |
| M5 | `chargeplan` (puro), `charge`, `payment`, `collect`, `paydriver` (`manual`, `stripe` opzionale e disattivato di default), `cash` (+ `summary` puro), `fiscal`, `collect/levels` (puro), `internal/xlsx`; nessuna fatturazione: gli addebiti si esportano (spec 33 D4) | [16](spec/16-addebiti-e-incassi.md) |
| M6a | edizione desktop: `cmd/porto-desktop`, `desktopport`, `desktop`, `lan`, `http/desktop`; sessioni di dispositivo per i browser abbinati via QR | [31](spec/31-multipiattaforma.md) |
| M7 | `report` (+ `calc`, `align`, `swap`, `export` puri): report di 21.3 sui domini esistenti, cruscotto del manager con drill-down ai record, CSV ed Excel; cantiere e noleggio restano agganci per M10/M11 (ADR 0012) | [21](spec/21-report-e-cruscotto.md) |
| M8 | `booking` (+ `policy` puro), `onlineconf`, `portal`, `channel` (+ `fakechannel`), `ext.BookingChannel` e `ext.PaymentAuthorizer`; pagina pubblica e widget in iframe (`/b/{slug}`, `/embed.js`, `frame-ancestors` per origine consentita), area cliente, quote online come contatori nel DB (INV-12), conferma anche da canale verificato (ADR 0013) | [23](spec/23-prenotazione-online.md) |
| M9 | `storage`, `optimizer` (puro), `maintenance` (+ `due` puro), `utility` (+ `consume` puro); contratti di rimessaggio sul dominio dei contratti, pianificatore e ipotesi separate dallo stato reale, applicazione tutto o niente, piani e interventi di manutenzione, letture e addebito dei consumi tramite M5, tipi offline `utility.reading`, `maintenance.report`, `maintenance.outcome` (ADR 0014) | [17](spec/17-rimessaggio.md), [20](spec/20-manutenzioni-e-risorse.md) |
| M10 | `formula` (puro), `yardquote` (+ `price` puro), `workorder` (+ `account` e `states` puri), `yardplan` (+ `load`, `auto` puri), `lifting` (+ `slots` puro); catalogo con formule sulle misure della barca, preventivo guidato prezzato dai listini, commesse con ore, materiali e costi esterni, consuntivo e addebiti tramite M5, planner, mezzi di sollevamento senza doppie prenotazioni per vincolo del DB, report e riquadri del cantiere, tipi offline `yard.timesheet` e `yard.activity_done`; gestione del magazzino rinviata (ADR 0015) | [18](spec/18-cantiere.md), [20](spec/20-manutenzioni-e-risorse.md) |
| M11 | `rental/fleet`, `rental/reservation`, `rental/handover`, `rental/price`, `rental/slots`, `rental/states` (puri), `pricing.RentalQuote`; flotta, formule e prezzi dai listini, planner unità × tempo, slot di 30 minuti senza doppie prenotazioni per vincolo del DB, consegna e restituzione dal telefono, cauzione mai ricavo finché non trattenuta, incasso rapido con `FiscalDriver`, report e riquadri del noleggio, tipo offline `rental.checklist`; vendita online del noleggio rinviata (ADR 0016) | [19](spec/19-noleggio.md) |
| successive | `sensor`, `profile` | [18](spec/18-cantiere.md)–[25](spec/25-scheda-pubblica.md) |

Le regole che nessuna sequenza di operazioni può violare sono in [`invariants.md`](invariants.md).
