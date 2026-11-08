//go:build integration

package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestReport_PresencesAbsenceBeforeLookback (§3.13): an annual vessel that departed long before the period
// (more than a year, e.g. during the previous contract) and never came back is absent every night of the
// period up to today.
func TestReport_PresencesAbsenceBeforeLookback(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		ctx := context.Background()
		from, to := d.today.AddDays(-30), d.today
		var c struct {
			ID       string        `bun:"id"`
			VesselID string        `bun:"vessel_id"`
			StartsOn calendar.Date `bun:"starts_on"`
			EndsOn   calendar.Date `bun:"ends_on"`
		}
		err := db.NewSelect().TableExpr("contracts AS k").ColumnExpr("k.id, k.vessel_id, k.starts_on, k.ends_on").
			Where("k.marina_id = ?", d.marina).Where("k.kind = ?", calc.ContractMooring).Where("k.term = 'annual'").
			Where("k.status IN (?)", bun.List(calc.QualifyingStatuses(calc.ContractMooring))).
			Where("k.vessel_id IS NOT NULL").Where("k.starts_on <= ?", from).Where("k.ends_on > ?", to).
			Where("NOT EXISTS (SELECT 1 FROM presences AS p WHERE p.marina_id = k.marina_id AND p.vessel_id = k.vessel_id)").
			OrderExpr("k.id").Limit(1).Scan(ctx, &c)
		if err != nil {
			t.Fatalf("no annual contract without presences in the demo: %v", err)
		}
		left := from.AddDays(-400)
		at := time.Date(left.Year(), left.Month(), left.Day(), 10, 0, 0, 0, time.UTC)
		_, err = db.NewRaw(`INSERT INTO presences (id, marina_id, vessel_id, event, at, night, source, created_at, updated_at)
			VALUES (?, ?, ?, 'departed', ?, ?, 'staff', ?, ?)`, store.NewID(), d.marina, c.VesselID, at, left, at, at).Exec(ctx)
		must(t, err)
		res, err := d.svc.Presences(ctx, d.principal(auth.RoleOwner), d.marina, report.Params{From: from, To: to, Section: "absences"})
		must(t, err)
		want := int64(from.DaysUntil(to) + 1)
		for _, r := range res.Records {
			if r.ContractID == c.ID {
				if r.AbsenceNights != want {
					t.Fatalf("absence nights %d, want %d", r.AbsenceNights, want)
				}
				return
			}
		}
		t.Fatalf("contract %s has no absence record (want %d nights, %d records)", c.ID, want, len(res.Records))
	})
}
