package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of rental blocks (M11, m11-plan §4.5). Owned by BOOKING. A block occupies its unit or skipper through
// block rows of the slot tables, inserted by the same insert-and-count path as the bookings (INV-18).

// InsertRentalBlock inserts a block.
func InsertRentalBlock(ctx context.Context, tx bun.IDB, s MarinaScope, b *RentalBlock) error {
	b.MarinaID = s.ID()
	if b.Version == 0 {
		b.Version = 1
	}
	b.CreatedAt, b.UpdatedAt = UTC(b.CreatedAt), UTC(b.UpdatedAt)
	_, err := tx.NewInsert().Model(b).Exec(ctx)
	return err
}

// GetRentalBlock returns a block of the marina.
func GetRentalBlock(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalBlock, error) {
	return getRentalRow[RentalBlock](ctx, db, s, id, false)
}

// LockRentalBlock locks a block row and returns it.
func LockRentalBlock(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalBlock, error) {
	return getRentalRow[RentalBlock](ctx, db, s, id, true)
}

// UpdateRentalBlock writes the range, reason and note of a block when the version equals expected and bumps it.
func UpdateRentalBlock(ctx context.Context, tx bun.IDB, s MarinaScope, b *RentalBlock, expected int, now time.Time) error {
	b.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(b).Column("from_on", "from_slot", "to_on", "to_slot", "reason", "note", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", b.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalBlock(ctx, tx, s, b.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	b.Version = expected + 1
	return nil
}

// DeleteRentalBlock deletes a block (its rows first) when the version equals expected.
func DeleteRentalBlock(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int) error {
	if err := DeleteRentalBlockSlots(ctx, tx, s, id); err != nil {
		return err
	}
	res, err := tx.NewDelete().Model((*RentalBlock)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalBlock(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// RentalBlockFilter selects blocks touching [From, To].
type RentalBlockFilter struct {
	From      calendar.Date
	To        calendar.Date
	UnitID    string
	SkipperID string
}

// ListRentalBlocks returns the blocks matching f, by start.
func ListRentalBlocks(ctx context.Context, db bun.IDB, s MarinaScope, f RentalBlockFilter) ([]RentalBlock, error) {
	rows := []RentalBlock{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if !f.From.IsZero() {
		q = q.Where("to_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("from_on <= ?", f.To)
	}
	for _, ref := range []struct{ col, id string }{{"unit_id", f.UnitID}, {"skipper_id", f.SkipperID}} {
		if ref.id == "" {
			continue
		}
		if !ValidID(ref.id) {
			return rows, nil
		}
		q = q.Where("? = ?", bun.Ident(ref.col), ref.id)
	}
	err := q.Order("from_on", "from_slot", "id").Scan(ctx)
	return rows, err
}
