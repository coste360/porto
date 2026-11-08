package store

import (
	"context"
	"errors"

	"github.com/uptrace/bun"
)

// Queries of work order billings (M10, m10-plan §10). Owned by ORDERS. Each billing creates at most one primary
// charge with origin_type 'work_order' and origin_id = the billing id (charges_origin_primary_uidx, INV-94); the
// charged lines of an order are the lines of its live charges, matched across billings by plan_key.

// InsertWOBilling inserts a billing row.
func InsertWOBilling(ctx context.Context, tx bun.IDB, s MarinaScope, b *WorkOrderBilling) error {
	b.MarinaID = s.ID()
	if len(b.Requested) == 0 {
		b.Requested = JSON("{}")
	}
	b.CreatedAt = UTC(b.CreatedAt)
	_, err := tx.NewInsert().Model(b).Exec(ctx)
	return err
}

// SetWOBillingCharge links the charge created for a billing (its id is the charge origin, so the row exists first).
func SetWOBillingCharge(ctx context.Context, tx bun.IDB, s MarinaScope, id, chargeID string, gross int64) error {
	res, err := tx.NewUpdate().TableExpr("work_order_billings").Set("charge_id = ?", chargeID).Set("status = ?", WOBillingCharged).
		Set("gross_cents = ?", gross).Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// WOBillingsOfOrder returns the billings of an order by seq.
func WOBillingsOfOrder(ctx context.Context, db bun.IDB, s MarinaScope, orderID string) ([]WorkOrderBilling, error) {
	out := []WorkOrderBilling{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("work_order_id = ?", orderID).OrderExpr("seq").Scan(ctx)
	return out, err
}

// NextWOBillingSeq returns the next billing seq of an order (call under the order lock).
func NextWOBillingSeq(ctx context.Context, db bun.IDB, s MarinaScope, orderID string) (int, error) {
	var n int
	err := db.NewSelect().TableExpr("work_order_billings").ColumnExpr("COALESCE(MAX(seq), 0) + 1").Where("marina_id = ?", s.ID()).
		Where("work_order_id = ?", orderID).Scan(ctx, &n)
	return n, err
}

// LiveChargesOfWorkOrder returns the non-cancelled charges of the billings of an order (origin work_order, origin
// id a billing of the order), in billing order (seq, then id: UUIDv7, time-ordered), so the last one is the latest
// live charge that an adjustment corrects. The charge number is not used: its format need not sort by creation.
func LiveChargesOfWorkOrder(ctx context.Context, db bun.IDB, s MarinaScope, orderID string) ([]Charge, error) {
	out := []Charge{}
	err := db.NewSelect().Model(&out).Where("charge.marina_id = ?", s.ID()).Where("charge.origin_type = ?", ChargeOriginWorkOrder).
		Where("charge.payment_status <> ?", ChargeCancelled).
		Where("charge.origin_id IN (SELECT b.id FROM work_order_billings AS b WHERE b.marina_id = ? AND b.work_order_id = ?)", s.ID(), orderID).
		OrderExpr("(SELECT b.seq FROM work_order_billings AS b WHERE b.id = charge.origin_id AND b.marina_id = charge.marina_id), charge.id").
		Scan(ctx)
	return out, err
}

// ChargesOfWorkOrders returns, per order, the charges (any status) of its billings, ordered by number.
func ChargesOfWorkOrders(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) (map[string][]Charge, error) {
	out := map[string][]Charge{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var bills []WorkOrderBilling
	if err := db.NewSelect().Model(&bills).Where("marina_id = ?", s.ID()).Where("work_order_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	if len(bills) == 0 {
		return out, nil
	}
	orderOf := make(map[string]string, len(bills))
	billIDs := make([]string, 0, len(bills))
	for _, b := range bills {
		orderOf[b.ID] = b.WorkOrderID
		billIDs = append(billIDs, b.ID)
	}
	var charges []Charge
	if err := db.NewSelect().Model(&charges).Where("marina_id = ?", s.ID()).Where("origin_type = ?", ChargeOriginWorkOrder).
		Where("origin_id IN (?)", bun.List(billIDs)).OrderExpr("number").Scan(ctx); err != nil {
		return nil, err
	}
	for _, c := range charges {
		out[orderOf[c.OriginID]] = append(out[orderOf[c.OriginID]], c)
	}
	return out, nil
}

// WorkOrderYardSettings reads the yard settings row of the marina (found = false when never saved).
func WorkOrderYardSettings(ctx context.Context, db bun.IDB, s MarinaScope) (YardSettings, bool, error) {
	var y YardSettings
	err := db.NewSelect().Model(&y).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err != nil {
		if errors.Is(notFound(err), ErrNotFound) {
			return YardSettings{}, false, nil
		}
		return YardSettings{}, false, err
	}
	return y, true, nil
}
