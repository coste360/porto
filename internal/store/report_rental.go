package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read queries of the rental report (m11-plan §11; owned by FLEET). Every query is scoped by marina_id and returns
// ids, codes, enums and amounts only (no renter or skipper data).

// ReportRentalSlotDay counts the slots of a unit on a date inside [openFrom, openTo): booked rows of bookings that
// are not cancelled or no-show, and block rows.
type ReportRentalSlotDay struct {
	UnitID  string        `bun:"unit_id"`
	Date    calendar.Date `bun:"date"`
	Rented  int64         `bun:"rented"`
	Blocked int64         `bun:"blocked"`
}

// ReportRentalSlotDays returns the slot counts of the units per date in [from, to].
func ReportRentalSlotDays(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date, openFrom, openTo int) ([]ReportRentalSlotDay, error) {
	out := []ReportRentalSlotDay{}
	err := db.NewSelect().TableExpr("rental_slots AS rs").
		Join("LEFT JOIN rental_bookings AS b ON b.id = rs.booking_id AND b.marina_id = rs.marina_id").
		ColumnExpr("rs.unit_id, rs.date").
		ColumnExpr("SUM(CASE WHEN rs.kind = ? THEN 1 ELSE 0 END) AS rented", RentalSlotBooked).
		ColumnExpr("SUM(CASE WHEN rs.kind = ? THEN 1 ELSE 0 END) AS blocked", RentalSlotBlock).
		Where("rs.marina_id = ?", s.ID()).Where("rs.date >= ?", from).Where("rs.date <= ?", to).
		Where("rs.slot >= ?", openFrom).Where("rs.slot < ?", openTo).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("rs.kind = ?", RentalSlotBlock).
				WhereOr("rs.kind = ? AND b.status NOT IN (?)", RentalSlotBooked, bun.List([]string{RentalStatusCancelled, RentalStatusNoShow}))
		}).
		GroupExpr("rs.unit_id, rs.date").OrderExpr("rs.unit_id, rs.date").Scan(ctx, &out)
	return out, err
}

// ReportRentalChargeLine is a line of a live rental-origin charge with the booking, unit and formula references
// the report needs to classify it (line kind).
type ReportRentalChargeLine struct {
	ChargeID      string        `bun:"charge_id"`
	IssuedOn      calendar.Date `bun:"issued_on"`
	LinkKind      string        `bun:"link_kind"` // rental_booking_charges.kind (main, return, adjustment, replaced)
	CorrectsID    string        `bun:"corrects_charge_id"`
	BookingID     string        `bun:"booking_id"`
	BookingYear   int           `bun:"booking_year"`
	BookingNumber int64         `bun:"booking_number"`
	UnitID        string        `bun:"unit_id"`
	UnitCode      string        `bun:"unit_code"`
	Category      string        `bun:"category"`
	VesselID      string        `bun:"vessel_id"`
	UnitItemID    string        `bun:"unit_item_id"`
	FormulaID     string        `bun:"formula_id"`
	FormulaCode   string        `bun:"formula_code"`
	FuelItemID    string        `bun:"fuel_item_id"`
	LateItemID    string        `bun:"late_item_id"`
	ItemID        string        `bun:"item_id"`
	PlanKey       string        `bun:"plan_key"`
	NetCents      int64         `bun:"net_cents"`
	VATCents      int64         `bun:"vat_cents"`
	GrossCents    int64         `bun:"gross_cents"`
}

// ReportRentalChargeLines returns the lines of the rental-origin charges issued in [from, to] that are not
// cancelled. Deposit retentions have their own origin and are never returned (INV-98).
func ReportRentalChargeLines(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportRentalChargeLine, error) {
	out := []ReportRentalChargeLine{}
	err := db.NewSelect().TableExpr("charge_lines AS l").
		Join("JOIN charges AS c ON c.id = l.charge_id AND c.marina_id = l.marina_id").
		Join("JOIN rental_bookings AS b ON b.id = c.origin_id AND b.marina_id = c.marina_id").
		Join("JOIN rental_units AS u ON u.id = b.unit_id AND u.marina_id = b.marina_id").
		Join("JOIN rental_formulas AS f ON f.id = b.formula_id AND f.marina_id = b.marina_id").
		Join("LEFT JOIN rental_booking_charges AS bc ON bc.charge_id = c.id AND bc.marina_id = c.marina_id").
		ColumnExpr("c.id AS charge_id, c.issued_on, COALESCE(bc.kind, '') AS link_kind, COALESCE(CAST(c.corrects_charge_id AS TEXT), '') AS corrects_charge_id").
		ColumnExpr("b.id AS booking_id, b.year AS booking_year, b.number AS booking_number").
		ColumnExpr("u.id AS unit_id, u.code AS unit_code, u.category, u.vessel_id, u.item_id AS unit_item_id").
		ColumnExpr("f.id AS formula_id, f.code AS formula_code, COALESCE(CAST(f.fuel_item_id AS TEXT), '') AS fuel_item_id, COALESCE(CAST(f.late_item_id AS TEXT), '') AS late_item_id").
		ColumnExpr("COALESCE(CAST(l.item_id AS TEXT), '') AS item_id, l.plan_key, l.net_cents, l.vat_cents, l.gross_cents").
		Where("l.marina_id = ?", s.ID()).Where("c.origin_type = ?", ChargeOriginRental).Where("c.cancelled_at IS NULL").
		Where("c.issued_on >= ?", from).Where("c.issued_on <= ?", to).
		OrderExpr("c.issued_on, c.id, l.seq").Scan(ctx, &out)
	return out, err
}

// ReportRentalDamage is a damage with its booking and unit.
type ReportRentalDamage struct {
	ID            string    `bun:"id"`
	CreatedAt     time.Time `bun:"created_at"`
	Status        string    `bun:"status"`
	EstimateCents int64     `bun:"estimate_cents"`
	RetainedCents int64     `bun:"retained_cents"`
	BookingID     string    `bun:"booking_id"`
	BookingYear   int       `bun:"booking_year"`
	BookingNumber int64     `bun:"booking_number"`
	UnitID        string    `bun:"unit_id"`
	UnitCode      string    `bun:"unit_code"`
	Category      string    `bun:"category"`
	VesselID      string    `bun:"vessel_id"`
}

// ReportRentalDamages returns the damages recorded in [fromAt, toAt).
func ReportRentalDamages(ctx context.Context, db bun.IDB, s MarinaScope, fromAt, toAt time.Time) ([]ReportRentalDamage, error) {
	out := []ReportRentalDamage{}
	err := db.NewSelect().TableExpr("rental_damages AS d").
		Join("JOIN rental_bookings AS b ON b.id = d.booking_id AND b.marina_id = d.marina_id").
		Join("JOIN rental_units AS u ON u.id = b.unit_id AND u.marina_id = b.marina_id").
		ColumnExpr("d.id, d.created_at, d.status, d.estimate_cents, d.retained_cents").
		ColumnExpr("b.id AS booking_id, b.year AS booking_year, b.number AS booking_number").
		ColumnExpr("u.id AS unit_id, u.code AS unit_code, u.category, u.vessel_id").
		Where("d.marina_id = ?", s.ID()).Where("d.created_at >= ?", UTC(fromAt)).Where("d.created_at < ?", UTC(toAt)).
		OrderExpr("d.created_at, d.id").Scan(ctx, &out)
	return out, err
}

// ReportRentalDeposit is the deposit of a booking.
type ReportRentalDeposit struct {
	BookingID     string        `bun:"booking_id"`
	BookingYear   int           `bun:"booking_year"`
	BookingNumber int64         `bun:"booking_number"`
	StartsOn      calendar.Date `bun:"starts_on"`
	Mode          string        `bun:"deposit_mode"`
	Status        string        `bun:"deposit_status"`
	DepositCents  int64         `bun:"deposit_cents"`
	RetainedCents int64         `bun:"deposit_retained_cents"`
	UnitID        string        `bun:"unit_id"`
	UnitCode      string        `bun:"unit_code"`
	Category      string        `bun:"category"`
	VesselID      string        `bun:"vessel_id"`
	FormulaID     string        `bun:"formula_id"`
	FormulaCode   string        `bun:"formula_code"`
}

// ReportRentalDeposits returns the deposits of the bookings starting in [from, to] that have one.
func ReportRentalDeposits(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportRentalDeposit, error) {
	out := []ReportRentalDeposit{}
	err := db.NewSelect().TableExpr("rental_bookings AS b").
		Join("JOIN rental_units AS u ON u.id = b.unit_id AND u.marina_id = b.marina_id").
		Join("JOIN rental_formulas AS f ON f.id = b.formula_id AND f.marina_id = b.marina_id").
		ColumnExpr("b.id AS booking_id, b.year AS booking_year, b.number AS booking_number, b.starts_on").
		ColumnExpr("COALESCE(b.deposit_mode, '') AS deposit_mode, b.deposit_status, b.deposit_cents, b.deposit_retained_cents").
		ColumnExpr("u.id AS unit_id, u.code AS unit_code, u.category, u.vessel_id, f.id AS formula_id, f.code AS formula_code").
		Where("b.marina_id = ?", s.ID()).Where("b.starts_on >= ?", from).Where("b.starts_on <= ?", to).
		Where("b.deposit_status <> ?", RentalDepositStatusNone).
		OrderExpr("b.starts_on, b.year, b.number").Scan(ctx, &out)
	return out, err
}
