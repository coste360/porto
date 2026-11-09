# M12 — Sensori e scheda pubblica

Status: Plan phase output. It is authoritative for the M12 builders. If it conflicts with `docs/spec/`, the spec
wins: stop and report to the orchestrator. Open decision D7 of `docs/spec/33-decisioni-aperte.md` (hardware partner
for certification) is **not** decided: Porto ships the open protocol of §4, a fake polled source and a simulator,
and names no vendor. D9 (foreign markets) is untouched. §19 lists every detail decided here where the spec is
silent. CONTRACT records the binding ones in ADR 0017.

Sources: spec 24 and 25 (all), 02 (M12 rows), 04 (`sensor/`, `profile/`), 06.2 (`presences.source = sensor`), 06.8
(`sensor_devices`, `sensor_readings`, `sensor_state_changes`, `marina_profile`), 07 (pedestals, dynamic areas,
GeoJSON positions), 09.2 (context *Sensori*, *Anomalie* with sensor discrepancy), 09.3 (quick card "letture del
sensore"), 10.6 (release policy, presence, early return), 12.5 (transit price matrix, publishable), 20.2
(maintenance tasks), 20.4 (`ext.SensorSource` also named for smart meters), 23 (public page `/b/{slug}`, "Prenota"),
26 (roles; "chiavi dei sensori"), 27.1 (`/api/ingest/v1`), 27.3 (`sensor.state_changed`, `sensor.anomaly`), 27.4
(topic `sensor.changed`), 27.5 (`SensorSources`), 28 (sensor anomalies as alerts), 29 (SSE, i18n), 30 (isolated
ingestion), 31.4 (`PORTO_SENSOR_RAW_RETENTION`), 32 (M12 acceptance). `docs/invariants.md` INV-01..09, 61–64,
70–73. ADR 0001, 0002, 0003, 0004, 0008, 0010, 0011, 0012, 0013, 0014. Plans: `m3-plan.md` §3.4 (releases, revoke),
§4.4 (presences), §7.5 (events), §8.3–8.4 (day view, anomalies); `m6a-plan.md` §2.10–2.11 (LAN listener, peer
keys); `m6b-plan.md` §6.2 (dock phone); `m7-plan.md` §3.0, §6–7 (report machinery, drill, export); `m8-plan.md`
§10–11 (public surface, framing, per-IP limits); `m9-plan.md` §8 (maintenance tasks, map entry endpoint pattern);
`m11-plan.md` (conventions).

## 0. Ground rules (every builder)

1. **Arenile is reference only** (ADR 0001). Arenile has no sensors and no public harbour profile. Never copy
   files, code, comments or tests. Never modify `../arenile`.
2. **Sensors never write occupancy** (new INV-100): no sensor code path inserts, deletes or updates
   `allocations`, `*_occupancies`, `releases`, `contracts` or charges. The only automatic effects are
   `presences`/`vessel_presence` rows with `source = 'sensor'` (only when the marina enables it, §8.4) and
   `sensor_alerts`. Everything else is a **proposal** confirmed by a user through the existing domain services
   (`presence.Record`, `occupancy.CreateRelease`, `occupancy.RevokeRelease`, `maintenance.CreateSensorTaskTx`), with
   that user's permission.
3. **Pure packages**: `internal/domain/sensor/hysteresis`, `internal/domain/sensor/ingestauth` (verification given
   key bytes and `now`), `internal/domain/sensor/reconcile`, `internal/domain/sensor/nights`,
   `internal/domain/profile/jsonld`, `internal/domain/profile/simplemap`, `internal/domain/profile/aggregate`. No
   store, no clock, no `time.Now`, no randomness, no float. Time is always passed in. CONTRACT adds them to
   `pure-engines` in `.golangci.yml`.
4. Units: lengths `geo.Cm` (`distance_cm`, `from_cm`/`to_cm`, depths), WGS84 positions as integers degrees × 10⁷
   (existing `marina.Geo`), battery percent 0..100 int, RSSI dBm int, durations as integer minutes/hours in
   settings. Occupancy expectations use **nights** (ADR 0002) in the marina time zone; a physical reading is an
   instant, mapped to a night only through §8.1 and §10.2.
5. **PostgreSQL READ COMMITTED**: lock first, then re-check under the lock. Lock orders: ingest locks
   `sensor_device_states` rows in ascending `device_id`; reconcile locks the marina's `sensor_settings` row, then
   (through `presence`) `vessel_presence`; alert actions lock the alert row, then the target. SQLite has a single
   writer through the FIFO queue.
6. **Audit and outbox**: every configuration write (devices, keys, gateways, sources, settings, profile), every
   alert decision and every automatic presence writes audit + outbox in the same transaction. **Raw readings are
   not audited and emit no event** (volume). A confirmed physical state change writes one `sensor_state_changes`
   row (the append-only history) and one outbox event `sensor.state_changed` in the ingest transaction; it is not
   duplicated in `audit_log` (§19 D6).
7. Every query is scoped by `marina_id`. **Ingestion resolves the marina from the authenticated device or gateway
   key, never from the payload** (INV-102). Another marina's resource is 404. **No PII** in logs, events, SSE,
   webhooks or public pages (INV-70, INV-103): events carry ids, enums, instants, counts, never vessel names,
   customer data, contract numbers, keys or signatures.
8. Secrets (device keys, gateway keys, source credentials) are random (`crypto/rand`), shown once, stored as a
   SHA-256 hash for lookup and bearer comparison, and, where HMAC verification needs it, as a KeyRing-encrypted
   verifier (§3.3). Comparisons use `crypto/subtle`/`hmac.Equal`.
9. **API first**: CONTRACT edits `api/openapi.yaml` and runs `make gen`. Nobody edits generated files. Migrations
   are `00026_sensors.sql` and `00027_marina_profile.sql`, both dialects, same names, registered in
   `store.BackupTables()` / `BackupExcludedTables()`. Never touch 00001–00025.
10. **UI**: shadcn only (official CLI, exported from `@porto/shared`), Tailwind token classes, no custom CSS, no
    arbitrary values. i18n it (source) and en. No new npm or Go dependency (MQTT, LoRaWAN decoders, image
    libraries stay out; photos use the standard library only).
11. `cmd/porto` stays `CGO_ENABLED=0`. Do not `git commit`. No destructive git, no `rm -rf`. Docker only for your own
    throwaway containers (`scripts/with-postgres.sh`). Build with `-o` into a temp dir. Edit only the files you own
    (§17). While iterating, test only the packages you touch. Run full gates per §17.5.

## 1. Scope

### 1.1 In M12

| Area | Content |
|---|---|
| Devices (24.2) | `sensor_devices`: vendor id, code, kind, model, associated resource (berth, linear dynamic-area segment `from_cm`/`to_cm`, pedestal with optional socket), map position, device key shown once (hash + encrypted verifier), gateway or source binding, active. Health in `sensor_device_states`: battery, RSSI, last contact, firmware, last distance. |
| Gateways (24.2) | `sensor_gateways`: an external LoRaWAN/NB-IoT/MQTT bridge posting decoded readings for the devices bound to it, with its own key. Porto manages only the key and the binding. |
| Ingestion (24.3) | `POST /api/ingest/v1/readings`: batches, HMAC per device or gateway key (gateway may also use bearer), timestamp tolerance, idempotency on `(device_id, observed_at)`, per-key and per-IP rate limits, per-item results. Documented for vendors in `docs/sensor-protocol.md` (§4). |
| Polled sources (24.3) | `ext.SensorSource` + `sensor_sources` (encrypted config, cursor) + job `sensors.poll` feeding the same ingest service. `fakesensor` driver for tests and demo. |
| Hysteresis (24.3) | Pure state machine per device: confirmed state after N agreeing readings or T minutes (defaults 2 / 5), silence → `unknown`. |
| Retention (24.3) | Raw readings kept `PORTO_SENSOR_RAW_RETENTION` (30 d), then deleted in chunks by `sensors.retention`; `sensor_state_changes` kept forever. Raw readings excluded from backup. |
| Reconciliation (24.4) | Pure rules R1–R8 (§8) on physical state × contractual occupancy × presence; alerts and proposals in `sensor_alerts`; automatic presence `out`/`in` only if the marina enables it; release proposal only where `release_policy = with_consent`; early return; silent/low-battery → maintenance proposal. |
| Map (9.2, 24.5) | Context *Sensori* (`GET M/sensors/map`) composed with the day view like *Pagamenti*/*Diritti*; device markers for segments and pedestals; SSE `sensor.changed` (`sensor.state_changed`, `sensor.anomaly`) without PII; anomaly code `sensor_discrepancy` in the server-computed *Anomalie* context. |
| History and report (24.5) | Per-resource physical history (state changes, raw readings within retention, night classification); report `sensors` view `discrepancies` in the M7 machinery (CSV/Excel). |
| Public aggregate occupancy (24.5) | Optional: free berths per published category tonight, never which berths or vessels, cached. |
| Public profile (25) | `marina_profiles` edited in the panel, published on `/p/{slug}` (served by the booking SPA with server-injected head and JSON-LD `Marina`/`Place`), it/en, photos, simplified map without personal data, transit price matrix if published, aggregate occupancy if enabled, "Prenota" button to `/b/{slug}`. Nothing is sent to external directories. |
| Load acceptance (32) | 1,000 simulated sensors, one reading per minute each, on SQLite: accepted reading → SSE state change and operational view freshness ≤ 2 s (§14). |

### 1.2 Out of M12

| Item | Reason / milestone | Seam left |
|---|---|---|
| Choice of a reference hardware partner and certification (D7) | open decision | open protocol, simulator, `ext.SensorSource` |
| Built-in MQTT client, LoRaWAN network-server decoders (TTN/ChirpStack payload formats) | no new dependency; vendors post decoded readings or ship an extension | `ext.SensorSource`, gateway keys |
| Smart utility meters through `SensorSource` (20.4) | utilities readings stay M9 manual/phone | `ext.SensorReading` is occupancy-only; a future `MeterSource` |
| `release_policy = automatic_after_days` (10.6) | not in the M3/M4 enum, CHECK or UI; adding it is a contract-policy change. Even then 24.4 forbids automatic release | R5 works on `with_consent` (§8.3, §19 D14) |
| Automatic release, automatic arrival of transits, automatic maintenance tasks | 24.4: only presence and alerts are automatic | proposals |
| Camera image/plate recognition on Porto's side, AI of any kind | ADR 0008 | readings carry only `state` |
| Email/push for sensor alerts | spec 28 lists them as alerts with owner and state, shown in the panel and phone | `sensor.anomaly` is a public webhook event |
| Directory of harbours, external portolano, sitemap submission | spec 25: out of perimeter | JSON-LD only |
| Custom domain for `/p/` | `ext.TenantResolver` already exists, not wired to `/p/` | handler resolves slug from path only |
| Rental online sale (deferred in M11) | after M12 | unchanged |

## 2. Architecture and packages

```
devices/gateways/sources ─► POST /api/ingest/v1/readings (internal/http/ingest) ─┐
ext.SensorSource ─► job sensors.poll ─────────────────────────────────────────────┤
                                                                                   ▼
                          internal/domain/sensor (Ingest service) ── ingestauth (pure) ── hysteresis (pure)
                            tx: readings ON CONFLICT DO NOTHING · device states · state changes · outbox
                                       │ wake                                   │ outbox.dispatch ─► SSE sensor.changed
                                       ▼                                         ▼
          internal/domain/sensor/watch (Reconcile job, alerts, map, history, report rows) ── reconcile, nights (pure)
             └► presence.RecordSensorTx (automatic, opt-in)   └► on user confirm: presence.Record, occupancy.Create/RevokeRelease,
                                                                  maintenance.CreateSensorTaskTx
          internal/domain/profile ── jsonld, simplemap, aggregate (pure) ──► /api/public/v1/{slug}/profile*, /p/{slug}
```

| Package | Kind | Owner |
|---|---|---|
| `pkg/ext/sensor.go` | `SensorSource`, `SensorReading`, `SensorSourceConfig` | CONTRACT |
| `internal/domain/sensor` | devices, gateways, sources, keys, settings, `Ingest`, poll/silence/retention jobs, seeds helpers | INGEST |
| `internal/domain/sensor/ingestauth` | pure: header parsing, canonical string, HMAC verify, tolerance, key format | INGEST |
| `internal/domain/sensor/hysteresis` | pure state machine | INGEST |
| `internal/domain/sensor/fakesensor` | in-memory `ext.SensorSource` (tests, demo) | INGEST |
| `internal/domain/sensor/watch` | reconcile job, alerts and actions, map, history, discrepancy rows, `occupancy.SensorReader` impl | RECONCILE |
| `internal/domain/sensor/reconcile` | pure: expectation, rules R1–R8, discrepancy | RECONCILE |
| `internal/domain/sensor/nights` | pure: physical night classification from state changes | RECONCILE |
| `internal/domain/profile` | profile CRUD, publish, photos, public views, matrix, occupancy | PUBLIC |
| `internal/domain/profile/{jsonld,simplemap,aggregate}` | pure | PUBLIC |
| `internal/testutil/sensorsim` | deterministic simulator (seeded PRNG, schedule, HMAC client) | INGEST |

Dependencies: `watch` → `sensor` (read helpers via store), `reconcile`, `nights`, `presence` (`RecordSensorTx`,
`Record`), `occupancy` (`CreateRelease`, `RevokeRelease`, read helpers), `maintenance` (`CreateSensorTaskTx`),
`report` (calc rows). `sensor` never imports `watch`; it signals it through a `chan struct{}` wake passed by
`app.go` (non-blocking send). `occupancy` never imports `sensor`: it reads sensor facts through the interface
`occupancy.SensorReader` (nil = no sensors) for the anomaly code. `profile` → `pricelist` (new file
`public_matrix.go`), `harbor` read helpers via store, `filestore`. `profile` never imports `sensor`: the aggregate
reads physical states through `store` read helpers (`SensorPhysicalByBerth`).

## 3. Devices, gateways, sources and keys (normative, INGEST)

### 3.1 `sensor_devices`

| Column | Rule |
|---|---|
| `external_id` | vendor id (DevEUI, serial), `^[A-Za-z0-9._:-]{1,64}$`, unique per marina. This is the `device_id` of the protocol. |
| `code` | `^[A-Z0-9_-]{1,32}$`, unique per marina (operator label, shown on map and lists) |
| `kind` | CHECK `ultrasonic`,`radar`,`magnetometer`,`infrared`,`camera`,`other` |
| `model`, `firmware` | ≤ 64 (firmware is reported by readings or set manually) |
| `resource_type`, `resource_id` | CHECK `berth`,`dynamic_area`,`pedestal`; resource of the same marina, active (composite FKs per type are not possible in one column: validated in the service, re-checked in the tx) |
| `from_cm`, `to_cm` | required iff `dynamic_area`; the area must be `kind = linear`; `0 ≤ from < to ≤ area length`, `to − from ≥ 100`; active segments of one area must not overlap (`422 segment_overlap`) |
| `pedestal_socket` | optional, only with `pedestal`; must be a socket code of the pedestal |
| `map_id`, `x_cm`, `y_cm` | map of the resource; position defaults to the berth anchor, the segment midpoint on the area axis, or the pedestal position; editable inside the map bounds |
| `gateway_id?`, `source_id?` | at most one set. A gateway posts only for devices bound to it; a source feeds only devices bound to it |
| `key_id`, `key_hash`, `key_verifier_enc`, `key_issued_at`, `prev_key_id?`, `prev_key_hash?`, `prev_key_verifier_enc?`, `prev_valid_until?` | §3.3. `key_id` unique globally (it is the lookup) |
| `active`, `version`, timestamps | deactivation keeps history; an inactive device's readings are rejected per item |

Uniqueness: one active device per berth and per `(pedestal, socket)` (partial unique index on `(marina_id,
resource_type, resource_id, coalesce(pedestal_socket,''))` WHERE `active AND resource_type <> 'dynamic_area'`).

Reassigning a device to another resource (or changing the segment) writes a state change to `unknown` with reason
`device_moved` on the old resource and resets the hysteresis (§5.4).

### 3.2 `sensor_gateways`, `sensor_sources`, `sensor_device_states`

- `sensor_gateways`: `code`, `name` (≤ 80), `kind` CHECK `lorawan`,`nbiot`,`mqtt_bridge`,`other`, key columns as
  §3.3 (prefix `po_gw_`), `last_contact_at`, `active`, `version`.
- `sensor_sources`: `code` (registered `ext.SensorSource.Code()`), `name`, `config_enc` (KeyRing, AAD
  `sensor-source:<id>`, never returned; the API returns `config_set: true` and the non-secret keys the driver
  declares), `poll_seconds` 15..3600 (default 60), `cursor` (≤ 512, opaque), `enabled`, `last_polled_at`,
  `last_error_code` (enum, no message text from the vendor), `version`.
- `sensor_device_states` (PK `device_id`, hot row, no `version`): `marina_id`, `physical` CHECK
  `occupied`,`free`,`unknown` (initial `unknown`), `physical_since`, `candidate` CHECK
  `occupied`,`free`,`unknown` or NULL, `candidate_since`, `candidate_count`, `last_observed_at`,
  `last_contact_at` (server receive time), `battery_pct`, `rssi`, `distance_cm`, `firmware`, `updated_at`.
  Written only by `sensor.Ingest`, the silence job and device reassignment.

### 3.3 Key format and lifecycle

- Device key: `po_sns_<kid>_<secret>`; gateway key: `po_gw_<kid>_<secret>`. `kid` = 16 chars lowercase base32
  (10 random bytes), `secret` = 43 chars base64url without padding (32 random bytes). Total 67 (device) / 66
  (gateway) ASCII characters.
- Stored: `key_id = po_sns_<kid>` (public, the `Porto-Key` header), `key_hash = hex(SHA-256(full key))`,
  `key_verifier_enc = KeyRing.Encrypt(full key, AAD "sensor-key:<device_id>")` (gateway AAD
  `"gateway-key:<gateway_id>"`). The plaintext key is never stored and never logged. The verifier is needed because
  HMAC verification requires the shared secret; a DB dump without `PORTO_SECRET_KEY` reveals neither the key nor a
  usable verifier (§19 D3).
- **Issue**: on create (`createSensorDevice`, `createSensorGateway`) the response carries `key` once
  (`Cache-Control: no-store`). Audit `sensor_device.key_issue` with `key_id` only.
- **Rotate** (`rotateSensorDeviceKey`, If-Match): the current key moves to `prev_*` with `prev_valid_until = now +
  grace_hours` (body, 0..168, default 24); the new key is returned once. During grace both verify. Audit + event
  `sensor.changed` (`key_rotated`).
- **Revoke** (`revokeSensorDeviceKey`): clears current and previous key; readings get `401`. A new key is issued
  only by rotate.
- Verification: look up by `key_id` (current or previous within grace); bearer mode compares `SHA-256(presented)`
  with the stored hash using `subtle.ConstantTimeCompare`; HMAC mode decrypts the verifier and compares with
  `hmac.Equal`. Unknown `key_id` still computes a dummy HMAC over the canonical string (comparable work).
- Restore on an instance with a different `PORTO_SECRET_KEY`: verifiers cannot be decrypted, devices answer 401
  `ingest_unauthorized`, and the panel shows "chiave da rigenerare" (`key_status = unusable`). Documented in the
  restore section of `docs/sensor-protocol.md`.

### 3.4 `ext.SensorSource` (`pkg/ext/sensor.go`, CONTRACT)

```go
type SensorReading struct {
    DeviceExternalID string    // the device's external_id
    ObservedAt       time.Time // UTC
    State            string    // occupied | free | unknown
    DistanceCm       *int64
    BatteryPct       *int
    RSSI             *int
    Firmware         string
}
type SensorSourceConfig struct {
    MarinaID string
    Values   map[string]string // decrypted config; secrets inside must never be logged
}
// SensorSource is a polled source of readings (vendor API, MQTT bridge, ...). Outbound HTTP must go through the
// egress client the host puts in the context. Poll must be idempotent for a given cursor.
type SensorSource interface {
    Code() string
    ConfigKeys() (public []string, secret []string)
    Poll(ctx context.Context, cfg SensorSourceConfig, cursor string, limit int) (readings []SensorReading, next string, err error)
}
```

`Extensions.SensorSources []SensorSource`. Job `sensors.poll` (every 15 s, INGEST): for each enabled source whose
`last_polled_at + poll_seconds ≤ now`, call `Poll(limit 500)` with a 20 s timeout, feed the result to
`Ingest(origin = source)`, store `next` and `last_polled_at` in the same transaction as the readings (so a crash
re-polls the same cursor; idempotency makes it harmless). Errors set `last_error_code` (`timeout`, `auth`,
`bad_response`, `unavailable`) without vendor text. Readings for devices not bound to the source are rejected and
counted in the job log (ids only).

## 4. Ingestion protocol (normative, INGEST; copy into `docs/sensor-protocol.md` for hardware vendors)

### 4.1 Request

`POST /api/ingest/v1/readings` over HTTPS (the LAN listener of the desktop edition is accepted: keys, not network
position, authenticate). `Content-Type: application/json`, body ≤ 256 KiB, UTF-8.

```json
{
  "readings": [
    {"device_id": "A81758FFFE0312AB", "observed_at": "2027-06-12T08:30:00Z", "state": "occupied",
     "distance_cm": 142, "battery_pct": 87, "rssi": -97, "firmware": "2.4.1"}
  ]
}
```

| Field | Rule (per item; a violation rejects the item, not the batch) |
|---|---|
| `device_id` | required, the device's `external_id` |
| `observed_at` | required, RFC 3339 with offset; normalised to UTC and **truncated to milliseconds** (idempotency key). Must be ≤ `now + 120 s` (`observed_in_future`) and ≥ `now − min(72 h, raw retention)` (`observed_too_old`) |
| `state` | required, `occupied` \| `free` \| `unknown` |
| `distance_cm` | optional integer 0..10 000 |
| `battery_pct` | optional integer 0..100 |
| `rssi` | optional integer −200..0 |
| `firmware` | optional ≤ 64 |

Batch: 1..500 readings (`400 invalid_payload` otherwise). Unknown JSON fields are ignored (forward compatible).

### 4.2 Authentication headers

| Mode | Headers | Allowed for |
|---|---|---|
| HMAC (recommended) | `Porto-Key: po_sns_<kid>` (or `po_gw_<kid>`), `Porto-Signature: t=<unix seconds>,v1=<hex>` | devices (mandatory), gateways |
| Bearer | `Authorization: Bearer po_gw_<kid>_<secret>` | gateways only (LoRaWAN network servers that cannot sign), HTTPS only |

Canonical string (LF separators, no trailing newline):

```
v1
<t>
POST
/api/ingest/v1/readings
<lowercase hex SHA-256 of the raw request body bytes>
```

`v1 = lowercase hex HMAC-SHA256(key = ASCII bytes of the full key string, message = canonical string)`. Several
`v1=` values may be sent (comma separated, at most 2) during the vendor's own key rollover; any match passes.

Tolerance: `|now − t| ≤ 300 s`. A request with a valid signature but `t` out of range gets `401
ingest_timestamp_out_of_range` with the header `Porto-Server-Time: <unix>`; any other failure (missing header,
unknown key, revoked key, bad signature, device key used as bearer) gets the generic `401 ingest_unauthorized`.
Replay inside the window is harmless: every reading is idempotent on `(device_id, observed_at)` (§4.4).

**Test vector** (normative, `TestIngestAuth_DocVector`):
key `po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8`, `t = 1812789005`
(2027-06-12T08:30:05Z), body (149 bytes, no trailing newline)
`{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"2027-06-12T08:30:00Z","state":"occupied","distance_cm":142,"battery_pct":87,"rssi":-97}]}`
→ body SHA-256 `7cf0c5ecad60883d179adee8544493fd7257aadd2a59255a5e0003ca414212b2`
→ `Porto-Signature: t=1812789005,v1=e5bdc2797062e970aa0d670ef05f915511c015e053da7dabd287de8c6ae88983`.
The key's stored hash is `de05e1d6e0072a34bb3ee2ea4b40d73a19416950304d012e13cf100dd340f98c`.

### 4.3 Scope rules (INV-102)

- The marina is the authenticated key's marina. A device key may post **only its own** `external_id` (other ids →
  item `device_mismatch`). A gateway key may post only devices bound to it (`device_not_bound`). An id unknown in
  that marina → `unknown_device` (same code whether it exists elsewhere or not).
- Inactive device → `device_inactive`. Inactive gateway or revoked key → 401.

### 4.4 Processing (one transaction per request, `sensor.Ingest`)

1. Parse and validate items; sort valid items by `(device_id, observed_at)`.
2. Lock the `sensor_device_states` rows of the involved devices in ascending device id (`FOR UPDATE` on
   PostgreSQL), creating missing rows with `INSERT … ON CONFLICT DO NOTHING` first.
3. `INSERT INTO sensor_readings … ON CONFLICT (device_id, observed_at) DO NOTHING` in chunks of 250, with
   `RETURNING device_id, observed_at` (both dialects support it) to learn which items are new. Items not returned
   are `duplicate`.
4. Fold the **new** readings of each device, in `observed_at` order, through `hysteresis.Step` starting from the
   locked state (§5). Readings older than `last_observed_at` are stored but ignored by the state machine
   (`out_of_order`, still `accepted`).
5. Update health (`last_contact_at = now`, battery, RSSI, distance, firmware from the newest reading of the batch)
   and the hysteresis fields. For each confirmed change insert `sensor_state_changes` and write the outbox event
   `sensor.state_changed` (§9.1). Update `sensor_gateways.last_contact_at` when relevant.
6. Commit, then (only if at least one change was written) non-blocking wake of the outbox dispatcher (already wired
   on commit) and of `sensors.reconcile`.

### 4.5 Responses

| Status | Body |
|---|---|
| `200` | `{"accepted": n, "duplicates": n, "rejected": [{"index": i, "device_id": "…", "code": "unknown_device"}], "server_time": "RFC3339"}` (`accepted` counts new readings, including `out_of_order` ones) |
| `400 invalid_payload` | malformed JSON, 0 or > 500 readings, missing `readings` |
| `401 ingest_unauthorized` / `ingest_timestamp_out_of_range` | §4.2 |
| `413 payload_too_large` | body > 256 KiB |
| `415` | content type is not JSON |
| `429 rate_limited` | `Retry-After` seconds |
| `503` | database busy beyond 10 s (vendors retry with backoff; idempotency makes retries safe) |

Item codes: `unknown_device`, `device_inactive`, `device_mismatch`, `device_not_bound`, `observed_in_future`,
`observed_too_old`, `invalid_state`, `invalid_value`.

### 4.6 Rate limits (in-memory fixed window, per process)

| Key | Limit |
|---|---|
| per client IP prefix, before authentication (`ingestSensorReadings` in `byIP`) | 3,000 / min (gateways behind one NAT) |
| per device key | 30 requests / min |
| per gateway key | 600 requests / min |
| per source poll | its `poll_seconds` (job side) |

The per-key limit is counted right after the `key_id` lookup, before the signature check, so a flood with a valid
`key_id` but bad signatures is capped too. Vendor guidance: one request per reading per device,
or gateway batches every 1–10 s; retry 5xx/429 with exponential backoff (1 s → 60 s) and the same body.

## 5. Hysteresis (normative, INGEST, pure `hysteresis`)

### 5.1 Types

```go
type State struct {
    Confirmed      string    // occupied | free | unknown
    ConfirmedSince time.Time
    Candidate      string    // "" | occupied | free | unknown
    CandidateSince time.Time
    CandidateCount int
    LastObservedAt time.Time
}
type Params struct { Readings int /* N ≥ 1 */; Window time.Duration /* T ≥ 1 min */ }
type Reading struct { ObservedAt time.Time; State string }
type Change struct { From, To string; At time.Time; Reason string /* readings | duration | silence | device_moved */ }
func Step(s State, r Reading, p Params) (State, *Change, Outcome) // Outcome: applied | out_of_order
func Silence(s State, now time.Time, offlineAfter time.Duration) (State, *Change)
func Fold(s State, rs []Reading, p Params) (State, []Change) // = repeated Step over rs sorted by ObservedAt
```

### 5.2 `Step`

1. `r.ObservedAt ≤ LastObservedAt` → unchanged, `out_of_order` (equal instants never reach `Step`: the DB dedupes).
2. `LastObservedAt = r.ObservedAt`.
3. `r.State == Confirmed` → clear the candidate; no change.
4. `r.State == Candidate` → `CandidateCount++`; otherwise `Candidate = r.State`, `CandidateSince = r.ObservedAt`,
   `CandidateCount = 1`.
5. If `CandidateCount ≥ N` (reason `readings`) or `r.ObservedAt − CandidateSince ≥ T` (reason `duration`): emit
   `Change{From: Confirmed, To: Candidate, At: CandidateSince}`, set `Confirmed = Candidate`, `ConfirmedSince =
   CandidateSince`, clear the candidate. `readings` wins when both hold.

`unknown` readings follow the same rule (a device that keeps saying it cannot tell becomes `unknown`). The change
instant is the first agreeing reading, so "free for N hours" (§8) counts from when the berth really emptied.

### 5.3 `Silence` (job `sensors.silence`, every 1 min, INGEST)

If `Confirmed ≠ unknown` and `now − LastObservedAt ≥ offline_after_minutes`: change to `unknown` at
`LastObservedAt + offline_after`, reason `silence`, candidate cleared. The job locks each affected state row and
re-checks `last_observed_at` under the lock (a reading may have arrived), then writes the state change and event as
§4.4 step 5. Batch ≤ 500 devices per transaction.

### 5.4 Cases (N = 2, T = 5 min unless stated; times are `observed_at`)

| # | Confirmed | Candidate (count, since) | Reading | Result |
|---|---|---|---|---|
| H1 | unknown | – | occupied 10:00 | candidate occupied (1, 10:00), no change |
| H2 | unknown | occupied (1, 10:00) | occupied 10:01 | **occupied** at 10:00, reason readings |
| H3 | occupied | – | free 10:05 | candidate free (1, 10:05) |
| H4 | occupied | free (1, 10:05) | occupied 10:06 | candidate cleared (wave), stays occupied |
| H5 | occupied | free (1, 10:05) | free 10:06 | **free** at 10:05 |
| H6 | occupied | free (1, 10:05) | unknown 10:06 | candidate unknown (1, 10:06); stays occupied |
| H7 | occupied (last 10:06) | – | free 09:59 | ignored, `out_of_order` (stored raw) |
| H8 | N = 3: free | occupied (1, 10:00) | occupied 10:06 | **occupied** at 10:00, reason duration |
| H9 | N = 3: free | occupied (2, 10:00) | occupied 10:02 | **occupied** at 10:00, reason readings |
| H10 | N = 1: free | – | occupied 10:00 | **occupied** at 10:00 |
| H11 | occupied, last 10:06, offline 30 | – | (silence, now 10:36) | **unknown** at 10:36, reason silence |
| H12 | unknown (after H11) | – | occupied 10:40, occupied 10:41 | **occupied** at 10:40 |
| H13 | free | – | same reading twice in one batch | the second is a DB duplicate: never counted |
| H14 | free | occupied (1, 10:00) | free 10:01 | candidate cleared |
| H15 | device reassigned | any | – | change to unknown at now, reason device_moved, state reset |

## 6. Settings `sensor_settings` (marina PK; defaults when never saved; `harbor.configure`, If-Match)

| Field | Default | Range | Used by |
|---|---|---|---|
| `hysteresis_readings` (N) | 2 | 1..10 | §5 |
| `hysteresis_minutes` (T) | 5 | 1..60 | §5 |
| `offline_after_minutes` | 30 | 5..1440 | §5.3, map health |
| `silent_alert_minutes` | 180 | 30..10 080, ≥ offline | R7 |
| `battery_low_pct` | 20 | 1..90 | R8 (recovery at + 5) |
| `unexpected_after_minutes` | 15 | 0..240 | R1, R2b, R6 |
| `departure_cutoff_minutes` | 720 (12:00) | 0..1439 | §8.1 departure-day window |
| `presence_effect_enabled` | false | bool | R3, R3b (automatic presence) |
| `presence_out_after_hours` | 6 | 1..168 | R3 |
| `release_proposal_after_days` | 3 | 1..60 | R5 |
| `release_proposal_nights` | 7 | 1..60 | R5 proposed range |
| `version` | | | |

Raw retention is the instance setting `PORTO_SENSOR_RAW_RETENTION` (31.4), shown read-only in the panel (§19 D9).

## 7. Retention and backup (INGEST)

- Job `sensors.retention` (every 1 h, `RunAtStart`): `DELETE FROM sensor_readings WHERE rowid/ctid IN (SELECT …
  WHERE observed_at < now − retention ORDER BY observed_at LIMIT 5000)` per transaction (dialect-specific statement in
  the store), up to 200 chunks per run, yielding between chunks so ingest never waits more than one chunk. Index
  `sensor_readings_observed_idx (observed_at)`.
- `sensor_state_changes`, `sensor_alerts` and device tables are never purged by the job.
- Backup: `sensor_readings` is in `BackupExcludedTables` with the comment "high-volume raw diagnostics, reproducible
  from devices only within 30 days; the history that matters is `sensor_state_changes`" (§19 D8). Every other new
  table is in `BackupTables` (encrypted columns listed as for earlier milestones). Restore keeps device states and
  history; raw-reading views simply show nothing before the restore date.

## 8. Reconciliation (normative, RECONCILE)

### 8.1 Expectation (pure `reconcile.Expect`)

For a device subject (berth, segment or pedestal-socket berth) at instant `now`, with `D = calendar.In(now, loc)`
and local minute `m`:

| Contractual facts (night D, and D − 1) | Expectation |
|---|---|
| active allocation covering D with a vessel; transit `arrived`, or mooring contract and presence `in_port` (or no presence row) | `occupied` |
| allocation covering D; transit `confirmed`/`held` not arrived with first night D | `either` (arrival day) |
| allocation covering D; mooring contract with presence `out`/`ashore`/`yard` | `free` (vessel away) |
| no allocation covering D, an allocation with a vessel covering D − 1 and `m < departure_cutoff_minutes` | `either` (departure day) |
| active release covering D, resold | as the resold allocation (rows above) |
| active release covering D, not resold | `free` |
| block, closure or `maintenance` allocation covering D, or nothing | `free` |

Segments: the facts are the area placements of D (and D − 1) whose interval `[x_cm, x_cm + loa_cm)` intersects
`[from_cm, to_cm)`; with several vessels the expectation is `occupied` if any is `occupied`, else `either` if any is
`either`, else `free`. Rules that act on one vessel (R2, R3, R4, R5) apply only when exactly one placement
intersects. Pedestal: the berth with `pedestal_id = pedestal` and `pedestal_socket = device.pedestal_socket`; without
a socket, the berth only if the pedestal serves exactly one berth, otherwise the device is informational (map and
history, no rules, no discrepancy).

**Discrepancy** (map, anomaly, report): physical `occupied` stable ≥ `unexpected_after_minutes` and expectation
`free`, or physical `free` stable ≥ `unexpected_after_minutes` and expectation `occupied`. `unknown` and `either`
never discrepant.

### 8.2 Rules (pure `reconcile.Evaluate(Input) []Action`)

`Input{Now, Loc, Settings, Subjects []Subject{Device, Physical, PhysicalSince, LastContactAt, BatteryPct, Facts,
Expectation, VesselPresence{State, Source, PresenceID, Since}, Policy{ReleasePolicy}, ActiveRelease?, Transit?},
Alerts []AlertKey}` → actions `OpenAlert{Kind, ConditionKey, Refs}`, `ClearCondition{Kind, ConditionKey}`,
`RecordPresence{VesselID, AllocationID, Event, At}`. `t_occ` / `t_free` = time since the physical state became
occupied / free.

| # | Situation (spec 24.4) | Condition | Action | Condition key | Clears when |
|---|---|---|---|---|---|
| R1 | Occupied without allocation | physical occupied, `t_occ ≥ unexpected_after_minutes`, expectation `free`, not R6 | alert `unexpected_occupied` (dock: "posto occupato non previsto", opens the map focused) | `occ:<physical_since>` | physical not occupied, or expectation becomes `occupied`/`either` |
| R2 | Transit expected today becomes occupied | transit `confirmed` (not arrived), first night ∈ {D, D − 1}, physical occupied with `physical_since ≥` local 00:00 of first night, `t_occ ≥ 0` (hysteresis already applied) | proposal `transit_arrival` (confirm = record arrival) | `contract:<id>` | transit arrived/cancelled, or physical free for ≥ `unexpected_after_minutes` |
| R3 | Annual contract, berth free over N hours | `presence_effect_enabled`, mooring contract with term `annual`, active, covering D, single vessel, vessel presence `in_port` or none, physical free with `t_free ≥ presence_out_after_hours` | **automatic** `RecordPresence(departed, at = physical_since)` via `presence.RecordSensorTx` | – | – |
| R3b | Return after a sensor absence | `presence_effect_enabled`, same contract, vessel presence `out` whose last presence has `source = sensor`, no active release covering D, physical occupied with `t_occ ≥ unexpected_after_minutes` | **automatic** `RecordPresence(arrived, at = physical_since)` | – | – |
| R4 | Return after a staff-recorded departure | as R3b but the last presence is not `sensor` (or the effect is disabled and presence is `out`) | proposal `return_detected` (confirm = record arrival) | `presence:<presence_id>` | presence changes, or physical free |
| R5 | Annual contract free over M days | mooring contract (any term) with `release_policy = with_consent`, vessel presence `out` (any source), physical free with `t_free ≥ release_proposal_after_days × 24 h`, allocation `last_night ≥ D + 1`, no active release covering D + 1 | proposal `release_proposal` with range `[D + 1, min(last_night, expected_return_on − 1 if set, D + release_proposal_nights)]` | `alloc:<id>:<physical_since>` | physical occupied, presence `in_port`, a release covering D + 1 exists, or allocation ends |
| R6 | Released berth occupied before the resale arrival | active release covering D, physical occupied with `t_occ ≥ unexpected_after_minutes`; `resold` = the resold allocation's transit is not arrived | alert `early_return` (`resold: true` → "rientro anticipato su posto rivenduto", office uses the existing move/search flows; `resold: false` → proposal to revoke the release from D) | `release:<id>` | physical free, release revoked/ended, or resale arrived |
| R7 | Sensor silent over T | device active, `now − last_contact_at ≥ silent_alert_minutes` (never contacted: from `created_at`) | proposal `device_silent` (confirm = maintenance task) | `silent:<last_contact_at>` | a new contact |
| R8 | Battery under threshold | last `battery_pct < battery_low_pct` | proposal `battery_low` (confirm = maintenance task) | `battery` | `battery_pct ≥ battery_low_pct + 5` |

`release_policy = never` → no R5. Rules R1–R6 skip informational pedestal devices and devices whose physical state
is `unknown`. R1 and R6 are mutually exclusive (R6 wins). `automatic` effects never touch contracts: the
presence's `allocation_id` is set, `contract.OnArrivalTx`/`OnDepartureTx` are not called (they are no-ops for
mooring contracts anyway), and R3/R3b never apply to transits.

### 8.3 Alerts `sensor_alerts`

Columns: `id`, `marina_id`, `kind` CHECK (`unexpected_occupied`,`transit_arrival`,`return_detected`,
`release_proposal`,`early_return`,`device_silent`,`battery_low`), `device_id`, `resource_type`, `resource_id`,
`map_id`, `condition_key` (≤ 120), `refs` json (ids and enums only: `allocation_id`, `contract_id`, `vessel_id`,
`release_id`, `presence_id`, `resold`, `proposed_from`, `proposed_to`), `status` CHECK
(`open`,`confirmed`,`dismissed`,`resolved`), `opened_at`, `cleared_at?`, `decided_at?`, `decided_by_user_id?`,
`outcome_ref?` (id of the presence, release or maintenance task created on confirm), `note` (≤ 500, never in
events), `version`.

- **One open alert per (device, kind)**: unique partial index `sensor_alerts_open_uidx (device_id, kind) WHERE
  status = 'open'` (INV-104). Reconcile inserts with `ON CONFLICT DO NOTHING`.
- **No reopen of a decided condition**: an alert is opened only if no row with the same `(device_id, kind,
  condition_key)` has `cleared_at IS NULL` (any status). `ClearCondition` sets `cleared_at` on those rows and moves
  `open` ones to `resolved`. So "dismiss" suppresses until the condition clears.
- **Actions** (`POST M/sensors/alerts/{id}/actions`, If-Match, `Idempotency-Key`):

| Action | Kinds | Effect (one tx: lock alert, re-check `open` and the condition still holds, call the domain service, set status, audit, event) | Permission |
|---|---|---|---|
| `dismiss` (`note?`) | all | `dismissed` | `presence.write` |
| `confirm_arrival` | `transit_arrival`, `return_detected` | `presence.Record(arrived, vessel, allocation, at = physical_since)` with the user's principal (transit → `contract.OnArrivalTx`, INV-30) | `presence.write` |
| `confirm_release` (`from`, `to` within the proposal, `consent_confirmed: true` required) | `release_proposal` | `occupancy.CreateRelease(allocation, from, to, source = office)` | `occupancy.move` |
| `revoke_release` | `early_return` with `resold: false` | `occupancy.RevokeRelease(release, from = D)` | `occupancy.move` |
| `create_task` (`priority?`) | `device_silent`, `battery_low` | `maintenance.CreateSensorTaskTx(element_type = sensor, element_id = device, source = sensor)`; idempotent per alert | `maintenance.manage` or `maintenance.report` |

A stale condition gives `409 alert_stale` (the alert is resolved in the same call). Domain errors of the called
service pass through (`409 resource_taken`, `422 …`). Confirm never bypasses the called service's own checks.

### 8.4 Reconcile job `sensors.reconcile` (RECONCILE)

Every 60 s, plus a wake from ingest (debounced: at most one extra run per 2 s). Per marina with active devices:
one transaction that (1) upserts and locks the `sensor_settings` row (`FOR UPDATE` on PostgreSQL; serialises
reconcilers across instances), (2) loads the snapshot with ≤ 8 queries (devices + states; allocations of D and
D − 1 for the device resources joined to contract kind/term/status/policy; area placements of D and D − 1 for the
segment areas; vessel presence + last presence source; releases covering D and D + 1 with resale status; open and
uncleared alerts), (3) `reconcile.Evaluate`, (4) applies actions: alerts insert/clear; `presence.RecordSensorTx`
(which re-reads and locks `vessel_presence`, refusing silently with `ErrInvalidTransition` when another writer got
there first), audit `sensor_alert.open|resolve` and `presence.record` (actor system), events. Budget: ≤ 60 ms for
1,000 devices on SQLite (`BenchmarkReconcile_1000`).

`presence.RecordSensorTx(ctx, tx, scope, vesselID, allocationID, event, at)` (CONTRACT signature and body in
`presence/sensor.go`: `recordTx` with `Source = sensor`, nil principal → audit actor system, no contract hook,
refuses offline-replay context and transits).

## 9. Events, SSE and map context

### 9.1 Events (CONTRACT constants and samples)

| Type | Public | Topic | Data (ids, enums, instants, counts only) |
|---|---|---|---|
| `sensor.state_changed` | yes | `sensor.changed` | `{device_id, resource_type, resource_id, resource_ids: [resource_id], map_id, from_cm?, to_cm?, from, to, at, reason}` |
| `sensor.anomaly` | yes | `sensor.changed` | `{alert_id, kind, status, device_id, resource_type, resource_ids, map_id}` (on open, decide, resolve) |
| `sensor.changed` | no | `sensor.changed` | `{device_id? gateway_id? source_id?, change: created\|updated\|key_rotated\|key_revoked\|deactivated\|settings}` |
| `profile.changed` | no | `marina.changed` | `{marina_id, change: updated\|published\|unpublished\|photo}` |

Never in data: `external_id`, keys, signatures, readings values, battery, vessel names, contract numbers, notes.
Webhook catalogue gains the two public types. SSE message gets `map_id` and `resource_ids` from the data (existing
M3 mechanism). Web: `SSE_TOPICS += 'sensor.changed'`; `keysForMessage`: `sensor.changed` → `sensorMap(m, map_id)`,
`sensorAlerts(m)`, `sensorDevices(m)`, `dayView(m, map_id)` (anomaly counts), debounce 250 ms.

### 9.2 Context *Sensori* (`GET M/sensors/map?map_id=&date?=`, `map.view`)

Response `{map_id, generated_at, entries: [{device_id, code, resource_type, resource_id, from_cm?, to_cm?, x_cm,
y_cm, physical, physical_since, health: ok|offline|battery_low|silent, discrepancy: bool, expectation:
occupied|free|either|none, open_alerts: [kind]}]}`. No vessel or customer data. For a past `date` it returns the
physical state at local 02:00 of that night from the history (§10.2 classification) and `health` omitted. Budget:
≤ 3 queries, ≤ 100 ms for 1,000 devices on SQLite.

Lens (`web/shared/src/map/lenses/sensors.ts`, pure): legend keys `free` (`--harbor-free`, no glyph), `occupied`
(new token `--harbor-sensor-occupied`, glyph `antenna`), `discrepancy` (`--harbor-bad`, glyph `alert`), `offline`
(`--harbor-muted`, glyph `antenna`, badge `!`; also silent and low battery), counts as the legend filter. Berth devices color
the berth; segment and pedestal devices are markers (new `HarborMap` prop `markers`, glyph `antenna`). Composed in
`OpsPage` like the *Pagamenti*/*Diritti* lenses (toggle button `ops-sensors-lens`, `SensorsLegend`). The quick card
(9.3) shows the device code, physical state and since, health, last readings (from history) and open alerts with
their actions.

*Anomalie*: new code `sensor_discrepancy` in `engine.Anomalies` from `AnomalyInput.Sensors map[resourceID]SensorFact{
Physical, StableMinutes, Informational}` filled by `occupancy.SensorReader` (implemented by `watch`; nil → none).
Segment discrepancies are reported on the area element.

### 9.3 Dock phone

`DockPage` gets an **Avvisi** tab (badge = open alerts) listing open sensor alerts (kind, berth code, since) with
"Mostra sulla mappa" and the allowed actions. Online only (not in the offline allowlist).

## 10. History and discrepancy report (RECONCILE)

### 10.1 History `GET M/sensors/history?resource_type=&resource_id=&from=&to=&include_readings=` (`map.view`)

Range ≤ 31 days. Returns state changes in range (+ the state at `from`), alerts touching the resource, the
contractual status per night (from occupancy rows, releases, blocks), the physical classification per night (§10.2)
and, with `include_readings=true` and a range inside retention, raw readings (≤ 10,000, newest first, cursor). Device
history `GET M/sensors/devices/{id}/history` the same for a device.

### 10.2 Physical night (pure `nights.Classify`)

A night D is physically `occupied` if the confirmed state is `occupied` for ≥ 240 of the 480 minutes of
`[D 22:00, D+1 06:00)` local (wall clock, DST-safe: minutes between real instants), `free` if `free` ≥ 240,
otherwise `unknown`. Ties (240/240) are `occupied`.

### 10.3 Report `sensors`, view `discrepancies` (M7 machinery; `reports.operational` or `reports.all`)

Params `from`, `to` (≤ 366 nights), `map_id?`, `group_id?`, `kind?`. One row per `(resource, night)` with a
discrepancy: `night`, `map`, `resource_code`, `resource_type`, `segment?`, `contractual` (`free`, `contract`,
`transit`, `released`, `released_resold`, `block`, `maintenance`), `presence` (state at 02:00), `physical`
(§10.2), `kind` (`occupied_unexpected` = physical occupied ∧ contractual ∈ {free, released, block, maintenance};
`absent` = physical free ∧ contractual ∈ {contract, transit, released_resold}), `alert_ids`. Summary tiles: nights
compared, discrepancies by kind, berths with ≥ 1 discrepancy, sensor coverage (% nights with a non-unknown physical
state). Totals = Σ rows (INV-62 drill equality). CSV/Excel via `report/export`. Rows carry codes only; vessel
names never (the drill links to the berth history).

## 11. Public aggregate occupancy (PUBLIC)

- Enabled per marina by `marina_profiles.show_occupancy` with `occupancy_categories` (berth category codes, 1..20).
- `GET /api/public/v1/{slug}/occupancy` → `{as_of (now truncated to 5 min), night, categories: [{code, name
  (I18n), free, total}]}`; 404 when the profile is unpublished or the option is off.
- Pure `aggregate.Count(berths, rows, physical)`: per published category, `total` = active berths with that
  `price_class`; a berth is **free** when it has no `berth_occupancies` row for tonight (D) **and** it is not in a
  closure **and** its sensor (if any, not informational) is not physically `occupied`. Dynamic areas, segments and
  land resources are not counted. Never ids, codes or positions (INV-103).
- Cache: computed at most once per 60 s per marina (in-process cache), `Cache-Control: public, max-age=60`,
  `ETag`. Rate limit 120/min per IP prefix.

## 12. Public profile (PUBLIC)

### 12.1 `marina_profiles` (PK `marina_id`; panel `GET/PUT M/profile`, read `marina.read`, write `marina.configure`, If-Match)

| Group | Fields (I18n = `{it, en}` text) |
|---|---|
| Publication | `published` (publish/unpublish actions), `published_at`, `locales` (subset of the marina locales, `it` always) |
| Position | `entrance_lat_e7`, `entrance_lon_e7` (WGS84 × 10⁷, required to publish), `address` json (street, postal code, city, province, country), prefilled from `marinas` |
| Contacts | `phone`, `email`, `website` (https), `vhf_channels` (1..4 ints 1..88), `office_hours`, `dock_hours` (weekly `[{weekday 1..7, from "HH:MM", to}]` + `hours_note` I18n ≤ 300) |
| Features | `berths_total` (computed from active berths unless `berths_total_override`), `transit_berths` (computed from berths tagged `transit_only` or online sellable, override allowed), `max_loa_cm`, `depth_entrance_cm`, `depth_quay_cm`, `depth_min_cm`, `depth_max_cm` (`geo.Cm`, min ≤ max), `seabed` CHECK `sand`,`mud`,`rock`,`weed`,`mixed`,`other`, `shelter` (subset of N, NE, E, SE, S, SW, W, NW = winds sheltered from), `access_notes` I18n ≤ 2000 + `access_images` (≤ 4 photos), `lights` I18n ≤ 1000 |
| Services | `services` json, typed: `water`, `power_amps` ([16,32,63,125]), `fuel` `{petrol, diesel, hours I18n}`, `wifi`, `toilets`, `showers`, `laundry`, `pump_out`, `waste`, `travel_lift_kg?`, `crane_kg?`, `slipway`, `yard`, `storage`, `rental`, `parking`, `restaurant`, `shops`, `transport`; booleans default false |
| Media | `photos` (≤ 12: `{file_id, alt I18n, sort}`) |
| Sections | `show_map` + `map_id?` (default the first map), `show_prices`, `show_occupancy` + `occupancy_categories` |
| Meta | `description` I18n ≤ 1000 (meta description), `version` |

Publish requires position, at least one contact and a name (`422 profile_incomplete {missing}`). Photos: upload
`POST M/profile/photos` (multipart, JPEG/PNG ≤ 8 MiB, decoded and **re-encoded with the standard library** to drop
EXIF/GPS metadata, longest side clamped to 2,400 px, stored encrypted through `filestore`), delete by id.

### 12.2 Public API (`/api/public/v1/{slug}/…`, `security: []`, rate limited, no cookies set)

| operationId (path) | Response | Cache-Control | Rate / min / IP prefix |
|---|---|---|---|
| `getPublicProfile` (`/profile?lang=`) | profile without internal fields + `booking_url` when `booking_enabled` + section flags | `public, max-age=300`, ETag = hash(version, lang) | 120 |
| `getPublicProfileMap` (`/profile/map`) | §12.4 | `public, max-age=3600`, ETag = map version | 60 |
| `getPublicProfilePrices` (`/profile/prices?lang=`) | transit matrix (§12.5) | `public, max-age=3600`, ETag = list id + version + date | 60 |
| `getPublicOccupancy` (`/occupancy`) | §11 | `public, max-age=60` | 120 |
| `getPublicProfilePhoto` (`/profile/photos/{fileId}`) | image bytes, only ids referenced by the **published** profile | `public, max-age=31536000, immutable` | 300 |

All return 404 when the profile is unpublished (same body as an unknown slug). `If-None-Match` → 304.
`getPublicMarina` gains `profile_published: bool` so `/b/{slug}` can link "Scheda del porto".

### 12.3 Page `/p/{slug}` (PUBLIC)

- Served by Go (`internal/http/public/profile_page.go`, mounted by CONTRACT next to `/b/*`): the booking SPA
  `index.html` with an injected head: `<title>`, `<meta name="description">`, canonical `/p/{slug}`, `hreflang`
  alternates (`?lang=en`), Open Graph (title, description, first photo URL), and `<script type="application/ld+json">`
  from `jsonld.Build` (JSON marshalled with HTML escaping, so `</script>` cannot break out). Unpublished/unknown →
  404 with `X-Robots-Tag: noindex`. `Cache-Control: public, max-age=300`, ETag; framing as the booking page
  (`EmbedFraming`). In-process cache of the rendered head per `(marina, lang, version)` for 60 s.
- SPA: `routes.ts` gains `{page: 'profile', slug}` for `^/p/([a-z0-9-]{3,63})/?$`; `web/booking/src/profile/**` is a
  lazy chunk (the booking first-load budget of 23.2 is unchanged: the profile code is not in the initial chunk).
  Sections: hero (name, photos carousel with `alt`), contacts (tel/mailto links, VHF), hours, features and depths
  (metres formatted from cm), shelter rose (SVG, tokens), services grid (icons + labels), access notes and images,
  lights, simplified map (§12.4), transit prices (§12.5), aggregate occupancy (§11, refetched every 60 s while
  visible), "Prenota" button (to `/b/{slug}`, hidden when booking is disabled). Language switch it/en. Mobile first,
  accessible (landmarks, headings order, contrast).
- When `meta.local_mode` is true the panel shows that the page is reachable only on the local network (spec 25
  "solo se l'istanza è raggiungibile da internet").

### 12.4 Simplified map (pure `simplemap.Build`)

From the published map: bounds, structures (polylines/polygons with width), berth footprints from
`layout.Footprint` as integer polygons with the berth category code only, dynamic-area outlines, land-resource
outlines without labels, and points of interest from the profile (entrance, fuel, office, pump-out when set as map
points `poi` json in the profile: `{kind, x_cm, y_cm}`). **No** berth codes, occupancy, vessels, customers, sensors
or devices. Polygons simplified to ≤ 32 points per element, coordinates rounded to 10 cm. Rendered in the booking
SPA by a small SVG component with token classes (no dependency on the admin map editor).

### 12.5 Transit price matrix (`pricelist.PublicTransitMatrix`, new file owned by PUBLIC)

When `show_prices` is set: the active price list (on today) with `settings.publish_matrix = true` and contract type
`transit` → `pricing.Matrix` (existing, INV-23) through the pricelist service as system, labels resolved in `lang`.
No list or no flag → section hidden and the endpoint 404s.

### 12.6 JSON-LD (pure `jsonld.Build(profile, marina, lang, baseURL)`)

```json
{"@context": "https://schema.org", "@type": ["Marina", "Place"], "@id": "<base>/p/<slug>#marina",
 "name": "...", "url": "<base>/p/<slug>", "description": "...", "telephone": "...", "email": "...",
 "address": {"@type": "PostalAddress", ...}, "geo": {"@type": "GeoCoordinates", "latitude": "43.7101234", "longitude": "10.4012345"},
 "openingHoursSpecification": [{"@type": "OpeningHoursSpecification", "dayOfWeek": "https://schema.org/Monday", "opens": "08:00", "closes": "19:00"}],
 "amenityFeature": [{"@type": "LocationFeatureSpecification", "name": "Acqua", "value": true}],
 "image": ["<base>/api/public/v1/<slug>/profile/photos/<id>"], "sameAs": ["<website>"],
 "potentialAction": {"@type": "ReserveAction", "target": "<base>/b/<slug>"}}
```

Coordinates formatted from e7 integers with exact decimal formatting (no float). Amenity labels come from a Go table
(it, en) checked against the keys of `profile.json` by `TestJSONLD_LabelsMatchI18n`. `potentialAction` only when
booking is enabled. `baseURL` = `PORTO_BASE_URL` (existing `config.BaseURL`).

## 13. API (CONTRACT writes all; `M` = `/api/admin/v1/marinas/{marinaId}`; tags `sensors`, `sensors-ingest`, `profile`, `profile-public`)

| Area | operationId (path) | Permission |
|---|---|---|
| Ingest | `ingestSensorReadings` (`POST /api/ingest/v1/readings`, `security: []` + documented schemes `sensorHmac`, `gatewayBearer`, `x-porto-max-body-bytes: 262144`) | key (§4) |
| Devices | `listSensorDevices` (filters map, resource, health, active) `createSensorDevice` (`Idempotency-Key`, returns `key` once) `getSensorDevice` `updateSensorDevice` (If-Match) `rotateSensorDeviceKey` `revokeSensorDeviceKey` | read `map.view`; write `harbor.configure` |
| Gateways | `listSensorGateways` `createSensorGateway` `updateSensorGateway` `rotateSensorGatewayKey` `revokeSensorGatewayKey` | `harbor.configure` |
| Sources | `listSensorSources` `createSensorSource` `updateSensorSource` (config write-only) `pollSensorSourceNow` | `harbor.configure` |
| Settings | `getSensorSettings` `putSensorSettings` (If-Match) | read `map.view`; write `harbor.configure` |
| Map | `getSensorMap` | `map.view` |
| Alerts | `listSensorAlerts` (status, kind, map, cursor) `getSensorAlert` `actOnSensorAlert` (If-Match, `Idempotency-Key`) | read `map.view`; actions §8.3 |
| History | `getSensorHistory` `getSensorDeviceHistory` | `map.view` |
| Report | existing `getReport` key `sensors`, view `discrepancies` (+ export) | `reports.operational`/`reports.all` |
| Profile | `getMarinaProfile` `putMarinaProfile` (If-Match) `publishMarinaProfile` `unpublishMarinaProfile` `uploadProfilePhoto` `deleteProfilePhoto` `previewMarinaProfile` (returns the public JSON + JSON-LD of the draft) | read `marina.read`; write `marina.configure` |
| Public | §12.2 operations | none, rate limited |

Problem slugs (+ `errors.<slug>` it/en): `ingest_unauthorized`, `ingest_timestamp_out_of_range`, `invalid_payload`,
`segment_overlap`, `resource_has_sensor`, `invalid_sensor_resource`, `alert_stale`, `alert_action_not_allowed`,
`consent_required`, `profile_incomplete`, `photo_invalid`, plus existing `rate_limited`, `version_conflict`,
`payload_too_large`. Meta capabilities `sensors`, `public_profile`. Jobs: `sensors.poll` (15 s), `sensors.silence`
(1 min), `sensors.retention` (1 h), `sensors.reconcile` (1 min + wake).

## 14. Load acceptance (INGEST; spec 32: 1,000 sensors, 1 reading/min each, SQLite, no operational-view delay > 2 s)

### 14.1 Setup (`TestAcceptance_M12_SensorLoad`, `internal/acceptance/m12_load_test.go`, tag `integration`, skipped with `-short`)

- SQLite **file** database in `t.TempDir()` (WAL, same DSN as production), full app router on `httptest.NewServer`,
  `clock.Real`, real jobs runner (outbox dispatch, reconcile, silence).
- Seed through the API: one marina, a generated map with 1,000 berths, 1,000 devices (500 with device keys posting
  one HMAC request per reading, 500 bound to 10 gateways posting batches every 3 s), settings defaults, presence
  effect enabled. Prefill `sensor_readings` with `PORTO_M12_LOAD_PREFILL_DAYS` (default 1 = 1.44 M rows) of
  synthetic history by direct store bulk insert, so the index is realistic (30 days adds one B-tree level; the
  manual run uses 30).
- Simulator `internal/testutil/sensorsim` (seed 12): device `i` reads at offset `i × 60 ms` every 60 s; each minute
  5 % of devices flip their true state; 3 % of readings are a single dissenting "wave"; 0.5 % of requests are
  replayed (duplicate). Duration `PORTO_M12_LOAD_DURATION` (default 3 min, manual 10 min). The simulator knows,
  for each true flip, which reading completes the hysteresis (the second agreeing reading).

### 14.2 Measurements (all asserted)

| # | Metric | How | Assertion |
|---|---|---|---|
| L1 | Ingest request latency | client side, every request | max ≤ 2 s, p99 ≤ 250 ms, zero 5xx |
| L2 | Accepted → SSE | one SSE client (dock user session) on `/api/admin/v1/stream`; for each expected change, `t_sse − t_accept` where `t_accept` = response received for the completing reading and `t_sse` = arrival of `sensor.state_changed` with that `resource_id` | every expected change seen exactly once; **max ≤ 2 s**; p95 ≤ 500 ms |
| L3 | Operational-view freshness | on each SSE message the client immediately `GET`s `getSensorMap` (as the UI invalidation does) and checks the berth's `physical`; `t_view − t_accept` | **max ≤ 2 s** |
| L4 | View query latency under load | 5 concurrent pollers each doing `getSensorMap` + `getDay` every 2 s | max ≤ 2 s, p95 ≤ 500 ms |
| L5 | Correctness | after drain: readings rows = accepted + prefill, duplicates counted, `sensor_state_changes` = expected flips (no wave produced a change), store check Q40/Q41 empty | exact |
| L6 | Write budget | `sensor.Metrics` (atomic counters: ingest tx count and total tx wall time; reconcile runs and time) | Σ ingest tx time + Σ reconcile time ≤ 30 % of wall time; reconcile run ≤ 250 ms |

The report (p50/p95/p99/max per metric, row counts) is printed with `t.Log`. A PostgreSQL run is optional
(`PORTO_M12_LOAD_PG=1`), not an acceptance gate. Benchmarks: `BenchmarkIngest_Batch500`, `BenchmarkHysteresis_Fold`,
`BenchmarkReconcile_1000`, `BenchmarkSensorMap_1000`.

### 14.3 Write path budget (design target)

1,000 readings/min ≈ 16.7/s. Device-key requests: one tx of 1 insert + 1 state update (+ rare change row + outbox)
≈ 1–3 ms on SQLite. Gateway batches of 50: one tx ≈ 5–10 ms. Expected writer occupancy < 10 %; reconcile adds ≤ 60 ms
per minute plus wake runs (≤ 1 per 2 s, only after changes). Outbox dispatch is woken on commit, so SSE latency is
dominated by the HTTP round trips (target < 100 ms).

## 15. Data (CONTRACT; both dialects, same names; backup registration)

| Migration | Tables / changes | Key constraints |
|---|---|---|
| `00026_sensors` | `sensor_settings`, `sensor_gateways`, `sensor_sources`, `sensor_devices`, `sensor_device_states`, `sensor_readings`, `sensor_state_changes`, `sensor_alerts`; CHECK edits (SQLite `writable_schema` procedure of 00016/00020/00024, PostgreSQL drop/add): `maintenance_tasks.element_type` + `sensor`, `maintenance_tasks.source` + `sensor` | devices unique `(marina_id, external_id)`, `(marina_id, code)`, `key_id` unique, partial unique one active device per berth/socket, CHECK `(resource_type = 'dynamic_area') = (from_cm IS NOT NULL AND to_cm IS NOT NULL)`, CHECK `from_cm < to_cm`, CHECK `NOT (gateway_id IS NOT NULL AND source_id IS NOT NULL)`, CHECK enums, composite FKs `(gateway_id, marina_id)`, `(source_id, marina_id)`, `(map_id, marina_id)`; **readings PK `(device_id, observed_at)`**, index `(observed_at)`, `(marina_id, device_id, observed_at)`; state changes index `(marina_id, resource_id, at)`, `(device_id, at)`; alerts **partial unique `(device_id, kind) WHERE status = 'open'`**, index `(marina_id, status, opened_at)`, `(device_id, kind, condition_key)`; gateways/sources unique `(marina_id, code)` |
| `00027_marina_profile` | `marina_profiles` | PK marina_id, CHECK seabed enum, CHECK depth min ≤ max, CHECK lat/lon ranges (±900 000 000 / ±1 800 000 000) |

Backup: all new tables in `BackupTables()` (order: settings, gateways, sources, devices, states, state changes,
alerts, profiles) except `sensor_readings` in `BackupExcludedTables()` (§7). Store invariant queries
(`store.CheckSensorInvariants`): **Q40** each device state's `last_observed_at` ≥ max(readings.observed_at) of
that device within retention, and `physical` = `to` of its latest state change (or `unknown` with none); **Q41**
state changes of a device alternate (`from` = previous `to`, `at` non-decreasing); **Q42** at most one open alert
per (device, kind) and no open alert with `cleared_at` set; **Q43** no `presences.source = 'sensor'` row for a
transit contract.

Invariants (CONTRACT writes them in Italian in `docs/invariants.md`, status `planned`; owners flip them):
- **INV-100 — I sensori non modificano occupazioni né contratti.** Verified by `TestReconcile_NeverChangesOccupancy`
  (every rule fired on a seeded scenario: hashes of `allocations`, `*_occupancies`, `releases`, `contracts`,
  `charges` unchanged; only `presences` with source sensor and `sensor_alerts` change), the grep test
  `TestSensor_NoOccupancyWrites` (no `store` occupancy/contract write call in `internal/domain/sensor/**` outside
  the confirm actions), Q43. RECONCILE.
- **INV-101 — Una lettura per (dispositivo, istante) e stato fisico come piega delle letture.** PK, Q40, Q41,
  `TestConcurrency_IngestIdempotency`, `TestRapid_HysteresisFold`. INGEST.
- **INV-102 — L'ingestione scrive solo nel marina e sui dispositivi della chiave.** `TestIngest_KeyScopes`,
  `TestIngest_CrossMarina`, `TestAcceptance_M12_IngestIsolation`. INGEST.
- **INV-103 — Pagine e API pubbliche senza dati personali né occupazione per posto.** `TestPublicProfile_NoPII`,
  `TestPublicOccupancy_AggregateOnly`, `TestSimpleMap_NoCodesNoOccupancy`. PUBLIC.
- **INV-104 — Un solo avviso aperto per sensore e tipo; una condizione decisa non si riapre finché non cessa.**
  Partial unique index, Q42, `TestConcurrency_ReconcileAlertsOnce`, `TestReconcile_DismissSuppresses`. RECONCILE.
- **INV-105 — Segreti dei sensori mai in chiaro, mostrati una volta, verificati a tempo costante, finestra anti-replay
  di 300 s.** `TestIngestAuth_*`, `TestSensorKeys_NotStoredOrLogged`. INGEST.
- INV-70 extended to `sensor.*` and `profile.changed` (samples), INV-73 to every new route, INV-72 unchanged.

## 16. UI

### 16.1 RECONCILE (`web/admin/src/features/sensors/**`, lens, dock tab, report page)

- **Sensori** `/m/$id/sensors` (nav group "Porto", `map.view`): tabs *Avvisi* (inbox with filters, action dialogs:
  confirm arrival, confirm release with range picker and the "consenso del cliente acquisito" checkbox, revoke
  release, create task, dismiss with note), *Dispositivi* (table: code, resource, kind, health badge, battery, last
  contact; form with resource picker from the map — berth click, segment drag on a linear area showing metres,
  pedestal + socket; key reveal dialog shown once with copy button and the HMAC example; rotate with grace; revoke),
  *Gateway*, *Sorgenti* (driver select, config fields from `ConfigKeys`, "Interroga ora"), *Impostazioni* (§6, with
  the presence-effect switch explained), and a per-berth **Storico** page `/m/$id/sensors/history?resource=` (timeline
  bar of physical states vs contractual nights, readings table within retention).
- Lens and legend in `OpsPage` (§9.2), markers, quick-card sensor section, dock **Avvisi** tab, report page
  `sensors/discrepancies` inside `features/reports`.

### 16.2 PUBLIC (`web/admin/src/features/profile/**`, `web/booking/src/profile/**`)

- **Scheda pubblica** `/m/$id/profile` (nav settings group, `marina.configure`): sections as §12.1 with it/en
  tabs per I18n field, position picker (lat/lon inputs in decimal degrees parsed to e7 integers by a pure TS helper,
  no floats stored), photos manager (upload, alt texts, reorder), depth inputs in metres converted to cm, services
  checklist, section switches (map, prices with a hint when no list publishes the matrix, occupancy with category
  picker), "Anteprima" opens `/p/{slug}?preview=1` in a new tab: the booking page then loads the draft from
  `previewMarinaProfile` with the panel session cookie (same origin; `marina.configure`; never cached, `noindex`)
  instead of the public endpoint, Publish/Unpublish, public URL with copy, local-mode note.
- Public page per §12.3.

## 17. Ownership, order and acceptance

### 17.1 CONTRACT (runs first, alone; then frozen)

- `api/openapi.yaml`: every operation and schema of §4, §9.2, §10, §12, §13 (ingest documented with
  `x-porto-max-body-bytes`, security schemes `sensorHmac`/`gatewayBearer`), `make gen`.
- Migrations 00026–00027 both dialects; `internal/store/{sensor_models.go,profile_models.go}`; backup registration
  (incl. `BackupExcludedTables` comment); `.golangci.yml` pure rules.
- `pkg/ext/sensor.go` + `Extensions.SensorSources`.
- Go skeletons with signatures and `ErrNotImplemented` bodies: `sensor/{types,service,errors,ingest,jobs}.go`,
  `sensor/ingestauth/{doc,api}.go`, `sensor/hysteresis/{doc,api}.go` (types of §5.1), `sensor/watch/{types,service,
  errors,actions,reader}.go`, `sensor/reconcile/{doc,types,api}.go`, `sensor/nights/api.go`,
  `profile/{types,service,errors,public}.go`, `profile/{jsonld,simplemap,aggregate}/api.go`,
  `pricelist/public_matrix.go` (signature), `maintenance/sensor.go` (signature `CreateSensorTaskTx`).
- Real code (small): `presence/sensor.go` (`RecordSensorTx`, with a `source` parameter threaded into `recordTx`);
  `occupancy.SensorReader` interface, `Deps.Sensors`, the call in `dayview.go` and `engine.AnomalySensorDiscrepancy`
  + `AnomalyInput.Sensors` + the call to `sensorAnomalies(in)` (stub returning nil) in `engine/anomalies.go`.
- Event constants, samples, webhook catalogue, SSE topic `sensor.changed`; problem slugs + `errmap.go`; meta
  capabilities.
- HTTP: `internal/http/ingest/` package skeleton (handler struct, 501), `ingestAPI` in `server.go`, per-IP limiter
  entries for ingest and the public profile operations, `/p/*` route mounted to a `ProfilePage http.Handler` dep
  (PUBLIC implements), `internal/http/admin` fields + 501 stubs `sensors_devices.go`, `sensors_watch.go`,
  `profile.go`, `internal/http/public/profile.go` stubs.
- `app.go` wiring: services, wake channel ingest → reconcile, `SensorReader`, `fakesensor` registered in dev/demo
  and tests only, jobs of §13; seed stubs `seed_sensors.go`, `seed_sensor_alerts.go`, `seed_profile.go` and their
  order in `seed.go` (sensors → alerts → profile).
- Report seam: key `sensors` routed to a stub.
- Web: `queryKeys.ts` (`sensorMap`, `sensorAlerts`, `sensorDevices`, `sensorHistory`, `profile`), hook stubs
  `web/shared/src/api/hooks/{sensors,profile}.ts`, `SSE_TOPICS` + `keysForMessage`, i18n skeletons `sensors.json`,
  `profile.json` (it, en; admin and booking namespaces) + `resources.ts`, `errors.*` keys, `occupancyEnums:
  anomaly.sensor_discrepancy`, `maintenance` keys for element type/source `sensor`, router factories
  `sensorsRoutes`, `profileRoutes`, `nav.ts` entries, `routes.ts` profile route + empty `ProfilePage` lazy import,
  `HarborMap` prop `markers` (+ render of a token-classed antenna glyph), glyph `antenna` in the kit, token
  `--harbor-sensor-occupied` (light/dark, contrast check), `DockPage` tab slot `alerts` rendering an exported stub,
  shadcn components via CLI if missing (`switch`, `carousel` only if the CLI provides it, else plain buttons).
- Docs: ADR `docs/adr/0017-sensori-scheda-pubblica-m12.md` (Italian, "proposta": §3.3, §4, §5, §7 backup choice,
  §8 rules and automatic effects, §11, §12.3 rendering, §19), invariants §15, `docs/architecture.md` §2/§10 rows,
  `docs/sensor-protocol.md` skeleton (INGEST fills).
- Ends with `make gen && make check` green.

### 17.2 Builders (parallel, exclusive files)

| Builder | Owns |
|---|---|
| **INGEST** | `internal/domain/sensor/*.go` (not `watch/`, `reconcile/`, `nights/`), `sensor/ingestauth/**`, `sensor/hysteresis/**`, `sensor/fakesensor/**`; `internal/store/{sensor_devices.go,sensor_gateways.go,sensor_sources.go,sensor_readings.go,sensor_states.go,sensor_invariants.go}` (Q40, Q41); `internal/http/ingest/**`; `internal/http/admin/sensors_devices.go` (devices, gateways, sources, settings) + tests; `internal/testutil/sensorsim/**`; `internal/app/seed_sensors.go` (gateway, fake source, 40 berth devices on one pier, 2 segment devices on a linear area, 1 pedestal device, 6 h of readings through `Ingest` plus back-dated state changes so the alert seed has a 4-day absence, one silent device, one at 12 % battery); `docs/sensor-protocol.md` (§4 verbatim + curl/openssl and Go examples, restore note); `internal/acceptance/m12_ingest_test.go`, `m12_load_test.go`; INV-101, 102, 105 |
| **RECONCILE** | `internal/domain/sensor/watch/**`, `sensor/reconcile/**`, `sensor/nights/**`; body of `occupancy/engine` sensor branch in a new file `engine/anomalies_sensor.go` (+ test); `internal/domain/maintenance/sensor.go` (+ test); tests of `presence/sensor.go`; `internal/store/{sensor_alerts.go,sensor_watch.go,report_sensors.go}` (Q42, Q43); `internal/domain/report/calc/sensors.go` + wiring of key `sensors`; `internal/http/admin/sensors_watch.go` (map, alerts, history) + tests; `internal/app/seed_sensor_alerts.go` (run reconcile once on the seed: one alert of every kind, one sensor presence out); `web/admin/src/features/sensors/**`, `web/shared/src/map/lenses/sensors.ts` (+ test), the sensors additions in `web/admin/src/features/operations/{OpsPage.tsx,components/SensorsLegend.tsx,components/QuickCard.tsx}`, `web/admin/src/features/phone/dock/AlertsTab.tsx`, sensors report additions in `features/reports/**`; `web/shared/src/api/hooks/sensors.ts`; i18n values `sensors` (+ the report and anomaly keys); `internal/acceptance/m12_reconcile_test.go`, `m12_isolation_test.go`; e2e `sensors-admin.spec.ts`, `sensors-map.spec.ts`; INV-100, 104 |
| **PUBLIC** | `internal/domain/profile/**`; `internal/domain/pricelist/public_matrix.go` (+ test); `internal/store/{marina_profile.go,public_profile.go}`; `internal/http/admin/profile.go` + tests; `internal/http/public/{profile.go,profile_page.go}` + tests; `internal/app/seed_profile.go` (published demo profile, 3 generated PNG photos, `publish_matrix` on the demo transit list, occupancy on two categories); `web/admin/src/features/profile/**`; `web/booking/src/profile/**`, the profile entries in `web/booking/src/routes.ts` tests and the "Scheda del porto" link in `web/booking/src/Page.tsx`; `web/shared/src/api/hooks/profile.ts`; i18n values `profile` (admin and booking); `internal/acceptance/m12_profile_test.go`; e2e `public-profile.spec.ts`, `profile-admin.spec.ts`; INV-103 |

### 17.3 Parallel-phase rules

- OpenAPI, migrations, models, `pkg/ext/sensor.go`, event constants, the hysteresis and reconcile types, the
  `SensorReader` and `RecordSensorTx` signatures are frozen. Changes go in the builder's report to the orchestrator.
- INGEST lands `hysteresis` and `sensor.Ingest` first and signals the orchestrator; until then RECONCILE writes
  state changes and device states in its tests through `store` helpers frozen in `sensor_models.go`
  (`InsertSensorStateChange`, `UpsertSensorDeviceState`: CONTRACT writes these simple bodies).
- PUBLIC reads physical states for §11 through the CONTRACT-written `store.SensorPhysicalByBerth` (simple select).
- Nobody edits `occupancy` (except RECONCILE's new engine file), `contract`, `presence` (except tests of
  `sensor.go`), the rest of `maintenance`, `pricelist`, `booking`, `harbor`, `report` machinery outside the listed
  files. Missing hooks are reported.
- Web builders work against the generated client with MSW fixtures until the backend lands.
- Each builder ends with its packages' tests (both dialects for integration), `golangci-lint` clean on its files,
  `npm --prefix web run lint && npm --prefix web run typecheck`, and the `invariants-guardian` subagent.

### 17.4 Tests

**Pure**
- INGEST: `TestHysteresis_Table` (H1–H15), `TestRapid_HysteresisFold` (random N 1..5, T 1..30 min, random readings
  with waves and out-of-order instants; properties: (P1) every change to X is preceded, since its `At`, by
  consecutive X readings with count ≥ N or span ≥ T; (P2) with N ≥ 2 a single dissenting reading between agreeing
  ones never changes state; (P3) changes alternate and `At` is non-decreasing and ≤ last observed; (P4) folding in
  one batch or any split into consecutive batches gives the same state and changes; (P5) removing duplicate instants
  changes nothing; (P6) N = 1 ⇒ confirmed = last reading's state), `TestHysteresis_Silence`,
  `TestHysteresis_Purity`. `TestIngestAuth_DocVector` (§4.2 exactly), `TestIngestAuth_Table` (missing/garbled
  headers, two v1 values, wrong key, previous key in/out of grace, bearer with a device key refused, `t` ± 299/301
  s, body tampering, path tampering), `TestIngestAuth_ConstantTime` (uses `hmac.Equal`/`subtle`; unknown key still
  computes an HMAC — asserted through a counter hook), `TestKeyFormat_Table`.
- RECONCILE: `TestExpect_Table` (every row of §8.1, departure cutoff edges 11:59/12:00, segments with 0/1/2
  placements, pedestal with/without socket), `TestReconcile_Table` (R1–R8 positive and negative cases at the exact
  thresholds: `t − 1 min` no action, `t` action; R1 vs R6 exclusivity; R3 only annual mooring, never transit; R3b
  only after a sensor departure; R5 only `with_consent`, range clamps to `expected_return_on − 1` and
  `last_night`; unknown physical → nothing; informational pedestal → nothing), `TestRapid_ReconcileDeterministic`
  (shuffled subject order → same actions; no action references a resource outside the input), `TestNights_Table`
  (240/480 tie, DST nights 23 h and 25 h measured on real minutes, unknown gaps).
- PUBLIC: `TestJSONLD_Golden` (it, en; with/without booking; coordinates from e7 exact, negative values),
  `TestJSONLD_EscapesScript`, `TestJSONLD_LabelsMatchI18n`, `TestSimpleMap_Golden`,
  `TestSimpleMap_NoCodesNoOccupancy`, `TestAggregate_Table` (rows, closures, sensor occupied, informational device
  ignored, categories not published excluded).

**Integration** (`//go:build integration`, SQLite + PostgreSQL via `testdb`):
- `TestMigrations_M12Constraints`: every PK/unique/partial unique/CHECK (incl. the maintenance CHECK edits),
  Up/Down/Up, parity.
- INGEST: ingest happy path, every item code, batch limits, 401/413/415/429, `out_of_order` stored but no state
  effect, state change + event in the same tx (rollback test), silence job, retention job (chunks, never touches
  state changes), poll job with `fakesensor` (cursor persisted with readings, crash between polls re-reads
  idempotently), key rotate/revoke/grace, `TestSensorKeys_NotStoredOrLogged` (DB columns and captured logs never
  contain the key or its secret part), `TestIngest_KeyScopes`, `TestIngest_CrossMarina` (a key of marina A posting
  B's `external_id` → `unknown_device`, nothing written in B), backup round trip with `sensor_readings` excluded,
  Q40/Q41 after each scenario.
- RECONCILE: every rule end to end on real data; every action (confirm arrival → transit `arrived` through
  contract; confirm release → release row; revoke; create task once per alert; stale → 409 and resolved);
  `TestReconcile_NeverChangesOccupancy`; `TestReconcile_DismissSuppresses`; presence effect off → no presence;
  `getSensorMap`, history (state at `from`, readings only inside retention), report rows = drill (INV-62), day-view
  anomaly `sensor_discrepancy`; `TestSensor_NoOccupancyWrites` (grep); Q42/Q43.
- PUBLIC: profile CRUD, publish preconditions, photo re-encode strips EXIF (fixture JPEG with GPS EXIF → output has
  none), public endpoints 404 when unpublished, ETag/304, `Cache-Control` values, no `Set-Cookie` on any public
  response, matrix only with `publish_matrix`, `/p/{slug}` head contains canonical, hreflang and one JSON-LD script,
  `TestPublicProfile_NoPII` (seeded customers' names, emails, phones, vessel names and berth codes absent from every
  public response and the page HTML), `TestPublicOccupancy_AggregateOnly` (response keys are only the documented
  ones, no ids).
- Cross-cutting: `TestEvents_DataHasNoPII`, `TestSSE_PayloadHasNoPII`, `TestLogs_NoPII` (ingest with a captured
  logger: no keys, signatures, external ids in error logs other than as counts) — samples added by the owners;
  cross-marina sweep for every new admin route (RECONCILE for watch routes, INGEST for device routes, PUBLIC for
  profile routes).

**Concurrency** (both dialects, `-race`, ≥ 100 goroutines):
- `TestConcurrency_IngestIdempotency` (INGEST): 100 goroutines post the same 3-reading batch for one device (half
  HMAC device key, half through its gateway): readings stored once, `accepted` summed over responses = 3, exactly
  one state change and one outbox event.
- `TestConcurrency_IngestSameDevice` (INGEST): 100 goroutines post distinct readings for one device in random
  order: final state = `hysteresis.Fold` of all readings sorted, except out-of-order ones ignored per arrival — the
  test asserts Q40/Q41 and that the change count equals the fold of the readings in **commit order** (recorded by
  `received_at`).
- `TestConcurrency_ReconcileAlertsOnce` (RECONCILE): 100 concurrent reconcile runs (+ ingest wakes) on a scenario
  with every rule: one open alert per (device, kind), one sensor presence per vessel, Q42 empty.
- `TestConcurrency_AlertActionOnce` (RECONCILE): 100 confirms of one `transit_arrival` alert: one presence, one
  transit transition, the rest `409 alert_stale`/`version_conflict`.
- `TestConcurrency_ProfilePublish` (PUBLIC): 100 concurrent PUT/publish with If-Match: one winner per version.

**Acceptance** (`internal/acceptance/m12_*_test.go`, API only, both dialects unless noted):
- `TestAcceptance_M12_SensorLoad` (criterion, SQLite, §14).
- `TestAcceptance_M12_Ingest` (signed requests from the doc vector style, replay, rotation with grace, gateway
  bearer, rate limit 429).
- `TestAcceptance_M12_IngestIsolation` (two marinas, keys and devices never cross; device routes 404 across).
- `TestAcceptance_M12_Reconcile` (`clock.Fake`: unexpected occupied → alert → SSE `sensor.anomaly`; transit arrival
  proposal → confirm → transit arrived; annual vessel away 6 h → presence out (enabled) → 3 days → release proposal
  → confirm with consent → release; return → presence in; early return on resold berth → alert; silence and battery
  → task).
- `TestAcceptance_M12_PublicProfile` (publish, fetch page and APIs anonymously, JSON-LD parse, aggregate counts
  match the seeded rows, matrix equals `getPriceMatrix` cells).

**Web**: Vitest for the sensors lens (legend counts, discrepancy precedence, markers), alerts inbox and action
dialogs (consent checkbox gating), device form (segment picker metres ↔ cm, socket list), key reveal (shown once,
cleared on close), settings validation, the dock Avvisi tab, profile form (e7 parsing helper exact, depth m ↔ cm,
I18n tabs, publish gating), public profile page sections (hidden sections when flags off, booking button rule,
occupancy refetch), `routes.test.ts` (`/p/{slug}`), simplified map renderer.
Playwright (chromium): `sensors-admin.spec.ts` (create device on a berth by map click, key shown once, rotate),
`sensors-map.spec.ts` (post a signed reading pair from the test via `fetch` with Node `crypto`, the berth turns
occupied on the *Sensori* lens within 2 s without reload, alert appears in the inbox and on a 390 px dock phone,
confirm arrival), `profile-admin.spec.ts`, `public-profile.spec.ts` (anonymous context, JSON-LD present, it/en
switch, "Prenota" leads to `/b/{slug}`, no request sets cookies), `a11y.spec.ts` additions per owner.

### 17.5 Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated     # empty
make check
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
go test -count=1 ./internal/domain/sensor/... ./internal/domain/profile/... ./internal/domain/occupancy/engine/...
go test -count=1 ./internal/domain/sensor/hysteresis/... ./internal/domain/sensor/reconcile/... -run 'TestRapid_' -rapid.checks=5000
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/store/... ./internal/domain/sensor/... ./internal/domain/profile/... ./internal/domain/presence/... ./internal/domain/maintenance/... ./internal/domain/pricelist/... ./internal/domain/report/... ./internal/http/... ./internal/backup/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -timeout 30m ./internal/domain/... ./internal/acceptance/... -run '^TestConcurrency_(IngestIdempotency|IngestSameDevice|ReconcileAlertsOnce|AlertActionOnce|ProfilePublish)$'
go test -tags=integration -count=1 -timeout 30m ./internal/acceptance/... -run '^TestAcceptance_M12_SensorLoad$' -v    # SQLite, the M12 criterion
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M12_(Ingest|IngestIsolation|Reconcile|PublicProfile)|TestAcceptance_M11|TestAcceptance_M8|TestAcceptance_M3|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII|TestSSE_PayloadHasNoPII'
make test-db && make test-race
npm --prefix web run test
make e2e            # sensors-admin, sensors-map, profile-admin, public-profile, a11y, plus the existing suite
make licenses && scripts/agents/check-naming.sh
```

Manual check (final report): `make seed && make dev`; open *Sensori* on the ops map (seeded discrepancies and
alerts), sign and post a reading pair with the `docs/sensor-protocol.md` openssl example and watch the berth change
live; confirm a transit arrival from the dock phone at 390 px; confirm a release proposal with consent; open
`/p/{demo-slug}` and `?lang=en` anonymously, validate the JSON-LD with a schema.org validator offline copy or by
eye, follow "Prenota"; run `PORTO_M12_LOAD_DURATION=10m PORTO_M12_LOAD_PREFILL_DAYS=30 go test -tags=integration
-run TestAcceptance_M12_SensorLoad -v ./internal/acceptance/` once and paste the latency table.

## 18. Order of work inside each builder

- INGEST: `ingestauth` + doc vector → `hysteresis` + rapid → store (devices, states, readings) → `Ingest` (signal) →
  HTTP ingest + limits → device/gateway/source admin + keys → silence, retention, poll jobs + `fakesensor` →
  concurrency tests → seed → simulator → load test → protocol doc.
- RECONCILE: `reconcile` (Expect, rules) + `nights` tables → store alerts/watch → reconcile job + presence effect →
  actions → map, history, anomaly branch → report → concurrency tests → seed → admin UI (devices, alerts, settings)
  → lens/markers/quick card → dock tab → e2e.
- PUBLIC: `aggregate`, `simplemap`, `jsonld` (pure, goldens) → store + profile service + photos → public matrix →
  public API + caching → `/p/{slug}` head injection → seed → panel UI → booking profile page → e2e.

## 19. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Packages `internal/domain/sensor` (devices, ingest, jobs) with sub-packages `watch` (reconciliation service), pure `hysteresis`, `ingestauth`, `reconcile`, `nights`, and `internal/domain/profile` with pure `jsonld`, `simplemap`, `aggregate` (spec 04 `sensor/`, `profile/`). |
| D2 | Protocol `device_id` is the vendor id (`external_id`, unique per marina), not Porto's UUID, so firmware needs no Porto-specific id. |
| D3 | HMAC needs a shared secret: keys are stored as a SHA-256 hash (lookup, bearer compare, "salvato come hash") **plus** a KeyRing-encrypted verifier for HMAC; the plaintext is never stored. A restore under another `PORTO_SECRET_KEY` requires re-keying. |
| D4 | Signature: `Porto-Key` + `Porto-Signature: t=…,v1=…` over `v1\nt\nPOST\npath\nsha256(body)`, ±300 s, up to two `v1` values; devices must sign, gateways may sign or use a bearer key over HTTPS. Replay inside the window is harmless thanks to `(device_id, observed_at)` idempotency, so no nonce store. |
| D5 | Batches ≤ 500 readings / 256 KiB; per-item results with HTTP 200; `observed_at` truncated to ms, accepted from `now − min(72 h, retention)` to `now + 120 s`. Rate limits: 30/min per device key, 600/min per gateway key, 3,000/min per IP prefix. |
| D6 | Audit: configuration, keys, alert decisions and automatic presences are audited; raw readings and physical state changes are not (state changes are the append-only `sensor_state_changes` history and emit `sensor.state_changed` in the same tx). |
| D7 | Hysteresis applies uniformly to `occupied`, `free` and `unknown` readings; the change instant is the first agreeing reading; out-of-order readings are stored but never change state; silence (`offline_after_minutes`, 30) forces `unknown`; reassignment resets with `device_moved`. |
| D8 | `sensor_readings` is excluded from backup (high volume, 30-day diagnostics); everything else is backed up. |
| D9 | Raw retention is the instance variable `PORTO_SENSOR_RAW_RETENTION` (31.4), not a per-marina setting; deletion in 5,000-row chunks hourly. |
| D10 | Expectations are tri-state (`occupied`/`free`/`either`): arrival and departure days (before `departure_cutoff_minutes`, 12:00) never raise discrepancies or R1. |
| D11 | Thresholds: unexpected 15 min, presence out after 6 h, release proposal after 3 days of absence, proposed range up to 7 nights (clamped to expected return − 1 and allocation end), silent alert 180 min, battery 20 % with 5-point recovery. All per-marina settings. |
| D12 | Automatic presence (opt-in, default off) only for active `annual` mooring contracts with a single vessel: `departed` after 6 h free (at the instant the berth emptied), `arrived` back only after a sensor-recorded departure; otherwise a `return_detected` proposal. Never for transits; contract hooks not called. |
| D13 | Alerts: one open per (device, kind) by partial unique index; condition keys suppress re-opening a dismissed/confirmed condition until it clears; actions run the existing domain services with the user's permission. |
| D14 | Release proposals only where `release_policy = with_consent`, confirmed by an operator attesting consent; `automatic_after_days` (10.6) is not added in M12 and, per 24.4, would still require confirmation. |
| D15 | Silent/low-battery sensors produce a **proposal**; the maintenance task (`element_type = sensor`, `source = sensor`) is created on confirm, once per alert. |
| D16 | Segments: a device covers `[from_cm, to_cm)` of a linear area (≥ 1 m, no overlap); expectation from placements intersecting the segment; single-vessel rules only with one intersecting placement. Pedestals map to the berth of the device's socket, or the only berth of the pedestal; otherwise informational. |
| D17 | Physical night = majority (≥ 240 of 480 real minutes) of `[22:00, 06:00)` local; used by history, the discrepancy report and the map for past dates. |
| D18 | Permissions: device/gateway/source/settings writes `harbor.configure`; map, alerts, history reads `map.view`; dismiss and arrival confirms `presence.write`; release confirms `occupancy.move`; tasks `maintenance.manage`/`maintenance.report`; report `reports.operational`/`reports.all`; profile write `marina.configure`. No new permission constants. |
| D19 | SSE: topic `sensor.changed` carries `sensor.state_changed`, `sensor.anomaly` and internal `sensor.changed`; device health changes (battery, contact) emit nothing and are polled with the map query. |
| D20 | Public aggregate = berths of the published categories with no occupancy row tonight, not closed, and not physically occupied by a sensor; computed at most every 60 s; no ids or codes. |
| D21 | `/p/{slug}` is the booking SPA (lazy profile chunk) with a Go-injected head (title, description, canonical, hreflang, Open Graph, JSON-LD `["Marina","Place"]`), cacheable 300 s, no cookies; unpublished → 404 noindex. |
| D22 | Simplified map from the published map: structures, berth footprints with category only, outlines and profile POIs; no codes, occupancy, vessels or sensors; ≤ 32 points per element, 10 cm rounding. |
| D23 | Transit price matrix on the page = the engine matrix of the active list flagged `publish_matrix` for contract type `transit` (existing flag). |
| D24 | Profile photos: JPEG/PNG only, re-encoded with the standard library to strip metadata, ≤ 2,400 px, served only when referenced by the published profile, immutable caching by file id. |
| D25 | WGS84 positions stay integers × 10⁷ (existing `marina.Geo`); JSON-LD formats them as exact decimals. JSON-LD amenity labels live in a Go table checked against the i18n keys. |
| D26 | Load acceptance is a Go acceptance test on a SQLite file DB with a real HTTP server, real clock and SSE client, 3 minutes by default (10 min manual) with 1 day of prefilled readings (30 manual); the 2 s bound is asserted on accepted→SSE, accepted→fresh view and view query latency (max), with p95/p99 targets reported. |
| D27 | No external MQTT/LoRaWAN code in core: gateways post decoded readings, other sources are `ext.SensorSource` extensions; `fakesensor` is registered only in dev, demo and tests. |
