package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the external costs of work orders (M10, m10-plan §8: supplier as text, billable with a markup). Owned
// by ORDERS.

func normaliseExternalCost(c *WorkOrderExternalCost) {
	c.CreatedAt, c.UpdatedAt = UTC(c.CreatedAt), UTC(c.UpdatedAt)
}

// InsertExternalCost inserts an external cost.
func InsertExternalCost(ctx context.Context, tx bun.IDB, s MarinaScope, c *WorkOrderExternalCost) error {
	c.MarinaID = s.ID()
	if c.Version == 0 {
		c.Version = 1
	}
	normaliseExternalCost(c)
	_, err := tx.NewInsert().Model(c).Exec(ctx)
	return err
}

// GetExternalCost returns an external cost of the marina.
func GetExternalCost(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*WorkOrderExternalCost, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(WorkOrderExternalCost)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// UpdateExternalCost writes the mutable columns when the version equals expected and bumps it.
func UpdateExternalCost(ctx context.Context, tx bun.IDB, s MarinaScope, c *WorkOrderExternalCost, expected int, now time.Time) error {
	c.UpdatedAt = UTC(now)
	normaliseExternalCost(c)
	res, err := tx.NewUpdate().Model(c).
		Column("supplier_name", "reference", "incurred_on", "description", "work_type_id", "cost_cents", "billable",
			"price_cents", "item_id", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetExternalCost(ctx, tx, s, c.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	c.Version = expected + 1
	return nil
}

// DeleteExternalCost deletes an external cost at the expected version.
func DeleteExternalCost(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int) error {
	res, err := tx.NewDelete().Model((*WorkOrderExternalCost)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetExternalCost(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ExternalCostsOfOrders returns the external costs of orders, oldest first.
func ExternalCostsOfOrders(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) ([]WorkOrderExternalCost, error) {
	out := []WorkOrderExternalCost{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("work_order_id IN (?)", bun.List(ids)).
		OrderExpr("id").Scan(ctx)
	return out, err
}
