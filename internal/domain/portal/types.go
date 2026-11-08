// Package portal is the customer area of the booking page (spec 23.4; m8-plan §7): magic-link login with the token
// in the URL fragment (single use, 30 minutes; the confirmation email carries a reusable booking link), sessions
// in a header (8 h idle, 7 days absolute, revocable), and the orchestration of the customer-area operations, which
// call the booking service with the session's marina scope and customer. Portal tokens resolve only their marina
// and customer (INV-88); anything else is ErrLinkInvalid (404, no enumeration).
//
// The login request answers the same way whether the email is known or not (no enumeration).
package portal

import (
	"errors"
	"time"

	"github.com/coste360/porto/internal/store"
)

// Lifetimes (m8-plan §7.1, §16 D21).
const (
	LoginLinkTTL       = 30 * time.Minute
	SessionIdleTTL     = 8 * time.Hour
	SessionAbsoluteTTL = 7 * 24 * time.Hour
	// BookingLinkGrace is added to the departure for the reusable link of the confirmation email.
	BookingLinkGrace = 30 * 24 * time.Hour
)

var (
	// ErrLinkInvalid is every unknown, used, expired or foreign link or session (404 link_invalid).
	ErrLinkInvalid = errors.New("portal: link invalid")
	// ErrNotImplemented is returned by the CONTRACT skeleton until ACCOUNT lands the bodies.
	ErrNotImplemented = errors.New("portal: not implemented")
)

// Session is an open customer-area session. Token is returned once by OpenSession and never stored.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Principal is what a valid session resolves to: the marina scope and the customer.
type Principal struct {
	Scope      store.MarinaScope
	CustomerID string
	SessionID  string
}

// Vessel is a saved vessel of the customer.
type Vessel struct {
	ID       string
	Name     string
	LOACm    int64
	BeamCm   int64
	DraftCm  int64
	HullType string
}
