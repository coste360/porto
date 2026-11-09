package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of rental_booking_charges (m11-plan §9.1; owned by HANDOVER). INV-99: the partial unique index on
// (booking_id, kind) WHERE kind IN ('main','return') keeps at most one live main and one return charge per booking,
// even concurrently; a second insert is ErrConflict and the caller's transaction rolls back.

// RentalLinkedCharge is a charge of a booking with its link kind.
type RentalLinkedCharge struct {
	Charge   `bun:",extend"`
	LinkKind string `bun:"link_kind"`
}

// InsertRentalBookingCharge links a charge to a booking (ErrConflict on a second main or return link).
func InsertRentalBookingCharge(ctx context.Context, db bun.IDB, l *RentalBookingCharge) error {
	_, err := db.NewInsert().Model(l).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// SetRentalBookingChargeKind changes the link kind of a charge (main -> replaced when a reprice re-creates it).
func SetRentalBookingChargeKind(ctx context.Context, db bun.IDB, s MarinaScope, bookingID, chargeID, kind string) error {
	res, err := db.NewUpdate().Table("rental_booking_charges").Set("kind = ?", kind).Where("marina_id = ?", s.ID()).
		Where("booking_id = ?", bookingID).Where("charge_id = ?", chargeID).Exec(ctx)
	return expectOne(res, err)
}

// RentalChargesOf returns the charges linked to a booking, oldest first.
func RentalChargesOf(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) ([]RentalLinkedCharge, error) {
	out := []RentalLinkedCharge{}
	err := db.NewSelect().Model(&out).ModelTableExpr("charges AS c").ColumnExpr("c.*").ColumnExpr("rbc.kind AS link_kind").
		Join("JOIN rental_booking_charges AS rbc ON rbc.charge_id = c.id AND rbc.marina_id = c.marina_id").
		Where("c.marina_id = ?", s.ID()).Where("rbc.booking_id = ?", bookingID).
		OrderExpr("rbc.created_at, c.id").Scan(ctx)
	return out, err
}

// RentalRetentionChargeOf returns the live deposit_retention charge created for a booking (origin_id = booking id).
func RentalRetentionChargeOf(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) (*Charge, error) {
	out := new(Charge)
	err := db.NewSelect().Model(out).Where("marina_id = ?", s.ID()).Where("origin_type = ?", ChargeOriginDepositRetention).
		Where("origin_id = ?", bookingID).Where("payment_status <> ?", ChargeCancelled).OrderExpr("created_at DESC").
		Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return out, nil
}

// NewRentalBookingCharge builds a link row.
func NewRentalBookingCharge(s MarinaScope, bookingID, chargeID, kind string, now time.Time) *RentalBookingCharge {
	return &RentalBookingCharge{BookingID: bookingID, ChargeID: chargeID, MarinaID: s.ID(), Kind: kind, CreatedAt: UTC(now)}
}
