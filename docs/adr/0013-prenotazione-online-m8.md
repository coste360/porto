# ADR 0013 — Prenotazione online (M8)

- **Stato:** proposta
- **Data:** 2026-11-08
- **Sezioni della specifica:** 23, 13.2, 13.5, 10.4, 10.7, 10.8, 16, 21.3, 26, 27, 29, 30, 32 (M8)

## Contesto

M8 porta la pagina pubblica di prenotazione e il widget in iframe, la ricerca per date e dimensioni con le quote
online, il checkout con acconto tramite i driver di pagamento esistenti, l'approvazione dal banco, l'area cliente
con accesso tramite link, l'interfaccia `BookingChannel` per i canali esterni e la registrazione della domanda persa.
Arenile ha pagina pubblica, widget, area cliente e un'interfaccia dei canali: restano un riferimento di
comportamento, non una fonte di codice (ADR 0001). Dove la specifica tace, le decisioni sono in `docs/m8-plan.md`
§16 (D1–D24); questo ADR registra quelle vincolanti.

## Decisione

1. **Quote online come contatori nel database (§3.3, D3–D5).** Una quota di gruppo per notte è una riga di
   `online_quota_usage` con `CHECK (used >= 0 AND used <= cap)`; la prenotazione la consuma con l'incremento
   condizionale `UPDATE … SET used = used + 1 WHERE used < cap` nella stessa transazione dell'hold, prima di
   `occupancy.AllocateTx`, e scrive le righe di `online_quota_claims` (INV-12). Zero righe aggiornate è quota
   esaurita: la transazione torna indietro e si prova la soluzione successiva. Solo i canali `online` ed `external`
   consumano quota; banco e API la ignorano. Le quote seguono il contratto, non il posto: gli spostamenti dal banco
   non le toccano. Una quota percentuale vale `floor(percentuale × posti attivi del gruppo / 100)`; i gruppi con
   aree dinamiche accettano solo quote a numero. Cambiare una quota riscrive il cap delle notti future come
   `max(nuovo cap, usato)`.
2. **Stati del transito online (§4.1).** Nuove righe della tabella dei transiti: `held → awaiting_approval`
   (`request_approval`), `pending_payment → awaiting_approval` (`payment_authorized`), `awaiting_approval →
   pending_payment` (`approve` senza autorizzazione), `awaiting_approval → confirmed` (`confirm`, solo
   `confirmTx`), `awaiting_approval → cancelled` (`reject`, `expire` alla scadenza, `cancel`),
   `awaiting_approval → awaiting_approval` (`update`) ed `expired → confirmed` (`confirm_late`, solo `confirmTx` con
   `source = provider`, canale online e `late_payment_recovery`). Ogni passaggio resta in `internal/domain/contract`
   con audit e outbox nella stessa transazione (INV-30); il package `booking` orchestra e non scrive mai
   `contracts.status`.
3. **Creazione con hold concreto e idempotenza di dominio (§4.3, D1, D11).** `POST /bookings` sceglie la soluzione
   con la stessa ricerca del banco (canale `online`, solo risorse vendibili online, nessuna deroga di compatibilità,
   nessun segmento) e crea in una transazione il transito `held`, l'hold (`PORTO_HOLD_TTL`), le quote, la riga
   `online_bookings`, audit ed eventi; al massimo tre tentativi su soluzioni diverse. L'`Idempotency-Key` è
   obbligatoria e gestita dal dominio (`online_bookings.idempotency_hash` unico per marina, `x-porto-idempotency:
   domain` esclude il middleware), così nessuna risposta con un token viene salvata. Il token di prenotazione è
   l'HMAC (KeyRing, scopo `booking-token`) dell'id e di un nonce salvato: non è mai memorizzato e si ricalcola per il
   replay.
4. **Conferma anche da un canale verificato (§8.3, D14).** Un canale esterno è il merchant of record: una
   prenotazione verificata (webhook firmato o pull autenticato) segnata come pagata al canale conferma il transito
   con `confirmTx(source = channel)`. `contracts.confirmation_source` accetta `channel`; INV-09 è esteso di
   conseguenza. Le prenotazioni non pagate restano `held` 24 ore per il banco; le modifiche sono rifiutate
   (`modify_unsupported`, D15).
5. **Pagamento con autorizzazione opzionale (§5.3, §6, D6–D8).** Con approvazione e un provider che implementa
   `ext.PaymentAuthorizer` il checkout chiede solo l'autorizzazione (`CheckoutRequest.CaptureManual`); l'intent resta
   `processing` con `authorized_at` (nessuna modifica del CHECK degli stati) e la cattura dopo l'approvazione
   conferma tramite il webhook. Senza autorizzazione l'approvazione viene prima e segue un link di pagamento di 24
   ore; senza alcun provider la prenotazione online è spenta salvo `allow_without_provider` (bonifico e driver
   manuale). L'hold dura fino alla scadenza dell'approvazione (48 ore di default, massimo 144); alla scadenza la
   richiesta è rifiutata in automatico.
6. **Incorporamento e superfici pubbliche (§10, D18, D22).** `/b/{slug}` può essere incorniciato solo dalle origini
   in `marinas.allowed_embed_origins` (`frame-ancestors` per marina); ogni altra risposta resta `frame-ancestors
   'none'` e `X-Frame-Options: DENY`. Il widget comunica con `postMessage` solo verso un'origine padre che sia nella
   lista e coincida con il referrer; il checkout ospitato apre la finestra principale e torna alla pagina di ritorno
   di Porto. Nessun cookie sulle superfici pubbliche: token di prenotazione, di stato e sessione dell'area cliente
   viaggiano in header, il token del link di accesso nel frammento dell'URL. Ogni token sconosciuto, scaduto o altrui
   è `404 link_invalid` (INV-88). Limiti per prefisso IP su ogni operazione pubblica, `max_live_holds` per marina e
   `ext.BotProtection` opzionale su creazione e accesso.
7. **Domanda persa anonima (§9, D17).** Una ricerca senza disponibilità e senza blocco di regole diventa una riga
   `inquiries` (canale `online`, sorgente `public`) senza cliente, barca, IP o user agent, con deduplica in memoria
   di 30 minuti; le prenotazioni online aggiornano l'esito della propria richiesta (`booked`, `lost`,
   `declined_other`).

8. **Storni parziali ripetuti di un abbinamento (§7.2, migrazione `00017`).** Un abbinamento di pagamento si può
   stornare più volte, in parti, fino al suo importo: il rimborso di un soggiorno accorciato storna una parte
   dell'abbinamento e l'annullamento successivo della stessa prenotazione storna il resto. Prima di M8 l'indice unico
   `payment_matches_reverses_uidx` ammetteva un solo storno, e l'annullamento dopo un cambio date con rimborso veniva
   rifiutato. Ora `payment_matches.reversed_cents` è la somma degli storni di un abbinamento, si muove solo con
   l'UPDATE condizionale `reversed_cents + Δ <= amount_cents` dopo il lock del pagamento (READ COMMITTED: lock e poi
   ricontrollo) ed è limitato dal CHECK `payment_matches_reversed_check`; il trigger insert-only degli abbinamenti
   ammette solo la crescita di questa colonna e nessun'altra modifica. `ReverseMatch` senza importo storna quello che
   resta; uno storno oltre il residuo è `validation_failed` (`above_match`), su un abbinamento già stornato per
   intero resta `409 already_reversed`. L'API espone `reversed_cents`; `reversed` vale vero solo a storno completo.

## Conseguenze

- Nessuna doppia vendita tra banco, online e canali: tutti passano da `occupancy.AllocateTx` e dai vincoli unique
  (INV-01, INV-13); le quote reggono la concorrenza su entrambi i dialetti senza lock applicativi.
- SQLite non sa modificare un CHECK: la migrazione `00016` allarga quelli di `contracts.confirmation_source` e
  `consents.kind` riscrivendo il testo della tabella con `writable_schema` (procedura documentata per allentare un
  vincolo, formato su disco invariato), seguita da DDL che fa ricaricare lo schema a ogni connessione; PostgreSQL
  ricrea i vincoli con lo stesso nome.
- I test di confine cambiano: `TestContract_ConfirmedOnlyInConfirmTransit` ammette `confirm` da `awaiting_approval`
  e `confirm_late` da `expired`, entrambi solo in `confirmTx`.
- Integrazioni reali con canali terzi, rimessaggio, cantiere e noleggio online restano fuori da M8.
- La migrazione `00017` sostituisce l'indice unico degli storni con un indice semplice e aggiunge `reversed_cents`
  (valorizzato dagli storni esistenti). Il Down rimette l'indice unico e fallisce di proposito se un abbinamento ha
  più di uno storno, che lo schema precedente non sa rappresentare. INV-50 (somme degli abbinamenti) non cambia;
  INV-52 si estende agli storni (query B52r, `TestConcurrency_PartialReversals`).
