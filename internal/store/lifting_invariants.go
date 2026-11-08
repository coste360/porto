package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// CheckLiftingInvariants runs the M10 lifting invariant query on a marina and returns every violation (none on a
// coherent database). Used by tests and the acceptance scenarios.
//
//	Q31 (INV-17) the slot rows of a booking are exactly its range: a booked or done booking has one equipment_slots
//	    row per slot from_slot..to_slot-1 on its equipment and date, and one operator_slots row per slot when it
//	    has an operator (none otherwise); a requested or cancelled booking has no slot rows. Double booking itself is
//	    impossible by the primary keys of equipment_slots and operator_slots.
func CheckLiftingInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var bookings []EquipmentBooking
	if err := db.NewSelect().Model(&bookings).Where("marina_id = ?", marinaID).OrderExpr("id").Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q31: %w", err)
	}
	var eq []EquipmentSlot
	if err := db.NewSelect().Model(&eq).Where("marina_id = ?", marinaID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q31: %w", err)
	}
	var op []OperatorSlot
	if err := db.NewSelect().Model(&op).Where("marina_id = ?", marinaID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q31: %w", err)
	}
	eqBy := map[string][]EquipmentSlot{}
	for _, r := range eq {
		eqBy[r.BookingID] = append(eqBy[r.BookingID], r)
	}
	opBy := map[string][]OperatorSlot{}
	for _, r := range op {
		opBy[r.BookingID] = append(opBy[r.BookingID], r)
	}
	var out []Violation
	bad := func(id, detail string) {
		out = append(out, Violation{Query: "Q31", IDs: []string{id}, Detail: detail})
	}
	known := map[string]bool{}
	for _, b := range bookings {
		known[b.ID] = true
		live := b.Status == LiftBookingBooked || b.Status == LiftBookingDone
		if !live {
			if len(eqBy[b.ID]) > 0 || len(opBy[b.ID]) > 0 {
				bad(b.ID, "slot rows on a "+b.Status+" booking")
			}
			continue
		}
		if b.FromSlot == nil || b.ToSlot == nil || b.EquipmentID == "" {
			bad(b.ID, "live booking without a window")
			continue
		}
		want := *b.ToSlot - *b.FromSlot
		got := map[int]bool{}
		for _, r := range eqBy[b.ID] {
			if r.EquipmentID != b.EquipmentID || !r.Date.Equal(b.Date) || r.Slot < *b.FromSlot || r.Slot >= *b.ToSlot {
				bad(b.ID, fmt.Sprintf("equipment slot %d outside the booking", r.Slot))
			}
			got[r.Slot] = true
		}
		if len(got) != want || len(eqBy[b.ID]) != want {
			bad(b.ID, fmt.Sprintf("equipment slots %d ≠ %d", len(eqBy[b.ID]), want))
		}
		wantOp := 0
		if b.OperatorEmployeeID != "" {
			wantOp = want
		}
		for _, r := range opBy[b.ID] {
			if r.EmployeeID != b.OperatorEmployeeID || !r.Date.Equal(b.Date) || r.Slot < *b.FromSlot || r.Slot >= *b.ToSlot {
				bad(b.ID, fmt.Sprintf("operator slot %d outside the booking", r.Slot))
			}
		}
		if len(opBy[b.ID]) != wantOp {
			bad(b.ID, fmt.Sprintf("operator slots %d ≠ %d", len(opBy[b.ID]), wantOp))
		}
	}
	for id := range eqBy {
		if !known[id] {
			bad(id, "equipment slots of an unknown booking")
		}
	}
	return out, nil
}
