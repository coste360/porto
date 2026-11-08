//go:build integration

package payment

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestConcurrency_PaymentMatching (INV-52): one payment of 100,00 matched by 100 goroutines to 10 charges of
// 30,00, then 100 payments matching the same charge: the sums never exceed the payment nor the charge and the
// losers get match_exceeds.
func TestConcurrency_PaymentMatching(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		var charges []string
		for range 10 {
			charges = append(charges, e.addCharge(t, chargeOpts{}, line{30_00, 2200}).ID)
		}
		p := e.record(t, RecordInput{Amount: 100_00})
		var wg sync.WaitGroup
		var ok, exceeded atomic.Int64
		var matched atomic.Int64
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				amount := money.Cents(100 * (1 + i%5)) // 1,00 .. 5,00
				_, err := e.svc.Match(ctx, e.Owner, e.MarinaID, p.ID, []MatchInput{{ChargeID: charges[i%10], Amount: amount}})
				switch {
				case err == nil:
					ok.Add(1)
					matched.Add(int64(amount))
				case errors.Is(err, ErrMatchExceeds):
					exceeded.Add(1)
				default:
					t.Errorf("match: %v", err)
				}
			}()
		}
		wg.Wait()
		pp := e.payment(t, p.ID)
		if pp.AllocatedCents != matched.Load() || pp.AllocatedCents > 100_00 {
			t.Fatalf("allocated %d, matched %d", pp.AllocatedCents, matched.Load())
		}
		if ok.Load()+exceeded.Load() != 100 || ok.Load() == 0 {
			t.Fatalf("ok %d exceeded %d", ok.Load(), exceeded.Load())
		}
		var paid int64
		for _, id := range charges {
			c := e.charge(t, id)
			if c.PaidAmountCents > c.TotalCents {
				t.Fatalf("charge %s paid %d > %d", id, c.PaidAmountCents, c.TotalCents)
			}
			paid += c.PaidAmountCents
		}
		if paid != pp.AllocatedCents {
			t.Fatalf("Σ paid %d ≠ allocated %d", paid, pp.AllocatedCents)
		}

		// 100 payments matching the same charge of 30,00.
		target := e.addCharge(t, chargeOpts{}, line{30_00, 2200})
		pays := make([]string, 100)
		for i := range pays {
			pays[i] = e.record(t, RecordInput{Amount: 1_00}).ID
		}
		ok.Store(0)
		exceeded.Store(0)
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.Match(ctx, e.Owner, e.MarinaID, pays[i], []MatchInput{{ChargeID: target.ID, Amount: 1_00}})
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrMatchExceeds):
					exceeded.Add(1)
				default:
					t.Errorf("match: %v", err)
				}
			}()
		}
		wg.Wait()
		if c := e.charge(t, target.ID); c.PaidAmountCents != 30_00 || c.PaymentStatus != store.ChargePaid || ok.Load() != 30 || exceeded.Load() != 70 {
			t.Fatalf("target %+v ok %d exceeded %d", c, ok.Load(), exceeded.Load())
		}
		e.invariants(t)
	})
}

// TestConcurrency_RefundVsMatch: refunds and matches on the same payment in parallel never exceed its amount.
func TestConcurrency_RefundVsMatch(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		var charges []string
		for range 5 {
			charges = append(charges, e.addCharge(t, chargeOpts{}, line{50_00, 2200}).ID)
		}
		p := e.record(t, RecordInput{Amount: 100_00})
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					_, err = e.svc.Refund(ctx, e.Owner, e.MarinaID, p.ID, RefundInput{Amount: 3_00, Reason: "test"})
					if errors.Is(err, ErrRefundExceeds) {
						err = nil
					}
				} else {
					_, err = e.svc.Match(ctx, e.Owner, e.MarinaID, p.ID, []MatchInput{{ChargeID: charges[i%5], Amount: 3_00}})
					if errors.Is(err, ErrMatchExceeds) {
						err = nil
					}
				}
				if err != nil {
					t.Errorf("op %d: %v", i, err)
				}
			}()
		}
		wg.Wait()
		pp := e.payment(t, p.ID)
		if pp.AllocatedCents+pp.RefundedCents > pp.AmountCents {
			t.Fatalf("allocated %d + refunded %d > %d", pp.AllocatedCents, pp.RefundedCents, pp.AmountCents)
		}
		if pp.AllocatedCents+pp.RefundedCents != 99_00 {
			t.Fatalf("the payment should be used up to 99,00: %d", pp.AllocatedCents+pp.RefundedCents)
		}
		e.invariants(t)
	})
}

// TestConcurrency_CreditApply: two staff (100 attempts) applying the same credit never use it twice.
func TestConcurrency_CreditApply(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		e.record(t, RecordInput{Amount: 50_00}) // credit, nothing to match yet
		for range 4 {
			e.addCharge(t, chargeOpts{}, line{20_00, 2200})
		}
		var wg sync.WaitGroup
		var total atomic.Int64
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := e.svc.ApplyCredit(ctx, e.Owner, e.MarinaID, e.Customer, nil)
				if err != nil && !errors.Is(err, ErrCreditInsufficient) {
					t.Errorf("apply: %v", err)
				}
				total.Add(int64(got))
			}()
		}
		wg.Wait()
		if total.Load() != 50_00 {
			t.Fatalf("applied %d", total.Load())
		}
		e.invariants(t)
	})
}

// TestConcurrency_InstallmentSettle (M4 name, rewired): 100 parallel settlements of the same instalment create
// one payment and settle it once, through the payment (settlement_source payment).
func TestConcurrency_InstallmentSettle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		_, plan := e.mooring(t)
		id := plan.Items[0].ID
		ch := e.chargeInstallment(t, id)
		var wg sync.WaitGroup
		var ok atomic.Int32
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					_, err = e.Contracts.SettleInstallment(ctx, e.Owner, e.MarinaID, id, 0, contract.SettleInput{Method: "bank_transfer"})
				} else {
					_, err = e.Contracts.SettleInstallments(ctx, e.Owner, e.MarinaID, []contract.SettleItem{{ID: id}}, contract.SettleInput{Method: "bank_transfer"})
				}
				if err == nil {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 {
			t.Fatalf("successes %d", ok.Load())
		}
		r, err := store.GetInstallment(context.Background(), e.DB.DB, e.scope, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != store.InstallmentPaid || r.SettlementSource != store.SettlementPayment {
			t.Fatalf("instalment %s source %s", r.Status, r.SettlementSource)
		}
		if n := e.count(t, "payment_matches", "charge_id = ?", ch.ID); n != 1 {
			t.Fatalf("matches %d", n)
		}
		if n := e.count(t, "payments", "marina_id = ?", e.MarinaID); n != 1 {
			t.Fatalf("payments %d", n)
		}
		e.invariants(t)
	})
}

// TestConcurrency_PartialReversals (INV-52, 00017): 100 goroutines refund 3,00 each of the same match of 100,00
// (partial reversals): exactly 33 succeed, Σ reversals = reversed_cents = 99,00, never above the match; then 100
// raw conditional updates on another match prove the DB bound alone (no service lock) holds.
func TestConcurrency_PartialReversals(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		ch := e.addCharge(t, chargeOpts{}, line{100_00, 2200})
		p := e.record(t, RecordInput{Amount: 100_00, AutoMatch: AutoMatchOldestDueFirst})
		if len(p.Matches) != 1 || p.Matches[0].AmountCents != 100_00 {
			t.Fatalf("matches %+v", p.Matches)
		}
		matchID := p.Matches[0].ID
		var wg sync.WaitGroup
		var ok, refused atomic.Int64
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.Refund(ctx, e.Owner, e.MarinaID, p.ID, RefundInput{
					Amount: 3_00, Reason: "test", Unmatch: []UnmatchInput{{MatchID: matchID, Amount: 3_00}},
				})
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrAlreadyReversed), errors.Is(err, auth.ErrInvalidInput):
					refused.Add(1)
				default:
					t.Errorf("refund: %v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 33 || refused.Load() != 67 {
			t.Fatalf("ok %d refused %d", ok.Load(), refused.Load())
		}
		m, err := store.GetPaymentMatch(ctx, db.DB, e.scope, matchID)
		if err != nil || m.ReversedCents != 99_00 {
			t.Fatalf("reversed %+v %v", m, err)
		}
		if c := e.charge(t, ch.ID); c.PaidAmountCents != 1_00 {
			t.Fatalf("charge paid %d", c.PaidAmountCents)
		}
		if pp := e.payment(t, p.ID); pp.RefundedCents != 99_00 || pp.AllocatedCents != 1_00 {
			t.Fatalf("payment %+v", pp)
		}
		// The rest (1,00) is still reversible; then nothing is.
		if _, err := e.svc.ReverseMatch(ctx, e.Owner, e.MarinaID, p.ID, matchID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.ReverseMatch(ctx, e.Owner, e.MarinaID, p.ID, matchID); !errors.Is(err, ErrAlreadyReversed) {
			t.Fatalf("over-reversal: %v", err)
		}
		e.invariants(t)

		// The DB bound alone: 100 transactions adding 7,00 to reversed_cents of a fresh match of 100,00.
		// No reversal rows are written here on purpose, so the ledger check (B52r) is not run afterwards.
		ch2 := e.addCharge(t, chargeOpts{}, line{100_00, 0})
		p2 := e.record(t, RecordInput{Amount: 100_00})
		p2d, err := e.svc.Match(ctx, e.Owner, e.MarinaID, p2.ID, []MatchInput{{ChargeID: ch2.ID, Amount: 100_00}})
		if err != nil {
			t.Fatal(err)
		}
		m2 := p2d.Matches[0].ID
		ok.Store(0)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
					_, done, err := store.AddMatchReversed(ctx, tx, e.scope, m2, 7_00)
					if done {
						ok.Add(1)
					}
					return err
				})
				if err != nil {
					t.Errorf("add reversed: %v", err)
				}
			}()
		}
		wg.Wait()
		got, err := store.GetPaymentMatch(ctx, db.DB, e.scope, m2)
		if err != nil || ok.Load() != 14 || got.ReversedCents != 98_00 {
			t.Fatalf("ok %d reversed %+v %v", ok.Load(), got, err)
		}
	})
}
