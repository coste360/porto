package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of rental_damages (m11-plan §8; owned by HANDOVER).

// GetRentalDamage returns a damage of the marina.
func GetRentalDamage(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalDamage, error) {
	return getRentalRow[RentalDamage](ctx, db, s, id, false)
}

// LockRentalDamage locks a damage row and returns it.
func LockRentalDamage(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalDamage, error) {
	return getRentalRow[RentalDamage](ctx, db, s, id, true)
}

// RentalDamagesOf returns the damages of a booking, oldest first.
func RentalDamagesOf(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) ([]RentalDamage, error) {
	out := []RentalDamage{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).
		OrderExpr("created_at, id").Scan(ctx)
	return out, err
}

// InsertRentalDamage inserts a damage.
func InsertRentalDamage(ctx context.Context, db bun.IDB, d *RentalDamage) error {
	_, err := db.NewInsert().Model(d).Exec(ctx)
	return err
}

// UpdateRentalDamage writes the mutable columns of a damage, conditional on its version (bumped).
func UpdateRentalDamage(ctx context.Context, db bun.IDB, s MarinaScope, d *RentalDamage, now time.Time) error {
	res, err := db.NewUpdate().Model(d).
		Column("description", "estimate_cents", "status", "retained_cents", "charged_cents", "waive_reason").
		Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", d.ID).Where("version = ?", d.Version).Exec(ctx)
	if err := expectOne(res, err); err != nil {
		return err
	}
	d.Version++
	d.UpdatedAt = UTC(now)
	return nil
}

// DeleteRentalDamage deletes a damage (its photo rows cascade).
func DeleteRentalDamage(ctx context.Context, db bun.IDB, s MarinaScope, id string) error {
	res, err := db.NewDelete().TableExpr("rental_damages").Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}
