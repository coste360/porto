---
title: Sensor ingestion
description: The open protocol through which sensors and gateways send occupancy readings to Porto.
sidebar:
  order: 5
---

Porto is not tied to a vendor: any sensor (ultrasonic, radar, magnetometer, infrared, camera) or gateway (LoRaWAN,
NB-IoT, MQTT bridge) that follows this protocol works. Devices push readings to the Porto instance, which never polls
external services on its own. The normative text, with a test vector, is
[docs/sensor-protocol.md](https://github.com/coste360/porto/blob/main/docs/sensor-protocol.md).

## Endpoint

`POST /api/ingest/v1/readings` over HTTPS, JSON up to 256 KiB, 1 to 500 readings per request. The marina is the one of
the key: the body never names it.

```json
{
  "readings": [
    { "device_id": "A81758FFFE0312AB", "observed_at": "2027-06-12T08:30:00Z", "state": "occupied",
      "distance_cm": 142, "battery_pct": 87, "rssi": -97 }
  ]
}
```

`state` is `occupied`, `free` or `unknown`. A reading is identified by `device_id` and `observed_at`: sending it again
has no effect, so retries are safe.

## Keys

| Key | Format | Use |
|---|---|---|
| Device | `po_sns_<kid>_<secret>` | one per sensor; the sensor signs every request |
| Gateway | `po_gw_<kid>_<secret>` | a gateway posting for the sensors bound to it |

Keys are created in the panel (*Sensori* / Sensors) and shown once. Rotation keeps the previous key valid for a grace
period (24 hours by default); revocation stops both at once.

## Signature

```text
Porto-Key: po_sns_<kid>
Porto-Signature: t=<unix>,v1=<hex HMAC-SHA256(full key, canonical)>
```

where the canonical text is, separated by newlines: `v1`, `t`, `POST`, `/api/ingest/v1/readings` and the lowercase hex
SHA-256 of the body. `t` must be within 300 seconds of the server clock. Gateways that cannot sign may use
`Authorization: Bearer po_gw_…` (HTTPS only).

## Response

`200` with the summary `{"accepted", "duplicates", "rejected": [...], "server_time"}`: an invalid reading is rejected
alone, not the whole batch. Limits: 30 requests per minute per device key, 600 per gateway key; above that you get
`429` with `Retry-After`.

## What Porto does with readings

The physical state of a berth changes only after agreeing readings (by default 2 readings or 5 minutes), so waves are
not mistaken for an arrival. Sensors **never change contracts or bookings**: Porto compares the physical state with
the expected occupancy and proposes actions to the operator. See [Sensors](/en/gestore/sensori/).
