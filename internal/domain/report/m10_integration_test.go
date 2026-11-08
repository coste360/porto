//go:build integration

package report_test

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/domain/workorder/workordertest"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestReport_Yard: the yard views on a worked order (§9.4 hours and margins), the yard tiles equal to the totals of
// their drill views (INV-62), and the permissions (reports.yard or reports.all; another marina 404).
func TestReport_Yard(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := workordertest.New(t, db)
		ctx := fixture.Ctx()
		ref := e.Accept(t, e.AcceptedQuote(t, e.AntifoulingLines(), store.BillingModeActual))
		h := qty.Milli(5000)
		en, err := e.WO.CreateEntry(ctx, e.Owner, e.MarinaID, workorder.EntryInput{ClientRef: "r1", EmployeeID: e.Luca, WorkOrderID: ref.WorkOrderID, Hours: &h})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.WO.Approve(ctx, e.Owner, e.MarinaID, []workorder.EntryVersion{{ID: en.Row.ID, Version: int64(en.Row.Version)}}); err != nil {
			t.Fatal(err)
		}
		svc := report.NewService(report.Deps{DB: db, Clock: e.Clock, Log: e.Log})
		day := en.Row.WorkDate
		q := report.Params{From: day.AddDays(-30), To: day}
		hours, err := svc.Yard(ctx, e.YardMgr, e.MarinaID, q)
		if err != nil || hours.Totals.HoursMilli != 5000 || hours.Totals.Cost.Value != 11000 || len(hours.Records) != 1 {
			t.Fatalf("hours %+v %v", hours, err)
		}
		q.View = calc.YardViewMargins
		m, err := svc.Yard(ctx, e.Owner, e.MarinaID, q)
		if err != nil || len(m.Records) != 1 || m.Records[0].QuoteNet.Value != 170260 || m.Totals.Cost.Value != 11000+5610 || m.Totals.Revenue.Value != 84640 {
			t.Fatalf("margins %+v %v", m, err)
		}
		q.View = calc.YardViewLoad
		ld, err := svc.Yard(ctx, e.Owner, e.MarinaID, q)
		if err != nil || ld.Totals.HoursMilli != 5000 || !ld.Totals.Capacity.OK {
			t.Fatalf("load %+v %v", ld.Totals, err)
		}
		q.View, q.GroupBy = calc.YardViewMarginsWT, "work_type"
		if _, err := svc.Yard(ctx, e.Owner, e.MarinaID, q); err != nil {
			t.Fatal(err)
		}
		q.View, q.GroupBy = calc.YardViewLifting, ""
		if _, err := svc.Yard(ctx, e.Owner, e.MarinaID, q); err != nil {
			t.Fatal(err)
		}
		// Permissions: office has reports.operational only.
		if _, err := svc.Yard(ctx, e.Office, e.MarinaID, q); err == nil {
			t.Fatal("office reads the yard report")
		}
		_, _, other := e.OtherOrganization(t)
		if _, err := svc.Yard(ctx, other, e.MarinaID, q); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
		// Tiles = Σ drill.
		d, err := svc.Dashboard(ctx, e.Owner, e.MarinaID, report.DashboardParams{From: day.AddDays(-30), To: day, Compare: "none"})
		if err != nil {
			t.Fatal(err)
		}
		tiles := map[calc.TileKey]calc.Tile{}
		for _, tl := range d.Tiles {
			tiles[tl.Key] = tl
		}
		if v := tiles[calc.TileYardHours].Current.Value.Value; !tiles[calc.TileYardHours].Available || v != hours.Totals.HoursMilli {
			t.Fatalf("yard_hours %d", v)
		}
		if v := tiles[calc.TileYardOpenOrders].Current.Value.Value; v != 1 {
			t.Fatalf("yard_open_orders %d", v)
		}
		if v := tiles[calc.TileYardMargin].Current.Value.Value; v != 0 {
			t.Fatalf("yard_margin %d", v)
		}
		od, err := svc.Dashboard(ctx, e.Office, e.MarinaID, report.DashboardParams{From: day.AddDays(-30), To: day, Compare: "none"})
		if err != nil {
			t.Fatal(err)
		}
		for _, tl := range od.Tiles {
			if tl.Key == calc.TileYardHours {
				t.Fatal("office sees the yard tiles")
			}
		}
	})
}
