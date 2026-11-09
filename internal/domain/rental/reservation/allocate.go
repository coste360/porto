package reservation

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// allocateTx writes the slot rows of b (m11-plan §4.4). The caller holds the booking lock (when the booking
// exists), the unit lock and the skipper lock, and validated the range. Steps: reclaim the expired holds touching
// the plan (every row of those bookings, marked slots_lost_at), delete b's own rows (reschedule), insert the unit
// rows (range + buffer) and the skipper rows (range) with ON CONFLICT DO NOTHING and count them. A short count is
// a *SlotConflict: the caller's transaction rolls back everything, the reclaim included. The primary keys are the
// only guarantee (INV-18); nothing here checks for free slots first.
func (s *Service) allocateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.RentalBooking, buffer int, now time.Time) error {
	r := RangeOf(b)
	keys := slots.Expand(r, buffer)
	n := slots.Count(r)
	if n == 0 {
		return invalid("end", "invalid")
	}
	d0, d1 := keys[0].Date, keys[len(keys)-1].Date
	cands, err := store.ExpiredRentalHoldOwners(ctx, tx, scope, b.UnitID, b.SkipperID, d0, d1, now, b.ID)
	if err != nil {
		return err
	}
	if len(cands) > 0 {
		lost, err := store.LockExpiredRentalHolds(ctx, tx, scope, cands, now)
		if err != nil {
			return err
		}
		if err := store.DeleteRentalBookingSlots(ctx, tx, scope, lost...); err != nil {
			return err
		}
		if err := store.MarkRentalSlotsLost(ctx, tx, scope, lost, now); err != nil {
			return err
		}
	}
	if err := store.DeleteRentalBookingSlots(ctx, tx, scope, b.ID); err != nil {
		return err
	}
	kind, expires := store.RentalSlotBooked, time.Time{}
	if b.Status == store.RentalStatusHeld {
		kind, expires = store.RentalSlotHold, b.HoldExpiresAt
	}
	rows := make([]store.RentalSlot, len(keys))
	for i, k := range keys {
		k2 := kind
		if i >= n {
			k2 = store.RentalSlotBuffer
		}
		rows[i] = store.RentalSlot{UnitID: b.UnitID, Date: k.Date, Slot: k.Slot, BookingID: b.ID, Kind: k2, ExpiresAt: expires}
	}
	inserted, err := store.InsertRentalSlots(ctx, tx, scope, rows)
	if err != nil {
		return err
	}
	if int(inserted) < len(rows) {
		return conflictOf(ctx, tx, scope, TargetUnit, b.UnitID, b.ID, keys)
	}
	if b.SkipperID == "" {
		return nil
	}
	skRows := make([]store.RentalSkipperSlot, n)
	for i, k := range keys[:n] {
		skRows[i] = store.RentalSkipperSlot{SkipperID: b.SkipperID, Date: k.Date, Slot: k.Slot, BookingID: b.ID, Kind: kind, ExpiresAt: expires}
	}
	inserted, err = store.InsertRentalSkipperSlots(ctx, tx, scope, skRows)
	if err != nil {
		return err
	}
	if int(inserted) < n {
		return conflictOf(ctx, tx, scope, TargetSkipper, b.SkipperID, b.ID, keys[:n])
	}
	return nil
}

// insertBlockTx writes the block rows of a unit or skipper block through the same insert-and-count path (a
// conflict is a *SlotConflict naming the bookings in the way; a block never moves bookings). Expired holds in the
// way are reclaimed first.
func (s *Service) insertBlockTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, blk *store.RentalBlock, now time.Time) error {
	r := slots.Range{Start: slots.Point{Date: blk.FromOn, Slot: blk.FromSlot}, End: slots.Point{Date: blk.ToOn, Slot: blk.ToSlot}}
	keys := slots.Expand(r, 0)
	if len(keys) == 0 {
		return invalid("to", "invalid")
	}
	d0, d1 := keys[0].Date, keys[len(keys)-1].Date
	cands, err := store.ExpiredRentalHoldOwners(ctx, tx, scope, blk.UnitID, blk.SkipperID, d0, d1, now, "")
	if err != nil {
		return err
	}
	if len(cands) > 0 {
		lost, err := store.LockExpiredRentalHolds(ctx, tx, scope, cands, now)
		if err != nil {
			return err
		}
		if err := store.DeleteRentalBookingSlots(ctx, tx, scope, lost...); err != nil {
			return err
		}
		if err := store.MarkRentalSlotsLost(ctx, tx, scope, lost, now); err != nil {
			return err
		}
	}
	if err := store.DeleteRentalBlockSlots(ctx, tx, scope, blk.ID); err != nil {
		return err
	}
	if blk.UnitID != "" {
		rows := make([]store.RentalSlot, len(keys))
		for i, k := range keys {
			rows[i] = store.RentalSlot{UnitID: blk.UnitID, Date: k.Date, Slot: k.Slot, BlockID: blk.ID, Kind: store.RentalSlotBlock}
		}
		inserted, err := store.InsertRentalSlots(ctx, tx, scope, rows)
		if err != nil {
			return err
		}
		if int(inserted) < len(rows) {
			return conflictOf(ctx, tx, scope, TargetUnit, blk.UnitID, blk.ID, keys)
		}
		return nil
	}
	rows := make([]store.RentalSkipperSlot, len(keys))
	for i, k := range keys {
		rows[i] = store.RentalSkipperSlot{SkipperID: blk.SkipperID, Date: k.Date, Slot: k.Slot, BlockID: blk.ID, Kind: store.RentalSlotBlock}
	}
	inserted, err := store.InsertRentalSkipperSlots(ctx, tx, scope, rows)
	if err != nil {
		return err
	}
	if int(inserted) < len(rows) {
		return conflictOf(ctx, tx, scope, TargetSkipper, blk.SkipperID, blk.ID, keys)
	}
	return nil
}

// conflictOf reads the rows in the way of keys (owned by anything but self) and returns the *SlotConflict: the
// first conflicting date with its slots, and every booking and block in the way.
func conflictOf(ctx context.Context, tx bun.IDB, scope store.MarinaScope, target, resourceID, self string, keys []slots.Key) error {
	want := make(map[slots.Key]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	owners, err := store.RentalSlotOwners(ctx, tx, scope, target == TargetSkipper, resourceID, keys[0].Date, keys[len(keys)-1].Date)
	if err != nil {
		return err
	}
	c := &SlotConflict{Target: target, ResourceID: resourceID}
	var first calendar.Date
	for _, o := range owners {
		if !want[slots.Key{Date: o.Date, Slot: o.Slot}] || o.BookingID == self || o.BlockID == self {
			continue
		}
		if first.IsZero() {
			first = o.Date
		}
		if o.Date.Equal(first) {
			c.Slots = append(c.Slots, o.Slot)
		}
		if o.BookingID != "" && !slices.Contains(c.BookingIDs, o.BookingID) {
			c.BookingIDs = append(c.BookingIDs, o.BookingID)
		}
		if o.BlockID != "" && !slices.Contains(c.BlockIDs, o.BlockID) {
			c.BlockIDs = append(c.BlockIDs, o.BlockID)
		}
	}
	c.Date = first
	return c
}

func decodeJSON(raw []byte, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

func qtyOf(v int64) qty.Milli { return qty.Milli(v) }

func moneyOf(v int64) money.Cents { return money.Cents(v) }
