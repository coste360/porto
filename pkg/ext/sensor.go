package ext

import (
	"context"
	"net/http"
	"time"
)

// SensorReading is one occupancy reading returned by a polled source (M12, spec 24.3; m12-plan §3.4). It is fed to
// the same ingest service as the signed HTTP readings: idempotent on (device, ObservedAt), validated per item.
type SensorReading struct {
	// DeviceExternalID is the device's external_id (vendor id) in Porto; readings for devices not bound to the
	// source are rejected.
	DeviceExternalID string
	// ObservedAt is the instant of the reading (UTC; truncated to milliseconds by Porto).
	ObservedAt time.Time
	// State is occupied, free or unknown.
	State      string
	DistanceCm *int64
	BatteryPct *int
	RSSI       *int
	Firmware   string
}

// SensorSourceConfig is the configuration of one source of a marina. Values holds the decrypted settings declared
// by ConfigKeys; secret values must never be logged or returned.
type SensorSourceConfig struct {
	MarinaID string
	Values   map[string]string
	// HTTP is the host's anti-SSRF egress client (https only, private and metadata addresses refused after DNS
	// resolution, redirects re-checked). Every outbound HTTP request of the driver must use it; never nil.
	HTTP HTTPDoer
}

// HTTPDoer sends an HTTP request (an *http.Client): the host's egress client handed to the drivers.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// SensorSource is a polled source of occupancy readings (vendor API, MQTT bridge, ...). Outbound HTTP must go
// through SensorSourceConfig.HTTP, the host's egress client. Poll must be idempotent for a given cursor: Porto stores
// next together with the readings, so a crash re-polls the same cursor. Errors carry no vendor text that could
// leak secrets; the host keeps only an error class.
type SensorSource interface {
	// Code is the stable driver code (lowercase, [a-z0-9_-]).
	Code() string
	// ConfigKeys lists the configuration keys: public ones are shown in the panel, secret ones are write-only.
	ConfigKeys() (public []string, secret []string)
	// Poll returns at most limit readings after cursor and the next cursor.
	Poll(ctx context.Context, cfg SensorSourceConfig, cursor string, limit int) (readings []SensorReading, next string, err error)
}
