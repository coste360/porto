// Package channel connects the marina to external booking channels through the ext.BookingChannel extensions
// (spec 23.2, 27.5; m8-plan §8): connections with write-only encrypted secrets and a category map, inbound
// reservations applied once (INV-89) through booking.CreateChannelBookingTx (same search, quotas and occupancy as
// the public page), cancellations, the pull and push jobs, and the signed webhook endpoint. Calls to channel
// drivers never run inside a database transaction; acknowledgements go out after commit. Guest data are never
// logged.
//
// CONTRACT phase: types and signatures; ENGINE implements them (and the in-memory fakechannel used by tests).
package channel

import (
	"errors"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

// Job names (m8-plan §8.2).
const (
	JobPull = "channels.pull"
	JobPush = "channels.push"
)

var (
	// ErrUnknownChannel means no registered extension has the code (404).
	ErrUnknownChannel = errors.New("channel: unknown channel")
	// ErrNoConnection means the marina has no enabled connection to the channel (404).
	ErrNoConnection = errors.New("channel: no connection")
	// ErrInvalidSignature means a webhook failed verification (401 invalid_signature).
	ErrInvalidSignature = ext.ErrInvalidSignature
	// ErrVersionMismatch means If-Match did not match the connection version (412 version_conflict).
	ErrVersionMismatch = errors.New("channel: version mismatch")
	// ErrSyncRunning means a pull of the connection is already running (409 conflict).
	ErrSyncRunning = errors.New("channel: sync running")
	// ErrNotImplemented is returned by the CONTRACT skeleton until ENGINE lands the bodies.
	ErrNotImplemented = errors.New("channel: not implemented")
)

// Connection is the marina's connection to one channel. Secrets are never returned.
type Connection struct {
	Enabled          bool
	PushEnabled      bool
	CategoryMap      map[string]string // external code → Porto category
	HasCredentials   bool
	HasWebhookSecret bool
	WebhookURL       string
	LastPushAt       time.Time
	LastPullAt       time.Time
	LastError        string
	Version          int
}

// Info is a registered channel and the marina's connection to it (nil when never configured).
type Info struct {
	Code         string
	Capabilities ext.ChannelCapabilities
	Connection   *Connection
}

// Update creates or updates a connection. Nil Credentials or WebhookSecret keep the stored values.
type Update struct {
	Enabled       bool
	PushEnabled   bool
	CategoryMap   map[string]string
	Credentials   map[string]string
	WebhookSecret *string
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	OK        bool
	ErrorCode string
}

// SyncResult is the outcome of a manual sync.
type SyncResult struct {
	Pulled, Booked, Rejected int
	Pushed                   bool
}

// Reservation is a received reservation in the desk list.
type Reservation struct {
	ID           string
	ChannelCode  string
	ExternalRef  string
	Status       string
	RejectReason string
	ContractID   string
	CreatedAt    time.Time
}

// ReservationFilter filters the desk list of reservations.
type ReservationFilter struct {
	Channel string
	Status  string
	Cursor  string
	Limit   int
}

// ReservationPage is a page of reservations.
type ReservationPage struct {
	Items      []Reservation
	NextCursor string
}
