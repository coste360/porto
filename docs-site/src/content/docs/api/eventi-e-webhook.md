---
title: Eventi e webhook
description: Webhook in uscita firmati HMAC-SHA256, catalogo degli eventi e aggiornamenti in tempo reale (SSE).
sidebar:
  order: 4
---

Ogni cambio di stato importante (un transito confermato, un addebito pagato, un contratto firmato…) produce un
**evento di dominio**, scritto nella stessa transazione della modifica: se la modifica c'è, c'è anche l'evento. Gli
eventi alimentano i webhook, gli aggiornamenti in tempo reale del pannello e le estensioni.

Gli eventi contengono **solo identificativi e valori enumerati**, mai dati personali: per i dettagli il ricevente
legge la risorsa con l'API.

## Webhook

Gli endpoint si configurano nel pannello (*Webhook*, fino a 20 per marina): URL `https`, eventi a cui iscriversi (o
tutti), attivazione. Alla creazione viene mostrato una sola volta il **segreto** (`whsec_…`).

Ogni consegna è una `POST` JSON:

```json
{
  "id": "evt_…",
  "type": "transit.confirmed",
  "version": 1,
  "marina_id": "…",
  "occurred_at": "2026-07-14T09:12:03Z",
  "data": { "contract_id": "…" }
}
```

con le intestazioni:

| Intestazione | Valore |
|---|---|
| `Porto-Signature` | `t=<unix>,v1=<hex>`: HMAC-SHA256 con il segreto di `"<t>.<corpo>"` |
| `Porto-Event` | il tipo di evento |
| `Porto-Delivery` | l'id della consegna (uguale nei tentativi successivi) |

### Verificare la firma

1. Leggi `t` e i valori `v1` dall'intestazione (durante la rotazione del segreto ce ne sono due: basta che uno
   corrisponda).
2. Calcola `HMAC-SHA256(segreto, t + "." + corpo)` sui byte grezzi del corpo e confrontalo in tempo costante.
3. Rifiuta i messaggi con `t` troppo lontano dall'ora attuale (per esempio oltre 5 minuti).

```js
import { createHmac, timingSafeEqual } from 'node:crypto';

function verify(secret, header, rawBody) {
  const parts = Object.groupBy(header.split(','), (p) => p.split('=')[0]);
  const t = parts.t?.[0]?.slice(2);
  if (!t || Math.abs(Date.now() / 1000 - Number(t)) > 300) return false;
  const expected = createHmac('sha256', secret).update(`${t}.`).update(rawBody).digest();
  return (parts.v1 ?? []).some((p) => {
    const got = Buffer.from(p.slice(3), 'hex');
    return got.length === expected.length && timingSafeEqual(got, expected);
  });
}
```

### Consegne e tentativi

- Una risposta `2xx` entro 10 secondi conferma la consegna; altrimenti Porto riprova con attese crescenti, per 24 ore.
- La consegna è *almeno una volta*: usa `id` o `Porto-Delivery` per ignorare i doppioni.
- Dopo 20 errori consecutivi l'endpoint viene disattivato; lo riattivi dal pannello.
- Il pannello mostra il registro delle consegne (conservato 30 giorni), permette di reinviarle, di inviare un evento
  di prova (`webhook.test`) e di ruotare il segreto (il precedente resta valido 24 ore).
- Gli URL privati, di loopback o locali sono rifiutati, anche dopo la risoluzione DNS e i redirect.

### Catalogo degli eventi

L'elenco completo, con le versioni, è restituito da `GET /api/admin/v1/event-types`. Tra gli altri:

| Area | Eventi |
|---|---|
| Occupazione e presenze | `occupancy.allocated`, `occupancy.moved`, `occupancy.released`, `occupancy.hold_expired`, `release.created`, `release.resold`, `presence.arrived`, `presence.departed` |
| Preventivi, contratti, transiti | `quote.sent`, `quote.accepted`, `quote.expired`, `contract.created`, `contract.activated`, `contract.signed`, `contract.renewed`, `contract.terminated`, `transit.held`, `transit.confirmed`, `transit.cancelled`, `transit.arrived`, `transit.departed`, `transit.approval_requested`, `installment.overdue` |
| Rinnovi | `renewal.proposed`, `renewal.accepted`, `renewal.declined`, `renewal.expired`, `renewal.completed` |
| Diritti d'uso | `right.created`, `right.transferred`, `right.ended`, `mandate.created`, `mandate.ended`, `mandate.revenue_accrued`, `management_fee.issued`, `common_charges.issued` |
| Addebiti e incassi | `charge.created`, `charge.paid`, `charge.cancelled`, `charge.exported`, `charge.invoiced`, `installment.charged`, `payment.succeeded`, `payment.failed`, `payment.refunded` |
| Cantiere e manutenzioni | `yard_quote.accepted`, `work_order.status_changed`, `equipment.booking_changed`, `maintenance.due` |
| Noleggio | `rental.confirmed`, `rental.cancelled`, `rental.checked_out`, `rental.returned` |
| Sensori | `sensor.state_changed`, `sensor.anomaly` |
| Anagrafiche e configurazione | `customer.created`, `customer.anonymized`, `config.map_changed`, `config.pricing_changed`, eventi di team, chiavi API e dispositivi |

## Aggiornamenti in tempo reale (SSE)

`GET /api/admin/v1/stream` è un flusso Server-Sent Events per il marina scelto. I messaggi sono leggeri (argomento,
tipo, id delle risorse e notti interessate) e senza dati personali: servono a sapere **che cosa rileggere**, non
portano i dati. Dopo una disconnessione il client riprende da `Last-Event-ID`. È lo stesso meccanismo che tiene
allineati ufficio, banchina e prenotazioni online.
