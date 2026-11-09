# ADR 0017 — Sensori e scheda pubblica (M12)

- **Stato:** proposta
- **Data:** 2026-11-09
- **Sezioni della specifica:** 24, 25, 32 (M12); 06.2, 06.8, 07, 09.2, 09.3, 10.6, 12.5, 20.2, 20.4, 23, 26, 27, 28,
  30, 31.4

## Contesto

M12 è l'ultima tappa della roadmap: sensori di occupazione dei posti (ultrasuoni, radar, magnetometri, infrarossi,
telecamere) che inviano letture firmate o arrivano da sorgenti interrogate periodicamente, una macchina a stati con
isteresi, la riconciliazione con l'occupazione contrattuale e le presenze, il contesto *Sensori* della mappa, lo
storico fisico, il report delle discrepanze, l'occupazione aggregata pubblica e la scheda pubblica del porto su
`/p/{slug}`. Arenile non ha sensori né scheda pubblica: non c'è comportamento da riprodurre (ADR 0001). La decisione
aperta D7 (partner hardware di riferimento e certificazione) resta aperta: Porto pubblica un protocollo aperto, un
simulatore e l'estensione `ext.SensorSource`, e non nomina fornitori. Le decisioni dove la specifica tace sono in
`docs/m12-plan.md` §19 (D1–D27); questo ADR registra quelle vincolanti.

## Decisione

1. **I sensori non modificano occupazioni né contratti (§0.2, INV-100).** Nessun percorso del codice dei sensori
   scrive `allocations`, `*_occupancies`, `releases`, `contracts` o addebiti. Gli unici effetti automatici sono gli
   avvisi (`sensor_alerts`) e, solo se la marina lo abilita, le presenze `out`/`in` dei contratti d'ormeggio annuali
   con una sola imbarcazione (`presences.source = sensor`, `presence.RecordSensorTx`, senza hook di contratto e mai
   per i transiti). Tutto il resto è una **proposta** confermata da un utente con i servizi di dominio esistenti
   (`presence.Record`, `occupancy` per rilasci e revoche, `maintenance.CreateSensorTaskTx`) e i suoi permessi.
2. **Chiavi e firma (§3.3, §4.2, D3, D4, INV-105).** Chiave del dispositivo `po_sns_<kid>_<segreto>`, del gateway
   `po_gw_<kid>_<segreto>`, generate con `crypto/rand` e mostrate una volta. Si conservano l'identificativo pubblico
   (`Porto-Key`), l'hash SHA-256 della chiave (ricerca, confronto bearer) e un verificatore cifrato con la KeyRing
   (AAD `sensor-key:<id>` / `gateway-key:<id>`), necessario perché l'HMAC richiede il segreto condiviso; il testo in
   chiaro non è mai salvato né registrato nei log. Firma `Porto-Signature: t=<unix>,v1=<hex>` = HMAC-SHA256 della
   stringa canonica `v1\n<t>\nPOST\n<path>\n<sha256 del corpo>`, tolleranza 300 s, confronti a tempo costante, lavoro
   comparabile per un `kid` sconosciuto. I dispositivi firmano sempre; i gateway possono firmare o usare la chiave
   come bearer su HTTPS. Il replay nella finestra è innocuo per l'idempotenza (punto 3): nessun archivio di nonce. Un
   ripristino con un altro `PORTO_SECRET_KEY` rende i verificatori inutilizzabili: i dispositivi vanno ri-chiavati.
3. **Ingestione isolata e idempotente (§4, D2, D5, INV-101, INV-102).** `POST /api/ingest/v1/readings`, lotti di
   1..500 letture (≤ 256 KiB), esiti per elemento con HTTP 200. La marina è sempre quella della chiave autenticata,
   mai del payload; una chiave di dispositivo scrive solo il proprio `external_id`, un gateway solo i dispositivi a
   esso associati. Ogni lettura è idempotente su `(device_id, observed_at)` (chiave primaria, `observed_at` troncato
   al millisecondo). Limiti: 3.000 richieste/min per prefisso IP prima dell'autenticazione, 30/min per chiave di
   dispositivo e 600/min per chiave di gateway, contati subito dopo la ricerca della chiave.
4. **Isteresi pura (§5, D7).** Lo stato fisico confermato cambia dopo N letture concordi o dopo T dalla prima di esse
   (predefiniti 2 letture o 5 minuti), uguale per `occupied`, `free` e `unknown`; l'istante del cambio è la prima
   lettura concorde, così "libero da N ore" conta da quando il posto si è svuotato davvero. Le letture fuori ordine si
   conservano ma non cambiano lo stato; il silenzio oltre `offline_after_minutes` porta a `unknown`; lo spostamento del
   dispositivo azzera lo stato (`device_moved`). La funzione è pura (`sensor/hysteresis`): tempo passato come
   argomento, nessun float.
5. **Audit ed eventi proporzionati al volume (§0.6, D6, D19).** Configurazione, chiavi, decisioni sugli avvisi e
   presenze automatiche scrivono audit e outbox nella stessa transazione. Le letture grezze non sono auditate e non
   emettono eventi. Un cambio di stato confermato scrive una riga di `sensor_state_changes` (storico append-only) e
   l'evento `sensor.state_changed` nella transazione d'ingestione, senza riga di audit. Gli eventi pubblici
   `sensor.state_changed` e `sensor.anomaly` e il topic SSE `sensor.changed` portano solo id, enumerazioni, istanti e
   conteggi: mai `external_id`, chiavi, firme, valori delle letture, batteria, nomi delle imbarcazioni o note.
6. **Conservazione e backup (§7, D8, D9).** Le letture grezze restano `PORTO_SENSOR_RAW_RETENTION` (30 giorni,
   variabile d'istanza) e poi sono eliminate a blocchi di 5.000 righe dal job orario `sensors.retention`; lo storico
   dei cambi di stato resta per sempre. `sensor_readings` è **escluso dal backup** (`BackupExcludedTables`): diagnostica
   ad alto volume, riproducibile solo dai dispositivi entro 30 giorni; ciò che conta è `sensor_state_changes`, che è
   nel backup con tutte le altre tabelle nuove. Dopo un ripristino le viste delle letture grezze sono vuote prima della
   data del ripristino.
7. **Riconciliazione (§8, D10–D16, INV-104).** L'aspettativa contrattuale è a tre valori (`occupied`, `free`,
   `either`): i giorni d'arrivo e di partenza (fino a `departure_cutoff_minutes`, 12:00) non generano mai discrepanze.
   Le regole R1–R8 della spec 24.4 sono pure (`sensor/reconcile`): posto occupato non previsto, arrivo del transito
   (proposta), contratto annuale libero da N ore (presenza `out` automatica se abilitata), rientro (presenza `in`
   automatica solo dopo un'uscita registrata dal sensore, altrimenti proposta), proposta di rilascio dopo M giorni
   solo dove `release_policy = with_consent` e con il consenso attestato dall'operatore, rientro anticipato su posto
   rilasciato (rivenduto o no), sensore silenzioso o batteria scarica (proposta di intervento di manutenzione). Un
   solo avviso aperto per sensore e tipo (indice unico parziale); una condizione decisa (ignorata o confermata) non si
   riapre finché non cessa. `release_policy = automatic_after_days` non entra in M12 e anche allora la 24.4 chiede una
   conferma.
8. **Occupazione pubblica aggregata (§11, D20, INV-103).** Opzionale per marina: posti liberi stanotte per categoria
   pubblicata (nessuna riga di occupazione, nessuna chiusura, sensore non fisicamente occupato), calcolata al più ogni
   60 s, mai quali posti né quali imbarcazioni.
9. **Scheda pubblica (§12, D21–D25, INV-103).** `marina_profiles` modificata nel pannello e pubblicata su `/p/{slug}`:
   la SPA della prenotazione (chunk lazy del profilo) con un `<head>` iniettato da Go (titolo, descrizione, canonical,
   `hreflang`, Open Graph, JSON-LD `["Marina","Place"]` serializzato con escape HTML), cache 300 s, nessun cookie,
   404 `noindex` se non pubblicata. Posizione WGS84 come interi × 10⁷ formattati in decimali esatti; profondità in cm;
   foto JPEG/PNG ricodificate con la libreria standard per eliminare EXIF/GPS, servite solo se referenziate dalla
   scheda pubblicata. La mappa semplificata non ha codici, occupazione, imbarcazioni né sensori. La matrice dei prezzi
   di transito è quella del motore del listino attivo con `publish_matrix`. Nulla è inviato a directory esterne.
10. **Nessuna dipendenza nuova, nessuna IA (§1.2, D27, ADR 0008).** Nessun client MQTT o decodificatore LoRaWAN nel
    core: i gateway inviano letture già decodificate, le altre sorgenti sono estensioni `ext.SensorSource`; la sorgente
    finta `fakesensor` è registrata solo in sviluppo, demo e test. Nessun riconoscimento d'immagine lato Porto.

## Conseguenze

- I fornitori integrano Porto con `docs/sensor-protocol.md` (con vettore di prova normativo) senza attendere la
  scelta del partner hardware (D7 resta aperta).
- L'accettazione di M12 (1.000 sensori, una lettura al minuto ciascuno, SQLite, nessun ritardo della vista operativa
  oltre 2 s) è un test Go con server HTTP reale, orologio reale e client SSE (`TestAcceptance_M12_SensorLoad`).
- Un ripristino su un'istanza con un'altra chiave segreta richiede di ruotare le chiavi dei dispositivi.
- Gli operatori restano gli unici a cambiare contratti e occupazione: i sensori segnalano, non decidono.
