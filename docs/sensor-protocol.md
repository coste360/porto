# Porto sensor ingestion protocol (v1)

This document is for hardware vendors and integrators of occupancy sensors (ultrasonic, radar, magnetometer,
infrared, camera) and of the gateways and network servers that relay them. Porto names no hardware partner: any
device, gateway or network server that follows this protocol works. The normative source is `docs/m12-plan.md`
§3.3 and §4; the test vector of §3.4 below is checked by `TestIngestAuth_DocVector`.

Porto works offline and calls no external service: the devices (or their gateway) push readings to the Porto
instance. Porto never polls a vendor cloud by itself; a polled source is a Porto extension (`ext.SensorSource`).

## 1. Endpoint

`POST /api/ingest/v1/readings` over HTTPS. The local-network listener of the desktop edition is accepted too: keys,
not the network position, authenticate a request.

- `Content-Type: application/json`, UTF-8, body at most 256 KiB, 1 to 500 readings per request.
- The marina is the one of the key. The payload never names a marina, and a key never writes in another marina.

## 2. Keys

| Key | Format | Who |
|---|---|---|
| Device key | `po_sns_<kid>_<secret>` (67 characters) | one per device; the device signs every request |
| Gateway key | `po_gw_<kid>_<secret>` (66 characters) | a LoRaWAN, NB-IoT or MQTT bridge posting for the devices bound to it |

- `<kid>` is 16 characters of lowercase base32 (`a`–`z`, `2`–`7`); `<secret>` is 43 characters of base64url without
  padding. `po_sns_<kid>` (or `po_gw_<kid>`) is the public **key id**, sent in the `Porto-Key` header.
- The key is shown **once**, when the device or gateway is created (or its key rotated) in the Porto panel
  (*Sensori → Dispositivi / Gateway*). Store it on the device. Porto keeps only a SHA-256 hash and an encrypted
  verifier: it cannot show the key again.
- **Rotation**: "Rigenera chiave" issues a new key; the previous one keeps working for the grace period chosen
  (0–168 hours, default 24), then stops. **Revocation** stops both keys at once.

## 3. Authentication

### 3.1 HMAC (devices: mandatory; gateways: recommended)

```
Porto-Key: po_sns_<kid>
Porto-Signature: t=<unix seconds>,v1=<hex>
```

The signature is `v1 = lowercase hex HMAC-SHA256(key, canonical)`, where the HMAC key is the ASCII bytes of the
**full** key string and `canonical` is (LF separators, no trailing newline):

```
v1
<t>
POST
/api/ingest/v1/readings
<lowercase hex SHA-256 of the raw request body bytes>
```

- Sign the exact bytes you send: re-serialising the JSON after signing breaks the signature.
- Up to two `v1=` values may be sent (comma separated) during your own key rollover; any match passes.
- `t` must be within **300 seconds** of the Porto server clock. Keep the device clock synchronised (NTP, network
  time of the LoRaWAN/NB-IoT network).

### 3.2 Bearer (gateways only)

```
Authorization: Bearer po_gw_<kid>_<secret>
```

For network servers that cannot sign. Accepted only over HTTPS (or the desktop local-network listener); a device key
is never accepted as a bearer.

### 3.3 Errors

| Status | `type` | When |
|---|---|---|
| `401` | `ingest_unauthorized` | missing or garbled headers, unknown, revoked or expired key, bad signature, inactive gateway, device key used as bearer, bearer over plain HTTP. The cause is never told. |
| `401` | `ingest_timestamp_out_of_range` | the signature is valid but `t` is more than 300 s away; the response carries `Porto-Server-Time: <unix seconds>` |

### 3.4 Test vector

- key `po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8`
- `t = 1812789005` (2027-06-12T08:30:05Z)
- body (149 bytes, no trailing newline):
  `{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"2027-06-12T08:30:00Z","state":"occupied","distance_cm":142,"battery_pct":87,"rssi":-97}]}`
- body SHA-256 `7cf0c5ecad60883d179adee8544493fd7257aadd2a59255a5e0003ca414212b2`
- header `Porto-Signature: t=1812789005,v1=e5bdc2797062e970aa0d670ef05f915511c015e053da7dabd287de8c6ae88983`
- the hash Porto stores for this key: `de05e1d6e0072a34bb3ee2ea4b40d73a19416950304d012e13cf100dd340f98c`

## 4. Readings

```json
{
  "readings": [
    {"device_id": "A81758FFFE0312AB", "observed_at": "2027-06-12T08:30:00Z", "state": "occupied",
     "distance_cm": 142, "battery_pct": 87, "rssi": -97, "firmware": "2.4.1"}
  ]
}
```

| Field | Rule (a violation rejects the item, not the batch) |
|---|---|
| `device_id` | required: the device's vendor id as registered in Porto (DevEUI, serial), `[A-Za-z0-9._:-]{1,64}` |
| `observed_at` | required, RFC 3339 with offset; Porto normalises it to UTC and **truncates it to milliseconds**. At most 120 s in the future, at most 72 hours (or the raw retention, if shorter) in the past |
| `state` | required: `occupied`, `free` or `unknown` (the device cannot tell) |
| `distance_cm` | optional integer 0..10 000 |
| `battery_pct` | optional integer 0..100 |
| `rssi` | optional integer −200..0 (dBm) |
| `firmware` | optional, at most 64 characters |

Unknown fields are ignored (forward compatible).

**Idempotency.** A reading is identified by `(device_id, observed_at)` after the millisecond truncation: sending it
again is harmless and counted as a duplicate. Replaying a whole request inside the 300 s window is therefore
harmless too, and so is retrying after a timeout.

**Physical state.** Porto does not take a single reading at face value: a berth becomes occupied (or free) after 2
agreeing readings or 5 minutes of agreeing readings (the marina may change both), counted from the first agreeing
reading. A device silent for 30 minutes becomes `unknown`. Readings older than the newest one already received are
stored but do not change the state, so send readings in order when you can. Sensors never change contracts or
bookings: the marina staff confirms what the sensors suggest.

**Scope.** A device key may post only its own `device_id`. A gateway key may post only the devices bound to that
gateway in the Porto panel.

## 5. Responses

| Status | Body |
|---|---|
| `200` | `{"accepted": n, "duplicates": n, "rejected": [{"index": i, "device_id": "…", "code": "…"}], "server_time": "RFC 3339"}` — `accepted` counts the new readings stored |
| `400 invalid_payload` | malformed JSON, no `readings`, 0 or more than 500 readings |
| `401` | §3.3 |
| `413` | body larger than 256 KiB |
| `415` | `Content-Type` is not JSON |
| `429 rate_limited` | over the limits of §6; wait `Retry-After` seconds |
| `503` | the database is busy beyond 10 s; retry |

Item codes in `rejected`:

| Code | Meaning |
|---|---|
| `unknown_device` | no device with this id in the key's marina |
| `device_inactive` | the device is deactivated in Porto |
| `device_mismatch` | a device key posted another device's id |
| `device_not_bound` | a gateway key posted a device not bound to that gateway |
| `observed_in_future` | `observed_at` more than 120 s ahead of the server clock |
| `observed_too_old` | `observed_at` older than 72 hours (or the raw retention) |
| `invalid_state` | `state` missing or not one of the three values |
| `invalid_value` | missing or malformed `device_id` or `observed_at`, a value out of range, an item that is not an object |

## 6. Rate limits and retries

| Key | Limit |
|---|---|
| per device key | 30 requests per minute |
| per gateway key | 600 requests per minute |
| per client IP prefix (before authentication) | 3,000 requests per minute (gateways behind one NAT) |

The per-key limit is counted before the signature check. Guidance: one request per reading per device, or gateway
batches every 1–10 seconds. Retry `5xx` and `429` with exponential backoff (1 s, 2 s, 4 s, … up to 60 s) and **the
same body** (idempotency makes it safe; re-sign with a fresh `t` when retrying after more than a few minutes).

## 7. Examples

### 7.1 curl and openssl

```bash
KEY='po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8'   # from the Porto panel
KID="${KEY%_*}"                                                             # po_sns_abcdefghijklmnop
BODY='{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","state":"occupied"}]}'
T=$(date +%s)
BODY_SHA=$(printf '%s' "$BODY" | openssl dgst -sha256 -hex | sed 's/^.* //')
SIG=$(printf 'v1\n%s\nPOST\n/api/ingest/v1/readings\n%s' "$T" "$BODY_SHA" | openssl dgst -sha256 -hmac "$KEY" -hex | sed 's/^.* //')
curl -sS https://porto.example.org/api/ingest/v1/readings \
  -H 'Content-Type: application/json' -H "Porto-Key: $KID" -H "Porto-Signature: t=$T,v1=$SIG" \
  --data-binary "$BODY"
```

A gateway that cannot sign:

```bash
curl -sS https://porto.example.org/api/ingest/v1/readings -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $GATEWAY_KEY" --data-binary "$BODY"
```

### 7.2 Go

```go
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const path = "/api/ingest/v1/readings"

func post(baseURL, key string, body []byte) (*http.Response, error) {
	kid := key[:strings.LastIndex(key, "_")]
	t := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("v1\n" + t + "\nPOST\n" + path + "\n" + hex.EncodeToString(sum[:])))
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Porto-Key", kid)
	req.Header.Set("Porto-Signature", "t="+t+",v1="+hex.EncodeToString(mac.Sum(nil)))
	return http.DefaultClient.Do(req)
}

func main() {
	body := []byte(`{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"` +
		time.Now().UTC().Format(time.RFC3339) + `","state":"free","battery_pct":91}]}`)
	res, err := post("https://porto.example.org", "po_sns_…", body)
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Status)
}
```

## 8. Restore on another instance

The encrypted verifiers are bound to the instance's `PORTO_SECRET_KEY`. After a backup is restored on an instance
with a **different** `PORTO_SECRET_KEY`, the devices and gateways answer `401 ingest_unauthorized` and the panel shows
their key as "chiave da rigenerare" (`key_status = unusable`): rotate each key and load the new one on the device.
A restore on the same instance (same `PORTO_SECRET_KEY`) needs nothing. Raw readings are not part of the backup (they
are diagnostics kept 30 days); the physical history (state changes), devices, gateways and settings are.
