package sensor

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Job names (m12-plan §13).
const (
	JobPoll      = "sensors.poll"      // every 15 s
	JobSilence   = "sensors.silence"   // every 1 min
	JobRetention = "sensors.retention" // every 1 h, at start
)

// KeyLimiter is the per-key rate limit of the ingestion (m12-plan §4.6), counted right after the key_id lookup and
// before the signature check. internal/app wires in-memory fixed windows (30 per minute per device key, 600 per
// gateway key); nil = no per-key limit.
type KeyLimiter interface {
	Allow(keyID string, gateway bool) (ok bool, retryAfter time.Duration)
}

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
	Keys   *crypto.KeyRing
	// Sources are the registered polled sources (ext.Extensions.SensorSources, plus fakesensor in dev, demo and
	// tests).
	Sources []ext.SensorSource
	// RawRetention is PORTO_SENSOR_RAW_RETENTION (default 30 days).
	RawRetention time.Duration
	// Limiter is the per-key limit (nil = none).
	Limiter KeyLimiter
	// Wake is signalled (non-blocking) after a commit that wrote at least one state change: the reconcile job of
	// sensor/watch receives it (nil = none). sensor never imports watch.
	Wake chan<- struct{}
	// HTTP is the anti-SSRF egress client handed to the polled sources (SensorSourceConfig.HTTP). nil: a client
	// with the default egress policy (no private addresses, https only).
	HTTP ext.HTTPDoer
}

// Metrics are the atomic counters of the write path (m12-plan §14.2 L6), read by the load acceptance test.
type Metrics struct {
	IngestTx     atomic.Int64 // committed ingest transactions
	IngestTxNano atomic.Int64 // total wall time of the ingest transactions
	Readings     atomic.Int64 // new readings stored
	Duplicates   atomic.Int64
	Changes      atomic.Int64 // confirmed state changes written
}

// ---- configuration inputs ----

// DeviceInput creates a device (createSensorDevice).
type DeviceInput struct {
	ExternalID, Code, Kind, Model, Firmware string
	ResourceType, ResourceID                string
	FromCm, ToCm                            *int64
	PedestalSocket                          string
	XCm, YCm                                *int64 // default: berth anchor, segment midpoint or pedestal position
	GatewayID, SourceID                     string
}

// Binding is the ingestion path of a device (both empty: the device signs with its own key).
type Binding struct{ GatewayID, SourceID string }

// DevicePatch updates a device (updateSensorDevice); nil fields are unchanged. ResourceType, ResourceID, FromCm,
// ToCm and PedestalSocket move together: a new resource or segment writes a state change to unknown
// (device_moved) and resets the hysteresis.
type DevicePatch struct {
	Code, Kind, Model, Firmware *string
	ResourceType, ResourceID    *string
	FromCm, ToCm                *int64
	PedestalSocket              *string
	XCm, YCm                    *int64
	Binding                     *Binding
	Active                      *bool
}

// GatewayInput creates a gateway.
type GatewayInput struct{ Code, Name, Kind string }

// GatewayPatch updates a gateway; nil fields are unchanged.
type GatewayPatch struct {
	Name, Kind *string
	Active     *bool
}

// SourceInput creates a polled source.
type SourceInput struct {
	Code, Name  string
	Config      map[string]string
	PollSeconds int // 0 = 60
	Enabled     *bool
}

// SourcePatch updates a source; Config replaces the stored values when not nil.
type SourcePatch struct {
	Name        *string
	Config      map[string]string
	PollSeconds *int
	Enabled     *bool
}

// SettingsInput saves the settings (m12-plan §6).
type SettingsInput struct {
	HysteresisReadings, HysteresisMinutes, OfflineAfterMinutes, SilentAlertMinutes int
	BatteryLowPct, UnexpectedAfterMinutes, DepartureCutoffMinutes                  int
	PresenceEffectEnabled                                                          bool
	PresenceOutAfterHours, ReleaseProposalAfterDays, ReleaseProposalNights         int
}

// DeviceFilter filters listSensorDevices.
type DeviceFilter struct {
	MapID, ResourceType, ResourceID, Health string
	Active                                  *bool
}

// ---- views ----

// Health values of a device (m12-plan §9.2).
const (
	HealthOK         = "ok"
	HealthOffline    = "offline"
	HealthBatteryLow = "battery_low"
	HealthSilent     = "silent"
)

// Key statuses (m12-plan §3.3).
const (
	KeyActive   = "active"
	KeyRotating = "rotating"
	KeyRevoked  = "revoked"
	KeyUnusable = "unusable"
)

// DeviceView is a device with its state, health and resource code.
type DeviceView struct {
	store.SensorDevice
	State         *store.SensorDeviceState // nil: never read (physical unknown)
	ResourceCode  string
	Informational bool
	Health        string
	KeyStatus     string
}

// DeviceWithKey is a device and its plaintext key, returned once by create and rotate.
type DeviceWithKey struct {
	Device DeviceView
	Key    string
}

// GatewayView is a gateway with its key status and bound device count.
type GatewayView struct {
	store.SensorGateway
	KeyStatus   string
	DeviceCount int
}

// GatewayWithKey is a gateway and its plaintext key, returned once.
type GatewayWithKey struct {
	Gateway GatewayView
	Key     string
}

// SourceView is a source with its public config values (secret values never leave the service).
type SourceView struct {
	store.SensorSource
	ConfigSet    bool
	ConfigPublic map[string]string
	DeviceCount  int
}

// Driver is a registered ext.SensorSource and its configuration keys.
type Driver struct {
	Code               string
	PublicKeys, Secret []string
}

// Settings are the sensor settings plus the read-only instance retention.
type Settings struct {
	store.SensorSettings
	RawRetentionDays int
}

// ---- ingestion ----

// IngestRequest is one POST /api/ingest/v1/readings, as received (the handler passes the raw body: the signature
// covers its bytes).
type IngestRequest struct {
	Method, Path   string
	Body           []byte
	PortoKey       string // Porto-Key
	PortoSignature string // Porto-Signature
	Authorization  string // Authorization (gateway bearer)
	// Secure is true over TLS or on the desktop LAN listener; a bearer key is refused otherwise.
	Secure bool
}

// Rejected is one refused item of a batch (index in the request).
type Rejected struct {
	Index    int
	DeviceID string // the external id as sent
	Code     string // unknown_device, device_inactive, device_mismatch, device_not_bound, observed_in_future, ...
}

// Item codes of a refused reading (m12-plan §4.5).
const (
	ItemUnknownDevice    = "unknown_device"
	ItemDeviceInactive   = "device_inactive"
	ItemDeviceMismatch   = "device_mismatch"
	ItemDeviceNotBound   = "device_not_bound"
	ItemObservedInFuture = "observed_in_future"
	ItemObservedTooOld   = "observed_too_old"
	ItemInvalidState     = "invalid_state"
	ItemInvalidValue     = "invalid_value"
)

// IngestResult is the per-item result of a batch.
type IngestResult struct {
	Accepted, Duplicates int
	Rejected             []Rejected
	ServerTime           time.Time
}

// PollResult is the result of one poll of a source.
type PollResult struct {
	Accepted, Duplicates, Rejected int
	ErrorCode                      string // timeout, auth, bad_response, unavailable
}

// PhysicalUnknown is the physical state of a device never read.
const PhysicalUnknown = store.SensorUnknown
