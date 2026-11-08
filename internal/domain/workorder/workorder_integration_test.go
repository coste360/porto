//go:build integration

package workorder_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/domain/workorder/account"
	"github.com/coste360/porto/internal/domain/workorder/workordertest"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func ctxT() context.Context { return fixture.Ctx() }

func hours(h int64) *qty.Milli { v := qty.Milli(h); return &v }

func invariants(t *testing.T, e *workordertest.Env) {
	t.Helper()
	vs, err := store.CheckYardInvariants(ctxT(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func tasksByTitle(t *testing.T, e *workordertest.Env, orderID string) map[string]workorder.TaskView {
	t.Helper()
	ts, err := e.WO.ListTasks(ctxT(), e.Owner, e.MarinaID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]workorder.TaskView{}
	for _, x := range ts {
		out[x.Row.Title] = x
	}
	return out
}

func liveGross(t *testing.T, e *workordertest.Env, orderID string) money.Cents {
	t.Helper()
	cs, err := store.LiveChargesOfWorkOrder(ctxT(), e.DB.DB, e.Scope, orderID)
	if err != nil {
		t.Fatal(err)
	}
	var g money.Cents
	for _, c := range cs {
		g += money.Cents(c.TotalCents)
	}
	return g
}

// TestWorkOrder_FinalAccountWorked: the order of m10-plan §4.4 from its accepted quote, the hours, materials and
// external cost of §9.4, the final account exactly as the table, the advance and final billings (Σ 2,319.10), then a
// late entry billed as a conguaglio (INV-93, INV-94).
func TestWorkOrder_FinalAccountWorked(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeActual))
		if ref.Number == "" || ref.AdvanceChargeID == "" {
			t.Fatalf("ref %+v", ref)
		}
		if g := liveGross(t, e, ref.WorkOrderID); g != 62315 {
			t.Fatalf("advance %d", g)
		}
		o, err := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if err != nil || o.Row.Status != store.WorkOrderPlanned || len(o.Lines) != 6 || o.HoursEstimated != 14000 {
			t.Fatalf("order %+v %v", o, err)
		}
		tasks := tasksByTitle(t, e, ref.WorkOrderID)
		hand, paint := tasks["Carteggiatura"], tasks["Antivegetativa"]
		if hand.Row.EstimatedHoursMilli != 8400 || paint.Row.EstimatedHoursMilli != 5600 || hand.Row.QualificationID != e.QualHand {
			t.Fatalf("tasks %+v", tasks)
		}
		// Hours: Luca 5.0, Marco 4.0 (his own entry from the phone, replayed offline twice), Giulia 5.5.
		mk := func(p *auth.Principal, emp, task, ref string, h int64) *workorder.EntryView {
			t.Helper()
			v, err := e.WO.CreateEntry(ctx, p, e.MarinaID, workorder.EntryInput{ClientRef: ref, EmployeeID: emp, TaskID: task, Hours: hours(h)})
			if err != nil {
				t.Fatalf("entry %s: %v", ref, err)
			}
			return v
		}
		luca := mk(e.Owner, e.Luca, hand.Row.ID, "e-luca", 5000)
		// The yard user records only on assigned tasks: assign Marco first.
		e.Insert(t, &store.TaskAssignment{
			ID: store.NewID(), MarinaID: e.MarinaID, TaskID: hand.Row.ID, EmployeeID: e.Marco, WorkDate: luca.Row.WorkDate, HoursMilli: 4000,
			Version: 1, CreatedAt: e.Clock.Now(), UpdatedAt: e.Clock.Now(),
		})
		qctx := reqctx.With(ctx, &reqctx.Info{OfflineQueuedAt: e.Clock.Now().Add(-time.Hour)})
		marco := mk(e.Yard, "", hand.Row.ID, "e-marco", 4000)
		again, err := e.WO.CreateEntry(qctx, e.Yard, e.MarinaID, workorder.EntryInput{ClientRef: "e-marco", TaskID: hand.Row.ID, Hours: hours(4000)})
		if err != nil || again.Row.ID != marco.Row.ID {
			t.Fatalf("offline replay %v %+v", err, again)
		}
		if _, err := e.WO.CreateEntry(ctx, e.Yard, e.MarinaID, workorder.EntryInput{ClientRef: "e-x", TaskID: paint.Row.ID, Hours: hours(1000)}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("unassigned task: %v", err)
		}
		if _, err := e.WO.CreateEntry(ctx, e.Yard, e.MarinaID, workorder.EntryInput{ClientRef: "e-y", EmployeeID: e.Luca, TaskID: hand.Row.ID, Hours: hours(1000)}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("other employee: %v", err)
		}
		if _, err := e.WO.CreateEntry(ctx, e.Owner, e.MarinaID, workorder.EntryInput{ClientRef: "e-z", EmployeeID: e.Luca, TaskID: hand.Row.ID, Hours: hours(16250)}); !errors.Is(err, workorder.ErrDurationTooLong) {
			t.Fatalf("too long: %v", err)
		}
		giulia := mk(e.Owner, e.Giulia, paint.Row.ID, "e-giulia", 5500)
		approved, err := e.WO.Approve(ctx, e.Owner, e.MarinaID, []workorder.EntryVersion{
			{ID: luca.Row.ID, Version: int64(luca.Row.Version)},
			{ID: marco.Row.ID, Version: int64(marco.Row.Version)},
			{ID: giulia.Row.ID, Version: int64(giulia.Row.Version)},
		})
		if err != nil || len(approved) != 3 {
			t.Fatalf("approve %v", err)
		}
		costs := map[string]int64{}
		for _, a := range approved {
			costs[a.Row.EmployeeID] = *a.Row.CostCents
		}
		if costs[e.Luca] != 11000 || costs[e.Marco] != 9600 || costs[e.Giulia] != 14300 {
			t.Fatalf("frozen costs %+v", costs)
		}
		if o, _ = e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID); o.Row.Status != store.WorkOrderInProgress {
			t.Fatalf("implicit start: %s", o.Row.Status)
		}
		// Materials: 6 tins issued, 1 returned; a return above the issued quantity is refused.
		for _, m := range []workorder.MovementInput{
			{WorkOrderID: ref.WorkOrderID, ItemID: e.Items["MAT-AVEG"], Kind: store.StockIssue, Qty: 6000},
			{WorkOrderID: ref.WorkOrderID, ItemID: e.Items["MAT-AVEG"], Kind: store.StockReturn, Qty: 1000},
		} {
			mv, err := e.WO.CreateMovement(ctx, e.Owner, e.MarinaID, m)
			if err != nil || mv.Row.UnitPriceCents != 9500 || mv.Row.UnitCostCents != 5800 {
				t.Fatalf("movement %+v %v", mv, err)
			}
		}
		if _, err := e.WO.CreateMovement(ctx, e.Owner, e.MarinaID, workorder.MovementInput{
			WorkOrderID: ref.WorkOrderID, ItemID: e.Items["MAT-AVEG"], Kind: store.StockReturn, Qty: 5001,
		}); !errors.Is(err, workorder.ErrReturnExceedsIssued) {
			t.Fatalf("return above issued: %v", err)
		}
		// External cost: propeller polishing, 150.00 at cost, billed 180.00 (markup 20 %).
		x, err := e.WO.CreateExternal(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.ExternalInput{
			SupplierName: "Eliche Srl", Description: "Lucidatura elica", WorkTypeID: e.WTProp, Cost: 15000,
		})
		if err != nil || x.PriceCents != 18000 || !x.Billable || x.ItemID != e.Items["EXT-SRV"] {
			t.Fatalf("external %+v %v", x, err)
		}
		// Final account = §9.4.
		acc, err := e.WO.Account(ctx, e.YardMgr, e.MarinaID, ref.WorkOrderID)
		if err != nil {
			t.Fatal(err)
		}
		tt := acc.Account.Totals
		if tt.Revenue != 190090 || tt.Cost != 84510 || tt.Margin != 105580 || *tt.MarginBP != 5554 || tt.VATCents != 41820 || tt.Gross != 231910 {
			t.Fatalf("totals %+v", tt)
		}
		if acc.Account.DeltaNet != 19830 || acc.ChargedGross != 62315 || acc.RemainingGross != 169595 || tt.HoursLabour != 14500 {
			t.Fatalf("delta %d charged %d remaining %d", acc.Account.DeltaNet, acc.ChargedGross, acc.RemainingGross)
		}
		var anti, prop account.WorkTypeTotals
		for _, w := range acc.Account.ByWorkType {
			switch w.WorkTypeID {
			case e.WTAnti:
				anti = w
			case e.WTProp:
				prop = w
			}
		}
		if anti.Revenue != 172090 || anti.Cost != 69510 || prop.Margin != 3000 {
			t.Fatalf("by work type %+v %+v", anti, prop)
		}
		// Billing: final needs a completed order.
		if _, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, true); !errors.Is(err, workorder.ErrInvalidTransition) {
			t.Fatalf("final before complete: %v", err)
		}
		if _, err := e.WO.CreateBilling(ctx, e.YardMgr, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingProgress}, true); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard_mgr bills: %v", err)
		}
		for _, tk := range []workorder.TaskView{hand, paint} {
			if _, err := e.WO.UpdateProgress(ctx, e.Owner, e.MarinaID, tk.Row.ID, workorder.ProgressInput{ClientRef: "done-" + tk.Row.ID, Done: ptrBool(true)}); err != nil {
				t.Fatal(err)
			}
		}
		o, _ = e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if o.Row.CompletionBP != 10000 {
			t.Fatalf("completion %d", o.Row.CompletionBP)
		}
		o, err = e.WO.Transition(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, int64(o.Row.Version), workorder.ActionComplete, "")
		if err != nil || o.Row.Status != store.WorkOrderCompleted || len(o.Warnings) != 0 {
			t.Fatalf("complete %v %+v", err, o)
		}
		prev, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, true)
		if err != nil || prev.Gross != 169595 || prev.Billing != nil {
			t.Fatalf("preview %+v %v", prev, err)
		}
		final, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, false)
		if err != nil || final.Gross != 169595 || final.ChargeID == "" {
			t.Fatalf("final %+v %v", final, err)
		}
		if g := liveGross(t, e, ref.WorkOrderID); g != 231910 {
			t.Fatalf("Σ charges %d", g)
		}
		if o, _ = e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID); o.Row.Status != store.WorkOrderInvoiced {
			t.Fatalf("status %s", o.Row.Status)
		}
		if _, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, false); !errors.Is(err, workorder.ErrInvalidTransition) {
			t.Fatalf("second final: %v", err)
		}
		if _, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingAdjustment}, false); !errors.Is(err, workorder.ErrNothingToBill) {
			t.Fatalf("empty adjustment: %v", err)
		}
		// Late entry after the final billing: yard.manage only, flagged late, billed as a conguaglio.
		if _, err := e.WO.CreateEntry(ctx, e.Yard, e.MarinaID, workorder.EntryInput{ClientRef: "late-yard", TaskID: hand.Row.ID, Hours: hours(500)}); !errors.Is(err, workorder.ErrClosed) {
			t.Fatalf("late by worker: %v", err)
		}
		late := mk(e.Owner, e.Luca, hand.Row.ID, "late-1", 500)
		if !late.Row.Late {
			t.Fatal("late flag")
		}
		if _, err := e.WO.Approve(ctx, e.Owner, e.MarinaID, []workorder.EntryVersion{{ID: late.Row.ID, Version: int64(late.Row.Version)}}); err != nil {
			t.Fatal(err)
		}
		adj, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingAdjustment}, false)
		if err != nil || adj.Gross != 2318 || adj.CorrectsChargeID != final.ChargeID { // 0.5 h × 38.00 + 22 % VAT
			t.Fatalf("adjustment %+v %v", adj, err)
		}
		bills, charged, err := e.WO.ListBillings(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if err != nil || len(bills) != 3 || charged != 231910+2318 {
			t.Fatalf("billings %d %d %v", len(bills), charged, err)
		}
		if n := len(e.Events(t, event.WorkOrderBilled)); n != 3 {
			t.Fatalf("%d billed events", n)
		}
		invariants(t, e)
	})
}

func ptrBool(b bool) *bool { return &b }

// TestWorkOrder_NegativeFinalNeedsRefunds: actuals below the advance end in a negative adjustment charge, which needs
// refunds.issue (office has charges.manage only).
func TestWorkOrder_NegativeFinalNeedsRefunds(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		lines := []workordertest.Line{{Code: "LAB-HAND", Qty: 20000, Unit: 3800, Qual: e.QualHand, TaskTitle: "Lavoro"}}
		ref := e.Accept(t, e.AcceptedQuote(t, lines, store.BillingModeActual))
		adv := liveGross(t, e, ref.WorkOrderID) // 30 % of 927.20 = 278.16
		if adv != 27816 {
			t.Fatalf("advance %d", adv)
		}
		o, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		o, _ = e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionStart, "")
		o, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionComplete, "")
		if err != nil || len(o.Warnings) != 1 || o.Warnings[0] != workorder.WarnTasksIncomplete {
			t.Fatalf("complete %v %+v", err, o)
		}
		if _, err := e.WO.CreateBilling(ctx, e.Office, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, false); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office negative: %v", err)
		}
		res, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, false)
		if err != nil || res.Gross != -27816 || res.CorrectsChargeID != ref.AdvanceChargeID {
			t.Fatalf("negative final %+v %v", res, err)
		}
		if g := liveGross(t, e, ref.WorkOrderID); g != 0 {
			t.Fatalf("Σ %d", g)
		}
		invariants(t, e)
	})
}

// TestWorkOrder_AdjustmentCorrectsLatestCharge: the charge an adjustment corrects is the latest live charge in
// billing order, whatever the charge numbers sort to (a numbering series with a prefix need not follow creation).
func TestWorkOrder_AdjustmentCorrectsLatestCharge(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		lines := []workordertest.Line{{Code: "LAB-HAND", Qty: 20000, Unit: 3800, Qual: e.QualHand, TaskTitle: "Lavoro"}}
		ref := e.Accept(t, e.AcceptedQuote(t, lines, store.BillingModeActual))
		task := tasksByTitle(t, e, ref.WorkOrderID)["Lavoro"]
		approved := func(clientRef string, h int64) {
			t.Helper()
			v, err := e.WO.CreateEntry(ctx, e.Owner, e.MarinaID, workorder.EntryInput{ClientRef: clientRef, EmployeeID: e.Luca, TaskID: task.Row.ID, Hours: hours(h)})
			if err != nil {
				t.Fatalf("entry %s: %v", clientRef, err)
			}
			if _, err := e.WO.Approve(ctx, e.Owner, e.MarinaID, []workorder.EntryVersion{{ID: v.Row.ID, Version: int64(v.Row.Version)}}); err != nil {
				t.Fatal(err)
			}
		}
		o, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionStart, ""); err != nil {
			t.Fatal(err)
		}
		approved("w-1", 10000)
		o, _ = e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionComplete, ""); err != nil {
			t.Fatal(err)
		}
		final, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingFinal}, false)
		if err != nil || final.ChargeID == "" {
			t.Fatalf("final %+v %v", final, err)
		}
		// The later charge gets a number that sorts before the advance's.
		for id, number := range map[string]string{final.ChargeID: "0-LATER", ref.AdvanceChargeID: "Z-EARLIER"} {
			if _, err := db.NewUpdate().TableExpr("charges").Set("number = ?", number).Where("id = ?", id).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}
		approved("late-1", 500)
		adj, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.BillingInput{Kind: workorder.BillingAdjustment}, false)
		if err != nil || adj.CorrectsChargeID != final.ChargeID {
			t.Fatalf("adjustment %+v %v, want corrects %s", adj, err, final.ChargeID)
		}
		invariants(t, e)
	})
}

// TestWorkOrder_TransitionsAndCancel: the state table through the service; cancel refuses an order with live
// charges and cancels the lifting bookings of the order.
func TestWorkOrder_TransitionsAndCancel(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeFixed))
		o, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionCancel, "cliente"); !errors.Is(err, workorder.ErrHasCharges) {
			t.Fatalf("cancel with charges: %v", err)
		}
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, 99, workorder.ActionStart, ""); !errors.Is(err, workorder.ErrVersionMismatch) {
			t.Fatalf("version: %v", err)
		}
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionComplete, ""); !errors.Is(err, workorder.ErrInvalidTransition) {
			t.Fatalf("planned complete: %v", err)
		}
		if _, err := e.WO.Transition(ctx, e.Yard, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionStart, ""); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("worker transition: %v", err)
		}
		steps := []string{
			workorder.ActionStart, workorder.ActionHold, workorder.ActionResume, workorder.ActionComplete, workorder.ActionReopen,
			workorder.ActionComplete, workorder.ActionDeliver,
		}
		for _, a := range steps {
			var err error
			if o, err = e.WO.Transition(ctx, e.YardMgr, e.MarinaID, o.Row.ID, int64(o.Row.Version), a, "motivo"); err != nil {
				t.Fatalf("%s: %v", a, err)
			}
		}
		if o.Row.Status != store.WorkOrderDelivered || o.Row.DeliveredAt.IsZero() {
			t.Fatalf("delivered %+v", o.Row)
		}
		// Fixed mode: the final bills the budget plus the billable external costs (none): 2,077.17 − 623.15.
		res, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, o.Row.ID, workorder.BillingInput{Kind: workorder.BillingFinal}, false)
		if err != nil || res.Gross != 207717-62315 {
			t.Fatalf("fixed final %+v %v", res, err)
		}
		if n := len(e.Events(t, event.WorkOrderStatusChanged)); n != 1+len(steps)+1 {
			t.Fatalf("%d status events", n)
		}
		// Cancel an order without charges (no deposit): bookings cancelled in the same transaction.
		q := e.AcceptedQuote(t, []workordertest.Line{{Code: "SRV-ALAG", Qty: 12000, Unit: 1800}}, store.BillingModeActual)
		q.Quote.DepositAmountCents = 0
		ref2 := e.Accept(t, q)
		o2, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref2.WorkOrderID)
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o2.Row.ID, int64(o2.Row.Version), workorder.ActionCancel, ""); err == nil {
			t.Fatal("cancel without reason")
		}
		o2, err = e.WO.Transition(ctx, e.Owner, e.MarinaID, o2.Row.ID, int64(o2.Row.Version), workorder.ActionCancel, "rinuncia")
		if err != nil || o2.Row.Status != store.WorkOrderCancelled || len(e.Bookings.Cancelled) != 1 {
			t.Fatalf("cancel %v %+v %v", err, o2, e.Bookings.Cancelled)
		}
		if _, err := e.WO.CreateTask(ctx, e.Owner, e.MarinaID, o2.Row.ID, workorder.TaskInput{Title: "x"}); !errors.Is(err, workorder.ErrClosed) {
			t.Fatalf("task on cancelled: %v", err)
		}
		// Another marina's principal: 404.
		_, _, other := e.OtherOrganization(t)
		if _, err := e.WO.GetOrder(ctx, other, e.MarinaID, o.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
		invariants(t, e)
	})
}

// TestWorkOrder_TasksAndDependencies: tasks, checklist, dependencies (acyclic, same order), progress replays never
// decrease, completion weighting, delete refused with entries.
func TestWorkOrder_TasksAndDependencies(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeActual))
		tasks := tasksByTitle(t, e, ref.WorkOrderID)
		hand, paint := tasks["Carteggiatura"], tasks["Antivegetativa"]
		extra, err := e.WO.CreateTask(ctx, e.Owner, e.MarinaID, ref.WorkOrderID, workorder.TaskInput{
			Title: "Controllo", EstimatedHours: 0, Checklist: []string{"Elica", "Anodi"}, DependsOn: []string{paint.Row.ID},
		})
		if err != nil || len(extra.DependsOn) != 1 || len(workorder.ChecklistOf(extra.Row.Checklist)) != 2 {
			t.Fatalf("task %+v %v", extra, err)
		}
		pv, err := e.WO.SetDependencies(ctx, e.Owner, e.MarinaID, paint.Row.ID, int64(paint.Row.Version), []string{hand.Row.ID})
		if err != nil {
			t.Fatal(err)
		}
		hv, _ := e.WO.ListTasks(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		_ = hv
		if _, err := e.WO.SetDependencies(ctx, e.Owner, e.MarinaID, hand.Row.ID, int64(hand.Row.Version), []string{extra.Row.ID}); !errors.Is(err, workorder.ErrDependencyViolation) {
			t.Fatalf("cycle: %v", err)
		}
		if _, err := e.WO.SetDependencies(ctx, e.Owner, e.MarinaID, hand.Row.ID, int64(hand.Row.Version), []string{hand.Row.ID}); !errors.Is(err, workorder.ErrDependencyViolation) {
			t.Fatalf("self: %v", err)
		}
		// Progress: hand 50 % → completion 8400×5000/14000 = 3000; a stale replay (30 %) never decreases it.
		qctx := reqctx.With(ctx, &reqctx.Info{OfflineQueuedAt: e.Clock.Now()})
		if _, err := e.WO.UpdateProgress(ctx, e.Owner, e.MarinaID, hand.Row.ID, workorder.ProgressInput{ClientRef: "p1", ProgressBP: ptrI(5000)}); err != nil {
			t.Fatal(err)
		}
		v, err := e.WO.UpdateProgress(qctx, e.Owner, e.MarinaID, hand.Row.ID, workorder.ProgressInput{ClientRef: "p0", ProgressBP: ptrI(3000)})
		if err != nil || v.Row.ProgressBP != 5000 || v.Row.Status != store.WorkTaskInProgress {
			t.Fatalf("replay %v %+v", err, v.Row)
		}
		old := e.Clock.Now().Add(-49 * time.Hour)
		if _, err := e.WO.UpdateProgress(qctx, e.Owner, e.MarinaID, hand.Row.ID, workorder.ProgressInput{ClientRef: "p2", ProgressBP: ptrI(6000), RecordedAt: &old}); !errors.Is(err, workorder.ErrOfflineTimeOutOfRange) {
			t.Fatalf("offline window: %v", err)
		}
		o, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if o.Row.CompletionBP != 3000 {
			t.Fatalf("completion %d", o.Row.CompletionBP)
		}
		// Checklist mark and done.
		cv, err := e.WO.UpdateProgress(ctx, e.Owner, e.MarinaID, extra.Row.ID, workorder.ProgressInput{
			ClientRef: "c1", Checklist: []workorder.ChecklistMark{{Key: "c1", Done: true}}, Done: ptrBool(true),
		})
		if err != nil || cv.Row.Status != store.WorkTaskDone || !workorder.ChecklistOf(cv.Row.Checklist)[0].Done {
			t.Fatalf("done %v %+v", err, cv.Row)
		}
		// Delete refused with entries; allowed without.
		en, err := e.WO.CreateEntry(ctx, e.Owner, e.MarinaID, workorder.EntryInput{ClientRef: "x1", EmployeeID: e.Giulia, TaskID: pv.Row.ID, Hours: hours(250)})
		if err != nil {
			t.Fatal(err)
		}
		_ = en
		pv, _ = e.WO.UpdateTask(ctx, e.Owner, e.MarinaID, pv.Row.ID, int64(pv.Row.Version), workorder.TaskPatch{})
		if err := e.WO.DeleteTask(ctx, e.Owner, e.MarinaID, pv.Row.ID, int64(pv.Row.Version)); !errors.Is(err, workorder.ErrHasEntries) {
			t.Fatalf("delete with entries: %v", err)
		}
		cur, _ := e.WO.ListTasks(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		for _, x := range cur {
			if x.Row.ID == extra.Row.ID {
				if err := e.WO.DeleteTask(ctx, e.Owner, e.MarinaID, x.Row.ID, int64(x.Row.Version)); err != nil {
					t.Fatalf("delete: %v", err)
				}
			}
		}
		// The yard worker sees only assigned tasks.
		if ts, err := e.WO.ListTasks(ctx, e.Yard, e.MarinaID, ref.WorkOrderID); err != nil || len(ts) != 0 {
			t.Fatalf("worker tasks %d %v", len(ts), err)
		}
	})
}

func ptrI(v int64) *int64 { return &v }

// TestWorkOrder_Timers: start, replay, one running timer (409), stop rounded to 0.01 h, stop replay, void and
// reject paths, my work.
func TestWorkOrder_Timers(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeActual))
		hand := tasksByTitle(t, e, ref.WorkOrderID)["Carteggiatura"]
		e.Insert(t, &store.TaskAssignment{
			ID: store.NewID(), MarinaID: e.MarinaID, TaskID: hand.Row.ID, EmployeeID: e.Marco, WorkDate: calendar.In(e.Clock.Now(), time.UTC), HoursMilli: 4000,
			Version: 1, CreatedAt: e.Clock.Now(), UpdatedAt: e.Clock.Now(),
		})
		start := e.Clock.Now()
		tm, err := e.WO.StartTimer(ctx, e.Yard, e.MarinaID, workorder.TimerStart{ClientRef: "t1", TaskID: hand.Row.ID, StartedAt: &start})
		if err != nil || tm.Row.Status != store.TimesheetRunning {
			t.Fatalf("start %v %+v", err, tm)
		}
		if again, err := e.WO.StartTimer(ctx, e.Yard, e.MarinaID, workorder.TimerStart{ClientRef: "t1", TaskID: hand.Row.ID}); err != nil || again.Row.ID != tm.Row.ID {
			t.Fatalf("replay %v", err)
		}
		if _, err := e.WO.StartTimer(ctx, e.Yard, e.MarinaID, workorder.TimerStart{ClientRef: "t2", TaskID: hand.Row.ID}); !errors.Is(err, workorder.ErrTimerAlreadyRunning) {
			t.Fatalf("second timer: %v", err)
		}
		mw, err := e.WO.MyWork(ctx, e.Yard, e.MarinaID)
		if err != nil || mw.RunningTimer == nil || mw.EmployeeID != e.Marco {
			t.Fatalf("my work %+v %v", mw, err)
		}
		end := start.Add(2*time.Hour + 20*time.Minute)
		e.Clock.Set(end.Add(time.Minute))
		st, err := e.WO.StopTimer(ctx, e.Yard, e.MarinaID, tm.Row.ID, workorder.TimerStop{ClientRef: "s1", EndedAt: &end})
		if err != nil || st.Row.HoursMilli != 2330 || st.Row.Status != store.TimesheetSubmitted {
			t.Fatalf("stop %v %+v", err, st)
		}
		if again, err := e.WO.StopTimer(ctx, e.Yard, e.MarinaID, tm.Row.ID, workorder.TimerStop{ClientRef: "s1", EndedAt: &end}); err != nil || again.Row.Version != st.Row.Version {
			t.Fatalf("stop replay %v", err)
		}
		long := e.Clock.Now()
		t3, err := e.WO.StartTimer(ctx, e.Owner, e.MarinaID, workorder.TimerStart{ClientRef: "t3", EmployeeID: e.Luca, WorkOrderID: ref.WorkOrderID, StartedAt: &long})
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(long.Add(17 * time.Hour))
		if _, err := e.WO.StopTimer(ctx, e.Owner, e.MarinaID, t3.Row.ID, workorder.TimerStop{}); !errors.Is(err, workorder.ErrDurationTooLong) {
			t.Fatalf("long: %v", err)
		}
		if err := e.WO.RunTimersGuard(ctx); err != nil {
			t.Fatal(err)
		}
		rj, err := e.WO.Reject(ctx, e.Owner, e.MarinaID, st.Row.ID, int64(st.Row.Version), "ore sbagliate")
		if err != nil || rj.Row.Status != store.TimesheetRejected {
			t.Fatalf("reject %v", err)
		}
		up, err := e.WO.UpdateEntry(ctx, e.Yard, e.MarinaID, st.Row.ID, int64(rj.Row.Version), workorder.EntryPatch{Hours: hours(2000)})
		if err != nil || up.Row.Status != store.TimesheetSubmitted || up.Row.HoursMilli != 2000 {
			t.Fatalf("resubmit %v %+v", err, up)
		}
		ap, err := e.WO.Approve(ctx, e.Owner, e.MarinaID, []workorder.EntryVersion{{ID: up.Row.ID, Version: int64(up.Row.Version)}})
		if err != nil || *ap[0].Row.CostCents != 4800 {
			t.Fatalf("approve %v", err)
		}
		vd, err := e.WO.Void(ctx, e.Owner, e.MarinaID, up.Row.ID, int64(ap[0].Row.Version), "doppia")
		if err != nil || vd.Row.Status != store.TimesheetVoided {
			t.Fatalf("void %v", err)
		}
		own, _, err := e.WO.ListEntries(ctx, e.Yard, e.MarinaID, workorder.EntryFilter{})
		if err != nil || len(own) != 1 {
			t.Fatalf("own entries %d %v", len(own), err)
		}
		invariants(t, e)
	})
}

// TestConcurrency_TimerStart: 100 starts for one employee: exactly one running timer (INV-95, DB unique).
func TestConcurrency_TimerStart(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeActual))
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, conflicts := 0, 0
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.WO.StartTimer(ctx, e.Owner, e.MarinaID, workorder.TimerStart{ClientRef: "c" + strconv.Itoa(i), EmployeeID: e.Luca, WorkOrderID: ref.WorkOrderID})
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, workorder.ErrTimerAlreadyRunning) || store.IsSerializationFailure(err):
					conflicts++
				default:
					t.Errorf("start: %v", err)
				}
			}()
		}
		wg.Wait()
		var n int
		if err := db.NewSelect().TableExpr("timesheets").ColumnExpr("COUNT(*)").Where("employee_id = ?", e.Luca).Where("status = 'running'").Scan(ctx, &n); err != nil {
			t.Fatal(err)
		}
		if ok != 1 || n != 1 || conflicts != 99 {
			t.Fatalf("ok %d conflicts %d running %d", ok, conflicts, n)
		}
		invariants(t, e)
	})
}

// TestConcurrency_FinalBillingOnce: 100 final and progress billings of one completed order: one final, Σ charged
// = target gross (INV-94).
func TestConcurrency_FinalBillingOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeFixed))
		o, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		o, _ = e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionStart, "")
		if _, err := e.WO.Transition(ctx, e.Owner, e.MarinaID, o.Row.ID, int64(o.Row.Version), workorder.ActionComplete, ""); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		finals := 0
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				in := workorder.BillingInput{Kind: workorder.BillingFinal}
				if i%3 == 0 {
					a := money.Cents(1000)
					in = workorder.BillingInput{Kind: workorder.BillingProgress, Amount: &a}
				}
				res, err := e.WO.CreateBilling(ctx, e.Owner, e.MarinaID, o.Row.ID, in, false)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil && in.Kind == workorder.BillingFinal:
					finals++
					_ = res
				case err == nil, errors.Is(err, workorder.ErrInvalidTransition), errors.Is(err, workorder.ErrNothingToBill), store.IsSerializationFailure(err):
				default:
					t.Errorf("billing: %v", err)
				}
			}()
		}
		wg.Wait()
		if finals != 1 {
			t.Fatalf("%d finals", finals)
		}
		if g := liveGross(t, e, o.Row.ID); g != 207717 {
			t.Fatalf("Σ %d", g)
		}
		invariants(t, e)
	})
}

// TestWorkOrder_Variation: an accepted variation quote adds its lines (origin variation) and tasks to the order; a
// second acceptance of the same quote is refused (INV-96); a closed order refuses variations.
func TestWorkOrder_Variation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := ctxT()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeActual))
		v := e.AcceptedQuote(t, []workordertest.Line{{Code: "LAB-HAND", Qty: 2000, Unit: 3800, Qual: e.QualHand, TaskTitle: "Riparazione chiglia"}}, store.BillingModeActual)
		v.Quote.Kind, v.Quote.WorkOrderID = store.YardQuoteVariation, ref.WorkOrderID
		if _, err := db.NewUpdate().Model(&v.Quote).Column("kind", "work_order_id").WherePK().Exec(ctx); err != nil {
			t.Fatal(err)
		}
		apply := func() error {
			return db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				_, err := e.WO.ApplyVariationTx(ctx, tx, e.Scope, e.Owner.Actor(), v)
				return err
			})
		}
		if err := apply(); err != nil {
			t.Fatal(err)
		}
		o, _ := e.WO.GetOrder(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if len(o.Lines) != 7 || o.Lines[6].Origin != store.WorkLineOriginVariation || o.Lines[6].Seq != 7 {
			t.Fatalf("lines %+v", o.Lines)
		}
		if _, ok := tasksByTitle(t, e, ref.WorkOrderID)["Riparazione chiglia"]; !ok || o.HoursEstimated != 16000 {
			t.Fatalf("variation task missing, estimated %d", o.HoursEstimated)
		}
		if err := apply(); !errors.Is(err, workorder.ErrInvalidTransition) {
			t.Fatalf("second variation: %v", err)
		}
		acc, _ := e.WO.Account(ctx, e.Owner, e.MarinaID, ref.WorkOrderID)
		if acc.Account.QuoteNet != 170260+7600 {
			t.Fatalf("quote net with variation %d", acc.Account.QuoteNet)
		}
	})
}
