package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the rental slot rows (M11, m11-plan §4). Owned by BOOKING. INV-18: the primary keys of rental_slots
// (unit_id, date, slot) and rental_skipper_slots (skipper_id, date, slot) are the only guarantee against double
// booking (ADR 0003). Rows are inserted with ON CONFLICT DO NOTHING and counted by the caller: a short count is a
// conflict and the caller's transaction rolls back. Never check for free slots before inserting as a guarantee.
// INV-97: only internal/domain/rental/reservation writes these rows.

// rentalSlotChunk keeps every insert well below the SQLite variable limit (500 rows x 8 columns).
const rentalSlotChunk = 500

// RentalSlotKey is a (date, slot) key.
type RentalSlotKey struct {
	Date calendar.Date
	Slot int
}

// InsertRentalSlots inserts unit rows with ON CONFLICT DO NOTHING and returns how many were inserted.
func InsertRentalSlots(ctx context.Context, tx bun.IDB, s MarinaScope, rows []RentalSlot) (int64, error) {
	var total int64
	for start := 0; start < len(rows); start += rentalSlotChunk {
		part := rows[start:min(len(rows), start+rentalSlotChunk)]
		for i := range part {
			part[i].MarinaID, part[i].ExpiresAt = s.ID(), UTC(part[i].ExpiresAt)
		}
		res, err := tx.NewInsert().Model(&part).On("CONFLICT DO NOTHING").Exec(ctx)
		if err != nil {
			return total, err
		}
		total += affected(res)
	}
	return total, nil
}

// InsertRentalSkipperSlots inserts skipper rows with ON CONFLICT DO NOTHING and returns how many were inserted.
func InsertRentalSkipperSlots(ctx context.Context, tx bun.IDB, s MarinaScope, rows []RentalSkipperSlot) (int64, error) {
	var total int64
	for start := 0; start < len(rows); start += rentalSlotChunk {
		part := rows[start:min(len(rows), start+rentalSlotChunk)]
		for i := range part {
			part[i].MarinaID, part[i].ExpiresAt = s.ID(), UTC(part[i].ExpiresAt)
		}
		res, err := tx.NewInsert().Model(&part).On("CONFLICT DO NOTHING").Exec(ctx)
		if err != nil {
			return total, err
		}
		total += affected(res)
	}
	return total, nil
}

// DeleteRentalBookingSlots deletes every unit and skipper row of the bookings.
func DeleteRentalBookingSlots(ctx context.Context, tx bun.IDB, s MarinaScope, bookingIDs ...string) error {
	if len(bookingIDs) == 0 {
		return nil
	}
	for _, table := range []string{"rental_slots", "rental_skipper_slots"} {
		if _, err := tx.NewDelete().TableExpr(table).Where("marina_id = ?", s.ID()).
			Where("booking_id IN (?)", bun.List(bookingIDs)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRentalBlockSlots deletes every unit and skipper row of a block.
func DeleteRentalBlockSlots(ctx context.Context, tx bun.IDB, s MarinaScope, blockID string) error {
	for _, table := range []string{"rental_slots", "rental_skipper_slots"} {
		if _, err := tx.NewDelete().TableExpr(table).Where("marina_id = ?", s.ID()).Where("block_id = ?", blockID).
			Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ExpiredRentalHoldOwners returns the held bookings owning rows of the unit (and of the skipper, when set) on dates
// [d0, d1] whose expiry is before now, excluding the booking self. Candidates only: the caller locks and re-checks.
func ExpiredRentalHoldOwners(ctx context.Context, tx bun.IDB, s MarinaScope, unitID, skipperID string, d0, d1 calendar.Date, now time.Time, self string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(table, col, id string) error {
		if id == "" {
			return nil
		}
		var ids []string
		// "marina_id || ''" keeps the marina filter out of index selection (the primary key is the right one).
		q := tx.NewSelect().TableExpr(table).ColumnExpr("DISTINCT booking_id").Where("marina_id || '' = ?", s.ID()).
			Where("? = ?", bun.Ident(col), id).Where("date >= ?", d0).Where("date <= ?", d1).
			Where("booking_id IS NOT NULL").Where("expires_at IS NOT NULL").Where("expires_at < ?", UTC(now))
		if self != "" {
			q = q.Where("booking_id <> ?", self)
		}
		if err := q.Scan(ctx, &ids); err != nil {
			return err
		}
		for _, b := range ids {
			if !seen[b] {
				seen[b] = true
				out = append(out, b)
			}
		}
		return nil
	}
	if err := add("rental_slots", "unit_id", unitID); err != nil {
		return nil, err
	}
	if err := add("rental_skipper_slots", "skipper_id", skipperID); err != nil {
		return nil, err
	}
	return out, nil
}

// LockExpiredRentalHolds locks the held bookings among ids whose hold expired before now and returns their ids, in
// id order. On PostgreSQL rows already locked by another transaction (a confirm in progress) are skipped (SKIP
// LOCKED), so a reclaim never waits on a booking lock while holding a unit lock.
func LockExpiredRentalHolds(ctx context.Context, tx bun.IDB, s MarinaScope, ids []string, now time.Time) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []string
	q := tx.NewSelect().Model((*RentalBooking)(nil)).Column("id").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Where("status = ?", RentalStatusHeld).Where("hold_expires_at < ?", UTC(now)).
		Order("id")
	if IsPostgres(tx) {
		q = q.For("UPDATE SKIP LOCKED")
	}
	err := q.Scan(ctx, &out)
	return out, err
}

// RentalSlotOwner is a row of a unit or skipper slot table.
type RentalSlotOwner struct {
	Date      calendar.Date `bun:"date"`
	Slot      int           `bun:"slot"`
	BookingID string        `bun:"booking_id"`
	BlockID   string        `bun:"block_id"`
	Kind      string        `bun:"kind"`
	ExpiresAt time.Time     `bun:"expires_at"`
}

// RentalSlotOwners returns the rows of a unit (skipper false) or skipper on dates [d0, d1], by date and slot.
func RentalSlotOwners(ctx context.Context, db bun.IDB, s MarinaScope, skipper bool, id string, d0, d1 calendar.Date) ([]RentalSlotOwner, error) {
	table, col := "rental_slots", "unit_id"
	if skipper {
		table, col = "rental_skipper_slots", "skipper_id"
	}
	var out []RentalSlotOwner
	err := db.NewSelect().TableExpr(table).
		ColumnExpr("date, slot, COALESCE(CAST(booking_id AS TEXT), '') AS booking_id, COALESCE(CAST(block_id AS TEXT), '') AS block_id, kind, expires_at").
		Where("marina_id || '' = ?", s.ID()).Where("? = ?", bun.Ident(col), id).Where("date >= ?", d0).
		Where("date <= ?", d1).OrderExpr("date, slot").Scan(ctx, &out)
	return out, err
}

// SetRentalBookingSlotsBooked turns the hold rows of a booking into booked rows (unit buffer rows stay buffer) and
// clears their expiry.
func SetRentalBookingSlotsBooked(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID string) error {
	for _, table := range []string{"rental_slots", "rental_skipper_slots"} {
		if _, err := tx.NewUpdate().TableExpr(table).Set("kind = CASE WHEN kind = ? THEN ? ELSE kind END", RentalSlotHold, RentalSlotBooked).
			Set("expires_at = NULL").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// SetRentalBookingSlotsExpiry moves the expiry of the rows of a held booking.
func SetRentalBookingSlotsExpiry(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID string, at time.Time) error {
	for _, table := range []string{"rental_slots", "rental_skipper_slots"} {
		if _, err := tx.NewUpdate().TableExpr(table).Set("expires_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).
			Where("booking_id = ?", bookingID).Where("expires_at IS NOT NULL").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CountRentalBookingSlots returns the number of unit and skipper rows of a booking.
func CountRentalBookingSlots(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) (unit, skipper int, err error) {
	n, err := db.NewSelect().TableExpr("rental_slots").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).Count(ctx)
	if err != nil {
		return 0, 0, err
	}
	m, err := db.NewSelect().TableExpr("rental_skipper_slots").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).Count(ctx)
	return int(n), int(m), err
}

// TruncateRentalBookingSlots applies an early return (D11): the unit rows from cut to bufferEnd (exclusive) become
// buffer rows, the unit rows from bufferEnd on and the skipper rows from cut on are deleted.
func TruncateRentalBookingSlots(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID string, cut, bufferEnd RentalSlotKey) error {
	const after = "(date > ? OR (date = ? AND slot >= ?))"
	if _, err := tx.NewDelete().TableExpr("rental_slots").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).
		Where(after, bufferEnd.Date, bufferEnd.Date, bufferEnd.Slot).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.NewUpdate().TableExpr("rental_slots").Set("kind = ?", RentalSlotBuffer).Where("marina_id = ?", s.ID()).
		Where("booking_id = ?", bookingID).Where(after, cut.Date, cut.Date, cut.Slot).Exec(ctx); err != nil {
		return err
	}
	_, err := tx.NewDelete().TableExpr("rental_skipper_slots").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).
		Where(after, cut.Date, cut.Date, cut.Slot).Exec(ctx)
	return err
}

// RentalWindowRow is a slot row of the planner window with the owning booking's status and number.
type RentalWindowRow struct {
	ResourceID string        `bun:"resource_id"`
	Date       calendar.Date `bun:"date"`
	Slot       int           `bun:"slot"`
	BookingID  string        `bun:"booking_id"`
	BlockID    string        `bun:"block_id"`
	Kind       string        `bun:"kind"`
	ExpiresAt  time.Time     `bun:"expires_at"`
	Status     string        `bun:"status"`
	Number     int           `bun:"number"`
	Year       int           `bun:"year"`
	Reason     string        `bun:"reason"`
}

// ListRentalWindowRows returns the unit (skipper false) or skipper rows of the marina on dates [d0, d1], by
// resource, date and slot, with the booking status and number or the block reason.
func ListRentalWindowRows(ctx context.Context, db bun.IDB, s MarinaScope, skipper bool, d0, d1 calendar.Date) ([]RentalWindowRow, error) {
	table, col := "rental_slots", "unit_id"
	if skipper {
		table, col = "rental_skipper_slots", "skipper_id"
	}
	var out []RentalWindowRow
	err := db.NewSelect().TableExpr(table+" AS r").
		ColumnExpr("r."+col+" AS resource_id, r.date, r.slot, r.kind, r.expires_at").
		ColumnExpr("COALESCE(CAST(r.booking_id AS TEXT), '') AS booking_id, COALESCE(CAST(r.block_id AS TEXT), '') AS block_id").
		ColumnExpr("COALESCE(b.status, '') AS status, COALESCE(b.number, 0) AS number, COALESCE(b.year, 0) AS year").
		ColumnExpr("COALESCE(k.reason, '') AS reason").
		Join("LEFT JOIN rental_bookings AS b ON b.id = r.booking_id AND b.marina_id = r.marina_id").
		Join("LEFT JOIN rental_blocks AS k ON k.id = r.block_id AND k.marina_id = r.marina_id").
		Where("r.marina_id = ?", s.ID()).Where("r.date >= ?", d0).Where("r.date <= ?", d1).
		OrderExpr("resource_id, r.date, r.slot").Scan(ctx, &out)
	return out, err
}

// ListRentalBookingSlots returns the unit (skipper false) or skipper rows of the bookings, by booking, date and slot.
func ListRentalBookingSlots(ctx context.Context, db bun.IDB, s MarinaScope, skipper bool, bookingIDs ...string) ([]RentalWindowRow, error) {
	table, col := "rental_slots", "unit_id"
	if skipper {
		table, col = "rental_skipper_slots", "skipper_id"
	}
	var out []RentalWindowRow
	if len(bookingIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr(table+" AS r").
		ColumnExpr("r."+col+" AS resource_id, r.date, r.slot, r.kind, r.expires_at, r.booking_id").
		Where("r.marina_id = ?", s.ID()).Where("r.booking_id IN (?)", bun.List(bookingIDs)).
		OrderExpr("r.booking_id, r.date, r.slot").Scan(ctx, &out)
	return out, err
}
