## 27. API, eventi, webhook ed estensioni

### 27.1 Superfici API

| Prefisso | Uso | Autenticazione |
|---|---|---|
| `/api/admin/v1` | pannello, desktop, integrazioni | sessione, chiave API |
| `/api/device/v1` | desktop remoto | token per dispositivo (`Authorization`) |
| `/api/public/v1/{slug}` | pagina pubblica, widget, scheda del porto | nessuna o link magico; rate limit |
| `/api/ingest/v1` | sensori e gateway | chiave del dispositivo, HMAC |
| `/webhooks/{payments,signature}/{provider}/{marina_id}` | callback dei provider | firma del provider |

Contratto OpenAPI 3.1 in `api/openapi.yaml`, fonte di verità (ADR Arenile 0005): `make gen` genera il server Go e il
client TypeScript. Convenzioni come Arenile (spec Arenile 17): errori `application/problem+json`, `If-Match` per le
risorse versionate, `Idempotency-Key` sulle scritture ripetibili, paginazione a cursore, date `YYYY-MM-DD`, importi
in centesimi, misure in cm, `GET /api/admin/v1/meta` con versione e capacità.

### 27.2 Risorse principali (estratto)

`/marinas`, `/maps`, `/maps/{id}/day`, `/berths`, `/dynamic-areas`, `/land-resources`, `/groups`,
`/occupancies/search`, `/occupancies/move`, `/releases`, `/presences`, `/customers`, `/vessels`, `/documents`,
`/docai/extract`, `/items`, `/price-lists`, `/pricing/simulate`, `/quotes`, `/contracts`, `/installments`,
`/renewal-campaigns`, `/rights`, `/right-transfers`, `/mandates`, `/management-fees`, `/common-charges`,
`/templates`, `/signature-requests`, `/charges`, `/charge-exports`, `/payments`, `/refunds`, `/credits`,
`/cash-sessions`, `/fiscal/daily-totals`, `/reminders`, `/storage/scenarios`,
`/yard/quotes`, `/yard/work-orders`, `/yard/planner`, `/timesheets`, `/stock-movements`, `/equipment-slots`,
`/rental/units`, `/rental/bookings`, `/maintenance/plans`, `/maintenance/tasks`, `/meter-readings`,
`/sensors`, `/reports/*`, `/dashboard`, `/profile`, `/webhook-endpoints`, `/api-keys`, `/devices`, `/stream`.

### 27.3 Eventi di dominio

Scritti nell'outbox nella stessa transazione della modifica (stesso meccanismo e formato di Arenile, spec Arenile
18.1, con `marina_id` al posto di `establishment_id`):

```
occupancy.allocated   occupancy.moved     occupancy.released   occupancy.hold_expired
presence.arrived      presence.departed   release.created      release.resold
quote.sent            quote.accepted      quote.expired
contract.created      contract.activated  contract.signed      contract.renewed   contract.terminated
transit.held          transit.confirmed   transit.cancelled    transit.arrived    transit.departed
installment.charged   installment.overdue
charge.created        charge.paid         charge.exported      charge.invoiced    charge.cancelled
payment.succeeded     payment.failed      payment.refunded
right.transferred     mandate.revenue_accrued
work_order.status_changed  rental.checked_out  rental.returned
maintenance.due       sensor.state_changed  sensor.anomaly
customer.created      customer.anonymized   config.map_changed  config.pricing_changed
```

Webhook in uscita firmati (`Porto-Signature: t=…,v1=…`) con backoff, registro e reinvio (come Arenile 18.2).

### 27.4 SSE

`GET /api/admin/v1/stream`: topic `occupancy.changed`, `presence.changed`, `sensor.changed`, `contract.changed`,
`billing.changed`, `yard.changed`, `rental.changed`; messaggi leggeri `{topic, type, map_id, resource_ids, nights}`
senza dati personali; il client invalida le query interessate. `ReplayBus` con `Last-Event-ID` come Arenile.

### 27.5 Punti di estensione (`pkg/ext`)

```go
type Extensions struct {
    PaymentProviders    []PaymentProvider      // come Arenile
    FiscalDrivers       []FiscalDriver         // come Arenile
    SignatureProviders  []SignatureProvider    // 15.4
    DocumentExtractors  []DocumentExtractor    // 22.3
    SensorSources       []SensorSource         // 24.3
    BookingChannels     []BookingChannel       // 23.2
    ChargeExporters     []ChargeExporter       // 16.5: verso il software di fatturazione esterno
    EventSinks          []EventSink
    Notifiers           []Notifier             // SMS, WhatsApp
    AuthProviders       []AuthProvider
    TenantResolver      TenantResolver
    BotProtection       BotProtection
    HTTPRoutes          func(r chi.Router)
    AdminUIPlugins      []UIPluginManifest
}
```

`BookingChannel` espone `PushAvailability`, `PushRates`, `PullReservations` e `ParseWebhook`; le prenotazioni in
ingresso passano da `contract.CreateTransit` con il canale del provider e occupano una risorsa concreta: un canale non
può mai creare un overbooking (INV-01).
