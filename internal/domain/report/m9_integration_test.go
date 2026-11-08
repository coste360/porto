//go:build integration

package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// shedOf returns the surface area of the demo shed (map kind shed) and a vessel of the demo.
func shedOf(t *testing.T, d *demo) (area store.DynamicArea, vesselID string) {
	t.Helper()
	ctx := context.Background()
	err := d.db.NewSelect().Model(&area).ModelTableExpr("dynamic_areas AS dynamic_area").ColumnExpr("dynamic_area.*").
		Join("JOIN maps AS m ON m.id = dynamic_area.map_id").
		Where("dynamic_area.marina_id = ?", d.marina).Where("m.kind = 'shed'").Where("dynamic_area.kind = 'surface'").
		Limit(1).Scan(ctx)
	must(t, err)
	must(t, d.db.NewSelect().TableExpr("vessels").Column("id").Where("marina_id = ?", d.marina).OrderExpr("id").Limit(1).Scan(ctx, &vesselID))
	return area, vesselID
}

// insertAreaAllocation writes an allocation of kind on the area with its cell rows (raw, as a fixture).
func insertAreaAllocation(t *testing.T, d *demo, area store.DynamicArea, kind, vesselID string, cells []int, first, last calendar.Date) string {
	t.Helper()
	ctx := context.Background()
	id := store.NewID()
	now := demoNow
	var vessel any = vesselID
	var reason any
	if kind == "block" {
		vessel, reason = nil, "test"
	}
	_, err := d.db.NewRaw(`INSERT INTO allocations (id, marina_id, vessel_id, resource_type, resource_id, map_id, x_cm, y_cm,
		rotation_dd, first_night, last_night, kind, status, block_reason, version, created_at, updated_at)
		VALUES (?, ?, ?, 'dynamic_area', ?, ?, 0, 0, 0, ?, ?, ?, 'active', ?, 1, ?, ?)`,
		id, d.marina, vessel, area.ID, area.MapID, first, last, kind, reason, now, now).Exec(ctx)
	must(t, err)
	for n := first; !n.After(last); n = n.AddDays(1) {
		for _, c := range cells {
			_, err := d.db.NewRaw(`INSERT INTO cell_occupancies (marina_id, area_id, cell, night, allocation_id, kind)
				VALUES (?, ?, ?, ?, ?, ?)`, d.marina, area.ID, c, n, id, kind).Exec(ctx)
			must(t, err)
		}
	}
	return id
}

// freeCells returns n cells of the area that no allocation holds in [first, last].
func freeCells(t *testing.T, d *demo, area store.DynamicArea, n int, first, last calendar.Date) []int {
	t.Helper()
	var used []int
	must(t, d.db.NewSelect().TableExpr("cell_occupancies").ColumnExpr("DISTINCT cell").Where("area_id = ?", area.ID).
		Where("night >= ?", first).Where("night <= ?", last).Scan(context.Background(), &used))
	taken := map[int]bool{}
	for _, c := range used {
		taken[c] = true
	}
	var out []int
	for c := int(area.CellCount) - 1; c >= 0 && len(out) < n; c-- {
		if !taken[c] {
			out = append(out, c)
		}
	}
	return out
}

// TestReport_Storage: an owner boat on the demo shed is a record with its footprint, a block removes surface from
// the available one, the exit is blocked when the accessibility says so, and the occupancy tile equals the fold of
// the drill records (INV-62).
func TestReport_Storage(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		ctx := context.Background()
		owner := d.principal(auth.RoleOwner)
		area, vessel := shedOf(t, d)
		from, to := d.today, d.today.AddDays(9)
		q := report.Params{From: from, To: to, AreaID: area.ID}
		before, err := d.svc.Storage(ctx, owner, d.marina, q)
		must(t, err)

		cells := freeCells(t, d, area, 14, from, to)
		boat := insertAreaAllocation(t, d, area, "owner", vessel, cells[:12], from, from.AddDays(4))
		insertAreaAllocation(t, d, area, "block", "", cells[12:], from, from)
		restore := report.SetStorageAccess(func(_ layout.Area, _ []int, _ []optimizer.Obstacle, items []optimizer.TimelineItem,
			_, _ calendar.Date,
		) []optimizer.Access {
			var out []optimizer.Access
			for _, it := range items {
				out = append(out, optimizer.Access{ID: it.ID, Event: optimizer.EventExit, On: it.ExitOn, Accessible: it.ID != boat})
			}
			return out
		})
		defer restore()

		res, err := d.svc.Storage(ctx, owner, d.marina, q)
		must(t, err)
		var rec *calc.StorageRecord
		for i := range res.Records {
			if res.Records[i].AllocationID == boat {
				rec = &res.Records[i]
			}
		}
		if rec == nil {
			t.Fatalf("no record for the boat: %+v", res.Records)
		}
		cell := area.CellCm * area.CellCm
		if rec.Nights != 5 || rec.FootprintCm2Nights != 12*cell*5 || rec.MapKind != "shed" || rec.Ref.Type != "allocation" {
			t.Errorf("record = %+v", rec)
		}
		if !rec.ExitBlocked || !rec.ExitOn.Equal(from.AddDays(5)) {
			t.Errorf("exit = %s blocked %v", rec.ExitOn, rec.ExitBlocked)
		}
		if got := before.Totals.AvailableCm2Nights - res.Totals.AvailableCm2Nights; got != 2*cell {
			t.Errorf("block removed %d cm², want %d", got, 2*cell)
		}
		if res.Totals.Boats != before.Totals.Boats+1 || res.Totals.ExitsBlocked < 1 {
			t.Errorf("totals %+v (before %+v)", res.Totals, before.Totals)
		}
		// Exits blocked only, by area.
		eb, err := d.svc.Storage(ctx, owner, d.marina, report.Params{From: from, To: to, ExitsBlockedOnly: true, GroupBy: "area"})
		must(t, err)
		if eb.Totals.ExitsBlocked != int64(len(eb.Records)) || eb.Totals.ExitsBlocked < 1 {
			t.Errorf("exits blocked only %+v", eb.Totals)
		}
		// Dashboard tiles = Σ drill records (the exits tile has its own 30-day period).
		dash, err := d.svc.Dashboard(ctx, owner, d.marina, report.DashboardParams{From: from, To: to, Compare: "none"})
		must(t, err)
		seen := 0
		for _, tile := range dash.Tiles {
			switch string(tile.Key) {
			case calc.IndStorageOccupancy, calc.IndStorageExitsBlocked, calc.IndMaintenanceOverdue, calc.IndUtilityUnbilled:
				seen++
				if got := sumRecords(t, d, owner, string(tile.Key), tile.Drill, from, to); got != tile.Current {
					t.Errorf("%s: records %+v != tile %+v", tile.Key, got, tile.Current)
				}
			}
			if string(tile.Key) == calc.IndStorageExitsBlocked {
				if tile.Drill.Params["from"] != d.today.String() || tile.Drill.Params["to"] != d.today.AddDays(29).String() {
					t.Errorf("exits tile drill %v", tile.Drill.Params)
				}
				if tile.Current.Value.Value < 1 {
					t.Errorf("exits tile %+v", tile.Current)
				}
			}
		}
		if seen != 4 {
			t.Errorf("M9 tiles = %d, want 4", seen)
		}
		// Office: no production; yard manager: no report.
		off, err := d.svc.Storage(ctx, d.principal(auth.RoleOffice), d.marina, q)
		must(t, err)
		if off.Totals.Production.OK {
			t.Error("office sees production")
		}
	})
}

// TestReport_Maintenance: tasks of the period with overdue days and costs (reports.all only).
func TestReport_Maintenance(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		ctx := context.Background()
		area, _ := shedOf(t, d)
		ins := func(status string, due *calendar.Date, doneAt *time.Time, minutes, cost *int64) string {
			id := store.NewID()
			outcome := any(nil)
			if status == "done" {
				outcome = "ok"
			}
			_, err := db.NewRaw(`INSERT INTO maintenance_tasks (id, marina_id, element_type, element_id, source, title, priority,
				status, due_on, outcome, internal_minutes, external_cost_cents, done_at, created_at, updated_at)
				VALUES (?, ?, 'dynamic_area', ?, 'office', 'Verifica', 'high', ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, d.marina, area.ID, status, due, outcome, minutes, cost, doneAt, demoNow.AddDate(0, 0, -20), demoNow).Exec(ctx)
			must(t, err)
			return id
		}
		from, to := d.today.AddDays(-30), d.today
		owner := d.principal(auth.RoleOwner)
		before, err := d.svc.Maintenance(ctx, owner, d.marina, report.Params{From: from, To: to})
		must(t, err)
		late := d.today.AddDays(-4)
		soon := d.today.AddDays(10)
		doneAt := demoNow.AddDate(0, 0, -1)
		m, c := int64(45), int64(25_000)
		overdue := ins("open", &late, nil, nil, nil)
		ins("scheduled", &soon, nil, nil, nil)
		done := ins("done", &late, &doneAt, &m, &c)
		res, err := d.svc.Maintenance(ctx, owner, d.marina, report.Params{From: from, To: to, GroupBy: "status"})
		must(t, err)
		b, a := before.Totals, res.Totals
		if a.Tasks-b.Tasks != 3 || a.Overdue-b.Overdue != 1 || a.Done-b.Done != 1 || a.InternalMinutes-b.InternalMinutes != 45 ||
			a.ExternalCost.Value-b.ExternalCost.Value != 25_000 {
			t.Errorf("totals %+v, before %+v", a, b)
		}
		var g int64
		for _, gr := range res.Groups {
			g += gr.Totals.Tasks
		}
		if g != a.Tasks {
			t.Errorf("Σ groups %d != %d", g, a.Tasks)
		}
		for _, r := range res.Records {
			switch r.TaskID {
			case overdue:
				if !r.Overdue || r.OverdueDays != 4 || r.ElementCode != area.Code {
					t.Errorf("overdue record %+v", r)
				}
			case done:
				if r.OverdueDays != 3 || !r.DoneOn.Equal(d.today.AddDays(-1)) {
					t.Errorf("done record %+v", r)
				}
			}
		}
		off, err := d.svc.Maintenance(ctx, d.principal(auth.RoleOffice), d.marina, report.Params{From: from, To: to, TaskOverdueOnly: true})
		must(t, err)
		if int64(len(off.Records)) != b.Overdue+1 || off.Totals.ExternalCost.OK {
			t.Errorf("office overdue only %+v", off)
		}
	})
}

// TestReport_Utilities: consumptions by kind and status; pending without a tariff price is noted, never valued.
func TestReport_Utilities(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		ctx := context.Background()
		meter := store.NewID()
		_, err := db.NewRaw(`INSERT INTO utility_meters (id, marina_id, code, kind, unit, created_at, updated_at)
			VALUES (?, ?, 'TEST-P1', 'power', 'kwh', ?, ?)`, meter, d.marina, demoNow, demoNow).Exec(ctx)
		must(t, err)
		reading := func(at time.Time, v int64) string {
			id := store.NewID()
			_, err := db.NewRaw(`INSERT INTO meter_readings (id, marina_id, meter_id, read_at, read_on, value_milli, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, d.marina, meter, at, calendar.In(at, time.UTC), v, at, at).Exec(ctx)
			must(t, err)
			return id
		}
		r1 := reading(demoNow.AddDate(0, 0, -10), 1_000)
		r2 := reading(demoNow.AddDate(0, 0, -1), 6_000)
		cons := func(share, status string, q int64) {
			_, err := db.NewRaw(`INSERT INTO utility_consumptions (id, marina_id, meter_id, from_reading_id, to_reading_id, share_key,
				first_night, last_night, qty_milli, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				store.NewID(), d.marina, meter, r1, r2, share, d.today.AddDays(-10), d.today.AddDays(-2), q, status, demoNow, demoNow).Exec(ctx)
			must(t, err)
		}
		cons("a", "pending", 3_000)
		cons("unassigned", "unassigned", 1_500)
		cons("b", "excluded", 500)
		res, err := d.svc.Utilities(ctx, d.principal(auth.RoleOwner), d.marina,
			report.Params{From: d.today.AddDays(-30), To: d.today, MeterID: meter, GroupBy: "status"})
		must(t, err)
		tt := res.Totals
		if len(res.Records) != 3 || tt.PowerMilli != 4_500 || tt.PendingMilli != 3_000 || tt.UnassignedMilli != 1_500 ||
			!tt.PendingValue.OK || tt.PendingValue.Value != 0 {
			t.Errorf("totals %+v", tt)
		}
		if len(res.Notes) != 1 || res.Notes[0].Code != calc.NoteNoPrice {
			t.Errorf("notes %+v", res.Notes)
		}
		if len(res.Groups) != 3 || res.Records[0].MeterCode != "TEST-P1" {
			t.Errorf("groups %+v", res.Groups)
		}
		off, err := d.svc.Utilities(ctx, d.principal(auth.RoleOffice), d.marina, report.Params{MeterID: meter, From: d.today.AddDays(-30), To: d.today})
		must(t, err)
		if off.Totals.PendingValue.OK || off.Totals.Charged.OK {
			t.Error("office sees utility amounts")
		}
	})
}
