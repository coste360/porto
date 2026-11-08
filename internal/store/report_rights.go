package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// ReportRightHolderRow is a holder of a right of use overlapping the period, with the berth code.
type ReportRightHolderRow struct {
	RightID     string `bun:"right_id"`
	BerthID     string `bun:"berth_id"`
	BerthCode   string `bun:"berth_code"`
	CustomerID  string `bun:"customer_id"`
	ShareMillis int64  `bun:"share_millis"`
}

// ReportRights returns the holders of the rights (not cancelled) overlapping the period.
func ReportRights(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportRightHolderRow, error) {
	out := []ReportRightHolderRow{}
	err := db.NewSelect().TableExpr("berth_rights AS r").
		ColumnExpr("r.id AS right_id, r.berth_id, b.code AS berth_code, h.customer_id, h.share_millis").
		Join("JOIN berth_right_holders AS h ON h.right_id = r.id").
		Join("JOIN berths AS b ON b.id = r.berth_id").
		Where("r.marina_id = ?", s.ID()).Where("r.status <> ?", "cancelled").
		Where("r.from_on <= ?", f.To).Where("r.to_on >= ?", f.From).
		OrderExpr("b.code, r.id, h.customer_id").Scan(ctx, &out)
	return out, err
}

// ReportHolderChargeRow is a charge of a holder instalment or of a right.
type ReportHolderChargeRow struct {
	ID              string        `bun:"id"`
	Number          string        `bun:"number"`
	RightID         string        `bun:"right_id"`
	CustomerID      string        `bun:"customer_id"`
	IssuedOn        calendar.Date `bun:"issued_on"`
	TotalCents      int64         `bun:"total_cents"`
	PaidAmountCents int64         `bun:"paid_amount_cents"`
	CreditedCents   int64         `bun:"credited_cents"`
}

// ReportHolderCharges returns the non-cancelled holder-instalment (or right) charges issued in the period.
func ReportHolderCharges(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportHolderChargeRow, error) {
	out := []ReportHolderChargeRow{}
	err := db.NewSelect().TableExpr("charges").
		ColumnExpr("id, number, right_id AS right_id, customer_id AS customer_id, issued_on").
		ColumnExpr("total_cents, paid_amount_cents, credited_cents").
		Where("marina_id = ?", s.ID()).Where("payment_status <> ?", ChargeCancelled).
		Where("(origin_type = ? OR right_id IS NOT NULL)", ChargeOriginHolderInstallment).
		Where("issued_on >= ?", f.From).Where("issued_on <= ?", f.To).OrderExpr("issued_on, number").Scan(ctx, &out)
	return out, err
}

// ReportAllotmentRow is a common-charge allocation of an issued run.
type ReportAllotmentRow struct {
	RunID       string    `bun:"run_id"`
	RunKind     string    `bun:"run_kind"`
	ItemName    string    `bun:"item_name"`
	BerthID     string    `bun:"berth_id"`
	BerthCode   string    `bun:"berth_code"`
	RightID     string    `bun:"right_id"`
	CustomerID  string    `bun:"customer_id"`
	IssuedAt    time.Time `bun:"issued_at"`
	AmountCents int64     `bun:"amount_cents"`
}

// ReportCommonRuns returns the allocations of the common-charge runs issued in [from, to) (instants).
func ReportCommonRuns(ctx context.Context, db bun.IDB, s MarinaScope, from, to time.Time) ([]ReportAllotmentRow, error) {
	out := []ReportAllotmentRow{}
	err := db.NewSelect().TableExpr("common_charge_allocations AS a").
		ColumnExpr("a.run_id, r.kind AS run_kind, i.name AS item_name, a.berth_id, b.code AS berth_code").
		ColumnExpr("a.right_id AS right_id, a.customer_id AS customer_id, r.issued_at, a.amount_cents").
		Join("JOIN common_charge_runs AS r ON r.id = a.run_id").
		Join("JOIN common_charge_items AS i ON i.id = a.item_id").
		Join("JOIN berths AS b ON b.id = a.berth_id").
		Where("a.marina_id = ?", s.ID()).Where("r.status = ?", "issued").
		Where("r.issued_at >= ?", UTC(from)).Where("r.issued_at < ?", UTC(to)).
		OrderExpr("r.issued_at, a.run_id, b.code, i.name, a.id").Scan(ctx, &out)
	return out, err
}

// ReportMandateRow is a mandate revenue, a mandate credit (payments source mandate) or a payout (refunds payout).
type ReportMandateRow struct {
	Kind        string        `bun:"kind"` // revenue, credit, payout
	ID          string        `bun:"id"`
	MandateID   string        `bun:"mandate_id"`
	RightID     string        `bun:"right_id"`
	Date        calendar.Date `bun:"date"`
	AmountCents int64         `bun:"amount_cents"`
}

// ReportMandates returns the mandate revenues (through_on in the period), the mandate credits received and the
// succeeded payouts of the period (3 queries).
func ReportMandates(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportMandateRow, error) {
	out := []ReportMandateRow{}
	err := db.NewSelect().TableExpr("mandate_revenues AS r").
		ColumnExpr("'revenue' AS kind, r.id, r.mandate_id, m.right_id, r.through_on AS date, r.revenue_cents AS amount_cents").
		Join("JOIN management_mandates AS m ON m.id = r.mandate_id").
		Where("r.marina_id = ?", s.ID()).Where("r.through_on >= ?", f.From).Where("r.through_on <= ?", f.To).
		OrderExpr("r.through_on, r.id").Scan(ctx, &out)
	if err != nil {
		return nil, err
	}
	var credits, payouts []ReportMandateRow
	err = db.NewSelect().TableExpr("payments AS p").
		ColumnExpr("'credit' AS kind, p.id, p.mandate_id AS mandate_id, m.right_id AS right_id").
		ColumnExpr("p.received_on AS date, p.amount_cents").
		Join("LEFT JOIN management_mandates AS m ON m.id = p.mandate_id").
		Where("p.marina_id = ?", s.ID()).Where("p.source = ?", "mandate").
		Where("p.received_on >= ?", f.From).Where("p.received_on <= ?", f.To).OrderExpr("p.received_on, p.id").Scan(ctx, &credits)
	if err != nil {
		return nil, err
	}
	err = db.NewSelect().TableExpr("refunds AS p").
		ColumnExpr("'payout' AS kind, p.id, p.mandate_id AS mandate_id, m.right_id AS right_id").
		ColumnExpr("p.business_date AS date, p.amount_cents").
		Join("LEFT JOIN management_mandates AS m ON m.id = p.mandate_id").
		Where("p.marina_id = ?", s.ID()).Where("p.kind = ?", "payout").Where("p.status = ?", "succeeded").
		Where("p.business_date >= ?", f.From).Where("p.business_date <= ?", f.To).OrderExpr("p.business_date, p.id").Scan(ctx, &payouts)
	return append(append(out, credits...), payouts...), err
}
