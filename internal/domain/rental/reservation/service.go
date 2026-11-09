// Package reservation is the rental booking engine (spec 19.3, m11-plan §4, §6): bookings, slot rows of units and
// skippers, holds with lazy reclaim of expired ones, blocks, the planner, availability, the day view and the
// holds-expiry job. INV-18: the primary keys of rental_slots and rental_skipper_slots are the only guarantee
// against double booking (ADR 0003): rows are inserted with ON CONFLICT DO NOTHING and counted, a short count is a
// *SlotConflict and the caller's transaction rolls back; never check-then-insert as a guarantee. INV-97: this
// package is the only writer of rental_bookings.status, the slot rows and hold_expires_at; every change writes
// audit and outbox in the same transaction. On PostgreSQL READ COMMITTED: lock the booking, then the unit, then
// the skipper (FOR UPDATE), then re-check state and version. Owned by BOOKING after the contract phase.
package reservation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/domain/rental/states"
	"github.com/coste360/porto/internal/store"
)

// JobHoldsExpire cancels the held bookings whose hold expired (every minute, m11-plan §4.6).
const JobHoldsExpire = "rental.holds_expire"

// Pricer prices a booking inside its transaction (fleet.Service.SnapshotTx).
type Pricer interface {
	SnapshotTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in fleet.SnapshotRequest) (price.Snapshot, error)
}

// Hooks are the handover side effects of the reservation transitions, run inside the same transaction
// (implemented by handover.Service and installed with SetHooks; nil = none).
type Hooks interface {
	// RentalConfirmedTx runs when a booking becomes confirmed (create or confirm): the contract document is composed.
	RentalConfirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error
	// RentalEndedTx runs when a booking is cancelled (desk, hold expiry) or marked no-show: a cancellable main charge
	// is cancelled (cancel) or the main charge created (no-show), a pre-authorisation voided after commit.
	RentalEndedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error
	// RentalRepricedTx runs after a reschedule or an extension changed the price snapshot, or an update changed the
	// customer or invoice_requested: the main charge is replaced or adjusted (§9.1).
	RentalRepricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, r *RepriceResult) error
}

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
	Keys   *crypto.KeyRing
	Fleet  *fleet.Service
	Notify *notify.Service
	// Pricer overrides Fleet for the price snapshot (tests); nil = Fleet.
	Pricer Pricer
}

// Service is the rental reservation service.
type Service struct {
	d Deps

	mu    sync.RWMutex
	hooks Hooks
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

// WithClock returns a service sharing the collaborators and hooks of s but reading time from c (demo seed: an
// expired hold is created in the past instead of being written by hand).
func (s *Service) WithClock(c clock.Clock) *Service {
	d := s.d
	d.Clock = c
	n := NewService(d)
	n.hooks = s.getHooks()
	return n
}

// SetHooks installs the handover side effects (app wiring; the handover service is built after this one).
func (s *Service) SetHooks(h Hooks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = h
}

func (s *Service) getHooks() Hooks {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hooks
}

func (s *Service) pricer() Pricer {
	if s.d.Pricer != nil {
		return s.d.Pricer
	}
	return s.d.Fleet
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

// authorize checks rental.manage on the marina (another marina is auth.ErrNotFound) and that each (table, id) pair
// of refs exists in the marina, so another marina's ids are 404 before anything else.
func (s *Service) authorize(ctx context.Context, p *auth.Principal, marinaID string, refs ...string) (store.MarinaScope, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRentalManage)
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

// CheckAccess checks rental.manage on the marina (handlers answer 404/403 before a 400 of a malformed query).
func (s *Service) CheckAccess(ctx context.Context, p *auth.Principal, marinaID string) error {
	_, err := s.authorize(ctx, p, marinaID)
	return err
}

// location returns the marina time zone (UTC when unknown).
func location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	if tz, err := store.MarinaTimezone(ctx, db, scope); err == nil && tz != "" {
		if l, err := calendar.LoadLocation(tz); err == nil {
			return l
		}
	}
	return time.UTC
}

// settingsTx decodes the rental settings of the marina (defaults when never saved).
func settingsTx(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*fleet.Settings, error) {
	row, err := store.LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := &fleet.Settings{Row: row}
	if err := decodeJSON(row.Bands, &out.Bands); err != nil {
		return nil, fmt.Errorf("rental settings bands: %w", err)
	}
	if err := decodeJSON(row.Checklist, &out.Checklist); err != nil {
		return nil, fmt.Errorf("rental settings checklist: %w", err)
	}
	if err := decodeJSON(row.DepositModes, &out.DepositModes); err != nil {
		return nil, fmt.Errorf("rental settings deposit modes: %w", err)
	}
	return out, nil
}

// Number formats a booking number (NL<year>-NNNNNN, D23).
func Number(year, n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("NL%d-%06d", year, n)
}

// RangeOf returns the range of a booking row.
func RangeOf(b *store.RentalBooking) slots.Range {
	return slots.Range{Start: slots.Point{Date: b.StartsOn, Slot: b.StartSlot}, End: slots.Point{Date: b.EndsOn, Slot: b.EndSlot}}
}

func setRange(b *store.RentalBooking, r slots.Range) {
	b.StartsOn, b.StartSlot, b.EndsOn, b.EndSlot = r.Start.Date, r.Start.Slot, r.End.Date, r.End.Slot
}

func renterAAD(id string) string { return "rental-renter:" + id }

func cleanText(v *auth.ValidationError, field, s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxLen {
		v.Add(field, "too_long")
	}
	return s
}

func cleanRenter(v *auth.ValidationError, r *Renter) *Renter {
	if r == nil {
		return nil
	}
	out := &Renter{
		Name:  cleanText(v, "renter.name", r.Name, 80),
		Phone: cleanText(v, "renter.phone", r.Phone, 40),
		Email: cleanText(v, "renter.email", r.Email, 254),
	}
	if out.Email != "" && !strings.Contains(out.Email, "@") {
		v.Add("renter.email", "invalid")
	}
	if *out == (Renter{}) {
		return nil
	}
	return out
}

func (s *Service) encryptRenter(id string, r *Renter) (string, error) {
	if r == nil {
		return "", nil
	}
	if s.d.Keys == nil {
		return "", errors.New("rental reservation: no key ring")
	}
	raw, err := marshalJSON(r)
	if err != nil {
		return "", err
	}
	return s.d.Keys.EncryptString(string(raw), renterAAD(id))
}

func (s *Service) decryptRenter(id, enc string) *Renter {
	if enc == "" || s.d.Keys == nil {
		return nil
	}
	plain, err := s.d.Keys.DecryptString(enc, renterAAD(id))
	if err != nil {
		s.d.Log.Warn("rental renter decrypt failed", "booking_id", id)
		return nil
	}
	r := new(Renter)
	if err := decodeJSON([]byte(plain), r); err != nil {
		return nil
	}
	return r
}

// audit writes an audit row of a booking: ids, enums, dates and amounts only (no renter data, INV-70).
func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action string, b *store.RentalBooking, extra map[string]any) error {
	diff := map[string]any{
		"status": b.Status, "unit_id": b.UnitID, "formula_id": b.FormulaID, "starts_on": b.StartsOn, "start_slot": b.StartSlot,
		"ends_on": b.EndsOn, "end_slot": b.EndSlot, "total_cents": b.TotalCents,
	}
	if b.SkipperID != "" {
		diff["skipper_id"] = b.SkipperID
	}
	if b.CustomerID != "" {
		diff["customer_id"] = b.CustomerID
	}
	for k, v := range extra {
		diff[k] = v
	}
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: "rental_booking",
		EntityID: b.ID, Diff: diff,
	})
}

// emit writes a rental event of a booking: ids, enums and dates only (INV-70).
func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ, change string, b *store.RentalBooking) error {
	if s.d.Outbox == nil {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, event.RentalData{
		BookingID: b.ID, UnitID: b.UnitID, SkipperID: b.SkipperID, Status: b.Status, Change: change,
		StartsOn: b.StartsOn.String(), EndsOn: b.EndsOn.String(),
	})
	return err
}

// emitEntity writes rental.changed for a non-booking entity (block).
func (s *Service) emitEntity(ctx context.Context, tx bun.IDB, scope store.MarinaScope, change, entity, id, unitID, skipperID string) error {
	if s.d.Outbox == nil {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.RentalChanged, event.RentalData{
		UnitID: unitID, SkipperID: skipperID, Change: change, Entity: entity, EntityID: id,
	})
	return err
}

// next checks a transition of the table (INV-97).
func next(from string, a states.Action) (string, error) {
	to, ok := states.Next(states.Status(from), a)
	if !ok {
		return "", ErrInvalidTransition
	}
	return string(to), nil
}

// ---- bookings (rental.manage) ----

// List lists the bookings, newest first.
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter) ([]BookingView, string, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	if f.Status != "" && !validStatus(f.Status) {
		return nil, "", invalid("status", "invalid")
	}
	rows, next, err := store.ListRentalBookings(ctx, s.d.DB.DB, scope, store.RentalBookingFilter{
		From: f.From, To: f.To, Status: f.Status, UnitID: f.UnitID, CustomerID: f.CustomerID, Cursor: f.Cursor,
		Limit: store.ClampLimit(f.Limit),
	})
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return nil, "", invalid("cursor", "invalid")
		}
		return nil, "", err
	}
	views, err := s.views(ctx, s.d.DB.DB, scope, rows)
	return views, next, err
}

func validStatus(st string) bool {
	switch st {
	case store.RentalStatusHeld, store.RentalStatusConfirmed, store.RentalStatusCheckedOut, store.RentalStatusReturned,
		store.RentalStatusClosed, store.RentalStatusCancelled, store.RentalStatusNoShow:
		return true
	}
	return false
}

// Get returns a booking.
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, bookingID string) (*BookingView, error) {
	scope, err := s.authorize(ctx, p, marinaID, store.RentalTableBookings, bookingID)
	if err != nil {
		return nil, err
	}
	return s.ViewTx(ctx, s.d.DB.DB, scope, bookingID)
}

// ViewTx returns the view of a booking of the scope in db (seam for the handover service).
func (s *Service) ViewTx(ctx context.Context, db bun.IDB, scope store.MarinaScope, bookingID string) (*BookingView, error) {
	b, err := store.GetRentalBooking(ctx, db, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.views(ctx, db, scope, []store.RentalBooking{*b})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// Create creates a held or confirmed booking (allocate, price snapshot, audit, rental.changed; confirmed also
// rental.confirmed and the contract document).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in CreateInput) (*BookingView, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.UnitID == "" {
		v.Add("unit_id", "required")
	}
	if in.FormulaID == "" {
		v.Add("formula_id", "required")
	}
	if !slots.WellFormed(in.Range) {
		v.Add("end", "invalid")
	}
	renter := cleanRenter(v, in.Renter)
	note := cleanText(v, "note", in.Note, 500)
	if in.PartySize != nil && (*in.PartySize < 1 || *in.PartySize > 99) {
		v.Add("party_size", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	channel := store.RentalChannelDesk
	if p != nil && p.Kind == auth.KindAPIKey {
		channel = store.RentalChannelAPI
	}
	if in.CustomerID != "" {
		if _, err := store.GetCustomer(ctx, s.d.DB.DB, scope, in.CustomerID); err != nil {
			return nil, invalid("customer_id", "not_found")
		}
	}
	if in.InvoiceRequested && in.CustomerID == "" {
		return nil, ErrCustomerRequired
	}
	now := s.now()
	actor := p.Actor()
	b := &store.RentalBooking{
		ID: store.NewID(), UnitID: in.UnitID, FormulaID: in.FormulaID, SkipperID: in.SkipperID, Channel: channel,
		CustomerID: in.CustomerID, PartySize: in.PartySize, InvoiceRequested: in.InvoiceRequested,
		AllowOutsideHours: in.AllowOutsideHours, Note: note, CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		DepositStatus: store.RentalDepositStatusNone,
	}
	setRange(b, in.Range)
	if b.RenterEnc, err = s.encryptRenter(b.ID, renter); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		loc := location(ctx, tx, scope)
		pl, err := s.prepareTx(ctx, tx, scope, b, true)
		if err != nil {
			return err
		}
		if in.PartySize != nil && *in.PartySize > pl.unit.Seats {
			return invalid("party_size", "exceeds_seats")
		}
		action := states.CreateConfirm
		if in.Hold {
			action = states.CreateHold
			b.HoldExpiresAt = now.Add(time.Duration(pl.settings.Row.DeskHoldMinutes) * time.Minute)
		}
		if b.Status, err = next(string(states.None), action); err != nil {
			return err
		}
		extras, err := s.extrasTx(ctx, tx, scope, pl.unit, in.Extras)
		if err != nil {
			return err
		}
		if err := s.priceTx(ctx, tx, scope, b, in.Extras); err != nil {
			return err
		}
		b.DepositCents = pl.unit.DepositCents
		today := calendar.In(now, loc)
		n, err := store.NextNumber(ctx, tx, scope, store.SeriesRental, today.Year())
		if err != nil {
			return err
		}
		b.Number, b.Year = int(n), today.Year()
		if err := store.InsertRentalBooking(ctx, tx, scope, b); err != nil {
			return err
		}
		if err := store.ReplaceRentalBookingExtras(ctx, tx, scope, b.ID, store.RentalExtraSourceBooking, extras, now); err != nil {
			return err
		}
		if err := s.allocateTx(ctx, tx, scope, b, pl.settings.Row.BufferSlots, now); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, actor, "rental_booking.create", b, map[string]any{
			"channel": b.Channel, "allow_outside_hours": b.AllowOutsideHours,
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.RentalChanged, "created", b); err != nil {
			return err
		}
		if b.Status == store.RentalStatusConfirmed {
			return s.confirmedTx(ctx, tx, scope, actor, b, renter, loc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ViewTx(ctx, s.d.DB.DB, scope, b.ID)
}

// plan is what a booking range was validated against, under the unit and skipper locks.
type plan struct {
	unit     *store.RentalUnit
	formula  *store.RentalFormula
	skipper  *store.RentalSkipper
	settings *fleet.Settings
	qty      slots.Quantity
}

// FormulaSlots returns the time part of a formula for the pure slots package.
func FormulaSlots(f *store.RentalFormula) slots.Formula {
	out := slots.Formula{Kind: f.Kind, Band: f.BandCode, MinSlots: f.MinSlots, MinDays: f.MinDays, MaxDays: f.MaxDays}
	if f.MaxSlots != nil {
		out.MaxSlots = *f.MaxSlots
	}
	return out
}

func formulaFits(f *store.RentalFormula, category string) bool {
	var cats []string
	_ = decodeJSON(f.Categories, &cats)
	if len(cats) == 0 {
		return true
	}
	for _, c := range cats {
		if c == category {
			return true
		}
	}
	return false
}

// prepareTx locks the unit and the skipper of b (after the booking, lock order §0.3), re-checks them under the
// lock and validates the range against the formula and the settings. checkUnit refuses a unit in maintenance or
// retired (new bookings and reschedules).
func (s *Service) prepareTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.RentalBooking, checkUnit bool) (*plan, error) {
	pl := &plan{}
	var err error
	if pl.unit, err = store.LockRentalUnit(ctx, tx, scope, b.UnitID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, invalid("unit_id", "not_found")
		}
		return nil, err
	}
	if checkUnit && pl.unit.Status != store.RentalUnitAvailable {
		return nil, ErrUnitNotBookable
	}
	if pl.formula, err = store.GetRentalFormula(ctx, tx, scope, b.FormulaID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, invalid("formula_id", "not_found")
		}
		return nil, err
	}
	if !pl.formula.Active {
		return nil, invalid("formula_id", "inactive")
	}
	if !formulaFits(pl.formula, pl.unit.Category) {
		return nil, invalid("formula_id", "category")
	}
	switch {
	case pl.formula.Skipper == store.RentalSkipperNone && b.SkipperID != "":
		return nil, invalid("skipper_id", "not_allowed")
	case pl.formula.Skipper == store.RentalSkipperRequired && b.SkipperID == "":
		return nil, ErrSkipperRequired
	}
	if pl.settings, err = settingsTx(ctx, tx, scope); err != nil {
		return nil, err
	}
	r := RangeOf(b)
	if pl.qty, err = slots.ValidateRange(FormulaSlots(pl.formula), pl.settings.Slots(), r); err != nil {
		return nil, err
	}
	if b.SkipperID != "" {
		if pl.skipper, err = store.LockRentalSkipper(ctx, tx, scope, b.SkipperID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, invalid("skipper_id", "not_found")
			}
			return nil, err
		}
		if !pl.skipper.Active {
			return nil, invalid("skipper_id", "inactive")
		}
		if !pl.skipper.LicenceExpiresOn.IsZero() && pl.skipper.LicenceExpiresOn.Before(b.EndsOn) {
			return nil, ErrSkipperLicenceExpired
		}
		if !b.AllowOutsideHours {
			hours, err := store.ListRentalSkipperHours(ctx, tx, scope, b.SkipperID)
			if err != nil {
				return nil, err
			}
			if !WithinHours(hours, r) {
				return nil, ErrSkipperUnavailable
			}
		}
	}
	return pl, nil
}

// WithinHours reports whether every slot of r falls inside the weekly hours of a skipper (no hours = no rule).
func WithinHours(hours []store.RentalSkipperHours, r slots.Range) bool {
	if len(hours) == 0 {
		return true
	}
	for _, k := range slots.Expand(r, 0) {
		wd, ok := k.Date.ISOWeekday(), false
		for _, h := range hours {
			if h.Weekday == wd && h.FromSlot <= k.Slot && k.Slot < h.ToSlot {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// extrasTx validates the extras of a booking (existing, active, of the unit category, qty within the maximum;
// qty 0 = the default) and returns their rows.
func (s *Service) extrasTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, unit *store.RentalUnit, in []fleet.ExtraQty) ([]store.RentalBookingExtra, error) {
	var out []store.RentalBookingExtra
	seen := map[string]bool{}
	for i := range in {
		x := &in[i]
		field := fmt.Sprintf("extras[%d]", i)
		e, err := store.GetRentalExtra(ctx, tx, scope, x.ExtraID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, invalid(field+".extra_id", "not_found")
			}
			return nil, err
		}
		if seen[e.ID] {
			return nil, invalid(field+".extra_id", "duplicate")
		}
		seen[e.ID] = true
		if !e.Active {
			return nil, invalid(field+".extra_id", "inactive")
		}
		var cats []string
		_ = decodeJSON(e.Categories, &cats)
		if len(cats) > 0 && !contains(cats, unit.Category) {
			return nil, invalid(field+".extra_id", "category")
		}
		if x.Qty == 0 {
			x.Qty = qtyOf(e.DefaultQtyMilli)
		}
		if x.Qty <= 0 || int64(x.Qty) > e.MaxQtyMilli {
			return nil, invalid(field+".qty_milli", "out_of_range")
		}
		out = append(out, store.RentalBookingExtra{ID: store.NewID(), ExtraID: e.ID, QtyMilli: int64(x.Qty)})
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// priceTx stores the price snapshot of b (pricing.RentalQuote through the fleet seam).
func (s *Service) priceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.RentalBooking, extras []fleet.ExtraQty) error {
	p := s.pricer()
	if p == nil {
		return errors.New("rental reservation: no pricer")
	}
	snap, err := p.SnapshotTx(ctx, tx, scope, fleet.SnapshotRequest{
		UnitID: b.UnitID, FormulaID: b.FormulaID, SkipperID: b.SkipperID, Range: RangeOf(b), Extras: extras,
		CustomerID: b.CustomerID, Channel: b.Channel,
	})
	if err != nil {
		return err
	}
	raw, err := marshalJSON(snap)
	if err != nil {
		return err
	}
	b.PriceSnapshot, b.TotalCents = store.JSON(raw), int64(snap.Total())
	b.EngineVersion = max(snap.EngineVersion, 1)
	return nil
}

// bookingExtras returns the booked extras of a booking as quote input.
func bookingExtras(ctx context.Context, tx bun.IDB, scope store.MarinaScope, bookingID string) ([]fleet.ExtraQty, error) {
	rows, err := store.ListRentalBookingExtras(ctx, tx, scope, bookingID)
	if err != nil {
		return nil, err
	}
	var out []fleet.ExtraQty
	for _, r := range rows {
		if r.Source == store.RentalExtraSourceBooking {
			out = append(out, fleet.ExtraQty{ExtraID: r.ExtraID, Qty: qtyOf(r.QtyMilli)})
		}
	}
	return out, nil
}

// confirmedTx runs the side effects of a booking becoming confirmed: rental.confirmed, the handover hook (contract
// document) and the optional rental_confirmed email.
func (s *Service) confirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking, renter *Renter, loc *time.Location) error {
	if err := s.emit(ctx, tx, scope, event.RentalConfirmed, "", b); err != nil {
		return err
	}
	if h := s.getHooks(); h != nil {
		if err := h.RentalConfirmedTx(ctx, tx, scope, actor, b); err != nil {
			return err
		}
	}
	return s.confirmEmailTx(ctx, tx, scope, b, renter, loc)
}

// Update reschedules or edits a held or confirmed booking (If-Match).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, bookingID string, ifMatch int, in Patch) (*BookingView, error) {
	scope, err := s.authorize(ctx, p, marinaID, store.RentalTableBookings, bookingID)
	if err != nil {
		return nil, err
	}
	if in.KeepPrice {
		if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure); err != nil {
			return nil, err
		}
	}
	v := &auth.ValidationError{}
	renter := cleanRenter(v, in.Renter)
	var note *string
	if in.Note != nil {
		n := cleanText(v, "note", *in.Note, 500)
		note = &n
	}
	if in.PartySize != nil && (*in.PartySize < 1 || *in.PartySize > 99) {
		v.Add("party_size", "invalid")
	}
	if in.Range != nil && !slots.WellFormed(*in.Range) {
		v.Add("end", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if in.CustomerID != nil && *in.CustomerID != "" {
		if _, err := store.GetCustomer(ctx, s.d.DB.DB, scope, *in.CustomerID); err != nil {
			return nil, invalid("customer_id", "not_found")
		}
	}
	now := s.now()
	actor := p.Actor()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockRentalBooking(ctx, tx, scope, bookingID)
		if err != nil {
			return notFound(err)
		}
		if b.Version != ifMatch {
			return ErrVersionMismatch
		}
		if _, err := next(b.Status, states.Reschedule); err != nil {
			return err
		}
		if b.Status == store.RentalStatusHeld && b.HoldExpiresAt.Before(now) {
			return ErrHoldExpired
		}
		prev := *b
		timeChanged := false
		set := func(dst *string, val *string) {
			if val != nil && *val != *dst {
				*dst, timeChanged = *val, true
			}
		}
		set(&b.UnitID, in.UnitID)
		set(&b.FormulaID, in.FormulaID)
		if in.ClearSkipper && b.SkipperID != "" {
			b.SkipperID, timeChanged = "", true
		}
		set(&b.SkipperID, in.SkipperID)
		if in.Range != nil && (RangeOf(b) != *in.Range) {
			setRange(b, *in.Range)
			timeChanged = true
		}
		if in.AllowOutsideHours != nil && *in.AllowOutsideHours != b.AllowOutsideHours {
			b.AllowOutsideHours = *in.AllowOutsideHours
			timeChanged = timeChanged || b.SkipperID != ""
		}
		priceChanged := timeChanged || in.ExtrasSet
		chargeStale := false // the live main charge no longer matches the booking's customer or fiscal mode
		if in.ClearCustomer {
			if b.CustomerID != "" {
				priceChanged, chargeStale = true, true
			}
			b.CustomerID = ""
		}
		if in.CustomerID != nil && *in.CustomerID != b.CustomerID {
			b.CustomerID, priceChanged, chargeStale = *in.CustomerID, true, true
		}
		if in.Renter != nil {
			if b.RenterEnc, err = s.encryptRenter(b.ID, renter); err != nil {
				return err
			}
		}
		if in.ClearPartySize {
			b.PartySize = nil
		}
		if in.PartySize != nil {
			b.PartySize = in.PartySize
		}
		if note != nil {
			b.Note = *note
		}
		if in.InvoiceRequested != nil && *in.InvoiceRequested != b.InvoiceRequested {
			exported, err := store.RentalBookingHasExportedCharge(ctx, tx, scope, b.ID)
			if err != nil {
				return err
			}
			if exported {
				return invalid("invoice_requested", "charge_exported")
			}
			b.InvoiceRequested = *in.InvoiceRequested
			chargeStale = true // the main charge follows the fiscal mode (or the switch is refused once it is paid)
		}
		if b.InvoiceRequested && b.CustomerID == "" {
			return ErrCustomerRequired
		}
		var pl *plan
		if timeChanged || in.ExtrasSet || in.PartySize != nil {
			if pl, err = s.prepareTx(ctx, tx, scope, b, timeChanged); err != nil {
				return err
			}
			if b.PartySize != nil && *b.PartySize > pl.unit.Seats {
				return invalid("party_size", "exceeds_seats")
			}
		}
		extras, err := bookingExtras(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		if in.ExtrasSet {
			extras = in.Extras
			rows, err := s.extrasTx(ctx, tx, scope, pl.unit, extras)
			if err != nil {
				return err
			}
			if err := store.ReplaceRentalBookingExtras(ctx, tx, scope, b.ID, store.RentalExtraSourceBooking, rows, now); err != nil {
				return err
			}
		}
		repriced := false
		if priceChanged && !in.KeepPrice {
			if err := s.priceTx(ctx, tx, scope, b, extras); err != nil {
				return err
			}
			repriced = true
		}
		if timeChanged {
			if err := s.allocateTx(ctx, tx, scope, b, pl.settings.Row.BufferSlots, now); err != nil {
				return err
			}
		}
		if err := store.WriteRentalBookingState(ctx, tx, scope, b, ifMatch, now); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, actor, "rental_booking.update", b, map[string]any{
			"rescheduled": timeChanged, "keep_price": in.KeepPrice, "allow_outside_hours": b.AllowOutsideHours,
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.RentalChanged, "rescheduled", b); err != nil {
			return err
		}
		if repriced || chargeStale {
			return s.repricedTx(ctx, tx, scope, actor, &prev, b)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ViewTx(ctx, s.d.DB.DB, scope, bookingID)
}

// repricedTx runs the reprice hook with the delta between two snapshots.
func (s *Service) repricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, prev, b *store.RentalBooking) error {
	h := s.getHooks()
	if h == nil {
		return nil
	}
	r := &RepriceResult{Booking: b, Delta: moneyOf(b.TotalCents - prev.TotalCents)}
	_ = decodeJSON(b.PriceSnapshot, &r.Snapshot)
	_ = decodeJSON(prev.PriceSnapshot, &r.Previous)
	return h.RentalRepricedTx(ctx, tx, scope, actor, r)
}

// Transition confirms, cancels, marks no-show, extends the hold or extends a checked-out booking (If-Match).
func (s *Service) Transition(ctx context.Context, p *auth.Principal, marinaID, bookingID string, ifMatch int, in TransitionInput) (*BookingView, error) {
	scope, err := s.authorize(ctx, p, marinaID, store.RentalTableBookings, bookingID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	reason := cleanText(v, "reason", in.Reason, 200)
	switch in.Action {
	case ActionConfirm, ActionCancel, ActionNoShow, ActionExtendHold:
	case ActionExtend:
		if in.End == nil {
			v.Add("end", "required")
		}
	default:
		v.Add("action", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	actor := p.Actor()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		switch in.Action {
		case ActionConfirm:
			return s.confirmTx(ctx, tx, scope, actor, bookingID, ifMatch)
		case ActionCancel:
			b, err := s.cancelTx(ctx, tx, scope, actor, bookingID, ifMatch, reason, states.Cancel)
			if err != nil {
				return err
			}
			return s.endedTx(ctx, tx, scope, actor, b)
		case ActionNoShow:
			return s.noShowTx(ctx, tx, scope, actor, bookingID, ifMatch)
		case ActionExtendHold:
			return s.extendHoldTx(ctx, tx, scope, actor, bookingID, ifMatch, in.Until)
		default:
			return s.extendTx(ctx, tx, scope, actor, bookingID, ifMatch, *in.End, in.FormulaID)
		}
	})
	if err != nil {
		return nil, err
	}
	return s.ViewTx(ctx, s.d.DB.DB, scope, bookingID)
}

func (s *Service) endedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error {
	if h := s.getHooks(); h != nil {
		return h.RentalEndedTx(ctx, tx, scope, actor, b)
	}
	return nil
}

// lockFor locks a booking, checks the expected version (when > 0) and the transition, and returns the booking and
// the target status.
func lockFor(ctx context.Context, tx bun.IDB, scope store.MarinaScope, bookingID string, expected int, a states.Action) (*store.RentalBooking, string, error) {
	b, err := store.LockRentalBooking(ctx, tx, scope, bookingID)
	if err != nil {
		return nil, "", notFound(err)
	}
	if expected > 0 && b.Version != expected {
		return nil, "", ErrVersionMismatch
	}
	to, err := next(b.Status, a)
	if err != nil {
		return nil, "", err
	}
	return b, to, nil
}

func (s *Service) confirmTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int) error {
	now := s.now()
	b, to, err := lockFor(ctx, tx, scope, bookingID, expected, states.Confirm)
	if err != nil {
		return err
	}
	version := b.Version
	unit, sk, err := store.CountRentalBookingSlots(ctx, tx, scope, b.ID)
	if err != nil {
		return err
	}
	settings, err := settingsTx(ctx, tx, scope)
	if err != nil {
		return err
	}
	r := RangeOf(b)
	wantSk := 0
	if b.SkipperID != "" {
		wantSk = slots.Count(r)
	}
	b.Status, b.HoldExpiresAt = to, time.Time{}
	if !b.SlotsLostAt.IsZero() || unit != len(slots.Expand(r, settings.Row.BufferSlots)) || sk != wantSk {
		// The hold rows were reclaimed: allocate again as booked, or the hold is lost.
		if _, err := store.LockRentalUnit(ctx, tx, scope, b.UnitID); err != nil {
			return err
		}
		if b.SkipperID != "" {
			if _, err := store.LockRentalSkipper(ctx, tx, scope, b.SkipperID); err != nil {
				return err
			}
		}
		if err := s.allocateTx(ctx, tx, scope, b, settings.Row.BufferSlots, now); err != nil {
			if _, ok := errors.AsType[*SlotConflict](err); ok {
				return ErrHoldExpired
			}
			return err
		}
	} else if err := store.SetRentalBookingSlotsBooked(ctx, tx, scope, b.ID); err != nil {
		return err
	}
	b.SlotsLostAt = time.Time{}
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, now); err != nil {
		return versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.confirm", b, nil); err != nil {
		return err
	}
	return s.confirmedTx(ctx, tx, scope, actor, b, s.decryptRenter(b.ID, b.RenterEnc), location(ctx, tx, scope))
}

// cancelTx cancels a held or confirmed booking (desk cancel or hold expiry) and deletes its rows.
func (s *Service) cancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, reason string, a states.Action) (*store.RentalBooking, error) {
	now := s.now()
	b, to, err := lockFor(ctx, tx, scope, bookingID, expected, a)
	if err != nil {
		return nil, err
	}
	if a == states.HoldExpire && !b.HoldExpiresAt.Before(now) {
		return nil, ErrInvalidTransition
	}
	if err := store.DeleteRentalBookingSlots(ctx, tx, scope, b.ID); err != nil {
		return nil, err
	}
	version := b.Version
	b.Status, b.HoldExpiresAt, b.CancelledAt, b.CancelReason = to, time.Time{}, now, reason
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, now); err != nil {
		return nil, versionErr(err)
	}
	action := "rental_booking.cancel"
	if a == states.HoldExpire {
		action = "rental_booking.hold_expire"
	}
	if err := s.audit(ctx, tx, scope, actor, action, b, map[string]any{"cancel_reason": reason}); err != nil {
		return nil, err
	}
	change := ""
	if a == states.HoldExpire {
		change = CancelReasonHoldExpired
	}
	if err := s.emit(ctx, tx, scope, event.RentalCancelled, change, b); err != nil {
		return nil, err
	}
	return b, nil
}

// CancelReasonHoldExpired is the cancel reason of a hold cancelled by the expiry job.
const CancelReasonHoldExpired = "hold_expired"

func (s *Service) noShowTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int) error {
	now := s.now()
	b, to, err := lockFor(ctx, tx, scope, bookingID, expected, states.MarkNoShow)
	if err != nil {
		return err
	}
	loc := location(ctx, tx, scope)
	if now.Before(slots.Instant(b.StartsOn, b.StartSlot, loc)) {
		return invalid("action", "not_started")
	}
	if err := store.DeleteRentalBookingSlots(ctx, tx, scope, b.ID); err != nil {
		return err
	}
	version := b.Version
	b.Status, b.NoShowAt = to, now
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, now); err != nil {
		return versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.no_show", b, nil); err != nil {
		return err
	}
	if err := s.emit(ctx, tx, scope, event.RentalChanged, "no_show", b); err != nil {
		return err
	}
	return s.endedTx(ctx, tx, scope, actor, b)
}

// maxHoldExtension bounds extend_hold.
const maxHoldExtension = 7 * 24 * time.Hour

func (s *Service) extendHoldTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, until time.Time) error {
	now := s.now()
	b, _, err := lockFor(ctx, tx, scope, bookingID, expected, states.ExtendHold)
	if err != nil {
		return err
	}
	if until.IsZero() {
		settings, err := settingsTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		until = now.Add(time.Duration(settings.Row.DeskHoldMinutes) * time.Minute)
	}
	until = store.UTC(until)
	if !until.After(now) || until.After(now.Add(maxHoldExtension)) {
		return invalid("until", "out_of_range")
	}
	if !b.SlotsLostAt.IsZero() {
		return ErrHoldExpired
	}
	if err := store.SetRentalBookingSlotsExpiry(ctx, tx, scope, b.ID, until); err != nil {
		return err
	}
	version := b.Version
	b.HoldExpiresAt = until
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, now); err != nil {
		return versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.extend_hold", b, map[string]any{"hold_expires_at": until}); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, event.RentalChanged, "hold_extended", b)
}

func (s *Service) extendTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, end slots.Point, formulaID string) error {
	now := s.now()
	b, _, err := lockFor(ctx, tx, scope, bookingID, expected, states.Extend)
	if err != nil {
		return err
	}
	if end.Slot == 0 && !end.Date.IsZero() {
		end = slots.Point{Date: end.Date.AddDays(-1), Slot: slots.PerDay}
	}
	if end.Date.IsZero() || end.Slot < 1 || end.Slot > slots.PerDay || slots.Index(b.EndsOn, end) <= b.EndSlot {
		return invalid("end", "not_after_end")
	}
	prev := *b
	b.EndsOn, b.EndSlot = end.Date, end.Slot
	if formulaID != "" {
		b.FormulaID = formulaID // prepareTx checks it exists, is active, fits the category and the new range
	}
	pl, err := s.prepareTx(ctx, tx, scope, b, false)
	if err != nil {
		return err
	}
	extras, err := bookingExtras(ctx, tx, scope, b.ID)
	if err != nil {
		return err
	}
	if err := s.priceTx(ctx, tx, scope, b, extras); err != nil {
		return err
	}
	if err := s.allocateTx(ctx, tx, scope, b, pl.settings.Row.BufferSlots, now); err != nil {
		return err
	}
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, prev.Version, now); err != nil {
		return versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.extend", b, map[string]any{
		"previous_ends_on": prev.EndsOn, "previous_end_slot": prev.EndSlot, "previous_formula_id": prev.FormulaID,
	}); err != nil {
		return err
	}
	if err := s.emit(ctx, tx, scope, event.RentalChanged, "extended", b); err != nil {
		return err
	}
	return s.repricedTx(ctx, tx, scope, actor, &prev, b)
}

// RunHoldsExpire cancels the held bookings whose hold expired, for every marina (job rental.holds_expire).
func (s *Service) RunHoldsExpire(ctx context.Context) error {
	now := s.now()
	const batch = 200
	for range 50 {
		rows, err := store.ExpiredRentalHolds(ctx, s.d.DB.DB, now, batch)
		if err != nil {
			return err
		}
		done := 0
		for _, r := range rows {
			scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, r.MarinaID)
			if err != nil {
				return err
			}
			err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				b, err := s.cancelTx(ctx, tx, scope, audit.System, r.ID, 0, CancelReasonHoldExpired, states.HoldExpire)
				if err != nil {
					return err
				}
				return s.endedTx(ctx, tx, scope, audit.System, b)
			})
			switch {
			case err == nil:
				done++
			case errors.Is(err, ErrInvalidTransition), errors.Is(err, auth.ErrNotFound):
				// confirmed, extended or cancelled in the meantime
			default:
				return fmt.Errorf("expire rental hold: %w", err)
			}
		}
		if len(rows) < batch || done == 0 {
			return nil
		}
	}
	return nil
}
