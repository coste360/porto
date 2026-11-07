//go:build integration

package charge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestInstallment_IssueCreatesCharge: activation issues the deposit, the issue job and the bulk issue issue the
// others; each issued instalment gets exactly one charge (charge_id set once), installment.charged and one payment
// request; when every instalment is issued the charges sum to the contract target (B57).
func TestInstallment_IssueCreatesCharge(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		c := e.mooring(t, e.Berths[0], "2027-06-10", true)
		rows, err := store.InstallmentsOfContracts(ctx, db.DB, e.scope, []string{c.Row.ID}, true)
		if err != nil || len(rows) == 0 {
			t.Fatalf("instalments %d %v", len(rows), err)
		}
		issued := 0
		for _, r := range rows {
			if r.Status == store.InstallmentIssued {
				issued++
				if r.ChargeID == "" {
					t.Fatalf("issued instalment %d without charge", r.Seq)
				}
				ch := e.charge(t, r.ChargeID)
				if ch.OriginType != store.ChargeOriginInstallment || ch.OriginID != r.ID || ch.TotalCents != r.AmountCents ||
					ch.ContractID != c.Row.ID || ch.CustomerID != e.Customer || !ch.DueOn.Equal(r.DueOn) {
					t.Fatalf("charge %+v of instalment %+v", ch, r)
				}
				if e.requests.keys["installment:"+r.ID+":issued"] != 1 {
					t.Fatalf("payment request of instalment %d: %d", r.Seq, e.requests.keys["installment:"+r.ID+":issued"])
				}
			}
		}
		if issued == 0 {
			t.Fatal("activation issued nothing")
		}
		// Issue the rest: the job for one, the bulk issue for the others.
		var ids []string
		for _, r := range rows {
			if r.Status == store.InstallmentScheduled {
				ids = append(ids, r.ID)
			}
		}
		if len(ids) > 0 {
			if _, err := e.contracts.IssueInstallments(ctx, e.Owner, e.MarinaID, ids); err != nil {
				t.Fatal(err)
			}
		}
		rows, _ = store.InstallmentsOfContracts(ctx, db.DB, e.scope, []string{c.Row.ID}, true)
		var sum int64
		for _, r := range rows {
			if r.ChargeID == "" {
				t.Fatalf("instalment %d (%s) without charge", r.Seq, r.Status)
			}
			sum += e.charge(t, r.ChargeID).TotalCents
		}
		got, _ := store.GetContract(ctx, db.DB, e.scope, c.Row.ID)
		if sum != got.TotalCents {
			t.Fatalf("Σ charges %d ≠ total %d", sum, got.TotalCents)
		}
		if n := len(e.Events(t, event.InstallmentCharged)); n != len(rows) {
			t.Fatalf("installment.charged %d, want %d", n, len(rows))
		}
		// No true-up: the instalments sum to the total.
		if tu := e.charges(t, store.ChargeOriginContractTrueUp, c.Row.ID); len(tu) != 0 {
			t.Fatalf("unexpected true-up %+v", tu)
		}
		e.invariants(t)
	})
}

// TestConcurrency_IssueSameInstallment (INV-54): 100 parallel issues of the same instalment create one charge.
func TestConcurrency_IssueSameInstallment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		// Not activated: the single-payment instalment stays scheduled until the parallel issues below.
		c := e.mooring(t, e.Berths[1], "2027-06-10", false)
		rows, _ := store.InstallmentsOfContracts(ctx, db.DB, e.scope, []string{c.Row.ID}, true)
		target := ""
		for _, r := range rows {
			if r.Status == store.InstallmentScheduled {
				target = r.ID
				break
			}
		}
		if target == "" {
			t.Fatal("no scheduled instalment")
		}
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = e.contracts.IssueInstallments(ctx, e.Owner, e.MarinaID, []string{target})
			}()
		}
		wg.Wait()
		cs := e.charges(t, store.ChargeOriginInstallment, target)
		if len(cs) != 1 {
			t.Fatalf("%d charges for one instalment", len(cs))
		}
		if e.requests.keys["installment:"+target+":issued"] != 1 {
			t.Fatalf("payment requests %d", e.requests.keys["installment:"+target+":issued"])
		}
		e.invariants(t)
	})
}

// TestCharge_OneLivePrimaryPerOrigin (INV-54): a second live primary charge of an origin is refused by the
// database; a correction or a cancelled primary does not count.
func TestCharge_OneLivePrimaryPerOrigin(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		origin := store.NewID()
		in := CreateInput{
			OriginType: store.ChargeOriginTermination, OriginID: origin, CustomerID: e.Customer,
			Lines: []LineInput{{ItemID: e.items["PENALE"], ItemCode: "PENALE", Net: 1000, Gross: 1000, VATNature: "N1", Qty: 1000}},
		}
		var first *store.Charge
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			var err error
			first, _, err = e.svc.CreateTx(ctx, tx, e.scope, audit.System, in)
			return err
		})
		err := e.DB.RunInTx(context.Background(), func(ctx context.Context, tx bun.Tx) error {
			_, _, err := e.svc.CreateTx(ctx, tx, e.scope, audit.System, in)
			return err
		})
		if !errors.Is(err, ErrChargeExists) {
			t.Fatalf("second primary: %v", err)
		}
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			ok, err := e.svc.cancelIfPossibleTx(ctx, tx, e.scope, audit.System, first, "test")
			if !ok {
				t.Fatal("not cancelled")
			}
			return err
		})
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			_, _, err := e.svc.CreateTx(ctx, tx, e.scope, audit.System, in)
			return err
		})
		e.invariants(t)
	})
}

// TestTransit_ConfirmCreatesCharge: confirmation charges the whole stay (due on arrival, or on departure with pay
// at departure); a change of stay before the export rewrites the charge, after the export adds a linked delta;
// cancelling after confirmation reverses what was charged.
func TestTransit_ConfirmCreatesCharge(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		tr := e.transit(t, e.Berths[0], "2027-06-05", "2027-06-08", true)
		cs := e.charges(t, store.ChargeOriginTransit, tr.Row.ID)
		if len(cs) != 1 || cs[0].TotalCents != tr.Row.TotalCents || !cs[0].DueOn.Equal(contracttest.D("2027-06-05")) {
			t.Fatalf("transit charges %+v (total %d)", cs, tr.Row.TotalCents)
		}
		first := cs[0]
		// Change of stay before the export: the charge is rewritten in place.
		dep := contracttest.D("2027-06-10")
		cur, _ := e.contracts.GetContract(ctx, e.Owner, e.MarinaID, tr.Row.ID)
		got, _, err := e.contracts.ChangeStay(ctx, e.Owner, e.MarinaID, tr.Row.ID, cur.Row.Version, contract.StayChange{Departure: &dep})
		if err != nil {
			t.Fatal(err)
		}
		cs = e.charges(t, store.ChargeOriginTransit, tr.Row.ID)
		if len(cs) != 1 || cs[0].ID != first.ID || cs[0].TotalCents != got.Row.TotalCents || cs[0].Version != first.Version+1 {
			t.Fatalf("after change of stay: %+v, total %d", cs, got.Row.TotalCents)
		}
		// Export, then change again: a linked delta adjustment.
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, ExportFilter{PeriodFrom: contracttest.D("2027-06-01"), PeriodTo: contracttest.D("2027-06-30")}, ""); err != nil {
			t.Fatal(err)
		}
		dep = contracttest.D("2027-06-12")
		got, _, err = e.contracts.ChangeStay(ctx, e.Owner, e.MarinaID, tr.Row.ID, got.Row.Version, contract.StayChange{Departure: &dep})
		if err != nil {
			t.Fatal(err)
		}
		cs = e.charges(t, store.ChargeOriginTransit, tr.Row.ID)
		if len(cs) != 2 || cs[1].OriginType != store.ChargeOriginAdjustment || cs[1].CorrectsChargeID != first.ID ||
			cs[0].TotalCents+cs[1].TotalCents != got.Row.TotalCents {
			t.Fatalf("after export and change: %+v total %d", cs, got.Row.TotalCents)
		}
		e.invariants(t)
		// Cancel after confirmation: the charged amount is reversed (offset to the residual, the rest credit).
		if _, err := e.contracts.CancelTransit(ctx, e.Owner, e.MarinaID, tr.Row.ID, got.Row.Version, "meteo"); err != nil {
			t.Fatal(err)
		}
		cs = e.charges(t, store.ChargeOriginTransit, tr.Row.ID)
		var sum int64
		for _, c := range cs {
			sum += c.TotalCents
		}
		if sum != 0 || len(cs) != 3 || cs[2].TotalCents >= 0 {
			t.Fatalf("after cancel: %+v Σ %d", cs, sum)
		}
		e.invariants(t)

		// Pay at departure: due on the departure day.
		in := contract.TransitInput{
			VesselID: e.Vessel, CustomerID: e.Customer, Arrival: contracttest.D("2027-06-20"), Departure: contracttest.D("2027-06-23"),
			Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[2], "2027-06-20", "2027-06-22")},
			Confirm:    &contract.ManualConfirmation{Reference: "x", PayAtDeparture: true},
		}
		pd, err := e.contracts.CreateTransit(ctx, e.Owner, e.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		cs = e.charges(t, store.ChargeOriginTransit, pd.Row.ID)
		if len(cs) != 1 || !cs[0].DueOn.Equal(contracttest.D("2027-06-23")) {
			t.Fatalf("pay at departure: %+v", cs)
		}
	})
}

// TestContract_TrueUpAfterLastInstalment: a reprice after the last instalment is charged by a true-up linked to
// the latest charge; Σ charges per line and per (rate, nature) equals the target.
func TestContract_TrueUpAfterLastInstalment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		c := e.mooring(t, e.Berths[2], "2027-06-10", true)
		rows, _ := store.InstallmentsOfContracts(ctx, db.DB, e.scope, []string{c.Row.ID}, true)
		var ids []string
		for _, r := range rows {
			if r.Status == store.InstallmentScheduled {
				ids = append(ids, r.ID)
			}
		}
		if len(ids) > 0 {
			if _, err := e.contracts.IssueInstallments(ctx, e.Owner, e.MarinaID, ids); err != nil {
				t.Fatal(err)
			}
		}
		// A reprice of +123,45 € after every instalment is issued (an addendum, recorded as the contract does).
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			k, err := store.LockContract(ctx, tx, e.scope, c.Row.ID)
			if err != nil {
				return err
			}
			snap := contract.DecodeSnapshot(k.PriceSnapshot)
			snap.Adjustments = append(snap.Adjustments, contract.PriceAdjustment{
				Kind: contract.AdjustmentAddendum, OldCents: k.TotalCents, NewCents: k.TotalCents + 12345, AddendumID: store.NewID(),
			})
			raw, _ := json.Marshal(snap)
			if _, err := tx.NewUpdate().Model((*store.Contract)(nil)).Set("price_snapshot = ?", string(raw)).
				Set("total_cents = total_cents + 12345").Where("id = ?", k.ID).Exec(ctx); err != nil {
				return err
			}
			k.TotalCents += 12345
			k.PriceSnapshot = raw
			return e.svc.OnContractRepricedTx(ctx, tx, e.scope, audit.System, k, contract.RepriceAddendum)
		})
		tu := e.charges(t, store.ChargeOriginContractTrueUp, c.Row.ID)
		if len(tu) != 1 || tu[0].TotalCents != 12345 || tu[0].CorrectsChargeID == "" {
			t.Fatalf("true-up %+v", tu)
		}
		e.invariants(t)
	})
}

// TestHolderInstallment_IssueCreatesCharge: fees and advances get one line of their origin item, a negative
// common-charge balance is a negative charge turned into credit, a missing origin item creates nothing.
func TestHolderInstallment_IssueCreatesCharge(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		now := store.UTC(e.Clock.Now())
		mk := func(source string, amount int64) *store.HolderInstallment {
			h := &store.HolderInstallment{
				ID: store.NewID(), MarinaID: e.MarinaID, SourceType: source, SourceID: store.NewID(), CustomerID: e.Customer, Seq: 1,
				Kind: "installment", IssueOn: contracttest.D("2027-06-02"), DueOn: contracttest.D("2027-07-02"), AmountCents: amount,
				Status: store.InstallmentIssued, IssuedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if amount < 0 {
				h.Kind = "credit"
			}
			e.Insert(t, h)
			return h
		}
		fee := mk("management_fee", 122000)
		bal := mk("common_balance", -5000)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			if err := e.svc.OnHolderInstallmentIssuedTx(ctx, tx, e.scope, audit.System, fee); err != nil {
				return err
			}
			return e.svc.OnHolderInstallmentIssuedTx(ctx, tx, e.scope, audit.System, bal)
		})
		fc := e.charges(t, store.ChargeOriginHolderInstallment, fee.ID)
		if len(fc) != 1 || fc[0].TotalCents != 122000 || fc[0].VATCents != 22000 {
			t.Fatalf("fee charge %+v", fc)
		}
		bc := e.charges(t, store.ChargeOriginHolderInstallment, bal.ID)
		if len(bc) != 1 || bc[0].TotalCents != -5000 || bc[0].PaymentStatus != store.ChargePaid || e.credits.grants[bc[0].ID] != 5000 {
			t.Fatalf("balance charge %+v credit %d", bc, e.credits.grants[bc[0].ID])
		}
		if e.requests.keys["holder_installment:"+fee.ID+":issued"] != 1 || e.requests.keys["holder_installment:"+bal.ID+":issued"] != 0 {
			t.Fatalf("requests %+v", e.requests.keys)
		}
		// Without the origin item: no charge, the backfill creates it once the item is configured.
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			return store.ReplaceOriginItems(ctx, tx, e.scope, map[string]string{}, now)
		})
		adv := mk("common_advance", 3000)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			return e.svc.OnHolderInstallmentIssuedTx(ctx, tx, e.scope, audit.System, adv)
		})
		if cs := e.charges(t, store.ChargeOriginHolderInstallment, adv.ID); len(cs) != 0 {
			t.Fatalf("charge without origin item %+v", cs)
		}
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			return store.ReplaceOriginItems(ctx, tx, e.scope, map[string]string{"common_advance": e.items["ONERI"]}, now)
		})
		if r, err := e.svc.BackfillMarina(context.Background(), e.scope); err != nil || r.Charges != 1 {
			t.Fatalf("backfill %+v %v", r, err)
		}
		if cs := e.charges(t, store.ChargeOriginHolderInstallment, adv.ID); len(cs) != 1 {
			t.Fatalf("backfilled %+v", cs)
		}
		e.invariants(t)
	})
}

// TestCharge_BackfillM4Idempotent: M4 instalments issued and paid without charges get them once (paid ones with
// their historical payment); a second run creates nothing.
func TestCharge_BackfillM4Idempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		// M4 data: a contract service without billing hooks.
		s, _ := contract.DefaultEndsOn(contract.TermAnnual, contracttest.D("2027-06-10"))
		c, err := e.Contracts.CreateContract(ctx, e.Owner, e.MarinaID, contract.MooringInput{
			Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: e.Vessel, StartsOn: contracttest.D("2027-06-10"),
			Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[3], "2027-06-10", s.AddDays(-1).String())}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := store.InstallmentsOfContracts(ctx, db.DB, e.scope, []string{c.Row.ID}, true)
		var paid string
		for _, r := range rows {
			if r.Status == store.InstallmentIssued && r.ChargeID == "" {
				paid = r.ID
				ok, err := store.SettleInstallmentRow(ctx, db.DB, e.scope, r.ID, 0, store.Settlement{
					Source: "manual", Method: "bank_transfer", SettledOn: contracttest.D("2027-06-03"), At: time.Now(),
				})
				if err != nil || !ok {
					t.Fatalf("settle %v %v", ok, err)
				}
				break
			}
		}
		if paid == "" {
			t.Fatal("no issued M4 instalment")
		}
		r, err := e.svc.BackfillMarina(context.Background(), e.scope)
		if err != nil || r.Charges == 0 || r.Payments != 1 {
			t.Fatalf("backfill %+v %v", r, err)
		}
		inst, _ := store.GetInstallment(ctx, db.DB, e.scope, paid)
		if inst.ChargeID == "" || e.credits.backfill[inst.ChargeID].Method != "bank_transfer" {
			t.Fatalf("paid instalment %+v backfill %+v", inst, e.credits.backfill)
		}
		again, err := e.svc.BackfillMarina(context.Background(), e.scope)
		if err != nil || again.Charges != 0 || again.Payments != 0 {
			t.Fatalf("second run %+v %v", again, err)
		}
		e.invariants(t)
	})
}

// TestBilling_SkippedChargeRaisesWorkItem (m5-plan §3.3, §16 D26): an instalment whose contract target does not
// match its total, an instalment of a contract without snapshot and without the fallback item, and a holder
// instalment without origin item get no charge, and each raises its worklist item (billing_target_mismatch or
// billing_item_missing, source = the instalment) in the same transaction.
func TestBilling_SkippedChargeRaisesWorkItem(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		now := store.UTC(e.Clock.Now())
		scheduled := func(c *contract.Detail) string {
			t.Helper()
			rows, _ := store.InstallmentsOfContracts(ctx, db.DB, e.scope, []string{c.Row.ID}, true)
			for _, r := range rows {
				if r.Status == store.InstallmentScheduled && r.AmountCents > 0 {
					return r.ID
				}
			}
			t.Fatal("no scheduled instalment")
			return ""
		}
		wantItem := func(kind, sourceID string) {
			t.Helper()
			st, err := store.GetWorkItemState(context.Background(), db.DB, e.scope, kind, sourceID)
			if err != nil || st.Status != store.WorkItemOpen {
				t.Fatalf("work item %s/%s: %+v %v", kind, sourceID, st, err)
			}
		}
		// Target mismatch: the contract total no longer equals its snapshot breakdown.
		m := e.mooring(t, e.Berths[0], "2027-06-10", false)
		inst := scheduled(m)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewUpdate().Model((*store.Contract)(nil)).Set("total_cents = total_cents + 1").Where("id = ?", m.Row.ID).Exec(ctx)
			return err
		})
		if _, err := e.contracts.IssueInstallments(ctx, e.Owner, e.MarinaID, []string{inst}); err != nil {
			t.Fatal(err)
		}
		if cs := e.charges(t, store.ChargeOriginInstallment, inst); len(cs) != 0 {
			t.Fatalf("charge despite the mismatch %+v", cs)
		}
		wantItem("billing_target_mismatch", inst)
		// Item missing: no snapshot and no contract_trueup_fallback item.
		m2 := e.mooring(t, e.Berths[1], "2027-06-10", false)
		inst2 := scheduled(m2)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewUpdate().Model((*store.Contract)(nil)).Set("price_snapshot = NULL").Where("id = ?", m2.Row.ID).Exec(ctx); err != nil {
				return err
			}
			return store.ReplaceOriginItems(ctx, tx, e.scope, map[string]string{}, now)
		})
		if _, err := e.contracts.IssueInstallments(ctx, e.Owner, e.MarinaID, []string{inst2}); err != nil {
			t.Fatal(err)
		}
		if cs := e.charges(t, store.ChargeOriginInstallment, inst2); len(cs) != 0 {
			t.Fatalf("charge without item %+v", cs)
		}
		wantItem("billing_item_missing", inst2)
		// Holder instalment without origin item.
		h := &store.HolderInstallment{
			ID: store.NewID(), MarinaID: e.MarinaID, SourceType: "management_fee", SourceID: store.NewID(), CustomerID: e.Customer, Seq: 1,
			Kind: "installment", IssueOn: contracttest.D("2027-06-02"), DueOn: contracttest.D("2027-07-02"), AmountCents: 1000,
			Status: store.InstallmentIssued, IssuedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.Insert(t, h)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			return e.svc.OnHolderInstallmentIssuedTx(ctx, tx, e.scope, audit.System, h)
		})
		if cs := e.charges(t, store.ChargeOriginHolderInstallment, h.ID); len(cs) != 0 {
			t.Fatalf("holder charge without item %+v", cs)
		}
		wantItem("billing_item_missing", h.ID)
	})
}

// TestSync_CancellingLineDifferencesRaiseMismatch (m5-plan §3.5, §16 D26): when the per-line differences between
// the target and what is charged cancel out (total 0) and the primary charge is no longer editable, no charge can
// carry them: no charge is written and a billing_target_mismatch item is raised on the contract.
func TestSync_CancellingLineDifferencesRaiseMismatch(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		tr := e.transit(t, e.Berths[0], "2027-06-05", "2027-06-08", true)
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, ExportFilter{PeriodFrom: contracttest.D("2027-06-01"), PeriodTo: contracttest.D("2027-06-30")}, ""); err != nil {
			t.Fatal(err)
		}
		before := e.charges(t, store.ChargeOriginTransit, tr.Row.ID)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			c, err := store.GetContract(ctx, tx, e.scope, tr.Row.ID)
			if err != nil {
				return err
			}
			target, err := e.svc.ContractTargetOf(ctx, tx, e.scope, c)
			if err != nil {
				return err
			}
			// Move 1,00 € from the first line to a new line: Σ unchanged, two non-zero line differences.
			target.Lines[0].Gross -= 100
			target.Lines[0].Net -= 100
			extra := target.Lines[0]
			extra.Key, extra.Gross, extra.Net, extra.VAT = "test:extra", 100, 100, 0
			target.Lines = append(target.Lines, extra)
			all, err := store.ChargesOfOrigin(ctx, tx, e.scope, store.ChargeOriginTransit, c.ID, true)
			if err != nil {
				return err
			}
			primary, err := store.LivePrimaryCharge(ctx, tx, e.scope, store.ChargeOriginTransit, c.ID)
			if err != nil {
				return err
			}
			return e.svc.syncToTargetTx(ctx, tx, e.scope, audit.System, c, target, all, primary, "reprice")
		})
		if after := e.charges(t, store.ChargeOriginTransit, tr.Row.ID); len(after) != len(before) {
			t.Fatalf("charges %d → %d", len(before), len(after))
		}
		st, err := store.GetWorkItemState(context.Background(), db.DB, e.scope, "billing_target_mismatch", tr.Row.ID)
		if err != nil || st.Status != store.WorkItemOpen {
			t.Fatalf("billing_target_mismatch item %+v %v", st, err)
		}
	})
}
