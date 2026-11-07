//go:build integration

package payment

import (
	"context"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestInstallment_SettleOnlyManualDriver (updated for M5, INV-32): an instalment becomes paid only through the
// matching of its charge (settlement_source payment); partial payments make it partially_paid, a reversal brings
// it back to issued.
func TestInstallment_SettleOnlyManualDriver(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		_, plan := e.mooring(t)
		inst := func(id string) *store.Installment {
			r, err := store.GetInstallment(context.Background(), e.DB.DB, e.scope, id)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		first := plan.Items[0].ID
		ch := e.chargeInstallment(t, first)
		half := money.Cents(ch.TotalCents / 2)
		p := e.record(t, RecordInput{Amount: half, Matches: []MatchInput{{ChargeID: ch.ID, Amount: half}}})
		if r := inst(first); r.Status != store.InstallmentPartiallyPaid {
			t.Fatalf("after half: %s", r.Status)
		}
		rest := money.Cents(ch.TotalCents) - half
		e.record(t, RecordInput{Amount: rest, AutoMatch: AutoMatchOldestDueFirst})
		r := inst(first)
		if r.Status != store.InstallmentPaid || r.SettlementSource != store.SettlementPayment || r.SettlementMethod != "bank_transfer" {
			t.Fatalf("after rest: %s %s %s", r.Status, r.SettlementSource, r.SettlementMethod)
		}
		if _, err := e.svc.ReverseMatch(ctx, e.Owner, e.MarinaID, p.ID, p.Matches[0].ID); err != nil {
			t.Fatal(err)
		}
		if r := inst(first); r.Status != store.InstallmentPartiallyPaid || r.SettlementSource != "" {
			t.Fatalf("after reversal: %s %s", r.Status, r.SettlementSource)
		}
		// The M4 settle route now records a payment for the residual.
		row, err := e.Contracts.SettleInstallment(ctx, e.Owner, e.MarinaID, first, 0, contract.SettleInput{Method: "cheque", Reference: "ASS 99"})
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != string(store.InstallmentPaid) {
			t.Fatalf("settle route: %s", row.Status)
		}
		if r := inst(first); r.SettlementSource != store.SettlementPayment || r.SettlementMethod != "cheque" {
			t.Fatalf("settle route source %s method %s", r.SettlementSource, r.SettlementMethod)
		}
		if c := e.charge(t, ch.ID); c.PaymentStatus != store.ChargePaid {
			t.Fatalf("charge %s", c.PaymentStatus)
		}
		// A scheduled instalment is issued (charge created) and settled in the same transaction.
		second := plan.Items[1].ID
		if inst(second).Status == store.InstallmentScheduled && e.charges.ContractHooks() != nil {
			if _, err := e.Contracts.SettleInstallment(ctx, e.Owner, e.MarinaID, second, 0, contract.SettleInput{Method: "bank_transfer"}); err != nil {
				t.Fatal(err)
			}
			if r := inst(second); r.Status != store.InstallmentPaid || r.ChargeID == "" || r.SettlementSource != store.SettlementPayment {
				t.Fatalf("scheduled settle %+v", r)
			}
		}
		e.invariants(t)
		vs, err := store.CheckContractInvariants(context.Background(), e.DB.DB, e.MarinaID)
		if err != nil || len(vs) != 0 {
			t.Fatalf("contract invariants %+v %v", vs, err)
		}
	})
}

func TestTransit_DeskConfirmWithPayment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		tr := e.Transit(t, e.Berths[1], "2027-06-02", "2027-06-05", false)
		pay, err := e.svc.ConfirmTransitWithPayment(ctx, e.Owner, e.MarinaID, tr.Row.ID, tr.Row.Version, TransitPayment{Method: "card_pos"})
		if err != nil {
			t.Fatal(err)
		}
		c, err := store.GetContract(context.Background(), e.DB.DB, e.scope, tr.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if c.Status != "confirmed" || c.ConfirmationSource != contract.SourceManual {
			t.Fatalf("transit %s %s", c.Status, c.ConfirmationSource)
		}
		open, err := store.LedgerOpenChargesOfContract(context.Background(), e.DB.DB, e.scope, tr.Row.ID)
		if err != nil || len(open) != 0 {
			t.Fatalf("open charges %d %v", len(open), err)
		}
		if pay.AllocatedCents != pay.AmountCents || pay.AmountCents == 0 {
			t.Fatalf("payment %+v", pay)
		}
		// A second confirmation finds nothing to collect.
		if _, err := e.svc.ConfirmTransitWithPayment(ctx, e.Owner, e.MarinaID, tr.Row.ID, 0, TransitPayment{Method: "cash"}); err == nil {
			t.Fatal("second desk confirmation collected again")
		}
		e.invariants(t)
	})
}

func TestMapPaymentStatus_States(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		today := calendar.In(e.Clock.Now(), nil)
		tr := e.Transit(t, e.Berths[2], today.String(), today.AddDays(3).String(), true)
		state := func() (string, *money.Cents) {
			_, rs, err := e.svc.MapPaymentStatus(ctx, e.Owner, e.MarinaID, e.MapID, today)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rs {
				if r.ResourceID == e.Berths[2] {
					return r.State, r.Residual
				}
			}
			t.Fatalf("berth not in the lens: %+v", rs)
			return "", nil
		}
		// Pay whatever the billing hook charged, so the transit starts clean.
		if open, _ := store.LedgerOpenChargesOfContract(context.Background(), e.DB.DB, e.scope, tr.Row.ID); len(open) > 0 {
			var due money.Cents
			for _, c := range open {
				due += money.Cents(c.ResidualCents())
			}
			e.record(t, RecordInput{Amount: due, AutoMatch: AutoMatchOldestDueFirst})
		}
		if s, _ := state(); s != LensOK {
			t.Fatalf("clean: %s", s)
		}
		late := e.addCharge(t, chargeOpts{contractID: tr.Row.ID, due: today.AddDays(-70)}, line{12_00, 2200})
		s, res := state()
		if s != LensOverdue60 || res == nil || *res != 12_00 {
			t.Fatalf("overdue 60: %s %v", s, res)
		}
		// The dock sees the state, never the amount.
		dock := e.AddUser(t, "dock@porto.test", "dock", e.OrgID, e.MarinaID)
		_, rs, err := e.svc.MapPaymentStatus(ctx, dock, e.MarinaID, e.MapID, today)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if r.Residual != nil {
				t.Fatal("amount leaked to the dock")
			}
		}
		now := store.UTC(e.Clock.Now())
		e.Insert(t, &store.DunningExclusion{ID: store.NewID(), MarinaID: e.MarinaID, ChargeID: late.ID, Kind: "disputed", CreatedAt: now, UpdatedAt: now})
		if s, _ := state(); s != LensDisputed {
			t.Fatalf("disputed: %s", s)
		}
	})
}
