// Package handover is the rental check-out and check-in (spec 19.4-19.5, m11-plan §7-§10): identity and licence
// typed by the operator (encrypted, purged after the retention), the rental contract signed on screen (or OTP, or
// paper), the deposit (cash, card POS, pre-authorisation, waived), checklist, fuel, engine hours and photos
// (offline kind rental.checklist), damages, the return charge, deposit release or retention (a deposit is never
// revenue until retained, INV-98), quick cash with the commercial document through the FiscalDriver, and the
// handover PDFs. Status changes go only through reservation's Tx methods (INV-97); charges only through
// charge.CreateTx, payments through payment.RecordTx/RefundTx/MatchTx/RetainDepositTx, fiscal documents through
// fiscal.RecordTx. At most one main and one return charge per booking (INV-99). Owned by HANDOVER after the
// contract phase.
package handover

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Reservations are the transitions of reservation called inside the handover transactions (frozen; BOOKING
// implements them in reservation/tx.go, HANDOVER unit-tests against a fake until then).
type Reservations interface {
	LockBookingTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, bookingID string) (*store.RentalBooking, error)
	CheckOutTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, at time.Time) (*store.RentalBooking, error)
	ReturnTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, returnedAt time.Time) (*store.RentalBooking, error)
	CloseTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string) (*store.RentalBooking, error)
	RepriceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string) (*reservation.RepriceResult, error)
	CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID, reason string) (*store.RentalBooking, error)
}

var _ Reservations = (*reservation.Service)(nil)

// BookingViews returns the booking views answered by check-out, check-in and close (reservation.Service).
type BookingViews interface {
	Get(ctx context.Context, p *auth.Principal, marinaID, bookingID string) (*reservation.BookingView, error)
}

var _ BookingViews = (*reservation.Service)(nil)

// Deps are the collaborators of the service.
type Deps struct {
	DB           *store.DB
	Clock        clock.Clock
	Audit        *audit.Writer
	Outbox       *event.Outbox
	Log          *slog.Logger
	Keys         *crypto.KeyRing
	Files        *filestore.Store
	Reservations Reservations
	Bookings     BookingViews // booking views returned by check-out, check-in and close
	Fleet        *fleet.Service
	Charges      *charge.Service
	Payments     *payment.Service
	Cash         *cash.Service
	Fiscal       *fiscal.Service
	Collect      *collect.Service
	Docs         *docs.Service
	Signatures   *signature.Service
}

// Service is the rental handover service. It implements collect.RentalDepositHooks and signature.Subject (kind
// rental), and is the docs data source of the rental_contract and rental_handover kinds.
type Service struct {
	d Deps
}

// NewService returns the service and registers it as the data source of the rental documents.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d}
	if d.Charges != nil {
		d.Charges.SetCancelHook(store.ChargeOriginRental, s.onChargeCancelledTx)
	}
	if d.Docs != nil {
		d.Docs.RegisterDataSource(dataSource{s: s})
	}
	return s
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// authorize checks perm on the marina (another marina is auth.ErrNotFound) and that each (table, id) pair of refs
// exists in the marina, so another marina's ids are 404 before anything else.
func (s *Service) authorize(ctx context.Context, p *auth.Principal, marinaID string, perm auth.Permission, refs ...string) (store.MarinaScope, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	if err != nil {
		return scope, err
	}
	for i := 0; i+1 < len(refs); i += 2 {
		if err := store.RentalExists(ctx, s.d.DB.DB, scope, refs[i], refs[i+1]); err != nil {
			return scope, notFound(err)
		}
	}
	return scope, nil
}

func (s *Service) bookingScope(ctx context.Context, p *auth.Principal, marinaID, bookingID string) (store.MarinaScope, error) {
	return s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableBookings, bookingID)
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// location returns the marina time zone (Europe/Rome when unknown).
func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	if m, err := store.GetMarina(ctx, db, scope); err == nil && m.Timezone != "" {
		if loc, err := calendar.LoadLocation(m.Timezone); err == nil {
			return loc
		}
	}
	loc, err := calendar.LoadLocation("Europe/Rome")
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return calendar.In(s.d.Clock.Now(), s.location(ctx, db, scope))
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

// changed writes rental.changed for a booking (ids, enums and dates only, INV-70).
func (s *Service) changed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.RentalBooking, change string) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.RentalChanged, event.RentalData{
		BookingID: b.ID, UnitID: b.UnitID, SkipperID: b.SkipperID, Status: b.Status, Change: change,
		StartsOn: b.StartsOn.String(), EndsOn: b.EndsOn.String(),
	})
	return err
}

// snapshotOf decodes the price snapshot of a booking.
func snapshotOf(b *store.RentalBooking) price.Snapshot {
	var snap price.Snapshot
	if len(b.PriceSnapshot) > 0 {
		_ = json.Unmarshal(b.PriceSnapshot, &snap)
	}
	return snap
}

// fiscalMode is the fiscal mode of a booking's charges (§9.1).
func fiscalMode(b *store.RentalBooking) string {
	if b.InvoiceRequested {
		return charge.FiscalInvoice
	}
	return charge.FiscalReceipt
}

// bookingRange returns the range of a booking.
func bookingRange(b *store.RentalBooking) slots.Range {
	return slots.Range{Start: slots.Point{Date: b.StartsOn, Slot: b.StartSlot}, End: slots.Point{Date: b.EndsOn, Slot: b.EndSlot}}
}

// lock locks a booking through reservation (lock order: booking first) and checks the expected status and version
// (expected <= 0 skips the version check).
func (s *Service) lock(ctx context.Context, tx bun.IDB, scope store.MarinaScope, bookingID string, expected int, statuses ...string) (*store.RentalBooking, error) {
	b, err := s.d.Reservations.LockBookingTx(ctx, tx, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	if expected > 0 && b.Version != expected {
		return nil, ErrVersionMismatch
	}
	if len(statuses) > 0 {
		ok := false
		for _, st := range statuses {
			if b.Status == st {
				ok = true
			}
		}
		if !ok {
			return nil, ErrInvalidTransition
		}
	}
	return b, nil
}

// view returns the booking view after a transition: the reservation view when available, else the stored row.
func (s *Service) view(ctx context.Context, p *auth.Principal, marinaID string, b *store.RentalBooking) (*reservation.BookingView, error) {
	if s.d.Bookings != nil {
		if v, err := s.d.Bookings.Get(ctx, p, marinaID, b.ID); err == nil {
			return v, nil
		}
	}
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	cur, err := store.GetRentalBooking(ctx, s.d.DB.DB, scope, b.ID)
	if err != nil {
		return nil, notFound(err)
	}
	loc := s.location(ctx, s.d.DB.DB, scope)
	snap := snapshotOf(cur)
	return &reservation.BookingView{
		RentalBooking: *cur, Kind: string(snap.Kind), Snapshot: snap, FiscalMode: fiscalMode(cur),
		LicenceRequired: snap.LicenceRequired, StartsAt: slots.Instant(cur.StartsOn, cur.StartSlot, loc),
		EndsAt: slots.Instant(cur.EndsOn, cur.EndSlot, loc),
	}, nil
}

// renter returns the decrypted walk-in renter of a booking (nil without one). In memory only, never logged.
func (s *Service) renter(b *store.RentalBooking) *reservation.Renter {
	if b.RenterEnc == "" || s.d.Keys == nil {
		return nil
	}
	raw, err := s.d.Keys.DecryptString(b.RenterEnc, "rental-renter:"+b.ID)
	if err != nil {
		return nil
	}
	var r reservation.Renter
	if json.Unmarshal([]byte(raw), &r) != nil {
		return nil
	}
	return &r
}

// renterName is the display name of the renter (customer or walk-in).
func (s *Service) renterName(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking) string {
	if b.CustomerID != "" {
		if c, err := store.GetCustomer(ctx, db, scope, b.CustomerID); err == nil {
			return docs.PartyOf(c).DisplayName
		}
	}
	if r := s.renter(b); r != nil {
		return r.Name
	}
	return ""
}

func userOf(a audit.Actor) string {
	if a.Type == audit.ActorUser {
		return a.ID
	}
	return ""
}
