package sensor

import (
	"errors"
	"time"
)

var (
	// ErrNotImplemented is kept for the error map (501); no INGEST operation returns it any more.
	ErrNotImplemented = errors.New("sensor: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("sensor: version mismatch")
	// ErrCodeTaken is a duplicate device code or external id, gateway code or source name (409 code_taken).
	ErrCodeTaken = errors.New("sensor: code taken")
	// ErrSegmentOverlap is a dynamic-area segment overlapping another active device of the area (422
	// segment_overlap).
	ErrSegmentOverlap = errors.New("sensor: segment overlap")
	// ErrResourceHasSensor is a second active device on a berth or a pedestal socket (409 resource_has_sensor).
	ErrResourceHasSensor = errors.New("sensor: resource has an active sensor")
	// ErrInvalidResource is a resource that cannot carry the device: inactive, of another kind, a surface area, a
	// segment outside the area, an unknown socket (422 invalid_sensor_resource).
	ErrInvalidResource = errors.New("sensor: invalid sensor resource")
	// ErrUnknownDriver is a source code without a registered ext.SensorSource (422 validation_failed on code).
	ErrUnknownDriver = errors.New("sensor: unknown source driver")
	// ErrSourceBusy is a manual poll while the job polls the same source (409 conflict).
	ErrSourceBusy = errors.New("sensor: source busy")

	// ErrIngestUnauthorized is every ingestion authentication failure except the timestamp (401
	// ingest_unauthorized): missing or garbled headers, unknown or revoked key, bad signature, inactive gateway,
	// device key used as bearer. The cause is never told to the client.
	ErrIngestUnauthorized = errors.New("sensor: ingest unauthorized")
	// ErrInvalidPayload is a malformed batch: not JSON, no readings, more than 500 (400 invalid_payload).
	ErrInvalidPayload = errors.New("sensor: invalid payload")
	// ErrBusy is a database busy beyond the ingest budget (503; vendors retry with backoff).
	ErrBusy = errors.New("sensor: database busy")
)

// TimestampError is a valid signature with a timestamp outside the tolerance (401 ingest_timestamp_out_of_range
// with the header Porto-Server-Time).
type TimestampError struct{ ServerTime time.Time }

func (e *TimestampError) Error() string { return "sensor: signature timestamp out of range" }

// RateLimitError is a key over its per-minute budget (429 rate_limited with Retry-After).
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string { return "sensor: rate limited" }
