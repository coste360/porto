---
title: Ingestione dei sensori
description: Il protocollo aperto con cui sensori e gateway inviano le letture di occupazione a Porto.
sidebar:
  order: 5
---

Porto non è legato a un produttore: qualsiasi sensore (ultrasuoni, radar, magnetometro, infrarossi, telecamera) o
gateway (LoRaWAN, NB-IoT, ponte MQTT) che rispetti questo protocollo funziona. Sono i dispositivi a inviare le
letture all'istanza di Porto, che non interroga mai servizi esterni da sola. Il testo normativo, con un vettore di
prova, è [docs/sensor-protocol.md](https://github.com/coste360/porto/blob/main/docs/sensor-protocol.md).

## Endpoint

`POST /api/ingest/v1/readings` in HTTPS, JSON fino a 256 KiB, da 1 a 500 letture per richiesta. Il marina è quello
della chiave: il corpo non lo indica mai.

```json
{
  "readings": [
    { "device_id": "A81758FFFE0312AB", "observed_at": "2027-06-12T08:30:00Z", "state": "occupied",
      "distance_cm": 142, "battery_pct": 87, "rssi": -97 }
  ]
}
```

`state` vale `occupied`, `free` o `unknown`. Una lettura è identificata da `device_id` e `observed_at`: inviarla di
nuovo non ha effetti, quindi i tentativi sono sicuri.

## Chiavi

| Chiave | Formato | Uso |
|---|---|---|
| Dispositivo | `po_sns_<kid>_<segreto>` | una per sensore; il sensore firma ogni richiesta |
| Gateway | `po_gw_<kid>_<segreto>` | un gateway che invia per i sensori associati |

Le chiavi si creano nel pannello (*Sensori*) e sono mostrate una sola volta. La rotazione mantiene valida la chiave
precedente per un periodo di grazia (predefinito 24 ore); la revoca le ferma subito.

## Firma

```text
Porto-Key: po_sns_<kid>
Porto-Signature: t=<unix>,v1=<hex HMAC-SHA256(chiave completa, canonico)>
```

dove il testo canonico è, separato da a capo: `v1`, `t`, `POST`, `/api/ingest/v1/readings` e l'hash SHA-256
esadecimale del corpo. `t` deve essere entro 300 secondi dall'ora del server. I gateway che non sanno firmare possono
usare `Authorization: Bearer po_gw_…` (solo in HTTPS).

## Risposta

`200` con il riepilogo `{"accepted", "duplicates", "rejected": [...], "server_time"}`: una lettura non valida è
rifiutata da sola, non l'intero lotto. Limiti: 30 richieste al minuto per chiave di dispositivo, 600 per chiave di
gateway; oltre si riceve `429` con `Retry-After`.

## Che cosa fa Porto con le letture

Lo stato fisico di un posto cambia solo dopo letture concordi (predefinito: 2 letture o 5 minuti), per non scambiare
il moto ondoso per un arrivo. I sensori **non cambiano mai contratti o prenotazioni**: Porto confronta lo stato fisico
con l'occupazione attesa e propone le azioni all'operatore. Vedi [Sensori](/gestore/sensori/).
