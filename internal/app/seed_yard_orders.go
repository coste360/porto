package app

import (
	"context"
	"fmt"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// seedYardOrders adds the M10 demo of ORDERS (m10-plan §16.2): the work order of the accepted demo quote (draft
// quote of the catalogue seed accepted from the panel, with its advance charge) with approved hours, an entry
// waiting for approval, materials issued and returned, an external cost and progress on a task; and a small order
// without a quote, completed and billed (final charge), for the margin tile. Everything goes through the services
// as the demo owner; a no-op when the marina already has work orders or no yard catalogue.
func (a *App) seedYardOrders(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.WorkOrders == nil || a.YardQuotes == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("work_orders").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	var quotes []store.YardQuote
	if err := a.DB.NewSelect().Model(&quotes).Where("marina_id = ?", scope.ID()).Where("kind = ?", store.YardQuoteJob).
		Where("status = ?", store.YardQuoteDraft).OrderExpr("year, number").Limit(1).Scan(ctx); err != nil || len(quotes) == 0 {
		return err
	}
	q := quotes[0]
	acc, err := a.YardQuotes.Accept(ctx, p, m, q.ID, q.Version)
	if err != nil {
		return fmt.Errorf("yard quote accept: %w", err)
	}
	orderID := acc.Order.WorkOrderID
	if orderID == "" {
		return nil
	}
	var emps []store.Employee
	if err := a.DB.NewSelect().Model(&emps).Where("marina_id = ?", scope.ID()).OrderExpr("code").Scan(ctx); err != nil {
		return err
	}
	tasks, err := a.WorkOrders.ListTasks(ctx, p, m, orderID)
	if err != nil {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), loc)
	h := func(v int64) *qty.Milli { x := qty.Milli(v); return &x }
	// Hours on the tasks: approved for the first two employees, one entry left to approve.
	var toApprove []workorder.EntryVersion
	for i, t := range tasks {
		if len(emps) == 0 {
			break
		}
		e := emps[i%len(emps)]
		v, err := a.WorkOrders.CreateEntry(ctx, p, m, workorder.EntryInput{
			ClientRef: "seed-" + t.Row.ID, EmployeeID: e.ID, TaskID: t.Row.ID, WorkDate: today.AddDays(-1), Hours: h(4000),
		})
		if err != nil {
			return fmt.Errorf("seed entry: %w", err)
		}
		toApprove = append(toApprove, workorder.EntryVersion{ID: v.Row.ID, Version: int64(v.Row.Version)})
		if len(emps) > 1 {
			if _, err := a.WorkOrders.CreateEntry(ctx, p, m, workorder.EntryInput{
				ClientRef: "seed-pending-" + t.Row.ID, EmployeeID: emps[(i+1)%len(emps)].ID, TaskID: t.Row.ID, WorkDate: today, Hours: h(2000),
			}); err != nil {
				return fmt.Errorf("seed entry: %w", err)
			}
		}
		if _, err := a.WorkOrders.UpdateProgress(ctx, p, m, t.Row.ID, workorder.ProgressInput{ClientRef: "seed-p-" + t.Row.ID, ProgressBP: ptrI64(5000)}); err != nil {
			return fmt.Errorf("seed progress: %w", err)
		}
	}
	if len(toApprove) > 0 {
		if _, err := a.WorkOrders.Approve(ctx, p, m, toApprove); err != nil {
			return fmt.Errorf("seed approve: %w", err)
		}
	}
	// Materials of the budget (issue one more than budgeted, return one) and an external cost.
	o, err := a.WorkOrders.GetOrder(ctx, p, m, orderID)
	if err != nil {
		return err
	}
	for _, l := range o.Lines {
		it := o.Items[l.ItemID]
		if it.ItemType != "material" && it.ItemType != "product" {
			continue
		}
		if _, err := a.WorkOrders.CreateMovement(ctx, p, m, workorder.MovementInput{
			WorkOrderID: orderID, ItemID: l.ItemID, Kind: store.StockIssue, Qty: qty.Milli(l.QtyMilli + 1000),
		}); err != nil {
			return fmt.Errorf("seed issue: %w", err)
		}
		if _, err := a.WorkOrders.CreateMovement(ctx, p, m, workorder.MovementInput{
			WorkOrderID: orderID, ItemID: l.ItemID, Kind: store.StockReturn, Qty: 1000,
		}); err != nil {
			return fmt.Errorf("seed return: %w", err)
		}
	}
	// The external cost uses the item of the yard settings, else the first service of the budget.
	extItem := ""
	if set, ok, err := store.WorkOrderYardSettings(ctx, a.DB.DB, scope); err != nil {
		return err
	} else if ok {
		extItem = set.ExternalItemID
	}
	for _, l := range o.Lines {
		if extItem == "" && o.Items[l.ItemID].ItemType == "service" {
			extItem = l.ItemID
		}
	}
	if extItem != "" {
		if _, err := a.WorkOrders.CreateExternal(ctx, p, m, orderID, workorder.ExternalInput{
			SupplierName: "Eliche Tirreno Srl", Reference: "FT 118/" + fmt.Sprint(today.Year()), Description: "Lucidatura elica",
			WorkTypeID: o.Row.WorkTypeID, Cost: 15000, ItemID: extItem,
		}); err != nil {
			return fmt.Errorf("seed external: %w", err)
		}
	}
	return a.seedYardOrderBilled(ctx, p, m, q, o, today)
}

// seedYardOrderBilled is a small order without a quote on the same vessel: two service lines, completed and billed
// (final charge), so that the margin of the invoiced orders is not empty.
func (a *App) seedYardOrderBilled(ctx context.Context, p *auth.Principal, m string, q store.YardQuote, o *workorder.OrderView, today calendar.Date) error {
	var lines []workorder.ManualLine
	for _, l := range o.Lines {
		it := o.Items[l.ItemID]
		if it.ItemType == "service" && len(lines) < 2 {
			lines = append(lines, workorder.ManualLine{ItemID: l.ItemID, WorkTypeID: l.WorkTypeID, Qty: qty.Milli(l.QtyMilli), Unit: money.Cents(l.UnitCents)})
		}
	}
	if len(lines) == 0 {
		return nil
	}
	v, err := a.WorkOrders.CreateOrder(ctx, p, m, workorder.OrderInput{
		CustomerID: q.CustomerID, VesselID: q.VesselID, WorkTypeID: o.Row.WorkTypeID, BillingMode: store.BillingModeActual,
		PlannedStartOn: today.AddDays(-10), PlannedDeliveryOn: today.AddDays(-2), ManualLines: lines, Notes: "Alaggio e varo per ispezione",
	})
	if err != nil {
		return fmt.Errorf("seed order: %w", err)
	}
	for _, action := range []string{workorder.ActionStart, workorder.ActionComplete} {
		if v, err = a.WorkOrders.Transition(ctx, p, m, v.Row.ID, int64(v.Row.Version), action, ""); err != nil {
			return fmt.Errorf("seed %s: %w", action, err)
		}
	}
	if _, err := a.WorkOrders.CreateBilling(ctx, p, m, v.Row.ID, workorder.BillingInput{Kind: workorder.BillingFinal, DueOn: today.AddDays(30)}, false); err != nil {
		return fmt.Errorf("seed final billing: %w", err)
	}
	return nil
}

func ptrI64(v int64) *int64 { return &v }
