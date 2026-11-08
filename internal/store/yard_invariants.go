package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// CheckYardInvariants runs the M10 work order invariant queries on a marina and returns every violation (none on a
// coherent database). Used by tests and the acceptance scenarios. The account equations themselves are pure
// (TestRapid_AccountReconciles); these queries check what the account relies on in the database:
//
//	Q30 (INV-93) an approved entry carries cost = hours × frozen rate (half-even to the cent); the net issued
//	    quantity of every item of an order is not negative (returns never exceed issues).
//	Q32 (INV-94) a charged billing has exactly its charge: origin work_order, origin id the billing, same total;
//	    an invoiced order has a final billing.
//	Q33 (INV-95) at most one running timer per employee.
func CheckYardInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	var entries []Timesheet
	if err := db.NewSelect().Model(&entries).Where("marina_id = ?", marinaID).Where("status = ?", TimesheetApproved).Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q30: %w", err)
	}
	for _, e := range entries {
		if e.CostCents == nil || e.CostRateCents == nil {
			out = append(out, Violation{Query: "Q30", IDs: []string{e.ID}, Detail: "approved entry without frozen cost"})
			continue
		}
		if want := halfEvenDiv(e.HoursMilli**e.CostRateCents, 1000); *e.CostCents != want {
			out = append(out, Violation{Query: "Q30", IDs: []string{e.ID}, Detail: fmt.Sprintf("cost %d != %d", *e.CostCents, want)})
		}
	}
	var negative []struct {
		WorkOrderID string `bun:"work_order_id"`
		ItemID      string `bun:"item_id"`
		Net         int64  `bun:"net"`
	}
	if err := db.NewSelect().TableExpr("stock_movements").
		ColumnExpr("work_order_id, item_id, SUM(CASE WHEN kind = 'issue' THEN qty_milli ELSE -qty_milli END) AS net").
		Where("marina_id = ?", marinaID).GroupExpr("work_order_id, item_id").
		Having("SUM(CASE WHEN kind = 'issue' THEN qty_milli ELSE -qty_milli END) < 0").Scan(ctx, &negative); err != nil {
		return nil, fmt.Errorf("Q30: %w", err)
	}
	for _, n := range negative {
		out = append(out, Violation{Query: "Q30", IDs: []string{n.WorkOrderID, n.ItemID}, Detail: fmt.Sprintf("net issued %d", n.Net)})
	}
	var bills []WorkOrderBilling
	if err := db.NewSelect().Model(&bills).Where("marina_id = ?", marinaID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q32: %w", err)
	}
	final := map[string]bool{}
	for _, b := range bills {
		if b.Kind == WOBillingFinal {
			final[b.WorkOrderID] = true
		}
		if b.Status != WOBillingCharged {
			continue
		}
		var c Charge
		if err := db.NewSelect().Model(&c).Where("marina_id = ?", marinaID).Where("id = ?", b.ChargeID).Scan(ctx); err != nil {
			out = append(out, Violation{Query: "Q32", IDs: []string{b.ID}, Detail: "billing charge missing"})
			continue
		}
		if c.OriginType != ChargeOriginWorkOrder || c.OriginID != b.ID || c.TotalCents != b.GrossCents {
			out = append(out, Violation{Query: "Q32", IDs: []string{b.ID, c.ID}, Detail: fmt.Sprintf("charge %s/%s total %d, billing gross %d",
				c.OriginType, c.OriginID, c.TotalCents, b.GrossCents)})
		}
	}
	var invoiced []string
	if err := db.NewSelect().TableExpr("work_orders").Column("id").Where("marina_id = ?", marinaID).
		Where("status = ?", WorkOrderInvoiced).Scan(ctx, &invoiced); err != nil {
		return nil, fmt.Errorf("Q32: %w", err)
	}
	for _, id := range invoiced {
		if !final[id] {
			out = append(out, Violation{Query: "Q32", IDs: []string{id}, Detail: "invoiced order without final billing"})
		}
	}
	var running []struct {
		EmployeeID string `bun:"employee_id"`
		N          int    `bun:"n"`
	}
	if err := db.NewSelect().TableExpr("timesheets").ColumnExpr("employee_id, COUNT(*) AS n").Where("marina_id = ?", marinaID).
		Where("status = ?", TimesheetRunning).GroupExpr("employee_id").Having("COUNT(*) > 1").Scan(ctx, &running); err != nil {
		return nil, fmt.Errorf("Q33: %w", err)
	}
	for _, r := range running {
		out = append(out, Violation{Query: "Q33", IDs: []string{r.EmployeeID}, Detail: fmt.Sprintf("%d running timers", r.N)})
	}
	return out, nil
}

// halfEvenDiv is num / den rounded half to even (den > 0).
func halfEvenDiv(num, den int64) int64 {
	if num < 0 {
		return -halfEvenDiv(-num, den)
	}
	q, r := num/den, num%den
	if 2*r > den || (2*r == den && q%2 != 0) {
		q++
	}
	return q
}
