//go:build integration

package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

func newSeedApp(t *testing.T, db *store.DB) (*app.App, string) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	dir := t.TempDir()
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_DEV": "true", "PORTO_DATA_DIR": dir,
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler),
		app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}})
	if err != nil {
		t.Fatal(err)
	}
	return a, dir
}

// TestSeed_DemoOperationalContents: a fresh demo shows the operational screens full around today — yard quotes in
// every state, work orders in every state with tasks, hours, materials and external costs, a planner week of
// assignments around today, lifting bookings today and tomorrow on the travel lift and the crane, the yard users
// linked to employees; the whole rental fleet with bookings in every state today and over the week; online
// bookings waiting for approval, confirmed and rejected; seasonal renewals so that the renewal rate has a value
// this year and last year; open sensor alerts. A second run adds nothing.
func TestSeed_DemoOperationalContents(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		a, _ := newSeedApp(t, db)
		ctx := fixture.Ctx()
		res, err := a.SeedDemo(ctx, "demo-password-123")
		if err != nil {
			t.Fatal(err)
		}
		count := func(table, where string, args ...any) int {
			t.Helper()
			q := db.NewSelect().TableExpr(table).Where("marina_id = ?", res.MarinaID)
			if where != "" {
				q = q.Where(where, args...)
			}
			n, err := q.Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		atLeast := func(what string, n, want int) {
			t.Helper()
			if n < want {
				t.Errorf("%s: %d, want >= %d", what, n, want)
			}
		}
		today := calendar.In(a.Clock.Now(), mustLoc(t, "Europe/Rome"))

		// Yard.
		for st, want := range map[string]int{
			store.YardQuoteDraft: 2, store.YardQuoteAccepted: 5, store.YardQuoteDeclined: 1, "sent": 2,
		} {
			atLeast("yard quotes "+st, count("yard_quotes", "status = ?", st), want)
		}
		for _, st := range []string{
			store.WorkOrderPlanned, store.WorkOrderInProgress, store.WorkOrderOnHold, store.WorkOrderCompleted,
			store.WorkOrderDelivered, store.WorkOrderInvoiced,
		} {
			atLeast("work orders "+st, count("work_orders", "status = ?", st), 1)
		}
		atLeast("work orders on a bay", count("work_orders", "allocation_id IS NOT NULL"), 3)
		atLeast("work tasks", count("work_tasks", ""), 15)
		atLeast("approved hours", count("timesheets", "status = ?", store.TimesheetApproved), 10)
		atLeast("hours to approve", count("timesheets", "status = ?", store.TimesheetSubmitted), 1)
		atLeast("stock movements", count("stock_movements", ""), 2)
		atLeast("external costs", count("work_order_external_costs", ""), 2)
		atLeast("final account", count("work_order_billings", "kind = ?", store.WOBillingFinal), 1)
		atLeast("assignments before today", count("task_assignments", "work_date < ?", today), 5)
		atLeast("assignments today", count("task_assignments", "work_date = ?", today), 3)
		atLeast("assignments this week", count("task_assignments", "work_date > ? AND work_date <= ?", today, today.AddDays(6)), 5)
		var assignees int
		if err := db.NewSelect().TableExpr("task_assignments").ColumnExpr("COUNT(DISTINCT employee_id)").
			Where("marina_id = ?", res.MarinaID).Scan(ctx, &assignees); err != nil {
			t.Fatal(err)
		}
		atLeast("employees with assignments", assignees, 3)
		atLeast("yard users linked to employees", count("employees", "user_id IS NOT NULL"), 2)
		atLeast("lifts done", count("equipment_bookings", "status = ?", store.LiftBookingDone), 1)
		atLeast("lifts today", count("equipment_bookings", "date = ? AND status = ?", today, store.LiftBookingBooked), 2)
		atLeast("lifts tomorrow", count("equipment_bookings", "date = ? AND status = ?", today.AddDays(1), store.LiftBookingBooked), 2)
		atLeast("lift requests", count("equipment_bookings", "status = ?", store.LiftBookingRequested), 1)
		var craneBookings int
		if err := db.NewSelect().TableExpr("equipment_bookings AS b").Join("JOIN equipment AS e ON e.id = b.equipment_id").
			ColumnExpr("COUNT(*)").Where("b.marina_id = ?", res.MarinaID).Where("e.kind = ?", "crane").Scan(ctx, &craneBookings); err != nil {
			t.Fatal(err)
		}
		atLeast("crane bookings", craneBookings, 2)
		if vs, err := store.CheckYardInvariants(ctx, db.DB, res.MarinaID); err != nil || len(vs) != 0 {
			t.Errorf("yard invariants %+v %v", vs, err)
		}
		if vs, err := store.CheckLiftingInvariants(ctx, db.DB, res.MarinaID); err != nil || len(vs) != 0 {
			t.Errorf("lifting invariants %+v %v", vs, err)
		}

		// Rental.
		atLeast("rental units", count("rental_units", ""), 8)
		for st, want := range map[string]int{
			store.RentalStatusHeld: 3, store.RentalStatusConfirmed: 8, store.RentalStatusCheckedOut: 2,
			store.RentalStatusClosed: 8, store.RentalStatusCancelled: 1, store.RentalStatusNoShow: 1,
		} {
			atLeast("rental bookings "+st, count("rental_bookings", "status = ?", st), want)
		}
		atLeast("rentals to check out today", count("rental_bookings", "starts_on = ? AND status = ?", today, store.RentalStatusConfirmed), 3)
		atLeast("rentals with a skipper", count("rental_bookings", "skipper_id IS NOT NULL"), 4)
		if vs, err := store.CheckRentalInvariants(ctx, db.DB, res.MarinaID); err != nil || len(vs) != 0 {
			t.Errorf("rental invariants %+v %v", vs, err)
		}

		// Online bookings (desk list).
		var online []struct {
			Status string `bun:"status"`
			N      int    `bun:"n"`
		}
		if err := db.NewSelect().TableExpr("online_bookings AS o").Join("JOIN contracts AS c ON c.id = o.contract_id").
			ColumnExpr("c.status AS status, COUNT(*) AS n").Where("o.marina_id = ?", res.MarinaID).GroupExpr("c.status").
			Scan(ctx, &online); err != nil {
			t.Fatal(err)
		}
		states := map[string]int{}
		for _, r := range online {
			states[r.Status] = r.N
		}
		if states["awaiting_approval"] < 2 || states["confirmed"] < 2 || states["cancelled"] < 1 {
			t.Errorf("online bookings by state %v", states)
		}

		// Renewal rate: seasonal contracts ended this year and last year, most renewed.
		p := demoOwnerPrincipal(ctx, t, a, res)
		y := today.Year()
		for _, period := range []struct{ from, to calendar.Date }{
			{calendar.New(y, time.January, 1), today}, {calendar.New(y-1, time.January, 1), today.AddDays(-365)},
		} {
			if !period.to.After(calendar.New(period.to.Year(), time.October, 1)) {
				continue // the seasons of this period have not ended yet
			}
			rows, err := a.Reports.Customers(ctx, p, res.MarinaID, report.Params{From: period.from, To: period.to, Section: "renewals"})
			if err != nil {
				t.Fatal(err)
			}
			if rows.Totals.RenewalEligible == 0 || rows.Totals.RenewalRenewed == 0 {
				t.Errorf("renewal rate %s..%s: %d of %d", period.from, period.to, rows.Totals.RenewalRenewed, rows.Totals.RenewalEligible)
			}
		}

		// Sensors.
		atLeast("open sensor alerts", count("sensor_alerts", "status = ?", "open"), 3)

		// Idempotency.
		tables := []string{
			"yard_quotes", "work_orders", "work_tasks", "timesheets", "task_assignments", "equipment_bookings", "rental_units",
			"rental_bookings", "online_bookings", "contracts", "sensor_alerts",
		}
		before := map[string]int{}
		for _, tb := range tables {
			before[tb] = count(tb, "")
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		for _, tb := range tables {
			if n := count(tb, ""); n != before[tb] {
				t.Errorf("second run: %s %d -> %d", tb, before[tb], n)
			}
		}
	})
}

// TestSeed_DemoResetGuard: the reset refuses a database with another organization or a user outside the demo
// organization; on the demo database it wipes the data and the stored files, and the demo seeds again.
func TestSeed_DemoResetGuard(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		a, dir := newSeedApp(t, db)
		ctx := fixture.Ctx()
		if err := a.CheckDemoReset(ctx); err != nil {
			t.Fatalf("empty database refused: %v", err)
		}
		demo, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "owner@" + app.DemoEmailDomain, Name: "Demo Owner", Password: "demo-password-123", Organization: app.DemoOrganization,
			MarinaName: app.DemoMarinaName, MarinaSlug: app.DemoMarinaSlug, Timezone: "Europe/Rome", Locale: "it",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.CheckDemoReset(ctx); err != nil {
			t.Fatalf("demo database refused: %v", err)
		}
		// A real customer next to the demo: refused, nothing wiped.
		if _, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "owner@marina-vera.test", Name: "Owner", Password: "real-password-123", Organization: "Marina Vera SRL",
		}); err != nil {
			t.Fatal(err)
		}
		if err := a.ResetDemo(ctx); !errors.Is(err, app.ErrNotDemoDatabase) {
			t.Fatalf("reset with another organization: %v", err)
		}
		if n, err := db.NewSelect().TableExpr("organizations").Count(ctx); err != nil || n != 2 {
			t.Fatalf("organizations after a refused reset: %d %v", n, err)
		}
		if _, err := db.NewDelete().TableExpr("organizations").Where("name <> ?", app.DemoOrganization).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		// The other owner is left without membership: still refused.
		if err := a.CheckDemoReset(ctx); !errors.Is(err, app.ErrNotDemoDatabase) {
			t.Fatalf("reset with a user outside the demo: %v", err)
		}
		if _, err := db.NewDelete().TableExpr("users").Where("id <> ?", demo.UserID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		files := filepath.Join(dir, "files", "x")
		if err := os.MkdirAll(files, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "blob"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := a.ResetDemo(ctx); err != nil {
			t.Fatalf("reset of the demo database: %v", err)
		}
		if n, err := db.NewSelect().TableExpr("users").Count(ctx); err != nil || n != 0 {
			t.Fatalf("users after the reset: %d %v", n, err)
		}
		if entries, err := os.ReadDir(filepath.Join(dir, "files")); err != nil || len(entries) != 0 {
			t.Fatalf("files after the reset: %v %v", entries, err)
		}
	})
}

// TestSeed_DemoResetAfterVisitorChanges: visitors of a public demo log in as the demo owner and may rename the
// organization and the marina, add marinas and remove team members. The reset still runs afterwards (the demo is
// recognised by the marker SeedDemo writes in the data directory), seeds the demo again, and keeps refusing a
// database with another organization.
func TestSeed_DemoResetAfterVisitorChanges(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		a, _ := newSeedApp(t, db)
		ctx := fixture.Ctx()
		res, err := a.SeedDemo(ctx, "demo-password-123")
		if err != nil {
			t.Fatal(err)
		}
		p := demoOwnerPrincipal(ctx, t, a, res)
		orgs, err := a.Marinas.ListOrganizations(ctx, p)
		if err != nil || len(orgs) != 1 {
			t.Fatalf("organizations: %+v %v", orgs, err)
		}
		org := orgs[0]
		orgName, marinaName := "Visitor Org", "Visitor Marina"
		if _, err := a.Marinas.UpdateOrganization(ctx, p, res.OrganizationID, org.Version, marina.OrganizationPatch{Name: &orgName}); err != nil {
			t.Fatal(err)
		}
		m, err := a.Marinas.GetMarina(ctx, p, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Marinas.UpdateMarina(ctx, p, res.MarinaID, m.Version, marina.MarinaPatch{Name: &marinaName}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Marinas.CreateMarina(ctx, p, res.OrganizationID, marina.CreateMarinaInput{
			Name: "Second Marina", Slug: "visitor-second", Timezone: "Europe/Rome",
		}); err != nil {
			t.Fatal(err)
		}
		viewer, err := store.UserByEmail(ctx, a.DB.DB, string(auth.RoleViewer)+"@"+app.DemoEmailDomain)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Team.RemoveMember(ctx, p, res.MarinaID, viewer.ID); err != nil {
			t.Fatal(err)
		}
		if err := a.ResetDemo(ctx); err != nil {
			t.Fatalf("reset after visitor changes: %v", err)
		}
		again, err := a.SeedDemo(ctx, "demo-password-123")
		if err != nil || !again.Created {
			t.Fatalf("seed after the reset: %+v %v", again, err)
		}
		if n, err := db.NewSelect().TableExpr("marinas").Count(ctx); err != nil || n != 1 {
			t.Fatalf("marinas after the reset: %d %v", n, err)
		}
		// The marker follows the new demo organization: a foreign organization is still refused.
		if _, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "owner@marina-vera.test", Name: "Owner", Password: "real-password-123", Organization: "Marina Vera SRL",
		}); err != nil {
			t.Fatal(err)
		}
		if err := a.ResetDemo(ctx); !errors.Is(err, app.ErrNotDemoDatabase) {
			t.Fatalf("reset with another organization: %v", err)
		}
		if n, err := db.NewSelect().TableExpr("organizations").Count(ctx); err != nil || n != 2 {
			t.Fatalf("organizations after a refused reset: %d %v", n, err)
		}
	})
}

func mustLoc(t *testing.T, tz string) *time.Location {
	t.Helper()
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func demoOwnerPrincipal(ctx context.Context, t *testing.T, a *app.App, res *app.SeedResult) *auth.Principal {
	t.Helper()
	u, err := store.UserByEmail(ctx, a.DB.DB, "owner@"+app.DemoEmailDomain)
	if err != nil {
		t.Fatal(err)
	}
	return &auth.Principal{Kind: auth.KindUser, UserID: u.ID, Grants: []auth.Grant{{OrganizationID: res.OrganizationID, Role: auth.RoleOwner}}}
}
