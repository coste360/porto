package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// CollectibleCharges returns a page of the open charges (residual > 0, not cancelled) of the contracts that have an
// active vessel allocation on night, or an arrival (first night) or a departure (last night + 1) on night: what the
// dock can collect today (m6b-plan §5.4). Ordered by id, newest first.
func CollectibleCharges(ctx context.Context, db bun.IDB, s MarinaScope, night calendar.Date, cursor string, limit int) (Page[Charge], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Charge]{}, err
	}
	contracts := db.NewSelect().TableExpr("allocations AS a").ColumnExpr("cl.contract_id").
		Join("JOIN contract_lines AS cl ON cl.id = a.contract_line_id AND cl.marina_id = a.marina_id").
		Where("a.marina_id = ?", s.ID()).Where("a.status = ?", AllocActive).Where("a.vessel_id IS NOT NULL").
		Where("a.first_night <= ?", night).Where("a.last_night >= ?", night.AddDays(-1))
	rows := []Charge{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).
		Where("payment_status <> ?", ChargeCancelled).Where("total_cents > 0").
		Where("total_cents - paid_amount_cents - credited_cents > 0").
		Where("contract_id IN (?)", contracts)
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Charge]{}, err
	}
	return pageOf(rows, limit, func(c Charge) string { return c.ID }), nil
}
