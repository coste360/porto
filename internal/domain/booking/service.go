// Package booking orchestrates online booking (spec 23; m8-plan §3-§9): public configuration, availability with
// online rules and quotas, quotes, booking creation (held transit + concrete hold + quota claims in one
// transaction), details and consents, documents, checkout through the collect intents, the approval flow, the
// customer-area operations on transits (called by portal), channel bookings (called by channel), lost demand and
// the jobs bookings.approval_expire and bookings.capture.
//
// The package never writes contracts.status: every transit state change goes through the Tx primitives of
// internal/domain/contract (INV-30), which write audit and outbox in the same transaction. Occupancy and quotas are
// DB constraints (INV-01, INV-12): a unique or quota violation makes the attempt roll back and the next solution be
// tried (at most 3 attempts). Confirmation comes only from the provider webhook, the manual driver or a verified
// paid channel reservation (INV-09, ADR 0013).
//
// Public methods resolve the marina from the slug and return ErrLinkInvalid for anything unknown, expired or
// foreign, and when booking is disabled (except Config). Logs and events carry no personal data (INV-70).
package booking

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Log       *slog.Logger
	Keys      *crypto.KeyRing
	Occupancy *occupancy.Service
	Contracts *contract.Service
	Prices    *pricelist.Service
	Collect   *collect.Service
	Drivers   *paydriver.Registry
	Documents *document.Service
	Notify    *notify.Service
	// Payments refunds customer cancellations (m8-plan §7.2); nil = refunds are left to the office.
	Payments *payment.Service
	// BotProtection verifies Porto-Bot-Token on createBooking when registered (nil = none).
	BotProtection ext.BotProtection
	// BaseURL is PORTO_BASE_URL (return pages, customer-area links in emails).
	BaseURL string
	// HoldTTL is PORTO_HOLD_TTL, the lifetime of the hold created by createBooking (m8-plan §16 D1).
	HoldTTL time.Duration
}

// Links issues the reusable customer-area link of a confirmation email (implemented by
// internal/domain/portal, which imports booking: the link is injected, never imported).
type Links interface {
	IssueBookingLinkTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, customerID, contractID string) (string, error)
	LinkURL(slug, token string) string
}

// SessionResolver returns the customer of a valid customer-area session of the marina of slug.
type SessionResolver func(ctx context.Context, slug, sessionToken string) (customerID string, err error)

// Service is the booking service.
type Service struct {
	d              Deps
	links          Links
	resolveSession SessionResolver
	lost           *dedupe
	// beforeCommit runs at the end of each creation attempt (tests force a conflict).
	beforeCommit func(attempt int) error
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.HoldTTL <= 0 {
		d.HoldTTL = 10 * time.Minute
	}
	return &Service{d: d, lost: newDedupe(lostDemandTTL, lostDemandEntries)}
}

// SetPortal wires the customer-area link issuer and session resolver (internal/app, after portal is built).
func (s *Service) SetPortal(links Links, resolve SessionResolver) {
	s.links, s.resolveSession = links, resolve
}

// SetBeforeCommit installs a hook run at the end of each creation attempt, inside its transaction (tests only: an
// error such as occupancy.ErrResourceTaken rolls the attempt back as a lost race would).
func (s *Service) SetBeforeCommit(fn func(attempt int) error) { s.beforeCommit = fn }

// SetPayments wires the payment service used for customer refunds.
func (s *Service) SetPayments(p *payment.Service) { s.d.Payments = p }

// Job names (m8-plan §4.4, §6).
const (
	JobApprovalExpire = "bookings.approval_expire"
	JobCapture        = "bookings.capture"
)

var _ contract.OnlineHooks = (*Service)(nil)
