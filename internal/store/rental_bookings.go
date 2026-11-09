package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of rental bookings (M11, m11-plan §4, §6). Owned by BOOKING. INV-97: WriteRentalBookingState, the only
// statement that writes rental_bookings.status and hold_expires_at, is called only by
// internal/domain/rental/reservation (TestRental_StatusWritesOnlyInReservation).

// InsertRentalBooking inserts a booking.
func InsertRentalBooking(ctx context.Context, tx bun.IDB, s MarinaScope, b *RentalBooking) error {
	b.MarinaID = s.ID()
	if b.Version == 0 {
		b.Version = 1
	}
	b.CreatedAt, b.UpdatedAt = UTC(b.CreatedAt), UTC(b.UpdatedAt)
	b.HoldExpiresAt = UTC(b.HoldExpiresAt)
	if len(b.PriceSnapshot) == 0 {
		b.PriceSnapshot = JSON(`{}`)
	}
	if b.DepositStatus == "" {
		b.DepositStatus = RentalDepositStatusNone
	}
	_, err := tx.NewInsert().Model(b).Exec(ctx)
	return err
}

// GetRentalBooking returns a booking of the marina.
func GetRentalBooking(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalBooking, error) {
	return getRentalRow[RentalBooking](ctx, db, s, id, false)
}

// LockRentalBooking locks a booking row (PostgreSQL FOR UPDATE; SQLite has one writer) and returns it. Lock order:
// booking, then unit, then skipper.
func LockRentalBooking(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalBooking, error) {
	return getRentalRow[RentalBooking](ctx, db, s, id, true)
}

// rentalStateColumns are the columns owned by the reservation engine (INV-97). Deposit columns belong to the
// handover side and are never written here.
var rentalStateColumns = []string{
	"unit_id", "formula_id", "skipper_id", "starts_on", "start_slot", "ends_on", "end_slot", "status",
	"hold_expires_at", "slots_lost_at", "customer_id", "renter_enc", "party_size", "invoice_requested",
	"allow_outside_hours", "price_snapshot", "engine_version", "total_cents", "checked_out_at",
	"returned_at", "closed_at", "cancelled_at", "cancel_reason", "no_show_at", "note", "updated_at",
}

// WriteRentalBookingState writes the reservation-owned columns of b when the version equals expected and bumps it
// (ErrVersionMismatch otherwise, ErrNotFound when the row is gone). Only internal/domain/rental/reservation calls
// it (INV-97).
func WriteRentalBookingState(ctx context.Context, tx bun.IDB, s MarinaScope, b *RentalBooking, expected int, now time.Time) error {
	b.UpdatedAt = UTC(now)
	b.HoldExpiresAt, b.SlotsLostAt = UTC(b.HoldExpiresAt), UTC(b.SlotsLostAt)
	b.CheckedOutAt, b.ReturnedAt, b.ClosedAt = UTC(b.CheckedOutAt), UTC(b.ReturnedAt), UTC(b.ClosedAt)
	b.CancelledAt, b.NoShowAt = UTC(b.CancelledAt), UTC(b.NoShowAt)
	res, err := tx.NewUpdate().Model(b).Column(rentalStateColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", b.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalBooking(ctx, tx, s, b.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	b.Version = expected + 1
	return nil
}

// MarkRentalSlotsLost records on held bookings that their rows were reclaimed by a conflicting allocation (status
// unchanged: the holds-expiry job cancels them).
func MarkRentalSlotsLost(ctx context.Context, tx bun.IDB, s MarinaScope, ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.NewUpdate().Model((*RentalBooking)(nil)).Set("slots_lost_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Where("status = ?", RentalStatusHeld).Exec(ctx)
	return err
}

// RentalBookingFilter selects bookings. From/To keep the bookings whose range touches [From, To].
type RentalBookingFilter struct {
	From       calendar.Date
	To         calendar.Date
	Status     string
	Statuses   []string
	UnitID     string
	CustomerID string
	StartsOn   calendar.Date // exact start date
	EndsOn     calendar.Date // exact end date
	IDs        []string
	Cursor     string
	Limit      int // 0 = no limit
	Oldest     bool
}

// ListRentalBookings returns the bookings matching f, newest first (or by start when Oldest), and the next cursor.
func ListRentalBookings(ctx context.Context, db bun.IDB, s MarinaScope, f RentalBookingFilter) ([]RentalBooking, string, error) {
	var rows []RentalBooking
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if !f.From.IsZero() {
		q = q.Where("ends_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("starts_on <= ?", f.To)
	}
	if !f.StartsOn.IsZero() {
		q = q.Where("starts_on = ?", f.StartsOn)
	}
	if !f.EndsOn.IsZero() {
		q = q.Where("ends_on = ?", f.EndsOn)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if len(f.Statuses) > 0 {
		q = q.Where("status IN (?)", bun.List(f.Statuses))
	}
	for _, ref := range []struct{ col, id string }{{"unit_id", f.UnitID}, {"customer_id", f.CustomerID}} {
		if ref.id == "" {
			continue
		}
		if !ValidID(ref.id) {
			return []RentalBooking{}, "", nil
		}
		q = q.Where("? = ?", bun.Ident(ref.col), ref.id)
	}
	if f.IDs != nil {
		if len(f.IDs) == 0 {
			return []RentalBooking{}, "", nil
		}
		q = q.Where("id IN (?)", bun.List(f.IDs))
	}
	if f.Oldest {
		q = q.Order("starts_on", "start_slot", "id")
	} else {
		after, err := DecodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		if after != "" {
			q = q.Where("id < ?", after)
		}
		q = q.OrderExpr("id DESC")
	}
	if f.Limit > 0 {
		q = q.Limit(int64(f.Limit + 1))
	}
	if err := q.Scan(ctx); err != nil {
		return nil, "", err
	}
	next := ""
	if f.Limit > 0 && len(rows) > f.Limit {
		rows = rows[:f.Limit]
		if !f.Oldest {
			next = EncodeCursor(rows[len(rows)-1].ID)
		}
	}
	if rows == nil {
		rows = []RentalBooking{}
	}
	return rows, next, nil
}

// ExpiredRentalHold is a held booking whose hold expired (job rental.holds_expire).
type ExpiredRentalHold struct {
	ID       string `bun:"id"`
	MarinaID string `bun:"marina_id"`
}

// ExpiredRentalHolds returns up to limit held bookings of every marina whose hold expired before now.
func ExpiredRentalHolds(ctx context.Context, db bun.IDB, now time.Time, limit int) ([]ExpiredRentalHold, error) {
	var out []ExpiredRentalHold
	err := db.NewSelect().Model((*RentalBooking)(nil)).Column("id", "marina_id").Where("status = ?", RentalStatusHeld).
		Where("hold_expires_at < ?", UTC(now)).OrderExpr("hold_expires_at, id").Limit(int64(limit)).Scan(ctx, &out)
	return out, err
}

// ---- extras ----

// ReplaceRentalBookingExtras replaces the extras of a booking with the given source.
func ReplaceRentalBookingExtras(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID, source string, rows []RentalBookingExtra, now time.Time) error {
	if _, err := tx.NewDelete().Model((*RentalBookingExtra)(nil)).Where("marina_id = ?", s.ID()).
		Where("booking_id = ?", bookingID).Where("source = ?", source).Exec(ctx); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].MarinaID, rows[i].BookingID, rows[i].Source, rows[i].CreatedAt = s.ID(), bookingID, source, UTC(now)
		if rows[i].ID == "" {
			rows[i].ID = NewID()
		}
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// ListRentalBookingExtras returns the extras of the bookings, by booking and creation.
func ListRentalBookingExtras(ctx context.Context, db bun.IDB, s MarinaScope, bookingIDs ...string) ([]RentalBookingExtra, error) {
	rows := []RentalBookingExtra{}
	if len(bookingIDs) == 0 {
		return rows, nil
	}
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("booking_id IN (?)", bun.List(bookingIDs)).
		Order("booking_id", "source", "created_at", "id").Scan(ctx)
	return rows, err
}

// ---- charge references (read only; rental_booking_charges is written by the handover side) ----

// RentalChargeRow is a charge linked to a booking.
type RentalChargeRow struct {
	BookingID       string `bun:"booking_id"`
	ChargeID        string `bun:"charge_id"`
	Kind            string `bun:"kind"`
	PaymentStatus   string `bun:"payment_status"`
	InvoicingStatus string `bun:"invoicing_status"`
	TotalCents      int64  `bun:"total_cents"`
	PaidAmountCents int64  `bun:"paid_amount_cents"`
	CreditedCents   int64  `bun:"credited_cents"`
	FiscalMode      string `bun:"fiscal_mode"`
}

// ListRentalChargeRows returns the charges linked to the bookings, by booking and link creation.
func ListRentalChargeRows(ctx context.Context, db bun.IDB, s MarinaScope, bookingIDs ...string) ([]RentalChargeRow, error) {
	rows := []RentalChargeRow{}
	if len(bookingIDs) == 0 {
		return rows, nil
	}
	err := db.NewSelect().TableExpr("rental_booking_charges AS l").
		Join("JOIN charges AS c ON c.id = l.charge_id AND c.marina_id = l.marina_id").
		ColumnExpr("l.booking_id, l.charge_id, l.kind, c.payment_status, c.invoicing_status, c.total_cents").
		ColumnExpr("c.paid_amount_cents, c.credited_cents, c.fiscal_mode").
		Where("l.marina_id = ?", s.ID()).Where("l.booking_id IN (?)", bun.List(bookingIDs)).
		OrderExpr("l.booking_id, l.created_at, l.charge_id").Scan(ctx, &rows)
	return rows, err
}

// RentalBookingHasExportedCharge reports whether a charge of the booking left the to_invoice state (exported or
// invoiced): the invoice flag can no longer change.
func RentalBookingHasExportedCharge(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) (bool, error) {
	n, err := db.NewSelect().TableExpr("rental_booking_charges AS l").
		Join("JOIN charges AS c ON c.id = l.charge_id AND c.marina_id = l.marina_id").
		Where("l.marina_id = ?", s.ID()).Where("l.booking_id = ?", bookingID).
		Where("c.invoicing_status <> ?", ChargeToInvoice).Count(ctx)
	return n > 0, err
}

// ---- small reads ----

// RentalVesselEnginesJSON returns the engines JSON of the vessel of a unit.
func RentalVesselEnginesJSON(ctx context.Context, db bun.IDB, s MarinaScope, vesselID string) (JSON, error) {
	var out JSON
	if !ValidID(vesselID) {
		return nil, ErrNotFound
	}
	err := db.NewSelect().Table("vessels").Column("engines").Where("marina_id = ?", s.ID()).Where("id = ?", vesselID).
		Limit(1).Scan(ctx, &out)
	return out, notFound(err)
}

// ListRentalUnitsForPlanner returns the units of the marina (retired excluded unless withRetired), by sort and code.
func ListRentalUnitsForPlanner(ctx context.Context, db bun.IDB, s MarinaScope, category string, withRetired bool) ([]RentalUnit, error) {
	rows := []RentalUnit{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if !withRetired {
		q = q.Where("status <> ?", RentalUnitRetired)
	}
	err := q.Order("sort", "code").Scan(ctx)
	return rows, err
}

// ListRentalSkippersForPlanner returns the active skippers of the marina, by sort and name.
func ListRentalSkippersForPlanner(ctx context.Context, db bun.IDB, s MarinaScope) ([]RentalSkipper, error) {
	rows := []RentalSkipper{}
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("active = ?", true).Order("sort", "name").Scan(ctx)
	return rows, err
}

// ListRentalFormulasForBooking returns the active formulas of the marina, by sort and code.
func ListRentalFormulasForBooking(ctx context.Context, db bun.IDB, s MarinaScope) ([]RentalFormula, error) {
	rows := []RentalFormula{}
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("active = ?", true).Order("sort", "code").Scan(ctx)
	return rows, err
}

// RentalIntentCheckout returns the checkout URL and expiry of a payment intent of the marina (the deposit
// pre-authorisation page shown as a QR while pending).
func RentalIntentCheckout(ctx context.Context, db bun.IDB, s MarinaScope, intentID string) (string, time.Time, error) {
	var row struct {
		URL       string    `bun:"checkout_url"`
		ExpiresAt time.Time `bun:"expires_at"`
	}
	if !ValidID(intentID) {
		return "", time.Time{}, ErrNotFound
	}
	err := db.NewSelect().Table("payment_intents").ColumnExpr("COALESCE(checkout_url, '') AS checkout_url, expires_at").
		Where("marina_id = ?", s.ID()).Where("id = ?", intentID).Limit(1).Scan(ctx, &row)
	return row.URL, row.ExpiresAt, notFound(err)
}

// RentalMarinaName returns the display name of the marina (rental emails).
func RentalMarinaName(ctx context.Context, db bun.IDB, s MarinaScope) string {
	var name string
	_ = db.NewSelect().Table("marinas").Column("name").Where("id = ?", s.ID()).Limit(1).Scan(ctx, &name)
	return name
}
