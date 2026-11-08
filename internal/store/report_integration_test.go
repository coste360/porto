//go:build integration

package store_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// reportQueries runs every report query of the store on a scope and returns the number of rows of each.
func reportQueries(t *testing.T, db *store.DB, s store.MarinaScope, f store.ReportFilter, allocIDs []string) map[string]int {
	t.Helper()
	ctx := context.Background()
	out := map[string]int{}
	check := func(name string, rows any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = reflect.ValueOf(rows).Len()
	}
	res, err := store.ReportResources(ctx, db.DB, s)
	if err != nil {
		t.Fatal(err)
	}
	out["ReportResources"] = len(res.Berths) + len(res.Areas) + len(res.Lands) + len(res.MapKinds) + len(res.Groups)
	r1, err := store.ReportClosures(ctx, db.DB, s, f)
	check("ReportClosures", r1, err)
	r2, err := store.ReportBerthNights(ctx, db.DB, s, f)
	check("ReportBerthNights", r2, err)
	r3, err := store.ReportLandNights(ctx, db.DB, s, f)
	check("ReportLandNights", r3, err)
	r4, err := store.ReportCellNights(ctx, db.DB, s, f)
	check("ReportCellNights", r4, err)
	r5, err := store.ReportAllocations(ctx, db.DB, s, store.ReportAllocationFilter{From: f.From, To: f.To})
	check("ReportAllocations", r5, err)
	r5b, err := store.ReportAllocations(ctx, db.DB, s, store.ReportAllocationFilter{IDs: allocIDs})
	check("ReportAllocationsByID", r5b, err)
	r6, err := store.ReportAllocationNights(ctx, db.DB, s, allocIDs)
	check("ReportAllocationNights", r6, err)
	r7, err := store.ReportReleases(ctx, db.DB, s, f)
	check("ReportReleases", r7, err)
	all := map[string][]string{"mooring": {"active", "expired", "renewed", "terminated", "suspended"}, "transit": {"confirmed", "arrived", "departed"}}
	r8, err := store.ReportContracts(ctx, db.DB, s, store.ReportContractFilter{Statuses: all, StartsBy: f.To})
	check("ReportContracts", r8, err)
	r9, err := store.ReportRenewals(ctx, db.DB, s)
	check("ReportRenewals", r9, err)
	r10, err := store.ReportInquiries(ctx, db.DB, s, f)
	check("ReportInquiries", r10, err)
	r11, err := store.ReportPresences(ctx, db.DB, s, f)
	check("ReportPresences", r11, err)
	var vids, cids []string
	for _, c := range r8 {
		vids, cids = append(vids, c.VesselID), append(cids, c.CustomerID)
	}
	r12, err := store.ReportVessels(ctx, db.DB, s, vids)
	check("ReportVessels", r12, err)
	r13, err := store.ReportCustomers(ctx, db.DB, s, cids)
	check("ReportCustomers", r13, err)
	r14, err := store.ReportChargeLines(ctx, db.DB, s, f)
	check("ReportChargeLines", r14, err)
	r15, err := store.ReportContractCharges(ctx, db.DB, s, f.To)
	check("ReportContractCharges", r15, err)
	r16, err := store.ReportPayments(ctx, db.DB, s, f)
	check("ReportPayments", r16, err)
	r17, err := store.ReportRefunds(ctx, db.DB, s, f)
	check("ReportRefunds", r17, err)
	r18, err := store.ReportOpenCharges(ctx, db.DB, s, f.To)
	check("ReportOpenCharges", r18, err)
	r19, err := store.ReportScheduledInstallments(ctx, db.DB, s, f.To.AddDays(400))
	check("ReportScheduledInstallments", r19, err)
	r20, err := store.ReportReminders(ctx, db.DB, s, f.From.Time(time.UTC), f.To.AddDays(1).Time(time.UTC))
	check("ReportReminders", r20, err)
	r21, err := store.ReportRevenueClasses(ctx, db.DB, s)
	check("ReportRevenueClasses", r21, err)
	r22, err := store.ReportRights(ctx, db.DB, s, f)
	check("ReportRights", r22, err)
	r23, err := store.ReportHolderCharges(ctx, db.DB, s, f)
	check("ReportHolderCharges", r23, err)
	r24, err := store.ReportCommonRuns(ctx, db.DB, s, f.From.Time(time.UTC), f.To.AddDays(1).Time(time.UTC))
	check("ReportCommonRuns", r24, err)
	r25, err := store.ReportMandates(ctx, db.DB, s, f)
	check("ReportMandates", r25, err)
	r26, err := store.ReportCashSessions(ctx, db.DB, s, f)
	check("ReportCashSessions", r26, err)
	r27, err := store.ReportFiscalDocuments(ctx, db.DB, s, f)
	check("ReportFiscalDocuments", r27, err)
	return out
}

// TestReportStore_Scoped (m7-plan §11.2): every report query of a marina returns rows of that marina only. The
// demo marina has data in every query; another marina of the same organization, asked with the ids of the demo
// rows, gets none of them.
func TestReportStore_Scoped(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		demoID := seedDemoMarina(t, db)
		demo, err := store.ResolveMarinaScope(ctx, db.DB, demoID)
		if err != nil {
			t.Fatal(err)
		}
		e := fixture.New(t, db)
		other, err := store.ResolveMarinaScope(ctx, db.DB, e.AddMarina(t, "altra"))
		if err != nil {
			t.Fatal(err)
		}
		f := store.ReportFilter{From: calendar.MustParse("2025-08-01"), To: calendar.MustParse("2026-08-31")}
		var allocIDs []string
		rows, err := store.ReportAllocations(ctx, db.DB, demo, store.ReportAllocationFilter{From: f.From, To: f.To})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			allocIDs = append(allocIDs, r.ID)
		}
		got := reportQueries(t, db, demo, f, allocIDs)
		for _, name := range []string{
			"ReportResources", "ReportBerthNights", "ReportAllocations", "ReportAllocationsByID",
			"ReportContracts", "ReportChargeLines", "ReportPayments", "ReportOpenCharges", "ReportPresences", "ReportCustomers",
		} {
			if got[name] == 0 {
				t.Errorf("%s: the demo marina should have rows", name)
			}
		}
		for name, n := range reportQueries(t, db, other, f, allocIDs) {
			if n != 0 {
				t.Errorf("%s: %d rows of another marina", name, n)
			}
		}
	})
}

// seedDemoMarina seeds the demo marina with the clock on 2026-08-20 and returns its id.
func seedDemoMarina(t *testing.T, db *store.DB) string {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_DEV": "true", "PORTO_DATA_DIR": t.TempDir(),
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler),
		app.Options{
			DB: db, Transport: &fixture.Mailbox{},
			Clock: clock.NewFake(time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)),
		})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.SeedDemo(fixture.Ctx(), "demo-password-123")
	if err != nil {
		t.Fatal(err)
	}
	return res.MarinaID
}
