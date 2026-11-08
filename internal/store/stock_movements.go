package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of material issues and returns on work orders (M10, m10-plan §8; no stock levels in M10, D13). Owned by
// ORDERS. A return never exceeds the net issued quantity of the item on the order, checked under the order lock.

// InsertStockMovement inserts a movement.
func InsertStockMovement(ctx context.Context, tx bun.IDB, s MarinaScope, m *StockMovement) error {
	m.MarinaID = s.ID()
	m.CreatedAt = UTC(m.CreatedAt)
	_, err := tx.NewInsert().Model(m).Exec(ctx)
	return err
}

// StockMovementFilter filters the movement list.
type StockMovementFilter struct {
	WorkOrderID string
	ItemID      string
	From, To    calendar.Date
}

// ListStockMovements returns a page of movements, newest first.
func ListStockMovements(ctx context.Context, db bun.IDB, s MarinaScope, f StockMovementFilter, cursor string, limit int) (Page[StockMovement], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[StockMovement]{}, err
	}
	limit = ClampLimit(limit)
	var rows []StockMovement
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	for col, v := range map[string]string{"work_order_id": f.WorkOrderID, "item_id": f.ItemID} {
		if v == "" {
			continue
		}
		if !ValidID(v) {
			return Page[StockMovement]{Items: []StockMovement{}}, nil
		}
		q = q.Where("? = ?", bun.Ident(col), v)
	}
	if !f.From.IsZero() {
		q = q.Where("moved_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("moved_on <= ?", f.To)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[StockMovement]{}, err
	}
	return pageOf(rows, limit, func(m StockMovement) string { return m.ID }), nil
}

// StockMovementsOfOrders returns the movements of orders, oldest first.
func StockMovementsOfOrders(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) ([]StockMovement, error) {
	out := []StockMovement{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("work_order_id IN (?)", bun.List(ids)).
		OrderExpr("id").Scan(ctx)
	return out, err
}

// NetIssuedQty returns Σ issue − Σ return (milli) of an item on an order.
func NetIssuedQty(ctx context.Context, db bun.IDB, s MarinaScope, orderID, itemID string) (int64, error) {
	var n int64
	err := db.NewSelect().TableExpr("stock_movements").
		ColumnExpr("COALESCE(SUM(CASE WHEN kind = 'issue' THEN qty_milli ELSE -qty_milli END), 0)").
		Where("marina_id = ?", s.ID()).Where("work_order_id = ?", orderID).Where("item_id = ?", itemID).Scan(ctx, &n)
	return n, err
}
