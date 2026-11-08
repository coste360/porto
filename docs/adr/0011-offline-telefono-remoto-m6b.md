# ADR 0011 — Offline, telefono e desktop remoto (M6b)

- **Stato:** proposta
- **Data:** 2026-11-08
- **Sezioni della specifica:** 26, 27, 29.2, 30, 31.2, 31.3, 32 (M6)

## Contesto

M6b completa la milestone M6 "Multipiattaforma": il pannello diventa una PWA che lavora offline per le sole
operazioni consentite dalla 29.2, il telefono ha schermate per ruolo (31.3) e l'app desktop può fare da client di
un Porto online con le periferiche locali (31.2). Arenile ha la stessa coda offline, la PWA e la modalità remota
(spec 21.2, ADR 0029): restano un riferimento, non una fonte di codice (ADR 0001). Dove la specifica tace, le
decisioni sono in `docs/m6b-plan.md` §13 (D1–D22); questo ADR registra quelle vincolanti.

## Decisione

1. **Coda prima di tutto, con doppia deduplica.** Le operazioni consentite offline (arrivo e partenza di una barca
   con allocazione esistente e note, foto della presenza, incasso in contanti su addebiti esistenti) passano sempre
   dalla coda, anche online. L'`Idempotency-Key` nasce all'accodamento e resta la stessa a ogni tentativo. Il
   server deduplica due volte: il middleware di idempotenza (chiavi tenute 7 giorni per le operazioni offline,
   presa in carico di una chiave rimasta `in_progress` oltre 2 minuti) e una chiave unica di dominio per effetto
   (`presences.idempotency_key`, `payments.idempotency_key`). Il client smette di riprovare dopo 6 giorni.
2. **Il server rifiuta le operazioni non consentite offline.** Le sole operazioni ripetibili dalla coda hanno
   `x-porto-offline: true` nel contratto. Una richiesta con `Porto-Offline-Queued-At` su qualunque altra operazione
   è 422 `offline_not_allowed` (`middleware.OfflineGuard`); l'istante di accodamento finisce nell'audit. Offline non
   si creano né si spostano allocazioni, transiti, contratti o noleggi e non si emettono documenti numerati (INV-71):
   l'incasso offline è ammesso solo su addebiti in modalità fattura.
3. **I conflitti si mostrano, non si risolvono da soli.** Una presenza accodata porta la risorsa vista
   dall'ormeggiatore (`expected_resource_id`): se l'allocazione è cambiata la risposta è 409 `allocation_changed`
   con il posto attuale; l'utente scarta o riprova come nuova operazione. Nessuna sincronizzazione, nessuna fusione
   lato client: il server resta l'unica fonte di verità (INV-01).
4. **Service worker scritto a mano, solo per la shell.** Il service worker mette in cache la shell del pannello e
   non intercetta mai `/api/*`; i dati offline (allowlist di query del marina e dell'utente correnti, 48 ore) e la
   coda vivono in IndexedDB, si cancellano all'uscita e non contengono segreti né link firmati.
5. **Modalità remota come proxy locale che custodisce il token.** In modalità remota l'app non avvia un server
   Porto: un reverse proxy su loopback aggiunge il token di dispositivo `po_dev_` (nel portachiavi del sistema, mai
   nella pagina né in `desktop.json`), mette in cache la shell per l'avvio offline e serve localmente l'API desktop e
   il bridge. L'accesso al proxy richiede un cookie per avvio ottenuto dalla finestra.
6. **Fissaggio del certificato: foglia, poi la sua CA.** Un Porto con certificato proprio (rete locale) si fissa
   con l'impronta del QR di abbinamento e poi con la CA che l'ha emessa, così la rotazione della foglia non rompe il
   collegamento; un Porto con CA pubblica usa le radici di sistema senza fissaggio.
7. **PIN dell'app.** In modalità remota non esistono utenti locali: periferiche, chiosco e scollegamento richiedono
   un PIN dell'app (Argon2id, 5 tentativi ogni 15 minuti). In modalità locale resta la regola M6a (titolare, loopback).
8. **Bridge HTTP solo su loopback.** Stampante scontrini (finta ed ESC/POS generica), stampante etichette (finta e
   ZPL), lettore QR (tastiera e finto) e dispositivo fiscale locale (solo finto) stanno dietro interfacce in
   `internal/desktopport`, con driver in Go puro; `/api/bridge/v1` risponde solo sul listener loopback (INV-85). I
   modelli fiscali reali sono estensioni.
9. **Fiscale in modalità remota con prese in carico.** Il driver fiscale `bridge` del server lascia i documenti
   `pending`; il desktop indicato nelle impostazioni li prende in carico con un lease di 60 secondi e ne riporta
   l'esito, idempotente per presa in carico. Un documento è emesso da al massimo una presa in carico (INV-86).
10. **CA locale con vincoli di nome.** Le nuove CA locali valgono solo per indirizzi privati e nomi `.local`; solo
    una CA vincolata si offre in download ai telefoni (profilo iOS non firmato, certificato DER per Android).
11. **QR del transito opaco.** Il payload è `PT1:<contract_id>`: nessun dato personale, nessun URL; lo risolve solo
    un pannello autenticato.

## Conseguenze

- Il contratto ha `x-porto-offline`, l'header `Porto-Offline-Queued-At`, gli schemi di sicurezza `appPin` e
  `remoteCookie` (verificati solo dal proxy remoto) e la superficie `/api/bridge/v1`.
- Migrazione `00014_offline_bridge`: chiave di idempotenza delle presenze, colonne di presa in carico dei documenti
  fiscali, dispositivo bridge nelle impostazioni di fatturazione.
- Restano per le milestone dei rispettivi domini le altre voci della 29.2 (letture contatori, manutenzione, ore e
  attività di cantiere, check-list dei noleggi): la coda le prevede come tipi riservati.
- Background Sync, notifiche push, stampa USB raw su Windows, lettori seriali e stampanti Bluetooth restano fuori.
