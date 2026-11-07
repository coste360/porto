//go:build integration

package rights

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// nights 2027-06-02 .. 2027-12-31
const restOfYear = 213

func TestRight_OwnerOccupancy(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1, a2 := e.Berths[0], e.Berths[1]
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), e.Vessel)
		if got := e.ownerRows(t, a1); got != restOfYear {
			t.Fatalf("owner rows %d, want %d (from today, past nights not written)", got, restOfYear)
		}
		if len(r.Allocations) != 1 || !r.Allocations[0].FirstNight.Equal(d(today)) || r.Allocations[0].Kind != store.AllocKindOwner {
			t.Fatalf("allocations %+v", r.Allocations)
		}
		// a transit on the right's berth is refused by the rows (INV-01)
		if _, err := e.Contracts.CreateTransit(ctx, e.Owner, e.MarinaID, contract.TransitInput{
			VesselID: e.Vessel, CustomerID: e.Customer, Arrival: d("2027-07-01"), Departure: d("2027-07-05"),
			Placements: []contract.Segment{contracttest.BerthSeg(a1, "2027-07-01", "2027-07-04")},
		}); err == nil {
			t.Fatal("transit on an owner berth accepted")
		}
		// a right on a berth with a sold transit: 409 resource_taken, nothing written
		e.Transit(t, a2, "2027-07-01", "2027-07-05", true)
		_, err := e.Rights.CreateRight(ctx, e.Owner, e.MarinaID, RightInput{
			BerthID: a2, Kind: KindLongTermUse, FromOn: d(today), ToOn: d("2027-12-31"), Holders: e.one(e.Customer),
		})
		if _, ok := errors.AsType[*occupancy.ConflictError](err); !ok {
			t.Fatalf("conflict: %v", err)
		}
		if n := e.ownerRows(t, a2); n != 0 {
			t.Fatalf("rows written on conflict: %d", n)
		}
		if n, _ := store.CountBerthRights(context.Background(), db.DB, e.scope(t)); n != 1 {
			t.Fatalf("rights %d", n)
		}
		if n := len(e.Events(t, event.RightCreated)); n != 1 {
			t.Fatalf("right.created %d", n)
		}
		if n := e.AuditCount(t, "right.create"); n != 1 {
			t.Fatalf("audit %d", n)
		}
		e.invariants(t)
		// INV-40 on update
		_, err = e.Rights.UpdateRight(ctx, e.Owner, e.MarinaID, r.ID, r.Version, RightPatch{Holders: []allot.Share{{CustomerID: e.Customer, Millis: 999}}})
		var se *SumError
		if !errors.As(err, &se) || se.SumMillis != 999 || !errors.Is(err, ErrSharesInvalid) {
			t.Fatalf("shares 999: %v", err)
		}
		// end the right: rows from on are gone
		ended, err := e.Rights.EndRight(ctx, e.Owner, e.MarinaID, r.ID, r.Version, d("2027-10-01"), "fine")
		if err != nil {
			t.Fatal(err)
		}
		if ended.Status != RightEnded || !ended.ToOn.Equal(d("2027-09-30")) {
			t.Fatalf("ended %+v", ended)
		}
		if got := e.ownerRows(t, a1); got != restOfYear-92 {
			t.Fatalf("owner rows after end %d", got)
		}
		e.invariants(t)
	})
}

func TestRight_PrimaryVesselChange(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		v2 := e.AddVessel(t, "Maestrale", 1000, 350, "monohull_sail")
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), e.Vessel)
		out, err := e.Rights.SetRightVessels(ctx, e.Owner, e.MarinaID, r.ID, r.Version, RightVesselsInput{
			Vessels: []VesselInput{{VesselID: v2, Primary: true}, {VesselID: e.Vessel}}, FromNight: d("2027-07-01"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Allocations) != 2 || out.Allocations[0].VesselID != e.Vessel || out.Allocations[1].VesselID != v2 ||
			!out.Allocations[0].LastNight.Equal(d("2027-06-30")) || !out.Allocations[1].FirstNight.Equal(d("2027-07-01")) {
			t.Fatalf("allocations %+v", out.Allocations)
		}
		if got := e.ownerRows(t, a1); got != restOfYear {
			t.Fatalf("rows %d", got)
		}
		if _, err := e.Rights.SetRightVessels(ctx, e.Owner, e.MarinaID, r.ID, r.Version, RightVesselsInput{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		e.invariants(t)
	})
}

func (e *testEnv) mandate(t *testing.T, right, from, to string) *Mandate {
	t.Helper()
	m, err := e.Rights.CreateMandate(ctx, e.Owner, e.MarinaID, MandateInput{
		RightID: right, FromOn: d(from), ToOn: d(to), OwnerShareBP: 6000,
		MinGuaranteedCents: 100_000, Channels: []string{"transit"}, Settlement: "credit",
	})
	if err != nil {
		t.Fatalf("mandate: %v", err)
	}
	return m
}

func TestMandate_ReleasesOwnerNights(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), e.Vessel)
		e.mandate(t, r.ID, "2027-07-01", "2027-08-31")
		if got := e.ownerRows(t, a1); got != restOfYear-62 {
			t.Fatalf("owner rows %d", got)
		}
		_, err := e.Rights.CreateMandate(ctx, e.Owner, e.MarinaID, MandateInput{
			RightID: r.ID, FromOn: d("2027-08-15"), ToOn: d("2027-09-15"),
			Channels: []string{"transit"}, Settlement: "credit",
		})
		if !errors.Is(err, ErrMandateOverlap) {
			t.Fatalf("overlap: %v", err)
		}
		// the berth is sellable inside the mandate
		e.Transit(t, a1, "2027-07-10", "2027-07-17", true)
		e.invariants(t)
	})
}

func TestMandate_RevenueAccrualIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), e.Vessel)
		m := e.mandate(t, r.ID, "2027-07-01", "2027-08-31")
		tr := e.Transit(t, a1, "2027-07-10", "2027-07-17", true)
		n, err := e.Rights.AccrueMandates(ctx, e.Owner, e.MarinaID)
		if err != nil || n != 1 {
			t.Fatalf("accrue: %d %v", n, err)
		}
		if n, err := e.Rights.AccrueMandates(ctx, e.Owner, e.MarinaID); err != nil || n != 0 {
			t.Fatalf("second accrue: %d %v", n, err)
		}
		if got := len(e.Events(t, event.MandateRevenueAccrued)); got != 1 {
			t.Fatalf("revenue events %d", got)
		}
		st, err := e.Rights.MandateStatement(ctx, e.Owner, e.MarinaID, m.ID, d("2027-07-01"), d("2027-08-31"))
		if err != nil {
			t.Fatal(err)
		}
		if st.RevenueCents != tr.Row.TotalCents || st.OwnerShareCents != int64(money.Cents(tr.Row.TotalCents).PercentBP(6000)) {
			t.Fatalf("statement %d/%d, transit total %d", st.RevenueCents, st.OwnerShareCents, tr.Row.TotalCents)
		}
		if want := max(0, 100_000-st.OwnerShareCents); st.GuaranteedTopUpCents != want || len(st.Lines) != 1 || st.Lines[0].Nights != 7 {
			t.Fatalf("statement %+v", st)
		}
		got, err := e.Rights.GetMandate(ctx, e.Owner, e.MarinaID, m.ID)
		if err != nil || got.RevenueCents != tr.Row.TotalCents {
			t.Fatalf("mandate revenue %+v %v", got, err)
		}
	})
}

func TestMandate_EndRestoresFreeNights(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), e.Vessel)
		m := e.mandate(t, r.ID, "2027-07-01", "2027-08-31")
		e.Transit(t, a1, "2027-08-10", "2027-08-15", true) // nights 10..14
		out, err := e.Rights.EndMandate(ctx, e.Owner, e.MarinaID, m.ID, m.Version, d("2027-08-01"), "")
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != MandateEnded || !out.EndedOn.Equal(d("2027-08-01")) || !out.ToOn.Equal(d("2027-08-31")) || out.SoldNightsKept != 5 {
			t.Fatalf("ended %+v", out)
		}
		if got := e.ownerRows(t, a1); got != restOfYear-62+31-5 {
			t.Fatalf("owner rows %d", got)
		}
		e.invariants(t)
		// cancel a later mandate: every free night goes back
		m2 := e.mandate(t, r.ID, "2027-10-01", "2027-10-31")
		if _, err := e.Rights.CancelMandate(ctx, e.Owner, e.MarinaID, m2.ID, m2.Version, "errore"); err != nil {
			t.Fatal(err)
		}
		if got := e.ownerRows(t, a1); got != restOfYear-62+31-5 {
			t.Fatalf("owner rows after cancel %d", got)
		}
		e.invariants(t)
	})
}

// feeTable creates and activates a per-length-band table with one band [1000, 1500) at 800 € and returns it.
func (e *testEnv) feeTable(t *testing.T, year int) *FeeTable {
	t.Helper()
	from, to, amount := int64(1000), int64(1500), int64(80_000)
	ft, err := e.Rights.CreateFeeTable(ctx, e.Owner, e.MarinaID, FeeTableInput{
		Year: year, Name: "Oneri", Basis: allot.BasisPerLengthBand,
		Rows: []FeeRow{{FromCm: &from, ToCm: &to, AmountCents: &amount}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ft, err = e.Rights.ActivateFeeTable(ctx, e.Owner, e.MarinaID, ft.ID, ft.Version)
	if err != nil {
		t.Fatal(err)
	}
	return ft
}

const semesters = `{"schedule":{"mode":"fixed_dates","dates":[{"due_on":"06-30","bp":5000},{"due_on":"12-15","bp":5000}]}}`

func (e *testEnv) issueFees(t *testing.T, table string, plan string) *FeeRun {
	t.Helper()
	run, err := e.Rights.CreateFeeRun(ctx, e.Owner, e.MarinaID, 2027, table, []byte(plan))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Rights.SimulateFeeRun(ctx, e.Owner, e.MarinaID, run.ID, run.Version); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.Rights.GetFeeRun(ctx, e.Owner, e.MarinaID, run.ID)
	out, err := e.Rights.IssueFeeRun(ctx, e.Owner, e.MarinaID, run.ID, cur.Version)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *testEnv) holderRows(t *testing.T, f store.HolderInstallmentFilter) []store.HolderInstallment {
	t.Helper()
	rows, err := store.AllHolderInstallments(context.Background(), e.DB.DB, e.scope(t), f)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func sumOf(rows []store.HolderInstallment) int64 {
	var s int64
	for _, r := range rows {
		s += r.AmountCents
	}
	return s
}

func TestFeeRun_SimulateIssue(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1, a2, a3 := e.Berths[0], e.Berths[1], e.Berths[2]
		if _, err := db.NewUpdate().Table("berths").Set("length_cm = 2000").Where("id = ?", a3).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		c2 := e.AddCustomer(t, "Bianchi")
		e.right(t, a1, "2027-01-01", "2027-12-31", []allot.Share{{CustomerID: e.Customer, Millis: 333}, {CustomerID: c2, Millis: 667}}, "")
		e.right(t, a2, "2027-07-01", "2030-12-31", e.one(c2), "")
		r3 := e.right(t, a3, "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		ft := e.feeTable(t, 2027)
		run, err := e.Rights.CreateFeeRun(ctx, e.Owner, e.MarinaID, 2027, ft.ID, []byte(semesters))
		if err != nil {
			t.Fatal(err)
		}
		sim, err := e.Rights.SimulateFeeRun(ctx, e.Owner, e.MarinaID, run.ID, run.Version)
		if err != nil {
			t.Fatal(err)
		}
		// A1 800 €, A2 800 € × 184 / 365 = 403.29 € (half-even), A3 2000 cm: no band
		if sim.TotalCents != 80_000+40_329 || len(sim.Excluded) != 1 || sim.Excluded[0].RightID != r3.ID || sim.Excluded[0].Reason != allot.ExcludedNoRate {
			t.Fatalf("simulation %+v", sim)
		}
		// re-simulate replaces the lines
		cur, _ := e.Rights.GetFeeRun(ctx, e.Owner, e.MarinaID, run.ID)
		if _, err := e.Rights.SimulateFeeRun(ctx, e.Owner, e.MarinaID, run.ID, cur.Version); err != nil {
			t.Fatal(err)
		}
		cur, _ = e.Rights.GetFeeRun(ctx, e.Owner, e.MarinaID, run.ID)
		issued, err := e.Rights.IssueFeeRun(ctx, e.Owner, e.MarinaID, run.ID, cur.Version)
		if err != nil {
			t.Fatal(err)
		}
		rows := e.holderRows(t, store.HolderInstallmentFilter{RunID: run.ID})
		if sumOf(rows) != 120_329 || len(rows) != 6 {
			t.Fatalf("holder instalments %d rows Σ %d", len(rows), sumOf(rows))
		}
		// INV-31 per holder line: Σ instalments = holder amount
		lines, _ := e.Rights.AllFeeLines(ctx, e.Owner, e.MarinaID, run.ID)
		for _, l := range lines {
			for _, h := range l.HolderShares {
				var s int64
				for _, x := range rows {
					if x.SourceID == l.ID && x.CustomerID == h.CustomerID {
						s += x.AmountCents
					}
				}
				if s != h.AmountCents {
					t.Fatalf("line %s holder Σ %d, amount %d", l.ID, s, h.AmountCents)
				}
			}
		}
		if _, err := e.Rights.IssueFeeRun(ctx, e.Owner, e.MarinaID, run.ID, issued.Version); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("second issue: %v", err)
		}
		// another run of the same year and table cannot be issued (partial unique)
		run2, _ := e.Rights.CreateFeeRun(ctx, e.Owner, e.MarinaID, 2027, ft.ID, nil)
		_, _ = e.Rights.SimulateFeeRun(ctx, e.Owner, e.MarinaID, run2.ID, run2.Version)
		cur2, _ := e.Rights.GetFeeRun(ctx, e.Owner, e.MarinaID, run2.ID)
		if _, err := e.Rights.IssueFeeRun(ctx, e.Owner, e.MarinaID, run2.ID, cur2.Version); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second issued run: %v", err)
		}
		if n := len(e.Events(t, event.ManagementFeeIssued)); n != 1 {
			t.Fatalf("issued events %d", n)
		}
		// lines of an issued run are frozen (INV-44)
		_, err = db.NewUpdate().Table("management_fee_lines").Set("amount_cents = 1").Where("run_id = ?", run.ID).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.AllocationsImmutableMessage) {
			t.Fatalf("issued lines changed: %v", err)
		}
		// the job issues the due ones
		n, err := e.Rights.IssueDueTx(context.Background(), db.DB, e.scope(t), d("2027-06-30"), 100)
		if err != nil || n == 0 {
			t.Fatalf("issue due: %d %v", n, err)
		}
		if again, _ := e.Rights.IssueDueTx(context.Background(), db.DB, e.scope(t), d("2027-06-30"), 100); again != 0 {
			t.Fatalf("issue job not idempotent: %d", again)
		}
	})
}

func TestTransfer_CompleteMovesEverything(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1, a2 := e.Berths[0], e.Berths[1]
		c2, c3 := e.AddCustomer(t, "Bianchi"), e.AddCustomer(t, "Verdi")
		r := e.right(t, a1, "2027-01-01", "2027-12-31", []allot.Share{{CustomerID: e.Customer, Millis: 500}, {CustomerID: c2, Millis: 500}}, e.Vessel)
		r2 := e.right(t, a2, "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		ft := e.feeTable(t, 2027)
		e.issueFees(t, ft.ID, semesters)

		// reassign
		tr, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{
			RightID: r.ID, EffectiveOn: d("2027-09-01"),
			ToHolders: e.one(c3), FeesPolicy: FeesReassign,
		})
		if err != nil {
			t.Fatal(err)
		}
		done, err := e.Rights.CompleteTransfer(ctx, e.Owner, e.MarinaID, tr.ID, tr.Version)
		if err != nil {
			t.Fatal(err)
		}
		old, _ := e.Rights.GetRight(ctx, e.Owner, e.MarinaID, r.ID)
		nr, err := e.Rights.GetRight(ctx, e.Owner, e.MarinaID, done.NewRightID)
		if err != nil {
			t.Fatal(err)
		}
		if old.Status != RightTransferred || !old.ToOn.Equal(d("2027-08-31")) || nr.Status != RightActive || !nr.FromOn.Equal(d("2027-09-01")) ||
			len(nr.Holders) != 1 || nr.Holders[0].CustomerID != c3 {
			t.Fatalf("old %+v new %+v", old, nr)
		}
		if got := e.ownerRows(t, a1); got != restOfYear {
			t.Fatalf("owner rows %d", got)
		}
		future := e.holderRows(t, store.HolderInstallmentFilter{RightID: nr.ID, Status: store.InstallmentScheduled})
		if len(future) != 1 || future[0].CustomerID != c3 || future[0].AmountCents != 40_000 || !future[0].DueOn.Equal(d("2027-12-15")) {
			t.Fatalf("reassigned %+v", future)
		}
		if got := e.holderRows(t, store.HolderInstallmentFilter{RightID: r.ID, Status: store.InstallmentCancelled}); len(got) != 2 {
			t.Fatalf("cancelled %d", len(got))
		}
		if n := len(e.Events(t, event.RightTransferred)); n != 1 {
			t.Fatalf("right.transferred %d", n)
		}
		h, err := e.Rights.RightHistory(ctx, e.Owner, e.MarinaID, a1)
		if err != nil || len(h.Entries) != 2 || h.Entries[1].TransferID != tr.ID {
			t.Fatalf("history %+v %v", h, err)
		}

		// prorata: 800 € for the year, 400 € due 12-15 split by days: Oct–Dec to the new holder
		tp, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{
			RightID: r2.ID, EffectiveOn: d("2027-10-01"),
			ToHolders: e.one(c2), FeesPolicy: FeesProRata,
		})
		if err != nil {
			t.Fatal(err)
		}
		dp, err := e.Rights.CompleteTransfer(ctx, e.Owner, e.MarinaID, tp.ID, tp.Version)
		if err != nil {
			t.Fatal(err)
		}
		newRows := e.holderRows(t, store.HolderInstallmentFilter{RightID: dp.NewRightID, Status: store.InstallmentScheduled})
		oldRows := e.holderRows(t, store.HolderInstallmentFilter{RightID: r2.ID, Status: store.InstallmentScheduled})
		// 80 000 / 365 = 219 per day; Oct 1..Dec 31 = 92 days with the remainder on Dec 31: 219 × 92 + 65 = 20 213
		if sumOf(newRows) != 20_213 || sumOf(newRows)+sumOf(oldRows) != 80_000 {
			t.Fatalf("prorata new %d old %d", sumOf(newRows), sumOf(oldRows))
		}
		e.invariants(t)
	})
}

func TestTransfer_RequirementsBlockOrWarn(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		c2 := e.AddCustomer(t, "Bianchi")
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		ft := e.feeTable(t, 2027)
		e.issueFees(t, ft.ID, `{"schedule":{"mode":"single","due_offset_days":5}}`)
		e.Clock.Set(contracttest.T0.Add(20 * 24 * time.Hour))
		if _, err := e.Rights.IssueDueTx(context.Background(), db.DB, e.scope(t), d("2027-06-22"), 100); err != nil {
			t.Fatal(err)
		}
		tw, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{RightID: r.ID, EffectiveOn: d("2027-09-01"), ToHolders: e.one(c2), FeesPolicy: FeesReassign})
		if err != nil {
			t.Fatal(err)
		}
		if tw.RequirementPolicy != RequirementWarn || len(tw.RequirementIssues) != 1 || tw.RequirementIssues[0].Code != "unsettled_fees" ||
			tw.RequirementIssues[0].AmountCents != 80_000 {
			t.Fatalf("warn %+v", tw.RequirementIssues)
		}
		if _, err := db.NewUpdate().Table("marinas").Set("settings = ?", `{"rights":{"transfer_requirements":"block"}}`).Where("id = ?", e.MarinaID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, err = e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{RightID: r.ID, EffectiveOn: d("2027-09-01"), ToHolders: e.one(c2), FeesPolicy: FeesReassign})
		var re *RequirementsError
		if !errors.As(err, &re) || len(re.Issues) != 1 || !errors.Is(err, ErrTransferRequirements) {
			t.Fatalf("block: %v", err)
		}
		// settle and retry: no issue
		rows := e.holderRows(t, store.HolderInstallmentFilter{RightID: r.ID, Status: store.InstallmentIssued})
		if _, err := e.Rights.SettleHolderInstallment(ctx, e.Owner, e.MarinaID, rows[0].ID, rows[0].Version, SettleInput{Method: "bank_transfer", Reference: "CRO 123"}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{RightID: r.ID, EffectiveOn: d("2027-09-01"), ToHolders: e.one(c2), FeesPolicy: FeesReassign}); err != nil {
			t.Fatalf("after settlement: %v", err)
		}
		paid, _ := store.GetHolderInstallment(context.Background(), db.DB, e.scope(t), rows[0].ID)
		if paid.Status != store.InstallmentPaid || paid.SettlementSource != store.SettlementManual || paid.SettlementRefEnc == "" || paid.SettlementRefEnc == "CRO 123" {
			t.Fatalf("settled %+v", paid)
		}
	})
}

func TestTransfer_SignedCompletes(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		c2 := e.AddCustomer(t, "Bianchi")
		r := e.right(t, e.Berths[0], "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		tr, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{RightID: r.ID, EffectiveOn: d("2027-09-01"), ToHolders: e.one(c2), FeesPolicy: FeesReassign})
		if err != nil {
			t.Fatal(err)
		}
		// simulate an open signature: awaiting_signature, then the subject callback
		if _, err := db.NewUpdate().Table("right_transfers").Set("status = 'awaiting_signature'").Where("id = ?", tr.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		sub := e.Rights.SignatureSubject()
		scope := e.scope(t)
		err = db.RunInTx(context.Background(), func(c context.Context, tx bun.Tx) error {
			return sub.OnSignedTx(c, tx, scope, tr.ID, "", e.Clock.Now())
		})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := e.Rights.GetTransfer(ctx, e.Owner, e.MarinaID, tr.ID)
		if got.Status != TransferCompleted || got.NewRightID == "" {
			t.Fatalf("signed %+v", got)
		}
		e.invariants(t)
	})
}

// ---- common charges ----

func (e *testEnv) table(t *testing.T, code string, entries map[string]int64) *MillesimalTable {
	t.Helper()
	mt, err := e.Rights.CreateMillesimalTable(ctx, e.Owner, e.MarinaID, "Tabella "+code, code)
	if err != nil {
		t.Fatal(err)
	}
	var es []MillesimalEntry
	for b, m := range entries {
		es = append(es, MillesimalEntry{BerthID: b, Millis: m})
	}
	if _, err := e.Rights.ReplaceMillesimalEntries(ctx, e.Owner, e.MarinaID, mt.ID, mt.Version, EntriesInput{Entries: es}); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.Rights.GetMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID)
	return cur
}

func TestCommonCharges_RejectsIncompleteTable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		b := e.Berths
		mt := e.table(t, "GEN", map[string]int64{b[0]: 500, b[1]: 499})
		_, err := e.Rights.ActivateMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID, mt.Version)
		var se *SumError
		if !errors.As(err, &se) || se.SumMillis != 999 || !errors.Is(err, ErrTableIncomplete) {
			t.Fatalf("Σ 999: %v", err)
		}
		if _, err := e.Rights.ReplaceMillesimalEntries(ctx, e.Owner, e.MarinaID, mt.ID, mt.Version, EntriesInput{
			CSV: strPtr("berth;millis\nA1;500\nA2;500\nZZ;1\n"),
		}); err != nil {
			t.Fatal(err)
		} // unknown berth reported, nothing written
		cur, _ := e.Rights.GetMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID)
		if cur.SumMillis != 999 {
			t.Fatalf("CSV with rejections wrote entries: %d", cur.SumMillis)
		}
		res, err := e.Rights.ReplaceMillesimalEntries(ctx, e.Owner, e.MarinaID, mt.ID, cur.Version, EntriesInput{CSV: strPtr("A1;500\nA2;500\n")})
		if err != nil || res.SumMillis != 1000 {
			t.Fatalf("csv: %+v %v", res, err)
		}
		cur, _ = e.Rights.GetMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID)
		act, err := e.Rights.ActivateMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID, cur.Version)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Rights.ReplaceMillesimalEntries(ctx, e.Owner, e.MarinaID, mt.ID, act.Version, EntriesInput{}); !errors.Is(err, ErrTableActive) {
			t.Fatalf("edit of an active table: %v", err)
		}
		// a run over a draft table is refused (INV-41)
		draft := e.table(t, "DRF", map[string]int64{b[0]: 1000})
		bud, _ := e.Rights.CreateBudget(ctx, e.Owner, e.MarinaID, 2027, "Bilancio")
		amount := int64(1000)
		name, tid := "Pulizia", draft.ID
		if _, err := e.Rights.CreateBudgetItem(ctx, e.Owner, e.MarinaID, bud.ID, ItemInput{Name: &name, TableID: &tid, BudgetCents: &amount}); err != nil {
			t.Fatal(err)
		}
		bud, _ = e.Rights.ApproveBudget(ctx, e.Owner, e.MarinaID, bud.ID, bud.Version)
		share := int64(5000)
		if _, err := e.Rights.CreateCommonRun(ctx, e.Owner, e.MarinaID, bud.ID, CommonRunInput{Kind: RunAdvance, ShareBP: &share}); !errors.Is(err, ErrTableIncomplete) {
			t.Fatalf("run on a draft table: %v", err)
		}
	})
}

func TestMillesimal_ActiveImmutable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		mt := e.table(t, "GEN", map[string]int64{e.Berths[0]: 1000})
		if _, err := e.Rights.ActivateMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID, mt.Version); err != nil {
			t.Fatal(err)
		}
		_, err := db.NewUpdate().Table("millesimal_entries").Set("millis = 999").Where("table_id = ?", mt.ID).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.MillesimalImmutableMessage) {
			t.Fatalf("active entries changed: %v", err)
		}
		_, err = db.NewDelete().Table("millesimal_entries").Where("table_id = ?", mt.ID).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.MillesimalImmutableMessage) {
			t.Fatalf("active entries deleted: %v", err)
		}
	})
}

// commonSetup: a table over A1..A4 (250 each), rights on A1 (one holder) and A2 (333/667), A3 and A4 the marina's;
// an approved 2027 budget with two items.
func (e *testEnv) commonSetup(t *testing.T) (budget *Budget, items []BudgetItem, c2 string) {
	t.Helper()
	b := e.Berths
	c2 = e.AddCustomer(t, "Bianchi")
	e.right(t, b[0], "2027-01-01", "2027-12-31", e.one(e.Customer), "")
	e.right(t, b[1], "2027-01-01", "2027-12-31", []allot.Share{{CustomerID: e.Customer, Millis: 333}, {CustomerID: c2, Millis: 667}}, "")
	mt := e.table(t, "GEN", map[string]int64{b[0]: 250, b[1]: 250, b[2]: 250, b[3]: 250})
	if _, err := e.Rights.ActivateMillesimalTable(ctx, e.Owner, e.MarinaID, mt.ID, mt.Version); err != nil {
		t.Fatal(err)
	}
	bud, err := e.Rights.CreateBudget(ctx, e.Owner, e.MarinaID, 2027, "Bilancio 2027")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range []struct {
		name   string
		amount int64
	}{{"Pulizia", 100_001}, {"Luce", 7}} {
		name, tid, amount := it.name, mt.ID, it.amount
		if _, err := e.Rights.CreateBudgetItem(ctx, e.Owner, e.MarinaID, bud.ID, ItemInput{Name: &name, TableID: &tid, BudgetCents: &amount}); err != nil {
			t.Fatal(err)
		}
	}
	bud, err = e.Rights.ApproveBudget(ctx, e.Owner, e.MarinaID, bud.ID, bud.Version)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := e.Rights.ListBudgetItems(ctx, e.Owner, e.MarinaID, bud.ID)
	if len(list) == 2 && list[0].Name != "Pulizia" {
		list[0], list[1] = list[1], list[0] // Pulizia first
	}
	return bud, list, c2
}

func (e *testEnv) allocSum(t *testing.T, runID string) (map[string]int64, int64) {
	t.Helper()
	rows, err := store.AllocationsOfRun(context.Background(), e.DB.DB, e.scope(t), runID, store.AllocationFilterCommon{})
	if err != nil {
		t.Fatal(err)
	}
	per := map[string]int64{}
	var total int64
	for _, a := range rows {
		per[a.ItemID] += a.AmountCents
		total += a.AmountCents
	}
	return per, total
}

func TestCommonCharges_BalanceEqualsActualMinusAdvances(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		bud, items, _ := e.commonSetup(t)
		share := int64(5000)
		adv, err := e.Rights.CreateCommonRun(ctx, e.Owner, e.MarinaID, bud.ID, CommonRunInput{Kind: RunAdvance, ShareBP: &share})
		if err != nil {
			t.Fatal(err)
		}
		per, total := e.allocSum(t, adv.ID)
		// 100 001 × 0.5 = 50 000.5 → 50 000 (half-even), 7 × 0.5 = 3.5 → 4
		if per[items[0].ID] != 50_000 || per[items[1].ID] != 4 || total != 50_004 || adv.Totals.TotalCents != 50_004 {
			t.Fatalf("advance per item %v total %d", per, total)
		}
		if _, err := e.Rights.IssueCommonRun(ctx, e.Owner, e.MarinaID, adv.ID, adv.Version); err != nil {
			t.Fatal(err)
		}
		inst := e.holderRows(t, store.HolderInstallmentFilter{RunID: adv.ID})
		var marina int64
		for _, x := range adv.Totals.Items {
			marina += x.MarinaShareCents
		}
		if sumOf(inst) != total-marina {
			t.Fatalf("holder instalments Σ %d, holders' share %d", sumOf(inst), total-marina)
		}
		// allocations of an issued run are frozen (INV-44)
		_, err = db.NewUpdate().Table("common_charge_allocations").Set("amount_cents = 0").Where("run_id = ?", adv.ID).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.AllocationsImmutableMessage) {
			t.Fatalf("issued allocations changed: %v", err)
		}
		// actuals: the first item lower than its advance → credits
		bud, _ = e.Rights.GetBudget(ctx, e.Owner, e.MarinaID, bud.ID)
		if _, err := e.Rights.SetBudgetActuals(ctx, e.Owner, e.MarinaID, bud.ID, bud.Version, nil, strPtr("voce;importo\nPulizia;400,00\nLuce;0,11\n")); err != nil {
			t.Fatal(err)
		}
		bal, err := e.Rights.CreateCommonRun(ctx, e.Owner, e.MarinaID, bud.ID, CommonRunInput{Kind: RunBalance})
		if err != nil {
			t.Fatal(err)
		}
		per, total = e.allocSum(t, bal.ID)
		if per[items[0].ID] != 40_000-50_000 || per[items[1].ID] != 11-4 || total != (40_000+11)-(50_000+4) {
			t.Fatalf("balance per item %v total %d", per, total)
		}
		if _, err := e.Rights.IssueCommonRun(ctx, e.Owner, e.MarinaID, bal.ID, bal.Version); err != nil {
			t.Fatal(err)
		}
		credits := e.holderRows(t, store.HolderInstallmentFilter{RunID: bal.ID})
		if len(credits) == 0 {
			t.Fatal("no balance instalments")
		}
		for _, c := range credits {
			if c.AmountCents >= 0 || c.Kind != "credit" || c.SourceType != SourceCommonBalance {
				t.Fatalf("balance row %+v", c)
			}
		}
		// a second balance run cannot be issued (partial unique, INV-44)
		bal2, err := e.Rights.CreateCommonRun(ctx, e.Owner, e.MarinaID, bud.ID, CommonRunInput{Kind: RunBalance})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Rights.IssueCommonRun(ctx, e.Owner, e.MarinaID, bal2.ID, bal2.Version); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second balance: %v", err)
		}
		hs, err := e.Rights.HolderStatements(ctx, e.Owner, e.MarinaID, adv.ID, "")
		if err != nil || len(hs) != 2 {
			t.Fatalf("statements %d %v", len(hs), err)
		}
		var st int64
		for _, h := range hs {
			st += int64(h.Total)
		}
		if st != total0(adv)-marina {
			t.Fatalf("statements Σ %d", st)
		}
		if n := len(e.Events(t, event.CommonChargesIssued)); n != 2 {
			t.Fatalf("issued events %d", n)
		}
	})
}

func total0(r *CommonRun) int64 { return r.Totals.TotalCents }

func strPtr(s string) *string { return &s }

// TestRight_HolderSharesWritePaths (INV-40): creation, update of the holders, refused updates and a transfer keep
// Σ share_millis = 1000 on every active right (C40); a corrupted right is reported by C40.
func TestRight_HolderSharesWritePaths(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		c2, c3 := e.AddCustomer(t, "Bianchi"), e.AddCustomer(t, "Verdi")
		r := e.right(t, e.Berths[0], "2027-01-01", "2027-12-31", []allot.Share{{CustomerID: e.Customer, Millis: 600}, {CustomerID: c2, Millis: 400}}, e.Vessel)
		e.sharesInvariant(t)
		up, err := e.Rights.UpdateRight(ctx, e.Owner, e.MarinaID, r.ID, r.Version, RightPatch{
			Holders: []allot.Share{{CustomerID: e.Customer, Millis: 334}, {CustomerID: c2, Millis: 333}, {CustomerID: c3, Millis: 333}},
		})
		if err != nil {
			t.Fatal(err)
		}
		e.sharesInvariant(t)
		if _, err := e.Rights.UpdateRight(ctx, e.Owner, e.MarinaID, r.ID, up.Version, RightPatch{
			Holders: []allot.Share{{CustomerID: e.Customer, Millis: 500}, {CustomerID: c2, Millis: 499}},
		}); err == nil {
			t.Fatal("shares summing to 999 accepted")
		}
		e.sharesInvariant(t)
		tr, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{
			RightID: r.ID, EffectiveOn: d("2027-09-01"), ToHolders: []allot.Share{{CustomerID: c3, Millis: 1000}}, FeesPolicy: FeesReassign,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Rights.CompleteTransfer(ctx, e.Owner, e.MarinaID, tr.ID, tr.Version); err != nil {
			t.Fatal(err)
		}
		e.sharesInvariant(t)
		// The query itself: a holder row removed behind the service is reported.
		if _, err := db.NewDelete().TableExpr("berth_right_holders").Where("right_id = ?", r.ID).Where("customer_id = ?", c2).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().TableExpr("berth_rights").Set("status = 'active'").Where("id = ?", r.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		vs, err := store.CheckContractInvariants(context.Background(), db.DB, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range vs {
			found = found || (v.Query == "C40" && len(v.IDs) == 1 && v.IDs[0] == r.ID)
		}
		if !found {
			t.Fatalf("C40 did not report the corrupted right: %+v", vs)
		}
	})
}

// TestInvariants_C31hHolderInstallments (INV-31, holder clause): after issuing a fee run and a common charge
// advance the live holder instalments of every fee line and (common run, customer) pair sum to their line, and C31h
// reports an instalment changed or cancelled behind the services.
func TestInvariants_C31hHolderInstallments(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		bud, _, _ := e.commonSetup(t)
		share := int64(5000)
		adv, err := e.Rights.CreateCommonRun(ctx, e.Owner, e.MarinaID, bud.ID, CommonRunInput{Kind: RunAdvance, ShareBP: &share})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Rights.IssueCommonRun(ctx, e.Owner, e.MarinaID, adv.ID, adv.Version); err != nil {
			t.Fatal(err)
		}
		run := e.issueFees(t, e.feeTable(t, 2027).ID, semesters)
		e.sharesInvariant(t)

		c31h := func() []store.Violation {
			t.Helper()
			vs, err := store.CheckContractInvariants(context.Background(), db.DB, e.MarinaID)
			if err != nil {
				t.Fatal(err)
			}
			var out []store.Violation
			for _, v := range vs {
				if v.Query == "C31h" {
					out = append(out, v)
				}
			}
			return out
		}
		fee := e.holderRows(t, store.HolderInstallmentFilter{RunID: run.ID})[0]
		if _, err := db.NewUpdate().TableExpr("holder_installments").Set("amount_cents = amount_cents + 1").Where("id = ?", fee.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if vs := c31h(); len(vs) != 1 || vs[0].IDs[0] != fee.SourceID {
			t.Fatalf("fee line drift: %+v", vs)
		}
		common := e.holderRows(t, store.HolderInstallmentFilter{RunID: adv.ID})[0]
		if _, err := db.NewUpdate().TableExpr("holder_installments").Set("status = 'cancelled'").Where("id = ?", common.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		vs := c31h()
		found := false
		for _, v := range vs {
			found = found || (len(v.IDs) == 2 && v.IDs[0] == adv.ID && v.IDs[1] == common.CustomerID)
		}
		if len(vs) != 2 || !found {
			t.Fatalf("common run drift: %+v", vs)
		}
	})
}
