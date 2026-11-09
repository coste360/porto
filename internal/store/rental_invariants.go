package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Store invariant query Q32 of the rental bookings (m11-plan §15, INV-19), run by the integration suites. Owned by
// BOOKING.

// rentalRangeKeys lists the (date, slot) keys from (d0, s0) inclusive to (d1, s1) exclusive followed by extra keys.
func rentalRangeKeys(d0 calendar.Date, s0 int, d1 calendar.Date, s1 int, extra int) []RentalSlotKey {
	n := d0.DaysUntil(d1)*48 + s1 - s0
	if n <= 0 {
		return nil
	}
	out := make([]RentalSlotKey, 0, n+extra)
	d, s := d0, s0
	for range n + extra {
		out = append(out, RentalSlotKey{Date: d, Slot: s})
		s++
		if s == 48 {
			s, d = 0, d.AddDays(1)
		}
	}
	return out
}

// CheckRentalSlots runs Q32 on a marina and returns one line per violation (empty = the invariant holds): the
// booked (or hold) unit rows of a live booking are exactly its range, followed by exactly buffer_slots buffer
// rows; a returned or closed booking keeps a non-empty prefix of its range (early return) followed by the buffer;
// its skipper rows are the same keys without buffer, none without a skipper; cancelled and no-show bookings have
// no rows; hold rows and expiries only for held bookings; a held booking whose rows were reclaimed has none.
func CheckRentalSlots(ctx context.Context, db bun.IDB, marinaID string) ([]string, error) {
	scope, err := SystemMarinaScope(ctx, db, marinaID)
	if err != nil {
		return nil, err
	}
	settings, err := LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	buffer := settings.BufferSlots
	var bookings []RentalBooking
	if err := db.NewSelect().Model(&bookings).Where("marina_id = ?", marinaID).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	unitRows, err := allRentalRows(ctx, db, marinaID, "rental_slots")
	if err != nil {
		return nil, err
	}
	skipperRows, err := allRentalRows(ctx, db, marinaID, "rental_skipper_slots")
	if err != nil {
		return nil, err
	}
	var out []string
	bad := func(b *RentalBooking, format string, args ...any) {
		out = append(out, fmt.Sprintf("Q32 booking %s (%s): ", b.ID, b.Status)+fmt.Sprintf(format, args...))
	}
	known := map[string]bool{}
	for i := range bookings {
		b := &bookings[i]
		known[b.ID] = true
		units, skippers := unitRows[b.ID], skipperRows[b.ID]
		switch b.Status {
		case RentalStatusCancelled, RentalStatusNoShow:
			if len(units)+len(skippers) > 0 {
				bad(b, "%d unit and %d skipper rows left", len(units), len(skippers))
			}
			continue
		case RentalStatusHeld:
			if !b.SlotsLostAt.IsZero() && len(units)+len(skippers) == 0 {
				continue
			}
		}
		held := b.Status == RentalStatusHeld
		var booked, buf []RentalSlotKey
		for _, r := range units {
			k := RentalSlotKey{Date: r.Date, Slot: r.Slot}
			switch r.Kind {
			case RentalSlotHold, RentalSlotBooked:
				if (r.Kind == RentalSlotHold) != held {
					bad(b, "unit row %s/%d has kind %s", r.Date, r.Slot, r.Kind)
				}
				booked = append(booked, k)
			case RentalSlotBuffer:
				buf = append(buf, k)
			default:
				bad(b, "unit row %s/%d has kind %s", r.Date, r.Slot, r.Kind)
			}
			if r.ExpiresAt.IsZero() == held {
				bad(b, "unit row %s/%d expiry %v", r.Date, r.Slot, r.ExpiresAt)
			}
		}
		rangeKeys := rentalRangeKeys(b.StartsOn, b.StartSlot, b.EndsOn, b.EndSlot, 0)
		truncatable := b.Status == RentalStatusReturned || b.Status == RentalStatusClosed
		switch {
		case len(booked) == 0 || len(booked) > len(rangeKeys):
			bad(b, "%d booked rows for a range of %d", len(booked), len(rangeKeys))
			continue
		case len(booked) < len(rangeKeys) && !truncatable:
			bad(b, "%d booked rows for a range of %d", len(booked), len(rangeKeys))
			continue
		}
		if !slices.Equal(booked, rangeKeys[:len(booked)]) {
			bad(b, "booked rows differ from the range")
		}
		last := booked[len(booked)-1]
		wantBuf := rentalRangeKeys(last.Date, last.Slot, last.Date, last.Slot+1, buffer)[1:]
		if !slices.Equal(buf, wantBuf) {
			bad(b, "buffer rows %v, want %v", buf, wantBuf)
		}
		var sk []RentalSlotKey
		for _, r := range skippers {
			sk = append(sk, RentalSlotKey{Date: r.Date, Slot: r.Slot})
			want := RentalSlotBooked
			if held {
				want = RentalSlotHold
			}
			if r.Kind != want || r.ExpiresAt.IsZero() == held {
				bad(b, "skipper row %s/%d kind %s", r.Date, r.Slot, r.Kind)
			}
		}
		if b.SkipperID == "" {
			if len(sk) > 0 {
				bad(b, "%d skipper rows without a skipper", len(sk))
			}
		} else if !slices.Equal(sk, booked) {
			bad(b, "skipper rows (%d) differ from the booked rows (%d)", len(sk), len(booked))
		}
	}
	for id := range unitRows {
		if !known[id] {
			out = append(out, "Q32 rows of unknown booking "+id)
		}
	}
	slices.Sort(out)
	return out, nil
}

func allRentalRows(ctx context.Context, db bun.IDB, marinaID, table string) (map[string][]RentalSlotOwner, error) {
	var rows []RentalSlotOwner
	if err := db.NewSelect().TableExpr(table).
		ColumnExpr("date, slot, booking_id, '' AS block_id, kind, expires_at").
		Where("marina_id = ?", marinaID).Where("booking_id IS NOT NULL").OrderExpr("booking_id, date, slot").
		Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string][]RentalSlotOwner{}
	for _, r := range rows {
		out[r.BookingID] = append(out[r.BookingID], r)
	}
	return out, nil
}

// CheckRentalInvariants runs the rental invariant queries of a marina (m11-plan §15): Q32 (INV-19, rows = range,
// CheckRentalSlots), Q33 (INV-99: at most one live main and one return link per booking; for a closed booking the
// live main charge plus its adjustments equals the snapshot total) and B60 (INV-98: every match of a rental deposit
// payment points to a deposit_retention charge, the retained amount of a closed booking equals what the deposit
// payment has matched, and never exceeds the deposit). Details carry ids and amounts only.
func CheckRentalInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	q32, err := CheckRentalSlots(ctx, db, marinaID)
	if err != nil {
		return nil, fmt.Errorf("Q32: %w", err)
	}
	for _, v := range q32 {
		out = append(out, Violation{Query: "Q32", Detail: v})
	}

	// Q33a: one live main and one return link per booking.
	var dup []struct {
		BookingID string `bun:"booking_id"`
		Kind      string `bun:"kind"`
		N         int    `bun:"n"`
	}
	if err := db.NewSelect().TableExpr("rental_booking_charges").ColumnExpr("booking_id, kind, COUNT(*) AS n").
		Where("marina_id = ?", marinaID).Where("kind IN (?)", bun.List([]string{RentalChargeMain, RentalChargeReturn})).
		GroupExpr("booking_id, kind").Having("COUNT(*) > 1").Scan(ctx, &dup); err != nil {
		return nil, fmt.Errorf("Q33: %w", err)
	}
	for _, d := range dup {
		out = append(out, Violation{Query: "Q33", IDs: []string{d.BookingID}, Detail: fmt.Sprintf("%d %s links", d.N, d.Kind)})
	}
	// Q33b: closed bookings: live main + adjustments = snapshot total. A closed booking without any live main or
	// adjustment charge counts 0, so a main charge cancelled and never re-created is reported.
	var sums []struct {
		BookingID  string `bun:"booking_id"`
		TotalCents int64  `bun:"total_cents"`
		Charged    int64  `bun:"charged"`
	}
	charged := db.NewSelect().TableExpr("rental_booking_charges AS l").
		Join("JOIN charges AS c ON c.id = l.charge_id AND c.marina_id = l.marina_id").
		ColumnExpr("COALESCE(SUM(c.total_cents), 0)").
		Where("l.booking_id = b.id").Where("l.marina_id = b.marina_id").
		Where("l.kind IN (?)", bun.List([]string{RentalChargeMain, RentalChargeAdjustment})).
		Where("c.payment_status <> ?", ChargeCancelled)
	if err := db.NewSelect().TableExpr("rental_bookings AS b").
		ColumnExpr("b.id AS booking_id, b.total_cents").ColumnExpr("(?) AS charged", charged).
		Where("b.marina_id = ?", marinaID).Where("b.status = ?", RentalStatusClosed).Scan(ctx, &sums); err != nil {
		return nil, fmt.Errorf("Q33: %w", err)
	}
	for _, s := range sums {
		if s.Charged != s.TotalCents {
			out = append(out, Violation{Query: "Q33", IDs: []string{s.BookingID}, Detail: fmt.Sprintf("main+adjustments %d, snapshot %d", s.Charged, s.TotalCents)})
		}
	}

	// B60a: matches of rental deposit payments only to deposit_retention charges.
	var bad []struct {
		BookingID string `bun:"booking_id"`
		ChargeID  string `bun:"charge_id"`
	}
	if err := db.NewSelect().TableExpr("rental_bookings AS b").
		Join("JOIN payment_matches AS m ON m.payment_id = b.deposit_payment_id AND m.marina_id = b.marina_id").
		Join("JOIN charges AS c ON c.id = m.charge_id AND c.marina_id = m.marina_id").
		ColumnExpr("b.id AS booking_id, c.id AS charge_id").
		Where("b.marina_id = ?", marinaID).Where("m.reverses_match_id IS NULL").
		Where("c.origin_type <> ?", ChargeOriginDepositRetention).Scan(ctx, &bad); err != nil {
		return nil, fmt.Errorf("B60: %w", err)
	}
	for _, b := range bad {
		out = append(out, Violation{Query: "B60", IDs: []string{b.BookingID, b.ChargeID}, Detail: "deposit matched to a non-retention charge"})
	}
	// B60b/c: retained = matched for closed bookings with a deposit payment; retained <= deposit.
	var dep []struct {
		BookingID string `bun:"booking_id"`
		Status    string `bun:"status"`
		Deposit   int64  `bun:"deposit_cents"`
		Retained  int64  `bun:"deposit_retained_cents"`
		HasPay    bool   `bun:"has_pay"`
		Matched   int64  `bun:"matched"`
	}
	if err := db.NewSelect().TableExpr("rental_bookings AS b").
		ColumnExpr("b.id AS booking_id, b.status, b.deposit_cents, b.deposit_retained_cents").
		ColumnExpr("b.deposit_payment_id IS NOT NULL AS has_pay").
		ColumnExpr("COALESCE((SELECT SUM(m.amount_cents - m.reversed_cents) FROM payment_matches AS m WHERE m.payment_id = b.deposit_payment_id "+
			"AND m.marina_id = b.marina_id AND m.reverses_match_id IS NULL), 0) AS matched").
		Where("b.marina_id = ?", marinaID).Scan(ctx, &dep); err != nil {
		return nil, fmt.Errorf("B60: %w", err)
	}
	for _, d := range dep {
		if d.Retained > d.Deposit {
			out = append(out, Violation{Query: "B60", IDs: []string{d.BookingID}, Detail: fmt.Sprintf("retained %d > deposit %d", d.Retained, d.Deposit)})
		}
		if d.HasPay && d.Status == RentalStatusClosed && d.Retained != d.Matched {
			out = append(out, Violation{Query: "B60", IDs: []string{d.BookingID}, Detail: fmt.Sprintf("retained %d, matched %d", d.Retained, d.Matched)})
		}
	}
	return out, nil
}
